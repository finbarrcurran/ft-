// D25 Phase 1 acceptance harness — exercises all 12 P1 ACs against an
// ephemeral SQLite DB seeded with embedded adapters + migrations.
//
// Non-destructive: uses a tmp DB path; cleans up on exit. Does NOT touch
// /var/lib/ft/ft.db.
//
// Run: ./ft-d25-p1-test
//
// Acceptance criteria (per D25_Build_Doctrine_Handoff.md):
//   P1.AC1  POST valid DeFi draft → 201; pillar scores computed
//   P1.AC2  POST DePIN missing RYR fields → lock fails 400
//   P1.AC3  POST RWA missing Custody Tier → lock fails 400
//   P1.AC4  POST adapter sub-type not in valid sub-types → 400 (deferred — sub-type free-text)
//   P1.AC5  POST invalid Q5 mechanism enum → 400
//   P1.AC6  PUT on draft → 200; updated
//   P1.AC7  PUT on locked → 400 cannot edit
//   P1.AC8  POST /lock valid draft → 200; cascade rows created
//   P1.AC9  POST /lock with oracleDependencyParentSymbol → forward cascade row
//   P1.AC10 POST /lock with VETO condition true → band=Exit + veto_triggered
//   P1.AC11 v0.5 rounding rule applied (AAVE/EIGEN tie-break fixture)
//   P1.AC12 PPG cap applied (Q8=0 → final band one below raw)
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"

	"ft/internal/cryptotheses"
	"ft/internal/store"

	_ "modernc.org/sqlite"
)

type tcase struct {
	name string
	fn   func(*testCtx) error
}

type testCtx struct {
	db      *sql.DB
	store   *store.Store
	cryptoSvc *cryptotheses.Service
	writeSvc  *cryptotheses.ThesisWriteService
	readSvc   *cryptotheses.ThesisService
	ctx     context.Context
	pass    int
	fail    int
}

