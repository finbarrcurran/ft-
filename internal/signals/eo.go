package signals

// Spec 9k §D5 — Executive Order ingestion from the Federal Register.
//
// Source (free, JSON):
//   https://www.federalregister.gov/api/v1/documents.json
//     ?conditions[type]=PRESDOCU
//     &conditions[presidential_document_type]=executive_order
//     &conditions[publication_date][gte]=YYYY-MM-DD
//
// Two passes per matched EO:
//   1. Sector pass — substring-match (case-insensitive) the EO title +
//      abstract against eo_sector_keywords. One row per matching
//      sector_universe_id, ticker NULL.
//   2. Company-name pass — for each currently-held ticker, substring-
//      match its company name against EO title + abstract. If hit,
//      one additional row with the ticker set and 'eo_names_company'
//      appended to alarm_reasons.
//
// Tier (Spec §D6 EO tier):
//   ALARM if alarm_reasons contains 'eo_names_company'
//   FLAG  if sector_universe_id maps to an active holding/watchlist
//   INFO  otherwise

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	federalRegisterURL = "https://www.federalregister.gov/api/v1/documents.json"
	eoLookback         = 90 * 24 * time.Hour
)

type frDocument struct {
	DocumentNumber  string `json:"document_number"`
	Title           string `json:"title"`
	Abstract        string `json:"abstract"`
	PublicationDate string `json:"publication_date"`
	SigningDate     string `json:"signing_date"`
	ExecutiveOrder  *struct {
		ExecutiveOrderNumber int    `json:"executive_order_number"`
		NotesOnRescission    string `json:"notes_on_rescission"`
	} `json:"executive_order_notes"`
	HTMLURL string `json:"html_url"`
}

type frResponse struct {
	Results []frDocument `json:"results"`
	Count   int          `json:"count"`
}

// IngestEOs runs the keyword pass + company-name pass and inserts
// matched signal_events rows. Re-runs are idempotent via the
// (signal_type, source, source_id) UNIQUE constraint.
func (s *Service) IngestEOs(ctx context.Context) (inserted int, retErr error) {
	t0 := time.Now()
	slog.Info("signals: EO ingest started")
	defer func() {
		slog.Info("signals: EO ingest finished",
			"inserted", inserted, "took", time.Since(t0).Round(time.Millisecond))
	}()

	docs, err := fetchEOs(ctx, eoLookback)
	if err != nil {
		return 0, err
	}
	slog.Info("signals: EOs fetched", "count", len(docs))

	// Load keyword map once.
	keywords, err := s.loadEOKeywords(ctx)
	if err != nil {
		return 0, err
	}
	// Load currently held + watched company names once.
	holdings, err := s.loadHoldingNames(ctx)
	if err != nil {
		return 0, err
	}

	for _, d := range docs {
		select {
		case <-ctx.Done():
			return inserted, ctx.Err()
		default:
		}

		eventDate := d.SigningDate
		if eventDate == "" {
			eventDate = d.PublicationDate
		}
		filedDate := d.PublicationDate
		if filedDate == "" {
			filedDate = time.Now().UTC().Format("2006-01-02")
		}

		haystack := strings.ToLower(d.Title + " \n " + d.Abstract)

		// Sector pass: one row per matching sector.
		matchedSectors := map[int64]bool{}
		for _, kw := range keywords {
			if strings.Contains(haystack, kw.Keyword) {
				matchedSectors[kw.SectorID] = true
			}
		}
		for sectorID := range matchedSectors {
			tier := "info"
			reasons := []string{}
			// FLAG if this sector has any active holding/watchlist match.
			if s.sectorHasActiveHolding(ctx, sectorID) {
				tier = "flag"
				reasons = append(reasons, "sector_match")
			}
			sourceID := fmt.Sprintf("%s|sector:%d", d.DocumentNumber, sectorID)
			res, err := s.DB.ExecContext(ctx, `
				INSERT OR IGNORE INTO signal_events
				  (signal_type, tier, event_date, filed_date,
				   sector_universe_id, source, source_url, source_id,
				   notes, alarm_reasons)
				VALUES ('executive_order', ?, ?, ?, ?, 'fed_reg', ?, ?, ?, ?)`,
				tier, eventDate, filedDate, sectorID,
				nullStr(d.HTMLURL), sourceID, nullStr(d.Title), reasonsToJSON(reasons))
			if err != nil {
				slog.Warn("signals: insert EO sector", "doc", d.DocumentNumber, "err", err)
				continue
			}
			if n, _ := res.RowsAffected(); n > 0 {
				inserted++
			}
		}

		// Company-name pass: one row per matched ticker.
		for _, h := range holdings {
			if h.Name == "" {
				continue
			}
			if !strings.Contains(haystack, strings.ToLower(h.Name)) {
				continue
			}
			reasons := []string{"eo_names_company"}
			sourceID := fmt.Sprintf("%s|ticker:%s", d.DocumentNumber, h.Ticker)
			res, err := s.DB.ExecContext(ctx, `
				INSERT OR IGNORE INTO signal_events
				  (signal_type, tier, event_date, filed_date,
				   ticker, issuer_name, sector_universe_id,
				   source, source_url, source_id, notes, alarm_reasons)
				VALUES ('executive_order', 'alarm', ?, ?, ?, ?, ?,
				        'fed_reg', ?, ?, ?, ?)`,
				eventDate, filedDate,
				h.Ticker, nullStr(h.Name), nullInt64(h.SectorID),
				nullStr(d.HTMLURL), sourceID, nullStr(d.Title),
				reasonsToJSON(reasons))
			if err != nil {
				slog.Warn("signals: insert EO company", "ticker", h.Ticker, "err", err)
				continue
			}
			if n, _ := res.RowsAffected(); n > 0 {
				inserted++
			}
		}
	}
	return inserted, nil
}

