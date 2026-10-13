-- Migration 0052 — SC-49: the date of the data behind a crypto indicator reading.
--
-- updated_at is when FT last refreshed the reading; data_as_of is the date of the newest
-- data point actually used (for the ETF-flow card: the newest Farside row). Freshness is
-- judged on data_as_of, never on a file's modification time. NULL = the provider does not
-- report one (every indicator except the ETF flow, so far).
ALTER TABLE crypto_indicators ADD COLUMN data_as_of TEXT;
