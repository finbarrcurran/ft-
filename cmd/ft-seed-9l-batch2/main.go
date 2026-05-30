// Seeder for LINK, SOL, POL, AVAX v1 locked theses + cascade rows.
//
// Per Claude_Code_Ingestion_4_Theses.md handoff (2026-05-30).
//
// Resolves Q5 enum gaps per Phase 1 Ask B rule:
//   - LINK uses required_for_service → stored as `other` with canonical
//     label in q5 mechanism note
//   - SOL/POL/AVAX use staking_yield + fee_burn combo → primary `staking_yield`,
//     fee burn captured in note
//
// LINK pillar scores tuned to sum to header total (14) using Infrastructure
// adapter's stated "round down" rule for Q6 and Q9 (resolving the prose-
// vs-header math inconsistency by deferring to locked header authority,
// same pattern used for ETH v1 Q2).
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
	dir := flag.String("dir", "/tmp", "dir holding the locked thesis MD files")
	flag.Parse()

	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=foreign_keys(ON)", *dbPath))
	if err != nil {
		log.Fatalf("open: %v", err)
	}
	defer db.Close()

	// Lookup adapter + thesis IDs needed for primary/secondary/cascade.
	adapterIDs := map[string]int64{}
	for _, slug := range []string{"infra", "l1", "l2"} {
		var id int64
		if err := db.QueryRow(`SELECT id FROM crypto_adapters WHERE slug = ?`, slug).Scan(&id); err != nil {
			log.Fatalf("adapter %q not found: %v", slug, err)
		}
		adapterIDs[slug] = id
	}
	var ethID, btcID int64
	if err := db.QueryRow(`SELECT id FROM crypto_theses WHERE coin_symbol='ETH' AND version='v1'`).Scan(&ethID); err != nil {
		log.Fatalf("ETH v1 prerequisite missing (LINK needs it as parent): %v", err)
	}
	if err := db.QueryRow(`SELECT id FROM crypto_theses WHERE coin_symbol='BTC' AND version='v1'`).Scan(&btcID); err != nil {
		log.Fatalf("BTC v1 prerequisite missing (btc_beta_implicit cascades need it): %v", err)
	}

	now := time.Now().Unix()
	mkNext := func(d string) int64 {
		t, _ := time.Parse("2006-01-02", d)
		return t.UTC().Unix()
	}

	// Catalyst date: Alpenglow Q3 2026 target.
	solCatalyst := "2026-08-30"

	// All 4 theses defined inline.
	type seed struct {
		Symbol             string
		Name               string
		CoinGecko          string
		PrimaryAdapter     string
		SecondaryAdapter   string // empty unless hybrid (POL only)
		ScorecardType      string
		Pillars            map[string]int
		Total              int
		MaxScore           int
		Band               string
		PassGateFailed     bool
		Q5Mechanism        string
		Q5AnnualUSD        float64
		Q5FDVUSD           float64
		Q9TeamNote         string
		CatalystDate       string
		CatalystNote       string
		HoldingHorizon     string
		BTCBeta            string
		Tags               []string
		MDFile             string
		NextReviewDate     string
	}
	seeds := []seed{
		{
			Symbol:         "LINK",
			Name:           "Chainlink",
			CoinGecko:      "chainlink",
			PrimaryAdapter: "infra",
			ScorecardType:  "alt_18",
			// Pillars tuned to header total 14. Prose-vs-header inconsistency
			// (prose math gives 16 with v0.4 strict; Infrastructure adapter MD
			// says "rounded down" yielding 14) resolved by trusting locked total.
			// Q6 = floor(1.83)=1 and Q9 = floor(1.83)=1 under adapter's
			// stated rule. Flagged in ingestion report §A.
			Pillars:        map[string]int{"Q1": 2, "Q2": 1, "Q3": 2, "Q4": 2, "Q5": 2, "Q6": 1, "Q7": 2, "Q8": 1, "Q9": 1},
			Total:          14,
			MaxScore:       18,
			Band:           "strong",
			PassGateFailed: false,
			Q5Mechanism:    "other", // canonical "required_for_service" not in enum
			Q5AnnualUSD:    95_000_000,
			Q5FDVUSD:       8_390_000_000,
			Q9TeamNote:     "Sergey Nazarov doxxed, multi-decade track record. Chainlink Labs consistent multi-year delivery (Staking v0.1 2022, v0.2 2023, CCIP mainnet GA 2024, Economics 2.0 2025-26). Foundation strong with transparent treasury; team allocations fully vested 9+ years post-ICO. Decentralized Council in progressive transition. Still founder-dependent for major strategic decisions (Q9 sub-criterion 6 = 1).",
			CatalystDate:   "", // No discrete date; ongoing CCIP institutional deployment + Economics 2.0 transition
			CatalystNote:   "CCIP institutional deployment expansion (SWIFT, central bank pilots) + Chainlink Economics 2.0 full transition to fee-funded staking — both ongoing within 90d window",
			HoldingHorizon: "multi_year",
			BTCBeta:        "medium",
			Tags:           []string{"oracle", "first-protocol-host-cascade", "infrastructure-ceiling-reference"},
			MDFile:         "LINK_v1_locked.md",
			NextReviewDate: "2026-06-07",
		},
		{
			Symbol:         "SOL",
			Name:           "Solana",
			CoinGecko:      "solana",
			PrimaryAdapter: "l1",
			ScorecardType:  "alt_18",
			Pillars:        map[string]int{"Q1": 2, "Q2": 1, "Q3": 2, "Q4": 2, "Q5": 2, "Q6": 1, "Q7": 2, "Q8": 0, "Q9": 1},
			Total:          13, // raw; band capped to accumulate via PPG
			MaxScore:       18,
			Band:           "accumulate", // capped from strong (raw 13 in 13-18) one band down via Q8=0
			PassGateFailed: true,
			Q5Mechanism:    "staking_yield",
			Q5AnnualUSD:    2_500_000_000,
			Q5FDVUSD:       54_000_000_000,
			Q9TeamNote:     "Anatoly Yakovenko ex-Qualcomm engineer + Raj Gokal doxxed, consistent multi-year delivery (Mainnet 2020, Firedancer 2024, Alpenglow test cluster May 2026). Solana Foundation healthy, transparent treasury. ~15-20% remaining team/early-backers vesting. FTX historical connection creates persistent reputational overhang (Q9 sub-criterion 5 = 1).",
			CatalystDate:   solCatalyst,
			CatalystNote:   "Alpenglow consensus upgrade (TowerBFT → Votor+Rotor; finality 12.8s → ~150ms) on test cluster May 11 2026, mainnet target Q3 2026. Q7 auto-decay risk if slips past Aug 2026.",
			HoldingHorizon: "multi_year",
			BTCBeta:        "high",
			Tags:           []string{"hp-l1", "post-drift-exploit-q6-cap", "alpenglow-watch"},
			MDFile:         "SOL_v1_locked.md",
			NextReviewDate: "2026-06-07",
		},
		{
			Symbol:           "POL",
			Name:             "Polygon",
			CoinGecko:        "polygon-ecosystem-token",
			PrimaryAdapter:   "l1",
			SecondaryAdapter: "l2", // first hybrid coin in framework
			ScorecardType:    "alt_18",
			Pillars:          map[string]int{"Q1": 1, "Q2": 1, "Q3": 1, "Q4": 1, "Q5": 1, "Q6": 1, "Q7": 1, "Q8": 0, "Q9": 1},
			Total:            8, // raw; band capped to trim via PPG
			MaxScore:         18,
			Band:             "trim", // capped from hold (raw 8 in 7-9) one band down via Q8=0
			PassGateFailed:   true,
			Q5Mechanism:      "staking_yield",
			Q5AnnualUSD:      95_000_000,
			Q5FDVUSD:         1_000_000_000,
			Q9TeamNote:       "Sandeep Nailwal, Jaynti Kanani, Anurag Arjun doxxed. Nailwal now Foundation CEO (March 2026). Mixed delivery: Polygon PoS shipped reliably; zkEVM under-performed promises (now sunsetting). Foundation strong post-Nailwal CEO transition. Nailwal holds material POL (largest individual holder). Vesting largely complete but transparent.",
			CatalystDate:     "", // AggLayer ongoing, no single date
			CatalystNote:     "AggLayer maturation milestones (Miden, CDK privacy, yield on bridged) active within window. Gigagas progression (3,800 → 100K TPS). Visa/Modern Treasury/Mastercard live. Counter-weight: zkEVM sunset overhang.",
			HoldingHorizon:   "medium",
			BTCBeta:          "high",
			Tags:             []string{"est-alt-l1", "first-hybrid-coin", "secondary-l2-advisory-4-18-delta-flag"},
			MDFile:           "POL_v1_locked.md",
			NextReviewDate:   "2026-06-14",
		},
		{
			Symbol:         "AVAX",
			Name:           "Avalanche",
			CoinGecko:      "avalanche-2",
			PrimaryAdapter: "l1",
			ScorecardType:  "alt_18",
			Pillars:        map[string]int{"Q1": 1, "Q2": 1, "Q3": 1, "Q4": 2, "Q5": 2, "Q6": 1, "Q7": 1, "Q8": 0, "Q9": 2},
			Total:          11, // raw; band capped to hold via PPG
			MaxScore:       18,
			Band:           "hold", // capped from accumulate (raw 11 in 10-12) one band down via Q8=0
			PassGateFailed: true,
			Q5Mechanism:    "staking_yield",
			Q5AnnualUSD:    200_000_000,
			Q5FDVUSD:       6_500_000_000,
			Q9TeamNote:     "Emin Gün Sirer doxxed, Cornell CS professor, multi-decade academic + industry record (Snowflake/Avalanche consensus designer, Snowman, Bloxroute). Consistent multi-year shipping (Mainnet 2020, Avalanche9000+Etna 2024, Granite+Octane 2025). Foundation strong with $250M ecosystem fund. $250M Nov 2025 Foundation token sale added recent VC overhang (Galaxy/Dragonfly/ParaFi); vesting disclosed.",
			CatalystDate:   "", // multiple ongoing, no single discrete date
			CatalystNote:   "Avalanche9000 ecosystem expansion (500+ L1s active, growing), VAVX ETF AUM growth, RWA tokenization (Securitize, JPMorgan pilots), AggLayer-equivalent network expansion. All incremental rather than transformational within 90d window.",
			HoldingHorizon: "medium",
			BTCBeta:        "high",
			Tags:           []string{"est-alt-l1", "anti-narrative-state-test-case", "avalanche9000-renewal"},
			MDFile:         "AVAX_v1_locked.md",
			NextReviewDate: "2026-06-14",
		},
	}

	// Track inserted IDs for cascade row creation.
	thesisIDs := map[string]int64{
		"ETH": ethID, "BTC": btcID,
	}

	for _, t := range seeds {
		// Read MD body.
		mdRaw, err := os.ReadFile(filepath.Join(*dir, t.MDFile))
		if err != nil {
			log.Fatalf("read %s: %v", t.MDFile, err)
		}
		md := string(mdRaw)
		html := cryptotheses.Render(md)

		pillarJSON, _ := json.Marshal(t.Pillars)
		tagsJSON, _ := json.Marshal(t.Tags)

		q5Pct := (t.Q5AnnualUSD / t.Q5FDVUSD) * 100

		// secondary_adapter_id (nullable; only set for hybrid coins).
		var secAdapterID sql.NullInt64
		if t.SecondaryAdapter != "" {
			secAdapterID = sql.NullInt64{Int64: adapterIDs[t.SecondaryAdapter], Valid: true}
		}

		var catalystDateNS, catalystNoteNS sql.NullString
		if t.CatalystDate != "" {
			catalystDateNS = sql.NullString{String: t.CatalystDate, Valid: true}
		}
		if t.CatalystNote != "" {
			catalystNoteNS = sql.NullString{String: t.CatalystNote, Valid: true}
		}

		var nextReviewNS sql.NullInt64
		if t.NextReviewDate != "" {
			nextReviewNS = sql.NullInt64{Int64: mkNext(t.NextReviewDate), Valid: true}
		}

		passGateInt := 0
		if t.PassGateFailed {
			passGateInt = 1
		}

		res, err := db.Exec(`
			INSERT INTO crypto_theses (
				coin_symbol, coin_name, coingecko_id,
				primary_adapter_id, secondary_adapter_id, scorecard_type,
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
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, ?, ?, ?, ?, ?, 1, ?, ?, 'locked', 'v1', ?, ?, ?, ?, ?, ?, ?)
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
			  secondary_adapter_id = excluded.secondary_adapter_id,
			  markdown_current = excluded.markdown_current,
			  rendered_html = excluded.rendered_html,
			  updated_at = excluded.updated_at`,
			t.Symbol, t.Name, t.CoinGecko,
			adapterIDs[t.PrimaryAdapter], secAdapterID, t.ScorecardType,
			string(pillarJSON), t.Total, t.MaxScore, t.Band, passGateInt,
			t.Q5Mechanism, t.Q5AnnualUSD, t.Q5FDVUSD, q5Pct,
			t.Q9TeamNote,
			catalystDateNS, catalystNoteNS,
			t.HoldingHorizon, t.BTCBeta, string(tagsJSON),
			`["kraken","coinbase","binance"]`, now,
			md, html,
			now, now, nextReviewNS,
			now, now)
		if err != nil {
			log.Fatalf("insert %s: %v", t.Symbol, err)
		}
		id, _ := res.LastInsertId()
		if id == 0 {
			_ = db.QueryRow(`SELECT id FROM crypto_theses WHERE coin_symbol=? AND version='v1'`, t.Symbol).Scan(&id)
		}
		thesisIDs[t.Symbol] = id
		secLabel := "—"
		if t.SecondaryAdapter != "" {
			secLabel = t.SecondaryAdapter + " advisory"
		}
		fmt.Printf("  [%-4s] id=%d %d/%d %s, %s, sec=%s, q5=%s ($%.0fM/$%.1fB=%.2f%%)\n",
			t.Symbol, id, t.Total, t.MaxScore, t.Band, t.HoldingHorizon, secLabel,
			t.Q5Mechanism, t.Q5AnnualUSD/1e6, t.Q5FDVUSD/1e9, q5Pct)
	}

	// Cascade row insertion.
	cascade := []struct {
		parent, child  string
		dtype, strength string
		note            string
	}{
		{"ETH", "LINK", "protocol_host", "moderate", "Auto-created on Infrastructure thesis lock per Infrastructure adapter §3. First protocol_host cascade for Infrastructure adapter."},
		{"BTC", "LINK", "btc_beta_implicit", "weak", "Auto-created from medium BTC-beta tag on thesis lock"},
		{"BTC", "SOL", "btc_beta_implicit", "weak", "Auto-created from high BTC-beta tag on thesis lock"},
		{"BTC", "POL", "btc_beta_implicit", "weak", "Auto-created from high BTC-beta tag on thesis lock"},
		{"BTC", "AVAX", "btc_beta_implicit", "weak", "Auto-created from high BTC-beta tag on thesis lock"},
	}
	for _, c := range cascade {
		_, err := db.Exec(`
			INSERT INTO crypto_thesis_dependencies (parent_thesis_id, child_thesis_id, dependency_type, cascade_strength, note, created_by)
			VALUES (?, ?, ?, ?, ?, 'system')
			ON CONFLICT(parent_thesis_id, child_thesis_id, dependency_type) DO NOTHING`,
			thesisIDs[c.parent], thesisIDs[c.child], c.dtype, c.strength, c.note)
		if err != nil {
			log.Fatalf("insert cascade %s→%s [%s]: %v", c.parent, c.child, c.dtype, err)
		}
		fmt.Printf("  cascade: %-4s → %-4s [%s, %s]\n", c.parent, c.child, c.dtype, c.strength)
	}

	// Final state summary.
	fmt.Println("\n--- crypto_theses (8 expected) ---")
	rows, _ := db.Query(`
		SELECT t.coin_symbol, a.slug, COALESCE(sa.slug,'-'), t.total_score, t.max_score, t.band,
		       t.holding_horizon, t.btc_beta, t.pillar_pass_gate_failed,
		       COALESCE(t.active_veto,'-'),
		       COALESCE(printf('%.2f%%', t.q5_accrual_pct), '-'),
		       t.q5_mechanism
		  FROM crypto_theses t
		  JOIN crypto_adapters a ON a.id = t.primary_adapter_id
		  LEFT JOIN crypto_adapters sa ON sa.id = t.secondary_adapter_id
		 ORDER BY t.id`)
	defer rows.Close()
	for rows.Next() {
		var sym, slug, secSlug, band, horizon, beta, veto, q5pct, q5mech string
		var total, max, pgf int
		_ = rows.Scan(&sym, &slug, &secSlug, &total, &max, &band, &horizon, &beta, &pgf, &veto, &q5pct, &q5mech)
		fmt.Printf("  %-4s primary=%-12s sec=%-5s %d/%d %s horizon=%-10s beta=%-6s ppg=%d veto=%-15s q5=%s (%s)\n",
			sym, slug, secSlug, total, max, band, horizon, beta, pgf, veto, q5pct, q5mech)
	}

	fmt.Println("\n--- crypto_thesis_dependencies (9 expected) ---")
	drows, _ := db.Query(`
		SELECT p.coin_symbol, c.coin_symbol, d.dependency_type, d.cascade_strength
		  FROM crypto_thesis_dependencies d
		  JOIN crypto_theses p ON p.id = d.parent_thesis_id
		  JOIN crypto_theses c ON c.id = d.child_thesis_id
		 ORDER BY d.id`)
	defer drows.Close()
	for drows.Next() {
		var parent, child, dtype, strength string
		_ = drows.Scan(&parent, &child, &dtype, &strength)
		fmt.Printf("  %-4s → %-4s [%s, %s]\n", parent, child, dtype, strength)
	}
}
