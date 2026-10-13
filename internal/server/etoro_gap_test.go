package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ft/internal/etoro"
	"ft/internal/store"
)

// SC-51 Part B: a statement starting after coverage end + 1 is held back until confirmed; an
// overlapping one and the clean tile apply straight away.
func TestEtoroApplyGapNeedsConfirmation(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxUserID, int64(1))
	st, err := store.Open(filepath.Join(t.TempDir(), "ft.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	s := &Server{store: st}
	stmt := func(start, end, id string) *etoro.Statement {
		return &etoro.Statement{FileName: id + ".xlsx", Period: etoro.Period{RangeStart: start, RangeEnd: end},
			Positions: []etoro.ClosedPosition{{PositionID: id, CloseDate: end, Year: 2026, AssetType: "Stocks", ProfitUSD: 1, ProfitEUR: 1}}}
	}
	apply := func(body string, p *etoro.Statement) *httptest.ResponseRecorder {
		storePendingEtoro(1, &pendingEtoro{Statement: p, Stored: time.Now()})
		rec := httptest.NewRecorder()
		s.handleEtoroImportApply(rec, httptest.NewRequest(http.MethodPost, "/api/etoro/import/apply", strings.NewReader(body)).WithContext(ctx))
		return rec
	}
	if rec := apply(`{}`, stmt("2026-01-01", "2026-10-09", "a")); rec.Code != 200 {
		t.Fatalf("first import: %d %s", rec.Code, rec.Body.String())
	}
	// Starts 2026-10-15: gap 10-10..10-14.
	rec := apply(`{}`, stmt("2026-10-15", "2026-10-20", "b"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("gap without confirmation must be refused: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		NeedsConfirm bool             `json:"needsConfirm"`
		StartCheck   store.StartCheck `json:"startCheck"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if !out.NeedsConfirm || out.StartCheck.MissingFrom != "2026-10-10" || out.StartCheck.MissingTo != "2026-10-14" {
		t.Fatalf("gap dates: %+v", out)
	}
	// The staged statement survives the refusal; confirming applies it.
	rec = httptest.NewRecorder()
	s.handleEtoroImportApply(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"confirmGap":true}`)).WithContext(ctx))
	if rec.Code != 200 {
		t.Fatalf("confirmed gap: %d %s", rec.Code, rec.Body.String())
	}
	// Overlap (starts 2026-10-05) and clean tile (starts 2026-10-21) apply without confirmation.
	if rec := apply(`{}`, stmt("2026-10-05", "2026-10-25", "c")); rec.Code != 200 {
		t.Fatalf("overlap: %d %s", rec.Code, rec.Body.String())
	}
	if rec := apply(`{}`, stmt("2026-10-26", "2026-10-30", "d")); rec.Code != 200 {
		t.Fatalf("clean tile: %d %s", rec.Code, rec.Body.String())
	}
}
