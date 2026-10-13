package providers

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type fixtureTransport struct{ body string }

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(f.body)), Request: r}, nil
}

// SC-49b: the readings carry the date of the data they used.
func TestFREDReadingCarriesObservationDate(t *testing.T) {
	c := &FREDClient{APIKey: "x", HTTP: &http.Client{Transport: fixtureTransport{`{"observations":[
		{"date":"2026-10-02","value":"121.38"},{"date":"2026-10-01","value":"121.78"},{"date":"2026-09-30","value":"."}]}`}}}
	for name, r := range map[string]Reading{"FetchSeries": c.FetchSeries(context.Background(), "DTWEXBGS"), "AbsoluteTrend": c.FetchSeriesAbsoluteTrend(context.Background(), "CFNAI")} {
		if r.Err != "" || r.Value == nil || r.AsOf != "2026-10-02" {
			t.Fatalf("%s: %+v", name, r)
		}
	}
	// A missing "." newest observation is skipped, so the date is the newest REAL value.
	c.HTTP = &http.Client{Transport: fixtureTransport{`{"observations":[{"date":"2026-10-09","value":"."},{"date":"2026-10-08","value":"4.75"}]}`}}
	if r := c.FetchSeries(context.Background(), "DGS2"); r.AsOf != "2026-10-08" {
		t.Fatalf("missing newest value: %+v", r)
	}
}

func TestDefiLlamaReadingCarriesNewestPointDate(t *testing.T) {
	// 1791849600 = 2026-10-13T00:00:00Z, 1791763200 = 2026-10-12.
	c := &DefiLlamaClient{HTTP: &http.Client{Transport: fixtureTransport{`[
		{"date":"1791763200","totalCirculatingUSD":{"peggedUSD":310000000000}},
		{"date":"1791849600","totalCirculatingUSD":{"peggedUSD":311000000000}}]`}}}
	r := c.FetchStablecoinSupply(context.Background())
	if r.Err != "" || r.Value == nil || r.AsOf != "2026-10-13" {
		t.Fatalf("%+v", r)
	}
}
