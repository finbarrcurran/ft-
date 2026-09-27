// Package etorosync polls eToro's Public API for the account's open positions
// and records them in FT (SC-42): a per-lot change history plus a per-ticker
// "effective" stop-loss / take-profit. Deterministic and scheduled — it makes
// no LLM calls and does not import internal/llm.
//
// eToro's levels are stored alongside FT's own stop/TP; FT's alert logic is
// untouched (Fin decision 2026-09-26). Since SC-44 each successful sync is
// followed by a holdings reconcile (reconcile.go): value fields eToro owns are
// kept in step silently, existence changes are queued for approval.
package etorosync

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"ft/internal/etoro"
	"ft/internal/store"
)

// DefaultBaseURL is eToro's Public API host (verified 2026-09-26: unauthenticated
// requests return 401, not a DNS or 404 error).
const DefaultBaseURL = "https://public-api.etoro.com"

// ErrNotConfigured means the API key pair isn't set; the sync is skipped.
var ErrNotConfigured = errors.New("etoro sync: FT_ETORO_API_KEY / FT_ETORO_USER_KEY not configured")

// Service runs syncs. APIKey is eToro's application key (x-api-key); UserKey is
// the account's user key (x-user-key). Both come from /etc/ft/env.
type Service struct {
	DB      *sql.DB
	Store   *store.Store // for approved holding inserts / soft-deletes (SC-44)
	BaseURL string
	APIKey  string
	UserKey string
	HTTP    *http.Client
	Now     func() time.Time
}

// New returns a Service; an empty baseURL means DefaultBaseURL.
func New(st *store.Store, baseURL, apiKey, userKey string) *Service {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Service{DB: st.DB, Store: st, BaseURL: strings.TrimRight(baseURL, "/"), APIKey: apiKey, UserKey: userKey,
		HTTP: &http.Client{Timeout: 30 * time.Second}, Now: time.Now}
}

// Configured reports whether both keys are present.
func (s *Service) Configured() bool { return s.APIKey != "" && s.UserKey != "" }

// ---------------------------------------------------------------------------
// eToro API client

// position mirrors the fields FT uses from eToro's Position schema.
type position struct {
	PositionID     int64   `json:"positionID"`
	InstrumentID   int     `json:"instrumentID"`
	OpenDateTime   string  `json:"openDateTime"`
	OpenRate       float64 `json:"openRate"`
	Amount         float64 `json:"amount"` // USD currently invested (eToro-resolved FX; shrinks on partial close)
	IsBuy          bool    `json:"isBuy"`
	TakeProfitRate float64 `json:"takeProfitRate"`
	StopLossRate   float64 `json:"stopLossRate"`
	MirrorID       int64   `json:"mirrorID"`
	Leverage       float64 `json:"leverage"`
	Units          float64 `json:"units"`
	IsTslEnabled   bool    `json:"isTslEnabled"`
	SettlementType int     `json:"settlementTypeID"`
	IsNoTakeProfit bool    `json:"isNoTakeProfit"`
	IsNoStopLoss   bool    `json:"isNoStopLoss"`
}

type portfolioResp struct {
	ClientPortfolio struct {
		Positions []position `json:"positions"`
		Mirrors   []struct {
			MirrorID  int64      `json:"mirrorID"`
			Positions []position `json:"positions"`
		} `json:"mirrors"`
	} `json:"clientPortfolio"`
}

type instrumentsResp struct {
	InstrumentDisplayDatas []struct {
		InstrumentID          int    `json:"instrumentID"`
		InstrumentDisplayName string `json:"instrumentDisplayName"`
		InstrumentTypeID      int    `json:"instrumentTypeID"`
		SymbolFull            string `json:"symbolFull"`
	} `json:"instrumentDisplayDatas"`
}

func requestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// APIError is a non-2xx response. The body preview never contains our keys
// (they travel in headers only).
type APIError struct {
	Status     int
	RetryAfter string
	Preview    string
}

func (e *APIError) Error() string {
	if e.Status == http.StatusTooManyRequests {
		return fmt.Sprintf("etoro 429 rate limited (retry after %ss)", e.RetryAfter)
	}
	return fmt.Sprintf("etoro %d: %s", e.Status, e.Preview)
}

