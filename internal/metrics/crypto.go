package metrics

import "ft/internal/domain"

// CryptoMetrics mirrors the prototype's CryptoDerivedMetrics interface.
type CryptoMetrics struct {
	TotalQuantity   float64  `json:"totalQuantity"`
	CurrentValueUSD *float64 `json:"currentValueUsd"`
	PNLUSD          *float64 `json:"pnlUsd"`
	PNLPct          *float64 `json:"pnlPct"`
	PNLEUR          *float64 `json:"pnlEur"`
}

// ComputeCrypto derives metrics for a crypto holding.
// Prefers the file's currentValueUsd when present (trustworthy source row from
// import), otherwise derives from quantity × price.
func ComputeCrypto(h *domain.CryptoHolding) CryptoMetrics {
	var m CryptoMetrics

	total := h.QuantityHeld + h.QuantityStaked
	m.TotalQuantity = total

	switch {
	case h.CurrentValueUSD != nil:
		m.CurrentValueUSD = ptr(*h.CurrentValueUSD)
	case h.CurrentPriceUSD != nil && total > 0:
		m.CurrentValueUSD = ptr(*h.CurrentPriceUSD * total)
	}

	if m.CurrentValueUSD != nil && h.CostBasisUSD != nil {
		p := *m.CurrentValueUSD - *h.CostBasisUSD
		m.PNLUSD = ptr(p)
		if *h.CostBasisUSD != 0 {
			m.PNLPct = ptr((p / *h.CostBasisUSD) * 100)
		}
	}

	if h.CurrentValueEUR != nil && h.CostBasisEUR != nil {
		m.PNLEUR = ptr(*h.CurrentValueEUR - *h.CostBasisEUR)
	}

	return m
}
