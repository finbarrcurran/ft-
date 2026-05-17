# FT — Master Spec (living document)

> **What this is.** The single canonical record of how FT works *right now*. Updated after every shipped change. If something in production behaves differently from what's here, this doc is wrong and should be fixed.
>
> **What this is not.** Not a roadmap, not requirements. Future work lives in the deferred-specs list at the bottom; here only describes what's deployed.
>
> **Editing.** Click `Edit` to update inline. `Save` for a tweak; `Save as new version` for a substantive change (records the changelog).
>
> **Last meaningful overhaul:** 2026-05-17 — initial author at the close of the Cross-Sector Investment Framework build (Specs 9f + 9g).

---

## 1. What FT is

A single-user portfolio dashboard for Fin Curran. Tracks stock + crypto holdings, regime overlay, sector rotation, performance retrospective, transaction history, framework scoring, and a thesis observation log. Deployed at `https://ft.curranhouse.dev` (Cloudflare Tunnel → `127.0.0.1:8081`).

Built in Go + SQLite + vanilla HTML/JS, no framework, no bundler. Runs in ~80 MB RSS on `jarvis` alongside HCT.

## 2. Layout — tabs in order

| # | Tab | Spec | Role |
|---|------|------|------|
| 1 | Summary | 2 | KPIs, donuts (asset / core-alt / sector / bottleneck / phase), stale-score banner, stale-thesis banner, regime pills, market pill |
| 2 | Stocks & ETFs | 3, 9c, 12 | Holdings table with alerts, proposed SL/TP (price\|%), 12m vol, score, sector-flow pill |
| 3 | Crypto | 3, 9c, 12 | Crypto holdings with classification (auto Core/Alt), current location, 12m vol, score |
| 4 | Performance | 9d | Closed-trades retrospective: window pill, R-multiple histogram, equity curve, methodology calibration, cohort drill-down |
| 5 | Screener | 9b | S&P sample with filters; "+ watchlist" prefilled |
| 6 | Sector Rotation | 9f | 34-row taxonomy (17 AI + 6 non-AI + 11 GICS), multi-window returns, RS vs SPY, tag pills, drag-reorder, weekly digest |
| 7 | Scorecards | 9g | Adapter MD repository (Philosophy + Energy + Hydrocarbons + Master Spec). Two-pane viewer/editor |
| 8 | Watchlist | 4, 9b, 12 | Names being considered, framework-scored, analyst Bear/Base/Bull, sortable, promotable |
| 9 | Heatmap | 6, 9d | SVG treemap. Three modes: market_cap / my_holdings / pnl |
| 10 | News | 2 D6, 12 D10 | NewsAPI feed + stocks F&G chip + macro calendar cards + filter mode |
| 11 | Crypto News | 2 D6 | CryptoPanic feed + alternative.me F&G chip |
| 12 | Settings | 3, 9b, 9c, 9c.1, 7, 11 | Portfolio risk dashboard, LLM spend dashboard, diagnostics + provider health, deleted-holdings restore, audit log, regime history, **Spec docs (this section)** |

Top bar: brand · market pill (clickable for all 7 exchanges, click-to-focus) · regime pills (Jordi / Cowen / Effective) · refresh status · refresh / import / export / ⌘K palette / logout.

## 3. Schema — every migration

