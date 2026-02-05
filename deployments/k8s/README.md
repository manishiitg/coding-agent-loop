# Kubernetes Deployment

Deploy MCP Agent Builder to Kubernetes with Gemini and OpenRouter providers.

## Services

| Service | Port | Health Endpoint |
|---------|------|-----------------|
| Agent API | 8000 | `/api/health` |
| Frontend | 80 | `/health` |
| Workspace API | 8080 | `/health` |

## URL

- **Production**: https://analytics-agent.citymall.live

## Configuration

### Secrets (deployments/k8s/.env)

```bash
GEMINI_API_KEY=<your-gemini-api-key>
OPENROUTER_API_KEY=<your-openrouter-api-key>
VERTEX_PROJECT_ID=<gcp-project-id>
VERTEX_LOCATION_ID=<gcp-location>
DATABASE_URL=<postgres-connection-string>
LANGFUSE_PUBLIC_KEY=<langfuse-public-key>
LANGFUSE_SECRET_KEY=<langfuse-secret-key>
LANGFUSE_HOST=<langfuse-host>
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

Place your MCP server config at **`deployments/k8s/agent/mcp_config.json`**. When you run the deploy script (with or without `--build`), this file is applied to the cluster as ConfigMap `mcpagent-agent-config` and the agent is restarted so it loads the new config. Format: `{"mcpServers":{"server-name":{...},...}}`. If the file is missing, the existing ConfigMap is left unchanged.

## Deploy

```bash
# Deploy all services
./deployments/scripts/deploy-k8s.sh

# Build and deploy all
./deployments/scripts/deploy-k8s.sh --build

# Deploy specific service
./deployments/scripts/deploy-k8s.sh agent
./deployments/scripts/deploy-k8s.sh frontend
./deployments/scripts/deploy-k8s.sh workspace-api

# Build and deploy specific service
./deployments/scripts/deploy-k8s.sh --build agent
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

Basic auth is enabled via nginx ingress. Credentials are stored in `basic-auth` secret.

To update credentials:
```bash
htpasswd -c auth admin
kubectl create secret generic basic-auth --from-file=auth -n prod-mcpagent
```
