package nexus

import "testing"

func sp(s string) *string    { return &s }
func fp2(f float64) *float64 { return &f }
func ip2(i int) *int         { return &i }

func TestPassesGate1(t *testing.T) {
	cases := map[*string]bool{
		sp("Strong Uptrend / Buyable"): true,
		sp("Constructive"):             true,
		sp("Pullback Opportunity"):     true,
		sp("Strong but Extended"):      false, // EXCLUDED per ruling
		sp("Neutral / Watch"):          false,
		sp("Early Trend Improvement"):  false,
		sp("Weakening"):                false,
		sp("Breakdown Risk"):           false,
		nil:                            false,
	}
	for lbl, want := range cases {
		if got := PassesGate1(lbl); got != want {
			ls := "<nil>"
			if lbl != nil {
				ls = *lbl
			}
			t.Errorf("PassesGate1(%q)=%v want %v", ls, got, want)
		}
	}
}

func TestPassesGate2Boundary(t *testing.T) {
	cases := []struct {
		s    *float64
		want bool
	}{
		{fp2(0), true}, {fp2(40), true}, {fp2(59.9), true},
		{fp2(60.0), false}, {fp2(60.1), false}, {fp2(75), false}, {nil, false},
	}
	for _, c := range cases {
		if got := PassesGate2(c.s); got != c.want {
			t.Errorf("PassesGate2(%v)=%v want %v", c.s, got, c.want)
		}
	}
}

func TestPassesDipBoundary(t *testing.T) {
	// Baseline that PASSES: vs200=5 (price>MA200), vs200>vs50 (2) so MA50>MA200,
	// change5D=-1 (<0), rsi=42.
	ok := func(rsi, ch, vs50, vs200 float64) bool {
		return PassesDip(fp2(vs50), fp2(vs200), fp2(ch), fp2(rsi))
	}
	if !ok(42, -1, 2, 5) {
		t.Fatal("baseline dip should pass")
	}
	// RSI boundary: 34.9 fail, 35.0 pass, 50.0 pass, 50.1 fail.
	if ok(34.9, -1, 2, 5) {
		t.Error("RSI 34.9 should fail")
	}
	if !ok(35.0, -1, 2, 5) {
		t.Error("RSI 35.0 should pass")
	}
	if !ok(50.0, -1, 2, 5) {
		t.Error("RSI 50.0 should pass")
	}
	if ok(50.1, -1, 2, 5) {
		t.Error("RSI 50.1 should fail")
	}
	// Change_5D must be < 0.
	if ok(42, 0, 2, 5) {
		t.Error("change5D=0 should fail")
	}
	// price>MA200 (vs200>0).
	if ok(42, -1, 2, -1) {
		t.Error("vs200<0 (price below MA200) should fail")
	}
	// MA50>MA200 (vs200>vs50).
	if ok(42, -1, 6, 5) {
		t.Error("vs200<vs50 (MA50 below MA200) should fail")
	}
	// nils fail.
	if PassesDip(nil, fp2(5), fp2(-1), fp2(42)) {
		t.Error("nil vs50 should fail")
	}
}

func TestChangePct(t *testing.T) {
	closes := []float64{100, 101, 102, 103, 104, 110} // 6 bars
	// 5-day change: (110-100)/100 = 10%.
	if v := ChangePct(closes, 5); v == nil || *v != 10 {
		t.Errorf("ChangePct 5d = %v want 10", v)
	}
	// too few bars for 20d.
	if v := ChangePct(closes, 20); v != nil {
		t.Errorf("ChangePct 20d = %v want nil", v)
	}
}

func TestComposeEntryCandidates(t *testing.T) {
	in := []EntryInput{
		// survives: buyable + not stretched, theme A, peg 1.5
		{Ticker: "AAA", Theme: "A", SetupLabel: sp("Strong Uptrend / Buyable"), ExhScore: fp2(30), TrendScore: ip2(85), FwdPeg: fp2(1.5)},
		// survives: constructive + not stretched, theme A, peg 1.0 (cheaper → rank 1 in A)
		{Ticker: "BBB", Theme: "A", SetupLabel: sp("Constructive"), ExhScore: fp2(50), TrendScore: ip2(70), FwdPeg: fp2(1.0)},
		// excluded: stretched (exh 60)
		{Ticker: "CCC", Theme: "B", SetupLabel: sp("Strong Uptrend / Buyable"), ExhScore: fp2(60), TrendScore: ip2(90), FwdPeg: fp2(0.8)},
		// excluded: non-qualifying label
		{Ticker: "DDD", Theme: "B", SetupLabel: sp("Breakdown Risk"), ExhScore: fp2(20), TrendScore: ip2(30)},
		// survives: pullback, theme B, peg nil (nulls last)
		{Ticker: "EEE", Theme: "B", SetupLabel: sp("Pullback Opportunity"), ExhScore: fp2(35), TrendScore: ip2(55)},
	}
	cands, tally := ComposeEntryCandidates(in, false)
	if len(cands) != 3 {
		t.Fatalf("want 3 survivors, got %d", len(cands))
	}
	if len(tally) != 5 {
		t.Fatalf("tally should cover all 5, got %d", len(tally))
	}
	// Ranking: rank-1s first. Theme A rank1 = BBB (peg 1.0 < 1.5). Theme B rank1 = EEE (only survivor).
	// Both rank 1 → tiebreak trendScore desc: BBB(70) > EEE(55). Then AAA (theme A rank 2).
	if cands[0].Ticker != "BBB" || cands[1].Ticker != "EEE" || cands[2].Ticker != "AAA" {
		t.Errorf("rank order = %s,%s,%s want BBB,EEE,AAA", cands[0].Ticker, cands[1].Ticker, cands[2].Ticker)
	}
	if cands[2].ThemeRank != 2 {
		t.Errorf("AAA themeRank = %d want 2", cands[2].ThemeRank)
	}
	// PriceVsMA50 wiring not exercised here (nil), just ensure no panic + CCC excluded.
	for _, c := range cands {
		if c.Ticker == "CCC" || c.Ticker == "DDD" {
			t.Errorf("%s should be excluded", c.Ticker)
		}
	}
}

func TestComposeEmptyState(t *testing.T) {
	in := []EntryInput{
		{Ticker: "XXX", Theme: "A", SetupLabel: sp("Breakdown Risk"), ExhScore: fp2(80)},
	}
	cands, tally := ComposeEntryCandidates(in, false)
	if len(cands) != 0 {
		t.Fatalf("want 0 survivors, got %d", len(cands))
	}
	if len(tally) != 1 || tally[0].G1 || tally[0].G2 {
		t.Errorf("tally should show XXX failing g1+g2: %+v", tally)
	}
}
