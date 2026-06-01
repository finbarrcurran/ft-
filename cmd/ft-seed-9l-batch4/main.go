// One-shot seeder for the Spec 9l Adapter Expansion v1 (adapters 9–12).
//
// Usage:
//
//	go run ./cmd/ft-seed-9l-batch4 \
//	  -db /var/lib/ft/ft.db \
//	  -dir /tmp
//
// For each of (stablecoin, privacy, cefi-exchange, ai-agent) it:
//  1. Reads /<dir>/<File>_adapter_v1.md
//  2. Renders to HTML via internal/cryptotheses Render()
//  3. Archives the existing markdown_current under its current version label
//     and the new v1 body in crypto_adapter_versions (idempotent)
//  4. UPDATEs crypto_adapters SET current_version='v1', markdown_current=<md>,
//     rendered_html=<html>, updated_at=NOW()
//
// Unlike ft-seed-9l (the original 8), these land status='draft' and are NOT
// locked: per the Expansion v1 cover note they are uncalibrated templates and
// no thesis may be locked on them until first-use calibration.
//
// The placeholder rows are created at boot by cryptotheses.SeedIfEmpty, so this
// seeder expects the four slugs to already exist; run it after a deploy that
// includes migration 0040 + the new SeedIfEmpty entries.
//
// Safe to re-run.
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
	dir := flag.String("dir", "/tmp", "directory containing <File>_adapter_v1.md files")
	flag.Parse()

	if !strings.HasPrefix(*dbPath, "/tmp") && !strings.HasPrefix(*dbPath, "/var") {
		log.Fatal("safety: db path must be under /tmp or /var")
	}

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
		{slug: "stablecoin", file: "Stablecoin_adapter_v1.md"},
		{slug: "privacy", file: "Privacy_adapter_v1.md"},
		{slug: "cefi-exchange", file: "CeFi_Exchange_Token_adapter_v1.md"},
		{slug: "ai-agent", file: "AI_Agent_adapter_v1.md"},
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

		var adapterID int64
		var currentVersion, currentMD string
		err = db.QueryRow(`SELECT id, current_version, markdown_current FROM crypto_adapters WHERE slug = ?`, sd.slug).
			Scan(&adapterID, &currentVersion, &currentMD)
		if err != nil {
			log.Fatalf("lookup %s (run after deploy so SeedIfEmpty created the row): %v", sd.slug, err)
		}

		tx, err := db.Begin()
		if err != nil {
			log.Fatalf("begin tx: %v", err)
		}

		// Archive the existing (placeholder) body under its current label.
		_, err = tx.Exec(`
			INSERT INTO crypto_adapter_versions (adapter_id, version, markdown, changelog_note)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(adapter_id, version) DO NOTHING`,
			adapterID, currentVersion, currentMD,
			sql.NullString{String: "auto-archived by batch4 seeder before v1 draft", Valid: true})
		if err != nil {
			tx.Rollback()
			log.Fatalf("archive %s: %v", sd.slug, err)
		}

		// Archive the v1 draft body for history.
		_, err = tx.Exec(`
			INSERT INTO crypto_adapter_versions (adapter_id, version, markdown, changelog_note)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(adapter_id, version) DO NOTHING`,
			adapterID, "v1", md,
			sql.NullString{String: "Adapter Expansion v1 draft template (uncalibrated) authored by Claude.ai", Valid: true})
		if err != nil {
			tx.Rollback()
			log.Fatalf("archive v1 %s: %v", sd.slug, err)
		}

		// Update current to v1 — status stays 'draft' (uncalibrated; not locked).
		_, err = tx.Exec(`
			UPDATE crypto_adapters
			   SET markdown_current = ?,
			       rendered_html    = ?,
			       current_version  = ?,
			       status           = 'draft',
			       updated_at       = ?
			 WHERE id = ?`,
			md, html, "v1", now, adapterID)
		if err != nil {
			tx.Rollback()
			log.Fatalf("update %s: %v", sd.slug, err)
		}

		if err := tx.Commit(); err != nil {
			log.Fatalf("commit %s: %v", sd.slug, err)
		}
		fmt.Printf("  [%-13s] %d bytes md, %d bytes html → v1 draft\n",
			sd.slug, len(md), len(html))
	}

	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM crypto_adapters`).Scan(&n)
	fmt.Printf("\n%d crypto adapters total.\n", n)
	if n != 12 {
		fmt.Fprintf(os.Stderr, "WARNING: expected 12 adapters, found %d\n", n)
		os.Exit(1)
	}
}
