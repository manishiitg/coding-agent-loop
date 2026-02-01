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

When **`skip_acr_managed_identity = true`**, Azure requires both ACR username and password for the container apps. Either:

**Option A – apply without putting the password in a file (recommended):**

```bash
cd deploy/azure
chmod +x apply-with-acr-password.sh
./apply-with-acr-password.sh -auto-approve
```

This script fetches the ACR password with `az acr credential show` and runs `terraform apply` with `TF_VAR_acr_admin_password` set so the password is never written to disk.

**Option B – set the password in tfvars:**

```bash
# Get password (run once)
az acr credential show -n <acr_name> -o tsv --query "passwords[0].value"

# Add to terraform.tfvars (do not commit):
# acr_admin_password = "<paste-here>"

terraform apply -auto-approve
```

Apply creates ACR (if new), Log Analytics, Container App Environment, and **agent**, **workspace-api**, and **frontend** Container Apps.

## 3. Build, push, and restart (one command)

After initial `terraform apply`, use the deploy script to build on ACR's native amd64 servers (no local Docker or QEMU needed), push images, restart apps, and run health checks:

```bash
cd deploy/azure

# Deploy all services (~5 min)
./deploy.sh all

# Or deploy a single service
./deploy.sh frontend
./deploy.sh workspace
./deploy.sh agent
```

The script uses `az acr build` which builds remotely on Azure — much faster than local `docker build --platform linux/amd64` on Apple Silicon.

**Important:** The agent build requires a `.dockerignore` file at the parent of the repo (e.g. `ai-work/.dockerignore`) to avoid uploading unnecessary files. This file is already created and excludes everything except `agent_go/`, `workspace/`, `mcpagent/`, and `multi-llm-provider-go/`.

### Manual build (alternative)

If you prefer local Docker builds:

```bash
ACR=$(cd deploy/azure && terraform output -raw acr_login_server)
az acr login -n <acr_name>

# From parent directory (for agent with local deps)
docker build --platform linux/amd64 -t "$ACR/mcp-agent:latest" -f mcp-agent-builder-go/agent_go/Dockerfile.localdeps .

# From repo root
docker build --platform linux/amd64 -t "$ACR/workspace-api:latest" -f workspace/Dockerfile workspace

AGENT_URL=$(cd deploy/azure && terraform output -raw agent_fqdn)
WORKSPACE_URL=$(cd deploy/azure && terraform output -raw workspace_api_fqdn)
docker build --platform linux/amd64 \
  --build-arg VITE_API_BASE_URL="$AGENT_URL" \
  --build-arg VITE_WORKSPACE_API_URL="$WORKSPACE_URL" \
  -t "$ACR/mcp-agent-frontend:latest" \
  -f frontend/Dockerfile.prod frontend

docker push "$ACR/mcp-agent:latest"
docker push "$ACR/workspace-api:latest"
docker push "$ACR/mcp-agent-frontend:latest"
```

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

## Known Limitation: Chat History (SQLite on Azure Files)

The agent uses SQLite for chat history. Azure Files (SMB) does not support the POSIX file locking that SQLite requires, so the agent falls back to `/tmp/chat_history.db`. This means **chat history is lost on every container restart or redeployment**.

### Planned fix: Azure Database for PostgreSQL

The Go agent already supports PostgreSQL via `--db-type postgres` and `DATABASE_URL`. To enable persistent chat history, add an Azure Database for PostgreSQL Flexible Server. This requires the `Microsoft.DBforPostgreSQL` resource provider to be registered on the subscription.

**Steps** (requires subscription Owner or Contributor):

1. Register the provider:
   ```bash
   az provider register --namespace Microsoft.DBforPostgreSQL
   ```
2. Add the PostgreSQL Terraform resources (server, firewall rule, database) to `main.tf`
3. Wire the agent container to use `DATABASE_URL` and `DB_TYPE=postgres` instead of `DB_PATH`
4. Run `terraform apply`

Until then, the agent-data Azure Files volume is still mounted for other data (OAuth tokens, configs) but SQLite writes go to the ephemeral `/tmp` directory.

## Workspace API: empty workspace-docs on deploy

The **workspace-api** image creates an empty `workspace-docs` tree: `Downloads/`, `Chats/`, and `Workspace/` under `/app/workspace-docs`. Rebuild and push the workspace image after changing `workspace/Dockerfile` for this to apply.

## Cleanup

```bash
cd deploy/azure
terraform destroy
```

Confirm with `yes`. This removes all created resources; the existing resource group is left unchanged.
