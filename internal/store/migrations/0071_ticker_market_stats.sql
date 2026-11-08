-- SC-57 — 52-week high/low, all-time high (from the available history only) and market cap for stock-watchlist
-- names, with the provider and the time each value was read. NULL means "not available"; nothing is estimated.
--   high_52w / low_52w   from the chart meta (never computed); chart_as_of = the provider's own quote time
--   ath                  highest monthly high in the history Yahoo returned (hist_from..hist_to); ath_date = that month
--   market_cap           from the price module; cap_as_of = when it was read
CREATE TABLE IF NOT EXISTS ticker_market_stats (
    ticker          TEXT PRIMARY KEY,
    provider_symbol TEXT,
    currency        TEXT,
    high_52w        REAL,
    low_52w         REAL,
    ath             REAL,
    ath_date        TEXT,
    hist_from       TEXT,
    hist_to         TEXT,
    chart_source    TEXT,
    chart_as_of     INTEGER,
    market_cap      REAL,
    cap_source      TEXT,
    cap_as_of       INTEGER,
    last_error      TEXT,
    error_count     INTEGER NOT NULL DEFAULT 0,
    updated_at      INTEGER NOT NULL
);
