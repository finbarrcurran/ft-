# ops/scrapers — Farside ETF flows and Capitol Trades (Playwright cron jobs)

| File | What |
|---|---|
| `farside-fetch.js` | 00:25 UTC. Headless Chromium → `/var/lib/ft/data/farside/etf-flow.json` (BTC spot-ETF flows). FT's Crypto Indicators chart reads it; the card falls back to a live HTTP fetch when the file is older than 36 h. |
| `capitol-trades-fetch.js` | 23:05 UTC. → `/var/lib/ft/data/capitol-trades/trades.json` for the Congress signal ingest. **Retired 2026-10-11** (blocked by the site since ~2026-08-14; replaced by Bargo inside FT, SC-47). Script kept for reference; its cron is no longer installed. |
| `ft-farside-fetch.cron` | the `/etc/cron.d` entry (user `curran`, log in `/home/curran/logs/`). The Capitol Trades cron file was removed from the repo and from Jarvis on 2026-10-19 (the file was still installed on Jarvis and ran, and failed, every night until then; `install.sh` deletes it on a rebuild) |
| `ft-scrapers.logrotate` | weekly, 8 kept, compressed |
| `install.sh` | installs all of it, including Playwright and Chromium |

`sudo /opt/ft/src/ops/scrapers/install.sh` — see the header for what it fixes and why.
After a rebuild, **run each job once by hand and read its log**: a failing cron redirect leaves no log at all.
