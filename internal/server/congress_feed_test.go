package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// SC-52 P5: the Signals tab reads Bargo's freshness (last good sync, newest disclosure), counts the
// legacy Capitol Trades rows as history only, and a failed latest run is visible.
func TestSignalsReportBargoFeedFreshness(t *testing.T) {
	s, st := snapServer(t)
	ctx := context.Background()
	fin := time.Date(2026, 10, 18, 23, 10, 5, 0, time.UTC).Unix()
	mustExec(t, st, `INSERT INTO bargo_sync_runs (started_at, finished_at, status, requests, rows_fetched, rows_new) VALUES (?, ?, 'ok', 3, 100, 8)`, fin-5, fin)
	mustExec(t, st, `INSERT INTO bargo_sync_runs (started_at, finished_at, status, requests, error) VALUES (?, ?, 'failed', 1, 'bargo: 500')`, fin+86400-5, fin+86400)
	ins := func(source, key, member, chamber, filed, trade string) {
		mustExec(t, st, `INSERT INTO congress_trades_raw (source, source_key, member, member_slug, chamber, ticker, trade_type, transaction_date, disclosure_date, first_seen_at, last_seen_at) VALUES (?,?,?,?,?,?,?,?,?,1,1)`,
			source, key, member, member, chamber, "AAPL", "purchase", trade, filed)
	}
	ins("bargo", "b1", "a", "house", "2026-10-15", "2026-10-12")
	ins("bargo", "b2", "b", "house", "2026-10-10", "2026-10-02")
	ins("bargo", "b3", "c", "senate", "2026-09-30", "2026-09-04")
	ins("capitol_trades", "c1", "x", "house", "2026-08-05", "2026-08-05") // newer than nothing: history only
	rec := httptest.NewRecorder()
	s.handleListSignals(rec, httptest.NewRequest(http.MethodGet, "/api/signals", nil).WithContext(ctx))
	var out struct {
		Feed struct {
			LastSyncAt, LastStatus, NewestFiled, NewestTrade   string
			BargoRows, LegacyRows, HouseMembers, SenateMembers int
		} `json:"congressFeed"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	f := out.Feed
	if f.LastSyncAt != "2026-10-18T23:10:05Z" || f.LastStatus != "failed" || f.NewestFiled != "2026-10-15" || f.NewestTrade != "2026-10-12" ||
		f.BargoRows != 3 || f.LegacyRows != 1 || f.HouseMembers != 2 || f.SenateMembers != 1 {
		t.Fatalf("feed: %+v (body %s)", f, rec.Body.String())
	}
}
