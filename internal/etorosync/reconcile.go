package etorosync

// SC-44 — automatic holdings reconcile from eToro.
//
// After each successful sync, FT's holdings are diffed against
// etoro_holdings_effective (own, long lots; copy-trades are already excluded):
//
//   - matched holding, values changed → silent update of invested_usd /
//     avg_open_price (stocks) or quantity / cost basis (crypto). eToro owns
//     those fields; FT keeps stop/TP, sl_method, notes, sectors, thesis links.
//   - eToro holding FT lacks          → 'add' proposal
//   - FT holding eToro no longer has  → 'remove' proposal
//
// Existence changes never apply without explicit approval. Crypto reconciles
// ONLY against crypto rows whose wallet is eToro — Ledger/Binance/MetaMask/
// Phantom holdings are never touched or proposed for removal.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math"

	"ft/internal/domain"
	"ft/internal/etoro"
	"ft/internal/store"
)

// Audit conventions for SC-44 (spec §7: every holdings mutation writes a
// holdings_audit row). Silent value updates are made by the sync itself
// (actor 'etoro-sync'); approved adds/removals are human decisions (actor 'fin').
// No trade_snapshot_json is written on purpose: performance.DeriveAll turns a
// soft_delete into a closed trade only when it finds a matching create with an
// entry snapshot, and eToro-side closures have no FT-side entry snapshot.
const (
	auditActorSync   = "etoro-sync"
	auditActorHuman  = "fin"
	auditReasonCode  = "etoro_reconcile"
	auditReasonAuto  = "SC-44 eToro reconcile: values updated from eToro"
	auditReasonAdd   = "SC-44 eToro reconcile: approved add (held at eToro, missing in FT)"
	auditReasonClose = "SC-44 eToro reconcile: approved removal (no longer held at eToro)"
)

// audit records one holdings_audit row; a failure is logged, never fatal — the
// holdings change has already happened and must not be rolled back by logging.
func (s *Service) audit(ctx context.Context, actor string, uid int64, kind string, holdingID int64,
	ticker string, action string, changes any, reason string) {
	if s.Store == nil {
		return
	}
	var tk, sym *string
	if kind == "crypto" {
		sym = &ticker
	} else {
		tk = &ticker
	}
	if err := s.Store.RecordAuditBy(ctx, actor, uid, kind, holdingID, tk, sym, action, changes, &reason, auditReasonCode); err != nil {
		slog.Warn("etoro reconcile: audit write failed", "kind", kind, "holding", holdingID, "ticker", ticker, "err", err)
	}
}

func fromTo(from sql.NullFloat64, to float64) map[string]any {
	var f any
	if from.Valid {
		f = from.Float64
	}
	return map[string]any{"from": f, "to": to}
}

// EtoroWallet is the crypto_holdings.wallet value for eToro-held crypto.
const EtoroWallet = "eToro"

// etoroCryptoType is eToro's instrumentTypeID for crypto (5 = stocks, 6 = ETF).
const etoroCryptoType = 10

type desired struct {
	kind, action, ticker, name string
	holdingID                  int64
	units, invested, avg       float64
	lots                       int
	ftInvested                 float64
	thesis                     bool
}

func (d desired) key() string { return d.kind + "|" + d.action + "|" + d.ticker }

type reconOutcome struct {
	valuesUpdated, pending, created int
}

func (s *Service) userID(ctx context.Context) (int64, error) {
	var id int64
	err := s.DB.QueryRowContext(ctx, `SELECT id FROM users ORDER BY id LIMIT 1`).Scan(&id)
	return id, err
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }

func changed(a, b float64, rel float64) bool {
	return math.Abs(a-b) > rel*math.Max(1e-9, math.Max(math.Abs(a), math.Abs(b)))
}

