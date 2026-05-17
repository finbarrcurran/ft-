// Spec 7 — Diagnostics & provider health.
//
// One endpoint, one big payload:
//   GET /api/diagnostics
//
// Returns everything Settings → Diagnostics needs to render in one round-trip:
//   - providers[]      — provider_health rows
//   - apiKeys[]        — which FT_*_API_KEY env vars are set / missing
//   - system           — last refresh, last daily job, DB size, schema version
//   - backups          — latest backup file + age + size (best-effort filesystem read)
//   - frameworks[]     — loaded framework IDs + question counts
//   - holidays[]       — exchange × year coverage flags
//
// Cookie OR token auth — bot doesn't need it but consistency.
package server

import (
	"ft/internal/frameworks"
	"ft/internal/marketdata"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

// GET /api/diagnostics
func (s *Server) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{}

	// Provider health rows.
	providers, _ := s.store.ListProviderHealth(r.Context())
	out["providers"] = providers

	// API key presence — no values, just bools.
	keys := []string{
		"FT_FINNHUB_API_KEY", "FT_TWELVEDATA_API_KEY",
		"NEWSAPI_API_KEY", "CRYPTOPANIC_API_KEY",
		"FT_ANTHROPIC_API_KEY",
		"FT_TELEGRAM_BOT_TOKEN", "FT_TELEGRAM_CHAT_ID",
	}
	apiKeys := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		apiKeys = append(apiKeys, map[string]any{
			"key":    k,
			"set":    os.Getenv(k) != "",
		})
	}
	out["apiKeys"] = apiKeys

	// System / meta.
	system := map[string]any{}
	if v, _ := s.store.GetMeta(r.Context(), "last_refreshed_at"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			t := time.Unix(n, 0).UTC()
			system["lastRefreshAt"] = t
			system["lastRefreshAgoSec"] = int64(time.Since(t).Seconds())
		}
	}
	if v, _ := s.store.GetMeta(r.Context(), "last_partial_failure_at"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			system["lastPartialFailureAt"] = time.Unix(n, 0).UTC()
		}
	}
	if v, _ := s.store.GetMeta(r.Context(), "last_daily_job_at"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			t := time.Unix(n, 0).UTC()
			system["lastDailyJobAt"] = t
			system["lastDailyJobAgoSec"] = int64(time.Since(t).Seconds())
		}
	}
	// DB file size — read from filesystem.
	dbPath := s.cfg.DBPath
	if dbPath != "" {
		if fi, err := os.Stat(dbPath); err == nil {
			system["dbSizeBytes"] = fi.Size()
			system["dbModifiedAt"] = fi.ModTime().UTC()
		}
		system["dbPath"] = dbPath
	}
	// Latest applied migration.
	row := s.store.DB.QueryRowContext(r.Context(),
		`SELECT version, applied_at FROM schema_migrations ORDER BY version DESC LIMIT 1`)
	var ver string
	var applied int64
	if err := row.Scan(&ver, &applied); err == nil {
		system["latestMigration"] = ver
		system["latestMigrationAt"] = time.Unix(applied, 0).UTC()
	}
	out["system"] = system

	// Backups — scan /var/backups/ft for the latest .db file.
	out["backups"] = scanBackups("/var/backups/ft")

	// Frameworks.
	fws := frameworks.All()
	fwOut := make([]map[string]any, 0, len(fws))
	for _, f := range fws {
		fwOut = append(fwOut, map[string]any{
			"id":            f.ID,
			"name":          f.Name,
			"appliesTo":     f.AppliesTo,
			"questions":     len(f.Questions),
			"passThreshold": f.Scoring.PassThreshold,
		})
	}
	out["frameworks"] = fwOut

	// Holidays — current year coverage per exchange.
	currentYear := time.Now().UTC().Year()
	out["holidays"] = holidayCoverage(currentYear)

	writeJSON(w, http.StatusOK, out)
}

// scanBackups walks dir for *.db files and returns up to 5 newest, with size + age.
func scanBackups(dir string) []map[string]any {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []map[string]any{}
	}
	type info struct {
		name    string
		modTime time.Time
		size    int64
	}
	var rows []info
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if filepath.Ext(name) != ".db" {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		rows = append(rows, info{name: name, modTime: fi.ModTime(), size: fi.Size()})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].modTime.After(rows[j].modTime) })
	if len(rows) > 5 {
		rows = rows[:5]
	}
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		out = append(out, map[string]any{
			"name":      r.name,
			"sizeBytes": r.size,
			"modTime":   r.modTime.UTC(),
			"ageHours":  int64(time.Since(r.modTime).Hours()),
		})
	}
	return out
}

// holidayCoverage reports whether each exchange has any holidays defined for
// the given year. Surfaces the "December rollover" issue Spec 5 flagged.
func holidayCoverage(year int) []map[string]any {
	exchanges := []string{"US", "LSE", "EURONEXT", "XETRA", "TSE", "HKEX", "B3"}
	out := make([]map[string]any, 0, len(exchanges))
	for _, ex := range exchanges {
		count := marketdata.HolidayCountForYear(ex, year)
		out = append(out, map[string]any{
			"exchange": ex,
			"year":     year,
			"count":    count,
		})
	}
	return out
}
