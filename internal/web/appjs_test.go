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
		"renderCommentarySection",       // FT-146 PR B: the per-holding panel, context only
		"What Jordi &amp; Cowen said",
		"hidden by the grounding gate",
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

// FT-039 (SC-85): the Telegram bot (ops/ft-bot) renders every alert kind and acknowledges only what it showed. Its
// own tests run with node when it is installed.
func TestFTBotNodeTests(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	if out, err := exec.Command(node, "--test", "../../ops/ft-bot/render.test.js").CombinedOutput(); err != nil {
		t.Fatalf("ft-bot tests failed:\n%s", out)
	}
}
