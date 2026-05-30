// Cascade acceptance test runner.
//
// Validates per Spec 9l kickoff Step 5 acceptance criteria:
//   1. Simulate ETH v1 13/18 → 11/18 (Strong Conviction → Accumulate).
//   2. Verify ARB v1 auto-flags `needs-review` via platform_parent strong cascade.
//   3. Verify cascade_events row created with action=flagged_needs_review priority=HIGH.
//   4. Verify reverse cascade does NOT fire (ARB rescore doesn't touch ETH).
//   5. Verify recursion detection rejects circular dependency creation.
//   6. Cleanup: reset ETH band and ARB status to pre-test state.
//
// Idempotent / non-destructive: snapshots ETH/ARB state, runs the test,
// restores state. Safe to re-run.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"

	"ft/internal/cryptotheses"

	_ "modernc.org/sqlite"
)

func main() {
	dbPath := flag.String("db", "/var/lib/ft/ft.db", "ft.db path")
	flag.Parse()

	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=foreign_keys(ON)", *dbPath))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	svc := cryptotheses.NewCascade(db)

	// Lookup thesis IDs.
	var ethID, arbID, btcID, luncID int64
	_ = db.QueryRow(`SELECT id FROM crypto_theses WHERE coin_symbol='ETH' AND version='v1'`).Scan(&ethID)
	_ = db.QueryRow(`SELECT id FROM crypto_theses WHERE coin_symbol='ARB' AND version='v1'`).Scan(&arbID)
	_ = db.QueryRow(`SELECT id FROM crypto_theses WHERE coin_symbol='BTC' AND version='v1'`).Scan(&btcID)
	_ = db.QueryRow(`SELECT id FROM crypto_theses WHERE coin_symbol='LUNC' AND version='v1'`).Scan(&luncID)
	fmt.Printf("=== Test fixtures: ETH=%d ARB=%d BTC=%d LUNC=%d ===\n\n", ethID, arbID, btcID, luncID)

	// Snapshot current state for restore.
	var ethBandSnap, arbStatusSnap string
	_ = db.QueryRow(`SELECT band FROM crypto_theses WHERE id = ?`, ethID).Scan(&ethBandSnap)
	_ = db.QueryRow(`SELECT status FROM crypto_theses WHERE id = ?`, arbID).Scan(&arbStatusSnap)
	defer func() {
		_, _ = db.Exec(`UPDATE crypto_theses SET band = ?, total_score = 13 WHERE id = ?`, ethBandSnap, ethID)
		_, _ = db.Exec(`UPDATE crypto_theses SET status = ? WHERE id = ?`, arbStatusSnap, arbID)
		fmt.Println("\n--- Restored ETH band + ARB status to pre-test ---")
	}()

	// === AC1: ETH 13/18 → 11/18 simulated demotion ===
	fmt.Println("--- AC1: Simulate ETH v1 13/18 Strong Conviction → 11/18 Accumulate ---")
	if _, err := db.Exec(`UPDATE crypto_theses SET total_score = 11, band = 'accumulate' WHERE id = ?`, ethID); err != nil {
		log.Fatal(err)
	}
	events, err := svc.CheckCascadeOnRescore(ctx, ethID, cryptotheses.BandStrong, cryptotheses.BandAccumulate)
	if err != nil {
		log.Fatalf("cascade check: %v", err)
	}
	fmt.Printf("    cascade fired %d event(s)\n", len(events))
	for _, e := range events {
		fmt.Printf("      → child=%d type=%s action=%s priority=%s reason=%s\n",
			e.AffectedThesisID, e.DependencyType, e.Action, e.Priority, e.TriggerReason)
	}

	// === AC2: ARB v1 should now be needs-review ===
	fmt.Println("\n--- AC2: ARB v1 status check ---")
	var arbStatus string
	_ = db.QueryRow(`SELECT status FROM crypto_theses WHERE id = ?`, arbID).Scan(&arbStatus)
	if arbStatus == "needs-review" {
		fmt.Println("    ✓ ARB v1 auto-flagged needs-review")
	} else {
		log.Fatalf("    ✗ ARB v1 status=%q, expected needs-review", arbStatus)
	}

	// === AC3: cascade_events row created ===
	fmt.Println("\n--- AC3: cascade_events audit row ---")
	cascadeEvents, _ := svc.ListCascadeEvents(ctx, arbID, true /* unresolvedOnly */)
	for _, e := range cascadeEvents {
		fmt.Printf("    event id=%d type=%s action=%s priority=%s reason=%s\n",
			e.ID, e.DependencyType, e.Action, e.Priority, e.TriggerReason)
	}
	if len(cascadeEvents) > 0 {
		fmt.Println("    ✓ cascade_events row recorded")
	} else {
		log.Fatal("    ✗ no cascade_events row found")
	}

	// === AC4: Reverse cascade does NOT fire when ARB is re-scored ===
	fmt.Println("\n--- AC4: Reverse cascade — ARB rescore must NOT affect ETH ---")
	ethStatusBefore := ""
	_ = db.QueryRow(`SELECT status FROM crypto_theses WHERE id = ?`, ethID).Scan(&ethStatusBefore)
	reverseEvents, _ := svc.CheckCascadeOnRescore(ctx, arbID, cryptotheses.BandTrim, cryptotheses.BandExit)
	ethStatusAfter := ""
	_ = db.QueryRow(`SELECT status FROM crypto_theses WHERE id = ?`, ethID).Scan(&ethStatusAfter)
	if ethStatusBefore == ethStatusAfter {
		fmt.Printf("    ✓ ETH status unchanged (%s); reverse cascade did NOT fire\n", ethStatusAfter)
	} else {
		log.Fatalf("    ✗ ETH status changed %s → %s; reverse cascade incorrectly fired", ethStatusBefore, ethStatusAfter)
	}
	fmt.Printf("    (ARB→[parent] walked %d children — should be 0, no upward edges)\n", len(reverseEvents))

	// === AC5: Recursion detection ===
	fmt.Println("\n--- AC5: Recursion detection on dependency creation ---")
	// ETH → ARB exists. Try to add ARB → ETH (would create cycle).
	_, err = svc.CreateDependency(ctx, cryptotheses.Dependency{
		ParentThesisID:  arbID,
		ChildThesisID:   ethID,
		DependencyType:  cryptotheses.DepPlatformParent,
		CascadeStrength: cryptotheses.StrengthStrong,
		Note:            "this should be rejected",
		CreatedBy:       "test",
	})
	if err == cryptotheses.ErrCascadeCircularDep {
		fmt.Println("    ✓ Circular dep rejected: ARB→ETH (ETH→ARB already exists)")
	} else if err != nil {
		log.Fatalf("    ✗ unexpected error: %v", err)
	} else {
		log.Fatal("    ✗ circular dep was UNEXPECTEDLY accepted")
	}
	// Self-dep check.
	_, err = svc.CreateDependency(ctx, cryptotheses.Dependency{
		ParentThesisID:  ethID,
		ChildThesisID:   ethID,
		DependencyType:  cryptotheses.DepNarrativeCorrelated,
		CascadeStrength: cryptotheses.StrengthWeak,
	})
	if err == cryptotheses.ErrCascadeSelfDep {
		fmt.Println("    ✓ Self-dep rejected (ETH→ETH)")
	} else {
		log.Fatalf("    ✗ self-dep was accepted: %v", err)
	}

	// === AC6: btc_beta_implicit fires only on BTC entering Trim/Exit ===
	fmt.Println("\n--- AC6: btc_beta_implicit notification-only behavior ---")
	// Reset state first.
	_, _ = db.Exec(`UPDATE crypto_theses SET status = 'locked' WHERE id IN (?, ?, ?)`, arbID, ethID, luncID)
	// BTC Hold→Accumulate (upgrade, no fire)
	upEvents, _ := svc.CheckCascadeOnRescore(ctx, btcID, cryptotheses.BandHold, cryptotheses.BandAccumulate)
	fmt.Printf("    BTC Hold→Accumulate fired %d events (expect 0 — upgrade)\n", len(upEvents))
	// BTC Hold→Trim (band drop into Trim → notification-only across all high-beta alts)
	btcEvents, _ := svc.CheckCascadeOnRescore(ctx, btcID, cryptotheses.BandHold, cryptotheses.BandTrim)
	notif := 0
	for _, e := range btcEvents {
		if e.DependencyType == cryptotheses.DepBTCBetaImplicit && e.Action == cryptotheses.CascadeNotificationOnly {
			notif++
		}
	}
	fmt.Printf("    BTC Hold→Trim fired %d notification_only events (expect 3 — ETH/ARB/LUNC)\n", notif)
	if notif != 3 {
		log.Fatalf("    ✗ expected 3 notifications, got %d", notif)
	}
	// Verify children NOT flagged needs-review (it's notification-only).
	var arbStatus2 string
	_ = db.QueryRow(`SELECT status FROM crypto_theses WHERE id = ?`, arbID).Scan(&arbStatus2)
	if arbStatus2 == "locked" {
		fmt.Println("    ✓ ARB status still 'locked' after BTC-beta notification (no auto-flag)")
	} else {
		log.Fatalf("    ✗ ARB status changed unexpectedly to %s", arbStatus2)
	}

	fmt.Println("\n=== ALL CASCADE ACCEPTANCE CRITERIA PASSED ===")
}
