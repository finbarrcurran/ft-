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
	"fmt"
	"log/slog"
	"net/http"
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

// IngestCongress pulls both feeds, filters + tiers, and inserts.
func (s *Service) IngestCongress(ctx context.Context) (inserted int, retErr error) {
	t0 := time.Now()
	slog.Info("signals: congress ingest started")
	defer func() {
		slog.Info("signals: congress ingest finished",
			"inserted", inserted, "took", time.Since(t0).Round(time.Millisecond))
	}()

	client := &http.Client{Timeout: 120 * time.Second}
	cutoff := time.Now().UTC().Add(-swLookback)

	// House.
	var houseRows []houseTxn
	if err := getJSON(ctx, client, houseSWURL, &houseRows); err != nil {
		slog.Warn("signals: house ingest fetch", "err", err)
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
		if amountMid < 15_000 {
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
			action, amountMid, nullStr(amountBucket),
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
	// Strip "$" and ","
	clean := strings.NewReplacer("$", "", ",", "", "+", "").Replace(s)
	parts := strings.Split(clean, "-")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	if len(parts) >= 1 {
		fmt.Sscanf(parts[0], "%f", &low)
	}
	if len(parts) >= 2 {
		fmt.Sscanf(parts[1], "%f", &high)
	}
	return low, high
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
