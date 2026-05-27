# Dedicated VM Deployment (Hetzner)

Production deployment of mcp-agent-builder-go on a single Hetzner VM. Hybrid setup: agent + workspace run **bare-metal** under systemd, frontend + caddy run in **Docker**.

## Access

| What | Value |
|---|---|
| **Public URL** | https://agents.excellencetechnologies.in |
| **Server IP** | `138.201.227.99` |
| **OS** | Ubuntu 24.04 LTS |
| **SSH** | `ssh -i ~/.ssh/hetzner_mcp root@138.201.227.99` |
| **SSH key** | `~/.ssh/hetzner_mcp` (ed25519) |

If the domain changes, update [`quick-deploy.sh`](quick-deploy.sh) (`PUBLIC_URL`) and the server `Caddyfile` (see [Changing the domain](#changing-the-domain)).

## Architecture

```
                        ┌──────────────────────────────┐
   Internet ──► :443 ──►│ caddy (Docker)               │
                        │   /api*, /ws* ──┐            │
                        │   /*  ──► frontend (Docker)  │
                        └──────────────────┼───────────┘
                                           │ host.docker.internal:8000
                                           ▼
                        ┌──────────────────────────────┐
                        │ mcp-agent (systemd, bare)    │  :8000
                        │  ↓ localhost:8080            │
                        │ mcp-workspace (systemd, bare)│  :8080 (127.0.0.1 only)
                        └──────────────────────────────┘
```

- **Bare-metal (systemd)**: `mcp-agent.service`, `mcp-workspace.service` — run Go binaries directly so they can spawn `tmux`, `claude`, `gemini`, `chromium`, etc. without container nesting.
- **Docker**: `caddy` (TLS termination + reverse proxy), `frontend` (nginx serving prebuilt vite bundle).
- Caddy reaches the bare-metal agent via `host.docker.internal:host-gateway`.

## Key paths on server

| Path | Purpose |
|---|---|
| `/opt/mcp-agent/` | Deploy root (compose, Caddyfile, .env, run scripts) |
| `/opt/mcp-agent/src/` | Source (`agent_go/`, `workspace/`, `mcpagent/`, `multi-llm-provider-go/`, `frontend-dist/`) |
| `/opt/mcp-agent/.env` | Provider keys + runtime config (loaded by `run-agent.sh`) |
| `/data/docs/` | Workspace documents (shared: workspace-api + agent) |
| `/data/logs/agent_server.log` | Agent log |
| `/data/workspace-db/`, `/data/agent-db/` | Persistent state |
| `/root/go/bin/mcpbridge` | Stdio↔HTTP MCP bridge binary (built on server) |

## Deploy

From your local machine, repo root:

```bash
cd deploy/dedicated-vm
./quick-deploy.sh all          # everything
./quick-deploy.sh agent        # just agent_go + mcpagent + multi-llm + workspace (Go)
./quick-deploy.sh frontend     # just frontend (builds locally, ships dist/)
./quick-deploy.sh workspace    # just restart workspace
```

What `quick-deploy.sh all` does:
1. Rsyncs `agent_go/`, `workspace/`, `mcpagent/`, `multi-llm-provider-go/` to `/opt/mcp-agent/src/`
2. Builds the frontend locally (`npm run build`) and rsyncs `dist/` → `src/frontend-dist/`
3. Bumps bare-metal CLI tools: `agent-browser`, `@anthropic-ai/claude-code`, `@google/gemini-cli` (all `@latest`)
4. Fixes `go.mod` replace directives in-place (paths differ on server)
5. Rebuilds `mcpbridge` (`go install ./cmd/mcpbridge/`)
6. `systemctl restart mcp-agent` and waits for `/api/health` → 200
7. Rebuilds frontend Docker image and `docker compose up -d --force-recreate frontend`
8. `systemctl restart mcp-workspace`
9. Probes the public URL (will only succeed if your local DNS can resolve the domain)

## Status & logs

```bash
# Quick health
ssh -i ~/.ssh/hetzner_mcp root@138.201.227.99 \
  'systemctl is-active mcp-agent mcp-workspace; \
   docker compose -f /opt/mcp-agent/docker-compose.yml ps; \
   curl -s -o /dev/null -w "API: %{http_code}\n" http://127.0.0.1:8000/api/health'

# Agent logs (file)
ssh -i ~/.ssh/hetzner_mcp root@138.201.227.99 'tail -200 /data/logs/agent_server.log'

# Systemd journals
ssh -i ~/.ssh/hetzner_mcp root@138.201.227.99 'journalctl -u mcp-agent    -n 200 --no-pager'
ssh -i ~/.ssh/hetzner_mcp root@138.201.227.99 'journalctl -u mcp-workspace -n 200 --no-pager'

# Caddy access log (for HTTPS issues)
ssh -i ~/.ssh/hetzner_mcp root@138.201.227.99 'docker logs mcp-agent-caddy-1 --tail 100'
```

## Start / stop

```bash
# Stop everything
ssh ... 'systemctl stop mcp-agent mcp-workspace; \
         cd /opt/mcp-agent && docker compose stop'

# Start everything
ssh ... 'systemctl start mcp-agent mcp-workspace; \
         cd /opt/mcp-agent && docker compose up -d'
```

## Changing the domain

1. Update `PUBLIC_URL` in [`quick-deploy.sh`](quick-deploy.sh).
2. Edit `/opt/mcp-agent/Caddyfile` on the server: replace the site block label + admin email with the new domain. Template is [`Caddyfile.https`](Caddyfile.https).
3. Restart caddy: `cd /opt/mcp-agent && docker compose up -d --force-recreate caddy`
4. Caddy will request a fresh Let's Encrypt cert automatically (DNS must already point to `138.201.227.99`).

## First-time server setup

`setup-server.sh` provisions a fresh Ubuntu 24.04 VM — installs Go, Node, Docker, tmux, chromium, the CLIs (`claude`, `gemini`, `agent-browser`), creates `/data` dirs, drops the systemd units, and configures UFW + fail2ban. Run once; subsequent updates go through `quick-deploy.sh`.

## Known gotchas

### 1. `go.mod` replace directives
Local `go.mod` uses `replace ../../mcpagent` (path differs on server). `quick-deploy.sh` rewrites them in-place after rsync. If you build manually on the server, run the `go mod edit -replace` block from [`quick-deploy.sh`](quick-deploy.sh#L158-L169) first.

### 2. Gemini CLI auth
Default `~/.gemini/settings.json` is OAuth — fails non-interactively. `ensureGeminiAPIKeyAuth()` in `llm_config_handlers.go` rewrites it to `"selectedType": "gemini-api-key"` on first use.

### 3. Claude Code + `ANTHROPIC_API_KEY` conflict
If `ANTHROPIC_API_KEY` is set, Claude Code uses it instead of its OAuth credentials. Validation in `llm_config_handlers.go` strips it from env before `claude --print`. Also: don't pass `--dangerously-skip-permissions` when running as root.

### 4. UFW blocks Docker→host
Containers can't reach the bare-metal agent without an explicit rule:
```bash
ufw allow from 172.16.0.0/12 to any port 8000
```

### 5. SSH hardening
Never set `PermitRootLogin prohibit-password` before SSH keys are confirmed working. Use `harden-ssh.sh` *after* you've logged in with the key. Ubuntu 24.04 uses `ssh.service`, not `sshd.service`.

### 6. `mcpbridge` must be built on the server
Local binary is mac-arm; server is linux-amd64. `quick-deploy.sh` runs `go install ./cmd/mcpbridge/` on every agent deploy.

### 7. `TOOL_EXECUTION_TIMEOUT`
Don't set this in `run-agent.sh` — a 15m cap was previously killing legitimate sub-agent runs. Sub-agents now use the default (no hard cap).

### 8. systemd env
The unit must set `HOME=/root`, `GOPATH=/root/go`, `GOMODCACHE=/root/go/pkg/mod`, and a PATH that includes `/usr/local/go/bin:/root/go/bin` (so the agent can find `go`, `mcpbridge`, `claude`, `gemini`).

## Files in this directory

| File | Purpose |
|---|---|
| `quick-deploy.sh` | The one you'll use 99% of the time |
| `deploy.sh` | Original full-build deploy (slower; uses Docker for everything) |
| `setup-server.sh` | First-time VM provisioning |
| `harden-ssh.sh` | Disable password SSH after keys are confirmed |
| `run-agent.sh` | What `mcp-agent.service` executes |
| `run-workspace.sh` | What `mcp-workspace.service` executes |
| `mcp-workspace.service` | systemd unit (workspace; agent unit lives on server only) |
| `docker-compose.yml` | Caddy + frontend |
| `Caddyfile` | IP-only (HTTP) variant — kept for reference |
| `Caddyfile.https` | Template with `{DOMAIN}` / `{EMAIL}` placeholders |
| `mcp_config.json` | MCP server definitions copied to server |
| `sync-workflow.sh` | One-off helper to push a single workflow folder |
| `supabase-keepalive.sh` | Pings Supabase to prevent project pause |
| `confida-access.md` | Runbook for the Confida logging pipeline |
