package videodigest

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// SC-52 P2 — the full transcript store. Rows live in video_transcripts (migration 0057) with an
// FTS5 index; they are written from the on-disk packages (captions), never fetched here, and
// no LLM is involved.

// TranscriptRow is the data stored for one video.
type TranscriptRow struct {
	VideoID, Source, Channel, Title, URL, PublishedAt, Method string
	DurationSec                                               int
	Text                                                      string
}

func shaHex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// StoreTranscript inserts or updates one transcript. It reports whether anything was written
// (an unchanged re-sync writes nothing).
func (s *Service) StoreTranscript(ctx context.Context, r TranscriptRow) (bool, error) {
	if r.VideoID == "" || strings.TrimSpace(r.Text) == "" {
		return false, fmt.Errorf("transcript needs a video id and text")
	}
	if r.Method == "" {
		r.Method = "captions"
	}
	sha := shaHex(r.Text)
	var oldSha string
	err := s.DB.QueryRowContext(ctx, `SELECT text_sha FROM video_transcripts WHERE video_id = ?`, r.VideoID).Scan(&oldSha)
	switch {
	case err == sql.ErrNoRows:
	case err != nil:
		return false, err
	case oldSha == sha:
		return false, nil
	}
	_, err = s.DB.ExecContext(ctx, `
		INSERT INTO video_transcripts (video_id, source, channel, title, url, published_at, duration_sec, method, text, text_sha, fetched_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(video_id) DO UPDATE SET source = excluded.source, channel = excluded.channel, title = excluded.title,
		  url = excluded.url, published_at = excluded.published_at, duration_sec = excluded.duration_sec,
		  method = excluded.method, text = excluded.text, text_sha = excluded.text_sha, fetched_at = excluded.fetched_at`,
		r.VideoID, r.Source, r.Channel, r.Title, r.URL, r.PublishedAt, r.DurationSec, r.Method, r.Text, sha, time.Now().Unix())
	return err == nil, err
}

func rowFromPackage(p pkg, compact string) TranscriptRow {
	m := p.Meta
	method := m.TranscriptMethod
	if method == "" {
		method = "captions"
	}
	return TranscriptRow{VideoID: m.VideoID, Source: m.Source, Channel: sources[m.Source], Title: m.Title, URL: m.URL,
		PublishedAt: m.PublishedAt, DurationSec: m.DurationSec, Method: method, Text: compact}
}

// SyncResult counts what a sync did.
type SyncResult struct{ Packages, Written, Unchanged, Failed int }

// SyncTranscripts stores the transcript of every complete package. Idempotent and free.
func (s *Service) SyncTranscripts(ctx context.Context) (SyncResult, error) {
	var out SyncResult
	all, err := s.packages()
	if err != nil {
		return out, err
	}
	for _, p := range all {
		out.Packages++
		vtt, err := os.ReadFile(filepath.Join(p.Dir, "transcript.vtt"))
		if err != nil {
			out.Failed++
			continue
		}
		compact := CompactTranscript(string(vtt))
		if compact == "" {
			out.Failed++
			continue
		}
		wrote, err := s.StoreTranscript(ctx, rowFromPackage(p, compact))
		switch {
		case err != nil:
			out.Failed++
		case wrote:
			out.Written++
		default:
			out.Unchanged++
		}
	}
	return out, nil
}

// TranscriptHit is one search result.
type TranscriptHit struct {
	VideoID     string `json:"videoId"`
	Source      string `json:"source"`
	Channel     string `json:"channel"`
	Title       string `json:"title"`
	PublishedAt string `json:"publishedAt"`
	URL         string `json:"url"`
	TsSec       int    `json:"tsSec"`   // start of the passage (nearest timestamp marker)
	Link        string `json:"link"`    // youtu.be link at that second
	Snippet     string `json:"snippet"` // match highlighted with « »
}

// SearchOptions filter a transcript search.
type SearchOptions struct {
	Source string // "", jordi or cowen
	From   string // published >= (YYYY-MM-DD)
	To     string // published <= (YYYY-MM-DD)
	Limit  int
}

var markerRe = regexp.MustCompile(`\[(\d+)s\]`)

