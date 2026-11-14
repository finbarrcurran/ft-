package videolinks

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"
)

// FT-148: what the panel shows must be about the ticker and must link to a real place.
type panelFx struct {
	t  *testing.T
	db *sql.DB
	n  int64
}

func newPanelFx(t *testing.T) *panelFx { return &panelFx{t: t, db: fixture(t), n: 10} }

func (f *panelFx) ex(q string, a ...any) {
	f.t.Helper()
	if _, err := f.db.Exec(q, a...); err != nil {
		f.t.Fatalf("%v: %s", err, q)
	}
}

// video adds a digest (and its transcript) for source on date; returns the digest id.
func (f *panelFx) video(videoID, source, date, url, transcript string) int64 {
	f.n++
	f.ex(`INSERT INTO video_digests (id, source, video_id, title, url, published_at, package_path, ingested_at) VALUES (?,?,?,?,?,?,'/x',1)`, f.n, source, videoID, "T "+videoID, url, date)
	if transcript != "" {
		f.ex(`INSERT INTO video_transcripts (video_id, source, title, published_at, text, text_sha, fetched_at) VALUES (?,?,?,?,?,'x',1)`, videoID, source, "T", date, transcript)
	}
	f.ex(`INSERT INTO video_item_runs (digest_id, schema_version, model, items_total, status, created_at) VALUES (?,5,'m',0,'ok',1)`, f.n)
	return f.n
}

func (f *panelFx) mention(dig int64, name, note string, ts any) {
	f.ex(`INSERT INTO video_mentions (digest_id, name, type, context, stance, ts_sec) VALUES (?,?,'stock',?,'bullish',?)`, dig, name, note, ts)
}

func (f *panelFx) item(dig int64, name string, start, end int, gate string) {
	f.ex(`INSERT INTO video_items (run_id, digest_id, block, ord, fields_json, ref_start, ref_end, gate_class, resolution_json, signal_ok)
		VALUES ((SELECT id FROM video_item_runs WHERE digest_id = ?), ?, 'tickers_named', 1, ?, ?, ?, ?, ?, 1)`, dig, dig,
		fmt.Sprintf(`{"name":%q,"stance":"bullish"}`, name), start, end, gate, fmt.Sprintf(`{"name":%q,"status":"resolved","ticker":"PLTR","source":"alias"}`, name))
}

func (f *panelFx) rows(ticker string) map[string]Row {
	f.t.Helper()
	if _, err := Rebuild(context.Background(), f.db, time.Unix(1_800_000_000, 0)); err != nil {
		f.t.Fatal(err)
	}
	c, err := ForTicker(context.Background(), f.db, ticker, 400, 50, time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC))
	if err != nil {
		f.t.Fatal(err)
	}
	out := map[string]Row{}
	for _, r := range c.Rows {
		out[r.VideoID] = r
	}
	return out
}

