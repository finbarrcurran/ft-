package signals

// Spec 9k §D4 — Congressional trade ingestion from the public
// House Stock Watcher + Senate Stock Watcher aggregators.
//
// Sources (both free, daily-updated, S3-hosted JSON):
//   https://house-stock-watcher-data.s3-us-west-2.amazonaws.com/data/all_transactions.json
//   https://senate-stock-watcher-data.s3-us-west-2.amazonaws.com/aggregate/all_transactions.json
//
// Each transaction is filtered to:
//   - in universe (holdings + watchlist + sector ETFs)
//   - amount bucket ≥ $15,001 (the "$15,001 - $50,000" disclosure tier)
//   - resolved against the legislators table via fuzzy full-name match
// then tiered (Spec §D6):
//   INFO  — outside universe / below threshold
//   FLAG  — in universe but legislator not on jurisdictional committee
//   ALARM — in universe AND legislator on a committee_sector_map row
//           matching the ticker's sector_universe_id

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	houseSWURL  = "https://house-stock-watcher-data.s3-us-west-2.amazonaws.com/data/all_transactions.json"
	senateSWURL = "https://senate-stock-watcher-data.s3-us-west-2.amazonaws.com/aggregate/all_transactions.json"
	swLookback  = 90 * 24 * time.Hour // ingest only trades disclosed in the last 90d
)

// House Stock Watcher record. Fields we use:
type houseTxn struct {
	TransactionID    string `json:"transaction_id"`     // primary dedup key (some omit it)
	DisclosureDate   string `json:"disclosure_date"`    // "MM/DD/YYYY"
	TransactionDate  string `json:"transaction_date"`   // "YYYY-MM-DD" usually
	Owner            string `json:"owner"`
	Ticker           string `json:"ticker"`
	AssetDescription string `json:"asset_description"`
	Type             string `json:"type"`               // "Purchase" | "Sale (Full)" | "Sale (Partial)" | "Exchange"
	Amount           string `json:"amount"`             // "$15,001 - $50,000" etc.
	Representative   string `json:"representative"`
	District         string `json:"district"`
	PtrLink          string `json:"ptr_link"`
}

// Senate Stock Watcher record. Shape differs from House.
type senateTxn struct {
	TransactionDate  string `json:"transaction_date"`   // "MM/DD/YYYY"
	DisclosureDate   string `json:"disclosure_date"`    // "MM/DD/YYYY"
	Owner            string `json:"owner"`
	Ticker           string `json:"ticker"`
	AssetDescription string `json:"asset_description"`
	Type             string `json:"type"`
	Amount           string `json:"amount"`
	Senator          string `json:"senator"`
	PtrLink          string `json:"ptr_link"`
}

// capitolTradesCachePath is the Playwright-scraped JSON cache produced
// by /home/curran/scripts/capitol-trades-fetch.js. v1.21B fallback for
// the dead Stock Watcher S3 buckets.
const capitolTradesCachePath = "/var/lib/ft/data/capitol-trades/trades.json"

// capitolTrade mirrors the JSON shape capitol-trades-fetch.js writes.
type capitolTrade struct {
	TradeDate      string `json:"tradeDate"`
	DisclosureDate string `json:"disclosureDate"`
	Politician     string `json:"politician"`
	Party          string `json:"party"`
	Chamber        string `json:"chamber"`
	State          string `json:"state"`
	Ticker         string `json:"ticker"`
	Type           string `json:"type"`
	Amount         string `json:"amount"`
	AssetName      string `json:"assetName"`
	SourceURL      string `json:"sourceUrl"`
}

type capitolTradesFile struct {
	FetchedAt  string         `json:"fetchedAt"`
	Source     string         `json:"source"`
	TradeCount int            `json:"tradeCount"`
	Trades     []capitolTrade `json:"trades"`
}

