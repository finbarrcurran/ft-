package providers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ISMFile is the on-disk JSON shape for the manually-curated ISM
// Manufacturing PMI series. Stored at
//
//   <CryptoIndicatorsDataDir>/ism.json    (default /var/lib/ft/data/ism.json)
//
// Maintained by the user via the Settings → Crypto Indicators →
// "Update ISM data" panel (Spec 9e Phase 2 sub-phase B2). One file
// covers the rolling window we need; the user updates monthly when a
// new PMI print drops.
//
// Sample:
//
//   {
//     "prints": [
//       {"month": "2026-04", "value": 48.7},
//       {"month": "2026-03", "value": 49.0},
//       ...
//     ]
//   }
//
// "direction" used to be a separate field in the spec example; we
// compute it from consecutive values instead so users don't have to
// hand-curate it. (Today's print direction = +0.5 → positive,
// -0.5 → negative, in between → flat.)
type ISMFile struct {
	Prints []ISMPrint `json:"prints"`
}
type ISMPrint struct {
	Month string  `json:"month"` // YYYY-MM
	Value float64 `json:"value"`
}

// LoadISM reads the latest ISM data from disk. Returns nil + nil if the
// file doesn't exist (treated as "stale"). Returns nil + error if the
// file exists but can't be parsed.
func LoadISM(dataDir string) (*ISMFile, error) {
	path := filepath.Join(dataDir, "ism.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var f ISMFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &f, nil
}

// SaveISM atomically writes the ISM data file. Used by the upload
// endpoint. Creates the directory if missing.
func SaveISM(dataDir string, f *ISMFile) error {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dataDir, "ism.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// FetchISMReading returns the latest ISM PMI value + the trend over
// the last 4 weeks (i.e. delta vs the previous month's print). If the
// file is missing or empty, returns a friendly stale message.
func FetchISMReading(dataDir string) Reading {
	f, err := LoadISM(dataDir)
	if err != nil {
		return Reading{Err: fmt.Sprintf("ISM file error: %v", err)}
	}
	if f == nil {
		return Reading{Err: "ISM data not uploaded yet (Settings → Crypto Indicators)"}
	}
	if len(f.Prints) == 0 {
		return Reading{Err: "ISM data file is empty"}
	}
	// Sort most-recent first.
	prints := make([]ISMPrint, len(f.Prints))
	copy(prints, f.Prints)
	sort.Slice(prints, func(i, j int) bool {
		return strings.Compare(prints[i].Month, prints[j].Month) > 0
	})
	latest := prints[0]
	out := Reading{Value: &latest.Value}
	if len(prints) >= 2 {
		prev := prints[1]
		if prev.Value != 0 {
			// trend = signed change vs prior month, as a percent of prior
			// (so the scoring engine's positive/negative/flat thresholds map).
			t := (latest.Value - prev.Value) / prev.Value * 100
			out.Trend4w = &t
		}
	}
	return out
}

// ValidateISM does a quick sanity check before saving: at least 1
// print, all months YYYY-MM, all values in 30..70 (ISM PMI hard bounds).
// Returns the cleaned ISMFile or an error message suitable for the
// upload response.
func ValidateISM(f *ISMFile) error {
	if f == nil || len(f.Prints) == 0 {
		return fmt.Errorf("at least one print required")
	}
	seen := map[string]bool{}
	for i, p := range f.Prints {
		if len(p.Month) != 7 || p.Month[4] != '-' {
			return fmt.Errorf("print %d: month must be YYYY-MM, got %q", i, p.Month)
		}
		if seen[p.Month] {
			return fmt.Errorf("duplicate month %q", p.Month)
		}
		seen[p.Month] = true
		if p.Value < 30 || p.Value > 70 {
			return fmt.Errorf("print %s: value %.2f outside plausible ISM range 30..70", p.Month, p.Value)
		}
	}
	return nil
}
