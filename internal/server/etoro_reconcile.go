// SC-17 Phase 2 — eToro holdings reconciliation (propose-and-approve).
//
// Endpoints (token-or-cookie via requireUser):
//   POST /api/etoro/reconcile/preview   multipart .xlsx → reconstruct current
//                                       holdings, match vs live FT, stage rows
//   POST /api/etoro/reconcile/apply     write ONLY the user-approved rows
//
// Matching (SC-17 R2/R3):
//   - ISIN match (FT holding already carries the statement's ISIN) = "high",
//     auto-eligible — the UI may pre-check it.
//   - Ticker-only match = "needs_confirm" — the UI leaves it unchecked and the
//     user MUST tick it; it can never be auto-approved.
//   - Routing is by underlying (R1): CFD wrapper never sends GLD/SLV to a CFD
//     silo; only crypto underlyings go to the Crypto tab.
//
// Nothing writes to live holdings without an explicit per-row approval (D17.3).
// The server only ever acts on the refs the client sends in the approved set;
// the confidence gate (needs_confirm cannot be auto-approved) is enforced by
// the UI defaulting those rows to unchecked.

package server

import (
	"context"
	"database/sql"
	"ft/internal/domain"
	"ft/internal/etoro"
	"ft/internal/etorosync"
	"ft/internal/store"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// reconRow is one proposed reconciliation action, staged between preview/apply.
type reconRow struct {
	Ref        string `json:"ref"`        // stable key for apply decisions
	Kind       string `json:"kind"`       // "stock" | "crypto"
	Action     string `json:"action"`     // "add" | "drift" | "close" | "insync"
	Confidence string `json:"confidence"` // "high" | "needs_confirm"

	Ticker  string `json:"ticker"`
	Name    string `json:"name"`
	Wrapper string `json:"wrapper,omitempty"` // "cfd" display flag (R1)
	ISIN    string `json:"isin,omitempty"`

	// eToro side (present for add / drift / insync)
	EtoroUnits    float64 `json:"etoroUnits,omitempty"`
	EtoroInvested float64 `json:"etoroInvestedUsd,omitempty"`
	EtoroAvgPrice float64 `json:"etoroAvgPriceUsd,omitempty"`
	EtoroLots     int     `json:"etoroLots,omitempty"`

	// FT side (present for drift / close / insync)
	FTHoldingID int64   `json:"ftHoldingId,omitempty"`
	FTUnits     float64 `json:"ftUnits,omitempty"`
	FTInvested  float64 `json:"ftInvestedUsd,omitempty"`

	DriftPct float64 `json:"driftPct,omitempty"`
	Note     string  `json:"note,omitempty"`
}

type pendingRecon struct {
	Rows     []reconRow
	FileName string
	Stored   time.Time
}

var (
	pendingRecons   = map[int64]*pendingRecon{}
	pendingReconsMu sync.Mutex
)

func storePendingRecon(userID int64, p *pendingRecon) {
	pendingReconsMu.Lock()
	defer pendingReconsMu.Unlock()
	cutoff := time.Now().Add(-pendingEtoroTTL)
	for k, v := range pendingRecons {
		if v.Stored.Before(cutoff) {
			delete(pendingRecons, k)
		}
	}
	pendingRecons[userID] = p
}

func popPendingRecon(userID int64) *pendingRecon {
	pendingReconsMu.Lock()
	defer pendingReconsMu.Unlock()
	p, ok := pendingRecons[userID]
	if !ok {
		return nil
	}
	delete(pendingRecons, userID)
	if time.Since(p.Stored) > pendingEtoroTTL {
		return nil
	}
	return p
}

const reconDriftThreshold = 0.01 // 1% quantity drift = actionable

// POST /api/etoro/reconcile/preview
func (s *Server) handleEtoroReconcilePreview(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())

	if err := r.ParseMultipartForm(10 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "multipart parse: "+err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing 'file' form field")
		return
	}
	defer file.Close()
	if !strings.HasSuffix(strings.ToLower(header.Filename), ".xlsx") {
		writeError(w, http.StatusBadRequest,
			"file must be an eToro .xlsx statement (got "+header.Filename+")")
		return
	}

	hr, err := etoro.ParseHoldingsSkipping(file, header.Filename, s.etoroCopyPositionIDs(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "parse: "+err.Error())
		return
	}
	if hr.CopySkipped > 0 {
		hr.Warnings = append(hr.Warnings, itoa(int64(hr.CopySkipped))+
			" copy-trade lots in this statement were left out — FT tracks only your own positions.")
	}

	stocks, err := s.store.ListStockHoldings(r.Context(), userID)
	if mapStoreError(w, err) {
		return
	}
	cryptos, err := s.store.ListCryptoHoldings(r.Context(), userID)
	if mapStoreError(w, err) {
		return
	}

	rows := reconcile(hr.Holdings, stocks, cryptos, s.etoroLiveHoldings(r.Context()))

	// A statement that doesn't cover the whole life of the open portfolio
	// rebuilds only some holdings: everything it misses would be proposed as
	// a closure, and matched units would show false drift. In that case only
	// "add" rows are staged — closures and drift are withheld, not just
	// unchecked, so apply can't act on them.
	oldest, newest := s.etoroOpenLotRange(r)
	complete, reason := hr.Coverage(oldest, newest)
	if !complete {
		kept := rows[:0]
		withheld := 0
		for _, rw := range rows {
			if rw.Action == "add" {
				kept = append(kept, rw)
			} else if rw.Action == "close" || rw.Action == "drift" {
				withheld++
			}
		}
		rows = kept
		hr.Warnings = append(hr.Warnings, "Incomplete statement: "+reason+". "+
			itoa(int64(withheld))+" closure/drift proposals withheld. Download a statement whose start date is your account's opening date and whose end date is today.")
	}

	storePendingRecon(userID, &pendingRecon{Rows: rows, FileName: hr.FileName, Stored: time.Now()})

	counts := map[string]int{}
	for _, rw := range rows {
		counts[rw.Action]++
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"fileName": hr.FileName,
		"rows":     rows,
		"counts":   counts,
		"warnings": hr.Warnings,
		"coverage": map[string]any{
			"complete": complete, "reason": reason,
			"startDate": hr.StartDate, "endDate": hr.EndDate,
			"oldestOpen": oldest, "newestOpen": newest,
		},
		"ttlSeconds": int(pendingEtoroTTL.Seconds()),
	})
}

