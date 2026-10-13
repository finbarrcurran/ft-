# FT — Master Spec (living document)

> **What this is.** The single canonical record of how FT works *right now*. Updated after every shipped change. If something in production behaves differently from what's here, this doc is wrong and should be fixed.
>
> **What this is not.** Not a roadmap, not requirements. Future work lives in the deferred-specs list at the bottom; here only describes what's deployed.
>
> **Editing.** Click `Edit` to update inline. `Save` for a tweak; `Save as new version` for a substantive change (records the changelog).
>
> **Current version: v1.82.0 · 2026-10-13** — latest change: ETF-flow freshness guard (§13).
>
> Sections 1–12 are the current state of FT. §13 lists the ten newest changes in short form; every older entry, and the full text of these, is in the archive — `docs/spec-archive/` in the repo (`INDEX.md` maps every version and pass to its file), mirrored to Drive at `FT-Bridge/spec/archive/`.

---

<!-- reference_reviewed: v1.82.0 2026-10-11 -->

> **Sections 1–12 were re-derived from the running build on 2026-10-09 (v1.76.0)** — tab list from `web/app.js`, migrations from `internal/store/migrations/` and the live `schema_migrations` table (48 applied), jobs from `cmd/ft/main.go` and Jarvis's timers/cron, endpoints from the route table in `internal/server/server.go`, environment names from the code and `/etc/ft/env`. Items marked *(carried over)* were not re-verified in that pass. §13 below lists the ten newest changes; older history is in the archive. **These sections are the current state.**

## 1. What FT is

A single-user portfolio dashboard for Fin Curran. Tracks stock + crypto holdings (stocks kept in step with eToro automatically), regime overlays (crypto + macro), sector rotation, realised performance, transaction history, framework scoring, stock and crypto thesis libraries, political/insider/13F signals, the AI Nexus replication layer and a video-digest brain. Deployed at `https://ft.curranhouse.dev` (Cloudflare Tunnel → `127.0.0.1:8081`).

Built in Go + SQLite + vanilla HTML/JS, no framework, no bundler. One binary (`/opt/ft/bin/ft`), one database (`/var/lib/ft/ft.db`), ~50 MB RSS on `jarvis` alongside HCT.

**Who can read it:** the browser UI (cookie login), the FT Telegram bot (bearer token), and Claude through a **read-only** MCP connector at `/mcp` (SC-41). **Demo / privacy mode** (SC-22) is a server-side toggle that serves a synthetic book; broker data (eToro positions, proposals) is never served in demo mode.

**LLM use** goes through one governor (`internal/llm`, Spec 9c.1): Haiku default, per-call caps of 30,000 input / 4,000 output tokens, hard stops at $0.50/day and $5/month, per-feature kill switches. The only scheduled LLM feature is the video digest (SC-43). The eToro sync and reconcile make no LLM calls.

## 2. Layout — tabs in order

Nineteen tabs, in nav order:

| # | Tab | Spec | Role |
|---|------|------|------|
| 1 | Summary | 2, SC-22 | KPIs, donuts, stale-score / stale-thesis banners, regime pills, market pill, demo-mode toggle, AI Nexus summary card |
| 2 | Stocks & ETFs | 3, 9c, 12, SC-35, SC-42, SC-44 | Holdings table with alerts, proposed SL/TP, eToro's own SL/TP shown as a second line (`⚠ no eToro SL` when none), 12m vol, score, sector-flow pill, HOLD/TRADE class. **"eToro changes need your approval" panel** at the top (SC-44). Toolbar: Add stock, Reconcile with eToro statement (manual fallback), Download CSV |
| 3 | Crypto | 3, 9c, 12, SC-29 | Crypto holdings by wallet with classification (Core/Alt), current location, 12m vol, score; XLSX export |
| 4 | Crypto Indicators | 9e, SC-20 | BTC-primary regime layer — Cowen 4-phase, Pal macro, ETF flows, F&G, stablecoin supply; indicator explainers |
| 5 | Crypto Theses | 9l | 12-adapter repository + per-coin scored theses with lock / fork / cascade acknowledgement, and the crypto allocation table |
| 6 | Signals | 9k, SC-23, SC-24 | Insider (SEC Form 4), Congress (Bargo feed, with a visible attribution line), executive-order, OGE 278-T, named political-figure tracker, 13F institutional tracker. Tab badge on alarms |
| 7 | Performance | 9d, SC-17, SC-26, SC-30 | Leads with real eToro realised history (annual + YTD from statement imports), copy-vs-systematic split; closed-trade retrospective below. Hosts "Upload statement" and "Reconcile holdings" |
| 8 | Screener | 9b, SC-21 | Stock screener ("+ watchlist") and the crypto market screener (CoinGecko top 250) |
| 9 | Macro Regime & Sector Rotation | 9f, 9p | Macro regime band (growth × inflation, FRED-driven, playbook) above the 34-row sector taxonomy with multi-window returns, RS vs SPY, tag pills, weekly digest |
| 10 | AI Nexus | SC-36, SC-39 | Visser replication: Trend Score, Exhaustion, Forward PEG over the Nexus universe; Entry Candidates view |
| 11 | Video Digest | SC-43 | LLM digests of Jordi Visser / Benjamin Cowen videos: summary, themes, mentions with timestamp links, verified quotes, Cowen snapshot fields |
| 12 | Scorecards | 9g, SC-34 | Adapter / doctrine document repository (18 documents incl. this Master Spec), two-pane viewer/editor, Process/Playbook card pinned on top |
| 13 | Stock Theses | 15, SC-38 | GitHub-backed thesis library (`cross_sector_research`), grouped owned / watchlist / other, earnings-revision warnings, score-vs-outcome calibration |
| 14 | Watchlist | 4, 9b, 12, SC-14/15/16, SC-31 | Names under consideration, framework-scored, Bear/Base/Bull forecasts with manual override, grouped-by-sector view, promotable |
| 15 | Heatmap | 6, 9d | SVG treemap: market_cap / my_holdings / pnl |
| 16 | News | 2 D6, 12 D10 | NewsAPI feed + stocks F&G chip + macro calendar cards |
| 17 | Crypto News | 2 D6 | CryptoPanic feed + alternative.me F&G chip |
| 18 | Registry | SC-28 | Document-control view of the methodology-notes registry and doctrine documents |
| 19 | Settings | 3, 7, 9b, 9c, 9c.1, 11 | Portfolio risk dashboard, LLM spend dashboard, diagnostics + provider health + connectivity monitor, deleted-holdings restore, audit log, regime history |

Top bar: brand · market pill (all 7 exchanges, click-to-focus) · regime pills (Jordi / Cowen / Effective) · refresh status · refresh / import / save master / ⌘K palette / user / sign out.

## 3. Schema — every migration

