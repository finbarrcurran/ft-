# FT — Master Spec (living document)

> **What this is.** The single canonical record of how FT works *right now*. Updated after every shipped change. If something in production behaves differently from what's here, this doc is wrong and should be fixed.
>
> **What this is not.** Not a roadmap, not requirements. Future work lives in the deferred-specs list at the bottom; here only describes what's deployed.
>
> **Editing.** Click `Edit` to update inline. `Save` for a tweak; `Save as new version` for a substantive change (records the changelog).
>
> **Last meaningful overhaul:** 2026-05-30 (PM late) — Spec 9l v0.5 patch §L resolutions locked from Claude.ai response (Batch 3a authoring approved + 0033 timing after Batch 3a + v0.5 rounding deferred to quarterly audit + LINK 14/18 honoured with MD prose amendment in Batch 3d + D25 sequenced ahead of Spec 9m). Claude Code on standby until Batch 3a delivers. Earlier (PM): batch 2 ingest landed (LINK/SOL/POL/AVAX). Earlier (AM): v0.4 + Phase 1 ship gate at 8/10.

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
| — | Crypto Indicators | 9e | BTC-primary regime layer — Cowen 4-phase, Pal macro, ETF flows, F&G, stablecoin supply |
| — | Crypto Theses | 9l | 8-adapter Repository (BTC + L1 + L2 + DeFi + Infra + DePIN + RWA + Speculative). Phase 1 = adapter repository only; Scoring Engine + per-coin theses pending locked adapter MDs |
| — | Signals | 9k | Political + insider signal tab (SEC EDGAR per-ticker, capitol-trades, OGE) |
| — | Stock Theses (was "Theses") | 15 | Renamed 2026-05-29 to disambiguate from new Crypto Theses sibling. Same GitHub-backed library + earnings-revision warnings |
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

**Import/export:** `POST /api/import/{preview,apply}`, `GET /api/export.xlsx`, `GET /api/export.csv?tab=stocks|crypto|watchlist` (v1.5)

**Spec 15 — Thesis Library:** `GET /api/theses{,/gaps,/{id}}`, `POST /api/theses/{upload,scoring-log,sync}` (v1.6, scoring-log added v1.7). GitHub repo `finbarrcurran/cross_sector_research` is the source of truth; FT keeps a local clone at `/var/lib/ft/research/` synced every 5 min. Theses rows tagged with ownership (owned/watchlist/other) via JOIN to stock_holdings + watchlist; UI groups them into three sections (v1.7).

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
- **Master Spec is bumped on every completed section or spec** (set 2026-05-19, refined same day). A "section" = a spec shipping, a feature batch completing, a sub-phase landing, a polish batch wrapping, or an architectural decision being made. **Not** every commit — typo fixes, single parser tweaks, comment-only changes, test-only commits do NOT bump on their own; they roll up into whatever section they're part of. Versioning: patch (v1.7.x) for polish batches, minor (v1.8) for new specs/features, major (v2.0) for breaking architecture. Each bump: (a) new row in §13, (b) commit includes the spec change, (c) live DB updated via SQL UPDATE after deploy.

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

Optional: `FT_TWELVEDATA_API_KEY`, `NEWSAPI_API_KEY`, `CRYPTOPANIC_API_KEY`, `FT_ANTHROPIC_API_KEY`, `FT_TELEGRAM_BOT_TOKEN`, `FT_TELEGRAM_CHAT_ID`, `FT_GITHUB_TOKEN` (Spec 15 Thesis Library, fine-grained PAT scoped to cross_sector_research).

Runtime: `FT_ADDR`, `FT_DB_PATH`, `FT_REFRESH_INTERVAL`, `FT_COOKIE_SECURE`.

