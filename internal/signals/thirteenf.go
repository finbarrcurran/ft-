package signals

// SC-23 — 13F Institutional Tracker.
//
// Tracks a user-curated watchlist of 13F filer CIKs (tracked_funds). Each
// quarter we pull the fund's latest 13F-HR from SEC EDGAR (free, no key —
// reuses the secClient + User-Agent from insider.go), snapshot the book into
// fund_13f_holdings, diff it quarter-over-quarter into fund_13f_diffs, and flag
// overlap with FT's universe. FLAG/ALARM diffs (the ones touching YOUR book)
// are emitted as signal_events rows (signal_type='thirteenf') so they ride
// 9k's existing unified feed + Telegram routing — no parallel system (AC7).
//
// Two hard rules:
//
//   - The §C caveat is acceptance-critical (S-23a). 13F is a PARTIAL, lagged,
//     quarterly snapshot: long US equity + listed options only — NOT shorts,
//     swaps, cash or private holdings, filed up to 45 days late. For a short-
//     thesis fund (Situational Awareness's semiconductor puts) reading the 13F
//     as "their bet" actively misleads. Caveat278T's sibling Caveat13F is
//     surfaced verbatim on every view + alert.
//
//   - CUSIP→ticker is enrich-and-flag (S-23b). 13F is CUSIP-keyed; FT keys on
//     ticker. We map via cusip_ticker_map; an unmapped CUSIP is kept, marked
//     'cusip_unmapped', and excluded from overlap evaluation — never guessed.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Caveat13F is the mandatory label that must accompany every 13F view and
// every alert (spec §C / AC5 / S-23a). Surfaced verbatim in the API payload so
// the UI banner and any downstream (Telegram) consumer carry the same words.
const Caveat13F = "Quarterly · ~45-day lag · long-equity + listed-options only · not the full book (no shorts / swaps / cash)."

// thirteenFMaxFilingsPerPull is how many recent 13F-HR filings to ingest on a
// pull. Two means a fresh fund yields a usable quarter-over-quarter diff on the
// very first run instead of waiting a quarter.
const thirteenFMaxFilingsPerPull = 2

// ---- EDGAR JSON / XML shapes ---------------------------------------------

// submissionsDoc is the slice of data.sec.gov/submissions/CIK##########.json
// we care about: the parallel "recent" arrays.
type submissionsDoc struct {
	CIK     string `json:"cik"`
	Name    string `json:"name"`
	Filings struct {
		Recent struct {
			AccessionNumber []string `json:"accessionNumber"`
			Form            []string `json:"form"`
			FilingDate      []string `json:"filingDate"`
			ReportDate      []string `json:"reportDate"`
			PrimaryDocument []string `json:"primaryDocument"`
		} `json:"recent"`
	} `json:"filings"`
}

// thirteenFiling is one resolved 13F-HR filing reference.
type thirteenFiling struct {
	Accession  string
	FilingDate string
	ReportDate string // period_of_report (quarter end)
}

// informationTable mirrors the 13F information-table XML. Tags omit the
// namespace so encoding/xml matches by local name regardless of the (varying)
// SEC namespace prefix.
type informationTable struct {
	InfoTables []infoTableEntry `xml:"infoTable"`
}

type infoTableEntry struct {
	NameOfIssuer string `xml:"nameOfIssuer"`
	TitleOfClass string `xml:"titleOfClass"`
	Cusip        string `xml:"cusip"`
	Value        string `xml:"value"`
	ShrsOrPrn    struct {
		SshPrnamt     string `xml:"sshPrnamt"`
		SshPrnamtType string `xml:"sshPrnamtType"`
	} `xml:"shrsOrPrnAmt"`
	PutCall string `xml:"putCall"`
}

// ---- Fund watchlist management -------------------------------------------

