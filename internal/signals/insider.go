package signals

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html/charset"
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
	secAtomURL   = "https://www.sec.gov/cgi-bin/browse-edgar?action=getcurrent&type=4&output=atom&count=100"
	secUserAgent = "FT-Dashboard fin@curranhouse.dev"
	secMinReqGap = 130 * time.Millisecond // ~7.7 req/sec — under 10/sec ceiling
)

// IngestInsiders fetches the current Form 4 ATOM feed (firehose: last 100
// filings across all companies) and processes every entry. Returns the
// number of new rows inserted (deduplicated by accession number). Called
// from the daily 23:00 UTC cron + the manual refresh endpoint.
//
// v1.21A — kept alongside IngestInsidersPerTicker. Firehose catches
// breaking filings that don't yet have a CIK we know about; per-ticker
// catches everything for tickers we explicitly watch (NOW, AVGO, etc.).
//
// Errors per filing are logged but don't abort the batch.
func (s *Service) IngestInsiders(ctx context.Context) (inserted int, retErr error) {
	t0 := time.Now()
	slog.Info("signals: insider ingest started")
	client := &secClient{HTTP: &http.Client{Timeout: 30 * time.Second}}
	feed, err := client.fetchAtomFeed(ctx)
	if err != nil {
		slog.Error("signals: ATOM fetch failed", "err", err)
		return 0, fmt.Errorf("fetch ATOM feed: %w", err)
	}
	slog.Info("signals: ATOM feed parsed", "entries", len(feed.Entries))
	if len(feed.Entries) == 0 {
		return 0, nil
	}

	thresholds := DefaultThresholds()
	defer func() {
		slog.Info("signals: insider ingest finished",
			"entries", len(feed.Entries),
			"inserted", inserted,
			"took", time.Since(t0).Round(time.Millisecond))
	}()

	inserted = s.processAtomEntries(ctx, client, feed.Entries, thresholds)

	// Cluster-buy escalation runs once per batch.
	if _, err := s.PromoteClusterBuys(ctx); err != nil {
		slog.Warn("signals: cluster-buy promotion", "err", err)
	}
	return inserted, nil
}

// IngestInsidersPerTicker runs the same Form 4 ingest as IngestInsiders
// but driven from a per-CIK query for every ticker in the user's
// universe (holdings + watchlist + sector ETFs), rather than the global
// firehose. v1.21A.
//
// Rationale: the global ATOM feed returns the last 100 Form 4s across
// the entire market — at hundreds of filings/day, anything not in the
// last hour gets missed. Per-CIK query returns the most recent ~40
// filings for THAT ticker, so we reliably catch every NOW (or any other
// universe ticker) Form 4 even if higher-volume names dominated the
// firehose between ticks.
//
// SEC rate-limit (10/sec) handled by the existing client throttle.
// ~41 tickers × ~1 req/0.13s = ~6s wall time.
func (s *Service) IngestInsidersPerTicker(ctx context.Context) (inserted int, retErr error) {
	t0 := time.Now()
	slog.Info("signals: per-ticker insider ingest started")
	client := &secClient{HTTP: &http.Client{Timeout: 30 * time.Second}}

	cikMap, err := client.fetchCIKMap(ctx)
	if err != nil {
		return 0, fmt.Errorf("fetch CIK map: %w", err)
	}
	tickers, err := s.universeTickers(ctx)
	if err != nil {
		return 0, fmt.Errorf("load universe: %w", err)
	}
	slog.Info("signals: per-ticker universe loaded", "tickers", len(tickers), "cik_map_size", len(cikMap))

	thresholds := DefaultThresholds()
	missing := 0
	for _, t := range tickers {
		select {
		case <-ctx.Done():
			return inserted, ctx.Err()
		default:
		}
		cik, ok := cikMap[strings.ToUpper(t)]
		if !ok {
			missing++
			continue
		}
		feed, err := client.fetchAtomFeedForCIK(ctx, cik)
		if err != nil {
			slog.Warn("signals: per-ticker ATOM fetch", "ticker", t, "cik", cik, "err", err)
			continue
		}
		n := s.processAtomEntries(ctx, client, feed.Entries, thresholds)
		if n > 0 {
			slog.Info("signals: per-ticker hits", "ticker", t, "new", n)
		}
		inserted += n
	}
	// Cluster-buy escalation runs once per batch.
	if _, err := s.PromoteClusterBuys(ctx); err != nil {
		slog.Warn("signals: cluster-buy promotion", "err", err)
	}
	slog.Info("signals: per-ticker insider ingest finished",
		"tickers_processed", len(tickers),
		"tickers_missing_cik", missing,
		"inserted", inserted,
		"took", time.Since(t0).Round(time.Millisecond))
	return inserted, nil
}

