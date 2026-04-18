# Kubernetes Deployment

Deploy AgentForge to Kubernetes with Gemini and OpenRouter providers.

## Services

| Service | Port | Health Endpoint |
|---------|------|-----------------|
| Agent API | 8000 | `/api/health` |
| Frontend | 80 | `/health` |
| Workspace API | 8080 | `/health` |

## URL

- **Production**: https://analytics-agent.citymall.live

## Configuration

### Secrets (deploy/k8s/.env)

```bash
# Authentication (REQUIRED when MULTI_USER_MODE=true)
# Generate with: openssl rand -base64 32
# Server will refuse to start without this in multi-user mode.
# Changing this value invalidates all existing JWTs (users must re-login).
AUTH_SECRET=<random-secret-for-jwt-signing>

# LLM Provider Keys
GEMINI_API_KEY=<your-gemini-api-key>
OPENROUTER_API_KEY=<your-openrouter-api-key>
VERTEX_PROJECT_ID=<gcp-project-id>
VERTEX_LOCATION_ID=<gcp-location>

# Database
DATABASE_URL=<postgres-connection-string>

# Observability (Langfuse)
LANGFUSE_PUBLIC_KEY=<langfuse-public-key>
LANGFUSE_SECRET_KEY=<langfuse-secret-key>
LANGFUSE_HOST=<langfuse-host>

# GitHub Sync (optional)
GITHUB_TOKEN=<github-token>
GITHUB_REPO=<owner/repo>
```

### ConfigMap (shared/configmap.yaml)

- `PROVIDER`: LLM provider (vertex, openrouter)
- `MODEL`: Model ID (gemini-3.0-flash)
- `MAX_TURNS`: Max conversation turns
- `TRACING_PROVIDER`: Observability provider
- **LLM lock (restricted mode):**
  - `SUPPORTED_LLM_PROVIDERS`: Comma-separated list of providers to show in UI (e.g. `vertex` for Gemini-only). Omit or leave empty for all six.
  - `LLM_CONFIG_LOCKED`: Set to `true` to lock LLM config: server uses env only, frontend shows "LLM settings are locked by admin", no editable modal. API keys are never sent to the client.
  - `DEFAULT_PUBLISHED_LLMS`: (Optional) JSON array of default published LLM entries. When locked, one entry is auto-built from primary config if unset.
  - `DEFAULT_PUBLISHED_LLMS_PATH`: (Optional) Path to a JSON file with the same array (e.g. mounted from a ConfigMap).
- **MCP lock (restricted mode):**
  - `MCP_CONFIG_LOCKED`: Set to `true` to lock MCP server configuration. Users can only use pre-configured MCP servers from `mcp_config.json`; add/edit/remove operations return 403 Forbidden. Frontend shows read-only view with "Configuration is locked by administrator" message.

### MCP servers (agent/mcp_config.json)

Place your MCP server config at **`deploy/k8s/agent/mcp_config.json`**. When you run the deploy script (with or without `--build`), this file is applied to the cluster as ConfigMap `mcpagent-agent-config` and the agent is restarted so it loads the new config. Format: `{"mcpServers":{"server-name":{...},...}}`. If the file is missing, the existing ConfigMap is left unchanged.

## Deploy

```bash
# Deploy all services
./deploy/k8s/scripts/deploy-k8s.sh

# Build and deploy all
./deploy/k8s/scripts/deploy-k8s.sh --build

# Deploy specific service
./deploy/k8s/scripts/deploy-k8s.sh agent
./deploy/k8s/scripts/deploy-k8s.sh frontend
./deploy/k8s/scripts/deploy-k8s.sh workspace-api

# Build and deploy specific service
./deploy/k8s/scripts/deploy-k8s.sh --build agent

# Sync a local workflow to the prod PVC as part of the deploy.
# Source: workspace-docs/Workflow/<name>/ on your machine.
# Behavior: clean-replace — deletes the remote folder, then uploads a zip
# of the local folder via the workspace-api /api/workspace/import endpoint.
# runs/ is excluded by default (historical execution artifacts); pass
# --sync-workflow-include-runs to ship them too.
./deploy/k8s/scripts/deploy-k8s.sh --sync-workflow citymall-infra
./deploy/k8s/scripts/deploy-k8s.sh --sync-workflow citymall-infra --sync-workflow codeanalysis
./deploy/k8s/scripts/deploy-k8s.sh --build --sync-workflow citymall-infra
./deploy/k8s/scripts/deploy-k8s.sh --sync-workflow citymall-infra --sync-workflow-include-runs
```

## Verify

```bash
# Check pods
kubectl get pods -n prod-mcpagent

# Check health
curl https://analytics-agent.citymall.live/api/health

# View logs
kubectl logs -f deployment/mcpagent-agent-cs -n prod-mcpagent
kubectl logs -f deployment/mcpagent-frontend-cs -n prod-mcpagent
kubectl logs -f deployment/mcpagent-workspace-api-cs -n prod-mcpagent

# Port forward for local testing
kubectl port-forward svc/mcpagent-agent-cs 8000:80 -n prod-mcpagent
```

## Authentication

The application supports multiple authentication providers. See [docs/authentication.md](docs/authentication.md) for detailed setup instructions.

### Quick Reference

| Provider | Config | Description |
|----------|--------|-------------|
| `cognito` | `COGNITO_*` env vars | AWS Cognito OAuth |
| `supabase` | `SUPABASE_*` env vars | Supabase Auth |
| `simple` | `AUTH_USERS` env var | Username/password |

### AUTH_SECRET

**Required** when `MULTI_USER_MODE=true`. This is the JWT signing key used to sign/verify tokens after the OAuth provider (Cognito/Supabase) authenticates users.

