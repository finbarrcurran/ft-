package signals

import (
	"context"
	"path/filepath"
	"testing"

	"ft/internal/store"
)

// SC-47: Bargo rows in the raw table become signal_events once, idempotently,
// and the raw rows are never consumed.
func TestIngestCongressFromRaw(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "ft.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	now := "strftime('%s','now')"
	ins := `INSERT INTO congress_trades_raw
	  (source, source_key, member, chamber, state, ticker, asset, trade_type, amount_low, amount_high,
	   amount_range, transaction_date, disclosure_date, filing_url, first_seen_at, last_seen_at)
	  VALUES (?, ?, ?, ?, 'OH06', ?, 'Acme', ?, 1001, 15000, ?, date('now','-5 day'), date('now','-2 day'),
	          'https://example.test/f', ` + now + `, ` + now + `)`
	rows := [][]any{
		{"bargo", "bargo:1", "Jane Roe", "house", "MSFT", "purchase", "$1,001 - $15,000"},
		{"bargo", "bargo:2", "John Doe", "senate", "NVDA", "sale", "$15,001 - $50,000"},
		{"bargo", "bargo:3", "No Ticker", "house", "", "purchase", "$1,001 - $15,000"},
		{"capitol_trades", "capitol:1", "Legacy", "house", "AAPL", "purchase", "$1,001 - $15,000"},
	}
	for _, r := range rows {
		if _, err := st.DB.Exec(ins, r...); err != nil {
			t.Fatal(err)
		}
	}
	svc := New(st.DB)
	n, err := svc.IngestCongressFromRaw(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("inserted %d, want 2 (Bargo rows with a ticker only)", n)
	}
	n, _ = svc.IngestCongressFromRaw(ctx)
	if n != 0 {
		t.Fatalf("second pass inserted %d, want 0", n)
	}
	var raw, ev int
	st.DB.QueryRow(`SELECT COUNT(*) FROM congress_trades_raw`).Scan(&raw)
	st.DB.QueryRow(`SELECT COUNT(*) FROM signal_events WHERE signal_type='congress'`).Scan(&ev)
	if raw != 4 || ev != 2 {
		t.Fatalf("raw=%d events=%d, want 4 and 2", raw, ev)
	}
}
