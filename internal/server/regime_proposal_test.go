package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"ft/internal/store"
)

func insertJordiDigest(t *testing.T, st *store.Store, videoID, published, state, conf, match string, ingestedAt int64) int64 {
	t.Helper()
	var stateArg, confArg any
	if state != "" {
		stateArg, confArg = state, conf
	}
	r, err := st.DB.Exec(`INSERT INTO video_digests (source, video_id, title, published_at, package_path, ingested_at, extraction_version,
		regime_narrative, catalysts, proposed_regime, proposed_confidence, proposed_quote, proposed_quote_ts, proposed_quote_match,
		cross_asset_tilt, cross_asset_quote, accumulation_stance, accumulation_quote)
		VALUES ('jordi', ?, ?, ?, '/x', ?, 3, 'He is cautious about adding.', '[{"event":"midterms","date":"2026-11"}]', ?, ?, 'wait for the hedge funds to be done', 1231, ?,
		'crypto', 'majority of my portfolio moved into crypto', 'waiting', 'wait another month')`,
		videoID, "Video "+videoID, published, ingestedAt, stateArg, confArg, match)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := r.LastInsertId()
	return id
}

func setJordi(t *testing.T, st *store.Store, regime string, setAt time.Time) {
	t.Helper()
	setRegime(t, st, "jordi", regime, setAt)
}

func getProposal(t *testing.T, s *Server) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleRegimeProposal(rec, httptest.NewRequest(http.MethodGet, "/api/regime/proposal", nil).WithContext(context.Background()))
	if rec.Code != 200 {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func postProposal(t *testing.T, s *Server, h func(http.ResponseWriter, *http.Request), body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader(b)).WithContext(context.Background()))
	return rec
}

func TestProposalShowAndHideConditions(t *testing.T) {
	s, st := snapServer(t)
	now := time.Now().UTC()
	// No digest, fresh regime: nothing to show.
	setJordi(t, st, "stable", now.AddDate(0, 0, -2))
	if p := getProposal(t, s); p["show"] != false {
		t.Fatalf("no digest, fresh regime must not show: %v", p)
	}
	// A new digest with a proposal shows, whatever the regime's age.
	id := insertJordiDigest(t, st, "aaaaaaaaaaa", "2026-10-25", "shifting", "high", "nearest", now.Add(-time.Hour).Unix())
	p := getProposal(t, s)
	if p["show"] != true || p["reason"] != "new_proposal" {
		t.Fatalf("new proposal: %v", p)
	}
	prop := p["proposal"].(map[string]any)
	if prop["state"] != "shifting" || prop["confidence"] != "high" || prop["quoteMatch"] != "nearest" || !strings.HasSuffix(prop["link"].(string), "?t=1231") {
		t.Fatalf("proposal content: %v", prop)
	}
	// Dismissed: hidden until the next digest.
	if rec := postProposal(t, s, s.handleRegimeProposalDismiss, map[string]any{"digestId": id}); rec.Code != 200 {
		t.Fatalf("dismiss: %d %s", rec.Code, rec.Body.String())
	}
	if p := getProposal(t, s); p["show"] != false {
		t.Fatalf("dismissed proposal must hide: %v", p)
	}
	// The next digest brings it back.
	id2 := insertJordiDigest(t, st, "bbbbbbbbbbb", "2026-11-01", "stable", "medium", "exact", now.Unix())
	if p := getProposal(t, s); p["show"] != true || p["proposal"].(map[string]any)["digestId"].(float64) != float64(id2) {
		t.Fatalf("next digest: %v", p)
	}
	// A manual set after the digest was ingested supersedes it.
	setJordi(t, st, "defensive", now.Add(time.Minute))
	if p := getProposal(t, s); p["show"] != false {
		t.Fatalf("superseded proposal must hide: %v", p)
	}
}

