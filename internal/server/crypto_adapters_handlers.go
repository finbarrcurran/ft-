// Spec 9l D11 — Crypto Adapter Repository handlers.
//
//   GET    /api/crypto/adapters                       list (no full body)
//   GET    /api/crypto/adapters/{slug}                full markdown + metadata
//   PUT    /api/crypto/adapters/{slug}                update body (Save or Save-as-new-version)
//   POST   /api/crypto/adapters/preview               markdown → sanitized HTML (live preview)
//   GET    /api/crypto/adapters/{slug}/versions       version history
//   GET    /api/crypto/adapters/{slug}/versions/{ver} one specific version's markdown
//   PUT    /api/crypto/adapters/{slug}/status         status workflow update
//
// Mirrors the Spec 9g pattern (scorecards_handlers.go) so the frontend code
// can almost duplicate its 9g implementation. Cookie OR bearer token throughout.

package server

import (
	"errors"
	"ft/internal/cryptotheses"
	"io"
	"net/http"
	"strings"
)

// GET /api/crypto/adapters — left-pane list.
func (s *Server) handleCryptoAdaptersList(w http.ResponseWriter, r *http.Request) {
	out, err := s.cryptoAdapters.List(r.Context())
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"adapters": out})
}

// GET /api/crypto/adapters/{slug}
func (s *Server) handleCryptoAdapterGet(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	a, err := s.cryptoAdapters.Get(r.Context(), slug)
	if errors.Is(err, cryptotheses.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"adapter": a,
		"html":    a.RenderedHTML,
	})
}

// PUT /api/crypto/adapters/{slug}
// Body: { markdown, asNewVersion?: {version, changelogNote} }
func (s *Server) handleCryptoAdapterUpdate(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
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
		err = s.cryptoAdapters.SaveAsNewVersion(r.Context(), slug,
			req.AsNewVersion.Version, req.Markdown, req.AsNewVersion.ChangelogNote)
	} else {
		err = s.cryptoAdapters.UpdateBody(r.Context(), slug, req.Markdown)
	}
	if errors.Is(err, cryptotheses.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if errors.Is(err, cryptotheses.ErrIsDoctrine) {
		writeError(w, http.StatusForbidden, "adapter is doctrine; edits require explicit unlock")
		return
	}
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved": slug})
}

// POST /api/crypto/adapters/preview — markdown → sanitized HTML for live preview.
func (s *Server) handleCryptoAdapterPreview(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 256<<10)) // 256 KiB cap
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}
	html := cryptotheses.Render(string(body))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(html))
}

// GET /api/crypto/adapters/{slug}/versions
func (s *Server) handleCryptoAdapterVersions(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	versions, err := s.cryptoAdapters.VersionHistory(r.Context(), slug)
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": versions})
}

// GET /api/crypto/adapters/{slug}/versions/{ver}
func (s *Server) handleCryptoAdapterVersionGet(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	ver := r.PathValue("ver")
	v, err := s.cryptoAdapters.GetVersion(r.Context(), slug, ver)
	if errors.Is(err, cryptotheses.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version": v,
		"html":    cryptotheses.Render(v.Markdown),
	})
}

// PUT /api/crypto/adapters/{slug}/status
func (s *Server) handleCryptoAdapterStatus(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	var req struct {
		Status string `json:"status"`
	}
	if !decodeJSON(r, w, &req) {
		return
	}
	err := s.cryptoAdapters.SetStatus(r.Context(), slug, cryptotheses.AdapterStatus(req.Status))
	if errors.Is(err, cryptotheses.ErrIsDoctrine) {
		writeError(w, http.StatusForbidden, "adapter is doctrine; edits require explicit unlock")
		return
	}
	if errors.Is(err, cryptotheses.ErrInvalidStatus) {
		writeError(w, http.StatusBadRequest, "status must be draft|locked|needs-review")
		return
	}
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": req.Status})
}
