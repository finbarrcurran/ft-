# SC-41b — OAuth for the claude.ai MCP connector (runbook)

FT is its own single-user OAuth 2.1 server (`internal/oauth`). claude.ai discovers it from the 401 on `/mcp`.

## Connect (Fin)
1. claude.ai → Settings → Connectors → Add custom connector → Name `FT`, URL `https://ft.curranhouse.dev/mcp` (nothing else).
2. claude.ai opens FT's approval page. Check it says "Claude wants read-only access to FT" and the redirect host is `claude.ai`. Enter your FT email + password, click **Approve**.
3. In a chat, ask Claude to use the FT connector (for example "list my FT alerts").

If the sign-in fails, send ai the log: `sudo journalctl -u ft --since "-10 min" | grep -i "oauth\|mcp request"`.

## Check / revoke
```
ft oauth status                      # clients and active sign-ins
ft oauth revoke-all                  # kill switch: every OAuth token
ft oauth revoke-client --id ftc_...  # one client and its tokens
ft token list ; ft token revoke --id N
```
Settings → Diagnostics shows the OAuth counts. Tokens: access 1 h, refresh 30 days (rotating). A re-used refresh token revokes its family and sends a Telegram alert.

## Manually registered client (only if the dialog asks for a client ID/secret)
`ft oauth create-client --name claude.ai --secret` prints a client id and a secret (shown once).

## Cloudflare (manual — FT has no API token)
Step-by-step with screens is in the status report `FT_Status_WorkOrder_E_SC41b_OAuth`. Summary:

1. Custom rule, action **Block**: `(http.host eq "ft.curranhouse.dev" and (http.request.uri.path eq "/token" or http.request.uri.path eq "/register" or starts_with(http.request.uri.path, "/mcp") or starts_with(http.request.uri.path, "/.well-known/")) and not ip.src in {160.79.104.0/21})`. `/authorize` stays open (Fin's browser). 160.79.104.0/21 is Anthropic's published egress range, shared by all Claude users: it cuts noise, it is not authentication. It also blocks Claude Code on Fin's own machine from using this connector (that is outside the range).
2. Rate-limiting rule on `/api/auth/login`. Free plans offer limited choices (typically one rule, a 10-second window and a short block): take the strictest the dashboard allows. FT's own limiter (5 failed logins per IP per 15 min) does the real work.
