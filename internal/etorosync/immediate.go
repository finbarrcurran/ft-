package etorosync

import (
	"context"
	"database/sql"
)

// immediateTx is a write transaction opened with BEGIN IMMEDIATE on a dedicated
// connection. The sync reads the open lots and then writes; as a default
// (deferred) transaction that read-then-write upgrade fails with
// SQLITE_BUSY_SNAPSHOT ("database is locked (517)") whenever any other writer
// commits in between, and busy_timeout does not retry that case. Taking the
// write lock first makes the sync wait its turn instead (busy_timeout applies).
type immediateTx struct{ conn *sql.Conn }

func beginImmediate(ctx context.Context, db *sql.DB) (*immediateTx, error) {
	c, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := c.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		c.Close()
		return nil, err
	}
	return &immediateTx{conn: c}, nil
}

func (t *immediateTx) ExecContext(ctx context.Context, q string, a ...any) (sql.Result, error) {
	return t.conn.ExecContext(ctx, q, a...)
}

func (t *immediateTx) QueryContext(ctx context.Context, q string, a ...any) (*sql.Rows, error) {
	return t.conn.QueryContext(ctx, q, a...)
}

func (t *immediateTx) Commit() error {
	_, err := t.conn.ExecContext(context.Background(), "COMMIT")
	if err == nil {
		t.conn.Close()
		t.conn = nil
	}
	return err
}

// Rollback is a no-op after a successful Commit, so it is safe to defer.
func (t *immediateTx) Rollback() {
	if t.conn == nil {
		return
	}
	_, _ = t.conn.ExecContext(context.Background(), "ROLLBACK")
	t.conn.Close()
	t.conn = nil
}

type queryer interface {
	QueryContext(ctx context.Context, q string, a ...any) (*sql.Rows, error)
}
