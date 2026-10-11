package oauth

import (
	"context"
	"encoding/json"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Mount registers every OAuth route on mux. They sit outside FT's cookie auth:
// each one authenticates itself (PKCE + password on /authorize, code/refresh on /token).
func (s *Service) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", s.handleProtectedResource)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/", s.handleProtectedResource)
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", s.handleAuthServer)
	mux.HandleFunc("GET /.well-known/oauth-authorization-server/", s.handleAuthServer)
	mux.HandleFunc("GET /.well-known/", func(w http.ResponseWriter, r *http.Request) { // never the SPA HTML
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
	})
	mux.HandleFunc("POST /register", s.handleRegister)
	mux.HandleFunc("GET /authorize", s.handleAuthorizeGet)
	mux.HandleFunc("POST /authorize", s.handleAuthorizePost)
	mux.HandleFunc("POST /token", s.handleToken)
}

// WWWAuthenticate is the header value /mcp must send with a 401.
func (s *Service) WWWAuthenticate() string {
	return `Bearer resource_metadata="` + s.Cfg.Issuer + `/.well-known/oauth-protected-resource", scope="read"`
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func oauthErr(w http.ResponseWriter, code int, e, desc string) {
	writeJSON(w, code, map[string]string{"error": e, "error_description": desc})
}

func (s *Service) ip(r *http.Request) string {
	if s.ClientIP != nil {
		return s.ClientIP(r)
	}
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

func (s *Service) alert(ctx context.Context, text string) {
	if s.Alerts != nil {
		s.Alerts.Alert(ctx, text)
	}
}

// --- discovery -------------------------------------------------------------

func (s *Service) handleProtectedResource(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 s.Cfg.Resource,
		"authorization_servers":    []string{s.Cfg.Issuer},
		"scopes_supported":         []string{ScopeRead},
		"bearer_methods_supported": []string{"header"},
	})
}

func (s *Service) handleAuthServer(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                s.Cfg.Issuer,
		"authorization_endpoint":                s.Cfg.Issuer + "/authorize",
		"token_endpoint":                        s.Cfg.Issuer + "/token",
		"registration_endpoint":                 s.Cfg.Issuer + "/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none", "client_secret_post", "client_secret_basic"},
		"scopes_supported":                      []string{ScopeRead, "offline_access"},
	})
}

// --- dynamic client registration (RFC 7591) --------------------------------

func (s *Service) handleRegister(w http.ResponseWriter, r *http.Request) {
	ip := s.ip(r)
	if ok, retry := s.regFail.Allow(ip); !ok {
		w.Header().Set("Retry-After", itoa(int(retry.Seconds())+1))
		oauthErr(w, http.StatusTooManyRequests, "temporarily_unavailable", "too many registrations")
		return
	}
	s.regFail.Fail(ip) // every attempt counts: registration is rare
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	var req struct {
		ClientName   string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		oauthErr(w, http.StatusBadRequest, "invalid_client_metadata", "bad JSON")
		return
	}
	if len(req.ClientName) > 100 {
		req.ClientName = req.ClientName[:100]
	}
	id, err := s.RegisterClient(r.Context(), req.ClientName, req.RedirectURIs, "", "dcr")
	switch {
	case err == ErrClientLimit:
		oauthErr(w, http.StatusServiceUnavailable, "temporarily_unavailable", "client limit reached")
		return
	case err != nil:
		oauthErr(w, http.StatusBadRequest, "invalid_redirect_uri", err.Error())
		return
	}
	slog.Info("oauth: client registered", "client", id, "name", req.ClientName, "ip", ip)
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  id,
		"client_name":                req.ClientName,
		"redirect_uris":              req.RedirectURIs,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	})
}

// --- /authorize ------------------------------------------------------------

type authReq struct {
	ClientID, RedirectURI, State, Challenge, Resource, Method, Type string
	Client                                                           *Client
}

