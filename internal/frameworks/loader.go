// Package frameworks loads Jordi/Cowen-style scoring frameworks from JSON
// definitions embedded at build time. The schema is generic so future
// frameworks slot in by dropping another JSON file in `definitions/` and
// listing it in the loader's manifest.
//
// Loaded once at startup via `Load()`; subsequent reads are mutex-protected
// for safety (hot-reload could come later). Malformed JSON disables that
// framework with a warning rather than crashing the server.
package frameworks

import (
	"embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

//go:embed definitions/*.json
var definitionsFS embed.FS

// Framework is the runtime shape of a loaded definition. Mirrors the JSON
// schema in the spec — keep field tags in lockstep with the JSON keys.
type Framework struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	AppliesTo  string            `json:"applies_to"` // "stock" | "crypto"
	Version    string            `json:"version"`
	Source     string            `json:"source"`
	Questions  []Question        `json:"questions"`
	Scoring    Scoring           `json:"scoring"`
	Tags       map[string][]string `json:"tags"`
}

// Question is one row in the 8-Question Screen UI.
type Question struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Prompt   string `json:"prompt"`
	Guidance string `json:"guidance,omitempty"`
	Weight   int    `json:"weight"`
}

type Scoring struct {
	Scale          string   `json:"scale"`            // "0_1_2"
	MaxTotal       int      `json:"max_total"`
	PassThreshold  int      `json:"pass_threshold"`
	StrongSignals  []string `json:"strong_signals,omitempty"`
}

// Validate enforces the contract the rest of the code relies on. Called once
// per framework at load time.
func (f Framework) Validate() error {
	if f.ID == "" {
		return fmt.Errorf("missing id")
	}
	if f.AppliesTo != "stock" && f.AppliesTo != "crypto" {
		return fmt.Errorf("applies_to must be stock|crypto, got %q", f.AppliesTo)
	}
	if len(f.Questions) == 0 {
		return fmt.Errorf("no questions")
	}
	seen := map[string]bool{}
	computedMax := 0
	for _, q := range f.Questions {
		if q.ID == "" {
			return fmt.Errorf("question with no id")
		}
		if seen[q.ID] {
			return fmt.Errorf("duplicate question id %q", q.ID)
		}
		seen[q.ID] = true
		if q.Weight < 0 {
			return fmt.Errorf("question %s has negative weight", q.ID)
		}
		w := q.Weight
		if w == 0 {
			w = 1
		}
		computedMax += 2 * w // scale 0_1_2 → max contribution = 2*weight per question
	}
	if f.Scoring.MaxTotal != 0 && f.Scoring.MaxTotal != computedMax {
		return fmt.Errorf("max_total mismatch: spec says %d, computed %d", f.Scoring.MaxTotal, computedMax)
	}
	if f.Scoring.PassThreshold < 0 || f.Scoring.PassThreshold > computedMax {
		return fmt.Errorf("pass_threshold %d out of [0, %d]", f.Scoring.PassThreshold, computedMax)
	}
	return nil
}

// QuestionByID is a small helper for handlers that take a payload + want to
// validate a known question id.
func (f Framework) QuestionByID(id string) (*Question, bool) {
	for i := range f.Questions {
		if f.Questions[i].ID == id {
			return &f.Questions[i], true
		}
	}
	return nil, false
}

// MaxScore returns the framework's max possible total. Derived from question
// weights × 2 (the 0/1/2 scale).
func (f Framework) MaxScore() int {
	max := 0
	for _, q := range f.Questions {
		w := q.Weight
		if w == 0 {
			w = 1
		}
		max += 2 * w
	}
	return max
}

// ----- registry ----------------------------------------------------------

var (
	mu     sync.RWMutex
	loaded = map[string]Framework{}
)

// Load reads every definitions/*.json. Malformed files log a warning and are
// skipped. Safe to call multiple times — overwrites the registry each call.
func Load() error {
	entries, err := definitionsFS.ReadDir("definitions")
	if err != nil {
		return fmt.Errorf("read definitions dir: %w", err)
	}
	next := map[string]Framework{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := definitionsFS.ReadFile("definitions/" + e.Name())
		if err != nil {
			slog.Warn("framework: read failed; skipping", "file", e.Name(), "err", err)
			continue
		}
		var f Framework
		if err := json.Unmarshal(raw, &f); err != nil {
			slog.Warn("framework: parse failed; skipping", "file", e.Name(), "err", err)
			continue
		}
		if err := f.Validate(); err != nil {
			slog.Warn("framework: validation failed; skipping", "file", e.Name(), "err", err)
			continue
		}
		if _, dup := next[f.ID]; dup {
			slog.Warn("framework: duplicate id; later wins", "id", f.ID)
		}
		next[f.ID] = f
		slog.Info("framework loaded", "id", f.ID, "appliesTo", f.AppliesTo, "questions", len(f.Questions))
	}
	mu.Lock()
	loaded = next
	mu.Unlock()
	return nil
}

// Get returns the framework by id, or false if not loaded.
func Get(id string) (Framework, bool) {
	mu.RLock()
	defer mu.RUnlock()
	f, ok := loaded[id]
	return f, ok
}

// ForKind returns the default framework for a holding kind. Jordi for stock,
// Cowen for crypto. Future: when more than one stock framework exists, the
// user picks per-entry.
func ForKind(kind string) (Framework, bool) {
	mu.RLock()
	defer mu.RUnlock()
	wantID := ""
	switch kind {
	case "stock":
		wantID = "jordi"
	case "crypto":
		wantID = "cowen"
	}
	f, ok := loaded[wantID]
	return f, ok
}

// All returns every loaded framework, sorted by id for deterministic order.
func All() []Framework {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Framework, 0, len(loaded))
	for _, f := range loaded {
		out = append(out, f)
	}
	// stable order: stock first, then crypto, alphabetical within
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j].AppliesTo < out[i].AppliesTo ||
				(out[j].AppliesTo == out[i].AppliesTo && out[j].ID < out[i].ID) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}
