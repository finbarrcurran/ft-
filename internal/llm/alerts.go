// Spec 9c.1 D10 — threshold alerts.
//
// Detection runs inside Call() (after the gate predicts post-call total)
// so the alert fires *before* the spend actually hits the wire. Dedup is
// per-month for the 50/75/90/100 monthly thresholds and per-day for the
// daily-100 threshold; we stash "last fired" timestamps in user_preferences
// keyed by `llm_alert_last_fired_<threshold>`.
//
// Telegram delivery is best-effort: when FT_TELEGRAM_BOT_TOKEN +
// FT_TELEGRAM_CHAT_ID are set in /etc/ft/env, we POST directly to the
// Telegram Bot API. When they aren't, the threshold is still logged and
// the dedup row still written (so a future delivery doesn't double-fire).

package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// thresholdLevel maps the boundary pct to its dedup key + alert wording.
type thresholdLevel struct {
	Pct          float64
	DedupKey     string // pref key for "last fired" timestamp
	DedupBucket  string // "2026-05" for monthly, "2026-05-17" for daily
	Title        string // "🟡 75% of monthly budget"
	BodyTemplate string // sprintf template; gets (spentUSD, capUSD, pct)
}

func (s *Service) checkAndFireThresholds(ctx context.Context, predictedMonthSpend, monthlyCap, predictedDaySpend, dailyCap float64) {
	monthBucket := s.clock().UTC().Format("2006-01")
	dayBucket := s.clock().UTC().Format("2006-01-02")
	monthlyPct := 0.0
	dailyPct := 0.0
	if monthlyCap > 0 {
		monthlyPct = predictedMonthSpend / monthlyCap * 100
	}
	if dailyCap > 0 {
		dailyPct = predictedDaySpend / dailyCap * 100
	}

	// Monthly thresholds (each fires once per month).
	monthlyLevels := []thresholdLevel{
		{50,  "llm_alert_last_fired_50_pct",  monthBucket, "🟢 50% of monthly LLM budget", "FT LLM spend: $%.2f of $%.2f monthly (%.0f%%). Pacing fine."},
		{75,  "llm_alert_last_fired_75_pct",  monthBucket, "🟡 75% of monthly LLM budget", "FT LLM spend: $%.2f of $%.2f (%.0f%%). Days remaining in month."},
		{90,  "llm_alert_last_fired_90_pct",  monthBucket, "🟠 90% of monthly LLM budget", "FT LLM spend: $%.2f of $%.2f (%.0f%%). LLM features may pause soon."},
		{100, "llm_alert_last_fired_100_pct", monthBucket, "🔴 100% of monthly LLM budget", "FT hit monthly LLM cap ($%.2f of $%.2f, %.0f%%). All LLM features paused until next month, or override in Settings."},
	}
	for _, t := range monthlyLevels {
		if monthlyPct >= t.Pct && !s.alreadyFired(ctx, t.DedupKey, t.DedupBucket) {
			s.fireThreshold(ctx, t, predictedMonthSpend, monthlyCap, monthlyPct)
		}
	}
	// Daily 100% (fires once per day).
	if dailyPct >= 100 {
		t := thresholdLevel{
			Pct: 100, DedupKey: "llm_alert_last_fired_daily_100", DedupBucket: dayBucket,
			Title: "🔴 Daily LLM cap hit",
			BodyTemplate: "FT hit today's $%.2f daily cap ($%.2f used, %.0f%%). LLM features pause until midnight UTC.",
		}
		if !s.alreadyFired(ctx, t.DedupKey, t.DedupBucket) {
			s.fireThreshold(ctx, t, predictedDaySpend, dailyCap, dailyPct)
		}
	}
}

func (s *Service) alreadyFired(ctx context.Context, key, bucket string) bool {
	v, _ := s.Store.GetPreference(ctx, key)
	return v == bucket
}

func (s *Service) fireThreshold(ctx context.Context, t thresholdLevel, spent, cap, pct float64) {
	// Honour per-threshold opt-outs (Settings can disable specific levels).
	prefKey := ""
	switch t.Pct {
	case 50:
		prefKey = "llm_alert_threshold_50_pct"
	case 75:
		prefKey = "llm_alert_threshold_75_pct"
	case 90:
		prefKey = "llm_alert_threshold_90_pct"
	case 100:
		prefKey = "llm_alert_threshold_100_pct"
	}
	if prefKey != "" && !s.boolPref(ctx, prefKey, true) {
		return
	}
	// Mark fired BEFORE attempting send so a Telegram outage doesn't
	// cause repeated firing of the same threshold.
	if err := s.Store.SetPreference(ctx, t.DedupKey, t.DedupBucket); err != nil {
		slog.Warn("llm: dedup write failed", "key", t.DedupKey, "err", err)
	}
	body := fmt.Sprintf(t.BodyTemplate, spent, cap, pct)
	msg := t.Title + "\n\n" + body
	if err := s.sendTelegram(ctx, msg); err != nil {
		slog.Warn("llm: threshold telegram failed", "level", t.Pct, "err", err)
	} else {
		slog.Info("llm: threshold alert fired", "pct", t.Pct, "spent", spent, "cap", cap)
	}
}

// sendTelegram posts a single message to Fin's chat via the FT bot's
// HTTP API. Best-effort: returns nil silently when token/chat aren't
// configured (the dedup row is still written so we don't retry-loop).
func (s *Service) sendTelegram(ctx context.Context, text string) error {
	botToken := os.Getenv("FT_TELEGRAM_BOT_TOKEN")
	chatID := os.Getenv("FT_TELEGRAM_CHAT_ID")
	if botToken == "" || chatID == "" {
		return nil // not configured; not an error
	}
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)
	body, _ := json.Marshal(map[string]any{
		"chat_id":                  chatID,
		"text":                     escapeMD2(text),
		"parse_mode":               "MarkdownV2",
		"disable_web_page_preview": true,
	})
	httpCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(httpCtx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telegram %d", resp.StatusCode)
	}
	return nil
}

// escapeMD2 escapes Telegram MarkdownV2 reserved chars. Same logic as
// ft-bot/index.js esc().
func escapeMD2(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '_', '*', '[', ']', '(', ')', '~', '`', '>', '#', '+', '-', '=', '|', '{', '}', '.', '!', '\\':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
