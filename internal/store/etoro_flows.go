package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// SC-52 P4 — capital flows: reading, summarising and the manual fallback.

// CapitalFlow is one stored flow.
type CapitalFlow struct {
	ID         int64   `json:"id"`
	OccurredAt string  `json:"occurredAt"`
	Day        string  `json:"day"`
	Kind       string  `json:"kind"`
	AmountUSD  float64 `json:"amountUsd"`
	Details    string  `json:"details"`
	Manual     bool    `json:"manual"`
	SourceFile string  `json:"sourceFile,omitempty"`
}

// FlowYear totals one calendar year.
type FlowYear struct {
	Year       int     `json:"year"`
	InUSD      float64 `json:"inUsd"`      // deposits + transfers to trading + manual in
	OutUSD     float64 `json:"outUsd"`     // withdrawals + transfers to eToro Money, net of reversals (negative)
	NetUSD     float64 `json:"netUsd"`
	FeesUSD    float64 `json:"feesUsd"`    // FX conversion fees on deposits/withdrawals (negative)
	Rows       int     `json:"rows"`
}

// FlowSummary is the Performance-tab / snapshot view.
type FlowSummary struct {
	Has      bool       `json:"has"`
	First    string     `json:"first,omitempty"`
	Last     string     `json:"last,omitempty"` // day of the latest flow row
	InUSD    float64    `json:"inUsd"`
	OutUSD   float64    `json:"outUsd"`
	NetUSD   float64    `json:"netUsd"`
	FeesUSD  float64    `json:"feesUsd"`
	ByYear   []FlowYear `json:"byYear"`
	Manual   int        `json:"manualRows"`
}

func isIn(kind string) bool  { return kind == "deposit" || kind == "transfer_in" || kind == "manual_in" }
func isOut(kind string) bool { return kind == "withdrawal" || kind == "transfer_out" || kind == "reversal" || kind == "manual_out" }

// ListCapitalFlows returns the flows in [from, to] (either may be ""), oldest first.
func (s *Store) ListCapitalFlows(ctx context.Context, userID int64, from, to string) ([]CapitalFlow, error) {
	q := `SELECT id, occurred_at, day, kind, amount_usd, details, manual, source_file FROM etoro_capital_flows WHERE user_id = ?`
	args := []any{userID}
	if from != "" {
		q += " AND day >= ?"
		args = append(args, from)
	}
	if to != "" {
		q += " AND day <= ?"
		args = append(args, to)
	}
	q += " ORDER BY occurred_at, id"
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CapitalFlow{}
	for rows.Next() {
		var f CapitalFlow
		var man int
		if err := rows.Scan(&f.ID, &f.OccurredAt, &f.Day, &f.Kind, &f.AmountUSD, &f.Details, &man, &f.SourceFile); err != nil {
			return nil, err
		}
		f.Manual = man == 1
		out = append(out, f)
	}
	return out, rows.Err()
}

// CapitalFlowSummary totals every stored flow, by year.
func (s *Store) CapitalFlowSummary(ctx context.Context, userID int64) (FlowSummary, error) {
	fl, err := s.ListCapitalFlows(ctx, userID, "", "")
	if err != nil {
		return FlowSummary{}, err
	}
	return SummariseFlows(fl), nil
}

