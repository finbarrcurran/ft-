package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ft/internal/videolinks"
)

// FT-146 PR B: the per-holding commentary endpoint is off until the link audit passes, then serves the links.
func TestHoldingCommentaryEndpoint(t *testing.T) {
	s, st := snapServer(t)
	ctx := context.Background()
	mustExec(t, st, `INSERT INTO stock_holdings (id, user_id, name, ticker, invested_usd, avg_open_price, current_price, updated_at) VALUES (31, 1, 'Palantir', 'PLTR', 100, 10, 12, 1)`)
	mustExec(t, st, `INSERT INTO video_digests (id, source, video_id, title, published_at, package_path, ingested_at) VALUES (1,'jordi','vidA','Weekly',?, '/x',1)`, time.Now().UTC().AddDate(0, 0, -3).Format("2006-01-02"))
	mustExec(t, st, `INSERT INTO video_mentions (digest_id, name, type, context, stance) VALUES (1,'Palantir','stock','Likes the AI software story.','bullish')`)
	if _, err := videolinks.Rebuild(ctx, st.DB, time.Now()); err != nil {
		t.Fatal(err)
	}
	call := func(kind, id, query string) (int, string) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/holdings/"+kind+"/"+id+"/commentary"+query, nil)
		req.SetPathValue("kind", kind)
		req.SetPathValue("id", id)
		s.handleHoldingCommentary(rec, req.WithContext(context.WithValue(context.Background(), ctxUserID, int64(1))))
		return rec.Code, rec.Body.String()
	}
	// off by default: nothing is read or returned
	if code, b := call("stock", "31", ""); code != 200 || !strings.Contains(b, `"enabled":false`) || strings.Contains(b, "Palantir") {
		t.Fatalf("default off: %d %s", code, b)
	}
	if err := st.SetPreference(ctx, "video_links_panel", "on"); err != nil {
		t.Fatal(err)
	}
	if code, b := call("stock", "31", "?days=30&limit=5"); code != 200 || !strings.Contains(b, `"enabled":true`) || !strings.Contains(b, "Likes the AI software story.") ||
		!strings.Contains(b, `"snippetKind":"note"`) || !strings.Contains(b, "Context only") || !strings.Contains(b, "https://youtu.be/vidA?t=0") {
		t.Fatalf("on: %d %s", code, b)
	}
	if code, _ := call("bond", "31", ""); code != http.StatusBadRequest {
		t.Fatalf("bad kind: %d", code)
	}
	if code, b := call("stock", "999", ""); code != 200 || !strings.Contains(b, `"commentary":null`) {
		t.Fatalf("unknown holding: %d %s", code, b)
	}
	// demo mode: the same public commentary, no position sizes or values anywhere in the payload
	if err := st.SetPreference(ctx, "demo_mode", "on"); err != nil {
		t.Fatal(err)
	}
	if _, b := call("stock", "31", ""); strings.Contains(b, "invested") || strings.Contains(b, "current_price") {
		t.Fatalf("demo payload carries position data: %s", b)
	}
}

func TestVideoLinksPanelPreferenceValidation(t *testing.T) {
	for v, ok := range map[string]bool{"on": true, "off": true, "maybe": false, "": false} {
		if got := validPreferenceValue("video_links_panel", v); got != ok {
			t.Errorf("video_links_panel=%q: valid=%v, want %v", v, got, ok)
		}
	}
}
