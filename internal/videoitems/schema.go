package videoitems

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Blocks are the item lists of the v5 extraction, in display order.
var Blocks = []string{"causal_claims", "analogues", "seasonality", "key_levels", "indicators_cited", "tickers_named", "predictions", "self_corrections"}

// SchemaVersion is stored with every run.
const SchemaVersion = 5

// Ref is the span of the transcript (marker seconds) an item cites.
type Ref struct {
	Start int `json:"start_sec"`
	End   int `json:"end_sec"`
}

// Item is one extracted item: its block, the fields the model wrote and the span it cited.
type Item struct {
	Block  string         `json:"block"`
	Fields map[string]any `json:"fields"`
	Ref    *Ref           `json:"ref,omitempty"`
}

// Extraction is the parsed v5 output.
type Extraction struct {
	Items []Item
	Tone  string
}

var enumFields = map[string][]string{
	"direction":  {"up", "down", "flat", "unclear"},
	"role":       {"support", "resistance", "target"},
	"stance":     {"bullish", "bearish", "neutral", "watching"},
	"asset_kind": nil,
}

// ParseExtraction reads the model's JSON. Items without a usable evidence_ref are kept (the gate then
// marks them invented); an enum value outside its list is replaced by "unclear" or removed, and the
// outcome of a prediction is always null.
func ParseExtraction(raw string) (*Extraction, error) {
	i, j := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if i < 0 || j <= i {
		return nil, fmt.Errorf("no JSON object in the model output")
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw[i:j+1]), &top); err != nil {
		return nil, fmt.Errorf("model output is not valid JSON: %w", err)
	}
	ex := &Extraction{}
	if t, ok := top["tone"]; ok {
		_ = json.Unmarshal(t, &ex.Tone)
	}
	for _, blk := range Blocks {
		var list []map[string]any
		if err := json.Unmarshal(top[blk], &list); err != nil {
			continue // a missing or malformed list is an empty one
		}
		for _, f := range list {
			it := Item{Block: blk, Fields: f}
			if r, ok := f["evidence_ref"].(map[string]any); ok {
				s, sok := num(r["start_sec"])
				e, eok := num(r["end_sec"])
				if sok && eok && s >= 0 && e >= s {
					it.Ref = &Ref{Start: s, End: e}
				}
			}
			delete(f, "evidence_ref")
			cleanEnums(&it)
			if blk == "predictions" {
				f["outcome"] = nil
			}
			ex.Items = append(ex.Items, it)
		}
	}
	return ex, nil
}

func num(v any) (int, bool) {
	switch x := v.(type) {
	case float64:
		return int(x), true
	case int:
		return x, true
	}
	return 0, false
}

func cleanEnums(it *Item) {
	for k, allowed := range enumFields {
		v, ok := it.Fields[k].(string)
		if !ok || allowed == nil {
			continue
		}
		good := false
		for _, a := range allowed {
			if strings.EqualFold(v, a) {
				it.Fields[k] = a
				good = true
			}
		}
		if !good {
			if k == "direction" || k == "stance" {
				it.Fields[k+"_raw"] = v
				it.Fields[k] = map[string]string{"direction": "unclear", "stance": "watching"}[k]
			} else {
				delete(it.Fields, k)
			}
		}
	}
}

// ItemText joins every string value of an item the gate checks (not the span, the outcome or the gate's own marks).
func ItemText(it Item) string {
	var parts []string
	var rec func(v any, key string)
	skip := map[string]bool{"evidence_ref": true, "outcome": true, "direction_raw": true, "stance_raw": true}
	rec = func(v any, key string) {
		if skip[key] {
			return
		}
		switch x := v.(type) {
		case string:
			parts = append(parts, x)
		case map[string]any:
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				rec(x[k], k)
			}
		case []any:
			for _, e := range x {
				rec(e, "")
			}
		}
	}
	keys := make([]string, 0, len(it.Fields))
	for k := range it.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		rec(it.Fields[k], k)
	}
	return strings.Join(parts, " ")
}

// ---------------------------------------------------------------------------
// Prompt

const refShape = `{ "start_sec": integer, "end_sec": integer }`

