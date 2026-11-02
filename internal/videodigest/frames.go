package videodigest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ft/internal/llm"
)

// SC-54 — what a vision model reads off the chart frames of a digest. Frames stay on Jarvis (local and
// restic-backed); only public videos are read; every call goes through the governor as feature
// "video_frames" with its own sub-caps ($0.05 per video, $0.25 per day). The values are display-only
// ("from chart, unverified") and never feed a signal. A re-read adds rows; nothing is overwritten.

const (
	FramesFeatureID = "video_frames"
	FramesModel     = "claude-haiku-5-5"
	framesPerCall   = 4
	framesMaxOut    = 4000
)

const framesSystem = "You read chart screenshots taken from a YouTube video by a market commentator, for personal research. " +
	"Each image is preceded by its label (frame time in seconds). For every frame that shows a chart, table or indicator, " +
	"report what is visibly printed: the series or indicator name, and the few values or labels you can actually READ in the image " +
	"(a printed number, a level label, a legend entry, the latest value). Never list axis tick labels and never list more than six values per frame. " +
	"Do not estimate values from line positions and do not use outside knowledge; if a value is not legible say so. " +
	"Skip frames that are only a talking head or contain nothing readable.\n" +
	`Return ONLY a JSON array: [ { "t_sec": integer, "series_or_indicator": string, "visible_values": string|null, "confidence": "high"|"medium"|"low" } ]`

// FrameReadOptions controls ReadFrames.
type FrameReadOptions struct {
	Force bool // read again even if a read pass is already stored (adds rows)
	// AssumePublic skips the availability check for packages built before SC-54 (their meta.json has no
	// availability). Only the CLI sets it, by hand, for a video the operator knows is public.
	AssumePublic bool
	DryRun       bool
	Max          int // frames, 0 = all
}

// FrameReadResult summarises one pass.
type FrameReadResult struct {
	VideoID string  `json:"videoId"`
	Status  string  `json:"status"` // done | partial | skipped | failed
	Reason  string  `json:"reason,omitempty"`
	Frames  int     `json:"frames"`
	Calls   int     `json:"calls"`
	Reads   int     `json:"reads"`
	CostUSD float64 `json:"costUsd"`
	Preview []FrameReadView
}

// FrameReadView is one stored (or dry-run) read.
type FrameReadView struct {
	ID         int64  `json:"id"`
	FrameID    int64  `json:"frameId"`
	TsSec      int    `json:"tsSec"`
	Model      string `json:"model"`
	ReadAt     int64  `json:"readAt"`
	Series     string `json:"series"`
	Values     string `json:"values"`
	Confidence string `json:"confidence"`
}

// FrameRun is how a read pass ended, as shown on the card.
type FrameRun struct {
	Model   string  `json:"model"`
	RunAt   int64   `json:"runAt"`
	Status  string  `json:"status"`
	Reason  string  `json:"reason,omitempty"`
	Frames  int     `json:"frames"`
	Calls   int     `json:"calls"`
	Reads   int     `json:"reads"`
	CostUSD float64 `json:"costUsd"`
}

// FramesView is what GET /api/video-digest/{id}/frames returns.
type FramesView struct {
	DigestID int64           `json:"digestId"`
	Run      *FrameRun       `json:"run"`
	Reads    []FrameReadView `json:"reads"`
	Frames   int             `json:"frames"` // frames stored for the digest
}

type frameRow struct {
	ID   int64
	Path string
	Ts   int
}

type rawRead struct {
	T          float64 `json:"t_sec"`
	Series     string  `json:"series_or_indicator"`
	Values     any     `json:"visible_values"`
	Confidence string  `json:"confidence"`
}

// parseFrameReads reads the model's JSON array. A reply cut off at the output cap is salvaged: every
// complete object before the cut is kept.
func parseFrameReads(raw string) []rawRead {
	i := strings.Index(raw, "[")
	if i < 0 {
		return nil
	}
	var all []rawRead
	if j := strings.LastIndex(raw, "]"); j > i {
		if err := json.Unmarshal([]byte(raw[i:j+1]), &all); err == nil {
			return all
		}
	}
	// salvage: walk the objects by brace depth, skipping braces inside strings
	depth, start, inStr, esc := 0, -1, false, false
	for k := i + 1; k < len(raw); k++ {
		c := raw[k]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			if depth == 0 {
				start = k
			}
			depth++
		case '}':
			if depth > 0 {
				depth--
				if depth == 0 && start >= 0 {
					var r rawRead
					if json.Unmarshal([]byte(raw[start:k+1]), &r) == nil {
						all = append(all, r)
					}
					start = -1
				}
			}
		}
	}
	return all
}

