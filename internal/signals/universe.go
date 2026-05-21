// Package signals implements Spec 9k — Political & Insider Signal Tab.
//
// Three signal classes (insider Form 4, US Congress trades, US Executive
// Orders) filtered to FT's universe (holdings + watchlist + sector ETFs)
// and classified into INFO / FLAG / ALARM tiers. v1.10.0 (Phase A) ships
// the insider-only MVP; Congress + EO + UI polish in v1.11.0 / v1.12.0.
package signals

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"time"
)

// UniverseHit is the result of an InUniverse check.
type UniverseHit struct {
	Matched           bool
	Source            string // 'holding' | 'watchlist' | 'sector_etf'
	SectorUniverseID  *int64 // populated for holdings/watchlist via sector_universe_id, or for sector_etf direct
}

// Service is the shared signals service handle. Owns DB access + the
// in-memory universe cache.
type Service struct {
	DB *sql.DB

	uniMu     sync.RWMutex
	uniCache  map[string]UniverseHit // ticker (uppercase) → hit
	uniLoaded time.Time

	ingestMu      sync.Mutex
	ingestRunning bool
}

// TryStartInsiderIngest returns true if it claimed the ingest slot, false
// if another ingest is already running. The caller is then responsible for
// kicking off the goroutine and releasing the slot via FinishInsiderIngest.
func (s *Service) TryStartInsiderIngest() bool {
	s.ingestMu.Lock()
	defer s.ingestMu.Unlock()
	if s.ingestRunning {
		return false
	}
	s.ingestRunning = true
	return true
}

// FinishInsiderIngest releases the lock. Always pair with TryStartInsiderIngest.
func (s *Service) FinishInsiderIngest() {
	s.ingestMu.Lock()
	s.ingestRunning = false
	s.ingestMu.Unlock()
}

// InsiderIngestRunning reports whether a manual or cron ingest is in flight.
func (s *Service) InsiderIngestRunning() bool {
	s.ingestMu.Lock()
	defer s.ingestMu.Unlock()
	return s.ingestRunning
}

const uniTTL = 30 * time.Minute

// New constructs a Service.
func New(db *sql.DB) *Service {
	return &Service{DB: db}
}

// InvalidateUniverse drops the cached universe membership. Called from
// callers that mutate stock_holdings / watchlist / sector_universe.
// Currently called from the daily-snapshot path and the manual refresh
// endpoint; we don't yet hook into every holding mutation but a stale
// cache only delays new-ticker pickup by ≤30 min.
func (s *Service) InvalidateUniverse() {
	s.uniMu.Lock()
	defer s.uniMu.Unlock()
	s.uniCache = nil
	s.uniLoaded = time.Time{}
}

// InUniverse reports whether the given ticker is one of:
//   - active stock_holdings.ticker
//   - active watchlist.ticker
//   - sector_universe.etf_ticker_primary / etf_ticker_secondary
//
// Case-insensitive. Returns Source = "holding" | "watchlist" |
// "sector_etf" and the linked sector_universe_id where present.
//
// Backed by a 30-min in-memory cache. Cold first call costs one round
// trip to the DB; subsequent calls are O(1) map lookup.
func (s *Service) InUniverse(ctx context.Context, ticker string) UniverseHit {
	tk := strings.ToUpper(strings.TrimSpace(ticker))
	if tk == "" {
		return UniverseHit{}
	}
	s.uniMu.RLock()
	if s.uniCache != nil && time.Since(s.uniLoaded) < uniTTL {
		hit, ok := s.uniCache[tk]
		s.uniMu.RUnlock()
		if ok {
			return hit
		}
		return UniverseHit{}
	}
	s.uniMu.RUnlock()

	// Cold or stale — reload.
	if err := s.reloadUniverse(ctx); err != nil {
		return UniverseHit{} // best-effort; signal_events still records the event as INFO
	}
	s.uniMu.RLock()
	defer s.uniMu.RUnlock()
	hit, ok := s.uniCache[tk]
	if !ok {
		return UniverseHit{}
	}
	return hit
}

