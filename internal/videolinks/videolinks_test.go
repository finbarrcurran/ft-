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
	item(2, "Lumentum", "invented", `{"name":"Lumentum","status":"resolved","ticker":"LITE"}`)  // invented: never linked
	item(2, "Some Proxy", "grounded", `{"name":"Some Proxy","status":"proxy","ticker":"PLTR"}`) // a proxy is not the asset
	item(2, "Tesla", "grounded", `{"name":"Tesla","status":"resolved","ticker":"TSLA"}`)        // resolved but FT holds/watches/tracks no TSLA
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
		{"Ford", "stock", "", ""},      // not held, watched or tracked
		{"S&P 500", "index", "", ""},   // type excluded
		{"inflation", "macro", "", ""}, // type excluded
		{"NOW", "stock", "", ""},       // stop-listed symbol
		{"now", "stock", "", ""},       // not all caps
		{"MU", "stock", "", ""},        // two characters is too short for a bare symbol
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
	add("energy stocks", 3)    // a group
	add("ETH Bitcoin", 2)      // a pair: base asset ETH
	add("ethusd", 1)           // a pair
	add("Ada", 4)              // an exact coin symbol FT maps
	add("USDC", 5)             // a stablecoin symbol
	add("XYZQ", 1)             // looks like a symbol, FT has no map for it
	add("Palunteer Inc", 1)    // within edit distance of an alias name -> a proposal, never applied
	add("Ray Dalio", 2)        // a person
	add("ETH Bitcoin pair", 1) // FT-146 PR B: a pair, not a group
	add("Dow Jones", 1)        // an index, not a person
	add("copper", 1)           // a commodity
	add("IGV", 1)              // written as a symbol (an ETF): symbol-unknown, not "crypto"
	add("GM", 1)
	rows, err := ReviewQueue(ctx, db, "2026-11-14")
	if err != nil {
		t.Fatal(err)
	}
	cat := map[string]QueueRow{}
	for _, r := range rows {
		cat[strings.ToLower(r.Name)] = r
	}
	want := map[string]string{"energy stocks": CatGroup, "eth bitcoin": CatCryptoPair, "ethusd": CatCryptoPair, "ada": CatCryptoSym,
		"usdc": CatCryptoSym, "xyzq": CatSymbolUnk, "ray dalio": CatOther,
		"eth bitcoin pair": CatCryptoPair, "dow jones": CatGroup, "copper": CatGroup, "igv": CatSymbolUnk, "gm": CatSymbolUnk}
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