func TestProposalStaleRegimeWithoutProposal(t *testing.T) {
	s, st := snapServer(t)
	now := time.Now().UTC()
	setJordi(t, st, "stable", now.AddDate(0, 0, -30))
	if p := getProposal(t, s); p["show"] != true || p["reason"] != "stale_no_digest" {
		t.Fatalf("stale and no digest: %v", p)
	}
	// Newest digest has no proposed state: stale regime still prompts.
	id := insertJordiDigest(t, st, "aaaaaaaaaaa", "2026-10-25", "", "", "", now.Unix())
	if p := getProposal(t, s); p["show"] != true || p["reason"] != "stale_no_proposal" {
		t.Fatalf("stale, digest without a proposal: %v", p)
	}
	// Dismissing snoozes it until the next digest.
	postProposal(t, s, s.handleRegimeProposalDismiss, map[string]any{"digestId": id})
	if p := getProposal(t, s); p["show"] != false {
		t.Fatalf("snoozed: %v", p)
	}
	// An old extraction (version < 3) never proposes.
	if _, err := st.DB.Exec(`UPDATE video_digests SET extraction_version = 2, proposed_regime='shifting', proposed_confidence='high'`); err != nil {
		t.Fatal(err)
	}
	st.SetPreference(context.Background(), prefProposalHandled, "0")
	if p := getProposal(t, s); p["reason"] == "new_proposal" {
		t.Fatalf("a v2 digest must not propose: %v", p)
	}
}

func TestConsequencePreviewMatchesLiveAlertLogic(t *testing.T) {
	s, st := snapServer(t)
	now := time.Now().UTC()
	setJordi(t, st, "stable", now.AddDate(0, 0, -1))
	// Two watchlist entries in the entry zone, one outside it.
	mustExec(t, st, `INSERT INTO watchlist (user_id, ticker, kind, current_price, target_entry_low, target_entry_high, added_at, updated_at) VALUES
	  (1,'AAA','stock',10,9,11,1,1),(1,'BBB','stock',50,9,11,1,1),(1,'CCC','stock',10,9,11,1,1)`)
	insertJordiDigest(t, st, "aaaaaaaaaaa", "2026-10-25", "shifting", "high", "exact", now.Unix())
	p := getProposal(t, s)
	c := p["consequence"].(map[string]any)
	if c["EffectiveNow"] != "stable" || c["EffectiveIf"] != "shifting" || c["MultiplierNow"].(float64) != 1.0 || c["MultiplierIf"].(float64) != 0.6 ||
		c["EntryAlertsNow"] != true || c["EntryAlertsIf"] != false {
		t.Fatalf("consequence: %v", c)
	}
	if mg := c["MarginPctIf"].(float64); mg < 2.99 || mg > 3.01 {
		t.Fatalf("margin 5%% x 0.6 = 3%%, got %v", mg)
	}
	var tk []string
	for _, x := range c["AffectedTickers"].([]any) {
		tk = append(tk, x.(string))
	}
	if strings.Join(tk, ",") != "AAA,CCC" {
		t.Fatalf("affected tickers: %v", tk)
	}
	// The preview comes from the same functions the alert path uses: compare directly.
	live := s.consequenceOf(context.Background(), "defensive")
	if live.MultiplierIf != 0.6 || live.EntryAlertsIf {
		t.Fatalf("defensive: %+v", live)
	}
	// Stable -> stable changes nothing, so no ticker list.
	if c := s.consequenceOf(context.Background(), "stable"); len(c.AffectedTickers) != 0 || c.MultiplierIf != 1.0 {
		t.Fatalf("no-op: %+v", c)
	}
}

