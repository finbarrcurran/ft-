package videoitems

import (
	"context"
	"strings"
	"testing"
)

func words(t *testing.T, compact string) []Word {
	t.Helper()
	w := WordsFromCompact(compact)
	if len(w) == 0 {
		t.Fatal("no words")
	}
	return w
}

func item(block string, ref *Ref, f map[string]any) Item {
	return Item{Block: block, Fields: f, Ref: ref}
}

const sample = "[0s] Welcome back. Nvidia reported and Micron went from 100 to 400 in about twelve months.\n" +
	"[15s] The recession happened from December of 1969 until 73 and we didn't get back for seven years.\n" +
	"[30s] The light CEO Lummentum went on TV and said we are completely sold out through 2029.\n" +
	"[45s] Zcash and Ford were discussed. The QQQs are not going to do it in my opinion.\n" +
	"[600s] Cloudflare forecasts bots overtaking web traffic by 2027. Down 22% from the peak.\n"

func TestGateTwoDigitYear(t *testing.T) {
	g := NewGate(nil, words(t, sample))
	v := g.Check(item("analogues", &Ref{15, 25}, map[string]any{"then": "the 1973 drawdown", "now": "today", "point": "x"}))
	if v.Class != ClassGrounded {
		t.Fatalf("1973 should match '73' next to 1969: %+v", v)
	}
}

func TestGateInventedYear(t *testing.T) {
	g := NewGate(nil, words(t, sample))
	v := g.Check(item("predictions", &Ref{30, 40}, map[string]any{"claim": "banks lose volume", "horizon": "2025–2026", "outcome": nil}))
	if v.Class != ClassInvented || v.SignalOK {
		t.Fatalf("2025 and 2026 are nowhere in the transcript: %+v", v)
	}
}

func TestGateNearAndFarPlace(t *testing.T) {
	g := NewGate(nil, words(t, sample))
	near := g.Check(item("predictions", &Ref{100, 110}, map[string]any{"claim": "sold out through 2029", "outcome": nil}))
	if near.Class != ClassWrongPlace || !near.SignalOK {
		t.Fatalf("2029 is said 30 s away: %+v", near)
	}
	far := g.Check(item("predictions", &Ref{0, 10}, map[string]any{"claim": "forecast to 2027", "outcome": nil}))
	if far.Class != ClassFar || !far.SignalOK {
		t.Fatalf("2027 is said 600 s away: %+v", far)
	}
}

func TestGateTickersFromField(t *testing.T) {
	g := NewGate(nil, words(t, sample))
	if v := g.Check(item("tickers_named", &Ref{0, 10}, map[string]any{"name": "Nvidia", "ticker": "NVDA (Nvidia)", "stance": "bullish"})); v.Class != ClassGrounded {
		t.Fatalf("NVDA (Nvidia) must match: %+v", v)
	}
	if v := g.Check(item("tickers_named", &Ref{45, 50}, map[string]any{"name": "Zcash", "ticker": "ZEC", "stance": "watching"})); v.Class != ClassGrounded {
		t.Fatalf("Zcash = ZEC: %+v", v)
	}
	if v := g.Check(item("tickers_named", &Ref{45, 50}, map[string]any{"name": "Ford", "ticker": "F", "stance": "bearish"})); v.Class != ClassGrounded {
		t.Fatalf("Ford = F: %+v", v)
	}
}

func TestGateCatchesTheZCases(t *testing.T) {
	g := NewGate(nil, words(t, sample))
	for _, tk := range []string{"TLT", "HBM", "IVV"} {
		v := g.Check(item("tickers_named", &Ref{45, 55}, map[string]any{"name": "bonds", "ticker": tk, "stance": "bearish"}))
		if v.Class != ClassInvented && v.Class != ClassNameMapping {
			t.Errorf("%s must be flagged, got %+v", tk, v)
		}
	}
	v := g.Check(item("tickers_named", &Ref{30, 40}, map[string]any{"name": "Lummentum", "ticker": "LUMN", "stance": "bullish"}))
	if v.Class != ClassNameMapping || v.SignalOK {
		t.Fatalf("LUMN for Lummentum (it is LITE): %+v", v)
	}
	ok := g.Check(item("tickers_named", &Ref{30, 40}, map[string]any{"name": "Lummentum", "ticker": "LITE", "stance": "bullish"}))
	if ok.Class != ClassGrounded {
		t.Fatalf("LITE for Lummentum (the spoken spelling): %+v", ok)
	}
}

func TestGateChartNumbersAreUnverifiedNotFailures(t *testing.T) {
	g := NewGate(nil, words(t, sample))
	v := g.Check(item("indicators_cited", &Ref{600, 610}, map[string]any{"name": "Drawdown chart", "reading": "index range 7610 to 7694", "from_chart": true}))
	if v.Class != ClassGrounded || len(v.ChartUnverified) == 0 {
		t.Fatalf("chart-only numbers must be marked, not failed: %+v", v)
	}
	v2 := g.Check(item("indicators_cited", &Ref{600, 610}, map[string]any{"name": "Drawdown chart", "reading": "index range 7610 to 7694", "from_chart": false}))
	if v2.Class == ClassGrounded {
		t.Fatalf("the same numbers on a non-chart item must fail: %+v", v2)
	}
}

