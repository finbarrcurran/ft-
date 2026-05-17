package performance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"ft/internal/store"
	"log/slog"
	"strings"
	"time"
)

// DeriveResult is the summary from a derivation pass.
type DeriveResult struct {
	StartedAt     time.Time
	FinishedAt    time.Time
	AuditScanned  int
	CloseEvents   int
	Derived       int
	AlreadyExist  int // skipped due to UNIQUE on source_audit_close_id
	SkippedNoOpen int // pre-9c trades with no trade_snapshot_json
	Errors        []string
}

// DeriveAll walks the user's holdings_audit looking for closed-trade
// signals and inserts a row into `closed_trades` for each one not already
// present. Idempotent via the UNIQUE constraint on source_audit_close_id.
//
// v1 closure detection: ONE signal — action='soft_delete'. Subsequent
// specs may add partial-sell handling, but soft-delete is unambiguous and
// covers the most common "I sold the whole position" case.
//
// For each close event:
//  1. Walk back through earlier audit rows for the same holding_id to
//     find the most recent 'create'. That row's changes_json carries the
//     entry snapshot via the "trade_snapshot_json" key (Spec 9c D17).
//  2. Parse entry data. Skip silently when no trade_snapshot_json (trade
//     opened before Spec 9c shipped — partial data is worse than no data
//     for cohort analytics).
//  3. Read the holding's current state for the exit price (deleted holdings
//     still have their last-known current_price).
//  4. Compute realized P&L + R-multiple.
//  5. Insert. Returns ErrAlreadyDerived from the store when this close
//     event was already processed — counted but not reported as an error.
func DeriveAll(ctx context.Context, st *store.Store, userID int64) (*DeriveResult, error) {
	res := &DeriveResult{StartedAt: time.Now().UTC()}

	// Pull a generous slice of audit history. 1000 rows covers 5+ years
	// at typical trading cadence; the cost of scanning is trivial.
	audits, err := st.ListAudit(ctx, userID, 1000, 0)
	if err != nil {
		return res, err
	}
	res.AuditScanned = len(audits)

	// Build a map: holding_id → most recent 'create' audit row. Walk audits
	// newest-first; the first time we see a holding's create, we keep it.
	// Audits are already ordered ts DESC by ListAudit.
	mostRecentCreate := map[int64]*auditRef{}
	for _, a := range audits {
		if a.Action != "create" {
			continue
		}
		if _, ok := mostRecentCreate[a.HoldingID]; !ok {
			mostRecentCreate[a.HoldingID] = &auditRef{ID: a.ID, Timestamp: a.Timestamp, ChangesJSON: a.Changes, Kind: a.HoldingKind, HoldingID: a.HoldingID}
		}
	}

	// Find close events.
	for _, a := range audits {
		if a.Action != "soft_delete" {
			continue
		}
		res.CloseEvents++

		open := mostRecentCreate[a.HoldingID]
		if open == nil {
			// Position was opened before audit logging existed, or before
			// Spec 3 audit shipped. Skip silently — partial data is bad data.
			res.SkippedNoOpen++
			continue
		}
		// Parse the trade snapshot from the create's changes_json.
		snap, hasSnap := parseTradeSnapshot(open.ChangesJSON)
		if !hasSnap {
			slog.Warn("performance.DeriveAll: skipping pre-9c trade", "holdingId", a.HoldingID, "openAuditId", open.ID)
			res.SkippedNoOpen++
			continue
		}

		// Pull current state of the holding for exit price. Even when
		// soft-deleted, the row's current_price is preserved.
		exitPrice, _ := readExitPrice(ctx, st, a.HoldingKind, a.HoldingID)
		if exitPrice == 0 && snap.EntryPrice != 0 {
			// Fall back to entry — produces a zero-R trade rather than
			// crashing the derivation.
			exitPrice = snap.EntryPrice
			slog.Warn("performance.DeriveAll: no exit price, using entry", "holdingId", a.HoldingID)
		}

		ticker := ""
		if a.Ticker != nil {
			ticker = *a.Ticker
		} else if a.Symbol != nil {
			ticker = *a.Symbol
		}

		closeView := &holdingsAuditView{
			ID:        a.ID,
			Timestamp: a.Timestamp,
			Ticker:    a.Ticker,
			Symbol:    a.Symbol,
		}
		ct := buildClosedTrade(open, closeView, snap, exitPrice, ticker, a.HoldingKind)
		if err := insertClosedTrade(ctx, st, ct); err != nil {
			if errors.Is(err, store.ErrAlreadyExists) {
				res.AlreadyExist++
				continue
			}
			res.Errors = append(res.Errors, fmt.Sprintf("close audit %d: %s", a.ID, err.Error()))
			continue
		}
		res.Derived++
	}

	res.FinishedAt = time.Now().UTC()
	slog.Info("performance.DeriveAll done",
		"scanned", res.AuditScanned,
		"closeEvents", res.CloseEvents,
		"derived", res.Derived,
		"alreadyExist", res.AlreadyExist,
		"skippedNoOpen", res.SkippedNoOpen,
		"errs", len(res.Errors),
	)
	return res, nil
}