func (s *Service) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.BaseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", s.APIKey)
	req.Header.Set("x-user-key", s.UserKey)
	req.Header.Set("x-request-id", requestID())
	req.Header.Set("Accept", "application/json")
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("etoro request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		p := string(body)
		if len(p) > 240 {
			p = p[:240] + "…"
		}
		return &APIError{Status: resp.StatusCode, RetryAfter: resp.Header.Get("Retry-After"), Preview: p}
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("etoro decode %s: %w", path, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Sync

// Result summarises one sync.
type Result struct {
	RunID      int64  `json:"runId"`
	Status     string `json:"status"` // ok | failed | skipped
	Positions  int    `json:"positions"`
	OwnLots    int    `json:"ownLots"`
	CopyLots   int    `json:"copyLots"`
	LotsNew    int    `json:"lotsNew"`
	LotsClosed int    `json:"lotsClosed"`
	Tickers    int    `json:"tickers"`
	// SC-44 reconcile
	ValuesUpdated    int    `json:"valuesUpdated"`
	ProposalsPending int    `json:"proposalsPending"`
	ProposalsNew     int    `json:"proposalsNew"`
	ReconcileError   string `json:"reconcileError,omitempty"`
	Error            string `json:"error,omitempty"`
}

// lot is one position, normalised for storage.
type lot struct {
	PositionID   int64
	InstrumentID int
	Ticker       string
	IsBuy        bool
	Units        float64
	OpenRate     float64
	AmountUSD    float64 // eToro's own USD figure for this lot
	OpenDate     string
	Leverage     float64
	StopLoss     *float64 // nil = disabled at eToro
	TakeProfit   *float64 // nil = disabled at eToro
	IsTSL        bool
	MirrorID     int64
	Settlement   int
}

// normalise converts an eToro position. The isNo* flags are authoritative:
// eToro still returns placeholder rates (e.g. stopLossRate 0.0001, takeProfitRate
// 0) when a level is disabled, and those must never enter the min/max rule.
func normalise(p position, mirrorID int64, ticker string) lot {
	l := lot{PositionID: p.PositionID, InstrumentID: p.InstrumentID, Ticker: ticker, IsBuy: p.IsBuy,
		Units: p.Units, OpenRate: p.OpenRate, AmountUSD: p.Amount, OpenDate: p.OpenDateTime, Leverage: p.Leverage,
		IsTSL: p.IsTslEnabled, MirrorID: mirrorID, Settlement: p.SettlementType}
	if l.MirrorID == 0 {
		l.MirrorID = p.MirrorID
	}
	if !p.IsNoStopLoss && p.StopLossRate > 0 {
		v := p.StopLossRate
		l.StopLoss = &v
	}
	if !p.IsNoTakeProfit && p.TakeProfitRate > 0 {
		v := p.TakeProfitRate
		l.TakeProfit = &v
	}
	return l
}

// Effective is the per-ticker, per-direction current state (own lots only).
type Effective struct {
	Ticker                string   `json:"ticker"`
	Direction             string   `json:"direction"`
	InstrumentID          int      `json:"instrumentId"`
	TotalUnits            float64  `json:"totalUnits"`
	InvestedUSD           float64  `json:"investedUsd"` // sum of eToro's USD amounts (SC-44)
	AvgOpenPrice          *float64 `json:"avgOpenPrice"`
	LotCount              int      `json:"lotCount"`
	SLEffective           *float64 `json:"slEffective"`
	TPEffective           *float64 `json:"tpEffective"`
	SLEffectivePositionID *int64   `json:"slEffectivePositionId"`
	TPEffectivePositionID *int64   `json:"tpEffectivePositionId"`
	HasNoSL               bool     `json:"hasNoSl"`
	LotsWithoutSL         int      `json:"lotsWithoutSl"`
	HasNoTP               bool     `json:"hasNoTp"`
	CopyLotCount          int      `json:"copyLotCount"`
	LastSyncedAt          int64    `json:"lastSyncedAt"`
}

// minMeaningfulStop: a long's stop below this fraction of its open price protects
// nothing. eToro reports such stops as ENABLED (isNoStopLoss=false) — seen live:
// SLV at 0.0001, 4063.T at 0.01 — so the flag alone can't be trusted. The raw
// value is still stored in the lot history; it just doesn't count as a stop.
const minMeaningfulStop = 0.05

// effectiveStop returns the lot's stop if it's a real one, else nil.
func effectiveStop(l lot) *float64 {
	if l.StopLoss == nil {
		return nil
	}
	if l.IsBuy && l.OpenRate > 0 && *l.StopLoss < minMeaningfulStop*l.OpenRate {
		return nil
	}
	return l.StopLoss
}

// ComputeEffective applies the SC-42 §2 rule. Longs: highest stop-loss, lowest
// take-profit across lots. Shorts: the mirror image (a short's stop sits above
// price, so its most conservative stop is the LOWEST; its nearest TP the
// HIGHEST). Copy-trade lots are excluded from the levels but counted. A
// near-zero stop counts as no stop (see minMeaningfulStop).
func ComputeEffective(lots []lot, syncedAt int64) []Effective {
	type key struct {
		ticker string
		buy    bool
	}
	groups := map[key]*Effective{}
	cost := map[key]float64{}
	copies := map[string]int{}
	for _, l := range lots {
		if l.MirrorID != 0 {
			copies[l.Ticker]++
			continue
		}
		k := key{l.Ticker, l.IsBuy}
		e := groups[k]
		if e == nil {
			dir := "long"
			if !l.IsBuy {
				dir = "short"
			}
			e = &Effective{Ticker: l.Ticker, Direction: dir, InstrumentID: l.InstrumentID, LastSyncedAt: syncedAt}
			groups[k] = e
		}
		e.LotCount++
		e.TotalUnits += l.Units
		e.InvestedUSD += l.AmountUSD
		cost[k] += l.Units * l.OpenRate
		if sl := effectiveStop(l); sl == nil {
			e.LotsWithoutSL++
		} else if e.SLEffective == nil || (l.IsBuy && *sl > *e.SLEffective) || (!l.IsBuy && *sl < *e.SLEffective) {
			v, id := *sl, l.PositionID
			e.SLEffective, e.SLEffectivePositionID = &v, &id
		}
		if l.TakeProfit != nil && (e.TPEffective == nil || (l.IsBuy && *l.TakeProfit < *e.TPEffective) || (!l.IsBuy && *l.TakeProfit > *e.TPEffective)) {
			v, id := *l.TakeProfit, l.PositionID
			e.TPEffective, e.TPEffectivePositionID = &v, &id
		}
	}
	out := make([]Effective, 0, len(groups))
	for k, e := range groups {
		if e.TotalUnits > 0 {
			avg := cost[k] / e.TotalUnits
			e.AvgOpenPrice = &avg
		}
		e.HasNoSL = e.SLEffective == nil
		e.HasNoTP = e.TPEffective == nil
		e.CopyLotCount = copies[e.Ticker]
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Ticker != out[j].Ticker {
			return out[i].Ticker < out[j].Ticker
		}
		return out[i].Direction < out[j].Direction
	})
	return out
}

var syncMu sync.Mutex

// Sync fetches the portfolio and records it. Safe to re-run: unchanged lots only
// have last_seen_at moved forward.
func (s *Service) Sync(ctx context.Context) Result {
	syncMu.Lock()
	defer syncMu.Unlock()
	started := s.Now().Unix()
	res := Result{Status: "skipped"}
	r, err := s.DB.ExecContext(ctx, `INSERT INTO etoro_sync_runs (started_at, status) VALUES (?, 'running')`, started)
	if err != nil {
		res.Status, res.Error = "failed", err.Error()
		return res
	}
	res.RunID, _ = r.LastInsertId()
	finish := func() Result {
		_, _ = s.DB.ExecContext(context.Background(), `UPDATE etoro_sync_runs SET finished_at=?, status=?, positions=?,
			lots_new=?, lots_closed=?, values_updated=?, proposals_pending=?, error=? WHERE id=?`, s.Now().Unix(),
			res.Status, res.Positions, res.LotsNew, res.LotsClosed, res.ValuesUpdated, res.ProposalsPending,
			nullIfEmpty(firstNonEmpty(res.Error, res.ReconcileError)), res.RunID)
		return res
	}
	if !s.Configured() {
		res.Error = ErrNotConfigured.Error()
		return finish()
	}
	fail := func(err error) Result { res.Status, res.Error = "failed", err.Error(); return finish() }

	var pf portfolioResp
	if err := s.get(ctx, "/api/v1/trading/info/portfolio", &pf); err != nil {
		return fail(err)
	}
	type raw struct {
		p      position
		mirror int64
	}
	var all []raw
	for _, p := range pf.ClientPortfolio.Positions {
		all = append(all, raw{p, 0})
	}
	for _, m := range pf.ClientPortfolio.Mirrors {
		for _, p := range m.Positions {
			all = append(all, raw{p, m.MirrorID})
		}
	}
	res.Positions = len(all)

	tickers, err := s.tickers(ctx, all2ids(all, func(r raw) int { return r.p.InstrumentID }))
	if err != nil {
		return fail(err)
	}
	lots := make([]lot, 0, len(all))
	for _, r := range all {
		l := normalise(r.p, r.mirror, tickers[r.p.InstrumentID])
		if l.MirrorID != 0 {
			res.CopyLots++
		} else {
			res.OwnLots++
		}
		lots = append(lots, l)
	}

	now := s.Now().Unix()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return fail(err)
	}
	defer tx.Rollback()

	current, err := openLots(ctx, tx)
	if err != nil {
		return fail(err)
	}
	// Guard: an empty portfolio while FT believes positions are open is far more
	// likely an API glitch than a full liquidation — don't close the history.
	if len(lots) == 0 && len(current) > 0 {
		return fail(fmt.Errorf("eToro returned 0 positions but %d are open in FT; history left untouched", len(current)))
	}
	seen := map[int64]bool{}
	for _, l := range lots {
		seen[l.PositionID] = true
		if c, ok := current[l.PositionID]; ok && sameState(c, l) {
			// amount_usd arrived with SC-44: backfill it in place on pre-existing
			// rows rather than treating its first appearance as a state change.
			if _, err := tx.ExecContext(ctx, `UPDATE etoro_holdings_lots SET last_seen_at=?, ticker=?,
				amount_usd=coalesce(amount_usd, ?) WHERE id=?`, now, l.Ticker, l.AmountUSD, c.rowID); err != nil {
				return fail(err)
			}
			continue
		} else if ok {
			if _, err := tx.ExecContext(ctx, `UPDATE etoro_holdings_lots SET superseded_at=? WHERE id=?`, now, c.rowID); err != nil {
				return fail(err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO etoro_holdings_lots (position_id, instrument_id, ticker, is_buy,
			units, open_rate, amount_usd, open_date, leverage, stop_loss, take_profit, is_tsl, mirror_id, settlement_type,
			first_seen_at, last_seen_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			l.PositionID, l.InstrumentID, l.Ticker, b2i(l.IsBuy), l.Units, l.OpenRate, l.AmountUSD, l.OpenDate, l.Leverage,
			l.StopLoss, l.TakeProfit, b2i(l.IsTSL), l.MirrorID, l.Settlement, now, now); err != nil {
			return fail(err)
		}
		res.LotsNew++
	}
	for pid, c := range current {
		if !seen[pid] {
			if _, err := tx.ExecContext(ctx, `UPDATE etoro_holdings_lots SET closed_at=? WHERE id=?`, now, c.rowID); err != nil {
				return fail(err)
			}
			res.LotsClosed++
		}
	}

	eff := ComputeEffective(lots, now)
	if _, err := tx.ExecContext(ctx, `DELETE FROM etoro_holdings_effective`); err != nil {
		return fail(err)
	}
	for _, e := range eff {
		if _, err := tx.ExecContext(ctx, `INSERT INTO etoro_holdings_effective (ticker, direction, instrument_id,
			total_units, invested_usd, avg_open_price, lot_count, sl_effective, tp_effective, sl_effective_position_id,
			tp_effective_position_id, has_no_sl, lots_without_sl, has_no_tp, copy_lot_count, last_synced_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, e.Ticker, e.Direction, e.InstrumentID, e.TotalUnits, e.InvestedUSD,
			e.AvgOpenPrice, e.LotCount, e.SLEffective, e.TPEffective, e.SLEffectivePositionID,
			e.TPEffectivePositionID, b2i(e.HasNoSL), e.LotsWithoutSL, b2i(e.HasNoTP), e.CopyLotCount, now); err != nil {
			return fail(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fail(err)
	}
	res.Status, res.Tickers = "ok", len(eff)
	// SC-44: reconcile FT holdings against the fresh effective table. A
	// reconcile failure doesn't fail the sync (the eToro data is committed).
	if ro, err := s.reconcile(ctx, eff); err != nil {
		res.ReconcileError = "reconcile: " + err.Error()
	} else {
		res.ValuesUpdated, res.ProposalsPending, res.ProposalsNew = ro.valuesUpdated, ro.pending, ro.created
	}
	return finish()
}

type openLot struct {
	rowID      int64
	units      float64
	amountUSD  sql.NullFloat64
	stopLoss   sql.NullFloat64
	takeProfit sql.NullFloat64
	isTSL      int
	leverage   sql.NullFloat64
}

func openLots(ctx context.Context, tx *sql.Tx) (map[int64]openLot, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, position_id, units, amount_usd, stop_loss, take_profit, is_tsl, leverage
		FROM etoro_holdings_lots WHERE superseded_at IS NULL AND closed_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]openLot{}
	for rows.Next() {
		var o openLot
		var pid int64
		if err := rows.Scan(&o.rowID, &pid, &o.units, &o.amountUSD, &o.stopLoss, &o.takeProfit, &o.isTSL, &o.leverage); err != nil {
			return nil, err
		}
		out[pid] = o
	}
	return out, rows.Err()
}

func near(a, b float64) bool {
	return math.Abs(a-b) <= 1e-6*math.Max(1, math.Max(math.Abs(a), math.Abs(b)))
}

func sameOpt(n sql.NullFloat64, v *float64) bool {
	if !n.Valid || v == nil {
		return !n.Valid && v == nil
	}
	return near(n.Float64, *v)
}

// sameState: has any tracked field of this position changed since the stored row?
// A NULL stored amount (row written before SC-44) is not a change.
func sameState(c openLot, l lot) bool {
	return near(c.units, l.Units) && (!c.amountUSD.Valid || near(c.amountUSD.Float64, l.AmountUSD)) && sameOpt(c.stopLoss, l.StopLoss) && sameOpt(c.takeProfit, l.TakeProfit) &&
		c.isTSL == b2i(l.IsTSL) && (!c.leverage.Valid || near(c.leverage.Float64, l.Leverage))
}

// tickers maps instrument ids to FT tickers, fetching unknown ids from eToro in
// one call and caching them in etoro_instruments.
func (s *Service) tickers(ctx context.Context, ids []int) (map[int]string, error) {
	out := map[int]string{}
	var missing []int
	for _, id := range ids {
		var t string
		err := s.DB.QueryRowContext(ctx, `SELECT ticker FROM etoro_instruments WHERE instrument_id=?`, id).Scan(&t)
		if err == nil {
			out[id] = t
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		missing = append(missing, id)
	}
	if len(missing) > 0 {
		strs := make([]string, len(missing))
		for i, id := range missing {
			strs[i] = strconv.Itoa(id)
		}
		var ir instrumentsResp
		if err := s.get(ctx, "/api/v1/market-data/instruments?instrumentIds="+strings.Join(strs, ","), &ir); err != nil {
			return nil, err
		}
		for _, d := range ir.InstrumentDisplayDatas {
			if d.SymbolFull == "" {
				continue
			}
			t := etoro.NormalizeTicker(d.SymbolFull)
			out[d.InstrumentID] = t
			if _, err := s.DB.ExecContext(ctx, `INSERT INTO etoro_instruments (instrument_id, symbol, ticker, name,
				instrument_type_id, updated_at) VALUES (?,?,?,?,?,?) ON CONFLICT(instrument_id) DO UPDATE SET
				symbol=excluded.symbol, ticker=excluded.ticker, name=excluded.name, updated_at=excluded.updated_at`,
				d.InstrumentID, d.SymbolFull, t, d.InstrumentDisplayName, d.InstrumentTypeID, s.Now().Unix()); err != nil {
				return nil, err
			}
		}
	}
	for _, id := range ids {
		if out[id] == "" {
			out[id] = "#" + strconv.Itoa(id) // unresolved: keep the lot, visibly unmapped
		}
	}
	return out, nil
}

func all2ids[T any](xs []T, f func(T) int) []int {
	seen := map[int]bool{}
	var out []int
	for _, x := range xs {
		if id := f(x); !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// ---------------------------------------------------------------------------
// Read side

// Status is the API/UI view: the last run and current effective levels.
type Status struct {
	Configured bool        `json:"configured"`
	LastRun    *RunInfo    `json:"lastRun"`
	LastOK     *RunInfo    `json:"lastOk"`
	Effective  []Effective `json:"effective"`
}

type RunInfo struct {
	ID         int64  `json:"id"`
	StartedAt  int64  `json:"startedAt"`
	Status     string `json:"status"`
	Positions  int    `json:"positions"`
	LotsNew    int    `json:"lotsNew"`
	LotsClosed int    `json:"lotsClosed"`
	Error      string `json:"error,omitempty"`
}

func (s *Service) run(ctx context.Context, where string) (*RunInfo, error) {
	var r RunInfo
	var pos, lnew, lclosed sql.NullInt64
	var e sql.NullString
	err := s.DB.QueryRowContext(ctx, `SELECT id, started_at, status, positions, lots_new, lots_closed, error
		FROM etoro_sync_runs `+where+` ORDER BY id DESC LIMIT 1`).Scan(&r.ID, &r.StartedAt, &r.Status, &pos, &lnew, &lclosed, &e)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.Positions, r.LotsNew, r.LotsClosed, r.Error = int(pos.Int64), int(lnew.Int64), int(lclosed.Int64), e.String
	return &r, nil
}

// Current returns the sync status and the effective per-ticker levels.
func (s *Service) Current(ctx context.Context) (Status, error) {
	st := Status{Configured: s.Configured(), Effective: []Effective{}}
	var err error
	if st.LastRun, err = s.run(ctx, ""); err != nil {
		return st, err
	}
	if st.LastOK, err = s.run(ctx, "WHERE status = 'ok'"); err != nil {
		return st, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT ticker, direction, instrument_id, total_units, coalesce(invested_usd, 0), avg_open_price,
		lot_count, sl_effective, tp_effective, sl_effective_position_id, tp_effective_position_id, has_no_sl,
		lots_without_sl, has_no_tp, copy_lot_count, last_synced_at FROM etoro_holdings_effective ORDER BY ticker, direction`)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	for rows.Next() {
		var e Effective
		var avg, sl, tp sql.NullFloat64
		var slID, tpID sql.NullInt64
		var noSL, noTP int
		if err := rows.Scan(&e.Ticker, &e.Direction, &e.InstrumentID, &e.TotalUnits, &e.InvestedUSD, &avg, &e.LotCount, &sl, &tp,
			&slID, &tpID, &noSL, &e.LotsWithoutSL, &noTP, &e.CopyLotCount, &e.LastSyncedAt); err != nil {
			return st, err
		}
		e.AvgOpenPrice, e.SLEffective, e.TPEffective = nf(avg), nf(sl), nf(tp)
		e.SLEffectivePositionID, e.TPEffectivePositionID = ni(slID), ni(tpID)
		e.HasNoSL, e.HasNoTP = noSL != 0, noTP != 0
		st.Effective = append(st.Effective, e)
	}
	return st, rows.Err()
}

func nf(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	return &n.Float64
}

func ni(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	return &n.Int64
}
