package videodigest

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"ft/internal/store"
)

func openT(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "ft.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	return New(st.DB, nil, t.TempDir()), st
}

func row(id, src, title, date, text string) TranscriptRow {
	return TranscriptRow{VideoID: id, Source: src, Channel: sources[src], Title: title, URL: "https://youtu.be/" + id, PublishedAt: date, Text: text}
}

func TestTranscriptSearchFindsTheRightVideoAndSecond(t *testing.T) {
	svc, _ := openT(t)
	ctx := context.Background()
	for _, r := range []TranscriptRow{
		row("aaaaaaaaaaa", "cowen", "Bitcoin risk levels", "2026-09-07", "[0s] welcome back everyone today we look at bitcoin\n[30s] the risk metric is still in the lower half so accumulation makes sense\n[60s] thanks for watching"),
		row("bbbbbbbbbbb", "jordi", "Climbing the wall of worry", "2026-10-11", "[0s] ai is not a bubble\n[30s] demand for compute exceeds supply\n[60s] the midterms are the next catalyst"),
		row("ccccccccccc", "cowen", "Gold and the dollar", "2025-03-02", "[0s] the dollar looks weak\n[30s] bitcoin follows liquidity"),
	} {
		if wrote, err := svc.StoreTranscript(ctx, r); err != nil || !wrote {
			t.Fatalf("store %s: %v %v", r.VideoID, wrote, err)
		}
	}
	hits, err := svc.SearchTranscripts(ctx, `"risk metric"`, SearchOptions{})
	if err != nil || len(hits) != 1 || hits[0].VideoID != "aaaaaaaaaaa" || hits[0].PublishedAt != "2026-09-07" {
		t.Fatalf("phrase search: %+v %v", hits, err)
	}
	if hits[0].TsSec != 30 || !strings.HasSuffix(hits[0].Link, "?t=30") || !strings.Contains(hits[0].Snippet, "«risk metric»") {
		t.Fatalf("hit detail: %+v", hits[0])
	}
	if strings.Contains(hits[0].Snippet, "[30s]") {
		t.Fatal("timestamp markers must not leak into the snippet")
	}
	// All words must occur; either order.
	if h, _ := svc.SearchTranscripts(ctx, "compute midterms", SearchOptions{}); len(h) != 1 || h[0].VideoID != "bbbbbbbbbbb" {
		t.Fatalf("multi-word: %+v", h)
	}
	// Source and date filters.
	if h, _ := svc.SearchTranscripts(ctx, "bitcoin", SearchOptions{}); len(h) != 2 {
		t.Fatalf("bitcoin in both cowen videos: %d", len(h))
	}
	if h, _ := svc.SearchTranscripts(ctx, "bitcoin", SearchOptions{From: "2026-01-01"}); len(h) != 1 || h[0].VideoID != "aaaaaaaaaaa" {
		t.Fatalf("date filter: %+v", h)
	}
	if h, _ := svc.SearchTranscripts(ctx, "bitcoin", SearchOptions{Source: "jordi"}); len(h) != 0 {
		t.Fatalf("source filter: %+v", h)
	}
	if h, _ := svc.SearchTranscripts(ctx, "zebra", SearchOptions{}); len(h) != 0 {
		t.Fatal("no match must be empty")
	}
}

// Search text is never an FTS5 expression: operators and quotes are neutralised.
func TestTranscriptSearchIsInjectionSafe(t *testing.T) {
	svc, _ := openT(t)
	ctx := context.Background()
	if _, err := svc.StoreTranscript(ctx, row("aaaaaaaaaaa", "cowen", "t", "2026-09-07", "[0s] near the money and not or something")); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`NEAR(a b)`, `a OR b`, `* `, `"unterminated`, `title:foo`, `-bitcoin`, `a AND`, `(((`} {
		if _, err := svc.SearchTranscripts(ctx, q, SearchOptions{}); err != nil && !strings.Contains(err.Error(), "empty search") {
			t.Errorf("query %q must not error: %v", q, err)
		}
	}
	if _, err := svc.SearchTranscripts(ctx, "   ", SearchOptions{}); err == nil {
		t.Error("an empty query must be refused")
	}
}

