package videoitems

import "testing"

// SC-69 (FT-125): the passage now starts 30 s before the cited start (was 15 s).
func TestGateLeadWindowIs30Seconds(t *testing.T) {
	g := NewGate(nil, words(t, "[0s] intro words.\n[20s] we are sold out through 2029 said the ceo.\n[100s] more words."))
	in := g.Check(item("predictions", &Ref{45, 55}, map[string]any{"claim": "sold out through 2029", "outcome": nil}))
	if in.Class != ClassGrounded {
		t.Fatalf("a value 25 s before the cited start is inside the 30 s lead: %+v", in)
	}
	out := g.Check(item("predictions", &Ref{55, 65}, map[string]any{"claim": "sold out through 2029", "outcome": nil}))
	if out.Class != ClassWrongPlace {
		t.Fatalf("a value 35 s before the cited start is still cited at the wrong place: %+v", out)
	}
}

// The window cap grew with the lead, so a fast speaker's late words are not cut off.
func TestWindowKeepsWordsPastTwoHundred(t *testing.T) {
	var s string
	for i := 0; i < 250; i++ {
		s += "word "
	}
	w := WordsFromCompact("[0s] " + s + "[40s] TARGETWORD")
	for i := range w { // spread the words over the window
		w[i].T = 10 + i/6
	}
	if got := Window(w, 40, 50); len(got) == 0 || !containsWord(got, "word") {
		t.Fatal("empty window")
	}
	if n := len(splitFields(Window(w, 40, 50))); n <= 200 || n > 300 {
		t.Fatalf("window holds %d words, want between 201 and 300", n)
	}
}

func containsWord(s, w string) bool {
	for _, f := range splitFields(s) {
		if f == w {
			return true
		}
	}
	return false
}

func splitFields(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ' ' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// The model's name for a company can differ from the spoken one; when the ticker it gave is known and a spoken
// name of that ticker is in the passage, the item is grounded.
func TestGateReverseAliasCheck(t *testing.T) {
	g := NewGate(nil, words(t, "[0s] Alphabet reported strong cloud numbers this quarter."))
	v := g.Check(item("tickers_named", &Ref{0, 10}, map[string]any{"name": "Search giant", "ticker": "GOOGL", "stance": "bullish"}))
	if v.Class != ClassGrounded {
		t.Fatalf("GOOGL is spoken as Alphabet: %+v", v)
	}
	// without a known ticker the name is still checked
	v = g.Check(item("tickers_named", &Ref{0, 10}, map[string]any{"name": "Search giant", "ticker": nil, "stance": "bullish"}))
	if v.Class == ClassGrounded {
		t.Fatalf("an unspoken name with no ticker must not be grounded: %+v", v)
	}
}
