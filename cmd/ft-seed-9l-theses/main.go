// One-shot seeder for Spec 9l calibration triad theses.
//
// Inserts BTC v1 (/12, 6/12 Hold), ETH v1 (/18, 13/18 Strong, lower edge,
// ceiling anchor), LUNC v1 (/18, 3/18 Exit + multi-VETO, floor anchor).
//
// Idempotent — uses INSERT ... ON CONFLICT DO UPDATE keyed on
// (coin_symbol, version) UNIQUE constraint. Safe to re-run as the
// canonical fixtures evolve.
//
// Usage:
//
//	go run ./cmd/ft-seed-9l-theses -db /var/lib/ft/ft.db -dir /tmp
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

type thesisSeed struct {
	CoinSymbol      string
	CoinName        string
	CoinGeckoID     string
	AdapterSlug     string
	ScorecardType   string
	PillarScores    map[string]int
	TotalScore      int
	MaxScore        int
	Band            string
	PassGateFailed  bool
	Q5Mechanism     *string
	Q5AnnualUSD     *float64
	Q5FDVUSD        *float64
	Q9TeamNote      *string
	ActiveVeto      *string
	ActiveVetoText  *string
	CatalystDate    *string
	CatalystNote    *string
	HoldingHorizon  string
	BTCBeta         string
	SecondaryTags   []string
	MDFile          string
	NextReviewDate  string // YYYY-MM-DD
}

func strp(s string) *string  { return &s }
func f64p(f float64) *float64 { return &f }

