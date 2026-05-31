// D25 Phase 3 — End-to-end integration scenarios.
//
// Six scenarios per D25_Build_Doctrine_Handoff.md §"Phase 3 — Integration":
//
//   A: Standard DeFi thesis creation
//      Create → Save draft → Re-edit → Re-save → Lock → Verify cascade rows
//   B: DePIN thesis with mandatory RYR
//      Create with NULL RYR → Lock fails → Populate RYR → Lock succeeds
//   C: RWA thesis with mandatory RABR + Custody
//      Create with Tier 3 → Lock succeeds (soft warning only) → Verify
//   D: VETO triggered thesis
//      Create with FounderRug=true → Lock → Verify band=Exit override
//   E: PPG cap thesis (EIGEN-like Q8=0)
//      Create with Q8=0 → Lock → Verify final = raw - 1
//   F: Cascade firing verification
//      Lock new DeFi thesis declaring LINK as oracle parent
//      → Simulate LINK band drop (Strong → Hold, 2-band)
//      → Verify new thesis auto-flags needs-review via forward oracle_dependency
//
// Distinct from Phase 1 ACs which were atomic single-step. Phase 3 chains
// multiple steps together as one motion.
//
// Non-destructive: uses ephemeral SQLite at /tmp/ft-d25-p3-test.db.
//
// Run: ./ft-d25-p3-test
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"

	"ft/internal/cryptotheses"
	"ft/internal/store"

	_ "modernc.org/sqlite"
)

type testCtx struct {
	db       *sql.DB
	store    *store.Store
	adapters *cryptotheses.Service
	writeSvc *cryptotheses.ThesisWriteService
	readSvc  *cryptotheses.ThesisService
	cascade  *cryptotheses.CascadeService
	ctx      context.Context
	pass     int
	fail     int
}

func main() {
	dbPath := "/tmp/ft-d25-p3-test.db"
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

	tc := &testCtx{
		db: st.DB, store: st,
		adapters: adapters, writeSvc: writeSvc, readSvc: readSvc, cascade: cascade,
		ctx: context.Background(),
	}

	// Seed adapters + lock them.
	if err := adapters.SeedIfEmpty(tc.ctx); err != nil {
		log.Fatalf("seed adapters: %v", err)
	}
	for _, slug := range []string{"btc", "l1", "l2", "defi", "infra", "depin", "rwa", "speculative"} {
		_, _ = st.DB.ExecContext(tc.ctx, `UPDATE crypto_adapters SET status='locked' WHERE slug=?`, slug)
	}

	// Pre-seed BTC + ETH + LINK as locked parents.
	mustPreseedParents(tc)

	scenarios := []struct {
		name string
		fn   func(*testCtx) error
	}{
		{"A — Standard DeFi creation (create → save → edit → save → lock → cascade verify)", scenarioA},
		{"B — DePIN mandatory RYR (NULL fails → populate → lock succeeds)", scenarioB},
		{"C — RWA mandatory RABR + Custody (Tier 3 soft warning, locks anyway)", scenarioC},
		{"D — VETO triggered (FounderRug → band=Exit override)", scenarioD},
		{"E — PPG cap (EIGEN-like Q8=0 → final = raw - 1)", scenarioE},
		{"F — Cascade firing (lock DeFi+LINK parent → drop LINK → DeFi flagged needs-review)", scenarioF},
	}

	for _, s := range scenarios {
		fmt.Printf("=== Scenario %s ===\n", s.name)
		if err := s.fn(tc); err != nil {
			fmt.Printf("    ✗ FAIL: %s\n\n", err)
			tc.fail++
		} else {
			fmt.Printf("    ✓ PASS\n\n")
			tc.pass++
		}
	}

	fmt.Printf("===========================================\n")
	fmt.Printf(" Phase 3 scenarios:  %d PASS / %d FAIL\n", tc.pass, tc.fail)
	fmt.Printf("===========================================\n")
	if tc.fail > 0 {
		os.Exit(1)
	}
}

