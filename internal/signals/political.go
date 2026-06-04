package signals

// SC-24 — Named Political-Figure Tracker.
//
// Layers a named-individual watchlist onto Spec 9k's existing feeds and adds
// the OGE Form 278-T (executive PERIODIC transaction report) ingest. This is
// distinct from the annual Form 278e path in oge.go:
//
//   278e  (oge.go)      — ANNUAL position disclosure  → signal_type 'oge'
//   278-T (this file)   — PERIODIC transaction report → signal_type 'oge_278t'
//
// 278-T reports are filed ~quarterly with a ~45-day lag and disclose
// individual transactions (buy/sell, date, OGE value BAND). Two hard rules
// from the spec:
//
//   - Value bands are stored as BANDS (amount_bucket), never as an invented
//     point value (amount_usd stays NULL). OGE discloses a range; we keep the
//     range.
//   - Parse-confident-or-flag: a line we can't confidently parse is SURFACED
//     as an INFO row with a 'parse_unconfident' reason and the raw text in
//     notes — never silently dropped, never guessed.
//
// The genuinely powerful feature is the EO-coincidence cross-link
// (PromoteEOCoincident): a 278-T trade whose sector is touched by an Executive
// Order in the same window is promoted to ALARM. It reuses 9k's existing EO
// ingest — it's just a join.

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Caveat278T is the mandatory label that must accompany every 278-T view and
// every alert (spec §C / AC4 / S-24c). Surfaced verbatim in the API payload so
// the UI banner and any downstream (Telegram) consumer carry the same words.
const Caveat278T = "Periodic (~quarterly) · ~45-day lag · OGE value bands (not exact) · does not state who directed the trade."

// eoCoincidenceWindowDays is the "same-window" span for the EO cross-link: a
// 278-T transaction within this many days of an EO touching the same sector is
// treated as coincident (spec §F ALARM / S-24d).
const eoCoincidenceWindowDays = 14

// OGE278TPayload is the JSON POSTed to /api/signals/upload-278t. One payload =
// one filing (one filer, one periodic report) with many transactions. 278-T is
// published as PDF/structured-disclosure, so the user (or an upstream parser)
// pastes the extracted transactions; lines that couldn't be parsed cleanly are
// flagged with Unparsed=true and carried as RawLine.
type OGE278TPayload struct {
	Filer        string         `json:"filer"`        // "Donald J. Trump"
	FilerRole    string         `json:"filerRole"`    // "President of the United States"
	FilingDate   string         `json:"filingDate"`   // ISO YYYY-MM-DD — when the 278-T was filed
	Source       string         `json:"source"`       // "OGE Form 278-T (2026 Q1)"
	SourceURL    string         `json:"sourceUrl"`    // canonical URL
	Transactions []OGE278TTxn   `json:"transactions"`
}

// OGE278TTxn is one disclosed transaction within a 278-T report.
type OGE278TTxn struct {
	Ticker    string `json:"ticker"`              // "NVDA"
	AssetName string `json:"assetName"`           // "NVIDIA Corp"
	Action    string `json:"action"`              // "BUY" | "SELL"
	ValueBand string `json:"valueBand"`           // "$1,000,001 - $5,000,000" — stored verbatim, never reduced to a point
	TxnDate   string `json:"txnDate"`             // ISO YYYY-MM-DD — transaction date
	Unparsed  bool   `json:"unparsed,omitempty"`  // true if the source line couldn't be confidently parsed
	RawLine   string `json:"rawLine,omitempty"`   // the original text — surfaced, never guessed
	Notes     string `json:"notes,omitempty"`
}