- In **single-user mode**: Not needed. Auth middleware is bypassed entirely.
- In **multi-user mode**: Server will **refuse to start** (`log.Fatal`) if not set.
- Generate: `openssl rand -base64 32`
- Changing it invalidates all existing JWTs — users must re-login via OAuth.
- Add to `deploy/k8s/.env` or directly to the k8s secret:
  ```bash
  kubectl patch secret prod-mcpagent-secret -n prod-mcpagent \
    --type merge -p '{"stringData":{"AUTH_SECRET":"<your-secret>"}}'
  ```

### Current Setup (AWS Cognito + Google Workspace SSO)

```yaml
# shared/configmap.yaml
MULTI_USER_MODE: "true"
AUTH_PROVIDERS: "cognito"
COGNITO_USER_POOL_ID: "ap-south-1_YhXWOPgST"
COGNITO_CLIENT_ID: "2gahrd23uppdrlil01naotmvme"
COGNITO_DOMAIN: "mcpagent-auth.auth.ap-south-1.amazoncognito.com"
AWS_REGION: "ap-south-1"
```

**Login Options:**
1. **Email/Password** - For manually created users
2. **Google SSO** - For @citymall.live Google Workspace users (domain-restricted)

### Test Users

| User | Password | Status |
|------|----------|--------|
| manish.prakash@citymall.live | Citymall@123 | CONFIRMED |
| nverdhan@citymall.live | Citymall@123 | CONFIRMED |

### Create Users

```bash
# Admin create user
aws cognito-idp admin-create-user \
  --user-pool-id ap-south-1_YhXWOPgST \
  --username user@example.com \
  --user-attributes Name=email,Value=user@example.com Name=email_verified,Value=true \
  --temporary-password TempPass123! \
  --message-action SUPPRESS \
  --region ap-south-1

# Set permanent password (so user doesn't need to change on first login)
aws cognito-idp admin-set-user-password \
  --user-pool-id ap-south-1_YhXWOPgST \
  --username user@example.com \
  --password "YourPassword123!" \
  --permanent \
  --region ap-south-1

# List users
aws cognito-idp list-users \
  --user-pool-id ap-south-1_YhXWOPgST \
  --region ap-south-1
```

## MCP Server Discovery

On startup, the agent runs **background tool discovery** — connecting to each MCP server in `mcp_config.json` to discover available tools.

**Key behaviors:**
- Each server gets its own **5-minute timeout** (one slow server won't block others)
- Servers that fail with **auth errors** (401/403/OAuth) are marked as permanently failed and **not retried** on the 24-hour refresh cycle
- Saving MCP config via the UI **clears the failed list** so servers can be retried with new credentials
- Discovery connections are **closed after extracting tool metadata** to avoid duplicate subprocesses and OOM
- Tool metadata is cached to disk — on restart, cached servers load instantly without reconnecting

**Common issues:**
- Smithery-hosted servers (`server.smithery.ai/*`) require OAuth tokens — they'll show as failed until configured
- URL-based servers with `"command": ""` need a `"url"` field and proper auth headers

## Build Architecture

### Go Module Dependencies

The agent Docker image pulls `mcpagent` and `multi-llm-provider-go` from GitHub via `go.mod` tags (e.g. `github.com/manishiitg/mcpagent v1.2.9`). The `workspace` module is local (lives in this repo) and resolved via `go.work`.

To update these dependencies, tag a new release on their GitHub repos and run `go get` in `agent_go/`:
```bash
cd agent_go
go get github.com/manishiitg/mcpagent@v1.2.10
go get github.com/manishiitg/multi-llm-provider-go@v0.3.7
go mod tidy
```

### MCP Connection Sharing

MCP server connections are shared globally across all chat sessions to prevent OOM from duplicate subprocesses:

- **Stateless servers** (all except `playwright`/`agent-browser`) use a fixed `"global"` session ID — one subprocess per server regardless of how many tabs/agents are active
- **Stateful servers** (`playwright`, `agent-browser`) keep per-session connections for browser state isolation
- The `SessionConnectionRegistry` in `mcpagent/mcpclient/session_registry.go` manages this with per-key mutexes to prevent race conditions
- If any code path creates an agent without setting `SessionID`, it defaults to `"global"` automatically (see `agent.go`)

**Resource expectations:** ~12 MCP servers = ~1.5-2GB RAM steady state. Memory should stay flat regardless of tab/session count.

## Troubleshooting

### Pod OOMKilled
The agent spawns MCP server subprocesses (Python/Node). Each uses 70-350MB RAM. With shared connections, ~12 servers use ~1.5-2GB total.
- Check: `kubectl top pod -n prod-mcpagent`
- Check process count: `kubectl exec <pod> -n prod-mcpagent -- ps aux --sort=-rss | head -20`
- Check for duplicates: each MCP server should have exactly 1 process. If you see duplicates, check logs for `session=global` vs per-session connections
- Current limits: 3Gi request / 5Gi limit (in `agent/deployment.yaml`)

### AUTH_SECRET warning spam
`[AUTH] WARNING: Using default AUTH_SECRET` on every request means `AUTH_SECRET` is not set in the k8s secret. See [AUTH_SECRET](#auth_secret) section above.

### pprof (memory/CPU profiling)
pprof is available at `/debug/pprof/` on the agent pod:
```bash
# Heap profile
kubectl exec <pod> -n prod-mcpagent -- curl -s 'http://localhost:8000/debug/pprof/heap?debug=1'

# Goroutine dump
kubectl exec <pod> -n prod-mcpagent -- curl -s 'http://localhost:8000/debug/pprof/goroutine?debug=1'

# CPU profile (30s)
kubectl exec <pod> -n prod-mcpagent -- curl -s 'http://localhost:8000/debug/pprof/profile?seconds=30' > cpu.prof
```
