// Spec 11 — Thesis Notes & Observation Log handlers.
//
// Endpoints (cookie or token; bot needs token for /note):
//   GET    /api/notes ?targetKind=&targetId=&ticker=&from=&to=&framework=&factor=&limit=
//   POST   /api/notes
//   PUT    /api/notes/{id}              edit (24h soft-lock for typo fix; v1 has no
//                                       hard enforcement — discipline note in spec)
//   DELETE /api/notes/{id}              soft-delete
//   GET    /api/notes/stale             Summary D6 candidates (90d cutoff)
//   GET    /api/notes/contradictions ?targetKind=&targetId=
//                                       D7 factor-flagging payload
//
// Validation:
//   - framework_id (if set) must match a loaded framework
//   - factor_id (if set with framework_id) must exist in that framework
//   - factor_direction must be one of confirms|contradicts|neutral
//   - source_kind must be one of news|earnings|youtube|twitter|manual|
//     cowen_weekly|other
//
// Observation text is capped at 4000 chars (well above the 1000 in spec D3
// but lets users paste longer passages on a desk session).

package server

import (
	"ft/internal/frameworks"
	"ft/internal/store"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ----- list ------------------------------------------------------------

func (s *Server) handleListNotes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.ThesisNoteFilter{
		TargetKind:  q.Get("targetKind"),
		Ticker:      q.Get("ticker"),
		FrameworkID: q.Get("framework"),
		FactorID:    q.Get("factor"),
		FromDate:    q.Get("from"),
		ToDate:      q.Get("to"),
	}
	if v := q.Get("targetId"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			f.TargetID = id
		}
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			f.Limit = n
		}
	}
	notes, err := s.store.ListThesisNotes(r.Context(), f)
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"notes": notes})
}

// ----- create ---------------------------------------------------------

type noteReq struct {
	TargetKind      string `json:"targetKind"`
	TargetID        int64  `json:"targetId"`
	Ticker          string `json:"ticker"`
	ObservationAt   string `json:"observationAt"`   // ISO YYYY-MM-DD; defaults to today
	ObservationText string `json:"observationText"`
	FrameworkID     string `json:"frameworkId,omitempty"`
	FactorID        string `json:"factorId,omitempty"`
	FactorDirection string `json:"factorDirection,omitempty"`
	SourceURL       string `json:"sourceUrl,omitempty"`
	SourceKind      string `json:"sourceKind,omitempty"`
}

func (s *Server) handleCreateNote(w http.ResponseWriter, r *http.Request) {
	var req noteReq
	if !decodeJSON(r, w, &req) {
		return
	}
	if msg := validateNoteReq(&req); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	n := noteFromReq(&req)
	id, err := s.store.InsertThesisNote(r.Context(), n)
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// ----- update --------------------------------------------------------

func (s *Server) handleUpdateNote(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	var req noteReq
	if !decodeJSON(r, w, &req) {
		return
	}
	if msg := validateNoteReq(&req); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	n := noteFromReq(&req)
	if err := s.store.UpdateThesisNote(r.Context(), id, n); mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"updated": id})
}

// ----- delete --------------------------------------------------------

func (s *Server) handleDeleteNote(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.store.SoftDeleteThesisNote(r.Context(), id); mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

// ----- stale (D6) ----------------------------------------------------

func (s *Server) handleStaleNotes(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	cutoff := 90
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cutoff = n
		}
	}
	rows, err := s.store.StaleThesisCandidates(r.Context(), userID, cutoff)
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"cutoffDays": cutoff,
		"stale":      rows,
	})
}

// ----- contradictions (D7) ------------------------------------------

func (s *Server) handleNoteContradictions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	kind := q.Get("targetKind")
	if kind != "holding" && kind != "watchlist" {
		writeError(w, http.StatusBadRequest, "targetKind must be holding|watchlist")
		return
	}
	id, err := strconv.ParseInt(q.Get("targetId"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad targetId")
		return
	}
	notes, err := s.store.FactorContradictions(r.Context(), kind, id)
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"contradictions": notes})
}

// ----- validation ----------------------------------------------------

var validSourceKind = map[string]bool{
	"news": true, "earnings": true, "youtube": true, "twitter": true,
	"manual": true, "cowen_weekly": true, "other": true,
}

var validDirection = map[string]bool{
	"confirms": true, "contradicts": true, "neutral": true,
}

func validateNoteReq(req *noteReq) string {
	req.TargetKind = strings.ToLower(strings.TrimSpace(req.TargetKind))
	req.Ticker = strings.ToUpper(strings.TrimSpace(req.Ticker))
	req.ObservationText = strings.TrimSpace(req.ObservationText)
	if req.TargetKind != "holding" && req.TargetKind != "watchlist" {
		return "targetKind must be holding|watchlist"
	}
	if req.TargetID == 0 {
		return "targetId required"
	}
	if req.Ticker == "" {
		return "ticker required"
	}
	if req.ObservationText == "" {
		return "observationText required"
	}
	if len(req.ObservationText) > 4000 {
		return "observationText too long (max 4000)"
	}
	if req.ObservationAt == "" {
		// handler fills with today on insert
	}
	// Framework / factor / direction must agree.
	if req.FactorID != "" && req.FrameworkID == "" {
		return "frameworkId required when factorId is set"
	}
	if req.FrameworkID != "" {
		f, ok := frameworks.Get(req.FrameworkID)
		if !ok {
			return "unknown frameworkId"
		}
		if req.FactorID != "" {
			if _, ok := f.QuestionByID(req.FactorID); !ok {
				return "unknown factorId for framework"
			}
		}
	}
	if req.FactorDirection != "" && !validDirection[req.FactorDirection] {
		return "factorDirection must be confirms|contradicts|neutral"
	}
	if req.SourceKind != "" && !validSourceKind[req.SourceKind] {
		return "invalid sourceKind"
	}
	return ""
}

func noteFromReq(req *noteReq) *store.ThesisNote {
	n := &store.ThesisNote{
		TargetKind:      req.TargetKind,
		TargetID:        req.TargetID,
		Ticker:          req.Ticker,
		ObservationAt:   req.ObservationAt,
		ObservationText: req.ObservationText,
	}
	if n.ObservationAt == "" {
		n.ObservationAt = time.Now().UTC().Format("2006-01-02")
	}
	if req.FrameworkID != "" {
		v := req.FrameworkID
		n.FrameworkID = &v
	}
	if req.FactorID != "" {
		v := req.FactorID
		n.FactorID = &v
	}
	if req.FactorDirection != "" {
		v := req.FactorDirection
		n.FactorDirection = &v
	}
	if req.SourceURL != "" {
		v := req.SourceURL
		n.SourceURL = &v
	}
	if req.SourceKind != "" {
		v := req.SourceKind
		n.SourceKind = &v
	}
	return n
}
