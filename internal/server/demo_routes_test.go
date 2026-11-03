package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDemoRouteWrappers(t *testing.T) {
	s, st := snapServer(t)
	ran := 0
	next := func(w http.ResponseWriter, r *http.Request) { ran++; w.Write([]byte(`{"real":"portfolio data"}`)) }
	deny := s.denyInDemo(next)
	empty := s.emptyInDemo(next, map[string]any{"watchlist": []any{}, "demo": true})
	call := func(h http.HandlerFunc) (int, string) {
		rec := httptest.NewRecorder()
		h(rec, httptest.NewRequest("GET", "/x", nil))
		return rec.Code, rec.Body.String()
	}

	// demo off: both pass straight through
	if c, b := call(deny); c != 200 || !strings.Contains(b, "portfolio data") {
		t.Fatalf("deny, demo off: %d %s", c, b)
	}
	if c, b := call(empty); c != 200 || !strings.Contains(b, "portfolio data") {
		t.Fatalf("empty, demo off: %d %s", c, b)
	}
	if ran != 2 {
		t.Fatalf("handler ran %d times with demo off", ran)
	}

	// demo on: the handler never runs and nothing real is in the body
	mustExec(t, st, `INSERT OR REPLACE INTO user_preferences (key, value, updated_at) VALUES ('demo_mode','on',0)`)
	ran = 0
	if c, b := call(deny); c != http.StatusForbidden || strings.Contains(b, "portfolio") {
		t.Fatalf("deny, demo on: %d %s", c, b)
	}
	if c, b := call(empty); c != 200 || strings.Contains(b, "portfolio") || !strings.Contains(b, `"watchlist":[]`) {
		t.Fatalf("empty, demo on: %d %s", c, b)
	}
	if ran != 0 {
		t.Fatalf("handler ran %d times in demo mode", ran)
	}
}
