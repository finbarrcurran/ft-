package videolinks

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ft/internal/store"
)

func fixture(t *testing.T) *sql.DB {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "ft.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	ex := func(q string, a ...any) {
		t.Helper()
		if _, err := st.DB.Exec(q, a...); err != nil {
			t.Fatalf("%v: %s", err, q)
		}
	}
	ex(`INSERT INTO users (id, email, password_hash, created_at) VALUES (1, 'fin@example.test', 'x', 0)`)
	// held: a stock and a coin; watchlist: Micron; universe: Lumentum
	ex(`INSERT INTO stock_holdings (id, user_id, name, ticker, invested_usd, avg_open_price, current_price, updated_at) VALUES (1, 1, 'Palantir', 'PLTR', 100, 10, 12, 1)`)
	ex(`INSERT INTO crypto_holdings (id, user_id, name, symbol, classification, category, wallet, updated_at) VALUES (1, 1, 'Bitcoin', 'BTC', 'core', 'store', 'eToro', 1)`)
	ex(`DELETE FROM watchlist`)
	ex(`INSERT INTO watchlist (user_id, ticker, kind, company_name, added_at, updated_at) VALUES (1, 'MU', 'stock', 'Micron', 1, 1)`)
	ex(`DELETE FROM nexus_universe`)
	ex(`INSERT INTO nexus_universe (ticker, company, theme, is_nexus, active, added_at) VALUES ('LITE', 'Lumentum', 'optical', 1, 1, 1)`)
	for i, src := range []string{"jordi", "cowen"} {
		ex(`INSERT INTO video_digests (id, source, video_id, published_at, package_path, ingested_at) VALUES (?,?,?,?,'/x',1)`, i+1, src, fmt.Sprintf("vid%d", i+1), "2026-11-0"+fmt.Sprint(i+1))
	}
	men := func(digest int, name, typ string) {
		ex(`INSERT INTO video_mentions (digest_id, name, type) VALUES (?,?,?)`, digest, name, typ)
	}
	men(1, "Palantir", "stock")  // alias/holding name -> held
	men(1, "Bitcoin", "crypto")  // holding name -> held
	men(1, "Micron", "stock")    // watchlist
	men(1, "Lumentum", "stock")  // universe
	men(1, "Ford", "stock")      // known to the alias file but not held/watched/tracked -> no link
	men(1, "S&P 500", "index")   // never linked
	men(2, "inflation", "macro") // never linked
	men(2, "PLTR", "stock")      // exact all-caps symbol -> held
	men(2, "now", "stock")       // an everyday word, not a symbol
	men(2, "NOW", "stock")       // stop-listed symbol
	item := func(digest int, name, gate, res string) {
		ex(`INSERT INTO video_item_runs (digest_id, schema_version, model, items_total, status, created_at)
			SELECT ?,5,'m',0,'ok',1 WHERE NOT EXISTS (SELECT 1 FROM video_item_runs WHERE digest_id = ?)`, digest, digest)
		ex(`INSERT INTO video_items (run_id, digest_id, block, ord, fields_json, gate_class, resolution_json, signal_ok)
			VALUES ((SELECT id FROM video_item_runs WHERE digest_id = ?), ?, 'tickers_named', 1, ?, ?, ?, 1)`, digest, digest,
			fmt.Sprintf(`{"name":%q,"stance":"bullish"}`, name), gate, res)
	}
	item(2, "Palunteer", "grounded", `{"name":"Palunteer","status":"resolved","ticker":"PLTR"}`)
	item(2, "Micron", "wrong_place", `{"name":"Micron","status":"resolved","ticker":"MU"}`)
	item(2, "Lumentum", "invented", `{"name":"Lumentum","status":"resolved","ticker":"LITE"}`) // invented: never linked
	item(2, "Some Proxy", "grounded", `{"name":"Some Proxy","status":"proxy","ticker":"PLTR"}`) // a proxy is not the asset
	item(2, "Tesla", "grounded", `{"name":"Tesla","status":"resolved","ticker":"TSLA"}`)      // resolved but FT holds/watches/tracks no TSLA
	return st.DB
}

func TestMatchRules(t *testing.T) {
	db := fixture(t)
	ix, err := BuildIndex(context.Background(), db, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, typ, want, method string
	}{
		{"Palantir", "stock", "PLTR", MethodAlias},
		{"palantir", "", "PLTR", MethodAlias},
		{"Bitcoin", "crypto", "BTC", MethodAlias},
		{"PLTR", "stock", "PLTR", MethodSymbol},
		{"BTC", "crypto", "BTC", MethodSymbol},
		{"Ford", "stock", "", ""},          // not held, watched or tracked
		{"S&P 500", "index", "", ""},       // type excluded
		{"inflation", "macro", "", ""},     // type excluded
		{"NOW", "stock", "", ""},           // stop-listed symbol
		{"now", "stock", "", ""},           // not all caps
		{"MU", "stock", "", ""},            // two characters is too short for a bare symbol
		{"Micron", "stock", "MU", MethodAlias},
	} {
		got, m, ok := ix.Match(c.name, c.typ)
		if (c.want == "") == ok || got != c.want || m != c.method {
			t.Errorf("%q (%s): got %q %q ok=%v, want %q %q", c.name, c.typ, got, m, ok, c.want, c.method)
		}
	}
}

