package videodigest

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"ft/internal/llm"
	"ft/internal/store"
)

func setExtract(t *testing.T, root, source, date, vid string, extract bool, flagship bool) {
	t.Helper()
	path := filepath.Join(root, source, date+"_"+vid, "meta.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	m["extract"], m["is_flagship"] = extract, flagship
	out, _ := json.Marshal(m)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
}

// Transcript-only packages are stored but never swept; the extract run digests them on request,
// newest first, within a ceiling, and never twice.
func TestTranscriptOnlyPackagesAndExtractRun(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	st, err := store.Open(filepath.Join(tmp, "ft.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(tmp, "packages")
	writePackage(t, root, "cowen", "2026-09-20", "CowenFlagA1", "Flagship A", true, 0)
	writePackage(t, root, "cowen", "2026-09-21", "CowenOther01", "Other", false, 0)
	writePackage(t, root, "cowen", "2026-09-27", "CowenFlagB1", "Flagship B", true, 0)
	for _, p := range []struct {
		date, id string
		flag     bool
	}{{"2026-09-20", "CowenFlagA1", true}, {"2026-09-21", "CowenOther01", false}, {"2026-09-27", "CowenFlagB1", true}} {
		setExtract(t, root, "cowen", p.date, p.id, false, p.flag)
	}
	sb := &stub{}
	srv := httptest.NewServer(sb)
	defer srv.Close()
	l := llm.NewService(st, "test-key")
	l.APIBaseURL = srv.URL
	svc := New(st.DB, l, root)

	// The sweep stores transcripts and does NOT extract transcript-only packages.
	res, err := svc.Sweep(ctx)
	if err != nil || len(res) != 0 {
		t.Fatalf("sweep must skip transcript-only packages: %v %v", res, err)
	}
	var tr, dg int
	st.DB.QueryRow(`SELECT COUNT(*) FROM video_transcripts`).Scan(&tr)
	st.DB.QueryRow(`SELECT COUNT(*) FROM video_digests`).Scan(&dg)
	if tr != 3 || dg != 0 {
		t.Fatalf("transcripts %d digests %d", tr, dg)
	}
	// Extract flagship-only, newest first, one video: picks B (2026-09-27).
	run, err := svc.Extract(ctx, ExtractOptions{Source: "cowen", FlagshipOnly: true, NewestFirst: true, MaxVideos: 1}, nil)
	if err != nil || len(run.Results) != 1 || run.Results[0].VideoID != "CowenFlagB1" || run.StoppedBy != "limit" || run.Remaining != 1 {
		t.Fatalf("newest-first: %+v %v", run, err)
	}
	// The next run continues with A, never re-digesting B, and skips the non-flagship.
	run, err = svc.Extract(ctx, ExtractOptions{Source: "cowen", FlagshipOnly: true, NewestFirst: true}, nil)
	if err != nil || len(run.Results) != 1 || run.Results[0].VideoID != "CowenFlagA1" {
		t.Fatalf("second run: %+v %v", run, err)
	}
	// A ceiling stops the run before spending more.
	run, err = svc.Extract(ctx, ExtractOptions{Source: "cowen", MaxCostUSD: 0.01}, nil)
	if err != nil || len(run.Results) != 0 || run.StoppedBy != "ceiling" {
		t.Fatalf("ceiling: %+v %v", run, err)
	}
	// Without the flagship filter the remaining package is eligible.
	todo, _ := svc.Eligible(ctx, ExtractOptions{Source: "cowen"})
	if len(todo) != 1 || todo[0].Meta.VideoID != "CowenOther01" {
		t.Fatalf("eligible: %v", todo)
	}
}
