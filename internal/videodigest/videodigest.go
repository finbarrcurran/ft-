// Package videodigest ingests SC-43 video packages (produced on Jarvis by the P0
// pipeline under /var/lib/video_digest) into FT: one governor-routed LLM
// extraction per video, stored in the video_* tables (migration 0046).
//
// FT reads packages read-only from disk and never re-fetches from YouTube.
// Video-derived data is context only — never a scoring input (D6) — and
// suggested_regime is never auto-applied (D7).
package videodigest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"ft/internal/llm"
)

// DefaultRoot is where the Jarvis pipeline writes packages (P0 §13 A4).
const DefaultRoot = "/var/lib/video_digest"

// FeatureID tags every extraction call in llm_usage_log; its kill switch is
// user_preferences.llm_feature_video_digest.
const FeatureID = "video_digest"

const (
	maxOutputTokens = 4000 // needs llm_max_output_tokens_per_call >= 4000 (raised from 2000, Fin 2026-09-26)
	markerEverySec  = 30   // timestamp marker spacing in the compact transcript
	maxMentions     = 20
	maxQuotes       = 2
)

var (
	sources      = map[string]string{"jordi": "Jordi Visser", "cowen": "Benjamin Cowen"}
	packageDirRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}_([A-Za-z0-9_-]{6,20})$`)
	videoIDRe    = regexp.MustCompile(`^[A-Za-z0-9_-]{6,20}$`)
)

// Service ingests and reads video digests. Stateless apart from its handles.
type Service struct {
	DB   *sql.DB
	LLM  *llm.Service
	Root string
}

// New returns a Service. An empty root means DefaultRoot.
func New(db *sql.DB, l *llm.Service, root string) *Service {
	if root == "" {
		root = DefaultRoot
	}
	return &Service{DB: db, LLM: l, Root: root}
}

// ---------------------------------------------------------------------------
// Package on disk (P0 §5 contract)

type metaFrame struct {
	File    string  `json:"file"`
	TsSec   int     `json:"ts_sec"`
	Origin  string  `json:"origin"`
	CueText *string `json:"cue_text"`
}

type packageMeta struct {
	Source           string          `json:"source"`
	VideoID          string          `json:"video_id"`
	Title            string          `json:"title"`
	URL              string          `json:"url"`
	PublishedAt      string          `json:"published_at"`
	DurationSec      int             `json:"duration_sec"`
	IsFlagship       bool            `json:"is_flagship"`
	TranscriptMethod string          `json:"transcript_method"`
	CaptionTrack     *string         `json:"transcript_caption_track"`
	Frames           []metaFrame     `json:"frames"`
	FrameParams      json.RawMessage `json:"frame_params"`
	PipelineVersion  string          `json:"pipeline_version"`
	Status           string          `json:"status"`
}

type pkg struct {
	Dir  string
	Meta packageMeta
}

func readPackage(dir string) (pkg, error) {
	b, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return pkg{}, err
	}
	var m packageMeta
	if err := json.Unmarshal(b, &m); err != nil {
		return pkg{}, fmt.Errorf("meta.json: %w", err)
	}
	if m.Status != "complete" {
		return pkg{}, fmt.Errorf("package status %q, not complete", m.Status)
	}
	if _, ok := sources[m.Source]; !ok {
		return pkg{}, fmt.Errorf("unknown source %q", m.Source)
	}
	return pkg{Dir: dir, Meta: m}, nil
}

// packages lists every complete package under Root, oldest first (so prior-week
// context exists when a backlog is ingested in one sweep).
func (s *Service) packages() ([]pkg, error) {
	var out []pkg
	for src := range sources {
		entries, err := os.ReadDir(filepath.Join(s.Root, src))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if !e.IsDir() || !packageDirRe.MatchString(e.Name()) {
				continue
			}
			p, err := readPackage(filepath.Join(s.Root, src, e.Name()))
			if err != nil {
				continue // incomplete or malformed — the pipeline owns fixing it
			}
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Meta.PublishedAt != out[j].Meta.PublishedAt {
			return out[i].Meta.PublishedAt < out[j].Meta.PublishedAt
		}
		return out[i].Meta.VideoID < out[j].Meta.VideoID
	})
	return out, nil
}

func (s *Service) findPackage(videoID string) (pkg, error) {
	if !videoIDRe.MatchString(videoID) {
		return pkg{}, fmt.Errorf("invalid video id")
	}
	all, err := s.packages()
	if err != nil {
		return pkg{}, err
	}
	for _, p := range all {
		if p.Meta.VideoID == videoID {
			return p, nil
		}
	}
	return pkg{}, fmt.Errorf("no complete package for %s", videoID)
}

// ---------------------------------------------------------------------------
// Timed transcript

var (
	cueTimeRe = regexp.MustCompile(`^\s*((?:\d+:)?\d{1,2}:\d{2}\.\d{3})\s*-->`)
	tagRe     = regexp.MustCompile(`<[^>]+>`)
	bracketRe = regexp.MustCompile(`\[[^\]]*\]`)
)

func vttSeconds(ts string) float64 {
	parts := strings.Split(ts, ":")
	var secs float64
	fmt.Sscanf(parts[len(parts)-1], "%f", &secs)
	mult := 60.0
	for i := len(parts) - 2; i >= 0; i-- {
		var v float64
		fmt.Sscanf(parts[i], "%f", &v)
		secs += v * mult
		mult *= 60
	}
	return secs
}

// CompactTranscript turns a .vtt into prose with a "[NNNs]" marker roughly every
// markerEverySec seconds. It keeps timing (for ts_sec) at close to plain-text
// size: raw YouTube auto-caption VTT repeats each line and carries per-word
// timestamps, which would blow the governor's input cap several times over.
func CompactTranscript(vtt string) string {
	var b strings.Builder
	var start float64
	next := 0.0
	last := ""
	for _, line := range strings.Split(vtt, "\n") {
		if m := cueTimeRe.FindStringSubmatch(line); m != nil {
			start = vttSeconds(m[1])
			continue
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "WEBVTT") || strings.HasPrefix(line, "Kind:") ||
			strings.HasPrefix(line, "Language:") || strings.HasPrefix(line, "NOTE") {
			continue
		}
		clean := strings.TrimSpace(bracketRe.ReplaceAllString(
			strings.ReplaceAll(tagRe.ReplaceAllString(line, ""), "&nbsp;", " "), ""))
		if clean == "" || clean == last {
			continue // auto-captions repeat the previous line in each rolling cue
		}
		last = clean
		if start >= next {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			fmt.Fprintf(&b, "[%ds] ", int(start))
			next = (math.Floor(start/markerEverySec) + 1) * markerEverySec
		} else {
			b.WriteString(" ")
		}
		b.WriteString(clean)
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Prompts

const commonSchema = `{
  "executive_summary": string — 3 to 5 sentences on what he actually argues,
  "themes": [string] — 3 to 8 short theme labels,
  "mentions": [ { "name": string, "type": "stock"|"etf"|"crypto"|"index"|"macro",
                  "context": string — one sentence on what he said about it,
                  "stance": "bullish"|"bearish"|"neutral"|"watching",
                  "ts_sec": integer|null } ] — at most 20, most substantive first,
  "notable_quotes": [ { "quote_text": string, "ts_sec": integer|null } ] — at most 2, each copied VERBATIM from the transcript and under 15 words,
  "regime_read": string|null — his read of the current market regime in one or two sentences, only if he gives one,
  "suggested_regime": "Stable"|"Shifting"|"Defensive"|"Unclassified"|null`

const jordiExtras = `,
  "theme_changes_vs_prior_week": [string]|null — how this video's themes differ from the PRIOR VIDEO CONTEXT supplied with the transcript (new, dropped, or changed emphasis); null if no prior context is supplied`

// Cowen extras are deliberately limited to the (b) list of the P1 handover:
// fields he demonstrably states in speech. The (c) snapshot columns are never
// requested — they are not reliably spoken and are left for P2 frame OCR.
const cowenExtras = `,
  "snapshot": {
    "risk_indicator_band": string|null — his own wording for where his risk metric/indicator stands, only if he states it,
    "btc_vs_200dma": "above"|"below"|null — Bitcoin versus its 200-day moving average, only if he states it,
    "mvrv_z_band": string|null,
    "cycle_phase": string|null — as he states it; do not convert it to a number,
    "macro_cpi_print": string|null — what he says about the latest CPI print,
    "macro_fed_posture": string|null,
    "macro_recession_flag": true|false|null — true only if he says a recession is likely or under way, false only if he says it is not,
    "confidence": "high"|"medium"|"low"|"unstated" — how explicitly he stated the snapshot fields overall
  }`

const rules = `

Rules:
- Only what he says in this transcript. If a field is not stated, use null (or [] for a list). Never infer, never fill from general knowledge. "Unstated" is a valid, useful answer.
- ts_sec is the number in the nearest "[NNNs]" marker at or before the point where he makes it.
- mentions are specific instruments, companies, indices or macro variables he discusses with a view — not passing name-drops.
- Return ONLY the JSON object: no prose, no code fences.`

func systemPrompt(source string) string {
	extras := jordiExtras
	if source == "cowen" {
		extras = cowenExtras
	}
	return "You extract a structured digest from one YouTube video transcript by " + sources[source] +
		", for personal research. The transcript is timestamped: each \"[NNNs]\" marker gives the video time, in seconds, at which the following text begins.\n\n" +
		"Return one JSON object with exactly these keys:\n" + commonSchema + extras + "\n}" + rules
}

type prior struct {
	ID          int64
	PublishedAt string
	Title       string
	Summary     string
	Themes      []string
}

func userPrompt(m packageMeta, transcript string, p *prior) string {
	var b strings.Builder
	fmt.Fprintf(&b, "VIDEO: %q, published %s, %d min.\n\n", m.Title, m.PublishedAt, m.DurationSec/60)
	if p != nil {
		fmt.Fprintf(&b, "PRIOR VIDEO CONTEXT (%s, %q):\nThemes: %s\nSummary: %s\n\n",
			p.PublishedAt, p.Title, strings.Join(p.Themes, "; "), p.Summary)
	}
	b.WriteString("TRANSCRIPT:\n")
	b.WriteString(transcript)
	return b.String()
}

// ---------------------------------------------------------------------------
// Extraction parsing + sanitising (the model's output is untrusted input)

type rawMention struct {
	Name    string   `json:"name"`
	Type    *string  `json:"type"`
	Context *string  `json:"context"`
	Stance  *string  `json:"stance"`
	TsSec   *float64 `json:"ts_sec"`
}
type rawQuote struct {
	QuoteText string   `json:"quote_text"`
	TsSec     *float64 `json:"ts_sec"`
}
type rawSnapshot struct {
	RiskIndicatorBand  *string `json:"risk_indicator_band"`
	BTCvs200DMA        *string `json:"btc_vs_200dma"`
	MVRVZBand          *string `json:"mvrv_z_band"`
	CyclePhase         *string `json:"cycle_phase"`
	MacroCPIPrint      *string `json:"macro_cpi_print"`
	MacroFedPosture    *string `json:"macro_fed_posture"`
	MacroRecessionFlag *bool   `json:"macro_recession_flag"`
	Confidence         *string `json:"confidence"`
}
type rawExtraction struct {
	ExecutiveSummary *string      `json:"executive_summary"`
	Themes           []string     `json:"themes"`
	Mentions         []rawMention `json:"mentions"`
	NotableQuotes    []rawQuote   `json:"notable_quotes"`
	RegimeRead       *string      `json:"regime_read"`
	SuggestedRegime  *string      `json:"suggested_regime"`
	ThemeChanges     []string     `json:"theme_changes_vs_prior_week"`
	Snapshot         *rawSnapshot `json:"snapshot"`
}

// Extraction is the sanitised result that gets stored.
type Extraction struct {
	rawExtraction
	QuotesRejected     int
	RejectedQuotes     []string // logged, not stored — for tuning the prompt
	MentionsUngrounded int      // name never found in the transcript → no timestamp
}

func enum(v *string, allowed ...string) *string {
	if v == nil {
		return nil
	}
	for _, a := range allowed {
		if strings.EqualFold(strings.TrimSpace(*v), a) {
			out := a
			return &out
		}
	}
	return nil
}

func text(v *string) *string {
	if v == nil || strings.TrimSpace(*v) == "" {
		return nil
	}
	t := strings.TrimSpace(*v)
	return &t
}

func clampTs(v *float64, dur int) *int {
	if v == nil || *v < 0 || (dur > 0 && *v > float64(dur)) {
		return nil
	}
	i := int(*v)
	return &i
}

var nonWordRe = regexp.MustCompile(`[^a-z0-9]+`)

// Filler words are dropped before matching: the verbatim (en-orig) captions keep
// every "uh"/"um", and the model tidies them out of quotes.
var fillers = map[string]bool{"uh": true, "um": true, "uhm": true, "erm": true, "er": true, "ah": true, "hmm": true, "mm": true}

func normalise(s string) string {
	words := strings.Fields(nonWordRe.ReplaceAllString(strings.ToLower(s), " "))
	out := words[:0]
	for _, w := range words {
		if !fillers[w] {
			out = append(out, w)
		}
	}
	return strings.Join(out, " ")
}

// ---------------------------------------------------------------------------
// Grounding: timestamps come from where things are actually said in the
// transcript, not from the model's guess (which drifts by minutes).

type segment struct {
	start int
	text  string // normalised
}

var markerLineRe = regexp.MustCompile(`^\[(\d+)s\] (.*)$`)

func segments(compact string) []segment {
	var out []segment
	for _, line := range strings.Split(compact, "\n") {
		if m := markerLineRe.FindStringSubmatch(line); m != nil {
			var t int
			fmt.Sscanf(m[1], "%d", &t)
			out = append(out, segment{start: t, text: normalise(m[2])})
		}
	}
	return out
}

var nameStopwords = map[string]bool{"the": true, "and": true, "of": true, "a": true, "an": true, "us": true,
	"u": true, "s": true, "in": true, "on": true, "for": true, "to": true, "vs": true, "inc": true, "corp": true}

func nameTokens(name string) []string {
	var out []string
	seen := map[string]bool{}
	for _, w := range strings.Fields(normalise(name)) {
		if nameStopwords[w] || seen[w] || (len(w) < 2 && !strings.ContainsAny(w, "0123456789")) {
			continue
		}
		seen[w] = true
		out = append(out, w)
	}
	return out
}

// groundMention returns the start of the transcript segment, nearest to the
// model's guess, where the mention's name is actually spoken (a segment plus the
// next one, to catch names split across a marker). Multi-word names need at
// least two of their words to match. nil if the name never appears — better no
// link than a wrong one.
func groundMention(name string, guess *float64, segs []segment) *int {
	toks := nameTokens(name)
	if len(toks) == 0 {
		return nil
	}
	need := 1
	if len(toks) > 2 {
		need = 2
	}
	g := 0.0
	if guess != nil {
		g = *guess
	}
	best, bestDist := -1, math.MaxFloat64
	for i, sg := range segs {
		win := " " + sg.text + " "
		if i+1 < len(segs) {
			win += segs[i+1].text + " "
		}
		own := " " + sg.text + " "
		hits, ownHits := 0, 0
		for _, t := range toks {
			if strings.Contains(win, " "+t+" ") {
				hits++
				if strings.Contains(own, " "+t+" ") {
					ownHits++
				}
			}
		}
		// The look-ahead only completes a name split across a marker; the segment
		// itself must contain part of it, or an earlier segment would claim it.
		if hits < need || ownHits == 0 {
			continue
		}
		if d := math.Abs(float64(sg.start) - g); d < bestDist {
			best, bestDist = i, d
		}
	}
	if best < 0 {
		return nil
	}
	return &segs[best].start
}

// quoteTs finds a quote verbatim (ignoring case, punctuation and filler words)
// anywhere in the transcript and returns the start of the segment it begins in.
func quoteTs(quote string, segs []segment) (int, bool) {
	q := normalise(quote)
	if q == "" {
		return 0, false
	}
	var b strings.Builder
	offs := make([]int, len(segs))
	for i, sg := range segs {
		offs[i] = b.Len()
		b.WriteString(" " + sg.text)
	}
	b.WriteString(" ")
	idx := strings.Index(b.String(), " "+q+" ")
	if idx < 0 {
		return 0, false
	}
	at := 0
	for i := range segs {
		if offs[i] <= idx {
			at = segs[i].start
		}
	}
	return at, true
}

// ParseExtraction decodes the model's JSON and enforces the schema: enum values,
// caps on list lengths, and grounding against the timed transcript — quotes must
// occur verbatim (filler words aside) and take the timestamp where they occur;
// mention timestamps are re-anchored to where the name is actually spoken.
func ParseExtraction(raw, compactTranscript string, source string) (Extraction, error) {
	s := strings.TrimSpace(raw)
	i, j := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if i < 0 || j <= i {
		return Extraction{}, fmt.Errorf("no JSON object in model output")
	}
	var r rawExtraction
	if err := json.Unmarshal([]byte(s[i:j+1]), &r); err != nil {
		return Extraction{}, fmt.Errorf("model output is not valid JSON: %w", err)
	}
	ex := Extraction{}
	ex.ExecutiveSummary = text(r.ExecutiveSummary)
	ex.RegimeRead = text(r.RegimeRead)
	ex.SuggestedRegime = enum(r.SuggestedRegime, "Stable", "Shifting", "Defensive", "Unclassified")
	for _, t := range r.Themes {
		if t = strings.TrimSpace(t); t != "" {
			ex.Themes = append(ex.Themes, t)
		}
	}
	if source == "jordi" {
		for _, t := range r.ThemeChanges {
			if t = strings.TrimSpace(t); t != "" {
				ex.ThemeChanges = append(ex.ThemeChanges, t)
			}
		}
	}
	segs := segments(compactTranscript)
	for _, m := range r.Mentions {
		if strings.TrimSpace(m.Name) == "" || len(ex.Mentions) >= maxMentions {
			continue
		}
		name := strings.TrimSpace(m.Name)
		var ts *float64
		if g := groundMention(name, m.TsSec, segs); g != nil {
			f := float64(*g)
			ts = &f
		} else {
			ex.MentionsUngrounded++
		}
		ex.Mentions = append(ex.Mentions, rawMention{
			Name:    name,
			Type:    enum(m.Type, "stock", "etf", "crypto", "index", "macro"),
			Context: text(m.Context),
			Stance:  enum(m.Stance, "bullish", "bearish", "neutral", "watching"),
			TsSec:   ts,
		})
	}
	for _, q := range r.NotableQuotes {
		qt := strings.TrimSpace(strings.Trim(q.QuoteText, `"“”`))
		if qt == "" || len(ex.NotableQuotes) >= maxQuotes {
			continue
		}
		at, ok := quoteTs(qt, segs)
		if !ok {
			ex.QuotesRejected++
			ex.RejectedQuotes = append(ex.RejectedQuotes, qt)
			continue
		}
		f := float64(at)
		ex.NotableQuotes = append(ex.NotableQuotes, rawQuote{QuoteText: qt, TsSec: &f})
	}
	if source == "cowen" {
		sn := rawSnapshot{}
		if r.Snapshot != nil {
			sn = rawSnapshot{
				RiskIndicatorBand:  text(r.Snapshot.RiskIndicatorBand),
				BTCvs200DMA:        enum(r.Snapshot.BTCvs200DMA, "above", "below"),
				MVRVZBand:          text(r.Snapshot.MVRVZBand),
				CyclePhase:         text(r.Snapshot.CyclePhase),
				MacroCPIPrint:      text(r.Snapshot.MacroCPIPrint),
				MacroFedPosture:    text(r.Snapshot.MacroFedPosture),
				MacroRecessionFlag: r.Snapshot.MacroRecessionFlag,
				Confidence:         enum(r.Snapshot.Confidence, "high", "medium", "low", "unstated"),
			}
		}
		if sn.Confidence == nil {
			u := "unstated"
			sn.Confidence = &u
		}
		ex.Snapshot = &sn
	}
	return ex, nil
}

// ---------------------------------------------------------------------------
// Ingest

// Result reports one package's ingest outcome.
type Result struct {
	VideoID        string   `json:"videoId"`
	Source         string   `json:"source"`
	PublishedAt    string   `json:"publishedAt"`
	Status         string   `json:"status"` // ingested | skipped | failed
	DigestID       int64    `json:"digestId,omitempty"`
	Mentions       int      `json:"mentions,omitempty"`
	Quotes         int      `json:"quotes,omitempty"`
	QuotesRejected int      `json:"quotesRejected,omitempty"`
	RejectedQuotes []string `json:"rejectedQuotes,omitempty"`
	Ungrounded     int      `json:"mentionsUngrounded,omitempty"`
	Frames         int      `json:"frames,omitempty"`
	PriorDigestID  int64    `json:"priorDigestId,omitempty"`
	CostUSD        float64  `json:"costUsd,omitempty"`
	InputTokens    int      `json:"inputTokens,omitempty"`
	Error          string   `json:"error,omitempty"`
}

func (s *Service) existingID(ctx context.Context, videoID string) (int64, error) {
	var id int64
	err := s.DB.QueryRowContext(ctx, `SELECT id FROM video_digests WHERE video_id = ?`, videoID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

func (s *Service) priorFor(ctx context.Context, m packageMeta) (*prior, error) {
	var p prior
	var themes sql.NullString
	var summary sql.NullString
	err := s.DB.QueryRowContext(ctx, `
		SELECT id, published_at, title, executive_summary, themes FROM video_digests
		 WHERE source = ? AND published_at < ? ORDER BY published_at DESC, id DESC LIMIT 1`,
		m.Source, m.PublishedAt).Scan(&p.ID, &p.PublishedAt, &p.Title, &summary, &themes)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.Summary = summary.String
	_ = json.Unmarshal([]byte(themes.String), &p.Themes)
	return &p, nil
}

func jsonOrNull(v []string) any {
	if v == nil {
		return nil
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ingestMu serialises ingests process-wide, so the scheduled sweep and a manual
// ingest from the tab can't both insert the same video.
var ingestMu sync.Mutex

func (s *Service) ingest(ctx context.Context, p pkg, force bool) Result {
	ingestMu.Lock()
	defer ingestMu.Unlock()
	m := p.Meta
	res := Result{VideoID: m.VideoID, Source: m.Source, PublishedAt: m.PublishedAt}
	fail := func(err error) Result { res.Status, res.Error = "failed", err.Error(); return res }

	oldID, err := s.existingID(ctx, m.VideoID)
	if err != nil {
		return fail(err)
	}
	if oldID != 0 && !force {
		res.Status, res.DigestID = "skipped", oldID
		return res
	}
	vtt, err := os.ReadFile(filepath.Join(p.Dir, "transcript.vtt"))
	if err != nil {
		return fail(err)
	}
	compact := CompactTranscript(string(vtt))
	if compact == "" {
		return fail(fmt.Errorf("transcript.vtt produced no text"))
	}

	var pr *prior
	if m.Source == "jordi" { // theme_changes_vs_prior_week is Jordi-only (§7)
		if pr, err = s.priorFor(ctx, m); err != nil {
			return fail(err)
		}
	}

	if s.LLM == nil {
		return fail(fmt.Errorf("llm service not configured"))
	}
	// Ask for maxOutputTokens, but never more than the governor's per-call cap:
	// a lowered cap then shortens output rather than failing every ingest.
	maxOut := maxOutputTokens
	if v, _ := s.LLM.Store.GetPreference(ctx, "llm_max_output_tokens_per_call"); v != "" {
		var c int
		if _, err := fmt.Sscanf(v, "%d", &c); err == nil && c > 0 && c < maxOut {
			maxOut = c
		}
	} else if maxOut > 2000 {
		maxOut = 2000 // governor default when the preference is unset
	}
	resp, err := s.LLM.Call(ctx, llm.CallRequest{
		FeatureID:       FeatureID,
		FeatureContext:  m.Source + ":" + m.VideoID + " " + m.PublishedAt,
		SystemPrompt:    systemPrompt(m.Source),
		UserPrompt:      userPrompt(m, compact, pr),
		MaxOutputTokens: maxOut,
	})
	if err != nil {
		return fail(fmt.Errorf("llm: %w", err))
	}
	res.CostUSD, res.InputTokens = resp.CostUSD, resp.InputTokens
	if resp.Outcome == "truncated" {
		return fail(fmt.Errorf("llm output truncated at %d tokens", maxOut))
	}
	ex, err := ParseExtraction(resp.Text, compact, m.Source)
	if err != nil {
		return fail(err)
	}

	// Jordi counts every weekly upload (select: all_new), so each is its own
	// flagship per the P1 handover; Cowen keeps the pipeline's flagship rule.
	isFlagship := m.IsFlagship || m.Source == "jordi"
	var priorID any
	if pr != nil {
		priorID = pr.ID
		res.PriorDigestID = pr.ID
	}
	var themeChanges []string
	if m.Source == "jordi" && pr != nil {
		themeChanges = ex.ThemeChanges
		if themeChanges == nil {
			themeChanges = []string{}
		}
	}

	// The governor resolves "" to llm_default_model; CallResponse doesn't echo it.
	model, _ := s.LLM.Store.GetPreference(ctx, "llm_default_model")
	if model == "" {
		model = "claude-haiku-4-5"
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return fail(err)
	}
	defer tx.Rollback()
	if oldID != 0 { // --force: children cascade; the id is reused so links survive
		if _, err := tx.ExecContext(ctx, `DELETE FROM video_digests WHERE id = ?`, oldID); err != nil {
			return fail(err)
		}
	}
	var idArg any
	if oldID != 0 {
		idArg = oldID
	}
	r, err := tx.ExecContext(ctx, `
		INSERT INTO video_digests (id, source, video_id, title, url, published_at, is_flagship, duration_sec,
		  transcript_method, transcript_caption_track, pipeline_version, package_path, executive_summary,
		  themes, theme_changes_vs_prior_week, prior_digest_id, regime_read, suggested_regime,
		  llm_model, llm_cost_usd, ingested_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		idArg, m.Source, m.VideoID, m.Title, m.URL, m.PublishedAt, boolInt(isFlagship), m.DurationSec,
		m.TranscriptMethod, m.CaptionTrack, m.PipelineVersion, p.Dir, ex.ExecutiveSummary,
		jsonOrNull(ex.Themes), jsonOrNull(themeChanges), priorID, ex.RegimeRead, ex.SuggestedRegime,
		model, resp.CostUSD, time.Now().Unix())
	if err != nil {
		return fail(err)
	}
	id, _ := r.LastInsertId()
	for _, mn := range ex.Mentions {
		if _, err := tx.ExecContext(ctx, `INSERT INTO video_mentions (digest_id, name, type, context, stance, ts_sec)
			VALUES (?,?,?,?,?,?)`, id, mn.Name, mn.Type, mn.Context, mn.Stance, clampTs(mn.TsSec, m.DurationSec)); err != nil {
			return fail(err)
		}
	}
	for _, q := range ex.NotableQuotes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO video_quotes (digest_id, quote_text, ts_sec) VALUES (?,?,?)`,
			id, q.QuoteText, clampTs(q.TsSec, m.DurationSec)); err != nil {
			return fail(err)
		}
	}
	var params any
	if len(m.FrameParams) > 0 && string(m.FrameParams) != "null" {
		params = string(m.FrameParams)
	}
	for _, f := range m.Frames {
		if _, err := tx.ExecContext(ctx, `INSERT INTO video_frames (digest_id, file_path, ts_sec, origin, cue_text, frame_params)
			VALUES (?,?,?,?,?,?)`, id, filepath.Join(p.Dir, f.File), f.TsSec, f.Origin, f.CueText, params); err != nil {
			return fail(err)
		}
	}
	if sn := ex.Snapshot; sn != nil && m.Source == "cowen" {
		var rec any
		if sn.MacroRecessionFlag != nil {
			rec = boolInt(*sn.MacroRecessionFlag)
		}
		// Only (b) columns are written; the (c) columns stay NULL until P2.
		if _, err := tx.ExecContext(ctx, `INSERT INTO cowen_weekly_snapshot (digest_id, risk_indicator_band,
			btc_vs_200dma, mvrv_z_band, cycle_phase, macro_cpi_print, macro_fed_posture, macro_recession_flag, confidence)
			VALUES (?,?,?,?,?,?,?,?,?)`, id, sn.RiskIndicatorBand, sn.BTCvs200DMA, sn.MVRVZBand, sn.CyclePhase,
			sn.MacroCPIPrint, sn.MacroFedPosture, rec, sn.Confidence); err != nil {
			return fail(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fail(err)
	}
	res.Status, res.DigestID = "ingested", id
	res.Mentions, res.Quotes, res.QuotesRejected, res.Frames = len(ex.Mentions), len(ex.NotableQuotes), ex.QuotesRejected, len(m.Frames)
	res.RejectedQuotes, res.Ungrounded = ex.RejectedQuotes, ex.MentionsUngrounded
	return res
}

// ReGroundResult summarises a Reground pass.
type ReGroundResult struct {
	Digests, Mentions, MentionsMoved, MentionsUngrounded, Quotes, QuotesMoved int
}

// Reground re-anchors the timestamps of already-stored mentions and quotes to
// where they are actually spoken, using each digest's package transcript. No LLM
// call. Used once for digests ingested before grounding existed; idempotent.
func (s *Service) Reground(ctx context.Context) (ReGroundResult, error) {
	var out ReGroundResult
	rows, err := s.DB.QueryContext(ctx, `SELECT id, package_path FROM video_digests`)
	if err != nil {
		return out, err
	}
	type dg struct {
		id   int64
		path string
	}
	var ds []dg
	for rows.Next() {
		var d dg
		if err := rows.Scan(&d.id, &d.path); err != nil {
			rows.Close()
			return out, err
		}
		ds = append(ds, d)
	}
	rows.Close()

	for _, d := range ds {
		vtt, err := os.ReadFile(filepath.Join(d.path, "transcript.vtt"))
		if err != nil {
			return out, fmt.Errorf("digest %d: %w", d.id, err)
		}
		segs := segments(CompactTranscript(string(vtt)))
		out.Digests++

		type row struct {
			id   int64
			text string
			ts   sql.NullInt64
		}
		load := func(q string) ([]row, error) {
			rs, err := s.DB.QueryContext(ctx, q, d.id)
			if err != nil {
				return nil, err
			}
			defer rs.Close()
			var got []row
			for rs.Next() {
				var r row
				if err := rs.Scan(&r.id, &r.text, &r.ts); err != nil {
					return nil, err
				}
				got = append(got, r)
			}
			return got, rs.Err()
		}
		ms, err := load(`SELECT id, name, ts_sec FROM video_mentions WHERE digest_id = ?`)
		if err != nil {
			return out, err
		}
		for _, m := range ms {
			out.Mentions++
			var guess *float64
			if m.ts.Valid {
				f := float64(m.ts.Int64)
				guess = &f
			}
			g := groundMention(m.text, guess, segs)
			var val any
			if g == nil {
				out.MentionsUngrounded++
			} else {
				val = *g
			}
			if g == nil && !m.ts.Valid || g != nil && m.ts.Valid && int64(*g) == m.ts.Int64 {
				continue
			}
			out.MentionsMoved++
			if _, err := s.DB.ExecContext(ctx, `UPDATE video_mentions SET ts_sec = ? WHERE id = ?`, val, m.id); err != nil {
				return out, err
			}
		}
		qs, err := load(`SELECT id, quote_text, ts_sec FROM video_quotes WHERE digest_id = ?`)
		if err != nil {
			return out, err
		}
		for _, q := range qs {
			out.Quotes++
			at, ok := quoteTs(q.text, segs)
			if !ok || q.ts.Valid && int64(at) == q.ts.Int64 {
				continue
			}
			out.QuotesMoved++
			if _, err := s.DB.ExecContext(ctx, `UPDATE video_quotes SET ts_sec = ? WHERE id = ?`, at, q.id); err != nil {
				return out, err
			}
		}
	}
	return out, nil
}

// IngestVideo ingests one package by YouTube video id (manual / force path).
func (s *Service) IngestVideo(ctx context.Context, videoID string, force bool) Result {
	p, err := s.findPackage(videoID)
	if err != nil {
		return Result{VideoID: videoID, Status: "failed", Error: err.Error()}
	}
	return s.ingest(ctx, p, force)
}

// Sweep ingests every complete package not yet in the DB, oldest first. It stops
// early when the governor blocks on budget or pause — the rest wait for the next
// sweep rather than failing one by one.
func (s *Service) Sweep(ctx context.Context) ([]Result, error) {
	all, err := s.packages()
	if err != nil {
		return nil, err
	}
	var out []Result
	for _, p := range all {
		id, err := s.existingID(ctx, p.Meta.VideoID)
		if err != nil {
			return out, err
		}
		if id != 0 {
			continue
		}
		r := s.ingest(ctx, p, false)
		out = append(out, r)
		if r.Status == "failed" && (strings.Contains(r.Error, llm.ErrBudgetBlocked.Error()) ||
			strings.Contains(r.Error, llm.ErrGloballyPaused.Error()) ||
			strings.Contains(r.Error, llm.ErrFeatureDisabled.Error()) ||
			strings.Contains(r.Error, llm.ErrAPIKeyMissing.Error())) {
			break
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Read side (Digest tab)

type Mention struct {
	Name      string  `json:"name"`
	Type      *string `json:"type"`
	Context   *string `json:"context"`
	Stance    *string `json:"stance"`
	TsSec     *int    `json:"tsSec"`
	Held      *bool   `json:"held,omitempty"`      // P3; stripped in demo mode
	Watchlist *bool   `json:"watchlist,omitempty"` // P3; stripped in demo mode
}

type Quote struct {
	Text  string `json:"text"`
	TsSec *int   `json:"tsSec"`
}

// Snapshot carries only the (b) fields — the (c) columns are NULL until P2 and
// aren't surfaced yet.
type Snapshot struct {
	RiskIndicatorBand  *string `json:"riskIndicatorBand"`
	BTCvs200DMA        *string `json:"btcVs200dma"`
	MVRVZBand          *string `json:"mvrvZBand"`
	CyclePhase         *string `json:"cyclePhase"`
	MacroCPIPrint      *string `json:"macroCpiPrint"`
	MacroFedPosture    *string `json:"macroFedPosture"`
	MacroRecessionFlag *bool   `json:"macroRecessionFlag"`
	Confidence         *string `json:"confidence"`
}

// Digest is the API shape. suggested_regime and regime_read are deliberately
// absent: not rendered in P1 (P3 / D7).
type Digest struct {
	ID               int64     `json:"id"`
	Source           string    `json:"source"`
	SourceName       string    `json:"sourceName"`
	VideoID          string    `json:"videoId"`
	Title            string    `json:"title"`
	URL              string    `json:"url"`
	PublishedAt      string    `json:"publishedAt"`
	IsFlagship       bool      `json:"isFlagship"`
	DurationSec      int       `json:"durationSec"`
	CaptionTrack     *string   `json:"captionTrack"`
	ExecutiveSummary *string   `json:"executiveSummary"`
	Themes           []string  `json:"themes"`
	ThemeChanges     []string  `json:"themeChanges"`
	PriorTitle       *string   `json:"priorTitle"`
	Mentions         []Mention `json:"mentions"`
	Quotes           []Quote   `json:"quotes"`
	Snapshot         *Snapshot `json:"snapshot"`
	FrameCount       int       `json:"frameCount"`
}

func nullStr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	return &n.String
}
func nullInt(n sql.NullInt64) *int {
	if !n.Valid {
		return nil
	}
	i := int(n.Int64)
	return &i
}
func nullBool(n sql.NullInt64) *bool {
	if !n.Valid {
		return nil
	}
	b := n.Int64 != 0
	return &b
}

// List returns digests newest first; source "" or "all" means both.
func (s *Service) List(ctx context.Context, source string) ([]Digest, error) {
	q := `SELECT d.id, d.source, d.video_id, d.title, d.url, d.published_at, d.is_flagship, d.duration_sec,
	             d.transcript_caption_track, d.executive_summary, d.themes, d.theme_changes_vs_prior_week,
	             p.title, (SELECT count(*) FROM video_frames f WHERE f.digest_id = d.id)
	        FROM video_digests d LEFT JOIN video_digests p ON p.id = d.prior_digest_id`
	var args []any
	if source != "" && source != "all" {
		q += ` WHERE d.source = ?`
		args = append(args, source)
	}
	q += ` ORDER BY d.published_at DESC, d.id DESC`
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	var out []Digest
	for rows.Next() {
		var d Digest
		var flag int
		var dur sql.NullInt64
		var track, summary, themes, changes, priorTitle sql.NullString
		if err := rows.Scan(&d.ID, &d.Source, &d.VideoID, &d.Title, &d.URL, &d.PublishedAt, &flag, &dur,
			&track, &summary, &themes, &changes, &priorTitle, &d.FrameCount); err != nil {
			rows.Close()
			return nil, err
		}
		d.SourceName = sources[d.Source]
		d.IsFlagship = flag != 0
		d.DurationSec = int(dur.Int64)
		d.CaptionTrack, d.ExecutiveSummary, d.PriorTitle = nullStr(track), nullStr(summary), nullStr(priorTitle)
		_ = json.Unmarshal([]byte(themes.String), &d.Themes)
		if changes.Valid {
			_ = json.Unmarshal([]byte(changes.String), &d.ThemeChanges)
		}
		out = append(out, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if err := s.fillChildren(ctx, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Service) fillChildren(ctx context.Context, d *Digest) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT name, type, context, stance, ts_sec, held, watchlist
		FROM video_mentions WHERE digest_id = ? ORDER BY id`, d.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var m Mention
		var typ, ctxt, stance sql.NullString
		var ts, held, wl sql.NullInt64
		if err := rows.Scan(&m.Name, &typ, &ctxt, &stance, &ts, &held, &wl); err != nil {
			rows.Close()
			return err
		}
		m.Type, m.Context, m.Stance, m.TsSec = nullStr(typ), nullStr(ctxt), nullStr(stance), nullInt(ts)
		m.Held, m.Watchlist = nullBool(held), nullBool(wl)
		d.Mentions = append(d.Mentions, m)
	}
	rows.Close()

	rows, err = s.DB.QueryContext(ctx, `SELECT quote_text, ts_sec FROM video_quotes WHERE digest_id = ? ORDER BY id`, d.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var q Quote
		var ts sql.NullInt64
		if err := rows.Scan(&q.Text, &ts); err != nil {
			rows.Close()
			return err
		}
		q.TsSec = nullInt(ts)
		d.Quotes = append(d.Quotes, q)
	}
	rows.Close()

	if d.Source != "cowen" {
		return nil
	}
	var sn Snapshot
	var risk, dma, mvrv, cyc, cpi, fed, conf sql.NullString
	var rec sql.NullInt64
	err = s.DB.QueryRowContext(ctx, `SELECT risk_indicator_band, btc_vs_200dma, mvrv_z_band, cycle_phase,
		macro_cpi_print, macro_fed_posture, macro_recession_flag, confidence
		FROM cowen_weekly_snapshot WHERE digest_id = ?`, d.ID).Scan(&risk, &dma, &mvrv, &cyc, &cpi, &fed, &rec, &conf)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	sn.RiskIndicatorBand, sn.BTCvs200DMA, sn.MVRVZBand, sn.CyclePhase = nullStr(risk), nullStr(dma), nullStr(mvrv), nullStr(cyc)
	sn.MacroCPIPrint, sn.MacroFedPosture, sn.MacroRecessionFlag, sn.Confidence = nullStr(cpi), nullStr(fed), nullBool(rec), nullStr(conf)
	d.Snapshot = &sn
	return nil
}
