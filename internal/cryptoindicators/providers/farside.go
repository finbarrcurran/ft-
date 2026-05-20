package providers

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// FarsideClient scrapes Farside Investors for Bitcoin spot ETF net flows.
//
// Farside doesn't have an official API. They publish a CSV (or HTML table
// that mirrors CSV columns) at:
//
//   https://farside.co.uk/bitcoin-etf-flow-all-data/
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
func (c *FarsideClient) FetchBTCETFFlow7d(ctx context.Context) Reading {
	url := "https://farside.co.uk/bitcoin-etf-flow-all-data/"
	body, status, err := doWithRetry(ctx, c.HTTP, url)
	if err != nil {
		return Reading{Err: fmt.Sprintf("farside fetch (HTTP %d): %v", status, err)}
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