// Ingest278T inserts one signal_events row per transaction with
// signal_type='oge_278t'. Idempotent via (signal_type, source, source_id).
// Returns (inserted, flagged) where flagged counts parse-unconfident lines.
func (s *Service) Ingest278T(ctx context.Context, p *OGE278TPayload) (inserted, flagged int, retErr error) {
	if p == nil {
		return 0, 0, fmt.Errorf("nil payload")
	}
	filer := strings.TrimSpace(p.Filer)
	role := strings.TrimSpace(p.FilerRole)
	if filer == "" {
		return 0, 0, fmt.Errorf("filer is required")
	}
	if p.FilingDate == "" {
		return 0, 0, fmt.Errorf("filingDate is required (YYYY-MM-DD)")
	}
	if _, err := time.Parse("2006-01-02", p.FilingDate); err != nil {
		return 0, 0, fmt.Errorf("invalid filingDate: %w", err)
	}
	if len(p.Transactions) == 0 {
		return 0, 0, fmt.Errorf("at least one transaction required")
	}
	src := strings.TrimSpace(p.Source)
	if src == "" {
		src = "oge_278t"
	}

	// Link to the named watchlist where the filer is tracked.
	trackedID := s.resolveTrackedIndividual(ctx, filer)

	for i, tx := range p.Transactions {
		ticker := strings.ToUpper(strings.TrimSpace(tx.Ticker))
		action := strings.ToUpper(strings.TrimSpace(tx.Action))
		band := strings.TrimSpace(tx.ValueBand)
		txnDate := strings.TrimSpace(tx.TxnDate)
		if txnDate == "" {
			txnDate = p.FilingDate
		}

		// Parse-confident-or-flag (S-24b): a line is unconfident if the source
		// flagged it, the ticker is missing, or the action isn't a clean
		// buy/sell. We still record it — surfaced as INFO with the raw text —
		// but never guess a ticker/action/value.
		confident := !tx.Unparsed && ticker != "" && (action == ActionBuy || action == ActionSell)

		reasons := []string{}
		tier := TierInfo
		var sectorID *int64
		var tickerVal any
		var actionVal any

		if confident {
			tickerVal = ticker
			actionVal = action
			hit := s.InUniverse(ctx, ticker)
			if hit.Matched {
				sectorID = hit.SectorUniverseID
				switch hit.Source {
				case "holding":
					// Overlap concentrated in / contrary to a held name → ALARM (§F).
					tier = TierAlarm
					reasons = append(reasons, "overlap_held_name")
				default:
					// watchlist / sector_etf overlap → FLAG.
					tier = TierFlag
					reasons = append(reasons, "overlap_universe")
				}
			}
		} else {
			// Unconfident — surface, don't guess.
			tickerVal = nil
			actionVal = nil
			reasons = append(reasons, "parse_unconfident")
			flagged++
		}

		// notes: asset detail (or raw line for unparsed) + any supplied note.
		noteParts := []string{}
		if !confident && strings.TrimSpace(tx.RawLine) != "" {
			noteParts = append(noteParts, "UNPARSED: "+strings.TrimSpace(tx.RawLine))
		} else if strings.TrimSpace(tx.AssetName) != "" {
			noteParts = append(noteParts, strings.TrimSpace(tx.AssetName))
		}
		if strings.TrimSpace(tx.Notes) != "" {
			noteParts = append(noteParts, strings.TrimSpace(tx.Notes))
		}
		notes := strings.Join(noteParts, " — ")

		// Dedup key — unique per filing line. Include the index so two
		// otherwise-identical disclosed lines don't collide.
		sourceID := fmt.Sprintf("%s|%s|%s|%s|%d", strings.ToLower(filer), txnDate, ticker, action, i)

		res, err := s.DB.ExecContext(ctx, `
			INSERT OR REPLACE INTO signal_events
			  (signal_type, tier, event_date, filed_date,
			   ticker, issuer_name, sector_universe_id,
			   actor_name, actor_role,
			   action, amount_usd, amount_bucket,
			   source, source_url, source_id, alarm_reasons, notes,
			   tracked_individual_id)
			VALUES ('oge_278t', ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, ?, ?, ?, ?)`,
			tier, txnDate, p.FilingDate,
			tickerVal,
			nullStr(strings.TrimSpace(tx.AssetName)),
			nullInt64Ptr(sectorID),
			nullStr(filer), nullStr(role),
			actionVal,
			nullStr(band), // amount_bucket = the OGE band, verbatim; amount_usd stays NULL
			src, nullStr(p.SourceURL), sourceID, reasonsToJSON(reasons), nullStr(notes),
			nullInt64Ptr(trackedID),
		)
		if err != nil {
			slog.Warn("signals: insert 278-T row", "filer", filer, "ticker", ticker, "err", err)
			continue
		}
		if n, _ := res.RowsAffected(); n > 0 {
			inserted++
		}
	}
	slog.Info("signals: 278-T ingest complete",
		"filer", filer, "txns", len(p.Transactions), "inserted", inserted, "flagged_unparsed", flagged)
	return inserted, flagged, nil
}

