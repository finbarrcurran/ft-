package server

import (
	"context"
	"strconv"
	"time"

	"ft/internal/etorosync"
	"ft/internal/store"
)

// FT-142 / SC-73 — the Summary cash is eToro cash: the Trading account's available cash plus the eToro Money
// cash account, two stored components. The manual cash entry (preferences) is the fallback, used and labelled
// "manual" when the Trading component is older than etoroCashFreshTrading.
const (
	etoroCashFreshTrading = 48 * time.Hour // the sync stores it every 30 minutes
	etoroCashFreshMoney   = 72 * time.Hour // one pull a day; a missed night is tolerated
)

// cashComponent is one part of the effective cash, as the Summary payload shows it.
type cashComponent struct {
	Name     string  `json:"name"` // trading_available | money_cash
	USD      float64 `json:"usd"`
	Native   float64 `json:"native"`
	Currency string  `json:"currency"`
	Source   string  `json:"source"`
	AsOf     string  `json:"asOf"` // RFC3339 UTC
	Stale    bool    `json:"stale,omitempty"`
}

// effectiveCashResult is the cash figure and where it came from.
type effectiveCashResult struct {
	USD        float64
	Source     string // etoro | manual
	AsOf       int64  // the older of the components' times (0 for manual)
	Components []cashComponent
	Partial    bool // Money cash missing or stale: the figure is the Trading component alone
}

// effectiveCash returns the eToro cash while the Trading component is fresh, else the manual cash entry.
// Demo mode never calls it (the caller has its own synthetic figure).
func (s *Server) effectiveCash(ctx context.Context, now time.Time) effectiveCashResult {
	if comps, err := s.store.GetEtoroCash(ctx); err == nil {
		if t, ok := comps[etorosync.CashTrading]; ok && now.Sub(time.Unix(t.AsOf, 0)) <= etoroCashFreshTrading {
			return combineEtoroCash(comps, now)
		}
	}
	v, _ := s.store.GetPreference(ctx, "cash_balance_usd")
	f, _ := strconv.ParseFloat(v, 64)
	return effectiveCashResult{USD: f, Source: "manual"}
}

func combineEtoroCash(comps map[string]store.EtoroCash, now time.Time) effectiveCashResult {
	t := comps[etorosync.CashTrading]
	res := effectiveCashResult{Source: "etoro", USD: t.USD, AsOf: t.AsOf}
	res.Components = append(res.Components, cashComponent{Name: t.Component, USD: t.USD, Native: t.AmountNative,
		Currency: t.Currency, Source: t.Source, AsOf: time.Unix(t.AsOf, 0).UTC().Format(time.RFC3339)})
	m, ok := comps[etorosync.CashMoney]
	switch {
	case !ok:
		res.Partial = true
	case now.Sub(time.Unix(m.AsOf, 0)) > etoroCashFreshMoney:
		res.Partial = true
		res.Components = append(res.Components, cashComponent{Name: m.Component, USD: m.USD, Native: m.AmountNative,
			Currency: m.Currency, Source: m.Source, AsOf: time.Unix(m.AsOf, 0).UTC().Format(time.RFC3339), Stale: true})
	default:
		res.USD += m.USD
		if m.AsOf < res.AsOf {
			res.AsOf = m.AsOf
		}
		res.Components = append(res.Components, cashComponent{Name: m.Component, USD: m.USD, Native: m.AmountNative,
			Currency: m.Currency, Source: m.Source, AsOf: time.Unix(m.AsOf, 0).UTC().Format(time.RFC3339)})
	}
	return res
}

// PullEtoroMoneyCash is the daily job: one read-only balances call. A failure is logged; the stored value
// ages out (after 72 h the Money component is dropped from the figure and the card says so).
func (s *Server) PullEtoroMoneyCash(ctx context.Context) error {
	if !s.etoroSync.Configured() {
		return nil
	}
	return s.etoroSync.PullMoneyCash(ctx)
}

// recordHistoryCash stores the day's cash on the value-history row with its source label (FT-142): "etoro_api:nightly"
// when the figure is eToro cash, "manual" when it is the manual entry.
func (s *Server) recordHistoryCash(ctx context.Context, date string, cashUSD float64) {
	src := "manual"
	if s.effectiveCash(ctx, time.Now()).Source == "etoro" {
		src = "etoro_api:nightly"
	}
	_ = s.store.SetPortfolioCashSource(ctx, date, cashUSD, src)
}
