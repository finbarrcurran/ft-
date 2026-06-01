package providers

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// FarsideJSONPath is where the Playwright-based daily scraper writes the
// pre-fetched ETF flow data. The scraper lives at
// /home/curran/scripts/farside-fetch.js and runs as a cron job under the
// curran user; FT reads the file at refresh time. This bypasses the
// Cloudflare bot block that defeats FT's native Go HTTP client.
const FarsideJSONPath = "/var/lib/ft/data/farside/etf-flow.json"

// FarsideJSONMaxAge is the freshness window — older than this, FT treats
// the cached file as stale and falls through to live HTTP scrape (which
// will likely 403 but at least surfaces the staleness as an error).
const FarsideJSONMaxAge = 36 * time.Hour

// farsideCachedFile mirrors the JSON shape produced by farside-fetch.js.
type farsideCachedFile struct {
	FetchedAt  time.Time `json:"fetchedAt"`
	Source     string    `json:"source"`
	Rolling7dM *float64  `json:"rolling7dM"`
	TotalRows  int       `json:"totalRows"`
	Rows       []struct {
		Date   string  `json:"date"`
		TotalM float64 `json:"totalM"`
	} `json:"rows"`
}

// FarsideClient scrapes Farside Investors for Bitcoin spot ETF net flows.
//
// Farside doesn't have an official API. They publish a CSV (or HTML table
// that mirrors CSV columns) at:
//
//	https://farside.co.uk/bitcoin-etf-flow-all-data/
//
// Format per row (as of 2024-2026): Date, IBIT, FBTC, BITB, ARKB, BTCO,
// EZBC, BRRR, HODL, BTCW, GBTC, BTC, Total
//
// "Total" is the all-ETF aggregate net flow for that day in USD millions.
// Negative = net outflows.
//
// Our indicator wants 7-day rolling sum of the Total column.
//
// FRAGILITY NOTE: Spec 9e §"Risk and rollback notes" flags this provider
// as the most likely to break. If Farside changes column order, page
// structure, or removes the all-data CSV, the parse silently degrades
// and the indicator shows a stale/error pill. We defend by:
//
//   - Header sniffing (find the "Total" column by name, not position)
//   - Numeric tolerance (strip $, commas, parentheses)
//   - Permissive date parsing (multiple formats)
//   - Returning a clear error on shape mismatch rather than crashing
type FarsideClient struct {
	HTTP *http.Client
}

func NewFarsideClient() *FarsideClient {
	return &FarsideClient{HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// FetchBTCETFFlow7d returns the trailing 7-day rolling sum of all BTC
// spot ETF net flows in USD millions. Positive = net buying.
//
// Strategy (v1.13 — 2026-05-28):
//
//  1. First, try the local JSON file at FarsideJSONPath. This is written
//     daily by the Playwright-based scraper (farside-fetch.js running
//     as a curran cron job). The Playwright route is the only thing
//     that gets past Cloudflare's bot management at farside.co.uk.
//
//  2. If the file is missing OR older than FarsideJSONMaxAge, fall
//     through to the legacy direct HTTP scrape. This is expected to
//     return 403 on most setups but is preserved as a fallback in case
//     Cloudflare ever relaxes, or for environments without the scraper.
func (c *FarsideClient) FetchBTCETFFlow7d(ctx context.Context) Reading {
	if r, ok := readFarsideFromFile(); ok {
		return r
	}
	url := "https://farside.co.uk/bitcoin-etf-flow-all-data/"
	body, status, err := doWithRetry(ctx, c.HTTP, url, "")
	if err != nil {
		return Reading{Err: fmt.Sprintf("farside fetch (HTTP %d): %v — and no fresh cache at %s", status, err, FarsideJSONPath)}
	}
	// The page sometimes returns HTML (with a CSV-shaped table) or
	// straight CSV depending on what they serve that day. Try CSV first;
	// fall back to extracting the table from HTML if needed.
	rows, parseErr := parseFarsideAsCSV(body)
	if parseErr != nil || len(rows) == 0 {
		rows, parseErr = parseFarsideAsHTML(body)
		if parseErr != nil {
			return Reading{Err: fmt.Sprintf("farside parse: %v", parseErr)}
		}
	}
	if len(rows) == 0 {
		return Reading{Err: "farside: zero data rows after parse"}
	}

	// Sort by date desc, then sum the most recent 7.
	sort.Slice(rows, func(i, j int) bool { return rows[i].Date.After(rows[j].Date) })
	var sum float64
	count := 0
	for _, r := range rows {
		if count >= 7 {
			break
		}
		sum += r.Total
		count++
	}
	if count < 7 {
		return Reading{Err: fmt.Sprintf("farside: only %d days available, need 7", count)}
	}
	return Reading{Value: &sum}
}

type farsideRow struct {
	Date  time.Time
	Total float64
}

// readFarsideFromFile loads the daily Playwright-scraped JSON cache and
// returns the trailing 7d rolling sum if fresh. Returns (zero, false)
// if the file is missing, unparseable, stale, or doesn't contain the
// rolling sum. Caller falls through to direct HTTP scrape on false.
func readFarsideFromFile() (Reading, bool) {
	stat, err := os.Stat(FarsideJSONPath)
	if err != nil {
		return Reading{}, false
	}
	age := time.Since(stat.ModTime())
	if age > FarsideJSONMaxAge {
		return Reading{}, false
	}
	raw, err := os.ReadFile(FarsideJSONPath)
	if err != nil {
		return Reading{}, false
	}
	var f farsideCachedFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return Reading{}, false
	}
	if f.Rolling7dM != nil {
		v := *f.Rolling7dM
		return Reading{Value: &v}, true
	}
	// Fall back to recomputing from the rows array if rolling7dM was
	// missing (defensive — scraper always writes it currently).
	if len(f.Rows) < 7 {
		return Reading{}, false
	}
	sum := 0.0
	for i := 0; i < 7; i++ {
		sum += f.Rows[i].TotalM
	}
	return Reading{Value: &sum}, true
}

// parseFarsideAsCSV tries to read the body as CSV. The first cell of the
// header row should be "Date"; if not, returns an error so the caller
// can try the HTML path.
func parseFarsideAsCSV(body []byte) ([]farsideRow, error) {
	r := csv.NewReader(strings.NewReader(string(body)))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("too few records")
	}
	header := records[0]
	dateIdx := -1
	totalIdx := -1
	for i, h := range header {
		hh := strings.ToLower(strings.TrimSpace(h))
		if hh == "date" {
			dateIdx = i
		}
		if hh == "total" {
			totalIdx = i
		}
	}
	if dateIdx < 0 || totalIdx < 0 {
		return nil, fmt.Errorf("header missing Date or Total column")
	}
	out := make([]farsideRow, 0, len(records))
	for _, rec := range records[1:] {
		if dateIdx >= len(rec) || totalIdx >= len(rec) {
			continue
		}
		d, ok := parseFarsideDate(rec[dateIdx])
		if !ok {
			continue
		}
		v, ok := parseFarsideNumber(rec[totalIdx])
		if !ok {
			continue
		}
		out = append(out, farsideRow{Date: d, Total: v})
	}
	return out, nil
}