func mustPreseedParents(tc *testCtx) {
	adapterID := map[string]int64{}
	for _, slug := range []string{"btc", "l1", "infra"} {
		var id int64
		if err := tc.db.QueryRowContext(tc.ctx, `SELECT id FROM crypto_adapters WHERE slug=?`, slug).Scan(&id); err != nil {
			log.Fatalf("adapter %s: %v", slug, err)
		}
		adapterID[slug] = id
	}
	seeds := []struct {
		sym, name, adapter, scorecard, band, beta, horizon, pillars string
		total, max                                                  int
	}{
		{"BTC", "Bitcoin", "btc", "monetary_12", "hold", "reference", "never_sell", `{"P1":1,"P2":1,"P3":1,"P4":1,"P5":1,"P6":1}`, 6, 12},
		{"ETH", "Ethereum", "l1", "alt_18", "strong", "high", "multi_year", `{"Q1":2,"Q2":2,"Q3":2,"Q4":1,"Q5":1,"Q6":2,"Q7":1,"Q8":1,"Q9":1}`, 13, 18},
		{"LINK", "Chainlink", "infra", "alt_18", "strong", "medium", "multi_year", `{"Q1":2,"Q2":1,"Q3":2,"Q4":1,"Q5":2,"Q6":2,"Q7":2,"Q8":2,"Q9":2}`, 16, 18},
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
			s.pillars, s.total, s.max, s.band, s.horizon, s.beta)
		if err != nil {
			log.Fatalf("preseed %s: %v", s.sym, err)
		}
	}
}

func ptrFloat(f float64) *float64 { return &f }
func ptrInt(i int) *int           { return &i }

// makeAltSubs returns sub-criteria producing a known pillar score per scoring.go rules.
func validAltSubs() map[string][]int {
	return map[string][]int{
		"q1": {2, 2, 2, 1},                   // avg 1.75 → 2
		"q2": {1, 2, 1, 2, 1},                // avg 1.4 → 1 (3 lows in 5+ → DOWN)
		"q3": {2, 2, 2, 1, 2},                // avg 1.8 → 2
		"q4": {1, 2, 1, 1, 1},                // avg 1.2 → 1 (3 lows in 5+ → DOWN)
		"q5": {2, 1, 2, 2},                   // avg 1.75 → 2
		"q6": {2, 1, 2, 2, 2, 1},             // avg 1.67 → 2
		"q7": {2},                            // 2
		"q8": {1, 1, 1, 1, 2, 1, 1, 1},       // avg 1.125 → 1 (lots of lows in 5+ → DOWN to 1)
		"q9": {2, 2, 2, 1, 2, 2},             // avg 1.83 → 2
	}
}

// ----- Scenarios -----------------------------------------------------------

func scenarioA(tc *testCtx) error {
	// Step 1: Create draft
	in := &cryptotheses.DraftThesisInput{
		Symbol: "P3DEFI", Version: "v1", Name: "Phase3 DeFi Test",
		AdapterSlug: "defi", Horizon: "multi_year", BTCBeta: "high",
		Q5Mechanism:  "real_yield_staking",
		SubCriteria:  validAltSubs(),
		PrimaryChainSymbol: "ETH",
		MarkdownCurrent: "# Phase3 DeFi v1\nIntegration scenario A.\n",
	}
	id, err := tc.writeSvc.CreateDraft(tc.ctx, in)
	if err != nil {
		return fmt.Errorf("step 1 CreateDraft: %w", err)
	}
	fmt.Printf("    step 1: draft created id=%d\n", id)

	// Step 2: Re-edit (change horizon)
	in.Horizon = "medium"
	in.SubCriteria["q1"] = []int{1, 2, 2, 1} // change to tie-break fixture
	if err := tc.writeSvc.UpdateDraft(tc.ctx, "P3DEFI", "v1", in); err != nil {
		return fmt.Errorf("step 2 UpdateDraft: %w", err)
	}
	fmt.Printf("    step 2: draft updated (horizon→medium, q1=[1,2,2,1])\n")

	// Step 3: Verify intermediate state shows Q1=1 via tie-break
	mid, err := tc.readSvc.Get(tc.ctx, "P3DEFI", "v1")
	if err != nil {
		return fmt.Errorf("step 3 Get: %w", err)
	}
	if mid.PillarScores["Q1"] != 1 {
		return fmt.Errorf("step 3 Q1=%d, want 1 (v0.5.1 #4 tie-break)", mid.PillarScores["Q1"])
	}
	fmt.Printf("    step 3: intermediate read Q1=%d ✓\n", mid.PillarScores["Q1"])

	// Step 4: Lock
	res, err := tc.writeSvc.Lock(tc.ctx, "P3DEFI", "v1")
	if err != nil {
		return fmt.Errorf("step 4 Lock: %w", err)
	}
	fmt.Printf("    step 4: locked total=%d raw=%s final=%s cascade=%v\n",
		res.Total, res.RawBand, res.FinalBand, res.CascadeRowsCreated)

	// Step 5: Verify status=locked + cascade rows in DB
	var status string
	if err := tc.db.QueryRowContext(tc.ctx, `SELECT status FROM crypto_theses WHERE id=?`, id).Scan(&status); err != nil {
		return err
	}
	if status != "locked" {
		return fmt.Errorf("step 5 status=%s, want locked", status)
	}
	var nCascade int
	if err := tc.db.QueryRowContext(tc.ctx,
		`SELECT COUNT(*) FROM crypto_thesis_dependencies WHERE child_thesis_id=?`, id).Scan(&nCascade); err != nil {
		return err
	}
	if nCascade < 2 {
		return fmt.Errorf("step 5 cascade rows=%d, want >= 2", nCascade)
	}
	fmt.Printf("    step 5: status=locked, %d cascade rows in DB ✓\n", nCascade)
	return nil
}

