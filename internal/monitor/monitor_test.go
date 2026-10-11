package monitor

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ft/internal/store"
)

type fakeNotifier struct {
	msgs []string
	fail bool
}

func (f *fakeNotifier) Send(_ context.Context, text string) error {
	if f.fail {
		return errors.New("boom")
	}
	f.msgs = append(f.msgs, text)
	return nil
}

func newMon(t *testing.T, last *time.Time, now *time.Time, n *fakeNotifier) *Monitor {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "ft.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	return &Monitor{
		DB:  st.DB,
		Now: func() time.Time { return *now },
		Checks: []Check{{
			Source: "feed", Label: "Test feed", MaxAge: 2 * time.Hour,
			Probe: func(context.Context) (time.Time, bool, string, error) { return *last, !last.IsZero(), "", nil },
		}},
		Notify: n,
	}
}

func TestDownReminderRecovery(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	last := now.Add(-30 * time.Minute)
	n := &fakeNotifier{}
	m := newMon(t, &last, &now, n)

	m.Tick(ctx) // healthy, first sight: no message
	if len(n.msgs) != 0 {
		t.Fatalf("healthy start sent %v", n.msgs)
	}

	now = now.Add(3 * time.Hour) // feed silent for 3.5 h > 2 h allowed
	m.Tick(ctx)
	if len(n.msgs) != 1 || !strings.Contains(n.msgs[0], "looks down") {
		t.Fatalf("expected one down alert, got %v", n.msgs)
	}
	now = now.Add(10 * time.Minute)
	m.Tick(ctx) // still down, within 24 h: no repeat
	if len(n.msgs) != 1 {
		t.Fatalf("duplicate alert: %v", n.msgs)
	}
	now = now.Add(25 * time.Hour)
	m.Tick(ctx) // reminder after 24 h
	if len(n.msgs) != 2 || !strings.Contains(n.msgs[1], "STILL down") {
		t.Fatalf("expected reminder, got %v", n.msgs)
	}
	last = now // feed recovers
	m.Tick(ctx)
	if len(n.msgs) != 3 || !strings.Contains(n.msgs[2], "is back") {
		t.Fatalf("expected recovery, got %v", n.msgs)
	}
	now = now.Add(time.Hour)
	m.Tick(ctx) // healthy again: silent
	if len(n.msgs) != 3 {
		t.Fatalf("noise after recovery: %v", n.msgs)
	}
}

func TestFirstRunConsolidatesAndOptionalIsSilent(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	last := now.Add(-48 * time.Hour) // already stale at first sight
	n := &fakeNotifier{}
	m := newMon(t, &last, &now, n)
	m.Checks = append(m.Checks, Check{
		Source: "dead", Label: "Known dead", MaxAge: time.Hour, Optional: true,
		Probe: func(context.Context) (time.Time, bool, string, error) { return time.Time{}, false, "never", nil },
	})
	m.Tick(ctx)
	if len(n.msgs) != 1 || !strings.Contains(n.msgs[0], "monitor is on") ||
		!strings.Contains(n.msgs[0], "Test feed") || strings.Contains(n.msgs[0], "Known dead") {
		t.Fatalf("want one consolidated message without optional sources, got %v", n.msgs)
	}
	m.Tick(ctx)
	if len(n.msgs) != 1 {
		t.Fatalf("repeat on second tick: %v", n.msgs)
	}
}

func TestUndeliveredIsRecorded(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	last := now.Add(-time.Minute)
	n := &fakeNotifier{fail: true}
	m := newMon(t, &last, &now, n)
	m.Tick(ctx)
	now = now.Add(5 * time.Hour)
	m.Tick(ctx)
	var delivered int
	if err := m.DB.QueryRow(`SELECT delivered FROM monitor_events WHERE kind='down'`).Scan(&delivered); err != nil {
		t.Fatal(err)
	}
	if delivered != 0 {
		t.Fatal("failed send must be recorded as undelivered")
	}
}
