// Package oauth is FT's single-user OAuth 2.1 authorization server for the
// claude.ai MCP connector (SC-41b). It issues short-lived read-only access
// tokens that /mcp accepts next to the static ft_mcp_ tokens.
//
// Flow (RFC 9728 discovery -> RFC 7591 registration -> authorization code with
// PKCE S256 -> token -> refresh rotation):
//   - /.well-known/oauth-protected-resource   what /mcp is and who issues tokens
//   - /.well-known/oauth-authorization-server endpoints and capabilities
//   - POST /register                          dynamic client registration (limited)
//   - GET/POST /authorize                     Fin approves with his FT password
//   - POST /token                             code and refresh-token exchange
//
// Tokens carry the single scope "read" and are bound to the /mcp resource. Only
// hashes are stored. A reused refresh token revokes its whole family.
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	ScopeRead    = "read"
	accessTTL    = time.Hour
	refreshTTL   = 30 * 24 * time.Hour
	codeTTL      = 60 * time.Second
	maxClients   = 50
	AccessPrefix = "ft_oat_"
	refreshPref  = "ft_ort_"
)

// ClaudeCallback is the one redirect URI the hosted Claude apps use.
const ClaudeCallback = "https://claude.ai/api/mcp/auth_callback"

// Authenticator checks FT credentials (the existing argon2id login).
type Authenticator interface {
	Authenticate(ctx context.Context, email, password string) (userID int64, ok bool)
}

// Alerter delivers a one-line operator alert (Telegram).
type Alerter interface {
	Alert(ctx context.Context, text string)
}

// Config names this server. Issuer has no trailing slash; Resource is the /mcp URL.
type Config struct {
	Issuer   string
	Resource string
}

// Service owns the OAuth state in SQLite.
type Service struct {
	DB       *sql.DB
	Cfg      Config
	Auth     Authenticator
	Alerts   Alerter
	Now      func() time.Time
	ClientIP func(*http.Request) string // caller supplies (Cloudflare-aware); default RemoteAddr host

	loginFail *Limiter // per client IP
	loginAll  *Limiter // whole server
	regFail   *Limiter
}

// New builds a Service with the default limits (5 failed logins / 15 min per IP,
// 30 / hour overall, 10 registrations / hour per IP).
func New(db *sql.DB, cfg Config, a Authenticator, al Alerter) *Service {
	return &Service{
		DB: db, Cfg: cfg, Auth: a, Alerts: al,
		loginFail: NewLimiter(5, 15*time.Minute),
		loginAll:  NewLimiter(30, time.Hour),
		regFail:   NewLimiter(10, time.Hour),
	}
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// --- helpers ---------------------------------------------------------------

func randToken(prefix string, n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand failing is unrecoverable
	}
	return prefix + hex.EncodeToString(b)
}

// Hash is the stored form of any code, secret or token.
func Hash(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])
}

// pkceOK verifies an S256 code_verifier against the stored challenge.
func pkceOK(verifier, challenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	got := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(got), []byte(challenge)) == 1
}

// RedirectAllowed says whether a redirect URI may be registered: the hosted
// Claude callback exactly, or a loopback URL (Claude Code) on any port.
func RedirectAllowed(u string) bool {
	if u == ClaudeCallback {
		return true
	}
	p, err := url.Parse(u)
	if err != nil || p.Fragment != "" || p.User != nil {
		return false
	}
	return p.Scheme == "http" && (p.Hostname() == "localhost" || p.Hostname() == "127.0.0.1")
}

func redirectMatches(registered []string, got string) bool {
	for _, r := range registered {
		if r == got {
			return true
		}
		// loopback registered without a port matches any port (RFC 8252 s7.3)
		rp, e1 := url.Parse(r)
		gp, e2 := url.Parse(got)
		if e1 == nil && e2 == nil && rp.Scheme == "http" && gp.Scheme == "http" &&
			(rp.Hostname() == "localhost" || rp.Hostname() == "127.0.0.1") &&
			rp.Hostname() == gp.Hostname() && rp.Port() == "" && rp.Path == gp.Path {
			return true
		}
	}
	return false
}

// --- clients ---------------------------------------------------------------

// Client is a registered OAuth client.
type Client struct {
	ID           string
	Name         string
	RedirectURIs []string
	SecretHash   string
}

var ErrClientLimit = errors.New("oauth: client limit reached")