func scenarioB(tc *testCtx) error {
	// Step 1: Create draft with NULL RYR fields
	in := &cryptotheses.DraftThesisInput{
		Symbol: "P3DEPIN", Version: "v1", Name: "Phase3 DePIN Test",
		AdapterSlug: "depin", Horizon: "medium", BTCBeta: "high",
		Q5Mechanism: "burn_and_mint",
		SubCriteria: validAltSubs(),
	}
	if _, err := tc.writeSvc.CreateDraft(tc.ctx, in); err != nil {
		return fmt.Errorf("step 1 CreateDraft: %w", err)
	}
	fmt.Printf("    step 1: draft created with NULL RYR fields\n")

	// Step 2: Lock with NULL RYR → fails with explicit field name
	if _, err := tc.writeSvc.Lock(tc.ctx, "P3DEPIN", "v1"); err == nil {
		return fmt.Errorf("step 2 lock should have failed with NULL RYR")
	} else {
		fmt.Printf("    step 2: lock rejected as expected — %s\n", err)
	}

	// Step 3: Verify status still draft (lock atomicity)
	var status string
	tc.db.QueryRowContext(tc.ctx, `SELECT status FROM crypto_theses WHERE coin_symbol='P3DEPIN'`).Scan(&status)
	if status != "draft" {
		return fmt.Errorf("step 3 status=%s after failed lock, want draft", status)
	}
	fmt.Printf("    step 3: status remains draft after failed lock ✓\n")

	// Step 4: Populate RYR fields and re-attempt lock
	in.Q4Q5RYR = ptrFloat(0.35)
	in.Q5PaidRevenueUSD = ptrFloat(1_400_000)
	in.Q5EmissionsUSD = ptrFloat(4_000_000)
	in.NetworkAgeMonths = ptrInt(30)
	if err := tc.writeSvc.UpdateDraft(tc.ctx, "P3DEPIN", "v1", in); err != nil {
		return fmt.Errorf("step 4 UpdateDraft: %w", err)
	}
	res, err := tc.writeSvc.Lock(tc.ctx, "P3DEPIN", "v1")
	if err != nil {
		return fmt.Errorf("step 4 Lock after populate: %w", err)
	}
	fmt.Printf("    step 4: lock succeeds after RYR populate — total=%d band=%s\n", res.Total, res.FinalBand)
	return nil
}

func scenarioC(tc *testCtx) error {
	// RWA with Tier 3 custody (soft warning, should still lock)
	in := &cryptotheses.DraftThesisInput{
		Symbol: "P3RWA", Version: "v1", Name: "Phase3 RWA Test",
		AdapterSlug: "rwa", Horizon: "never_sell", BTCBeta: "low",
		Q5Mechanism: "direct_asset_claim",
		SubCriteria: validAltSubs(),
		Q5RABR:                  ptrFloat(1.0),
		Q5VerifiedAssetValueUSD: ptrFloat(1_000_000_000),
		Q5TokenSupplyAtParUSD:   ptrFloat(1_000_000_000),
		Q5AuditDate:             "2026-04-15",
		Q5Auditor:               "Phase3 Audit Co",
		Q6CustodyTier:           "tier_3", // soft warning
		Q6CustodyCadence:        "annual",
		Q6CustodyJurisdiction:   "CAYMAN",
	}
	if _, err := tc.writeSvc.CreateDraft(tc.ctx, in); err != nil {
		return fmt.Errorf("step 1 CreateDraft: %w", err)
	}
	fmt.Printf("    step 1: draft created with Tier 3 custody\n")

	// Step 2: Lock (should succeed — Tier 3 is soft warning, not blocker)
	res, err := tc.writeSvc.Lock(tc.ctx, "P3RWA", "v1")
	if err != nil {
		return fmt.Errorf("step 2 Lock should succeed despite Tier 3: %w", err)
	}
	fmt.Printf("    step 2: lock succeeded — total=%d band=%s (Tier 3 soft warning per doctrine)\n", res.Total, res.FinalBand)

	// Step 3: Verify Custody Tier stored
	var tier string
	tc.db.QueryRowContext(tc.ctx, `SELECT COALESCE(q6_custody_tier,'') FROM crypto_theses WHERE coin_symbol='P3RWA'`).Scan(&tier)
	if tier != "tier_3" {
		return fmt.Errorf("step 3 stored tier=%s, want tier_3", tier)
	}
	fmt.Printf("    step 3: q6_custody_tier persisted as tier_3 ✓\n")
	return nil
}

