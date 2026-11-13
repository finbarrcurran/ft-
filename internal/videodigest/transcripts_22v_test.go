package videodigest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const v22Sample = "Title: Intraweek Market Update Nov 11\nDate: 2026-11-11\nSource: https://ai.22vresearch.com/video-stream/2538/\nWhisper small auto-transcript\n\nthe fed needs two more hikes\n\nbecause oil came down sharply\n"

func TestParseTextTranscript(t *testing.T) {
	r, err := ParseTextTranscript(v22Sample, "jordi")
	if err != nil {
		t.Fatal(err)
	}
	if r.VideoID != "22v-2538" || r.Method != "whisper" || r.Source != "jordi" || r.PublishedAt != "2026-11-11" ||
		r.URL != "https://ai.22vresearch.com/video-stream/2538/" || r.Title != "Intraweek Market Update Nov 11" ||
		r.Text != "the fed needs two more hikes because oil came down sharply" {
		t.Fatalf("%+v", r)
	}
	if _, err := ParseTextTranscript("no header here", "jordi"); err == nil {
		t.Fatal("expected an error without the whisper header")
	}
}

func TestImportTextTranscriptsOnlyChosenAndSearchLink(t *testing.T) {
	svc, _ := openT(t)
	ctx := context.Background()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte(v22Sample), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte(strings.NewReplacer("2538", "2517", "Nov 11", "Nov 08", "2026-11-11", "2026-11-08").Replace(v22Sample)), 0o644)
	r, err := svc.ImportTextTranscripts(ctx, dir, "jordi", map[string]bool{"2538": true})
	if err != nil || r.Written != 1 || r.Failed != 0 {
		t.Fatalf("%+v %v", r, err)
	}
	if r2, _ := svc.ImportTextTranscripts(ctx, dir, "jordi", map[string]bool{"2538": true}); r2.Unchanged != 1 || r2.Written != 0 {
		t.Fatalf("re-import must write nothing: %+v", r2)
	}
	hits, err := svc.SearchTranscripts(ctx, "hikes", SearchOptions{Source: "jordi"})
	if err != nil || len(hits) != 1 {
		t.Fatalf("%v %v", hits, err)
	}
	if hits[0].Link != "https://ai.22vresearch.com/video-stream/2538/" || hits[0].TsSec != 0 {
		t.Fatalf("a non-YouTube url must be returned as-is: %+v", hits[0])
	}
	// the YouTube path is unchanged
	if got := transcriptLink("https://youtu.be/abc", "abc", 90); got != "https://youtu.be/abc?t=90" {
		t.Fatal(got)
	}
	if got := transcriptLink("", "abc", 5); got != "https://youtu.be/abc?t=5" {
		t.Fatal(got)
	}
}