func main() {
	dbPath := flag.String("db", "/var/lib/ft/ft.db", "ft.db path")
	dir := flag.String("dir", "/tmp", "dir holding the locked thesis MD files")
	flag.Parse()

	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)", *dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		log.Fatalf("open: %v", err)
	}
	defer db.Close()

	seeds := []thesisSeed{
		{
			CoinSymbol:    "BTC",
			CoinName:      "Bitcoin",
			CoinGeckoID:   "bitcoin",
			AdapterSlug:   "btc",
			ScorecardType: "monetary_12",
			PillarScores:  map[string]int{"M1": 1, "M2": 1, "M3": 1, "M4": 1, "M5": 1, "M6": 1},
			TotalScore:    6,
			MaxScore:      12,
			Band:          "hold",
			PassGateFailed: false,
			// BTC has no Q5 (uses /12 monetary scorecard); no Q9 either.
			HoldingHorizon: "never_sell",
			BTCBeta:        "low", // BTC vs itself; not in framework as "N/A" so closest semantic is low
			SecondaryTags:  []string{"monetary-asset", "spot-self"},
			MDFile:         "BTC_v1_locked.md",
			NextReviewDate: "2026-06-07",
		},
		{
			CoinSymbol:    "ETH",
			CoinName:      "Ethereum",
			CoinGeckoID:   "ethereum",
			AdapterSlug:   "l1",
			ScorecardType: "alt_18",
			// Uses the authoritative locked total from thesis header (13/18).
			// Pillar Q2 is contested in the thesis prose (header says 2; rigorous
			// re-scoring drops to 1). Using 2 to match the locked total.
			// Flagged in Spec 9l v0.3 patch §B for product-owner reconciliation.
			PillarScores:   map[string]int{"Q1": 2, "Q2": 2, "Q3": 2, "Q4": 1, "Q5": 1, "Q6": 1, "Q7": 1, "Q8": 1, "Q9": 2},
			TotalScore:     13,
			MaxScore:       18,
			Band:           "strong",
			PassGateFailed: false,
			Q5Mechanism:    strp("fee_burn"),
			Q5AnnualUSD:    f64p(700_000_000),
			Q5FDVUSD:       f64p(252_000_000_000),
			Q9TeamNote:     strp("Post-founder maturity. Vitalik doxxed with multi-decade record. Ethereum Foundation + 2,000+ active devs are the broader team. Twice-yearly upgrade cadence (Pectra May 2025, Fusaka Dec 2025) shipped clean."),
			CatalystDate:   strp("2026-08-15"),
			CatalystNote:   strp("Glamsterdam upgrade scheduled H1 2026 (Q7 auto-decay if slips past mid-August)"),
			HoldingHorizon: "multi_year",
			BTCBeta:        "high",
			SecondaryTags:  []string{"monetary-l1", "settlement-layer", "ceiling-anchor"},
			MDFile:         "ETH_v1_locked.md",
			NextReviewDate: "2026-06-07",
		},
		{
			CoinSymbol:    "LUNC",
			CoinName:      "Terra Luna Classic",
			CoinGeckoID:   "terra-luna",
			AdapterSlug:   "speculative",
			ScorecardType: "alt_18",
			PillarScores:  map[string]int{"Q1": 0, "Q2": 1, "Q3": 0, "Q4": 0, "Q5": 1, "Q6": 0, "Q7": 1, "Q8": 0, "Q9": 0},
			TotalScore:    3,
			MaxScore:      18,
			Band:          "exit",
			PassGateFailed: true, // Q1=0, Q6=0, Q9=0 all fail; many pillars at 0
			Q5Mechanism:    strp("buyback"), // 1.2% transaction tax burn — closest semantic enum
			Q5AnnualUSD:    f64p(17_500_000),
			Q5FDVUSD:       f64p(526_000_000),
			Q9TeamNote:     strp("Do Kwon convicted of fraud, sentenced 15 years prison Dec 2025 (Judge Engelmayer: 'fraud of epic generational scale'). Banned from crypto transactions for life. Terraform Labs in bankruptcy. SEC won $4.55B settlement. UNIVERSAL VETO #5 TRIGGERED."),
			ActiveVeto:     strp("founder_rug"),
			ActiveVetoText: strp("Multi-VETO: Universal #5 (founder criminal record — Do Kwon 15yr sentence Dec 2025) + Speculative-specific kill criterion (50% drawdown from peak with no narrative renewal — LUNC down 99.99%+ from May 2022 peak)"),
			HoldingHorizon: "trade", // Speculative adapter enforces Trade/Medium; trigger 0032 verifies this
			BTCBeta:        "high",
			SecondaryTags:  []string{"floor-anchor", "post-collapse", "calibration-reference"},
			MDFile:         "LUNC_v1_locked.md",
			NextReviewDate: "2026-06-07",
		},
	}

	now := time.Now().Unix()
	for _, t := range seeds {
		// Look up adapter id.
		var adapterID int64
		if err := db.QueryRow(`SELECT id FROM crypto_adapters WHERE slug = ?`, t.AdapterSlug).Scan(&adapterID); err != nil {
			log.Fatalf("adapter %q not found: %v", t.AdapterSlug, err)
		}

		// Read MD body.
		mdPath := filepath.Join(*dir, t.MDFile)
		raw, err := os.ReadFile(mdPath)
		if err != nil {
			log.Fatalf("read %s: %v", mdPath, err)
		}
		md := string(raw)
		html := cryptotheses.Render(md)

		// Marshal JSON fields.
		pillarJSON, _ := json.Marshal(t.PillarScores)
		tagsJSON, _ := json.Marshal(t.SecondaryTags)

		// Compute Q5 derived pct.
		var q5Pct *float64
		if t.Q5AnnualUSD != nil && t.Q5FDVUSD != nil && *t.Q5FDVUSD > 0 {
			pct := (*t.Q5AnnualUSD / *t.Q5FDVUSD) * 100
			q5Pct = &pct
		}

		nextReviewUnix := parseDateUnix(t.NextReviewDate)

		passGateInt := 0
		if t.PassGateFailed {
			passGateInt = 1
		}
		var vetoTrippedAt sql.NullInt64
		if t.ActiveVeto != nil {
			vetoTrippedAt = sql.NullInt64{Int64: now, Valid: true}
		}

		_, err = db.Exec(`
			INSERT INTO crypto_theses (
				coin_symbol, coin_name, coingecko_id,
				primary_adapter_id, scorecard_type,
				pillar_scores_json, total_score, max_score, band,
				pillar_pass_gate_failed,
				q5_mechanism, q5_annual_usd, q5_fdv_usd, q5_accrual_pct,
				q9_team_note,
				active_veto, active_veto_reason, veto_tripped_at,
				catalyst_date, catalyst_note,
				holding_horizon, btc_beta, secondary_tags_json,
				liquidity_passed, liquidity_venues_json, liquidity_checked_at,
				status, version, markdown_current, rendered_html,
				locked_at, last_reviewed_at, next_review_at,
				created_at, updated_at
			) VALUES (
				?, ?, ?, ?, ?,
				?, ?, ?, ?, ?,
				?, ?, ?, ?, ?,
				?, ?, ?, ?, ?,
				?, ?, ?,
				1, ?, ?,
				'locked', 'v1', ?, ?,
				?, ?, ?,
				?, ?
			)
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
				active_veto = excluded.active_veto,
				active_veto_reason = excluded.active_veto_reason,
				holding_horizon = excluded.holding_horizon,
				btc_beta = excluded.btc_beta,
				markdown_current = excluded.markdown_current,
				rendered_html = excluded.rendered_html,
				updated_at = excluded.updated_at`,
			t.CoinSymbol, t.CoinName, t.CoinGeckoID,
			adapterID, t.ScorecardType,
			string(pillarJSON), t.TotalScore, t.MaxScore, t.Band, passGateInt,
			nullStr(t.Q5Mechanism), nullF64(t.Q5AnnualUSD), nullF64(t.Q5FDVUSD), nullF64(q5Pct),
			nullStr(t.Q9TeamNote),
			nullStr(t.ActiveVeto), nullStr(t.ActiveVetoText), vetoTrippedAt,
			nullStr(t.CatalystDate), nullStr(t.CatalystNote),
			t.HoldingHorizon, t.BTCBeta, string(tagsJSON),
			`["kraken","coinbase","binance"]`, now,
			md, html,
			now, now, nextReviewUnix,
			now, now)
		if err != nil {
			log.Fatalf("insert %s: %v", t.CoinSymbol, err)
		}

		fmt.Printf("  [%-4s] %d/%d %s, horizon=%s, beta=%s, veto=%v, q5_pct=%v\n",
			t.CoinSymbol, t.TotalScore, t.MaxScore, t.Band, t.HoldingHorizon, t.BTCBeta,
			t.ActiveVeto != nil, q5Pct)
	}

	// Summary.
	rows, err := db.Query(`
		SELECT t.coin_symbol, a.slug, t.scorecard_type, t.total_score, t.max_score, t.band,
		       t.holding_horizon, t.btc_beta, t.pillar_pass_gate_failed,
		       COALESCE(t.active_veto, '-'),
		       COALESCE(printf('%.2f%%', t.q5_accrual_pct), '-')
		  FROM crypto_theses t
		  JOIN crypto_adapters a ON a.id = t.primary_adapter_id
		 ORDER BY t.id`)
	if err != nil {
		log.Fatalf("summary: %v", err)
	}
	defer rows.Close()
	fmt.Println("\n--- final state of crypto_theses ---")
	for rows.Next() {
		var sym, slug, sc, band, horizon, beta, veto, q5 string
		var total, max, pgf int
		_ = rows.Scan(&sym, &slug, &sc, &total, &max, &band, &horizon, &beta, &pgf, &veto, &q5)
		fmt.Printf("  %-4s adapter=%-12s %d/%d %s horizon=%-10s beta=%-6s pass-gate-failed=%d veto=%-15s q5=%s\n",
			sym, slug, total, max, band, horizon, beta, pgf, veto, q5)
	}
}

func parseDateUnix(s string) sql.NullInt64 {
	if s == "" {
		return sql.NullInt64{}
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: t.Unix(), Valid: true}
}

func nullStr(p *string) sql.NullString {
	if p == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *p, Valid: true}
}

func nullF64(p *float64) sql.NullFloat64 {
	if p == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *p, Valid: true}
}
