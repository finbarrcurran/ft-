package videodigest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"ft/internal/llm"
	"ft/internal/market"
	"ft/internal/videoitems"
)

// SC-53 — the v5 item extraction of a digest, the grounding gate, the ticker resolver and the
// direction flag. A separate call from the digest's own extraction (which stays on Haiku 4.5 with its
// vote and is the only source of the card's regime): Haiku 5.5, thinking off, no regime block. Every
// call goes through the governor (features "video_items" and "video_direction"), so each one is in
// the ledger and under the caps.

const (
	// ItemsFeatureID and DirectionFeatureID tag the governor calls.
	ItemsFeatureID     = "video_items"
	DirectionFeatureID = "video_direction"
	// ItemsModel is the model of both calls (Fin 2026-11-01).
	ItemsModel = "claude-haiku-5-5"

	itemsMaxOut      = 8000
	directionMaxOut  = 4000
	directionBatch   = 40
	chunkOverSec     = 3600 // a video longer than this is read in chunks
	chunkSec         = 1800
	chunkOverlapSec  = 60
	resolveTimeout   = 8 * time.Second
	maxResolvesPerRun = 40
)

// ItemsOptions controls ExtractItems.
type ItemsOptions struct {
	Force  bool // replace an existing run
	DryRun bool // do everything (the calls are governed and in the ledger) but store nothing
}

// ItemsResult summarises one extraction.
type ItemsResult struct {
	VideoID       string         `json:"videoId"`
	DigestID      int64          `json:"digestId"`
	Status        string         `json:"status"` // extracted | skipped | failed
	Error         string         `json:"error,omitempty"`
	Items         int            `json:"items"`
	Counts        map[string]int `json:"counts,omitempty"`
	Chunks        int            `json:"chunks"`
	InputTokens   int            `json:"inputTokens"`
	OutputTokens  int            `json:"outputTokens"`
	CostUSD       float64        `json:"costUsd"`
	DirectionCost float64        `json:"directionCostUsd"`
	Unresolved    int            `json:"unresolved"`
	Contradicts   int            `json:"contradicts"`
	Stored        bool           `json:"stored"`
	Preview       []ItemView     `json:"-"`
}

// ItemView is one stored item as the tab shows it.
type ItemView struct {
	ID              int64                  `json:"id"`
	Block           string                 `json:"block"`
	Fields          map[string]any         `json:"fields"`
	RefStart        *int                   `json:"refStart,omitempty"`
	RefEnd          *int                   `json:"refEnd,omitempty"`
	Class           string                 `json:"class"`
	Issues          []videoitems.Issue     `json:"issues,omitempty"`
	ChartUnverified []string               `json:"chartUnverified,omitempty"`
	SignalOK        bool                   `json:"signalOk"`
	Resolution      *videoitems.Resolution `json:"resolution,omitempty"`
	Direction       *DirectionView         `json:"direction,omitempty"`
	Passage         string                 `json:"passage,omitempty"`
}

// DirectionView is the direction flag of an item.
type DirectionView struct {
	Verdict string `json:"verdict"`
	View    string `json:"speakerView"`
	Reason  string `json:"reason"`
}

// ItemsRun is the run summary.
type ItemsRun struct {
	SchemaVersion int            `json:"schemaVersion"`
	Model         string         `json:"model"`
	Chunks        int            `json:"chunks"`
	CostUSD       float64        `json:"costUsd"`
	DirectionCost float64        `json:"directionCostUsd"`
	Items         int            `json:"items"`
	Counts        map[string]int `json:"counts"`
	Tone          string         `json:"tone,omitempty"`
	Status        string         `json:"status"`
	CreatedAt     int64          `json:"createdAt"`
}

// ItemsView is what GET /api/video-digest/{id}/items returns.
type ItemsView struct {
	DigestID int64      `json:"digestId"`
	Run      *ItemsRun  `json:"run"`
	Items    []ItemView `json:"items"`
}

func yahooLookup(ctx context.Context, q string) ([]videoitems.Candidate, error) {
	ctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()
	hits, err := market.SearchSymbols(ctx, q)
	if err != nil {
		return nil, err
	}
	out := make([]videoitems.Candidate, 0, len(hits))
	for _, h := range hits {
		out = append(out, videoitems.Candidate{Symbol: h.Symbol, Name: h.Name, Exchange: h.Exchange})
	}
	return out, nil
}

