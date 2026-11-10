-- FT-082: the last price seen in the same Yahoo chart call that feeds the 52-week range. The Watchlist reads it
-- instead of watchlist.current_price, which is a one-time quote taken when the row was added.
ALTER TABLE ticker_market_stats ADD COLUMN price REAL;
ALTER TABLE ticker_market_stats ADD COLUMN price_as_of INTEGER;
