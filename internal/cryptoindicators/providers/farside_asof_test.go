package providers

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// SC-49: the reading carries the date of the newest row; a file that was just written
// (fresh mtime) but holds old rows reports the OLD date, so freshness is never inferred
// from the file's modification time.
func TestFarsideReadingCarriesDataDateNotFileTime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "etf-flow.json")
	body := `{"fetchedAt":"2026-10-13T00:25:00Z","rolling7dM":-489,"rows":[{"date":"2026-10-09","totalM":21.1},{"date":"2026-10-08","totalM":-244.1}]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil { // mtime = now
		t.Fatal(err)
	}
	r, ok := readFarsideFromPath(path)
	if !ok || r.Value == nil || *r.Value != -489 {
		t.Fatalf("reading not served: %+v ok=%v", r, ok)
	}
	if r.AsOf != "2026-10-09" {
		t.Fatalf("as-of must be the newest ROW date (2026-10-09), got %q", r.AsOf)
	}
	// rows in any order: the maximum date wins
	body2 := `{"fetchedAt":"2026-10-13T00:25:00Z","rolling7dM":1,"rows":[{"date":"2026-10-01","totalM":1},{"date":"2026-10-12","totalM":2},{"date":"2026-10-05","totalM":3}]}`
	_ = os.WriteFile(path, []byte(body2), 0o644)
	if r2, _ := readFarsideFromPath(path); r2.AsOf != "2026-10-12" {
		t.Fatalf("as-of must be the max date, got %q", r2.AsOf)
	}
	// a file older than FarsideJSONMaxAge is still not served from cache (fetch decision unchanged)
	old := time.Now().Add(-48 * time.Hour)
	_ = os.Chtimes(path, old, old)
	if _, ok := readFarsideFromPath(path); ok {
		t.Fatal("the 36 h fetch rule must be unchanged")
	}
}
