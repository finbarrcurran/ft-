// Spec 4: Watchlist + Framework Scoring handlers.
//
// Endpoints:
//
//	GET    /api/watchlist                          list active entries (with latest score)
//	POST   /api/watchlist                          create entry
//	PUT    /api/watchlist/{id}                     update editable fields
//	DELETE /api/watchlist/{id}                     soft-delete
//	POST   /api/watchlist/{id}/promote             promote to holdings
//	GET    /api/frameworks                         list all loaded frameworks
//	GET    /api/frameworks/{id}                    one framework definition
//	GET    /api/scores?targetKind=&targetId=       latest score for a target
//	GET    /api/scores/history?...                 full history
//	POST   /api/scores                             append a new score
//
// All require cookie auth (no bearer-token surface).

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"ft/internal/domain"
	"ft/internal/frameworks"
	"ft/internal/market"
	"ft/internal/marketdata"
	"ft/internal/regime"
	"ft/internal/store"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ----- watchlist ---------------------------------------------------------

type watchlistReq struct {
	Ticker          string   `json:"ticker"`
	Kind            string   `json:"kind"` // "stock" | "crypto"
	CompanyName     *string  `json:"companyName"`
	Sector          *string  `json:"sector"`
	CurrentPrice    *float64 `json:"currentPrice"`
	TargetEntryLow  *float64 `json:"targetEntryLow"`
	TargetEntryHigh *float64 `json:"targetEntryHigh"`
	ThesisLink      *string  `json:"thesisLink"`
	Note            *string  `json:"note"`
	// Spec 9f D9 — Whitespace "+ watchlist" affordance preselects the
	// sector_universe row when adding from the rotation tab.
	SectorUniverseID *int64 `json:"sectorUniverseId,omitempty"`
	// SC-31 — manual analyst-target override (stock only). ForecastManual
	// true = set the supplied Bear/Base/Bull(+median) and flip the row to
	// source='manual' (daily cron then skips it); false = revert to auto
	// (source='yahoo', next run repopulates); nil = leave forecast untouched.
	ForecastManual *bool    `json:"forecastManual,omitempty"`
	ForecastLow    *float64 `json:"forecastLow,omitempty"`
	ForecastMean   *float64 `json:"forecastMean,omitempty"`
	ForecastHigh   *float64 `json:"forecastHigh,omitempty"`
	ForecastMedian *float64 `json:"forecastMedian,omitempty"`
}

type watchlistRow struct {
	*domain.WatchlistEntry
	LatestScore *domain.FrameworkScore `json:"latestScore,omitempty"`

	// SC-14 — thesis-preferred SCORE, mirroring the Stocks/Crypto tabs.
	// Locked thesis (theses_index for stocks / crypto_theses for crypto)
	// wins over the legacy manual framework_scores; nil when neither exists.
	// The frontend renders this via the shared scoreCell() helper.
	Score *scoreSummary `json:"score,omitempty"`

	// Spec 9b D6: entry-zone state per regime.
	//   InRange       — current price falls within [low, high]
	//   AlertActive   — entry-zone alerts permitted (effective regime == stable)
	//   AlertSuppressed — InRange && NOT AlertActive — UI shows
	//                     "in range (suppressed)" badge
	InRange         bool `json:"inRange"`
	AlertActive     bool `json:"alertActive"`
	AlertSuppressed bool `json:"alertSuppressed"`

	// v1.5: per-row market open/closed badge (matches Stocks tab). Resolved
	// by ticker suffix via marketdata.ExchangeForTicker. nil for crypto and
	// unknown exchanges.
	Market *marketdata.MarketStatus `json:"market,omitempty"`
}

