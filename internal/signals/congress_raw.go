package signals

// SC-47 — Congress trades from the raw history table (fed by Bargo, plus a
// one-time import of the legacy Capitol Trades file). The raw table keeps every
// trade; this file turns the recent ones into tiered signal_events through the
// same ingestCongressBatch path the other feeds use.

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"ft/internal/bargo"
)

// ImportLegacyCapitolTrades copies the old Playwright-scraped trades.json into
// congress_trades_raw (source 'capitol_trades'), once and idempotently, so the
// history that predates Bargo is kept. Returns rows newly inserted.
func (s *Service) ImportLegacyCapitolTrades(ctx context.Context) (int, error) {
	raw, err := os.ReadFile(capitolTradesCachePath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	var f capitolTradesFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return 0, fmt.Errorf("parse capitol-trades json: %w", err)
	}
	now := time.Now().UTC().Unix()
	added := 0
	for _, t := range f.Trades {
		h := sha1.Sum([]byte(strings.Join([]string{
			t.Politician, t.Chamber, strings.ToUpper(t.Ticker), t.Type, t.TradeDate, t.DisclosureDate, t.Amount,
		}, "|")))
		key := "capitol:" + hex.EncodeToString(h[:10])
		lo, hi := parseBucketBounds(t.Amount)
		res, err := s.DB.ExecContext(ctx, `
			INSERT OR IGNORE INTO congress_trades_raw
			  (source, source_key, member, chamber, state, party, ticker, asset, trade_type,
			   amount_low, amount_high, amount_range, transaction_date, disclosure_date,
			   filing_url, first_seen_at, last_seen_at)
			VALUES ('capitol_trades', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			key, t.Politician, t.Chamber, t.State, t.Party, strings.ToUpper(strings.TrimSpace(t.Ticker)),
			t.AssetName, legacyType(t.Type), lo, hi, t.Amount, t.TradeDate, t.DisclosureDate,
			t.SourceURL, now, now)
		if err != nil {
			return added, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			added++
		}
	}
	return added, nil
}

func legacyType(t string) string {
	switch strings.ToUpper(strings.TrimSpace(t)) {
	case "BUY":
		return "purchase"
	case "SELL":
		return "sale"
	case "EXCH":
		return "exchange"
	}
	return ""
}

// parseBucketBounds extracts the low/high of "$1,001 - $15,000".
func parseBucketBounds(b string) (lo, hi *float64) {
	parts := strings.Split(b, "-")
	num := func(s string) *float64 {
		s = strings.NewReplacer("$", "", ",", "", " ", "").Replace(s)
		var v float64
		if _, err := fmt.Sscanf(s, "%f", &v); err != nil {
			return nil
		}
		return &v
	}
	if len(parts) == 2 {
		return num(parts[0]), num(parts[1])
	}
	return nil, nil
}

// IngestCongressFromRaw turns raw rows disclosed within the lookback window into
// signal_events via ingestCongressBatch (idempotent: INSERT OR IGNORE on
// signal_type+source+source_id). Only Bargo rows are used here; legacy rows are
// already covered by the legacy file path.
func (s *Service) IngestCongressFromRaw(ctx context.Context) (int, error) {
	cutoff := time.Now().UTC().Add(-swLookback)
	rows, err := s.DB.QueryContext(ctx, `
		SELECT source_key, COALESCE(member,''), COALESCE(chamber,''), COALESCE(state,''),
		       COALESCE(ticker,''), COALESCE(asset,''), COALESCE(trade_type,''),
		       COALESCE(amount_range,''), COALESCE(transaction_date,''),
		       COALESCE(disclosure_date,''), COALESCE(filing_url,'')
		  FROM congress_trades_raw
		 WHERE source = 'bargo' AND disclosure_date >= ?
		 ORDER BY disclosure_date`, cutoff.Format("2006-01-02"))
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var house, senate []houseTxn
	for rows.Next() {
		var key, member, chamber, state, ticker, asset, typ, amt, txn, disc, link string
		if err := rows.Scan(&key, &member, &chamber, &state, &ticker, &asset, &typ, &amt, &txn, &disc, &link); err != nil {
			slog.Warn("signals: congress raw scan", "err", err)
			continue
		}
		t := houseTxn{
			TransactionID:    key,
			DisclosureDate:   disc,
			TransactionDate:  txn,
			Ticker:           ticker,
			AssetDescription: asset,
			Type:             bargoTypeToCongressType(typ),
			Amount:           amt,
			Representative:   member,
			District:         state,
			PtrLink:          link,
		}
		if chamber == "senate" {
			senate = append(senate, t)
		} else {
			house = append(house, t)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	n := s.ingestCongressBatch(ctx, house, "house", cutoff) +
		s.ingestCongressBatch(ctx, senate, "senate", cutoff)
	return n, nil
}

func bargoTypeToCongressType(t string) string {
	switch t {
	case "purchase":
		return "Purchase"
	case "sale":
		return "Sale (Partial)"
	case "exchange":
		return "Exchange"
	}
	return ""
}

// RefreshCongress is the Congress feed entry point (SC-47): keep the legacy
// Capitol Trades history, pull Bargo into the raw table, then turn recent raw
// rows into signal_events. A Bargo failure is returned, but whatever is already
// in the raw table is still ingested.
func (s *Service) RefreshCongress(ctx context.Context) (int, error) {
	if _, err := s.ImportLegacyCapitolTrades(ctx); err != nil {
		slog.Warn("signals: legacy capitol import", "err", err)
	}
	_, syncErr := bargo.Sync(ctx, s.DB, bargo.NewClient(), time.Now())
	if syncErr != nil {
		slog.Error("signals: bargo sync", "err", syncErr)
	}
	n, err := s.IngestCongressFromRaw(ctx)
	if err != nil {
		return n, err
	}
	return n, syncErr
}