// ExtractItems runs the v5 extraction for a stored digest.
func (s *Service) ExtractItems(ctx context.Context, videoID string, opt ItemsOptions) ItemsResult {
	res := ItemsResult{VideoID: videoID, Status: "failed"}
	fail := func(err error) ItemsResult { res.Error = err.Error(); return res }
	if s.LLM == nil {
		return fail(errors.New("llm service not configured"))
	}
	var source, title, published string
	var dur sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `SELECT id, source, title, published_at, duration_sec FROM video_digests WHERE video_id = ?`, videoID).
		Scan(&res.DigestID, &source, &title, &published, &dur)
	if errors.Is(err, sql.ErrNoRows) {
		return fail(fmt.Errorf("no digest for %s", videoID))
	}
	if err != nil {
		return fail(err)
	}
	if !opt.Force {
		var n int
		_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM video_item_runs WHERE digest_id = ?`, res.DigestID).Scan(&n)
		if n > 0 {
			res.Status = "skipped"
			return res
		}
	}
	var compact string
	if err := s.DB.QueryRowContext(ctx, `SELECT text FROM video_transcripts WHERE video_id = ?`, videoID).Scan(&compact); err != nil || strings.TrimSpace(compact) == "" {
		return fail(fmt.Errorf("no stored transcript for %s", videoID))
	}
	words := videoitems.WordsFromCompact(compact)
	duration := int(dur.Int64)
	if duration <= 0 && len(words) > 0 {
		duration = words[len(words)-1].T + 15
	}

	extract := func(pieces []string) (*videoitems.Extraction, bool, error) {
		var parts []*videoitems.Extraction
		for i, piece := range pieces {
			resp, err := s.LLM.Call(ctx, llm.CallRequest{
				FeatureID:       ItemsFeatureID,
				FeatureContext:  fmt.Sprintf("%s:%s %s chunk %d/%d", source, videoID, published, i+1, len(pieces)),
				Model:           ItemsModel,
				AllowUpgrade:    true,
				ThinkingOff:     true,
				SystemPrompt:    videoitems.SystemPrompt(source),
				UserPrompt:      fmt.Sprintf("Video: %s (%s, %d minutes)\n\nTRANSCRIPT:\n%s", title, published, duration/60, piece),
				MaxOutputTokens: itemsMaxOut,
			})
			res.CostUSD += resp.CostUSD
			res.InputTokens += resp.InputTokens
			res.OutputTokens += resp.OutputTokens
			if err != nil {
				return nil, false, fmt.Errorf("llm: %w", err)
			}
			if resp.Outcome == "truncated" {
				return nil, true, nil
			}
			ex, err := videoitems.ParseExtraction(resp.Text)
			if err != nil {
				return nil, false, err
			}
			parts = append(parts, ex)
		}
		return videoitems.Merge(parts), false, nil
	}
	pieces := videoitems.Chunk(compact, duration, chunkOverSec, chunkSec, chunkOverlapSec)
	ex, truncated, err := extract(pieces)
	if err == nil && truncated && len(pieces) == 1 { // the output outgrew the cap: read it in 30-minute chunks
		pieces = videoitems.Chunk(compact, duration, 0, chunkSec, chunkOverlapSec)
		ex, truncated, err = extract(pieces)
	}
	if err != nil {
		return fail(err)
	}
	if truncated {
		return fail(fmt.Errorf("the v5 output was cut off at %d tokens", itemsMaxOut))
	}
	res.Chunks = len(pieces)

	// gate
	gate := videoitems.NewGate(videoitems.Default, words)
	verdicts := make([]videoitems.Verdict, len(ex.Items))
	res.Counts = map[string]int{}
	for i, it := range ex.Items {
		verdicts[i] = gate.Check(it)
		res.Counts[verdicts[i].Class]++
	}
	// ticker resolver (names as spoken), at most maxResolvesPerRun searches
	resolver := &videoitems.Resolver{A: videoitems.Default, Lookup: yahooLookup}
	resolutions := make([]*videoitems.Resolution, len(ex.Items))
	searched := 0
	for i, it := range ex.Items {
		if it.Block != "tickers_named" {
			continue
		}
		name, _ := it.Fields["name"].(string)
		if strings.TrimSpace(name) == "" {
			continue
		}
		mt, _ := it.Fields["ticker"].(string)
		rs := &videoitems.Resolver{A: resolver.A}
		if searched < maxResolvesPerRun {
			rs.Lookup = resolver.Lookup
		}
		r := rs.Resolve(ctx, name, mt)
		if r.Source == "search" || len(r.Candidates) > 0 {
			searched++
		}
		resolutions[i] = &r
		if r.Status == "unresolved" {
			res.Unresolved++
		}
	}
	// direction flag
	dir := s.directionFlags(ctx, ex.Items, words, &res)

	res.Items = len(ex.Items)
	for _, d := range dir {
		if d != nil && d.Verdict == "contradicts" {
			res.Contradicts++
		}
	}
	res.Status = "extracted"
	for i, it := range ex.Items { // preview (also used by the CLI dry run)
		res.Preview = append(res.Preview, buildView(0, it, verdicts[i], resolutions[i], dir[i], ""))
	}
	if opt.DryRun {
		return res
	}
	if err := s.storeItems(ctx, res, ex, verdicts, resolutions, dir); err != nil {
		res.Status, res.Error = "failed", err.Error()
		return res
	}
	res.Stored = true
	return res
}

// directionFlags runs the direction call in batches and returns one view (or nil) per item. A failure
// of this optional pass never fails the extraction: it is logged and the flags stay empty.
func (s *Service) directionFlags(ctx context.Context, items []videoitems.Item, words []videoitems.Word, res *ItemsResult) []*DirectionView {
	out := make([]*DirectionView, len(items))
	for lo := 0; lo < len(items); lo += directionBatch {
		hi := lo + directionBatch
		if hi > len(items) {
			hi = len(items)
		}
		prompt, idx := videoitems.DirectionPrompt(items[lo:hi], words)
		if len(idx) == 0 {
			continue
		}
		resp, err := s.LLM.Call(ctx, llm.CallRequest{
			FeatureID:       DirectionFeatureID,
			FeatureContext:  res.VideoID,
			Model:           ItemsModel,
			AllowUpgrade:    true,
			ThinkingOff:     true,
			SystemPrompt:    videoitems.DirectionSystem,
			UserPrompt:      prompt,
			MaxOutputTokens: directionMaxOut,
		})
		res.DirectionCost += resp.CostUSD
		if err != nil {
			slog.Warn("videodigest: direction flag skipped", "video", res.VideoID, "err", err)
			return out
		}
		for _, v := range videoitems.ParseDirection(resp.Text) {
			if v.N >= 1 && v.N <= len(idx) {
				out[lo+idx[v.N-1]] = &DirectionView{Verdict: v.Verdict, View: v.View, Reason: v.Reason}
			}
		}
	}
	return out
}

func buildView(id int64, it videoitems.Item, v videoitems.Verdict, r *videoitems.Resolution, d *DirectionView, passage string) ItemView {
	iv := ItemView{ID: id, Block: it.Block, Fields: it.Fields, Class: v.Class, Issues: v.Issues, ChartUnverified: v.ChartUnverified,
		SignalOK: v.SignalOK, Resolution: r, Direction: d, Passage: passage}
	if it.Ref != nil {
		s, e := it.Ref.Start, it.Ref.End
		iv.RefStart, iv.RefEnd = &s, &e
	}
	return iv
}

func (s *Service) storeItems(ctx context.Context, res ItemsResult, ex *videoitems.Extraction, verdicts []videoitems.Verdict,
	resolutions []*videoitems.Resolution, dir []*DirectionView) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM video_item_runs WHERE digest_id = ?`, res.DigestID); err != nil {
		return err
	}
	counts, _ := json.Marshal(res.Counts)
	r, err := tx.ExecContext(ctx, `INSERT INTO video_item_runs (digest_id, schema_version, model, chunks, input_tokens, output_tokens,
		cost_usd, direction_cost_usd, items_total, counts_json, tone, status, created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		res.DigestID, videoitems.SchemaVersion, ItemsModel, res.Chunks, res.InputTokens, res.OutputTokens, res.CostUSD, res.DirectionCost,
		res.Items, string(counts), ex.Tone, "ok", time.Now().Unix())
	if err != nil {
		return err
	}
	runID, _ := r.LastInsertId()
	ord := map[string]int{}
	for i, it := range ex.Items {
		fb, _ := json.Marshal(it.Fields)
		var issues, chart, reso any
		if len(verdicts[i].Issues) > 0 {
			b, _ := json.Marshal(verdicts[i].Issues)
			issues = string(b)
		}
		if len(verdicts[i].ChartUnverified) > 0 {
			b, _ := json.Marshal(verdicts[i].ChartUnverified)
			chart = string(b)
		}
		if resolutions[i] != nil {
			b, _ := json.Marshal(resolutions[i])
			reso = string(b)
		}
		var rs, re, dv, dview, dreason any
		if it.Ref != nil {
			rs, re = it.Ref.Start, it.Ref.End
		}
		if dir[i] != nil {
			dv, dview, dreason = dir[i].Verdict, dir[i].View, dir[i].Reason
		}
		ord[it.Block]++
		if _, err := tx.ExecContext(ctx, `INSERT INTO video_items (run_id, digest_id, block, ord, fields_json, ref_start, ref_end, gate_class,
			gate_issues_json, chart_unverified_json, signal_ok, resolution_json, direction_verdict, direction_view, direction_reason)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, runID, res.DigestID, it.Block, ord[it.Block], string(fb), rs, re, verdicts[i].Class,
			issues, chart, boolInt(verdicts[i].SignalOK), reso, dv, dview, dreason); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Items returns the stored items of a digest with the wide passage of each cited span.
