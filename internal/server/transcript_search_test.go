package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"ft/internal/videodigest"
)

func TestTranscriptSearchEndpoint(t *testing.T) {
	s, _ := snapServer(t)
	ctx := context.Background()
	if _, err := s.videoDigest.StoreTranscript(ctx, videodigest.TranscriptRow{VideoID: "aaaaaaaaaaa", Source: "cowen", Channel: "Benjamin Cowen",
		Title: "Bitcoin risk", URL: "https://youtu.be/aaaaaaaaaaa", PublishedAt: "2026-09-07",
		Text: "[0s] hello\n[30s] the risk metric says accumulate slowly"}); err != nil {
		t.Fatal(err)
	}
	get := func(qs string) (int, map[string]any) {
		rec := httptest.NewRecorder()
		s.handleTranscriptSearch(rec, httptest.NewRequest(http.MethodGet, "/api/transcripts/search?"+qs, nil).WithContext(ctx))
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	code, out := get(`q=%22risk+metric%22&source=cowen`)
	hits, _ := out["hits"].([]any)
	if code != 200 || len(hits) != 1 {
		t.Fatalf("search: %d %v", code, out)
	}
	h := hits[0].(map[string]any)
	if h["videoId"] != "aaaaaaaaaaa" || h["publishedAt"] != "2026-09-07" || h["tsSec"].(float64) != 30 {
		t.Fatalf("hit: %v", h)
	}
	if corpus, _ := out["corpus"].(map[string]any); corpus["total"].(float64) != 1 {
		t.Fatalf("corpus: %v", out["corpus"])
	}
	if code, out := get(`q=zebra`); code != 200 || len(out["hits"].([]any)) != 0 {
		t.Fatalf("no match must be an empty list: %d %v", code, out)
	}
	for _, qs := range []string{``, `q=x&source=bogus`, `q=x&limit=0`, `q=x&from=yesterday`} {
		if code, _ := get(qs); code != http.StatusBadRequest {
			t.Errorf("%q must be a 400, got %d", qs, code)
		}
	}
}
