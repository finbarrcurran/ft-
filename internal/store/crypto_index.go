package store

import "context"

// CryptoIndexMember is one constituent of a reference index (Jordi-46 Phase 1: the Visser-Labs
// 46-name tokenized index). Reference data only: nothing in alerts, entry zones, holdings or theses reads it.
type CryptoIndexMember struct {
	ID          int64   `json:"id"`
	IndexKey    string  `json:"indexKey"`
	ListVersion string  `json:"listVersion"`
	Symbol      string  `json:"symbol"`
	Name        string  `json:"name"`
	Vertical    string  `json:"vertical"`
	AssetKind   string  `json:"assetKind"` // token | stock
	CoinGeckoID *string `json:"coingeckoId,omitempty"`
	StockTicker *string `json:"stockTicker,omitempty"`
	Exchange    *string `json:"exchange,omitempty"`
	AddedAt     int64   `json:"addedAt"`
	RemovedAt   *int64  `json:"removedAt,omitempty"`
	SourceNote  string  `json:"sourceNote,omitempty"`
}

// ListCryptoIndexMembers returns the current (not removed) members of an index, ordered by vertical then symbol.
func (s *Store) ListCryptoIndexMembers(ctx context.Context, indexKey string) ([]CryptoIndexMember, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, index_key, list_version, symbol, name, vertical, asset_kind, coingecko_id, stock_ticker, exchange,
		       added_at, removed_at, COALESCE(source_note,'')
		FROM crypto_index_members WHERE index_key = ? AND removed_at IS NULL
		ORDER BY vertical, symbol`, indexKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CryptoIndexMember{}
	for rows.Next() {
		var m CryptoIndexMember
		if err := rows.Scan(&m.ID, &m.IndexKey, &m.ListVersion, &m.Symbol, &m.Name, &m.Vertical, &m.AssetKind,
			&m.CoinGeckoID, &m.StockTicker, &m.Exchange, &m.AddedAt, &m.RemovedAt, &m.SourceNote); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