52 migrations applied (latest `0052_indicator_data_as_of.sql`).

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
| 0020 | `scorecards` | `sector_scorecards`, `sector_scorecard_versions`. Seeded 5 docs (Philosophy + Energy-Power + Hydrocarbons + Pharma + Master Spec) |
| 0021 | `holding_theses` | `holding_theses` (one per kind+holding_id), `holding_thesis_versions` (append-only history) |
| 0022 | `mapping_v1_1_retag` | Refresh-session re-tag of ORCL → `data_center_reits` and WPM → `precious_metals_gold` per Sector_Holdings_Mapping_v1.1 |
| 0023 | `theses_index` | Spec 15 — GitHub-backed thesis library cache |
| 0024 | `crypto_indicators` | Spec 9e Phase 1 — `crypto_indicators`, `crypto_indicator_snapshots`, `crypto_indicator_weights`, `crypto_composite_snapshots` |
| 0025 | `btc_price_history` | Spec 9e — daily BTC OHLC for Cowen log-band + 200WMA |
| 0026 | `pal_ism_to_cfnai` | Spec 9e — swap ISM proxy to CFNAI series |
| 0027 | `signals` | Spec 9k Phase A — political + insider event store |
| 0028 | `signals_issuer` | Spec 9k — issuer-side attribution columns |
| 0029 | `signals_committee_seed` | Spec 9k — committee allow-list seed |
| 0030 | `signal_events_oge` | Spec 9k Phase B — adds 'oge' to signal_type CHECK + 'HOLD' action |
| 0031 | `crypto_theses` | Spec 9l Phase 1 — `crypto_adapters` + `crypto_adapter_versions` + `crypto_theses` + `crypto_thesis_history` + `crypto_thesis_dependencies` + `cascade_events` + `crypto_allocation_current` + `crypto_allocation_history`. 8 adapters seeded as drafts |
| 0032 | `crypto_theses_speculative_horizon` | Spec 9l v0.3 — BEFORE INSERT/UPDATE trigger pair on `crypto_theses`. Blocks Speculative adapter theses from locking at Never-Sell/Cycle/Multi-year horizon. Trade/Medium/TBD allowed |
| 0033 | `crypto_theses_q5_ryr_rabr_custody_btc_ref` | Spec 9l v0.6 doctrinal + v0.7 as-built. **Five doctrinal items in single migration.** Item 1: Q5 mechanism CHECK 7→14 values (added `direct_asset_claim`, `required_for_service`, `dsr_surplus`, `burn_and_mint`, `buyback_stake`, `real_yield_staking`, `governance_with_fee_switch`). Item 2: DePIN RYR cross-pillar columns (`q4_q5_ryr`, `q5_paid_revenue_usd`, `q5_emissions_usd`, `network_age_months`). Item 3: RWA RABR cross-pillar columns (`q5_rabr`, `q5_verified_asset_value_usd`, `q5_token_supply_at_par_usd`, `q5_audit_date`, `q5_auditor`). Item 4: RWA Custody Verification Tier (`q6_custody_tier` with CHECK + `q6_custody_cadence` + `q6_custody_jurisdiction`). Item 5: BTC β CHECK 4→5 values (added `reference`). Implemented as table rebuild (SQLite CHECK replacement requires it; all column adds folded into same rebuild for clean schema). 8 indexes + 2 Speculative horizon triggers from 0032 re-created. Post-migration UPDATEs: 5 Q5 re-tags (LINK→required_for_service, AAVE→real_yield_staking, BUIDL→direct_asset_claim, LUNC→burn_and_mint, RNDR→burn_and_mint) + BTC β `low`→`reference` + RNDR RYR populate (0.35, $2.7M, $8M, 30 months) + BUIDL RABR populate (1.00, $2.45B, $2.45B, 2026-04-30, BNY Mellon) + BUIDL Custody populate (tier_1, monthly, United States). |
| 0034 | `stock_holdings_sector_adapter_subtype` | `stock_holdings.sector_adapter_subtype` (table rebuild) — stock-side sector-adapter sub-type tag |
| 0035 | `relax_sector_adapter_subtype_check` | Loosens the 0034 CHECK so single-word adapter slugs are accepted (table rebuild) |
| 0036 | `stock_holdings_sl_method` | SC-08 — explicit per-holding stop-loss method (`sl_method`, `sl_safety_pct`) |
| 0037 | `macro_regime` | Spec 9p — `macro_indicators`, `macro_indicator_snapshots`, `macro_regime_history`, `regime_playbook`, `ism_manual` |
| 0038 | `etoro_performance` | SC-17 P1 — `etoro_performance`, `etoro_performance_year` (statement-import performance history) |
| 0039 | `stock_holding_isin` | SC-17 P2 — `stock_holdings.isin` (durable match key for statement reconcile) |
| 0040 | `crypto_adapters_expand_12` | Spec 9l — crypto adapters 8 → 12 (Stablecoin, Privacy, CeFi/Exchange, AI-Agent) |
| 0041 | `tracked_individuals` | SC-24 — `tracked_individuals` + OGE 278-T signal class |
| 0042 | `13f_tracker` | SC-23 — `tracked_funds`, `fund_13f_holdings`, `fund_13f_diffs`, `cusip_ticker_map` |
| 0043 | `forecast_targets_sc31` | SC-31 — `forecast_median`, `forecast_analyst_count`, `forecast_source` on watchlist + stock_holdings |
| 0044 | `position_class_levels_source` | SC-35 — per-holding `position_class` (hold/trade) and `levels_source`; `ma_50w`, `ma_200d` |
| 0045 | `nexus` | SC-36 — `nexus_universe`, `nexus_ticker_map`, `nexus_technical`, `nexus_exhaustion`, `nexus_fundamentals` |
| 0046 | `video_digest` | SC-43 — `video_sources` (seeded: jordi, cowen), `video_digests`, `video_mentions`, `video_quotes`, `video_frames`, `cowen_weekly_snapshot` |
| 0047 | `etoro_sync` | SC-42 — `etoro_instruments`, `etoro_sync_runs`, `etoro_holdings_lots` (per-lot change history), `etoro_holdings_effective` (per ticker + direction) |
| 0052 | `indicator_data_as_of` | SC-49 — `crypto_indicators.data_as_of` (date of the newest data point behind a reading) |
| 0051 | `provider_daily` | SC-48 — per-provider per-day call counters; retired providers' health rows removed |
| 0050 | `oauth` | SC-41b — `oauth_clients`, `oauth_codes`, `oauth_tokens` (hashes only; refresh tokens rotate, families revoked on reuse) |
| 0049 | `bargo_congress_monitor` | SC-47 — `congress_trades_raw` (every Congress trade ever received, unfiltered, unique on `source_key`), `bargo_sync_runs`, `monitor_state`, `monitor_events` |
| 0048 | `etoro_reconcile` | SC-44 — `etoro_reconcile_proposals`; `amount_usd` on lots, `invested_usd` on effective, `values_updated` / `proposals_pending` on sync runs |