func scenarioD(tc *testCtx) error {
	// All-2 scoring (would otherwise be Strong 18/18) + VETO triggered → band=Exit
	in := &cryptotheses.DraftThesisInput{
		Symbol: "P3VETO", Version: "v1", Name: "Phase3 VETO Test",
		AdapterSlug: "defi", Horizon: "trade", BTCBeta: "high",
		Q5Mechanism: "governance_only",
		SubCriteria: map[string][]int{
			"q1": {2, 2, 2, 2}, "q2": {2, 2, 2, 2, 2}, "q3": {2, 2, 2, 2, 2},
			"q4": {2, 2, 2, 2, 2}, "q5": {2, 2, 2, 2}, "q6": {2, 2, 2, 2, 2, 2},
			"q7": {2}, "q8": {2, 2, 2, 2, 2, 2, 2, 2}, "q9": {2, 2, 2, 2, 2, 2},
		},
		VetoConditions: cryptotheses.VetoConditions{
			FounderRug:     true,
			FounderRugNote: "P3 scenario D — VETO test fixture",
		},
		PrimaryChainSymbol: "ETH",
	}
	if _, err := tc.writeSvc.CreateDraft(tc.ctx, in); err != nil {
		return fmt.Errorf("step 1 CreateDraft: %w", err)
	}
	res, err := tc.writeSvc.Lock(tc.ctx, "P3VETO", "v1")
	if err != nil {
		return fmt.Errorf("step 2 Lock: %w", err)
	}
	if !res.VetoTriggered {
		return fmt.Errorf("step 2 VetoTriggered=false, want true")
	}
	if res.FinalBand != cryptotheses.BandExit {
		return fmt.Errorf("step 2 finalBand=%s, want exit (VETO override)", res.FinalBand)
	}
	// Step 3: Verify DB band='exit' + active_veto='founder_rug' stored
	var band, veto string
	tc.db.QueryRowContext(tc.ctx,
		`SELECT band, COALESCE(active_veto,'') FROM crypto_theses WHERE coin_symbol='P3VETO'`).Scan(&band, &veto)
	if band != "exit" || veto != "founder_rug" {
		return fmt.Errorf("step 3 band=%s veto=%s, want exit/founder_rug", band, veto)
	}
	fmt.Printf("    end-to-end: raw=%s VETO=%v final=%s active_veto=%s (band=Exit overrides 18/18 strong) ✓\n",
		res.RawBand, res.VetoTriggered, band, veto)
	return nil
}

