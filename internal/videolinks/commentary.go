package videolinks

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"ft/internal/videoitems"
)

// Row is one video that talked about a ticker, as the per-holding panel shows it (FT-146 PR B). A video that has
// both a v5 item and a digest mention is one row: the item supplies the verbatim passage, the gate class and the
// direction flag; the mention supplies the stance when there is no item.
type Row struct {
	Date          string `json:"date"`
	Source        string `json:"source"` // jordi | cowen
	VideoID       string `json:"videoId"`
	Title         string `json:"title"`
	Link          string `json:"link"`
	TsSec         *int   `json:"tsSec,omitempty"`
	Stance        string `json:"stance,omitempty"`
	Snippet       string `json:"snippet"`
	SnippetKind   string `json:"snippetKind"` // passage (verbatim from the transcript) | note (the digest's own summary)
	Gate          string `json:"gate,omitempty"`
	CitedLoosely  bool   `json:"citedLoosely,omitempty"` // wrong_place or far: the value is in the video but not at the cited second
	Direction     string `json:"direction,omitempty"`
	DirectionView string `json:"directionView,omitempty"`
	Method        string `json:"method"`
}

// Last says when each commentator last mentioned the ticker.
type Last struct {
	Date    string `json:"date"`
	DaysAgo int    `json:"daysAgo"`
	Stance  string `json:"stance,omitempty"`
}

// Commentary is the panel's payload.
type Commentary struct {
	Ticker  string          `json:"ticker"`
	Days    int             `json:"days"`
	Total   int             `json:"total"`  // videos in the window
	Shown   int             `json:"shown"`  // rows returned
	Hidden  int             `json:"hidden"` // items for this ticker held back by the grounding gate (invented, name_mapping)
	Last    map[string]Last `json:"last"`
	Rows    []Row           `json:"rows"`
	Context string          `json:"context"` // D6, stated on the panel itself
}

// ContextNote is shown with the panel: it is reading material, never an input to scoring or alerts (D6).
const ContextNote = "Context only: what the commentators said. It is not a score, a signal or advice."

type linkRef struct {
	kind                string
	id                  int64
	method, gate        string
	source, date, video string
	title               string
}