// auditRef is a tiny holder for the audit-row info we need during derivation.
type auditRef struct {
	ID          int64
	Timestamp   time.Time
	ChangesJSON string
	Kind        string
	HoldingID   int64
}

// tradeSnapshotIn is the in-memory shape of the trade_snapshot_json blob.
// Pointer fields so the parser can detect "field present but null" vs
// "field missing".
type tradeSnapshotIn struct {
	SetupType             *string  `json:"setup_type"`
	RegimeEffective       string   `json:"regime_effective"`
	Stage                 string   `json:"stage"`
	ATRWeekly             *float64 `json:"atr_weekly"`
	VolTierAuto           *string  `json:"vol_tier_auto"`
	Support1              *float64 `json:"support_1"`
	Resistance1           *float64 `json:"resistance_1"`
	Resistance2           *float64 `json:"resistance_2"`
	Entry                 *float64 `json:"entry"`
	SL                    *float64 `json:"sl"`
	TP1                   *float64 `json:"tp1"`
	TP2                   *float64 `json:"tp2"`
	PerTradeRiskPct       *float64 `json:"per_trade_risk_pct"`
	PortfolioValueAtEntry *float64 `json:"portfolio_value_at_entry"`
	JordiScore            *int     `json:"jordi_score"`
	PercocoScore          *int     `json:"percoco_score"`
	SuggestedSLPct        *float64 `json:"suggested_sl_pct"`
	SuggestedTPPct        *float64 `json:"suggested_tp_pct"`
}

// parsedSnap is the flat struct we hand to buildClosedTrade. All numbers
// resolved with sensible defaults (0 for missing optional fields).
type parsedSnap struct {
	SetupType       string
	RegimeEffective string
	JordiScore      *int
	PercocoScore    *int
	ATRWeekly       float64
	VolTierAuto     string
	Support1        float64
	Resistance1     float64
	Resistance2     float64
	EntryPrice      float64
	SL              float64
	TP1             float64
	TP2             float64
	PerTradeRiskPct float64
	PortfolioValue  float64
}

// parseTradeSnapshot extracts the `trade_snapshot_json` sub-object from
// the audit row's changes_json. Returns (snap, true) when present;
// (zero, false) when missing (pre-9c trade).
func parseTradeSnapshot(rawChanges string) (parsedSnap, bool) {
	if rawChanges == "" {
		return parsedSnap{}, false
	}
	var envelope struct {
		TradeSnap *tradeSnapshotIn `json:"trade_snapshot_json"`
	}
	if err := json.Unmarshal([]byte(rawChanges), &envelope); err != nil {
		return parsedSnap{}, false
	}
	if envelope.TradeSnap == nil {
		return parsedSnap{}, false
	}
	t := envelope.TradeSnap
	snap := parsedSnap{
		RegimeEffective: t.RegimeEffective,
		JordiScore:      t.JordiScore,
		PercocoScore:    t.PercocoScore,
	}
	if t.SetupType != nil {
		snap.SetupType = *t.SetupType
	}
	if t.VolTierAuto != nil {
		snap.VolTierAuto = *t.VolTierAuto
	}
	if t.ATRWeekly != nil {
		snap.ATRWeekly = *t.ATRWeekly
	}
	if t.Support1 != nil {
		snap.Support1 = *t.Support1
	}
	if t.Resistance1 != nil {
		snap.Resistance1 = *t.Resistance1
	}
	if t.Resistance2 != nil {
		snap.Resistance2 = *t.Resistance2
	}
	if t.Entry != nil {
		snap.EntryPrice = *t.Entry
	}
	if t.SL != nil {
		snap.SL = *t.SL
	}
	if t.TP1 != nil {
		snap.TP1 = *t.TP1
	}
	if t.TP2 != nil {
		snap.TP2 = *t.TP2
	}
	if t.PerTradeRiskPct != nil {
		snap.PerTradeRiskPct = *t.PerTradeRiskPct
	}
	if t.PortfolioValueAtEntry != nil {
		snap.PortfolioValue = *t.PortfolioValueAtEntry
	}
	return snap, true
}

