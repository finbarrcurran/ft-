// D27 fork-to-v2 acceptance tests.
//
// 5 ACs covering the fork workflow:
//   AC1 — Fork from locked → v2 draft created + v1 transitions to forked
//   AC2 — Fork from needs-review → v2 draft created (same as AC1 path)
//   AC3 — Fork from draft → ErrCannotForkDraft (no-op forbidden)
//   AC4 — Sub-criteria + adapter-specific quant fields copied to v2
//   AC5 — Cascade events on v1 auto-resolved + history rows written on both
//
// Non-destructive ephemeral SQLite.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"ft/internal/cryptotheses"
	"ft/internal/store"

	_ "modernc.org/sqlite"
)

type testCtx struct {
	db       *sql.DB
	adapters *cryptotheses.Service
	cascade  *cryptotheses.CascadeService
	writeSvc *cryptotheses.ThesisWriteService
	ctx      context.Context
	pass     int
	fail     int
}

func main() {
	dbPath := "/tmp/ft-d27-test.db"
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

	tc := &testCtx{
		db: st.DB, adapters: adapters, cascade: cascade, writeSvc: writeSvc,
		ctx: context.Background(),
	}

	if err := adapters.SeedIfEmpty(tc.ctx); err != nil {
		log.Fatalf("seed: %v", err)
	}
	for _, slug := range []string{"btc", "l1", "l2", "defi", "infra", "depin", "rwa", "speculative"} {
		_, _ = st.DB.ExecContext(tc.ctx, `UPDATE crypto_adapters SET status='locked' WHERE slug=?`, slug)
	}

	cases := []struct {
		name string
		fn   func(*testCtx) error
	}{
		{"D27.AC1 — Fork from locked → v2 draft + v1 forked", testAC1},
		{"D27.AC2 — Fork from needs-review → v2 draft", testAC2},
		{"D27.AC3 — Fork draft rejected", testAC3},
		{"D27.AC4 — Sub-criteria + adapter quant fields copied", testAC4},
		{"D27.AC5 — Cascade events auto-resolved + history rows written", testAC5},
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
	fmt.Printf(" D27 ACs: %d PASS / %d FAIL\n", tc.pass, tc.fail)
	fmt.Printf("==========================================\n")
	if tc.fail > 0 {
		os.Exit(1)
	}
}

func adapterID(tc *testCtx, slug string) int64 {
	var id int64
	if err := tc.db.QueryRowContext(tc.ctx, `SELECT id FROM crypto_adapters WHERE slug=?`, slug).Scan(&id); err != nil {
		log.Fatalf("adapter %s: %v", slug, err)
	}
	return id
}

func seedThesis(tc *testCtx, sym, adapter, status, pillarJSON string) int64 {
	res, err := tc.db.ExecContext(tc.ctx, `
		INSERT INTO crypto_theses (
			coin_symbol, coin_name, primary_adapter_id, scorecard_type,
			pillar_scores_json, total_score, max_score, band, pillar_pass_gate_failed,
			holding_horizon, btc_beta, secondary_tags_json, liquidity_passed, liquidity_venues_json,
			status, version, markdown_current, rendered_html, locked_at, created_at, updated_at
		) VALUES (?, ?, ?, 'alt_18',
			?, 13, 18, 'accumulate', 1,
			'multi_year', 'high', '[]', 1, '[]',
			?, 'v1', '# fixture body', '', strftime('%s','now'),
			strftime('%s','now'), strftime('%s','now'))`,
		sym, sym+" Co", adapterID(tc, adapter), pillarJSON, status)
	if err != nil {
		log.Fatalf("seed %s: %v", sym, err)
	}
	id, _ := res.LastInsertId()
	return id
}

// ----- Tests -----------------------------------------------------------

func testAC1(tc *testCtx) error {
	id := seedThesis(tc, "FORK1", "defi", "locked", `{"Q1":2,"Q2":1,"Q3":2,"Q4":1,"Q5":2,"Q6":2,"Q7":2,"Q8":1,"Q9":2}`)
	fmt.Printf("    seeded FORK1 v1 id=%d status=locked\n", id)

	res, err := tc.writeSvc.ForkToV2(tc.ctx, "FORK1", "v1", "test fork from locked")
	if err != nil {
		return fmt.Errorf("ForkToV2: %w", err)
	}
	fmt.Printf("    fork result: source %s %s (status %s) → new %s %s (status %s)\n",
		res.SourceSymbol, res.SourceVersion, res.SourcePreviousStatus,
		res.NewSymbol, res.NewVersion, res.NewStatus)

	// Verify source status='forked'
	var status string
	tc.db.QueryRowContext(tc.ctx, `SELECT status FROM crypto_theses WHERE id=?`, id).Scan(&status)
	if status != "forked" {
		return fmt.Errorf("source status=%s, want forked", status)
	}
	// Verify new is draft + version v2
	var newID int64
	var newStatus, newVersion string
	tc.db.QueryRowContext(tc.ctx,
		`SELECT id, status, version FROM crypto_theses WHERE coin_symbol='FORK1' AND version='v2'`).Scan(&newID, &newStatus, &newVersion)
	if newStatus != "draft" || newVersion != "v2" {
		return fmt.Errorf("new status=%s version=%s, want draft/v2", newStatus, newVersion)
	}
	if res.NewThesisID != newID {
		return fmt.Errorf("ForkResult newID=%d, DB id=%d", res.NewThesisID, newID)
	}
	fmt.Printf("    DB verify: FORK1 v1 status=forked, FORK1 v2 status=draft id=%d ✓\n", newID)
	return nil
}

func testAC2(tc *testCtx) error {
	id := seedThesis(tc, "FORK2", "defi", "needs-review", `{"Q1":2,"Q2":1,"Q3":2,"Q4":1,"Q5":2,"Q6":2,"Q7":2,"Q8":1,"Q9":2}`)
	res, err := tc.writeSvc.ForkToV2(tc.ctx, "FORK2", "v1", "test fork from needs-review")
	if err != nil {
		return fmt.Errorf("ForkToV2: %w", err)
	}
	if res.SourcePreviousStatus != "needs-review" {
		return fmt.Errorf("source previousStatus=%s, want needs-review", res.SourcePreviousStatus)
	}
	if res.NewVersion != "v2" {
		return fmt.Errorf("new version=%s, want v2", res.NewVersion)
	}
	fmt.Printf("    fork from needs-review succeeded; source id=%d → forked, new v2 id=%d → draft\n", id, res.NewThesisID)
	return nil
}

func testAC3(tc *testCtx) error {
	seedThesis(tc, "FORK3", "defi", "draft", `{"Q1":2,"Q2":1,"Q3":2,"Q4":1,"Q5":2,"Q6":2,"Q7":2,"Q8":1,"Q9":2}`)
	_, err := tc.writeSvc.ForkToV2(tc.ctx, "FORK3", "v1", "")
	if err == nil {
		return fmt.Errorf("expected ErrCannotForkDraft")
	}
	if !errors.Is(err, cryptotheses.ErrCannotForkDraft) {
		return fmt.Errorf("unexpected err: %v", err)
	}
	fmt.Printf("    rejected: %s\n", err)
	return nil
}

func testAC4(tc *testCtx) error {
	// Seed a thesis with adapter-specific RWA fields populated
	_, err := tc.db.ExecContext(tc.ctx, `
		INSERT INTO crypto_theses (
			coin_symbol, coin_name, primary_adapter_id, scorecard_type,
			pillar_scores_json, total_score, max_score, band, pillar_pass_gate_failed,
			holding_horizon, btc_beta, secondary_tags_json, liquidity_passed, liquidity_venues_json,
			q5_rabr, q5_verified_asset_value_usd, q5_token_supply_at_par_usd,
			q5_audit_date, q5_auditor,
			q6_custody_tier, q6_custody_cadence, q6_custody_jurisdiction,
			q5_mechanism,
			status, version, markdown_current, rendered_html, locked_at, created_at, updated_at
		) VALUES ('FORK4', 'FORK4 RWA', ?, 'alt_18',
			?, 17, 18, 'strong', 0,
			'never_sell', 'low', '["compound-test"]', 1, '["securitize_platform"]',
			1.0, 1000000000.0, 1000000000.0,
			'2026-05-15', 'Test Audit',
			'tier_1', 'monthly', 'US',
			'direct_asset_claim',
			'locked', 'v1', '# RWA fork test', '', strftime('%s','now'),
			strftime('%s','now'), strftime('%s','now'))`,
		adapterID(tc, "rwa"), `{"Q1":{"subs":[2,2,2,2],"score":2},"Q2":{"subs":[2,2,2,2,2],"score":2}}`)
	if err != nil {
		return fmt.Errorf("seed RWA: %w", err)
	}

	res, err := tc.writeSvc.ForkToV2(tc.ctx, "FORK4", "v1", "RWA fork test")
	if err != nil {
		return fmt.Errorf("ForkToV2: %w", err)
	}

	// Verify v2 has all adapter-specific fields copied + compound pillar shape preserved
	var v2RABR sql.NullFloat64
	var v2Tier, v2Auditor, v2Mech, v2PillarJSON sql.NullString
	tc.db.QueryRowContext(tc.ctx, `
		SELECT q5_rabr, q6_custody_tier, q5_auditor, q5_mechanism, pillar_scores_json
		  FROM crypto_theses WHERE id=?`, res.NewThesisID).Scan(
		&v2RABR, &v2Tier, &v2Auditor, &v2Mech, &v2PillarJSON)
	if !v2RABR.Valid || v2RABR.Float64 != 1.0 {
		return fmt.Errorf("v2 q5_rabr not copied: %+v", v2RABR)
	}
	if v2Tier.String != "tier_1" {
		return fmt.Errorf("v2 q6_custody_tier=%s, want tier_1", v2Tier.String)
	}
	if v2Auditor.String != "Test Audit" {
		return fmt.Errorf("v2 q5_auditor=%s, want 'Test Audit'", v2Auditor.String)
	}
	if v2Mech.String != "direct_asset_claim" {
		return fmt.Errorf("v2 q5_mechanism=%s, want direct_asset_claim", v2Mech.String)
	}
	if !strings.Contains(v2PillarJSON.String, "subs") {
		return fmt.Errorf("v2 pillar_scores_json lost compound shape: %s", v2PillarJSON.String)
	}
	fmt.Printf("    v2 inherited: q5_rabr=%g, custody=%s, auditor=%s, q5_mech=%s, compound JSON preserved\n",
		v2RABR.Float64, v2Tier.String, v2Auditor.String, v2Mech.String)
	return nil
}

func testAC5(tc *testCtx) error {
	srcID := seedThesis(tc, "FORK5", "defi", "needs-review", `{"Q1":2,"Q2":1,"Q3":2,"Q4":1,"Q5":2,"Q6":2,"Q7":2,"Q8":1,"Q9":2}`)
	// Seed a parent + cascade_event row pointing at FORK5
	parentID := seedThesis(tc, "FORK5PARENT", "defi", "locked", `{"Q1":2,"Q2":1,"Q3":2,"Q4":1,"Q5":2,"Q6":2,"Q7":2,"Q8":1,"Q9":2}`)
	_, _ = tc.db.ExecContext(tc.ctx, `
		INSERT INTO cascade_events
		  (triggering_thesis_id, affected_thesis_id, dependency_type, trigger_reason, action, priority)
		VALUES (?, ?, 'protocol_host', 'parent_band_strong_to_hold', 'flagged_needs_review', 'medium')`,
		parentID, srcID)

	res, err := tc.writeSvc.ForkToV2(tc.ctx, "FORK5", "v1", "fork with cascade events")
	if err != nil {
		return fmt.Errorf("ForkToV2: %w", err)
	}
	if res.CascadeEventsResolvedCount != 1 {
		return fmt.Errorf("events resolved=%d, want 1", res.CascadeEventsResolvedCount)
	}
	// Verify both history rows written
	var nSource, nTarget int
	tc.db.QueryRowContext(tc.ctx, `
		SELECT COUNT(*) FROM crypto_thesis_history
		 WHERE thesis_id=? AND event_type='fork_rescore' AND event_reason LIKE 'fork_source:%'`,
		srcID).Scan(&nSource)
	tc.db.QueryRowContext(tc.ctx, `
		SELECT COUNT(*) FROM crypto_thesis_history
		 WHERE thesis_id=? AND event_type='fork_rescore' AND event_reason LIKE 'fork_target:%'`,
		res.NewThesisID).Scan(&nTarget)
	if nSource != 1 || nTarget != 1 {
		return fmt.Errorf("history rows: source=%d, target=%d, want 1 each", nSource, nTarget)
	}
	fmt.Printf("    cascade events resolved=1; history rows source=1 + target=1 ✓\n")
	return nil
}
