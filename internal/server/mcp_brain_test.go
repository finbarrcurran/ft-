package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeBrain stands in for the loopback brain connector.
func fakeBrain(t *testing.T, token string) (*[]string, func()) {
	t.Helper()
	var seen []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		var req struct {
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		b, _ := json.Marshal(req.Params.Arguments)
		seen = append(seen, req.Params.Name+" "+string(b))
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"brain says hi"}],"isError":false}}`))
	}))
	dir := t.TempDir()
	tf := filepath.Join(dir, "token")
	if err := os.WriteFile(tf, []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FT_BRAIN_URL", ts.URL+"/mcp")
	t.Setenv("FT_BRAIN_TOKEN_FILE", tf)
	return &seen, ts.Close
}

func mcpSession(t *testing.T, s *Server) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	ct, st := mcp.NewInMemoryTransports()
	srv := s.newMCPServer()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func toolNames(t *testing.T, cs *mcp.ClientSession) map[string]bool {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]bool{}
	for _, tl := range res.Tools {
		m[tl.Name] = true
	}
	return m
}

func resultText(r *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range r.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func TestBrainToolsForwardAndHideInDemoMode(t *testing.T) {
	s, st := snapServer(t)
	seen, closeFn := fakeBrain(t, "sekrit-token-value")
	defer closeFn()
	cs := mcpSession(t, s)
	ctx := context.Background()

	names := toolNames(t, cs)
	for _, n := range []string{"brain_search", "brain_sheet", "brain_read", "brain_stats", "search_transcripts"} {
		if !names[n] {
			t.Errorf("tool %s missing from tools/list", n)
		}
	}

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "brain_search", Arguments: map[string]any{"query": "gold", "limit": 3, "from": "2026-11-01"}})
	if err != nil || res.IsError || resultText(res) != "brain says hi" {
		t.Fatalf("brain_search: %v %+v", err, res)
	}
	res, _ = cs.CallTool(ctx, &mcp.CallToolParams{Name: "brain_read", Arguments: map[string]any{"doc_id": 727, "chunk_from": 2}})
	if res.IsError {
		t.Fatalf("brain_read: %s", resultText(res))
	}
	res, _ = cs.CallTool(ctx, &mcp.CallToolParams{Name: "brain_stats", Arguments: map[string]any{}})
	if res.IsError {
		t.Fatalf("brain_stats: %s", resultText(res))
	}
	want := []string{`brain_search {"from":"2026-11-01","limit":3,"query":"gold"}`, `brain_read {"chunk_from":2,"doc_id":727}`, `brain_stats {}`}
	if strings.Join(*seen, "|") != strings.Join(want, "|") {
		t.Fatalf("forwarded calls:\n got %v\nwant %v", *seen, want)
	}

	// Demo mode: hidden from the list (same session) and refused if called anyway.
	mustExec(t, st, `INSERT OR REPLACE INTO user_preferences (key, value, updated_at) VALUES ('demo_mode','on',0)`)
	names = toolNames(t, cs)
	for n := range names {
		if strings.HasPrefix(n, "brain_") {
			t.Errorf("%s still listed in demo mode", n)
		}
	}
	if !names["search_transcripts"] {
		t.Error("demo mode must not remove the other tools")
	}
	before := len(*seen)
	res, _ = cs.CallTool(ctx, &mcp.CallToolParams{Name: "brain_search", Arguments: map[string]any{"query": "gold"}})
	if !res.IsError || !strings.Contains(resultText(res), "demo mode") || len(*seen) != before {
		t.Fatalf("demo-mode call must be refused without reaching the brain: %+v", res)
	}
}

func TestBrainToolErrorsNeverLeakTokenAndRefuseNonLoopback(t *testing.T) {
	_, closeFn := fakeBrain(t, "right-token")
	defer closeFn()
	ctx := context.Background()

	// Wrong token on file: the connector answers 401; the message is fixed and carries no token.
	tf := os.Getenv("FT_BRAIN_TOKEN_FILE")
	if err := os.WriteFile(tf, []byte("wrong-token-xyz\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	res := callBrain(ctx, "brain_stats", nil)
	txt := resultText(res)
	if !res.IsError || strings.Contains(txt, "wrong-token-xyz") || !strings.Contains(txt, "rejected") {
		t.Fatalf("401 handling: %+v", res)
	}

	// Missing token file.
	t.Setenv("FT_BRAIN_TOKEN_FILE", filepath.Join(t.TempDir(), "nope"))
	if res := callBrain(ctx, "brain_stats", nil); !res.IsError || !strings.Contains(resultText(res), "not readable") {
		t.Fatalf("missing token file: %+v", res)
	}

	// A non-loopback URL is refused before any token is read or sent.
	for _, u := range []string{"http://example.com/mcp", "https://127.0.0.1/mcp", "http://10.0.0.5:18765/mcp", "http://localhost.evil.test/mcp"} {
		t.Setenv("FT_BRAIN_URL", u)
		if _, err := brainEndpoint(); err == nil {
			t.Errorf("%s must be refused", u)
		}
	}
	t.Setenv("FT_BRAIN_URL", "http://[::1]:18765/mcp")
	if _, err := brainEndpoint(); err != nil {
		t.Errorf("::1 loopback must be allowed: %v", err)
	}
}