// universeTickers returns the deduplicated set of tickers we watch:
// active stock holdings + active watchlist. Sector ETFs are excluded
// (separate ingest if ever needed). v1.21A.
func (s *Service) universeTickers(ctx context.Context) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT DISTINCT ticker FROM (
		  SELECT ticker FROM stock_holdings
		    WHERE deleted_at IS NULL AND ticker IS NOT NULL AND ticker != ''
		  UNION
		  SELECT ticker FROM watchlist
		    WHERE deleted_at IS NULL AND ticker IS NOT NULL AND ticker != ''
		)
		ORDER BY ticker`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// processAtomEntries is the shared inner loop used by both
// IngestInsiders (firehose) and IngestInsidersPerTicker (per-CIK).
// Skips entries with non-"4" categories, dedupes by accession, fetches
// form4.xml, extracts events, and inserts. Returns insert count.
func (s *Service) processAtomEntries(ctx context.Context, client *secClient, entries []atomEntry, thresholds Thresholds) int {
	inserted := 0
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return inserted
		default:
		}
		// EDGAR's type=4 filter is loose — entries of other form types
		// (497J, 4/A, etc.) sometimes leak through. Hard-filter here.
		if entry.Category.Term != "" && entry.Category.Term != "4" {
			continue
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
		issuerName := strings.TrimSpace(filing.Issuer.IssuerName)
		for _, r := range rows {
			r.UniverseHit = s.InUniverse(ctx, r.Ticker)
			tier, reasons := InsiderTier(r, thresholds)
			alarmJSON := reasonsToJSON(reasons)
			actorRole := filing.actorRole()
			res, err := s.DB.ExecContext(ctx, `
				INSERT OR IGNORE INTO signal_events
				  (signal_type, tier, event_date, filed_date,
				   ticker, issuer_name, sector_universe_id, actor_name, actor_role,
				   action, amount_usd, source, source_url, source_id,
				   alarm_reasons)
				VALUES ('insider', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'sec_edgar', ?, ?, ?)`,
				tier, r.EventDate, r.FiledDate,
				nullStr(r.Ticker),
				nullStr(issuerName),
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
	return inserted
}

// ----- ATOM feed parsing -------------------------------------------------

type atomFeed struct {
	XMLName xml.Name    `xml:"feed"`
	Entries []atomEntry `xml:"entry"`
}
type atomEntry struct {
	Title    string       `xml:"title"`
	Link     atomLink     `xml:"link"`
	ID       string       `xml:"id"`
	Updated  string       `xml:"updated"`
	Category atomCategory `xml:"category"`
}
type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
}
type atomCategory struct {
	Term string `xml:"term,attr"`
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
	// NOTE: do NOT set Accept-Encoding manually — Go's net/http
	// auto-decompresses gzip ONLY when the caller hasn't touched the header.
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
	// SEC ATOM feeds declare encoding="ISO-8859-1" — provide a charset
	// reader so the stdlib decoder doesn't bail.
	dec := xml.NewDecoder(bytes.NewReader(body))
	dec.CharsetReader = charset.NewReaderLabel
	var feed atomFeed
	if err := dec.Decode(&feed); err != nil {
		return nil, fmt.Errorf("parse atom: %w", err)
	}
	return &feed, nil
}

// fetchAtomFeedForCIK pulls the most recent ~40 Form 4 filings for a
// specific issuer CIK. Used by IngestInsidersPerTicker. v1.21A.
func (c *secClient) fetchAtomFeedForCIK(ctx context.Context, cik string) (*atomFeed, error) {
	u := fmt.Sprintf(
		"https://www.sec.gov/cgi-bin/browse-edgar?action=getcompany&CIK=%s&type=4&dateb=&owner=include&count=40&output=atom",
		cik,
	)
	body, err := c.do(ctx, u)
	if err != nil {
		return nil, err
	}
	dec := xml.NewDecoder(bytes.NewReader(body))
	dec.CharsetReader = charset.NewReaderLabel
	var feed atomFeed
	if err := dec.Decode(&feed); err != nil {
		return nil, fmt.Errorf("parse atom for CIK %s: %w", cik, err)
	}
	return &feed, nil
}

// fetchCIKMap pulls SEC's official ticker→CIK mapping
// (https://www.sec.gov/files/company_tickers.json) and returns a map
// keyed by UPPER-CASE ticker → 10-digit-padded CIK string. v1.21A.
//
// The file is ~800KB, ~10k tickers — pulled once per ingest run, not
// cached across runs (so newly-added tickers don't lag).
func (c *secClient) fetchCIKMap(ctx context.Context) (map[string]string, error) {
	body, err := c.do(ctx, "https://www.sec.gov/files/company_tickers.json")
	if err != nil {
		return nil, err
	}
	// Shape: {"0":{"cik_str":1373715,"ticker":"NOW","title":"SERVICENOW, INC."}, ...}
	var raw map[string]struct {
		CIKStr int    `json:"cik_str"`
		Ticker string `json:"ticker"`
		Title  string `json:"title"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse company_tickers.json: %w", err)
	}
	out := make(map[string]string, len(raw))
	for _, v := range raw {
		if v.Ticker == "" || v.CIKStr == 0 {
			continue
		}
		out[strings.ToUpper(v.Ticker)] = fmt.Sprintf("%010d", v.CIKStr)
	}
	return out, nil
}

