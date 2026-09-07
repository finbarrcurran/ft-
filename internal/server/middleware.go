package server

import (
	"context"
	"ft/internal/auth"
	"log/slog"
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
		// SC-41: a token that declares a scope set is held to it — a scoped,
		// non-"write" token (e.g. an ft_mcp_ read-only token minted for the
		// MCP connector) cannot reach a non-GET route, even outside /mcp.
		// Every token minted before SC-41 (incl. OpenClaw's) has an EMPTY
		// scope list — CreateServiceToken was historically called with a nil
		// scopes slice — so len(st.Scopes)==0 and this is a no-op for all of
		// them. Deliberate: no behavior change for anything already in use.
		if len(st.Scopes) > 0 && r.Method != http.MethodGet && !hasScope(st.Scopes, "write") {
			writeError(w, http.StatusForbidden, "token scope does not permit write access")
			return
		}
		s.store.TouchServiceTokenLastUsed(r.Context(), st.ID)

		ctx := context.WithValue(r.Context(), ctxUserID, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

// requireReadToken gates the SC-41 /mcp surface. Deliberately separate from
// requireUserOrToken: bearer-only (no cookie fallback — an MCP client never
// holds an ft_session cookie) and requires the token's scopes to actually
// contain "read". This is one layer of the read-only guarantee; the other
// is that no mutating store method is ever wired into the six /mcp tools
// (see mcp_handlers.go) — so even a bug here can't reach a write, and no
// HTTP-method check is applied (MCP's Streamable HTTP transport is
// JSON-RPC over POST, not a REST verb per tool).
func (s *Server) requireReadToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// SC-41 ops visibility: every hit on /mcp is logged here, regardless
		// of outcome — this middleware is the ONLY thing standing between a
		// raw request and a 401/403/handler call, so this is the one place
		// that can answer "did a request even reach FT" for any connector
		// debugging (a rejected request previously left zero trace; only a
		// successful tool call logged anything, deep inside mcp_handlers.go).
		log := slog.With("path", r.URL.Path, "method", r.Method, "remote", r.RemoteAddr, "ua", r.Header.Get("User-Agent"))
		hdr := r.Header.Get("Authorization")
		if !strings.HasPrefix(hdr, "Bearer ") {
			log.Info("mcp request rejected", "reason", "no bearer header")
			writeError(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(hdr, "Bearer "))
		if token == "" {
			log.Info("mcp request rejected", "reason", "empty bearer token")
			writeError(w, http.StatusUnauthorized, "empty bearer token")
			return
		}
		hash := auth.HashServiceToken(token)
		st, userID, err := s.store.FindServiceTokenByHash(r.Context(), hash)
		if err != nil {
			log.Info("mcp request rejected", "reason", "invalid token")
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		if !hasScope(st.Scopes, "read") {
			log.Info("mcp request rejected", "reason", "missing read scope", "token_id", st.ID)
			writeError(w, http.StatusForbidden, "token lacks 'read' scope")
			return
		}
		log.Info("mcp request accepted", "token_id", st.ID)
		s.store.TouchServiceTokenLastUsed(r.Context(), st.ID)

		ctx := context.WithValue(r.Context(), ctxUserID, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

func hasScope(scopes []string, want string) bool {
	for _, sc := range scopes {
		if sc == want {
			return true
		}
	}
	return false
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
