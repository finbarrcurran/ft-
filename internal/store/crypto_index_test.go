package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCryptoIndexSeed(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "ft.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ms, err := st.ListCryptoIndexMembers(ctx, "visser46")
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 46 {
		t.Fatalf("members = %d, want 46", len(ms))
	}
	tokens, stocks := 0, 0
	verts := map[string]int{}
	for _, m := range ms {
		verts[m.Vertical]++
		if m.ListVersion != "2026-09-21" {
			t.Errorf("%s list_version %s", m.Symbol, m.ListVersion)
		}
		switch m.AssetKind {
		case "token":
			tokens++
			if m.CoinGeckoID == nil || *m.CoinGeckoID == "" {
				t.Errorf("%s: token without a CoinGecko id", m.Symbol)
			}
		case "stock":
			stocks++
			if m.StockTicker == nil || m.CoinGeckoID != nil {
				t.Errorf("%s: stock row malformed", m.Symbol)
			}
		}
	}
	if tokens != 42 || stocks != 4 {
		t.Fatalf("tokens/stocks = %d/%d, want 42/4", tokens, stocks)
	}
	want := map[string]int{
		"AI Agents & Machine Economy": 3, "DePIN, Storage & Physical Networks": 8, "Digital Monetary Assets": 3,
		"Exchange, Brokerage & Market Structure": 6, "Lending, Credit & Rates": 4, "Oracle, Data & Interop Middleware": 4,
		"Settlement & Smart Contract Rails": 8, "Stablecoin & Synthetic-Dollar Systems": 3,
		"Staking, Restaking & Validation": 3, "Tokenization & RWA Rails": 4,
	}
	if len(verts) != 10 {
		t.Fatalf("verticals = %d, want 10: %v", len(verts), verts)
	}
	for v, n := range want {
		if verts[v] != n {
			t.Errorf("vertical %q = %d, want %d", v, verts[v], n)
		}
	}
	// re-seeding adds nothing (the UNIQUE key + INSERT OR IGNORE of the migration)
	if _, err := st.DB.Exec(`INSERT OR IGNORE INTO crypto_index_members (index_key, list_version, symbol, name, vertical, asset_kind, coingecko_id)
		VALUES ('visser46','2026-09-21','BTC','Bitcoin','Digital Monetary Assets','token','bitcoin')`); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = st.DB.QueryRow(`SELECT COUNT(*) FROM crypto_index_members`).Scan(&n)
	if n != 46 {
		t.Fatalf("after re-seed %d rows", n)
	}
	// the migration touches no other table: every statement targets crypto_index_members
	sqlText, err := os.ReadFile("migrations/0063_crypto_index_members.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(sqlText), "\n") {
		l := strings.ToUpper(strings.TrimSpace(line))
		for _, kw := range []string{"INSERT ", "UPDATE ", "DELETE ", "ALTER ", "DROP ", "CREATE TABLE", "CREATE INDEX"} {
			if strings.HasPrefix(l, kw) && !strings.Contains(l, "CRYPTO_INDEX_MEMBERS") && !strings.Contains(l, "IDX_CIM") && !strings.HasPrefix(l, "INSERT OR IGNORE INTO CRYPTO") {
				t.Errorf("statement outside crypto_index_members: %s", line)
			}
		}
	}
}

func TestCryptoIndexStockExchanges(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "ft.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	ms, _ := st.ListCryptoIndexMembers(context.Background(), "visser46")
	want := map[string]string{"COIN": "NASDAQ", "HOOD": "NASDAQ", "CRCL": "NYSE", "FIGR": "NASDAQ"}
	for _, m := range ms {
		if w, ok := want[m.Symbol]; ok {
			if m.Exchange == nil || *m.Exchange != w {
				t.Errorf("%s exchange = %v, want %s", m.Symbol, m.Exchange, w)
			}
			delete(want, m.Symbol)
		}
	}
	if len(want) != 0 {
		t.Errorf("stocks missing: %v", want)
	}
}
