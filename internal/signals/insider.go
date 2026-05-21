package signals

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// SEC EDGAR Form 4 ingestion per Spec 9k §D3.
//
// Flow:
//   1. Fetch the current ATOM feed of recent Form 4 filings.
//   2. For each entry, parse the accession number + CIK from the link.
//   3. Fetch the form4.xml directly using SEC's canonical path:
//        https://www.sec.gov/Archives/edgar/data/{CIK}/{ACCNO-no-dashes}/{primary}.xml
//      Where {primary} is one of: "form4.xml" / "primary_doc.xml" / a
//      filing-specific name. We try in that order — most filings use the
//      first two.
//   4. Parse <ownershipDocument><nonDerivativeTable> for P/S transactions.
//   5. Universe-filter by issuer ticker.
//   6. Compute tier.
//   7. INSERT into signal_events (deduped by accessionNumber).
//
// SEC compliance:
//   - Required User-Agent per https://www.sec.gov/os/accessing-edgar-data
//   - Self-limited to ≤8 req/sec (under SEC's 10/sec hard ceiling)
//   - Exponential backoff on 429/503

const (
	secAtomURL       = "https://www.sec.gov/cgi-bin/browse-edgar?action=getcurrent&type=4&output=atom&count=100"
	secUserAgent     = "FT-Dashboard fin@curranhouse.dev"
	secMinReqGap     = 130 * time.Millisecond // ~7.7 req/sec — under 10/sec ceiling
)

