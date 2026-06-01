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
	"ft/internal/domain"
	"ft/internal/etoro"
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

	hr, err := etoro.ParseHoldings(file, header.Filename)
	if err != nil {
		writeError(w, http.StatusBadRequest, "parse: "+err.Error())
		return
	}

	stocks, err := s.store.ListStockHoldings(r.Context(), userID)
	if mapStoreError(w, err) {
		return
	}
	cryptos, err := s.store.ListCryptoHoldings(r.Context(), userID)
	if mapStoreError(w, err) {
		return
	}

	rows := reconcile(hr.Holdings, stocks, cryptos)

	storePendingRecon(userID, &pendingRecon{Rows: rows, FileName: hr.FileName, Stored: time.Now()})

	counts := map[string]int{}
	for _, rw := range rows {
		counts[rw.Action]++
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"fileName":   hr.FileName,
		"rows":       rows,
		"counts":     counts,
		"warnings":   hr.Warnings,
		"ttlSeconds": int(pendingEtoroTTL.Seconds()),
	})
}

// reconcile diffs reconstructed eToro holdings against live FT holdings.
func reconcile(holdings []etoro.Holding, stocks []*domain.StockHolding, cryptos []*domain.CryptoHolding) []reconRow {
	// Index FT holdings.
	stockByTicker := map[string]*domain.StockHolding{}
	stockByISIN := map[string]*domain.StockHolding{}
	for _, h := range stocks {
		if h.Ticker != nil {
			stockByTicker[etoro.NormalizeTicker(*h.Ticker)] = h
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
				Note: "new crypto — not in FT",
			})
			continue
		}

		// Stock / ETF / non-crypto CFD underlying.
		var ft *domain.StockHolding
		conf := "needs_confirm"
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
		if ft != nil {
			matchedStock[ft.ID] = true
			rows = append(rows, stockMatchRow(e, ft, conf))
			continue
		}
		// New holding. ISIN present → we trust the identity (high); otherwise
		// the bare ticker can be ambiguous (e.g. SU) → needs_confirm (R3).
		addConf := "needs_confirm"
		if e.ISIN != "" {
			addConf = "high"
		}
		rows = append(rows, reconRow{
			Ref: "stock:add:" + e.Ticker, Kind: "stock", Action: "add",
			Confidence: addConf, Ticker: e.Ticker, Name: e.Name,
			Wrapper: cfdFlag(e.Wrapper), ISIN: e.ISIN,
			EtoroUnits: e.Units, EtoroInvested: e.InvestedUSD,
			EtoroAvgPrice: e.AvgPriceUSD, EtoroLots: e.Lots,
			Note: "new — not in FT",
		})
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
	for _, h := range cryptos {
		if matchedCrypto[h.ID] {
			continue
		}
		rows = append(rows, reconRow{
			Ref: "crypto:close:" + itoa(h.ID), Kind: "crypto", Action: "close",
			Confidence: "needs_confirm", Ticker: h.Symbol, Name: h.Name,
			FTHoldingID: h.ID, FTUnits: round6f(h.QuantityHeld),
			Note: "in FT, not open in eToro — close?",
		})
	}
	return rows
}

func stockMatchRow(e etoro.Holding, ft *domain.StockHolding, conf string) reconRow {
	ftUnits := ftStockUnits(ft)
	row := reconRow{
		Ref: "stock:drift:" + itoa(ft.ID), Kind: "stock", Confidence: conf,
		Ticker: e.Ticker, Name: e.Name, Wrapper: cfdFlag(e.Wrapper), ISIN: e.ISIN,
		EtoroUnits: e.Units, EtoroInvested: e.InvestedUSD, EtoroAvgPrice: e.AvgPriceUSD,
		EtoroLots: e.Lots, FTHoldingID: ft.ID, FTUnits: ftUnits,
		FTInvested: round2f(ft.InvestedUSD),
	}
	if ftUnits > 0 && e.Units > 0 {
		drift := (e.Units - ftUnits) / ftUnits
		row.DriftPct = round4f(drift * 100)
		if abs64(drift) > reconDriftThreshold {
			row.Action = "drift"
			row.Note = "quantity drift"
			return row
		}
	} else if ftUnits == 0 {
		row.Action = "drift"
		row.Note = "FT cost basis incomplete — set from eToro?"
		return row
	}
	row.Action = "insync"
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

// ftStockUnits derives quantity from FT's cost-basis model (invested / avg open).
func ftStockUnits(h *domain.StockHolding) float64 {
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
		protected []string
		errs      []string
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
			added++

		case row.Kind == "crypto" && row.Action == "add":
			h := &domain.CryptoHolding{
				UserID:         userID,
				Name:           row.Name,
				Symbol:         strings.ToUpper(strings.TrimSpace(row.Ticker)),
				Classification: "alt",
				QuantityHeld:   round6f(row.EtoroUnits),
			}
			if _, err := s.store.InsertCryptoHolding(ctx, h); err != nil {
				errs = append(errs, "add "+row.Ticker+": "+err.Error())
				continue
			}
			added++

		case row.Kind == "stock" && row.Action == "drift":
			h, err := s.store.GetStockHolding(ctx, userID, row.FTHoldingID)
			if err != nil {
				errs = append(errs, "drift "+row.Ticker+": "+err.Error())
				continue
			}
			h.InvestedUSD = round2f(row.EtoroInvested)
			if row.EtoroAvgPrice > 0 {
				avg := row.EtoroAvgPrice
				h.AvgOpenPrice = &avg
			}
			if err := s.store.UpdateStockHolding(ctx, h); err != nil {
				errs = append(errs, "drift "+row.Ticker+": "+err.Error())
				continue
			}
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
			h.QuantityHeld = round6f(row.EtoroUnits)
			if err := s.store.UpdateCryptoHolding(ctx, h); err != nil {
				errs = append(errs, "drift "+row.Ticker+": "+err.Error())
				continue
			}
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
