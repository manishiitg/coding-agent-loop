# Azure Container Apps Deployment

Deploy the MCP Agent Builder stack: **agent**, **workspace-api**, and **frontend** as Container Apps. Frontend runs nginx serving the built app (no Storage or CDN).

## Prerequisites

- Azure CLI logged in: `az login`
- Resource group **`code-analysis-phase-1`** exists in your subscription (create with `az group create -n code-analysis-phase-1 -l eastus` if needed)
- Docker (for building images)
- Terraform >= 1.0

## Permissions required

Deploy runs in the **existing resource group** only. The identity running Terraform needs the following.

### Recommended: built-in role on the resource group

- **Contributor** on the resource group  
  Covers create/read/update/delete for all resources and allows assigning the AcrPull role to the managed identity on the ACR.

Assign at resource group scope (e.g. `code-analysis-phase-1`):

```bash
az role assignment create --assignee "your-user@domain.com" \
  --role "Contributor" \
  --scope "/subscriptions/<subscription-id>/resourceGroups/code-analysis-phase-1"
```

### Minimum RBAC actions (if using custom roles)

| Purpose | Provider / scope | Actions |
|--------|-------------------|--------|
| Read resource group | `Microsoft.Resources` | `resourceGroups/read` |
| Container Registry | `Microsoft.ContainerRegistry` | `registries/*` (or create, read, update, delete, listKeys) |
| Log Analytics | `Microsoft.OperationalInsights` | `workspaces/*` |
| Container Apps | `Microsoft.App` | `managedEnvironments/*`, `containerApps/*` |
| User-assigned identity | `Microsoft.ManagedIdentity` | `userAssignedIdentities/*` |
| Assign AcrPull to identity | `Microsoft.Authorization` | `roleAssignments/write`, `roleAssignments/read` (on ACR scope or RG) |

All of the above are at **resource group scope**. Subscription-wide permissions are not required.

### What is *not* required

- **Resource provider registration** (`Microsoft.*/register/action`) – Terraform is configured with `skip_provider_registration = true`. Required providers must already be registered on the subscription by an Owner.

## 1. Test Terraform (no resources created)

```bash
cd deploy/azure
cp terraform.tfvars.example terraform.tfvars
# Edit terraform.tfvars if needed (acr_name must be globally unique)

terraform init
terraform plan
```

- **plan**: Shows what would be created (ACR, Log Analytics, Container App Environment, **3 Container Apps**: agent, workspace-api, frontend). Fails if resource group is missing.

## 2. Deploy infrastructure

When **`skip_acr_managed_identity = true`** (default when you lack permission to assign AcrPull to a managed identity), Azure requires both ACR username and password for the container apps. Use one of the following for **initial deploy and any later `terraform apply`**:

**Option A – apply without putting the password in a file (recommended):**

```bash
cd deploy/azure
chmod +x apply-with-acr-password.sh
./apply-with-acr-password.sh -auto-approve
```

`apply-with-acr-password.sh` fetches the ACR password with `az acr credential show` and runs `terraform apply` with `TF_VAR_acr_admin_password` set so the password is never written to disk. Use it whenever you run Terraform (first apply or subsequent changes). You can pass any Terraform args, e.g. `./apply-with-acr-password.sh -auto-approve` or `./apply-with-acr-password.sh` (prompts for confirm).

**Option B – set the password in tfvars:**

```bash
# Get password (run once)
az acr credential show -n <acr_name> -o tsv --query "passwords[0].value"

# Add to terraform.tfvars (do not commit):
# acr_admin_password = "<paste-here>"

terraform apply -auto-approve
```

Apply creates ACR (if new), Log Analytics, Container App Environment, and **agent**, **workspace-api**, and **frontend** Container Apps.

## 3. Build and Deploy (Local Build - Recommended)

Always use local builds (`--local` flag) for the fastest and most reliable deployment. This builds images on your machine and pushes them to Azure, bypassing slow source code uploads.