func TestPanelLinksNeverBuildBadURLs(t *testing.T) {
	f := newPanelFx(t)
	tr := "[0s] intro\n[100s] palantir is the one\n[130s] later words"
	a := f.video("AAAAAAAAAAA", "jordi", "2026-11-05", "", tr) // a normal second
	f.item(a, "Palantir", 100, 115, "grounded")
	z := f.video("BBBBBBBBBBB", "jordi", "2026-11-04", "", "[0s] palantir opens the show\n[30s] more")
	f.item(z, "Palantir", 0, 15, "grounded") // t = 0
	o := f.video("CCCCCCCCCCC", "cowen", "2026-11-03", "", "[1s] palantir again\n[30s] more")
	f.item(o, "Palantir", 1, 16, "grounded") // t = 1
	v := f.video("22v-2538", "jordi", "2026-11-11", "https://ai.22vresearch.com/video-stream/2538/", "palantir was discussed here with no time markers at all")
	f.mention(v, "Palantir", "Palantir came up in the intraweek update.", nil)
	bad := f.video("22v-9999", "jordi", "2026-11-10", "", "palantir again")
	f.mention(bad, "Palantir", "Palantir again.", nil)
	odd := f.video("short-id", "cowen", "2026-11-02", "", "[50s] palantir")
	f.item(odd, "Palantir", 50, 60, "grounded")

	r := f.rows("PLTR")
	if got := r["AAAAAAAAAAA"]; got.Link != "https://youtu.be/AAAAAAAAAAA?t=100" || got.TsSec == nil || *got.TsSec != 100 || got.LinkKind != "youtube" {
		t.Errorf("a real second keeps it: %+v", got)
	}
	for _, id := range []string{"BBBBBBBBBBB", "CCCCCCCCCCC"} {
		if got := r[id]; got.Link != "https://youtu.be/"+id || got.TsSec != nil || strings.Contains(got.Link, "t=") {
			t.Errorf("%s: t=0 and t=1 mean no timestamp, link without t=: %+v", id, got)
		}
	}
	got := r["22v-2538"]
	if got.Link != "https://ai.22vresearch.com/video-stream/2538/" || got.LinkKind != "22v" || got.TsSec != nil || strings.Contains(got.Link, "youtu") {
		t.Errorf("a 22v row links to the 22v page, never youtu.be/22v-…: %+v", got)
	}
	if got := r["22v-9999"]; got.Link != "" || got.LinkKind != "none" {
		t.Errorf("a 22v digest with no page URL has no link: %+v", got)
	}
	if got := r["short-id"]; got.Link != "" || got.LinkKind != "none" {
		t.Errorf("an id that is not a YouTube id gets no link: %+v", got)
	}
	for id, row := range r {
		if strings.Contains(row.Link, "youtu.be/22v-") {
			t.Errorf("%s: dead 22v YouTube link %q", id, row.Link)
		}
	}
}

func TestPanelSnippetMustBeAboutTheTicker(t *testing.T) {
	f := newPanelFx(t)
	// 1. the cited span is about something else; the name is spoken 40 s later: the window around the real mention is used
	a := f.video("AAAAAAAAAAA", "jordi", "2026-11-09", "", "[0s] intro\n[100s] we talk about oil prices and the fed\n[140s] palantir reported strong numbers today\n[200s] bye")
	f.item(a, "Palantir", 100, 115, "wrong_place")
	// 2. the cited span is about something else and the video never says the name: no snippet, "mentioned in this video"
	b := f.video("BBBBBBBBBBB", "jordi", "2026-11-08", "", "[0s] intro\n[100s] oil prices and the fed again\n[200s] bye")
	f.item(b, "Palantir", 100, 115, "far")
	// 3. an ASR spelling in the spoken name counts: the passage says "palanteer", the item was linked as "Palanteer"
	c := f.video("CCCCCCCCCCC", "cowen", "2026-11-07", "", "[0s] intro\n[100s] palanteer is bouncing off the line\n[200s] bye")
	f.item(c, "Palanteer", 100, 115, "grounded")
	// 4. a mention whose digest note is about it: the note is shown; one whose note is not and the transcript is silent: none
	d := f.video("DDDDDDDDDDD", "cowen", "2026-11-06", "", "[0s] nothing relevant here\n[50s] still nothing")
	f.mention(d, "Palantir", "Valuation looks stretched.", 50)
	e := f.video("EEEEEEEEEEE", "cowen", "2026-11-05", "", "[0s] intro\n[50s] the pltr chart looks fine\n[90s] bye")
	f.mention(e, "Palantir", "Chart looks fine.", 50)
	g := f.video("GGGGGGGGGGG", "jordi", "2026-11-04", "", "[0s] intro")
	f.mention(g, "Palantir", "Palantir had a strong week.", 30)
	// 5. 22v: no time markers; the first mention in the text is the window; no timestamp, the 22v page
	v := f.video("22v-2504", "jordi", "2026-11-06", "https://ai.22vresearch.com/video-stream/2504/",
		"opening words about the market then palantir was one of the names he liked and more words follow here")
	f.mention(v, "Palantir", "Rotation talk.", nil)

	r := f.rows("PLTR")
	if got := r["AAAAAAAAAAA"]; got.SnippetKind != "passage" || !strings.Contains(strings.ToLower(got.Snippet), "palantir reported strong numbers") || got.TsSec == nil || *got.TsSec != 140 ||
		got.Link != "https://youtu.be/AAAAAAAAAAA?t=140" || !got.CitedLoosely {
		t.Errorf("1 (window around the real mention): %+v", got)
	}
	if got := r["BBBBBBBBBBB"]; got.SnippetKind != "none" || got.Snippet != "" || got.TsSec != nil || got.Link != "https://youtu.be/BBBBBBBBBBB" {
		t.Errorf("2 (never an unrelated passage): %+v", got)
	}
	if got := r["CCCCCCCCCCC"]; got.SnippetKind != "passage" || !strings.Contains(got.Snippet, "palanteer") {
		t.Errorf("3 (ASR spelling): %+v", got)
	}
	if got := r["DDDDDDDDDDD"]; got.SnippetKind != "none" || got.Snippet != "" {
		t.Errorf("4a (note without the name, silent transcript): %+v", got)
	}
	if got := r["EEEEEEEEEEE"]; got.SnippetKind != "passage" || !strings.Contains(got.Snippet, "pltr chart") || got.TsSec == nil || *got.TsSec != 50 {
		t.Errorf("4b (the symbol in the transcript): %+v", got)
	}
	if got := r["GGGGGGGGGGG"]; got.SnippetKind != "note" || got.Snippet != "Palantir had a strong week." {
		t.Errorf("4c (a note that names it): %+v", got)
	}
	if got := r["22v-2504"]; got.SnippetKind != "passage" || !strings.Contains(got.Snippet, "palantir was one of the names") || got.TsSec != nil || got.LinkKind != "22v" {
		t.Errorf("5 (untimed 22v transcript): %+v", got)
	}
	// the rule, for every row: a snippet is only ever shown when it names the ticker
	terms := termsFor(context.Background(), f.db, "PLTR")
	for id, row := range r {
		if row.SnippetKind == "none" {
			continue
		}
		if !containsTerm(strings.Fields(normText(row.Snippet)), terms) {
			t.Errorf("%s: the snippet %q does not mention the ticker", id, row.Snippet)
		}
	}
}

