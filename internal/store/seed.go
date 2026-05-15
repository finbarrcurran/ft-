package store

import (
	"context"
	"fmt"
	"ft/internal/domain"
)

// SeedFXSnapshotEURUSD is the EUR→USD rate baked into the seed data. The real
// app updates this from the FX adapter on each import / refresh.
const SeedFXSnapshotEURUSD = 1.08

// SeedHoldings replaces all holdings owned by userID with the seeded set
// (Fin's real 23 stocks + 13 crypto from the prototype's lib/data-store/*).
// Idempotent: safe to re-run.
func (s *Store) SeedHoldings(ctx context.Context, userID int64) (nStocks, nCrypto int, err error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM stock_holdings WHERE user_id = ?`, userID); err != nil {
		return 0, 0, fmt.Errorf("delete stocks: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM crypto_holdings WHERE user_id = ?`, userID); err != nil {
		return 0, 0, fmt.Errorf("delete crypto: %w", err)
	}

	for _, h := range seedStocks(userID) {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO stock_holdings (
			    user_id, name, ticker, category, sector,
			    invested_usd, avg_open_price, current_price,
			    rsi14, ma50, ma200, golden_cross,
			    support, resistance, analyst_target,
			    proposed_entry, technical_setup, analyst_rr_view,
			    stop_loss, take_profit, strategy_note,
			    updated_at
			 ) VALUES (?,?,?,?,?, ?,?,?, ?,?,?,?, ?,?,?, ?,?,?, ?,?,?, strftime('%s','now'))`,
			h.UserID, h.Name, strPtrToNull(h.Ticker), strPtrToNull(h.Category), strPtrToNull(h.Sector),
			h.InvestedUSD, fp(h.AvgOpenPrice), fp(h.CurrentPrice),
			fp(h.RSI14), fp(h.MA50), fp(h.MA200), bp(h.GoldenCross),
			fp(h.Support), fp(h.Resistance), fp(h.AnalystTarget),
			fp(h.ProposedEntry),
			stringFromPtrOrEmpty(h.TechnicalSetup),
			stringFromPtrOrEmpty(h.AnalystRRView),
			fp(h.StopLoss), fp(h.TakeProfit), h.StrategyNote,
		)
		if err != nil {
			return 0, 0, fmt.Errorf("insert stock %s: %w", h.Name, err)
		}
		nStocks++
	}

	for _, h := range seedCrypto(userID) {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO crypto_holdings (
			    user_id, name, symbol, classification, category, wallet,
			    quantity_held, quantity_staked,
			    avg_buy_eur, cost_basis_eur, current_price_eur, current_value_eur,
			    avg_buy_usd, cost_basis_usd, current_price_usd, current_value_usd,
			    rsi14, change_7d_pct, change_30d_pct, strategy_note,
			    updated_at
			 ) VALUES (?,?,?,?,?,?, ?,?, ?,?,?,?, ?,?,?,?, ?,?,?,?, strftime('%s','now'))`,
			h.UserID, h.Name, h.Symbol, h.Classification, strPtrToNull(h.Category), strPtrToNull(h.Wallet),
			h.QuantityHeld, h.QuantityStaked,
			fp(h.AvgBuyEUR), fp(h.CostBasisEUR), fp(h.CurrentPriceEUR), fp(h.CurrentValueEUR),
			fp(h.AvgBuyUSD), fp(h.CostBasisUSD), fp(h.CurrentPriceUSD), fp(h.CurrentValueUSD),
			fp(h.RSI14), fp(h.Change7dPct), fp(h.Change30dPct), h.StrategyNote,
		)
		if err != nil {
			return 0, 0, fmt.Errorf("insert crypto %s: %w", h.Name, err)
		}
		nCrypto++
	}

	// Persist FX snapshot.
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO meta (key, value) VALUES ('fx_snapshot_eur_usd', ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		fmt.Sprintf("%g", SeedFXSnapshotEURUSD),
	); err != nil {
		return 0, 0, fmt.Errorf("meta fx: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return nStocks, nCrypto, nil
}

// --- Seed data: Fin's actual 23 stocks (from lib/data-store/seed.ts) -------

func seedStocks(userID int64) []*domain.StockHolding {
	type row struct {
		name, ticker, category                                              string
		invested, avgOpen, current                                          float64
		rsi, ma50, ma200                                                    float64
		golden                                                              bool
		support, resistance, stopLoss, takeProfit, analystTarget            float64
	}
	rows := []row{
		{"Rolls-Royce", "RR.L", "Defense", 1727.56, 1126.51, 1188.0, 58, 1150, 1080, true, 1110, 1240, 1080, 1300, 1280},
		{"SPDR Gold", "GLD", "Precious Metals", 1917.68, 466.91, 428.6, 42, 442, 448, false, 420, 458, 410, 470, 455},
		{"iShares Silver Trust", "SLV", "Precious Metals", 1254.34, 76.46, 76.34, 51, 75.5, 73.0, true, 73, 80, 71, 84, 82},
		{"NVIDIA Corporation", "NVDA", "Semiconductors", 200.0, 198.14, 217.48, 78, 205, 180, true, 200, 225, 195, 240, 235},
		{"Taiwan Semiconductor", "TSM", "Semiconductors", 200.0, 404.04, 388.4, 48, 395, 380, true, 375, 410, 360, 425, 420},
		{"ASML Holding NV", "ASML", "Semiconductors", 200.0, 1565.62, 1481.33, 45, 1510, 1530, false, 1450, 1560, 1440, 1620, 1600},
		{"Wheaton Precious Metals", "WPM", "Precious Metals", 145.0, 138.3, 138.31, 52, 137, 130, true, 132, 145, 128, 152, 150},
		{"Agnico Eagle Mines", "AEM", "Precious Metals", 135.0, 191.23, 191.13, 51, 189, 178, true, 182, 198, 176, 210, 205},
		{"Oracle Corporation", "ORCL", "Software", 100.0, 172.67, 183.95, 64, 178, 165, true, 175, 190, 168, 200, 195},
		{"Exxon Mobil", "XOM", "Energy", 100.0, 149.12, 151.44, 54, 150, 145, true, 146, 156, 142, 162, 160},
		{"Caterpillar", "CAT", "Industrials", 100.0, 915.79, 898.23, 49, 905, 880, true, 875, 925, 855, 950, 940},
		{"ARM Holdings", "ARM", "Semiconductors", 100.0, 209.89, 203.92, 47, 208, 195, true, 195, 215, 188, 225, 220},
		{"Broadcom Inc", "AVGO", "Semiconductors", 100.0, 430.22, 412.82, 46, 420, 410, true, 410, 432, 405, 445, 440},
		{"Schneider Electric", "SU.PA", "Industrials", 100.0, 275.05, 264.05, 47, 270, 260, true, 258, 275, 250, 285, 282},
		{"Lattice Semiconductor", "LSCC", "Semiconductors", 100.0, 129.08, 123.54, 70, 125, 118, true, 118, 130, 112, 138, 135},
		{"Coherent Corp", "COHR", "Semiconductors", 100.0, 379.99, 357.7, 44, 370, 360, true, 350, 380, 335, 395, 390},
		{"Modine Manufacturing", "MOD", "Industrials", 100.0, 286.61, 268.89, 42, 278, 265, true, 260, 285, 250, 300, 295},
		{"Bloom Energy", "BE", "Energy", 100.0, 291.57, 272.23, 40, 282, 270, true, 265, 290, 255, 305, 298},
		{"Rigetti Computing", "RGTI", "Quantum", 100.0, 20.4, 18.77, 38, 19.5, 20.2, false, 18.3, 20.0, 18.2, 22.0, 21.5},
		{"AngloGold Ashanti", "AU", "Precious Metals", 85.0, 101.25, 101.35, 52, 100, 95, true, 95, 106, 92, 112, 110},
		{"Pan American Silver", "PAAS", "Precious Metals", 85.0, 61.2, 61.12, 53, 60.5, 58, true, 58, 64, 56, 68, 66},
		{"Rheinmetall AG", "RHM.DE", "Defense", 88.51, 1879.7, 1161.0, 30, 1180, 1175, true, 1100, 1280, 1050, 1400, 1380},
		{"Shin-Etsu Chemical", "4063.T", "Materials", 50.0, 7601.0, 7526.0, 49, 7550, 7400, true, 7400, 7700, 7250, 7900, 7850},
	}
	out := make([]*domain.StockHolding, 0, len(rows))
	for _, r := range rows {
		ticker := r.ticker
		category := r.category
		out = append(out, &domain.StockHolding{
			UserID:        userID,
			Name:          r.name,
			Ticker:        &ticker,
			Category:      &category,
			InvestedUSD:   r.invested,
			AvgOpenPrice:  &r.avgOpen,
			CurrentPrice:  &r.current,
			RSI14:         &r.rsi,
			MA50:          &r.ma50,
			MA200:         &r.ma200,
			GoldenCross:   &r.golden,
			Support:       &r.support,
			Resistance:    &r.resistance,
			StopLoss:      &r.stopLoss,
			TakeProfit:    &r.takeProfit,
			AnalystTarget: &r.analystTarget,
		})
	}
	return out
}

// --- Seed data: Fin's actual 13 crypto (from lib/data-store/crypto-seed.ts) ---

func seedCrypto(userID int64) []*domain.CryptoHolding {
	type row struct {
		name, symbol, classification, category, wallet                        string
		qtyHeld, qtyStaked                                                   float64
		avgEur, costEur                                                       *float64 // nullable for XVM
		curEur, valEur                                                        float64
		rsi, c7, c30                                                          float64
	}
	f := func(v float64) *float64 { return &v }
	rows := []row{
		{"Bitcoin", "BTC", "core", "Layer 1", "Ledger", 0.160754, 0, f(96304.90), f(15512.79), 83578.00, 13435.50, 44, -3.2, -8.1},
		{"Ethereum", "ETH", "core", "Layer 1", "Ledger", 0.609190, 0, f(3323.76), f(2027.49), 2884.03, 1756.92, 42, -2.8, -6.4},
		{"XRP", "XRP", "alt", "Payments", "Binance", 939.691654, 0, f(2.07), f(1974.68), 1.85, 1736.08, 46, -1.5, -5.2},
		{"Avalanche", "AVAX", "alt", "Layer 1", "Binance", 13.982, 0, f(14.45), f(202.30), 12.54, 175.33, 38, -4.1, -12.3},
		{"Cardano", "ADA", "alt", "Layer 1", "Binance", 318.88, 0, f(0.47), f(150.94), 0.36, 113.68, 34, -5.8, -18.2},
		{"Sui", "SUI", "alt", "Layer 1", "Binance", 83.260770, 0, f(1.76), f(146.70), 1.58, 131.33, 41, -3.0, -7.5},
		{"Solana", "SOL", "alt", "Layer 1", "Phantom", 10.679671, 0, f(121.52), f(1299.90), 126.10, 1346.71, 58, 4.2, 8.9},
		{"Hedera", "HBAR", "alt", "Layer 1", "Binance", 969.87, 0, f(0.20), f(200.00), 0.11, 102.38, 31, -7.4, -22.1},
		{"Arbitrum", "ARB", "alt", "Layer 2", "MetaMask", 3645.5, 0, f(0.1783), f(649.51), 0.2000, 721.08, 62, 6.8, 11.5},
		{"Polygon", "POL", "alt", "Layer 2", "MetaMask", 1728.2399, 0, f(0.14), f(250.00), 0.14, 233.83, 48, -1.2, -2.8},
		{"Optimism", "OP", "alt", "Layer 2", "MetaMask", 1269.106111, 0, f(0.27), f(349.24), 0.30, 385.81, 56, 3.4, 7.2},
		{"Chainlink", "LINK", "alt", "Oracle", "Binance", 100.647250, 0, f(11.91), f(1199.93), 12.89, 1297.34, 59, 5.1, 10.3},
		// Volt — partial record (no avg / cost; record was lost)
		{"Volt", "XVM", "alt", "Other", "Phantom", 2137.723343, 0, nil, nil, 0.0018, 3.85, 50, 0, 0},
	}
	fx := SeedFXSnapshotEURUSD
	toUsd := func(eur *float64) *float64 {
		if eur == nil {
			return nil
		}
		v := *eur * fx
		return &v
	}
	out := make([]*domain.CryptoHolding, 0, len(rows))
	for _, r := range rows {
		category := r.category
		wallet := r.wallet
		curEur := r.curEur
		valEur := r.valEur
		curUsd := r.curEur * fx
		valUsd := r.valEur * fx
		out = append(out, &domain.CryptoHolding{
			UserID:          userID,
			Name:            r.name,
			Symbol:          r.symbol,
			Classification:  r.classification,
			Category:        &category,
			Wallet:          &wallet,
			QuantityHeld:    r.qtyHeld,
			QuantityStaked:  r.qtyStaked,
			AvgBuyEUR:       r.avgEur,
			CostBasisEUR:    r.costEur,
			CurrentPriceEUR: &curEur,
			CurrentValueEUR: &valEur,
			AvgBuyUSD:       toUsd(r.avgEur),
			CostBasisUSD:    toUsd(r.costEur),
			CurrentPriceUSD: &curUsd,
			CurrentValueUSD: &valUsd,
			RSI14:           &r.rsi,
			Change7dPct:     &r.c7,
			Change30dPct:    &r.c30,
		})
	}
	return out
}
