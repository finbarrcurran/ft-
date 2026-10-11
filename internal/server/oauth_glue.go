package server

// SC-41b glue: the OAuth server (internal/oauth) borrows FT's password check
// and Telegram sender, and shares one failed-login limiter with /api/auth/login.

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ft/internal/auth"
	"ft/internal/monitor"
	"ft/internal/oauth"
	"ft/internal/store"
)

type pwAuthenticator struct{ st *store.Store }

func (p pwAuthenticator) Authenticate(ctx context.Context, email, password string) (int64, bool) {
	u, hash, err := p.st.FindUserByEmail(ctx, strings.TrimSpace(email))
	if err != nil {
		return 0, false
	}
	if auth.VerifyPassword(hash, password) != nil {
		return 0, false
	}
	return u.ID, true
}

type telegramAlerter struct{ t *monitor.Telegram }

func (a telegramAlerter) Alert(ctx context.Context, text string) {
	if err := a.t.Send(ctx, text); err != nil {
		slog.Warn("oauth: alert not delivered", "err", err)
	}
}

// initOAuth builds the OAuth service when FT_BASE_URL is set (it names the
// issuer and the /mcp resource) and the shared login limiter.
func (s *Server) initOAuth() {
	s.loginFail = oauth.NewLimiter(5, 15*time.Minute)
	s.loginAll = oauth.NewLimiter(30, time.Hour)
	base := strings.TrimRight(s.cfg.BaseURL, "/")
	if base == "" {
		slog.Warn("oauth: FT_BASE_URL not set; the OAuth server is disabled")
		return
	}
	s.oauth = oauth.New(s.store.DB, oauth.Config{Issuer: base, Resource: base + "/mcp"},
		pwAuthenticator{s.store}, telegramAlerter{&monitor.Telegram{}})
	s.oauth.ClientIP = clientIP
}

// loginBlocked answers 429 when the client (or the whole server) has too many
// recent failed logins. It never locks permanently.
func (s *Server) loginBlocked(w http.ResponseWriter, r *http.Request) bool {
	okIP, retry := s.loginFail.Allow(clientIP(r))
	okAll, retryAll := s.loginAll.Allow("all")
	if okIP && okAll {
		return false
	}
	if retryAll > retry {
		retry = retryAll
	}
	w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
	writeError(w, http.StatusTooManyRequests, "too many failed logins; try again later")
	return true
}

// loginFailed records a failed login: log line, limiter, and one Telegram
// alert the moment a limit trips.
func (s *Server) loginFailed(r *http.Request) {
	ip := clientIP(r)
	slog.Warn("login failed", "ip", ip, "ua", r.Header.Get("User-Agent"))
	tripped := s.loginFail.Fail(ip)
	trippedAll := s.loginAll.Fail("all")
	if tripped || trippedAll {
		go (&monitor.Telegram{}).Send(context.Background(),
			"🟠 FT: repeated failed logins (client IP "+ip+"). Further attempts are blocked for a while.")
	}
}
