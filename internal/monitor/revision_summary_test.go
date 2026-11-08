package monitor

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type failingNotifier struct {
	fail bool
	msgs []string
}

func (f *failingNotifier) Send(_ context.Context, text string) error {
	if f.fail {
		return errors.New("telegram down")
	}
	f.msgs = append(f.msgs, text)
	return nil
}

func (e *calEnv) thesisRow(tk, locked, urgency, reported string, status string) {
	e.exec(`INSERT INTO theses_index (ticker, adapter, version, status, locked_date, github_path, github_url, markdown_content, rendered_html, file_sha, earnings_urgency, created_at, updated_at)
		VALUES (?, 'pharma', 1, ?, ?, ?, 'u', 'x', 'x', 'sha', ?, 0, 0)`, tk, status, locked, "theses/pharma/"+tk+".md", urgency)
	if reported != "" {
		e.exec(`INSERT OR REPLACE INTO ticker_calendar (ticker, earnings_status, last_reported_date, updated_at) VALUES (?, 'DATE_KNOWN', ?, ?)`, tk, reported, e.now.Unix())
	}
}

// One message a day, held tickers only, with the days since the report; silent when empty; a restart or a
// second call the same day does not repeat it; the next day repeats it while the list is not empty.
func TestRevisionSummary_OneDailyMessage_HeldOnly_DaysSinceReport(t *testing.T) {
	e := newCalEnv(t)
	n := &failingNotifier{}
	now := e.now // 2026-11-07 10:00 UTC
	send := func() bool {
		ok, err := RevisionSummary(context.Background(), e.st.DB, n, now)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	// nothing to re-score: silent
	if send() || len(n.msgs) != 0 {
		t.Fatalf("empty list sent: %q", n.msgs)
	}
	e.holding("AEM")
	e.holding("MRVL")
	e.holding("NVO")
	e.thesisRow("AEM", "2026-05-18", "revision_needed", "2026-10-28", "locked")     // 10 days
	e.thesisRow("MRVL", "2026-05-20", "revision_needed", "2026-08-27", "locked")    // 72 days
	e.thesisRow("NVO", "2026-05-24", "none", "2026-04-01", "locked")                // reported before the lock
	e.thesisRow("NOTHELD", "2026-05-24", "revision_needed", "2026-11-04", "locked") // not held
	e.thesisRow("OLDV", "2026-01-01", "revision_needed", "2026-11-04", "superseded")
	e.holding("OLDV")
	if !send() || len(n.msgs) != 1 {
		t.Fatalf("expected one message, got %q", n.msgs)
	}
	m := n.msgs[0]
	if !strings.Contains(m, "2 held theses to re-score") || !strings.Contains(m, "• MRVL — reported 72 days ago (2026-08-27), locked 2026-05-20") ||
		!strings.Contains(m, "• AEM — reported 10 days ago (2026-10-28), locked 2026-05-18") {
		t.Fatalf("message: %q", m)
	}
	if strings.Contains(m, "NVO") || strings.Contains(m, "NOTHELD") || strings.Contains(m, "OLDV") {
		t.Fatalf("message lists a ticker it should not: %q", m)
	}
	if strings.Index(m, "MRVL") > strings.Index(m, "AEM") {
		t.Fatalf("longest wait should come first: %q", m)
	}
	// same day again (a restart, a second schedule): nothing
	if send() || len(n.msgs) != 1 {
		t.Fatalf("repeated the same day: %q", n.msgs)
	}
	// next day: one more, with the days moved on
	now = now.Add(24 * time.Hour)
	if !send() || len(n.msgs) != 2 || !strings.Contains(n.msgs[1], "reported 73 days ago") {
		t.Fatalf("next day: %q", n.msgs)
	}
	// re-scored (urgency cleared): silent again
	e.exec(`UPDATE theses_index SET earnings_urgency = 'none'`)
	now = now.Add(24 * time.Hour)
	if send() || len(n.msgs) != 2 {
		t.Fatalf("sent after everything was re-scored: %q", n.msgs)
	}
}

func TestRevisionSummary_FailedSendIsRetriedNotRecorded(t *testing.T) {
	e := newCalEnv(t)
	n := &failingNotifier{fail: true}
	e.holding("AEM")
	e.thesisRow("AEM", "2026-05-18", "revision_needed", "2026-10-28", "locked")
	if ok, err := RevisionSummary(context.Background(), e.st.DB, n, e.now); ok || err == nil {
		t.Fatalf("a failed send must report an error, got ok=%v err=%v", ok, err)
	}
	n.fail = false
	if ok, err := RevisionSummary(context.Background(), e.st.DB, n, e.now); !ok || err != nil || len(n.msgs) != 1 {
		t.Fatalf("retry the same day: ok=%v err=%v msgs=%q", ok, err, n.msgs)
	}
}

func TestRevisionSummary_LongListIsCapped(t *testing.T) {
	e := newCalEnv(t)
	n := &failingNotifier{}
	for i := 0; i < 29; i++ { // the cap is 25 lines
		tk := "T" + string(rune('A'+i/26)) + string(rune('A'+i%26))
		e.holding(tk)
		e.thesisRow(tk, "2026-05-18", "revision_needed", "2026-10-28", "locked")
	}
	if ok, _ := RevisionSummary(context.Background(), e.st.DB, n, e.now); !ok || !strings.Contains(n.msgs[0], "… and 4 more") {
		t.Fatalf("cap: %q", n.msgs)
	}
}