// etoroCopyPositionIDs returns every position ID the SC-42 sync has seen as a
// copy-trade lot. The statement can't tell copied opens from the user's own.
func (s *Server) etoroCopyPositionIDs(r *http.Request) map[string]bool {
	ids := map[string]bool{}
	rows, err := s.store.DB.QueryContext(r.Context(), `SELECT DISTINCT position_id FROM etoro_holdings_lots WHERE mirror_id != 0`)
	if err != nil {
		return ids
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids[itoa(id)] = true
		}
	}
	return ids
}

// etoroOpenLotRange returns the open dates (YYYY-MM-DD) of the oldest and newest
// own eToro lots currently open, from the SC-42 API sync; "" if unknown.
func (s *Server) etoroOpenLotRange(r *http.Request) (oldest, newest string) {
	var o, n sql.NullString
	_ = s.store.DB.QueryRowContext(r.Context(), `SELECT min(substr(open_date,1,10)), max(substr(open_date,1,10))
		FROM etoro_holdings_lots WHERE mirror_id = 0 AND superseded_at IS NULL AND closed_at IS NULL
		AND coalesce(open_date,'') != ''`).Scan(&o, &n)
	return o.String, n.String
}

// liveHolding is one row of the SC-42 API data (etoro_holdings_effective): the
// broker's own units, invested USD and average open price (in the listing's own
// currency, the same unit FT stores).
type liveHolding struct{ Units, InvestedUSD, AvgOpen float64 }

// etoroLiveHoldings loads the SC-42 data keyed by FT ticker. Empty when the sync
// isn't configured or has never run — the reconcile then falls back to statement-only rules.
func (s *Server) etoroLiveHoldings(ctx context.Context) map[string]liveHolding {
	out := map[string]liveHolding{}
	rows, err := s.store.DB.QueryContext(ctx, `SELECT ticker, total_units, coalesce(invested_usd,0),
		coalesce(avg_open_price,0) FROM etoro_holdings_effective WHERE direction='long'`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var t string
		var l liveHolding
		if rows.Scan(&t, &l.Units, &l.InvestedUSD, &l.AvgOpen) == nil {
			out[t] = l
		}
	}
	return out
}

func baseTicker(t string) string {
	if i := strings.Index(t, "."); i >= 0 {
		return t[:i]
	}
	return t
}

