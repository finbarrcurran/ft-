package monitor

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// SC-54 Drive archive of chart frames (FT-Frames, outside FT-Bridge). Off by default: while the flag
// file is absent the check is quiet. When it is on, an upload-failure marker written by the archive
// script alerts at once (with the first line of the marker as the reason), and a missing success
// marker for more than framesArchiveAllowed alerts as stale.
const (
	framesArchiveFlag    = "/etc/ft/frames-archive.enabled"
	framesArchiveFailed  = "/var/lib/ft/data/bridge/frames.failed"
	framesArchiveOK      = "/var/lib/ft/data/bridge/frames.ok"
	framesArchiveAllowed = 3 * time.Hour
)

func framesArchiveEval(flag, failed, okFile string) func(ctx context.Context, now time.Time) (bool, time.Time, bool, string, error) {
	return func(ctx context.Context, now time.Time) (bool, time.Time, bool, string, error) {
		if _, err := os.Stat(flag); err != nil {
			return false, now, true, "frame archive is off (default)", nil
		}
		if st, err := os.Stat(failed); err == nil {
			b, _ := os.ReadFile(failed)
			line := strings.TrimSpace(strings.SplitN(string(b), "\n", 2)[0])
			return true, st.ModTime().UTC(), true, "a frame upload to Drive failed: " + line + " (the frame stays on Jarvis; the next run retries)", nil
		}
		st, err := os.Stat(okFile)
		if err != nil {
			return true, time.Time{}, false, "the frame archive is on but has not completed a run yet", nil
		}
		last := st.ModTime().UTC()
		if age := now.Sub(last); age > framesArchiveAllowed {
			return true, last, true, fmt.Sprintf("the frame archive last completed a run %s ago (it runs every 10 minutes)", age.Round(time.Minute)), nil
		}
		return false, last, true, "frame archive healthy", nil
	}
}
