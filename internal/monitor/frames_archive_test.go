package monitor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFramesArchiveEval(t *testing.T) {
	d := t.TempDir()
	flag, failed, ok := filepath.Join(d, "flag"), filepath.Join(d, "failed"), filepath.Join(d, "ok")
	ev := framesArchiveEval(flag, failed, ok)
	now := time.Now()
	if down, _, _, _, _ := ev(context.Background(), now); down {
		t.Fatal("off by default must stay quiet, even with a failure marker present")
	}
	_ = os.WriteFile(failed, []byte("f_12.jpg: checksum mismatch\nmore"), 0o644)
	if down, _, _, _, _ := ev(context.Background(), now); down {
		t.Fatal("marker is ignored while the archive is off")
	}
	_ = os.WriteFile(flag, nil, 0o644)
	down, _, _, detail, _ := ev(context.Background(), now)
	if !down || detail == "" {
		t.Fatalf("failure marker must alert: %v %q", down, detail)
	}
	_ = os.Remove(failed)
	if down, _, found, _, _ := ev(context.Background(), now); !down || found {
		t.Fatal("on without a success marker must alert as not run yet")
	}
	_ = os.WriteFile(ok, nil, 0o644)
	if down, _, _, _, _ := ev(context.Background(), now); down {
		t.Fatal("fresh success marker is healthy")
	}
	if down, _, _, _, _ := ev(context.Background(), now.Add(4*time.Hour)); !down {
		t.Fatal("a stale success marker must alert")
	}
}
