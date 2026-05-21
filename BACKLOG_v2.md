# FT — Open Backlog at end of Spec-1-through-6 cycle (2026-05-16)

Companion to `STATE_v2.md`. Lists every item **deferred** during Specs 1–6
plus the bot work. Use this to avoid re-spec'ing things that are already
tracked, and to spot existing hooks that future specs can wire to.

Each item carries:
- **Source** — which spec/work item parked it
- **Effort** — rough size based on the work already in
- **Hook status** — whether infra exists, in which case the new spec is
  pure consumer of an existing API/field

---

## 1 — Polish (small, easy, would-be-nice)

| Item | Source | Effort | Hook status |
|---|---|---|---|
| **Click-to-rescore from holdings table** — the Score column on `/stocks` and `/crypto` tabs is read-only; user must re-add to watchlist to rescore an existing holding | Spec 4 D7 — spec says "Click column value → opens 8-Question Screen pre-loaded with prior values", current build trimmed for time | ~30 min frontend only | `openScoreScreen()` already exists; just needs a variant that takes `targetKind='holding'` + holding row data instead of watchlist entry. POST /api/scores already accepts `targetKind: "holding"`. |
| **Watchlist column sorting** — currently fixed at `added_at DESC` | Spec 4 D3 — "Sortable by any numeric column" listed as a deliverable, not built | ~45 min | Pure frontend; data is already in `state.watchlist`. |
| **Stale-score banner click target** — banner on Summary is informational; spec said "Click → filtered holdings view showing only those" | Spec 4 D8 | ~30 min | Banner element already renders; needs a filter UI on the Stocks/Crypto tabs. |
| **Exchange override UI** — backend supports `stock_holdings.exchange_override` per-row, but no field in the Edit modal | Spec 5 D2 — backend done, UI not added | ~20 min | Add to `stockFields` in `app.js` as a select with the 7 exchange codes. PUT handler already accepts the field (or would if added to `stockMutationReq`). |
| **Snooze command in FT bot** — proactive ping ends with "Reply /snooze to mute for 24h (not yet implemented)" placeholder | FT bot v0.1 | ~45 min | Needs a new `notification_log` row type (e.g. `notified_via='snooze'`) or a `snooze_until` column. Until built, just remove the placeholder line. |
| **Tile sized by P&L** — third heatmap mode | Spec 6 — listed under "Out of scope, possible later" | ~30 min | `RenderOptions.Source []MarketTile` already accepts arbitrary sizing weights. Compute `pnlUsd = currentValueUsd - investedUsd` per holding; feed in. Add `pnl` to the preference allowlist. |

---

## 2 — Explicitly parked by source spec (intentional v1 limits)

These were in scope but called out as "v1 omission" / "out of scope" in the
spec doc itself. They become future specs when business demand justifies.

| Item | Source | Notes |
|---|---|---|
| App-level rate-limiting on /api/auth/login | Spec 1 D7 — Cloudflare WAF free tier only offers 10s windows; app-layer would let us do 15-min cooldowns | Argon2id verify time (~50-80ms) is the practical floor anyway. Pair with Spec 7 (Diagnostics) when it lands. |
| Half-day market sessions (NYSE day-after-Thanksgiving etc.) | Spec 5 — `half_days` JSON field reserved; treated as full closures for v1 | Add support in `marketdata/status.go` when needed. |
| Holiday data auto-refresh | Spec 5 — calendar files hand-curated 2026+2027. Manual annual refresh in December | Spec 7 (Diagnostics, later) will surface "no holidays defined for current year" warnings. |
| Pre-market / after-hours data feeds | Spec 5 — out of scope | Would require a different data source; Finnhub free doesn't include extended hours. |
| Crypto in heatmap | Spec 6 — stocks-only v1 | CoinGecko has the data via `coins/markets`; would need a sector-equivalent (L1, DeFi, etc.) since crypto doesn't fit GICS. |
| Custom heatmap color scales | Spec 6 — out of scope | Current gradient is hard-coded in `internal/heatmap/color.go`. |
| Heatmap animations between modes | Spec 6 — out of scope | Would need a different render approach (CSS transform on tiles, not just `innerHTML` swap). |

---

## 3 — Future-spec hooks ALREADY wired

Existing code has been written expecting these specs to land. **Future
specs should consume these hooks, not invent new ones.**

