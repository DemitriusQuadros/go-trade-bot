# Exposing the bot through a Cloudflare Tunnel

This is how to let friends use the web UI on your homelab without opening a port on your router.
Two layers protect the app:

1. **Cloudflare Access** is the outer gate. Friends prove their email with a one-time code or Google.
2. **App accounts** are the inner gate. Each friend has a username, password and capabilities (auth-01).

The app is safe even when reached without Cloudflare: every `/api` route needs a session, and auth is on by
default. Cloudflare Access adds defense in depth. With `CF_ACCESS.*` set, cmd/api also refuses any request
that did not pass through Access, so someone who reaches the homelab port directly gets
`403 {"error":"access_required"}`.

Everyone shares one bot and one Binance account. Keep `MODE: dryrun` unless you have a reason not to.

## 1. Expose only cmd/api

Publish **only** cmd/api's port, `8080` (the web UI and `/api`). Never expose:

| Port / service | Why |
|---|---|
| `9191` (cmd/worker Asynqmon + `/metrics`) | Asynqmon has **no auth**, and it can delete or retry tasks. |
| `9194` (cmd/agent Asynqmon + `/metrics`) | Same as 9191. |
| `9193` (worker settings bridge) | Internal, loopback only. |
| `8090` (cmd/mcp HTTP) | Service-token only; keep it local. |
| Postgres, Redis, Prometheus, Grafana, Alertmanager | Internal. |

Point the tunnel at `localhost:8080` only. Don't publish 8080 on your router or firewall either.

## 2. cloudflared ingress

`~/.cloudflared/config.yml` (or the tunnel's dashboard config):

```yaml
tunnel: <tunnel-id>
credentials-file: /home/<you>/.cloudflared/<tunnel-id>.json

ingress:
  # Prometheus metrics are for your LAN scraper only, never the internet.
  - hostname: bot.example.com
    path: ^/metrics
    service: http_status:404
  - hostname: bot.example.com
    service: http://localhost:8080
  - service: http_status:404
```

The `^/metrics` rule matters. cloudflared connects to cmd/api from `127.0.0.1`, and cmd/api lets loopback and
private addresses read `/metrics` without an Access JWT (so Prometheus can scrape it). cmd/api also refuses
the exemption when a request carries `Cf-Connecting-IP`, but block `/metrics` at the tunnel anyway.

## 3. Cloudflare Access application

In Cloudflare Zero Trust, go to **Access → Applications → Add an application → Self-hosted**:

- **Application domain**: `bot.example.com`, with no path. The application covers the whole hostname: the SPA,
  `/api`, SSE streams and report iframes.
- **Identity providers**: One-time PIN and/or Google.
- **Policy**: Allow, with *Include → Emails* listing your friends' addresses and your own.
- After saving, open the application's **Overview** and copy its **Application Audience (AUD) Tag**.
- Your team domain is shown under **Settings → Custom Pages**, as `<team>.cloudflareaccess.com`.

## 4. config.yml

```yaml
MODE: dryrun
API_BASE_URL: https://bot.example.com

AUTH:
  BOOTSTRAP_ADMIN_USERNAME: yourname      # first run only
  BOOTSTRAP_ADMIN_PASSWORD: "a-long-password"  # >= 10 chars; delete after first start
  TRUST_PROXY: true                       # the cookie gets Secure via X-Forwarded-Proto: https

CF_ACCESS:
  TEAM_DOMAIN: myteam.cloudflareaccess.com
  AUD: 0123456789abcdef...                # the AUD tag from step 3

# Optional, for scripts / MCP-over-HTTP (Authorization: Bearer <token>, acts as admin):
API_TOKEN: ""

# Must stay false (or absent) on anything reachable from a network:
ALLOW_INSECURE_NO_AUTH: false
```

Start cmd/api once and look for these log lines:

```
auth: bootstrap admin "yourname" created - remove AUTH.BOOTSTRAP_ADMIN_PASSWORD from config
cf-access: enabled for team myteam.cloudflareaccess.com
```

Then **remove `AUTH.BOOTSTRAP_ADMIN_PASSWORD`** from `config.yml`. It is only read while the users table is
empty. If you see `cf-access: disabled`, one of the two `CF_ACCESS` values is missing.

## 5. Add friends

Sign in at `https://bot.example.com` as the admin, open **Users**, and create one account per friend with
the `friend` role. A friend can:

- view everything;
- create, edit and delete backtest drafts (mode `backtest`, not productive);
- run backtests and optimizations;
- chat with agents up to their daily budget (default $1.00 per UTC day);
- approve or reject proposals.

Everything else is admin-only: settings and credentials, the kill switch, strategy mode/status changes,
agent personas, webhook targets, the deploy gate, closing signals, candle schedules and users. `viewer` is
read-only.

Share the password over a private channel. Friends can change it under **Profile**. A password change or
reset signs out that user's other sessions, and disabling a user signs them out everywhere.

## Checklist

- [ ] Only `localhost:8080` is behind the tunnel; `^/metrics` returns 404 at the tunnel.
- [ ] Ports 9191/9193/9194/8090, Postgres and Redis are not reachable from outside.
- [ ] The Access application covers the whole hostname; its policy lists only your friends.
- [ ] `CF_ACCESS.TEAM_DOMAIN` and `CF_ACCESS.AUD` are set (the log says `cf-access: enabled`).
- [ ] `AUTH.TRUST_PROXY: true`; `AUTH.BOOTSTRAP_ADMIN_PASSWORD` has been removed after the first start.
- [ ] `ALLOW_INSECURE_NO_AUTH` is false or absent.
- [ ] `MODE: dryrun` and `API_BASE_URL` is `https://<host>`.
- [ ] A request straight to `http://<homelab-ip>:8080/api/strategy` (bypassing the tunnel) gets 403
      `access_required`.