// SC-81: the first 100-link audit found symbol-search resolutions of generic words ("energy" to Bloom Energy,
// "manufacturing" to TSMC). A search resolution is now trusted only when the spoken name starts the matched company
// name and is not a generic word; a conflict is never linked.
func TestTrustResolutionTightened(t *testing.T) {
	db := fixture(t)
	ctx := context.Background()
	if _, err := db.Exec(`INSERT INTO nexus_universe (ticker, company, theme, is_nexus, active, added_at) VALUES ('BE', 'Bloom Energy', 'power', 1, 1, 1)`); err != nil {
		t.Fatal(err)
	}
	add := func(name, gate, res string) {
		if _, err := db.Exec(`INSERT INTO video_items (run_id, digest_id, block, ord, fields_json, gate_class, resolution_json, signal_ok)
			VALUES ((SELECT id FROM video_item_runs WHERE digest_id = 2), 2, 'tickers_named', 9, ?, ?, ?, 1)`, fmt.Sprintf(`{"name":%q}`, name), gate, res); err != nil {
			t.Fatal(err)
		}
	}
	add("energy", "grounded", `{"name":"energy","status":"resolved","ticker":"BE","source":"search","matched":"Bloom Energy Corporation"}`)                      // generic word: no
	add("Bloom Energy", "grounded", `{"name":"Bloom Energy","status":"resolved","ticker":"BE","source":"search","matched":"Bloom Energy Corporation"}`)          // starts the company name: yes
	add("Micron Technology", "grounded", `{"name":"Micron Technology","status":"resolved","ticker":"MU","source":"search","matched":"Micron Technology, Inc."}`) // yes
	add("Technology", "grounded", `{"name":"Technology","status":"resolved","ticker":"MU","source":"search","matched":"Micron Technology, Inc."}`)               // not a prefix, generic: no
	add("Conflicted", "grounded", `{"name":"Conflicted","status":"resolved","ticker":"MU","source":"alias","conflict":true}`)                                    // conflict: no
	if _, err := Rebuild(ctx, db, time.Unix(1_800_000_000, 0)); err != nil {
		t.Fatal(err)
	}
	has := func(name, ticker string) bool {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM video_links l JOIN video_items i ON i.id = l.source_id AND l.source_kind = 'item'
			WHERE l.ticker = ? AND i.fields_json LIKE ?`, ticker, `%"name":"`+name+`"%`).Scan(&n)
		return n > 0
	}
	if has("energy", "BE") || has("Technology", "MU") || has("Conflicted", "MU") {
		t.Fatal("a generic word, a non-prefix search match or a conflict must not be linked")
	}
	if !has("Bloom Energy", "BE") || !has("Micron Technology", "MU") {
		t.Fatal("a search match that starts the company name must still link")
	}
}

func TestForTickerPanelRows(t *testing.T) {
	db := fixture(t)
	ctx := context.Background()
	ex := func(q string, a ...any) {
		t.Helper()
		if _, err := db.Exec(q, a...); err != nil {
			t.Fatalf("%v: %s", err, q)
		}
	}
	// vid2 (cowen, 2026-11-02) has a PLTR item "Palunteer" and a PLTR mention "PLTR"; give the item a cited span,
	// a direction flag and a transcript. vid1 (jordi, 2026-11-01) has a PLTR mention only.
	ex(`UPDATE video_items SET ref_start = 100, ref_end = 115, direction_verdict = 'contradicts', direction_view = 'net bearish' WHERE resolution_json LIKE '%PLTR%' AND gate_class = 'grounded'`)
	ex(`INSERT INTO video_transcripts (video_id, source, title, published_at, text, text_sha, fetched_at) VALUES ('vid2','cowen','T','2026-11-02', ?, 'x', 1)`,
		"[0s] intro words\n[100s] palantir is the one i would buy here today\n[130s] later words")
	ex(`UPDATE video_digests SET title = 'Weekly' WHERE id = 2`)
	// an older Jordi mention outside a 30-day window
	ex(`INSERT INTO video_digests (id, source, video_id, published_at, package_path, ingested_at) VALUES (3,'jordi','vid3','2026-06-01','/x',1)`)
	ex(`INSERT INTO video_mentions (digest_id, name, type, context, stance) VALUES (3,'Palantir','stock','Old take.','bullish')`)
	// a gate-held item for PLTR: counted as hidden, never shown
	ex(`INSERT INTO video_items (run_id, digest_id, block, ord, fields_json, gate_class, resolution_json, signal_ok)
		VALUES ((SELECT id FROM video_item_runs WHERE digest_id = 2), 2, 'tickers_named', 5, '{"name":"Palantir"}', 'invented', '{"name":"Palantir","status":"resolved","ticker":"PLTR"}', 0)`)
	if _, err := Rebuild(ctx, db, time.Unix(1_800_000_000, 0)); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	c, err := ForTicker(ctx, db, "pltr", 30, 10, now)
	if err != nil {
		t.Fatal(err)
	}
	if c.Ticker != "PLTR" || c.Total != 2 || c.Shown != 2 || c.Rows[0].VideoID != "vid2" || c.Rows[1].VideoID != "vid1" {
		t.Fatalf("one row per video, newest first, inside 30 days: %+v", c)
	}
	r := c.Rows[0] // the Cowen video: the item supplies the verbatim passage, the gate class and the direction flag
	if r.SnippetKind != "passage" || r.Snippet != "palantir is the one i would buy here today" || r.Direction != "contradicts" || r.Gate != "grounded" || r.CitedLoosely {
		t.Fatalf("item row: %+v", r)
	}
	if r.Link != "https://youtu.be/vid2?t=100" || r.TsSec == nil || *r.TsSec != 100 || r.Title != "Weekly" {
		t.Fatalf("deep link: %+v", r)
	}
	if n := c.Rows[1]; n.SnippetKind != "note" || n.Stance != "" && n.Source != "jordi" {
		t.Fatalf("mention row: %+v", n)
	}
	if c.Hidden != 1 {
		t.Fatalf("one gate-held item is counted, not shown: %d", c.Hidden)
	}
	if l := c.Last["jordi"]; l.Date != "2026-11-01" || l.DaysAgo != 13 {
		t.Fatalf("last Jordi: %+v", l)
	}
	// a wider window brings the old Jordi video in; the limit cuts rows but not the total
	wide, _ := ForTicker(ctx, db, "PLTR", 400, 1, now)
	if wide.Total != 3 || wide.Shown != 1 {
		t.Fatalf("window and limit: total %d shown %d", wide.Total, wide.Shown)
	}
	if none, _ := ForTicker(ctx, db, "NOPE", 90, 10, now); none.Total != 0 || len(none.Rows) != 0 || none.Context == "" {
		t.Fatalf("unknown ticker: %+v", none)
	}
}

// SC-82: a fuzzy resolution links only at edit distance 1.
func TestFuzzyResolutionOnlyAtDistanceOne(t *testing.T) {
	db := fixture(t)
	ctx := context.Background()
	add := func(name, matched, ticker string) {
		if _, err := db.Exec(`INSERT INTO video_items (run_id, digest_id, block, ord, fields_json, gate_class, resolution_json, signal_ok)
			VALUES ((SELECT id FROM video_item_runs WHERE digest_id = 2), 2, 'tickers_named', 9, ?, 'grounded', ?, 1)`,
			fmt.Sprintf(`{"name":%q}`, name), fmt.Sprintf(`{"name":%q,"status":"resolved","ticker":%q,"source":"fuzzy","matched":%q}`, name, ticker, matched)); err != nil {
			t.Fatal(err)
		}
	}
	add("Palantr", "palantir", "PLTR")   // one letter off: linked
	add("altcoin", "bitcoin", "BTC")     // distance 2: a different thing, never linked
	add("Palanteer", "palantir", "PLTR") // distance 2: waits for a reviewed alias
	if _, err := Rebuild(ctx, db, time.Unix(1_800_000_000, 0)); err != nil {
		t.Fatal(err)
	}
	has := func(name string) bool {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM video_links l JOIN video_items i ON i.id = l.source_id AND l.source_kind = 'item' WHERE i.fields_json LIKE ?`, `%"name":"`+name+`"%`).Scan(&n)
		return n > 0
	}
	if !has("Palantr") || has("altcoin") || has("Palanteer") {
		t.Fatalf("fuzzy rule: Palantr=%v altcoin=%v Palanteer=%v", has("Palantr"), has("altcoin"), has("Palanteer"))
	}
}