var authPage = template.Must(template.New("a").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>Authorize access to FT</title>
<style>body{font:16px system-ui,sans-serif;max-width:30rem;margin:3rem auto;padding:0 1rem;color:#222;background:#fafafa}
@media(prefers-color-scheme:dark){body{color:#e6e6e6;background:#161616}input{background:#222;color:#eee;border:1px solid #555}}
h1{font-size:1.25rem}.box{border:1px solid #8886;border-radius:8px;padding:1rem;margin:1rem 0}
input{width:100%;box-sizing:border-box;padding:.6rem;margin:.3rem 0 .8rem;border-radius:6px;border:1px solid #999;font-size:1rem}
button{padding:.6rem 1.1rem;font-size:1rem;border-radius:6px;border:1px solid #888;cursor:pointer}
.go{background:#1a7f37;color:#fff;border-color:#1a7f37}.err{color:#c62828;font-weight:600}.dim{opacity:.7;font-size:.9rem}</style></head><body>
<h1>Claude wants read-only access to FT</h1>
<div class="box"><p><strong>{{.Name}}</strong> is asking to read your FT data: holdings, theses, scorecards, alerts, performance and watchlist. It <strong>cannot change anything</strong>.</p>
<p class="dim">It will be sent back to: <strong>{{.Host}}</strong></p></div>
{{if .Err}}<p class="err">{{.Err}}</p>{{end}}
<form method="post" action="/authorize">
{{range $k, $v := .Hidden}}<input type="hidden" name="{{$k}}" value="{{$v}}">{{end}}
<label>FT email<input name="email" type="email" autocomplete="username" required></label>
<label>FT password<input name="password" type="password" autocomplete="current-password" required></label>
<button class="go" name="decision" value="approve">Approve</button>
<button name="decision" value="deny" formnovalidate>Deny</button>
</form><p class="dim">Only approve if you just clicked Connect in Claude.</p></body></html>`))

func (s *Service) parseAuth(r *http.Request, get func(string) string) (*authReq, string) {
	a := &authReq{
		ClientID: get("client_id"), RedirectURI: get("redirect_uri"), State: get("state"),
		Challenge: get("code_challenge"), Method: get("code_challenge_method"),
		Resource: get("resource"), Type: get("response_type"),
	}
	c, err := s.client(r.Context(), a.ClientID)
	if err != nil {
		return nil, "Unknown client."
	}
	if !redirectMatches(c.RedirectURIs, a.RedirectURI) {
		return nil, "The redirect address does not match this client."
	}
	a.Client = c
	return a, ""
}

func (a *authReq) invalid(s *Service) string {
	switch {
	case a.Type != "code":
		return "unsupported_response_type"
	case a.Method != "S256" || len(a.Challenge) < 43 || len(a.Challenge) > 128:
		return "invalid_request"
	case a.Resource != "" && a.Resource != s.Cfg.Resource:
		return "invalid_target"
	}
	return ""
}

func redirectErr(w http.ResponseWriter, r *http.Request, a *authReq, code string) {
	u, _ := url.Parse(a.RedirectURI)
	q := u.Query()
	q.Set("error", code)
	if a.State != "" {
		q.Set("state", a.State)
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func secHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Type", "text/html; charset=utf-8")
}

func (s *Service) render(w http.ResponseWriter, a *authReq, errMsg string, code int) {
	secHeaders(w)
	w.WriteHeader(code)
	host := a.RedirectURI
	if u, err := url.Parse(a.RedirectURI); err == nil {
		host = u.Host
	}
	name := a.Client.Name
	if name == "" {
		name = "An application"
	}
	_ = authPage.Execute(w, map[string]any{
		"Name": name, "Host": host, "Err": errMsg,
		"Hidden": map[string]string{
			"client_id": a.ClientID, "redirect_uri": a.RedirectURI, "state": a.State,
			"code_challenge": a.Challenge, "code_challenge_method": a.Method,
			"resource": a.Resource, "response_type": a.Type,
		},
	})
}

func plainPage(w http.ResponseWriter, code int, msg string) {
	secHeaders(w)
	w.WriteHeader(code)
	_ = template.Must(template.New("p").Parse(`<!doctype html><meta charset="utf-8"><title>FT</title><body style="font:16px system-ui;max-width:30rem;margin:3rem auto"><p>{{.}}</p></body>`)).Execute(w, msg)
}

func (s *Service) handleAuthorizeGet(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	a, msg := s.parseAuth(r, q.Get)
	if a == nil {
		plainPage(w, http.StatusBadRequest, msg)
		return
	}
	if e := a.invalid(s); e != "" {
		redirectErr(w, r, a, e)
		return
	}
	s.render(w, a, "", http.StatusOK)
}

func (s *Service) handleAuthorizePost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		plainPage(w, http.StatusBadRequest, "Bad request.")
		return
	}
	a, msg := s.parseAuth(r, r.PostForm.Get)
	if a == nil {
		plainPage(w, http.StatusBadRequest, msg)
		return
	}
	if e := a.invalid(s); e != "" {
		redirectErr(w, r, a, e)
		return
	}
	if r.PostForm.Get("decision") == "deny" {
		redirectErr(w, r, a, "access_denied")
		return
	}
	ip := s.ip(r)
	okIP, retry := s.loginFail.Allow(ip)
	okAll, retryAll := s.loginAll.Allow("all")
	if !okIP || !okAll {
		if retryAll > retry {
			retry = retryAll
		}
		w.Header().Set("Retry-After", itoa(int(retry.Seconds())+1))
		s.render(w, a, "Too many failed attempts. Try again later.", http.StatusTooManyRequests)
		return
	}
	uid, ok := int64(0), false
	if s.Auth != nil {
		uid, ok = s.Auth.Authenticate(r.Context(), strings.TrimSpace(r.PostForm.Get("email")), r.PostForm.Get("password"))
	}
	if !ok {
		tripped := s.loginFail.Fail(ip)
		tripAll := s.loginAll.Fail("all")
		slog.Warn("oauth: failed authorize login", "ip", ip)
		if tripped || tripAll {
			s.alert(r.Context(), "🟠 FT OAuth: repeated failed approval logins (client IP "+ip+"). Further attempts are blocked for a while.")
		}
		s.render(w, a, "Wrong email or password.", http.StatusUnauthorized)
		return
	}
	s.loginFail.Reset(ip)
	code, err := s.NewCode(r.Context(), a.ClientID, uid, a.RedirectURI, a.Challenge, s.Cfg.Resource)
	if err != nil {
		slog.Error("oauth: new code", "err", err)
		plainPage(w, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	slog.Info("oauth: authorization approved", "client", a.ClientID, "ip", ip)
	u, _ := url.Parse(a.RedirectURI)
	q := u.Query()
	q.Set("code", code)
	if a.State != "" {
		q.Set("state", a.State)
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

// --- /token ----------------------------------------------------------------

func (s *Service) handleToken(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/x-www-form-urlencoded") {
		oauthErr(w, http.StatusBadRequest, "invalid_request", "token requests must be application/x-www-form-urlencoded")
		return
	}
	if err := r.ParseForm(); err != nil {
		oauthErr(w, http.StatusBadRequest, "invalid_request", "bad form")
		return
	}
	id, secret, hasBasic := r.BasicAuth()
	if !hasBasic {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	c, err := s.client(r.Context(), id)
	if err != nil {
		oauthErr(w, http.StatusUnauthorized, "invalid_client", "unknown client")
		return
	}
	if c.SecretHash != "" && Hash(secret) != c.SecretHash {
		oauthErr(w, http.StatusUnauthorized, "invalid_client", "bad client secret")
		return
	}
	var tp *TokenPair
	var e *Error
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		if !redirectMatches(c.RedirectURIs, r.PostForm.Get("redirect_uri")) {
			oauthErr(w, http.StatusBadRequest, "invalid_grant", "redirect_uri mismatch")
			return
		}
		tp, e = s.ExchangeCode(r.Context(), c, r.PostForm.Get("code"), r.PostForm.Get("redirect_uri"),
			r.PostForm.Get("code_verifier"), r.PostForm.Get("resource"))
	case "refresh_token":
		tp, e = s.Refresh(r.Context(), c, r.PostForm.Get("refresh_token"))
	default:
		oauthErr(w, http.StatusBadRequest, "unsupported_grant_type", "use authorization_code or refresh_token")
		return
	}
	if e != nil {
		code := http.StatusBadRequest
		if e.Code == "server_error" {
			code = http.StatusInternalServerError
		}
		slog.Info("oauth: token request rejected", "client", c.ID, "error", e.Code, "desc", e.Desc, "ip", s.ip(r))
		oauthErr(w, code, e.Code, e.Desc)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  tp.Access,
		"token_type":    "Bearer",
		"expires_in":    tp.ExpiresIn,
		"refresh_token": tp.Refresh,
		"scope":         tp.Scope,
	})
}

func itoa(n int) string { return strconv.Itoa(n) }