The script:
1.  **Builds in parallel:** Agent, Workspace, and Frontend build simultaneously.
2.  **Uses unique tags:** Generates a timestamped tag (e.g., `v20260201-1200`) for every deploy.
3.  **Fast Context:** Automatically prunes `node_modules` and heavy folders before building.
4.  **Zero-Downtime Update:** Triggers a new revision on Azure only after a successful push.

```bash
cd deploy/azure

# Deploy all services (fastest/standard)
./deploy.sh all --local

# Or deploy a single service
./deploy.sh agent --local
./deploy.sh workspace --local
./deploy.sh frontend --local
```

### Shared base image (agent + workspace)

Agent and workspace use a **shared base image** (`mcp-agent-base:latest`) so most layers are reused and only app layers (binary, configs) are pushed on each deploy. Build and push the base **once** (or when you change OS/runtime deps), then deploy as usual:

```bash
# One-time (or when base deps change): build and push the shared base
./deploy.sh base --local

# Then deploy app images (only small layers are pushed)
./deploy.sh all --local
```

Without building the base first, agent and workspace builds will fail (they `FROM` the base). Targets: `agent` | `workspace` | `frontend` | `all` | `base`.

> **Note:** Remote builds (without `--local`) are available but much slower as they upload your entire source code to Azure for every build.

## 4. Test the deployment

Get URLs:

```bash
terraform output frontend_fqdn
terraform output agent_fqdn
terraform output workspace_api_fqdn
```

- Open **frontend_fqdn** in a browser after pushing the frontend image.

### Health check API

| Service       | Endpoint         | Example response                          |
|---------------|------------------|-------------------------------------------|
| Agent         | `GET /api/health` | `{"status":"healthy","time":"...","config":{...}}` |
| Workspace API | `GET /health`    | `{"status":"healthy","service":"planner-api"}` |
| Frontend      | `GET /health`    | `{"status":"healthy","service":"frontend"}` |

Run health checks (from `deploy/azure`):

```bash
chmod +x healthcheck.sh
./healthcheck.sh
```

### Viewing Azure Container App logs

To see what happened (e.g. for "all LLMs failed" or other errors), stream or show recent logs:

```bash
cd deploy/azure

# Quick check: last 200 lines and highlight known LLM/API errors (response is nil, DeploymentNotFound, etc.)
./agent-logs.sh
./agent-logs.sh --tail 500          # more lines
./agent-logs.sh --follow            # stream live

# Or manually (resource group and app name from Terraform):
RG="$(terraform output -raw resource_group_name)"
AGENT_APP="$(terraform output -raw agent_container_app_name)"
az containerapp logs show --name "$AGENT_APP" --resource-group "$RG" --follow
az containerapp logs show --name "$AGENT_APP" --resource-group "$RG" --tail 100
```

**What to look for in agent logs when Azure fails with “same config” as local:**
- **`response is nil`** or **`choice.Content is empty`** → server is likely on an **old agent image** (without the multi-llm-provider-go streaming fixes). Rebuild and redeploy the agent (e.g. `./deploy.sh agent --local`) and ensure the new revision is active.
- **`DeploymentNotFound`** or **404** → deployment name or resource mismatch; compare deployment name and endpoint in logs with Azure OpenAI Studio and with what works locally.

Or in **Azure Portal**: open the Container App → **Log stream** or **Logs** (Log Analytics).

## 5. Test locally first (optional)

Run the full stack locally with production-style images:

```bash
# From repo root
docker compose -f docker-compose.yml -f docker-compose.prod.yml up --build
```

Then open http://localhost:5173 (frontend), http://localhost:8000 (agent), http://localhost:8081 (workspace-api).

## Variables

