package health

import "testing"

func TestCoinGeckoEndpoint(t *testing.T) {
	cases := map[string]string{
		"https://api.coingecko.com/api/v3/coins/markets?vs_currency=usd&ids=a,b": "markets",
		"https://api.coingecko.com/api/v3/coins/bitcoin/market_chart?days=365":   "market_chart",
		"https://api.coingecko.com/api/v3/global":                                "global",
		"https://api.coingecko.com/api/v3/simple/price?ids=bitcoin":              "simple_price",
		"https://api.coingecko.com/api/v3/coins/bitcoin?localization=false":      "other",
		"http://127.0.0.1:5555/coins/markets":                                    "markets",
	}
	for u, want := range cases {
		if got := CoinGeckoEndpoint(u); got != want {
			t.Errorf("%s = %q, want %q", u, got, want)
		}
	}
}