// reconcile runs after the sync transaction has committed.
func (s *Service) reconcile(ctx context.Context, eff []Effective) (reconOutcome, error) {
	var out reconOutcome
	uid, err := s.userID(ctx)
	if err != nil {
		return out, fmt.Errorf("reconcile: no FT user: %w", err)
	}
	now := s.Now().Unix()

	type ftRow struct {
		id            int64
		ticker, name  string
		invested, avg sql.NullFloat64
		thesis        bool
	}
	loadStocks := func() (map[string][]ftRow, error) {
		rows, err := s.DB.QueryContext(ctx, `SELECT id, upper(trim(ticker)), name, invested_usd, avg_open_price,
			coalesce(thesis_link,'') != '' OR EXISTS (SELECT 1 FROM holding_theses t WHERE t.holding_kind='stock' AND t.holding_id=stock_holdings.id)
			FROM stock_holdings WHERE user_id=? AND deleted_at IS NULL AND coalesce(trim(ticker),'') != '' ORDER BY id`, uid)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		m := map[string][]ftRow{}
		for rows.Next() {
			var r ftRow
			if err := rows.Scan(&r.id, &r.ticker, &r.name, &r.invested, &r.avg, &r.thesis); err != nil {
				return nil, err
			}
			m[r.ticker] = append(m[r.ticker], r)
		}
		return m, rows.Err()
	}
	loadCrypto := func() (map[string][]ftRow, error) {
		rows, err := s.DB.QueryContext(ctx, `SELECT id, upper(trim(symbol)), name, cost_basis_usd, avg_buy_usd,
			coalesce(thesis_link,'') != '' OR EXISTS (SELECT 1 FROM holding_theses t WHERE t.holding_kind='crypto' AND t.holding_id=crypto_holdings.id)
			FROM crypto_holdings WHERE user_id=? AND deleted_at IS NULL AND wallet = ? ORDER BY id`, uid, EtoroWallet)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		m := map[string][]ftRow{}
		for rows.Next() {
			var r ftRow
			if err := rows.Scan(&r.id, &r.ticker, &r.name, &r.invested, &r.avg, &r.thesis); err != nil {
				return nil, err
			}
			m[r.ticker] = append(m[r.ticker], r)
		}
		return m, rows.Err()
	}
	stocks, err := loadStocks()
	if err != nil {
		return out, err
	}
	cryptos, err := loadCrypto()
	if err != nil {
		return out, err
	}

	names := map[string]string{}
	cryptoInst := map[int]bool{}
	if rows, err := s.DB.QueryContext(ctx, `SELECT instrument_id, ticker, coalesce(name, symbol),
		coalesce(instrument_type_id, 0) FROM etoro_instruments`); err == nil {
		for rows.Next() {
			var id, typ int
			var t, n string
			if rows.Scan(&id, &t, &n, &typ) == nil {
				names[t] = n
				cryptoInst[id] = typ == etoroCryptoType
			}
		}
		rows.Close()
	}

	want := map[string]desired{}
	held := map[string]map[string]bool{"stock": {}, "crypto": {}}
	for _, e := range eff {
		if e.Direction != "long" {
			continue // FT holdings are long-only; shorts stay in the eToro tables
		}
		kind := "stock"
		if cryptoInst[e.InstrumentID] || etoro.IsCryptoUnderlying(e.Ticker) {
			kind = "crypto"
		}
		held[kind][e.Ticker] = true
		avg := 0.0
		if e.AvgOpenPrice != nil {
			avg = *e.AvgOpenPrice
		}
		ftRows := stocks[e.Ticker]
		if kind == "crypto" {
			ftRows = cryptos[e.Ticker]
		}
		if len(ftRows) == 0 {
			n := names[e.Ticker]
			if n == "" {
				n = e.Ticker
			}
			d := desired{kind: kind, action: "add", ticker: e.Ticker, name: n, units: e.TotalUnits,
				invested: round2(e.InvestedUSD), avg: avg, lots: e.LotCount}
			want[d.key()] = d
			continue
		}
		// Matched: eToro owns the values. One FT row per ticker is the norm;
		// if there are several, only the oldest is kept in step (and noted).
		r := ftRows[0]
		switch kind {
		case "stock":
			if !r.invested.Valid || changed(r.invested.Float64, round2(e.InvestedUSD), 1e-6) ||
				(avg > 0 && (!r.avg.Valid || changed(r.avg.Float64, avg, 1e-9))) {
				if _, err := s.DB.ExecContext(ctx, `UPDATE stock_holdings SET invested_usd=?, avg_open_price=?,
					updated_at=strftime('%s','now') WHERE id=?`, round2(e.InvestedUSD), nullIfZero(avg), r.id); err != nil {
					return out, err
				}
				out.valuesUpdated++
				ch := map[string]any{"investedUsd": fromTo(r.invested, round2(e.InvestedUSD))}
				if avg > 0 {
					ch["avgOpenPrice"] = fromTo(r.avg, avg)
				}
				s.audit(ctx, auditActorSync, uid, "stock", r.id, e.Ticker, store.AuditUpdate, ch, auditReasonAuto)
			}
		case "crypto":
			var qty float64
			_ = s.DB.QueryRowContext(ctx, `SELECT quantity_held FROM crypto_holdings WHERE id=?`, r.id).Scan(&qty)
			if changed(qty, e.TotalUnits, 1e-9) || !r.invested.Valid || changed(r.invested.Float64, round2(e.InvestedUSD), 1e-6) {
				if _, err := s.DB.ExecContext(ctx, `UPDATE crypto_holdings SET quantity_held=?, cost_basis_usd=?,
					avg_buy_usd=?, updated_at=strftime('%s','now') WHERE id=?`, e.TotalUnits, round2(e.InvestedUSD),
					nullIfZero(avg), r.id); err != nil {
					return out, err
				}
				out.valuesUpdated++
				ch := map[string]any{
					"quantityHeld": fromTo(sql.NullFloat64{Float64: qty, Valid: true}, e.TotalUnits),
					"costBasisUsd": fromTo(r.invested, round2(e.InvestedUSD)),
				}
				s.audit(ctx, auditActorSync, uid, "crypto", r.id, e.Ticker, store.AuditUpdate, ch, auditReasonAuto)
			}
		}
	}
	for kind, rowsByTicker := range map[string]map[string][]ftRow{"stock": stocks, "crypto": cryptos} {
		for t, rs := range rowsByTicker {
			if held[kind][t] {
				continue
			}
			for _, r := range rs {
				d := desired{kind: kind, action: "remove", ticker: t, name: r.name, holdingID: r.id,
					ftInvested: r.invested.Float64, thesis: r.thesis}
				want[d.key()] = d
			}
		}
	}

	// Upkeep of the queue.
	rows, err := s.DB.QueryContext(ctx, `SELECT id, kind, action, ticker, status FROM etoro_reconcile_proposals
		WHERE status IN ('pending','dismissed')`)
	if err != nil {
		return out, err
	}
	type live struct {
		id     int64
		status string
	}
	existing := map[string]live{}
	for rows.Next() {
		var l live
		var k, a, t string
		if err := rows.Scan(&l.id, &k, &a, &t, &l.status); err != nil {
			rows.Close()
			return out, err
		}
		existing[k+"|"+a+"|"+t] = l
	}
	rows.Close()

	for k, l := range existing {
		if _, still := want[k]; !still {
			// Condition cleared (bought/sold/fixed elsewhere): retire it, so a
			// later recurrence is proposed afresh.
			if _, err := s.DB.ExecContext(ctx, `UPDATE etoro_reconcile_proposals SET status='resolved', updated_at=?,
				note=coalesce(note,'') || CASE WHEN note IS NULL THEN '' ELSE '; ' END || 'condition cleared on sync'
				WHERE id=?`, now, l.id); err != nil {
				return out, err
			}
		}
	}
	for k, d := range want {
		if l, ok := existing[k]; ok {
			if l.status == "pending" {
				if _, err := s.DB.ExecContext(ctx, `UPDATE etoro_reconcile_proposals SET name=?, holding_id=?,
					etoro_units=?, etoro_invested_usd=?, etoro_avg_price=?, etoro_lots=?, ft_invested_usd=?,
					has_thesis_link=?, updated_at=? WHERE id=?`, d.name, nullIfZeroI(d.holdingID), d.units, d.invested,
					nullIfZero(d.avg), d.lots, d.ftInvested, b2i(d.thesis), now, l.id); err != nil {
					return out, err
				}
				out.pending++
			}
			continue // dismissed stays dismissed
		}
		if _, err := s.DB.ExecContext(ctx, `INSERT INTO etoro_reconcile_proposals (kind, action, ticker, name,
			holding_id, etoro_units, etoro_invested_usd, etoro_avg_price, etoro_lots, ft_invested_usd, has_thesis_link,
			status, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,'pending',?,?)`, d.kind, d.action, d.ticker,
			d.name, nullIfZeroI(d.holdingID), d.units, d.invested, nullIfZero(d.avg), d.lots, d.ftInvested,
			b2i(d.thesis), now, now); err != nil {
			return out, err
		}
		out.pending++
		out.created++
	}
	return out, nil
}