func TestRebuildLinksFlagsAndIsIdempotent(t *testing.T) {
	db := fixture(t)
	ctx := context.Background()
	r, err := Rebuild(ctx, db, time.Unix(1_800_000_000, 0))
	if err != nil {
		t.Fatal(err)
	}
	// mentions: Palantir, Bitcoin, Micron, Lumentum, PLTR link; Ford, S&P, inflation, now, NOW do not
	if r.Mentions != 10 || r.MentionsLinked != 5 {
		t.Fatalf("mentions: %+v", r)
	}
	// items: Palunteer (resolved PLTR) and Micron (wrong_place MU) link; invented, proxy and the unheld TSLA do not
	if r.Items != 5 || r.ItemsLinked != 2 {
		t.Fatalf("items: %+v", r)
	}
	if r.ByKind[KindHeld] != 4 || r.ByKind[KindWatchlist] != 2 || r.ByKind[KindUniverse] != 1 {
		t.Fatalf("by kind: %+v", r.ByKind)
	}
	var held, watch, nulls int
	db.QueryRow(`SELECT COALESCE(SUM(held),0), COALESCE(SUM(watchlist),0), SUM(held IS NULL) FROM video_mentions`).Scan(&held, &watch, &nulls)
	if held != 3 || watch != 1 || nulls != 5 { // PLTR x2 + Bitcoin held; Micron watchlist; five mentions unlinked stay NULL
		t.Fatalf("mention flags: held=%d watch=%d null=%d", held, watch, nulls)
	}
	var gate string
	if err := db.QueryRow(`SELECT gate_class FROM video_links WHERE source_kind='item' AND ticker='MU'`).Scan(&gate); err != nil || gate != "wrong_place" {
		t.Fatalf("a wrong_place item keeps its gate class for the panel: %q %v", gate, err)
	}
	dump := func() string {
		rows, _ := db.Query(`SELECT source_kind, source_id, digest_id, ticker, link_kind, in_held, in_watch, method, COALESCE(gate_class,'') FROM video_links ORDER BY id`)
		defer rows.Close()
		var b strings.Builder
		for rows.Next() {
			var a, tk, k, m, g string
			var id, d, h, w int
			rows.Scan(&a, &id, &d, &tk, &k, &h, &w, &m, &g)
			fmt.Fprintln(&b, a, id, d, tk, k, h, w, m, g)
		}
		return b.String()
	}
	first := dump()
	if _, err := Rebuild(ctx, db, time.Unix(1_800_000_100, 0)); err != nil {
		t.Fatal(err)
	}
	if second := dump(); second != first {
		t.Fatalf("a rebuild must reproduce the same links:\n%s\n---\n%s", first, second)
	}
}

func TestReviewQueueCategories(t *testing.T) {
	db := fixture(t)
	ctx := context.Background()
	add := func(name string, n int) {
		for i := 0; i < n; i++ {
			if _, err := db.Exec(`INSERT INTO video_items (run_id, digest_id, block, ord, fields_json, gate_class, resolution_json, ref_start, signal_ok)
				VALUES ((SELECT id FROM video_item_runs WHERE digest_id = 2), 2, 'tickers_named', 1, ?, 'grounded', ?, 100, 1)`,
				fmt.Sprintf(`{"name":%q}`, name), fmt.Sprintf(`{"name":%q,"status":"unresolved"}`, name)); err != nil {
				t.Fatal(err)
			}
		}
	}
	add("energy stocks", 3)   // a group
	add("ETH Bitcoin", 2)     // a pair: base asset ETH
	add("ethusd", 1)          // a pair
	add("Ada", 4)             // an exact coin symbol FT maps
	add("USDC", 5)            // a stablecoin symbol
	add("XYZQ", 1)            // looks like a symbol, FT has no map for it
	add("Palunteer Inc", 1)   // within edit distance of an alias name -> a proposal, never applied
	add("Ray Dalio", 2)       // a person
	rows, err := ReviewQueue(ctx, db, "2026-11-14")
	if err != nil {
		t.Fatal(err)
	}
	cat := map[string]QueueRow{}
	for _, r := range rows {
		cat[strings.ToLower(r.Name)] = r
	}
	want := map[string]string{"energy stocks": CatGroup, "eth bitcoin": CatCryptoPair, "ethusd": CatCryptoPair, "ada": CatCryptoSym,
		"usdc": CatCryptoSym, "xyzq": CatCryptoUnk, "ray dalio": CatOther}
	for name, c := range want {
		if cat[name].Category != c {
			t.Errorf("%q: category %q, want %q", name, cat[name].Category, c)
		}
	}
	if !strings.HasPrefix(cat["eth bitcoin"].Proposal, "ETH") || !strings.Contains(cat["ada"].AliasLine, "ADA\tada\tmanual-2026-11-14") {
		t.Errorf("proposals: %+v / %+v", cat["eth bitcoin"], cat["ada"])
	}
	if rows[0].Name != "USDC" || rows[0].Items != 5 {
		t.Errorf("most frequent first: %+v", rows[0])
	}
	// nothing was applied: no link was created from the queue
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM video_links`).Scan(&n)
	if n != 0 {
		t.Fatalf("the review queue must not write links: %d", n)
	}
	var buf bytes.Buffer
	if err := WriteCSV(&buf, rows); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(buf.String(), "name,items,cowen,jordi,first_seen,last_seen,category,proposal,alias_line_if_approved,example,decision,corrected_ticker\n") {
		t.Fatalf("csv header: %.200s", buf.String())
	}
}