// itemSchema is the v5 schema without a regime block (the card's regime stays on the v4 extraction)
// and without list caps (Fin 2026-11-01: uncapped, thinking off, no quote field).
var itemSchema = `{
  "causal_claims": [ { "cause": string, "effect": string, "asset": string|null, "direction": "up"|"down"|"flat"|"unclear", "horizon": string|null, "confidence_wording": string|null — his own hedging words, "evidence_ref": ` + refShape + ` } ],
  "analogues": [ { "then": string, "now": string, "point": string, "evidence_ref": ` + refShape + ` } ],
  "seasonality": [ { "pattern": string, "asset": string|null, "evidence_ref": ` + refShape + ` } ],
  "key_levels": [ { "asset": string, "level": string — as he states it, "role": "support"|"resistance"|"target", "invalidation": string|null, "evidence_ref": ` + refShape + ` } ],
  "indicators_cited": [ { "name": string, "reading": string|null — the value or state he gives, "from_chart": true|false — true if he is reading it off a chart on screen, "evidence_ref": ` + refShape + ` } ],
  "tickers_named": [ { "name": string — the company, token or instrument EXACTLY as he says it (keep the transcript's spelling), "ticker": string|null — only when you are sure of the symbol, otherwise null, "stance": "bullish"|"bearish"|"neutral"|"watching", "evidence_ref": ` + refShape + ` } ],
  "predictions": [ { "claim": string, "asset": string|null, "direction": "up"|"down"|"flat"|"unclear", "horizon": string|null, "condition": string|null, "evidence_ref": ` + refShape + `, "outcome": null } ],
  "self_corrections": [ { "what": string, "evidence_ref": ` + refShape + ` } ],
  "tone": string — two or three words
}`

const itemRules = `

Rules:
- Record what the speaker SAYS, not what is true. Only what is in this transcript. Never infer, never fill from general knowledge. Empty lists and null are correct when nothing is stated.
- Every item carries evidence_ref: the [NNNs] marker seconds where the passage begins and ends. end_sec minus start_sec MUST be 30 or less: cite the single 15 to 30 second passage that carries the item; if the support is longer, cite only its strongest 15 to 30 seconds. Never cite a longer span. Do NOT copy or paraphrase his words into any quote field: there is none.
- Extract EVERY claim, prediction, level and indicator he states. There is no limit on the number of items in any list: include all of them, not just the most important ones.
- tickers_named: record every company, token or instrument he discusses with a view, whether or not you know its symbol. Write the name as he says it; leave ticker null unless you are sure of the symbol.
- Return ONLY the JSON object: no prose, no code fences.`

var sourceNames = map[string]string{"jordi": "Jordi Visser", "cowen": "Benjamin Cowen"}

// SystemPrompt is the v5 item-extraction system prompt for a source.
func SystemPrompt(source string) string {
	name := sourceNames[source]
	if name == "" {
		name = "the speaker"
	}
	return "You extract a structured v5 digest of the claims in one YouTube video transcript by " + name + ", for personal research. " +
		"The transcript is timestamped: each \"[NNNs]\" marker gives the video time, in seconds, at which the following text begins.\n\n" +
		"Return one JSON object with exactly these keys:\n" + itemSchema + itemRules
}

var markerRe = regexp.MustCompile(`^\[(\d+)s\]`)

// Chunk splits a compact transcript into pieces of about chunkSec seconds, each starting overlapSec
// before the previous one ended (Jordi's long videos are over an hour). A transcript up to
// maxSingleSec is returned whole.
func Chunk(compact string, durationSec, maxSingleSec, chunkSec, overlapSec int) []string {
	if durationSec <= maxSingleSec {
		return []string{compact}
	}
	lines := strings.Split(compact, "\n")
	var out []string
	for start := 0; start < durationSec; start += chunkSec {
		lo, hi := start, start+chunkSec
		if start > 0 {
			lo = start - overlapSec
		}
		var b []string
		for _, l := range lines {
			m := markerRe.FindStringSubmatch(l)
			if m == nil {
				continue
			}
			t := atoi(m[1])
			if t >= lo-14 && t < hi {
				b = append(b, l)
			}
		}
		if len(b) > 0 {
			out = append(out, strings.Join(b, "\n"))
		}
	}
	return out
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}

// Merge joins the items of several chunks, dropping a repeat (same block, cited start within 20 s,
// same fields) that the overlap produced.
func Merge(parts []*Extraction) *Extraction {
	if len(parts) == 1 {
		return parts[0]
	}
	out := &Extraction{}
	seen := map[string]bool{}
	for _, p := range parts {
		if out.Tone == "" {
			out.Tone = p.Tone
		}
		for _, it := range p.Items {
			start := -1
			if it.Ref != nil {
				start = it.Ref.Start / 20
			}
			b, _ := json.Marshal(it.Fields)
			k := fmt.Sprintf("%s|%d|%s", it.Block, start, b)
			if seen[k] {
				continue
			}
			seen[k] = true
			out.Items = append(out.Items, it)
		}
	}
	return out
}