// GET /api/watchlist
func (s *Server) handleListWatchlist(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	entries, err := s.store.ListWatchlist(r.Context(), userID)
	if mapStoreError(w, err) {
		return
	}
	// Pull latest score per entry in one trip.
	ids := make([]int64, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.ID)
	}
	scoreByID, _ := s.store.LatestFrameworkScoresMany(r.Context(), userID, "watchlist", ids)
	// SC-14 — overlay locked-thesis scores so the Watchlist SCORE column
	// tracks the same source of truth as the Stocks/Crypto tabs. Stocks read
	// theses_index by ticker; crypto reads crypto_theses by symbol. Thesis
	// wins over the (now-empty) manual framework_scores via pickScore.
	stockTickers := make([]string, 0, len(entries))
	cryptoSymbols := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Kind == "crypto" {
			cryptoSymbols = append(cryptoSymbols, e.Ticker)
		} else {
			stockTickers = append(stockTickers, e.Ticker)
		}
	}
	thesisByTicker, _ := s.store.ThesisScoresByTicker(r.Context(), stockTickers)
	cryptoThesisBySymbol, _ := s.store.CryptoThesisScoresBySymbol(r.Context(), cryptoSymbols)
	eff := s.currentEffectiveRegime(r.Context())
	alertsActive := regime.GatesWatchlistEntryZone(eff)

	now := time.Now().UTC()
	out := make([]watchlistRow, 0, len(entries))
	for _, e := range entries {
		inRange := isInEntryZone(e)
		// v1.5: resolve per-row market for stock entries via ticker suffix.
		// (Watchlist has no exchange_override column — suffix rule only.)
		var marketPtr *marketdata.MarketStatus
		if e.Kind == "stock" {
			if exch := marketdata.ExchangeForTicker(e.Ticker); exch != "" {
				st := marketdata.Status(exch, now)
				if st.Exchange != "" {
					marketPtr = &st
				}
			}
		}
		// SC-14 — thesis-preferred score (manual framework_scores as fallback).
		var score *scoreSummary
		if e.Kind == "crypto" {
			score = pickCryptoScore(scoreByID[e.ID], cryptoThesisBySymbol, e.Ticker)
		} else {
			score = pickScore(scoreByID[e.ID], thesisByTicker, e.Ticker)
		}
		out = append(out, watchlistRow{
			WatchlistEntry:  e,
			LatestScore:     scoreByID[e.ID],
			Score:           score,
			InRange:         inRange,
			AlertActive:     inRange && alertsActive,
			AlertSuppressed: inRange && !alertsActive,
			Market:          marketPtr,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"watchlist": out})
}

// isInEntryZone reports whether the entry's current price falls inside the
// [target_entry_low, target_entry_high] band. Returns false if any required
// field is missing.
func isInEntryZone(e *domain.WatchlistEntry) bool {
	if e.CurrentPrice == nil {
		return false
	}
	p := *e.CurrentPrice
	if e.TargetEntryLow != nil && p < *e.TargetEntryLow {
		return false
	}
	if e.TargetEntryHigh != nil && p > *e.TargetEntryHigh {
		return false
	}
	// At least one bound must be set to count as "in range".
	return e.TargetEntryLow != nil || e.TargetEntryHigh != nil
}

