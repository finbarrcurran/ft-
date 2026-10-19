package videodigest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"ft/internal/llm"
	"ft/internal/store"
)

const sampleVTT = `WEBVTT
Kind: captions
Language: en

00:00:00.000 --> 00:00:03.000
hello and <c>welcome</c> to the show

00:00:03.000 --> 00:00:03.010
hello and welcome to the show

00:00:31.000 --> 00:00:34.000 align:start position:0%
hello and welcome to the show
bitcoin is above the 200 day moving average [Music]

00:00:40.000 --> 00:00:44.000
and liquidity is the whole story this week
`

func TestCompactTranscript(t *testing.T) {
	got := CompactTranscript(sampleVTT)
	want := "[0s] hello and welcome to the show\n[31s] bitcoin is above the 200 day moving average and liquidity is the whole story this week"
	if got != want {
		t.Fatalf("CompactTranscript:\n got %q\nwant %q", got, want)
	}
}

// AC4: the Cowen prompt must request only the (b) snapshot fields — never the
// (c) columns that are left for P2 frame OCR.
func TestCowenPromptExcludesCFields(t *testing.T) {
	p := strings.ToLower(systemPrompt("cowen"))
	for _, banned := range []string{"dominance", "200-week", "200 week", "200wma", "eth", "log band", "log_band", "log regression"} {
		if strings.Contains(p, banned) {
			t.Errorf("cowen prompt mentions (c)-list field %q", banned)
		}
	}
	for _, want := range []string{"risk_indicator_band", "btc_vs_200dma", "mvrv_z_band", "cycle_phase", "macro_recession_flag"} {
		if !strings.Contains(p, want) {
			t.Errorf("cowen prompt missing (b)-list field %q", want)
		}
	}
	if strings.Contains(strings.ToLower(systemPrompt("jordi")), "snapshot") {
		t.Error("jordi prompt should not request the Cowen snapshot")
	}
}

func TestParseExtractionSanitises(t *testing.T) {
	raw := "```json\n" + `{"executive_summary":" s ","themes":["a"," ",""],
	 "mentions":[{"name":"BTC","type":"CRYPTO","stance":"very bullish","context":"c","ts_sec":999999}],
	 "notable_quotes":[{"quote_text":"“liquidity is the whole story”","ts_sec":40},{"quote_text":"never said this at all","ts_sec":1}],
	 "suggested_regime":"shifting","snapshot":{"btc_vs_200dma":"sideways","btc_dominance_level":"high"}}` + "\n```"
	ex, err := ParseExtraction(raw, CompactTranscript(sampleVTT), "cowen")
	if err != nil {
		t.Fatal(err)
	}
	if *ex.ExecutiveSummary != "s" || len(ex.Themes) != 1 {
		t.Errorf("summary/themes not trimmed: %q %v", *ex.ExecutiveSummary, ex.Themes)
	}
	m := ex.Mentions[0]
	if *m.Type != "crypto" || m.Stance != nil || clampTs(m.TsSec, 600) != nil {
		t.Errorf("mention not sanitised: type=%v stance=%v ts=%v", *m.Type, m.Stance, clampTs(m.TsSec, 600))
	}
	if len(ex.NotableQuotes) != 1 || ex.QuotesRejected != 1 || ex.RejectedQuotes[0] != "never said this at all" {
		t.Errorf("quotes: kept %d rejected %d %v, want 1/1", len(ex.NotableQuotes), ex.QuotesRejected, ex.RejectedQuotes)
	}
	if ts := ex.NotableQuotes[0].TsSec; ts == nil || *ts != 31 {
		t.Errorf("quote ts should come from where it occurs (31s), got %v", ts)
	}
	if ex.ProposedRegime != nil || ex.Snapshot.BTCvs200DMA != nil || *ex.Snapshot.Confidence != "unstated" {
		t.Errorf("enums: legacy key must not produce a proposal; dma=%v conf=%v", ex.Snapshot.BTCvs200DMA, *ex.Snapshot.Confidence)
	}
}