func valuesText(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	default:
		b, _ := json.Marshal(x)
		return string(b)
	}
}

// packageDirFor finds the package directory of a stored digest (<root>/<source>/<date>_<id>).
func (s *Service) packageDirFor(source, published, videoID string) string {
	return filepath.Join(s.Root, source, published+"_"+videoID)
}

// ReadFrames reads the chart frames of a stored digest with the vision model.
func (s *Service) ReadFrames(ctx context.Context, videoID string, opt FrameReadOptions) FrameReadResult {
	res := FrameReadResult{VideoID: videoID, Status: "failed"}
	fail := func(err error) FrameReadResult { res.Reason = err.Error(); return res }
	if s.LLM == nil {
		return fail(errors.New("llm service not configured"))
	}
	var digestID int64
	var source, published string
	err := s.DB.QueryRowContext(ctx, `SELECT id, source, published_at FROM video_digests WHERE video_id = ?`, videoID).Scan(&digestID, &source, &published)
	if errors.Is(err, sql.ErrNoRows) {
		return fail(fmt.Errorf("no digest for %s", videoID))
	}
	if err != nil {
		return fail(err)
	}
	if !opt.Force {
		var n int
		_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM video_frame_runs WHERE digest_id = ? AND status IN ('done','skipped')`, digestID).Scan(&n)
		if n > 0 {
			res.Status, res.Reason = "skipped", "already read"
			return res
		}
	}
	skip := func(reason string) FrameReadResult {
		res.Status, res.Reason = "skipped", reason
		if !opt.DryRun {
			s.storeFrameRun(ctx, digestID, res)
		}
		return res
	}
	// public videos only: the package says what YouTube reported
	p, perr := readPackage(s.packageDirFor(source, published, videoID))
	avail := ""
	if perr == nil {
		avail = strings.ToLower(strings.TrimSpace(p.Meta.Availability))
	}
	if avail != "public" && !opt.AssumePublic {
		if avail == "" {
			return skip("frames skipped: availability not recorded (package built before the public-only check)")
		}
		return skip("frames skipped: video is not public (" + avail + ")")
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, file_path, COALESCE(ts_sec,0) FROM video_frames WHERE digest_id = ? ORDER BY ts_sec`, digestID)
	if err != nil {
		return fail(err)
	}
	var frames []frameRow
	for rows.Next() {
		var f frameRow
		if err := rows.Scan(&f.ID, &f.Path, &f.Ts); err != nil {
			rows.Close()
			return fail(err)
		}
		frames = append(frames, f)
	}
	rows.Close()
	if opt.Max > 0 && len(frames) > opt.Max {
		frames = frames[:opt.Max]
	}
	if len(frames) == 0 {
		return skip("no frames stored")
	}

	published = strings.TrimSpace(published)
	stopped := ""
	for lo := 0; lo < len(frames); lo += framesPerCall {
		hi := lo + framesPerCall
		if hi > len(frames) {
			hi = len(frames)
		}
		batch := frames[lo:hi]
		var imgs []llm.Image
		var labels []string
		okFrames := map[int]frameRow{}
		for _, f := range batch {
			if !strings.HasPrefix(filepath.Clean(f.Path), filepath.Clean(s.Root)+string(filepath.Separator)) {
				continue // never read outside the package root
			}
			b, err := os.ReadFile(f.Path)
			if err != nil {
				slog.Warn("videodigest: frame unreadable", "path", f.Path, "err", err)
				continue
			}
			imgs = append(imgs, llm.Image{MediaType: "image/jpeg", Data: b})
			labels = append(labels, fmt.Sprintf("frame_%ds (video time %d s)", f.Ts, f.Ts))
			okFrames[f.Ts] = f
		}
		if len(imgs) == 0 {
			continue
		}
		cr, err := s.LLM.Call(ctx, llm.CallRequest{
			FeatureID:       FramesFeatureID,
			FeatureContext:  fmt.Sprintf("%s:%s %s frames %d-%d/%d", source, videoID, published, lo+1, hi, len(frames)),
			Model:           FramesModel,
			AllowUpgrade:    true,
			ThinkingOff:     true,
			SystemPrompt:    framesSystem,
			UserPrompt:      "Frames, in order:\n" + strings.Join(labels, "\n"),
			Images:          imgs,
			MaxOutputTokens: framesMaxOut,
		})
		res.Frames += len(imgs)
		if err != nil {
			if errors.Is(err, llm.ErrFeatureCap) || errors.Is(err, llm.ErrBudgetBlocked) || errors.Is(err, llm.ErrGloballyPaused) {
				stopped = "frames skipped: " + err.Error()
				res.Frames -= len(imgs)
				break
			}
			return fail(err)
		}
		res.Calls++
		res.CostUSD += cr.CostUSD
		obs := parseFrameReads(cr.Text)
		perRead := 0.0
		if len(obs) > 0 {
			perRead = cr.CostUSD / float64(len(obs))
		}
		for _, o := range obs {
			ts := int(o.T)
			f, ok := okFrames[ts]
			if !ok { // nearest frame of the batch
				best := 1 << 30
				for t, fr := range okFrames {
					if d := abs(t - ts); d < best {
						best, f = d, fr
					}
				}
			}
			v := FrameReadView{FrameID: f.ID, TsSec: f.Ts, Model: FramesModel, ReadAt: time.Now().Unix(),
				Series: strings.TrimSpace(o.Series), Values: valuesText(o.Values), Confidence: o.Confidence}
			if v.Series == "" && v.Values == "" {
				continue
			}
			if !opt.DryRun {
				r, err := s.DB.ExecContext(ctx, `INSERT INTO video_frame_reads (digest_id, frame_id, ts_sec, model, read_at, series, visible_values, confidence, cost_usd)
					VALUES (?,?,?,?,?,?,?,?,?)`, digestID, nullID(f.ID), v.TsSec, v.Model, v.ReadAt, v.Series, v.Values, v.Confidence, perRead)
				if err != nil {
					return fail(err)
				}
				v.ID, _ = r.LastInsertId()
			}
			res.Reads++
			res.Preview = append(res.Preview, v)
		}
	}
	sort.Slice(res.Preview, func(i, j int) bool { return res.Preview[i].TsSec < res.Preview[j].TsSec })
	res.Status = "done"
	if stopped != "" {
		res.Reason = stopped
		res.Status = "partial"
		if res.Calls == 0 {
			res.Status = "skipped"
		}
	}
	if !opt.DryRun {
		s.storeFrameRun(ctx, digestID, res)
	}
	return res
}

