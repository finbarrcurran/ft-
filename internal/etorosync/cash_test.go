package etorosync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"ft/internal/store"
)

// FT-142: the sync stores the Trading account's available cash; PullMoneyCash stores the eToro Money cash
// in its own currency and in USD. Read-only GETs with the key pair only.
func TestCashComponentsStored(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "ft.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method+" "+r.URL.Path)
		if r.Header.Get("x-api-key") != "app-key" || r.Header.Get("x-user-key") != "user-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v1/trading/info/portfolio":
			w.Write([]byte(`{"clientPortfolio":{"positions":[],"credit":17347.94,"bonusCredit":0,"mirrors":[]}}`))
		case "/api/v1/balances":
			if r.URL.Query().Get("accountTypes") != "Cash" {
				t.Errorf("balances must be filtered to Cash: %s", r.URL.RawQuery)
			}
			w.Write([]byte(`{"balances":[{"accountType":"Cash","balance":202.6,"currency":"EUR","displayBalance":223.47,"exchangeRate":1.10303,"equityDetails":{"available":202.6}}]}`))
		default:
			w.Write([]byte(`{"instrumentDisplayDatas":[]}`))
		}
	}))
	defer srv.Close()
	svc := New(st, srv.URL, "app-key", "user-key")
	svc.Now = func() time.Time { return time.Unix(1_800_000_000, 0) }

	if r := svc.Sync(ctx); r.Status != "ok" {
		t.Fatalf("sync: %+v", r)
	}
	if err := svc.PullMoneyCash(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetEtoroCash(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tr, mo := got[CashTrading], got[CashMoney]
	if tr.USD != 17347.94 || tr.Currency != "USD" || tr.Source != "etoro_api:portfolio.credit" || tr.AsOf != 1_800_000_000 {
		t.Fatalf("trading component: %+v", tr)
	}
	if mo.AmountNative != 202.6 || mo.Currency != "EUR" || mo.USD != 223.47 || mo.FxToUSD != 1.10303 || mo.Source != "etoro_api:balances" {
		t.Fatalf("money component: %+v", mo)
	}
	for _, m := range methods {
		if m[:3] != "GET" {
			t.Fatalf("only read-only GETs are allowed: %v", methods)
		}
	}
	// an absent credit field is not stored as zero cash
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"clientPortfolio":{"positions":[]}}`))
	}))
	defer srv2.Close()
	st2, _ := store.Open(filepath.Join(t.TempDir(), "ft2.db"))
	defer st2.Close()
	st2.Migrate()
	New(st2, srv2.URL, "app-key", "user-key").Sync(ctx)
	if g, _ := st2.GetEtoroCash(ctx); len(g) != 0 {
		t.Fatalf("no credit field must store nothing: %+v", g)
	}
}
