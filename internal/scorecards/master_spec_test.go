package scorecards

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The working master spec must stay readable in one Claude Drive read (which
// stops near 100,000 characters) and must carry its structure markers. The
// fuller checks live in tools/spec_archive.py (`check`); this keeps the core of
// them in `go test` so a bump that skips them fails the usual build gate.
func TestMasterSpecStructure(t *testing.T) {
	b, err := os.ReadFile("seed/master-spec.md")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if n := len([]rune(s)); n > 90000 {
		t.Errorf("working spec is %d characters (max 90,000): move history to docs/spec-archive/", n)
	}

	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	end := regexp.MustCompile(`^<!-- END FT-master-spec v(\d+\.\d+\.\d+) -->$`).FindStringSubmatch(lines[len(lines)-1])
	if end == nil {
		t.Fatal("last line must be `<!-- END FT-master-spec vX.Y.Z -->`")
	}
	cur := regexp.MustCompile(`\*\*Current version: v(\d+\.\d+\.\d+) · \d{4}-\d\d-\d\d\*\*`).FindStringSubmatch(s)
	ref := regexp.MustCompile(`<!-- reference_reviewed: v(\d+\.\d+\.\d+) \d{4}-\d\d-\d\d -->`).FindStringSubmatch(s)
	if cur == nil || ref == nil {
		t.Fatalf("missing current-version line (%v) or reference_reviewed marker (%v)", cur != nil, ref != nil)
	}
	if cur[1] != end[1] || ref[1] != end[1] {
		t.Errorf("versions disagree: current v%s, END marker v%s, reference_reviewed v%s — update sections 1-12 and the marker on every bump", cur[1], end[1], ref[1])
	}

	i := strings.Index(s, "\n## 13.")
	if i < 0 {
		t.Fatal("missing section 13")
	}
	entries := regexp.MustCompile(`(?m)^### v\d+\.\d+\.\d+ · `).FindAllStringIndex(s[i:], -1)
	if len(entries) == 0 || len(entries) > 10 {
		t.Errorf("section 13 has %d recent entries, want 1-10", len(entries))
	}
	for k, loc := range entries {
		stop := len(s[i:])
		if k+1 < len(entries) {
			stop = entries[k+1][0]
		} else if j := strings.Index(s[i+loc[0]:], "\n---\n"); j >= 0 {
			stop = loc[0] + j
		}
		if n := len([]rune(strings.TrimSpace(s[i+loc[0] : i+stop]))); n > 800 {
			t.Errorf("recent entry %d is %d characters (max 800): put the detail in the archive entry", k+1, n)
		}
	}
}