// ATOM link patterns we see:
//
//	https://www.sec.gov/cgi-bin/browse-edgar?action=getcompany&CIK=0001045810&type=4&dateb=&owner=include&count=40
//	https://www.sec.gov/Archives/edgar/data/1045810/000104581026000123/0001045810-26-000123-index.htm
//
// And the <id> URN:
//
//	urn:tag:sec.gov,2008:accession-number=0001045810-26-000123
var (
	accFromURNRe  = regexp.MustCompile(`accession-number=([\d\-]+)`)
	accFromURLRe  = regexp.MustCompile(`/Archives/edgar/data/(\d+)/([\d]+)/`)
	cikFromAnyURL = regexp.MustCompile(`CIK=(\d+)`)
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
	XMLName            xml.Name            `xml:"ownershipDocument"`
	Issuer             form4Issuer         `xml:"issuer"`
	ReportingOwner     form4ReportingOwner `xml:"reportingOwner"`
	NonDerivativeTable form4NonDerivative  `xml:"nonDerivativeTable"`
}
type form4Issuer struct {
	IssuerCIK     string `xml:"issuerCik"`
	IssuerName    string `xml:"issuerName"`
	TradingSymbol string `xml:"issuerTradingSymbol"`
}
type form4ReportingOwner struct {
	OwnerID  form4OwnerID  `xml:"reportingOwnerId"`
	OwnerRel form4OwnerRel `xml:"reportingOwnerRelationship"`
}
type form4OwnerID struct {
	CIK       string `xml:"rptOwnerCik"`
	OwnerName string `xml:"rptOwnerName"`
}
type form4OwnerRel struct {
	IsDirector   string `xml:"isDirector"`
	IsOfficer    string `xml:"isOfficer"`
	IsTenPercent string `xml:"isTenPercentOwner"`
	OfficerTitle string `xml:"officerTitle"`
}
type form4NonDerivative struct {
	Transactions []form4NonDerivTxn `xml:"nonDerivativeTransaction"`
}
type form4NonDerivTxn struct {
	TransactionDate    form4Value   `xml:"transactionDate>value"`
	TransactionCoding  form4Coding  `xml:"transactionCoding"`
	TransactionAmounts form4Amounts `xml:"transactionAmounts"`
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

// fetchForm4XML uses SEC's index.json endpoint to discover the actual
// filing XML filename (it's filing-specific, e.g. "wk-form4_1779378422.xml"),
// then fetches that XML directly. Two round-trips per filing — half what
// the old "try primary_doc.xml then form4.xml then index.htm" path needed.
func (c *secClient) fetchForm4XML(ctx context.Context, cik, accession string) (*form4Document, error) {
	noDash := strings.ReplaceAll(accession, "-", "")
	indexURL := fmt.Sprintf("https://www.sec.gov/Archives/edgar/data/%s/%s/index.json", cik, noDash)
	body, err := c.do(ctx, indexURL)
	if err != nil {
		return nil, fmt.Errorf("index.json: %w", err)
	}
	var idx secIndex
	if err := json.Unmarshal(body, &idx); err != nil {
		return nil, fmt.Errorf("parse index.json: %w", err)
	}
	xmlName := idx.pickFormXMLName()
	if xmlName == "" {
		return nil, fmt.Errorf("no form4 .xml in index for %s", accession)
	}
	xmlURL := fmt.Sprintf("https://www.sec.gov/Archives/edgar/data/%s/%s/%s", cik, noDash, xmlName)
	xmlBody, err := c.do(ctx, xmlURL)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", xmlName, err)
	}
	var doc form4Document
	if err := decodeXML(xmlBody, &doc); err != nil {
		return nil, fmt.Errorf("decode %s: %w", xmlName, err)
	}
	return &doc, nil
}

// SEC's index.json shape — only the parts we care about.
type secIndex struct {
	Directory struct {
		Items []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"item"`
	} `json:"directory"`
}

// pickFormXMLName returns the most likely Form 4 XML file from an index.
// Preference order:
//  1. primary_doc.xml (some filers use this canonical name)
//  2. *form4*.xml (e.g. wk-form4_NNN.xml, the common pattern)
//  3. Any .xml NOT inside an xslF345X0X/ subdirectory (those are HTML
//     stylesheets, not the structured filing).
func (idx *secIndex) pickFormXMLName() string {
	var anyXML string
	for _, it := range idx.Directory.Items {
		name := strings.ToLower(it.Name)
		if !strings.HasSuffix(name, ".xml") {
			continue
		}
		if strings.HasPrefix(name, "xslf345") || strings.Contains(name, "/xslf345") {
			continue
		}
		if name == "primary_doc.xml" {
			return it.Name
		}
		if strings.Contains(name, "form4") {
			return it.Name
		}
		if anyXML == "" {
			anyXML = it.Name
		}
	}
	return anyXML
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

// decodeXML wraps xml.Unmarshal with a charset-aware decoder so
// ISO-8859-1 (and other non-UTF-8 declared) payloads from SEC parse cleanly.
func decodeXML(body []byte, v any) error {
	dec := xml.NewDecoder(bytes.NewReader(body))
	dec.CharsetReader = charset.NewReaderLabel
	return dec.Decode(v)
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