func scenarioE(tc *testCtx) error {
	// EIGEN-like Q8=0 → PPG cap drops final one band below raw
	in := &cryptotheses.DraftThesisInput{
		Symbol: "P3PPG", Version: "v1", Name: "Phase3 PPG Cap Test",
		AdapterSlug: "infra", Horizon: "medium", BTCBeta: "medium",
		Q5Mechanism: "governance_with_fee_switch",
		SubCriteria: map[string][]int{
			"q1": {1, 2, 2, 1},                // 1.5 tie → 1
			"q2": {2, 2, 2, 1},                // 1.75 → 2
			"q3": {2, 2, 1, 2, 2},             // 1.8 → 2
			"q4": {2, 2, 1, 2, 2},             // 1.8 → 2
			"q5": {1, 1, 1, 1},                // 1
			"q6": {2, 1, 2, 2, 2, 2},          // 1.83 → 2
			"q7": {2},
			"q8": {0, 0, 1, 1, 0, 1, 1, 1},    // zeros → 0 (PPG fail)
			"q9": {2, 2, 2, 1, 2, 2},          // 1.83 → 2
		},
		PrimaryChainSymbol: "ETH",
	}
	if _, err := tc.writeSvc.CreateDraft(tc.ctx, in); err != nil {
		return fmt.Errorf("step 1 CreateDraft: %w", err)
	}
	res, err := tc.writeSvc.Lock(tc.ctx, "P3PPG", "v1")
	if err != nil {
		return fmt.Errorf("step 2 Lock: %w", err)
	}
	if !res.PPGCapApplied {
		return fmt.Errorf("step 2 PPGCapApplied=false; expected true (Q8=0)")
	}
	if res.RawBand.Rank()+1 != res.FinalBand.Rank() {
		return fmt.Errorf("step 2 raw=%s final=%s — expected exactly one band below", res.RawBand, res.FinalBand)
	}
	fmt.Printf("    end-to-end: total=%d raw=%s final=%s (one-below via PPG cap; Q8 in failedGates=%v) ✓\n",
		res.Total, res.RawBand, res.FinalBand, res.PPGFailedGates)
	return nil
}

func scenarioF(tc *testCtx) error {
	// Step 1: Create + lock DeFi thesis declaring LINK as oracle parent
	in := &cryptotheses.DraftThesisInput{
		Symbol: "P3CASC", Version: "v1", Name: "Phase3 Cascade Test",
		AdapterSlug: "defi", Horizon: "multi_year", BTCBeta: "high",
		Q5Mechanism:                  "real_yield_staking",
		SubCriteria:                  validAltSubs(),
		PrimaryChainSymbol:           "ETH",
		OracleDependencyParentSymbol: "LINK",
		OracleDependencyParentVersion: "v1",
	}
	if _, err := tc.writeSvc.CreateDraft(tc.ctx, in); err != nil {
		return fmt.Errorf("step 1 CreateDraft: %w", err)
	}
	res, err := tc.writeSvc.Lock(tc.ctx, "P3CASC", "v1")
	if err != nil {
		return fmt.Errorf("step 2 Lock: %w", err)
	}
	hasOracle := false
	for _, c := range res.CascadeRowsCreated {
		if c == "LINK → P3CASC [oracle_dependency, moderate]" {
			hasOracle = true
		}
	}
	if !hasOracle {
		return fmt.Errorf("step 2 forward oracle_dependency not in cascade rows: %v", res.CascadeRowsCreated)
	}
	var p3id int64
	tc.db.QueryRowContext(tc.ctx, `SELECT id FROM crypto_theses WHERE coin_symbol='P3CASC'`).Scan(&p3id)
	fmt.Printf("    step 2: locked id=%d, forward oracle_dependency row created\n", p3id)

	// Step 3: Simulate LINK band drop (Strong 16/18 → Hold ~8/18) → 2-band drop
	var linkID int64
	tc.db.QueryRowContext(tc.ctx, `SELECT id FROM crypto_theses WHERE coin_symbol='LINK'`).Scan(&linkID)
	_, _ = tc.db.ExecContext(tc.ctx, `UPDATE crypto_theses SET total_score=8, band='hold' WHERE id=?`, linkID)
	events, err := tc.cascade.CheckCascadeOnRescore(tc.ctx, linkID, cryptotheses.BandStrong, cryptotheses.BandHold)
	if err != nil {
		return fmt.Errorf("step 3 cascade: %w", err)
	}
	firedP3 := false
	for _, e := range events {
		if e.AffectedThesisID == p3id && e.DependencyType == cryptotheses.DepOracleDependency {
			firedP3 = true
		}
	}
	if !firedP3 {
		return fmt.Errorf("step 3 P3CASC did not fire via oracle_dependency: events=%+v", events)
	}
	fmt.Printf("    step 3: LINK band drop fired %d cascade events; P3CASC flagged via oracle_dependency ✓\n", len(events))

	// Step 4: Verify P3CASC status flipped to needs-review
	var statusAfter string
	tc.db.QueryRowContext(tc.ctx, `SELECT status FROM crypto_theses WHERE id=?`, p3id).Scan(&statusAfter)
	if statusAfter != "needs-review" {
		return fmt.Errorf("step 4 P3CASC status=%s, want needs-review", statusAfter)
	}
	fmt.Printf("    step 4: P3CASC status=needs-review ✓\n")
	return nil
}
