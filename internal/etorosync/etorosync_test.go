package etorosync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"ft/internal/store"
)

func pos(id int64, inst int, buy bool, units, sl, tp float64, noSL, noTP bool) position {
	return position{PositionID: id, InstrumentID: inst, IsBuy: buy, Units: units, OpenRate: 100 + float64(id),
		StopLossRate: sl, TakeProfitRate: tp, IsNoStopLoss: noSL, IsNoTakeProfit: noTP, Leverage: 1,
		OpenDateTime: "2026-09-01T10:00:00Z", SettlementType: 1}
}

func find(es []Effective, ticker, dir string) *Effective {
	for i := range es {
		if es[i].Ticker == ticker && es[i].Direction == dir {
			return &es[i]
		}
	}
	return nil
}

// SC-42 §2: longs take the HIGHEST stop and LOWEST take-profit; eToro's
// placeholder rates for disabled levels never count; shorts are mirrored;
// copy-trade lots are excluded from levels but counted.
func TestComputeEffective(t *testing.T) {
	lots := []lot{
		normalise(pos(1, 10, true, 2, 180, 250, false, false), 0, "GLD"),
		normalise(pos(2, 10, true, 1, 190, 240, false, false), 0, "GLD"),
		normalise(pos(3, 10, true, 1, 0.0001, 0, true, true), 0, "GLD"), // disabled: placeholders
		normalise(pos(4, 20, true, 5, 0.0001, 0, true, true), 0, "SLV"), // no SL anywhere
		normalise(pos(5, 30, false, 3, 60, 40, false, false), 0, "XOM"), // short
		normalise(pos(6, 30, false, 1, 55, 45, false, false), 0, "XOM"),
		normalise(pos(7, 10, true, 9, 500, 900, false, false), 77, "GLD"), // copy-trade lot
	}
	es := ComputeEffective(lots, 1000)
	g := find(es, "GLD", "long")
	if g == nil || *g.SLEffective != 190 || *g.TPEffective != 240 || *g.SLEffectivePositionID != 2 || *g.TPEffectivePositionID != 2 {
		t.Fatalf("GLD long: %+v", g)
	}
	if g.HasNoSL || g.LotsWithoutSL != 1 || g.LotCount != 3 || g.TotalUnits != 4 || g.CopyLotCount != 1 {
		t.Errorf("GLD counts: hasNoSL=%v withoutSL=%d lots=%d units=%v copies=%d", g.HasNoSL, g.LotsWithoutSL, g.LotCount, g.TotalUnits, g.CopyLotCount)
	}
	if s := find(es, "SLV", "long"); s == nil || !s.HasNoSL || s.SLEffective != nil || !s.HasNoTP || s.TPEffective != nil {
		t.Errorf("SLV must be flagged has_no_sl with NULL levels (not the 0.0001 placeholder): %+v", s)
	}
	x := find(es, "XOM", "short")
	if x == nil || *x.SLEffective != 55 || *x.TPEffective != 45 {
		t.Errorf("XOM short should take the LOWEST stop (55) and HIGHEST TP (45): %+v", x)
	}

	// Seen live: eToro reports a stop as ENABLED at a meaningless price (SLV
	// 0.0001, 4063.T 0.01). It must count as no stop, not become the effective SL.
	nz := ComputeEffective([]lot{
		normalise(pos(20, 40, true, 1, 0.0001, 90, false, false), 0, "SLV"),
		normalise(pos(21, 40, true, 1, 0.0001, 0, true, true), 0, "SLV"),
		normalise(pos(22, 50, true, 1, 0.01, 8500, false, false), 0, "4063.T"),
		normalise(pos(23, 50, true, 1, 80, 8600, false, false), 0, "4063.T"), // real stop (open 123)
	}, 1000)
	if s := find(nz, "SLV", "long"); s == nil || !s.HasNoSL || s.SLEffective != nil || s.LotsWithoutSL != 2 {
		t.Errorf("near-zero enabled stop must count as no stop: %+v", s)
	}
	if j := find(nz, "4063.T", "long"); j == nil || j.HasNoSL || *j.SLEffective != 80 || j.LotsWithoutSL != 1 {
		t.Errorf("4063.T: real stop 80 wins, near-zero lot counted as unprotected: %+v", j)
	}
}

type fakeEtoro struct {
	mu        sync.Mutex
	positions []position
	mirrors   []position
	instCalls int
	badAuth   bool
}