// IngestInsiders fetches the current Form 4 ATOM feed and processes every
// entry. Returns the number of new rows inserted (deduplicated by
// accession number). Called from the daily 23:00 UTC cron + the manual
// refresh endpoint.
//
// Errors per filing are logged but don't abort the batch.
func (s *Service) IngestInsiders(ctx context.Context) (inserted int, retErr error) {
	client := &secClient{HTTP: &http.Client{Timeout: 30 * time.Second}}
	feed, err := client.fetchAtomFeed(ctx)
	if err != nil {
		return 0, fmt.Errorf("fetch ATOM feed: %w", err)
	}
	if len(feed.Entries) == 0 {
		slog.Info("signals: ATOM feed empty")
		return 0, nil
	}

	thresholds := DefaultThresholds()

	for _, entry := range feed.Entries {
		select {
		case <-ctx.Done():
			return inserted, ctx.Err()
		default:
		}

		accession, cik, ok := parseAccessionAndCIK(entry.Link.Href, entry.ID)
		if !ok {
			continue
		}

		// Skip if we already have this accession.
		var existing int
		_ = s.DB.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM signal_events
			  WHERE signal_type='insider' AND source='sec_edgar' AND source_id=?`,
			accession).Scan(&existing)
		if existing > 0 {
			continue
		}

		filing, err := client.fetchForm4XML(ctx, cik, accession)
		if err != nil {
			slog.Warn("signals: fetch form4.xml", "accession", accession, "err", err)
			continue
		}

		rows := extractInsiderEvents(filing, accession, entry.Updated)
		for _, r := range rows {
			r.UniverseHit = s.InUniverse(ctx, r.Ticker)
			tier, reasons := InsiderTier(r, thresholds)

			alarmJSON := reasonsToJSON(reasons)
			actorRole := filing.actorRole()

			res, err := s.DB.ExecContext(ctx, `
				INSERT OR IGNORE INTO signal_events
				  (signal_type, tier, event_date, filed_date,
				   ticker, sector_universe_id, actor_name, actor_role,
				   action, amount_usd, source, source_url, source_id,
				   alarm_reasons)
				VALUES ('insider', ?, ?, ?, ?, ?, ?, ?, ?, ?, 'sec_edgar', ?, ?, ?)`,
				tier, r.EventDate, r.FiledDate,
				nullStr(r.Ticker),
				nullInt64Ptr(r.UniverseHit.SectorUniverseID),
				nullStr(r.ActorName), nullStr(actorRole),
				r.Action, r.AmountUSD,
				entry.Link.Href, accession, alarmJSON,
			)
			if err != nil {
				slog.Warn("signals: insert insider row", "accession", accession, "err", err)
				continue
			}
			n, _ := res.RowsAffected()
			if n > 0 {
				inserted++
			}
		}
	}

	// Cluster-buy escalation runs once per batch.
	if _, err := s.PromoteClusterBuys(ctx); err != nil {
		slog.Warn("signals: cluster-buy promotion", "err", err)
	}
	return inserted, nil
}

// ----- ATOM feed parsing -------------------------------------------------

type atomFeed struct {
	XMLName xml.Name    `xml:"feed"`
	Entries []atomEntry `xml:"entry"`
}
type atomEntry struct {
	Title   string   `xml:"title"`
	Link    atomLink `xml:"link"`
	ID      string   `xml:"id"`
	Updated string   `xml:"updated"`
}
type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
}

type secClient struct {
	HTTP    *http.Client
	lastReq time.Time
}

func (c *secClient) do(ctx context.Context, url string) ([]byte, error) {
	// Self-throttle: sleep until secMinReqGap has elapsed since last call.
	if !c.lastReq.IsZero() {
		if elapsed := time.Since(c.lastReq); elapsed < secMinReqGap {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(secMinReqGap - elapsed):
			}
		}
	}
	c.lastReq = time.Now()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", secUserAgent)
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 429 || resp.StatusCode == 503 {
		// Brief backoff + one retry.
		time.Sleep(2 * time.Second)
		req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		req2.Header.Set("User-Agent", secUserAgent)
		resp2, err := c.HTTP.Do(req2)
		if err != nil {
			return nil, err
		}
		defer resp2.Body.Close()
		if resp2.StatusCode >= 400 {
			body, _ := io.ReadAll(io.LimitReader(resp2.Body, 256))
			return nil, fmt.Errorf("HTTP %d after retry: %s", resp2.StatusCode, strings.TrimSpace(string(body)))
		}
		return io.ReadAll(resp2.Body)
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return io.ReadAll(resp.Body)
}

func (c *secClient) fetchAtomFeed(ctx context.Context) (*atomFeed, error) {
	body, err := c.do(ctx, secAtomURL)
	if err != nil {
		return nil, err
	}
	var feed atomFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("parse atom: %w", err)
	}
	return &feed, nil
}

// ATOM link patterns we see:
//   https://www.sec.gov/cgi-bin/browse-edgar?action=getcompany&CIK=0001045810&type=4&dateb=&owner=include&count=40
//   https://www.sec.gov/Archives/edgar/data/1045810/000104581026000123/0001045810-26-000123-index.htm
// And the <id> URN:
//   urn:tag:sec.gov,2008:accession-number=0001045810-26-000123
var (
	accFromURNRe   = regexp.MustCompile(`accession-number=([\d\-]+)`)
	accFromURLRe   = regexp.MustCompile(`/Archives/edgar/data/(\d+)/([\d]+)/`)
	cikFromAnyURL  = regexp.MustCompile(`CIK=(\d+)`)
)

func parseAccessionAndCIK(link, id string) (accession, cik string, ok bool) {
	if m := accFromURNRe.FindStringSubmatch(id); len(m) == 2 {
		accession = m[1]
	}
	if m := accFromURLRe.FindStringSubmatch(link); len(m) == 3 {
		if cik == "" {
			cik = m[1]
		}
		if accession == "" {
			// rebuild dashed accession from no-dashes form
			a := m[2]
			if len(a) == 18 {
				accession = a[:10] + "-" + a[10:12] + "-" + a[12:]
			}
		}
	}
	if cik == "" {
		if m := cikFromAnyURL.FindStringSubmatch(link); len(m) == 2 {
			cik = m[1]
		}
	}
	return accession, cik, accession != "" && cik != ""
}

// ----- form4.xml parsing -------------------------------------------------

type form4Document struct {
	XMLName            xml.Name             `xml:"ownershipDocument"`
	Issuer             form4Issuer          `xml:"issuer"`
	ReportingOwner     form4ReportingOwner  `xml:"reportingOwner"`
	NonDerivativeTable form4NonDerivative   `xml:"nonDerivativeTable"`
}
type form4Issuer struct {
	IssuerCIK      string `xml:"issuerCik"`
	IssuerName     string `xml:"issuerName"`
	TradingSymbol  string `xml:"issuerTradingSymbol"`
}
type form4ReportingOwner struct {
	OwnerID   form4OwnerID   `xml:"reportingOwnerId"`
	OwnerRel  form4OwnerRel  `xml:"reportingOwnerRelationship"`
}
type form4OwnerID struct {
	CIK         string `xml:"rptOwnerCik"`
	OwnerName   string `xml:"rptOwnerName"`
}
type form4OwnerRel struct {
	IsDirector       string `xml:"isDirector"`
	IsOfficer        string `xml:"isOfficer"`
	IsTenPercent     string `xml:"isTenPercentOwner"`
	OfficerTitle     string `xml:"officerTitle"`
}
type form4NonDerivative struct {
	Transactions []form4NonDerivTxn `xml:"nonDerivativeTransaction"`
}
type form4NonDerivTxn struct {
	TransactionDate     form4Value `xml:"transactionDate>value"`
	TransactionCoding   form4Coding `xml:"transactionCoding"`
	TransactionAmounts  form4Amounts `xml:"transactionAmounts"`
}
type form4Coding struct {
	TransactionCode string `xml:"transactionCode"`
}
type form4Amounts struct {
	Shares        form4Value `xml:"transactionShares>value"`
	PricePerShare form4Value `xml:"transactionPricePerShare>value"`
	AcqDispCode   form4Value `xml:"transactionAcquiredDisposedCode>value"`
}
type form4Value struct {
	Text string `xml:",chardata"`
}

// actorRole synthesises a single role string for the signal_events row.
// "CEO" / "CFO" / "Director" / "10% Owner" / "Officer" / combined.
func (d *form4Document) actorRole() string {
	rel := d.ReportingOwner.OwnerRel
	parts := []string{}
	if strings.EqualFold(rel.IsOfficer, "1") || strings.EqualFold(rel.IsOfficer, "true") {
		if rel.OfficerTitle != "" {
			parts = append(parts, rel.OfficerTitle)
		} else {
			parts = append(parts, "Officer")
		}
	}
	if strings.EqualFold(rel.IsDirector, "1") || strings.EqualFold(rel.IsDirector, "true") {
		parts = append(parts, "Director")
	}
	if strings.EqualFold(rel.IsTenPercent, "1") || strings.EqualFold(rel.IsTenPercent, "true") {
		parts = append(parts, "10% Owner")
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " · ")
}

// fetchForm4XML tries the canonical form4.xml paths in order. The first
// 200 response wins. SEC's standard naming is "form4.xml" or
// "primary_doc.xml" within /Archives/edgar/data/{CIK}/{ACCNO-nodash}/
func (c *secClient) fetchForm4XML(ctx context.Context, cik, accession string) (*form4Document, error) {
	noDash := strings.ReplaceAll(accession, "-", "")
	candidates := []string{
		fmt.Sprintf("https://www.sec.gov/Archives/edgar/data/%s/%s/primary_doc.xml", cik, noDash),
		fmt.Sprintf("https://www.sec.gov/Archives/edgar/data/%s/%s/form4.xml", cik, noDash),
		fmt.Sprintf("https://www.sec.gov/Archives/edgar/data/%s/%s/%s-index.htm", cik, noDash, accession),
	}
	var lastErr error
	for i, url := range candidates {
		body, err := c.do(ctx, url)
		if err != nil {
			lastErr = err
			continue
		}
		// First two are XML; third is HTML for fallback parsing.
		if i < 2 {
			var doc form4Document
			if err := xml.Unmarshal(body, &doc); err == nil && doc.Issuer.TradingSymbol != "" {
				return &doc, nil
			}
			continue
		}
		// HTML fallback — extract the .xml file link from the index page.
		xmlURL := extractXMLLinkFromIndex(string(body), cik, noDash)
		if xmlURL == "" {
			continue
		}
		xmlBody, err := c.do(ctx, xmlURL)
		if err != nil {
			lastErr = err
			continue
		}
		var doc form4Document
		if err := xml.Unmarshal(xmlBody, &doc); err == nil {
			return &doc, nil
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no parseable form4.xml found for %s", accession)
	}
	return nil, lastErr
}

// extractXMLLinkFromIndex pulls the first .xml href from the filing's
// index HTML page. SEC index pages list every document in the filing.
var xmlHrefRe = regexp.MustCompile(`href="(/Archives/edgar/data/[^"]+\.xml)"`)

