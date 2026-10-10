package etorosync

import (
	"context"
	"errors"
	"math"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ft/internal/domain"
	"ft/internal/store"
)

func withAmount(p position, amt float64) position { p.Amount = amt; return p }

func pending(t *testing.T, svc *Service) map[string]Proposal {
	t.Helper()
	ps, err := svc.Proposals(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]Proposal{}
	for _, p := range ps {
		m[p.Kind+"/"+p.Action+"/"+p.Ticker] = p
	}
	return m
}

// SC-44: value fields sync silently; adds/removes queue for approval and
// apply only when approved; copy-trade tickers and non-eToro crypto wallets
// are never proposed.
func TestReconcile(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "ft.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`INSERT INTO users (id, email, password_hash, created_at) VALUES (1, 'fin@example.test', 'x', 0)`); err != nil {
		t.Fatal(err)
	}
	str := func(s string) *string { return &s }
	f := func(v float64) *float64 { return &v }
	gldID, _ := st.InsertStockHolding(ctx, &domain.StockHolding{UserID: 1, Name: "Gold", Ticker: str("GLD"), InvestedUSD: 100, AvgOpenPrice: f(50),
		StopLoss: f(170), StrategyNote: "keep me"})
	abbvID, _ := st.InsertStockHolding(ctx, &domain.StockHolding{UserID: 1, Name: "AbbVie", Ticker: str("ABBV"), InvestedUSD: 250})
	if _, err := st.DB.Exec(`INSERT INTO holding_theses (holding_kind, holding_id, ticker, current_version, markdown_current)
		VALUES ('stock', ?, 'ABBV', 'v1', '# thesis')`, abbvID); err != nil {
		t.Fatal(err)
	}
	st.InsertCryptoHolding(ctx, &domain.CryptoHolding{UserID: 1, Name: "Bitcoin", Symbol: "BTC", Classification: "core", Wallet: str("Ledger"), QuantityHeld: 0.5})

	fake := &fakeEtoro{
		positions: []position{
			withAmount(pos(1, 10, true, 2, 180, 250, false, false), 300), // GLD
			withAmount(pos(2, 10, true, 1, 190, 0, false, true), 150),    // GLD
			withAmount(pos(3, 20, true, 5, 0, 0, true, true), 500),       // SLV — new
			withAmount(pos(4, 30, true, 1, 150, 200, false, false), 200), // RHM.DE — new (EUR-listed; USD from eToro)
			withAmount(pos(5, 40, true, 0.01, 0, 0, true, true), 1000),   // BTC at eToro — crypto
		},
		mirrors: []position{withAmount(pos(9, 50, true, 3, 0, 0, true, true), 400)}, // MSTR via copy-trade only
	}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	clock := int64(1_800_000_000)
	svc := New(st, srv.URL, "app-key", "user-key")
	svc.Now = func() time.Time { clock += 60; return time.Unix(clock, 0) }

	r1 := svc.Sync(ctx)
	if r1.Status != "ok" || r1.ReconcileError != "" {
		t.Fatalf("sync: %+v", r1)
	}
	if r1.ValuesUpdated != 1 || r1.ProposalsNew != 4 || r1.ProposalsPending != 4 {
		t.Fatalf("first reconcile: updated=%d new=%d pending=%d (want 1/4/4)", r1.ValuesUpdated, r1.ProposalsNew, r1.ProposalsPending)
	}
	// AC2 + AC7: GLD values follow eToro's own USD figure; FT-owned fields untouched.
	g, _ := st.GetStockHolding(ctx, 1, gldID)
	wantAvg := (2*101.0 + 1*102.0) / 3
	if g.InvestedUSD != 450 || g.AvgOpenPrice == nil || math.Abs(*g.AvgOpenPrice-wantAvg) > 1e-9 {
		t.Errorf("GLD values: invested=%v avg=%v (want 450 / %v)", g.InvestedUSD, g.AvgOpenPrice, wantAvg)
	}
	if g.StopLoss == nil || *g.StopLoss != 170 || g.StrategyNote != "keep me" {
		t.Errorf("FT-owned fields must survive: sl=%v note=%q", g.StopLoss, g.StrategyNote)
	}
	p := pending(t, svc)
	for _, k := range []string{"stock/add/SLV", "stock/add/RHM.DE", "crypto/add/BTC", "stock/remove/ABBV"} {
		if _, ok := p[k]; !ok {
			t.Errorf("missing proposal %s (have %v)", k, p)
		}
	}
	if len(p) != 4 {
		t.Errorf("want exactly 4 proposals, got %v", p)
	}
	if !p["stock/remove/ABBV"].HasThesisLink {
		t.Error("ABBV removal must be flagged as thesis-linked")
	}
	if a := p["stock/add/RHM.DE"]; a.EtoroInvestedUSD == nil || *a.EtoroInvestedUSD != 200 {
		t.Errorf("RHM.DE invested must be eToro's USD amount: %+v", a)
	}
	// AC3: nothing applied yet.
	var live int
	st.DB.QueryRow(`SELECT count(*) FROM stock_holdings WHERE deleted_at IS NULL`).Scan(&live)
	if live != 2 {
		t.Fatalf("stock_holdings must be untouched before approval: %d live rows", live)
	}

	// Audit log (spec §7): the silent GLD update is recorded by the sync…
	var auditN int
	var actor, code string
	if err := st.DB.QueryRow(`SELECT count(*), max(actor), max(reason_code) FROM holdings_audit
		WHERE holding_kind='stock' AND holding_id=? AND action='update'`, gldID).Scan(&auditN, &actor, &code); err != nil ||
		auditN != 1 || actor != "etoro-sync" || code != "etoro_reconcile" {
		t.Errorf("GLD silent update audit: n=%d actor=%q code=%q err=%v", auditN, actor, code, err)
	}
	var chg string
	st.DB.QueryRow(`SELECT changes_json FROM holdings_audit WHERE holding_id=? AND action='update'`, gldID).Scan(&chg)
	if !strings.Contains(chg, `"investedUsd":{"from":100,"to":450}`) {
		t.Errorf("audit should carry the invested change: %s", chg)
	}
	st.DB.QueryRow(`SELECT count(*) FROM holdings_audit`).Scan(&auditN)
	before := auditN

	// Idempotent re-run.
	r2 := svc.Sync(ctx)
	if r2.ValuesUpdated != 0 || r2.ProposalsNew != 0 || r2.ProposalsPending != 4 {
		t.Fatalf("re-run: %+v", r2)
	}
	st.DB.QueryRow(`SELECT count(*) FROM holdings_audit`).Scan(&auditN)
	if auditN != before {
		t.Errorf("an unchanged re-sync must not write audit rows: %d -> %d", before, auditN)
	}

	// Dismiss sticks across syncs.
	if err := svc.Dismiss(ctx, p["stock/add/RHM.DE"].ID); err != nil {
		t.Fatal(err)
	}
	if r3 := svc.Sync(ctx); r3.ProposalsPending != 3 || r3.ProposalsNew != 0 {
		t.Fatalf("after dismiss: %+v", r3)
	}

	// Approvals.
	for _, k := range []string{"stock/add/SLV", "crypto/add/BTC", "stock/remove/ABBV"} {
		if msg, err := svc.Approve(ctx, p[k].ID); err != nil || strings.HasPrefix(msg, "resolved") {
			t.Fatalf("approve %s: %q %v", k, msg, err)
		}
	}
	if _, err := svc.Approve(ctx, p["stock/add/SLV"].ID); !errors.Is(err, ErrProposalNotPending) {
		t.Errorf("double approve: %v", err)
	}
	var slvInv, slvAvg float64
	if err := st.DB.QueryRow(`SELECT invested_usd, avg_open_price FROM stock_holdings WHERE ticker='SLV' AND deleted_at IS NULL`).Scan(&slvInv, &slvAvg); err != nil || slvInv != 500 || slvAvg != 103 {
		t.Errorf("SLV added: inv=%v avg=%v err=%v", slvInv, slvAvg, err)
	}
	var btcQty, btcCost float64
	if err := st.DB.QueryRow(`SELECT quantity_held, cost_basis_usd FROM crypto_holdings WHERE symbol='BTC' AND wallet='eToro' AND deleted_at IS NULL`).Scan(&btcQty, &btcCost); err != nil || btcQty != 0.01 || btcCost != 1000 {
		t.Errorf("AC6 BTC routed to crypto_holdings: qty=%v cost=%v err=%v", btcQty, btcCost, err)
	}
	var ledger float64
	st.DB.QueryRow(`SELECT quantity_held FROM crypto_holdings WHERE wallet='Ledger' AND deleted_at IS NULL`).Scan(&ledger)
	if ledger != 0.5 {
		t.Errorf("Ledger BTC must be untouched: %v", ledger)
	}
	// …and approvals are recorded as human decisions, with no entry snapshot
	// (so performance.DeriveAll never fabricates a closed trade from them).
	rows, _ := st.DB.Query(`SELECT holding_kind, action, actor, coalesce(ticker,symbol,''), changes_json FROM holdings_audit WHERE id > 0 AND actor='fin' ORDER BY id`)
	got := map[string]string{}
	for rows.Next() {
		var k, ac, who, tk, cj string
		rows.Scan(&k, &ac, &who, &tk, &cj)
		got[k+"/"+ac+"/"+tk] = cj
		if strings.Contains(cj, "trade_snapshot_json") {
			t.Errorf("approved-change audit must not carry trade_snapshot_json: %s", cj)
		}
	}
	rows.Close()
	for _, k := range []string{"stock/create/SLV", "crypto/create/BTC", "stock/soft_delete/ABBV"} {
		if _, ok := got[k]; !ok {
			t.Errorf("missing audit row %s (have %v)", k, got)
		}
	}
	if !strings.Contains(got["stock/soft_delete/ABBV"], `"hadThesisLink":true`) {
		t.Errorf("ABBV removal audit should flag the thesis link: %s", got["stock/soft_delete/ABBV"])
	}

	// AC4: soft-delete, thesis kept.
	var deleted, theses int
	st.DB.QueryRow(`SELECT deleted_at IS NOT NULL FROM stock_holdings WHERE id=?`, abbvID).Scan(&deleted)
	st.DB.QueryRow(`SELECT count(*) FROM holding_theses WHERE holding_kind='stock' AND holding_id=?`, abbvID).Scan(&theses)
	if deleted != 1 || theses != 1 {
		t.Errorf("ABBV soft-delete: deleted=%d theses=%d", deleted, theses)
	}

	r4 := svc.Sync(ctx)
	if r4.ProposalsPending != 0 || r4.ValuesUpdated != 0 {
		t.Fatalf("after approvals: %+v", r4)
	}

	// Condition clears → the dismissed proposal resolves; a recurrence is proposed afresh.
	fake.mu.Lock()
	rhm := fake.positions[3]
	fake.positions = append(fake.positions[:3], fake.positions[4])
	fake.mu.Unlock()
	svc.Sync(ctx)
	var status string
	st.DB.QueryRow(`SELECT status FROM etoro_reconcile_proposals WHERE id=?`, p["stock/add/RHM.DE"].ID).Scan(&status)
	if status != "resolved" {
		t.Errorf("RHM.DE dismissed proposal should resolve when the position closes: %s", status)
	}
	fake.mu.Lock()
	fake.positions = append(fake.positions, rhm)
	fake.mu.Unlock()
	if r := svc.Sync(ctx); r.ProposalsNew != 1 {
		t.Fatalf("recurrence: %+v", r)
	}
	// Re-verify at approval time: FT gained RHM.DE meanwhile (e.g. manual upload).
	st.InsertStockHolding(ctx, &domain.StockHolding{UserID: 1, Name: "Rheinmetall", Ticker: str("RHM.DE"), InvestedUSD: 200})
	np := pending(t, svc)["stock/add/RHM.DE"]
	if msg, err := svc.Approve(ctx, np.ID); err != nil || !strings.HasPrefix(msg, "resolved") {
		t.Errorf("stale approve should resolve, not duplicate: %q %v", msg, err)
	}
	var rhmRows int
	st.DB.QueryRow(`SELECT count(*) FROM stock_holdings WHERE ticker='RHM.DE' AND deleted_at IS NULL`).Scan(&rhmRows)
	if rhmRows != 1 {
		t.Errorf("RHM.DE rows = %d, want 1", rhmRows)
	}
	// AC5: the copy-trade-only ticker never appeared.
	var mstr int
	st.DB.QueryRow(`SELECT count(*) FROM etoro_reconcile_proposals WHERE ticker='MSTR'`).Scan(&mstr)
	if mstr != 0 {
		t.Error("copy-trade ticker must never be proposed")
	}
}
