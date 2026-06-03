// SC-22 Demo / Privacy Mode — server-side masking helpers.
//
// Demo mode is a single server-read preference (`demo_mode` = "on"|"off"),
// never a URL param (S-22b — a shareable URL could toggle it off). When it is
// ON these helpers load the holdings and rewrite their sensitive financial
// fields via the demomask package *before* anything is computed or serialized,
// so no real number ever reaches the browser (D22.1).
//
// The masking always runs over the FULL combined book (stocks + crypto) even
// when a handler only needs one side, so the synthetic per-line values are
// identical across /api/summary, /api/holdings/stocks, /api/holdings/crypto and
// /api/risk/dashboard (deterministic, D22.5).

package server

import (
	"context"

	"ft/internal/demomask"
	"ft/internal/domain"
)

// demoModeOn reports whether the demo/privacy mode preference is enabled.
func (s *Server) demoModeOn(ctx context.Context) bool {
	v, err := s.store.GetPreference(ctx, "demo_mode")
	return err == nil && v == "on"
}

// loadHoldings fetches both holding lists and, when demo mode is on, masks them
// together so the synthetic book is internally consistent. Handlers that need
// both lists (Summary, risk dashboard) use this.
func (s *Server) loadHoldings(ctx context.Context, userID int64) ([]*domain.StockHolding, []*domain.CryptoHolding, error) {
	stocks, err := s.store.ListStockHoldings(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	cryptos, err := s.store.ListCryptoHoldings(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	if s.demoModeOn(ctx) {
		fx := s.store.GetMetaFloat(ctx, "fx_snapshot_eur_usd", 1.08)
		demomask.MaskBook(stocks, cryptos, fx)
	}
	return stocks, cryptos, nil
}

// loadStockHoldings returns the stock list, masked when demo mode is on. The
// crypto side is still fetched (and masked) so the £30k allocation matches the
// other endpoints exactly, but only the stocks are returned.
func (s *Server) loadStockHoldings(ctx context.Context, userID int64) ([]*domain.StockHolding, error) {
	if !s.demoModeOn(ctx) {
		return s.store.ListStockHoldings(ctx, userID)
	}
	stocks, _, err := s.loadHoldings(ctx, userID)
	return stocks, err
}

// loadCryptoHoldings returns the crypto list, masked when demo mode is on.
func (s *Server) loadCryptoHoldings(ctx context.Context, userID int64) ([]*domain.CryptoHolding, error) {
	if !s.demoModeOn(ctx) {
		return s.store.ListCryptoHoldings(ctx, userID)
	}
	_, cryptos, err := s.loadHoldings(ctx, userID)
	return cryptos, err
}