// IngestCongressFromCapitolTradesFile reads the Playwright cache and
// adapts each row to the existing houseTxn / senateTxn shape so the
// established ingestCongressBatch path picks them up. Returns inserted
// count. v1.21B.
func (s *Service) IngestCongressFromCapitolTradesFile(ctx context.Context) (int, error) {
	raw, err := os.ReadFile(capitolTradesCachePath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	var f capitolTradesFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return 0, fmt.Errorf("parse capitol-trades json: %w", err)
	}
	if len(f.Trades) == 0 {
		return 0, nil
	}
	cutoff := time.Now().UTC().Add(-swLookback)

	// Split into House + Senate batches so legislator chamber match works.
	houseRows := make([]houseTxn, 0)
	senateRows := make([]houseTxn, 0) // re-using houseTxn shape; chamber arg controls source label
	for _, t := range f.Trades {
		row := houseTxn{
			TransactionID:    t.SourceURL,
			DisclosureDate:   t.DisclosureDate,
			TransactionDate:  t.TradeDate,
			Owner:            "",
			Ticker:           t.Ticker,
			AssetDescription: t.AssetName,
			Type:             capitolTypeToCongressType(t.Type),
			Amount:           t.Amount,
			Representative:   t.Politician,
			District:         t.State,
			PtrLink:          t.SourceURL,
		}
		if t.Chamber == "senate" {
			senateRows = append(senateRows, row)
		} else {
			houseRows = append(houseRows, row)
		}
	}
	inserted := 0
	if len(houseRows) > 0 {
		inserted += s.ingestCongressBatch(ctx, houseRows, "house", cutoff)
	}
	if len(senateRows) > 0 {
		inserted += s.ingestCongressBatch(ctx, senateRows, "senate", cutoff)
	}
	return inserted, nil
}

// capitolTypeToCongressType maps capitol-trades' "BUY"/"SELL"/"EXCH" to
// the strings the existing congress ingest's mapCongressAction recognises.
func capitolTypeToCongressType(t string) string {
	switch strings.ToUpper(strings.TrimSpace(t)) {
	case "BUY":
		return "Purchase"
	case "SELL":
		return "Sale (Partial)"
	case "EXCH":
		return "Exchange"
	}
	return ""
}

// IngestCongress pulls both feeds, filters + tiers, and inserts.
func (s *Service) IngestCongress(ctx context.Context) (inserted int, retErr error) {
	t0 := time.Now()
	slog.Info("signals: congress ingest started")
	defer func() {
		slog.Info("signals: congress ingest finished",
			"inserted", inserted, "took", time.Since(t0).Round(time.Millisecond))
	}()

	// v1.21B — Playwright cache first (the dead S3 fallbacks remain for
	// historical compatibility but always fail now). Cache is written by
	// /etc/cron.d/ft-capitol-trades 5 min before this ingest.
	if n, err := s.IngestCongressFromCapitolTradesFile(ctx); err != nil {
		slog.Warn("signals: capitol-trades file ingest", "err", err)
	} else {
		inserted += n
		slog.Info("signals: capitol-trades file ingest", "inserted", n)
	}

	client := &http.Client{Timeout: 120 * time.Second}
	cutoff := time.Now().UTC().Add(-swLookback)

	// House.
	var houseRows []houseTxn
	if err := getJSON(ctx, client, houseSWURL, &houseRows); err != nil {
		slog.Warn("signals: house ingest fetch (expected — S3 dead)", "err", err)
	} else {
		n := s.ingestCongressBatch(ctx, houseRows, "house", cutoff)
		inserted += n
		slog.Info("signals: house ingest", "fetched", len(houseRows), "inserted", n)
	}

	// Senate.
	var senateRows []senateTxn
	if err := getJSON(ctx, client, senateSWURL, &senateRows); err != nil {
		slog.Warn("signals: senate ingest fetch", "err", err)
	} else {
		// Adapt senate → house shape for re-use.
		adapted := make([]houseTxn, 0, len(senateRows))
		for i, r := range senateRows {
			adapted = append(adapted, houseTxn{
				TransactionID:    fmt.Sprintf("senate_%d_%s_%s", i, r.Senator, r.TransactionDate),
				DisclosureDate:   r.DisclosureDate,
				TransactionDate:  r.TransactionDate,
				Owner:            r.Owner,
				Ticker:           r.Ticker,
				AssetDescription: r.AssetDescription,
				Type:             r.Type,
				Amount:           r.Amount,
				Representative:   r.Senator, // re-used as actor name
				PtrLink:          r.PtrLink,
			})
		}
		n := s.ingestCongressBatch(ctx, adapted, "senate", cutoff)
		inserted += n
		slog.Info("signals: senate ingest", "fetched", len(senateRows), "inserted", n)
	}
	return inserted, nil
}