func nullIfZero(f float64) any {
	if f == 0 {
		return nil
	}
	return f
}

func nullIfZeroI(i int64) any {
	if i == 0 {
		return nil
	}
	return i
}

// ---------------------------------------------------------------------------
// Approve / dismiss

// Proposal is the API view of one queued change.
type Proposal struct {
	ID               int64    `json:"id"`
	Kind             string   `json:"kind"`
	Action           string   `json:"action"`
	Ticker           string   `json:"ticker"`
	Name             string   `json:"name"`
	HoldingID        *int64   `json:"holdingId,omitempty"`
	EtoroUnits       *float64 `json:"etoroUnits,omitempty"`
	EtoroInvestedUSD *float64 `json:"etoroInvestedUsd,omitempty"`
	EtoroAvgPrice    *float64 `json:"etoroAvgPrice,omitempty"`
	EtoroLots        *int64   `json:"etoroLots,omitempty"`
	FTInvestedUSD    *float64 `json:"ftInvestedUsd,omitempty"`
	HasThesisLink    bool     `json:"hasThesisLink"`
	Status           string   `json:"status"`
	CreatedAt        int64    `json:"createdAt"`
	Note             string   `json:"note,omitempty"`
}

// ErrProposalNotPending is returned when approving/dismissing a proposal that
// is no longer pending (already decided, or resolved by a later sync).
var ErrProposalNotPending = errors.New("proposal is no longer pending")

