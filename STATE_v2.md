# FT — State at end of Spec-1-through-6 cycle (2026-05-16)

One-page reference for drafting Part-2 specs. Everything below is shipped,
deployed at `https://ft.curranhouse.dev`, and was end-to-end smoke-tested.

## Schema (SQLite, WAL, FK on)

Migration files in `internal/store/migrations/`:

| # | File | Adds |
|---|---|---|
| 0001 | `init.sql` | `users`, `sessions`, `service_tokens`, `stock_holdings`, `crypto_holdings`, `meta`, `news_cache`, `notification_log` |
| 0002 | `daily_change.sql` | `daily_change_pct` columns on stock/crypto |
| 0003 | `crypto_is_core.sql` | `crypto_holdings.is_core` (BTC/ETH seeded 1) |
| 0004 | `spec3_holdings_extensions.sql` | stocks `+note +deleted_at +beta +earnings_date +ex_dividend_date`; crypto `+note +deleted_at +vol_tier`; new `holdings_audit`, `price_history` |
| 0005 | `watchlist_frameworks.sql` | `watchlist`, `framework_scores` (append-only history, target_kind ∈ holding\|watchlist) |
| 0006 | `exchange_override.sql` | `stock_holdings.exchange_override` |
| 0007 | `user_preferences.sql` | `user_preferences (key, value, updated_at)`, seeded `heatmap_mode='market_cap'` |

Soft-delete via `deleted_at INTEGER` (unix ts) everywhere; list queries
filter `WHERE deleted_at IS NULL`. `holdings_audit` captures every CRUD
mutation as `{field: {old, new}}` JSON diff.

## Endpoints (all cookie-auth unless marked **token**)

```
GET  /healthz
GET  /api/auth/{state,me}           POST /api/auth/{setup,login,logout}

GET  /api/holdings/{stocks,crypto}
POST /api/holdings/{stocks,crypto}                          (create)
PUT  /api/holdings/{stocks,crypto}/{id}                     (update)
DELETE /api/holdings/{stocks,crypto}/{id}                   (soft-delete + audit)
POST /api/holdings/{stocks,crypto}/{id}/restore             (un-soft-delete)
GET  /api/holdings/{stocks,crypto}/deleted                  (Settings panel)
GET  /api/audit                                              (Spec 3 D13)

GET  /api/summary                       KPIs + 3 donuts + USD/EUR aware
GET  /api/marketstatus                  US-only legacy shape (Spec 2)
GET  /api/marketstatus/all              7 exchanges + summary block (Spec 5)
GET  /api/heatmap.svg ?mode=&sector=    treemap SVG (Spec 6 modes)

GET  /api/news/{market,crypto}          NewsAPI / CryptoPanic
GET  /api/feargreed{,/stocks}           CNN F&G (crypto + stocks)

GET  /api/watchlist  POST/PUT/DELETE    Spec 4 CRUD
POST /api/watchlist/{id}/promote        watchlist → holding (atomic tx)
GET  /api/frameworks{,/{id}}            Jordi/Cowen JSON defs
GET  /api/scores ?targetKind=&targetId= POST /api/scores  (append-only)

GET  /api/preferences{,/{key}}  PUT     Spec 6 key/value store

POST /api/refresh             token-or-cookie  trigger refresh
GET  /api/refresh-status      token-or-cookie  "Xs ago" footer
GET  /api/bot/alerts          token-or-cookie  RED/AMBER list ± dedup
POST /api/bot/alerts/ack      token-or-cookie  notification_log upsert
GET  /api/bot/holdings/{summary,movers}  token-or-cookie  KPIs / movers
POST /api/bot/refresh         token-or-cookie  alias for /api/refresh
GET  /api/bot/refresh-status  token-or-cookie  alias

POST /api/import/{preview,apply}    GET /api/export.xlsx    xlsx round-trip
```

**Token auth**: bearer `ft_st_<64hex>`, mint via `ft token create
--user-id N --name LABEL`. Hashes stored, plaintext shown once.

## Background jobs (inside the FT process)

| Job | Schedule | What |
|---|---|---|
| Live refresh | every `FT_REFRESH_INTERVAL` (default 15m) | FX → stocks (Finnhub→TwelveData→Yahoo) → crypto (CoinGecko) → heatmap-only S&P sample → meta `last_refreshed_at`. Skipped when interval=0. |
| Daily job | next 04:00 UTC then every 24h | Sparkline backfill into `price_history` (Yahoo daily closes for stocks, CoinGecko market_chart for crypto — sequential w/ 2.5s gap to avoid rate-limits). Calendar dates (Yahoo `calendarEvents`). Beta auto-resolve (Yahoo `summaryDetail`). Prunes `price_history` >35 days. CLI mirror: `ft daily --user-id N`. |
| Session GC | every 1h | `PurgeExpiredSessions` |

