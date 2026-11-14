// Package videolinks links what the commentators said (digest mentions and v5 ticker items) to what FT holds or
// watches (FT-146 / SC-80, P3). Deterministic and free: a ticker the resolver already found, an exact alias name,
// or an exact symbol. No fuzzy match is ever accepted here (fuzzy only produces review proposals), no LLM, and
// nothing in scoring, alerts or risk reads the result (D6: context only; D7: never sets the regime).
package videolinks

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"ft/internal/videoitems"
)

// Link kinds, strongest first.
const (
	KindHeld      = "held"
	KindWatchlist = "watchlist"
	KindUniverse  = "universe"
)

// Methods.
const (
	MethodResolution = "resolution" // the ticker v5 already resolved for the item
	MethodAlias      = "alias"      // an exact name from the alias file or an FT holding/watchlist name
	MethodSymbol     = "symbol"     // an exact all-caps symbol
)

// symbolStop lists all-caps words that are also tickers but, written as a bare name, are far more likely English
// or an abbreviation. A symbol match never uses them (an alias match still can).
var symbolStop = func() map[string]bool {
	m := map[string]bool{}
	for _, s := range strings.Fields(`ALL ARE ANY CAN FOR NOW ONE OUT NEW OLD TOP BIG LOW HIGH WELL OPEN REAL TRUE FAST FREE FUN GOOD LOVE MAIN
		MAN MAY MORE MOST NEXT ON OR OF IT IS AT BY GM GE AI US UK EU USD EUR GBP ETF IPO CEO GDP CPI PCE FED SEC DOJ
		PE VIX NAV ATH YTD QE QT EPS CTA LLM API GPU CPU SAAS TV PM AM Q1 Q2 Q3 Q4 H1 H2`) {
		m[s] = true
	}
	return m
}()

// Entity is something FT holds, watches or tracks.
type Entity struct {
	Ticker string
	Name   string
	InHeld bool
	InWatch bool
	InUniverse bool
}

// Index is the lookup built from FT's own tables plus the alias file.
type Index struct {
	byTicker map[string]*Entity
	byName   map[string][]string // normalised name -> tickers (entities' names and the alias file's)
	aliases  *videoitems.Aliases
}

// BuildIndex loads holdings, watchlist and the universe. Stock and crypto holdings are "held".
func BuildIndex(ctx context.Context, db *sql.DB, aliases *videoitems.Aliases) (*Index, error) {
	if aliases == nil {
		aliases = videoitems.Default
	}
	ix := &Index{byTicker: map[string]*Entity{}, byName: map[string][]string{}, aliases: aliases}
	add := func(ticker, name string, held, watch, uni bool) {
		t := strings.ToUpper(strings.TrimSpace(ticker))
		if t == "" {
			return
		}
		e := ix.byTicker[t]
		if e == nil {
			e = &Entity{Ticker: t, Name: strings.TrimSpace(name)}
			ix.byTicker[t] = e
		}
		e.InHeld = e.InHeld || held
		e.InWatch = e.InWatch || watch
		e.InUniverse = e.InUniverse || uni
		if n := videoitems.NormName(name); n != "" {
			ix.addName(n, t)
		}
	}
	queries := []struct {
		q            string
		held, w, uni bool
	}{
		{`SELECT ticker, COALESCE(name,'') FROM stock_holdings WHERE deleted_at IS NULL`, true, false, false},
		{`SELECT symbol, COALESCE(name,'') FROM crypto_holdings WHERE deleted_at IS NULL`, true, false, false},
		{`SELECT ticker, COALESCE(company_name,'') FROM watchlist WHERE deleted_at IS NULL`, false, true, false},
		{`SELECT ticker, COALESCE(company,'') FROM nexus_universe WHERE deleted_at IS NULL AND active = 1`, false, false, true},
	}
	for _, q := range queries {
		rows, err := db.QueryContext(ctx, q.q)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var t, n string
			if err := rows.Scan(&t, &n); err != nil {
				rows.Close()
				return nil, err
			}
			add(t, n, q.held, q.w, q.uni)
		}
		rows.Close()
	}
	return ix, nil
}

func (ix *Index) addName(n, ticker string) {
	for _, t := range ix.byName[n] {
		if t == ticker {
			return
		}
	}
	ix.byName[n] = append(ix.byName[n], ticker)
}

// Entity returns what FT knows about a ticker (nil when it is neither held, watched nor in the universe).
func (ix *Index) Entity(ticker string) *Entity { return ix.byTicker[strings.ToUpper(ticker)] }

// Match links a spoken name to a ticker FT holds, watches or tracks. It returns ok only when exactly one such
// ticker results; an ambiguous name is never guessed. typ is the mention type ("" for items).
func (ix *Index) Match(name, typ string) (ticker, method string, ok bool) {
	switch typ {
	case "macro", "index":
		return "", "", false // not securities FT holds
	}
	n := videoitems.NormName(name)
	if n == "" {
		return "", "", false
	}
	// 1. an exact name: FT's own holding/watchlist/universe names, then the alias file
	cands := map[string]bool{}
	for _, t := range ix.byName[n] {
		cands[t] = true
	}
	for _, t := range ix.aliases.Tickers(name) {
		if ix.byTicker[strings.ToUpper(t)] != nil {
			cands[strings.ToUpper(t)] = true
		}
	}
	if len(cands) == 1 {
		for t := range cands {
			return t, MethodAlias, true
		}
	}
	if len(cands) > 1 {
		return "", "", false
	}
	// 2. an exact all-caps symbol as written, three or more characters, not an everyday word
	raw := strings.TrimSpace(name)
	if len(raw) >= 3 && raw == strings.ToUpper(raw) && !symbolStop[raw] && ix.byTicker[raw] != nil {
		return raw, MethodSymbol, true
	}
	return "", "", false
}