func TestTranscriptStoreIsIdempotentAndUpdates(t *testing.T) {
	svc, st := openT(t)
	ctx := context.Background()
	r := row("aaaaaaaaaaa", "jordi", "t", "2026-10-11", "[0s] first version about gold")
	if w, _ := svc.StoreTranscript(ctx, r); !w {
		t.Fatal("first store writes")
	}
	if w, _ := svc.StoreTranscript(ctx, r); w {
		t.Fatal("an unchanged transcript writes nothing")
	}
	r.Text = "[0s] second version about silver"
	if w, _ := svc.StoreTranscript(ctx, r); !w {
		t.Fatal("a changed transcript is updated")
	}
	if h, _ := svc.SearchTranscripts(ctx, "gold", SearchOptions{}); len(h) != 0 {
		t.Fatal("the old text must leave the index")
	}
	if h, _ := svc.SearchTranscripts(ctx, "silver", SearchOptions{}); len(h) != 1 {
		t.Fatal("the new text must be searchable")
	}
	var n int
	if err := st.DB.QueryRow(`SELECT COUNT(*) FROM video_transcripts`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("one row expected, got %d", n)
	}
	c, err := svc.TranscriptStats(ctx)
	if err != nil || c.Total != 1 || c.BySource["jordi"] != 1 {
		t.Fatalf("stats: %+v %v", c, err)
	}
	if _, err := svc.StoreTranscript(ctx, TranscriptRow{VideoID: "x"}); err == nil {
		t.Fatal("an empty transcript is refused")
	}
}

// A plain SQLite backup carries the table and its index (the backup/restic AC).
func TestTranscriptsSurviveABackup(t *testing.T) {
	svc, st := openT(t)
	ctx := context.Background()
	if _, err := svc.StoreTranscript(ctx, row("aaaaaaaaaaa", "cowen", "t", "2026-09-07", "[0s] accumulation zone talk")); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "backup.db")
	if _, err := st.DB.Exec(`VACUUM INTO ?`, dst); err != nil {
		t.Fatal(err)
	}
	b, err := sql.Open("sqlite", dst)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	var n int
	if err := b.QueryRow(`SELECT COUNT(*) FROM video_transcripts_fts WHERE video_transcripts_fts MATCH '"accumulation zone"'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("backup must carry the table and the index: n=%d err=%v", n, err)
	}
}

// Sweep stores every package's transcript, and a transcript-only sync is free and idempotent.
func TestSweepAndSyncStoreTranscripts(t *testing.T) {
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
	writePackage(t, root, "jordi", "2026-09-20", "JordiWeek2", "Week two", false, 2)
	writePackage(t, root, "cowen", "2026-09-23", "CowenWeek1", "Cowen flagship", true, 1)
	// Transcript-only (no LLM configured): the sync works, the extraction would not.
	svc := New(st.DB, nil, root)
	r, err := svc.SyncTranscripts(ctx)
	if err != nil || r.Packages != 2 || r.Written != 2 || r.Failed != 0 {
		t.Fatalf("sync: %+v %v", r, err)
	}
	if r2, _ := svc.SyncTranscripts(ctx); r2.Written != 0 || r2.Unchanged != 2 {
		t.Fatalf("second sync must write nothing: %+v", r2)
	}
	hits, err := svc.SearchTranscripts(ctx, "liquidity", SearchOptions{})
	if err != nil || len(hits) != 2 {
		t.Fatalf("both transcripts mention liquidity: %+v %v", hits, err)
	}
	var digests int
	if err := st.DB.QueryRow(`SELECT COUNT(*) FROM video_digests`).Scan(&digests); err != nil || digests != 0 {
		t.Fatalf("a transcript sync must not create digests: %d", digests)
	}
}
