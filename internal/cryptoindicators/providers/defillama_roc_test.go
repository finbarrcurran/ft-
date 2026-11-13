package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func llamaServer(t *testing.T, points []float64, gapAt int) *DefiLlamaClient {
	t.Helper()
	end := time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)
	var parts []string
	for i, v := range points {
		if i == gapAt {
			continue // a missing day in the source
		}
		d := end.AddDate(0, 0, -(len(points) - 1 - i))
		parts = append(parts, `{"date":"`+strconv.FormatInt(d.Unix(), 10)+`","totalCirculatingUSD":{"peggedUSD":`+strconv.FormatFloat(v*1e9, 'f', 0, 64)+`}}`)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("[" + strings.Join(parts, ",") + "]")) }))
	t.Cleanup(srv.Close)
	c := NewDefiLlamaClient()
	c.HTTP = &http.Client{Transport: rewriteTransport{srv.URL}}
	return c
}

type rewriteTransport struct{ base string }

func (rt rewriteTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r2 := r.Clone(r.Context())
	u, _ := http.NewRequest("GET", rt.base, nil)
	r2.URL.Scheme, r2.URL.Host = u.URL.Scheme, u.URL.Host
	return http.DefaultTransport.RoundTrip(r2)
}

// FT-084: the 4-week change uses the entry dated 28 days back even when the source skips a day, and an implausible
// change is rejected rather than scored.
func TestFetchStablecoinSupply_ROCByDateAndGuard(t *testing.T) {
	// 40 days at 300 falling to 285 on the last: the day 28 back (index 11) is 300.
	pts := make([]float64, 40)
	for i := range pts {
		pts[i] = 300
	}
	pts[39] = 285
	c := llamaServer(t, pts, 12) // the source skips the day after the target day
	r := c.FetchStablecoinSupply(context.Background())
	if r.Err != "" || r.Trend4w == nil || *r.Trend4w < -5.01 || *r.Trend4w > -4.99 {
		t.Fatalf("roc: %+v err=%q", r.Trend4w, r.Err)
	}
	// a glitch: supply 28 days back reads 70 -> +300% is rejected
	pts2 := make([]float64, 40)
	for i := range pts2 {
		pts2[i] = 70
	}
	pts2[39] = 290
	for i := 20; i < 39; i++ {
		pts2[i] = 280
	}
	c2 := llamaServer(t, pts2, -1)
	if r := c2.FetchStablecoinSupply(context.Background()); r.Err == "" || r.Trend4w != nil {
		t.Fatalf("an implausible change must be rejected: %+v", r)
	}
}
