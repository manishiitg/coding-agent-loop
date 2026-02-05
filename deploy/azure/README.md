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

## Workspace API: empty workspace-docs on deploy

The **workspace-api** image creates an empty `workspace-docs` tree: `Downloads/`, `Chats/`, and `Workspace/` under `/app/workspace-docs`. Rebuild and push the workspace image after changing `workspace/Dockerfile` for this to apply.

## Cleanup

```bash
cd deploy/azure
terraform destroy
```

Confirm with `yes`. This removes all created resources; the existing resource group is left unchanged.