func TestApproveSetAndAudit(t *testing.T) {
	s, st := snapServer(t)
	ctx := context.Background()
	now := time.Now().UTC()
	setJordi(t, st, "stable", now.AddDate(0, 0, -20))
	id := insertJordiDigest(t, st, "aaaaaaaaaaa", "2026-10-25", "shifting", "medium", "nearest", now.Unix()-60)
	// Wrong digest id: refused, nothing changes.
	if rec := postProposal(t, s, s.handleRegimeProposalApprove, map[string]any{"digestId": id + 5}); rec.Code != http.StatusConflict {
		t.Fatalf("stale id: %d", rec.Code)
	}
	if v, _ := st.GetPreference(ctx, "regime_jordi"); v != "stable" {
		t.Fatal("a refused approve must not change the regime")
	}
	rec := postProposal(t, s, s.handleRegimeProposalApprove, map[string]any{"digestId": id})
	if rec.Code != 200 {
		t.Fatalf("approve: %d %s", rec.Code, rec.Body.String())
	}
	if v, _ := st.GetPreference(ctx, "regime_jordi"); v != "shifting" {
		t.Fatalf("approved state not applied: %q", v)
	}
	if v, _ := st.GetPreference(ctx, "regime_jordi_source"); v != "video-suggestion" {
		t.Fatalf("source: %q", v)
	}
	if v, _ := st.GetPreference(ctx, "regime_jordi_set_at"); v == "" {
		t.Fatal("set_at must be refreshed")
	}
	var inputs, note, regimeV, src string
	if err := st.DB.QueryRow(`SELECT regime, source, inputs_json, note FROM regime_history WHERE framework_id='jordi' ORDER BY id DESC LIMIT 1`).Scan(&regimeV, &src, &inputs, &note); err != nil {
		t.Fatal(err)
	}
	if regimeV != "shifting" || src != "manual" || !strings.Contains(note, "Approved from video proposal (digest "+strconv.FormatInt(id, 10)) {
		t.Fatalf("history row: %s %s %q", regimeV, src, note)
	}
	var in map[string]any
	_ = json.Unmarshal([]byte(inputs), &in)
	if in["digestId"].(float64) != float64(id) || in["quote"] != "wait for the hedge funds to be done" || in["quoteMatch"] != "nearest" || in["confidence"] != "medium" || in["prior"] != "stable" {
		t.Fatalf("inputs_json: %v", in)
	}
	// Handled: the card is gone, and the effective regime now reflects it.
	if p := getProposal(t, s); p["show"] != false {
		t.Fatalf("after approval: %v", p)
	}
	rec2 := httptest.NewRecorder()
	s.handleGetRegime(rec2, httptest.NewRequest(http.MethodGet, "/api/regime", nil).WithContext(ctx))
	var reg map[string]any
	_ = json.Unmarshal(rec2.Body.Bytes(), &reg)
	if reg["effective"] != "shifting" || reg["jordi"].(map[string]any)["source"] != "video-suggestion" {
		t.Fatalf("effective regime: %v", reg)
	}
}

func TestSetDifferentAndValidation(t *testing.T) {
	s, st := snapServer(t)
	now := time.Now().UTC()
	id := insertJordiDigest(t, st, "aaaaaaaaaaa", "2026-10-25", "shifting", "high", "exact", now.Unix())
	for _, bad := range []string{"", "bullish", "unclassified"} {
		if rec := postProposal(t, s, s.handleRegimeProposalSet, map[string]any{"digestId": id, "state": bad}); rec.Code != http.StatusBadRequest {
			t.Errorf("state %q must be a 400, got %d", bad, rec.Code)
		}
	}
	if rec := postProposal(t, s, s.handleRegimeProposalSet, map[string]any{"digestId": id, "state": "defensive"}); rec.Code != 200 {
		t.Fatalf("set: %d %s", rec.Code, rec.Body.String())
	}
	if v, _ := st.GetPreference(context.Background(), "regime_jordi"); v != "defensive" {
		t.Fatalf("applied: %q", v)
	}
	var note string
	st.DB.QueryRow(`SELECT note FROM regime_history WHERE framework_id='jordi' ORDER BY id DESC LIMIT 1`).Scan(&note)
	if !strings.HasPrefix(note, "Set differently from video proposal") {
		t.Fatalf("note: %q", note)
	}
}

// A digest with no proposed state cannot be approved; demo mode shows and changes nothing.
func TestNoProposalAndDemoMode(t *testing.T) {
	s, st := snapServer(t)
	now := time.Now().UTC()
	id := insertJordiDigest(t, st, "aaaaaaaaaaa", "2026-10-25", "", "", "", now.Unix())
	if rec := postProposal(t, s, s.handleRegimeProposalApprove, map[string]any{"digestId": id}); rec.Code != http.StatusBadRequest {
		t.Fatalf("approve without a proposal: %d", rec.Code)
	}
	if err := st.SetPreference(context.Background(), "demo_mode", "on"); err != nil {
		t.Fatal(err)
	}
	if p := getProposal(t, s); p["show"] != false || p["reason"] != "demo" {
		t.Fatalf("demo: %v", p)
	}
	if rec := postProposal(t, s, s.handleRegimeProposalDismiss, map[string]any{"digestId": id}); rec.Code != http.StatusForbidden {
		t.Fatalf("demo mode must refuse changes: %d", rec.Code)
	}
}