// The flags the panel shows come from the payload: a contradicting direction, a loosely cited item and the hidden count.
func TestPanelPayloadCarriesDirectionLooseAndHidden(t *testing.T) {
	f := newPanelFx(t)
	a := f.video("AAAAAAAAAAA", "jordi", "2026-11-09", "", "[0s] intro\n[100s] palantir is a core holding for me\n[200s] bye")
	f.item(a, "Palantir", 100, 115, "wrong_place")
	f.ex(`UPDATE video_items SET direction_verdict = 'contradicts', direction_view = 'net bearish on software' WHERE digest_id = ?`, a)
	f.ex(`INSERT INTO video_items (run_id, digest_id, block, ord, fields_json, gate_class, resolution_json, signal_ok)
		VALUES ((SELECT id FROM video_item_runs WHERE digest_id = ?), ?, 'tickers_named', 5, '{"name":"Palantir"}', 'invented', '{"name":"Palantir","status":"resolved","ticker":"PLTR"}', 0)`, a, a)
	f.ex(`INSERT INTO video_items (run_id, digest_id, block, ord, fields_json, gate_class, resolution_json, signal_ok)
		VALUES ((SELECT id FROM video_item_runs WHERE digest_id = ?), ?, 'tickers_named', 6, '{"name":"Palantir"}', 'name_mapping', '{"name":"Palantir","status":"resolved","ticker":"PLTR"}', 0)`, a, a)
	if _, err := Rebuild(context.Background(), f.db, time.Unix(1_800_000_000, 0)); err != nil {
		t.Fatal(err)
	}
	c, err := ForTicker(context.Background(), f.db, "PLTR", 400, 10, time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	r := c.Rows[0]
	if r.Direction != "contradicts" || r.DirectionView != "net bearish on software" || !r.CitedLoosely || r.Gate != "wrong_place" || c.Hidden != 2 {
		t.Fatalf("direction=%q view=%q loose=%v gate=%q hidden=%d", r.Direction, r.DirectionView, r.CitedLoosely, r.Gate, c.Hidden)
	}
}
