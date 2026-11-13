package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// SC-57: /api/watchlist always carries marketStats; unavailable values are null, never an error.
func TestWatchlist_MarketStatsNullsAndValues(t *testing.T) {
	s, st := snapServer(t)
	mustExec(t, st, `INSERT INTO watchlist (user_id, ticker, kind, added_at, updated_at) VALUES (1, 'AUR', 'stock', 0, 0), (1, 'BESI', 'stock', 0, 0), (1, 'HYPE', 'crypto', 0, 0)`)
	mustExec(t, st, `INSERT INTO ticker_market_stats (ticker, high_52w, low_52w, ath, ath_date, hist_from, hist_to, chart_source, chart_as_of, market_cap, cap_source, cap_as_of, updated_at)
		VALUES ('AUR', 9.5, 2.1, 14.9, '2021-10-01', '2021-09-01', '2026-11-01', 'yahoo', 1793000000, 8e9, 'yahoo', 1793000100, 0)`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/watchlist", nil).WithContext(context.WithValue(context.Background(), ctxUserID, int64(1)))
	s.handleListWatchlist(rec, req)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Watchlist []struct {
			Ticker      string         `json:"ticker"`
			MarketStats map[string]any `json:"marketStats"`
		} `json:"watchlist"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	got := map[string]map[string]map[string]any{}
	for _, r := range out.Watchlist {
		if r.MarketStats == nil {
			t.Fatalf("%s: marketStats missing", r.Ticker)
		}
		blk := map[string]map[string]any{}
		for k, v := range r.MarketStats {
			if m, ok := v.(map[string]any); ok {
				blk[k] = m
			}
		}
		got[r.Ticker] = blk
	}
	a := got["AUR"]
	for _, k := range []string{"high52w", "low52w", "ath", "marketCap"} {
		if a[k]["value"] == nil || a[k]["source"] != "yahoo" || a[k]["asOf"] == nil {
			t.Fatalf("AUR %s: %v", k, a[k])
		}
	}
	if a["ath"]["historyFrom"] != "2021-09-01" || a["ath"]["historyTo"] != "2026-11-01" || a["ath"]["date"] != "2021-10-01" {
		t.Fatalf("ath labels %v", a["ath"])
	}
	for _, tk := range []string{"BESI", "HYPE"} {
		for _, k := range []string{"high52w", "low52w", "ath", "marketCap"} {
			if got[tk][k]["value"] != nil {
				t.Fatalf("%s %s must be null: %v", tk, k, got[tk][k])
			}
		}
	}
}

// FT-082: the shown price and the in-range flag come from the daily stats pass when it has a price; the stored
// one-time quote is the fallback; a row with neither stays priceless. The stored entry is never rewritten.
func TestWatchlist_PriceFromStatsPass(t *testing.T) {
	s, st := snapServer(t)
	mustExec(t, st, `INSERT INTO watchlist (user_id, ticker, kind, current_price, target_entry_low, target_entry_high, added_at, updated_at)
		VALUES (1, 'BE', 'stock', 281.76, 150, 200, 0, 0), (1, 'OLD', 'stock', 50, 40, 60, 0, 0), (1, 'NEW', 'stock', NULL, NULL, NULL, 0, 0)`)
	mustExec(t, st, `INSERT INTO ticker_market_stats (ticker, chart_source, chart_as_of, price, price_as_of, updated_at)
		VALUES ('BE', 'yahoo', 1793000000, 173.24, 1793000000, 0), ('NEW', 'yahoo', 1793000000, 12.5, 1793000000, 0)`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/watchlist", nil).WithContext(context.WithValue(context.Background(), ctxUserID, int64(1)))
	s.handleListWatchlist(rec, req)
	var out struct {
		Watchlist []struct {
			Ticker       string   `json:"ticker"`
			CurrentPrice *float64 `json:"currentPrice"`
			PriceAsOf    *int64   `json:"priceAsOf"`
			InRange      bool     `json:"inRange"`
		} `json:"watchlist"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	for _, r := range out.Watchlist {
		switch r.Ticker {
		case "BE":
			if r.CurrentPrice == nil || *r.CurrentPrice != 173.24 || r.PriceAsOf == nil || !r.InRange {
				t.Fatalf("BE must use the fresh 173.24 (in 150-200): %+v", r)
			}
		case "OLD":
			if r.CurrentPrice == nil || *r.CurrentPrice != 50 || r.PriceAsOf != nil {
				t.Fatalf("OLD keeps its stored price with no as-of: %+v", r)
			}
		case "NEW":
			if r.CurrentPrice == nil || *r.CurrentPrice != 12.5 {
				t.Fatalf("NEW had no price and must get the stats one: %+v", r)
			}
		}
	}
	var stored float64
	if err := st.DB.QueryRow(`SELECT current_price FROM watchlist WHERE ticker='BE'`).Scan(&stored); err != nil || stored != 281.76 {
		t.Fatalf("stored quote must be untouched: %v %v", stored, err)
	}
}

// SC-57 v1.110.1: the "history shorter than 52 weeks" note must not fire on a rounding difference (TSLA: all-time
// high 498.8299865 against a 52-week high of 498.83) but must fire when the all-time high is really lower.
func TestWatchlist_ShortHistoryNoteTolerance(t *testing.T) {
	s, st := snapServer(t)
	mustExec(t, st, `DELETE FROM watchlist`) // migrations seed a few rows
	mustExec(t, st, `INSERT INTO watchlist (user_id, ticker, kind, added_at, updated_at) VALUES (1, 'TSLA', 'stock', 0, 0), (1, 'DJTWW', 'stock', 0, 0)`)
	mustExec(t, st, `INSERT INTO ticker_market_stats (ticker, high_52w, ath, chart_source, chart_as_of, updated_at) VALUES ('TSLA', 498.83, 498.8299865, 'yahoo', 1, 0), ('DJTWW', 31.99, 2.59, 'yahoo', 1, 0)`)
	rec := httptest.NewRecorder()
	s.handleListWatchlist(rec, httptest.NewRequest("GET", "/api/watchlist", nil).WithContext(context.WithValue(context.Background(), ctxUserID, int64(1))))
	var out struct {
		Watchlist []struct {
			Ticker      string `json:"ticker"`
			MarketStats struct {
				Note string `json:"note"`
			} `json:"marketStats"`
		} `json:"watchlist"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	for _, r := range out.Watchlist {
		hasNote := r.MarketStats.Note != ""
		if r.Ticker == "TSLA" && hasNote {
			t.Fatalf("TSLA must have no short-history note: %q", r.MarketStats.Note)
		}
		if r.Ticker == "DJTWW" && !hasNote {
			t.Fatal("DJTWW (ATH far below the 52-week high) must keep the note")
		}
	}
}
