package videodigest

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"ft/internal/llm"
	"ft/internal/store"
)

// SC-68 (FT-124): a 22v intraweek video digested from the stored transcript is context only. The
// 11-11 intraweek never replaces the 11-08 weekly as the newest Jordi digest, carries no regime
// proposal, quotes or timestamps, and is not the prior-week context of anything.
func TestIngestStoredIsContextOnlyAndNeverNewest(t *testing.T) {
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
	sb := &stub{}
	srv := httptest.NewServer(sb)
	defer srv.Close()
	l := llm.NewService(st, "test-key")
	l.APIBaseURL = srv.URL
	root := filepath.Join(tmp, "packages")
	writePackage(t, root, "jordi", "2026-11-08", "WeeklyNov08", "Weekly paper", false, 0)
	svc := New(st.DB, l, root)
	if res, err := svc.Sweep(ctx); err != nil || len(res) != 1 || res[0].Status != "ingested" {
		t.Fatalf("weekly: %+v %v", res, err)
	}

	row := TranscriptRow{VideoID: "22v-2538", Source: "jordi", Channel: sources["jordi"], Title: "Intraweek Market Update Nov 11",
		URL: "https://ai.22vresearch.com/video-stream/2538/", PublishedAt: "2026-11-11", Method: "whisper", DurationSec: 975,
		Text: "bitcoin is above the two hundred day and liquidity is the whole story this week"}
	if _, err := svc.StoreTranscript(ctx, row); err != nil {
		t.Fatal(err)
	}
	r := svc.IngestStored(ctx, "22v-2538", false)
	if r.Status != "ingested" {
		t.Fatalf("%+v", r)
	}
	// the prompt tells the model there are no markers
	if last := sb.users[len(sb.users)-1]; !strings.Contains(last, "NO time markers") {
		t.Fatalf("prompt: %.200q", last)
	}

	var flag int
	var path string
	var proposed, tilt *string
	st.DB.QueryRow(`SELECT is_flagship, package_path, proposed_regime, cross_asset_tilt FROM video_digests WHERE video_id='22v-2538'`).Scan(&flag, &path, &proposed, &tilt)
	if flag != 0 || path != "" || proposed != nil || tilt != nil {
		t.Fatalf("flagship=%d path=%q proposed=%v tilt=%v", flag, path, proposed, tilt)
	}
	var quotes, tsMentions, mentions int
	st.DB.QueryRow(`SELECT count(*) FROM video_quotes q JOIN video_digests d ON d.id=q.digest_id WHERE d.video_id='22v-2538'`).Scan(&quotes)
	st.DB.QueryRow(`SELECT count(*), count(ts_sec) FROM video_mentions m JOIN video_digests d ON d.id=m.digest_id WHERE d.video_id='22v-2538'`).Scan(&mentions, &tsMentions)
	if quotes != 0 || mentions == 0 || tsMentions != 0 {
		t.Fatalf("quotes=%d mentions=%d with ts=%d", quotes, mentions, tsMentions)
	}

	// the weekly stays the newest Jordi digest, for the vote and for the card query
	if svc.isNewestJordi(ctx, "22v-2538") || !svc.isNewestJordi(ctx, "WeeklyNov08") {
		t.Fatal("the intraweek video replaced the weekly as newest Jordi digest")
	}
	var vid string
	st.DB.QueryRow(`SELECT video_id FROM video_digests WHERE source='jordi' AND is_flagship=1 ORDER BY published_at DESC, id DESC LIMIT 1`).Scan(&vid)
	if vid != "WeeklyNov08" {
		t.Fatalf("card query picks %s", vid)
	}
	// and it is never the prior-week context of the next weekly
	pr, err := svc.priorFor(ctx, packageMeta{Source: "jordi", PublishedAt: "2026-11-15"})
	if err != nil || pr == nil || pr.PublishedAt != "2026-11-08" {
		t.Fatalf("prior = %+v %v", pr, err)
	}
	// Reground skips it (no package on disk) instead of failing
	if _, err := svc.Reground(ctx); err != nil {
		t.Fatalf("reground: %v", err)
	}
	// only 22v Jordi transcripts are accepted
	if r := svc.IngestStored(ctx, "WeeklyNov08", true); r.Status != "failed" {
		t.Fatalf("%+v", r)
	}
	// idempotent
	if r := svc.IngestStored(ctx, "22v-2538", false); r.Status != "skipped" {
		t.Fatalf("%+v", r)
	}
}
