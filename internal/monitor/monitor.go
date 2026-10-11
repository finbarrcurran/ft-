// Package monitor is FT's connectivity monitor (SC-47). Every few minutes it
// asks each watched external source "when did you last work?", compares the
// answer with how fresh that source should be, and sends ONE Telegram message
// when a source goes stale, a reminder every 24 h while it stays down, and a
// message when it recovers.
//
// It is deliberately a freshness monitor, not a ping: a free-tier API that
// fails 90% of calls but succeeds a few times a day is healthy enough; a feed
// that has not succeeded for longer than its schedule allows is not.
package monitor

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// ErrNotConfigured is returned by the Telegram notifier when no bot token or
// chat id is set; events are then recorded as undelivered.
var ErrNotConfigured = errors.New("monitor: Telegram not configured")

const (
	tickEvery     = 10 * time.Minute
	bootGrace     = 5 * time.Minute
	reminderEvery = 24 * time.Hour
	statusOK      = "ok"
	statusDown    = "down"
)

// Probe reports a source's last success. found=false means it has never worked.
type Probe func(ctx context.Context) (lastOK time.Time, found bool, detail string, err error)

// Check is one watched source.
type Check struct {
	Source   string        // stable key, e.g. "bargo"
	Label    string        // human name for the alert
	MaxAge   time.Duration // stale when older than this
	Optional bool          // shown in Diagnostics, never alerts (known-dead feeds)
	Probe    Probe
}

// Notifier delivers an alert text.
type Notifier interface {
	Send(ctx context.Context, text string) error
}

// Monitor evaluates checks and persists state in monitor_state/monitor_events.
type Monitor struct {
	DB     *sql.DB
	Checks []Check
	Notify Notifier
	Now    func() time.Time
}

// Result is the outcome of one check.
type Result struct {
	Source   string
	Label    string
	Down     bool
	Optional bool
	LastOK   time.Time
	Found    bool
	MaxAge   time.Duration
	Detail   string
}

func (m *Monitor) now() time.Time {
	if m.Now != nil {
		return m.Now().UTC()
	}
	return time.Now().UTC()
}

// Evaluate runs every check once, without touching state.
func (m *Monitor) Evaluate(ctx context.Context) []Result {
	now := m.now()
	out := make([]Result, 0, len(m.Checks))
	for _, c := range m.Checks {
		r := Result{Source: c.Source, Label: c.Label, Optional: c.Optional, MaxAge: c.MaxAge}
		last, found, detail, err := c.Probe(ctx)
		r.LastOK, r.Found, r.Detail = last, found, detail
		switch {
		case err != nil:
			r.Down = true
			r.Detail = "check error: " + err.Error()
		case !found:
			r.Down = true
			if r.Detail == "" {
				r.Detail = "no successful run on record"
			}
		case now.Sub(last) > c.MaxAge:
			r.Down = true
		}
		out = append(out, r)
	}
	return out
}

// Tick evaluates, updates state and sends alerts for transitions.
func (m *Monitor) Tick(ctx context.Context) {
	now := m.now()
	results := m.Evaluate(ctx)
	var initial []Result
	for _, r := range results {
		var status string
		var since int64
		var lastAlert sql.NullInt64
		err := m.DB.QueryRowContext(ctx,
			`SELECT status, since_ts, last_alert_ts FROM monitor_state WHERE source = ?`, r.Source).
			Scan(&status, &since, &lastAlert)
		first := errors.Is(err, sql.ErrNoRows)
		if err != nil && !first {
			slog.Warn("monitor: read state", "source", r.Source, "err", err)
			continue
		}
		cur := statusOK
		if r.Down {
			cur = statusDown
		}
		lastOK := sql.NullInt64{}
		if r.Found {
			lastOK = sql.NullInt64{Int64: r.LastOK.Unix(), Valid: true}
		}
		switch {
		case first:
			sinceTS := now.Unix()
			if r.Down && r.Found {
				sinceTS = r.LastOK.Add(r.MaxAge).Unix()
			}
			la := sql.NullInt64{}
			if r.Down && !r.Optional {
				initial = append(initial, r)
				la = sql.NullInt64{Int64: now.Unix(), Valid: true}
			}
			m.upsert(ctx, r, cur, sinceTS, lastOK, la)
		case status == statusOK && r.Down:
			la := sql.NullInt64{}
			if !r.Optional {
				text := fmt.Sprintf("🔴 FT connectivity: %s looks down\n%s", r.Label, describe(r, now))
				m.alert(ctx, r.Source, "down", text, now)
				la = sql.NullInt64{Int64: now.Unix(), Valid: true}
			}
			m.upsert(ctx, r, statusDown, now.Unix(), lastOK, la)
		case status == statusDown && r.Down:
			la := lastAlert
			if !r.Optional && (!lastAlert.Valid || now.Sub(time.Unix(lastAlert.Int64, 0)) >= reminderEvery) {
				text := fmt.Sprintf("🟠 FT connectivity: %s is STILL down (since %s)\n%s",
					r.Label, time.Unix(since, 0).UTC().Format("2006-01-02 15:04 UTC"), describe(r, now))
				m.alert(ctx, r.Source, "still_down", text, now)
				la = sql.NullInt64{Int64: now.Unix(), Valid: true}
			}
			m.upsert(ctx, r, statusDown, since, lastOK, la)
		case status == statusDown && !r.Down:
			if !r.Optional && lastAlert.Valid {
				text := fmt.Sprintf("🟢 FT connectivity: %s is back (down for %s)", r.Label,
					humanDur(now.Sub(time.Unix(since, 0))))
				m.alert(ctx, r.Source, "recovered", text, now)
			}
			m.upsert(ctx, r, statusOK, now.Unix(), lastOK, sql.NullInt64{})
		default: // ok → ok
			m.upsert(ctx, r, statusOK, since, lastOK, sql.NullInt64{})
		}
	}
	if len(initial) > 0 {
		sort.Slice(initial, func(i, j int) bool { return initial[i].Source < initial[j].Source })
		var b strings.Builder
		fmt.Fprintf(&b, "🛰 FT connectivity monitor is on. %d source(s) already look stale:\n", len(initial))
		for _, r := range initial {
			fmt.Fprintf(&b, "• %s — %s\n", r.Label, describe(r, now))
		}
		m.alert(ctx, "_monitor", "down", strings.TrimSpace(b.String()), now)
	}
}