| # | File | Adds |
|---|------|------|
| 0001 | `init.sql` | `users`, `sessions`, `service_tokens`, `stock_holdings`, `crypto_holdings`, `meta`, `news_cache`, `notification_log` |
| 0002 | `daily_change` | `daily_change_pct` on stock/crypto |
| 0003 | `crypto_is_core` | `is_core` on crypto (BTC/ETH=1) |
| 0004 | `spec3_holdings_extensions` | `note`, `deleted_at`, `beta`, `earnings_date`, `ex_dividend_date`, `vol_tier`. `holdings_audit`, `price_history` tables |
| 0005 | `watchlist_frameworks` | `watchlist`, `framework_scores` (append-only) |
| 0006 | `exchange_override` | `stock_holdings.exchange_override` |
| 0007 | `user_preferences` | k/v store, seeded `heatmap_mode=market_cap` |
| 0008 | `regime_history` | Append-only regime log; preference seeds for current state |
| 0009 | `percoco_execution` | Spec 9c columns: `support_1/2`, `resistance_1/2`, `atr_weekly`, `vol_tier_auto`, `setup_type`, `stage`, `tp1/tp2_hit_at`, `time_stop_review_at`. New: `portfolio_value_history`, `sr_candidates`, `daily_bars`, `weekly_bars`. 8 risk-cap preference seeds |
| 0010 | `llm_cost_discipline` | `llm_usage_log`, `llm_usage_daily`. 25 user_preferences seeds (caps, model, tools=0, kill-switches) |
| 0011 | `performance` | `closed_trades` (UNIQUE source_audit_close_id), `performance_snapshots` |
| 0012 | `transactions` | `transactions` (append-only, supersede column), `dividends`. `thesis_link` + `realized_pnl_usd` on holdings. Backfill: synthetic `opening_position` per active holding |
| 0013 | `thesis_notes` | Append-only observation log (target_kind, factor_id, factor_direction, source_kind) |
| 0014 | `provider_health` | One row per external provider tracking last_success/failure + counts |
| 0015 | `spec12_batch_a` | Cash balance preference seeds + focused_exchange seed + `holdings_audit.reason_code` |
| 0016 | `spec12_batch_b` | `crypto_holdings.current_location`, `volatility_12m_pct` on both holdings tables |
| 0017 | `spec12_batch_c` | Forecast columns (low/mean/high/fetched_at) on watchlist + stock_holdings |
| 0018 | `spec12_currency` | `stock_holdings.currency` (autofilled from Yahoo) |
| 0019 | `sector_rotation` | `sector_universe` (34 rows), `sector_snapshots`, `user_sector_ordering`, `sector_rotation_digests`. `sector_universe_id` on holdings + watchlist. 22/24 holdings backfilled |
| 0020 | `scorecards` | `sector_scorecards`, `sector_scorecard_versions`. Seeded 4 docs (Philosophy + Energy-Power + Hydrocarbons + Master Spec) |
| 0021 | `holding_theses` | `holding_theses` (one per kind+holding_id), `holding_thesis_versions` (append-only history) |
| 0022 | `mapping_v1_1_retag` | Refresh-session re-tag of ORCL → `data_center_reits` and WPM → `precious_metals_gold` per Sector_Holdings_Mapping_v1.1 |

## 4. Background jobs

| Job | Schedule | What |
|-----|----------|------|
| Live refresh | `FT_REFRESH_INTERVAL` (default 15m) | FX → stocks → crypto → heatmap. Provider chain: Finnhub → TwelveData → Yahoo |
| Daily job | 04:00 UTC | 365-day price_history backfill, calendar dates, beta auto-resolve, 12m vol compute, analyst forecasts. CLI: `ft daily` |
| Sector rotation ingest | 22:00 UTC | 34-ETF + SPY + VWRL daily close. CLI: `ft sector-ingest` |
| Weekly sector digest | Fri 22:00 UTC | Top/bottom 5 by RS, WoW movers, newly-tagged "rotating in" → `sector_rotation_digests` |
| Sunday regime nudge | Sun 18:00 UTC | FT bot Telegram nudge unless `regime_skip_week` set or Cowen submitted in last 7 days |
| Weekly perf summary | Sun 19:00 UTC | FT bot — 30-day perf snapshot |
| Session GC | Hourly | Purges expired session rows |
| DB backup | 03:15 UTC (cron) | `sqlite3 .backup` to `/var/backups/ft/`, prunes >14d |

## 5. Endpoints — by spec

(Cookie auth unless marked **T** for cookie-or-token.)

**Auth:** `GET /api/auth/{state,me}`, `POST /api/auth/{setup,login,logout}`

**Holdings:** `GET /api/holdings/{stocks,crypto}`, `POST` / `PUT/{id}` / `DELETE/{id}` / `/{id}/restore` / `/deleted`. `PUT /api/holdings/stocks/{id}/sector` (9f).

**Summary + status:** `GET /api/summary`, `GET /api/marketstatus`, `GET /api/marketstatus/all`, `GET /api/audit`

**Refresh:** `POST /api/refresh` **T**, `GET /api/refresh-status` **T**

**Import/export:** `POST /api/import/{preview,apply}`, `GET /api/export.xlsx`

**Heatmap:** `GET /api/heatmap.svg?mode={market_cap|my_holdings|pnl}&sector=`

**News + F&G:** `GET /api/news/{market,crypto}`, `GET /api/feargreed{,/stocks}`

**Spec 4 — Watchlist + Frameworks:** `GET/POST /api/watchlist`, `PUT/DELETE/{id}`, `POST/{id}/promote`. `GET /api/frameworks{,/{id}}`. `GET/POST /api/scores`

**Spec 6 — Preferences:** `GET/PUT /api/preferences/{key}` **T**