// TrackedFund is the API shape for the tracked-funds panel.
type TrackedFund struct {
	CIK          string  `json:"cik"`
	Name         string  `json:"name"`
	Notes        *string `json:"notes,omitempty"`
	LastPeriod   *string `json:"lastPeriod,omitempty"`
	LastPulledAt *string `json:"lastPulledAt,omitempty"`
	Holdings     int     `json:"holdings"`     // positions in the latest period
	DiffNew      int     `json:"diffNew"`
	DiffExit     int     `json:"diffExit"`
	DiffIncrease int     `json:"diffIncrease"`
	DiffDecrease int     `json:"diffDecrease"`
	Overlaps     int     `json:"overlaps"`     // diffs touching the universe
	Alarms       int     `json:"alarms"`
}

// normaliseCIK zero-pads a CIK to the 10-digit form EDGAR's submissions API
// and our tracked_funds PK use. Accepts "2045724", "0002045724", "CIK2045724".
func normaliseCIK(raw string) (string, error) {
	s := strings.TrimSpace(strings.ToUpper(raw))
	s = strings.TrimPrefix(s, "CIK")
	s = strings.TrimSpace(s)
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return "", fmt.Errorf("invalid CIK: %q", raw)
	}
	return fmt.Sprintf("%010d", n), nil
}