func main() {
	dbPath := "/tmp/ft-d25-p1-test.db"
	_ = os.Remove(dbPath)
	defer os.Remove(dbPath)

	st, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("store open: %v", err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	adapters := cryptotheses.New(st.DB)
	cascade := cryptotheses.NewCascade(st.DB)
	writeSvc := cryptotheses.NewThesisWriteService(st.DB, adapters, cascade)
	readSvc := cryptotheses.NewThesisService(st.DB)

	tc := &testCtx{db: st.DB, store: st, cryptoSvc: adapters, writeSvc: writeSvc, readSvc: readSvc, ctx: context.Background()}

	// Seed adapters
	if err := adapters.SeedIfEmpty(tc.ctx); err != nil {
		log.Fatalf("seed: %v", err)
	}
	// Lock all adapters so theses can reference them
	for _, slug := range []string{"btc", "l1", "l2", "defi", "infra", "depin", "rwa", "speculative"} {
		_, _ = st.DB.ExecContext(tc.ctx,
			`UPDATE crypto_adapters SET status='locked' WHERE slug=?`, slug)
	}

	// Pre-seed 3 parent theses to enable cascade auto-create tests:
	// BTC v1 (locked), ETH v1 (locked), LINK v1 (locked Infrastructure).
	mustPreseedParents(tc)

	cases := []tcase{
		{"P1.AC1 — POST valid DeFi draft, pillar scores computed", testAC1},
		{"P1.AC2 — DePIN lock missing RYR fields fails", testAC2},
		{"P1.AC3 — RWA lock missing custody tier fails", testAC3},
		{"P1.AC5 — Invalid Q5 mechanism rejected", testAC5},
		{"P1.AC6 — PUT on draft succeeds", testAC6},
		{"P1.AC7 — PUT on locked rejected", testAC7},
		{"P1.AC8 — Lock valid draft creates cascade rows", testAC8},
		{"P1.AC9 — Lock with oracle_dependency parent creates forward row", testAC9},
		{"P1.AC10 — Lock with VETO forces band=Exit", testAC10},
		{"P1.AC11 — v0.5 rounding tie-break applied (AAVE Q1)", testAC11},
		{"P1.AC12 — PPG cap one band below (Q8=0 → drop)", testAC12},
	}

	for _, c := range cases {
		fmt.Printf("--- %s ---\n", c.name)
		if err := c.fn(tc); err != nil {
			fmt.Printf("    ✗ FAIL: %s\n\n", err)
			tc.fail++
		} else {
			fmt.Printf("    ✓ PASS\n\n")
			tc.pass++
		}
	}

	fmt.Printf("==========================================\n")
	fmt.Printf(" Phase 1 ACs:  %d PASS / %d FAIL\n", tc.pass, tc.fail)
	fmt.Printf("==========================================\n")
	if tc.fail > 0 {
		os.Exit(1)
	}
}

func mustPreseedParents(tc *testCtx) {
	// We bypass DraftThesisInput here and write directly to crypto_theses
	// with locked status so the cascade-auto-create logic can find parents.
	// Pillar scores are fixture values not under test.
	adapterID := map[string]int64{}
	for _, slug := range []string{"btc", "l1", "infra"} {
		var id int64
		if err := tc.db.QueryRowContext(tc.ctx, `SELECT id FROM crypto_adapters WHERE slug=?`, slug).Scan(&id); err != nil {
			log.Fatalf("adapter %s lookup: %v", slug, err)
		}
		adapterID[slug] = id
	}
	type seed struct {
		sym, name, adapter, scorecard, band, beta, horizon, pillars string
		total int
		max   int
	}
	seeds := []seed{
		{"BTC", "Bitcoin", "btc", "monetary_12", "hold", "reference", "never_sell", `{"p1":1,"p2":1,"p3":1,"p4":1,"p5":1,"p6":1}`, 6, 12},
		{"ETH", "Ethereum", "l1", "alt_18", "strong", "high", "multi_year", `{"q1":2,"q2":2,"q3":2,"q4":1,"q5":1,"q6":2,"q7":1,"q8":1,"q9":1}`, 13, 18},
		{"LINK", "Chainlink", "infra", "alt_18", "strong", "medium", "multi_year", `{"q1":2,"q2":1,"q3":2,"q4":1,"q5":2,"q6":2,"q7":2,"q8":2,"q9":2}`, 16, 18},
	}
	for _, s := range seeds {
		_, err := tc.db.ExecContext(tc.ctx, `
			INSERT INTO crypto_theses (
				coin_symbol, coin_name, primary_adapter_id, scorecard_type,
				pillar_scores_json, total_score, max_score, band, pillar_pass_gate_failed,
				holding_horizon, btc_beta, secondary_tags_json, liquidity_passed, liquidity_venues_json,
				status, version, markdown_current, rendered_html, locked_at, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, '[]', 1, '[]', 'locked', 'v1', '# fixture', '', strftime('%s','now'), strftime('%s','now'), strftime('%s','now'))`,
			s.sym, s.name, adapterID[s.adapter], s.scorecard,
			s.pillars, s.total, s.max, s.band,
			s.horizon, s.beta)
		if err != nil {
			log.Fatalf("preseed %s: %v", s.sym, err)
		}
	}
}

func ptrFloat(f float64) *float64 { return &f }
func ptrInt(i int) *int           { return &i }
func ptrBool(b bool) *bool        { return &b }

// ----- AC tests --------------------------------------------------------------

func testAC1(tc *testCtx) error {
	in := &cryptotheses.DraftThesisInput{
		Symbol:       "TESTDEFI1",
		Version:      "v1",
		Name:         "Test DeFi Protocol",
		AdapterSlug:  "defi",
		Horizon:      "multi_year",
		BTCBeta:      "high",
		SubCriteria: map[string][]int{
			"q1": {1, 2, 2, 1}, // avg 1.5 → tie → round DOWN → 1 (v0.5.1 #4)
			"q2": {1, 2, 1, 1, 1},
			"q3": {2, 2, 2, 1, 2},
			"q4": {1, 2, 1, 1, 1},
			"q5": {2, 1, 2, 2}, "q6": {2, 1, 2, 2, 2, 1},
			"q7": {2}, "q8": {1, 1, 1, 1, 2, 1, 1, 1}, "q9": {2, 2, 2, 1, 2, 2},
		},
		Q5Mechanism:  "real_yield_staking",
		PrimaryChainSymbol: "ETH",
		MarkdownCurrent: "# Test DeFi v1\nSeed body.\n",
	}
	id, err := tc.writeSvc.CreateDraft(tc.ctx, in)
	if err != nil {
		return fmt.Errorf("CreateDraft: %w", err)
	}
	d, err := tc.readSvc.Get(tc.ctx, "TESTDEFI1", "v1")
	if err != nil {
		return fmt.Errorf("re-fetch: %w", err)
	}
	if d.Status != cryptotheses.StatusDraft {
		return fmt.Errorf("status=%s, want draft", d.Status)
	}
	if d.PillarScores["Q1"] != 1 {
		return fmt.Errorf("Q1=%d, want 1 (avg 1.5 round down per v0.5.1 #4)", d.PillarScores["Q1"])
	}
	fmt.Printf("    id=%d status=%s Q1=%d total=%d\n", id, d.Status, d.PillarScores["Q1"], d.Score)
	return nil
}

func testAC2(tc *testCtx) error {
	in := &cryptotheses.DraftThesisInput{
		Symbol: "TESTDEPIN", Version: "v1", Name: "Test DePIN",
		AdapterSlug: "depin", Horizon: "medium", BTCBeta: "high",
		Q5Mechanism: "burn_and_mint",
		SubCriteria: map[string][]int{
			"q1": {1, 1, 1, 1}, "q2": {1, 1, 1, 1, 1}, "q3": {1, 1, 1, 1, 1},
			"q4": {1, 1, 1, 1, 1}, "q5": {1, 1, 1, 1}, "q6": {1, 1, 1, 1, 1, 1},
			"q7": {1}, "q8": {1, 1, 1, 1, 1, 1, 1, 1}, "q9": {1, 1, 1, 1, 1, 1},
		},
		// Intentionally missing q4_q5_ryr + q5_paid_revenue_usd + etc.
	}
	if _, err := tc.writeSvc.CreateDraft(tc.ctx, in); err != nil {
		return fmt.Errorf("CreateDraft: %w", err)
	}
	_, err := tc.writeSvc.Lock(tc.ctx, "TESTDEPIN", "v1")
	if err == nil {
		return fmt.Errorf("expected lock to fail with missing RYR fields")
	}
	if !strings.Contains(err.Error(), "q4q5Ryr") && !strings.Contains(err.Error(), "RYR") {
		return fmt.Errorf("unexpected err: %v", err)
	}
	fmt.Printf("    lock rejected: %s\n", err)
	return nil
}

func testAC3(tc *testCtx) error {
	in := &cryptotheses.DraftThesisInput{
		Symbol: "TESTRWA", Version: "v1", Name: "Test RWA",
		AdapterSlug: "rwa", Horizon: "never_sell", BTCBeta: "low",
		Q5Mechanism: "direct_asset_claim",
		SubCriteria: map[string][]int{
			"q1": {1, 1, 1, 1}, "q2": {1, 1, 1, 1, 1}, "q3": {1, 1, 1, 1, 1},
			"q4": {1, 1, 1, 1, 1}, "q5": {1, 1, 1, 1}, "q6": {1, 1, 1, 1, 1, 1},
			"q7": {1}, "q8": {1, 1, 1, 1, 1, 1, 1, 1}, "q9": {1, 1, 1, 1, 1, 1},
		},
		Q5RABR: ptrFloat(1.0),
		Q5VerifiedAssetValueUSD: ptrFloat(1_000_000_000),
		Q5TokenSupplyAtParUSD: ptrFloat(1_000_000_000),
		Q5AuditDate: "2026-05-15", Q5Auditor: "Test Audit Co",
		// Intentionally missing q6_custody_tier
	}
	if _, err := tc.writeSvc.CreateDraft(tc.ctx, in); err != nil {
		return fmt.Errorf("CreateDraft: %w", err)
	}
	_, err := tc.writeSvc.Lock(tc.ctx, "TESTRWA", "v1")
	if err == nil {
		return fmt.Errorf("expected lock to fail with missing custody tier")
	}
	if !strings.Contains(err.Error(), "q6CustodyTier") && !strings.Contains(err.Error(), "Custody") {
		return fmt.Errorf("unexpected err: %v", err)
	}
	fmt.Printf("    lock rejected: %s\n", err)
	return nil
}

func testAC5(tc *testCtx) error {
	in := &cryptotheses.DraftThesisInput{
		Symbol: "TESTBADQ5", Version: "v1", Name: "Test Bad Q5",
		AdapterSlug: "defi", Horizon: "medium", BTCBeta: "high",
		Q5Mechanism: "bogus_mechanism_xyz",
	}
	_, err := tc.writeSvc.CreateDraft(tc.ctx, in)
	if err == nil {
		return fmt.Errorf("expected create to reject invalid q5Mechanism")
	}
	if !strings.Contains(err.Error(), "q5Mechanism") {
		return fmt.Errorf("unexpected err: %v", err)
	}
	fmt.Printf("    rejected: %s\n", err)
	return nil
}

func testAC6(tc *testCtx) error {
	// Update TESTDEFI1 draft from AC1
	in := &cryptotheses.DraftThesisInput{
		Symbol: "TESTDEFI1", Version: "v1", Name: "Updated DeFi Protocol",
		AdapterSlug: "defi", Horizon: "multi_year", BTCBeta: "high",
		Q5Mechanism:  "real_yield_staking",
		SubCriteria: map[string][]int{
			"q1": {2, 2, 2, 2}, // change avg to 2
			"q2": {1, 2, 1, 1, 1}, "q3": {2, 2, 2, 1, 2}, "q4": {1, 2, 1, 1, 1},
			"q5": {2, 1, 2, 2}, "q6": {2, 1, 2, 2, 2, 1}, "q7": {2},
			"q8": {1, 1, 1, 1, 2, 1, 1, 1}, "q9": {2, 2, 2, 1, 2, 2},
		},
		PrimaryChainSymbol: "ETH",
	}
	if err := tc.writeSvc.UpdateDraft(tc.ctx, "TESTDEFI1", "v1", in); err != nil {
		return fmt.Errorf("UpdateDraft: %w", err)
	}
	d, _ := tc.readSvc.Get(tc.ctx, "TESTDEFI1", "v1")
	if d.PillarScores["Q1"] != 2 {
		return fmt.Errorf("Q1=%d, want 2 after update", d.PillarScores["Q1"])
	}
	if d.CoinName != "Updated DeFi Protocol" {
		return fmt.Errorf("name not updated")
	}
	fmt.Printf("    name=%s Q1=%d\n", d.CoinName, d.PillarScores["Q1"])
	return nil
}

func testAC7(tc *testCtx) error {
	// LINK v1 is preseeded locked; try to update it.
	in := &cryptotheses.DraftThesisInput{
		Symbol: "LINK", Version: "v1", Name: "Tampered Name",
		AdapterSlug: "infra", Horizon: "multi_year", BTCBeta: "medium",
	}
	err := tc.writeSvc.UpdateDraft(tc.ctx, "LINK", "v1", in)
	if err == nil {
		return fmt.Errorf("expected reject on locked thesis")
	}
	if !strings.Contains(err.Error(), "locked") {
		return fmt.Errorf("unexpected err: %v", err)
	}
	fmt.Printf("    rejected: %s\n", err)
	return nil
}

func testAC8(tc *testCtx) error {
	// Use the TESTDEFI1 draft (already updated in AC6).
	res, err := tc.writeSvc.Lock(tc.ctx, "TESTDEFI1", "v1")
	if err != nil {
		return fmt.Errorf("Lock: %w", err)
	}
	if res.Total == 0 {
		return fmt.Errorf("total=0, expected > 0")
	}
	// Must have created at least the ETH→TESTDEFI1 protocol_host + BTC→TESTDEFI1 btc_beta_implicit
	if len(res.CascadeRowsCreated) < 2 {
		return fmt.Errorf("cascade rows: got %d, want >= 2 (got %v)", len(res.CascadeRowsCreated), res.CascadeRowsCreated)
	}
	fmt.Printf("    total=%d raw=%s final=%s cascade=%v\n", res.Total, res.RawBand, res.FinalBand, res.CascadeRowsCreated)
	return nil
}

func testAC9(tc *testCtx) error {
	// Create a DeFi draft declaring LINK as oracle_dependency parent
	in := &cryptotheses.DraftThesisInput{
		Symbol: "TESTORACLE", Version: "v1", Name: "Test Oracle-Dep DeFi",
		AdapterSlug: "defi", Horizon: "multi_year", BTCBeta: "high",
		Q5Mechanism:  "fee_share",
		SubCriteria: map[string][]int{
			"q1": {2, 2, 2, 2}, "q2": {1, 2, 1, 1, 1}, "q3": {2, 2, 2, 1, 2},
			"q4": {1, 2, 1, 1, 1}, "q5": {2, 1, 2, 2}, "q6": {2, 1, 2, 2, 2, 1},
			"q7": {2}, "q8": {1, 1, 1, 1, 2, 1, 1, 1}, "q9": {2, 2, 2, 1, 2, 2},
		},
		PrimaryChainSymbol: "ETH",
		OracleDependencyParentSymbol: "LINK",
		OracleDependencyParentVersion: "v1",
	}
	if _, err := tc.writeSvc.CreateDraft(tc.ctx, in); err != nil {
		return fmt.Errorf("CreateDraft: %w", err)
	}
	res, err := tc.writeSvc.Lock(tc.ctx, "TESTORACLE", "v1")
	if err != nil {
		return fmt.Errorf("Lock: %w", err)
	}
	hasOracleRow := false
	for _, c := range res.CascadeRowsCreated {
		if strings.Contains(c, "oracle_dependency") && strings.Contains(c, "LINK") {
			hasOracleRow = true
		}
	}
	if !hasOracleRow {
		return fmt.Errorf("oracle_dependency row not in cascade list: %v", res.CascadeRowsCreated)
	}
	// Verify in DB too
	var n int
	tc.db.QueryRowContext(tc.ctx, `
		SELECT COUNT(*) FROM crypto_thesis_dependencies d
		  JOIN crypto_theses p ON p.id = d.parent_thesis_id
		  JOIN crypto_theses c ON c.id = d.child_thesis_id
		 WHERE p.coin_symbol = 'LINK' AND c.coin_symbol = 'TESTORACLE'
		   AND d.dependency_type = 'oracle_dependency'`).Scan(&n)
	if n != 1 {
		return fmt.Errorf("DB row count = %d, want 1", n)
	}
	fmt.Printf("    forward cascade: %v\n", res.CascadeRowsCreated)
	return nil
}

func testAC10(tc *testCtx) error {
	in := &cryptotheses.DraftThesisInput{
		Symbol: "TESTVETO", Version: "v1", Name: "Test VETO",
		AdapterSlug: "defi", Horizon: "trade", BTCBeta: "high",
		Q5Mechanism: "governance_only",
		SubCriteria: map[string][]int{
			"q1": {2, 2, 2, 2}, "q2": {2, 2, 2, 2, 2}, "q3": {2, 2, 2, 2, 2},
			"q4": {2, 2, 2, 2, 2}, "q5": {2, 2, 2, 2}, "q6": {2, 2, 2, 2, 2, 2},
			"q7": {2}, "q8": {2, 2, 2, 2, 2, 2, 2, 2}, "q9": {2, 2, 2, 2, 2, 2},
		},
		VetoConditions: cryptotheses.VetoConditions{
			FounderRug:     true,
			FounderRugNote: "test rug scenario",
		},
		PrimaryChainSymbol: "ETH",
	}
	if _, err := tc.writeSvc.CreateDraft(tc.ctx, in); err != nil {
		return fmt.Errorf("CreateDraft: %w", err)
	}
	res, err := tc.writeSvc.Lock(tc.ctx, "TESTVETO", "v1")
	if err != nil {
		return fmt.Errorf("Lock: %w", err)
	}
	if !res.VetoTriggered {
		return fmt.Errorf("veto_triggered = false, expected true")
	}
	if res.FinalBand != cryptotheses.BandExit {
		return fmt.Errorf("final band = %s, want exit (VETO override)", res.FinalBand)
	}
	fmt.Printf("    total=%d raw=%s final=%s veto=%v reasons=%v\n",
		res.Total, res.RawBand, res.FinalBand, res.VetoTriggered, res.VetoReasons)
	return nil
}

func testAC11(tc *testCtx) error {
	// Verify the AAVE Q1 fixture (1,2,2,1 → 1) at the public computation layer.
	got := cryptotheses.ComputePillarScore(cryptotheses.SubCriteria{1, 2, 2, 1})
	if got != 1 {
		return fmt.Errorf("AAVE Q1 fixture: got %d, want 1 (v0.5.1 #4)", got)
	}
	// EIGEN Q2 multi-zero
	got2 := cryptotheses.ComputePillarScore(cryptotheses.SubCriteria{0, 0, 1, 0, 1})
	if got2 != 0 {
		return fmt.Errorf("EIGEN Q2 fixture: got %d, want 0", got2)
	}
	// 5+ pillar with 2 lows
	got3 := cryptotheses.ComputePillarScore(cryptotheses.SubCriteria{1, 2, 1, 2, 2})
	if got3 != 1 {
		return fmt.Errorf("5+ pillar 2 lows: got %d, want 1", got3)
	}
	fmt.Printf("    AAVE Q1 = 1; EIGEN Q2 = 0; 5+ pillar 2 lows = 1\n")
	return nil
}

func testAC12(tc *testCtx) error {
	// Create draft with EIGEN-like profile: Q8=0 → expect PPG cap one band below.
	in := &cryptotheses.DraftThesisInput{
		Symbol: "TESTPPG", Version: "v1", Name: "Test PPG Cap",
		AdapterSlug: "infra", Horizon: "medium", BTCBeta: "medium",
		Q5Mechanism: "governance_with_fee_switch",
		SubCriteria: map[string][]int{
			"q1": {1, 2, 2, 1},                                   // 1.5 → 1
			"q2": {2, 2, 1, 1},                                   // 1.5 tie 4-pillar → 1 (v0.5.1)
			"q3": {2, 2, 1, 2, 2},                                // 1.8 → 2
			"q4": {1, 2, 0, 2, 2},                                // zero → 1
			"q5": {1, 1, 1, 1},                                   // 1
			"q6": {2, 1, 1, 2, 2, 1},                             // 1.5 → 1 (3 subs ≤ 1 in 6-pillar)
			"q7": {2},                                            // 2
			"q8": {0, 0, 1, 1, 0, 1, 1, 1},                       // zeros → 0
			"q9": {2, 2, 2, 1, 2, 2},                             // 1.83 → 2
		},
		PrimaryChainSymbol: "ETH",
	}
	if _, err := tc.writeSvc.CreateDraft(tc.ctx, in); err != nil {
		return fmt.Errorf("CreateDraft: %w", err)
	}
	res, err := tc.writeSvc.Lock(tc.ctx, "TESTPPG", "v1")
	if err != nil {
		return fmt.Errorf("Lock: %w", err)
	}
	if !res.PPGCapApplied {
		return fmt.Errorf("PPG cap not applied; expected true (Q8=0)")
	}
	if res.RawBand == res.FinalBand {
		return fmt.Errorf("final band = raw band; expected one-below drop")
	}
	if res.FinalBand.Rank() != res.RawBand.Rank()+1 {
		return fmt.Errorf("final band drop wrong: raw=%s final=%s", res.RawBand, res.FinalBand)
	}
	fmt.Printf("    total=%d raw=%s final=%s (one-below), gates=%v\n",
		res.Total, res.RawBand, res.FinalBand, res.PPGFailedGates)
	return nil
}
