package videolinks

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"ft/internal/market"
	"ft/internal/videoitems"
)

// Review-queue categories.
const (
	CatGroup      = "group"          // an asset class or basket, not a security: never linked
	CatCryptoPair = "crypto-pair"    // "ETH Bitcoin", "ethusd": the base asset is the proposal
	CatCryptoSym  = "crypto-symbol"  // an exact coin symbol FT already maps (CoinGecko ids)
	CatSymbolUnk  = "symbol-unknown" // written as a symbol (a coin, stock or ETF): FT has no mapping for it
	CatNear       = "near-match"     // within edit distance 3 of ONE alias name (a proposal, never applied)
	CatOther      = "person-or-unlisted"
)

var (
	groupRe = regexp.MustCompile(`(?i)\b(market|markets|index|indices|indexes|stocks|sector|sectors|banks|bonds?|yields?|dominance|coins|companies|equities|equity|names|etfs?|mag ?7|magnificent|pairs?|basket|treasur(?:y|ies)|commodit(?:y|ies)|currencies|miners)\b`)
	pairRe  = regexp.MustCompile(`(?i)^(.*?)(?:\s+(?:bitcoin|btc|usd|usdt)\s*(?:pairs?)?|\s*usd|\s+pairs?)$`)
	symRe   = regexp.MustCompile(`^[A-Za-z]{2,6}$`)
	// index names and commodities are not securities FT holds: group (never linked)
	indexRe     = regexp.MustCompile(`(?i)\b(dow jones|dow|nasdaq|s&p|sp ?500|russell|nikkei|ftse|dax|kospi|hang seng)\b`)
	commodityRe = regexp.MustCompile(`(?i)^(copper|gold|silver|oil|crude|natural gas|platinum|palladium|lithium|uranium|wheat|corn|sugar|coffee)$`)
	// well-known quote/stable symbols that are coins but not in the held-coin map; a proposal only
	stableSyms = map[string]bool{"USDC": true, "USDT": true, "DAI": true, "USDE": true, "FDUSD": true}
)

// QueueRow is one name still unresolved after extraction, with a category and a proposal for Fin to accept or reject.
type QueueRow struct {
	Name       string
	Items      int
	Cowen      int
	Jordi      int
	FirstSeen  string
	LastSeen   string
	Category   string
	Proposal   string // "TICKER (why)" or empty
	AliasLine  string // the one line that would be appended to ticker_aliases.tsv if approved
	ExampleVid string
	ExampleTs  int
}

// ReviewQueue lists every distinct unresolved tickers_named name, most frequent first. Nothing here is applied:
// the proposals are for a human (an approved row becomes one line in ticker_aliases.tsv).
func ReviewQueue(ctx context.Context, db *sql.DB, today string) ([]QueueRow, error) {
	ix, err := BuildIndex(ctx, db, nil)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT i.fields_json, COALESCE(i.resolution_json,''), d.source, d.published_at, d.video_id, i.ref_start
		FROM video_items i JOIN video_digests d ON d.id = i.digest_id WHERE i.block = 'tickers_named' ORDER BY d.published_at, i.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	agg := map[string]*QueueRow{}
	for rows.Next() {
		var fields, resj, src, pub, vid string
		var ts sql.NullInt64
		if err := rows.Scan(&fields, &resj, &src, &pub, &vid, &ts); err != nil {
			return nil, err
		}
		var r videoitems.Resolution
		_ = json.Unmarshal([]byte(resj), &r)
		if r.Status != "unresolved" {
			continue
		}
		var f struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal([]byte(fields), &f)
		name := strings.TrimSpace(f.Name)
		if name == "" {
			continue
		}
		key := videoitems.NormName(name)
		q := agg[key]
		if q == nil {
			q = &QueueRow{Name: name, FirstSeen: pub, ExampleVid: vid}
			if ts.Valid {
				q.ExampleTs = int(ts.Int64)
			}
			agg[key] = q
		}
		q.Items++
		if src == "cowen" {
			q.Cowen++
		} else {
			q.Jordi++
		}
		q.LastSeen = pub
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]QueueRow, 0, len(agg))
	for _, q := range agg {
		classify(q, ix, today)
		out = append(out, *q)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Items != out[j].Items {
			return out[i].Items > out[j].Items
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func classify(q *QueueRow, ix *Index, today string) {
	name := q.Name
	norm := videoitems.NormName(name)
	up := strings.ToUpper(strings.TrimSpace(name))
	// a pair or a pairing with Bitcoin: the base asset is the proposal
	if m := pairRe.FindStringSubmatch(name); m != nil && strings.TrimSpace(m[1]) != "" {
		base := strings.ToUpper(strings.TrimSpace(m[1]))
		if _, ok := market.SymbolToGeckoID[base]; ok || ix.Entity(base) != nil || stableSyms[base] {
			q.Category, q.Proposal = CatCryptoPair, base+" (base asset of a pair)"
			q.AliasLine = fmt.Sprintf("%s\t%s\tmanual-%s", base, strings.ToLower(name), today)
			return
		}
	}
	if strings.HasSuffix(norm, "usd") && len(norm) > 3 { // ethusd, btcusd
		base := strings.ToUpper(strings.TrimSuffix(norm, "usd"))
		if _, ok := market.SymbolToGeckoID[base]; ok || ix.Entity(base) != nil {
			q.Category, q.Proposal = CatCryptoPair, base+" (base asset of a pair)"
			q.AliasLine = fmt.Sprintf("%s\t%s\tmanual-%s", base, norm, today)
			return
		}
	}
	if groupRe.MatchString(name) || indexRe.MatchString(name) || commodityRe.MatchString(strings.TrimSpace(name)) {
		q.Category = CatGroup
		return
	}
	if symRe.MatchString(strings.TrimSpace(name)) {
		if _, ok := market.SymbolToGeckoID[up]; ok || stableSyms[up] {
			q.Category, q.Proposal = CatCryptoSym, up+" (exact coin symbol)"
			q.AliasLine = fmt.Sprintf("%s\t%s\tmanual-%s", up, strings.ToLower(name), today)
			return
		}
		if strings.TrimSpace(name) == up { // written as a symbol
			q.Category = CatSymbolUnk
			return
		}
	}
	if tks, matched, d, ok := ix.aliases.Fuzzy(name, 3); ok {
		q.Category, q.Proposal = CatNear, fmt.Sprintf("%s (near %q, edit distance %d)", tks[0], matched, d)
		q.AliasLine = fmt.Sprintf("%s\t%s\tmanual-%s", tks[0], norm, today)
		return
	}
	q.Category = CatOther
}

// WriteCSV writes the queue for review. The last two columns are for Fin: decision (approve / reject) and an
// optional corrected ticker.
func WriteCSV(w io.Writer, rows []QueueRow) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"name", "items", "cowen", "jordi", "first_seen", "last_seen", "category", "proposal", "alias_line_if_approved", "example", "decision", "corrected_ticker"}); err != nil {
		return err
	}
	for _, r := range rows {
		ex := ""
		if r.ExampleVid != "" && !strings.HasPrefix(r.ExampleVid, "22v-") {
			ex = fmt.Sprintf("https://youtu.be/%s?t=%d", r.ExampleVid, r.ExampleTs)
		}
		if err := cw.Write([]string{r.Name, fmt.Sprint(r.Items), fmt.Sprint(r.Cowen), fmt.Sprint(r.Jordi), r.FirstSeen, r.LastSeen,
			r.Category, r.Proposal, r.AliasLine, ex, "", ""}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