// PromoteEOCoincident is the SC-24 standout cross-link (§F / S-24d). For every
// 'oge_278t' row that has a resolved sector, it looks for an 'executive_order'
// row touching the SAME sector within ±eoCoincidenceWindowDays of the
// transaction date. A match promotes the 278-T row to ALARM and appends an
// 'eo_coincident' reason — "Trump bought X and an EO touching X's sector landed
// the same week". Pure join over existing data; safe to re-run.
func (s *Service) PromoteEOCoincident(ctx context.Context) (promoted int, retErr error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT t.id, t.event_date, t.sector_universe_id, t.alarm_reasons,
		       eo.event_date, eo.notes
		  FROM signal_events t
		  JOIN signal_events eo
		    ON eo.signal_type = 'executive_order'
		   AND eo.sector_universe_id = t.sector_universe_id
		   AND ABS(julianday(eo.event_date) - julianday(t.event_date)) <= ?
		 WHERE t.signal_type = 'oge_278t'
		   AND t.sector_universe_id IS NOT NULL`, eoCoincidenceWindowDays)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type promo struct {
		id       int64
		reasons  string
		eoDate   string
		eoTitle  string
	}
	var todo []promo
	seen := map[int64]bool{}
	for rows.Next() {
		var id int64
		var tDate string
		var secID sql.NullInt64
		var reasons, eoDate, eoTitle sql.NullString
		if err := rows.Scan(&id, &tDate, &secID, &reasons, &eoDate, &eoTitle); err != nil {
			return promoted, err
		}
		if seen[id] {
			continue // one promotion per 278-T row, even if several EOs match
		}
		seen[id] = true
		todo = append(todo, promo{
			id:      id,
			reasons: reasons.String,
			eoDate:  eoDate.String,
			eoTitle: strings.TrimSpace(eoTitle.String),
		})
	}
	if err := rows.Err(); err != nil {
		return promoted, err
	}

	for _, p := range todo {
		newReasons := appendReason(p.reasons, "eo_coincident")
		_, err := s.DB.ExecContext(ctx, `
			UPDATE signal_events
			   SET tier = 'alarm', alarm_reasons = ?
			 WHERE id = ?`, newReasons, p.id)
		if err != nil {
			slog.Warn("signals: promote EO-coincident", "id", p.id, "err", err)
			continue
		}
		promoted++
	}
	if promoted > 0 {
		slog.Info("signals: 278-T EO-coincidence promotions", "promoted", promoted)
	}
	return promoted, nil
}

// resolveTrackedIndividual returns the tracked_individuals.id whose name
// matches the filer (case-insensitive), or nil if the filer isn't tracked.
func (s *Service) resolveTrackedIndividual(ctx context.Context, filer string) *int64 {
	filer = strings.TrimSpace(filer)
	if filer == "" {
		return nil
	}
	var id int64
	err := s.DB.QueryRowContext(ctx,
		`SELECT id FROM tracked_individuals WHERE LOWER(name) = LOWER(?) LIMIT 1`, filer).Scan(&id)
	if err != nil {
		return nil
	}
	return &id
}

// TrackedIndividual is the API shape for the named-watchlist panel.
type TrackedIndividual struct {
	ID               int64   `json:"id"`
	Name             string  `json:"name"`
	Role             *string `json:"role,omitempty"`
	DisclosureRegime string  `json:"disclosureRegime"` // executive_278t | congressional_ptr | none
	Notes            *string `json:"notes,omitempty"`
	SignalCount      int     `json:"signalCount"`      // matched signal_events rows
}

// ListTrackedIndividuals returns the active named watchlist with a count of
// linked signal rows. Drives the "tracked individuals" panel — rows with
// regime 'none' render the explicit "no filings — linked-tickers/news only"
// state so absence-of-data is never mistaken for no-trades (AC5 / S-24a).
func (s *Service) ListTrackedIndividuals(ctx context.Context) ([]TrackedIndividual, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT ti.id, ti.name, ti.role, ti.disclosure_regime, ti.notes,
		       (SELECT COUNT(*) FROM signal_events se WHERE se.tracked_individual_id = ti.id) AS n
		  FROM tracked_individuals ti
		 WHERE ti.active = 1
		 ORDER BY CASE ti.disclosure_regime
		            WHEN 'executive_278t' THEN 0
		            WHEN 'congressional_ptr' THEN 1
		            ELSE 2 END,
		          ti.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TrackedIndividual{}
	for rows.Next() {
		var t TrackedIndividual
		var role, notes sql.NullString
		if err := rows.Scan(&t.ID, &t.Name, &role, &t.DisclosureRegime, &notes, &t.SignalCount); err != nil {
			return nil, err
		}
		if role.Valid && strings.TrimSpace(role.String) != "" {
			t.Role = &role.String
		}
		if notes.Valid && strings.TrimSpace(notes.String) != "" {
			t.Notes = &notes.String
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// AddTrackedIndividual inserts a user-managed name into the watchlist. Regime
// must be one of executive_278t | congressional_ptr | none. Idempotent on name.
func (s *Service) AddTrackedIndividual(ctx context.Context, name, role, regime, notes string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("name is required")
	}
	switch regime {
	case "executive_278t", "congressional_ptr", "none":
	default:
		return fmt.Errorf("invalid disclosure_regime: %q", regime)
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO tracked_individuals (name, role, disclosure_regime, notes)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
		    role = excluded.role,
		    disclosure_regime = excluded.disclosure_regime,
		    notes = excluded.notes,
		    active = 1`,
		name, nullStr(strings.TrimSpace(role)), regime, nullStr(strings.TrimSpace(notes)))
	return err
}
