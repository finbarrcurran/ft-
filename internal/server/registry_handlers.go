package server

import "net/http"

// GET /api/registry — SC-28 Registry / Document-Control tab.
//
// Read-only composition over the four live source tables (theses_index,
// crypto_theses, sector_scorecards, crypto_adapters). No persisted Registry
// table, no cache: a source edit is reflected on the next load with no sync
// step (D28.2/D28.3). The client takes this one pull and does all grouping,
// sorting, filtering and search in the browser (D28.12).
func (s *Server) handleRegistry(w http.ResponseWriter, r *http.Request) {
	arts, err := s.store.RegistryArtifacts(r.Context())
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"artifacts": arts})
}