func (f *fakeEtoro) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("x-api-key") != "app-key" || r.Header.Get("x-user-key") != "user-key" || len(r.Header.Get("x-request-id")) != 36 {
		f.mu.Lock()
		f.badAuth = true
		f.mu.Unlock()
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.URL.Path == "/api/v1/trading/info/portfolio":
		var resp portfolioResp
		resp.ClientPortfolio.Positions = f.positions
		if len(f.mirrors) > 0 {
			resp.ClientPortfolio.Mirrors = append(resp.ClientPortfolio.Mirrors, struct {
				MirrorID  int64      `json:"mirrorID"`
				Positions []position `json:"positions"`
			}{MirrorID: 77, Positions: f.mirrors})
		}
		json.NewEncoder(w).Encode(resp)
	case r.URL.Path == "/api/v1/market-data/instruments":
		f.instCalls++
		names := map[string]string{"10": "GLD", "20": "slv", "30": "RHM.de", "40": "BTC", "50": "MSTR"}
		var out []map[string]any
		for _, id := range strings.Split(r.URL.Query().Get("instrumentIds"), ",") {
			if sym, ok := names[id]; ok {
				var n int
				json.Unmarshal([]byte(id), &n)
				typ := 5
				if sym == "BTC" {
					typ = etoroCryptoType
				}
				out = append(out, map[string]any{"instrumentID": n, "symbolFull": sym, "instrumentDisplayName": sym + " name", "instrumentTypeID": typ})
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"instrumentDisplayDatas": out})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestSyncEndToEnd(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "ft.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	fake := &fakeEtoro{
		positions: []position{
			pos(1, 10, true, 2, 180, 250, false, false),
			pos(2, 10, true, 1, 190, 0, false, true),
			pos(3, 20, true, 5, 0.0001, 0, true, true),
			pos(4, 30, true, 1, 150, 200, false, false),
		},
		mirrors: []position{pos(9, 10, true, 3, 100, 300, false, false)},
	}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	clock := int64(1_800_000_000)
	svc := New(st, srv.URL, "app-key", "user-key")
	svc.Now = func() time.Time { clock += 60; return time.Unix(clock, 0) }

	if r := New(st, srv.URL, "", "").Sync(ctx); r.Status != "skipped" {
		t.Fatalf("unconfigured sync = %s, want skipped", r.Status)
	}

	r1 := svc.Sync(ctx)
	if r1.Status != "ok" || r1.Positions != 5 || r1.OwnLots != 4 || r1.CopyLots != 1 || r1.LotsNew != 5 || r1.Tickers != 3 {
		t.Fatalf("first sync: %+v", r1)
	}
	if fake.badAuth {
		t.Fatal("requests must carry x-api-key, x-user-key and a UUID x-request-id")
	}
	stat, _ := svc.Current(ctx)
	gld := find(stat.Effective, "GLD", "long")
	if gld == nil || *gld.SLEffective != 190 || *gld.TPEffective != 250 || gld.CopyLotCount != 1 || gld.LotCount != 2 {
		t.Fatalf("GLD effective: %+v", gld)
	}
	if slv := find(stat.Effective, "SLV", "long"); slv == nil || !slv.HasNoSL {
		t.Errorf("SLV must be flagged has_no_sl: %+v", slv)
	}
	if find(stat.Effective, "RHM.DE", "long") == nil {
		t.Error("tickers must be normalised to FT's convention (RHM.de → RHM.DE)")
	}

	// Idempotent: same portfolio → no new history rows; instruments are cached.
	r2 := svc.Sync(ctx)
	var rows int
	st.DB.QueryRow(`SELECT count(*) FROM etoro_holdings_lots`).Scan(&rows)
	if r2.Status != "ok" || r2.LotsNew != 0 || rows != 5 || fake.instCalls != 1 {
		t.Fatalf("re-run: new=%d rows=%d instrumentCalls=%d (want 0/5/1)", r2.LotsNew, rows, fake.instCalls)
	}

	// Change a stop and close a position: one new state row, one closure.
	fake.mu.Lock()
	fake.positions[0].StopLossRate = 195
	fake.positions = fake.positions[:3] // position 4 (RHM.DE) closed
	fake.mu.Unlock()
	r3 := svc.Sync(ctx)
	var superseded, closed, open1 int
	st.DB.QueryRow(`SELECT count(*) FROM etoro_holdings_lots WHERE position_id=1 AND superseded_at IS NOT NULL`).Scan(&superseded)
	st.DB.QueryRow(`SELECT count(*) FROM etoro_holdings_lots WHERE position_id=4 AND closed_at IS NOT NULL`).Scan(&closed)
	st.DB.QueryRow(`SELECT count(*) FROM etoro_holdings_lots WHERE position_id=1 AND superseded_at IS NULL AND closed_at IS NULL AND stop_loss=195`).Scan(&open1)
	if r3.LotsNew != 1 || r3.LotsClosed != 1 || superseded != 1 || closed != 1 || open1 != 1 {
		t.Fatalf("change tracking: %+v superseded=%d closed=%d current=%d", r3, superseded, closed, open1)
	}
	stat, _ = svc.Current(ctx)
	if g := find(stat.Effective, "GLD", "long"); g == nil || *g.SLEffective != 195 {
		t.Errorf("effective SL should follow the change to 195: %+v", g)
	}
	if find(stat.Effective, "RHM.DE", "long") != nil {
		t.Error("closed ticker must drop out of the effective table")
	}

	// Guard: an empty portfolio doesn't wipe the history.
	fake.mu.Lock()
	fake.positions, fake.mirrors = nil, nil
	fake.mu.Unlock()
	r4 := svc.Sync(ctx)
	var stillOpen int
	st.DB.QueryRow(`SELECT count(*) FROM etoro_holdings_lots WHERE superseded_at IS NULL AND closed_at IS NULL`).Scan(&stillOpen)
	if r4.Status != "failed" || stillOpen != 4 {
		t.Errorf("empty-portfolio guard: status=%s open=%d (want failed / 4)", r4.Status, stillOpen)
	}
	if last, _ := svc.Current(ctx); last.LastRun.Status != "failed" || last.LastOK == nil || last.LastOK.ID != r3.RunID {
		t.Errorf("run log: last=%+v lastOK=%+v", last.LastRun, last.LastOK)
	}
}
