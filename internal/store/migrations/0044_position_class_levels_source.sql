-- Migration 0044 — SC-35 (Percoco Technical Levels — Activation + Alert
-- Intelligence). Two new per-holding levers:
--
--   * position_class ('hold' | 'trade') — the conviction-vs-tactical switch.
--     Drives the sl_method default (hold ⇒ vol_envelope catastrophe stop;
--     trade ⇒ technical support−0.5×ATR stop), TP behaviour (holds have no
--     fixed target; trades exit at resistance_1/2), and alert tone (Phase 4).
--     Existing book is conviction-led, so every current row backfills to
--     'hold' (status-quo behaviour preserved).
--
--   * levels_source ('auto' | 'manual') — manual-override-wins flag mirroring
--     migration 0036 / forecast_source. When a user hand-edits an S/R level
--     the row flips to 'manual' and the nightly auto-fill SKIPS its S/R writes
--     (ATR still updates — it's a measurement, not a chosen level). "Revert to
--     auto" resets it and the next nightly run reclaims the levels.
--
-- All columns are additive with safe defaults; rollback = ignore them
-- (everything keeps behaving as 'hold' / 'auto').

ALTER TABLE stock_holdings
  ADD COLUMN position_class TEXT NOT NULL DEFAULT 'hold'
    CHECK (position_class IN ('hold','trade'));

ALTER TABLE stock_holdings
  ADD COLUMN levels_source TEXT NOT NULL DEFAULT 'auto'
    CHECK (levels_source IN ('auto','manual'));

-- Bar-computed trend MAs (Percoco trend gate). DISTINCT from the existing
-- ma50/ma200 columns, which hold Yahoo's 50-DAY / 200-DAY quote averages and
-- cannot serve a weekly trend gate. ma_50w = 50-week SMA from weekly bars;
-- ma_200d = 200-day SMA from daily bars. Nightly cron owns both; NULL until a
-- holding has enough history (≥50 weekly bars for MA50W).
ALTER TABLE stock_holdings ADD COLUMN ma_50w REAL;
ALTER TABLE stock_holdings ADD COLUMN ma_200d REAL;

-- watchlist carries levels too (S/R candidates pre-promotion); give it the
-- same manual-wins flag so a hand-tuned watchlist level survives the cron.
ALTER TABLE watchlist
  ADD COLUMN levels_source TEXT NOT NULL DEFAULT 'auto'
    CHECK (levels_source IN ('auto','manual'));

-- Explicit backfill (the DEFAULT already covers existing rows on most SQLite
-- builds, but be unambiguous about the conviction-led starting state).
UPDATE stock_holdings SET position_class = 'hold' WHERE position_class IS NULL;
UPDATE stock_holdings SET levels_source = 'auto' WHERE levels_source IS NULL;
UPDATE watchlist SET levels_source = 'auto' WHERE levels_source IS NULL;

-- SC-35 Phase 4 / Decision D-mute — standalone RSI/momentum on a trend-intact
-- hold is the noise the user tunes out today, so the default is to mute it from
-- the proactive 13/17/21 ping (it still surfaces on demand via /alerts). The
-- toggle is fully reversible per-preference if Phase 4 feels too quiet in trial.
INSERT OR IGNORE INTO user_preferences (key, value, updated_at)
  VALUES ('alert_mute_rsi_only_on_holds', 'true', strftime('%s','now'));
