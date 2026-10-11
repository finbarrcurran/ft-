package llm

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ft/internal/store"
)

func newAlertService(t *testing.T) (*Service, *[]string) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "ft.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	var sent []string
	svc := NewService(st, "")
	svc.clock = func() time.Time { return time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC) }
	svc.Notify = func(_ context.Context, text string) error { sent = append(sent, text); return nil }
	return svc, &sent
}

// SC-48: the 9c.1 thresholds map to alerts exactly once each (monthly 50/75/90/100 per
// month, daily 100 per day), and re-crossing does not repeat.
func TestBudgetThresholdsMapToAlerts(t *testing.T) {
	ctx := context.Background()
	svc, sent := newAlertService(t)
	const monthlyCap, dailyCap = 100.0, 10.0

	step := func(month, day float64) { svc.checkAndFireThresholds(ctx, month, monthlyCap, day, dailyCap) }
	step(49, 0)
	if len(*sent) != 0 {
		t.Fatalf("49%% must not alert: %v", *sent)
	}
	step(50, 0)
	step(76, 0) // crosses 75 only (50 already fired)
	step(91, 0)
	step(100, 0)
	if len(*sent) != 4 {
		t.Fatalf("want 4 monthly alerts (50/75/90/100), got %d: %v", len(*sent), *sent)
	}
	for i, want := range []string{"50%", "75%", "90%", "100%"} {
		if !strings.Contains((*sent)[i], want) {
			t.Fatalf("alert %d should mention %s: %q", i, want, (*sent)[i])
		}
	}
	step(100, 0) // no repeat in the same month
	if len(*sent) != 4 {
		t.Fatalf("repeat alert in the same month: %d", len(*sent))
	}
	step(100, 10) // daily cap
	if len(*sent) != 5 || !strings.Contains((*sent)[4], "Daily LLM cap") {
		t.Fatalf("daily 100%% alert missing: %v", *sent)
	}
	step(100, 12)
	if len(*sent) != 5 {
		t.Fatal("daily alert repeated on the same day")
	}
}

func TestBudgetAlertRespectsOptOut(t *testing.T) {
	ctx := context.Background()
	svc, sent := newAlertService(t)
	if err := svc.Store.SetPreference(ctx, "llm_alert_threshold_75_pct", "false"); err != nil {
		t.Fatal(err)
	}
	svc.checkAndFireThresholds(ctx, 80, 100, 0, 10)
	if len(*sent) != 1 || !strings.Contains((*sent)[0], "50%") {
		t.Fatalf("with 75%% opted out, 80%% spend should fire only the 50%% alert, got %v", *sent)
	}
}

func TestSendTestAlertAndNotConfigured(t *testing.T) {
	svc, sent := newAlertService(t)
	if err := svc.SendTestAlert(context.Background()); err != nil || len(*sent) != 1 || !strings.Contains((*sent)[0], "TEST") {
		t.Fatalf("test alert: %v %v", err, *sent)
	}
	// Without the hook and without Telegram env, delivery must say so (not silently succeed).
	t.Setenv("FT_TELEGRAM_BOT_TOKEN", "")
	t.Setenv("FT_TELEGRAM_CHAT_ID", "")
	svc.Notify = nil
	if err := svc.SendTestAlert(context.Background()); err == nil {
		t.Fatal("unconfigured Telegram must return an error, not nil")
	}
}