// Result summarises a rebuild.
type Result struct {
	Mentions, MentionsLinked int
	Items, ItemsLinked       int
	Links                    int
	ByKind                   map[string]int
	ByMethod                 map[string]int
}

type linkRow struct {
	kind, id, digest int64
	srcKind          string
	ticker           string
	e                *Entity
	method           string
	gate             any
}

func kindOf(e *Entity) string {
	switch {
	case e.InHeld:
		return KindHeld
	case e.InWatch:
		return KindWatchlist
	}
	return KindUniverse
}

// Rebuild recomputes every link from scratch in one transaction (idempotent: the same data gives the same rows)
// and sets the existing video_mentions.held / watchlist flags for mentions that link to a ticker (NULL otherwise).
func Rebuild(ctx context.Context, db *sql.DB, now time.Time) (Result, error) {
	res := Result{ByKind: map[string]int{}, ByMethod: map[string]int{}}
	ix, err := BuildIndex(ctx, db, nil)
	if err != nil {
		return res, err
	}
	type mention struct {
		id, digest int64
		name, typ  string
	}
	var rows []linkRow
	mrows, err := db.QueryContext(ctx, `SELECT id, digest_id, name, COALESCE(type,'') FROM video_mentions ORDER BY id`)
	if err != nil {
		return res, err
	}
	var ms []mention
	for mrows.Next() {
		var m mention
		if err := mrows.Scan(&m.id, &m.digest, &m.name, &m.typ); err != nil {
			mrows.Close()
			return res, err
		}
		ms = append(ms, m)
	}
	mrows.Close()
	for _, m := range ms {
		res.Mentions++
		if t, method, ok := ix.Match(m.name, m.typ); ok {
			res.MentionsLinked++
			rows = append(rows, linkRow{srcKind: "mention", id: m.id, digest: m.digest, ticker: t, e: ix.Entity(t), method: method})
		}
	}
	irows, err := db.QueryContext(ctx, `SELECT id, digest_id, COALESCE(resolution_json,''), gate_class, fields_json FROM video_items WHERE block = 'tickers_named' ORDER BY id`)
	if err != nil {
		return res, err
	}
	type item struct {
		id, digest int64
		res, gate  string
		fields     string
	}
	var is []item
	for irows.Next() {
		var it item
		if err := irows.Scan(&it.id, &it.digest, &it.res, &it.gate, &it.fields); err != nil {
			irows.Close()
			return res, err
		}
		is = append(is, it)
	}
	irows.Close()
	for _, it := range is {
		res.Items++
		if it.gate == videoitems.ClassInvented || it.gate == videoitems.ClassNameMapping {
			continue // never linked: the grounding gate keeps these out of any signal
		}
		var r videoitems.Resolution
		if it.res != "" {
			_ = json.Unmarshal([]byte(it.res), &r)
		}
		ticker, method := "", ""
		if r.Status == "resolved" && r.Ticker != "" && ix.Entity(r.Ticker) != nil {
			ticker, method = strings.ToUpper(r.Ticker), MethodResolution
		} else if r.Status == "" || r.Status == "unresolved" {
			// not resolved by the extraction: an exact name or symbol may still be one FT holds
			var f struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal([]byte(it.fields), &f)
			if t, m, ok := ix.Match(f.Name, ""); ok {
				ticker, method = t, m
			}
		}
		if ticker == "" {
			continue
		}
		res.ItemsLinked++
		rows = append(rows, linkRow{srcKind: "item", id: it.id, digest: it.digest, ticker: ticker, e: ix.Entity(ticker), method: method, gate: it.gate})
	}

	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].srcKind != rows[j].srcKind {
			return rows[i].srcKind < rows[j].srcKind
		}
		if rows[i].id != rows[j].id {
			return rows[i].id < rows[j].id
		}
		return rows[i].ticker < rows[j].ticker
	})
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM video_links`); err != nil {
		return res, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE video_mentions SET held = NULL, watchlist = NULL`); err != nil {
		return res, err
	}
	for _, r := range rows {
		k := kindOf(r.e)
		if _, err := tx.ExecContext(ctx, `INSERT INTO video_links (source_kind, source_id, digest_id, ticker, link_kind, in_held, in_watch, method, gate_class, built_at)
			VALUES (?,?,?,?,?,?,?,?,?,?)`, r.srcKind, r.id, r.digest, r.ticker, k, b2i(r.e.InHeld), b2i(r.e.InWatch), r.method, r.gate, now.Unix()); err != nil {
			return res, err
		}
		res.Links++
		res.ByKind[k]++
		res.ByMethod[r.method]++
		if r.srcKind == "mention" {
			if _, err := tx.ExecContext(ctx, `UPDATE video_mentions SET held = ?, watchlist = ? WHERE id = ?`, b2i(r.e.InHeld), b2i(r.e.InWatch), r.id); err != nil {
				return res, err
			}
		}
	}
	return res, tx.Commit()
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
