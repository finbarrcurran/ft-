package server

import (
	"errors"
	"ft/internal/auth"
	"ft/internal/store"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// GET /api/auth/state
//
// Single call the frontend uses on page load to decide what to render.
// Returns one of:
//
//	{ "state": "needs_setup" }                              — no user exists yet
//	{ "state": "needs_login" }                              — has users; no valid session
//	{ "state": "authenticated", "user": {...} }             — has valid session
func (s *Server) handleAuthState(w http.ResponseWriter, r *http.Request) {
	count, err := s.store.CountUsers(r.Context())
	if err != nil {
		mapStoreError(w, err)
		return
	}
	if count == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"state": "needs_setup"})
		return
	}
	if userID, _, ok := s.userFromCookie(r); ok {
		u, err := s.store.FindUserByID(r.Context(), userID)
		if err != nil {
			// Stale cookie pointing at a deleted user.
			writeJSON(w, http.StatusOK, map[string]any{"state": "needs_login"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"state": "authenticated",
			"user":  userToResp(u),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"state": "needs_login"})
}

// POST /api/auth/setup
//
// First-run only. Creates the (single) user account and immediately signs them in.
// Returns 403 if a user already exists.
type setupReq struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"displayName"`
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	count, err := s.store.CountUsers(r.Context())
	if err != nil {
		mapStoreError(w, err)
		return
	}
	if count > 0 {
		writeError(w, http.StatusForbidden, "setup already complete")
		return
	}
	var req setupReq
	if !decodeJSON(r, w, &req) {
		return
	}
	if !stringNotBlank(req.Email) {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}
	if !strings.Contains(req.Email, "@") {
		writeError(w, http.StatusBadRequest, "email must contain @")
		return
	}
	if len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		slog.Error("hash password", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	userID, err := s.store.CreateUser(
		r.Context(),
		strings.TrimSpace(req.Email),
		hash,
		strings.TrimSpace(req.DisplayName),
	)
	if err != nil {
		mapStoreError(w, err)
		return
	}
	s.issueSession(w, r, userID)
	u, _ := s.store.FindUserByID(r.Context(), userID)
	writeJSON(w, http.StatusOK, map[string]any{"user": userToResp(u)})
}

// POST /api/auth/login
type loginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if !decodeJSON(r, w, &req) {
		return
	}
	if !stringNotBlank(req.Email) || req.Password == "" {
		writeError(w, http.StatusBadRequest, "email and password are required")
		return
	}
	u, hash, err := s.store.FindUserByEmail(r.Context(), strings.TrimSpace(req.Email))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// Always respond with the same message — don't leak which half is wrong.
			writeError(w, http.StatusUnauthorized, "invalid credentials")
			return
		}
		mapStoreError(w, err)
		return
	}
	if err := auth.VerifyPassword(hash, req.Password); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	s.issueSession(w, r, u.ID)
	s.store.TouchUserLastLogin(r.Context(), u.ID)
	writeJSON(w, http.StatusOK, map[string]any{"user": userToResp(u)})
}

// POST /api/auth/logout
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if tok, ok := r.Context().Value(ctxSessionToken).(string); ok && tok != "" {
		_ = s.store.DeleteSession(r.Context(), tok)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		Domain:   s.cfg.CookieDomain,
		Secure:   s.cfg.CookieSecure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// GET /api/auth/me
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	u, err := s.store.FindUserByID(r.Context(), userID)
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": userToResp(u)})
}

// issueSession mints a new session row and writes the cookie. Used by both
// /api/auth/setup and /api/auth/login.
func (s *Server) issueSession(w http.ResponseWriter, r *http.Request, userID int64) {
	token, err := auth.NewSessionToken()
	if err != nil {
		slog.Error("gen session token", "err", err)
		return
	}
	ttl := time.Duration(s.cfg.SessionDays) * 24 * time.Hour
	if err := s.store.CreateSession(r.Context(), token, userID, ttl, r.Header.Get("User-Agent"), clientIP(r)); err != nil {
		slog.Error("create session", "err", err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Domain:   s.cfg.CookieDomain,
		Secure:   s.cfg.CookieSecure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(ttl),
	})
}
