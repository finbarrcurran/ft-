package videolinks

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"ft/internal/videoitems"
)

// Row is one video that talked about a ticker, as the per-holding panel shows it (FT-146 PR B, FT-148 fixes). A video
// that has both a v5 item and a digest mention is one row. The snippet is ALWAYS about the ticker: it must contain the
// ticker, the company name, an alias or a spoken form (including ASR spellings such as "Salana") of it; when the cited
// passage or the digest note does not, the transcript window around the nearest real mention is used instead, and when
// the transcript has none the row says "mentioned in this video" with no snippet.
type Row struct {
	Date          string `json:"date"`
	Source        string `json:"source"` // jordi | cowen
	VideoID       string `json:"videoId"`
	Title         string `json:"title"`
	Link          string `json:"link,omitempty"`
	LinkKind      string `json:"linkKind"` // youtube | 22v | none
	TsSec         *int   `json:"tsSec,omitempty"`
	Stance        string `json:"stance,omitempty"`
	Snippet       string `json:"snippet,omitempty"`
	SnippetKind   string `json:"snippetKind"` // passage (verbatim from the transcript) | note (the digest's own summary) | none
	Gate          string `json:"gate,omitempty"`
	CitedLoosely  bool   `json:"citedLoosely,omitempty"` // wrong_place or far: the value is in the video but not at the cited second
	Direction     string `json:"direction,omitempty"`
	DirectionView string `json:"directionView,omitempty"`
	Method        string `json:"method"`
}

// Last says when each commentator last mentioned the ticker.
type Last struct {
	Date    string `json:"date"`
	DaysAgo int    `json:"daysAgo"`
	Stance  string `json:"stance,omitempty"`
}

// Commentary is the panel's payload.
type Commentary struct {
	Ticker  string          `json:"ticker"`
	Days    int             `json:"days"`
	Total   int             `json:"total"`  // videos in the window
	Shown   int             `json:"shown"`  // rows returned
	Hidden  int             `json:"hidden"` // items for this ticker held back by the grounding gate (invented, name_mapping)
	Last    map[string]Last `json:"last"`
	Rows    []Row           `json:"rows"`
	Context string          `json:"context"` // D6, stated on the panel itself
}

// ContextNote is shown with the panel: it is reading material, never an input to scoring or alerts (D6).
const ContextNote = "Context only: what the commentators said. It is not a score, a signal or advice."

var youtubeID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

type linkRef struct {
	kind                string
	id                  int64
	method, gate        string
	source, date, video string
	title, url          string
}

