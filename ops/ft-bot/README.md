# ft-bot — Telegram daemon (source of truth)

`index.js` is the code that runs on Jarvis as the `ft-bot` systemd unit (`/opt/ft-bot/index.js`, env from
`/etc/ft-bot/env`). It was not in the repo before FT-039 (SC-85); this copy is now the source.

What it does: at the configured poll hours it reads `GET /api/bot/alerts?only_unnotified=1`, sends one Telegram
message, then acknowledges (`POST /api/bot/alerts/ack`) the alerts that message showed; it also answers `/alerts`,
`/movers`, `/summary`.

FT-039: every alert kind is rendered (RED, AMBER, PROXIMITY, TRADE EVENTS, AI NEXUS, OTHER), a message is never sent
empty, and only alerts that were shown are acknowledged.

Test: `node --test ops/ft-bot/render.test.js` (also run by the Go suite when node is installed).

Deploy (as `curran` on Jarvis): back up `/opt/ft-bot/index.js`, copy this `index.js` over it, `sudo systemctl restart ft-bot`,
check `journalctl -u ft-bot -n 20`.
