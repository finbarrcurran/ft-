package signals

import (
	"context"
	"database/sql"
	"time"
)

// CongressFeed is the freshness of the Congress feed as the Signals tab shows it (SC-52 P5):
// Bargo's last good sync and the newest disclosure it has delivered. The legacy Capitol Trades
// rows are history only and never count toward freshness.
type CongressFeed struct {
	Source        string `json:"source"`                // always "bargo"
	LastSyncAt    string `json:"lastSyncAt,omitempty"`  // RFC3339, last ok or partial run
	LastStatus    string `json:"lastStatus,omitempty"`  // ok | partial | failed (the most recent run of any status)
	NewestFiled   string `json:"newestFiled,omitempty"` // newest disclosure date delivered by Bargo
	NewestTrade   string `json:"newestTrade,omitempty"` // newest transaction date delivered by Bargo
	BargoRows     int    `json:"bargoRows"`
	LegacyRows    int    `json:"legacyRows"` // Capitol Trades history kept for reference
	HouseMembers  int    `json:"houseMembers"`
	SenateMembers int    `json:"senateMembers"`
}

// CongressFeedStatus reads the Bargo sync log and the raw table. Read-only.
func (s *Service) CongressFeedStatus(ctx context.Context) (CongressFeed, error) {
	out := CongressFeed{Source: "bargo"}
	var fin sql.NullInt64
	if err := s.DB.QueryRowContext(ctx, `SELECT MAX(finished_at) FROM bargo_sync_runs WHERE status IN ('ok','partial')`).Scan(&fin); err != nil {
		return out, err
	}
	if fin.Valid {
		out.LastSyncAt = time.Unix(fin.Int64, 0).UTC().Format(time.RFC3339)
	}
	var st sql.NullString
	if err := s.DB.QueryRowContext(ctx, `SELECT status FROM bargo_sync_runs ORDER BY id DESC LIMIT 1`).Scan(&st); err != nil && err != sql.ErrNoRows {
		return out, err
	}
	out.LastStatus = st.String
	var nf, nt sql.NullString
	if err := s.DB.QueryRowContext(ctx, `SELECT MAX(disclosure_date), MAX(transaction_date), COUNT(*) FROM congress_trades_raw WHERE source = 'bargo'`).Scan(&nf, &nt, &out.BargoRows); err != nil {
		return out, err
	}
	out.NewestFiled, out.NewestTrade = nf.String, nt.String
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM congress_trades_raw WHERE source = 'capitol_trades'`).Scan(&out.LegacyRows); err != nil {
		return out, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT chamber, COUNT(DISTINCT member_slug) FROM congress_trades_raw WHERE source = 'bargo' GROUP BY chamber`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var ch sql.NullString
		var n int
		if err := rows.Scan(&ch, &n); err != nil {
			return out, err
		}
		switch ch.String {
		case "house":
			out.HouseMembers = n
		case "senate":
			out.SenateMembers = n
		}
	}
	return out, rows.Err()
}