// readExitPrice fetches the holding's current price (last-known, preserved
// even after soft-delete). Returns 0 + nil error when not retrievable.
func readExitPrice(ctx context.Context, st *store.Store, kind string, holdingID int64) (float64, error) {
	if kind == "stock" {
		h, err := st.GetStockHolding(ctx, 1, holdingID)
		if err != nil || h == nil || h.CurrentPrice == nil {
			return 0, nil
		}
		return *h.CurrentPrice, nil
	}
	c, err := st.GetCryptoHolding(ctx, 1, holdingID)
	if err != nil || c == nil || c.CurrentPriceUSD == nil {
		return 0, nil
	}
	return *c.CurrentPriceUSD, nil
}

// buildClosedTrade assembles the ClosedTrade from the open + close audit
// rows and the parsed trade snapshot. Pure-ish — does the math, no IO.
func buildClosedTrade(open *auditRef, close *holdingsAuditView, snap parsedSnap, exitPrice float64, ticker, kind string) ClosedTrade {

	holdingPeriod := int(close.Timestamp.Sub(open.Timestamp).Hours() / 24)
	if holdingPeriod < 0 {
		holdingPeriod = 0
	}

	risk := snap.EntryPrice - snap.SL
	rMult := 0.0
	if risk > 0 {
		rMult = (exitPrice - snap.EntryPrice) / risk
	}

	pnlPct := 0.0
	if snap.EntryPrice > 0 {
		pnlPct = (exitPrice - snap.EntryPrice) / snap.EntryPrice * 100
	}

	// Position size: estimated from entry × risk_pct math (we didn't store
	// size_units explicitly in the trade_snapshot_json; we can recover it
	// from risk_pct × portfolio_value ÷ (entry − stop)).
	posUnits, posUSD, riskUSD := 0.0, 0.0, 0.0
	if snap.PortfolioValue > 0 && snap.PerTradeRiskPct > 0 && risk > 0 {
		riskUSD = snap.PortfolioValue * snap.PerTradeRiskPct / 100
		posUnits = riskUSD / risk
		posUSD = posUnits * snap.EntryPrice
	}

	pnlUSD := posUnits * (exitPrice - snap.EntryPrice)

	// Determine exit reason. v1 heuristic: if exit ≤ SL, sl_hit; if exit
	// ≥ TP2, tp2_hit; if exit ≥ TP1, tp1_hit; else manual_close.
	exitReason := ExitManualClose
	switch {
	case snap.SL > 0 && exitPrice <= snap.SL:
		exitReason = ExitSLHit
	case snap.TP2 > 0 && exitPrice >= snap.TP2:
		exitReason = ExitTP2Hit
	case snap.TP1 > 0 && exitPrice >= snap.TP1:
		exitReason = ExitTP1Hit
	}

	// Planned R-multiples.
	plannedR1 := 0.0
	plannedR2 := 0.0
	if risk > 0 {
		if snap.TP1 > 0 {
			plannedR1 = (snap.TP1 - snap.EntryPrice) / risk
		}
		if snap.TP2 > 0 {
			plannedR2 = (snap.TP2 - snap.EntryPrice) / risk
		}
	}

	return ClosedTrade{
		Ticker:                strings.ToUpper(ticker),
		Kind:                  kind,
		HoldingID:             open.HoldingID,
		OpenedAt:              open.Timestamp,
		SetupType:             snap.SetupType,
		RegimeEffective:       snap.RegimeEffective,
		JordiScore:            snap.JordiScore,
		PercocoScore:          snap.PercocoScore,
		ATRWeeklyAtEntry:      snap.ATRWeekly,
		VolTierAtEntry:        snap.VolTierAuto,
		Support1AtEntry:       snap.Support1,
		Resistance1AtEntry:    snap.Resistance1,
		Resistance2AtEntry:    snap.Resistance2,
		EntryPrice:            snap.EntryPrice,
		SLAtEntry:             snap.SL,
		TP1AtEntry:            snap.TP1,
		TP2AtEntry:            snap.TP2,
		RMultipleTP1Planned:   plannedR1,
		RMultipleTP2Planned:   plannedR2,
		PositionSizeUnits:     posUnits,
		PositionSizeUSD:       posUSD,
		PerTradeRiskPct:       snap.PerTradeRiskPct,
		PerTradeRiskUSD:       riskUSD,
		PortfolioValueAtEntry: snap.PortfolioValue,
		ClosedAt:              close.Timestamp,
		ExitReason:            exitReason,
		ExitPriceAvg:          exitPrice,
		HoldingPeriodDays:     holdingPeriod,
		RealizedPnLUSD:        pnlUSD,
		RealizedPnLPct:        pnlPct,
		RealizedRMultiple:     rMult,
		SourceAuditOpenID:     open.ID,
		SourceAuditCloseID:    close.ID,
		DerivedAt:             time.Now().UTC(),
	}
}

