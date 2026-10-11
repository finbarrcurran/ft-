// Package bargo ingests U.S. Congress stock-trade disclosures from the free
// Bargo Congress API (https://www.bargo.ai/free-apis/congress) into the
// congress_trades_raw history table (SC-47).
//
// Free tier: last ~3 months only, newest transaction first. Keyless 30
// requests + 100 rows/day; a free key lifts that to 100 requests + 1,000
// rows/day and 250 rows/page. Terms: visible attribution to Bargo, no bulk
// redistribution of the raw data.
//
// Disclosures lag the trade by up to ~45 days, so each run re-reads a rolling
// window of recent transaction dates and upserts; the raw table keeps what
// has already aged out of Bargo's window.
package bargo

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"ft/internal/health"
)

const (
	DefaultBaseURL = "https://www.bargo.ai/free-apis/congress/v1"
	// ProviderName is the provider_health / monitor identifier.
	ProviderName = "bargo"

	sliceDays    = 7  // transaction-date window fetched per query
	steadyDays   = 14 // steady-state re-read window (late filings)
	deepDays     = 60 // Sunday deep pass window
	maxWindowAge = 92 // Bargo only serves ~3 months
	metaCursor   = "bargo_cursor"
)

// Errors surfaced by the client.
var (
	ErrQuota = errors.New("bargo: daily quota exhausted (HTTP 429)")
	ErrAuth  = errors.New("bargo: key rejected (HTTP 401/403)")
)

// Trade is one record of GET /trades.
type Trade struct {
	Member          string   `json:"member"`
	MemberSlug      string   `json:"member_slug"`
	Chamber         string   `json:"chamber"`
	State           string   `json:"state"`
	Ticker          string   `json:"ticker"`
	Asset           string   `json:"asset"`
	Type            string   `json:"type"`
	AmountLow       *float64 `json:"amount_low"`
	AmountHigh      *float64 `json:"amount_high"`
	AmountRange     string   `json:"amount_range"`
	TransactionDate string   `json:"transaction_date"`
	DisclosureDate  string   `json:"disclosure_date"`
	EstPrice        *float64 `json:"est_price"`
	RecentPrice     *float64 `json:"recent_price"`
	FilingPortal    string   `json:"filing_portal"`
}

type tradePage struct {
	Trades []Trade `json:"trades"`
	Page   int     `json:"page"`
	Limit  int     `json:"limit"`
	Count  int     `json:"count"`
}

// Client talks to the Bargo REST API.
type Client struct {
	BaseURL string
	Key     string // optional; sent as a Bearer header, never in the URL
	HTTP    *http.Client
}