func (s *Service) ingestCongressBatch(ctx context.Context, rows []houseTxn, chamber string, cutoff time.Time) int {
	inserted := 0
	source := "house_sw"
	if chamber == "senate" {
		source = "senate_sw"
	}
	for _, r := range rows {
		select {
		case <-ctx.Done():
			return inserted
		default:
		}

		ticker := strings.ToUpper(strings.TrimSpace(r.Ticker))
		if ticker == "" || ticker == "--" || ticker == "N/A" {
			continue
		}

		eventDate := parseCongressDate(r.TransactionDate)
		filedDate := parseCongressDate(r.DisclosureDate)
		if filedDate == "" {
			filedDate = time.Now().UTC().Format("2006-01-02")
		}
		// Honor the 90-day lookback on filed_date.
		if t, err := time.Parse("2006-01-02", filedDate); err == nil && t.Before(cutoff) {
			continue
		}
		if eventDate == "" {
			eventDate = filedDate
		}

		amountMid, amountBucket := parseCongressAmount(r.Amount)
		// v1.21B — lowered floor from $15K to $1K (STOCK Act disclosure
		// minimum). Sub-$15K trades land as INFO tier; the Tier system
		// already gates promotion to FLAG/ALARM by amount + universe +
		// committee membership, so this just brings in the long-tail
		// of small disclosed trades the user can still find useful.
		if amountMid < 1_000 {
			continue
		}

		actor := strings.TrimSpace(r.Representative)
		action := mapCongressAction(r.Type)

		// Dedup key: source + transaction_id (House) or synthetic key (Senate).
		sourceID := r.TransactionID
		if sourceID == "" {
			sourceID = fmt.Sprintf("%s|%s|%s|%s|%s", chamber, actor, ticker, eventDate, r.Type)
		}

		// Resolve legislator + universe + tier.
		legID, committeeMatch := s.resolveLegislator(ctx, actor, chamber, ticker)
		hit := s.InUniverse(ctx, ticker)
		tier, reasons := CongressTier(CongressEvent{
			Ticker:         ticker,
			AmountUSD:      amountMid,
			UniverseHit:    hit,
			CommitteeMatch: committeeMatch,
		}, DefaultThresholds())

		alarmJSON := reasonsToJSON(reasons)
		res, err := s.DB.ExecContext(ctx, `
			INSERT OR IGNORE INTO signal_events
			  (signal_type, tier, event_date, filed_date,
			   ticker, issuer_name, sector_universe_id,
			   actor_name, actor_role, legislator_id,
			   action, amount_usd, amount_bucket,
			   source, source_url, source_id, alarm_reasons)
			VALUES ('congress', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			tier, eventDate, filedDate,
			ticker, nullStr(strings.TrimSpace(r.AssetDescription)),
			nullInt64Ptr(hit.SectorUniverseID),
			nullStr(actor), nullStr(chamber),
			nullInt64(legID),
			nullStr(action), amountMid, nullStr(amountBucket),
			source, nullStr(r.PtrLink), sourceID, alarmJSON,
		)
		if err != nil {
			slog.Warn("signals: insert congress", "actor", actor, "ticker", ticker, "err", err)
			continue
		}
		n, _ := res.RowsAffected()
		if n > 0 {
			inserted++
		}
	}
	return inserted
}

// resolveLegislator does a case-insensitive substring match against
// legislators.full_name in the requested chamber, plus a committee-
// match check for ALARM-tier promotion.
func (s *Service) resolveLegislator(ctx context.Context, name, chamber, ticker string) (legID int64, committeeMatch bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, false
	}
	// Try Hon. prefix strip.
	name = strings.TrimPrefix(name, "Hon. ")
	name = strings.TrimPrefix(name, "Hon ")
	// Best-effort first match.
	var id int64
	_ = s.DB.QueryRowContext(ctx, `
		SELECT id FROM legislators
		 WHERE chamber = ?
		   AND lower(full_name) LIKE ?
		 LIMIT 1`,
		chamber, "%"+strings.ToLower(name)+"%").Scan(&id)
	if id == 0 {
		return 0, false
	}
	// Committee jurisdiction match: does this legislator sit on a
	// committee whose committee_sector_map row matches the traded
	// ticker's sector_universe_id?
	hit := s.InUniverse(ctx, ticker)
	if hit.SectorUniverseID == nil {
		return id, false
	}
	var match int
	_ = s.DB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM committee_assignments ca
		  JOIN committee_sector_map sm ON sm.committee_code = ca.committee_code
		 WHERE ca.legislator_id = ?
		   AND sm.sector_universe_id = ?`,
		id, *hit.SectorUniverseID).Scan(&match)
	return id, match > 0
}

func parseCongressDate(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, layout := range []string{"2006-01-02", "01/02/2006", "1/2/2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC().Format("2006-01-02")
		}
	}
	return ""
}