// ftsQuery turns user text into a safe FTS5 expression: a double-quoted phrase is kept as a
// phrase, every other word becomes a quoted term, all ANDed. FTS5 operators never reach MATCH.
func ftsQuery(q string) string {
	var parts []string
	rest := q
	for {
		i := strings.Index(rest, `"`)
		if i < 0 {
			break
		}
		j := strings.Index(rest[i+1:], `"`)
		if j < 0 {
			break
		}
		if ph := strings.TrimSpace(rest[i+1 : i+1+j]); ph != "" {
			parts = append(parts, `"`+strings.ReplaceAll(ph, `"`, ``)+`"`)
		}
		rest = rest[:i] + " " + rest[i+1+j+1:]
	}
	for _, w := range strings.Fields(rest) {
		w = strings.Trim(w, `"'()*^:-`)
		if w != "" {
			parts = append(parts, `"`+strings.ReplaceAll(w, `"`, ``)+`"`)
		}
	}
	return strings.Join(parts, " AND ")
}

// SearchTranscripts returns the best-matching videos for a keyword or phrase, newest first
// among equal ranks. Read-only.
func (s *Service) SearchTranscripts(ctx context.Context, q string, o SearchOptions) ([]TranscriptHit, error) {
	match := ftsQuery(q)
	if match == "" {
		return nil, fmt.Errorf("empty search")
	}
	if o.Limit <= 0 || o.Limit > 50 {
		o.Limit = 10
	}
	args := []any{match}
	where := ""
	if o.Source != "" {
		where += " AND t.source = ?"
		args = append(args, o.Source)
	}
	if o.From != "" {
		where += " AND t.published_at >= ?"
		args = append(args, o.From)
	}
	if o.To != "" {
		where += " AND t.published_at <= ?"
		args = append(args, o.To)
	}
	args = append(args, o.Limit)
	rows, err := s.DB.QueryContext(ctx, `
		SELECT t.video_id, t.source, t.channel, t.title, t.published_at, t.url, t.text,
		       snippet(video_transcripts_fts, 0, '«', '»', ' … ', 28)
		  FROM video_transcripts_fts f JOIN video_transcripts t ON t.id = f.rowid
		 WHERE video_transcripts_fts MATCH ?`+where+`
		 ORDER BY bm25(video_transcripts_fts), t.published_at DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TranscriptHit
	for rows.Next() {
		var h TranscriptHit
		var text, snip string
		if err := rows.Scan(&h.VideoID, &h.Source, &h.Channel, &h.Title, &h.PublishedAt, &h.URL, &text, &snip); err != nil {
			return nil, err
		}
		h.Snippet = strings.Join(strings.Fields(markerRe.ReplaceAllString(snip, " ")), " ")
		h.TsSec = hitTs(text, snip)
		h.Link = fmt.Sprintf("https://youtu.be/%s?t=%d", h.VideoID, h.TsSec)
		out = append(out, h)
	}
	return out, rows.Err()
}

// hitTs finds where the highlighted passage sits in the full text and returns the nearest
// timestamp marker at or before it (0 when none).
func hitTs(text, snippet string) int {
	i := strings.Index(snippet, "«")
	j := strings.Index(snippet, "»")
	if i < 0 || j <= i {
		return 0
	}
	term := strings.TrimSpace(snippet[i+len("«") : j])
	if term == "" {
		return 0
	}
	pos := strings.Index(strings.ToLower(text), strings.ToLower(term))
	if pos < 0 {
		return 0
	}
	ts := 0
	for _, m := range markerRe.FindAllStringSubmatchIndex(text[:pos], -1) {
		ts, _ = strconv.Atoi(text[m[2]:m[3]])
	}
	return ts
}

// TranscriptCounts is the corpus size, for Diagnostics and the CLI.
type TranscriptCounts struct {
	Total    int            `json:"total"`
	BySource map[string]int `json:"bySource"`
	Bytes    int64          `json:"bytes"`
	Oldest   string         `json:"oldest"`
	Newest   string         `json:"newest"`
}

func (s *Service) TranscriptStats(ctx context.Context) (TranscriptCounts, error) {
	out := TranscriptCounts{BySource: map[string]int{}}
	rows, err := s.DB.QueryContext(ctx, `SELECT source, COUNT(*), COALESCE(SUM(LENGTH(text)),0), COALESCE(MIN(published_at),''), COALESCE(MAX(published_at),'') FROM video_transcripts GROUP BY source`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var src, lo, hi string
		var n int
		var b int64
		if err := rows.Scan(&src, &n, &b, &lo, &hi); err != nil {
			return out, err
		}
		out.BySource[src] = n
		out.Total += n
		out.Bytes += b
		if out.Oldest == "" || lo < out.Oldest {
			out.Oldest = lo
		}
		if hi > out.Newest {
			out.Newest = hi
		}
	}
	return out, rows.Err()
}
