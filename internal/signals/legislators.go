package signals

// Quarterly refresh of the legislator + committee-assignment roster
// from the public unitedstates/congress-legislators dataset. Used by
// the Congress tier compute (Spec 9k §D6) to determine whether a
// trading legislator sits on a committee with jurisdiction over the
// traded sector.
//
// Two JSON feeds, both free, both ASCII:
//   https://theunitedstates.io/congress-legislators/legislators-current.json
//   https://theunitedstates.io/congress-legislators/committee-membership-current.json

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	rosterURL     = "https://theunitedstates.io/congress-legislators/legislators-current.json"
	committeesURL = "https://theunitedstates.io/congress-legislators/committee-membership-current.json"
)

// Roster JSON entry — only the fields we actually use.
type rosterEntry struct {
	ID struct {
		Bioguide string `json:"bioguide"`
	} `json:"id"`
	Name struct {
		First    string `json:"first"`
		Last     string `json:"last"`
		Official string `json:"official_full"`
	} `json:"name"`
	Terms []struct {
		Type     string `json:"type"`     // "rep" | "sen"
		State    string `json:"state"`
		District int    `json:"district"` // House only
		Party    string `json:"party"`
		Start    string `json:"start"`
		End      string `json:"end"`
	} `json:"terms"`
}

// IngestLegislators pulls the current roster + committee assignments
// and upserts them. Run quarterly. Skips no rows on the first pass —
// the roster is small (~535 active).
func (s *Service) IngestLegislators(ctx context.Context) (legCount, commCount int, retErr error) {
	t0 := time.Now()
	slog.Info("signals: legislator refresh started")
	defer func() {
		slog.Info("signals: legislator refresh finished",
			"legislators", legCount, "committee_rows", commCount,
			"took", time.Since(t0).Round(time.Millisecond))
	}()

	client := &http.Client{Timeout: 60 * time.Second}

	// 1. Roster.
	var roster []rosterEntry
	if err := getJSON(ctx, client, rosterURL, &roster); err != nil {
		return 0, 0, fmt.Errorf("fetch roster: %w", err)
	}
	// Track bioguide → DB id for the second pass.
	idByBioguide := map[string]int64{}
	for _, e := range roster {
		if e.ID.Bioguide == "" || len(e.Terms) == 0 {
			continue
		}
		// Use most-recent term for chamber/state/district/party.
		last := e.Terms[len(e.Terms)-1]
		chamber := "house"
		if last.Type == "sen" {
			chamber = "senate"
		}
		fullName := strings.TrimSpace(e.Name.Official)
		if fullName == "" {
			fullName = strings.TrimSpace(e.Name.First + " " + e.Name.Last)
		}
		district := ""
		if chamber == "house" && last.District > 0 {
			district = fmt.Sprintf("%d", last.District)
		}
		res, err := s.DB.ExecContext(ctx, `
			INSERT INTO legislators (full_name, chamber, party, state, district, bioguide_id, active)
			VALUES (?, ?, ?, ?, ?, ?, 1)
			ON CONFLICT(bioguide_id) DO UPDATE SET
				full_name=excluded.full_name,
				chamber=excluded.chamber,
				party=excluded.party,
				state=excluded.state,
				district=excluded.district,
				active=1,
				last_refreshed=CURRENT_TIMESTAMP`,
			fullName, chamber, nullStr(last.Party), last.State, nullStr(district), e.ID.Bioguide)
		if err != nil {
			slog.Warn("signals: upsert legislator", "bioguide", e.ID.Bioguide, "err", err)
			continue
		}
		// id may come from update (LastInsertId is 0 in that case).
		id, _ := res.LastInsertId()
		if id == 0 {
			_ = s.DB.QueryRowContext(ctx,
				`SELECT id FROM legislators WHERE bioguide_id=?`, e.ID.Bioguide).Scan(&id)
		}
		if id > 0 {
			idByBioguide[e.ID.Bioguide] = id
			legCount++
		}
	}

	// 2. Committee assignments. Shape:
	//   { "HSAS": [ { "name": "...", "bioguide": "...", "title": "Chair" }, ... ], ... }
	var committees map[string][]struct {
		Bioguide string `json:"bioguide"`
		Name     string `json:"name"`
		Title    string `json:"title"`
	}
	if err := getJSON(ctx, client, committeesURL, &committees); err != nil {
		// Roster already saved — log + return what we have.
		slog.Warn("signals: committee fetch failed", "err", err)
		return legCount, 0, fmt.Errorf("fetch committees: %w", err)
	}

	// Clear existing assignments so renames/departures don't linger.
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM committee_assignments`); err != nil {
		return legCount, 0, fmt.Errorf("clear committee_assignments: %w", err)
	}

	for code, members := range committees {
		lowerCode := strings.ToLower(code)
		for _, m := range members {
			legID, ok := idByBioguide[m.Bioguide]
			if !ok {
				continue
			}
			role := strings.TrimSpace(m.Title)
			if _, err := s.DB.ExecContext(ctx, `
				INSERT OR IGNORE INTO committee_assignments
				  (legislator_id, committee_code, committee_name, role)
				VALUES (?, ?, ?, ?)`,
				legID, lowerCode, lowerCode /* name not in feed; fallback to code */, nullStr(role)); err == nil {
				commCount++
			}
		}
	}
	return legCount, commCount, nil
}

func getJSON(ctx context.Context, client *http.Client, url string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "FT-Dashboard fin@curranhouse.dev")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}