// reloadUniverse pulls the full membership in one pass per source. Order
// of precedence (most→least specific): holding > watchlist > sector_etf.
// A ticker held AND on the watchlist gets `source=holding`.
func (s *Service) reloadUniverse(ctx context.Context) error {
	cache := map[string]UniverseHit{}

	put := func(tk string, source string, sectorID *int64) {
		tk = strings.ToUpper(strings.TrimSpace(tk))
		if tk == "" {
			return
		}
		existing, ok := cache[tk]
		if ok {
			// Don't downgrade an already-recorded holding to watchlist/etf.
			if rank(existing.Source) >= rank(source) {
				return
			}
		}
		cache[tk] = UniverseHit{Matched: true, Source: source, SectorUniverseID: sectorID}
	}

	// 1. stock_holdings (highest precedence).
	rows, err := s.DB.QueryContext(ctx, `
		SELECT ticker, sector_universe_id
		  FROM stock_holdings
		 WHERE deleted_at IS NULL AND ticker IS NOT NULL AND ticker != ''`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var ticker string
		var suID sql.NullInt64
		if err := rows.Scan(&ticker, &suID); err != nil {
			rows.Close()
			return err
		}
		var sid *int64
		if suID.Valid {
			v := suID.Int64
			sid = &v
		}
		put(ticker, "holding", sid)
	}
	rows.Close()

	// 2. watchlist.
	rows, err = s.DB.QueryContext(ctx, `
		SELECT ticker, sector_universe_id
		  FROM watchlist
		 WHERE deleted_at IS NULL AND kind = 'stock' AND ticker IS NOT NULL AND ticker != ''`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var ticker string
		var suID sql.NullInt64
		if err := rows.Scan(&ticker, &suID); err != nil {
			rows.Close()
			return err
		}
		var sid *int64
		if suID.Valid {
			v := suID.Int64
			sid = &v
		}
		put(ticker, "watchlist", sid)
	}
	rows.Close()

	// 3. sector_universe ETFs — primary + secondary.
	rows, err = s.DB.QueryContext(ctx, `
		SELECT id, etf_ticker_primary, etf_ticker_secondary
		  FROM sector_universe
		 WHERE active = 1`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var primary, secondary sql.NullString
		if err := rows.Scan(&id, &primary, &secondary); err != nil {
			rows.Close()
			return err
		}
		sid := id
		if primary.Valid && primary.String != "" {
			put(primary.String, "sector_etf", &sid)
		}
		if secondary.Valid && secondary.String != "" {
			put(secondary.String, "sector_etf", &sid)
		}
	}
	rows.Close()

	s.uniMu.Lock()
	s.uniCache = cache
	s.uniLoaded = time.Now()
	s.uniMu.Unlock()
	return nil
}

// rank gives source-precedence weight. Higher = more specific.
func rank(src string) int {
	switch src {
	case "holding":
		return 3
	case "watchlist":
		return 2
	case "sector_etf":
		return 1
	}
	return 0
}

// UniverseSnapshot is a debug dump of the current universe. Returned by
// GET /api/signals/universe so the user can verify what's matched.
type UniverseSnapshot struct {
	Tickers   map[string]string `json:"tickers"`   // ticker → source
	Count     int               `json:"count"`
	LoadedAt  time.Time         `json:"loadedAt"`
}

// Snapshot returns a debug view of the universe cache (used by the
// /api/signals/universe debug endpoint).
func (s *Service) Snapshot(ctx context.Context) UniverseSnapshot {
	if s.uniCache == nil || time.Since(s.uniLoaded) >= uniTTL {
		_ = s.reloadUniverse(ctx)
	}
	s.uniMu.RLock()
	defer s.uniMu.RUnlock()
	out := UniverseSnapshot{
		Tickers:  make(map[string]string, len(s.uniCache)),
		Count:    len(s.uniCache),
		LoadedAt: s.uniLoaded,
	}
	for k, v := range s.uniCache {
		out.Tickers[k] = v.Source
	}
	return out
}
