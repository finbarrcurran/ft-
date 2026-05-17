// Spec 9g §5 — Scorecard Repository handlers.
//
//   GET    /api/scorecards                       list (no full body)
//   GET    /api/scorecards/{code}                full markdown + metadata
//   PUT    /api/scorecards/{code}                update body (Save or Save-as-new-version)
//   POST   /api/scorecards/preview               markdown → sanitized HTML (live preview)
//   GET    /api/scorecards/{code}/versions       version history
//   GET    /api/scorecards/{code}/versions/{ver} one specific version's markdown
//   PUT    /api/scorecards/{code}/status         status workflow update
//   POST   /api/scorecards                       admin-create new scorecard
//
// Cookie OR bearer token throughout. Doctrine row (Philosophy v1.1) is
// guarded server-side: PUT returns 403 regardless of auth.

package server

import (
	"errors"
	"ft/internal/scorecards"
	"io"
	"net/http"
	"strings"
)

// GET /api/scorecards — left-pane list.
func (s *Server) handleScorecardsList(w http.ResponseWriter, r *http.Request) {
	out, err := s.scorecards.List(r.Context())
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"scorecards": out})
}

// GET /api/scorecards/{code}
func (s *Server) handleScorecardGet(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	sc, err := s.scorecards.Get(r.Context(), code)
	if errors.Is(err, scorecards.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if mapStoreError(w, err) {
		return
	}
	// Render once server-side so the client can drop sanitized HTML in.
	html := scorecards.Render(sc.MarkdownCurrent)
	writeJSON(w, http.StatusOK, map[string]any{
		"scorecard": sc,
		"html":      html,
	})
}

// PUT /api/scorecards/{code}
// Body: { markdown, asNewVersion?: {version, changelogNote} }
func (s *Server) handleScorecardUpdate(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
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
	var err error
	if req.AsNewVersion != nil {
		err = s.scorecards.SaveAsNewVersion(r.Context(), code, req.AsNewVersion.Version, req.Markdown, req.AsNewVersion.ChangelogNote)
	} else {
		err = s.scorecards.UpdateBody(r.Context(), code, req.Markdown)
	}
	if errors.Is(err, scorecards.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if errors.Is(err, scorecards.ErrIsDoctrine) {
		writeError(w, http.StatusForbidden, "scorecard is doctrine; edits require explicit unlock")
		return
	}
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved": code})
}

// POST /api/scorecards/preview — markdown → sanitized HTML for live preview.
func (s *Server) handleScorecardPreview(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 256<<10)) // 256 KiB cap
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}
	html := scorecards.Render(string(body))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(html))
}

// GET /api/scorecards/{code}/versions
func (s *Server) handleScorecardVersions(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	versions, err := s.scorecards.VersionHistory(r.Context(), code)
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": versions})
}

// PUT /api/scorecards/{code}/status
func (s *Server) handleScorecardStatus(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	var req struct {
		Status string `json:"status"`
	}
	if !decodeJSON(r, w, &req) {
		return
	}
	err := s.scorecards.SetStatus(r.Context(), code, req.Status)
	if errors.Is(err, scorecards.ErrIsDoctrine) {
		writeError(w, http.StatusForbidden, "scorecard is doctrine; edits require explicit unlock")
		return
	}
	if errors.Is(err, scorecards.ErrInvalidStatus) {
		writeError(w, http.StatusBadRequest, "status must be draft|locked|needs-review")
		return
	}
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": req.Status})
}
