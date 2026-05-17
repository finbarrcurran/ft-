// Spec 14 — Per-holding thesis endpoints.
//
//   GET    /api/holdings/{kind}/{id}/thesis           → { thesis, html }  (404 if absent)
//   PUT    /api/holdings/{kind}/{id}/thesis           body { markdown, asNewVersion? }
//   GET    /api/holdings/{kind}/{id}/thesis/versions  version history
//   PUT    /api/holdings/{kind}/{id}/thesis/status    workflow update
//   POST   /api/holdings/{kind}/{id}/thesis/preview   markdown → sanitized HTML
//
// Markdown render reuses scorecards.Render (goldmark + bluemonday) — same
// sanitizer policy used by sector adapters.

package server

import (
	"errors"
	"ft/internal/scorecards"
	"ft/internal/store"
	"io"
	"net/http"
	"strings"
)

// helper: validate {kind} path param.
func validHoldingKind(k string) bool { return k == "stock" || k == "crypto" }

// GET /api/holdings/{kind}/{id}/thesis
func (s *Server) handleGetHoldingThesis(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if !validHoldingKind(kind) {
		writeError(w, http.StatusBadRequest, "kind must be stock|crypto")
		return
	}
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	t, err := s.store.GetHoldingThesis(r.Context(), kind, id)
	if errors.Is(err, store.ErrNotFound) {
		// 200 with empty payload so the UI can render a "start a thesis"
		// affordance without a 404 in the network log.
		writeJSON(w, http.StatusOK, map[string]any{"thesis": nil})
		return
	}
	if mapStoreError(w, err) {
		return
	}
	html := scorecards.Render(t.MarkdownCurrent)
	writeJSON(w, http.StatusOK, map[string]any{
		"thesis": t,
		"html":   html,
	})
}

// PUT /api/holdings/{kind}/{id}/thesis
// Body: { markdown, asNewVersion?: {version, changelogNote} }
func (s *Server) handlePutHoldingThesis(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	kind := r.PathValue("kind")
	if !validHoldingKind(kind) {
		writeError(w, http.StatusBadRequest, "kind must be stock|crypto")
		return
	}
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	var req struct {
		Markdown     string `json:"markdown"`
		AsNewVersion *struct {
			Version       string `json:"version"`
			ChangelogNote string `json:"changelogNote"`
		} `json:"asNewVersion,omitempty"`
	}
	if !decodeJSON(r, w, &req) {
		return
	}
	if strings.TrimSpace(req.Markdown) == "" {
		writeError(w, http.StatusBadRequest, "markdown required")
		return
	}
	if len(req.Markdown) > 200000 { // 200 KB cap
		writeError(w, http.StatusBadRequest, "markdown too large (max 200KB)")
		return
	}
	ticker := s.resolveTickerCtx(r, userID, kind, id)

	if req.AsNewVersion != nil {
		err = s.store.SaveHoldingThesisAsNewVersion(r.Context(), kind, id, ticker,
			req.AsNewVersion.Version, req.Markdown, req.AsNewVersion.ChangelogNote)
	} else {
		err = s.store.UpsertHoldingThesisBody(r.Context(), kind, id, ticker, req.Markdown)
	}
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved": id})
}

// resolveTickerCtx uses the request's context (proper cancellation).
func (s *Server) resolveTickerCtx(r *http.Request, userID int64, kind string, id int64) string {
	if kind == "stock" {
		h, err := s.store.GetStockHolding(r.Context(), userID, id)
		if err == nil && h != nil {
			if h.Ticker != nil {
				return *h.Ticker
			}
			return h.Name
		}
		return ""
	}
	h, err := s.store.GetCryptoHolding(r.Context(), userID, id)
	if err == nil && h != nil {
		return h.Symbol
	}
	return ""
}

// GET /api/holdings/{kind}/{id}/thesis/versions
func (s *Server) handleHoldingThesisVersions(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if !validHoldingKind(kind) {
		writeError(w, http.StatusBadRequest, "kind must be stock|crypto")
		return
	}
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	versions, err := s.store.ListHoldingThesisVersions(r.Context(), kind, id)
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": versions})
}

// PUT /api/holdings/{kind}/{id}/thesis/status
func (s *Server) handleHoldingThesisStatus(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if !validHoldingKind(kind) {
		writeError(w, http.StatusBadRequest, "kind must be stock|crypto")
		return
	}
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if !decodeJSON(r, w, &req) {
		return
	}
	if err := s.store.SetHoldingThesisStatus(r.Context(), kind, id, req.Status); mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": req.Status})
}

// POST /api/holdings/{kind}/{id}/thesis/preview
// text/plain body → text/html (sanitized) for live preview in the editor.
func (s *Server) handleHoldingThesisPreview(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 256<<10))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}
	html := scorecards.Render(string(body))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(html))
}
