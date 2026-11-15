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

// FT-146 PR C: the seven review-queue proposals Fin approved on 2026-11-14 (and nothing else).
func TestApprovedReviewQueueAliases(t *testing.T) {
	for name, want := range map[string]string{
		"USDC": "USDC", "Ada": "ADA", "eth Bitcoin": "ETH", "ethusd": "ETH", "ADA Bitcoin": "ADA", "SOL Bitcoin": "SOL", "Palunteer": "PLTR",
	} {
		got := Default.Tickers(name)
		if len(got) != 1 || got[0] != want {
			t.Errorf("%q maps to %v, want [%s]", name, got, want)
		}
	}
	// names Fin did not approve stay unresolved
	for _, name := range []string{"ETH Bitcoin pair", "Ray Dalio", "energy stocks"} { // "Palanteer" and "Soul Bitcoin" were approved later (SC-88)
		if got := Default.Tickers(name); len(got) != 0 {
			t.Errorf("%q must stay unresolved, maps to %v", name, got)
		}
	}
}

// FT-148 / SC-88: the alias candidates Fin approved on 2026-11-15, and nothing else from that list.
func TestApprovedAliasesRound2(t *testing.T) {
	for name, want := range map[string]string{
		"Salana": "SOL", "salano": "SOL", "Soul Bitcoin": "SOL", "Sole Bitcoin": "SOL", "ETH/USD": "ETH", "Palanteer": "PLTR",
		"ExxonMobil": "XOM", "Integris": "ENTG", "Enterris": "ENTG", "Hyperlquid": "HYPE", "Marlell": "MRVL", "Microns": "MU",
		"Eli Lilly (LLY)": "LLY",
	} {
		got := Default.Tickers(name)
		if len(got) != 1 || got[0] != want {
			t.Errorf("%q maps to %v, want [%s]", name, got, want)
		}
	}
	// rejected by Fin: every fuzzy proposal that was not approved above
	for _, name := range []string{"Marvel", "synopsis", "Brazil (EWZ)", "Altcoins", "altcoin", "Litecoin", "Soitec", "Costco", "Integrys", "Harvey", "Pax Gold", "memecoin", "Dot Bitcoin", "Avac Bitcoin", "alt USD", "Arweave", "Sharplink", "Mosaic"} {
		if got := Default.Tickers(name); len(got) != 0 {
			t.Errorf("%q must stay unmapped, maps to %v", name, got)
		}
	}
}
