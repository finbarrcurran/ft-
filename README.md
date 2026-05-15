# FT — Finance Tracker

Personal finance dashboard for tracking stocks/ETFs and crypto holdings, with
rules-based alerts (RED/AMBER/GREEN), live market data refresh, and a
bot-facing API for OpenClaw integration.

Ported from the Next.js phase-7 prototype, mirroring HCT's
Go + SQLite + embedded-frontend pattern.

## Quick start (dev)

```sh
go mod tidy
make build
make dev
# → listens on :8081, insecure cookies so http://localhost works
curl -s localhost:8081/healthz
```

## Production

See `deploy/RUNBOOK.md`. The short version:

```sh
sudo FT_DOMAIN=ft.curranhouse.dev ./deploy/install.sh
```

Provisions the `ft` system user, installs to `/opt/ft/bin/ft`, writes
`/etc/systemd/system/ft.service`, starts the service, and waits for `/healthz`.

## Status

Phase 8, in-progress port. Skeleton stands up an empty server with healthz
+ embedded boot page. Auth scaffolding (Argon2id, sessions, service tokens)
is wired but not yet exposed via handlers. Holdings, alerts, news, heatmap,
xlsx import/export are TODO.
