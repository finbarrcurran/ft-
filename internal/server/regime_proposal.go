package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	"ft/internal/regime"
)

// SC-52 P1 — the login regime proposal (Jordi only). The stored video-digest fields (P0) become a
// card Fin approves, sets differently or dismisses. Nothing is ever applied automatically (D7):
// every state change is a click, goes through the same path as the manual set, and leaves a
// regime_history row that names the digest and the evidence. No LLM call happens at login.

const (
	prefProposalHandled = "regime_proposal_handled_digest" // the newest digest id Fin approved, set or dismissed
)

type proposalDigest struct {
	ID             int64
	VideoID, Title string
	PublishedAt    string
	IngestedAt     int64
	Narrative      sql.NullString
	Catalysts      sql.NullString
	State          sql.NullString
	Confidence     sql.NullString
	Quote          sql.NullString
	QuoteTs        sql.NullInt64
	Match          sql.NullString
	Tilt           sql.NullString
	TiltQuote      sql.NullString
	Stance         sql.NullString
	StanceQuote    sql.NullString
	Version        int
}

func (s *Server) newestJordiDigest(ctx context.Context) (*proposalDigest, error) {
	var d proposalDigest
	err := s.store.DB.QueryRowContext(ctx, `
		SELECT id, video_id, title, published_at, ingested_at, regime_narrative, catalysts, proposed_regime, proposed_confidence,
		       proposed_quote, proposed_quote_ts, proposed_quote_match, cross_asset_tilt, cross_asset_quote,
		       accumulation_stance, accumulation_quote, extraction_version
		  FROM video_digests WHERE source = 'jordi' ORDER BY published_at DESC, id DESC LIMIT 1`).
		Scan(&d.ID, &d.VideoID, &d.Title, &d.PublishedAt, &d.IngestedAt, &d.Narrative, &d.Catalysts, &d.State, &d.Confidence,
			&d.Quote, &d.QuoteTs, &d.Match, &d.Tilt, &d.TiltQuote, &d.Stance, &d.StanceQuote, &d.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &d, err
}

func nullStr(n sql.NullString) any {
	if n.Valid && n.String != "" {
		return n.String
	}
	return nil
}

// Consequence is what a proposed Jordi state would change, computed at read time from the live
// alert logic (the same functions the alert path uses).
type Consequence struct {
	EffectiveNow, EffectiveIf     string
	MultiplierNow, MultiplierIf   float64
	MarginPctNow, MarginPctIf     float64
	EntryAlertsNow, EntryAlertsIf bool
	AffectedTickers               []string // watchlist tickers in the entry zone whose entry alert would switch on or off
}

func (s *Server) consequenceOf(ctx context.Context, proposed string) Consequence {
	j := s.loadRegimeSideCtx(ctx, "jordi")
	c := s.loadRegimeSideCtx(ctx, "cowen")
	now := regime.Effective(regime.Regime(j.Regime), regime.Regime(c.Regime))
	ifp := regime.Effective(regime.Regime(proposed), regime.Regime(c.Regime))
	out := Consequence{
		EffectiveNow: string(now), EffectiveIf: string(ifp),
		MultiplierNow: regime.AlertMarginMultiplier(now), MultiplierIf: regime.AlertMarginMultiplier(ifp),
		EntryAlertsNow: regime.GatesWatchlistEntryZone(now), EntryAlertsIf: regime.GatesWatchlistEntryZone(ifp),
	}
	out.MarginPctNow, out.MarginPctIf = 5*out.MultiplierNow, 5*out.MultiplierIf // Spec 3 D12 base margin 5%
	if out.EntryAlertsNow != out.EntryAlertsIf {
		wl, err := s.callHandlerJSON(ctx, 1, "/api/watchlist", url.Values{}, s.handleListWatchlist)
		if err == nil {
			for _, x := range arr(wl, "watchlist") {
				m, _ := x.(map[string]any)
				if b, _ := m["inRange"].(bool); b {
					out.AffectedTickers = append(out.AffectedTickers, fstr(m["ticker"]))
				}
			}
			sort.Strings(out.AffectedTickers)
		}
	}
	return out
}

// loadRegimeSideCtx is loadRegimeSide without an *http.Request.
func (s *Server) loadRegimeSideCtx(ctx context.Context, framework string) regimeSide {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
	return s.loadRegimeSide(req, framework)
}

// proposalState decides whether the card shows, and why.
func (s *Server) proposalState(ctx context.Context) (show bool, reason string, d *proposalDigest, err error) {
	d, err = s.newestJordiDigest(ctx)
	if err != nil {
		return false, "", nil, err
	}
	j := s.loadRegimeSideCtx(ctx, "jordi")
	handled := int64(0)
	if v, e := s.store.GetPreference(ctx, prefProposalHandled); e == nil {
		handled, _ = strconv.ParseInt(v, 10, 64)
	}
	hasProposal := d != nil && d.Version >= 3 && d.State.Valid && d.State.String != "" && d.State.String != "unclassified"
	fresher := d != nil && handled < d.ID
	// A manual set made after the digest was ingested supersedes its proposal.
	superseded := d != nil && j.SetAt != nil && j.SetAt.Unix() > d.IngestedAt
	switch {
	case hasProposal && fresher && !superseded:
		return true, "new_proposal", d, nil
	case j.Stale && fresher && !superseded:
		return true, "stale_no_proposal", d, nil
	case j.Stale && d == nil:
		return true, "stale_no_digest", d, nil
	}
	return false, "none", d, nil
}

// GET /api/regime/proposal
func (s *Server) handleRegimeProposal(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if s.demoModeOn(ctx) {
		writeJSON(w, http.StatusOK, map[string]any{"show": false, "reason": "demo"})
		return
	}
	show, reason, d, err := s.proposalState(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	j := s.loadRegimeSideCtx(ctx, "jordi")
	out := map[string]any{"show": show, "reason": reason, "current": map[string]any{
		"state": j.Regime, "setAt": j.SetAt, "stale": j.Stale, "source": j.Source,
		"ageDays": func() any {
			if j.SetAt == nil {
				return nil
			}
			return int(time.Since(*j.SetAt).Hours() / 24)
		}(),
	}}
	if mr, ok, _ := s.macroRegime.LatestRegime(ctx); ok {
		out["macro"] = map[string]any{"suggestedJordi": mr.SuggestedJordi, "quadrant": mr.Quadrant, "shorthand": mr.Shorthand, "computedAt": mr.ComputedAt}
	}
	if d != nil {
		var cats any
		if d.Catalysts.Valid {
			_ = json.Unmarshal([]byte(d.Catalysts.String), &cats)
		}
		prop := map[string]any{
			"digestId": d.ID, "videoId": d.VideoID, "title": d.Title, "publishedAt": d.PublishedAt,
			"narrative": nullStr(d.Narrative), "catalysts": cats,
			"state": nullStr(d.State), "confidence": nullStr(d.Confidence),
			"quote": nullStr(d.Quote), "quoteMatch": nullStr(d.Match),
			"tilt": nullStr(d.Tilt), "tiltQuote": nullStr(d.TiltQuote),
			"stance": nullStr(d.Stance), "stanceQuote": nullStr(d.StanceQuote),
			"extractionVersion": d.Version,
		}
		if d.QuoteTs.Valid {
			prop["quoteTs"] = d.QuoteTs.Int64
			prop["link"] = fmt.Sprintf("https://youtu.be/%s?t=%d", d.VideoID, d.QuoteTs.Int64)
		} else {
			prop["link"] = "https://youtu.be/" + d.VideoID
		}
		out["proposal"] = prop
		if d.State.Valid && d.State.String != "" {
			out["consequence"] = s.consequenceOf(ctx, d.State.String)
		}
	}
	// Consequences of each state, so "set a different state" shows its effect before the click.
	byState := map[string]Consequence{}
	for _, st := range []string{"stable", "shifting", "defensive"} {
		byState[st] = s.consequenceOf(ctx, st)
	}
	out["consequences"] = byState
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) applyProposalState(ctx context.Context, d *proposalDigest, state, how string) error {
	if !regime.Valid(state) || state == "unclassified" {
		return errBadRegime
	}
	now := time.Now().UTC()
	prior, _ := s.store.GetPreference(ctx, "regime_jordi")
	inputs := map[string]any{"prior": prior, "digestId": d.ID, "videoId": d.VideoID, "video": d.Title, "published": d.PublishedAt,
		"proposed": nullStr(d.State), "confidence": nullStr(d.Confidence), "quote": nullStr(d.Quote), "quoteMatch": nullStr(d.Match),
		"quoteTs": func() any {
			if d.QuoteTs.Valid {
				return d.QuoteTs.Int64
			}
			return nil
		}(), "how": how}
	b, _ := json.Marshal(inputs)
	for k, v := range map[string]string{
		"regime_jordi":        state,
		"regime_jordi_set_at": strconv.FormatInt(now.Unix(), 10),
		"regime_jordi_source": "video-suggestion",
		prefProposalHandled:   strconv.FormatInt(d.ID, 10),
	} {
		if err := s.store.SetPreference(ctx, k, v); err != nil {
			return err
		}
	}
	note := fmt.Sprintf("%s from video proposal (digest %d: %q, %s)", how, d.ID, d.Title, d.PublishedAt)
	// regime_history.source is CHECKed to manual|auto_cowen_form, so the row says 'manual'; the
	// video origin is in the note, in inputs_json and in regime_jordi_source.
	_, err := s.store.RecordRegimeChange(ctx, "jordi", state, "manual", string(b), note)
	return err
}

type proposalAction struct {
	DigestID int64  `json:"digestId"`
	State    string `json:"state"`
}

func (s *Server) proposalDigestFor(w http.ResponseWriter, r *http.Request, a proposalAction) (*proposalDigest, bool) {
	if s.demoModeOn(r.Context()) {
		writeError(w, http.StatusForbidden, "regime changes are disabled in demo mode")
		return nil, false
	}
	d, err := s.newestJordiDigest(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	if d == nil || d.ID != a.DigestID {
		writeError(w, http.StatusConflict, "that proposal is no longer the newest; reload")
		return nil, false
	}
	return d, true
}

// POST /api/regime/proposal/approve {"digestId"} — applies the PROPOSED state of that digest (the
// client cannot name another state here; "set a different state" is its own call).
func (s *Server) handleRegimeProposalApprove(w http.ResponseWriter, r *http.Request) {
	var a proposalAction
	if !decodeJSON(r, w, &a) {
		return
	}
	d, ok := s.proposalDigestFor(w, r, a)
	if !ok {
		return
	}
	if !d.State.Valid || d.State.String == "" || d.State.String == "unclassified" {
		writeError(w, http.StatusBadRequest, "this digest has no proposed state to approve")
		return
	}
	if err := s.applyProposalState(r.Context(), d, d.State.String, "Approved"); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.logProposalAudit(r.Context(), d, d.State.String, "approved")
	writeJSON(w, http.StatusOK, map[string]any{"applied": d.State.String})
}

// POST /api/regime/proposal/set {"digestId","state"} — Fin picks a different state.
func (s *Server) handleRegimeProposalSet(w http.ResponseWriter, r *http.Request) {
	var a proposalAction
	if !decodeJSON(r, w, &a) {
		return
	}
	d, ok := s.proposalDigestFor(w, r, a)
	if !ok {
		return
	}
	if err := s.applyProposalState(r.Context(), d, a.State, "Set differently"); errors.Is(err, errBadRegime) {
		writeError(w, http.StatusBadRequest, "state must be stable, shifting or defensive")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.logProposalAudit(r.Context(), d, a.State, "set_differently")
	writeJSON(w, http.StatusOK, map[string]any{"applied": a.State})
}

// POST /api/regime/proposal/dismiss {"digestId"} — snooze until the next digest.
func (s *Server) handleRegimeProposalDismiss(w http.ResponseWriter, r *http.Request) {
	var a proposalAction
	if !decodeJSON(r, w, &a) {
		return
	}
	d, ok := s.proposalDigestFor(w, r, a)
	if !ok {
		return
	}
	if err := s.store.SetPreference(r.Context(), prefProposalHandled, strconv.FormatInt(d.ID, 10)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.logProposalAudit(r.Context(), d, "", "dismissed")
	writeJSON(w, http.StatusOK, map[string]any{"dismissed": d.ID})
}

// logProposalAudit leaves a line in the service log; the regime change itself is in regime_history.
func (s *Server) logProposalAudit(ctx context.Context, d *proposalDigest, state, action string) {
	slogProposal(action, d.ID, state)
}

func slogProposal(action string, digestID int64, state string) {
	slog.Info("regime proposal", "action", action, "digest", digestID, "state", state)
}
