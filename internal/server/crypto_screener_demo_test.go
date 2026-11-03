package server

import (
	"testing"

	"ft/internal/cryptoscreener"
)

func TestOverlayCryptoRowsFlags(t *testing.T) {
	rows := []cryptoscreener.Row{{Symbol: "BTC", Category: "Currency"}, {Symbol: "ETH", Category: "Chain"}, {Symbol: "PEPE", Category: "Memecoin"}}
	out, cats := overlayCryptoRows(rows, map[string]bool{"BTC": true}, map[string]bool{"ETH": true})
	if !out[0].Held || out[0].OnWatchlist || out[1].Held || !out[1].OnWatchlist || out[2].Held || out[2].OnWatchlist {
		t.Fatalf("demo off: flags wrong: %+v", out)
	}
	if len(cats) != 3 {
		t.Fatalf("categories %v", cats)
	}
	// demo mode hands the function empty sets: no flag anywhere
	demo, _ := overlayCryptoRows(rows, map[string]bool{}, map[string]bool{})
	for _, r := range demo {
		if r.Held || r.OnWatchlist {
			t.Fatalf("demo on: %s still flagged", r.Symbol)
		}
	}
}