func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

func (s *Service) storeFrameRun(ctx context.Context, digestID int64, r FrameReadResult) {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO video_frame_runs (digest_id, model, run_at, status, reason, frames, calls, reads, cost_usd)
		VALUES (?,?,?,?,?,?,?,?,?)`, digestID, FramesModel, time.Now().Unix(), r.Status, r.Reason, r.Frames, r.Calls, r.Reads, r.CostUSD)
	if err != nil {
		slog.Warn("videodigest: frame run not recorded", "err", err)
	}
}

// Frames returns the latest read pass and every stored read of a digest (newest pass last per frame).
func (s *Service) Frames(ctx context.Context, digestID int64) (*FramesView, error) {
	v := &FramesView{DigestID: digestID, Reads: []FrameReadView{}}
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM video_frames WHERE digest_id = ?`, digestID).Scan(&v.Frames)
	var r FrameRun
	var reason sql.NullString
	err := s.DB.QueryRowContext(ctx, `SELECT model, run_at, status, reason, frames, calls, reads, cost_usd FROM video_frame_runs
		WHERE digest_id = ? ORDER BY id DESC LIMIT 1`, digestID).Scan(&r.Model, &r.RunAt, &r.Status, &reason, &r.Frames, &r.Calls, &r.Reads, &r.CostUSD)
	if err == nil {
		r.Reason = reason.String
		v.Run = &r
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, COALESCE(frame_id,0), ts_sec, model, read_at, COALESCE(series,''), COALESCE(visible_values,''), COALESCE(confidence,'')
		FROM video_frame_reads WHERE digest_id = ? ORDER BY ts_sec, id`, digestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var x FrameReadView
		if err := rows.Scan(&x.ID, &x.FrameID, &x.TsSec, &x.Model, &x.ReadAt, &x.Series, &x.Values, &x.Confidence); err != nil {
			return nil, err
		}
		v.Reads = append(v.Reads, x)
	}
	return v, rows.Err()
}

// FramePath returns the file of a stored frame, checked to sit under the package root.
func (s *Service) FramePath(ctx context.Context, frameID int64) (string, error) {
	var p string
	if err := s.DB.QueryRowContext(ctx, `SELECT file_path FROM video_frames WHERE id = ?`, frameID).Scan(&p); err != nil {
		return "", err
	}
	if !strings.HasPrefix(filepath.Clean(p), filepath.Clean(s.Root)+string(filepath.Separator)) {
		return "", errors.New("frame outside the package root")
	}
	return p, nil
}
