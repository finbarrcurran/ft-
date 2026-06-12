// SC-36 — AI Nexus tab endpoints.
//
//	POST /api/nexus/upload            multipart: one or more Visser xlsx (auto-detected)
//	GET  /api/nexus/universe          the seeded universe (+ is_nexus)
//	GET  /api/nexus/technical         latest (or ?as_of=) Trend-Score snapshot
//	GET  /api/nexus/exhaustion        latest (or ?as_of=) Exhaustion snapshot
//	GET  /api/nexus/fundamentals      latest (or ?as_of=) Fundamentals snapshot
//
// All require cookie auth. Snapshots default to ?source=upload (FT-computed
// rows arrive in W4).
package server

import (
	"ft/internal/domain"
	"io"
	"net/http"
	"regexp"
)

var nexusDateRe = regexp.MustCompile(`(\d{4}-\d{2}-\d{2})`)

func nexusSource(r *http.Request) string {
	if s := r.URL.Query().Get("source"); s != "" {
		return s
	}
	return "upload"
}

// POST /api/nexus/upload — multipart, one or more "file" parts. An optional
// "as_of" form value (or a YYYY-MM-DD in the filename) supplies the date for
// Technical sheets, which carry no internal date.
func (s *Server) handleUploadNexus(w http.ResponseWriter, r *http.Request) {
	if s.nexus == nil {
		writeError(w, http.StatusServiceUnavailable, "nexus engine not configured")
		return
	}
	if err := r.ParseMultipartForm(8 << 20); err != nil { // 8 MB (sheets are larger than theses MD)
		writeError(w, http.StatusBadRequest, "could not parse multipart form: "+err.Error())
		return
	}
	formAsOf := r.FormValue("as_of")
	files := r.MultipartForm.File["file"]
	if len(files) == 0 {
		writeError(w, http.StatusBadRequest, "missing 'file' part (one or more xlsx)")
		return
	}
	results := make([]*domain.NexusIngestResult, 0, len(files))
	for _, fh := range files {
		f, err := fh.Open()
		if err != nil {
			writeError(w, http.StatusBadRequest, "could not open "+fh.Filename)
			return
		}
		data, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			writeError(w, http.StatusBadRequest, "could not read "+fh.Filename)
			return
		}
		asOf := formAsOf
		if asOf == "" {
			if m := nexusDateRe.FindString(fh.Filename); m != "" {
				asOf = m
			}
		}
		res, err := s.nexus.Ingest(r.Context(), data, asOf)
		if err != nil {
			writeError(w, http.StatusBadRequest, fh.Filename+": "+err.Error())
			return
		}
		results = append(results, res)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ingested": results})
}

// universeByTicker builds a ticker→{company,theme} lookup for read enrichment.
func (s *Server) universeByTicker(r *http.Request) map[string]domain.NexusUniverseRow {
	m := map[string]domain.NexusUniverseRow{}
	rows, err := s.store.ListNexusUniverse(r.Context())
	if err != nil {
		return m
	}
	for _, u := range rows {
		m[u.Ticker] = u
	}
	return m
}

func (s *Server) handleNexusUniverse(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ListNexusUniverse(r.Context())
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"universe": rows, "count": len(rows)})
}

func (s *Server) handleNexusTechnical(w http.ResponseWriter, r *http.Request) {
	src := nexusSource(r)
	asOf := r.URL.Query().Get("as_of")
	if asOf == "" {
		d, err := s.store.LatestNexusTechnicalAsOf(r.Context(), src)
		if mapStoreError(w, err) {
			return
		}
		asOf = d
	}
	rows, err := s.store.ListNexusTechnical(r.Context(), asOf, src)
	if mapStoreError(w, err) {
		return
	}
	uni := s.universeByTicker(r)
	for i := range rows {
		if u, ok := uni[rows[i].Ticker]; ok {
			rows[i].Company, rows[i].Theme = u.Company, u.Theme
		}
	}
	benches, _ := s.store.GetBenchmarkSnapshot(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"asOf": asOf, "source": src, "rows": rows, "benchmarks": benches})
}

func (s *Server) handleNexusExhaustion(w http.ResponseWriter, r *http.Request) {
	src := nexusSource(r)
	asOf := r.URL.Query().Get("as_of")
	if asOf == "" {
		d, err := s.store.LatestNexusExhaustionAsOf(r.Context(), src)
		if mapStoreError(w, err) {
			return
		}
		asOf = d
	}
	rows, err := s.store.ListNexusExhaustion(r.Context(), asOf, src)
	if mapStoreError(w, err) {
		return
	}
	uni := s.universeByTicker(r)
	for i := range rows {
		if u, ok := uni[rows[i].Ticker]; ok {
			rows[i].Company, rows[i].Theme = u.Company, u.Theme
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"asOf": asOf, "source": src, "rows": rows})
}

func (s *Server) handleNexusFundamentals(w http.ResponseWriter, r *http.Request) {
	src := nexusSource(r)
	asOf := r.URL.Query().Get("as_of")
	if asOf == "" {
		d, err := s.store.LatestNexusFundamentalsAsOf(r.Context(), src)
		if mapStoreError(w, err) {
			return
		}
		asOf = d
	}
	rows, err := s.store.ListNexusFundamentals(r.Context(), asOf, src)
	if mapStoreError(w, err) {
		return
	}
	uni := s.universeByTicker(r)
	for i := range rows {
		if u, ok := uni[rows[i].Ticker]; ok {
			rows[i].Company, rows[i].Theme = u.Company, u.Theme
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"asOf": asOf, "source": src, "rows": rows})
}
