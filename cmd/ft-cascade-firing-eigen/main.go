// Cascade firing tests 5.3 → 5.7 — post-EIGEN-lock acceptance.
//
// Companion to ft-cascade-firing (5.1 + 5.2 ETH→ARB/LINK), this tool exercises
// the cascade machinery on Batch 3a + EIGEN topology:
//
//   5.3  LINK→AAVE forward oracle_dependency moderate
//        LINK 16 Strong → 11 Hold (2-band drop) → AAVE fires MEDIUM
//
//   5.4  SOL→RNDR protocol_host moderate
//        SOL 13 Accumulate → 5 Trim (2-band drop) → RNDR fires MEDIUM
//
//   5.5  BUIDL is a cascade leaf (RWA terminal — no LRTs/wrappers depend on it)
//        BUIDL 17 Strong → 11 Hold (2-band drop) → ZERO children fire
//
//   5.6  ETH→EIGEN protocol_host moderate
//        ETH 13 Strong → 9 Hold (2-band drop) → EIGEN fires MEDIUM
//        (alongside ARB HIGH + LINK/AAVE/BUIDL MEDIUM — verified count)
//
//   5.7  EIGEN has zero forward cascade rows (lazy forward pattern)
//        EIGEN simulated Hold → Exit (2-band drop) → ZERO children fire
//        Validates Infrastructure adapter §3 lazy-create commitment.
//
// All tests non-destructive: snapshot original band/status, run, restore.
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

type snap struct {
	band   string
	status string
	total  int
}

func snapshot(db *sql.DB, id int64) snap {
	var s snap
	_ = db.QueryRow(`SELECT band, status, total_score FROM crypto_theses WHERE id = ?`, id).Scan(&s.band, &s.status, &s.total)
	return s
}

func restore(db *sql.DB, id int64, s snap) {
	_, _ = db.Exec(`UPDATE crypto_theses SET band = ?, status = ?, total_score = ? WHERE id = ?`, s.band, s.status, s.total, id)
}

