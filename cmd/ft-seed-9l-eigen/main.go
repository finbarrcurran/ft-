// Seeder for EIGEN v1 locked thesis + 2 cascade rows.
//
// 12th locked thesis in Spec 9l framework. Second Infrastructure seed thesis
// (first non-oracle Infrastructure sub-type — restaking). First thesis with
// structurally-broken value capture mechanism (Q5=0, ELIP-12 PROPOSED not
// active). First thesis with multi-zero pillar pattern (Q2=0, Q5=0, Q8=0).
// Tests lazy forward cascade creation pattern (no forward oracle_dependency
// rows pre-created at lock; they lazy-create when downstream LRT theses lock).
//
// Pillars: Q1=1, Q2=0, Q3=2, Q4=1, Q5=0, Q6=1, Q7=2, Q8=0, Q9=2 = 9/18 raw.
// PPG cap (Q2/Q5/Q8 triple-zero, single one-band-below penalty) drops to Trim.
//
// Q5 mechanism tagged `governance_with_fee_switch` per v0.4 §B + v0.6 §H +
// EIGEN MD §Q5 mechanism note: ELIP-12 proposed (20% fee on AVS rewards +
// 100% of EigenCloud infra revenue → EIGEN buybacks); aspirational not active.
//
// Cascade rows added (2 total → 18 in graph):
//   ETH→EIGEN  protocol_host moderate  (sixth protocol_host cascade)
//   BTC→EIGEN  btc_beta_implicit weak  (medium BTC-beta; Infrastructure default)
//
// NO forward oracle_dependency rows created at this lock (lazy creation per
// Infrastructure adapter §3 doctrine). Future LRT/AVS-consuming theses will
// auto-create forward rows on their own lock.
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
	dir := flag.String("dir", "/tmp", "thesis MD dir")
	flag.Parse()

	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=foreign_keys(ON)", *dbPath))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Adapter ID
	var infraAdapterID int64
	if err := db.QueryRow(`SELECT id FROM crypto_adapters WHERE slug = 'infra'`).Scan(&infraAdapterID); err != nil {
		log.Fatalf("adapter infra: %v", err)
	}

	// Existing thesis IDs (parents for cascade)
	thesisID := map[string]int64{}
	for _, sym := range []string{"BTC", "ETH"} {
		var id int64
		if err := db.QueryRow(`SELECT id FROM crypto_theses WHERE coin_symbol=? AND version='v1'`, sym).Scan(&id); err != nil {
			log.Fatalf("prerequisite thesis %s missing: %v", sym, err)
		}
		thesisID[sym] = id
	}

	now := time.Now().Unix()
	mkUnix := func(d string) sql.NullInt64 {
		if d == "" {
			return sql.NullInt64{}
		}
		t, _ := time.Parse("2006-01-02", d)
		return sql.NullInt64{Int64: t.UTC().Unix(), Valid: true}
	}

	// EIGEN seed
	pillars := map[string]int{"Q1": 1, "Q2": 0, "Q3": 2, "Q4": 1, "Q5": 0, "Q6": 1, "Q7": 2, "Q8": 0, "Q9": 2}
	total := 9
	band := "trim" // 9 raw = Hold (7-9); PPG cap one band below → Trim
	passGateFailed := true
	q5Mechanism := "governance_with_fee_switch" // ELIP-12 PROPOSED; aspirational, scored = 0
	q5AnnualUSD := 0.0                          // Current value flow to EIGEN holders = $0 (CoinGecko verified)
	q5FDVUSD := 405_000_000.0                   // 1.82B × $0.22
	q5Pct := 0.0                                // 0% currently; potential 5-12% post-ELIP-12 activation
	tags := []string{
		"restaking",
		"second-infra-thesis",
		"first-non-oracle-infra-subtype",
		"first-multi-zero-pillar-thesis",
		"first-aspirational-mechanism",
		"lazy-forward-cascade-architecture",
	}
	liquidityVenues := `["binance","gate","bybit"]`
	q9TeamNote := "Sreeram Kannan (ex-University of Washington professor) doxxed, Eigen Labs CEO. Multi-year academic + protocol delivery: EigenLayer mainnet 2023, EIGEN token Sept 2024, EigenDA + EigenCompute mainnet alpha Jan 2026, EigenAI mainnet late 2025, EigenCloud rebrand May 2026. a16z led $100M Series B (2024) + $70M direct token purchase from Foundation (2026). Polychain + Coinbase Ventures + Blockchain Capital backers. Cayman-based Eigen Foundation. ELIP governance process operational with community voting + timelock. 3-year investor lockup vesting through 2027 = active supply pressure but commitment signal."
	catalystNote := "EigenCloud rebrand (May 2026 — restaking → verifiable cloud platform) + ELIP-12 tokenomics overhaul (governance proposal: 20% fee on AVS rewards + 100% EigenCloud infra revenue → EIGEN buybacks; binary inflection point for Q5 0 → ~2) + Nebius acquires Eigen AI for $643M (May 1 2026) + a16z $70M direct token purchase + EigenCompute mainnet alpha (Jan 2026). ELIP-12 activation = primary catalyst trigger for Trim → Hold → Accumulate re-rate path."

	raw, err := os.ReadFile(filepath.Join(*dir, "EIGEN_v1_locked.md"))
	if err != nil {
		log.Fatalf("read EIGEN_v1_locked.md: %v", err)
	}
	md := string(raw)
	html := cryptotheses.Render(md)
	pillarJSON, _ := json.Marshal(pillars)
	tagsJSON, _ := json.Marshal(tags)
	passGateInt := 0
	if passGateFailed {
		passGateInt = 1
	}

	res, err := db.Exec(`
		INSERT INTO crypto_theses (
			coin_symbol, coin_name, coingecko_id,
			primary_adapter_id, scorecard_type,
			pillar_scores_json, total_score, max_score, band, pillar_pass_gate_failed,
			q5_mechanism, q5_annual_usd, q5_fdv_usd, q5_accrual_pct,
			q9_team_note,
			catalyst_date, catalyst_note,
			holding_horizon, btc_beta, secondary_tags_json,
			liquidity_passed, liquidity_venues_json, liquidity_checked_at,
			status, version, markdown_current, rendered_html,
			locked_at, last_reviewed_at, next_review_at,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, 18, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, ?, 1, ?, ?, 'locked', 'v1', ?, ?, ?, ?, ?, ?, ?)
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
		  catalyst_note = excluded.catalyst_note,
		  holding_horizon = excluded.holding_horizon,
		  btc_beta = excluded.btc_beta,
		  secondary_tags_json = excluded.secondary_tags_json,
		  liquidity_venues_json = excluded.liquidity_venues_json,
		  markdown_current = excluded.markdown_current,
		  rendered_html = excluded.rendered_html,
		  status = 'locked',
		  updated_at = excluded.updated_at`,
		"EIGEN", "EigenLayer / EigenCloud", sql.NullString{String: "eigenlayer", Valid: true},
		infraAdapterID, "alt_18",
		string(pillarJSON), total, band, passGateInt,
		q5Mechanism, q5AnnualUSD, q5FDVUSD, q5Pct,
		q9TeamNote,
		sql.NullString{String: catalystNote, Valid: true},
		"medium", "medium", string(tagsJSON),
		liquidityVenues, now,
		md, html,
		now, now, mkUnix("2026-06-14"),
		now, now)
	if err != nil {
		log.Fatalf("insert EIGEN: %v", err)
	}
	id, _ := res.LastInsertId()
	if id == 0 {
		_ = db.QueryRow(`SELECT id FROM crypto_theses WHERE coin_symbol='EIGEN' AND version='v1'`).Scan(&id)
	}
	thesisID["EIGEN"] = id
	fmt.Printf("  [EIGEN] id=%d %d/18 %s, horizon=medium, beta=medium, q5=%s (%.2f%%)\n",
		id, total, band, q5Mechanism, q5Pct)

	// 2 cascade rows
	cascade := []struct {
		parent, child   string
		dtype, strength string
		note            string
	}{
		{"ETH", "EIGEN", "protocol_host", "moderate", "Auto-created on Infrastructure-restaking thesis lock per Infrastructure adapter §3. Sixth protocol_host cascade in framework. EIGEN restaking layer 100% ETH/LST-denominated."},
		{"BTC", "EIGEN", "btc_beta_implicit", "weak", "Auto-created from medium BTC-beta tag (Infrastructure default per adapter §8). Notification-only cascade."},
	}
	for _, c := range cascade {
		_, err := db.Exec(`
			INSERT INTO crypto_thesis_dependencies (parent_thesis_id, child_thesis_id, dependency_type, cascade_strength, note, created_by)
			VALUES (?, ?, ?, ?, ?, 'system')
			ON CONFLICT(parent_thesis_id, child_thesis_id, dependency_type) DO NOTHING`,
			thesisID[c.parent], thesisID[c.child], c.dtype, c.strength, c.note)
		if err != nil {
			log.Fatalf("cascade %s→%s [%s]: %v", c.parent, c.child, c.dtype, err)
		}
		fmt.Printf("  cascade: %-5s → %-5s [%s, %s]\n", c.parent, c.child, c.dtype, c.strength)
	}

	// Summary
	fmt.Println("\n--- crypto_theses (12 expected) ---")
	rows, _ := db.Query(`
		SELECT t.coin_symbol, a.slug, t.total_score, t.max_score, t.band,
		       t.holding_horizon, t.btc_beta, t.pillar_pass_gate_failed,
		       COALESCE(t.active_veto,'-'),
		       COALESCE(printf('%.2f%%', t.q5_accrual_pct), '-'),
		       COALESCE(t.q5_mechanism, '-')
		  FROM crypto_theses t
		  JOIN crypto_adapters a ON a.id = t.primary_adapter_id
		 ORDER BY t.total_score DESC, t.coin_symbol`)
	defer rows.Close()
	n := 0
	for rows.Next() {
		var sym, slug, band, horizon, beta, veto, q5pct, q5mech string
		var total, max, pgf int
		_ = rows.Scan(&sym, &slug, &total, &max, &band, &horizon, &beta, &pgf, &veto, &q5pct, &q5mech)
		fmt.Printf("  %-5s adapter=%-12s %d/%d %-11s horizon=%-10s beta=%-9s ppg=%d veto=%-15s q5=%-28s (%s)\n",
			sym, slug, total, max, band, horizon, beta, pgf, veto, q5mech, q5pct)
		n++
	}
	fmt.Printf("\nTotal: %d theses\n", n)

	fmt.Println("\n--- crypto_thesis_dependencies (18 expected) ---")
	drows, _ := db.Query(`
		SELECT p.coin_symbol, c.coin_symbol, d.dependency_type, d.cascade_strength
		  FROM crypto_thesis_dependencies d
		  JOIN crypto_theses p ON p.id = d.parent_thesis_id
		  JOIN crypto_theses c ON c.id = d.child_thesis_id
		 ORDER BY d.id`)
	defer drows.Close()
	n = 0
	for drows.Next() {
		var p, c, dtype, strength string
		_ = drows.Scan(&p, &c, &dtype, &strength)
		fmt.Printf("  %-5s → %-5s [%s, %s]\n", p, c, dtype, strength)
		n++
	}
	fmt.Printf("\nTotal: %d cascade rows\n", n)
}