// NewClient builds a client from FT_BARGO_API_KEY (optional).
func NewClient() *Client {
	return &Client{
		BaseURL: DefaultBaseURL,
		Key:     strings.TrimSpace(os.Getenv("FT_BARGO_API_KEY")),
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// PageLimit is the largest rows-per-page the current tier allows.
func (c *Client) PageLimit() int {
	if c.Key != "" {
		return 250
	}
	return 100
}

// Trades fetches one page of trades with transaction_date in [from, to].
func (c *Client) Trades(ctx context.Context, from, to string, page int) ([]Trade, error) {
	q := url.Values{}
	q.Set("from", from)
	q.Set("to", to)
	q.Set("limit", strconv.Itoa(c.PageLimit()))
	q.Set("page", strconv.Itoa(page))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/trades?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if c.Key != "" {
		req.Header.Set("Authorization", "Bearer "+c.Key)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, ErrQuota
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, ErrAuth
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("bargo: http %d: %.120s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var tp tradePage
	if err := json.Unmarshal(body, &tp); err != nil {
		return nil, fmt.Errorf("bargo: decode: %w", err)
	}
	return tp.Trades, nil
}

// SourceKey is the stable identity of a trade. Bargo exposes no record id, so
// it is a hash of the fields that identify a disclosed line item. Two genuinely
// identical lines (same member, asset, dates, type and amount band) collapse
// into one; that is rare and judged acceptable.
func SourceKey(t Trade) string {
	h := sha1.Sum([]byte(strings.Join([]string{
		t.MemberSlug, t.Chamber, strings.ToUpper(t.Ticker), t.Type,
		t.TransactionDate, t.DisclosureDate, t.AmountRange,
	}, "|")))
	return "bargo:" + hex.EncodeToString(h[:10])
}

// Result summarises one Sync run.
type Result struct {
	Requests int
	Fetched  int
	New      int
	Partial  bool // stopped early on the daily quota
}

// Sync pulls the rolling window and upserts into congress_trades_raw. It
// records a bargo_sync_runs row and a provider_health outcome. A quota stop
// after some data was stored is "partial", not a failure.
func Sync(ctx context.Context, db *sql.DB, c *Client, now time.Time) (res Result, err error) {
	now = now.UTC()
	started := now.Unix()
	var runID int64
	if r, e := db.ExecContext(ctx,
		`INSERT INTO bargo_sync_runs (started_at, status) VALUES (?, 'running')`, started); e == nil {
		runID, _ = r.LastInsertId()
	}
	defer func() {
		res.New = countNew(context.Background(), db, started)
		status, msg := "ok", ""
		switch {
		case err != nil:
			status, msg = "failed", err.Error()
		case res.Partial:
			status, msg = "partial", ErrQuota.Error()
		}
		if runID != 0 {
			_, _ = db.ExecContext(context.Background(),
				`UPDATE bargo_sync_runs SET finished_at=?, status=?, requests=?, rows_fetched=?, rows_new=?, error=? WHERE id=?`,
				time.Now().UTC().Unix(), status, res.Requests, res.Fetched, res.New, nullIfEmpty(msg), runID)
		}
		// A quota stop with data stored still counts as a healthy connection.
		health.Record(context.Background(), ProviderName, err)
	}()

	today := now.Format("2006-01-02")
	from := windowStart(ctx, db, now)
	okCalls := 0 // successful requests so far; a quota stop after any is "partial"
	for _, w := range slices(from, today) {
		page := 0
		for {
			rows, e := c.Trades(ctx, w[0], w[1], page)
			res.Requests++
			if e != nil {
				if errors.Is(e, ErrQuota) && okCalls > 0 {
					res.Partial = true
					setCursor(ctx, db, w[0])
					return res, nil
				}
				return res, e
			}
			okCalls++
			if e := upsert(ctx, db, rows, now); e != nil {
				return res, e
			}
			res.Fetched += len(rows)
			if len(rows) < c.PageLimit() {
				break
			}
			page++
		}
	}
	// Whole window done: next run starts steadyDays back (or deepDays on Sundays).
	setCursor(ctx, db, "")
	return res, nil
}

// windowStart decides the first transaction date to query.
func windowStart(ctx context.Context, db *sql.DB, now time.Time) string {
	var cur string
	_ = db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, metaCursor).Scan(&cur)
	min := now.AddDate(0, 0, -maxWindowAge).Format("2006-01-02")
	if cur != "" {
		if cur < min {
			cur = min
		}
		return cur // resume an unfinished backfill
	}
	var have int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM congress_trades_raw WHERE source='bargo'`).Scan(&have)
	switch {
	case have == 0:
		return min // first run: full 3-month backfill
	case now.Weekday() == time.Sunday:
		return now.AddDate(0, 0, -deepDays).Format("2006-01-02")
	}
	return now.AddDate(0, 0, -steadyDays).Format("2006-01-02")
}

func setCursor(ctx context.Context, db *sql.DB, v string) {
	_, _ = db.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, metaCursor, v)
}

// slices splits [from, to] (YYYY-MM-DD) into sliceDays windows, oldest first.
func slices(from, to string) [][2]string {
	f, err1 := time.Parse("2006-01-02", from)
	t, err2 := time.Parse("2006-01-02", to)
	if err1 != nil || err2 != nil || f.After(t) {
		return nil
	}
	var out [][2]string
	for d := f; !d.After(t); d = d.AddDate(0, 0, sliceDays) {
		e := d.AddDate(0, 0, sliceDays-1)
		if e.After(t) {
			e = t
		}
		out = append(out, [2]string{d.Format("2006-01-02"), e.Format("2006-01-02")})
	}
	return out
}

func upsert(ctx context.Context, db *sql.DB, rows []Trade, now time.Time) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ts := now.Unix()
	for _, t := range rows {
		// History is unfiltered: untickered lines are kept too.
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO congress_trades_raw
			  (source, source_key, member, member_slug, chamber, state, ticker, asset,
			   trade_type, amount_low, amount_high, amount_range, transaction_date,
			   disclosure_date, est_price, recent_price, filing_url, first_seen_at, last_seen_at)
			VALUES ('bargo', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(source_key) DO UPDATE SET
			  last_seen_at = excluded.last_seen_at,
			  est_price = excluded.est_price,
			  recent_price = excluded.recent_price`,
			SourceKey(t), t.Member, t.MemberSlug, t.Chamber, t.State,
			strings.ToUpper(strings.TrimSpace(t.Ticker)), t.Asset, t.Type,
			t.AmountLow, t.AmountHigh, t.AmountRange, t.TransactionDate, t.DisclosureDate,
			t.EstPrice, t.RecentPrice, t.FilingPortal, ts, ts); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// countNew counts rows first seen during the run that started at `started`.
func countNew(ctx context.Context, db *sql.DB, started int64) int {
	var n int
	_ = db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM congress_trades_raw WHERE source='bargo' AND first_seen_at >= ?`, started).Scan(&n)
	return n
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