// holdingsAuditView is a minimal shape so derive.go doesn't depend on the
// full domain.HoldingsAudit type (avoids circular import if domain ever
// needs to reach into performance).
type holdingsAuditView struct {
	ID        int64
	Timestamp time.Time
	Ticker    *string
	Symbol    *string
}

// insertClosedTrade is the store-IO arm. Defined here as a forward-decl
// so the file compiles independently of store-layer changes; the real
// impl lives in internal/store/performance.go.
func insertClosedTrade(ctx context.Context, st *store.Store, t ClosedTrade) error {
	return st.InsertClosedTrade(ctx, store.ClosedTradeRow{
		Ticker: t.Ticker, Kind: t.Kind, HoldingID: t.HoldingID,
		OpenedAt: t.OpenedAt, SetupType: t.SetupType, RegimeEffective: t.RegimeEffective,
		JordiScore: t.JordiScore, CowenScore: t.CowenScore, PercocoScore: t.PercocoScore,
		ATRWeeklyAtEntry: t.ATRWeeklyAtEntry, VolTierAtEntry: t.VolTierAtEntry,
		Support1AtEntry: t.Support1AtEntry, Resistance1AtEntry: t.Resistance1AtEntry,
		Resistance2AtEntry: t.Resistance2AtEntry,
		EntryPrice: t.EntryPrice, SLAtEntry: t.SLAtEntry,
		TP1AtEntry: t.TP1AtEntry, TP2AtEntry: t.TP2AtEntry,
		RMultipleTP1Planned: t.RMultipleTP1Planned, RMultipleTP2Planned: t.RMultipleTP2Planned,
		PositionSizeUnits: t.PositionSizeUnits, PositionSizeUSD: t.PositionSizeUSD,
		PerTradeRiskPct: t.PerTradeRiskPct, PerTradeRiskUSD: t.PerTradeRiskUSD,
		PortfolioValueAtEntry: t.PortfolioValueAtEntry,
		ClosedAt: t.ClosedAt, ExitReason: t.ExitReason, ExitPriceAvg: t.ExitPriceAvg,
		HoldingPeriodDays: t.HoldingPeriodDays,
		RealizedPnLUSD: t.RealizedPnLUSD, RealizedPnLPct: t.RealizedPnLPct,
		RealizedRMultiple: t.RealizedRMultiple,
		SourceAuditOpenID: t.SourceAuditOpenID, SourceAuditCloseID: t.SourceAuditCloseID,
		DerivedAt: t.DerivedAt,
	})
}
