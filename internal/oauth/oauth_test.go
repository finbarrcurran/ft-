package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ft/internal/store"
)

type fakeAuth struct{}

func (fakeAuth) Authenticate(_ context.Context, email, pw string) (int64, bool) {
	return 1, email == "fin@example.test" && pw == "right"
}

type fakeAlerts struct{ msgs []string }

func (f *fakeAlerts) Alert(_ context.Context, t string) { f.msgs = append(f.msgs, t) }

const resource = "https://ft.example.test/mcp"

func setup(t *testing.T) (*Service, *httptest.Server, *fakeAlerts, *time.Time) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "ft.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	al := &fakeAlerts{}
	svc := New(st.DB, Config{Issuer: "https://ft.example.test", Resource: resource}, fakeAuth{}, al)
	clock := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	svc.Now = func() time.Time { return clock }
	mux := http.NewServeMux()
	svc.Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return svc, srv, al, &clock
}

func noRedirect() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func pkce() (verifier, challenge string) {
	verifier = strings.Repeat("a1b2c3d4e5", 5) // 50 chars
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

func register(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	body := `{"client_name":"Claude","redirect_uris":["` + ClaudeCallback + `"]}`
	resp, err := http.Post(srv.URL+"/register", "application/json", strings.NewReader(body))
	if err != nil || resp.StatusCode != 201 {
		t.Fatalf("register: %v %v", err, resp)
	}
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return out["client_id"].(string)
}

func authorize(t *testing.T, srv *httptest.Server, cid, challenge, pw, decision string) *http.Response {
	t.Helper()
	f := url.Values{"client_id": {cid}, "redirect_uri": {ClaudeCallback}, "state": {"xyz"},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"}, "resource": {resource},
		"response_type": {"code"}, "email": {"fin@example.test"}, "password": {pw}, "decision": {decision}}
	resp, err := noRedirect().PostForm(srv.URL+"/authorize", f)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func token(t *testing.T, srv *httptest.Server, f url.Values) (int, map[string]any) {
	t.Helper()
	resp, err := http.PostForm(srv.URL+"/token", f)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func getCode(t *testing.T, srv *httptest.Server, cid, challenge string) string {
	t.Helper()
	resp := authorize(t, srv, cid, challenge, "right", "approve")
	loc, _ := url.Parse(resp.Header.Get("Location"))
	if resp.StatusCode != 302 || loc.Query().Get("code") == "" || loc.Query().Get("state") != "xyz" {
		t.Fatalf("approve: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	if !strings.HasPrefix(loc.String(), ClaudeCallback) {
		t.Fatalf("redirected off the registered callback: %s", loc)
	}
	return loc.Query().Get("code")
}

func TestDiscoveryAndNoHTML(t *testing.T) {
	_, srv, _, _ := setup(t)
	for _, p := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-authorization-server"} {
		resp, _ := http.Get(srv.URL + p)
		var m map[string]any
		json.NewDecoder(resp.Body).Decode(&m)
		if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") || len(m) == 0 {
			t.Fatalf("%s: %d %s", p, resp.StatusCode, resp.Header.Get("Content-Type"))
		}
	}
	resp, _ := http.Get(srv.URL + "/.well-known/oauth-protected-resource")
	var pr map[string]any
	json.NewDecoder(resp.Body).Decode(&pr)
	if pr["resource"] != resource {
		t.Fatalf("resource = %v", pr["resource"])
	}
	resp, _ = http.Get(srv.URL + "/.well-known/anything-else")
	if resp.StatusCode != 404 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("unknown well-known: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	resp, _ = http.Get(srv.URL + "/.well-known/oauth-authorization-server")
	var as map[string]any
	json.NewDecoder(resp.Body).Decode(&as)
	if m := as["code_challenge_methods_supported"].([]any); len(m) != 1 || m[0] != "S256" {
		t.Fatalf("PKCE advert: %v", m)
	}
}

func TestRegistrationRules(t *testing.T) {
	_, srv, _, _ := setup(t)
	bad := `{"client_name":"x","redirect_uris":["https://evil.example/cb"]}`
	resp, _ := http.Post(srv.URL+"/register", "application/json", strings.NewReader(bad))
	if resp.StatusCode != 400 {
		t.Fatalf("foreign redirect accepted: %d", resp.StatusCode)
	}
	ok := `{"client_name":"cc","redirect_uris":["http://localhost/callback"]}`
	resp, _ = http.Post(srv.URL+"/register", "application/json", strings.NewReader(ok))
	if resp.StatusCode != 201 {
		t.Fatalf("loopback rejected: %d", resp.StatusCode)
	}
	// rate limit: 10 attempts per hour per IP (2 used above)
	got429 := false
	for i := 0; i < 12; i++ {
		resp, _ = http.Post(srv.URL+"/register", "application/json", strings.NewReader(ok))
		if resp.StatusCode == 429 {
			got429 = true
			break
		}
	}
	if !got429 {
		t.Fatal("registration is not rate limited")
	}
}

func TestFullFlowRefreshRotationAndReuse(t *testing.T) {
	svc, srv, al, clock := setup(t)
	cid := register(t, srv)
	verifier, challenge := pkce()

	// Authorize page renders and refuses a foreign redirect.
	q := url.Values{"client_id": {cid}, "redirect_uri": {ClaudeCallback}, "response_type": {"code"},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"}, "resource": {resource}}
	resp, _ := http.Get(srv.URL + "/authorize?" + q.Encode())
	b := new(strings.Builder)
	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	b.Write(buf[:n])
	if resp.StatusCode != 200 || !strings.Contains(b.String(), "read-only access") || resp.Header.Get("X-Frame-Options") != "DENY" {
		t.Fatalf("authorize page: %d", resp.StatusCode)
	}
	q.Set("redirect_uri", "https://evil.example/cb")
	resp, _ = http.Get(srv.URL + "/authorize?" + q.Encode())
	if resp.StatusCode != 400 {
		t.Fatalf("foreign redirect_uri on authorize: %d", resp.StatusCode)
	}

	code := getCode(t, srv, cid, challenge)
	form := url.Values{"grant_type": {"authorization_code"}, "client_id": {cid}, "code": {code},
		"redirect_uri": {ClaudeCallback}, "code_verifier": {"wrong" + verifier}, "resource": {resource}}
	if st, out := token(t, srv, form); st != 400 || out["error"] != "invalid_grant" {
		t.Fatalf("bad verifier accepted: %d %v", st, out)
	}
	// A failed PKCE check must not burn the code for the real client... but it must stay single-use after success.
	form.Set("code_verifier", verifier)
	st, out := token(t, srv, form)
	if st != 200 || out["token_type"] != "Bearer" || out["scope"] != "read" || out["expires_in"].(float64) != 3600 {
		t.Fatalf("exchange: %d %v", st, out)
	}
	access, refresh := out["access_token"].(string), out["refresh_token"].(string)
	if uid, ok := svc.ValidateAccess(context.Background(), access); !ok || uid != 1 {
		t.Fatal("fresh access token not valid")
	}
	if _, ok := svc.ValidateAccess(context.Background(), "ft_mcp_notanoauthtoken"); ok {
		t.Fatal("static token accepted by OAuth validator")
	}

	// Code replay revokes what the code produced.
	if st, out := token(t, srv, form); st != 400 || out["error"] != "invalid_grant" {
		t.Fatalf("code replay: %d %v", st, out)
	}
	if _, ok := svc.ValidateAccess(context.Background(), access); ok {
		t.Fatal("tokens from a replayed code survived")
	}

	// New authorization, then refresh rotation and reuse detection.
	code = getCode(t, srv, cid, challenge)
	form.Set("code", code)
	_, out = token(t, srv, form)
	refresh = out["refresh_token"].(string)
	rf := url.Values{"grant_type": {"refresh_token"}, "client_id": {cid}, "refresh_token": {refresh}}
	st, out2 := token(t, srv, rf)
	if st != 200 || out2["refresh_token"] == refresh {
		t.Fatalf("rotation: %d %v", st, out2)
	}
	newAccess := out2["access_token"].(string)
	if _, ok := svc.ValidateAccess(context.Background(), newAccess); !ok {
		t.Fatal("rotated access token invalid")
	}
	// Old refresh token presented again -> invalid_grant, family revoked, alert sent.
	if st, o := token(t, srv, rf); st != 400 || o["error"] != "invalid_grant" {
		t.Fatalf("reuse: %d %v", st, o)
	}
	if _, ok := svc.ValidateAccess(context.Background(), newAccess); ok {
		t.Fatal("family not revoked after refresh-token reuse")
	}
	if len(al.msgs) == 0 || !strings.Contains(al.msgs[len(al.msgs)-1], "spent refresh token") {
		t.Fatalf("no reuse alert: %v", al.msgs)
	}

	// Expiry: access token dies after 1 h.
	code = getCode(t, srv, cid, challenge)
	form.Set("code", code)
	_, out = token(t, srv, form)
	acc := out["access_token"].(string)
	*clock = clock.Add(61 * time.Minute)
	if _, ok := svc.ValidateAccess(context.Background(), acc); ok {
		t.Fatal("expired access token accepted")
	}
	// ... and a code older than 60 s is dead.
	_, ch2 := pkce()
	code = getCode(t, srv, cid, ch2)
	*clock = clock.Add(2 * time.Minute)
	form.Set("code", code)
	if st, o := token(t, srv, form); st != 400 || o["error"] != "invalid_grant" {
		t.Fatalf("expired code: %d %v", st, o)
	}
}

func TestResourceBindingAndContentType(t *testing.T) {
	_, srv, _, _ := setup(t)
	cid := register(t, srv)
	verifier, challenge := pkce()
	code := getCode(t, srv, cid, challenge)
	form := url.Values{"grant_type": {"authorization_code"}, "client_id": {cid}, "code": {code},
		"redirect_uri": {ClaudeCallback}, "code_verifier": {verifier}, "resource": {"https://other.example/mcp"}}
	if st, o := token(t, srv, form); st != 400 || o["error"] != "invalid_target" {
		t.Fatalf("foreign resource accepted: %d %v", st, o)
	}
	resp, _ := http.Post(srv.URL+"/token", "application/json", strings.NewReader(`{}`))
	if resp.StatusCode != 400 {
		t.Fatalf("JSON token request accepted: %d", resp.StatusCode)
	}
	// authorize with a foreign resource is refused (redirect with error)
	f := url.Values{"client_id": {cid}, "redirect_uri": {ClaudeCallback}, "code_challenge": {challenge},
		"code_challenge_method": {"S256"}, "resource": {"https://other.example/mcp"}, "response_type": {"code"},
		"email": {"fin@example.test"}, "password": {"right"}, "decision": {"approve"}}
	r2, _ := noRedirect().PostForm(srv.URL+"/authorize", f)
	if !strings.Contains(r2.Header.Get("Location"), "error=invalid_target") {
		t.Fatalf("foreign resource on authorize: %d %s", r2.StatusCode, r2.Header.Get("Location"))
	}
}

func TestLoginRateLimitAndDeny(t *testing.T) {
	_, srv, al, _ := setup(t)
	cid := register(t, srv)
	_, challenge := pkce()
	for i := 0; i < 5; i++ {
		if r := authorize(t, srv, cid, challenge, "wrong", "approve"); r.StatusCode != 401 {
			t.Fatalf("attempt %d: %d", i, r.StatusCode)
		}
	}
	// 6th is blocked even with the right password
	r := authorize(t, srv, cid, challenge, "right", "approve")
	if r.StatusCode != 429 || r.Header.Get("Retry-After") == "" {
		t.Fatalf("not rate limited: %d", r.StatusCode)
	}
	if len(al.msgs) != 1 || !strings.Contains(al.msgs[0], "failed approval logins") {
		t.Fatalf("expected one lockout alert, got %v", al.msgs)
	}
	// Deny never needs a password and redirects with access_denied.
	d := authorize(t, srv, cid, challenge, "", "deny")
	if !strings.Contains(d.Header.Get("Location"), "error=access_denied") {
		t.Fatalf("deny: %s", d.Header.Get("Location"))
	}
}

func TestLoopbackPortMatching(t *testing.T) {
	if !redirectMatches([]string{"http://localhost/callback"}, "http://localhost:3118/callback") {
		t.Fatal("loopback with port must match a portless registration")
	}
	if redirectMatches([]string{"http://localhost/callback"}, "http://localhost:3118/other") {
		t.Fatal("path must still match")
	}
	if redirectMatches([]string{ClaudeCallback}, "https://claude.ai/api/mcp/auth_callback?x=1") {
		t.Fatal("exact match required for https")
	}
}

func TestLimiterWindow(t *testing.T) {
	l := NewLimiter(2, time.Minute)
	now := time.Now()
	l.Now = func() time.Time { return now }
	l.Fail("k")
	if ok, _ := l.Allow("k"); !ok {
		t.Fatal("blocked after one failure")
	}
	l.Fail("k")
	if ok, retry := l.Allow("k"); ok || retry <= 0 {
		t.Fatal("not blocked after two failures")
	}
	now = now.Add(61 * time.Second)
	if ok, _ := l.Allow("k"); !ok {
		t.Fatal("still blocked after the window")
	}
}
