package videoitems

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// The direction flag (SC-53 D): a second small pass over predictions, causal claims and ticker
// stances. It asks, per item, whether the passage supports the item's NET direction, contradicts it
// (including a reversed condition) or does not settle it. It is a flag for a human, never a filter,
// and it shows the speaker's net view beside every "contradicts".

// DirectionSystem is the system prompt.
const DirectionSystem = `You check one thing about each numbered item taken from a market commentator's video: whether the item's NET direction and stance are what the speaker said in the PASSAGE. For each item you get the passage (transcript around the cited moment) and the item's own fields: claim or cause/effect, direction, condition, confidence wording, horizon. Judge ONLY from the passage.
Work in two steps. (1) Say in a few words what the speaker's net view is in the passage. (2) Compare it with the item's direction. If the item has a condition ('unless X', 'only if X', 'if X'), first decide whether the speaker expects X to happen; the net direction follows from that. Example: the speaker says 'the only way this is a bad buy is if rates spike, and I do not expect that'. An item that says 'bad buy, direction down' has the wrong net direction: the speaker's net view is that it is not a bad buy. That is 'contradicts'.
Verdicts: supports = the passage says or clearly implies the item's net direction; contradicts = the passage says the opposite net direction (including a reversed condition); unclear = the passage does not settle it, or is about something else. An item whose subject is simply not in the passage is 'unclear', never 'contradicts'.
Return ONLY a JSON array: [ { "n": integer, "speaker_view": string of at most 15 words, "verdict": "supports"|"contradicts"|"unclear", "reason": string of at most 15 words } ]`

// DirectionBlocks are the blocks the flag covers.
var DirectionBlocks = map[string]bool{"predictions": true, "causal_claims": true, "tickers_named": true}

// DirectionVerdict is one parsed answer.
type DirectionVerdict struct {
	N       int    `json:"n"`
	View    string `json:"speaker_view"`
	Verdict string `json:"verdict"`
	Reason  string `json:"reason"`
}

var directionKeep = []string{"claim", "cause", "effect", "asset", "direction", "condition", "confidence_wording", "horizon", "name", "ticker", "stance"}

// DirectionPrompt builds the user message for a batch of items (each with its wide passage) and
// returns the indexes (into items) in the order they were numbered.
func DirectionPrompt(items []Item, words []Word) (string, []int) {
	var b strings.Builder
	var idx []int
	for i, it := range items {
		if !DirectionBlocks[it.Block] || it.Ref == nil {
			continue
		}
		idx = append(idx, i)
		fields := map[string]any{}
		for _, k := range directionKeep {
			if v, ok := it.Fields[k]; ok && v != nil && v != "" {
				if k == "stance" {
					fields["direction (stance)"] = v
				} else {
					fields[k] = v
				}
			}
		}
		fb, _ := json.Marshal(fields)
		p := strings.Fields(Window(words, it.Ref.Start, it.Ref.End))
		if len(p) > 150 {
			p = p[:150]
		}
		fmt.Fprintf(&b, "ITEM %d\nPASSAGE: %s\nFIELDS: %s\n\n", len(idx), strings.Join(p, " "), fb)
	}
	return b.String(), idx
}

var jsonArrayRe = regexp.MustCompile(`(?s)\[.*\]`)

// ParseDirection reads the model's array; entries that cannot be read are skipped.
func ParseDirection(raw string) []DirectionVerdict {
	var out []DirectionVerdict
	if m := jsonArrayRe.FindString(raw); m != "" {
		if json.Unmarshal([]byte(m), &out) == nil {
			return clean(out)
		}
	}
	// truncated array: salvage the complete objects
	for _, o := range regexp.MustCompile(`\{[^{}]*\}`).FindAllString(raw, -1) {
		var v DirectionVerdict
		if json.Unmarshal([]byte(o), &v) == nil && v.N > 0 {
			out = append(out, v)
		}
	}
	return clean(out)
}

func clean(in []DirectionVerdict) []DirectionVerdict {
	var out []DirectionVerdict
	for _, v := range in {
		v.Verdict = strings.ToLower(strings.TrimSpace(v.Verdict))
		if v.N > 0 && (v.Verdict == "supports" || v.Verdict == "contradicts" || v.Verdict == "unclear") {
			out = append(out, v)
		}
	}
	return out
}
