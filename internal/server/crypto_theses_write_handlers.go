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
