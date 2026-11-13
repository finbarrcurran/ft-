package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// FT-080: stock tax lots come from the eToro lots by ticker; a synthetic opening_position row keyed to the same
// holding id but another ticker is NOT read; the lots add up to the Stocks-tab P&L for a USD name and for a
// non-USD name (price and rate in local currency).
func TestStockTaxLots_FromEtoroLotsByTicker(t *testing.T) {
	s, st := snapServer(t)
	ctx := context.Background()
	mustExec(t, st, `INSERT INTO stock_holdings (id, user_id, name, ticker, invested_usd, avg_open_price, current_price, updated_at)
		VALUES (23, 1, 'PAAS', 'PAAS', 85, 61.2, 37.72, 0), (25, 1, '4063.T', '4063.T', 50, 7601, 5704, 0)`)
	// the cross-wired synthetic row: holding id 23 carries 4063.T's opening position
	mustExec(t, st, `INSERT INTO transactions (holding_kind, holding_id, ticker, txn_type, executed_at, quantity, price_usd, fees_usd, total_usd, venue, created_at)
		VALUES ('stock', 23, '4063.T', 'opening_position', 0, 0.00657808, 7601, 0, 50, 'pre-spec-10-backfill', 0)`)
	for _, l := range []struct {
		pid int
		tk  string
		d   string
		u   float64
		r   float64
		a   float64
	}{{1, "PAAS", "2026-02-01T10:00:00.000Z", 1.388888, 61.2, 85}, {2, "4063.T", "2026-03-01T10:00:00.000Z", 1.034337, 7601, 50}} {
		mustExec(t, st, `INSERT INTO etoro_holdings_lots (position_id, instrument_id, ticker, is_buy, units, open_rate, open_date, leverage, mirror_id, first_seen_at, last_seen_at, amount_usd)
			VALUES (?, 1, ?, 1, ?, ?, ?, 1, 0, '', '', ?)`, l.pid, l.tk, l.u, l.r, l.d, l.a)
	}
	get := func(id string) map[string]any {
		req := httptest.NewRequest("GET", "/api/holdings/stocks/"+id+"/taxlots", nil).WithContext(context.WithValue(context.Background(), ctxUserID, int64(1)))
		req.SetPathValue("id", id)
		rec := httptest.NewRecorder()
		s.handleStockTaxLots(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out["position"].(map[string]any)
	}
	_ = ctx
	paas := get("23")
	lots := paas["taxLots"].([]any)
	if len(lots) != 1 {
		t.Fatalf("PAAS must have its own single lot, got %v", lots)
	}
	l := lots[0].(map[string]any)
	if l["pricePerUnit"].(float64) != 61.2 {
		t.Fatalf("PAAS lot price must be 61.2 (not 4063.T's 7601): %v", l["pricePerUnit"])
	}
	want := 85 * (37.72/61.2 - 1) // Stocks tab: invested * (price/avg - 1)
	if got := l["unrealizedPnlUsd"].(float64); got < want-0.01 || got > want+0.01 {
		t.Fatalf("PAAS unrealised %.2f, Stocks-tab formula %.2f", got, want)
	}
	jp := get("25")["taxLots"].([]any)[0].(map[string]any)
	wantJP := 50 * (5704.0/7601 - 1)
	if got := jp["unrealizedPnlUsd"].(float64); got < wantJP-0.01 || got > wantJP+0.01 {
		t.Fatalf("4063.T unrealised %.2f, Stocks-tab formula %.2f", got, wantJP)
	}
}