func (m *Monitor) upsert(ctx context.Context, r Result, status string, since int64, lastOK, lastAlert sql.NullInt64) {
	_, err := m.DB.ExecContext(ctx, `
		INSERT INTO monitor_state (source, status, since_ts, last_checked_ts, last_ok_ts, detail, last_alert_ts)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(source) DO UPDATE SET
		  status = excluded.status,
		  since_ts = excluded.since_ts,
		  last_checked_ts = excluded.last_checked_ts,
		  last_ok_ts = excluded.last_ok_ts,
		  detail = excluded.detail,
		  last_alert_ts = COALESCE(excluded.last_alert_ts, CASE WHEN excluded.status = 'ok' THEN NULL ELSE monitor_state.last_alert_ts END)`,
		r.Source, status, since, m.now().Unix(), lastOK, r.Detail, lastAlert)
	if err != nil {
		slog.Warn("monitor: write state", "source", r.Source, "err", err)
	}
}

func (m *Monitor) alert(ctx context.Context, source, kind, text string, now time.Time) {
	delivered := 0
	if m.Notify != nil {
		if err := m.Notify.Send(ctx, text); err != nil {
			slog.Warn("monitor: alert not delivered", "source", source, "kind", kind, "err", err)
		} else {
			delivered = 1
		}
	}
	if _, err := m.DB.ExecContext(ctx,
		`INSERT INTO monitor_events (ts, source, kind, detail, delivered) VALUES (?, ?, ?, ?, ?)`,
		now.Unix(), source, kind, text, delivered); err != nil {
		slog.Warn("monitor: write event", "err", err)
	}
	slog.Warn("monitor: "+kind, "source", source, "delivered", delivered)
}

func describe(r Result, now time.Time) string {
	if !r.Found {
		return r.Detail
	}
	s := fmt.Sprintf("last success %s ago (allowed %s)", humanDur(now.Sub(r.LastOK)), humanDur(r.MaxAge))
	if r.Detail != "" {
		s += " — " + r.Detail
	}
	return s
}

func humanDur(d time.Duration) string {
	if d < time.Minute {
		return "<1m"
	}
	if d < 2*time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 72*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// Run ticks until ctx ends. Waits bootGrace first so refresh jobs have run.
func (m *Monitor) Run(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(bootGrace):
	}
	for {
		m.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(tickEvery):
		}
	}
}

// Row is one line of the Diagnostics view.
type Row struct {
	Source     string `json:"source"`
	Label      string `json:"label"`
	Status     string `json:"status"` // ok | down | optional-down
	LastOKUnix *int64 `json:"lastOkUnix,omitempty"`
	AgeSec     *int64 `json:"ageSec,omitempty"`
	MaxAgeSec  int64  `json:"maxAgeSec"`
	Detail     string `json:"detail,omitempty"`
	Optional   bool   `json:"optional,omitempty"`
}

// Snapshot evaluates live (no state change) for the Diagnostics view.
func (m *Monitor) Snapshot(ctx context.Context) []Row {
	now := m.now()
	rs := m.Evaluate(ctx)
	out := make([]Row, 0, len(rs))
	for _, r := range rs {
		row := Row{Source: r.Source, Label: r.Label, MaxAgeSec: int64(r.MaxAge.Seconds()), Detail: r.Detail, Optional: r.Optional}
		row.Status = statusOK
		if r.Down {
			row.Status = statusDown
			if r.Optional {
				row.Status = "optional-down"
			}
		}
		if r.Found {
			u := r.LastOK.Unix()
			a := int64(now.Sub(r.LastOK).Seconds())
			row.LastOKUnix, row.AgeSec = &u, &a
		}
		out = append(out, row)
	}
	return out
}

// Telegram sends alerts straight to Fin's chat. Uses plain text on purpose (no
// parse_mode) so error strings need no escaping.
type Telegram struct {
	HTTP *http.Client
}

// Send posts one message; ErrNotConfigured when the env is missing.
func (t *Telegram) Send(ctx context.Context, text string) error {
	token := os.Getenv("FT_TELEGRAM_BOT_TOKEN")
	chat := os.Getenv("FT_TELEGRAM_CHAT_ID")
	if token == "" || chat == "" {
		return ErrNotConfigured
	}
	c := t.HTTP
	if c == nil {
		c = &http.Client{Timeout: 10 * time.Second}
	}
	body, _ := json.Marshal(map[string]any{
		"chat_id": chat, "text": text, "disable_web_page_preview": true,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.telegram.org/bot"+token+"/sendMessage", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		// Never include the URL (it carries the token) in the error.
		return errors.New("telegram request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telegram http %d", resp.StatusCode)
	}
	return nil
}
