package signals

// Spec 9k — Office of Government Ethics (OGE) Form 278e ingestion.
// v1.21C.
//
// OGE Form 278e is the Public Annual Statement of financial holdings
// filed by senior US officials (President, Vice President, Cabinet
// members, agency heads). Unlike STOCK Act PTRs (which Congress files
// per-transaction), OGE filings are ANNUAL POSITION DISCLOSURES — they
// reveal what positions an official held during the reporting period
// but not the exact transaction dates.
//
// Source: manual JSON upload via POST /api/signals/upload-oge. The
// canonical filings live at https://www.oge.gov/ and are released in
// public formats that don't lend to scripted scraping (PDF, irregular).
// The Barron's / Bloomberg / CNBC reports on a given filing summarise
// the disclosed positions in a usable form — the user pastes that
// summary as JSON.

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// OGEUploadPayload is the JSON shape POSTed to /api/signals/upload-oge.
// One payload represents one filing (one filer, one filing date) with
// many disclosed positions.
type OGEUploadPayload struct {
	Filer       string        `json:"filer"`        // "Donald J. Trump"
	FilerRole   string        `json:"filerRole"`    // "President of the United States"
	FilingDate  string        `json:"filingDate"`   // ISO YYYY-MM-DD
	Source      string        `json:"source"`       // "OGE Form 278e (2025 Annual)"
	SourceURL   string        `json:"sourceUrl"`    // canonical URL to the filing
	Positions   []OGEPosition `json:"positions"`
}

// OGEPosition is one disclosed holding within an OGE filing.
type OGEPosition struct {
	Ticker    string `json:"ticker"`     // "NOW"
	AssetName string `json:"assetName"`  // "ServiceNow Inc"
	Amount    string `json:"amount"`     // "$1,000,001 - $5,000,000" or "1M-5M"
	Action    string `json:"action"`     // "BUY" | "SELL" | "HOLD" (default "HOLD")
	Notes     string `json:"notes,omitempty"`
}

// IngestOGE inserts one row per disclosed position into signal_events
// with signal_type='oge'. Idempotent via the (signal_type, source,
// source_id) unique constraint — source_id is a synthetic key built
// from the filer + filing date + ticker.
func (s *Service) IngestOGE(ctx context.Context, p *OGEUploadPayload) (int, error) {
	if p == nil {
		return 0, fmt.Errorf("nil payload")
	}
	filer := strings.TrimSpace(p.Filer)
	role := strings.TrimSpace(p.FilerRole)
	if filer == "" {
		return 0, fmt.Errorf("filer is required")
	}
	if p.FilingDate == "" {
		return 0, fmt.Errorf("filingDate is required (YYYY-MM-DD)")
	}
	if _, err := time.Parse("2006-01-02", p.FilingDate); err != nil {
		return 0, fmt.Errorf("invalid filingDate: %w", err)
	}
	if len(p.Positions) == 0 {
		return 0, fmt.Errorf("at least one position required")
	}
	src := strings.TrimSpace(p.Source)
	if src == "" {
		src = "oge"
	}
	inserted := 0
	for _, pos := range p.Positions {
		ticker := strings.ToUpper(strings.TrimSpace(pos.Ticker))
		if ticker == "" {
			continue
		}
		action := strings.ToUpper(strings.TrimSpace(pos.Action))
		if action == "" {
			action = "HOLD"
		}
		if action != "BUY" && action != "SELL" && action != "HOLD" {
			action = "HOLD"
		}
		amountMid, amountBucket := parseCongressAmount(pos.Amount)
		hit := s.InUniverse(ctx, ticker)
		// Tier: OGE filings disclosed positions in the user's universe
		// are FLAG by default (relevant signal). Untracked tickers stay
		// INFO. Source-specific labels come from UniverseHit.Source.
		tier := "info"
		reasons := []string{}
		if hit.Matched {
			tier = "flag"
			reasons = append(reasons, "OGE disclosure: position in "+hit.Source+" ticker")
		}
		// Synthetic dedup key: filer | filing_date | ticker — re-uploads
		// of the same filing overwrite rather than duplicate.
		sourceID := fmt.Sprintf("%s|%s|%s", strings.ToLower(filer), p.FilingDate, ticker)
		alarmJSON := reasonsToJSON(reasons)

		res, err := s.DB.ExecContext(ctx, `
			INSERT OR REPLACE INTO signal_events
			  (signal_type, tier, event_date, filed_date,
			   ticker, issuer_name, sector_universe_id,
			   actor_name, actor_role,
			   action, amount_usd, amount_bucket,
			   source, source_url, source_id, alarm_reasons, notes)
			VALUES ('oge', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			tier, p.FilingDate, p.FilingDate,
			ticker,
			nullStr(strings.TrimSpace(pos.AssetName)),
			nullInt64Ptr(hit.SectorUniverseID),
			nullStr(filer), nullStr(role),
			action, amountMid, nullStr(amountBucket),
			src, nullStr(p.SourceURL), sourceID, alarmJSON,
			nullStr(strings.TrimSpace(pos.Notes)),
		)
		if err != nil {
			slog.Warn("signals: insert OGE row", "ticker", ticker, "err", err)
			continue
		}
		n, _ := res.RowsAffected()
		if n > 0 {
			inserted++
		}
	}
	slog.Info("signals: OGE ingest complete", "filer", filer, "positions", len(p.Positions), "inserted", inserted)
	return inserted, nil
}