| Hook | Defined in | Used by future spec |
|---|---|---|
| `alert.ProximityMargin = 0.05` const | `internal/alert/alert.go` | Spec 9b regime overlay: when status is `SHIFTING` or `DEFENSIVE`, this tightens to 0.03. Just needs a function that reads the regime state and returns the right margin. |
| `framework_scores.tags_json` free-form per-framework tags | Spec 4 D1 | Spec 9b bottleneck donut (stocks) reads `tags.bottleneck_position`; phase donut (crypto) reads `tags.cycle_phase`. Data is already there for any holding/watchlist entry that's been scored. |
| `user_preferences` key/value store | Spec 6 D1 | Any future "remember my filter / shortcut / default tab" need. Per-key validation lives in `validPreferenceValue()` — extend the switch as new keys land. |
| `holdings_audit.changes_json` structured diff | Spec 3 D13 | Any spec wanting "what changed in the last N days" can query this directly; no new infra needed. |
| `service_tokens` + bearer middleware | Spec 1 / Phase A | Any new bot or external client uses `ft token create` and the existing `requireUserOrToken` middleware. |
| `/api/bot/*` namespace | FT bot | Any future bot or curl client: same auth model, same dedup. Adding a new bot endpoint = add a route + `requireUserOrToken`. |
| `notification_log` UNIQUE on `(user, kind, holding_id, alert_kind, alert_day)` | Spec 1 / Phase C | Any new alert type just needs to call `POST /api/bot/alerts/ack` to dedupe. No schema change required. |
| `news_cache` scoped by `feargreed_stocks` / `feargreed` / `market` / `crypto` | Spec 2 D6 | Any new news/sentiment source slots in as a new scope; cache TTLs already 6h stale-fallback. |

---

## 4 — Known limits / "won't fix unless infra changes"

| Limit | Why | Mitigation if hit |
|---|---|---|
| **Yahoo crumb dance fragility** | Yahoo changes their cookie+crumb flow every few months; symptom is 401 on `/v7/quote` after crumb refresh | Update `internal/market/yahoo.go` to match the new flow. ~30 min when it breaks. |
| **CoinGecko free-tier rate limit** | Burst >~10 calls/min trips a sticky 429 for several minutes | Daily cron is now sequential w/ 2.5s gap. If still hit, fall back to next-day cron or accept partial fill. Affects sparkline freshness only. |
| **TwelveData dead branch** | Free tier became US-only mid-2024 | Kept in provider chain in case policy changes. Effectively unreachable today; not a blocker. |
| **OpenClaw 2026.3 skill model change** | Folder-drop skills (HCT, original FT plan) no longer load — must be npm packages | Both FT bot and HCT skill currently run as standalone Node services. Re-integration is a future spec ("OpenClaw plugin migration"). |
| **Finnhub free: US-only, no RSI/MA** | Finnhub free tier | RSI/MA filled by Yahoo chart enrichment for US tickers (already wired). Beta auto-resolved via Yahoo. Paid Finnhub or paid TwelveData would simplify. |

---

## 5 — Already-addressed-by-bot items (do NOT re-spec)

When drafting alert/notification-related specs, **assume these work**:

- Proactive Telegram pings 3×/day weekdays for RED/AMBER (cron in ft-bot)
- 24h dedup via `notification_log` UNIQUE (the bot ACKs after each send)
- Slash-command query interface: `/alerts /summary /movers /help`
- Single-user enforcement (replies only to `TELEGRAM_CHAT_ID`)
- MarkdownV2-formatted output with emoji + bold tickers + sub-bullets per trigger
- Separate bot identity from `Jarvis_curran_bot` (isolation from OpenClaw)
- Bearer-token auth model (`ft_st_…` plaintext shown once, sha256 hash stored)

What's **still open** in the bot space:

- Unified-bot UX (one Telegram contact, currently two: Jarvis_curran_bot + FinsFTAlerts_bot)
- Free-text NL parsing (currently only structured slash commands)
- Snooze / per-holding mute (placeholder text exists, no mechanism)
- Per-holding alert subscriptions (all-or-nothing right now)
- OpenClaw plugin migration (both HCT and FT live outside the Jarvis ecosystem currently)

---

## 6 — Initial-deploy artifacts (not bugs, will self-heal)

| Symptom | Cause | Resolution |
|---|---|---|
| 6 cryptos (POL, AVAX, XVM, SUI, HBAR, ADA) show "—" sparkline | CoinGecko penalty box during initial backfill | Next 04:00 UTC daily cron fills them in (no live-refresh competition at that hour). |
| `last_partial_failure_at` meta key may be present | Recorded when refresh has any per-ticker error | Cosmetic; informational. Surface in future Spec 7 (Diagnostics). |
| Some 2026 holiday calendars are "best-effort" | Asian markets (TSE, HKEX) have lunar/observed-date wrinkles | Verify against the relevant exchange site before relying on edge dates. |