func fetchEOs(ctx context.Context, lookback time.Duration) ([]frDocument, error) {
	gte := time.Now().UTC().Add(-lookback).Format("2006-01-02")
	q := url.Values{}
	q.Set("conditions[type]", "PRESDOCU")
	q.Set("conditions[presidential_document_type]", "executive_order")
	q.Set("conditions[publication_date][gte]", gte)
	q.Set("per_page", "200")
	q.Set("fields[]", "document_number")
	q.Add("fields[]", "title")
	q.Add("fields[]", "abstract")
	q.Add("fields[]", "publication_date")
	q.Add("fields[]", "signing_date")
	q.Add("fields[]", "html_url")
	full := federalRegisterURL + "?" + q.Encode()

	client := &http.Client{Timeout: 60 * time.Second}
	var resp frResponse
	if err := getJSON(ctx, client, full, &resp); err != nil {
		return nil, fmt.Errorf("federal register: %w", err)
	}
	return resp.Results, nil
}

type eoKeyword struct {
	Keyword  string
	SectorID int64
}

func (s *Service) loadEOKeywords(ctx context.Context) ([]eoKeyword, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT lower(keyword), sector_universe_id FROM eo_sector_keywords`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []eoKeyword
	for rows.Next() {
		var k eoKeyword
		if err := rows.Scan(&k.Keyword, &k.SectorID); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

type holdingName struct {
	Ticker   string
	Name     string
	SectorID int64
}

func (s *Service) loadHoldingNames(ctx context.Context) ([]holdingName, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT UPPER(ticker), name, COALESCE(sector_universe_id, 0)
		  FROM stock_holdings
		 WHERE (deleted_at IS NULL OR deleted_at = 0)
		   AND ticker IS NOT NULL AND ticker <> ''
		UNION
		SELECT UPPER(ticker), COALESCE(company_name, ''), COALESCE(sector_universe_id, 0)
		  FROM watchlist
		 WHERE deleted_at IS NULL
		   AND kind = 'stock'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []holdingName
	for rows.Next() {
		var h holdingName
		if err := rows.Scan(&h.Ticker, &h.Name, &h.SectorID); err != nil {
			return nil, err
		}
		// Skip very short company names that would generate huge false-positive risk.
		if len(strings.TrimSpace(h.Name)) < 4 {
			continue
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (s *Service) sectorHasActiveHolding(ctx context.Context, sectorID int64) bool {
	var n int
	_ = s.DB.QueryRowContext(ctx, `
		SELECT (
			SELECT COUNT(*) FROM stock_holdings
			 WHERE sector_universe_id = ?
			   AND (deleted_at IS NULL OR deleted_at = 0)
		) + (
			SELECT COUNT(*) FROM watchlist
			 WHERE sector_universe_id = ?
			   AND deleted_at IS NULL
			   AND kind = 'stock'
		)`, sectorID, sectorID).Scan(&n)
	return n > 0
}
