// Seed ARB v1 locked thesis + auto-create platform_parent cascade row
// (ETH v1 → ARB v1, strong) + btc_beta_implicit cascade rows for all
// 4 locked theses to BTC v1.
//
// Idempotent: ON CONFLICT DO UPDATE on (coin_symbol, version) for thesis;
// ON CONFLICT DO NOTHING on (parent, child, dep_type) for dependencies.
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"ft/internal/cryptotheses"

	_ "modernc.org/sqlite"
)

func main() {
	dbPath := flag.String("db", "/var/lib/ft/ft.db", "ft.db path")
	dir := flag.String("dir", "/tmp", "directory holding ARB_v1_locked.md")
	flag.Parse()

	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)", *dbPath))
	if err != nil {
		log.Fatalf("open: %v", err)
	}
	defer db.Close()

	// Lookup adapter IDs.
	var l2ID int64
	if err := db.QueryRow(`SELECT id FROM crypto_adapters WHERE slug = 'l2'`).Scan(&l2ID); err != nil {
		log.Fatalf("l2 adapter not found: %v", err)
	}

	// Lookup thesis IDs for cascade insertion.
	var ethID, btcID, lunc int64
	_ = db.QueryRow(`SELECT id FROM crypto_theses WHERE coin_symbol='ETH' AND version='v1'`).Scan(&ethID)
	_ = db.QueryRow(`SELECT id FROM crypto_theses WHERE coin_symbol='BTC' AND version='v1'`).Scan(&btcID)
	_ = db.QueryRow(`SELECT id FROM crypto_theses WHERE coin_symbol='LUNC' AND version='v1'`).Scan(&lunc)
	if ethID == 0 || btcID == 0 {
		log.Fatalf("missing parent theses; eth=%d btc=%d", ethID, btcID)
	}

	// Read ARB v1 markdown.
	md, err := os.ReadFile(filepath.Join(*dir, "ARB_v1_locked.md"))
	if err != nil {
		log.Fatalf("read ARB v1 MD: %v", err)
	}
	mdStr := string(md)
	html := cryptotheses.Render(mdStr)

	pillarJSON, _ := json.Marshal(map[string]int{
		"Q1": 2, "Q2": 0, "Q3": 2, "Q4": 1, "Q5": 0, "Q6": 1, "Q7": 1, "Q8": 0, "Q9": 1,
	})
	tagsJSON, _ := json.Marshal([]string{"optimistic", "platform_parent_test_fixture", "first-cascade-demo"})
	nextReview := time.Date(2026, 6, 12, 0, 30, 0, 0, time.UTC).Unix() // bi-weekly Medium
	now := time.Now().Unix()

	// 0% Q5 accrual mechanically (governance-only); annual_usd = 0, fdv = $1.13B.
	q5Accrual := 0.0
	res, err := db.Exec(`
		INSERT INTO crypto_theses (
			coin_symbol, coin_name, coingecko_id,
			primary_adapter_id, scorecard_type,
			pillar_scores_json, total_score, max_score, band, pillar_pass_gate_failed,
			q5_mechanism, q5_annual_usd, q5_fdv_usd, q5_accrual_pct,
			q9_team_note,
			active_veto, active_veto_reason,
			catalyst_date, catalyst_note,
			holding_horizon, btc_beta, secondary_tags_json,
			liquidity_passed, liquidity_venues_json, liquidity_checked_at,
			status, version, markdown_current, rendered_html,
			locked_at, last_reviewed_at, next_review_at,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, ?, ?, ?, ?, ?, 1, ?, ?, 'locked', 'v1', ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(coin_symbol, version) DO UPDATE SET
		  pillar_scores_json = excluded.pillar_scores_json,
		  total_score = excluded.total_score,
		  band = excluded.band,
		  pillar_pass_gate_failed = excluded.pillar_pass_gate_failed,
		  q5_mechanism = excluded.q5_mechanism,
		  q5_annual_usd = excluded.q5_annual_usd,
		  q5_fdv_usd = excluded.q5_fdv_usd,
		  q5_accrual_pct = excluded.q5_accrual_pct,
		  q9_team_note = excluded.q9_team_note,
		  holding_horizon = excluded.holding_horizon,
		  markdown_current = excluded.markdown_current,
		  rendered_html = excluded.rendered_html,
		  updated_at = excluded.updated_at`,
		"ARB", "Arbitrum One", "arbitrum",
		l2ID, "alt_18",
		string(pillarJSON), 8, 18, "trim", 1,
		"governance_only", 0.0, 1130000000.0, q5Accrual,
		"Offchain Labs (Ed Felten ex-Princeton/US Deputy CTO, Harry Kalodner, Steven Goldfeder). Consistent multi-year delivery: Nitro 2022, BoLD 2024, Stylus 2025, Timeboost 2025. Sequencer operator single-party with public decentralization roadmap. Team/investor vesting through March 2027 (~13% supply unlock next 12mo).",
		"2026-08-15", "Glamsterdam upgrade H1 2026 + L2BEAT Stage 2 progression watch",
		"medium", "high", string(tagsJSON),
		`["kraken","coinbase","binance"]`, now,
		mdStr, html,
		now, now, nextReview,
		now, now)
	if err != nil {
		log.Fatalf("insert ARB: %v", err)
	}
	arbID, _ := res.LastInsertId()
	if arbID == 0 {
		_ = db.QueryRow(`SELECT id FROM crypto_theses WHERE coin_symbol='ARB' AND version='v1'`).Scan(&arbID)
	}
	fmt.Printf("ARB v1: id=%d, 8/18 raw → Trim band via PPG cap (Q2=0, Q5=0, Q8=0), governance_only Q5\n", arbID)

	// Cascade dependencies.
	// 1. platform_parent: ETH v1 → ARB v1 (strong) — first formal cascade.
	if _, err := db.Exec(`
		INSERT INTO crypto_thesis_dependencies (parent_thesis_id, child_thesis_id, dependency_type, cascade_strength, note, created_by)
		VALUES (?, ?, 'platform_parent', 'strong', 'Auto-created on ARB v1 lock per L2 adapter §7. First formal cascade in framework.', 'system')
		ON CONFLICT(parent_thesis_id, child_thesis_id, dependency_type) DO NOTHING`,
		ethID, arbID); err != nil {
		log.Fatalf("insert platform_parent dep: %v", err)
	}
	fmt.Printf("  cascade row: ETH v1 (id=%d) → ARB v1 (id=%d) [platform_parent, strong]\n", ethID, arbID)

	// 2. btc_beta_implicit: BTC v1 → all high-beta alts (weak).
	for _, child := range []struct {
		id     int64
		symbol string
	}{{ethID, "ETH"}, {arbID, "ARB"}, {lunc, "LUNC"}} {
		if child.id == 0 {
			continue
		}
		if _, err := db.Exec(`
			INSERT INTO crypto_thesis_dependencies (parent_thesis_id, child_thesis_id, dependency_type, cascade_strength, note, created_by)
			VALUES (?, ?, 'btc_beta_implicit', 'weak', 'Auto-created from high BTC-beta tag on thesis lock', 'system')
			ON CONFLICT(parent_thesis_id, child_thesis_id, dependency_type) DO NOTHING`,
			btcID, child.id); err != nil {
			log.Fatalf("insert btc_beta_implicit for %s: %v", child.symbol, err)
		}
		fmt.Printf("  cascade row: BTC v1 → %s [btc_beta_implicit, weak]\n", child.symbol)
	}

	// Summary.
	fmt.Println("\n--- crypto_theses ---")
	rows, _ := db.Query(`
		SELECT t.coin_symbol, a.slug, t.total_score, t.max_score, t.band, t.holding_horizon, t.pillar_pass_gate_failed,
		       COALESCE(t.active_veto, '-')
		  FROM crypto_theses t JOIN crypto_adapters a ON a.id = t.primary_adapter_id
		 ORDER BY t.id`)
	defer rows.Close()
	for rows.Next() {
		var sym, slug, band, horizon, veto string
		var total, max, pgf int
		_ = rows.Scan(&sym, &slug, &total, &max, &band, &horizon, &pgf, &veto)
		fmt.Printf("  %-4s adapter=%-12s %d/%d %s horizon=%-10s ppg_fail=%d veto=%s\n", sym, slug, total, max, band, horizon, pgf, veto)
	}
	fmt.Println("\n--- crypto_thesis_dependencies ---")
	drows, _ := db.Query(`
		SELECT p.coin_symbol || ' v' || p.version, c.coin_symbol || ' v' || c.version, d.dependency_type, d.cascade_strength
		  FROM crypto_thesis_dependencies d
		  JOIN crypto_theses p ON p.id = d.parent_thesis_id
		  JOIN crypto_theses c ON c.id = d.child_thesis_id
		 ORDER BY d.id`)
	defer drows.Close()
	for drows.Next() {
		var parent, child, dtype, strength string
		_ = drows.Scan(&parent, &child, &dtype, &strength)
		fmt.Printf("  %-10s → %-10s [%s, %s]\n", parent, child, dtype, strength)
	}
}
