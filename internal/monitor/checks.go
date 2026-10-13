package monitor

import (
	"context"
	"database/sql"
	"ft/internal/news"
	"os"
	"path/filepath"
	"time"
)

// providerProbe reads provider_health.last_success_at for one provider.
func providerProbe(db *sql.DB, provider string) Probe {
	return func(ctx context.Context) (time.Time, bool, string, error) {
		var ls sql.NullInt64
		var lastErr sql.NullString
		var consec int
		err := db.QueryRowContext(ctx,
			`SELECT last_success_at, last_error, consecutive_failures FROM provider_health WHERE provider = ?`,
			provider).Scan(&ls, &lastErr, &consec)
		if err == sql.ErrNoRows {
			return time.Time{}, false, "never called", nil
		}
		if err != nil {
			return time.Time{}, false, "", err
		}
		detail := ""
		if consec > 0 && lastErr.Valid {
			detail = lastErr.String
			if len(detail) > 90 {
				detail = detail[:90] + "…"
			}
		}
		if !ls.Valid {
			return time.Time{}, false, "never succeeded; " + detail, nil
		}
		return time.Unix(ls.Int64, 0).UTC(), true, detail, nil
	}
}

// sqlTimeProbe runs a query that returns one unix timestamp (NULL = never).
func sqlTimeProbe(db *sql.DB, query string) Probe {
	return func(ctx context.Context) (time.Time, bool, string, error) {
		var v sql.NullInt64
		if err := db.QueryRowContext(ctx, query).Scan(&v); err != nil && err != sql.ErrNoRows {
			return time.Time{}, false, "", err
		}
		if !v.Valid {
			return time.Time{}, false, "no successful run on record", nil
		}
		return time.Unix(v.Int64, 0).UTC(), true, "", nil
	}
}

// fileProbe uses a file's modification time (newest match for a glob).
func fileProbe(glob string) Probe {
	return func(ctx context.Context) (time.Time, bool, string, error) {
		matches, _ := filepath.Glob(glob)
		var newest time.Time
		for _, m := range matches {
			if st, err := os.Stat(m); err == nil && st.ModTime().After(newest) {
				newest = st.ModTime()
			}
		}
		if newest.IsZero() {
			return time.Time{}, false, "file not found", nil
		}
		return newest.UTC(), true, "", nil
	}
}

// DefaultChecks lists every external source FT depends on, each with the
// longest silence its schedule allows. Tune MaxAge here; Optional sources are
// shown in Diagnostics but never alert.
func DefaultChecks(db *sql.DB) []Check {
	const h = time.Hour
	checks := []Check{
		// Free-tier price/market APIs (called many times a day; the limits are
		// tight, so a long silence — not individual 429s — is the signal).
		{Source: "yahoo", Label: "Yahoo Finance (prices)", MaxAge: 12 * h, Probe: providerProbe(db, "yahoo")},
		{Source: "coingecko", Label: "CoinGecko (crypto prices)", MaxAge: 12 * h, Probe: providerProbe(db, "coingecko")},
		{Source: "finnhub", Label: "Finnhub", MaxAge: 12 * h, Probe: providerProbe(db, "finnhub")},
		{Source: "frankfurter", Label: "Frankfurter (FX)", MaxAge: 36 * h, Probe: providerProbe(db, "frankfurter")},
		{Source: "alternative_me", Label: "Alternative.me (Fear & Greed)", MaxAge: 36 * h, Probe: providerProbe(db, "alternative_me")},
		// Request-driven feeds (called when a tab/card is opened, cached 60 min). The daily
		// 06:30 UTC warm-up (internal/server/warmup.go) keeps them fresh, so 36 h is the
		// right allowance; before SC-48 they sat on a 72 h rule nobody satisfied.
		{Source: "cnn_feargreed", Label: "CNN Fear & Greed", MaxAge: 36 * h, Probe: providerProbe(db, "cnn_feargreed")},
		{Source: "rss_crypto", Label: "Crypto RSS news", MaxAge: 36 * h, Probe: providerProbe(db, "rss_crypto")},
		// Market news (RSS cut-over 2026-10-13; NewsAPI is no longer called or monitored).
		// Tier A feeds are displayed and alert on their own; tier B feeds are a measured
		// backup (optional: shown, never alert). One alert per outage names the feed.

		// Congress feed (SC-47): daily 23:10 UTC sync. A quota stop with data
		// stored is 'partial' and still counts as a success.
		{Source: "bargo", Label: "Bargo Congress feed", MaxAge: 36 * h,
			Probe: sqlTimeProbe(db, `SELECT MAX(finished_at) FROM bargo_sync_runs WHERE status IN ('ok','partial')`)},

		// eToro sync polls every 30 min.
		{Source: "etoro", Label: "eToro sync", MaxAge: 3 * h,
			Probe: sqlTimeProbe(db, `SELECT MAX(finished_at) FROM etoro_sync_runs WHERE status = 'ok'`)},

		// Farside ETF flows (nightly 00:25 scraper). SC-49: judged by the NEWEST ROW'S DATE in US
		// trading days (alert at >= 3), not by the file's modification time.
		{Source: "farside", Label: "Farside ETF-flow data", MaxAge: 2 * 24 * h, Eval: etfFlowEval(FarsideFile)},

		// eToro statement coverage (SC-51): Fin uploads statements by hand, so the data goes
		// quiet when he forgets. Alert once the coverage end is more than 35 days old, remind weekly.
		{Source: "statement_coverage", Label: "eToro statement coverage", MaxAge: 35 * 24 * h,
			ReminderEvery: 7 * 24 * h, Eval: statementCoverageEval(db)},

		// Google Drive bridge (SC-45 spec mirror, SC-47 library): the exporters touch a marker
		// file on every successful run, including "nothing changed" runs (every 10 minutes).
		{Source: "drive_spec", Label: "Drive spec mirror", MaxAge: 2 * h, Probe: fileProbe("/var/lib/ft/data/bridge/spec.ok")},
		{Source: "drive_library", Label: "Drive library mirror", MaxAge: 2 * h, Probe: fileProbe("/var/lib/ft/data/bridge/library.ok")},

		// Nightly DB backup (cron 03:15).
		{Source: "db_backup", Label: "Nightly DB backup", MaxAge: 30 * h, Probe: fileProbe("/var/backups/ft/ft-*.db")},
	}
	checks = append(checks, indicatorChecksFor(db)...)
	return append(checks, marketFeedChecks(db)...)
}

// marketFeedChecks adds one check per market-news RSS feed (tier A alert, tier B optional).
func marketFeedChecks(db *sql.DB) []Check {
	var out []Check
	for _, f := range news.MarketFeedList() {
		out = append(out, Check{
			Source: "rss_market_" + f.Key, Label: f.Name + " (market news RSS)", MaxAge: 36 * time.Hour,
			Optional: f.Tier != "A", Probe: providerProbe(db, "rss_market_"+f.Key),
		})
	}
	return out
}