// parseFarsideAsHTML extracts rows from a vanilla HTML table. Defensive
// regex/text-scraping rather than a full HTML parser — keeps the dep
// surface tiny. If their HTML changes we degrade gracefully.
func parseFarsideAsHTML(body []byte) ([]farsideRow, error) {
	// Look for <td>...</td> sequences, walk them in groups assuming a
	// row pattern. The "Total" cell is the last numeric column on each row.
	text := string(body)
	// Quick reject if no <td>.
	if !strings.Contains(text, "<td") {
		return nil, fmt.Errorf("no <td> tags in body")
	}
	// Split by <tr>. Each <tr> is a row.
	rows := strings.Split(text, "<tr")
	out := []farsideRow{}
	for _, row := range rows {
		// Extract cell texts.
		cells := extractTDCells(row)
		if len(cells) < 3 {
			continue // header or summary row
		}
		// Date is in the first cell.
		d, ok := parseFarsideDate(cells[0])
		if !ok {
			continue
		}
		// Total is the LAST cell on each row (Farside convention).
		v, ok := parseFarsideNumber(cells[len(cells)-1])
		if !ok {
			continue
		}
		out = append(out, farsideRow{Date: d, Total: v})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("HTML parsed but no rows extracted")
	}
	return out, nil
}

// extractTDCells pulls the text content of every <td> in a chunk.
func extractTDCells(chunk string) []string {
	var out []string
	for {
		i := strings.Index(chunk, "<td")
		if i < 0 {
			break
		}
		// Find end of opening tag.
		j := strings.Index(chunk[i:], ">")
		if j < 0 {
			break
		}
		chunk = chunk[i+j+1:]
		// Find closing </td>.
		k := strings.Index(chunk, "</td>")
		if k < 0 {
			break
		}
		out = append(out, stripTags(chunk[:k]))
		chunk = chunk[k+5:]
	}
	return out
}

// stripTags removes any HTML tags inside a string + trims.
func stripTags(s string) string {
	for {
		i := strings.Index(s, "<")
		if i < 0 {
			break
		}
		j := strings.Index(s[i:], ">")
		if j < 0 {
			break
		}
		s = s[:i] + s[i+j+1:]
	}
	return strings.TrimSpace(s)
}

func parseFarsideDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	formats := []string{
		"02 Jan 2006",
		"2 Jan 2006",
		"2006-01-02",
		"01/02/2006",
		"02/01/2006",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// parseFarsideNumber handles "$1,234.56", "(123.4)" (= negative),
// "1,234", "-456.78", and plain "1234". Returns false on "-" placeholder.
func parseFarsideNumber(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" || s == "-" || s == "—" || strings.EqualFold(s, "n/a") {
		return 0, false
	}
	neg := false
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		neg = true
		s = s[1 : len(s)-1]
	}
	if strings.HasPrefix(s, "-") {
		neg = true
		s = s[1:]
	}
	// Strip currency symbols and commas.
	s = strings.ReplaceAll(s, "$", "")
	s = strings.ReplaceAll(s, ",", "")
	s = strings.TrimSpace(s)
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	if neg {
		v = -v
	}
	return v, true
}

// Ensure unused import io stays referenced if we extend.
var _ = io.EOF
