// Cascade firing test per Claude_Code_Ingestion_4_Theses.md Step 5(4).
//
// Verifies the protocol_host moderate-strength trigger behaves correctly:
//   - ETH 13/18 Strong → 11/18 Accumulate (single band drop) →
//     ARB fires (platform_parent strong), LINK does NOT (protocol_host
//     moderate needs 2+ bands)
//   - ETH 13/18 Strong → 9/18 Hold (2-band drop) →
//     ARB fires, LINK ALSO fires (2+ bands satisfies protocol_host moderate)
//
// Non-destructive — snapshots ETH band + ARB/LINK status, runs test, restores.
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

	var ethID, arbID, linkID int64
	_ = db.QueryRow(`SELECT id FROM crypto_theses WHERE coin_symbol='ETH' AND version='v1'`).Scan(&ethID)
	_ = db.QueryRow(`SELECT id FROM crypto_theses WHERE coin_symbol='ARB' AND version='v1'`).Scan(&arbID)
	_ = db.QueryRow(`SELECT id FROM crypto_theses WHERE coin_symbol='LINK' AND version='v1'`).Scan(&linkID)
	fmt.Printf("=== fixtures: ETH=%d ARB=%d LINK=%d ===\n\n", ethID, arbID, linkID)

	// Snapshot original ETH band + ARB/LINK status.
	var ethBandSnap, arbStatusSnap, linkStatusSnap string
	_ = db.QueryRow(`SELECT band FROM crypto_theses WHERE id = ?`, ethID).Scan(&ethBandSnap)
	_ = db.QueryRow(`SELECT status FROM crypto_theses WHERE id = ?`, arbID).Scan(&arbStatusSnap)
	_ = db.QueryRow(`SELECT status FROM crypto_theses WHERE id = ?`, linkID).Scan(&linkStatusSnap)
	defer func() {
		_, _ = db.Exec(`UPDATE crypto_theses SET band = ?, total_score = 13 WHERE id = ?`, ethBandSnap, ethID)
		_, _ = db.Exec(`UPDATE crypto_theses SET status = ? WHERE id = ?`, arbStatusSnap, arbID)
		_, _ = db.Exec(`UPDATE crypto_theses SET status = ? WHERE id = ?`, linkStatusSnap, linkID)
		fmt.Println("\n--- Restored ETH band + ARB/LINK status to pre-test ---")
	}()

	// === Test 1: ETH 13/18 Strong → 11/18 Accumulate (1-band drop) ===
	fmt.Println("--- Test 1: ETH 13/18 Strong → 11/18 Accumulate (single band drop) ---")
	_, _ = db.Exec(`UPDATE crypto_theses SET total_score = 11, band = 'accumulate' WHERE id = ?`, ethID)
	events1, err := svc.CheckCascadeOnRescore(ctx, ethID, cryptotheses.BandStrong, cryptotheses.BandAccumulate)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("    cascade fired %d event(s)\n", len(events1))
	arbFire := 0
	linkFire := 0
	for _, e := range events1 {
		if e.AffectedThesisID == arbID {
			arbFire++
		}
		if e.AffectedThesisID == linkID {
			linkFire++
		}
		fmt.Printf("      → child=%d type=%s action=%s priority=%s\n",
			e.AffectedThesisID, e.DependencyType, e.Action, e.Priority)
	}
	if arbFire == 1 && linkFire == 0 {
		fmt.Println("    ✓ ARB fired (platform_parent strong); LINK did NOT fire (protocol_host moderate needs 2+ bands)")
	} else {
		log.Fatalf("    ✗ expected ARB=1, LINK=0, got ARB=%d LINK=%d", arbFire, linkFire)
	}
	// Reset ARB status to locked before test 2.
	_, _ = db.Exec(`UPDATE crypto_theses SET status = 'locked' WHERE id = ?`, arbID)

	// === Test 2: ETH 13/18 Strong → 9/18 Hold (2-band drop) ===
	fmt.Println("\n--- Test 2: ETH 13/18 Strong → 9/18 Hold (2-band drop) ---")
	_, _ = db.Exec(`UPDATE crypto_theses SET total_score = 9, band = 'hold' WHERE id = ?`, ethID)
	events2, err := svc.CheckCascadeOnRescore(ctx, ethID, cryptotheses.BandStrong, cryptotheses.BandHold)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("    cascade fired %d event(s)\n", len(events2))
	arbFire2 := 0
	linkFire2 := 0
	for _, e := range events2 {
		if e.AffectedThesisID == arbID {
			arbFire2++
		}
		if e.AffectedThesisID == linkID {
			linkFire2++
		}
		fmt.Printf("      → child=%d type=%s action=%s priority=%s\n",
			e.AffectedThesisID, e.DependencyType, e.Action, e.Priority)
	}
	if arbFire2 == 1 && linkFire2 == 1 {
		fmt.Println("    ✓ Both ARB (platform_parent HIGH) and LINK (protocol_host MEDIUM) fired on 2-band drop")
	} else {
		log.Fatalf("    ✗ expected ARB=1, LINK=1, got ARB=%d LINK=%d", arbFire2, linkFire2)
	}

	// Verify LINK status changed to needs-review.
	var linkStatus string
	_ = db.QueryRow(`SELECT status FROM crypto_theses WHERE id = ?`, linkID).Scan(&linkStatus)
	if linkStatus == "needs-review" {
		fmt.Println("    ✓ LINK status updated to needs-review")
	} else {
		log.Fatalf("    ✗ LINK status=%q, expected needs-review", linkStatus)
	}

	fmt.Println("\n=== ALL CASCADE FIRING TEST CASES PASSED ===")
}