func (s *Service) Items(ctx context.Context, digestID int64) (*ItemsView, error) {
	view := &ItemsView{DigestID: digestID, Items: []ItemView{}}
	var run ItemsRun
	var counts sql.NullString
	var tone sql.NullString
	var runID int64
	err := s.DB.QueryRowContext(ctx, `SELECT id, schema_version, model, chunks, cost_usd, direction_cost_usd, items_total, counts_json, tone, status, created_at
		FROM video_item_runs WHERE digest_id = ?`, digestID).Scan(&runID, &run.SchemaVersion, &run.Model, &run.Chunks, &run.CostUSD,
		&run.DirectionCost, &run.Items, &counts, &tone, &run.Status, &run.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return view, nil
	}
	if err != nil {
		return nil, err
	}
	run.Tone = tone.String
	_ = json.Unmarshal([]byte(counts.String), &run.Counts)
	view.Run = &run
	var compact string
	_ = s.DB.QueryRowContext(ctx, `SELECT t.text FROM video_transcripts t JOIN video_digests d ON d.video_id = t.video_id WHERE d.id = ?`, digestID).Scan(&compact)
	words := videoitems.WordsFromCompact(compact)
	rows, err := s.DB.QueryContext(ctx, `SELECT id, block, fields_json, ref_start, ref_end, gate_class, gate_issues_json, chart_unverified_json, signal_ok,
		resolution_json, direction_verdict, direction_view, direction_reason FROM video_items WHERE run_id = ? ORDER BY block, ord`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var iv ItemView
		var fields string
		var rs, re sql.NullInt64
		var issues, chart, reso, dv, dview, dreason sql.NullString
		var sig int
		if err := rows.Scan(&iv.ID, &iv.Block, &fields, &rs, &re, &iv.Class, &issues, &chart, &sig, &reso, &dv, &dview, &dreason); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(fields), &iv.Fields)
		iv.SignalOK = sig == 1
		if issues.Valid {
			_ = json.Unmarshal([]byte(issues.String), &iv.Issues)
		}
		if chart.Valid {
			_ = json.Unmarshal([]byte(chart.String), &iv.ChartUnverified)
		}
		if reso.Valid {
			var r videoitems.Resolution
			if json.Unmarshal([]byte(reso.String), &r) == nil {
				iv.Resolution = &r
			}
		}
		if dv.Valid {
			iv.Direction = &DirectionView{Verdict: dv.String, View: dview.String, Reason: dreason.String}
		}
		if rs.Valid && re.Valid {
			a, b := int(rs.Int64), int(re.Int64)
			iv.RefStart, iv.RefEnd = &a, &b
			p := strings.Fields(videoitems.Window(words, a, b))
			if len(p) > 150 {
				p = p[:150]
			}
			iv.Passage = strings.Join(p, " ")
		}
		view.Items = append(view.Items, iv)
	}
	return view, rows.Err()
}

// fillItemRuns attaches the run summary to each digest that has one (one query for the whole list).
func (s *Service) fillItemRuns(ctx context.Context, ds []Digest) {
	rows, err := s.DB.QueryContext(ctx, `SELECT digest_id, schema_version, model, chunks, cost_usd, direction_cost_usd, items_total, counts_json, tone, status, created_at FROM video_item_runs`)
	if err != nil {
		return
	}
	defer rows.Close()
	byID := map[int64]*ItemsRun{}
	for rows.Next() {
		var id int64
		var r ItemsRun
		var counts, tone sql.NullString
		if rows.Scan(&id, &r.SchemaVersion, &r.Model, &r.Chunks, &r.CostUSD, &r.DirectionCost, &r.Items, &counts, &tone, &r.Status, &r.CreatedAt) != nil {
			continue
		}
		r.Tone = tone.String
		_ = json.Unmarshal([]byte(counts.String), &r.Counts)
		byID[id] = &r
	}
	for i := range ds {
		ds[i].Items = byID[ds[i].ID]
	}
}
