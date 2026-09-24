# Deploying `cmd/mcp` behind nginx + Cloudflare Tunnel

For the specific setup: an existing notebook running a docker-compose stack
with nginx + `cloudflared`, one subdomain per tool. This wires the MCP
server (`cmd/mcp`) into that same pattern.

## 1. Bring up the `mcp` service

`docker-compose.yml`'s new `mcp` service builds and runs `cmd/mcp
--transport=http` on port 8090, on the compose network only (not published
to the host - reachable via the `mcp` container name from other services on
the same network, e.g. nginx).

```bash
docker compose build mcp
docker compose up -d mcp
```

It depends on `redis`+`postgres` (already in this compose file) and reads
the same `config.yml` `worker`/`api` use. No `AGENT.*` block is required for
`cmd/mcp` to start - only `cmd/api`'s chat panel needs a configured model
provider.

## 2. Point nginx at it

Copy `deploy/nginx/mcp.conf.example` into wherever your other tools'
nginx server blocks live, and set `server_name` to your real hostname
(e.g. `mcp.yourdomain.com`). Requires nginx to be on the same Docker network
as the `mcp` container - either add nginx to this compose project, or put
both stacks on one shared external Docker network
(`docker network create shared-tools` and reference it as `external: true`
in both compose files).

## 3. Add the tunnel hostname

Add the entry from `deploy/cloudflared/mcp-ingress-snippet.yml` to your
existing `cloudflared` `config.yml`, above any catch-all 404 rule, then
reload/restart `cloudflared` and add the matching DNS record (`cloudflared
tunnel route dns <tunnel-name> mcp.yourdomain.com`, or via the Cloudflare
dashboard if you manage DNS there instead).

## 4. Gate it with Cloudflare Access (recommended, done in the dashboard)

This is account/dashboard configuration, not a file in this repo - I can't
do this step for you. In the Cloudflare Zero Trust dashboard:

1. **Access → Applications → Add an application → Self-hosted.**
2. Domain: `mcp.yourdomain.com`.
3. Add a policy - simplest is "Allow" with an Include rule of "Emails ->
   your own email" (or a one-time-PIN rule), so only you can complete the
   Access login challenge in front of the tunnel.
4. Save. Cloudflare now requires that login before a request ever reaches
   `cloudflared` → nginx → `mcp` - a second layer in front of the
   `API_TOKEN` bearer check `cmd/mcp` itself still enforces (Access gates
   *reaching* the service at all; `API_TOKEN` still gates the MCP protocol
   calls themselves once through).

An MCP client that supports it can authenticate through Access using a
`CF_Authorization` cookie/service token; check whichever client you're
connecting with (Claude Desktop, etc.) for how it handles an Access-gated
endpoint - this may need a Cloudflare Access **service token** (Access →
Service Auth) instead of interactive login if the client can't complete a
browser-based challenge itself.

## Notes / caveats carried over from the architecture doc

- `mem_limit: 512m` on the `mcp` service is the same placeholder the
  `worker` service already carries, for the same reason: no per-script
  memory ceiling exists at the Go/Lua level yet, and `cmd/mcp` runs the
  identical Lua script runner via `save_strategy_script`/`run_backtest`.
  Revisit both once a real steady-state RSS baseline exists.
- No `/metrics` endpoint is actually served by `cmd/mcp` yet (it registers a
  Prometheus collector but never binds an HTTP handler for it) - nothing to
  scrape here yet if you're also running Prometheus on this notebook.