// SummariseFlows is pure (tested without a database).
func SummariseFlows(fl []CapitalFlow) FlowSummary {
	var sum FlowSummary
	years := map[int]*FlowYear{}
	var order []int
	for _, f := range fl {
		sum.Has = true
		if sum.First == "" || f.Day < sum.First {
			sum.First = f.Day
		}
		if f.Day > sum.Last && f.Kind != "fx_fee" {
			sum.Last = f.Day
		}
		if f.Manual {
			sum.Manual++
		}
		y := 0
		fmt.Sscanf(f.Day[:4], "%d", &y)
		fy := years[y]
		if fy == nil {
			fy = &FlowYear{Year: y}
			years[y] = fy
			order = append(order, y)
		}
		fy.Rows++
		switch {
		case isIn(f.Kind):
			fy.InUSD += f.AmountUSD
			sum.InUSD += f.AmountUSD
		case isOut(f.Kind):
			fy.OutUSD += f.AmountUSD
			sum.OutUSD += f.AmountUSD
		case f.Kind == "fx_fee":
			fy.FeesUSD += f.AmountUSD
			sum.FeesUSD += f.AmountUSD
		}
	}
	for i := 0; i < len(order); i++ { // oldest first
		for j := i + 1; j < len(order); j++ {
			if order[j] < order[i] {
				order[i], order[j] = order[j], order[i]
			}
		}
	}
	for _, y := range order {
		years[y].NetUSD = years[y].InUSD + years[y].OutUSD
		sum.ByYear = append(sum.ByYear, *years[y])
	}
	sum.NetUSD = sum.InUSD + sum.OutUSD
	return sum
}

// AddManualFlow is the fallback for periods without a statement. dir is "in" or "out"; amount is
// positive. The row is flagged manual and can be deleted again; statement rows cannot.
func (s *Store) AddManualFlow(ctx context.Context, userID int64, day, dir string, amountUSD float64, note string) (int64, error) {
	if _, err := time.Parse("2006-01-02", day); err != nil {
		return 0, fmt.Errorf("day must be YYYY-MM-DD")
	}
	if dir != "in" && dir != "out" {
		return 0, fmt.Errorf("direction must be in or out")
	}
	if amountUSD <= 0 || amountUSD > 1e9 {
		return 0, fmt.Errorf("amount must be a positive number")
	}
	note = strings.TrimSpace(note)
	if len(note) > 200 {
		note = note[:200]
	}
	amt, kind := amountUSD, "manual_in"
	if dir == "out" {
		amt, kind = -amountUSD, "manual_out"
	}
	now := time.Now()
	key := fmt.Sprintf("manual-%d-%d", now.UnixNano(), userID)
	r, err := s.DB.ExecContext(ctx, `
		INSERT INTO etoro_capital_flows (user_id, flow_key, occurred_at, day, kind, amount_usd, details, manual, source_file, imported_at)
		VALUES (?,?,?,?,?,?,?,1,'manual',?)`, userID, key, day+"T00:00:00", day, kind, amt, note, now.Unix())
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

// DeleteManualFlow removes a manual row only; it reports whether anything was removed.
func (s *Store) DeleteManualFlow(ctx context.Context, userID, id int64) (bool, error) {
	r, err := s.DB.ExecContext(ctx, `DELETE FROM etoro_capital_flows WHERE user_id = ? AND id = ? AND manual = 1`, userID, id)
	if err != nil {
		return false, err
	}
	n, _ := r.RowsAffected()
	return n > 0, nil
}

// CapitalFlowsBetween totals the flows with occurred_at in (from, to] (statement clock; both are
// instants), for the snapshot's "of which" notes.
func (s *Store) CapitalFlowsBetween(ctx context.Context, userID int64, from, to time.Time) (FlowSummary, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, occurred_at, day, kind, amount_usd, details, manual, source_file FROM etoro_capital_flows
		 WHERE user_id = ? AND occurred_at > ? AND occurred_at <= ? ORDER BY occurred_at`,
		userID, from.UTC().Format("2006-01-02T15:04:05"), to.UTC().Format("2006-01-02T15:04:05"))
	if err != nil {
		return FlowSummary{}, err
	}
	defer rows.Close()
	var fl []CapitalFlow
	for rows.Next() {
		var f CapitalFlow
		var man int
		if err := rows.Scan(&f.ID, &f.OccurredAt, &f.Day, &f.Kind, &f.AmountUSD, &f.Details, &man, &f.SourceFile); err != nil {
			return FlowSummary{}, err
		}
		f.Manual = man == 1
		fl = append(fl, f)
	}
	return SummariseFlows(fl), rows.Err()
}

var _ = sql.ErrNoRows