**Holdings model, as it stands:** `stock_holdings` has no units column — it stores `invested_usd` and `avg_open_price` (in the listing's own currency); units live only in the eToro tables. Deletes are soft (`deleted_at`). Thesis links are `stock_holdings.thesis_link` (legacy URL) and `holding_theses` (in-app); both survive a soft-delete.

## 4. Background jobs

**Inside the FT process** (all times UTC):

| Job | Schedule | What |
|-----|----------|------|
| Live refresh | `FT_REFRESH_INTERVAL` (default 15m), plus once ~5 s after boot | FX → stocks → crypto → heatmap. Provider chain: Finnhub → TwelveData → Yahoo |
| eToro sync + holdings reconcile | every 30 min, around the clock; first run ~1 min after boot | SC-42: one portfolio call to eToro's Public API → lot history + effective SL/TP. SC-44: then diffs against FT holdings — matched values update silently, adds/removes queue for approval |
| Earnings-only refresh | hourly | Updates `earnings_date` / `ex_dividend_date` so thesis revision warnings fire within ~1 h |
| Thesis Library sync | `FT_THESIS_SYNC_EVERY` (default 5m) | `git pull` + re-index of `cross_sector_research`; no-op without `FT_GITHUB_TOKEN` |
| Session GC | hourly | Purges expired sessions |
| Crypto Indicators | 00:30, plus ~30 s after boot | Refresh all indicators + composite snapshot |
| Macro Regime | 01:00, plus ~75 s after boot | FRED refresh + regime snapshot |
| Legislators / committees | 02:00, self-gated to the first day of each quarter | Signals reference data |
| Daily job | 04:00 | 365-day price history, calendar dates, beta, 12m vol, analyst forecasts. CLI: `ft daily` |
| Video digest sweep | 09:15, plus ~2 min after boot | SC-43: ingests any complete package in `/var/lib/video_digest` not yet digested (one governor LLM call per new video) |
| Sector rotation ingest | 22:00 | 34 sector ETFs + SPY + VWRL daily close. CLI: `ft sector-ingest` |
| Weekly sector digest | Fri 22:00 | Top/bottom by RS, WoW movers → `sector_rotation_digests` |
| AI Nexus daily | 22:30 | Universe + benchmark bars, Trend Score + Exhaustion |
| AI Nexus weekly | Sun 22:45 | Forward PEG recompute |
| Signals ingest | 23:00 / 23:10 / 23:20 (+ quarterly legislators 02:00) | SEC Form 4 insiders (firehose + per-ticker), Congress, executive orders. The 23:10 Congress step (SC-47) imports the legacy Capitol Trades file into `congress_trades_raw` once, syncs Bargo (a rolling window of recent transaction dates, 7-day slices; first run backfills 3 months; Sundays re-read 60 days because disclosures lag up to ~45 days). **Before v1.80.1 only the 23:00 insider job ever ran** (see the scheduler note below), then turns recent raw rows into tiered `signal_events` |
| Connectivity monitor | every 10 min (first run 5 min after start) | SC-47: reads each watched source's last success (`provider_health`, `bargo_sync_runs`, `etoro_sync_runs`, Farside file mtime, newest DB backup) and compares it with the silence that source is allowed (`internal/monitor/checks.go`). One Telegram message when a source goes stale, a reminder every 24 h while it stays stale, one on recovery; on first sight, already-stale sources are folded into a single "monitor is on" message. Known-dead feeds are flagged optional (shown, never alert). Alerts go straight to Telegram via `FT_TELEGRAM_BOT_TOKEN` / `FT_TELEGRAM_CHAT_ID`; with them unset events are recorded as undelivered **The Farside ETF-flow check (SC-49) judges the newest ROW's date in US trading days, not the file's modification time:** alert at ≥ 3 US trading days old (one message, reminder every 24 h, recovery note), and the text names the cause — scraper failed (its `fetchedAt` older than 30 h) versus source not updated (scraper fine, no newer row). |
| Feed warm-up | 06:30 daily | SC-48: runs the real handlers for CNN Fear & Greed, crypto Fear & Greed, market news and crypto news once (same path as a user request, cache and stale fallback included) so request-driven feeds stay fresh; one Telegram alert naming any that still fail. Startup (30 s after boot): one Telegram alert naming any missing required provider variable (names only) |
| Scheduler note | — | `scheduleAt` (cmd/ft/main.go) runs each job in its own goroutine since v1.80.1. It used to block, so in a goroutine that armed several jobs only the first ever fired: the Congress/Bargo ingest (23:10), the executive-order ingest (23:20), the quarterly legislator refresh (02:00) and the weekly Nexus fundamentals job (Sunday 22:45) had never run on schedule since v1.11.0 (Congress and EO data were only ever refreshed by manual buttons or one-off runs). A regression test arms three jobs in a row and requires all to fire |

**On Jarvis, outside the FT process:**

| Job | Schedule | What |
|-----|----------|------|
| `ft-bridge-spec.timer` | every 10 min | SC-45: exports the working spec (live DB row) and the archive (`docs/spec-archive/`) to Google Drive (`FT-Bridge/spec`, `FT-Bridge/spec/archive`) when either changes. Source and installer in `ops/bridge/` (`sudo ops/bridge/install.sh`) |
| `ft-bridge-library.timer` | every 10 min | SC-47: mirrors the research library to Google Drive `FT-Bridge/library/` (one-way, Claude reads only): `specs/` (repo `ft_specs/`), `handovers/`, `theses/<adapter>/` (per-ticker locked stock theses, added v1.79.1), `scoring/` (stock + crypto logs, methodology registry, and the GENERATED `scoring_log_live` / `crypto_scoring_log_live` tallies built from `theses_index` / `crypto_theses`), `crypto/` (log, `adapters/`, `theses/`) and `FT-library-INDEX` (per-file last-updated + source commit + repo HEAD). Uploads only documents whose content changed (so a thesis lock/upload or a DB change shows within 10 min); `flock` prevents overlap. Source `ops/bridge/ft-bridge-library`; both bridge exporters touch `/var/lib/ft/data/bridge/{spec,library}.ok` on success, which the connectivity monitor watches (stale > 2 h → Telegram) |
| `video-digest.timer` | Sun 20:00 + Mon 08:00 Europe/Dublin | SC-43 P0: fetches new Jordi/Cowen videos into `/var/lib/video_digest` |
| `ft-backup.timer`, `ft-restore-verify.timer` | systemd timers | SC-37 backup / DR and restore verification |
| DB backup (cron, user `ft`) | 03:15 daily | `/opt/ft/bin/backup-db.sh` |
| Jarvis config backup (cron) | Sun 03:30 | `/opt/ft/bin/backup-jarvis-config.sh` |
| Whole-box backup (cron) | 03:30 daily | `jarvis_backup.sh` — restic to Backblaze B2 |
| Capitol-trades fetch (cron) | **retired 2026-10-11** | Replaced by Bargo (SC-47). The `/etc/cron.d/ft-capitol-trades` entry was removed; `trades.json` (last written 2026-08-14) is kept only as history and was imported into `congress_trades_raw` |
| Farside ETF-flow fetch (cron, user `curran`) | 00:25 daily | Playwright scraper → `/var/lib/ft/data/farside/etf-flow.json` (the Crypto Indicators ETF-flow chart reads it; the card falls back to a live fetch when it is older than 36 h). **Working again since 2026-10-11.** Logs to `~curran/logs/ft-farside-fetch.log`. Scripts, cron entries and installer in `ops/scrapers/` |
| FT Telegram bot | see §6 | Proactive alert crons |

## 5. Endpoints — by area

Cookie auth unless marked **T** (cookie or bearer token). About 190 routes; grouped, not exhaustive per verb.

**Auth:** `GET /api/auth/{state,me}`, `POST /api/auth/{setup,login,logout}`. `GET /healthz` (no auth).

**Holdings:** `GET /api/holdings/{stocks,crypto}`, `POST`, `PUT/{id}`, `DELETE/{id}` (soft), `POST/{id}/restore`, `GET .../deleted`. Stock-only setters: `PUT /api/holdings/stocks/{id}/{sector,sl-method,position-class,levels-source}`.

**Summary + status:** `GET /api/summary`, `/api/marketstatus`, `/api/marketstatus/all`, `/api/audit`

**Refresh:** `POST /api/refresh` **T**, `GET /api/refresh-status` **T**

**Import/export:** `POST /api/import/{preview,apply}` (master file, `.xlsx`/`.csv`; an eToro account statement is rejected with a pointer to the eToro importers), `GET /api/export.xlsx`, `GET /api/export.csv?tab=stocks|crypto|watchlist`, `GET /api/crypto/export.xlsx`

**eToro (SC-17, SC-42, SC-44):**
- Direct sync: `GET /api/etoro/sync` (last run + effective per-ticker SL/TP), `POST /api/etoro/sync` (run now).
- Approval queue: `GET /api/etoro/reconcile/proposals?status=`, `POST /api/etoro/reconcile/proposals/{id}/{approve,dismiss}`.
- Statement fallback (`.xlsx` only): `POST /api/etoro/reconcile/{preview,apply}` — the preview returns `coverage {complete, reason, startDate, endDate, oldestOpen, newestOpen}`; an incomplete statement has closure/drift rows withheld, and copy-trade lots are left out. **Rules since 2026-10-11:** only crypto in the `eToro` wallet is reconciled (other wallets are never matched or proposed for closure); FT has no units column, so "FT units" are the live SC-42 broker units when known (units are derived as invested ÷ avg only for USD listings — for GBp/JPY/EUR listings that mixes currencies); when FT already matches the live data a differing statement is shown in sync with a partial-close note and never applied; drift proposals carry the live figures, and the statement's USD average is never written to a non-USD listing; a statement ticker without an exchange suffix (`SU`) matches the single FT holding with that base (`SU.PA`); applied crypto adds are tagged wallet `eToro`; every applied change writes a `holdings_audit` row (reason_code `etoro_statement_reconcile`).
- Performance history: `POST /api/etoro/import/{preview,apply}`, `GET /api/etoro/performance`.

**Heatmap:** `GET /api/heatmap.svg?mode={market_cap|my_holdings|pnl}&sector=`

**News + F&G:** `GET /api/news/{market,crypto}`, `GET /api/feargreed{,/stocks}`

**Watchlist + frameworks (Spec 4):** `GET/POST /api/watchlist`, `PUT/DELETE/{id}`, `POST/{id}/promote`. `GET /api/frameworks{,/{id}}`. `GET/POST /api/scores`

**Preferences (Spec 6):** `GET /api/preferences` **T**, `GET/PUT /api/preferences/{key}` **T**

**Regime (9b) + macro (9p):** `GET /api/regime` **T**, `POST /api/regime/{jordi,cowen/manual,cowen/auto}`, `GET /api/regime/history`. `GET /api/macro/regime` **T**, `POST /api/macro/{refresh,ism}`, `GET/POST /api/macro/playbook`, `DELETE /api/macro/playbook/{id}`, `GET /api/macro`

**Screeners:** `GET /api/screener`, `GET /api/crypto-screener` (SC-21)

**Percoco levels + risk (9c, SC-35):** `GET /api/holdings/{stocks,crypto}/{id}/levels` **T**, `POST .../autoscore`, `GET /api/risk/dashboard` **T**, `POST /api/risk/snapshot` **T**

**LLM governor (9c.1):** `GET /api/llm/{spend,log}` **T**, `POST /api/llm/{pause,override,override/clear}` **T**

**Performance (9d):** `GET /api/performance/{overview,cohorts,calibration,cohort/{key},export.csv}` **T**

**Transactions (Spec 10):** `GET/POST /api/transactions`, `POST /api/transactions/{id}/supersede`, `GET /api/holdings/{kind}/{id}/taxlots`, `GET/POST /api/dividends`, `POST /api/transactions/import` (historical transactions, `.csv` only)

**Thesis notes (Spec 11):** `GET/POST /api/notes` **T**, `PUT/DELETE /api/notes/{id}` **T**, `GET /api/notes/{stale,contradictions,resolve}` **T**

**Diagnostics + lookup:** `GET /api/diagnostics` **T** (also returns the connectivity monitor: `monitor[]` per-source freshness, `monitorEvents[]` last 15 alerts, `monitorAlertsConfigured`), `GET /api/lookup/ticker?q=&kind=`

**Sector rotation (9f):** `GET /api/sector-rotation/{metrics,sectors,digests}` **T**, `POST/DELETE /api/sector-rotation/ordering`, `POST /api/sector-rotation/refresh` **T**

**Scorecards (9g) + Registry (SC-28):** `GET /api/scorecards{,/{code}{,/versions}}` **T**, `PUT /api/scorecards/{code}`, `POST /api/scorecards/preview`, `PUT /api/scorecards/{code}/status`, `GET /api/registry`

**Per-holding theses (Spec 14):** `GET/PUT /api/holdings/{kind}/{id}/thesis`, `GET .../thesis/versions`, `PUT .../thesis/status`, `POST .../thesis/preview`

**Stock Thesis Library (Spec 15, SC-38):** `GET /api/theses{,/gaps,/{id},/{id}/revision-prompt}`, `POST /api/theses/{upload,scoring-log,sync}`, `GET /api/calibration/theses`. GitHub repo `finbarrcurran/cross_sector_research` is the source of truth; FT keeps a clone at `/var/lib/ft/research/`.

**Crypto Indicators (9e):** `GET /api/crypto-indicators{,/composite/latest,/composite/history,/btc-history,/etf-flow/history,/ism}`, `POST /api/crypto-indicators/{refresh,backfill,ism}`

**Crypto Theses (9l):** adapters — `GET /api/crypto/adapters{,/{slug}{,/versions{,/{ver}}}}` **T**, `PUT /api/crypto/adapters/{slug}{,/status}`, `POST /api/crypto/adapters/preview`. Theses — `GET /api/crypto/theses` **T**, `GET /api/crypto/theses/drafts`, `POST /api/crypto/theses`, and per `{symbol}/{version}`: `GET` **T**, `PUT`, `DELETE`, `POST .../{lock,fork,acknowledge-cascade}`, `GET .../events` **T**. Allocation — `GET` **T** / `PUT /api/crypto/allocation`.

**Signals (9k, SC-23, SC-24):** `GET /api/signals`, `POST /api/signals/{id}/ack`, `POST /api/signals/refresh-{insiders,congress,eo,committees,13f,278t-eo-link}`, `POST /api/signals/upload-{oge,278t}`, `GET /api/signals/universe`, `GET/POST /api/signals/tracked-individuals`, `GET/POST /api/signals/tracked-funds`, `POST /api/signals/tracked-funds/remove`, `GET /api/signals/fund-13f-diffs`

**AI Nexus (SC-36, SC-39):** `POST /api/nexus/upload`, `GET /api/nexus/{universe,technical,exhaustion,fundamentals,entry-candidates}`

**Video Digest (SC-43):** `GET /api/video-digest?source=`, `POST /api/video-digest/ingest`

**Bot:** `GET /api/bot/{alerts,holdings/summary,holdings/movers,refresh-status}` **T**, `POST /api/bot/{alerts/ack,refresh}` **T**

**MCP connector (SC-41):** `/mcp` — JSON-RPC, bearer token whose scope contains `read` (minted with `ft token create-mcp`). Six read-only tools: `get_theses`, `get_scorecards`, `get_gap_report`, `get_alerts`, `get_performance`, `get_watchlist`. Each returns what the matching dashboard endpoint returns (same demo-mode gating) inside an envelope with `queriedAt`. No write tool exists.

**OAuth for the claude.ai connector (SC-41b, v1.79.0):** `/mcp` also accepts an OAuth access token (`ft_oat_…`, 1 h, scope `read`, bound to `https://ft.curranhouse.dev/mcp`) issued by FT's own single-user OAuth 2.1 server, because the claude.ai connector dialog has no field for a static bearer. Routes (no cookie; each authenticates itself): `GET /.well-known/oauth-protected-resource[/…]` (RFC 9728), `GET /.well-known/oauth-authorization-server` (RFC 8414), `POST /register` (RFC 7591; redirect URIs limited to `https://claude.ai/api/mcp/auth_callback` and loopback; 50-client cap, 10 registrations/h/IP), `GET|POST /authorize` (Fin approves with his FT email + password; PKCE S256 required; single-use code, 60 s), `POST /token` (form-encoded; 1 h access tokens, rotating 30-day refresh tokens; a re-presented spent refresh token revokes its family and alerts). An unauthenticated `/mcp` call now answers 401 with `WWW-Authenticate: Bearer resource_metadata=…, scope="read"`; unknown `/.well-known/*` paths return JSON 404 (they used to return the SPA page). **Login hardening (same release):** `POST /api/auth/login` and `/authorize` share a limiter — 5 failed attempts per client IP per 15 min and 30 overall per hour, then 429 with `Retry-After`, never a permanent lock; failures are logged and the first lockout sends a Telegram alert. The static `ft_mcp_` tokens keep working. Kill switches: `ft oauth revoke-all`, `ft oauth revoke-client`, `ft token revoke`; Diagnostics shows client and sign-in counts. **Cloudflare:** FT cannot apply the recommended WAF rule itself (no API token); it must be set in the dashboard (restrict `/mcp`, `/token`, `/register`, `/.well-known/*` to `160.79.104.0/21`, and rate-limit `/api/auth/login`).

## 6. FT Telegram bot

*(carried over — the service is running; commands and schedules were not re-verified in the 2026-10-09 pass.)*

Standalone Node 22 daemon at `/opt/ft-bot/`, system user `ft-bot`. Bearer-token auth. Bot identity `@FinsFTAlerts_bot`.

**Reactive commands:** `/alerts /summary /movers /regime /skip /levels /size /risk /positions /done /llm /perf /note /snooze /unsnooze /help`

**Proactive crons:**
- 13:00 / 17:00 / 21:00 UTC weekdays — RED/AMBER alerts with `notification_log` dedup
- Sunday 18:00 UTC — regime nudge (unless `regime_skip_week` set or Cowen submitted in last 7 days)
- Sunday 19:00 UTC — weekly perf summary
- AI Nexus anomaly flags are relayed through the alert poll (SC-36 W4)

`/snooze [hours]` writes `alerts_snooze_until`; proactive crons short-circuit when set.

## 7. Conventions

- Schema changes → new migration `internal/store/migrations/00NN_foo.sql`
- All numeric columns get `class="num"`
- Frontend is vanilla JS in `web/app.js`; no framework, no bundler
- Cache-busting via 8-char hash of `app.js+app.css` stamped into index.html as `?v=`
- Every mutation writes a `holdings_audit` row with `changes_json` + optional `reason_code`. That includes SC-44's: silent eToro value updates (actor `etoro-sync`) and approved adds/removals (actor `fin`), all with `reason_code = etoro_reconcile` and **no** `trade_snapshot_json` (so `performance.DeriveAll` never turns an eToro-side closure into a closed trade)
- `user_preferences` is the home for any new k/v setting; per-key validation in `validPreferenceValue()`
- Endpoints the bot or the MCP connector need accept a bearer token as well as the cookie (marked **T** in §5); everything else is cookie-only
- Append-only tables (`transactions`, `closed_trades`, `thesis_notes`, `sector_snapshots`, `sector_scorecard_versions`, `framework_scores`, `holdings_audit`, `etoro_holdings_lots`) — corrections via supersede/soft-delete, never UPDATE on data columns
- **Broker data rules:** eToro owns units, `invested_usd` and `avg_open_price` (taken from eToro's own USD figures — no FT-side FX math); FT owns stop/TP, `sl_method`, notes, sector tags and thesis links. Copy-trade positions are stored but never become FT holdings. Anything that changes *which* holdings exist needs explicit approval. Crypto reconciles only against rows whose wallet is `eToro` — in SC-44 and in the statement fallback alike.
- **Demo mode** is enforced server-side; portfolio and broker data are never served in demo.
- **LLM calls** only through the `internal/llm` governor, each feature behind its own kill switch.
- **Methodology-notes registry is the canonical note list** — the numbered notes live in `cross_sector_research/theses/_methodology_notes_registry.md`, version-stamped, and integrity-checked by `tools/check_registry.sh`. Cite a note ONLY by its exact registry number + name; a `candidate` note is never citable as established. After editing the registry, bump its `REGISTRY v<N>` stamp or `check_registry.sh` fails.
- **Registry + doctrine drop-ins are re-exported on change** (set 2026-06-10) so the ai-side reads the fresh, stamped copy.
- **Master Spec is bumped on every completed section or spec** (set 2026-05-19). Versioning: patch for polish batches, minor for new specs/features, major for breaking architecture. **Each bump (rule changed 2026-10-10, v1.77.0):** (a) run `tools/spec_archive.py bump --version X.Y.Z --full FULL.md --short SHORT.md` — it appends the *full* entry (one `> **Overhaul:** …` line, no length limit) to the archive, puts the *short* entry (≤800 characters: what and why in two sentences, behaviour change yes/no, migrations/endpoints, binding rulings, `Supersedes vX`) at the top of §13, drops the oldest beyond ten, and refreshes the current-version line, the END marker and `docs/spec-archive/INDEX.md`; (b) **the affected parts of sections 1–12 are updated in the same commit** and the `reference_reviewed` marker is set to the new version (a bump that only adds an entry leaves the reference stale — how these sections drifted from v1.5x to v1.75); (c) `tools/spec_archive.py check` and `go test ./internal/scorecards/` must pass; (d) the commit includes the spec and the archive; (e) the live DB row is updated after deploy. Anything that must stay true goes in sections 1–12, not in a change entry. Archive text is **never edited** — a correction is a new entry.
- **The spec is mirrored to Google Drive** (`FT-Bridge/spec`, SC-45): `FT-master-spec` is the whole working spec from the live DB row — about 40,000 characters, ending with `<!-- END FT-master-spec vX.Y.Z -->` so a reader can tell it got everything (the Drive copy adds one short tail line after the marker, because readers sometimes clip the last 10–40 characters: a clip eats the tail, not the marker, and a marker line that has started counts as complete) — `FT-spec-current` is the index and freshness page, and `spec/archive/` carries the archive. Claude's Drive read stops near 100,000 characters, so no file may be larger than that. The mirror marks the reference STALE whenever the `reference_reviewed` marker is behind the spec version or the live schema is ahead of the migrations these sections mention.

## 8. Provider chain

*(Rewritten 2026-10-12 from the SC-48 diagnosis; each row was checked against the code and a live probe from Jarvis on 2026-10-11. Per-provider call counts per UTC day are in `provider_daily` and Settings → Diagnostics.)*

| Domain | Primary | Fallback | Notes |
|--------|---------|----------|-------|
| US stock quote (holdings) | Finnhub `/quote` | Yahoo | Free tier **60 calls/min** (read from the `x-ratelimit-*` response headers, shown in Diagnostics). FT gates calls ≥ 1.1 s apart with jitter, backs off 30 s on a 429, caches 5 min, collapses duplicate requests, and **skips tickers with an exchange suffix** (`.L .PA .DE .T`: not served by the free tier). A rejected or skipped ticker falls to Yahoo. |
| Non-US stock quote | Yahoo (crumb dance) | — | Unofficial; fragile by nature |
| Heatmap tiles (~80 S&P sample) | Yahoo (direct) | previous live values kept | Sequential, 150 ms apart; the batch stops after 5 consecutive failures. Refreshed **every ~30 minutes while the tile's exchange is open**, once more after the close (closing prints), and not at all while closed (`internal/refresh`, uses `marketdata.Status`); all 80 tiles are US, so this is the US session. They never touch Finnhub |
| Stock history (sparkline + 12m vol) | Yahoo `v8/chart` | — | 365-day window |
| Stock fundamentals (beta, calendar, targets) | Yahoo `quoteSummary` | — | Patchy for non-US |
| ~~Twelve Data~~ | **retired 2026-10-12 (SC-48)** | | Its free plan is US-only and capped at 800 credits/day; FT spent ~3,400/day on it and every call failed (Yahoo supplied the data). No key use remains; the variable may stay in env unused |
| Congress trades | Bargo Congress API (`bargo.ai/free-apis/congress/v1`) | — | Free key (`FT_BARGO_API_KEY`, Bearer header): 100 requests + 1,000 rows/day, 250 rows/page; free tier serves only the last ~3 months, so FT keeps its own history in `congress_trades_raw`. Terms: visible attribution (Signals tab footer) and no bulk redistribution |
| Broker positions, SL/TP, invested USD | eToro Public API (`public-api.etoro.com`) | manual `.xlsx` statement upload | Key pair in env; 60 req/60 s shared limit; one call per 30-min sync; the sync transaction is `BEGIN IMMEDIATE` |
| Crypto quote | CoinGecko `/simple/price` | serve-stale | Demo key `FT_COINGECKO_API_KEY`; free Demo plan 100 calls/min and 10,000/month (FT uses ~100–200/day) |
| Crypto history | CoinGecko `/market_chart` | — | Sequential w/ 2.5s gap |
| Crypto market screener | CoinGecko top 250 | — | SC-21 |
| Macro + Pal indicators | FRED | manual ISM entry | `FRED_API_KEY` |
| FX EUR→USD | Frankfurter (`api.frankfurter.dev/v1`) | stored rate, then static 1.08 | No key; moved from `frankfurter.app` (v1.80.0) |
| Stocks Fear & Greed | CNN `production.dataviz.cnn.io` | stale cache ≤ 6 h | Unofficial; answers HTTP 418 unless a cnn.com `Referer`/`Origin` is sent (sent since v1.80.0). Fragile: the daily warm-up alerts if it still fails |
| Crypto Fear & Greed | alternative.me | stale cache | Stable |
| News (stocks) | **Free publisher RSS** (cut over 2026-10-13) | stale cache | **Tier A (displayed):** CNBC Top News, BBC Business, Nasdaq Markets. **Tier B (backup, fetched every run so its health is measured, displayed only when < 2 tier-A feeds are healthy or < 10 tier-A items are under 24 h old):** MarketWatch Top Stories, The Guardian Business. No key. Headline, source, time, link out, plus the feed's own summary snippet (≤ ~200 chars) when it supplies one; no article body is fetched or stored. Merge deduped by URL and normalised title, newest first, per-feed attribution, a dead feed never blanks the tab; per-feed health `rss_market_<key>` in the monitor (one alert per outage names the feed). A feed can be dropped without a deploy: `meta.market_news_disabled_feeds` (comma-separated keys: cnbc, bbc, nasdaq, marketwatch, guardian). **NewsAPI is no longer called** (`meta.market_news_source` = `rss`; setting it back to `newsapi` re-enables the old path, kept for about two weeks, planned removal ~2026-10-27). Terms: see §13 entry. RSS requests use the User-Agent `Mozilla/5.0 (compatible; FT-Dashboard/1.0)` (no URL: Nasdaq's CDN resets connections otherwise) |
| News (crypto) | Publisher RSS (CoinDesk, Cointelegraph, The Block, Decrypt) | stale cache | No key. (CryptoPanic and CryptoCompare are no longer used; CryptoCompare's stale health row was removed) |
| Sector ETFs | Yahoo daily closes | — | 36 ETFs × 1 call/day |
| Insider / 13F filings | SEC EDGAR | — | Daily ingest; 13F on demand |
| Video digests | Anthropic API via the governor | — | Haiku; packages come from the Jarvis yt-dlp pipeline |
| ETF flows (crypto) | Farside via the Playwright cron | live HTTP for the card after 36 h | **Freshness (SC-49):** the card and the chart show the data's own date ("as of …" / "latest data …"); a badge appears by US trading days since that date (none ≤ 2, amber 3–4, red ≥ 5; never hides data); computed server-side from `crypto_indicators.data_as_of` (card) and the chart's last bar, so both use `marketdata.USTradingDaysSince`/`StaleLevel`. The 36 h file-age rule stays a *fetch* decision only. Settings → Diagnostics shows newest row, age and scraper last success. The cache file holds ONE series (BTC spot-ETF aggregate, newest-first, 90 rows), so one as-of covers card and chart; there is no ETH-flow series |

**Request-driven feeds** (CNN F&G, both news feeds, alternative.me) are only fetched when a tab/card is opened and cached 60 minutes; a **daily 06:30 UTC warm-up** (`internal/server/warmup.go`) runs the real handlers once so they stay fresh, and sends a Telegram alert for any that still fail. The monitor allows them 36 h. All provider calls go through `health.Record` (Spec 7), which also feeds the per-day counters. **At startup** FT sends one Telegram alert naming any missing required provider variable (names only).

## 9. Environment variables

In `/etc/ft/env` (mode 0600, root:root). Names only — values never leave that file.

**Set on Jarvis today:** `FT_FINNHUB_API_KEY`, `FT_TWELVEDATA_API_KEY`, `FT_GITHUB_TOKEN`, `FRED_API_KEY`, `NEWSAPI_API_KEY`, `FT_COINGECKO_API_KEY`, `FT_ANTHROPIC_API_KEY`, `FT_ETORO_API_KEY`, `FT_ETORO_USER_KEY`, `FT_BARGO_API_KEY` (SC-47). **Not yet set unless noted in §13:** `FT_TELEGRAM_BOT_TOKEN`, `FT_TELEGRAM_CHAT_ID` (needed by the connectivity monitor and the LLM budget alerts; the Telegram bot itself keeps its own copy in `/etc/ft-bot/env`).

**eToro key pair — easy to swap by mistake:** `FT_ETORO_API_KEY` is the *Public Key* shown on eToro's settings page (sent as `x-api-key`); `FT_ETORO_USER_KEY` is the long key generated by "Create Your API Key" (sent as `x-user-key`). Swapped keys give a 401.

**Read by the code, optional / defaulted:** `CRYPTOPANIC_API_KEY`, `FT_TELEGRAM_BOT_TOKEN`, `FT_TELEGRAM_CHAT_ID`, `FT_ETORO_API_BASE`, `FT_VIDEO_DIGEST_ROOT` (default `/var/lib/video_digest`), `FT_THESIS_REPO_OWNER` / `_NAME` / `_DIR`, `FT_THESIS_SYNC_EVERY` (5m), `FT_CRYPTO_INDICATORS_DATA_DIR`.

**Runtime:** `FT_ADDR` (`:8081`), `FT_BASE_URL`, `FT_DB_PATH`, `FT_SESSION_DAYS` (30), `FT_COOKIE_SECURE`, `FT_COOKIE_DOMAIN`, `FT_REFRESH_INTERVAL` (15m; 0 disables).

Backups have their own credentials outside `/etc/ft/env` (`/etc/ft/backup.env` and the backup tools' own config — see SC-37 in the change log). The Google Drive mirror uses an rclone remote owned by user `curran`, scope `drive.file`.

## 10. Iteration loop

As actually practised (the laptop has no Go toolchain; the server's GitHub deploy key is read-only):

1. Claude.ai (ai) writes a build handover; Claude Code (cc) confirms its premises against the code before building.
2. cc edits `/opt/ft/src` on Jarvis as user `ft`, then `go build ./... && go vet ./... && go test ./...` there (Go 1.26).
3. Commit on Jarvis — code plus this spec: `tools/spec_archive.py bump` (short entry in §13, full entry in the archive), the affected sections 1–12 and the `reference_reviewed` marker; `tools/spec_archive.py check` and `go test ./internal/scorecards/` must pass.
4. `sudo /opt/ft/bin/deploy.sh` — pulls, builds, installs, restarts, checks `/healthz`.
5. Update the live spec row in the DB from the committed file. The Drive mirror picks it up within 10 minutes.
6. Bundle the commit back to the laptop clone (`~/ft-src`) and `git push` to GitHub from there.
7. cc writes a status report for ai covering each acceptance criterion, including anything not verified.

## 11. CLI subcommands

```
ft                                        run server
ft serve                                  run server (explicit)
ft seed [--user-id N]                     load Fin's holdings
ft daily [--user-id N] [--days 365]       daily job (history, calendar, beta, 12m vol)
ft backfill-bars [--user-id N] [--range 2y]   Spec 9c daily OHLC
ft perf-derive [--user-id N]              Spec 9d derive closed_trades + snapshots
ft sector-backfill [--months 14]          Spec 9f one-off ETF history
ft sector-ingest                          Spec 9f manual daily ingest
ft nexus-ingest | nexus-backfill | nexus-compute | nexus-fundamentals    SC-36 AI Nexus
ft video-digest-reground                  SC-43 re-ground stored digest timestamps (no LLM call)
ft token create --user-id N --name X      mint a bearer token (plaintext shown once)
ft token create-mcp ...                   mint a read-scoped token for the MCP connector (SC-41)
ft token list                             list tokens (no plaintext)
ft token revoke --id N [--user-id N]      revoke a service token
ft oauth status | revoke-all | revoke-client --id ID | create-client [--name N] [--redirect URI] [--secret]   SC-41b OAuth sign-ins
ft help
```

`ft help` was brought in line with this list in v1.79.0.

## 12. What's deferred — known queue

Current as of 2026-10-12 (verified against the code and the repo unless marked):

| Item | Purpose | Status |
|------|---------|--------|
| P5 — FT snapshot report | Generated report (theses, gap report, watchlist, regime, latest video-digest week) written to `FT-Bridge/reports/ft-snapshot.md` | Not built. Transport ready (SC-45); the file there is a placeholder |
| SC-43 P2 | OCR of Cowen chart frames to fill the snapshot columns left NULL in P1 (200WMA, log band, dominance, ETH/BTC) | Not built |
| SC-43 P3 / P4 | Regime read surfaced in the UI; digest query tool | Not built — `suggested_regime` / `regime_read` are stored but not exposed |
| SC-04 batch automation | Thesis updates at scale | Parked pending Fin's decisions on model / scope / sourcing |
| Cron scrapers (Farside) | Restore on the Lenovo, or replace | **Farside: fixed 2026-10-11** — the post-migration breakage had three independent causes: (1) cron logs pointed at `/var/log` (now `~curran/logs`); (2) `curran` could not reach/write `/var/lib/ft/data/{farside,capitol-trades}` (`curran` joined group `ft`, dirs `curran:ft` 2775 — `/var/lib/ft` itself stays 750 and `ft.db` is mode 644, so it is not opened to everyone); (3) Playwright and Chromium were not installed (`node_modules` and `~/.cache` are outside the backups; installed 2026-10-11, v1.60). All captured in `ops/scrapers/install.sh`; a logrotate rule keeps 8 weekly logs. **Capitol Trades: replaced by Bargo (SC-47, 2026-10-11)** — the cron was removed. Farside: the ETF-flow **card** stays live regardless (FT ignores a cache file older than 36 h and fetches directly), but the **chart** reads the cache file with no freshness check, so a stale file shows silently |
| Manual eToro statement fallback | Retire or keep as break-glass | Kept (permanent break-glass). Fixed 2026-10-11: wallet scoping, live units, SU/SU.PA matching, audit rows (see §5). Remaining limit: a statement can't see partial closes (when live data exists it no longer proposes updates from the statement). Still open: the performance importer's handling of multi-year statements (whole statement attributed to the End-Date year), "Income from Refunds" in the Financial Summary, and its €-only reconciliation warning — ai to spec |
| eToro crypto | First real eToro-held crypto position | Path is test-only; FT's EUR-based crypto refresh may recompute the USD cost fields SC-44 writes — check on first use |

*(older queue — checked 2026-10-12 against the code and the spec files in the research repo:)*

| Spec | Purpose | Status |
|------|---------|--------|
| 9e (old numbering) | Correlation matrix tracking | **Not built** (no matrix code; the only correlation in FT is the score-vs-outcome calibration, SC-38). Original rationale (30+ positions) not re-checked |
| 9h | Real-time technicals monitoring (Finnhub WebSocket level breaks) | **Not built** (no WebSocket code). Note Finnhub's free tier is rate-limited to 60 calls/min (§8) |
| 9i | *(label corrected)* The repo spec `FT_Spec_9i` is "Three-Framework Integration Architecture v1 (draft)". The crypto scoring engine shipped as D25 (§3, §5); the stock-side status of 9i was **not verified** | Mixed; see note |
| 16 | Alert strategy overhaul | **Closed as unverified (2026-10-13):** no record of it in the archive or backlog; SC-35 (Percoco levels + alert intelligence, §5) shipped later. Reopen only if Fin names it |
| 13 (numbering collision) | "Test coverage" vs "Score automation engine" — rename one | **Closed as unverified (2026-10-13):** the archive records Spec 13 = test coverage as shipped (v1.4, 2026-05-18); no second "Score automation engine" 13 exists in the archive, backlog or repo. The only numbering collision on record is SC-36 vs SC-40, resolved in v1.61.0 |

## 13. Recent changes and the archive

The ten newest changes, newest first, in short form (≤ 800 characters each). The full text of these, and every older change, is in the archive: `docs/spec-archive/` in the repo (`INDEX.md` maps every version and pass to its file), mirrored to Drive at `FT-Bridge/spec/archive/`. Anything that must stay true is in sections 1–12; this list only records that something changed.

### v1.82.0 · 2026-10-13 · pass 83 · SC-49

**ETF-flow freshness guard.** The ETF-flow card and chart now show the data's own date and an amber/red badge by US trading days (3–4 / 5+), the monitor judges the newest row's date (not the file's time) and names whether the scraper or the source is at fault, and Diagnostics shows the age. Migration 0052. Behaviour: yes. Reference updated: §3, §4, §8. Supersedes v1.81.1.

### v1.81.1 · 2026-10-13 · pass 82 · SC-48

**RSS User-Agent fix.** Nasdaq's feed reset connections whose User-Agent contained a URL; the RSS fetcher now sends a plain `FT-Dashboard` UA, which all nine feeds accept. Behaviour: yes. Migrations: none. Reference updated: §8. Supersedes v1.81.0.


### v1.81.0 · 2026-10-13 · pass 81 · SC-48

**RSS cut-over, heatmap, theses.** Market News now reads free publisher RSS (CNBC, BBC Business, Nasdaq as primary; MarketWatch and The Guardian as measured backups) with per-feed health; NewsAPI is no longer called. The heatmap tiles refresh every ~30 minutes while their market is open instead of every 15 around the clock. The six crypto theses with empty text were restored from their originals (scores unchanged). §12 rows 16 and 13 closed as unverified. Behaviour: yes. Migrations: none. Reference updated: §8, §12. Supersedes v1.80.1.



### v1.80.1 · 2026-10-13 · pass 80 · SC-47

**Scheduler fix.** Four scheduled jobs had never run because `scheduleAt` blocked after the first job in each group: the Bargo/Congress ingest, the executive-order ingest, the quarterly legislator refresh and the weekly Nexus fundamentals. Each job now has its own goroutine, with a regression test. The Bargo alert was a true positive; the key, quota and monitor rule are fine. Behaviour: yes. Migrations: none. Reference updated: §4. Supersedes v1.80.0.




### v1.80.0 · 2026-10-12 · pass 79 · SC-48

**Data-provider fixes.** Finnhub is rate-gated, cached and limited to US holdings; heatmap tiles use Yahoo; Twelve Data retired; CNN Fear & Greed works again (Referer fix); a daily warm-up keeps the request-driven feeds fresh and alerts if one fails; market-news RSS is built but not yet switched on; per-provider daily call counters and usage lines in Diagnostics; a startup alert for missing provider keys; budget-alert path tested. Behaviour: yes. Migrations: 1. Reference updated: §8, §12. Supersedes v1.79.1.





### v1.79.1 · 2026-10-12 · pass 78 · SC-47

**Decisions applied.** AVGO and RR.L re-tagged, connector token #12 revoked, OpenClaw's dead FT skill retired; the eToro sync transaction is now `BEGIN IMMEDIATE` (fixes the intermittent `database is locked (517)`); the ETH thesis prose in the DB replaced by the amended original (scores unchanged); the unused legacy PPG gate removed; per-ticker stock theses added to the Drive library. Behaviour: yes. Migrations/endpoints: none. Reference updated: §4. Supersedes v1.79.0.






### v1.79.0 · 2026-10-12 · pass 77 · SC-41b

**OAuth for the claude.ai connector; login rate limiting.** FT now runs its own single-user OAuth 2.1 server (discovery, registration, authorize with Fin's password, token with PKCE and rotating refresh tokens) so claude.ai can connect to `/mcp` with the URL alone; the 401 carries the OAuth discovery header and `/.well-known/*` no longer returns the SPA page. Login and authorize are now rate limited (5 failures per IP per 15 min) with a Telegram alert; migration 0050; `ft oauth` and `ft token revoke` commands; `ft help` aligned. Behaviour: yes. Migrations: 1. Reference updated: §3, §5, §11. Supersedes v1.78.1.







### v1.78.1 · 2026-10-11 · pass 76 · SC-47

**Drive library mirror and bridge alerting.** The research library (specs incl. the 9l chain, handovers, scoring logs, crypto adapters and theses) is mirrored one-way to Drive `FT-Bridge/library/` every 10 minutes (changed docs only), with an INDEX of freshness and two DB-generated live tallies. Both Drive mirrors now report success to the connectivity monitor, so a failing mirror alerts over Telegram. Behaviour: yes. Migrations/endpoints: none. Reference updated: §4. Supersedes v1.78.0.








### v1.78.0 · 2026-10-11 · pass 75 · SC-47

**Bargo Congress feed, raw history, connectivity monitor.** Congress trades now come from Bargo (free key; Capitol Trades cron retired) into a new unfiltered `congress_trades_raw` history table (migration 0049) that outlives Bargo's 3-month window; recent rows feed the Signals tab, which now carries Bargo's required attribution. A freshness monitor watches every external source (price APIs, Bargo, eToro sync, Farside, backups) and sends one Telegram alert per outage, a 24 h reminder and a recovery note; Diagnostics shows it. Needs `FT_TELEGRAM_*` in FT's env. Behaviour: yes. Migrations: 1. Reference updated: §2–5, §8, §9, §12. Supersedes v1.77.3.









### v1.77.3 · 2026-10-11 · pass 74 · SC-17

**Statement-fallback fixes; scrapers restored.** The fallback now reconciles eToro-wallet crypto only, uses live SC-42 units (derived units only for USD listings, so no false RR.L/4063.T drift), matches `SU` to `SU.PA`, never applies a partial-close-blind statement over matching live data, and audits every applied change. The master-file import rejects eToro statements. Playwright installed; the Farside job works again (`ops/scrapers/`); Capitol Trades is still blocked by the site. Behaviour: yes. Migrations/endpoints: none. Reference updated: §4, §5, §7, §12. Supersedes v1.77.2.










---

*Personal use only. Not investment advice.*

<!-- END FT-master-spec v1.82.0 -->