// RegisterClient stores a new client. secret=="" makes a public client.
func (s *Service) RegisterClient(ctx context.Context, name string, redirects []string, secret, source string) (string, error) {
	if len(redirects) == 0 {
		return "", errors.New("redirect_uris required")
	}
	for _, r := range redirects {
		if !RedirectAllowed(r) {
			return "", fmt.Errorf("redirect_uri not allowed: %s", r)
		}
	}
	// Prune clients that never got a token and are older than a day, then cap.
	cutoff := s.now().Add(-24 * time.Hour).Unix()
	_, _ = s.DB.ExecContext(ctx, `
		DELETE FROM oauth_clients
		 WHERE source = 'dcr' AND created_at < ?
		   AND client_id NOT IN (SELECT client_id FROM oauth_tokens)`, cutoff)
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM oauth_clients`).Scan(&n)
	if n >= maxClients {
		return "", ErrClientLimit
	}
	id := randToken("ftc_", 16)
	var sh any
	if secret != "" {
		sh = Hash(secret)
	}
	jb, _ := json.Marshal(redirects)
	js := string(jb)
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO oauth_clients (client_id, client_name, redirect_uris, secret_hash, source, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, id, name, js, sh, source, s.now().Unix())
	return id, err
}

func parseRedirects(js string) []string {
	var out []string
	_ = json.Unmarshal([]byte(js), &out)
	return out
}

func (s *Service) client(ctx context.Context, id string) (*Client, error) {
	var c Client
	var uris string
	var sec, name sql.NullString
	err := s.DB.QueryRowContext(ctx,
		`SELECT client_id, client_name, redirect_uris, secret_hash FROM oauth_clients WHERE client_id = ?`, id).
		Scan(&c.ID, &name, &uris, &sec)
	if err != nil {
		return nil, err
	}
	c.Name, c.SecretHash, c.RedirectURIs = name.String, sec.String, parseRedirects(uris)
	return &c, nil
}

// --- authorization codes ---------------------------------------------------

// NewCode stores a single-use code (60 s) bound to client, redirect URI,
// PKCE challenge and resource, and returns its plaintext.
func (s *Service) NewCode(ctx context.Context, clientID string, userID int64, redirect, challenge, resource string) (string, error) {
	code := randToken("", 32)
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO oauth_codes (code_hash, client_id, user_id, redirect_uri, code_challenge, resource, scope, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		Hash(code), clientID, userID, redirect, challenge, resource, ScopeRead, s.now().Add(codeTTL).Unix())
	return code, err
}

// TokenPair is what /token returns.
type TokenPair struct {
	Access, Refresh string
	ExpiresIn       int
	Scope           string
}

// Error is an RFC 6749 error response.
type Error struct{ Code, Desc string }

func (e *Error) Error() string { return e.Code + ": " + e.Desc }

func invalidGrant(d string) *Error { return &Error{"invalid_grant", d} }

// ExchangeCode redeems an authorization code. A second use of the same code
// revokes everything that code produced.
func (s *Service) ExchangeCode(ctx context.Context, c *Client, code, redirect, verifier, resource string) (*TokenPair, *Error) {
	now := s.now().Unix()
	var userID int64
	var cid, ruri, chal, res string
	var exp int64
	var used sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `
		SELECT client_id, user_id, redirect_uri, code_challenge, resource, expires_at, used_at
		  FROM oauth_codes WHERE code_hash = ?`, Hash(code)).Scan(&cid, &userID, &ruri, &chal, &res, &exp, &used)
	if err != nil {
		return nil, invalidGrant("unknown code")
	}
	if used.Valid {
		_, _ = s.DB.ExecContext(ctx, `UPDATE oauth_tokens SET revoked_at = ? WHERE family_id = ? AND revoked_at IS NULL`, now, "code:"+Hash(code))
		return nil, invalidGrant("code already used")
	}
	if now > exp {
		return nil, invalidGrant("code expired")
	}
	if cid != c.ID || ruri != redirect {
		return nil, invalidGrant("client or redirect_uri mismatch")
	}
	if resource != "" && resource != res {
		return nil, &Error{"invalid_target", "resource does not match"}
	}
	if !pkceOK(verifier, chal) {
		return nil, invalidGrant("PKCE verification failed")
	}
	if r, e := s.DB.ExecContext(ctx, `UPDATE oauth_codes SET used_at = ? WHERE code_hash = ? AND used_at IS NULL`, now, Hash(code)); e != nil {
		return nil, &Error{"server_error", "database"}
	} else if n, _ := r.RowsAffected(); n == 0 {
		return nil, invalidGrant("code already used")
	}
	tp, err := s.issue(ctx, "code:"+Hash(code), c.ID, userID, res)
	if err != nil {
		return nil, &Error{"server_error", "database"}
	}
	_, _ = s.DB.ExecContext(ctx, `UPDATE oauth_clients SET last_used_at = ? WHERE client_id = ?`, now, c.ID)
	return tp, nil
}

func (s *Service) issue(ctx context.Context, family, clientID string, userID int64, resource string) (*TokenPair, error) {
	now := s.now()
	acc, ref := randToken(AccessPrefix, 32), randToken(refreshPref, 32)
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO oauth_tokens (family_id, client_id, user_id, scope, resource, access_hash, refresh_hash,
		                          access_expires_at, refresh_expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		family, clientID, userID, ScopeRead, resource, Hash(acc), Hash(ref),
		now.Add(accessTTL).Unix(), now.Add(refreshTTL).Unix(), now.Unix())
	if err != nil {
		return nil, err
	}
	return &TokenPair{Access: acc, Refresh: ref, ExpiresIn: int(accessTTL.Seconds()), Scope: ScopeRead}, nil
}

// Refresh rotates a refresh token. Presenting one that was already rotated is a
// theft signal: the whole family is revoked.
func (s *Service) Refresh(ctx context.Context, c *Client, refresh string) (*TokenPair, *Error) {
	now := s.now().Unix()
	var id, userID int64
	var family, cid, resource string
	var exp int64
	var rotated, revoked sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `
		SELECT id, family_id, client_id, user_id, resource, refresh_expires_at, rotated_at, revoked_at
		  FROM oauth_tokens WHERE refresh_hash = ?`, Hash(refresh)).
		Scan(&id, &family, &cid, &userID, &resource, &exp, &rotated, &revoked)
	if err != nil || cid != c.ID {
		return nil, invalidGrant("unknown refresh token")
	}
	if rotated.Valid || revoked.Valid {
		_, _ = s.DB.ExecContext(ctx, `UPDATE oauth_tokens SET revoked_at = ? WHERE family_id = ? AND revoked_at IS NULL`, now, family)
		if s.Alerts != nil {
			s.Alerts.Alert(ctx, "🔴 FT OAuth: a spent refresh token was presented again — the whole token family was revoked.")
		}
		return nil, invalidGrant("refresh token already used or revoked")
	}
	if now > exp {
		return nil, invalidGrant("refresh token expired")
	}
	if r, e := s.DB.ExecContext(ctx, `UPDATE oauth_tokens SET rotated_at = ? WHERE id = ? AND rotated_at IS NULL`, now, id); e != nil {
		return nil, &Error{"server_error", "database"}
	} else if n, _ := r.RowsAffected(); n == 0 {
		return nil, invalidGrant("refresh token already used")
	}
	tp, err := s.issue(ctx, family, c.ID, userID, resource)
	if err != nil {
		return nil, &Error{"server_error", "database"}
	}
	return tp, nil
}

// ValidateAccess resolves a bearer token for /mcp. It returns the user id when
// the token is a live, unrevoked OAuth access token for this resource.
func (s *Service) ValidateAccess(ctx context.Context, token string) (userID int64, ok bool) {
	if !strings.HasPrefix(token, AccessPrefix) {
		return 0, false
	}
	now := s.now().Unix()
	var id int64
	var exp int64
	var revoked sql.NullInt64
	var res string
	err := s.DB.QueryRowContext(ctx, `
		SELECT id, user_id, access_expires_at, revoked_at, resource FROM oauth_tokens WHERE access_hash = ?`,
		Hash(token)).Scan(&id, &userID, &exp, &revoked, &res)
	if err != nil || revoked.Valid || now > exp || res != s.Cfg.Resource {
		return 0, false
	}
	_, _ = s.DB.ExecContext(ctx, `UPDATE oauth_tokens SET last_used_at = ? WHERE id = ?`, now, id)
	return userID, true
}

// RevokeAll revokes every OAuth token (kill switch); returns how many.
func (s *Service) RevokeAll(ctx context.Context) (int64, error) {
	r, err := s.DB.ExecContext(ctx, `UPDATE oauth_tokens SET revoked_at = ? WHERE revoked_at IS NULL`, s.now().Unix())
	if err != nil {
		return 0, err
	}
	return r.RowsAffected()
}

// RevokeClient revokes one client's tokens and deletes the client.
func (s *Service) RevokeClient(ctx context.Context, clientID string) error {
	now := s.now().Unix()
	if _, err := s.DB.ExecContext(ctx, `UPDATE oauth_tokens SET revoked_at = ? WHERE client_id = ? AND revoked_at IS NULL`, now, clientID); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, `DELETE FROM oauth_clients WHERE client_id = ?`, clientID)
	return err
}

// Status feeds Diagnostics.
type Status struct {
	Clients       int    `json:"clients"`
	ActiveTokens  int    `json:"activeTokens"`
	LastUsedUnix  *int64 `json:"lastUsedUnix,omitempty"`
}

func (s *Service) Status(ctx context.Context) Status {
	var st Status
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM oauth_clients`).Scan(&st.Clients)
	now := s.now().Unix()
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM oauth_tokens WHERE revoked_at IS NULL AND rotated_at IS NULL AND refresh_expires_at > ?`, now).Scan(&st.ActiveTokens)
	var lu sql.NullInt64
	_ = s.DB.QueryRowContext(ctx, `SELECT MAX(last_used_at) FROM oauth_tokens`).Scan(&lu)
	if lu.Valid {
		st.LastUsedUnix = &lu.Int64
	}
	return st
}