// POST /api/watchlist
func (s *Server) handleCreateWatchlist(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	var req watchlistReq
	if !decodeJSON(r, w, &req) {
		return
	}
	if strings.TrimSpace(req.Ticker) == "" {
		writeError(w, http.StatusBadRequest, "ticker is required")
		return
	}
	if req.Kind != "stock" && req.Kind != "crypto" {
		writeError(w, http.StatusBadRequest, "kind must be stock or crypto")
		return
	}
	if req.TargetEntryLow != nil && req.TargetEntryHigh != nil &&
		*req.TargetEntryLow > *req.TargetEntryHigh {
		writeError(w, http.StatusBadRequest, "targetEntryLow must be ≤ targetEntryHigh")
		return
	}

	e := &domain.WatchlistEntry{
		UserID:           userID,
		Ticker:           strings.ToUpper(strings.TrimSpace(req.Ticker)),
		Kind:             req.Kind,
		CompanyName:      trimStrPtrW(req.CompanyName),
		Sector:           trimStrPtrW(req.Sector),
		CurrentPrice:     req.CurrentPrice,
		TargetEntryLow:   req.TargetEntryLow,
		TargetEntryHigh:  req.TargetEntryHigh,
		ThesisLink:       trimStrPtrW(req.ThesisLink),
		Note:             trimStrPtrW(req.Note),
		SectorUniverseID: req.SectorUniverseID, // Spec 9f D9
	}
	// SC-16 — enrich a manually-added row (company name + price + free-text
	// sector) so bare ticker-adds don't land blank. Enrich-and-flag: fills
	// only the fields the caller left empty (non-destructive); never guesses
	// a sector_universe_id, so unsectored rows surface in SC-15's "Unsectored"
	// group with a "needs sector" hint, and rows the provider couldn't price
	// show "needs data". Best-effort — provider failure leaves fields blank.
	s.enrichWatchlistEntry(r.Context(), e)
	created, err := s.store.CreateWatchlistEntry(r.Context(), e)
	if err != nil {
		mapStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": created.ID})
}

// enrichWatchlistEntry fills missing company name / current price / free-text
// sector for a manually-added row using the providers FT already uses
// elsewhere (Yahoo profile + the stock-quote chain; CoinGecko for crypto).
// SC-16 (D16.1/D16.2):
//   - Non-destructive: only blank fields are filled; caller-supplied values
//     (e.g. from the Edit-modal lookup) are never overwritten.
//   - Never guesses sector_universe_id — the free-text GICS string Yahoo
//     returns doesn't map 1:1 to the 34-row taxonomy, so the row stays
//     "Unsectored" until the user assigns it (surfaced by SC-15).
//   - Best-effort: provider errors/timeouts leave fields blank so the UI can
//     flag "needs data"; they never block the create.
func (s *Server) enrichWatchlistEntry(ctx context.Context, e *domain.WatchlistEntry) {
	if e.CompanyName != nil && e.CurrentPrice != nil && e.Sector != nil {
		return // nothing to fill
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	if e.Kind == "crypto" {
		// CoinGecko profile: name + category (no free synchronous EUR price
		// source here, so CurrentPrice stays nil → UI flags "needs data").
		if p, err := market.LookupCryptoBySymbol(ctx, e.Ticker); err == nil && p != nil {
			if e.CompanyName == nil && p.Name != "" {
				e.CompanyName = ptrString(p.Name)
			}
			if e.Sector == nil && p.Category != "" {
				e.Sector = ptrString(p.Category)
			}
		}
		return
	}

	// Stock: name + free-text sector from the Yahoo profile.
	if e.CompanyName == nil || e.Sector == nil {
		if p, err := market.FetchYahooProfile(ctx, e.Ticker); err == nil && p != nil {
			if e.CompanyName == nil && p.Name != "" {
				e.CompanyName = ptrString(p.Name)
			}
			if e.Sector == nil && p.Sector != "" {
				e.Sector = ptrString(p.Sector)
			}
		}
	}
	// Current price from the same quote chain the rest of FT uses
	// (Finnhub → TwelveData → Yahoo).
	if e.CurrentPrice == nil {
		if q, err := market.FetchStockQuote(ctx, e.Ticker); err == nil && q != nil {
			if q.Price > 0 {
				v := q.Price
				e.CurrentPrice = &v
			}
			if e.CompanyName == nil && q.Name != "" {
				e.CompanyName = ptrString(q.Name)
			}
		}
	}
}

// PUT /api/watchlist/{id}
func (s *Server) handleUpdateWatchlist(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	existing, err := s.store.GetWatchlistEntry(r.Context(), userID, id)
	if mapStoreError(w, err) {
		return
	}
	var req watchlistReq
	if !decodeJSON(r, w, &req) {
		return
	}
	// Only the editable fields update; ticker/kind are immutable.
	existing.CompanyName = trimStrPtrW(req.CompanyName)
	existing.Sector = trimStrPtrW(req.Sector)
	existing.CurrentPrice = req.CurrentPrice
	existing.TargetEntryLow = req.TargetEntryLow
	existing.TargetEntryHigh = req.TargetEntryHigh
	existing.ThesisLink = trimStrPtrW(req.ThesisLink)
	existing.Note = trimStrPtrW(req.Note)
	if err := s.store.UpdateWatchlistEntry(r.Context(), existing); err != nil {
		mapStoreError(w, err)
		return
	}
	// SC-31 — manual analyst-target override (stock only). The forecast
	// columns are otherwise owned by the daily cron, so they're set through
	// dedicated calls that flip forecast_source rather than the generic update.
	if existing.Kind == "stock" && req.ForecastManual != nil {
		if *req.ForecastManual {
			_ = s.store.SetWatchlistForecastManual(r.Context(), userID, id,
				req.ForecastLow, req.ForecastMean, req.ForecastHigh, req.ForecastMedian)
		} else {
			_ = s.store.RevertWatchlistForecastAuto(r.Context(), userID, id)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// DELETE /api/watchlist/{id}
func (s *Server) handleSoftDeleteWatchlist(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.SoftDeleteWatchlistEntry(r.Context(), userID, id); err != nil {
		mapStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// POST /api/watchlist/{id}/promote
//
// Body:
//
//	{
//	  "investedUsd": 5000,        // stocks
//	  "avgOpenPrice": 120,        // stocks (optional)
//	  "stopLoss": 100,            // optional
//	  "takeProfit": 180,          // optional
//	  "category": "AI infra",     // stocks (optional)
//	  "quantityHeld": 0.5,        // crypto
//	  "avgBuyEur": 30000,         // crypto
//	  "costBasisEur": 15000,      // crypto
//	  "classification": "core",   // crypto
//	  "volTier": "low"            // crypto
//	}
//
// Creates the holding, copies the latest framework score over, soft-deletes
// the watchlist row, and writes a holdings_audit row.
func (s *Server) handlePromoteWatchlist(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	entry, err := s.store.GetWatchlistEntry(r.Context(), userID, id)
	if mapStoreError(w, err) {
		return
	}
	if entry.DeletedAt != nil {
		writeError(w, http.StatusBadRequest, "entry already deleted/promoted")
		return
	}

	var req struct {
		// stocks
		InvestedUSD  float64  `json:"investedUsd"`
		AvgOpenPrice *float64 `json:"avgOpenPrice"`
		StopLoss     *float64 `json:"stopLoss"`
		TakeProfit   *float64 `json:"takeProfit"`
		Category     *string  `json:"category"`
		// crypto
		QuantityHeld   float64 `json:"quantityHeld"`
		QuantityStaked float64 `json:"quantityStaked"`
		AvgBuyEUR      *float64 `json:"avgBuyEur"`
		CostBasisEUR   *float64 `json:"costBasisEur"`
		Classification string   `json:"classification"`
		VolTier        string   `json:"volTier"`
		// optional override
		Reason *string `json:"reason"`
	}
	if !decodeJSON(r, w, &req) {
		return
	}

	tx, err := s.store.DB.BeginTx(r.Context(), nil)
	if err != nil {
		mapStoreError(w, err)
		return
	}
	defer tx.Rollback()

	var holdingID int64
	if entry.Kind == "stock" {
		h := &domain.StockHolding{
			UserID:       userID,
			Name:         strOrFallback(entry.CompanyName, entry.Ticker),
			Ticker:       &entry.Ticker,
			Category:     req.Category,
			Sector:       entry.Sector,
			InvestedUSD:  req.InvestedUSD,
			AvgOpenPrice: req.AvgOpenPrice,
			CurrentPrice: entry.CurrentPrice,
			StopLoss:     req.StopLoss,
			TakeProfit:   req.TakeProfit,
			Note:         entry.Note,
			StrategyNote: "",
		}
		holdingID, err = s.store.InsertStockHoldingTx(r.Context(), tx, h)
		if err != nil {
			slog.Error("promote stock", "err", err)
			writeError(w, http.StatusInternalServerError, "promote failed")
			return
		}
	} else {
		// crypto
		classification := req.Classification
		if classification == "" {
			classification = "alt"
		}
		volTier := req.VolTier
		if volTier == "" {
			volTier = "medium"
		}
		c := &domain.CryptoHolding{
			UserID:         userID,
			Name:           strOrFallback(entry.CompanyName, entry.Ticker),
			Symbol:         entry.Ticker,
			Classification: classification,
			IsCore:         classification == "core",
			QuantityHeld:   req.QuantityHeld,
			QuantityStaked: req.QuantityStaked,
			AvgBuyEUR:      req.AvgBuyEUR,
			CostBasisEUR:   req.CostBasisEUR,
			Note:           entry.Note,
			VolTier:        volTier,
		}
		holdingID, err = s.store.InsertCryptoHoldingTx(r.Context(), tx, c)
		if err != nil {
			slog.Error("promote crypto", "err", err)
			writeError(w, http.StatusInternalServerError, "promote failed")
			return
		}
	}

	// Copy the latest watchlist score to the new holding (if any).
	latest, err := s.store.LatestFrameworkScore(r.Context(), userID, "watchlist", entry.ID)
	if err == nil && latest != nil {
		copyFS := &domain.FrameworkScore{
			UserID:       userID,
			TargetKind:   "holding",
			TargetID:     holdingID,
			FrameworkID:  latest.FrameworkID,
			TotalScore:   latest.TotalScore,
			MaxScore:     latest.MaxScore,
			Passes:       latest.Passes,
			ScoresJSON:   latest.ScoresJSON,
			TagsJSON:     latest.TagsJSON,
			ReviewerNote: ptrString(fmt.Sprintf("Promoted from watchlist #%d", entry.ID)),
		}
		if err := s.store.InsertFrameworkScoreTx(r.Context(), tx, copyFS); err != nil {
			slog.Warn("promote: copy score failed", "err", err)
		}
	}

	// Mark watchlist entry promoted + soft-deleted.
	if err := s.store.SetPromotedHoldingID(r.Context(), tx, userID, entry.ID, holdingID); err != nil {
		mapStoreError(w, err)
		return
	}

	if err := tx.Commit(); err != nil {
		mapStoreError(w, err)
		return
	}

	// Audit (out of tx — already-committed holding is the source of truth).
	reason := req.Reason
	if reason == nil {
		v := fmt.Sprintf("Promoted from watchlist #%d", entry.ID)
		reason = &v
	}
	_ = s.store.RecordAudit(r.Context(), userID, entry.Kind, holdingID,
		stockOrCryptoIdent(entry.Kind, entry.Ticker),
		stockOrCryptoIdent(invertKind(entry.Kind), entry.Ticker),
		store.AuditCreate,
		map[string]any{"promoted_from_watchlist_id": entry.ID, "ticker": entry.Ticker},
		reason)

	writeJSON(w, http.StatusOK, map[string]any{"holdingId": holdingID})
}

// ----- frameworks --------------------------------------------------------

// GET /api/frameworks
func (s *Server) handleListFrameworks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"frameworks": frameworks.All()})
}

// GET /api/frameworks/{id}
func (s *Server) handleGetFramework(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	f, ok := frameworks.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "framework not found")
		return
	}
	writeJSON(w, http.StatusOK, f)
}

// ----- framework_scores --------------------------------------------------

type scoreReq struct {
	TargetKind   string `json:"targetKind"`  // "holding" | "watchlist"
	TargetID     int64  `json:"targetId"`
	FrameworkID  string `json:"frameworkId"`
	Scores       map[string]struct {
		Score int    `json:"score"`
		Note  string `json:"note,omitempty"`
	} `json:"scores"`
	Tags         map[string]string `json:"tags,omitempty"`
	ReviewerNote *string           `json:"reviewerNote,omitempty"`
}

// POST /api/scores — append a new score row.
func (s *Server) handleCreateScore(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	var req scoreReq
	if !decodeJSON(r, w, &req) {
		return
	}
	if req.TargetKind != "holding" && req.TargetKind != "watchlist" {
		writeError(w, http.StatusBadRequest, "targetKind must be holding or watchlist")
		return
	}
	if req.TargetID == 0 {
		writeError(w, http.StatusBadRequest, "targetId required")
		return
	}
	fw, ok := frameworks.Get(req.FrameworkID)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown frameworkId")
		return
	}

	// Validate every score is one of {0,1,2} and the q.ID exists.
	// Arithmetic is unweighted: each question contributes 0/1/2 directly.
	// `weight` on the framework is metadata only (strong-signal badges in UI).
	total := 0
	for qid, v := range req.Scores {
		if _, ok := fw.QuestionByID(qid); !ok {
			writeError(w, http.StatusBadRequest, "unknown question id: "+qid)
			return
		}
		if v.Score < 0 || v.Score > 2 {
			writeError(w, http.StatusBadRequest, "score must be 0/1/2 for "+qid)
			return
		}
		total += v.Score
	}

	scoresJSON, err := json.Marshal(req.Scores)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "encode scores")
		return
	}
	var tagsJSONp *string
	if len(req.Tags) > 0 {
		b, err := json.Marshal(req.Tags)
		if err == nil {
			s := string(b)
			tagsJSONp = &s
		}
	}

	// Spec 9c — build a numeric score map and use Framework.Passes() so
	// veto questions are honored (Percoco Q4 risk_reward fails the whole
	// trade when scored 0).
	numericScores := make(map[string]int, len(req.Scores))
	for k, v := range req.Scores {
		numericScores[k] = v.Score
	}

	fs := &domain.FrameworkScore{
		UserID:       userID,
		TargetKind:   req.TargetKind,
		TargetID:     req.TargetID,
		FrameworkID:  req.FrameworkID,
		TotalScore:   total,
		MaxScore:     fw.MaxScore(),
		Passes:       fw.Passes(numericScores),
		ScoresJSON:   string(scoresJSON),
		TagsJSON:     tagsJSONp,
		ReviewerNote: req.ReviewerNote,
	}
	created, err := s.store.InsertFrameworkScore(r.Context(), fs)
	if err != nil {
		mapStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         created.ID,
		"totalScore": created.TotalScore,
		"maxScore":   created.MaxScore,
		"passes":     created.Passes,
	})
}

