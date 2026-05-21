package signals

import (
	"context"
	"strings"
)

// Tier values used in signal_events.tier.
const (
	TierInfo  = "info"
	TierFlag  = "flag"
	TierAlarm = "alarm"
)

// Action values used in signal_events.action.
const (
	ActionBuy   = "BUY"
	ActionSell  = "SELL"
	ActionOrder = "ORDER" // EOs
)

// Thresholds bundles all the dollar cutoffs from Spec 9k §3. All defaults
// match the locked decisions; user overrides via user_preferences ship in
// 9k.C.
type Thresholds struct {
	InsiderBuyUSD            float64 // non-CEO/CFO BUY (default $25,000)
	InsiderSellUSD           float64 // non-CEO/CFO SELL (default $100,000)
	InsiderSellCEOCFOUSD     float64 // CEO/CFO SELL (default $250,000)
	// CEO/CFO BUY is always flagged at any size (no threshold needed).
	CongressUSD              float64 // default $15,000 (matches $15K-$50K bucket and above)
}

// DefaultThresholds returns the Spec 9k §3 locked defaults.
func DefaultThresholds() Thresholds {
	return Thresholds{
		InsiderBuyUSD:        25_000,
		InsiderSellUSD:       100_000,
		InsiderSellCEOCFOUSD: 250_000,
		CongressUSD:          15_000,
	}
}

// InsiderEvent is the shape the tier-computation function works with. The
// ingester (D3) constructs one per transaction line in a Form 4 filing.
type InsiderEvent struct {
	Ticker          string
	ActorName       string
	ActorRole       string  // typically "CEO" / "CFO" / "Director" / "10% Owner" / "Officer" — free-text from SEC
	Action          string  // "BUY" / "SELL"
	AmountUSD       float64 // shares × price-per-share
	EventDate       string  // YYYY-MM-DD
	FiledDate       string  // YYYY-MM-DD
	SourceID        string  // accession number
	UniverseHit     UniverseHit
}

// InsiderTier returns the tier for a single insider transaction (before
// cluster-buy escalation). Pure function — easily unit-testable.
//
// Spec 9k §4 D6 insider logic:
//
//   not in universe                                              → INFO
//   BUY:
//     CEO/CFO → FLAG (any size)
//     other  → FLAG if ≥ $25K, else INFO
//   SELL:
//     CEO/CFO → FLAG if ≥ $250K, else INFO
//     other  → FLAG if ≥ $100K, else INFO
//
// Cluster-buy ALARM escalation is a separate post-insert step (see
// PromoteClusterBuys below) because it depends on cross-row state.
func InsiderTier(e InsiderEvent, t Thresholds) (tier string, reasons []string) {
	reasons = []string{}
	if !e.UniverseHit.Matched {
		return TierInfo, reasons
	}
	isCXO := isCEOOrCFO(e.ActorRole)

	switch strings.ToUpper(e.Action) {
	case ActionBuy:
		if isCXO {
			reasons = append(reasons, "ceo_cfo_buy")
			return TierFlag, reasons
		}
		if e.AmountUSD >= t.InsiderBuyUSD {
			return TierFlag, reasons
		}
		return TierInfo, reasons
	case ActionSell:
		if isCXO {
			if e.AmountUSD >= t.InsiderSellCEOCFOUSD {
				reasons = append(reasons, "ceo_cfo_large_sell")
				return TierFlag, reasons
			}
			return TierInfo, reasons
		}
		if e.AmountUSD >= t.InsiderSellUSD {
			return TierFlag, reasons
		}
		return TierInfo, reasons
	}
	return TierInfo, reasons
}

// CongressEvent is the shape CongressTier consumes.
type CongressEvent struct {
	Ticker         string
	AmountUSD      float64
	UniverseHit    UniverseHit
	CommitteeMatch bool // legislator sits on a committee whose committee_sector_map
	                    // includes this ticker's sector_universe_id
}

// CongressTier computes the tier for a Congressional trade. Spec 9k §D6:
//
//   not in universe                               → INFO
//   amount < $15K                                 → INFO
//   in universe AND committee jurisdiction match  → ALARM (+ committee_match)
//   in universe, no committee match               → FLAG
func CongressTier(e CongressEvent, t Thresholds) (tier string, reasons []string) {
	reasons = []string{}
	if !e.UniverseHit.Matched {
		return TierInfo, reasons
	}
	if e.AmountUSD < t.CongressUSD {
		return TierInfo, reasons
	}
	if e.CommitteeMatch {
		reasons = append(reasons, "committee_match")
		return TierAlarm, reasons
	}
	return TierFlag, reasons
}