// ListTrackedFunds returns the active fund watchlist with per-fund diff
// summaries for the latest period. Drives the tracked-funds panel.
func (s *Service) ListTrackedFunds(ctx context.Context) ([]TrackedFund, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT cik, name, notes, last_period, last_pulled_at
		  FROM tracked_funds
		 WHERE active = 1
		 ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TrackedFund{}
	for rows.Next() {
		var f TrackedFund
		var notes, lastPeriod, lastPulled sql.NullString
		if err := rows.Scan(&f.CIK, &f.Name, &notes, &lastPeriod, &lastPulled); err != nil {
			return nil, err
		}
		if notes.Valid && strings.TrimSpace(notes.String) != "" {
			f.Notes = &notes.String
		}
		if lastPeriod.Valid && lastPeriod.String != "" {
			f.LastPeriod = &lastPeriod.String
		}
		if lastPulled.Valid && lastPulled.String != "" {
			f.LastPulledAt = &lastPulled.String
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Enrich each fund with counts for its latest period.
	for i := range out {
		f := &out[i]
		if f.LastPeriod == nil {
			continue
		}
		_ = s.DB.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM fund_13f_holdings WHERE cik = ? AND period_of_report = ?`,
			f.CIK, *f.LastPeriod).Scan(&f.Holdings)
		drows, err := s.DB.QueryContext(ctx,
			`SELECT change_type, COUNT(*),
			        SUM(CASE WHEN overlaps_universe = 1 THEN 1 ELSE 0 END),
			        SUM(CASE WHEN tier = 'alarm' THEN 1 ELSE 0 END)
			   FROM fund_13f_diffs WHERE cik = ? AND period = ? GROUP BY change_type`,
			f.CIK, *f.LastPeriod)
		if err != nil {
			continue
		}
		for drows.Next() {
			var ct string
			var n, ov, al int
			if err := drows.Scan(&ct, &n, &ov, &al); err != nil {
				break
			}
			f.Overlaps += ov
			f.Alarms += al
			switch ct {
			case "new":
				f.DiffNew = n
			case "exit":
				f.DiffExit = n
			case "increase":
				f.DiffIncrease = n
			case "decrease":
				f.DiffDecrease = n
			}
		}
		drows.Close()
	}
	return out, nil
}

// AddTrackedFund adds (or reactivates) a fund CIK. Name is best-effort: if the
// caller doesn't supply one we leave a placeholder until the first pull fills
// it from the submissions doc.
func (s *Service) AddTrackedFund(ctx context.Context, rawCIK, name, notes string) (string, error) {
	cik, err := normaliseCIK(rawCIK)
	if err != nil {
		return "", err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "CIK " + cik
	}
	_, err = s.DB.ExecContext(ctx, `
		INSERT INTO tracked_funds (cik, name, notes, active)
		VALUES (?, ?, ?, 1)
		ON CONFLICT(cik) DO UPDATE SET
		    name = CASE WHEN excluded.name LIKE 'CIK %' THEN tracked_funds.name ELSE excluded.name END,
		    notes = COALESCE(NULLIF(excluded.notes, ''), tracked_funds.notes),
		    active = 1`,
		cik, name, nullStr(strings.TrimSpace(notes)))
	return cik, err
}

// RemoveTrackedFund soft-deactivates a fund (keeps its history).
func (s *Service) RemoveTrackedFund(ctx context.Context, rawCIK string) error {
	cik, err := normaliseCIK(rawCIK)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE tracked_funds SET active = 0 WHERE cik = ?`, cik)
	return err
}

// ---- EDGAR pull ----------------------------------------------------------

// RefreshFund13F pulls the latest 13F-HR filing(s) for one fund, snapshots the
// book into fund_13f_holdings, then computes the diff. Returns
// (periodsIngested, diffsComputed). Network errors propagate so the caller can
// surface them; per-row issues are logged and skipped.
func (s *Service) RefreshFund13F(ctx context.Context, rawCIK string) (periods, diffs int, retErr error) {
	cik, err := normaliseCIK(rawCIK)
	if err != nil {
		return 0, 0, err
	}
	client := &secClient{HTTP: &http.Client{Timeout: 40 * time.Second}}

	// 1) submissions doc → recent 13F-HR filings.
	subURL := fmt.Sprintf("https://data.sec.gov/submissions/CIK%s.json", cik)
	body, err := client.do(ctx, subURL)
	if err != nil {
		return 0, 0, fmt.Errorf("submissions %s: %w", cik, err)
	}
	var sub submissionsDoc
	if err := json.Unmarshal(body, &sub); err != nil {
		return 0, 0, fmt.Errorf("parse submissions: %w", err)
	}
	filings := pick13FFilings(&sub, thirteenFMaxFilingsPerPull)
	if len(filings) == 0 {
		return 0, 0, fmt.Errorf("no 13F-HR filings found for CIK %s", cik)
	}

	// Backfill the fund's real name from EDGAR if we only had a placeholder.
	if strings.TrimSpace(sub.Name) != "" {
		_, _ = s.DB.ExecContext(ctx,
			`UPDATE tracked_funds SET name = ? WHERE cik = ? AND name LIKE 'CIK %'`,
			strings.TrimSpace(sub.Name), cik)
	}

	cikNoZeros := strings.TrimLeft(cik, "0")
	var latestPeriod string
	for _, fl := range filings {
		n, err := s.ingestOneFiling(ctx, client, cik, cikNoZeros, fl)
		if err != nil {
			slog.Warn("signals: 13F filing ingest", "cik", cik, "accession", fl.Accession, "err", err)
			continue
		}
		if n > 0 {
			periods++
		}
		if fl.ReportDate > latestPeriod {
			latestPeriod = fl.ReportDate
		}
	}

	if latestPeriod != "" {
		_, _ = s.DB.ExecContext(ctx,
			`UPDATE tracked_funds SET last_period = ?, last_pulled_at = ? WHERE cik = ?`,
			latestPeriod, time.Now().UTC().Format(time.RFC3339), cik)
	}

	// 2) diff the two most recent periods.
	diffs, err = s.DiffFund13F(ctx, cik)
	if err != nil {
		slog.Warn("signals: 13F diff", "cik", cik, "err", err)
	}
	slog.Info("signals: 13F refresh complete", "cik", cik, "periods", periods, "diffs", diffs)
	return periods, diffs, nil
}

// RefreshAllFunds refreshes every active tracked fund. Errors per fund are
// logged, never aborting the batch. Called by the quarterly cron + manual
// refresh endpoint.
func (s *Service) RefreshAllFunds(ctx context.Context) (funds, diffs int, retErr error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT cik FROM tracked_funds WHERE active = 1`)
	if err != nil {
		return 0, 0, err
	}
	var ciks []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err == nil {
			ciks = append(ciks, c)
		}
	}
	rows.Close()
	for _, c := range ciks {
		_, d, err := s.RefreshFund13F(ctx, c)
		if err != nil {
			slog.Warn("signals: refresh fund", "cik", c, "err", err)
			continue
		}
		funds++
		diffs += d
	}
	return funds, diffs, nil
}

// ingestOneFiling fetches + parses one 13F-HR info table and upserts its rows
// into fund_13f_holdings. Returns the number of holding rows written.
func (s *Service) ingestOneFiling(ctx context.Context, client *secClient, cik, cikNoZeros string, fl thirteenFiling) (int, error) {
	noDash := strings.ReplaceAll(fl.Accession, "-", "")
	indexURL := fmt.Sprintf("https://www.sec.gov/Archives/edgar/data/%s/%s/index.json", cikNoZeros, noDash)
	idxBody, err := client.do(ctx, indexURL)
	if err != nil {
		return 0, fmt.Errorf("index.json: %w", err)
	}
	var idx secIndex
	if err := json.Unmarshal(idxBody, &idx); err != nil {
		return 0, fmt.Errorf("parse index.json: %w", err)
	}
	xmlName := pick13FInfoTableName(&idx)
	if xmlName == "" {
		return 0, fmt.Errorf("no info-table xml in %s", fl.Accession)
	}
	xmlURL := fmt.Sprintf("https://www.sec.gov/Archives/edgar/data/%s/%s/%s", cikNoZeros, noDash, xmlName)
	xmlBody, err := client.do(ctx, xmlURL)
	if err != nil {
		return 0, fmt.Errorf("fetch %s: %w", xmlName, err)
	}
	var tbl informationTable
	if err := decodeXML(xmlBody, &tbl); err != nil {
		return 0, fmt.Errorf("decode info table: %w", err)
	}
	if len(tbl.InfoTables) == 0 {
		return 0, fmt.Errorf("info table empty for %s", fl.Accession)
	}

	period := normaliseDate(fl.ReportDate)
	filedAt := normaliseDate(fl.FilingDate)
	written := 0
	for _, it := range tbl.InfoTables {
		cusip := strings.ToUpper(strings.TrimSpace(it.Cusip))
		if cusip == "" {
			continue
		}
		value := parseEDGARFloat(it.Value)
		shares := parseEDGARFloat(it.ShrsOrPrn.SshPrnamt)
		putCall := normalisePutCall(it.PutCall)
		ticker := s.lookupTicker(ctx, cusip)
		var tickerVal any
		if ticker != "" {
			tickerVal = ticker
		}
		_, err := s.DB.ExecContext(ctx, `
			INSERT INTO fund_13f_holdings
			  (cik, period_of_report, cusip, ticker, issuer_name, value_usd, shares, put_call, accession, filed_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(cik, period_of_report, cusip, put_call) DO UPDATE SET
			    ticker = excluded.ticker,
			    issuer_name = excluded.issuer_name,
			    value_usd = excluded.value_usd,
			    shares = excluded.shares,
			    accession = excluded.accession,
			    filed_at = excluded.filed_at`,
			cik, period, cusip, tickerVal, nullStr(strings.TrimSpace(it.NameOfIssuer)),
			value, shares, nullStr(putCall), fl.Accession, filedAt)
		if err != nil {
			slog.Warn("signals: 13F holding upsert", "cik", cik, "cusip", cusip, "err", err)
			continue
		}
		written++
	}
	return written, nil
}

// lookupTicker resolves a CUSIP via cusip_ticker_map. Empty string = unmapped
// (enrich-and-flag — never guessed).
func (s *Service) lookupTicker(ctx context.Context, cusip string) string {
	var t string
	err := s.DB.QueryRowContext(ctx,
		`SELECT ticker FROM cusip_ticker_map WHERE cusip = ? LIMIT 1`, cusip).Scan(&t)
	if err != nil {
		return ""
	}
	return strings.ToUpper(strings.TrimSpace(t))
}

// ---- Diff ----------------------------------------------------------------

type holdingKey struct {
	cusip   string
	putCall string // "" | "call" | "put"
}

type holdingSnap struct {
	ticker    string
	issuer    string
	value     float64
	shares    float64
	putCall   string
}

// DiffFund13F compares the two most recent periods for a fund, writes
// fund_13f_diffs, and emits FLAG/ALARM rows into signal_events. Idempotent.
// Returns the number of diff rows written.
func (s *Service) DiffFund13F(ctx context.Context, rawCIK string) (int, error) {
	cik, err := normaliseCIK(rawCIK)
	if err != nil {
		return 0, err
	}
	periods, err := s.recentPeriods(ctx, cik, 2)
	if err != nil {
		return 0, err
	}
	if len(periods) < 2 {
		return 0, nil // need two periods to diff
	}
	newPeriod, priorPeriod := periods[0], periods[1]

	newBook, err := s.loadBook(ctx, cik, newPeriod)
	if err != nil {
		return 0, err
	}
	priorBook, err := s.loadBook(ctx, cik, priorPeriod)
	if err != nil {
		return 0, err
	}

	// Clear any prior diff rows for this period (recompute cleanly).
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM fund_13f_diffs WHERE cik = ? AND period = ?`, cik, newPeriod)

	fundName := s.fundName(ctx, cik)
	written := 0

	// new / increase / decrease (iterate the new book).
	for k, nv := range newBook {
		pv, existed := priorBook[k]
		var changeType string
		var priorValue, priorShares *float64
		if !existed {
			changeType = "new"
		} else {
			priorValue = &pv.value
			priorShares = &pv.shares
			switch {
			case nv.value > pv.value*1.0001:
				changeType = "increase"
			case nv.value < pv.value*0.9999:
				changeType = "decrease"
			default:
				continue // unchanged — no diff row
			}
		}
		if s.writeDiff(ctx, cik, newPeriod, priorPeriod, k, nv, priorValue, priorShares, changeType, fundName) {
			written++
		}
	}
	// exits (in prior, gone from new).
	for k, pv := range priorBook {
		if _, ok := newBook[k]; ok {
			continue
		}
		pvVal := pv.value
		pvShares := pv.shares
		exitSnap := holdingSnap{ticker: pv.ticker, issuer: pv.issuer, value: 0, shares: 0, putCall: pv.putCall}
		if s.writeDiff(ctx, cik, newPeriod, priorPeriod, k, exitSnap, &pvVal, &pvShares, "exit", fundName) {
			written++
		}
	}
	return written, nil
}

// writeDiff classifies one position change (tier + overlap), writes the
// fund_13f_diffs row, and emits a signal_events row for FLAG/ALARM. Returns
// true if a diff row was written.
func (s *Service) writeDiff(ctx context.Context, cik, period, priorPeriod string, k holdingKey, snap holdingSnap, priorValue, priorShares *float64, changeType, fundName string) bool {
	ticker := strings.ToUpper(strings.TrimSpace(snap.ticker))
	reasons := []string{}
	tier := TierInfo
	overlaps := false
	overlapSource := ""
	var sectorID *int64

	if ticker == "" {
		// Unmapped CUSIP — keep, flag, can't evaluate overlap (S-23b).
		reasons = append(reasons, "cusip_unmapped")
	} else {
		hit := s.InUniverse(ctx, ticker)
		if hit.Matched {
			overlaps = true
			overlapSource = hit.Source
			sectorID = hit.SectorUniverseID
			if hit.Source == "holding" {
				reasons = append(reasons, "overlap_held_name")
				// Contrary-to-held or notable new position in a held name → ALARM (§F / AC4).
				switch {
				case k.putCall == "put":
					tier = TierAlarm
					reasons = append(reasons, "contrary_put_on_held")
				case changeType == "exit":
					tier = TierAlarm
					reasons = append(reasons, "contrary_exit_of_held")
				case changeType == "new":
					tier = TierAlarm
					reasons = append(reasons, "new_position_in_held")
				default:
					tier = TierFlag
				}
			} else {
				tier = TierFlag
				reasons = append(reasons, "overlap_universe")
			}
		}
	}

	putCallVal := nullStr(k.putCall)
	newValPtr := &snap.value
	if changeType == "exit" {
		newValPtr = nil // exited — no current value
	}
	newSharesPtr := &snap.shares
	if changeType == "exit" {
		newSharesPtr = nil
	}

	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO fund_13f_diffs
		  (cik, period, prior_period, cusip, ticker, issuer_name, put_call,
		   change_type, prior_value, new_value, prior_shares, new_shares,
		   overlaps_universe, overlap_source, tier, reasons)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(cik, period, cusip, put_call, change_type) DO UPDATE SET
		    ticker = excluded.ticker, issuer_name = excluded.issuer_name,
		    prior_value = excluded.prior_value, new_value = excluded.new_value,
		    prior_shares = excluded.prior_shares, new_shares = excluded.new_shares,
		    overlaps_universe = excluded.overlaps_universe, overlap_source = excluded.overlap_source,
		    tier = excluded.tier, reasons = excluded.reasons, computed_at = CURRENT_TIMESTAMP`,
		cik, period, priorPeriod, k.cusip, nullStr(ticker), nullStr(snap.issuer), putCallVal,
		changeType, nullFloatPtr(priorValue), nullFloatPtr(newValPtr),
		nullFloatPtr(priorShares), nullFloatPtr(newSharesPtr),
		boolToInt(overlaps), nullStr(overlapSource), tier, reasonsToJSON(reasons))
	if err != nil {
		slog.Warn("signals: write 13F diff", "cik", cik, "cusip", k.cusip, "err", err)
		return false
	}

	// Emit FLAG/ALARM diffs into the unified feed + Telegram (AC7). INFO-tier
	// changes stay in fund_13f_diffs only (the dedicated sub-section shows the
	// full book diff) so the unified feed isn't flooded with every reshuffle.
	if tier == TierFlag || tier == TierAlarm {
		s.emit13FSignal(ctx, cik, period, k, snap, changeType, tier, reasons, sectorID, fundName)
	}
	return true
}

// emit13FSignal writes one signal_events row for a tiered 13F diff.
func (s *Service) emit13FSignal(ctx context.Context, cik, period string, k holdingKey, snap holdingSnap, changeType string, tier string, reasons []string, sectorID *int64, fundName string) {
	action := ActionBuy
	if changeType == "exit" || changeType == "decrease" {
		action = ActionSell
	}
	ticker := strings.ToUpper(strings.TrimSpace(snap.ticker))
	var tickerVal any
	if ticker != "" {
		tickerVal = ticker
	}
	var amountVal any
	if changeType != "exit" && snap.value > 0 {
		amountVal = snap.value
	}
	// Human summary for the feed.
	parts := []string{fundName, changeType}
	if k.putCall != "" {
		parts = append(parts, k.putCall+" option")
	}
	if snap.value > 0 {
		parts = append(parts, "$"+formatUSDShort(snap.value))
	}
	notes := strings.Join(parts, " · ") + " — " + Caveat13F

	source := "13F " + period
	sourceID := fmt.Sprintf("%s|%s|%s|%s|%s", cik, period, k.cusip, k.putCall, changeType)

	_, err := s.DB.ExecContext(ctx, `
		INSERT OR REPLACE INTO signal_events
		  (signal_type, tier, event_date, filed_date,
		   ticker, issuer_name, sector_universe_id,
		   actor_name, actor_role, action, amount_usd, amount_bucket,
		   source, source_url, source_id, alarm_reasons, notes)
		VALUES ('thirteenf', ?, ?, ?, ?, ?, ?, ?, '13F filer', ?, ?, NULL, ?, ?, ?, ?, ?)`,
		tier, period, period,
		tickerVal, nullStr(snap.issuer), nullInt64Ptr(sectorID),
		nullStr(fundName), action, amountVal,
		source, fmt.Sprintf("https://www.sec.gov/cgi-bin/browse-edgar?action=getcompany&CIK=%s&type=13F", cik),
		sourceID, reasonsToJSON(reasons), nullStr(notes))
	if err != nil {
		slog.Warn("signals: emit 13F signal", "cik", cik, "cusip", k.cusip, "err", err)
	}
}

// recentPeriods returns up to n distinct period_of_report values for a fund,
// newest first.
func (s *Service) recentPeriods(ctx context.Context, cik string, n int) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT DISTINCT period_of_report FROM fund_13f_holdings
		  WHERE cik = ? ORDER BY period_of_report DESC LIMIT ?`, cik, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// loadBook loads a period's positions keyed by (cusip, put_call).
func (s *Service) loadBook(ctx context.Context, cik, period string) (map[holdingKey]holdingSnap, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT cusip, ticker, issuer_name, value_usd, shares, put_call
		   FROM fund_13f_holdings WHERE cik = ? AND period_of_report = ?`, cik, period)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	book := map[holdingKey]holdingSnap{}
	for rows.Next() {
		var cusip string
		var ticker, issuer, putCall sql.NullString
		var value, shares sql.NullFloat64
		if err := rows.Scan(&cusip, &ticker, &issuer, &value, &shares, &putCall); err != nil {
			return nil, err
		}
		pc := ""
		if putCall.Valid {
			pc = putCall.String
		}
		book[holdingKey{cusip: cusip, putCall: pc}] = holdingSnap{
			ticker:  ticker.String,
			issuer:  issuer.String,
			value:   value.Float64,
			shares:  shares.Float64,
			putCall: pc,
		}
	}
	return book, rows.Err()
}

func (s *Service) fundName(ctx context.Context, cik string) string {
	var n string
	_ = s.DB.QueryRowContext(ctx, `SELECT name FROM tracked_funds WHERE cik = ?`, cik).Scan(&n)
	if strings.TrimSpace(n) == "" {
		return "CIK " + cik
	}
	return n
}

// Fund13FDiffRow is the API shape for the per-fund diff detail list.
type Fund13FDiffRow struct {
	Cusip       string   `json:"cusip"`
	Ticker      *string  `json:"ticker,omitempty"`
	IssuerName  *string  `json:"issuerName,omitempty"`
	PutCall     *string  `json:"putCall,omitempty"`
	ChangeType  string   `json:"changeType"`
	PriorValue  *float64 `json:"priorValue,omitempty"`
	NewValue    *float64 `json:"newValue,omitempty"`
	Overlaps    bool     `json:"overlapsUniverse"`
	OverlapSrc  *string  `json:"overlapSource,omitempty"`
	Tier        string   `json:"tier"`
	Reasons     []string `json:"reasons,omitempty"`
}

// ListFund13FDiffs returns the diff rows for a fund's latest period, tier-first.
func (s *Service) ListFund13FDiffs(ctx context.Context, rawCIK string) ([]Fund13FDiffRow, string, error) {
	cik, err := normaliseCIK(rawCIK)
	if err != nil {
		return nil, "", err
	}
	var period string
	_ = s.DB.QueryRowContext(ctx, `SELECT last_period FROM tracked_funds WHERE cik = ?`, cik).Scan(&period)
	if period == "" {
		return []Fund13FDiffRow{}, "", nil
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT cusip, ticker, issuer_name, put_call, change_type,
		       prior_value, new_value, overlaps_universe, overlap_source, tier, reasons
		  FROM fund_13f_diffs
		 WHERE cik = ? AND period = ?
		 ORDER BY CASE tier WHEN 'alarm' THEN 0 WHEN 'flag' THEN 1 ELSE 2 END,
		          overlaps_universe DESC, new_value DESC`, cik, period)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	out := []Fund13FDiffRow{}
	for rows.Next() {
		var d Fund13FDiffRow
		var ticker, issuer, putCall, overlapSrc, reasons sql.NullString
		var priorVal, newVal sql.NullFloat64
		var overlaps int
		if err := rows.Scan(&d.Cusip, &ticker, &issuer, &putCall, &d.ChangeType,
			&priorVal, &newVal, &overlaps, &overlapSrc, &d.Tier, &reasons); err != nil {
			return nil, "", err
		}
		if ticker.Valid && ticker.String != "" {
			d.Ticker = &ticker.String
		}
		if issuer.Valid && issuer.String != "" {
			d.IssuerName = &issuer.String
		}
		if putCall.Valid && putCall.String != "" {
			d.PutCall = &putCall.String
		}
		if priorVal.Valid {
			d.PriorValue = &priorVal.Float64
		}
		if newVal.Valid {
			d.NewValue = &newVal.Float64
		}
		if overlapSrc.Valid && overlapSrc.String != "" {
			d.OverlapSrc = &overlapSrc.String
		}
		d.Overlaps = overlaps == 1
		d.Reasons = parseReasonsJSON(reasons.String)
		out = append(out, d)
	}
	return out, period, rows.Err()
}

// ---- small helpers -------------------------------------------------------

// pick13FFilings returns up to max recent 13F-HR filings, newest reportDate
// first.
func pick13FFilings(sub *submissionsDoc, max int) []thirteenFiling {
	r := sub.Filings.Recent
	var out []thirteenFiling
	for i := 0; i < len(r.Form); i++ {
		if r.Form[i] != "13F-HR" {
			continue
		}
		fl := thirteenFiling{}
		if i < len(r.AccessionNumber) {
			fl.Accession = r.AccessionNumber[i]
		}
		if i < len(r.FilingDate) {
			fl.FilingDate = r.FilingDate[i]
		}
		if i < len(r.ReportDate) {
			fl.ReportDate = r.ReportDate[i]
		}
		if fl.Accession == "" || fl.ReportDate == "" {
			continue
		}
		out = append(out, fl)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ReportDate > out[b].ReportDate })
	if len(out) > max {
		out = out[:max]
	}
	return out
}

// pick13FInfoTableName finds the information-table XML in a filing index.
// Preference: a name containing "infotable"/"form13f"/"information"/"table",
// else any .xml that isn't primary_doc.xml or an xsl stylesheet.
func pick13FInfoTableName(idx *secIndex) string {
	var fallback string
	for _, it := range idx.Directory.Items {
		name := strings.ToLower(it.Name)
		if !strings.HasSuffix(name, ".xml") {
			continue
		}
		if name == "primary_doc.xml" || strings.HasPrefix(name, "primary_doc") {
			continue
		}
		if strings.Contains(name, "xsl") {
			continue
		}
		if strings.Contains(name, "infotable") || strings.Contains(name, "form13f") ||
			strings.Contains(name, "information") || strings.Contains(name, "table") {
			return it.Name
		}
		if fallback == "" {
			fallback = it.Name
		}
	}
	return fallback
}

func normalisePutCall(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "put":
		return "put"
	case "call":
		return "call"
	default:
		return ""
	}
}

// parseEDGARFloat parses a 13F numeric value, tolerating commas.
func parseEDGARFloat(s string) float64 {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", "")
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}

func nullFloatPtr(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// formatUSDShort renders a dollar value as e.g. 1.2B / 340.0M / 12.3K.
func formatUSDShort(v float64) string {
	switch {
	case v >= 1e9:
		return fmt.Sprintf("%.1fB", v/1e9)
	case v >= 1e6:
		return fmt.Sprintf("%.1fM", v/1e6)
	case v >= 1e3:
		return fmt.Sprintf("%.1fK", v/1e3)
	default:
		return fmt.Sprintf("%.0f", v)
	}
}

// parseReasonsJSON decodes the stored alarm_reasons JSON array; tolerant of
// empty/null/malformed (returns nil).
func parseReasonsJSON(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" || s == "null" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}