// Proposals returns queued changes; status "" means pending only.
func (s *Service) Proposals(ctx context.Context, status string) ([]Proposal, error) {
	if status == "" {
		status = "pending"
	}
	return s.listProposals(ctx, `WHERE status = ? ORDER BY action, kind, ticker`, status)
}

func (s *Service) listProposals(ctx context.Context, where string, args ...any) ([]Proposal, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, kind, action, ticker, coalesce(name,''), holding_id, etoro_units,
		etoro_invested_usd, etoro_avg_price, etoro_lots, ft_invested_usd, has_thesis_link, status, created_at,
		coalesce(note,'') FROM etoro_reconcile_proposals `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Proposal{}
	for rows.Next() {
		var p Proposal
		var hid, lots sql.NullInt64
		var units, inv, avg, ftinv sql.NullFloat64
		var th int
		if err := rows.Scan(&p.ID, &p.Kind, &p.Action, &p.Ticker, &p.Name, &hid, &units, &inv, &avg, &lots, &ftinv,
			&th, &p.Status, &p.CreatedAt, &p.Note); err != nil {
			return nil, err
		}
		p.HoldingID, p.EtoroLots = ni(hid), ni(lots)
		p.EtoroUnits, p.EtoroInvestedUSD, p.EtoroAvgPrice, p.FTInvestedUSD = nf(units), nf(inv), nf(avg), nf(ftinv)
		p.HasThesisLink = th != 0
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Service) getProposal(ctx context.Context, id int64) (*Proposal, error) {
	ps, err := s.listProposals(ctx, `WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	if len(ps) == 0 {
		return nil, sql.ErrNoRows
	}
	return &ps[0], nil
}