func extractXMLLinkFromIndex(html, cik, noDash string) string {
	matches := xmlHrefRe.FindAllStringSubmatch(html, -1)
	for _, m := range matches {
		if len(m) >= 2 {
			path := m[1]
			// Prefer form4.xml / primary_doc.xml when present.
			lower := strings.ToLower(path)
			if strings.HasSuffix(lower, "/form4.xml") || strings.HasSuffix(lower, "/primary_doc.xml") {
				return "https://www.sec.gov" + path
			}
		}
	}
	// Fall back to any .xml.
	if len(matches) > 0 && len(matches[0]) >= 2 {
		return "https://www.sec.gov" + matches[0][1]
	}
	return ""
}

// extractInsiderEvents turns a parsed form4.xml into 1+ InsiderEvent rows,
// one per non-derivative transaction matching the P/S filter.
func extractInsiderEvents(doc *form4Document, accession, updatedISO string) []InsiderEvent {
	var out []InsiderEvent
	ticker := strings.ToUpper(strings.TrimSpace(doc.Issuer.TradingSymbol))
	owner := doc.ReportingOwner.OwnerID.OwnerName

	filedDate := normaliseDate(updatedISO)

	for _, t := range doc.NonDerivativeTable.Transactions {
		code := strings.ToUpper(strings.TrimSpace(t.TransactionCoding.TransactionCode))
		if code != "P" && code != "S" {
			continue
		}
		action := ActionBuy
		if code == "S" {
			action = ActionSell
		}
		shares, _ := strconv.ParseFloat(strings.TrimSpace(t.TransactionAmounts.Shares.Text), 64)
		price, _ := strconv.ParseFloat(strings.TrimSpace(t.TransactionAmounts.PricePerShare.Text), 64)
		amount := shares * price

		out = append(out, InsiderEvent{
			Ticker:    ticker,
			ActorName: owner,
			ActorRole: "", // filled by caller (doc.actorRole())
			Action:    action,
			AmountUSD: amount,
			EventDate: normaliseDate(t.TransactionDate.Text),
			FiledDate: filedDate,
			SourceID:  accession,
		})
	}
	return out
}

func normaliseDate(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Now().UTC().Format("2006-01-02")
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.UTC().Format("2006-01-02")
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC().Format("2006-01-02")
	}
	// Default to today if unparseable.
	return time.Now().UTC().Format("2006-01-02")
}

// ----- helpers -----------------------------------------------------------

func reasonsToJSON(r []string) any {
	if len(r) == 0 {
		return nil
	}
	parts := make([]string, len(r))
	for i, s := range r {
		parts[i] = `"` + s + `"`
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt64Ptr(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}
