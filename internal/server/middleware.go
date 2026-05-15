package server

import (
	"context"
	"ft/internal/auth"
	"net/http"
	"strings"
)

// requireUser enforces a valid session cookie. On success, the user_id is
// placed in the request context.
//
// Not wired up yet (no protected routes exist in this skeleton) — provided
// here so the next iteration can plug it in directly.
func (s *Server) requireUser(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, sessionToken, ok := s.userFromCookie(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		ctx := context.WithValue(r.Context(), ctxUserID, userID)
		ctx = context.WithValue(ctx, ctxSessionToken, sessionToken)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

// requireUserOrToken accepts either a cookie session or a Bearer service token.
// Used for endpoints the OpenClaw skill needs to hit (phase 2 wiring).
func (s *Server) requireUserOrToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 1) Try cookie first (humans take precedence).
		if userID, sessionToken, ok := s.userFromCookie(r); ok {
			ctx := context.WithValue(r.Context(), ctxUserID, userID)
			ctx = context.WithValue(ctx, ctxSessionToken, sessionToken)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		// 2) Then Authorization: Bearer …
		hdr := r.Header.Get("Authorization")
		if !strings.HasPrefix(hdr, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(hdr, "Bearer "))
		if token == "" {
			writeError(w, http.StatusUnauthorized, "empty bearer token")
			return
		}
		hash := auth.HashServiceToken(token)
		st, userID, err := s.store.FindServiceTokenByHash(r.Context(), hash)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		s.store.TouchServiceTokenLastUsed(r.Context(), st.ID)

		ctx := context.WithValue(r.Context(), ctxUserID, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

// userFromCookie resolves a session cookie to a user id. Returns ok=false on
// any failure (no cookie, expired, deleted).
func (s *Server) userFromCookie(r *http.Request) (userID int64, token string, ok bool) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		return 0, "", false
	}
	sess, err := s.store.FindSession(r.Context(), c.Value)
	if err != nil {
		return 0, "", false
	}
	return sess.UserID, sess.Token, true
}
