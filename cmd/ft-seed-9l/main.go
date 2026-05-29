// One-shot seeder for Spec 9l adapter MD bodies.
//
// Usage:
//
//	go run /tmp/seed_9l_adapters.go \
//	  -db /var/lib/ft/ft.db \
//	  -dir /tmp
//
// For each of (btc, l1, speculative) it:
//  1. Reads /<dir>/<SLUG>_adapter_v1.md  (case follows the file convention)
//  2. Renders to HTML via internal/cryptotheses Render()
//  3. Archives the existing markdown_current as v0-placeholder in
//     crypto_adapter_versions (idempotent — INSERT OR IGNORE)
//  4. UPDATEs crypto_adapters SET current_version='v1', status='locked',
//     locked_at=NOW(), markdown_current=<md>, rendered_html=<html>
//
// Safe to re-run — the v0-placeholder archive is idempotent via UNIQUE
// (adapter_id, version); the UPDATE just re-sets the v1 body.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ft/internal/cryptotheses"

	_ "modernc.org/sqlite"
)

func main() {
	dbPath := flag.String("db", "/var/lib/ft/ft.db", "ft.db path")
	dir := flag.String("dir", "/tmp", "directory containing <SLUG>_adapter_v1.md files")
	flag.Parse()

	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)", *dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		log.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	type seed struct {
		slug string
		file string
	}
	seeds := []seed{
		{slug: "btc", file: "BTC_adapter_v1.md"},
		{slug: "l1", file: "L1_adapter_v1.md"},
		{slug: "speculative", file: "Speculative_adapter_v1.md"},
	}

	now := time.Now().Unix()
	for _, sd := range seeds {
		path := filepath.Join(*dir, sd.file)
		raw, err := os.ReadFile(path)
		if err != nil {
			log.Fatalf("read %s: %v", path, err)
		}
		md := string(raw)
		html := cryptotheses.Render(md)

		// Look up adapter id + current_version + current markdown.
		var adapterID int64
		var currentVersion, currentMD string
		err = db.QueryRow(`SELECT id, current_version, markdown_current FROM crypto_adapters WHERE slug = ?`, sd.slug).
			Scan(&adapterID, &currentVersion, &currentMD)
		if err != nil {
			log.Fatalf("lookup %s: %v", sd.slug, err)
		}

		tx, err := db.Begin()
		if err != nil {
			log.Fatalf("begin tx: %v", err)
		}

		// Archive existing body under its existing version label (idempotent).
		_, err = tx.Exec(`
			INSERT INTO crypto_adapter_versions (adapter_id, version, markdown, changelog_note)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(adapter_id, version) DO NOTHING`,
			adapterID, currentVersion, currentMD,
			sql.NullString{String: "auto-archived by seed_9l_adapters before v1 lock", Valid: true})
		if err != nil {
			tx.Rollback()
			log.Fatalf("archive %s: %v", sd.slug, err)
		}

		// Also archive the new v1 body for full history.
		_, err = tx.Exec(`
			INSERT INTO crypto_adapter_versions (adapter_id, version, markdown, changelog_note)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(adapter_id, version) DO NOTHING`,
			adapterID, "v1", md,
			sql.NullString{String: "First locked adapter MD authored by Claude.ai under Spec 9l", Valid: true})
		if err != nil {
			tx.Rollback()
			log.Fatalf("archive v1 %s: %v", sd.slug, err)
		}

		// Update current to v1 / locked.
		_, err = tx.Exec(`
			UPDATE crypto_adapters
			   SET markdown_current = ?,
			       rendered_html    = ?,
			       current_version  = ?,
			       status           = 'locked',
			       locked_at        = ?,
			       updated_at       = ?
			 WHERE id = ?`,
			md, html, "v1", now, now, adapterID)
		if err != nil {
			tx.Rollback()
			log.Fatalf("update %s: %v", sd.slug, err)
		}

		if err := tx.Commit(); err != nil {
			log.Fatalf("commit %s: %v", sd.slug, err)
		}
		fmt.Printf("  [%-12s] %d bytes md, %d bytes html → v1 locked\n",
			sd.slug, len(md), len(html))
	}

	// Quick summary.
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM crypto_adapters WHERE status = 'locked'`).Scan(&n)
	fmt.Printf("\n%d adapters now status=locked.\n", n)
	if n != 3 {
		fmt.Fprintln(os.Stderr, "WARNING: expected 3 locked adapters")
		os.Exit(1)
	}
	if !strings.HasPrefix(*dbPath, "/tmp") && !strings.HasPrefix(*dbPath, "/var") {
		log.Fatal("safety: db path must be under /tmp or /var")
	}
}
