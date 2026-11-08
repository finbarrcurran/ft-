-- SC-56 — one calendar store for report (earnings) dates, keyed by ticker, covering every active stock
-- holding, every non-deleted stock-watchlist row and every live thesis (previously only holdings had a date,
-- in stock_holdings.earnings_date, which stays populated for back-compat).
-- A single table is smaller than adding the same eight columns to holdings AND watchlist, and it also covers
-- thesis-only tickers, which have neither.
--   earnings_date       next report date (ISO, UTC date) or NULL when there is none to show
--   earnings_status     UNKNOWN | DATE_KNOWN | DATE_ESTIMATED | REPORTED | NOT_ANNOUNCED | NOT_APPLICABLE | FETCH_ERROR
--   last_reported_date  the most recent report date that has passed (drives thesis revision_needed)
CREATE TABLE IF NOT EXISTS ticker_calendar (
    ticker                TEXT PRIMARY KEY,
    provider_symbol       TEXT,
    earnings_date         TEXT,
    earnings_is_estimate  INTEGER NOT NULL DEFAULT 0,
    earnings_status       TEXT    NOT NULL DEFAULT 'UNKNOWN'
        CHECK (earnings_status IN ('UNKNOWN','DATE_KNOWN','DATE_ESTIMATED','REPORTED','NOT_ANNOUNCED','NOT_APPLICABLE','FETCH_ERROR')),
    earnings_source       TEXT,                          -- yahoo_calendar | yahoo_earnings_page
    earnings_fetched_at   INTEGER,                       -- last successful provider answer
    last_error            TEXT,
    error_count           INTEGER NOT NULL DEFAULT 0,    -- consecutive failed refreshes
    error_permanent       INTEGER NOT NULL DEFAULT 0,    -- 1 = provider said 404 (symbol unknown to it)
    last_reported_date    TEXT,
    ex_dividend_date      TEXT,
    not_applicable_reason TEXT,
    second_checked_at     INTEGER,                       -- last time the second source was asked (at most daily)
    prev_earnings_date    TEXT,                          -- the date before the latest change (for the daily digest)
    date_changed_at       INTEGER,
    updated_at            INTEGER NOT NULL
);

-- Ticker -> the symbol Yahoo expects, applied before every provider call.
CREATE TABLE IF NOT EXISTS ticker_symbol_map (
    ticker          TEXT PRIMARY KEY,
    provider_symbol TEXT NOT NULL,
    note            TEXT
);
INSERT OR IGNORE INTO ticker_symbol_map (ticker, provider_symbol, note) VALUES
    ('BESI', 'BESI.AS',  'BE Semiconductor Industries, Euronext Amsterdam; the bare symbol is a 404 at Yahoo'),
    ('BYD',  '1211.HK',  'BYD Company Limited H-shares (the locked thesis is for 1211.HK); bare BYD is Boyd Gaming on US exchanges');

-- Tickers with no report date by nature: shown as NOT_APPLICABLE with the reason, never as a fetch error.
CREATE TABLE IF NOT EXISTS ticker_calendar_na (
    ticker TEXT PRIMARY KEY,
    reason TEXT NOT NULL
);
INSERT OR IGNORE INTO ticker_calendar_na (ticker, reason) VALUES
    ('GLD',   'ETF (physical gold trust): no earnings'),
    ('SLV',   'ETF (physical silver trust): no earnings'),
    ('WBA',   'Yahoo has no data for this symbol (404); reported taken private in 2025'),
    ('DJTWW', 'warrants of DJT: no separate earnings');

-- Continuity: carry today's holding dates over so nothing blanks between deploy and the first refresh.
INSERT OR IGNORE INTO ticker_calendar
    (ticker, earnings_date, earnings_status, earnings_source, last_reported_date, ex_dividend_date, updated_at)
SELECT ticker,
       CASE WHEN earnings_date >= date('now') THEN earnings_date END,
       CASE WHEN earnings_date >= date('now') THEN 'DATE_KNOWN' ELSE 'REPORTED' END,
       'yahoo_calendar',
       CASE WHEN earnings_date < date('now') THEN earnings_date END,
       ex_dividend_date,
       strftime('%s','now')
  FROM stock_holdings
 WHERE deleted_at IS NULL AND ticker IS NOT NULL AND ticker != '' AND earnings_date IS NOT NULL AND earnings_date != '';
