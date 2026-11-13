package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ft/internal/store"
)

// FT-142: the Summary cash is eToro cash (Trading available + eToro Money) while fresh; the manual entry is the
// fallback; demo mode never shows either.
func TestEffectiveCash(t *testing.T) {
	s, st := snapServer(t)
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0)
	put := func(c store.EtoroCash) {
		if err := st.SetEtoroCash(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(t, st, `INSERT OR REPLACE INTO user_preferences (key, value, updated_at) VALUES ('cash_balance_usd','17571.53',0)`)

	if r := s.effectiveCash(ctx, now); r.Source != "manual" || r.USD != 17571.53 {
		t.Fatalf("nothing stored: %+v", r)
	}
	trading := store.EtoroCash{Component: "trading_available", AmountNative: 17347.94, Currency: "USD", USD: 17347.94, FxToUSD: 1, Source: "etoro_api:portfolio.credit", AsOf: now.Unix() - 600}
	money := store.EtoroCash{Component: "money_cash", AmountNative: 202.6, Currency: "EUR", USD: 223.47, FxToUSD: 1.10303, Source: "etoro_api:balances", AsOf: now.Unix() - 3600}
	put(trading)
	if r := s.effectiveCash(ctx, now); r.Source != "etoro" || r.USD != 17347.94 || !r.Partial {
		t.Fatalf("Trading only (no Money yet): %+v", r)
	}
	put(money)
	r := s.effectiveCash(ctx, now)
	if r.Source != "etoro" || r.USD != 17347.94+223.47 || r.Partial || len(r.Components) != 2 || r.AsOf != money.AsOf {
		t.Fatalf("both components: %+v", r)
	}
	money.AsOf = now.Unix() - 80*3600 // a missed night too many
	put(money)
	if r := s.effectiveCash(ctx, now); r.USD != 17347.94 || !r.Partial || !r.Components[1].Stale {
		t.Fatalf("stale Money cash is not counted: %+v", r)
	}
	trading.AsOf = now.Unix() - 49*3600 // the sync has not stored a value for two days
	put(trading)
	if r := s.effectiveCash(ctx, now); r.Source != "manual" || r.USD != 17571.53 {
		t.Fatalf("stale Trading cash falls back to the manual entry: %+v", r)
	}

	// the risk check uses the same figure
	trading.AsOf, money.AsOf = time.Now().Unix(), time.Now().Unix()
	put(trading)
	put(money)
	if got := s.riskCashUSD(ctx); got != 17347.94+223.47 {
		t.Fatalf("risk cash %v", got)
	}
}

func TestSummaryCashSourceAndDemo(t *testing.T) {
	s, st := snapServer(t)
	ctx := context.Background()
	now := time.Now().Unix()
	for _, c := range []store.EtoroCash{
		{Component: "trading_available", AmountNative: 17347.94, Currency: "USD", USD: 17347.94, FxToUSD: 1, Source: "etoro_api:portfolio.credit", AsOf: now},
		{Component: "money_cash", AmountNative: 202.6, Currency: "EUR", USD: 223.47, FxToUSD: 1.10303, Source: "etoro_api:balances", AsOf: now},
	} {
		if err := st.SetEtoroCash(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	call := func() string {
		rec := httptest.NewRecorder()
		s.handleSummary(rec, httptest.NewRequest(http.MethodGet, "/api/summary", nil).WithContext(context.WithValue(context.Background(), ctxUserID, int64(1))))
		return rec.Body.String()
	}
	b := call()
	if !strings.Contains(b, `"cashSource":"etoro"`) || !strings.Contains(b, "17571.41") || !strings.Contains(b, "money_cash") {
		t.Fatalf("summary: %.600s", b)
	}
	mustExec(t, st, `INSERT OR REPLACE INTO user_preferences (key, value, updated_at) VALUES ('demo_mode','on',0)`)
	b = call()
	if !strings.Contains(b, `"cashSource":"demo"`) || strings.Contains(b, "17347") || strings.Contains(b, "17571") || strings.Contains(b, "money_cash") || strings.Contains(b, "202.6") {
		t.Fatalf("demo summary leaks eToro cash: %.800s", b)
	}
}
