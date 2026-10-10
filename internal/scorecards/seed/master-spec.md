# FT — Master Spec (living document)

> **What this is.** The single canonical record of how FT works *right now*. Updated after every shipped change. If something in production behaves differently from what's here, this doc is wrong and should be fixed.
>
> **What this is not.** Not a roadmap, not requirements. Future work lives in the deferred-specs list at the bottom; here only describes what's deployed.
>
> **Editing.** Click `Edit` to update inline. `Save` for a tweak; `Save as new version` for a substantive change (records the changelog).
>
> **Current version: v1.77.0 · 2026-10-10** — latest change: Spec restructured; history moved to an archive (§13).
>
> Sections 1–12 are the current state of FT. §13 lists the ten newest changes in short form; every older entry, and the full text of these, is in the archive — `docs/spec-archive/` in the repo (`INDEX.md` maps every version and pass to its file), mirrored to Drive at `FT-Bridge/spec/archive/`.

---

<!-- reference_reviewed: v1.77.0 2026-10-10 -->

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
| 6 | Signals | 9k, SC-23, SC-24 | Insider (SEC Form 4), Congress, executive-order, OGE 278-T, named political-figure tracker, 13F institutional tracker. Tab badge on alarms |
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
| 19 | Settings | 3, 7, 9b, 9c, 9c.1, 11 | Portfolio risk dashboard, LLM spend dashboard, diagnostics + provider health, deleted-holdings restore, audit log, regime history |

Top bar: brand · market pill (all 7 exchanges, click-to-focus) · regime pills (Jordi / Cowen / Effective) · refresh status · refresh / import / save master / ⌘K palette / user / sign out.

## 3. Schema — every migration

48 migrations applied (latest `0048_etoro_reconcile.sql`).

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
| Signals ingest | 23:00 / 23:10 / 23:20 | SEC Form 4 insiders (firehose + per-ticker), Congress, executive orders |

**On Jarvis, outside the FT process:**

| Job | Schedule | What |
|-----|----------|------|
| `ft-bridge-spec.timer` | every 10 min | SC-45: exports the working spec (live DB row) and the archive (`docs/spec-archive/`) to Google Drive (`FT-Bridge/spec`, `FT-Bridge/spec/archive`) when either changes |
| `video-digest.timer` | Sun 20:00 + Mon 08:00 Europe/Dublin | SC-43 P0: fetches new Jordi/Cowen videos into `/var/lib/video_digest` |
| `ft-backup.timer`, `ft-restore-verify.timer` | systemd timers | SC-37 backup / DR and restore verification |
| DB backup (cron, user `ft`) | 03:15 daily | `/opt/ft/bin/backup-db.sh` |
| Jarvis config backup (cron) | Sun 03:30 | `/opt/ft/bin/backup-jarvis-config.sh` |
| Whole-box backup (cron) | 03:30 daily | `jarvis_backup.sh` — restic to Backblaze B2 |
| Capitol-trades fetch (cron) | 23:05 daily | Feeds the Congress signal ingest |
| Farside ETF-flow fetch (cron) | 00:25 daily | Feeds the Crypto Indicators ETF-flow series |
| FT Telegram bot | see §6 | Proactive alert crons |

## 5. Endpoints — by area

Cookie auth unless marked **T** (cookie or bearer token). About 190 routes; grouped, not exhaustive per verb.

**Auth:** `GET /api/auth/{state,me}`, `POST /api/auth/{setup,login,logout}`. `GET /healthz` (no auth).

**Holdings:** `GET /api/holdings/{stocks,crypto}`, `POST`, `PUT/{id}`, `DELETE/{id}` (soft), `POST/{id}/restore`, `GET .../deleted`. Stock-only setters: `PUT /api/holdings/stocks/{id}/{sector,sl-method,position-class,levels-source}`.

**Summary + status:** `GET /api/summary`, `/api/marketstatus`, `/api/marketstatus/all`, `/api/audit`

