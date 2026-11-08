package market

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// HTTPError is a non-200 answer from Yahoo. Its message is the one the older code produced
// ("http 404: …"), so existing log lines do not change.
type HTTPError struct {
	Status int
	Msg    string
}

func (e *HTTPError) Error() string { return e.Msg }

// ErrNoCalendar means Yahoo answered 200 but with no calendarEvents result.
var ErrNoCalendar = errors.New("no calendarEvents")

// ErrEarningsPageLayout means the earnings page did not contain the expected table (Yahoo changed the
// page or served the generic calendar). It is a fetch failure, not "no date".
var ErrEarningsPageLayout = errors.New("earnings page: expected table with an 'Earnings Date' column not found")

// CalendarEvents is what quoteSummary?modules=calendarEvents says about one symbol (SC-56).
type CalendarEvents struct {
	Symbol        string
	EarningsDates []string // ISO UTC dates in Yahoo's order; empty when Yahoo has none
	IsEstimate    bool     // Yahoo's isEarningsDateEstimate
	ExDividend    string   // ISO, "" when absent
}

// ParseCalendarEvents decodes a quoteSummary calendarEvents response body.
func ParseCalendarEvents(symbol string, raw []byte) (*CalendarEvents, error) {
	var env struct {
		QuoteSummary struct {
			Result []struct {
				CalendarEvents struct {
					Earnings struct {
						EarningsDate []struct {
							Raw int64 `json:"raw"`
						} `json:"earningsDate"`
						IsEstimate bool `json:"isEarningsDateEstimate"`
					} `json:"earnings"`
					ExDividendDate struct {
						Raw int64 `json:"raw"`
					} `json:"exDividendDate"`
				} `json:"calendarEvents"`
			} `json:"result"`
		} `json:"quoteSummary"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("parse calendarEvents: %w", err)
	}
	if len(env.QuoteSummary.Result) == 0 {
		return nil, ErrNoCalendar
	}
	ce := env.QuoteSummary.Result[0].CalendarEvents
	out := &CalendarEvents{Symbol: symbol, IsEstimate: ce.Earnings.IsEstimate}
	for _, d := range ce.Earnings.EarningsDate {
		if d.Raw > 0 {
			out.EarningsDates = append(out.EarningsDates, time.Unix(d.Raw, 0).UTC().Format("2006-01-02"))
		}
	}
	if ce.ExDividendDate.Raw > 0 {
		out.ExDividend = time.Unix(ce.ExDividendDate.Raw, 0).UTC().Format("2006-01-02")
	}
	return out, nil
}

// FetchYahooCalendarEvents is the primary report-date source: quoteSummary?modules=calendarEvents.
// Errors are *HTTPError for a non-200 answer (404 = Yahoo does not know the symbol or has no
// fundamentals for it, as for ETFs), ErrNoCalendar, or a parse/network error.
func FetchYahooCalendarEvents(ctx context.Context, symbol string) (*CalendarEvents, error) {
	u := fmt.Sprintf("https://query1.finance.yahoo.com/v10/finance/quoteSummary/%s?modules=calendarEvents", url.PathEscape(symbol))
	raw, err := yahoo.yahooGet(ctx, u)
	if err != nil {
		return nil, err
	}
	return ParseCalendarEvents(symbol, raw)
}

// EarningsRow is one row of Yahoo's per-symbol earnings calendar page.
type EarningsRow struct {
	Date     string // ISO calendar date as written on the page (exchange-local)
	Reported bool   // the row has a Reported EPS
	Raw      string // the cell text, for logs
}

var (
	reRowDate = regexp.MustCompile(`^([A-Z][a-z]+ \d{1,2}, \d{4})`)
)

// ParseEarningsPage reads the table of https://finance.yahoo.com/calendar/earnings?symbol=… . A page
// without a table that has an "Earnings Date" column returns ErrEarningsPageLayout (Yahoo serves a
// generic day calendar to some clients; that must never be read as "no dates").
func ParseEarningsPage(r io.Reader) ([]EarningsRow, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("parse earnings page: %w", err)
	}
	var table *html.Node
	var find func(*html.Node)
	find = func(n *html.Node) {
		if table != nil {
			return
		}
		if n.Type == html.ElementNode && n.Data == "table" {
			table = n
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(doc)
	if table == nil {
		return nil, ErrEarningsPageLayout
	}
	text := func(n *html.Node) string {
		var b strings.Builder
		var walk func(*html.Node)
		walk = func(x *html.Node) {
			if x.Type == html.TextNode {
				b.WriteString(x.Data)
			}
			for c := x.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
		walk(n)
		return strings.Join(strings.Fields(b.String()), " ")
	}
	var rows [][]string
	var collect func(*html.Node)
	collect = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "tr" {
			var cells []string
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.ElementNode && (c.Data == "td" || c.Data == "th") {
					cells = append(cells, text(c))
				}
			}
			rows = append(rows, cells)
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			collect(c)
		}
	}
	collect(table)
	if len(rows) == 0 {
		return nil, ErrEarningsPageLayout
	}
	dateCol, repCol := -1, -1
	for i, h := range rows[0] {
		switch h {
		case "Earnings Date":
			dateCol = i
		case "Reported EPS":
			repCol = i
		}
	}
	if dateCol < 0 {
		return nil, ErrEarningsPageLayout
	}
	var out []EarningsRow
	for _, row := range rows[1:] {
		if dateCol >= len(row) {
			continue
		}
		m := reRowDate.FindStringSubmatch(row[dateCol])
		if m == nil {
			continue
		}
		d, err := time.Parse("January 2, 2006", m[1])
		if err != nil {
			continue
		}
		er := EarningsRow{Date: d.Format("2006-01-02"), Raw: row[dateCol]}
		if repCol >= 0 && repCol < len(row) {
			v := strings.TrimSpace(row[repCol])
			er.Reported = v != "" && v != "-" && v != "—"
		}
		out = append(out, er)
	}
	return out, nil
}

// FetchYahooEarningsPage is the second report-date source, used only to find the NEXT date when the
// primary one has already passed or is missing. It is an HTML scrape of Yahoo's per-symbol earnings
// calendar; the older JSON endpoints (visualization, v7 quote, quoteSummary earnings) were checked on
// 2026-11-06 and either stop updating or return the same stale date. Yahoo marks no estimate flag on this
// page, so callers treat its dates as estimates.
func FetchYahooEarningsPage(ctx context.Context, symbol string) ([]EarningsRow, error) {
	if _, err := yahoo.ensureCrumb(ctx); err != nil { // warms the cookie jar
		return nil, err
	}
	u := fmt.Sprintf("https://finance.yahoo.com/calendar/earnings?symbol=%s&offset=0&size=25", url.QueryEscape(symbol))
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
		req.Header.Set("Accept", "text/html,application/xhtml+xml")
		res, err := yahoo.client.Do(req)
		if err != nil {
			return nil, err
		}
		if res.StatusCode == 429 || res.StatusCode == 503 {
			res.Body.Close()
			lastErr = &HTTPError{Status: res.StatusCode, Msg: fmt.Sprintf("earnings page http %d (rate-limited)", res.StatusCode)}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Second):
			}
			continue
		}
		if res.StatusCode != 200 {
			res.Body.Close()
			return nil, &HTTPError{Status: res.StatusCode, Msg: fmt.Sprintf("earnings page http %d", res.StatusCode)}
		}
		rows, perr := ParseEarningsPage(io.LimitReader(res.Body, 4<<20))
		res.Body.Close()
		return rows, perr
	}
	return nil, lastErr
}

// ParseQuoteEarnings reads a v7/finance/quote answer and returns the earnings timestamps it carries as ISO
// UTC dates (earningsTimestamp, earningsTimestampStart, earningsTimestampEnd), oldest first, de-duplicated.
// The caller decides which of them have passed. An answer with no result is ErrNoCalendar.
func ParseQuoteEarnings(symbol string, raw []byte) ([]string, error) {
	var env struct {
		QuoteResponse struct {
			Result []struct {
				Symbol    string `json:"symbol"`
				Timestamp int64  `json:"earningsTimestamp"`
				Start     int64  `json:"earningsTimestampStart"`
				End       int64  `json:"earningsTimestampEnd"`
			} `json:"result"`
		} `json:"quoteResponse"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("quote %s: %w", symbol, err)
	}
	if len(env.QuoteResponse.Result) == 0 {
		return nil, ErrNoCalendar
	}
	r := env.QuoteResponse.Result[0]
	seen := map[string]bool{}
	var out []string
	for _, ts := range []int64{r.Timestamp, r.Start, r.End} {
		if ts <= 0 {
			continue
		}
		d := time.Unix(ts, 0).UTC().Format("2006-01-02")
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	sortStrings(out)
	return out, nil
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// FetchYahooQuoteEarnings asks v7/finance/quote for a symbol's earnings timestamps. It is used only to learn
// the LAST REPORTED date for tickers whose earnings page has no recent reported row.
func FetchYahooQuoteEarnings(ctx context.Context, symbol string) ([]string, error) {
	u := fmt.Sprintf("https://query1.finance.yahoo.com/v7/finance/quote?symbols=%s", url.QueryEscape(symbol))
	raw, err := yahoo.yahooGet(ctx, u)
	if err != nil {
		return nil, err
	}
	return ParseQuoteEarnings(symbol, raw)
}
