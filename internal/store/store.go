// Package store owns the *sql.DB handle and applies embedded migrations.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// ErrNotFound is returned by store methods when a row doesn't exist.
var ErrNotFound = errors.New("not found")

type Store struct {
	DB *sql.DB
}

// Open returns a connected store with WAL + foreign keys enabled.
func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)",
		path,
	)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sql.Open: %w", err)
	}
	// SQLite is single-writer; cap concurrency to keep contention low.
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	db.SetConnMaxLifetime(0)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &Store{DB: db}, nil
}

func (s *Store) Close() error {
	return s.DB.Close()
}

// Migrate runs embedded *.sql files in lexicographic order. Each file is
// recorded in schema_migrations and skipped on subsequent runs.
func (s *Store) Migrate() error {
	if _, err := s.DB.Exec(
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applied_at INTEGER NOT NULL
		)`,
	); err != nil {
		return err
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		var v string
		err := s.DB.QueryRow(`SELECT version FROM schema_migrations WHERE version = ?`, name).Scan(&v)
		if err == nil {
			continue // already applied
		}
		if err != sql.ErrNoRows {
			return err
		}
		body, err := fs.ReadFile(migrationsFS, "migrations/"+name)
		if err != nil {
			return err
		}
		if _, err := s.DB.Exec(string(body)); err != nil {
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := s.DB.Exec(
			`INSERT INTO schema_migrations (version, applied_at) VALUES (?, strftime('%s','now'))`,
			name,
		); err != nil {
			return err
		}
	}
	return nil
}