| Variable | Description |
|----------|-------------|
| `resource_group_name` | Existing RG name (e.g. `code-analysis-phase-1`) |
| `project_name` | Prefix for resource names (e.g. `mcpagent`) |
| `acr_name` | ACR name (globally unique, 5–50 alphanumeric) |
| `skip_acr_managed_identity` | If true, use ACR admin credentials; set `acr_admin_password` in tfvars |
| `agent_image_tag` | Tag for mcp-agent image (default `latest`) |
| `workspace_api_image_tag` | Tag for workspace-api image |
| `frontend_image_tag` | Tag for frontend image (default `latest`) |
| `openai_api_key` | Optional; set via TF_VAR or tfvars (sensitive) |
| `agent_env` | Optional map of env vars for agent |
| `workspace_api_env` | Optional map of env vars for workspace-api |
| `multi_user_mode` | `false` = single-user (no login); `true` = JWT multi-user (see [multi_user_authentication.md](../../docs/multi_user_authentication.md)) |

**Authentication:** By default the deployment is **single-user** (`multi_user_mode = false`): no login, all requests use default user. Set `multi_user_mode = true` and configure `AUTH_*` in `agent_env` for JWT multi-user. See [docs/multi_user_authentication.md](../../docs/multi_user_authentication.md).

### MCP server config (like K8s)

The agent image uses an MCP config file baked in at build time. Optionally create **`deploy/azure/mcp_config.json`** (same idea as **`deploy/k8s/agent/mcp_config.json`**): copy from **`agent_go/configs/mcp_servers_clean_user.json`** and edit. If `mcp_config.json` is missing, the build uses `agent_go/configs/mcp_servers_clean_user.json`. The container runs with `--mcp-config /app/configs/mcp_servers_clean_user.json`. The file is in `.gitignore` so you can keep local edits uncommitted.

## Database (PostgreSQL)

The deployment now provisions an **Azure Database for PostgreSQL Flexible Server** for persistent chat history and reliability. This replaces the previous SQLite/Azure Files setup which was prone to locking issues.

### Configuration Required

You **must** provide a database admin password.

Add this to your `terraform.tfvars` file (do not commit it):

```hcl
postgres_admin_password = "YourStrongPassword123!"
```

Or set it via environment variable:

```bash
export TF_VAR_postgres_admin_password="YourStrongPassword123!"
```

The `agent` container app is automatically configured with:
- `DB_TYPE=postgres`
- `DATABASE_URL` (constructed securely from your inputs)

### Access

The database is configured to allow access from Azure Services (Container Apps). It also has `public_network_access_enabled = true` to allow connections from your local machine if you whitelist your IP, but the firewall rule defaults to Azure-internal only.

### Enable uuid-ossp extension (required for agent migrations)

Agent migrations need the `uuid-ossp` extension. Terraform handles this automatically:

1. **Allowlist** – `azurerm_postgresql_flexible_server_configuration.uuid_ossp` sets `azure.extensions = uuid-ossp`.
2. **Firewall (optional)** – If `allow_terraform_runner_ip` is true (default), a firewall rule is added for the machine running `terraform apply` so it can connect to PostgreSQL.
3. **CREATE EXTENSION** – A `null_resource` runs `psql ... -c 'CREATE EXTENSION IF NOT EXISTS "uuid-ossp";'` during apply (requires `psql` and `postgres_admin_password` in tfvars or `TF_VAR_postgres_admin_password`). If apply runs from a network that cannot reach the DB (e.g. CI), this step may time out; the agent’s migration 000 also runs `CREATE EXTENSION IF NOT EXISTS "uuid-ossp"` on startup, so once the extension is allow-listed, restarting the agent is enough.

**Manual fallback** – From `deploy/azure`, run `./create_uuid_extension.sh` (prompts for password or uses `TF_VAR_postgres_admin_password`). Then restart the agent (new revision or `./deploy.sh agent --local`).

## Workspace API: empty workspace-docs on deploy

The **workspace-api** image creates an empty `workspace-docs` tree: `Downloads/`, `Chats/`, and `Workspace/` under `/app/workspace-docs`. Rebuild and push the workspace image after changing `workspace/Dockerfile` for this to apply.

## Cleanup

```bash
cd deploy/azure
terraform destroy
```

Confirm with `yes`. This removes all created resources; the existing resource group is left unchanged.
