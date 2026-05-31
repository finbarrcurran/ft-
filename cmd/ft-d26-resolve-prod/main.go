// D26 production resolution — acknowledges AAVE + BUIDL cascade flagging.
//
// Both theses are in `needs-review` state from prior cascade firing tests
// during Batch 3a + EIGEN closeout. Their cascade_events rows were cleaned
// up during testing but the status persisted. D26 service handles the
// "needs-review but no underlying cascade events" gracefully.
//
// Non-destructive in the broader sense: only flips status + writes audit
// history; pillar scores unchanged.
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

	adapters := cryptotheses.New(db)
	cascade := cryptotheses.NewCascade(db)
	writeSvc := cryptotheses.NewThesisWriteService(db, adapters, cascade)

	note := "D26 launch — historical needs-review from Batch 3a + EIGEN closeout cascade tests; pillar scores unchanged; re-locked for clean state going forward"

	for _, sym := range []string{"AAVE", "BUIDL"} {
		var status string
		_ = db.QueryRowContext(ctx, `SELECT status FROM crypto_theses WHERE coin_symbol=? AND version='v1'`, sym).Scan(&status)
		fmt.Printf("--- %s: pre-status=%s ---\n", sym, status)
		if status != "needs-review" {
			fmt.Printf("    skipped (not in needs-review)\n\n")
			continue
		}
		res, err := writeSvc.AcknowledgeCascade(ctx, sym, "v1", note)
		if err != nil {
			fmt.Printf("    ✗ FAIL: %s\n\n", err)
			continue
		}
		fmt.Printf("    ✓ acknowledged: status %s→%s, events resolved=%d, history id=%d\n\n",
			res.PreviousStatus, res.NewStatus, res.CascadeEventsResolvedCount, res.HistoryRowID)
	}

	// Final state summary
	fmt.Println("--- post-state ---")
	rows, _ := db.QueryContext(ctx, `SELECT coin_symbol, status FROM crypto_theses ORDER BY total_score DESC, coin_symbol`)
	defer rows.Close()
	for rows.Next() {
		var sym, status string
		_ = rows.Scan(&sym, &status)
		fmt.Printf("  %-6s %s\n", sym, status)
	}
}