// usdListed: avg_open_price is stored in the listing's own currency, so units can
// only be derived as invested(USD) ÷ avg for USD listings (empty hint = US ticker).
func usdListed(currency string) bool {
	c := strings.ToUpper(strings.TrimSpace(currency))
	return c == "" || c == "USD"
}

func relDiff(a, b float64) float64 {
	m := math.Max(math.Abs(a), math.Abs(b))
	if m == 0 {
		return 0
	}
	return math.Abs(a-b) / m
}

// liveFor finds the API row for a ticker, tolerating a missing exchange suffix
// (the statement says "SU", eToro's API and FT say "SU.PA") when exactly one matches.
func liveFor(live map[string]liveHolding, ticker string) (string, liveHolding, bool) {
	if l, ok := live[ticker]; ok {
		return ticker, l, true
	}
	if !strings.Contains(ticker, ".") {
		var key string
		n := 0
		for k := range live {
			if baseTicker(k) == ticker {
				key, n = k, n+1
			}
		}
		if n == 1 {
			return key, live[key], true
		}
	}
	return "", liveHolding{}, false
}

// reconcile diffs reconstructed eToro holdings against live FT holdings.
//
// Rules (SC-17 fallback, tightened 2026-10-11):
//   - Crypto: only rows whose wallet is eToro are eToro's to reconcile. Binance,
//     Ledger, MetaMask and Phantom rows are never matched, updated or proposed for
//     closure (same rule as SC-44).
//   - Units: FT stores none. When the SC-42 API data is available it is the truth
//     for "FT units" and for what a drift proposal applies; statements can't see
//     partial closes. Without it, units are derived (invested ÷ avg) only for
//     USD-listed instruments — for GBp/JPY/EUR listings the derivation mixes
//     currencies and would invent drift.
//   - Tickers: a statement ticker with no exchange suffix ("SU") matches the one FT
//     holding with the same base ("SU.PA") instead of becoming an add plus a closure.
func reconcile(holdings []etoro.Holding, stocks []*domain.StockHolding, cryptos []*domain.CryptoHolding, live map[string]liveHolding) []reconRow {
	var etoroCrypto []*domain.CryptoHolding
	for _, h := range cryptos {
		if h.Wallet != nil && strings.EqualFold(strings.TrimSpace(*h.Wallet), etorosync.EtoroWallet) {
			etoroCrypto = append(etoroCrypto, h)
		}
	}
	cryptos = etoroCrypto

	// Index FT holdings.
	stockByTicker := map[string]*domain.StockHolding{}
	stockByISIN := map[string]*domain.StockHolding{}
	stockByBase := map[string][]*domain.StockHolding{}
	for _, h := range stocks {
		if h.Ticker != nil {
			nt := etoro.NormalizeTicker(*h.Ticker)
			stockByTicker[nt] = h
			stockByBase[baseTicker(nt)] = append(stockByBase[baseTicker(nt)], h)
		}
		if h.ISIN != nil && *h.ISIN != "" {
			stockByISIN[strings.ToUpper(strings.TrimSpace(*h.ISIN))] = h
		}
	}
	cryptoBySymbol := map[string]*domain.CryptoHolding{}
	for _, h := range cryptos {
		cryptoBySymbol[strings.ToUpper(strings.TrimSpace(h.Symbol))] = h
	}

	matchedStock := map[int64]bool{}
	matchedCrypto := map[int64]bool{}
	var rows []reconRow

	for _, e := range holdings {
		if e.Underlying == "crypto" {
			sym := cryptoBaseSymbol(e.Ticker)
			if ft, ok := cryptoBySymbol[sym]; ok {
				matchedCrypto[ft.ID] = true
				rows = append(rows, cryptoMatchRow(e, ft, sym))
				continue
			}
			rows = append(rows, reconRow{
				Ref: "crypto:add:" + sym, Kind: "crypto", Action: "add",
				Confidence: "needs_confirm", Ticker: sym, Name: e.Name,
				EtoroUnits: e.Units, EtoroInvested: e.InvestedUSD,
				EtoroAvgPrice: e.AvgPriceUSD, EtoroLots: e.Lots,
				Note: "new eToro crypto — not in FT",
			})
			continue
		}

		// Stock / ETF / non-crypto CFD underlying.
		var ft *domain.StockHolding
		conf := "needs_confirm"
		matchNote := ""
		if e.ISIN != "" {
			if h, ok := stockByISIN[strings.ToUpper(e.ISIN)]; ok {
				ft, conf = h, "high"
			}
		}
		if ft == nil {
			if h, ok := stockByTicker[e.Ticker]; ok {
				ft = h
			}
		}
		if ft == nil && !strings.Contains(e.Ticker, ".") {
			if c := stockByBase[e.Ticker]; len(c) == 1 && !matchedStock[c[0].ID] {
				ft = c[0]
				matchNote = "statement ticker " + e.Ticker + " matched to FT's " + derefStr(c[0].Ticker) +
					" (the statement omits the exchange suffix)"
			}
		}
		if ft != nil {
			matchedStock[ft.ID] = true
			rows = append(rows, stockMatchRow(e, ft, conf, live, matchNote))
			continue
		}
		// New holding. ISIN present → we trust the identity (high); otherwise
		// the bare ticker can be ambiguous (R3) → needs_confirm.
		addConf := "needs_confirm"
		if e.ISIN != "" {
			addConf = "high"
		}
		row := reconRow{
			Ref: "stock:add:" + e.Ticker, Kind: "stock", Action: "add",
			Confidence: addConf, Ticker: e.Ticker, Name: e.Name,
			Wrapper: cfdFlag(e.Wrapper), ISIN: e.ISIN,
			EtoroUnits: e.Units, EtoroInvested: e.InvestedUSD,
			EtoroAvgPrice: stmtAvg(e), EtoroLots: e.Lots,
			Note: "new — not in FT",
		}
		if _, lv, ok := liveFor(live, e.Ticker); ok { // the broker's own figures beat the statement's
			row.EtoroUnits, row.EtoroInvested, row.EtoroAvgPrice = lv.Units, round2f(lv.InvestedUSD), lv.AvgOpen
		}
		rows = append(rows, row)
	}

	// FT holdings with no eToro counterpart → possible closures.
	for _, h := range stocks {
		if matchedStock[h.ID] {
			continue
		}
		tk := ""
		if h.Ticker != nil {
			tk = *h.Ticker
		}
		rows = append(rows, reconRow{
			Ref: "stock:close:" + itoa(h.ID), Kind: "stock", Action: "close",
			Confidence: "needs_confirm", Ticker: tk, Name: h.Name,
			FTHoldingID: h.ID, FTInvested: round2f(h.InvestedUSD),
			FTUnits: ftStockUnits(h),
			Note:    "in FT, not open in eToro — close?",
		})
	}
	for _, h := range cryptos { // eToro-wallet rows only (filtered above)
		if matchedCrypto[h.ID] {
			continue
		}
		rows = append(rows, reconRow{
			Ref: "crypto:close:" + itoa(h.ID), Kind: "crypto", Action: "close",
			Confidence: "needs_confirm", Ticker: h.Symbol, Name: h.Name,
			FTHoldingID: h.ID, FTUnits: round6f(h.QuantityHeld),
			Note: "in FT's eToro wallet, not open in eToro — close?",
		})
	}
	return rows
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// auditStatement writes the holdings_audit row for a human-approved fallback change
// (spec §7). No trade_snapshot_json, same reasoning as the SC-44 paths. Failure is
// logged, never fatal.
func (s *Server) auditStatement(ctx context.Context, userID int64, kind string, id int64, ticker, action string, changes any, reason string) {
	var tk, sym *string
	if kind == "crypto" {
		sym = &ticker
	} else {
		tk = &ticker
	}
	if err := s.store.RecordAuditWithCode(ctx, userID, kind, id, tk, sym, action, changes, &reason, "etoro_statement_reconcile"); err != nil {
		slog.Warn("statement reconcile: audit write failed", "kind", kind, "holding", id, "ticker", ticker, "err", err)
	}
}

// stmtAvg: the statement's average price is in USD, FT's avg_open_price is in the
// listing's own currency — only hand it on for USD listings.
func stmtAvg(e etoro.Holding) float64 {
	if usdListed(e.Currency) {
		return e.AvgPriceUSD
	}
	return 0
}

func stockMatchRow(e etoro.Holding, ft *domain.StockHolding, conf string, live map[string]liveHolding, matchNote string) reconRow {
	ftTicker := e.Ticker
	if ft.Ticker != nil {
		ftTicker = etoro.NormalizeTicker(*ft.Ticker)
	}
	row := reconRow{
		Ref: "stock:drift:" + itoa(ft.ID), Kind: "stock", Confidence: conf,
		Ticker: ftTicker, Name: e.Name, Wrapper: cfdFlag(e.Wrapper), ISIN: e.ISIN,
		EtoroUnits: e.Units, EtoroInvested: e.InvestedUSD, EtoroAvgPrice: stmtAvg(e),
		EtoroLots: e.Lots, FTHoldingID: ft.ID, FTInvested: round2f(ft.InvestedUSD),
	}
	note := func(n string) string {
		if matchNote != "" {
			return matchNote + "; " + n
		}
		return n
	}

	if lv, ok := live[ftTicker]; ok {
		row.FTUnits = round6f(lv.Units) // FT stores no units: show the broker's
		if relDiff(ft.InvestedUSD, lv.InvestedUSD) <= 0.005 {
			// FT already matches the live eToro data. A differing statement just
			// can't see partial closes — never "update" FT from it.
			row.Action = "insync"
			if relDiff(e.Units, lv.Units) > reconDriftThreshold {
				row.Note = note("FT already matches live eToro data; the statement shows the original open amounts (partial closes aren't visible in statements)")
			} else if matchNote != "" {
				row.Note = matchNote
			}
			return row
		}
		// FT is behind the live data: propose the broker's figures.
		row.Action = "drift"
		row.EtoroUnits, row.EtoroInvested, row.EtoroAvgPrice = lv.Units, round2f(lv.InvestedUSD), lv.AvgOpen
		if ft.InvestedUSD > 0 {
			row.DriftPct = round4f((lv.InvestedUSD - ft.InvestedUSD) / ft.InvestedUSD * 100)
		}
		row.Note = note("FT differs from live eToro data — set from eToro?")
		return row
	}

	// No live data for this holding: statement-only rules.
	cur := ""
	if ft.Currency != nil {
		cur = *ft.Currency
	}
	if !usdListed(cur) {
		// Units can't be derived (price is in another currency) — compare cost only.
		if ft.InvestedUSD > 0 && relDiff(e.InvestedUSD, ft.InvestedUSD) > reconDriftThreshold {
			row.Action = "drift"
			row.DriftPct = round4f((e.InvestedUSD - ft.InvestedUSD) / ft.InvestedUSD * 100)
			row.Note = note("invested amount differs (units not comparable: non-USD listing)")
			return row
		}
		row.Action = "insync"
		if matchNote != "" {
			row.Note = matchNote
		}
		return row
	}
	ftUnits := ftStockUnits(ft)
	row.FTUnits = ftUnits
	if ftUnits > 0 && e.Units > 0 {
		drift := (e.Units - ftUnits) / ftUnits
		row.DriftPct = round4f(drift * 100)
		if abs64(drift) > reconDriftThreshold {
			row.Action = "drift"
			row.Note = note("quantity drift")
			return row
		}
	} else if ftUnits == 0 {
		row.Action = "drift"
		row.Note = note("FT cost basis incomplete — set from eToro?")
		return row
	}
	row.Action = "insync"
	if matchNote != "" {
		row.Note = matchNote
	}
	return row
}

func cryptoMatchRow(e etoro.Holding, ft *domain.CryptoHolding, sym string) reconRow {
	row := reconRow{
		Ref: "crypto:drift:" + itoa(ft.ID), Kind: "crypto", Confidence: "needs_confirm",
		Ticker: sym, Name: e.Name, EtoroUnits: e.Units, EtoroInvested: e.InvestedUSD,
		EtoroAvgPrice: e.AvgPriceUSD, EtoroLots: e.Lots,
		FTHoldingID: ft.ID, FTUnits: round6f(ft.QuantityHeld),
	}
	if ft.QuantityHeld > 0 && e.Units > 0 {
		drift := (e.Units - ft.QuantityHeld) / ft.QuantityHeld
		row.DriftPct = round4f(drift * 100)
		if abs64(drift) > reconDriftThreshold {
			row.Action = "drift"
			row.Note = "quantity drift"
			return row
		}
	}
	row.Action = "insync"
	return row
}

// ftStockUnits derives quantity from FT's cost-basis model (invested / avg open),
// for USD-listed instruments only (0 = unknown).
func ftStockUnits(h *domain.StockHolding) float64 {
	if h.Currency != nil && !usdListed(*h.Currency) {
		return 0 // avg_open_price is in another currency: the division would be meaningless
	}
	if h.AvgOpenPrice != nil && *h.AvgOpenPrice > 0 {
		return round6f(h.InvestedUSD / *h.AvgOpenPrice)
	}
	return 0
}

// cryptoBaseSymbol strips any exchange suffix from a reconstructed ticker.
func cryptoBaseSymbol(ticker string) string {
	if i := strings.Index(ticker, "."); i >= 0 {
		ticker = ticker[:i]
	}
	return strings.ToUpper(strings.TrimSpace(ticker))
}

func cfdFlag(wrapper string) string {
	if wrapper == "cfd" {
		return "cfd"
	}
	return ""
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func abs64(f float64) float64 { return math.Abs(f) }

func round2f(f float64) float64 { return math.Round(f*1e2) / 1e2 }

func round4f(f float64) float64 { return math.Round(f*1e4) / 1e4 }

func round6f(f float64) float64 { return math.Round(f*1e6) / 1e6 }

// reconDecision is one client approval for a previously-staged reconRow.
type reconDecision struct {
	Ref      string `json:"ref"`
	Approved bool   `json:"approved"`
}

type reconApplyRequest struct {
	Decisions []reconDecision `json:"decisions"`
}

// POST /api/etoro/reconcile/apply
//
// Writes ONLY the rows the client explicitly approved (D17.3). We re-read each
// FT holding at write time (the staged preview can be up to pendingEtoroTTL old)
// and refuse to soft-delete a thesis-linked holding — a close proposal on a
// holding the user has tied to a thesis is surfaced as "protected", never acted
// on. ISIN is seeded on every approved stock add/drift that carries one, so the
// next upload can match high-confidence on the durable key (R2).
func (s *Server) handleEtoroReconcileApply(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())

	var req reconApplyRequest
	if !decodeJSON(r, w, &req) {
		return
	}

	pending := popPendingRecon(userID)
	if pending == nil {
		writeError(w, http.StatusConflict,
			"no staged reconciliation (it expired or was already applied — re-upload the statement)")
		return
	}

	byRef := map[string]reconRow{}
	for _, rw := range pending.Rows {
		byRef[rw.Ref] = rw
	}

	var (
		added, updated, closed, skipped int
		protected                       []string
		errs                            []string
	)
	ctx := r.Context()

	for _, d := range req.Decisions {
		if !d.Approved {
			continue
		}
		row, ok := byRef[d.Ref]
		if !ok {
			errs = append(errs, "unknown ref: "+d.Ref)
			continue
		}

		switch {
		case row.Kind == "stock" && row.Action == "add":
			tk := row.Ticker
			h := &domain.StockHolding{
				UserID:      userID,
				Name:        row.Name,
				Ticker:      &tk,
				InvestedUSD: round2f(row.EtoroInvested),
			}
			if row.EtoroAvgPrice > 0 {
				avg := row.EtoroAvgPrice
				h.AvgOpenPrice = &avg
			}
			id, err := s.store.InsertStockHolding(ctx, h)
			if err != nil {
				errs = append(errs, "add "+row.Ticker+": "+err.Error())
				continue
			}
			if row.ISIN != "" {
				if err := s.store.SeedStockHoldingISIN(ctx, userID, id, row.ISIN); err != nil {
					errs = append(errs, "seed isin "+row.Ticker+": "+err.Error())
				}
			}
			s.auditStatement(ctx, userID, "stock", id, row.Ticker, store.AuditCreate, map[string]any{
				"new": map[string]any{"name": row.Name, "ticker": row.Ticker, "investedUsd": h.InvestedUSD, "avgOpenPrice": h.AvgOpenPrice},
			}, "SC-17 statement reconcile: add")
			added++

		case row.Kind == "crypto" && row.Action == "add":
			h := &domain.CryptoHolding{
				UserID:         userID,
				Name:           row.Name,
				Symbol:         strings.ToUpper(strings.TrimSpace(row.Ticker)),
				Classification: "alt",
				QuantityHeld:   round6f(row.EtoroUnits),
			}
			// Tag the wallet, or the row would never be matched again now that the
			// fallback reconciles eToro-wallet crypto only.
			wallet := etorosync.EtoroWallet
			h.Wallet = &wallet
			id, err := s.store.InsertCryptoHolding(ctx, h)
			if err != nil {
				errs = append(errs, "add "+row.Ticker+": "+err.Error())
				continue
			}
			s.auditStatement(ctx, userID, "crypto", id, h.Symbol, store.AuditCreate, map[string]any{
				"new": map[string]any{"name": row.Name, "symbol": h.Symbol, "wallet": wallet, "quantityHeld": h.QuantityHeld},
			}, "SC-17 statement reconcile: add")
			added++

		case row.Kind == "stock" && row.Action == "drift":
			h, err := s.store.GetStockHolding(ctx, userID, row.FTHoldingID)
			if err != nil {
				errs = append(errs, "drift "+row.Ticker+": "+err.Error())
				continue
			}
			oldInv, oldAvg := h.InvestedUSD, h.AvgOpenPrice
			h.InvestedUSD = round2f(row.EtoroInvested)
			if row.EtoroAvgPrice > 0 {
				avg := row.EtoroAvgPrice
				h.AvgOpenPrice = &avg
			}
			if err := s.store.UpdateStockHolding(ctx, h); err != nil {
				errs = append(errs, "drift "+row.Ticker+": "+err.Error())
				continue
			}
			s.auditStatement(ctx, userID, "stock", h.ID, row.Ticker, store.AuditUpdate, map[string]any{
				"investedUsd":  map[string]any{"from": oldInv, "to": h.InvestedUSD},
				"avgOpenPrice": map[string]any{"from": oldAvg, "to": h.AvgOpenPrice},
			}, "SC-17 statement reconcile: update from eToro")
			if row.ISIN != "" && (h.ISIN == nil || *h.ISIN == "") {
				if err := s.store.SeedStockHoldingISIN(ctx, userID, h.ID, row.ISIN); err != nil {
					errs = append(errs, "seed isin "+row.Ticker+": "+err.Error())
				}
			}
			updated++

		case row.Kind == "crypto" && row.Action == "drift":
			h, err := s.store.GetCryptoHolding(ctx, userID, row.FTHoldingID)
			if err != nil {
				errs = append(errs, "drift "+row.Ticker+": "+err.Error())
				continue
			}
			oldQty := h.QuantityHeld
			h.QuantityHeld = round6f(row.EtoroUnits)
			if err := s.store.UpdateCryptoHolding(ctx, h); err != nil {
				errs = append(errs, "drift "+row.Ticker+": "+err.Error())
				continue
			}
			s.auditStatement(ctx, userID, "crypto", h.ID, row.Ticker, store.AuditUpdate, map[string]any{
				"quantityHeld": map[string]any{"from": oldQty, "to": h.QuantityHeld},
			}, "SC-17 statement reconcile: update from eToro")
			updated++

		case row.Kind == "stock" && row.Action == "close":
			h, err := s.store.GetStockHolding(ctx, userID, row.FTHoldingID)
			if err != nil {
				errs = append(errs, "close "+row.Ticker+": "+err.Error())
				continue
			}
			if h.ThesisLink != nil && strings.TrimSpace(*h.ThesisLink) != "" {
				protected = append(protected, row.Ticker)
				continue
			}
			if err := s.store.SoftDeleteStockHolding(ctx, userID, row.FTHoldingID); err != nil {
				errs = append(errs, "close "+row.Ticker+": "+err.Error())
				continue
			}
			s.auditStatement(ctx, userID, "stock", row.FTHoldingID, row.Ticker, store.AuditSoftDelete,
				map[string]any{"ftInvestedUsd": row.FTInvested}, "SC-17 statement reconcile: close")
			closed++

		case row.Kind == "crypto" && row.Action == "close":
			h, err := s.store.GetCryptoHolding(ctx, userID, row.FTHoldingID)
			if err != nil {
				errs = append(errs, "close "+row.Ticker+": "+err.Error())
				continue
			}
			if h.ThesisLink != nil && strings.TrimSpace(*h.ThesisLink) != "" {
				protected = append(protected, row.Ticker)
				continue
			}
			if err := s.store.SoftDeleteCryptoHolding(ctx, userID, row.FTHoldingID); err != nil {
				errs = append(errs, "close "+row.Ticker+": "+err.Error())
				continue
			}
			s.auditStatement(ctx, userID, "crypto", row.FTHoldingID, row.Ticker, store.AuditSoftDelete,
				map[string]any{}, "SC-17 statement reconcile: close")
			closed++

		default:
			// insync or unhandled — nothing to write.
			skipped++
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"added":     added,
		"updated":   updated,
		"closed":    closed,
		"skipped":   skipped,
		"protected": protected,
		"errors":    errs,
	})
}
