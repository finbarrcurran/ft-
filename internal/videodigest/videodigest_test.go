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
	ex, err := ParseExtraction(raw, "and liquidity is the whole story this week", "cowen")
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
	if len(ex.NotableQuotes) != 1 || ex.QuotesRejected != 1 {
		t.Errorf("quotes: kept %d rejected %d, want 1/1", len(ex.NotableQuotes), ex.QuotesRejected)
	}
	if *ex.SuggestedRegime != "Shifting" || ex.Snapshot.BTCvs200DMA != nil || *ex.Snapshot.Confidence != "unstated" {
		t.Errorf("enums: regime=%v dma=%v conf=%v", *ex.SuggestedRegime, ex.Snapshot.BTCvs200DMA, *ex.Snapshot.Confidence)
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
	r := svc.IngestVideo(ctx, "JordiWeek1", true)
	st.DB.QueryRow(`SELECT count(*) FROM video_mentions`).Scan(&nMentions)
	if r.Status != "ingested" || r.DigestID != res[0].DigestID || nMentions != 5 {
		t.Errorf("force: status=%s id=%d (want %d) mentions=%d (want 5)", r.Status, r.DigestID, res[0].DigestID, nMentions)
	}
	if r := svc.IngestVideo(ctx, "../../etc", false); r.Status != "failed" {
		t.Error("path-like video id must be rejected")
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
