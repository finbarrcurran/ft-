package providers

import (
	"strconv"
	"testing"
	"time"
)

// FT-084: the ICE DXY reading is the latest close with the % change against the close on or before 28 days earlier.
func TestParseICEDXY(t *testing.T) {
	day := func(d string) int64 {
		tm, _ := time.Parse("2006-01-02", d)
		return tm.Unix()
	}
	body := []byte(`{"chart":{"result":[{"timestamp":[` +
		strconv.FormatInt(day("2026-10-14"), 10) + `,` + strconv.FormatInt(day("2026-10-16"), 10) + `,` + strconv.FormatInt(day("2026-11-12"), 10) + `,` + strconv.FormatInt(day("2026-11-13"), 10) +
		`],"indicators":{"quote":[{"close":[100.0,101.0,null,103.0]}]}}]}}`)
	r := ParseICEDXY(body)
	if r.Err != "" || r.Value == nil || *r.Value != 103.0 || r.AsOf != "2026-11-13" {
		t.Fatalf("latest: %+v", r)
	}
	// 28 days before 2026-11-13 is 2026-10-16 (101.0): +1.98%
	if r.Trend4w == nil || *r.Trend4w < 1.97 || *r.Trend4w > 1.99 {
		t.Fatalf("trend: %v", r.Trend4w)
	}
	if bad := ParseICEDXY([]byte(`{"chart":{"result":[]}}`)); bad.Err == "" {
		t.Fatal("an empty response must be an error")
	}
}