func TestNoReferenceIsInvented(t *testing.T) {
	g := NewGate(nil, words(t, sample))
	if v := g.Check(item("predictions", nil, map[string]any{"claim": "x"})); v.Class != ClassInvented {
		t.Fatalf("%+v", v)
	}
}

func TestNumberWordsAndScale(t *testing.T) {
	vals := NumValues("up twenty five percent, 20 trillion, 100K and 1,500, three months")
	for _, want := range []float64{25, 20e12, 100000, 1500, 3} {
		if !hasValue(vals, want) {
			t.Errorf("missing %v in %v", want, vals)
		}
	}
}

func TestParseExtraction(t *testing.T) {
	raw := "```json\n" + `{"predictions":[{"claim":"x","direction":"UP","evidence_ref":{"start_sec":10,"end_sec":20},"outcome":"yes"},{"claim":"y","direction":"sideways"}],"tickers_named":[{"name":"Stripe","ticker":null,"stance":"loved","evidence_ref":{"start_sec":5,"end_sec":9}}],"tone":"calm"}` + "\n```"
	ex, err := ParseExtraction(raw)
	if err != nil || len(ex.Items) != 3 || ex.Tone != "calm" {
		t.Fatalf("%v %+v", err, ex)
	}
	// order follows Blocks: tickers_named comes before predictions
	if ex.Items[0].Fields["stance"] != "watching" || ex.Items[0].Fields["ticker"] != nil {
		t.Fatalf("%+v", ex.Items[0])
	}
	p0 := ex.Items[1]
	if p0.Fields["direction"] != "up" || p0.Fields["outcome"] != nil || p0.Ref == nil {
		t.Fatalf("%+v", p0)
	}
	if ex.Items[2].Fields["direction"] != "unclear" || ex.Items[2].Ref != nil {
		t.Fatalf("%+v", ex.Items[2])
	}
}

func TestResolver(t *testing.T) {
	r := &Resolver{Lookup: func(_ context.Context, q string) ([]Candidate, error) {
		if strings.EqualFold(q, "Stripe") {
			return []Candidate{{Symbol: "STRP", Name: "Stripe Inc"}}, nil
		}
		if strings.EqualFold(q, "Quirkco") {
			return []Candidate{{Symbol: "XYZ", Name: "Something Else Holdings"}}, nil
		}
		return nil, nil
	}}
	ctx := context.Background()
	if x := r.Resolve(ctx, "Lummentum", ""); x.Ticker != "LITE" || x.Source != "alias" {
		t.Fatalf("the ASR spelling is in the alias file: %+v", x)
	}
	if x := r.Resolve(ctx, "Lumenttum", ""); x.Ticker != "LITE" || x.Source != "fuzzy" {
		t.Fatalf("an unseen misspelling is matched by edit distance: %+v", x)
	}
	if x := r.Resolve(ctx, "SpaceX", ""); x.Ticker != "SPCX" || x.Source != "alias" {
		t.Fatalf("%+v", x)
	}
	if x := r.Resolve(ctx, "Stripe", "SQ"); x.Ticker != "STRP" || x.Source != "search" || !x.Conflict {
		t.Fatalf("a search hit disagreeing with the model's ticker is a conflict: %+v", x)
	}
	if x := r.Resolve(ctx, "Quirkco", "QRK"); x.Status != "unresolved" || x.ModelTicker != "QRK" || len(x.Candidates) == 0 {
		t.Fatalf("an unrelated search hit must stay unresolved, with the model's ticker kept as unverified: %+v", x)
	}
	if x := r.Resolve(ctx, "Nobody Inc", ""); x.Status != "unresolved" {
		t.Fatalf("%+v", x)
	}
}