// isCEOOrCFO reports whether the SEC "officerTitle" / "reportingOwnerRelationship"
// text identifies the actor as Chief Executive or Chief Financial Officer.
// SEC filings vary in capitalisation and exact wording ("Chief Executive Officer",
// "President & CEO", "C.E.O.", etc.) — we cast a wide net.
func isCEOOrCFO(role string) bool {
	r := strings.ToLower(role)
	if r == "" {
		return false
	}
	// Strip common punctuation variants for the abbreviation check.
	stripped := strings.NewReplacer(".", "", " ", "").Replace(r)
	if strings.Contains(stripped, "ceo") || strings.Contains(stripped, "cfo") {
		return true
	}
	if strings.Contains(r, "chief executive") || strings.Contains(r, "chief financial") {
		return true
	}
	return false
}

// PromoteClusterBuys scans recent BUY insider events for tickers with ≥3
// distinct actors in the last 14 days. Each matching row gets:
//   - tier set to 'alarm'
//   - 'cluster_buy' appended to alarm_reasons
//
// Returns the IDs that were promoted (so the caller can push them).
// Called after each insider-ingest batch.
func (s *Service) PromoteClusterBuys(ctx context.Context) ([]int64, error) {
	// Find tickers with ≥3 distinct actor_name in the trailing 14d for BUY.
	rows, err := s.DB.QueryContext(ctx, `
		SELECT ticker
		  FROM signal_events
		 WHERE signal_type = 'insider'
		   AND action = 'BUY'
		   AND ticker IS NOT NULL
		   AND event_date >= date('now', '-14 days')
		 GROUP BY ticker
		HAVING COUNT(DISTINCT actor_name) >= 3`)
	if err != nil {
		return nil, err
	}
	var tickers []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			rows.Close()
			return nil, err
		}
		tickers = append(tickers, t)
	}
	rows.Close()
	if len(tickers) == 0 {
		return nil, nil
	}

	// Promote each affected row to alarm, append 'cluster_buy' to reasons.
	// SQLite doesn't have JSON_INSERT-style appending; we use a small
	// JSON-string concat that handles existing [] / empty / no-array.
	var promoted []int64
	for _, t := range tickers {
		updRows, err := s.DB.QueryContext(ctx, `
			SELECT id, COALESCE(alarm_reasons, '[]')
			  FROM signal_events
			 WHERE signal_type = 'insider'
			   AND action = 'BUY'
			   AND ticker = ?
			   AND event_date >= date('now', '-14 days')`, t)
		if err != nil {
			continue
		}
		type rowUpd struct {
			id      int64
			reasons string
		}
		var batch []rowUpd
		for updRows.Next() {
			var ru rowUpd
			if err := updRows.Scan(&ru.id, &ru.reasons); err == nil {
				batch = append(batch, ru)
			}
		}
		updRows.Close()
		for _, ru := range batch {
			newReasons := appendReason(ru.reasons, "cluster_buy")
			if _, err := s.DB.ExecContext(ctx, `
				UPDATE signal_events
				   SET tier = 'alarm', alarm_reasons = ?
				 WHERE id = ?
				   AND tier != 'alarm'`,
				newReasons, ru.id); err == nil {
				promoted = append(promoted, ru.id)
			}
		}
	}
	return promoted, nil
}

// appendReason adds a reason string to a JSON-encoded array, deduped.
// Tolerant of empty / NULL / unparseable input — falls back to a fresh
// single-element array.
func appendReason(existing string, reason string) string {
	existing = strings.TrimSpace(existing)
	if existing == "" || existing == "[]" || existing == "null" {
		return `["` + reason + `"]`
	}
	// Cheap substring check to avoid re-appending the same reason.
	needle := `"` + reason + `"`
	if strings.Contains(existing, needle) {
		return existing
	}
	// Insert before closing bracket.
	if strings.HasSuffix(existing, "]") {
		body := existing[:len(existing)-1]
		if body == "[" {
			return `["` + reason + `"]`
		}
		return body + `,"` + reason + `"]`
	}
	// Malformed — overwrite with a fresh single-element array.
	return `["` + reason + `"]`
}