// ForTicker returns what Jordi and Cowen said about a ticker: the newest first, one row per video, within days,
// at most limit rows. now is a parameter so the recency is testable.
func ForTicker(ctx context.Context, db *sql.DB, ticker string, days, limit int, now time.Time) (*Commentary, error) {
	t := strings.ToUpper(strings.TrimSpace(ticker))
	if days <= 0 {
		days = 90
	}
	if days > 730 {
		days = 730
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	out := &Commentary{Ticker: t, Days: days, Last: map[string]Last{}, Rows: []Row{}, Context: ContextNote}
	rows, err := db.QueryContext(ctx, `SELECT l.source_kind, l.source_id, l.method, COALESCE(l.gate_class,''), d.source, d.published_at, d.video_id, COALESCE(d.title,''), COALESCE(d.url,'')
		FROM video_links l JOIN video_digests d ON d.id = l.digest_id
		WHERE l.ticker = ? ORDER BY d.published_at DESC, d.id DESC, l.source_kind DESC, l.source_id`, t)
	if err != nil {
		return nil, err
	}
	var all []linkRef
	for rows.Next() {
		var r linkRef
		if err := rows.Scan(&r.kind, &r.id, &r.method, &r.gate, &r.source, &r.date, &r.video, &r.title, &r.url); err != nil {
			rows.Close()
			return nil, err
		}
		all = append(all, r)
	}
	rows.Close()

	cutoff := now.UTC().AddDate(0, 0, -days).Format("2006-01-02")
	type vid struct {
		item, mention *linkRef
	}
	byVideo := map[string]*vid{}
	var order []string
	lastSeen := map[string]bool{}
	for i := range all {
		r := &all[i]
		if !lastSeen[r.source] { // the newest link of each commentator, whatever the window
			lastSeen[r.source] = true
			d, _ := time.Parse("2006-01-02", r.date)
			out.Last[r.source] = Last{Date: r.date, DaysAgo: int(now.UTC().Sub(d).Hours() / 24)}
		}
		if r.date < cutoff {
			continue
		}
		v := byVideo[r.video]
		if v == nil {
			v = &vid{}
			byVideo[r.video] = v
			order = append(order, r.video)
		}
		if r.kind == "item" && v.item == nil {
			v.item = r
		} else if r.kind == "mention" && v.mention == nil {
			v.mention = r
		}
	}
	out.Total = len(order)
	terms := termsFor(ctx, db, t)
	for _, key := range order {
		if len(out.Rows) >= limit {
			break
		}
		v := byVideo[key]
		base := v.item
		if base == nil {
			base = v.mention
		}
		out.Rows = append(out.Rows, buildRow(ctx, db, base, v.item, v.mention, terms))
	}
	out.Shown = len(out.Rows)
	// the stance of each commentator's newest mention, from the rows (or, if it is older than the window, left empty)
	for src, l := range out.Last {
		for _, r := range out.Rows {
			if r.Source == src && r.Date == l.Date {
				l.Stance = r.Stance
				out.Last[src] = l
				break
			}
		}
	}
	// items for this ticker the grounding gate holds back (never linked): counted so the hiding is visible
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM video_items i JOIN video_digests d ON d.id = i.digest_id
		WHERE i.block = 'tickers_named' AND i.gate_class IN ('invented','name_mapping') AND d.published_at >= ?
		AND UPPER(COALESCE(json_extract(i.resolution_json, '$.ticker'),'')) = ?`, cutoff, t).Scan(&out.Hidden)
	return out, nil
}

// buildRow assembles one panel row, enforcing that the snippet is about the ticker and that the link is real.
func buildRow(ctx context.Context, db *sql.DB, base, item, mention *linkRef, terms [][]string) Row {
	row := Row{Date: base.date, Source: base.source, VideoID: base.video, Title: base.title, Method: base.method, SnippetKind: "none"}
	is22v := strings.HasPrefix(base.video, "22v-")
	var words []videoitems.Word
	var toks []string // normalised tokens of the transcript
	var owner []int   // toks[i] belongs to words[owner[i]] (a hyphenated word is several tokens)
	loaded := false
	load := func() {
		if loaded {
			return
		}
		loaded = true
		var compact string
		if err := db.QueryRowContext(ctx, `SELECT text FROM video_transcripts WHERE video_id = ?`, base.video).Scan(&compact); err == nil {
			words = videoitems.WordsFromCompact(compact)
			for i, w := range words {
				for _, tk := range strings.Fields(normText(w.W)) {
					toks = append(toks, tk)
					owner = append(owner, i)
				}
			}
		}
	}
	var ts *int // the second the row points at; nil when unknown (or when it is the start of the video)
	setTS := func(sec int) {
		if !is22v && sec > 1 {
			s := sec
			ts = &s
		}
	}
	var itemStart, itemEnd = -1, -1
	if item != nil {
		var fields string
		var rs, re sql.NullInt64
		var dv, dview sql.NullString
		if err := db.QueryRowContext(ctx, `SELECT fields_json, ref_start, ref_end, direction_verdict, direction_view FROM video_items WHERE id = ?`, item.id).
			Scan(&fields, &rs, &re, &dv, &dview); err == nil {
			var f struct {
				Stance string `json:"stance"`
			}
			_ = json.Unmarshal([]byte(fields), &f)
			row.Stance = f.Stance
			row.Gate = item.gate
			row.CitedLoosely = item.gate == videoitems.ClassWrongPlace || item.gate == videoitems.ClassFar
			row.Direction, row.DirectionView = dv.String, dview.String
			if rs.Valid && re.Valid {
				itemStart, itemEnd = int(rs.Int64), int(re.Int64)
			}
		}
	}
	var note string
	var mentionTS = -1
	if mention != nil {
		var stance string
		var mts sql.NullInt64
		if err := db.QueryRowContext(ctx, `SELECT COALESCE(context,''), COALESCE(stance,''), ts_sec FROM video_mentions WHERE id = ?`, mention.id).Scan(&note, &stance, &mts); err == nil {
			if row.Stance == "" {
				row.Stance = stance
			}
			if mts.Valid {
				mentionTS = int(mts.Int64)
			}
		}
	}

	// 1. the cited passage, if it is about the ticker
	if itemStart >= 0 {
		load()
		var b []string
		for _, w := range words {
			if w.T >= itemStart && w.T <= itemEnd+10 {
				b = append(b, w.W)
				if len(b) == 60 {
					break
				}
			}
		}
		if len(b) > 0 && containsTerm(strings.Fields(normText(strings.Join(b, " "))), terms) {
			row.Snippet, row.SnippetKind = strings.Join(b, " "), "passage"
			setTS(itemStart)
		}
	}
	// 2. the digest note, if it is about the ticker
	if row.SnippetKind == "none" && note != "" && containsTerm(strings.Fields(normText(note)), terms) {
		row.Snippet, row.SnippetKind = note, "note"
		if mentionTS >= 0 {
			setTS(mentionTS)
		} else if itemStart >= 0 {
			setTS(itemStart)
		}
	}
	// 3. the transcript window around the nearest real mention (to the cited span or the mention time)
	if row.SnippetKind == "none" {
		load()
		target := -1
		if itemStart >= 0 {
			target = itemStart
		} else if mentionTS >= 0 {
			target = mentionTS
		}
		if ti := nearestTerm(words, toks, owner, terms, target, is22v); ti >= 0 {
			idx := owner[ti]
			lo, hi := idx-10, idx+50
			if lo < 0 {
				lo = 0
			}
			if hi > len(words) {
				hi = len(words)
			}
			var b []string
			for _, w := range words[lo:hi] {
				b = append(b, w.W)
			}
			row.Snippet, row.SnippetKind = strings.Join(b, " "), "passage"
			setTS(words[idx].T)
		}
	}
	// 4. nothing about the ticker could be shown: say so, point at the video, show no snippet
	row.TsSec = ts
	switch {
	case is22v:
		if base.url != "" {
			row.Link, row.LinkKind = base.url, "22v" // the 22v page, never a youtu.be link and never a timestamp
		} else {
			row.LinkKind = "none"
		}
	case youtubeID.MatchString(base.video):
		row.Link, row.LinkKind = "https://youtu.be/"+base.video, "youtube"
		if ts != nil {
			row.Link = fmt.Sprintf("https://youtu.be/%s?t=%d", base.video, *ts)
		}
	default:
		row.LinkKind = "none"
	}
	return row
}

// termsFor collects the phrases that identify a ticker in text: the symbol (3+ letters), FT's own company names,
// the alias file's names and every spoken form the commentators used for it in the links (ASR spellings included).
func termsFor(ctx context.Context, db *sql.DB, ticker string) [][]string {
	seen := map[string]bool{}
	var out [][]string
	add := func(s string) {
		n := normText(s)
		if len(n) < 3 || seen[n] || genericWords[n] {
			return
		}
		seen[n] = true
		out = append(out, strings.Fields(n))
	}
	add(strings.SplitN(ticker, ".", 2)[0])
	for _, n := range videoitems.Default.Names(ticker) {
		add(n)
	}
	for _, q := range []string{
		`SELECT name FROM stock_holdings WHERE UPPER(ticker) = ?1 AND deleted_at IS NULL`,
		`SELECT name FROM crypto_holdings WHERE UPPER(symbol) = ?1 AND deleted_at IS NULL`,
		`SELECT company_name FROM watchlist WHERE UPPER(ticker) = ?1 AND deleted_at IS NULL`,
		`SELECT company FROM nexus_universe WHERE UPPER(ticker) = ?1`,
		`SELECT DISTINCT m.name FROM video_links l JOIN video_mentions m ON l.source_kind = 'mention' AND m.id = l.source_id WHERE l.ticker = ?1`,
		`SELECT DISTINCT json_extract(i.fields_json, '$.name') FROM video_links l JOIN video_items i ON l.source_kind = 'item' AND i.id = l.source_id WHERE l.ticker = ?1`,
	} {
		rows, err := db.QueryContext(ctx, q, ticker)
		if err != nil {
			continue
		}
		for rows.Next() {
			var n sql.NullString
			if rows.Scan(&n) == nil && n.Valid {
				add(n.String)
			}
		}
		rows.Close()
	}
	return out
}

var nonWord = regexp.MustCompile(`[^a-z0-9& ]+`)

// normText lower-cases and strips punctuation the same way for terms and for text.
func normText(s string) string {
	return strings.Join(strings.Fields(nonWord.ReplaceAllString(strings.ToLower(s), " ")), " ")
}

// containsTerm reports whether the token sequence contains any term phrase as consecutive tokens.
func containsTerm(toks []string, terms [][]string) bool {
	for i := range toks {
		if matchAt(toks, i, terms) {
			return true
		}
	}
	return false
}

func matchAt(toks []string, i int, terms [][]string) bool {
	for _, t := range terms {
		if i+len(t) > len(toks) {
			continue
		}
		ok := true
		for j, w := range t {
			if toks[i+j] != w {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// nearestTerm returns the token index where a term starts that is nearest (in time) to target, or the
// first one when there is no usable time (target < 0, or a transcript without time markers such as 22v). -1 if none.
func nearestTerm(words []videoitems.Word, toks []string, owner []int, terms [][]string, target int, untimed bool) int {
	best, bestD := -1, 1<<30
	for i := range toks {
		if !matchAt(toks, i, terms) {
			continue
		}
		if untimed || target < 0 {
			return i
		}
		d := words[owner[i]].T - target
		if d < 0 {
			d = -d
		}
		if d < bestD {
			best, bestD = i, d
		}
	}
	return best
}