// ForTicker returns what Jordi and Cowen said about a ticker: the newest first, one row per video, within days,
// at most limit rows. now is a parameter so the recency is testable.
func ForTicker(ctx context.Context, db *sql.DB, ticker string, days, limit int, now time.Time) (*Commentary, error) {
	t := strings.ToUpper(strings.TrimSpace(ticker))
	if days <= 0 {
		days = 90
	}
	if days > 730 {
		days = 730
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	out := &Commentary{Ticker: t, Days: days, Last: map[string]Last{}, Rows: []Row{}, Context: ContextNote}
	rows, err := db.QueryContext(ctx, `SELECT l.source_kind, l.source_id, l.method, COALESCE(l.gate_class,''), d.source, d.published_at, d.video_id, COALESCE(d.title,'')
		FROM video_links l JOIN video_digests d ON d.id = l.digest_id
		WHERE l.ticker = ? ORDER BY d.published_at DESC, d.id DESC, l.source_kind DESC, l.source_id`, t)
	if err != nil {
		return nil, err
	}
	var all []linkRef
	for rows.Next() {
		var r linkRef
		if err := rows.Scan(&r.kind, &r.id, &r.method, &r.gate, &r.source, &r.date, &r.video, &r.title); err != nil {
			rows.Close()
			return nil, err
		}
		all = append(all, r)
	}
	rows.Close()

	cutoff := now.UTC().AddDate(0, 0, -days).Format("2006-01-02")
	type vid struct {
		item, mention *linkRef
	}
	byVideo := map[string]*vid{}
	var order []string
	lastSeen := map[string]bool{}
	for i := range all {
		r := &all[i]
		if !lastSeen[r.source] { // the newest link of each commentator, whatever the window
			lastSeen[r.source] = true
			d, _ := time.Parse("2006-01-02", r.date)
			out.Last[r.source] = Last{Date: r.date, DaysAgo: int(now.UTC().Sub(d).Hours() / 24)}
		}
		if r.date < cutoff {
			continue
		}
		v := byVideo[r.video]
		if v == nil {
			v = &vid{}
			byVideo[r.video] = v
			order = append(order, r.video)
		}
		if r.kind == "item" && v.item == nil {
			v.item = r
		} else if r.kind == "mention" && v.mention == nil {
			v.mention = r
		}
	}
	out.Total = len(order)
	for _, key := range order {
		if len(out.Rows) >= limit {
			break
		}
		v := byVideo[key]
		base := v.item
		if base == nil {
			base = v.mention
		}
		row := Row{Date: base.date, Source: base.source, VideoID: base.video, Title: base.title, Method: base.method}
		var ts *int
		if v.item != nil {
			var fields string
			var rs, re sql.NullInt64
			var dv, dview sql.NullString
			if err := db.QueryRowContext(ctx, `SELECT fields_json, ref_start, ref_end, direction_verdict, direction_view FROM video_items WHERE id = ?`, v.item.id).
				Scan(&fields, &rs, &re, &dv, &dview); err == nil {
				var f struct {
					Stance string `json:"stance"`
				}
				_ = json.Unmarshal([]byte(fields), &f)
				row.Stance = f.Stance
				row.Gate = v.item.gate
				row.CitedLoosely = v.item.gate == videoitems.ClassWrongPlace || v.item.gate == videoitems.ClassFar
				row.Direction, row.DirectionView = dv.String, dview.String
				if rs.Valid && re.Valid {
					a := int(rs.Int64)
					ts = &a
					row.Snippet = passage(ctx, db, base.video, a, int(re.Int64))
					row.SnippetKind = "passage"
				}
			}
		}
		if v.mention != nil {
			var note, stance string
			var mts sql.NullInt64
			if err := db.QueryRowContext(ctx, `SELECT COALESCE(context,''), COALESCE(stance,''), ts_sec FROM video_mentions WHERE id = ?`, v.mention.id).Scan(&note, &stance, &mts); err == nil {
				if row.Stance == "" {
					row.Stance = stance
				}
				if row.Snippet == "" {
					row.Snippet, row.SnippetKind = note, "note"
				}
				if ts == nil && mts.Valid {
					a := int(mts.Int64)
					ts = &a
				}
			}
		}
		row.TsSec = ts
		sec := 0
		if ts != nil {
			sec = *ts
		}
		row.Link = fmt.Sprintf("https://youtu.be/%s?t=%d", row.VideoID, sec)
		out.Rows = append(out.Rows, row)
	}
	out.Shown = len(out.Rows)
	// the stance of each commentator's newest mention, from the rows (or, if it is older than the window, left empty)
	for src, l := range out.Last {
		for _, r := range out.Rows {
			if r.Source == src && r.Date == l.Date {
				l.Stance = r.Stance
				out.Last[src] = l
				break
			}
		}
	}
	// items for this ticker the grounding gate holds back (never linked): counted so the hiding is visible
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM video_items i JOIN video_digests d ON d.id = i.digest_id
		WHERE i.block = 'tickers_named' AND i.gate_class IN ('invented','name_mapping') AND d.published_at >= ?
		AND UPPER(COALESCE(json_extract(i.resolution_json, '$.ticker'),'')) = ?`, cutoff, t).Scan(&out.Hidden)
	return out, nil
}

// passage returns up to 60 words of the transcript from the cited start to just after the cited end: verbatim.
func passage(ctx context.Context, db *sql.DB, videoID string, start, end int) string {
	var compact string
	if err := db.QueryRowContext(ctx, `SELECT text FROM video_transcripts WHERE video_id = ?`, videoID).Scan(&compact); err != nil {
		return ""
	}
	var b []string
	for _, w := range videoitems.WordsFromCompact(compact) {
		if w.T >= start && w.T <= end+10 {
			b = append(b, w.W)
			if len(b) == 60 {
				break
			}
		}
	}
	return strings.Join(b, " ")
}
