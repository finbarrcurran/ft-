// Spec 9l D25 Phase 1 — Crypto theses write handlers.
//
// New endpoints (companion to existing GET /api/crypto/theses + GET .../{symbol}/{version}):
//
//   POST   /api/crypto/theses                              Create draft thesis
//   PUT    /api/crypto/theses/{symbol}/{version}           Update draft (status='draft' only)
//   POST   /api/crypto/theses/{symbol}/{version}/lock      Transition draft → locked
//   GET    /api/crypto/theses/drafts                       List all drafts
//   DELETE /api/crypto/theses/{symbol}/{version}           Delete draft
//
// Status carve-outs per v0.6.1 §B:
//   PUT on status='locked' → 400 ErrCannotEditLocked
//   PUT on status='needs-review' → 400 ErrCannotEditNeedsReview (D26 follow-on)
//   POST /lock on status='locked' → 400 (already locked)
//   POST /lock on status='needs-review' → 400 (D26 acknowledgment path)

package server

import (
	"encoding/json"
	"errors"
	"ft/internal/cryptotheses"
	"net/http"
)

// POST /api/crypto/theses
func (s *Server) handleCryptoThesisCreate(w http.ResponseWriter, r *http.Request) {
	var in cryptotheses.DraftThesisInput
	if !decodeJSON(r, w, &in) {
		return
	}
	id, err := s.cryptoWrite.CreateDraft(r.Context(), &in)
	if err != nil {
		mapWriteErr(w, err)
		return
	}
	// Re-fetch detail for canonical response shape
	detail, derr := s.cryptoTheses.Get(r.Context(), in.Symbol, in.Version)
	if derr != nil {
		// Created OK, but read-back failed — return ID-only response
		writeJSON(w, http.StatusCreated, map[string]any{
			"thesisId": id,
			"symbol":   in.Symbol,
			"version":  in.Version,
		})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"thesisId":    id,
		"thesis":      detail,
		"validations": map[string]any{"passed": true, "warnings": []string{}, "errors": []string{}},
	})
}

// PUT /api/crypto/theses/{symbol}/{version}
func (s *Server) handleCryptoThesisUpdateDraft(w http.ResponseWriter, r *http.Request) {
	symbol := r.PathValue("symbol")
	version := r.PathValue("version")
	var in cryptotheses.DraftThesisInput
	if !decodeJSON(r, w, &in) {
		return
	}
	in.Symbol = symbol
	in.Version = version
	if err := s.cryptoWrite.UpdateDraft(r.Context(), symbol, version, &in); err != nil {
		mapWriteErr(w, err)
		return
	}
	detail, err := s.cryptoTheses.Get(r.Context(), symbol, version)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"saved": true})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"thesis": detail})
}

// POST /api/crypto/theses/{symbol}/{version}/lock
func (s *Server) handleCryptoThesisLock(w http.ResponseWriter, r *http.Request) {
	symbol := r.PathValue("symbol")
	version := r.PathValue("version")
	res, err := s.cryptoWrite.Lock(r.Context(), symbol, version)
	if err != nil {
		mapWriteErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"locked": res})
}

// GET /api/crypto/theses/drafts
func (s *Server) handleCryptoDraftsList(w http.ResponseWriter, r *http.Request) {
	drafts, err := s.cryptoWrite.ListDrafts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"drafts": drafts})
}

// DELETE /api/crypto/theses/{symbol}/{version}
func (s *Server) handleCryptoThesisDelete(w http.ResponseWriter, r *http.Request) {
	symbol := r.PathValue("symbol")
	version := r.PathValue("version")
	if err := s.cryptoWrite.DeleteDraft(r.Context(), symbol, version); err != nil {
		mapWriteErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// mapWriteErr translates write-service errors to HTTP codes.
func mapWriteErr(w http.ResponseWriter, err error) {
	var mmf cryptotheses.ErrMissingMandatoryField
	switch {
	case errors.Is(err, cryptotheses.ErrThesisNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, cryptotheses.ErrAdapterNotFound):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, cryptotheses.ErrCannotEditLocked):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, cryptotheses.ErrCannotEditNeedsReview):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, cryptotheses.ErrNotDraft):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, cryptotheses.ErrSelfDependency):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, cryptotheses.ErrDuplicateDependency):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, cryptotheses.ErrNonInfraOracleParent):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, cryptotheses.ErrBadSubCriterion):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.As(err, &mmf):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

// POST /api/crypto/theses/{symbol}/{version}/acknowledge-cascade  (D26)
//
// Body (optional):
//   { "note": "free-text explanation of acknowledgment" }
//
// Transitions a needs-review thesis back to locked. Marks all unresolved
// cascade_events for the thesis as resolved. Appends an event_rescore
// history row.
func (s *Server) handleCryptoThesisAcknowledgeCascade(w http.ResponseWriter, r *http.Request) {
	symbol := r.PathValue("symbol")
	version := r.PathValue("version")
	var body struct {
		Note string `json:"note,omitempty"`
	}
	// Body is optional; ignore decode error
	_ = decodeJSONOptional(r, &body)
	res, err := s.cryptoWrite.AcknowledgeCascade(r.Context(), symbol, version, body.Note)
	if err != nil {
		mapAcknowledgeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"acknowledged": res})
}

func mapAcknowledgeErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, cryptotheses.ErrThesisNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, cryptotheses.ErrNotNeedsReview):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, cryptotheses.ErrPPGNowFails):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

// decodeJSONOptional decodes JSON body without writing an error on parse failure.
// Used for endpoints where the body is optional.
func decodeJSONOptional(r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	return dec.Decode(dst)
}

// POST /api/crypto/theses/{symbol}/{version}/fork  (D27)
//
// Body (optional):
//   { "note": "free-text rationale for the fork" }
//
// Spawns a new draft at vN+1 inheriting source content. Transitions source
// from locked/needs-review to 'forked'. Returns ForkResult with both ids.
func (s *Server) handleCryptoThesisFork(w http.ResponseWriter, r *http.Request) {
	symbol := r.PathValue("symbol")
	version := r.PathValue("version")
	var body struct {
		Note string `json:"note,omitempty"`
	}
	_ = decodeJSONOptional(r, &body)
	res, err := s.cryptoWrite.ForkToV2(r.Context(), symbol, version, body.Note)
	if err != nil {
		mapForkErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"forked": res})
}

func mapForkErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, cryptotheses.ErrThesisNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, cryptotheses.ErrCannotForkDraft),
		errors.Is(err, cryptotheses.ErrCannotForkForked),
		errors.Is(err, cryptotheses.ErrCannotForkInvalid),
		errors.Is(err, cryptotheses.ErrVersionExists),
		errors.Is(err, cryptotheses.ErrVersionUnparseable):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}