**Spec 9b — Regime:** `GET /api/regime` **T**, `POST /api/regime/{jordi,cowen/manual,cowen/auto}`, `GET /api/regime/history`, `GET /api/screener`, `GET /api/macro`

**Spec 9c — Percoco:** `GET /api/holdings/{stocks,crypto}/{id}/levels` **T**, `POST .../autoscore`, `GET /api/risk/dashboard` **T**, `POST /api/risk/snapshot` **T**

**Spec 9c.1 — LLM:** `GET /api/llm/{spend,log}` **T**, `POST /api/llm/{pause,override,override/clear}` **T**

**Spec 9d — Performance:** `GET /api/performance/{overview,cohorts,calibration,cohort/{key},export.csv}` **T**

**Spec 10 — Transactions:** `GET/POST /api/transactions`, `POST /api/transactions/{id}/supersede`, `GET /api/holdings/{kind}/{id}/taxlots`, `GET/POST /api/dividends`, `POST /api/transactions/import`

**Spec 11 — Thesis notes:** `GET/POST /api/notes` **T**, `PUT/DELETE /api/notes/{id}` **T**, `GET /api/notes/{stale,contradictions,resolve}` **T**

**Spec 7 — Diagnostics:** `GET /api/diagnostics` **T**

**Spec 12 — Lookup:** `GET /api/lookup/ticker?q=&kind=`

**Spec 9f — Sector rotation:** `GET /api/sector-rotation/{metrics,sectors,digests}` **T**, `POST/DELETE /api/sector-rotation/ordering`, `POST /api/sector-rotation/refresh` **T**

**Spec 9g — Scorecards:** `GET /api/scorecards{,/{code}{,/versions}}` **T**, `PUT /api/scorecards/{code}`, `POST /api/scorecards/preview`, `PUT /api/scorecards/{code}/status`

**Spec 14 — Per-holding theses:** `GET/PUT /api/holdings/{kind}/{id}/thesis`, `GET /api/holdings/{kind}/{id}/thesis/versions`, `PUT /api/holdings/{kind}/{id}/thesis/status`, `POST /api/holdings/{kind}/{id}/thesis/preview`

**Bot:** `GET /api/bot/{alerts,holdings/summary,holdings/movers}` **T**, `POST /api/bot/alerts/ack` **T**, `POST /api/bot/refresh` **T**

## 6. FT Telegram bot

Standalone Node 22 daemon at `/opt/ft-bot/`, system user `ft-bot`. Bearer-token auth. Bot identity `@FinsFTAlerts_bot`.

**Reactive commands:** `/alerts /summary /movers /regime /skip /levels /size /risk /positions /done /llm /perf /note /snooze /unsnooze /help`

**Proactive crons:**
- 13:00 / 17:00 / 21:00 UTC weekdays — RED/AMBER alerts with `notification_log` dedup
- Sunday 18:00 UTC — regime nudge
- Sunday 19:00 UTC — weekly perf summary

`/snooze [hours]` writes `alerts_snooze_until` preference; proactive crons short-circuit when set.

## 7. Conventions

- Schema changes → new migration `internal/store/migrations/00NN_foo.sql`
- All numeric columns get `class="num"`
- Frontend is vanilla JS in `web/app.js`; no framework, no bundler
- Cache-busting via 8-char hash of `app.js+app.css` stamped into index.html as `?v=`
- Every mutation writes a `holdings_audit` row with `changes_json` + optional `reason_code`
- `user_preferences` is the home for any new k/v setting; per-key validation in `validPreferenceValue()`
- All endpoints accept cookie auth + `ft_st_…` bearer token where the bot needs them
- Append-only tables (`transactions`, `closed_trades`, `thesis_notes`, `sector_snapshots`, `sector_scorecard_versions`, `framework_scores`, `holdings_audit`) — corrections via supersede/soft-delete, never UPDATE on data columns

## 8. Provider chain

| Domain | Primary | Fallback | Free-tier notes |
|--------|---------|----------|-----------------|
| US stock quote | Finnhub | TwelveData → Yahoo | TwelveData free is US-only since 2024 |
| Non-US stock quote | Yahoo (crumb dance) | — | Fragile; updates every few months |
| Stock history (sparkline + 12m vol) | Yahoo `v8/chart` | — | 365-day window |
| Stock fundamentals (beta, calendar, targets) | Yahoo `quoteSummary` | — | Patchy for non-US |
| Crypto quote | CoinGecko `/simple/price` | — | Bursts trigger sticky 429 |
| Crypto history | CoinGecko `/market_chart` | — | Sequential w/ 2.5s gap |
| FX EUR→USD | Frankfurter | static fallback 1.08 | No key needed |
| Stocks F&G | CNN `dataviz.cnn.io` | — | Unofficial; UA-gated |
| Crypto F&G | alternative.me | — | Stable |
| News (stocks) | NewsAPI | stale cache | Free 100 req/24h |
| News (crypto) | CryptoPanic | stale cache | Free tier sufficient |
| Sector ETFs | Yahoo daily closes | — | 36 ETFs × 1 call/day |