// Dismiss marks a pending proposal dismissed; it won't be re-proposed while its
// condition persists.
func (s *Service) Dismiss(ctx context.Context, id int64) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE etoro_reconcile_proposals SET status='dismissed', decided_at=?,
		updated_at=? WHERE id=? AND status='pending'`, s.Now().Unix(), s.Now().Unix(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrProposalNotPending
	}
	return nil
}

// Approve applies one pending proposal through FT's normal store functions.
// The condition is re-checked first: if it no longer holds (bought/sold since),
// the proposal is resolved instead of applied.
func (s *Service) Approve(ctx context.Context, id int64) (string, error) {
	if s.Store == nil {
		return "", errors.New("approve: store not configured")
	}
	p, err := s.getProposal(ctx, id)
	if err != nil {
		return "", err
	}
	if p.Status != "pending" {
		return "", ErrProposalNotPending
	}
	uid, err := s.userID(ctx)
	if err != nil {
		return "", err
	}
	stillValid, why, err := s.stillValid(ctx, uid, p)
	if err != nil {
		return "", err
	}
	now := s.Now().Unix()
	if !stillValid {
		_, err := s.DB.ExecContext(ctx, `UPDATE etoro_reconcile_proposals SET status='resolved', decided_at=?,
			updated_at=?, note=? WHERE id=?`, now, now, why, id)
		return "resolved: " + why, err
	}

	var msg string
	switch {
	case p.Kind == "stock" && p.Action == "add":
		tk := p.Ticker
		h := &domain.StockHolding{UserID: uid, Name: p.Name, Ticker: &tk}
		if p.EtoroInvestedUSD != nil {
			h.InvestedUSD = *p.EtoroInvestedUSD
		}
		if p.EtoroAvgPrice != nil {
			avg := *p.EtoroAvgPrice
			h.AvgOpenPrice = &avg
		}
		hid, err := s.Store.InsertStockHolding(ctx, h)
		if err != nil {
			return "", err
		}
		s.audit(ctx, auditActorHuman, uid, "stock", hid, p.Ticker, store.AuditCreate, map[string]any{
			"new":        map[string]any{"name": p.Name, "ticker": p.Ticker, "investedUsd": h.InvestedUSD, "avgOpenPrice": h.AvgOpenPrice},
			"proposalId": p.ID,
		}, auditReasonAdd)
		msg = fmt.Sprintf("added %s as stock holding %d", p.Ticker, hid)
	case p.Kind == "crypto" && p.Action == "add":
		w := EtoroWallet
		h := &domain.CryptoHolding{UserID: uid, Name: p.Name, Symbol: p.Ticker, Classification: "alt", Wallet: &w,
			CostBasisUSD: p.EtoroInvestedUSD, AvgBuyUSD: p.EtoroAvgPrice}
		if p.EtoroUnits != nil {
			h.QuantityHeld = *p.EtoroUnits
		}
		hid, err := s.Store.InsertCryptoHolding(ctx, h)
		if err != nil {
			return "", err
		}
		s.audit(ctx, auditActorHuman, uid, "crypto", hid, p.Ticker, store.AuditCreate, map[string]any{
			"new":        map[string]any{"name": p.Name, "symbol": p.Ticker, "wallet": EtoroWallet, "quantityHeld": h.QuantityHeld, "costBasisUsd": p.EtoroInvestedUSD},
			"proposalId": p.ID,
		}, auditReasonAdd)
		msg = fmt.Sprintf("added %s as eToro crypto holding %d", p.Ticker, hid)
	case p.Kind == "stock" && p.Action == "remove":
		if err := s.Store.SoftDeleteStockHolding(ctx, uid, *p.HoldingID); err != nil {
			return "", err
		}
		s.audit(ctx, auditActorHuman, uid, "stock", *p.HoldingID, p.Ticker, store.AuditSoftDelete, map[string]any{
			"proposalId": p.ID, "ftInvestedUsd": p.FTInvestedUSD, "hadThesisLink": p.HasThesisLink,
		}, auditReasonClose)
		msg = fmt.Sprintf("soft-deleted stock holding %d (%s); thesis links and history kept", *p.HoldingID, p.Ticker)
	case p.Kind == "crypto" && p.Action == "remove":
		if err := s.Store.SoftDeleteCryptoHolding(ctx, uid, *p.HoldingID); err != nil {
			return "", err
		}
		s.audit(ctx, auditActorHuman, uid, "crypto", *p.HoldingID, p.Ticker, store.AuditSoftDelete, map[string]any{
			"proposalId": p.ID, "ftInvestedUsd": p.FTInvestedUSD, "hadThesisLink": p.HasThesisLink,
		}, auditReasonClose)
		msg = fmt.Sprintf("soft-deleted eToro crypto holding %d (%s)", *p.HoldingID, p.Ticker)
	default:
		return "", fmt.Errorf("unknown proposal %s/%s", p.Kind, p.Action)
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE etoro_reconcile_proposals SET status='approved', decided_at=?, updated_at=?,
		note=? WHERE id=?`, now, now, msg, id)
	return msg, err
}

// stillValid re-checks a proposal against current data at approval time.
func (s *Service) stillValid(ctx context.Context, uid int64, p *Proposal) (bool, string, error) {
	var inEtoro int
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM etoro_holdings_effective WHERE ticker=? AND direction='long'`,
		p.Ticker).Scan(&inEtoro); err != nil {
		return false, "", err
	}
	var inFT int
	var q string
	var args []any
	if p.Kind == "stock" {
		q, args = `SELECT count(*) FROM stock_holdings WHERE user_id=? AND deleted_at IS NULL AND upper(trim(ticker))=?`, []any{uid, p.Ticker}
	} else {
		q, args = `SELECT count(*) FROM crypto_holdings WHERE user_id=? AND deleted_at IS NULL AND wallet=? AND upper(trim(symbol))=?`, []any{uid, EtoroWallet, p.Ticker}
	}
	if err := s.DB.QueryRowContext(ctx, q, args...).Scan(&inFT); err != nil {
		return false, "", err
	}
	switch p.Action {
	case "add":
		if inFT > 0 {
			return false, "FT already has " + p.Ticker, nil
		}
		if inEtoro == 0 {
			return false, p.Ticker + " is no longer held at eToro", nil
		}
	case "remove":
		if inEtoro > 0 {
			return false, p.Ticker + " is held at eToro again", nil
		}
		if p.HoldingID == nil {
			return false, "no FT holding to remove", nil
		}
		var live int
		table := "stock_holdings"
		if p.Kind == "crypto" {
			table = "crypto_holdings"
		}
		if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM `+table+` WHERE id=? AND deleted_at IS NULL`,
			*p.HoldingID).Scan(&live); err != nil {
			return false, "", err
		}
		if live == 0 {
			return false, "FT holding already removed", nil
		}
	}
	return true, "", nil
}
