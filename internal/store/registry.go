package store

import (
	"context"
	"database/sql"
	"fmt"
)

// SC-28 Registry / Document-Control — read-only projection over the four live
// source tables (theses_index, crypto_theses, sector_scorecards,
// crypto_adapters). There is NO Registry table and no cache (D28.2): this
// composes from source on every call, so a source edit (version bump, status
// flip) shows on the next read with zero sync step. Field-name divergence
// across the sources is normalised here, server-side (D28.4 / S-28b).

// RegistryArtifact is one normalised index row.
type RegistryArtifact struct {
	Group         string `json:"group"`      // theses | scorecards_adapters | framework
	Artifact      string `json:"artifact"`   // ticker/symbol/code/slug (the id within Source)
	Name          string `json:"name"`       // company / coin / display name ("" if none)
	AssetClass    string `json:"assetClass"` // stock | crypto | cross-asset
	Version       string `json:"version"`
	Status        string `json:"status"`      // raw source status (locked|draft|needs-review|superseded)
	LastUpdated   string `json:"lastUpdated"` // YYYY-MM-DD, "" when the source carries no date
	Source        string `json:"source"`      // table#id pointer (D28.11 fallback, C3=a)
	Score         *int   `json:"score,omitempty"`
	MaxScore      *int   `json:"maxScore,omitempty"`
	Band          string `json:"band,omitempty"`
	NextReview    string `json:"nextReview,omitempty"`
	GithubURL     string `json:"githubUrl,omitempty"`
	ScorecardType string `json:"scorecardType,omitempty"`
}

// registryFrameworkCodes routes these sector_scorecards rows to the Framework &
// Governance group (D28.7); every other sector_scorecards row is a Group-2
// Scorecard/Adapter. Route on code, never on table name (S-28e).
var registryFrameworkCodes = map[string]bool{
	"master-spec": true,
	"philosophy":  true,
	"asset-hedge": true,
}

// RegistryArtifacts composes the full artifact index from the four sources.
func (s *Store) RegistryArtifacts(ctx context.Context) ([]RegistryArtifact, error) {
	out := make([]RegistryArtifact, 0, 128)

	// 1) Stock theses (theses_index) — ALL rows incl. superseded (D28.10).
	rows, err := s.DB.QueryContext(ctx, `
		SELECT ticker, COALESCE(company_name,''), version, score, max_score,
		       status, COALESCE(locked_date,''), COALESCE(github_url,'')
		  FROM theses_index`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var ticker, name, status, locked, github string
		var version, maxScore int
		var score sql.NullInt64
		if err := rows.Scan(&ticker, &name, &version, &score, &maxScore, &status, &locked, &github); err != nil {
			rows.Close()
			return nil, err
		}
		mx := maxScore
		a := RegistryArtifact{
			Group: "theses", Artifact: ticker, Name: name, AssetClass: "stock",
			Version: fmt.Sprintf("v%d", version), Status: status, LastUpdated: locked,
			Source: "theses_index#" + ticker, MaxScore: &mx, GithubURL: github,
		}
		if score.Valid {
			v := int(score.Int64)
			a.Score = &v
		}
		out = append(out, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 2) Crypto theses (crypto_theses).
	rows, err = s.DB.QueryContext(ctx, `
		SELECT coin_symbol, COALESCE(coin_name,''), COALESCE(version,''),
		       total_score, max_score, COALESCE(band,''), status,
		       COALESCE(strftime('%Y-%m-%d', locked_at, 'unixepoch'), ''),
		       COALESCE(strftime('%Y-%m-%d', next_review_at, 'unixepoch'), '')
		  FROM crypto_theses`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var sym, name, version, band, status, locked, nextRev string
		var score, maxScore int
		if err := rows.Scan(&sym, &name, &version, &score, &maxScore, &band, &status, &locked, &nextRev); err != nil {
			rows.Close()
			return nil, err
		}
		sc, mx := score, maxScore
		out = append(out, RegistryArtifact{
			Group: "theses", Artifact: sym, Name: name, AssetClass: "crypto",
			Version: version, Status: status, LastUpdated: locked,
			Source: "crypto_theses#" + sym, Score: &sc, MaxScore: &mx, Band: band, NextReview: nextRev,
		})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 3) Sector scorecards (sector_scorecards) — framework codes → group 3,
	//    all others → group 2 (D28.7 / S-28e). Group-2 sector adapters carry
	//    the 8-Q /16 operating-stock framework as their scorecard type.
	rows, err = s.DB.QueryContext(ctx, `
		SELECT code, COALESCE(display_name,''), current_version, status,
		       COALESCE(strftime('%Y-%m-%d', updated_at, 'unixepoch'), '')
		  FROM sector_scorecards`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var code, name, version, status, updated string
		if err := rows.Scan(&code, &name, &version, &status, &updated); err != nil {
			rows.Close()
			return nil, err
		}
		a := RegistryArtifact{
			Artifact: code, Name: name, Version: version, Status: status,
			LastUpdated: updated, Source: "sector_scorecards#" + code,
		}
		if registryFrameworkCodes[code] {
			a.Group = "framework"
			a.AssetClass = "cross-asset"
		} else {
			a.Group = "scorecards_adapters"
			a.AssetClass = "stock"
			a.ScorecardType = "8-Q /16"
		}
		out = append(out, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 4) Crypto adapters (crypto_adapters) → group 2.
	rows, err = s.DB.QueryContext(ctx, `
		SELECT slug, COALESCE(display_name,''), current_version, status,
		       COALESCE(scorecard_type,''),
		       COALESCE(strftime('%Y-%m-%d', updated_at, 'unixepoch'), '')
		  FROM crypto_adapters`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var slug, name, version, status, sctype, updated string
		if err := rows.Scan(&slug, &name, &version, &status, &sctype, &updated); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, RegistryArtifact{
			Group: "scorecards_adapters", Artifact: slug, Name: name, AssetClass: "crypto",
			Version: version, Status: status, LastUpdated: updated,
			Source: "crypto_adapters#" + slug, ScorecardType: sctype,
		})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return out, nil
}
