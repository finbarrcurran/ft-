package signals

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// Row is the API shape returned by GET /api/signals.
type Row struct {
	ID              int64   `json:"id"`
	SignalType      string  `json:"signalType"`      // insider | congress | executive_order
	Tier            string  `json:"tier"`            // info | flag | alarm
	EventDate       string  `json:"eventDate"`
	FiledDate       string  `json:"filedDate"`
	Ticker          *string `json:"ticker,omitempty"`
	ActorName       *string `json:"actorName,omitempty"`
	ActorRole       *string `json:"actorRole,omitempty"`
	Action          *string `json:"action,omitempty"`
	AmountUSD       *float64 `json:"amountUsd,omitempty"`
	AmountBucket    *string  `json:"amountBucket,omitempty"`
	Source          string   `json:"source"`
	SourceURL       *string  `json:"sourceUrl,omitempty"`
	AlarmReasons    *string  `json:"alarmReasons,omitempty"` // JSON-encoded string array
	Acknowledged    bool     `json:"acknowledged"`
	Universe        string   `json:"universe"` // 'owned' | 'watchlist' | 'sector_etf' | 'unowned'
}

// ListFilter is the query-string filter passed to List.
type ListFilter struct {
	Tier         string // "" | info | flag | alarm
	Type         string // "" | insider | congress | executive_order
	RangeDays    int    // e.g. 1, 7, 30, 90. 0 = no range filter.
	IncludeAcked bool
	Universe     string // "" | owned | watchlist | sector_etf | unowned
}

// List returns matching signal_events rows ordered by event_date DESC,
// then tier (alarm before flag before info).
func (s *Service) List(ctx context.Context, f ListFilter) ([]Row, error) {
	q := `
		SELECT id, signal_type, tier, event_date, filed_date, ticker,
		       actor_name, actor_role, action, amount_usd, amount_bucket,
		       source, source_url, alarm_reasons, acknowledged
		  FROM signal_events
		 WHERE 1=1`
	args := []any{}
	if f.Tier != "" {
		q += " AND tier = ?"
		args = append(args, f.Tier)
	}
	if f.Type != "" {
		q += " AND signal_type = ?"
		args = append(args, f.Type)
	}
	if f.RangeDays > 0 {
		cutoff := time.Now().UTC().AddDate(0, 0, -f.RangeDays).Format("2006-01-02")
		q += " AND event_date >= ?"
		args = append(args, cutoff)
	}
	if !f.IncludeAcked {
		q += " AND acknowledged = 0"
	}
	q += `
		 ORDER BY event_date DESC,
		          CASE tier WHEN 'alarm' THEN 0 WHEN 'flag' THEN 1 ELSE 2 END,
		          id DESC
		 LIMIT 500`

	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Row{}
	for rows.Next() {
		var r Row
		var ticker, actorName, actorRole, action, amountBucket, sourceURL, alarmReasons sql.NullString
		var amountUSD sql.NullFloat64
		var acked int
		if err := rows.Scan(&r.ID, &r.SignalType, &r.Tier, &r.EventDate, &r.FiledDate,
			&ticker, &actorName, &actorRole, &action, &amountUSD, &amountBucket,
			&r.Source, &sourceURL, &alarmReasons, &acked); err != nil {
			return nil, err
		}
		r.Acknowledged = acked == 1
		if ticker.Valid {
			r.Ticker = &ticker.String
		}
		if actorName.Valid {
			r.ActorName = &actorName.String
		}
		if actorRole.Valid {
			r.ActorRole = &actorRole.String
		}
		if action.Valid {
			r.Action = &action.String
		}
		if amountUSD.Valid {
			v := amountUSD.Float64
			r.AmountUSD = &v
		}
		if amountBucket.Valid {
			r.AmountBucket = &amountBucket.String
		}
		if sourceURL.Valid {
			r.SourceURL = &sourceURL.String
		}
		if alarmReasons.Valid && strings.TrimSpace(alarmReasons.String) != "" {
			r.AlarmReasons = &alarmReasons.String
		}
		// Classify against current universe so the frontend can filter
		// owned/watchlist/sector/unowned without a separate roundtrip.
		r.Universe = classifyUniverse(s, ctx, r.Ticker)
		// Apply universe filter post-classification.
		if f.Universe != "" && f.Universe != "all" && r.Universe != f.Universe {
			continue
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// classifyUniverse returns 'owned' | 'watchlist' | 'sector_etf' | 'unowned'
// for a given ticker. Returns 'unowned' for nil/empty tickers too.
func classifyUniverse(s *Service, ctx context.Context, ticker *string) string {
	if ticker == nil || strings.TrimSpace(*ticker) == "" {
		return "unowned"
	}
	hit := s.InUniverse(ctx, *ticker)
	if !hit.Matched {
		return "unowned"
	}
	switch hit.Source {
	case "holding":
		return "owned"
	case "watchlist":
		return "watchlist"
	case "sector_etf":
		return "sector_etf"
	}
	return "unowned"
}

// Counts returns per-tier counts of unacked rows in the given window.
// Used by the tier-filter chips at the top of the Signals tab.
func (s *Service) Counts(ctx context.Context, rangeDays int) (map[string]int, error) {
	q := `
		SELECT tier, COUNT(*)
		  FROM signal_events
		 WHERE acknowledged = 0`
	args := []any{}
	if rangeDays > 0 {
		cutoff := time.Now().UTC().AddDate(0, 0, -rangeDays).Format("2006-01-02")
		q += " AND event_date >= ?"
		args = append(args, cutoff)
	}
	q += " GROUP BY tier"

	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{"info": 0, "flag": 0, "alarm": 0}
	for rows.Next() {
		var tier string
		var n int
		if err := rows.Scan(&tier, &n); err != nil {
			return nil, err
		}
		out[tier] = n
	}
	return out, nil
}

// Acknowledge marks a row acknowledged. Idempotent.
func (s *Service) Acknowledge(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE signal_events
		    SET acknowledged = 1, acknowledged_at = CURRENT_TIMESTAMP
		  WHERE id = ?`, id)
	return err
}
