# ops/scrapers — Farside ETF flows and Capitol Trades (Playwright cron jobs)

| File | What |
|---|---|
| `farside-fetch.js` | 00:25 UTC. Headless Chromium → `/var/lib/ft/data/farside/etf-flow.json` (BTC spot-ETF flows). FT's Crypto Indicators chart reads it; the card falls back to a live HTTP fetch when the file is older than 36 h. |
| `capitol-trades-fetch.js` | 23:05 UTC. → `/var/lib/ft/data/capitol-trades/trades.json` for the Congress signal ingest. **Blocked by the site since ~2026-08-14** (HTTP 429, "Vercel Security Checkpoint"). |
| `ft-farside-fetch.cron`, `ft-capitol-trades.cron` | the `/etc/cron.d` entries (user `curran`, logs in `/home/curran/logs/`) |
| `ft-scrapers.logrotate` | weekly, 8 kept, compressed |
| `install.sh` | installs all of it, including Playwright and Chromium |

`sudo /opt/ft/src/ops/scrapers/install.sh` — see the header for what it fixes and why.
After a rebuild, **run each job once by hand and read its log**: a failing cron redirect leaves no log at all.