func lookup(db *sql.DB, sym string) int64 {
	var id int64
	if err := db.QueryRow(`SELECT id FROM crypto_theses WHERE coin_symbol=? AND version='v1'`, sym).Scan(&id); err != nil {
		log.Fatalf("lookup %s: %v", sym, err)
	}
	return id
}

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

	ids := map[string]int64{}
	for _, sym := range []string{"BTC", "ETH", "ARB", "LINK", "AAVE", "SOL", "RNDR", "BUIDL", "EIGEN"} {
		ids[sym] = lookup(db, sym)
	}
	fmt.Printf("=== fixtures: BTC=%d ETH=%d ARB=%d LINK=%d AAVE=%d SOL=%d RNDR=%d BUIDL=%d EIGEN=%d ===\n\n",
		ids["BTC"], ids["ETH"], ids["ARB"], ids["LINK"], ids["AAVE"],
		ids["SOL"], ids["RNDR"], ids["BUIDL"], ids["EIGEN"])

	// === Test 5.3: LINK → AAVE forward oracle_dependency moderate ===
	fmt.Println("--- Test 5.3: LINK 16 Strong → 11 Hold (2-band drop) — AAVE forward oracle_dependency moderate ---")
	{
		snapLINK := snapshot(db, ids["LINK"])
		snapAAVE := snapshot(db, ids["AAVE"])
		defer func() { restore(db, ids["LINK"], snapLINK); restore(db, ids["AAVE"], snapAAVE) }()
		_, _ = db.Exec(`UPDATE crypto_theses SET total_score = 11, band = 'hold' WHERE id = ?`, ids["LINK"])
		evs, err := svc.CheckCascadeOnRescore(ctx, ids["LINK"], cryptotheses.BandStrong, cryptotheses.BandHold)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("    cascade fired %d event(s)\n", len(evs))
		aaveFired := 0
		for _, e := range evs {
			if e.AffectedThesisID == ids["AAVE"] {
				aaveFired++
			}
			fmt.Printf("      → child=%d type=%s action=%s priority=%s\n",
				e.AffectedThesisID, e.DependencyType, e.Action, e.Priority)
		}
		if aaveFired == 1 {
			fmt.Println("    ✓ AAVE fired via oracle_dependency moderate (2-band drop satisfies)")
		} else {
			log.Fatalf("    ✗ expected AAVE=1, got %d", aaveFired)
		}
		// Restore inline so subsequent tests start from clean state.
		restore(db, ids["LINK"], snapLINK)
		restore(db, ids["AAVE"], snapAAVE)
	}

	// === Test 5.4: SOL → RNDR protocol_host moderate ===
	fmt.Println("\n--- Test 5.4: SOL 13 Accumulate → 5 Trim (2-band drop) — RNDR protocol_host moderate ---")
	{
		snapSOL := snapshot(db, ids["SOL"])
		snapRNDR := snapshot(db, ids["RNDR"])
		defer func() { restore(db, ids["SOL"], snapSOL); restore(db, ids["RNDR"], snapRNDR) }()
		_, _ = db.Exec(`UPDATE crypto_theses SET total_score = 5, band = 'trim' WHERE id = ?`, ids["SOL"])
		evs, err := svc.CheckCascadeOnRescore(ctx, ids["SOL"], cryptotheses.BandAccumulate, cryptotheses.BandTrim)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("    cascade fired %d event(s)\n", len(evs))
		rndrFired := 0
		for _, e := range evs {
			if e.AffectedThesisID == ids["RNDR"] {
				rndrFired++
			}
			fmt.Printf("      → child=%d type=%s action=%s priority=%s\n",
				e.AffectedThesisID, e.DependencyType, e.Action, e.Priority)
		}
		if rndrFired == 1 {
			fmt.Println("    ✓ RNDR fired via protocol_host moderate (2-band drop satisfies)")
		} else {
			log.Fatalf("    ✗ expected RNDR=1, got %d", rndrFired)
		}
		restore(db, ids["SOL"], snapSOL)
		restore(db, ids["RNDR"], snapRNDR)
	}

	// === Test 5.5: BUIDL leaf — no downstream children ===
	fmt.Println("\n--- Test 5.5: BUIDL 17 Strong → 11 Hold (2-band drop) — leaf, no children fire ---")
	{
		snapBUIDL := snapshot(db, ids["BUIDL"])
		defer func() { restore(db, ids["BUIDL"], snapBUIDL) }()
		_, _ = db.Exec(`UPDATE crypto_theses SET total_score = 11, band = 'hold' WHERE id = ?`, ids["BUIDL"])
		evs, err := svc.CheckCascadeOnRescore(ctx, ids["BUIDL"], cryptotheses.BandStrong, cryptotheses.BandHold)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("    cascade fired %d event(s)\n", len(evs))
		for _, e := range evs {
			fmt.Printf("      → child=%d type=%s action=%s priority=%s\n",
				e.AffectedThesisID, e.DependencyType, e.Action, e.Priority)
		}
		if len(evs) == 0 {
			fmt.Println("    ✓ Zero children fired — BUIDL is RWA leaf, no downstream cascade dependencies in graph")
		} else {
			log.Fatalf("    ✗ expected 0 cascade events, got %d", len(evs))
		}
		restore(db, ids["BUIDL"], snapBUIDL)
	}

	// === Test 5.6: ETH → EIGEN protocol_host moderate (within multi-child cascade) ===
	fmt.Println("\n--- Test 5.6: ETH 13 Strong → 9 Hold (2-band drop) — EIGEN fires within expanded graph ---")
	{
		snapETH := snapshot(db, ids["ETH"])
		snapARB := snapshot(db, ids["ARB"])
		snapLINK := snapshot(db, ids["LINK"])
		snapAAVE := snapshot(db, ids["AAVE"])
		snapBUIDL := snapshot(db, ids["BUIDL"])
		snapEIGEN := snapshot(db, ids["EIGEN"])
		defer func() {
			restore(db, ids["ETH"], snapETH)
			restore(db, ids["ARB"], snapARB)
			restore(db, ids["LINK"], snapLINK)
			restore(db, ids["AAVE"], snapAAVE)
			restore(db, ids["BUIDL"], snapBUIDL)
			restore(db, ids["EIGEN"], snapEIGEN)
		}()
		_, _ = db.Exec(`UPDATE crypto_theses SET total_score = 9, band = 'hold' WHERE id = ?`, ids["ETH"])
		evs, err := svc.CheckCascadeOnRescore(ctx, ids["ETH"], cryptotheses.BandStrong, cryptotheses.BandHold)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("    cascade fired %d event(s)\n", len(evs))
		fired := map[string]int{}
		for _, e := range evs {
			for sym, id := range ids {
				if id == e.AffectedThesisID {
					fired[sym]++
				}
			}
			fmt.Printf("      → child=%d type=%s action=%s priority=%s\n",
				e.AffectedThesisID, e.DependencyType, e.Action, e.Priority)
		}
		want := map[string]int{"ARB": 1, "LINK": 1, "AAVE": 1, "BUIDL": 1, "EIGEN": 1}
		ok := true
		for sym, c := range want {
			if fired[sym] != c {
				ok = false
				fmt.Printf("    ✗ %s expected %d, got %d\n", sym, c, fired[sym])
			}
		}
		if ok && len(evs) == 5 {
			fmt.Println("    ✓ All 5 children fired: ARB HIGH (platform_parent) + LINK/AAVE/BUIDL/EIGEN MEDIUM (protocol_host)")
		} else {
			log.Fatalf("    ✗ expected exactly 5 fires, got %d", len(evs))
		}
		restore(db, ids["ETH"], snapETH)
		restore(db, ids["ARB"], snapARB)
		restore(db, ids["LINK"], snapLINK)
		restore(db, ids["AAVE"], snapAAVE)
		restore(db, ids["BUIDL"], snapBUIDL)
		restore(db, ids["EIGEN"], snapEIGEN)
	}

	// === Test 5.7: EIGEN forward — zero downstream rows in graph (lazy forward pattern) ===
	fmt.Println("\n--- Test 5.7: EIGEN simulated Hold → Exit (2-band drop) — zero forward rows in graph ---")
	{
		snapEIGEN := snapshot(db, ids["EIGEN"])
		defer func() { restore(db, ids["EIGEN"], snapEIGEN) }()
		_, _ = db.Exec(`UPDATE crypto_theses SET total_score = 3, band = 'exit' WHERE id = ?`, ids["EIGEN"])
		evs, err := svc.CheckCascadeOnRescore(ctx, ids["EIGEN"], cryptotheses.BandHold, cryptotheses.BandExit)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("    cascade fired %d event(s)\n", len(evs))
		for _, e := range evs {
			fmt.Printf("      → child=%d type=%s action=%s priority=%s\n",
				e.AffectedThesisID, e.DependencyType, e.Action, e.Priority)
		}
		if len(evs) == 0 {
			fmt.Println("    ✓ Zero children fired — Infrastructure adapter §3 lazy forward cascade pattern validated")
		} else {
			log.Fatalf("    ✗ expected 0 cascade events (lazy-forward), got %d", len(evs))
		}
		restore(db, ids["EIGEN"], snapEIGEN)
	}

	fmt.Println("\n=== ALL 5 CASCADE FIRING TESTS 5.3 → 5.7 PASSED ===")
}