**Refresh:** `POST /api/refresh` **T**, `GET /api/refresh-status` **T**

**Import/export:** `POST /api/import/{preview,apply}` (master file, `.xlsx`/`.csv`), `GET /api/export.xlsx`, `GET /api/export.csv?tab=stocks|crypto|watchlist`, `GET /api/crypto/export.xlsx`

**eToro (SC-17, SC-42, SC-44):**
- Direct sync: `GET /api/etoro/sync` (last run + effective per-ticker SL/TP), `POST /api/etoro/sync` (run now).
- Approval queue: `GET /api/etoro/reconcile/proposals?status=`, `POST /api/etoro/reconcile/proposals/{id}/{approve,dismiss}`.
- Statement fallback (`.xlsx` only): `POST /api/etoro/reconcile/{preview,apply}` — the preview returns `coverage {complete, reason, startDate, endDate, oldestOpen, newestOpen}`; an incomplete statement has closure/drift rows withheld, and copy-trade lots are left out.
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

**Diagnostics + lookup:** `GET /api/diagnostics` **T**, `GET /api/lookup/ticker?q=&kind=`

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
- Every mutation writes a `holdings_audit` row with `changes_json` + optional `reason_code`
- `user_preferences` is the home for any new k/v setting; per-key validation in `validPreferenceValue()`
- Endpoints the bot or the MCP connector need accept a bearer token as well as the cookie (marked **T** in §5); everything else is cookie-only
- Append-only tables (`transactions`, `closed_trades`, `thesis_notes`, `sector_snapshots`, `sector_scorecard_versions`, `framework_scores`, `holdings_audit`, `etoro_holdings_lots`) — corrections via supersede/soft-delete, never UPDATE on data columns
- **Broker data rules:** eToro owns units, `invested_usd` and `avg_open_price` (taken from eToro's own USD figures — no FT-side FX math); FT owns stop/TP, `sl_method`, notes, sector tags and thesis links. Copy-trade positions are stored but never become FT holdings. Anything that changes *which* holdings exist needs explicit approval. Crypto reconciles only against rows whose wallet is `eToro`.
- **Demo mode** is enforced server-side; portfolio and broker data are never served in demo.
- **LLM calls** only through the `internal/llm` governor, each feature behind its own kill switch.
- **Methodology-notes registry is the canonical note list** — the numbered notes live in `cross_sector_research/theses/_methodology_notes_registry.md`, version-stamped, and integrity-checked by `tools/check_registry.sh`. Cite a note ONLY by its exact registry number + name; a `candidate` note is never citable as established. After editing the registry, bump its `REGISTRY v<N>` stamp or `check_registry.sh` fails.
- **Registry + doctrine drop-ins are re-exported on change** (set 2026-06-10) so the ai-side reads the fresh, stamped copy.
- **Master Spec is bumped on every completed section or spec** (set 2026-05-19). Versioning: patch for polish batches, minor for new specs/features, major for breaking architecture. **Each bump (rule changed 2026-10-10, v1.77.0):** (a) run `tools/spec_archive.py bump --version X.Y.Z --full FULL.md --short SHORT.md` — it appends the *full* entry (one `> **Overhaul:** …` line, no length limit) to the archive, puts the *short* entry (≤800 characters: what and why in two sentences, behaviour change yes/no, migrations/endpoints, binding rulings, `Supersedes vX`) at the top of §13, drops the oldest beyond ten, and refreshes the current-version line, the END marker and `docs/spec-archive/INDEX.md`; (b) **the affected parts of sections 1–12 are updated in the same commit** and the `reference_reviewed` marker is set to the new version (a bump that only adds an entry leaves the reference stale — how these sections drifted from v1.5x to v1.75); (c) `tools/spec_archive.py check` and `go test ./internal/scorecards/` must pass; (d) the commit includes the spec and the archive; (e) the live DB row is updated after deploy. Anything that must stay true goes in sections 1–12, not in a change entry. Archive text is **never edited** — a correction is a new entry.
- **The spec is mirrored to Google Drive** (`FT-Bridge/spec`, SC-45): `FT-master-spec` is the whole working spec from the live DB row — about 40,000 characters, ending with `<!-- END FT-master-spec vX.Y.Z -->` so a reader can tell it got everything — `FT-spec-current` is the index and freshness page, and `spec/archive/` carries the archive. Claude's Drive read stops near 100,000 characters, so no file may be larger than that. The mirror marks the reference STALE whenever the `reference_reviewed` marker is behind the spec version or the live schema is ahead of the migrations these sections mention.

## 8. Provider chain

*(carried over, with the eToro, FRED and CoinGecko-key rows added 2026-10-09; the other rows were not re-verified.)*

| Domain | Primary | Fallback | Notes |
|--------|---------|----------|-------|
| US stock quote | Finnhub | TwelveData → Yahoo | TwelveData free is US-only since 2024 |
| Non-US stock quote | Yahoo (crumb dance) | — | Fragile; updates every few months |
| Stock history (sparkline + 12m vol) | Yahoo `v8/chart` | — | 365-day window |
| Stock fundamentals (beta, calendar, targets) | Yahoo `quoteSummary` | — | Patchy for non-US |
| Broker positions, SL/TP, invested USD | eToro Public API (`public-api.etoro.com`) | manual `.xlsx` statement upload | Key pair in env; 60 req/60 s shared limit; one call per 30-min sync |
| Crypto quote | CoinGecko `/simple/price` | serve-stale | Demo key via `FT_COINGECKO_API_KEY` (SC-18) |
| Crypto history | CoinGecko `/market_chart` | — | Sequential w/ 2.5s gap |
| Crypto market screener | CoinGecko top 250 | — | SC-21 |
| Macro + Pal indicators | FRED | manual ISM entry | `FRED_API_KEY` |
| FX EUR→USD | Frankfurter | static fallback 1.08 | No key needed |
| Stocks F&G | CNN `dataviz.cnn.io` | — | Unofficial; UA-gated |
| Crypto F&G | alternative.me | — | Stable |
| News (stocks) | NewsAPI | stale cache | Free 100 req/24h |
| News (crypto) | CryptoPanic | stale cache | Free tier |
| Sector ETFs | Yahoo daily closes | — | 36 ETFs × 1 call/day |
| Insider / 13F filings | SEC EDGAR | — | Daily ingest; 13F on demand |
| Video digests | Anthropic API via the governor | — | Haiku; packages come from the Jarvis yt-dlp pipeline |

All provider calls wrapped with `health.Record` (Spec 7) for diagnostics surfacing.

## 9. Environment variables

In `/etc/ft/env` (mode 0600, root:root). Names only — values never leave that file.

**Set on Jarvis today:** `FT_FINNHUB_API_KEY`, `FT_TWELVEDATA_API_KEY`, `FT_GITHUB_TOKEN`, `FRED_API_KEY`, `NEWSAPI_API_KEY`, `FT_COINGECKO_API_KEY`, `FT_ANTHROPIC_API_KEY`, `FT_ETORO_API_KEY`, `FT_ETORO_USER_KEY`.

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
ft help
```

`ft help`'s built-in text is older than this list — it omits the `nexus-*`, `video-digest-reground` and `token create-mcp` commands.

## 12. What's deferred — known queue

Current as of 2026-10-09:

| Item | Purpose | Status |
|------|---------|--------|
| P5 — FT snapshot report | Generated report (theses, gap report, watchlist, regime, latest video-digest week) written to `FT-Bridge/reports/ft-snapshot.md` | Not built. Transport ready (SC-45); the file there is a placeholder |
| SC-43 P2 | OCR of Cowen chart frames to fill the snapshot columns left NULL in P1 (200WMA, log band, dominance, ETH/BTC) | Not built |
| SC-43 P3 / P4 | Regime read surfaced in the UI; digest query tool | Not built — `suggested_regime` / `regime_read` are stored but not exposed |
| SC-04 batch automation | Thesis updates at scale | Parked pending Fin's decisions on model / scope / sourcing |
| Manual eToro statement fallback | Retire or keep as break-glass | Kept. Known limits: can't see partial closes; names SU.PA as "SU"; needs SC-42 data to exclude copy-trades |
| eToro crypto | First real eToro-held crypto position | Path is test-only; FT's EUR-based crypto refresh may recompute the USD cost fields SC-44 writes — check on first use |
| `ft help` text | Bring the built-in usage text in line with §11 | Open, cosmetic |

*(carried over from the earlier queue, not re-verified:)*

| Spec | Purpose | Status |
|------|---------|--------|
| 9e (old numbering) | Correlation matrix tracking | Drafted; awaiting 30+ positions to justify |
| 9h | Real-time technicals monitoring (Finnhub WebSocket level breaks) | Deferred until alert noise is a felt problem |
| 9i | Adapter scoring engine | Drafted in handoff |
| 16 | Alert strategy overhaul | Needs alert-noise audit first |
| 13 (numbering collision) | "Test coverage" vs "Score automation engine" — rename one | Open |

## 13. Recent changes and the archive

The ten newest changes, newest first, in short form (≤ 800 characters each). The full text of these, and every older change, is in the archive: `docs/spec-archive/` in the repo (`INDEX.md` maps every version and pass to its file), mirrored to Drive at `FT-Bridge/spec/archive/`. Anything that must stay true is in sections 1–12; this list only records that something changed.

### v1.77.0 · 2026-10-10 · pass 71 · SC-45

**Spec restructured; history moved to an archive.** Working spec is now sections 1–12 + the 10 newest changes (§13), ~40k chars (was 409k); all 66 change-log entries and 97 old §13 rows moved byte-for-byte to `docs/spec-archive/` (`INDEX.md`; Drive `FT-Bridge/spec/archive/`). Behaviour: no. Migrations/endpoints: none. Reference updated: §4, §7, §10, §12. Binding: each bump = `tools/spec_archive.py bump` (short entry here, full entry to archive) + affected §1–12 + `reference_reviewed` + `check`; archive text never edited. Drive `FT-master-spec` is now the whole working spec. Supersedes v1.76.0.

### v1.76.0 · 2026-10-09 · pass 70 · SC-45

**Sections 1–12 rewritten from the build; Drive bridge recorded.** Reference had drifted since ~v1.5x (12 of 19 tabs, migrations to 0033) because bumps only added change-log entries. Re-derived from code and Jarvis; bot, most providers and older deferred items marked *carried over*. SC-45: rclone `drive.file` bridge to `FT-Bridge/`, 10-min spec mirror. Behaviour: no. Migrations/endpoints: none. Binding: a bump must update affected §1–12 and the `reference_reviewed` marker in the same commit. Supersedes v1.75.2.

### v1.75.2 · 2026-10-06 · pass 69 · SC-17

**Statement fallback excludes copy-trade lots; Stocks-tab entry point.** The statement can't mark copied opens, so a full-history upload proposed FIG, MSFT, UNH, UPS (copy-trades); position IDs SC-42 has seen as copy lots are now skipped and counted. Added "Reconcile with eToro statement" to the Stocks toolbar. Behaviour: yes (fallback only). Endpoint: `/api/etoro/reconcile/preview` gains `copySkipped`. No migration. Binding: copy-trade positions never become FT holdings by any route. Supersedes v1.75.1.

### v1.75.1 · 2026-09-27 · pass 68 · SC-17

**Statement fallback guards against incomplete statements.** The upload is `.xlsx` only (CSV-only is the separate transactions import). A short-range statement rebuilt 0 holdings silently and would have proposed closing everything. The preview now checks the statement's dates against the oldest/newest open eToro lot; if incomplete, closure and drift rows are withheld server-side, with a banner. Behaviour: yes (fallback only). Endpoint: `preview` gains `coverage`. No migration. Binding: statements must run from account opening to today. Supersedes v1.75.0.

### v1.75.0 · 2026-09-27 · pass 67 · SC-44

**Automatic holdings reconcile from eToro.** After each SC-42 sync, matched holdings' eToro-owned values (`invested_usd` from eToro's own USD amount, `avg_open_price`) update silently; adds/removals queue in `etoro_reconcile_proposals` and apply only on approval (removals soft-delete, thesis kept). Crypto reconciles only against `wallet='eToro'`. Behaviour: yes. Migration 0048. Endpoints: `/api/etoro/reconcile/proposals{,/{id}/approve,/{id}/dismiss}`. Binding (Fin): eToro owns units/invested/avg; FT owns stop/TP, sl_method, notes, tags, thesis links; thesis-linked removal allowed on approval (differs from SC-17). Supersedes v1.74.1.

### v1.74.1 · 2026-09-27 · pass 66 · SC-42

**Near-zero eToro stops count as no stop.** First live sync (98 positions, 22 own tickers): eToro reports some stops as enabled at meaningless prices (SLV 0.0001, 4063.T 0.01); a long's stop below 5% of its open price now counts as no stop, raw value kept in the lot history. Also found: FT's `stock_holdings` was stale vs eToro (fixed by SC-44). Behaviour: yes. No migration/endpoint. Binding: the `isNoStopLoss` flag alone is not enough. Supersedes v1.74.0.

### v1.74.0 · 2026-09-26 · pass 65 · SC-42

**eToro direct sync: holdings and broker SL/TP.** FT polls eToro's Public API (key pair, every 30 min, no LLM) into per-lot change history and per-ticker effective levels (longs: highest stop, lowest take-profit; shorts mirrored). Behaviour: yes. Migration 0047. Endpoints: `GET/POST /api/etoro/sync`. Binding (Fin): eToro levels shown alongside FT's own SL/TP, alerts unchanged; copy-trade lots stored but excluded from levels; manual upload kept as fallback; an empty portfolio is treated as an API glitch. Supersedes v1.73.1.

### v1.73.1 · 2026-09-26 · pass 64 · SC-43

**Video digest fix-up after the first real run.** 4 long videos truncated at the 2,000-token output cap; model timestamps drifted; 13 of ~27 quotes rejected. Fixed: output cap 4,000 (Fin); mention timestamps re-anchored to where the name is spoken (no match, no timestamp); quote matching ignores fillers; new `ft video-digest-reground`. Behaviour: yes. No migration. Binding: quotes must still be verbatim in the transcript. Supersedes v1.73.0.

### v1.73.0 · 2026-09-26 · pass 63 · SC-43

**Video Digest Brain P1: schema, ingest, tab.** FT sweeps `/var/lib/video_digest` itself (boot+2 min, daily 09:15 UTC) and makes one governor LLM call per new video (Haiku, feature `video_digest`); output validated, unverifiable quotes dropped; Cowen snapshot asks only the (b) fields. Behaviour: yes. Migration 0046 (6 tables). Endpoints: `GET /api/video-digest`, `POST /api/video-digest/ingest`. Binding (Fin): FT sweeps (no Jarvis cron POST); governor input cap 30,000 tokens. Supersedes v1.72.1.

### v1.72.1 · 2026-09-08 · pass 62 · SC-41

**`/mcp` request logging fix.** `requireReadToken` logged only successful calls, so rejected requests left no trace; every hit on `/mcp` now logs at the middleware with the rejection reason. It showed claude.ai's connector reaches FT but sends no bearer token (the custom-connector dialog had no auth field): a connector-auth gap, not FT or Cloudflare. Behaviour: logging only. No migration/endpoint. Open: OAuth 2.1 support would be a separate, larger SC-41 follow-up. Supersedes v1.72.0.

---

*Personal use only. Not investment advice.*

<!-- END FT-master-spec v1.77.0 -->
