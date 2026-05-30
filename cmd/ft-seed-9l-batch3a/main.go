// Seeder for Batch 3a: AAVE, RNDR, BUIDL v1 locked theses + 7 cascade rows.
//
// Per Migration_0033_Doctrine_Handoff.md sequencing: ingest BEFORE 0033.
// Q5 mechanisms set per Phase 1 v0.4 §B fallback rules (re-tagged in 0033).
//
// Cascade rows added (7 total → 16 in graph):
//   ETH→AAVE   protocol_host moderate
//   LINK→AAVE  oracle_dependency moderate ← FIRST FORWARD CASCADE
//   BTC→AAVE   btc_beta_implicit weak
//   SOL→RNDR   protocol_host moderate
//   BTC→RNDR   btc_beta_implicit weak
//   ETH→BUIDL  protocol_host moderate
//   BTC→BUIDL  btc_beta_implicit weak
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

	// Adapter IDs
	adapterID := map[string]int64{}
	for _, slug := range []string{"defi", "depin", "rwa"} {
		var id int64
		if err := db.QueryRow(`SELECT id FROM crypto_adapters WHERE slug = ?`, slug).Scan(&id); err != nil {
			log.Fatalf("adapter %q: %v", slug, err)
		}
		adapterID[slug] = id
	}

	// Existing thesis IDs (parents for cascade)
	thesisID := map[string]int64{}
	for _, sym := range []string{"BTC", "ETH", "SOL", "LINK"} {
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

	type seed struct {
		Symbol           string
		Name             string
		CoinGecko        string
		PrimaryAdapter   string
		ScorecardType    string
		Pillars          map[string]int
		Total            int
		Band             string
		PassGateFailed   bool
		Q5Mechanism      string // Phase 1 fallback; re-tagged in 0033
		Q5AnnualUSD      float64
		Q5FDVUSD         float64
		Q9TeamNote       string
		CatalystDate     string
		CatalystNote     string
		Horizon          string
		BTCBeta          string
		Tags             []string
		LiquidityVenues  string // JSON
		MDFile           string
		NextReview       string
	}
	seeds := []seed{
		{
			Symbol:         "AAVE",
			Name:           "Aave Protocol",
			CoinGecko:      "aave",
			PrimaryAdapter: "defi",
			ScorecardType:  "alt_18",
			Pillars:        map[string]int{"Q1": 1, "Q2": 1, "Q3": 2, "Q4": 1, "Q5": 2, "Q6": 2, "Q7": 2, "Q8": 0, "Q9": 2},
			Total:          13,
			Band:           "accumulate", // PPG cap from Q8=0; raw 13 in Strong → capped to Accumulate
			PassGateFailed: true,
			Q5Mechanism:    "staking_yield", // re-tag → real_yield_staking at 0033
			Q5AnnualUSD:    90_000_000,
			Q5FDVUSD:       1_440_000_000,
			Q9TeamNote:     "Stani Kulechov doxxed, ETHLend 2017 → Aave 2020 transition. Consistent multi-year delivery: V1 (2020), V2 (2020), V3 (2022), GHO (2023), V4 mainnet (March 2026). Aave Labs $31.8M April 2026 grant transparent. Aavenomics 2.0 $50M buyback program active. Multicoin Capital staged exit (286K AAVE = 1.7% supply) creates secondary overhang but team holdings clean.",
			CatalystDate:   "",
			CatalystNote:   "V4 ecosystem expansion (March 30 2026 mainnet) + GHO multichain (Plasma/Linea/Aptos) + $50M Aavenomics 2.0 buyback + sGHO savings product (April 2026) + RWA permissioned pools — multiple catalysts in window.",
			Horizon:        "multi_year",
			BTCBeta:        "high",
			Tags:           []string{"lending", "first-defi-thesis", "first-protocol-host-defi-cascade", "first-forward-oracle-dependency-cascade"},
			LiquidityVenues: `["kraken","coinbase","binance"]`,
			MDFile:         "AAVE_v1_locked.md",
			NextReview:     "2026-06-07",
		},
		{
			Symbol:         "RNDR",
			Name:           "Render Network",
			CoinGecko:      "render-token",
			PrimaryAdapter: "depin",
			ScorecardType:  "alt_18",
			Pillars:        map[string]int{"Q1": 2, "Q2": 1, "Q3": 2, "Q4": 1, "Q5": 1, "Q6": 2, "Q7": 2, "Q8": 1, "Q9": 2},
			Total:          14,
			Band:           "strong", // clean, no PPG cap (Q8=1 not 0)
			PassGateFailed: false,
			Q5Mechanism:    "other", // re-tag → burn_and_mint at 0033
			Q5AnnualUSD:    1_400_000, // 90d annualized direct burns USD-value
			Q5FDVUSD:       1_270_000_000,
			Q9TeamNote:     "Jules Urbach (OTOY CEO since 2017), doxxed, multi-year shipping record (OctaneRender industry-standard 2014-present). Render Network: mainnet 2020, Solana migration 2023 (RNP-002), BME 2023, Dispersed AI subnet Dec 2025. OTOY Treasury holds 23.3% of supply — significant founder concentration but transparent via Foundation monthly reports. Cayman-based Render Network Foundation.",
			CatalystDate:   "",
			CatalystNote:   "Dispersed AI subnet commercial ramp (live since Dec 2025 with NVIDIA H100/H200 + AMD MI300X) + RenderCon 2026 April momentum + RNP-021 tokenomics refinements + Render Foundation 2025 Annual Report — multiple catalysts in window. Key Q4+Q5 inflection variable: AI inference enterprise adoption.",
			Horizon:        "medium",
			BTCBeta:        "high",
			Tags:           []string{"compute", "first-depin-thesis", "first-ryr-quantification", "ryr-concern-classification", "first-depin-protocol-host-cascade"},
			LiquidityVenues: `["kraken","coinbase","binance","mexc","gate"]`,
			MDFile:         "RNDR_v1_locked.md",
			NextReview:     "2026-06-14",
		},
		{
			Symbol:         "BUIDL",
			Name:           "BlackRock USD Institutional Digital Liquidity Fund",
			CoinGecko:      "", // BUIDL is qualified-purchaser only; no CoinGecko listing
			PrimaryAdapter: "rwa",
			ScorecardType:  "alt_18",
			Pillars:        map[string]int{"Q1": 2, "Q2": 2, "Q3": 2, "Q4": 2, "Q5": 2, "Q6": 2, "Q7": 2, "Q8": 1, "Q9": 2},
			Total:          17,
			Band:           "strong", // clean, no PPG cap; upper-edge first thesis above LINK
			PassGateFailed: false,
			Q5Mechanism:    "other", // re-tag → direct_asset_claim at 0033
			Q5AnnualUSD:    90_000_000, // ~$85-100M T-Bill yield rebase
			Q5FDVUSD:       2_450_000_000,
			Q9TeamNote:     "BlackRock = highest possible issuer credibility ($10T+ AUM globally). Larry Fink CEO since 1988; Robert Mitchnick Head of Digital Assets. BNY Mellon Investment Servicing as transfer agent + custodian (third-oldest US bank, $50T+ AUM, G-SIB classification). Securitize as tokenization platform (founded 2017). Three-party institutional governance: BlackRock + Securitize + BNY Mellon. Prior: iShares Bitcoin Trust IBIT ($60B+ AUM). BUIDL operational since March 2024 launch — clean record. No team token overhang (it's a money-market fund tokenized).",
			CatalystDate:   "",
			CatalystNote:   "BlackRock May 2026 SEC filings to expand Select Treasury-Based Liquidity Fund to $7B on-chain (potential 3-4x AUM expansion if approved) + GENIUS Act regulatory clarity + multi-chain expansion (Aptos + Solana additions) + Ondo OUSG + Sky + Frax + Ethena integration expansion + BlackRock 2025 Investment Outlook tokenization thesis ($14T TAM).",
			Horizon:        "never_sell",
			BTCBeta:        "low",
			Tags:           []string{"treasuries", "first-rwa-thesis", "first-rabr-quantification", "first-custody-tier-classification", "first-rwa-protocol-host-cascade", "qualified-purchaser-only-liquidity-exception", "highest-score-in-framework-17-18"},
			// Liquidity pre-filter EXCEPTION (qualified-purchaser RWA):
			// per RWA adapter §3 + BUIDL thesis VETO check note — Tier 1 custody + Circle USDC redemption facility
			// substitutes for exchange-listing pre-filter. Liquidity_passed=1 with alternative venues.
			LiquidityVenues: `["securitize_platform","circle_usdc_redemption","bny_mellon_admin"]`,
			MDFile:          "BUIDL_v1_locked.md",
			NextReview:      "2026-08-30",
		},
	}

	newIDs := map[string]int64{}
	for _, t := range seeds {
		raw, err := os.ReadFile(filepath.Join(*dir, t.MDFile))
		if err != nil {
			log.Fatalf("read %s: %v", t.MDFile, err)
		}
		md := string(raw)
		html := cryptotheses.Render(md)

		pillarJSON, _ := json.Marshal(t.Pillars)
		tagsJSON, _ := json.Marshal(t.Tags)

		q5Pct := (t.Q5AnnualUSD / t.Q5FDVUSD) * 100
		passGateInt := 0
		if t.PassGateFailed {
			passGateInt = 1
		}

		var cgID sql.NullString
		if t.CoinGecko != "" {
			cgID = sql.NullString{String: t.CoinGecko, Valid: true}
		}
		var catalystDateNS, catalystNoteNS sql.NullString
		if t.CatalystDate != "" {
			catalystDateNS = sql.NullString{String: t.CatalystDate, Valid: true}
		}
		if t.CatalystNote != "" {
			catalystNoteNS = sql.NullString{String: t.CatalystNote, Valid: true}
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
			) VALUES (?, ?, ?, ?, ?, ?, ?, 18, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, 'locked', 'v1', ?, ?, ?, ?, ?, ?, ?)
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
			t.Symbol, t.Name, cgID,
			adapterID[t.PrimaryAdapter], t.ScorecardType,
			string(pillarJSON), t.Total, t.Band, passGateInt,
			t.Q5Mechanism, t.Q5AnnualUSD, t.Q5FDVUSD, q5Pct,
			t.Q9TeamNote,
			catalystDateNS, catalystNoteNS,
			t.Horizon, t.BTCBeta, string(tagsJSON),
			t.LiquidityVenues, now,
			md, html,
			now, now, mkUnix(t.NextReview),
			now, now)
		if err != nil {
			log.Fatalf("insert %s: %v", t.Symbol, err)
		}
		id, _ := res.LastInsertId()
		if id == 0 {
			_ = db.QueryRow(`SELECT id FROM crypto_theses WHERE coin_symbol=? AND version='v1'`, t.Symbol).Scan(&id)
		}
		newIDs[t.Symbol] = id
		fmt.Printf("  [%-5s] id=%d %d/18 %s, horizon=%s, beta=%s, q5=%s (%.2f%%)\n",
			t.Symbol, id, t.Total, t.Band, t.Horizon, t.BTCBeta, t.Q5Mechanism, q5Pct)
	}

	// 7 cascade rows
	cascade := []struct {
		parent, child   string
		dtype, strength string
		note            string
	}{
		{"ETH", "AAVE", "protocol_host", "moderate", "Auto-created on DeFi thesis lock per DeFi adapter §3. First protocol_host cascade for DeFi adapter."},
		{"LINK", "AAVE", "oracle_dependency", "moderate", "Auto-created on AAVE lock declaring LINK in Q6 sub-criterion 4. **First forward cascade in framework** per Infrastructure adapter §3 bidirectional design."},
		{"BTC", "AAVE", "btc_beta_implicit", "weak", "Auto-created from high BTC-beta tag"},
		{"SOL", "RNDR", "protocol_host", "moderate", "Auto-created on DePIN thesis lock per DePIN adapter §3. First protocol_host cascade for DePIN adapter."},
		{"BTC", "RNDR", "btc_beta_implicit", "weak", "Auto-created from high BTC-beta tag"},
		{"ETH", "BUIDL", "protocol_host", "moderate", "Auto-created on RWA thesis lock per RWA adapter §3. First protocol_host cascade for RWA adapter."},
		{"BTC", "BUIDL", "btc_beta_implicit", "weak", "Auto-created from low BTC-beta tag (near-zero correlation by design; notification only)"},
	}
	allIDs := map[string]int64{}
	for k, v := range thesisID {
		allIDs[k] = v
	}
	for k, v := range newIDs {
		allIDs[k] = v
	}
	for _, c := range cascade {
		_, err := db.Exec(`
			INSERT INTO crypto_thesis_dependencies (parent_thesis_id, child_thesis_id, dependency_type, cascade_strength, note, created_by)
			VALUES (?, ?, ?, ?, ?, 'system')
			ON CONFLICT(parent_thesis_id, child_thesis_id, dependency_type) DO NOTHING`,
			allIDs[c.parent], allIDs[c.child], c.dtype, c.strength, c.note)
		if err != nil {
			log.Fatalf("cascade %s→%s [%s]: %v", c.parent, c.child, c.dtype, err)
		}
		fmt.Printf("  cascade: %-5s → %-5s [%s, %s]\n", c.parent, c.child, c.dtype, c.strength)
	}

	// Summary
	fmt.Println("\n--- crypto_theses (11 expected) ---")
	rows, _ := db.Query(`
		SELECT t.coin_symbol, a.slug, t.total_score, t.max_score, t.band,
		       t.holding_horizon, t.btc_beta, t.pillar_pass_gate_failed,
		       COALESCE(t.active_veto,'-'),
		       COALESCE(printf('%.2f%%', t.q5_accrual_pct), '-'),
		       t.q5_mechanism
		  FROM crypto_theses t
		  JOIN crypto_adapters a ON a.id = t.primary_adapter_id
		 ORDER BY t.id`)
	defer rows.Close()
	n := 0
	for rows.Next() {
		var sym, slug, band, horizon, beta, veto, q5pct, q5mech string
		var total, max, pgf int
		_ = rows.Scan(&sym, &slug, &total, &max, &band, &horizon, &beta, &pgf, &veto, &q5pct, &q5mech)
		fmt.Printf("  %-5s adapter=%-12s %d/%d %-11s horizon=%-10s beta=%-9s ppg=%d veto=%-15s q5=%-7s (%s)\n",
			sym, slug, total, max, band, horizon, beta, pgf, veto, q5pct, q5mech)
		n++
	}
	fmt.Printf("\nTotal: %d theses\n", n)

	fmt.Println("\n--- crypto_thesis_dependencies (16 expected) ---")
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