func TestGroundingAndFillers(t *testing.T) {
	segs := segments("[0s] welcome back everyone\n[30s] so bitcoin is uh above the two hundred day\n" +
		"[60s] now the ten year treasury yield is at five percent\n[90s] bitcoin again here")
	at := func(name string, guess *float64) any {
		if g := groundMention(name, guess, segs); g != nil {
			return *g
		}
		return nil
	}
	late, early := 85.0, 5.0
	if got := at("Bitcoin", &late); got != 90 {
		t.Errorf("Bitcoin near 85s grounded to %v, want 90 (nearest occurrence)", got)
	}
	if got := at("Bitcoin", &early); got != 30 {
		t.Errorf("Bitcoin near 5s grounded to %v, want 30", got)
	}
	if got := at("U.S. 10-year Treasury yield", nil); got != 60 {
		t.Errorf("multi-word name grounded to %v, want 60", got)
	}
	if got := at("Ethereum", &early); got != nil {
		t.Errorf("unspoken name must get no timestamp, got %v", got)
	}
	if ts, ok := quoteTs("Bitcoin is above the two hundred day", segs); !ok || ts != 30 {
		t.Errorf("quote with filler removed: ok=%v ts=%d, want true/30", ok, ts)
	}
	if _, ok := quoteTs("bitcoin is below the two hundred day", segs); ok {
		t.Error("altered quote must not verify")
	}
}

func writePackage(t *testing.T, root, source, date, vid, title string, flagship bool, frames int) {
	t.Helper()
	dir := filepath.Join(root, source, date+"_"+vid)
	if err := os.MkdirAll(filepath.Join(dir, "frames"), 0o755); err != nil {
		t.Fatal(err)
	}
	var fr []map[string]any
	for i := 0; i < frames; i++ {
		name := filepath.Join("frames", "f_"+string(rune('1'+i))+"0.jpg")
		os.WriteFile(filepath.Join(dir, name), []byte("jpg"), 0o644)
		fr = append(fr, map[string]any{"file": name, "ts_sec": 10 * (i + 1), "origin": "scene", "cue_text": nil})
	}
	meta := map[string]any{"schema_version": 1, "source": source, "video_id": vid, "title": title,
		"url": "https://youtu.be/" + vid, "published_at": date, "duration_sec": 600, "is_flagship": flagship,
		"transcript_method": "captions", "transcript_caption_track": "en-orig", "frames": fr,
		"frame_params":     map[string]any{"scene_threshold": 0.1, "phash_max_distance": 4},
		"pipeline_version": "0.2.1", "status": "complete"}
	b, _ := json.Marshal(meta)
	os.WriteFile(filepath.Join(dir, "meta.json"), b, 0o644)
	os.WriteFile(filepath.Join(dir, "transcript.vtt"), []byte(sampleVTT), 0o644)
	os.WriteFile(filepath.Join(dir, "transcript.txt"), []byte("hello and welcome to the show bitcoin is above the 200 day moving average and liquidity is the whole story this week"), 0o644)
}

const jordiReply = `{"executive_summary":"Liquidity drives everything.","themes":["liquidity","AI capex"],
 "mentions":[{"name":"Bitcoin","type":"crypto","context":"He likes it.","stance":"bullish","ts_sec":31},
             {"name":"10Y","type":"macro","context":"Yields matter.","stance":"weird","ts_sec":5000}],
 "notable_quotes":[{"quote_text":"liquidity is the whole story","ts_sec":40},{"quote_text":"invented quote here","ts_sec":1}],
 "regime_read":"Risk-on.","suggested_regime":"Stable","theme_changes_vs_prior_week":["AI capex is new"]}`

const cowenReply = `{"executive_summary":"Bitcoin above the 200DMA.","themes":["BTC trend"],
 "mentions":[{"name":"Bitcoin","type":"crypto","context":"Above 200DMA.","stance":"watching","ts_sec":31}],
 "notable_quotes":[],"regime_read":null,"suggested_regime":null,
 "snapshot":{"btc_vs_200dma":"Above","risk_indicator_band":null,"btc_dominance_level":"58%"}}`

