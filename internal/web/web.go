// Package web embeds the SPA assets into the binary and serves them.
//
// Cache-busting: at startup we compute a SHA-256 hash of app.js + app.css and
// stamp the first 8 hex chars into index.html in place of __VERSION__. Asset
// URLs in the page become `/app.css?v=<hash>` and `/app.js?v=<hash>`, so when
// a deploy changes either file the URL changes too and the browser refetches
// without needing a hard reload.
//
// Cache headers:
//   * index.html — `Cache-Control: no-cache` so the browser always revalidates
//     the page itself (so it always sees the latest version hash).
//   * versioned static assets — `public, max-age=31536000, immutable` so the
//     browser keeps them forever (the URL changes when the content does).
package web

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"sync"
)

//go:embed all:assets
var assetsFS embed.FS

var (
	setupOnce sync.Once
	indexHTML []byte
	subFS     fs.FS
)

func setup() {
	s, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		panic(err)
	}
	subFS = s

	// Hash app.js + app.css to derive the cache-bust version string.
	h := sha256.New()
	for _, name := range []string{"app.css", "app.js"} {
		if f, err := s.Open(name); err == nil {
			_, _ = io.Copy(h, f)
			_ = f.Close()
		}
	}
	version := hex.EncodeToString(h.Sum(nil))[:8]

	// Load index.html and substitute __VERSION__.
	if f, err := s.Open("index.html"); err == nil {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, f)
		_ = f.Close()
		body := strings.ReplaceAll(buf.String(), "__VERSION__", version)
		indexHTML = []byte(body)
	}
}

// Handler returns an http.Handler that serves frontend assets and falls back
// to index.html for any non-asset, non-/api path (SPA routing).
func Handler() http.Handler {
	setupOnce.Do(setup)
	fileServer := http.FileServer(http.FS(subFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// API + healthz are handled by the main mux ahead of this catch-all;
		// defend in depth.
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/healthz" {
			http.NotFound(w, r)
			return
		}

		clean := strings.TrimPrefix(r.URL.Path, "/")
		if clean == "" {
			serveIndex(w)
			return
		}

		if _, err := fs.Stat(subFS, clean); err == nil {
			// Versioned URL (`?v=...`) means content is immutable for that
			// version; let the browser cache aggressively.
			if r.URL.RawQuery != "" {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA fallback.
		serveIndex(w)
	})
}

func serveIndex(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(indexHTML)
}
