package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"ft/internal/videodigest"
)

// FT-157: the 22v-* transcripts are private, so demo mode leaves them out of search and counts.
func TestTranscriptSearchHidesPrivate22vInDemoMode(t *testing.T) {
	s, st := snapServer(t)
	ctx := context.Background()
	for _, r := range []videodigest.TranscriptRow{
		{VideoID: "aaaaaaaaaaa", Source: "cowen", Channel: "Benjamin Cowen", Title: "Public", URL: "https://youtu.be/aaaaaaaaaaa", PublishedAt: "2026-09-07", Text: "[0s] palantir is discussed here"},
		{VideoID: "22v-2538", Source: "jordi", Channel: "22V", Title: "Private", URL: "https://ai.22vresearch.com/video-stream/2538/", PublishedAt: "2026-11-11", Text: "palantir is discussed in the private video"},
	} {
		if _, err := s.videoDigest.StoreTranscript(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	get := func() (hits int, total float64) {
		rec := httptest.NewRecorder()
		s.handleTranscriptSearch(rec, httptest.NewRequest(http.MethodGet, "/api/transcripts/search?q=palantir", nil).WithContext(ctx))
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return len(out["hits"].([]any)), out["corpus"].(map[string]any)["total"].(float64)
	}
	if h, n := get(); h != 2 || n != 2 {
		t.Fatalf("demo off: want 2 hits and 2 in corpus, got %d / %v", h, n)
	}
	mustExec(t, st, `INSERT OR REPLACE INTO user_preferences (key, value, updated_at) VALUES ('demo_mode','on',0)`)
	if h, n := get(); h != 1 || n != 1 {
		t.Fatalf("demo on: want 1 public hit and 1 in corpus, got %d / %v", h, n)
	}
}
