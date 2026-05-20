package cryptoindicators

import (
	"context"
	"database/sql"
	"fmt"
	"ft/internal/cryptoindicators/providers"
	"log/slog"
	"math"
	"time"
)

// SeedBTCHistory does a one-shot full backfill of BTC daily closes from
// CoinGecko (~3500 rows back to 2013) if btc_price_history is empty.
// Idempotent — re-running with the table already populated is a no-op.
//
// Called from the refresher's daily run. Initial deploy: ~one big call.
// Subsequent days: appendOnly() handles the delta.
func (s *Service) SeedBTCHistory(ctx context.Context, cg *providers.CoinGeckoClient) error {
	var count int
	if err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM btc_price_history`).Scan(&count); err != nil {
		return err
	}
	if count > 100 {
		// Already seeded; just top-up recent days.
		return s.AppendBTCHistory(ctx, cg)
	}
	slog.Info("btc_price_history: seeding full history from CoinGecko")
	rows, err := cg.FetchBTCDailyHistory(ctx)
	if err != nil {
		return fmt.Errorf("fetch BTC history: %w", err)
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, r := range rows {
		date := r.Date.Format("2006-01-02")
		if _, err := tx.ExecContext(ctx,
			`INSERT OR REPLACE INTO btc_price_history (snapshot_date, close_usd) VALUES (?, ?)`,
			date, r.Close); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	slog.Info("btc_price_history: seeded", "rows", len(rows))
	return nil
}

// AppendBTCHistory tops up the last ~30 days from CoinGecko in case the
// cron has been off, or to catch the latest close. Cheap — same endpoint
// but a smaller window would be ideal; CoinGecko free tier doesn't
// support arbitrary date ranges for non-Pro callers, so we fetch the
// last ~30 days via days=30 and upsert.
func (s *Service) AppendBTCHistory(ctx context.Context, cg *providers.CoinGeckoClient) error {
	// Re-using FetchBTCDailyHistory is overkill (gets all of history). We
	// could be smarter, but the cost is one extra MB of JSON per day from
	// CoinGecko — negligible vs the rate-limit concern. Keep it simple.
	rows, err := cg.FetchBTCDailyHistory(ctx)
	if err != nil {
		return fmt.Errorf("append BTC history: %w", err)
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, r := range rows {
		date := r.Date.Format("2006-01-02")
		if _, err := tx.ExecContext(ctx,
			`INSERT OR REPLACE INTO btc_price_history (snapshot_date, close_usd) VALUES (?, ?)`,
			date, r.Close); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// BTCHistory returns all rows in chronological order. Used by the log-band
// regression. If the table is empty, returns an empty slice (caller treats
// indicators as "awaiting data").
func (s *Service) BTCHistory(ctx context.Context) ([]providers.BTCMarketChartDay, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT snapshot_date, close_usd FROM btc_price_history ORDER BY snapshot_date ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []providers.BTCMarketChartDay{}
	for rows.Next() {
		var dateStr string
		var close_ float64
		if err := rows.Scan(&dateStr, &close_); err != nil {
			return nil, err
		}
		t, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue
		}
		out = append(out, providers.BTCMarketChartDay{Date: t, Close: close_})
	}
	return out, rows.Err()
}

// ----- Cowen indicator math -----------------------------------------------

// CowenIndicators is the bundled result of running all three
// price-history-derived Cowen indicators. Returned by ComputeCowen so
// the refresher upserts them in one go.
type CowenIndicators struct {
	LogBand         string   // "lower" | "mid_lower" | "mid" | "mid_upper" | "upper" | ""
	LogBandValue    *float64 // residual in band-width units (for display)
	PriceVs200WMA   *float64 // current_close / 200wma (ratio)
	RiskProxy       *float64 // 0..1, mean of normalised inputs (per Spec 9e §D8)
	Latest          time.Time
}

// ComputeCowen runs the Cowen indicator math on the BTC history.
// Returns the latest readings + the log-band classification.
// If history is too short (<14 weeks) all fields are nil.
func ComputeCowen(history []providers.BTCMarketChartDay) CowenIndicators {
	var out CowenIndicators
	if len(history) < 14*7 {
		return out
	}
	out.Latest = history[len(history)-1].Date
	latest := history[len(history)-1].Close

	// --- 200-week MA ---
	wma := bucketize(history, 7)
	if len(wma) >= 200 {
		sum := 0.0
		for _, p := range wma[len(wma)-200:] {
			sum += p
		}
		ma200 := sum / 200.0
		if ma200 > 0 {
			ratio := latest / ma200
			out.PriceVs200WMA = &ratio
		}
	}

	// --- Log-linear regression: log10(close) = a + b*log10(days_since_genesis) ---
	// Genesis = first available date in history.
	if len(history) >= 100 {
		genesis := history[0].Date
		var sumX, sumY, sumXY, sumXX float64
		n := 0.0
		for _, p := range history {
			if p.Close <= 0 {
				continue
			}
			days := p.Date.Sub(genesis).Hours() / 24.0
			if days < 1 {
				continue
			}
			x := math.Log10(days)
			y := math.Log10(p.Close)
			sumX += x
			sumY += y
			sumXY += x * y
			sumXX += x * x
			n++
		}
		if n > 30 {
			meanX := sumX / n
			meanY := sumY / n
			slope := (sumXY - n*meanX*meanY) / (sumXX - n*meanX*meanX)
			intercept := meanY - slope*meanX

			// Residuals to determine band thirds.
			var residuals []float64
			for _, p := range history {
				if p.Close <= 0 {
					continue
				}
				days := p.Date.Sub(genesis).Hours() / 24.0
				if days < 1 {
					continue
				}
				predicted := intercept + slope*math.Log10(days)
				actual := math.Log10(p.Close)
				residuals = append(residuals, actual-predicted)
			}
			// Find percentiles of residuals to define thirds (Glassnode style).
			// For simplicity: classify by quintiles to map to 5 named bands.
			pct := func(p float64) float64 {
				if len(residuals) == 0 {
					return 0
				}
				sorted := make([]float64, len(residuals))
				copy(sorted, residuals)
				// quick insertion sort (residuals is up to ~3500 items; still fine)
				for i := 1; i < len(sorted); i++ {
					for j := i; j > 0 && sorted[j-1] > sorted[j]; j-- {
						sorted[j-1], sorted[j] = sorted[j], sorted[j-1]
					}
				}
				idx := int(p * float64(len(sorted)-1))
				return sorted[idx]
			}
			// Current residual.
			daysNow := history[len(history)-1].Date.Sub(genesis).Hours() / 24.0
			currentResidual := math.Log10(latest) - (intercept + slope*math.Log10(daysNow))
			out.LogBandValue = &currentResidual

			q20 := pct(0.20)
			q40 := pct(0.40)
			q60 := pct(0.60)
			q80 := pct(0.80)
			switch {
			case currentResidual <= q20:
				out.LogBand = "lower"
			case currentResidual <= q40:
				out.LogBand = "mid_lower"
			case currentResidual <= q60:
				out.LogBand = "mid"
			case currentResidual <= q80:
				out.LogBand = "mid_upper"
			default:
				out.LogBand = "upper"
			}
		}
	}

	// --- Risk proxy: mean of (log_band_normalized, price/200wma normalized) ---
	// Per Spec 9e §D8: MVRV-Z is Phase 2+ (we don't have it), so this is
	// the "2-input proxy" the spec explicitly accepts in v1.
	if out.LogBandValue != nil && out.PriceVs200WMA != nil {
		// Normalise log-band residual to [0, 1] via quintile position.
		var lbNorm float64
		switch out.LogBand {
		case "lower":
			lbNorm = 0.1
		case "mid_lower":
			lbNorm = 0.3
		case "mid":
			lbNorm = 0.5
		case "mid_upper":
			lbNorm = 0.7
		case "upper":
			lbNorm = 0.9
		}
		// Normalise price/200wma ratio: clamp to [0.5, 5.0] then map to [0, 1].
		ratio := *out.PriceVs200WMA
		if ratio < 0.5 {
			ratio = 0.5
		}
		if ratio > 5.0 {
			ratio = 5.0
		}
		ratioNorm := (ratio - 0.5) / (5.0 - 0.5)
		// Mean.
		risk := (lbNorm + ratioNorm) / 2.0
		if risk < 0 {
			risk = 0
		}
		if risk > 1 {
			risk = 1
		}
		out.RiskProxy = &risk
	}
	return out
}

// bucketize folds a daily series into weekly closes by taking every
// stride-th element. Stride=7 for weekly. Returned in chronological order.
func bucketize(daily []providers.BTCMarketChartDay, stride int) []float64 {
	out := make([]float64, 0, len(daily)/stride+1)
	for i := stride - 1; i < len(daily); i += stride {
		out = append(out, daily[i].Close)
	}
	return out
}

// PriceVs200WMARatio is a tiny convenience for use in places where we
// only want the ratio (e.g. exposing a separate indicator).
func PriceVs200WMARatio(c CowenIndicators) *float64 {
	return c.PriceVs200WMA
}

// suppress unused
var _ = sql.ErrNoRows