All provider calls wrapped with `health.Record` (Spec 7) for diagnostics surfacing.

## 9. Environment variables

In `/etc/ft/env` (mode 0600, root:root). See `deploy/env.example` for the full list with signup URLs.

Required: `FT_FINNHUB_API_KEY` (else US quotes fail).

Optional: `FT_TWELVEDATA_API_KEY`, `NEWSAPI_API_KEY`, `CRYPTOPANIC_API_KEY`, `FT_ANTHROPIC_API_KEY`, `FT_TELEGRAM_BOT_TOKEN`, `FT_TELEGRAM_CHAT_ID`.

Runtime: `FT_ADDR`, `FT_DB_PATH`, `FT_REFRESH_INTERVAL`, `FT_COOKIE_SECURE`.

## 10. Iteration loop

1. Edit `staging/ft/` on laptop
2. `git add . && git commit -m "..." && git push`
3. On jarvis: `sudo /opt/ft/bin/deploy.sh` — pulls, builds, installs, restarts, healthz-checks
4. Update this Master Spec to reflect any behavioural change

## 11. CLI subcommands

```
ft                                       run server
ft serve                                 run server (explicit)
ft seed [--user-id N]                    load Fin's holdings
ft daily [--user-id N] [--days 365]      Spec 3 D8/D10/D11 daily job + Spec 12 12m vol
ft backfill-bars [--user-id N] [--range 2y]  Spec 9c daily OHLC
ft perf-derive [--user-id N]             Spec 9d derive closed_trades + snapshots
ft sector-backfill [--months 14]         Spec 9f one-off ETF history
ft sector-ingest                         Spec 9f manual daily ingest
ft token create --user-id N --name X     mint a bearer token (plaintext shown once)
ft token list                            list tokens (no plaintext)
```

## 12. What's deferred — known queue

| Spec | Purpose | Status |
|------|---------|--------|
| 9e | Correlation matrix tracking | Drafted in v2; awaiting 30+ positions to justify |
| 9h | Real-time technicals monitoring (Finnhub WebSocket level breaks) | Drafted in handoff; defer until alert noise is a felt problem |
| 9i | Adapter scoring engine | Drafted in handoff; depends on 4-5 adapters drafted first |
| 9j | Additional sector adapters (Pharma, Defense, Mining, Industrial, Semi/AI-Infra) | User-authored as needed |
| 14 | Thesis repository inside FT (long-form per-holding) | ✅ Shipped 2026-05-17 — Detail page Thesis section gains 📄 In-app thesis subsection with goldmark render + textarea editor with live preview + Save / Save-as-new-version + history modal. Migration 0021 + reuses scorecards.Render. |
| 15 | Investment strategy / allocation framework | Depends on Spec 12 D2 ✓ |
| 16 | Alert strategy overhaul (851-alert problem) | Needs alert-noise audit first |
| 13 (numbering collision) | "Test coverage" (v2 doc) vs "Score automation engine" (Spec 12). Rename one | Open |
| 12 D5g | EUR toggle on stocks P&L column | Explicitly nice-to-have |

## 13. Version history of this doc

| Version | Date | Notes |
|---------|------|-------|
| 1.0 | 2026-05-17 | Initial author at the close of Specs 9f + 9g build. Captures every spec 1 → 12 + 9b/c/c.1/d/f/g + 7 + 10 + 11 + 11b. |
| 1.1 | 2026-05-17 | Spec 14 (per-holding theses) shipped. Added migration 0021 + thesis endpoints + Detail-page UI block. |
| 1.2 | 2026-05-17 | Handoff package v2 — Spec 9f D9 Whitespace Watchlist view shipped (filter pills, "+ watchlist" affordance carrying sector pre-selection). Migration 0022 retagged ORCL → data_center_reits + WPM → precious_metals_gold per Sector_Holdings_Mapping_v1.1. Watchlist schema extension: `sectorUniverseId` accepted on create. |

---

*Personal use only. Not investment advice.*