// parseCongressAmount turns "$15,001 - $50,000" → (32_500, "$15K-$50K").
// Buckets above $5M are capped at $5M for the midpoint.
func parseCongressAmount(s string) (mid float64, bucket string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, ""
	}
	bucket = condenseBucket(s)
	low, high := parseDollarRange(s)
	if low == 0 && high == 0 {
		return 0, bucket
	}
	if high == 0 {
		high = low
	}
	return (low + high) / 2, bucket
}

func parseDollarRange(s string) (low, high float64) {
	// v1.21B — also accept capitol-trades' compact format ("1K–15K",
	// "100K–250K", "1M–5M", "5M+", with en-dash "–"). The old Stock
	// Watcher format ("$15,001 - $50,000") still works after the
	// $/comma/space strip below.
	clean := strings.NewReplacer("$", "", ",", "", "+", "").Replace(s)
	// Normalise dash characters: en-dash, em-dash → hyphen
	clean = strings.NewReplacer("–", "-", "—", "-").Replace(clean)
	parts := strings.Split(clean, "-")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	low = parseAmountToken(parts[0])
	if len(parts) >= 2 {
		high = parseAmountToken(parts[1])
	}
	return low, high
}

// parseAmountToken converts "1K", "100K", "1.5M", "1500000" → float64.
// v1.21B.
func parseAmountToken(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	mult := 1.0
	last := s[len(s)-1]
	if last == 'K' || last == 'k' {
		mult = 1_000
		s = s[:len(s)-1]
	} else if last == 'M' || last == 'm' {
		mult = 1_000_000
		s = s[:len(s)-1]
	} else if last == 'B' || last == 'b' {
		mult = 1_000_000_000
		s = s[:len(s)-1]
	}
	var v float64
	fmt.Sscanf(strings.TrimSpace(s), "%f", &v)
	return v * mult
}

func condenseBucket(s string) string {
	// "$15,001 - $50,000" → "$15K-$50K"
	rep := strings.NewReplacer(",000,000", "M", ",000", "K", "$", "$", " ", "", ",001", "K", ",", "")
	return rep.Replace(s)
}

func mapCongressAction(t string) string {
	t = strings.ToLower(t)
	switch {
	case strings.Contains(t, "purchase"):
		return ActionBuy
	case strings.Contains(t, "sale"):
		return ActionSell
	case strings.Contains(t, "exchange"):
		return ActionSell // conservative
	}
	return ""
}

func nullInt64(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}
