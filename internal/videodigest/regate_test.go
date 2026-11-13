package videodigest

import (
	"context"
	"testing"
)

// SC-69 (FT-125): RegateItems re-runs the gate on stored items without an LLM call; a dry run writes
// nothing, --apply writes the new class and the run's counts.
func TestRegateItemsDryRunThenApply(t *testing.T) {
	svc, st := openT(t)
	ctx := context.Background()
	if _, err := svc.StoreTranscript(ctx, TranscriptRow{VideoID: "vid1", Source: "jordi", Title: "t", PublishedAt: "2026-11-01",
		Text: "[0s] intro words here.\n[20s] we are sold out through 2029 said the ceo.\n[100s] more words."}); err != nil {
		t.Fatal(err)
	}
	r, err := st.DB.Exec(`INSERT INTO video_digests (source, video_id, published_at, package_path, ingested_at) VALUES ('jordi','vid1','2026-11-01','/x',1)`)
	if err != nil {
		t.Fatal(err)
	}
	did, _ := r.LastInsertId()
	rr, err := st.DB.Exec(`INSERT INTO video_item_runs (digest_id, schema_version, model, items_total, counts_json, status, created_at) VALUES (?,5,'m',1,'{"wrong_place":1}','ok',1)`, did)
	if err != nil {
		t.Fatal(err)
	}
	rid, _ := rr.LastInsertId()
	if _, err := st.DB.Exec(`INSERT INTO video_items (run_id, digest_id, block, ord, fields_json, ref_start, ref_end, gate_class, gate_issues_json, signal_ok)
		VALUES (?,?,'predictions',1,'{"claim":"sold out through 2029","outcome":null}',45,55,'wrong_place','[{"type":"year","detail":"2029","scope":"near"}]',1)`, rid, did); err != nil {
		t.Fatal(err)
	}
	res, err := svc.RegateItems(ctx, false)
	if err != nil || res.Changed != 1 || res.Moved["wrong_place->grounded"] != 1 || res.After["grounded"] != 1 {
		t.Fatalf("dry run: %+v %v", res, err)
	}
	var class string
	st.DB.QueryRow(`SELECT gate_class FROM video_items`).Scan(&class)
	if class != "wrong_place" {
		t.Fatalf("a dry run must write nothing, class is %s", class)
	}
	if _, err := svc.RegateItems(ctx, true); err != nil {
		t.Fatal(err)
	}
	var issues *string
	var counts string
	st.DB.QueryRow(`SELECT gate_class, gate_issues_json FROM video_items`).Scan(&class, &issues)
	st.DB.QueryRow(`SELECT counts_json FROM video_item_runs`).Scan(&counts)
	if class != "grounded" || issues != nil || counts != `{"grounded":1}` {
		t.Fatalf("after apply: class=%s issues=%v counts=%s", class, issues, counts)
	}
	// idempotent
	if res, _ := svc.RegateItems(ctx, true); res.Changed != 0 {
		t.Fatalf("second pass changed %d", res.Changed)
	}
}
