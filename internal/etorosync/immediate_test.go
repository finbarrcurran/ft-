package etorosync

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ft/internal/store"
)

// Documents the intermittent "database is locked (517)" and proves the fix: a
// deferred transaction that reads, loses the race to another writer and then
// writes fails with SQLITE_BUSY_SNAPSHOT; an immediate transaction takes the
// write lock first, so the other writer waits and the write succeeds.
func TestImmediateTransactionAvoidsBusySnapshot(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "ft.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`CREATE TABLE race (v INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`INSERT INTO race VALUES (1)`); err != nil {
		t.Fatal(err)
	}

	// 1. The old behaviour: deferred tx, read, another writer commits, then write.
	tx, err := st.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var v int
	if err := tx.QueryRowContext(ctx, `SELECT v FROM race`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`UPDATE race SET v = 2`); err != nil { // the "other writer"
		t.Fatal(err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE race SET v = 3`)
	tx.Rollback()
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "locked") {
		t.Fatalf("expected the deferred read-then-write to fail with 'database is locked', got %v", err)
	}

	// 2. The fix: immediate tx; the other writer waits for it instead.
	itx, err := beginImmediate(ctx, st.DB)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := itx.QueryContext(ctx, `SELECT v FROM race`)
	if err != nil {
		t.Fatal(err)
	}
	rows.Close()
	done := make(chan error, 1)
	go func() { _, e := st.DB.Exec(`UPDATE race SET v = 10`); done <- e }()
	time.Sleep(200 * time.Millisecond) // the other writer is now waiting on the lock
	if _, err := itx.ExecContext(ctx, `UPDATE race SET v = 3`); err != nil {
		t.Fatalf("immediate tx write failed: %v", err)
	}
	if err := itx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("the other writer should wait and then succeed: %v", err)
	}
	itx.Rollback() // safe after commit
	if err := st.DB.QueryRow(`SELECT v FROM race`).Scan(&v); err != nil || v != 10 {
		t.Fatalf("final value = %d (%v), want 10 (the waiting writer went last)", v, err)
	}
}