func TestChunkAndMerge(t *testing.T) {
	var b strings.Builder
	for s := 0; s <= 4500; s += 15 {
		b.WriteString("[" + itoa(s) + "s] word\n")
	}
	compact := strings.TrimSuffix(b.String(), "\n")
	one := Chunk(compact, 3000, 3600, 1800, 60)
	if len(one) != 1 {
		t.Fatalf("short videos are not chunked: %d", len(one))
	}
	cs := Chunk(compact, 4596, 3600, 1800, 60)
	if len(cs) != 3 {
		t.Fatalf("a 77-minute video is three chunks, got %d", len(cs))
	}
	a := &Extraction{Items: []Item{{Block: "predictions", Fields: map[string]any{"claim": "x"}, Ref: &Ref{1790, 1800}}}}
	bb := &Extraction{Items: []Item{{Block: "predictions", Fields: map[string]any{"claim": "x"}, Ref: &Ref{1795, 1805}}, {Block: "predictions", Fields: map[string]any{"claim": "y"}, Ref: &Ref{1900, 1910}}}}
	m := Merge([]*Extraction{a, bb})
	if len(m.Items) != 2 {
		t.Fatalf("the overlap repeat must be dropped: %d", len(m.Items))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}

func TestParseDirectionSalvagesTruncatedArray(t *testing.T) {
	raw := `[{"n":1,"speaker_view":"a","verdict":"supports","reason":"r"},{"n":2,"speaker_view":"b","verdict":"CONTRADICTS","reason":"r"},{"n":3,"speaker_vi`
	v := ParseDirection(raw)
	if len(v) != 2 || v[1].Verdict != "contradicts" {
		t.Fatalf("%+v", v)
	}
}

func TestNameSpacingAndTickerLookup(t *testing.T) {
	g := NewGate(Default, WordsFromCompact("[0s] I wired money to Robin Hood and moved it. They just acquired Open Router. Cityroup 226,000 employees. like Cisco at 100 PE"))
	for _, c := range []struct{ name, tk string }{{"Robinhood", "HOOD"}, {"OpenRouter", ""}, {"Cisco", "CSCO"}, {"Citi", "C"}} {
		f := map[string]any{"name": c.name, "stance": "neutral"}
		if c.tk != "" {
			f["ticker"] = c.tk
		}
		if v := g.Check(item("tickers_named", &Ref{0, 10}, f)); v.Class != ClassGrounded {
			t.Errorf("%s: want grounded, got %s %+v", c.name, v.Class, v.Issues)
		}
	}
}

func TestV1952NameRules(t *testing.T) {
	words := WordsFromCompact("[0s] first part here\n[15s] more words\n[600s] the eli liy report was strong and total two fell\n[615s] end")
	g := NewGate(Default, words)
	// a name present anywhere is far, never invented
	v := g.Check(item("tickers_named", &Ref{0, 10}, map[string]any{"name": "Eli Liy", "stance": "bullish"}))
	if v.Class != ClassFar || !v.SignalOK {
		t.Errorf("eli liy: want far and signal ok, got %s %+v", v.Class, v.Issues)
	}
	// absent from the whole transcript: invented
	v = g.Check(item("tickers_named", &Ref{0, 10}, map[string]any{"name": "Microsoft", "stance": "watching"}))
	if v.Class != ClassInvented || v.SignalOK {
		t.Errorf("microsoft: want invented, got %s", v.Class)
	}
	// number words in names: "total 2" is spoken as "total two"
	v = g.Check(item("tickers_named", &Ref{600, 610}, map[string]any{"name": "total 2", "stance": "watching"}))
	if v.Class != ClassGrounded {
		t.Errorf("total 2: want grounded, got %s %+v", v.Class, v.Issues)
	}
	// the near window: 150 s away is wrong place at 180 s, far at 120 s
	g.NearSec = 180
	if v := g.Check(item("tickers_named", &Ref{450, 460}, map[string]any{"name": "Eli Liy", "stance": "bullish"})); v.Class != ClassWrongPlace {
		t.Errorf("180 s window: got %s", v.Class)
	}
	g.NearSec = 120
	if v := g.Check(item("tickers_named", &Ref{450, 460}, map[string]any{"name": "Eli Liy", "stance": "bullish"})); v.Class != ClassFar {
		t.Errorf("120 s window: got %s", v.Class)
	}
}

func TestV1952Kinds(t *testing.T) {
	r := &Resolver{A: Default}
	for name, want := range map[string]string{"total two": "not_listed", "TOTAL3": "unresolved", "DTCC": "not_listed", "Anthropic IPO": "not_listed",
		"South Korean market": "proxy", "oil prices": "proxy", "WTI": "resolved", "HSI": "resolved", "SpaceX IPO": "resolved", "IBM": "resolved", "EWZ": "resolved"} {
		got := r.Resolve(context.Background(), name, "")
		if got.Status != want {
			t.Errorf("%s: want %s, got %s (%+v)", name, want, got.Status, got)
		}
	}
	if got := r.Resolve(context.Background(), "oil", ""); got.Ticker != "CL=F" || got.Source != "proxy" {
		t.Errorf("oil = %+v", got)
	}
}

func TestTickerSpokenElsewhereIsFar(t *testing.T) {
	g := NewGate(Default, WordsFromCompact("[0s] intro words\n[600s] the eli lilly report was strong\n[615s] end"))
	v := g.Check(item("predictions", &Ref{0, 10}, map[string]any{"asset": "LLY", "claim": "bigger", "direction": "up"}))
	if v.Class != ClassFar || !v.SignalOK {
		t.Errorf("want far, got %s %+v", v.Class, v.Issues)
	}
}
