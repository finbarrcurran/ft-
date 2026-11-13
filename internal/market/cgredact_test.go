package market

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// SC-70 (FT-127): the Diagnostics provider error for a failed CoinGecko call must not list the held coin ids.
func TestRedactQueryRemovesCoinIDs(t *testing.T) {
	var out map[string]any
	err := cgGetJSON(context.Background(), "http://127.0.0.1:1/api/v3/coins/markets?ids=bitcoin%2Cethereum%2Csolana&vs_currency=usd", false, 0, &out)
	if err == nil {
		t.Fatal("expected a connection error")
	}
	m := err.Error()
	if strings.Contains(m, "bitcoin") || strings.Contains(m, "ethereum") || strings.Contains(m, "ids=") {
		t.Fatalf("error still carries the query: %s", m)
	}
	if !strings.Contains(m, "/api/v3/coins/markets?<redacted>") {
		t.Fatalf("the path should stay readable: %s", m)
	}
	var re *redactedErr
	if !errors.As(err, &re) || re.Unwrap() == nil {
		t.Fatal("the original error must stay reachable")
	}
	// an error with no URL is returned as is
	plain := errors.New("http 429: rate limit")
	if redactQuery(plain) != plain {
		t.Fatal("an error without a query must be untouched")
	}
	if redactQuery(nil) != nil {
		t.Fatal("nil stays nil")
	}
}
