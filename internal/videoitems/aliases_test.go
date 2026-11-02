package videoitems

import "testing"

func TestAliasStrikeListAndKeeps(t *testing.T) {
	for _, n := range []string{"gold", "silver", "block", "arm", "ark", "ether", "riot", "marathon", "cipher"} {
		if len(Default.Tickers(n)) > 0 {
			t.Errorf("%q must not ground a ticker (struck 2026-11-01)", n)
		}
	}
	for n, tk := range map[string]string{"meta": "META", "intel": "INTC", "constellation": "CEG", "dell": "DELL", "zcash": "ZEC", "ford": "F", "lumentum": "LITE", "spacex": "SPCX"} {
		got := Default.Tickers(n)
		if len(got) == 0 || got[0] != tk {
			t.Errorf("%q should map to %s, got %v", n, tk, got)
		}
	}
}

func TestFuzzyASRSpelling(t *testing.T) {
	tk, matched, dist, ok := Default.Fuzzy("Lummentum", 2)
	if !ok || tk[0] != "LITE" || dist > 2 {
		t.Fatalf("Lummentum -> %v %q %d %v", tk, matched, dist, ok)
	}
	if _, _, _, ok := Default.Fuzzy("armada", 2); ok {
		t.Fatal("a near miss on an unrelated word must not match")
	}
	if _, _, _, ok := Default.Fuzzy("Meta", 2); ok {
		t.Fatal("names under 6 letters are never fuzzy-matched")
	}
}
