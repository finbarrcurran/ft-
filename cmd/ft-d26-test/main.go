// D26 — needs-review resolution test harness.
//
// 3 scenarios per v0.6.1 §B + v0.9.3 §A:
//   AC1: Acknowledge happy path
//     Seed thesis at status='needs-review' with unresolved cascade_events
//     → AcknowledgeCascade → status='locked', events resolved, history row written
//
//   AC2: Reject when status='locked' (no-op acknowledgment forbidden)
//     AcknowledgeCascade on a locked thesis → ErrNotNeedsReview
//
//   AC3: Reject when status='draft'
//     AcknowledgeCascade on a draft thesis → ErrNotNeedsReview
//
// Non-destructive ephemeral SQLite. Production DB unchanged.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"

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
	dbPath := "/tmp/ft-d26-test.db"
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
		{"D26.AC1 — Acknowledge happy path (needs-review + cascade events → locked)", testAC1},
		{"D26.AC2 — Reject when status='locked' (no-op forbidden)", testAC2},
		{"D26.AC3 — Reject when status='draft' (not applicable)", testAC3},
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
	fmt.Printf(" D26 ACs: %d PASS / %d FAIL\n", tc.pass, tc.fail)
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

func seedThesis(tc *testCtx, sym, status string) int64 {
	res, err := tc.db.ExecContext(tc.ctx, `
		INSERT INTO crypto_theses (
			coin_symbol, coin_name, primary_adapter_id, scorecard_type,
			pillar_scores_json, total_score, max_score, band, pillar_pass_gate_failed,
			holding_horizon, btc_beta, secondary_tags_json, liquidity_passed, liquidity_venues_json,
			status, version, markdown_current, rendered_html, locked_at, created_at, updated_at
		) VALUES (?, ?, ?, 'alt_18',
			?, 13, 18, 'accumulate', 1,
			'multi_year', 'high', '[]', 1, '[]',
			?, 'v1', '# fixture', '', strftime('%s','now'),
			strftime('%s','now'), strftime('%s','now'))`,
		sym, sym+" test",
		adapterID(tc, "defi"),
		`{"Q1":1,"Q2":1,"Q3":2,"Q4":1,"Q5":2,"Q6":2,"Q7":2,"Q8":0,"Q9":2}`,
		status)
	if err != nil {
		log.Fatalf("seed %s: %v", sym, err)
	}
	id, _ := res.LastInsertId()
	return id
}

func seedCascadeEvent(tc *testCtx, parentID, childID int64) int64 {
	res, _ := tc.db.ExecContext(tc.ctx, `
		INSERT INTO cascade_events
		  (triggering_thesis_id, affected_thesis_id, dependency_type, trigger_reason, action, priority)
		VALUES (?, ?, 'protocol_host', 'parent_band_strong_to_hold', 'flagged_needs_review', 'medium')`,
		parentID, childID)
	id, _ := res.LastInsertId()
	return id
}

// ----- AC tests --------------------------------------------------------------

func testAC1(tc *testCtx) error {
	parentID := seedThesis(tc, "PARENT", "locked")
	childID := seedThesis(tc, "CHILDACK", "needs-review")
	evID := seedCascadeEvent(tc, parentID, childID)
	fmt.Printf("    seeded: parent id=%d (locked), child id=%d (needs-review), cascade_event id=%d (unresolved)\n", parentID, childID, evID)

	res, err := tc.writeSvc.AcknowledgeCascade(tc.ctx, "CHILDACK", "v1", "test ack note")
	if err != nil {
		return fmt.Errorf("AcknowledgeCascade: %w", err)
	}
	fmt.Printf("    ack result: status %s→%s, events resolved=%d, history id=%d\n",
		res.PreviousStatus, res.NewStatus, res.CascadeEventsResolvedCount, res.HistoryRowID)

	// Verify status='locked'
	var status string
	tc.db.QueryRowContext(tc.ctx, `SELECT status FROM crypto_theses WHERE id=?`, childID).Scan(&status)
	if status != "locked" {
		return fmt.Errorf("status=%s, want locked", status)
	}

	// Verify cascade event resolved
	var resolvedAt sql.NullInt64
	var resolutionNote sql.NullString
	tc.db.QueryRowContext(tc.ctx, `SELECT resolved_at, resolution_note FROM cascade_events WHERE id=?`, evID).Scan(&resolvedAt, &resolutionNote)
	if !resolvedAt.Valid {
		return fmt.Errorf("cascade event %d not marked resolved", evID)
	}
	if resolutionNote.String != "test ack note" {
		return fmt.Errorf("resolution_note=%q, want 'test ack note'", resolutionNote.String)
	}

	// Verify history row written
	var nHistory int
	tc.db.QueryRowContext(tc.ctx, `
		SELECT COUNT(*) FROM crypto_thesis_history
		 WHERE thesis_id=? AND event_type='event_rescore'
		   AND event_reason LIKE 'cascade_acknowledgment:%'`, childID).Scan(&nHistory)
	if nHistory != 1 {
		return fmt.Errorf("history row count=%d, want 1", nHistory)
	}
	fmt.Printf("    ✓ status=locked, cascade event resolved, history row written\n")
	return nil
}

func testAC2(tc *testCtx) error {
	seedThesis(tc, "LOCKEDFIX", "locked")
	_, err := tc.writeSvc.AcknowledgeCascade(tc.ctx, "LOCKEDFIX", "v1", "")
	if err == nil {
		return fmt.Errorf("expected ErrNotNeedsReview on locked thesis")
	}
	if !errors.Is(err, cryptotheses.ErrNotNeedsReview) {
		return fmt.Errorf("unexpected err: %v", err)
	}
	fmt.Printf("    rejected: %s\n", err)
	return nil
}

func testAC3(tc *testCtx) error {
	seedThesis(tc, "DRAFTFIX", "draft")
	_, err := tc.writeSvc.AcknowledgeCascade(tc.ctx, "DRAFTFIX", "v1", "")
	if err == nil {
		return fmt.Errorf("expected ErrNotNeedsReview on draft thesis")
	}
	if !errors.Is(err, cryptotheses.ErrNotNeedsReview) {
		return fmt.Errorf("unexpected err: %v", err)
	}
	fmt.Printf("    rejected: %s\n", err)
	return nil
}