type stub struct {
	mu    sync.Mutex
	users []string
}

func (s *stub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body struct {
		System   []struct{ Text string }    `json:"system"`
		Messages []struct{ Content string } `json:"messages"`
	}
	b, _ := io.ReadAll(r.Body)
	json.Unmarshal(b, &body)
	s.mu.Lock()
	s.users = append(s.users, body.Messages[0].Content)
	s.mu.Unlock()
	reply := jordiReply
	if strings.Contains(body.System[0].Text, "Benjamin Cowen") {
		reply = cowenReply
	}
	json.NewEncoder(w).Encode(map[string]any{
		"content":     []map[string]string{{"type": "text", "text": reply}},
		"stop_reason": "end_turn",
		"usage":       map[string]int{"input_tokens": 1200, "output_tokens": 300},
	})
}

func TestIngestEndToEnd(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	st, err := store.Open(filepath.Join(tmp, "ft.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	sb := &stub{}
	srv := httptest.NewServer(sb)
	defer srv.Close()
	l := llm.NewService(st, "test-key")
	l.APIBaseURL = srv.URL

	root := filepath.Join(tmp, "packages")
	writePackage(t, root, "jordi", "2026-09-20", "JordiWeek2", "Week two", false, 2)
	writePackage(t, root, "jordi", "2026-09-13", "JordiWeek1", "Week one", false, 3)
	writePackage(t, root, "cowen", "2026-09-23", "CowenWeek1", "Cowen flagship", true, 1)
	os.MkdirAll(filepath.Join(root, "cowen", ".partial-Unfinished1"), 0o755) // must be ignored
	svc := New(st.DB, l, root)

	res, err := svc.Sweep(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 3 {
		t.Fatalf("sweep results = %d, want 3: %+v", len(res), res)
	}
	for _, r := range res {
		if r.Status != "ingested" {
			t.Fatalf("%s: %s %s", r.VideoID, r.Status, r.Error)
		}
	}
	if res[0].VideoID != "JordiWeek1" || res[1].VideoID != "JordiWeek2" {
		t.Errorf("not oldest-first: %s, %s", res[0].VideoID, res[1].VideoID)
	}
	// Prior-week context: only the second Jordi video gets it (AC5 mechanism).
	if strings.Contains(sb.users[0], "PRIOR VIDEO CONTEXT") || !strings.Contains(sb.users[1], `PRIOR VIDEO CONTEXT (2026-09-13, "Week one")`) {
		t.Errorf("prior context wiring wrong:\n1st: %.120q\n2nd: %.160q", sb.users[0], sb.users[1])
	}
	if !strings.Contains(sb.users[0], "[31s] bitcoin is above") {
		t.Errorf("user prompt should carry the compact timed transcript: %.200q", sb.users[0])
	}
	if res[1].PriorDigestID != res[0].DigestID || res[0].QuotesRejected != 1 || res[0].Quotes != 1 {
		t.Errorf("jordi 2 prior=%d want %d; quotes kept=%d rejected=%d", res[1].PriorDigestID, res[0].DigestID, res[0].Quotes, res[0].QuotesRejected)
	}

	var changes1, changes2 *string
	var flag int
	st.DB.QueryRow(`SELECT theme_changes_vs_prior_week, is_flagship FROM video_digests WHERE video_id='JordiWeek1'`).Scan(&changes1, &flag)
	st.DB.QueryRow(`SELECT theme_changes_vs_prior_week FROM video_digests WHERE video_id='JordiWeek2'`).Scan(&changes2)
	if changes1 != nil || changes2 == nil || flag != 1 {
		t.Errorf("theme changes: first=%v (want NULL) second=%v (want set); jordi flagship=%d (want 1)", changes1, changes2, flag)
	}
	var dma, conf string
	var dom *string
	st.DB.QueryRow(`SELECT btc_vs_200dma, confidence, btc_dominance_level FROM cowen_weekly_snapshot`).Scan(&dma, &conf, &dom)
	if dma != "above" || conf != "unstated" || dom != nil {
		t.Errorf("cowen snapshot: dma=%q conf=%q dominance=%v (want above/unstated/NULL)", dma, conf, dom)
	}
	var nFrames, nMentions, badTs int
	st.DB.QueryRow(`SELECT count(*) FROM video_frames`).Scan(&nFrames)
	st.DB.QueryRow(`SELECT count(*) FROM video_mentions`).Scan(&nMentions)
	st.DB.QueryRow(`SELECT count(*) FROM video_mentions WHERE ts_sec > 600 OR (stance IS NOT NULL AND stance NOT IN ('bullish','bearish','neutral','watching'))`).Scan(&badTs)
	if nFrames != 6 || nMentions != 5 || badTs != 0 {
		t.Errorf("frames=%d (want 6) mentions=%d (want 5) invalid=%d", nFrames, nMentions, badTs)
	}
	var calls int
	st.DB.QueryRow(`SELECT count(*) FROM llm_usage_log WHERE feature_id = ?`, FeatureID).Scan(&calls)
	if calls != 3 {
		t.Errorf("governor usage log rows = %d, want 3 (every call routed through llm.Call)", calls)
	}

	// Idempotent: a second sweep does nothing and makes no LLM call.
	if res2, _ := svc.Sweep(ctx); len(res2) != 0 || len(sb.users) != 3 {
		t.Errorf("second sweep ingested %d / calls %d, want 0 / 3", len(res2), len(sb.users))
	}
	if r := svc.IngestVideo(ctx, "JordiWeek1", false); r.Status != "skipped" {
		t.Errorf("re-ingest without force = %s, want skipped", r.Status)
	}
	// --force rebuilds in place: same id, no duplicate children.
	st.DB.Exec(`UPDATE video_digests SET suggested_regime = 'Shifting' WHERE video_id = 'JordiWeek1'`)
	r := svc.IngestVideo(ctx, "JordiWeek1", true)
	var legacy *string
	var ver int
	st.DB.QueryRow(`SELECT suggested_regime, extraction_version FROM video_digests WHERE video_id='JordiWeek1'`).Scan(&legacy, &ver)
	if legacy == nil || *legacy != "Shifting" || ver != ExtractionVersion {
		t.Errorf("force must keep the legacy suggested_regime and set extraction_version: legacy=%v ver=%d", legacy, ver)
	}
	st.DB.QueryRow(`SELECT count(*) FROM video_mentions`).Scan(&nMentions)
	if r.Status != "ingested" || r.DigestID != res[0].DigestID || nMentions != 5 {
		t.Errorf("force: status=%s id=%d (want %d) mentions=%d (want 5)", r.Status, r.DigestID, res[0].DigestID, nMentions)
	}
	if r := svc.IngestVideo(ctx, "../../etc", false); r.Status != "failed" {
		t.Error("path-like video id must be rejected")
	}

	// Reground: a drifted timestamp is re-anchored; a second pass changes nothing.
	st.DB.Exec(`UPDATE video_mentions SET ts_sec = 2 WHERE name = 'Bitcoin'`)
	rg, err := svc.Reground(ctx)
	var bad int
	st.DB.QueryRow(`SELECT count(*) FROM video_mentions WHERE name = 'Bitcoin' AND ts_sec != 31`).Scan(&bad)
	if err != nil || rg.MentionsMoved != 3 || bad != 0 {
		t.Errorf("reground: err=%v moved=%d (want 3) still-wrong=%d", err, rg.MentionsMoved, bad)
	}
	if rg2, _ := svc.Reground(ctx); rg2.MentionsMoved != 0 || rg2.QuotesMoved != 0 {
		t.Errorf("reground not idempotent: %+v", rg2)
	}

	ds, err := svc.List(ctx, "all")
	if err != nil || len(ds) != 3 || ds[0].VideoID != "CowenWeek1" {
		t.Fatalf("List: %v len=%d first=%v", err, len(ds), ds)
	}
	if ds[0].Snapshot == nil || *ds[0].Snapshot.BTCvs200DMA != "above" || ds[1].PriorTitle == nil {
		t.Errorf("List children missing: snapshot=%v priorTitle=%v", ds[0].Snapshot, ds[1].PriorTitle)
	}
	if only, _ := svc.List(ctx, "cowen"); len(only) != 1 {
		t.Errorf("source filter: %d rows, want 1", len(only))
	}
}

// P0 AC(a)/(c): the prompt carries FT's definitions, the "regime shift" disambiguation,
// and none of the steering inputs; the legacy key is no longer requested.
func TestPromptCarriesFTRegimeDefinitions(t *testing.T) {
	for _, src := range []string{"jordi", "cowen"} {
		p := systemPrompt(src)
		for _, want := range []string{"Stable:", "Shifting:", "Defensive:", "Unclassified:", "x0.6", "entry-zone alerts off",
			"names no near-term risk event", "dated catalysts", "does NOT expect a material equity drawdown",
			"explicitly expects or warns of a material drawdown, or advises cutting risk", "no regime content at all", "more cautious of the two best-fitting states",
			`NOT FT "Shifting"`, "evidence_quote", "regime_narrative", "catalysts", "cross_asset_tilt", "accumulation_stance"} {
			if !strings.Contains(p, want) {
				t.Errorf("%s prompt missing %q", src, want)
			}
		}
		if strings.Contains(p, "suggested_regime") {
			t.Errorf("%s prompt still requests the legacy suggested_regime", src)
		}
	}
	// No-steering: the user prompt is title/date/duration, optional prior-video themes
	// and the transcript. Nothing about the current regime.
	u := strings.ToLower(userPrompt(packageMeta{Title: "t", PublishedAt: "2026-10-12", DurationSec: 600}, "[0s] hi", nil))
	for _, banned := range []string{"current regime", "defensive", "shifting", "stable", "score"} {
		if strings.Contains(u, banned) {
			t.Errorf("user prompt leaks %q", banned)
		}
	}
}

// A quote joined with an ellipsis is accepted only when every fragment is verbatim.
func TestEvidenceQuoteWithEllipsis(t *testing.T) {
	tr := CompactTranscript(sampleVTT)
	segs := segments(tr)
	good := "liquidity is the whole story this week"
	if _, _, ok := groundedQuote(&good, segs); !ok {
		t.Fatal("a verbatim quote must ground")
	}
	bad := "liquidity is the whole story this week... and nothing else matters at all"
	if _, _, ok := groundedQuote(&bad, segs); ok {
		t.Fatal("an ungrounded second fragment must reject the quote")
	}
	one := "..."
	if _, _, ok := groundedQuote(&one, segs); ok {
		t.Fatal("an empty quote must not ground")
	}
}

func TestParseRegimeFields(t *testing.T) {
	tr := CompactTranscript(sampleVTT)
	parse := func(ftRegime string) Extraction {
		raw := `{"regime_narrative":" He is cautious. ","catalysts":[{"event":"midterms","date":"2026-11-03"},{"event":" "}],
		 "ft_regime":` + ftRegime + `,
		 "cross_asset_tilt":{"tilt":"Crypto","quote":"liquidity is the whole story"},
		 "accumulation_stance":{"stance":"hoarding","quote":"made up"}}`
		ex, err := ParseExtraction(raw, tr, "jordi")
		if err != nil {
			t.Fatal(err)
		}
		return ex
	}

	ex := parse(`{"state":"Shifting","confidence":"medium","evidence_quote":"liquidity is the whole story this week"}`)
	if ex.ProposedRegime == nil || *ex.ProposedRegime != "shifting" || *ex.ProposedConfidence != "medium" ||
		ex.ProposedQuote == nil || ex.ProposedQuoteTs == nil || *ex.ProposedQuoteTs != 31 {
		t.Errorf("valid proposal not kept: %+v", ex)
	}
	if *ex.RegimeNarrative != "He is cautious." || len(ex.CatalystsClean) != 1 || *ex.CatalystsClean[0].Date != "2026-11-03" {
		t.Errorf("narrative/catalysts: %v %v", ex.RegimeNarrative, ex.CatalystsClean)
	}
	if *ex.Tilt != "crypto" || ex.TiltQuote == nil || ex.AccumStance != nil || ex.AccumQuote != nil {
		t.Errorf("tilt/stance: tilt=%v quote=%v stance=%v", ex.Tilt, ex.TiltQuote, ex.AccumStance)
	}

	for name, in := range map[string]string{
		"not an FT state":    `{"state":"Bearish","confidence":"high","evidence_quote":"liquidity is the whole story"}`,
		"invented quote":     `{"state":"Defensive","confidence":"high","evidence_quote":"sell everything now"}`,
		"missing quote":      `{"state":"Defensive","confidence":"high"}`,
		"missing confidence": `{"state":"Stable","evidence_quote":"liquidity is the whole story"}`,
	} {
		if ex := parse(in); ex.ProposedRegime != nil || ex.ProposalRejected == "" {
			t.Errorf("%s: proposal should be rejected, got %v / %q", name, ex.ProposedRegime, ex.ProposalRejected)
		}
	}
	if ex := parse(`{"state":"Unclassified","confidence":"low"}`); ex.ProposedRegime == nil || *ex.ProposedRegime != "unclassified" {
		t.Error("unclassified needs no quote")
	}
}

const snapTranscript = `[0s] welcome back everyone today is a big one
[30s] hedge funds were already delevered at this point and they are not delevered as much as they should have been based on all the metrics that I got so this is a hedge fund deleveraging event and you will hear me talk about it more and more
[60s] the second thing is the midterms which could change regulation for ai and crypto
[90s] thanks for watching and see you next week with another update on the markets and the economy overall`

// The nearest-passage fallback: a stitched or lightly paraphrased quote snaps to real transcript
// text (verbatim by construction); an unrelated quote is still dropped.
func TestSnapQuoteToNearestTranscriptPassage(t *testing.T) {
	toks := transcriptTokens(snapTranscript)
	if len(toks) < 60 {
		t.Fatalf("tokens: %d", len(toks))
	}
	q := "Hedge funds were already delevered at this scenario. Not delevered as much as they should have been based on the metrics. This is a hedge fund deleveraging event."
	text, ts, ok := snapQuote(q, toks)
	if !ok || ts != 30 {
		t.Fatalf("expected a snap at 30s, got ok=%v ts=%d %q", ok, ts, text)
	}
	if !strings.Contains(snapTranscript, strings.Fields(text)[0]) || !strings.Contains(strings.Join(strings.Fields(snapTranscript), " "), strings.Join(strings.Fields(text), " ")) {
		t.Fatalf("snapped text must be a verbatim stretch of the transcript: %q", text)
	}
	if _, _, ok := snapQuote("The yield curve inverted while oil prices collapsed and gold rallied sharply", toks); ok {
		t.Fatal("an unrelated quote must not snap")
	}
	if _, _, ok := snapQuote("too short", toks); ok {
		t.Fatal("a quote with almost no content words must not snap")
	}
	// Through the sanitiser: an inexact quote is kept as 'nearest', an exact one as 'exact'.
	raw := func(quote string) Extraction {
		ex, err := ParseExtraction(`{"ft_regime":{"state":"Shifting","confidence":"medium","evidence_quote":`+jsonString(quote)+`}}`, snapTranscript, "jordi")
		if err != nil {
			t.Fatal(err)
		}
		return ex
	}
	if ex := raw(q); ex.ProposedRegime == nil || ex.ProposedQuoteMatch == nil || *ex.ProposedQuoteMatch != "nearest" {
		t.Fatalf("stitched quote should be kept as nearest: %+v", ex)
	}
	if ex := raw("so this is a hedge fund deleveraging event"); ex.ProposedRegime == nil || *ex.ProposedQuoteMatch != "exact" {
		t.Fatalf("verbatim quote should stay exact: %+v", ex)
	}
	if ex := raw("The yield curve inverted while oil prices collapsed and gold rallied sharply"); ex.ProposedRegime != nil || ex.ProposalRejected == "" {
		t.Fatalf("an unrelated quote must still drop the state: %+v", ex)
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