// GET /api/scores?targetKind=...&targetId=...&history=1
func (s *Server) handleGetScores(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	kind := r.URL.Query().Get("targetKind")
	idStr := r.URL.Query().Get("targetId")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || (kind != "holding" && kind != "watchlist") {
		writeError(w, http.StatusBadRequest, "bad targetKind/targetId")
		return
	}
	if r.URL.Query().Get("history") == "1" {
		hist, err := s.store.HistoryFrameworkScores(r.Context(), userID, kind, id, 50)
		if mapStoreError(w, err) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"scores": hist})
		return
	}
	latest, err := s.store.LatestFrameworkScore(r.Context(), userID, kind, id)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusOK, map[string]any{"score": nil})
		return
	}
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"score": latest})
}

// ----- shared helpers ----------------------------------------------------

func trimStrPtrW(p *string) *string {
	if p == nil {
		return nil
	}
	v := strings.TrimSpace(*p)
	if v == "" {
		return nil
	}
	return &v
}

func strOrFallback(p *string, fallback string) string {
	if p != nil && *p != "" {
		return *p
	}
	return fallback
}

func ptrString(s string) *string { return &s }

func invertKind(k string) string {
	if k == "stock" {
		return "crypto"
	}
	return "stock"
}

func stockOrCryptoIdent(k, v string) *string {
	if k == "stock" {
		return &v
	}
	return nil
}

// (tx-aware inserts live in internal/store/holdings.go as
// InsertStockHoldingTx / InsertCryptoHoldingTx — used by the promote flow.)
