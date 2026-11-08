-- SC-56 follow-ups. Second-source failures become visible (a Yahoo page change must not fail silently) and the
-- last reported date can be backfilled from the quote endpoint for tickers whose earnings page has no recent row.
--   second_error / second_error_count  last failure of the second source for this ticker, consecutive count
--   quote_checked_at                   last time the quote endpoint was asked for the last reported date
ALTER TABLE ticker_calendar ADD COLUMN second_error TEXT;
ALTER TABLE ticker_calendar ADD COLUMN second_error_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE ticker_calendar ADD COLUMN quote_checked_at INTEGER;