External, not inside FT:

| Job | Schedule | What |
|---|---|---|
| Nightly DB backup | 03:15 UTC daily (`/etc/cron.d/ft-backup`) | `sqlite3 .backup` to `/var/backups/ft/`, `PRAGMA integrity_check`, prune >14 days |

## FT Telegram bot (deployed and running)

This is the standalone Node daemon that replaced the previously-deferred
"OpenClaw skill" backlog item. **OpenClaw 2026.3 changed its skill model
to npm-package authoring**, breaking the folder-drop pattern; the
existing HCT skill is also dormant for the same reason. Standalone gives
us isolation from OpenClaw breakage and uses a separate Telegram bot
(no shared context with `Jarvis_curran_bot`).

**Identity**: `@FinsFTAlerts_bot` (Telegram bot id 8955129725).
Single-user — replies only to `TELEGRAM_CHAT_ID`, ignores strangers.

**Runtime**: Node 22 systemd service `ft-bot.service` running as system
user `ft-bot:ft-bot`. ~12 MB resident, capped at 64 MB. Hardening:
`NoNewPrivileges`, `ProtectSystem=strict`, `ProtectHome`, `PrivateTmp`.

**Secrets**: `/etc/ft-bot/env` (root:root, mode 600). Variables:
`TELEGRAM_BOT_TOKEN`, `TELEGRAM_CHAT_ID`, `FT_BASE_URL`, `FT_TOKEN`
(an `ft_st_…` bearer), optional `ALERT_HOURS`, `ALERT_WEEKENDS`.

**Triggers**:

| Path | Trigger | Endpoint(s) used | Behaviour |
|---|---|---|---|
| Proactive cron | 13:00 / 17:00 / 21:00 UTC weekdays (configurable) | `GET /api/bot/alerts?only_unnotified=1` then `POST /api/bot/alerts/ack` per item | Push only un-ACKed RED/AMBER. ACK writes to `notification_log` → no re-fire within UTC day (UNIQUE constraint). Silent if nothing to send. |
| Reactive long-poll | Continuous Telegram `getUpdates` (50s long-poll, offset persisted to `/var/lib/ft-bot/offset.json`) | `/alerts`, `/summary`, `/movers`, `/help`/`/start` | `/alerts` → `GET /api/bot/alerts?only_unnotified=0` (does NOT ACK, user is asking). `/summary` → `GET /api/bot/holdings/summary`. `/movers` → `GET /api/bot/holdings/movers?limit=5`. |

All responses are MarkdownV2 — bold tickers, emoji bullets, sub-bullets
per trigger reason. Bad token → 401 surfaces as "Couldn't reach FT".

**Deploy path** (idempotent — re-run to update):

```
# laptop
scp staging/ft-bot/{index.js,package.json,ft-bot.service,install.sh} curran@jarvis:/tmp/ft-bot/
ssh curran@jarvis 'sudo bash /tmp/ft-bot/install.sh'
```

`install.sh` creates the `ft-bot` user (if missing), installs to
`/opt/ft-bot/`, copies `ft-bot.service` to `/etc/systemd/system/`,
`systemctl daemon-reload && enable && restart`, then prints last 20
journal lines. Refuses if `/etc/ft-bot/env` is absent.

**Implications for Part-2 specs that touch alerts**:

- Proactive Telegram pinging of RED/AMBER is **already covered**.
- 24h dedup via `notification_log` is **already covered**.
- Three NL-ish queries (`/alerts /summary /movers`) cover the basic
  bot use case via slash commands instead of free-text LLM parsing.
- Open items if Part 2 wants more: unifying with `Jarvis_curran_bot`
  (single-bot UX), free-text NL parsing via LLM, snooze/mute commands
  (placeholder text exists in code), per-holding alert subscriptions.

## Frontend

Vanilla HTML + JS embedded in the Go binary via `embed.FS`. No bundler,
no framework, no `node_modules` for the web side. Cache-busted on every
deploy via the first 8 hex chars of `sha256(app.js+app.css)`. Tabs:
**Summary** (default landing) · **Stocks & ETFs** · **Crypto** ·
**Watchlist** · **Heatmap** · **News** · **Crypto News** · **Settings**.
