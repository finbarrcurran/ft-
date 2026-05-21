// Spec 15 — Thesis Library endpoints.
//
// (Sibling to theses_handlers.go which serves Spec 14 per-holding theses.
// Different feature, different table — Spec 14 stores theses *inside* FT,
// Spec 15 stores them on GitHub and indexes them here.)
//
//	GET    /api/theses                  list rows (optional ?adapter=)
//	GET    /api/theses/{id}             full thesis (markdown + rendered HTML)
//	GET    /api/theses/gaps             stocks owned/watched without a thesis
//	POST   /api/theses/upload           multipart: thesis MD + optional scoring log
//	POST   /api/theses/sync             force re-sync from GitHub
//
// All require cookie auth.

package server

import (
	"fmt"
	"ft/internal/theses"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// GET /api/theses?adapter=pharma
func (s *Server) handleListTheses(w http.ResponseWriter, r *http.Request) {
	if s.theses == nil {
		writeJSON(w, http.StatusOK, map[string]any{"theses": []any{}, "configured": false})
		return
	}
	adapter := r.URL.Query().Get("adapter")
	rows, err := s.theses.List(r.Context(), adapter)
	if err != nil {
		mapStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"theses":     rows,
		"configured": s.theses.Configured(),
	})
}

// GET /api/theses/{id}
func (s *Server) handleGetThesisLibrary(w http.ResponseWriter, r *http.Request) {
	if s.theses == nil {
		writeError(w, http.StatusNotFound, "theses engine not configured")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	t, err := s.theses.Get(r.Context(), id)
	if err != nil {
		mapStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// GET /api/theses/gaps
func (s *Server) handleThesesGaps(w http.ResponseWriter, r *http.Request) {
	if s.theses == nil {
		writeJSON(w, http.StatusOK, map[string]any{"gaps": []any{}})
		return
	}
	gaps, err := s.theses.Gaps(r.Context())
	if err != nil {
		mapStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"gaps": gaps})
}

// POST /api/theses/upload
//
// multipart/form-data:
//
//	thesis        — required, the locked-thesis MD file
//	scoring_log   — optional, the updated _scoring_log.md
func (s *Server) handleUploadThesis(w http.ResponseWriter, r *http.Request) {
	if s.theses == nil || !s.theses.Configured() {
		writeError(w, http.StatusServiceUnavailable,
			"thesis library not configured — set FT_GITHUB_TOKEN on the server")
		return
	}
	if err := r.ParseMultipartForm(2 << 20); err != nil { // 2 MB
		writeError(w, http.StatusBadRequest, "could not parse multipart form: "+err.Error())
		return
	}
	thesisFile, thesisHdr, err := r.FormFile("thesis")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing 'thesis' file part")
		return
	}
	defer thesisFile.Close()
	thesisBytes, err := io.ReadAll(thesisFile)
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read thesis upload")
		return
	}

	var scoringLogBytes []byte
	if logFile, _, lerr := r.FormFile("scoring_log"); lerr == nil {
		defer logFile.Close()
		if b, rerr := io.ReadAll(logFile); rerr == nil {
			scoringLogBytes = b
		}
	}

	res, err := s.theses.Upload(r.Context(), theses.UploadOpts{
		ThesisFilename: thesisHdr.Filename,
		ThesisContent:  thesisBytes,
		ScoringLog:     scoringLogBytes,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// GET /api/theses/{id}/revision-prompt — v1.9.0
//
// Returns a markdown prompt template populated with:
//   - Current thesis content
//   - Indicator metadata (ticker, score, pillar breakdown if present)
//   - Latest earnings date (the trigger)
//   - Explicit instructions to revise per the framework
//
// User copy-pastes the result into Gemini/Claude/etc. to draft v2.
func (s *Server) handleThesisRevisionPrompt(w http.ResponseWriter, r *http.Request) {
	if s.theses == nil {
		writeError(w, http.StatusNotFound, "theses engine not configured")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	t, err := s.theses.Get(r.Context(), id)
	if err != nil {
		mapStoreError(w, err)
		return
	}
	var prompt strings.Builder
	prompt.WriteString("# Thesis Revision Request — " + t.Ticker)
	if t.CompanyName != nil {
		prompt.WriteString(" (" + *t.CompanyName + ")")
	}
	prompt.WriteString("\n\n## Context\n\n")
	prompt.WriteString("- **Ticker:** " + t.Ticker + "\n")
	if t.CompanyName != nil {
		prompt.WriteString("- **Company:** " + *t.CompanyName + "\n")
	}
	prompt.WriteString("- **Adapter:** " + t.Adapter + "\n")
	if t.SubType != nil {
		prompt.WriteString("- **Sub-type:** " + *t.SubType + "\n")
	}
	if t.Score != nil {
		prompt.WriteString(fmt.Sprintf("- **Current locked score:** %d / %d\n", *t.Score, t.MaxScore))
	}
	prompt.WriteString("- **Current version:** v" + strconv.Itoa(t.Version) + "\n")
	if t.LockedDate != nil {
		prompt.WriteString("- **Locked date:** " + *t.LockedDate + "\n")
	}
	if t.NextEarningsDate != nil {
		prompt.WriteString("- **Earnings (the trigger):** " + *t.NextEarningsDate + "\n")
	}
	prompt.WriteString("\n## Task\n\n")
	prompt.WriteString("An earnings report has been issued since this thesis was locked. ")
	prompt.WriteString("Revise the thesis below as v" + strconv.Itoa(t.Version+1) + " considering ")
	prompt.WriteString("the new print. Update specifically:\n\n")
	prompt.WriteString("1. Re-score each of the 8 pillars (or 4 for Asset-Hedge) against the new data\n")
	prompt.WriteString("2. Update the Verified Empirical Anchors section with new Q figures\n")
	prompt.WriteString("3. Re-check Critical Invalidation Triggers — did any fire?\n")
	prompt.WriteString("4. Update Watch Flags + Bull/Bear cases for new context\n")
	prompt.WriteString("5. Add a new row to the Score History table\n")
	prompt.WriteString("6. Keep the same MD structure so it round-trips through the FT Thesis Library uploader\n")
	prompt.WriteString("7. Save the file as `" + t.Ticker + "_v" + strconv.Itoa(t.Version+1) + "_locked.md` — DO NOT overwrite v" + strconv.Itoa(t.Version) + "\n\n")
	prompt.WriteString("## Current locked thesis (verbatim, for context)\n\n")
	prompt.WriteString("```markdown\n")
	prompt.WriteString(t.MarkdownContent)
	if !strings.HasSuffix(t.MarkdownContent, "\n") {
		prompt.WriteString("\n")
	}
	prompt.WriteString("```\n\n")
	prompt.WriteString("## Output format\n\n")
	prompt.WriteString("Return ONLY the full revised thesis markdown for v" + strconv.Itoa(t.Version+1) + ", ")
	prompt.WriteString("nothing else. I will paste it into a `" + t.Ticker + "_v" + strconv.Itoa(t.Version+1) + "_locked.md` file and drop it into FT's Theses dropzone.\n")

	writeJSON(w, http.StatusOK, map[string]any{
		"ticker":      t.Ticker,
		"nextVersion": t.Version + 1,
		"prompt":      prompt.String(),
	})
}

// POST /api/theses/scoring-log
//
// multipart/form-data:
//
//	scoring_log — required, the new _scoring_log.md body
//
// Replaces theses/_scoring_log.md verbatim and pushes. Use when refreshing
// methodology notes or distribution diagrams without locking a new thesis.
func (s *Server) handleUploadScoringLog(w http.ResponseWriter, r *http.Request) {
	if s.theses == nil || !s.theses.Configured() {
		writeError(w, http.StatusServiceUnavailable,
			"thesis library not configured — set FT_GITHUB_TOKEN on the server")
		return
	}
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "could not parse multipart form: "+err.Error())
		return
	}
	logFile, _, err := r.FormFile("scoring_log")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing 'scoring_log' file part")
		return
	}
	defer logFile.Close()
	body, err := io.ReadAll(logFile)
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read scoring log upload")
		return
	}
	res, err := s.theses.UploadScoringLog(r.Context(), body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// POST /api/theses/sync — force a refresh.
func (s *Server) handleThesesSync(w http.ResponseWriter, r *http.Request) {
	if s.theses == nil || !s.theses.Configured() {
		writeError(w, http.StatusServiceUnavailable, "thesis library not configured")
		return
	}
	if err := s.theses.Sync(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