Off-site backup creds (NOT in `/etc/ft/env` — they live in rclone's own config): `/var/lib/ft/.config/rclone/rclone.conf` holds the Cloudflare R2 access key + secret + endpoint for daily DB snapshots (v1.7.3). Mode 0600, ft:ft. Bucket: `ft-backups`. 90-day retention.

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
| ~~12 D5g~~ | ~~EUR toggle on stocks P&L column~~ | ✅ Shipped 2026-05-18 — `$ / €` toggle in P&L header, persists via `pnl_currency` preference, converts via the existing FX snapshot |

## 13. Version history of this doc

| Version | Date | Notes |
|---------|------|-------|
| 1.0 | 2026-05-17 | Initial author at the close of Specs 9f + 9g build. Captures every spec 1 → 12 + 9b/c/c.1/d/f/g + 7 + 10 + 11 + 11b. |
| 1.1 | 2026-05-17 | Spec 14 (per-holding theses) shipped. Added migration 0021 + thesis endpoints + Detail-page UI block. |
| 1.2 | 2026-05-17 | Handoff package v2 — Spec 9f D9 Whitespace Watchlist view shipped (filter pills, "+ watchlist" affordance carrying sector pre-selection). Migration 0022 retagged ORCL → data_center_reits + WPM → precious_metals_gold per Sector_Holdings_Mapping_v1.1. Watchlist schema extension: `sectorUniverseId` accepted on create. |
| 1.3 | 2026-05-17 | Pharma adapter v1 (draft) added to Scorecards repo. Applies to pharma_metabolic + pharma_immunology + gics_healthcare. Status `needs-review` (5 open decisions in §7 of the source MD). LLY + ABBV worked examples both calibrate to 13/16. |
| 1.4 | 2026-05-18 | Spec 13 — Test coverage on alert / technicals / portfolio_risk / performance / sector_rotation packages. ~95 test funcs, `go test ./...` clean. Spec 12 D5g — `$ / €` toggle on stocks P&L column. `pnl_currency` preference; converts via FX snapshot. |
| 1.5 | 2026-05-18 | **5 new sector adapters** (Defense / Mining & Metals / Industrial Electrical / AI Infra & Semis / Cloud Infra) — all v1 drafts, status `needs-review`, with open decisions in §7 of each source MD. Applies-to-sector wiring drives the 📋 button across the Sector Rotation tab. **Watchlist Market column** — per-row open/closed badge resolved by ticker suffix (matches Stocks tab behaviour). **Per-tab CSV export** — `GET /api/export.csv?tab=stocks\|crypto\|watchlist` + ⬇ Download CSV button on each tab toolbar. Flat column shape; no FX denormalization needed (Crypto rows already carry both EUR & USD columns). |
| 1.6 | 2026-05-18 | **Spec 15 — Thesis Library** shipped. New top-level **Theses** tab (between Scorecards and Watchlist) backed by the user's private GitHub repo `cross_sector_research`. Drop-zone accepts `<TICKER>_v<N>_locked.md` + optional `_scoring_log.md` and pushes both in one commit via stored fine-grained PAT (`FT_GITHUB_TOKEN` in `/etc/ft/env`, scoped to one repo, Contents:write only). Sortable/filterable index of every locked thesis with score / version / locked-date / next-earnings columns. Earnings-revision trigger: 🟡 amber ≤14d, 🔴 red ≤3d, 📝 revision_needed post-earnings if earnings > locked_date. "Stocks owned with no thesis" gap report. Inline goldmark+bluemonday MD renderer. Background sync cron pulls every 5 min. Endpoints: `GET /api/theses`, `GET /api/theses/{id}`, `GET /api/theses/gaps`, `POST /api/theses/upload`, `POST /api/theses/sync`. Migration 0023 creates `theses_index` table (cache; GitHub remains source of truth). Re-labels the Spec 14 holding-detail external-thesis-link field from "Notion / Google Doc" → "GitHub — cross_sector_research". |
| 1.7 | 2026-05-19 | **Spec 15 polish batch** — six incremental changes from the live-use shakedown of the Thesis Library: (1) **gap report split** into separate OWNED vs WATCHLIST sections (no more "+N more" truncation); (2) **multi-segment doctrine** — parser accepts `> **Primary Adapter:**` (RR.L pattern) in addition to `> **Adapter:**`; (3) **scoring-log-only uploads** — new endpoint `POST /api/theses/scoring-log` for methodology refreshes without a thesis attached, plus browser-suffix-tolerant filename matching (handles `_scoring_log (3).md` etc); (4) **Asset-Hedge framework parser support** (first piece of Spec 9i implementation, lives at `cross_sector_research/ft_specs/FT_Spec_9i_Three_Framework_Integration_v1.md` as a draft) — `> **Framework:** Asset-Hedge Scorecard` header line, `Instrument Type:` fallback for sub-type, new `asset_hedge` folder slug; (5) **per-framework score thresholds** — /16 passes at ≥12, /8 passes at ≥5 (Spec 9i calibration ladder), default ≥75% otherwise; (6) **keyword-based adapter routing fallback** — distinctive substrings (`semi`, `pharma`, `defence`, `oil`+`gas`, `precious`+`metals`, etc.) route correctly even when Gemini/Claude phrases an adapter name in a new way; (7) **Theses table grouped by ownership** — `theses_index` rows tagged `owned`/`watchlist`/`other` via JOIN to stock_holdings + watchlist; tab renders three stacked sections (OWNED green, WATCHLIST amber, OTHER dim if non-empty) so monitoring vs candidates are visually separated. Cross-sector research repo now has 9 locked theses: RHM.DE 14/16, ASML & LLY 12/16, ABBV/CLS/NVDA/RR.L/MTZ 11/16, GLD 5/8 (first Asset-Hedge). |
| 1.7.1 | 2026-05-19 | **Process change** — Master Spec is now bumped on **every FT commit** going forward (per user request 2026-05-19). Convention added to §7. Patch (v1.7.x) for polish, minor (v1.8) for new features, major (v2.0) for breaking architecture changes. Captures one-line "what changed" rationale per commit so the spec stays an honest record of how FT got to its current shape. |
| 1.7.2 | 2026-05-19 | **Process refined** — bump cadence relaxed from "every commit" to "every completed section / spec / sub-phase / polish batch / architectural decision." Typo fixes and single-line tweaks no longer trigger their own bump; they roll up into the next section bump. Same versioning scheme. §7 updated. |
| 1.7.3 | 2026-05-19 | **Off-site DB backup to Cloudflare R2.** Daily backup script (`deploy/backup-db.sh`, run 03:15 UTC via `/etc/cron.d/ft-backup`) now extends past the existing 14-day local rolling snapshot to also push to R2 bucket `ft-backups` via rclone. 90-day retention on the R2 side handled by `rclone delete --min-age 90d` at the end of each run. Credentials stored at `/var/lib/ft/.config/rclone/rclone.conf` (mode 0600, ft:ft). Fine-grained R2 API token scoped to one bucket (Object Read & Write). R2 upload failures log but don't fail the cron — local backup remains the primary recovery surface. Restore path: `rclone copy r2:ft-backups/ft-YYYY-MM-DD.db /var/lib/ft/ft.db.restored` + `systemctl stop ft` + swap files + `systemctl start ft`. |
| 1.7.4 | 2026-05-19 | **Weekly jarvis-config backup to R2** (`deploy/backup-jarvis-config.sh`, 03:30 UTC Sundays as root via `/etc/cron.d/ft-backup`). Tarballs everything needed to rebuild jarvis on a fresh Ubuntu box: `/etc/ft`, `/etc/ft-bot`, `/etc/cloudflared` (tunnel cert + named-tunnel JSON — the **most irreplaceable items**), `/etc/cron.d/ft-backup`, custom systemd units (ft / ft-bot / hct / cloudflared), `/var/lib/hct` (DB + attachments), `/var/lib/ft/.config/rclone/rclone.conf`, `/opt/ft-bot/*` (source not in git), `/opt/hct/*` (binary + state). Plus `packages.txt` (dpkg selections) and `os.txt` (uname + lsb_release). Includes a generated `RESTORE.md` with the 11-step rebuild walkthrough. Lands at `r2:ft-backups/jarvis-config/jarvis-YYYY-MM-DD.tar.gz`, 365-day retention. Combined with v1.7.3 daily DB backup = complete rebuild recipe. `deploy.sh` now also syncs `/etc/cron.d/ft-backup` from `deploy/ft-backup.cron` so cron stays git-tracked. |
| 1.7.5 | 2026-05-19 | **CSV import support** (`internal/persistence/csv_import.go`). The existing `POST /api/import/preview` endpoint now accepts `.csv` files in addition to `.xlsx`. Routes by filename extension. CSV schema matches the v1.5 per-tab export (`GET /api/export.csv?tab=stocks\|crypto`) so download → edit → re-upload is symmetric. Header row detects stocks (`ticker` + `invested_usd`) vs crypto (`symbol` + `quantity_held`). Single CSV upload populates only one kind; the other side stays untouched in the apply step. Same lenient parsing as xlsx (empty cells = NULL, `$/€/,/whitespace` stripped before ParseFloat, unknown columns ignored). Same slam-replace apply semantics as xlsx — preview diff shows additions/deletions before commit. Frontend import modal accepts `.xlsx,.xls,.csv`; ⌘K menu label updated to "Import xlsx / csv". |
| 1.7.6 | 2026-05-19 | **Import demote-to-watchlist** — stocks missing from a holdings import are now **moved to the watchlist** (not deleted). Carries ticker, company name, sector, sector_universe_id, current_price, thesis_link forward to the watchlist row. Sets a descriptive note ("Auto-moved from holdings on YYYY-MM-DD — removed via import (was invested $X)"). Idempotent: skips silently if the ticker already has an active watchlist row, so re-imports don't duplicate. Confirms what user explicitly wanted: (a) import never modifies watchlist entries (always true — they're separate tables), and (b) previously-held tickers that drop out of the import keep their context on the watchlist instead of vanishing. Preview modal shows a "↘ Will move to Watchlist (N)" panel before the user clicks Apply; completion screen confirms what moved. New store method `DemoteHoldingToWatchlist`. Crypto is unchanged for now (no demote pathway). |
| 1.7.7 | 2026-05-19 | **Import preview: stocks ↔ crypto independence made obvious.** When the imported file contains no rows for a kind (e.g. stocks-only CSV), the preview modal now shows a clear "✓ Your existing crypto will not be modified — this file contains no crypto data" panel for the absent section, with the checkbox + count chips + sheet tag hidden entirely. Previously the absent section showed a vague "No rows detected" message which read as worrying. No behaviour change — the data path was already safe (frontend defaults applyCrypto=false when cryptoCount=0; backend has `len(pending) > 0` guards) — just clearer communication. CSS adds an `.absent` modifier with dashed border + green-tinted success message. |
| 1.8.0 | 2026-05-20 | **Spec 9e Phase 1 sub-phase A — Crypto Indicators tab foundation.** New top-level **Crypto Indicators** tab between Crypto and Performance. Migration 0024 adds 5 tables (`crypto_indicators` seeded with 11 Phase 1 indicators, `crypto_indicator_snapshots`, `crypto_composite_snapshots`, `crypto_indicator_weights` seeded 25/20/35/20, `cowen_risk_manual`). New `internal/cryptoindicators/` package: scoring engine (linear / step / level_and_trend / trend_only function types, JSON-defined thresholds in `definitions/crypto_indicators.json`), composite engine (weighted bucket mean + redistribution + action band mapping: ≥60 strong_accumulate, ≥20 accumulate, >-20 neutral, >-60 caution, else distribute_wait), tooltips dictionary (layman language per indicator). New endpoints `GET /api/crypto-indicators` and `GET /api/crypto-indicators/composite/latest`. Tab UI: composite hero with -100..+100 scale bar + band label + 4 sub-score chips, 4 bucket sections with simple indicator cards (Phase 3 will replace with proper SVG gauge + 240×180 card grid). Sentiment bucket surfaces live F&G via existing `/api/feargreed` endpoint. **No data providers yet** — current_value/current_score columns are NULL until v1.8.1 (Phase 2 — FRED, DefiLlama, Farside, ISM, Cowen log-band) wires fetchers. Indicators render with "awaiting data" pills. F&G stays on Crypto News (already moved there by Spec 12 D10a, predating Spec 9e). |
| 1.8.1 | 2026-05-20 | **Import: invalidate Summary + Watchlist caches on done.** Previously the import completion screen only invalidated `state.stocks` and `state.crypto`, leaving `state.summary` and `state.watchlist` cached. Result: a user who navigated to Summary right after an import saw pre-import totals until a manual reload — which read as "the totals are wrong" even though the DB was correct. Adds both to the invalidation set in the import-done handler. No backend / DB / API change. (Spec 9e Phase 2 — data providers — bumps the next minor version, not the next patch.) |
| 1.8.2 | 2026-05-20 | **Spec 9e Phase 1 sub-phase B (part 1) — 5 indicators light up.** `internal/cryptoindicators/providers/` package added with three providers: `fred.go` (DGS2 + DTWEXBGS via api.stlouisfed.org with embedded 4w trend computed from the 60-obs window), `coingecko.go` (BTC dominance from `/global`, ETH/BTC ratio from `/simple/price`, BTC spot for snapshot row), `feargreed.go` (wraps existing `internal/market.FetchFearGreed`). New `refresher.go` orchestrates the sweep — fetches each provider sequentially with an 800ms gap, scores via the existing engine, upserts into `crypto_indicators`. Trend_4w for CoinGecko/F&G is computed from local snapshot history (`PriorSnapshotValue` SQL query, ±2 day window); stays NULL until 28 days accumulate. New endpoint `POST /api/crypto-indicators/refresh` triggers the same sweep on demand. Daily 00:30 UTC cron in `cmd/ft/main.go` runs `RefreshAllAndSnapshot` (refresh + write `crypto_indicator_snapshots` + `crypto_composite_snapshots`); also a 30s post-boot warm-up refresh so the tab has data on first visit. Frontend gains a `⟳ Refresh now` button next to "Auto-syncs daily at 00:30 UTC" hint. New config: `FRED_API_KEY` (env var) + `FT_CRYPTO_INDICATORS_DATA_DIR` (default `/var/lib/ft/data`). Indicators NOT wired in v1.8.2 (Cowen log-band, price_vs_200wma, risk_indicator, Farside ETF flow, DefiLlama stablecoin supply, ISM) keep showing "awaiting data"; v1.8.3 fills them. |
| 1.8.3 | 2026-05-20 | **Spec 9e Phase 1 sub-phase B (part 2) — remaining 6 indicators wired, all 11 now scoring.** Migration 0025 adds `btc_price_history` (cache table, ~3500 daily closes back to 2013, one-shot seed from CoinGecko `/coins/bitcoin/market_chart?days=max`). New `btc_history.go` runs the Cowen math: log-linear regression on log10(close) vs log10(days since CG genesis) → residual classified into quintiles (lower/mid_lower/mid/mid_upper/upper) for `cowen_log_band`; 200-week MA via weekly bucketize → `cowen_price_vs_200wma`; risk proxy = mean(normalised log_band, normalised price/200wma) for `cowen_risk_indicator` (2-input proxy per Spec 9e §D8 — MVRV-Z deferred to Phase 3). New providers: `defillama.go` (stablecoin supply with 4w ROC computed directly from `/stablecoincharts/all`), `farside.go` (BTC spot ETF flow CSV/HTML scrape with defensive parsing; HTML fallback if CSV fails), `ism.go` (reads `/var/lib/ft/data/ism.json` + helpers SaveISM/ValidateISM). `universal_stablecoin_supply` definition switched from `linear` to `trend_only` so it scores by 4w ROC. CoinGecko client now retries on 429 with 2/4/8s exponential backoff (fixes the warm-up rate-limit issue from v1.8.2). New endpoints `GET /api/crypto-indicators/ism` + `POST /api/crypto-indicators/ism` (multipart or JSON body). Frontend gains a 📊 Update ISM data… button that file-picks a JSON, posts it, fires a refresh. `pal_ism` stays "awaiting data" until user uploads the 12-month seed. |
| 1.9.0 | 2026-05-21 | **Earnings-revision trigger** — three-tier notification when a thesis becomes revision-needed (earnings published since the thesis was locked). (1) **Visual nav badge:** Theses tab shows `⚠ N` count when `theses_index.earnings_urgency='revision_needed'` rows > 0. (2) **Telegram push:** `handleBotAlerts` now scans `theses_index` and emits `thesis_revision_needed` alerts for tickers held in `stock_holdings`. The existing bot cron (`@FinsFTAlerts_bot` 13:00/17:00/21:00 UTC) picks these up automatically — no bot deploy needed. Same `notification_log` dedup, ack via existing `/done`. (3) **Revision prompt generator:** new endpoint `GET /api/theses/{id}/revision-prompt` returns a markdown LLM prompt populated with current thesis content + pillar scores + explicit revision instructions for Gemini/Claude/etc. Per-row 📋 button on revision-needed theses opens a modal with copy-to-clipboard. **Detection cadence improved:** new `RefreshEarningsOnly` in `internal/refresh/earnings_only.go` runs hourly — slim Yahoo calendar refresh that only updates `earnings_date`/`ex_dividend_date`. Total practical lag from earnings release → notification: ~3-4 hours (was 12-18 hours under daily 04:00 UTC cadence). |
| 1.9.1 | 2026-05-21 | **Pal business-cycle indicator: ISM → CFNAI (auto from FRED).** Migration 0026 drops `pal_ism` (which required monthly manual JSON upload via the 📊 button) and inserts `pal_cfnai` (Chicago Fed National Activity Index — same monthly cadence, same business-cycle pulse signal Pal uses ISM for, free + automated via FRED `CFNAI` series). New scoring thresholds reflect CFNAI's signed scale: ≥0.2 + rising → +1.0, ≥0 + rising → +0.5, ≥0 + flat → 0, ≤0 + falling → -0.5, ≤-0.7 + falling → -1.0 (recession likely). Refresher swaps the ISM JSON read for a FRED series fetch (third CFNAI call alongside DGS2 + DTWEXBGS). Frontend: removes the 📊 Update ISM data… button. Tooltip updated to explain CFNAI as the Pal-equivalent. All 11 Phase 1 indicators now fully automated — zero manual maintenance for indicator data. The `internal/cryptoindicators/providers/ism.go` reader + `POST /api/crypto-indicators/ism` endpoint are kept (cheap) in case a future hybrid (CFNAI default + manual ISM override, à la Cowen Risk) is wanted. |
| 1.9.2 | 2026-05-21 | **News tabs unblocked + crypto news provider swap.** NewsAPI key installed on jarvis (Market News tab now populating). CryptoPanic's free Developer tier was discontinued 2026-04-01 (cheapest paid is $50/wk → not worth it for personal use); swapped `internal/news/crypto.go` from CryptoPanic to **CryptoCompare News API** — free, no API key needed, ~100k req/month limit, same publisher mix (CoinDesk, Cointelegraph, The Block, Decrypt, etc.). Article shape unchanged; sentiment now derived from CryptoCompare's upvotes/downvotes (most articles 0/0 → neutral, which the UI already handles). Source slug `cryptopanic` → `cryptocompare` in the handler. Legacy `CRYPTOPANIC_API_KEY` env var still parsed by config (unused, no breakage). |
| 1.9.3 | 2026-05-21 | **Crypto news: RSS aggregation (third time's the charm).** CryptoCompare turned out to also require an API key now — "free" tier needs registration. After two rugged providers in one day, pivoted to direct RSS reads from publisher feeds: **CoinDesk, Cointelegraph, The Block, Decrypt**. No third-party aggregator, no API keys, no rate limits, no free-tier-discontinued risk. The publishers publish RSS and actively want it crawled. Generic RSS 2.0 parser via `encoding/xml` stdlib; multi-format `pubDate` tolerance; partial-failure handling (one bad feed doesn't kill the aggregate); deduped by URL; capped at 50 newest articles. Real-browser User-Agent + redirect-follow handles CoinDesk's Arc CDN. Sentiment is "neutral" for every article (RSS doesn't carry sentiment data — UI already handles neutral). Handler source slug `cryptocompare` → `rss`. Adding/removing a publisher is a one-line append/delete in the `cryptoFeeds` slice. |
| 1.10.0 | 2026-05-21 | **Spec 9k Phase A — Political & Insider Signal tab MVP (insider Form 4).** New top-level **Signals** tab between Crypto Indicators and Performance. Migration 0027 creates all five Spec 9k tables (`signal_events`, `legislators`, `committee_assignments`, `committee_sector_map`, `eo_sector_keywords`) — only `signal_events` is used in this phase; the rest are scaffolded for v1.11.0. New `internal/signals/` package: `universe.go` (InUniverse filter with 30-min cache covering stock_holdings + watchlist + sector_universe ETFs), `tiers.go` (insider tier computation + cluster-buy ALARM promotion: ≥3 distinct actors / 14d / same ticker / BUY), `insider.go` (SEC EDGAR Form 4 ingest — ATOM feed → per-filing index → form4.xml; ~7.7 req/sec self-limit per SEC fair-access policy; polite User-Agent), `query.go` (List/Counts/Acknowledge). Spec 9k §3 locked thresholds: BUY $25K / SELL $100K / CEO-CFO BUY any size / CEO-CFO SELL $250K. Endpoints `GET /api/signals?tier=&type=&range=&include_acked=`, `POST /api/signals/{id}/ack`, `POST /api/signals/refresh-insiders` (debug manual trigger), `GET /api/signals/universe` (debug snapshot). Daily 23:00 UTC cron (after sector_rotation 22:00) wired in `cmd/ft/main.go`. Frontend: tier/range/type chip filters, sortable signal table, per-row ack button, nav-bar `🔴 N` alarm badge. Telegram routing + Congress/EO ingest follow in v1.11.0 (9k.B) and v1.12.0 (9k.C). |
| 1.10.1–5 | 2026-05-21 | **Spec 9k Phase A — same-day stabilisation pass.** Five small post-deploy fixes rolled into the live tab: (1) **gzip fix** — `Accept-Encoding` header was set manually, which disabled `net/http`'s auto-decompression; XML parser silently choked on raw gzip bytes. (2) **ATOM `<category>` filter** — EDGAR's `type=4` query leaks other forms (497J, 4/A); now hard-filtered. (3) **Charset reader** — SEC ATOM declares `encoding="ISO-8859-1"`; switched to `xml.NewDecoder + charset.NewReaderLabel` (golang.org/x/net/html/charset). (4) **Per-filing fetch via index.json** — SEC's actual XML name is per-filing (`wk-form4_NNN.xml`), not `form4.xml`/`primary_doc.xml`. One JSON discovery + one XML fetch replaced the old three-attempt waterfall, halving total ingest time. (5) **Async manual refresh** — the synchronous endpoint blew the reverse-proxy's 30s gateway timeout (ingest takes ~40s); now returns 202 and runs in a goroutine, frontend polls `/api/signals` every 10s for new rows. (6) **Universe + company-name enrichment** — added universe-class filter chips (💼 Owned / 👁 Watchlist / 🏷 Sector ETF / 🌐 Unowned), per-row badge in the ticker cell, company name (from Form 4 `<issuerName>`, migration 0028), and sector resolved at read-time from holdings/watchlist/sector_universe. |
| 1.11.0 | 2026-05-21 | **Spec 9k Phase B — Congress + Executive Order ingest + full UI.** Migration 0029 seeds the committee→sector jurisdiction map (13 committees: House/Senate Armed Services, House Energy & Commerce, Senate Commerce, Senate/House Banking, Senate Energy & Natural Resources, House Natural Resources, House Agriculture, Senate HELP, House Veterans, House/Senate Intelligence) plus a starter set of ~60 EO sector keywords across Defense, Semiconductors, AI/GPUs, Energy, Nuclear, Power/Grid, Pharma, Critical Minerals, Comms, Data Center, Software/Cloud. Three new ingesters: `internal/signals/congress.go` (House Stock Watcher + Senate Stock Watcher JSON aggregators, 90-day lookback, fuzzy legislator name resolve, committee-jurisdiction ALARM tier compute), `internal/signals/eo.go` (Federal Register API, keyword pass + company-name pass; ALARM when EO names a held company, FLAG when EO matches a sector with active holdings/watchlist), `internal/signals/legislators.go` (quarterly refresh from `unitedstates/congress-legislators` for `legislators` + `committee_assignments` — current 535 active reps/sens). Tier compute extended (`tiers.go` adds `CongressTier(CongressEvent, Thresholds)`). Three new endpoints: `POST /api/signals/refresh-congress`, `/refresh-eo`, `/refresh-committees` (all 202-async). Crons: Congress 23:10 UTC, EO 23:20 UTC, quarterly legislator refresh 02:00 UTC on first day of Jan/Apr/Jul/Oct. Frontend: four refresh buttons (Insiders/Congress/EO/Committees), EO title rendered in the Ticker column when ticker is null (sector-only matches), `notes` field returned in API. Telegram routing + Phase C polish ship in v1.12.0. |
| 1.11.1 | 2026-05-21 | **Signals tab usability + Settings maintenance checklist.** Three polish changes after v1.11.0 first-use feedback. (1) **Signals page restructured into three sections** (Insider / Congress / EO) instead of one flat table; each section has its own sortable column headers (Date / Tier / Ticker / Actor / Amount) — click to toggle asc/desc, click a different column to switch sort axis. Section row-counts in the header. (2) **⟳ Refresh all signals** button — single click fires all four endpoints (insiders + congress + EO + committees) in parallel, status text shows what launched + a live "still running" counter; individual triggers moved into an expandable `<details>` block. (3) **Settings → Maintenance checklist** — new section with 9 recurring tasks (master spec sync, quarterly committee verify, EO keyword false-positive review, committee-jurisdiction audit, API key rotation, DB backup, holdings audit, thesis-revision scan, sector universe review), each with cadence + detail + a localStorage-backed "last done" timestamp + ✓ done / ↻ redo buttons. List is editable by editing `MAINTENANCE_ITEMS` in app.js — no backend storage. |
| 1.22.0 | 2026-05-29 | **Spec 9l Phase 1 baseline — Crypto Thesis Framework.** Migration 0031 creates 8 new tables (`crypto_adapters`, `crypto_adapter_versions`, `crypto_theses`, `crypto_thesis_history`, `crypto_thesis_dependencies`, `cascade_events`, `crypto_allocation_current`, `crypto_allocation_history`). New `internal/cryptotheses/` package (`types.go` + `service.go`): enum suite (AdapterType, ScorecardType, Band, HoldingHorizon, BTCBeta, Q5Mechanism, Status, DependencyType, CascadeStrength); `PillarScores` handling both 9-Q /18 alts and 6-pillar /12 BTC; `Q5Detail` structured triple (mechanism + annual_usd + fdv_usd → computed accrual_pct); `ComputeBand` / `ComputePillarPassGate` / `ApplyPassGate` / `RecommendedActionFromDelta`; goldmark + bluemonday Render; full Adapter CRUD with version archive on save-as-new-version. New `internal/server/crypto_adapters_handlers.go` exposes 7 endpoints under `/api/crypto/adapters/*` parallel to Spec 9g pattern. Frontend: `Theses` tab renamed `Stock Theses`; new sibling `Crypto Theses` tab next to Crypto Indicators; two-pane Repository view with status icons + /12 vs /18 scorecard chip + kill-criteria collapsible + edit modal with Save-as-new-version toggle + version-history modal. `SeedIfEmpty` seeds 8 adapters as `v0-placeholder` drafts. `crypto_allocation_current` seeded 60/10/5/5/20 default split. v0.2 spec patch drafted (closes open items #4 scoring-log columns, #6 drift threshold ±15%, #7 allocation panel two-table model, #8 Spec 9m placeholder; promotes cascade rules into spec body). |
| 1.22.1 | 2026-05-29 (PM) | **Spec 9l v0.3 patch — adapter MDs + calibration triad land.** Migration 0032 adds BEFORE INSERT/UPDATE OF trigger pair on `crypto_theses` enforcing Speculative-adapter horizon restriction (blocks Never-Sell/Cycle/Multi-year, allows Trade/Medium/TBD); RAISE(ABORT, ...) with explicit message that the API layer can match for 422 translation. 3 adapter MDs loaded into `crypto_adapters` and locked at `v1` via one-off seeder (`cmd/ft-seed-9l/main.go`): BTC (6-pillar Monetary /12), L1 (9-Q template for all /18 adapters, ETH is reference case), Speculative (strictest adapter with mandatory 5% per-thesis / 15% aggregate position caps + Trade/Medium-only horizon). 3 calibration-anchor theses loaded into `crypto_theses` via second one-off seeder (`cmd/ft-seed-9l-theses/main.go`): BTC v1 (6/12 Hold, Never-Sell), ETH v1 (13/18 Strong, ceiling anchor, Multi-year, Q5=fee_burn $700M/$252B=0.28%), LUNC v1 (3/18 Exit, floor anchor, Trade, multi-VETO via Do Kwon 15yr sentence + 99.99% drawdown, Q5=buyback $17.5M/$526M=3.33%, pillar_pass_gate_failed=1). 5 remaining adapter MDs (l2, defi, infra, depin, rwa) still draft v0-placeholder — pending Claude.ai authoring per kickoff Step 4 stop. ETH v1 internal Q2 contradiction flagged in v0.3 patch §B for product-owner reconciliation (header total 13 requires Q2=2; detail prose rounds to Q2=1). D12 thesis CRUD endpoints, D17 liquidity pre-filter, D18 secondary sanity-check, D19 cascade-flag, D25 Scoring Engine, D26 cross-thesis table, D27 detail page, D29 Allocation UI, D30 9e sell-window verdict all remain Phase 2 — sequencing waits for L2 adapter MD landing. |
| 1.22.4 | 2026-05-30 (PM late) | **Spec 9l v0.5 patch §L resolutions locked.** Claude.ai response (`Response_to_Claude_Code_Post_Batch_2.md`) closes all 5 decision questions raised in the post-batch-2 handover. v0.5 patch §B + §F status changed from OPEN → RESOLVED. **§L.1 Batch 3a authoring approved** (AAVE→RNDR→BUIDL; ~1 work session each on Claude.ai side; NOT in user's portfolio but acceptable for framework validation). **§L.2 0033 timing: AFTER Batch 3a** (Phase 1 `other` + free-text fallback handles ingestion; real RYR/RABR data shapes inform proper column design; re-tag cost trivial). **§L.3 v0.5 rounding rule: DEFER to quarterly methodology audit** — v0.4 *"round down if any sub-criterion = 0; else round to nearest"* stands canonical. Honest reasoning: Claude.ai's inconsistent prose-math application surfaced the "gap"; not evidence of framework limitation. Quarterly audit (next 2026-08) decides on v0.5 refinement after 5-10 more theses score under strict v0.4. **§L.4 LINK v1: honour 14/18; amend MD prose in Batch 3d** — current DB state correct (Q6=1, Q9=1 stored). 5 reasons recorded: calibration intent of ~14/18 ceiling for Infrastructure, Q6/Q9 sub-criterion 5 = 1 are real drags, ETH ceiling at 13 means LINK at 16 would break framework. **§L.5 D25 Scoring Engine sequenced ahead of Spec 9m** by wide margin. **§L.6 other ambiguities resolved inline**: ETH v1 Q2 prose amendment = Batch 3d hygiene (no urgency, DB matches v0.4 §C); BTC `btc_beta` enum `reference` value rolls into 0033 (5-way enum, re-tag BTC v1 from `low` → `reference` in same migration); LUNC mechanism re-tag to `burn_and_mint` at 0033; `_crypto_scoring_log.md` stays at `/var/lib/ft/research/crypto/`; Spec 9m defers (trade-history pipe gating). **§L.7 canonical sequencing locked**: 3a author → 3a ingest → 0033 → 3d prose amendments → D25 → Phase 2 crons → Spec 9m future. **§L.8 Claude Code standby posture**: no proactive build work until Batch 3a delivers; flag only if ft.service degrades or cascade engine misfires. |
| 1.22.3 | 2026-05-30 (PM) | **Spec 9l batch 2 ingest + v0.5 status-capture patch.** Four new locked v1 seed theses ingested via `cmd/ft-seed-9l-batch2/`: LINK Infrastructure oracle (14/18 Strong Conviction lower edge — first non-L1 Strong; first `protocol_host` cascade ETH→LINK moderate; Q5 mechanism stored as `other` with canonical label `required_for_service` in note per v0.4 §B Phase 1 rule), SOL L1 hp-l1 (13/18 raw → Accumulate via PPG cap Q8=0; Alpenglow Q3 2026 catalyst; Drift exploit MONITOR), POL L1 est-alt-l1 primary + L2 zk secondary advisory (8/18 raw → Trim via PPG cap; **first hybrid coin under §13**; primary/secondary delta = 4 points at flag threshold), AVAX L1 est-alt-l1 (11/18 raw → Hold via PPG cap; anti-narrative-state test case for Avalanche9000 renewal). 5 new cascade rows: 1 protocol_host (ETH→LINK moderate) + 4 btc_beta_implicit (BTC→[LINK/SOL/POL/AVAX] weak) — total 9 cascade rows now. Cascade firing acceptance tests extended (`cmd/ft-cascade-firing/`): ETH 13/18→11/18 single band drop fires HIGH on ARB only (platform_parent strong); LINK does NOT fire (protocol_host moderate needs 2+ bands); ETH 13/18→9/18 2-band drop fires both ARB (HIGH) + LINK (MEDIUM) with LINK status auto-updating to needs-review. Non-destructive test cleanup confirmed. API extension: `ThesisDetail.SecondaryAdapterSlug` + `SecondaryAdapterType` exposed via GET endpoint for hybrid-coin frontend display ("+ l2 advisory" tag on POL detail modal). `_crypto_scoring_log.md` initialised per v0.2 §"Item 4" 16-column schema with 8 entries + VETO event log + methodology amendment log + cascade graph state + quarterly audit checklist (staged at `/var/lib/ft/research/crypto/`). v0.5 patch (status-capture only — no doctrine locked) flags two product-owner items: (a) LINK v1 header-vs-prose math inconsistency (header 14/18 vs prose-strict-v0.4-rounding 16/18; same pattern as ETH v1 Q2; Claude Code applied same trust-the-locked-header resolution with Q6=1 + Q9=1 in DB), (b) v0.5 rounding rule refinement candidate surfaced by batch 2 prose ("round down if any sub-criterion = 0 OR if 2+ sub-criteria ≤ 1 in 5+-pillar; else nearest"); proposed rule preserves all v0.4 outcomes for calibration triad + ARB AND captures batch 2 author's manual overrides as default; recommendation: lock now to unblock Batch 3a (DeFi/DePIN/RWA seed theses). Calibration gap remaining: DeFi/DePIN/RWA adapters locked but zero theses authored. Phase 2 migration 0033 scope consolidated: 7 new Q5 enum values + DePIN RYR fields + RWA RABR + Custody Verification Tier + optional BTC `reference` btc_beta value. Cascade types deployed: 3 of 5 (`platform_parent`, `protocol_host`, `btc_beta_implicit`); `oracle_dependency` waits for downstream DeFi/Speculative thesis referencing Infrastructure; `narrative_correlated` is manual-creation only by design. Phase 1 acceptance gate unchanged at 8/10 (D25 Scoring Engine modal + cron framework deferred to Phase 2). Commit `a476640`. Handover authored at `/home/finbarr/Downloads/Handover_for_Claude_ai_Spec_9l_Post_Batch_2.md` for next Claude.ai session pick-up. |
| 1.22.2 | 2026-05-30 | **Spec 9l v0.4 patch — Phase 1 ship gate.** All 5 remaining adapter MDs locked (l2/defi/infra/depin/rwa at v1) via extended `cmd/ft-seed-9l` seeder. ARB v1 calibration thesis seeded (8/18 raw → Trim band via PPG cap; parent ETH v1 via `platform_parent` strong cascade — first formal cascade row in framework; pillar_pass_gate_failed=1 from Q2=0+Q5=0+Q8=0). All 4 calibration anchors now in place. **Step 5 cascade engine** (`internal/cryptotheses/cascade.go`): async single-pass walker, BFS depth-5 recursion detection at dep-creation, evaluateTrigger() per (dep_type × strength × band drop), cascade_events audit log; 6/6 acceptance criteria verified via `cmd/ft-cascade-test` against prod DB (ETH 13→11 demotion fires HIGH on ARB, reverse cascade does not fire, circular dep rejected, btc_beta_implicit notification-only on BTC→Trim). **Step 7 cross-thesis table + per-thesis detail** (`internal/cryptotheses/thesis_service.go` + `internal/server/crypto_theses_handlers.go`): 16-column ListAll with parent-symbol cascade join; Get with pillar scores + Q5 detail + cascade deps both directions + history; new endpoints `GET /api/crypto/theses`, `GET /api/crypto/theses/{symbol}/{version}`, `GET /api/crypto/theses/{symbol}/{version}/events`. **Step 8 Allocation Panel UI**: inline panel at top of Crypto Theses tab with 5 number inputs + live sum-to-100 validation + computed alt allocation suggestion table; `GET/PUT /api/crypto/allocation` with two-table (current+history) backing. **Step 8 last — 9e sell-window verdict badge** (D30): frontend-only addition to Crypto Indicators tab; fires when 9e composite is Distribute/Strong-Distribute AND any locked Crypto Thesis is in Trim/Exit band; amber/red gradient banner above hero with clickable thesis links opening detail modal. Frontend: `renderCryptoTheses` restructured into Allocation Panel → Cross-thesis table → Adapter Repository three-section layout; new `openCryptoThesisDetail` modal with pillar table + Q5 mechanism + Q9 note + cascade deps + VETO banner + collapsible MD body. **Doctrine locked in v0.4 patch**: (1) Two-table allocation schema supersedes v0.2 §"Item 7" `user_settings` extension; (2) Q5 mechanism enum extension plan for Phase 2 (7 new values including direct_asset_claim/required_for_service/dsr_surplus/burn_and_mint/buyback_stake/real_yield_staking/governance_with_fee_switch); (3) New rounding rule "round down if any sub-criterion = 0; else round to nearest" preserves PPG protection + ETH Q2=2 reality; (4) ETH v1 Q2 reconciliation at 13/18 with new rule (no DB update needed — seeded values already match). **Phase 1 ship-gate status: 8/10 kickoff acceptance criteria green**; Scoring Engine modal UI (D25) + cron framework (D13-D16) deferred to Phase 2 with clean architecture stubs in place. |

---

*Personal use only. Not investment advice.*
