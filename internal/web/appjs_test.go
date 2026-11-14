package web

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// FT-144: the front end keeps a long-open tab honest. The cash card labels eToro cash, the header's refresh time
// is re-read every minute, and a tab running older JS than the server is told to reload. app.js is not built
// into the test binary, so the source is checked directly (and syntax-checked when node is installed).
func TestAppJSStaleTabGuards(t *testing.T) {
	b, err := os.ReadFile("../../web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, want := range []string{
		"cashFromEtoro",                 // the Summary cash card source label
		"eToro cash",                    // its text
		"startRefreshPolling",           // the header is re-read every minute
		"checkNewVersion",               // an out-of-date tab is told to reload
		"setInterval(() => {\n    refreshPollTicks++",
		"last price refresh: ",          // what the header timestamp tracks
		"eToro cash is live.",           // FT-145: the manual-cash dialog says eToro is live, manual is the fallback
		"cashManualUsd",                 // and edits the manual entry, not the eToro figure
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("app.js lost %q", want)
		}
	}
	if node, err := exec.LookPath("node"); err == nil {
		if out, err := exec.Command(node, "--check", "../../web/app.js").CombinedOutput(); err != nil {
			t.Fatalf("app.js does not parse: %s", out)
		}
	}
}
