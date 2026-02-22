# Azure VM Deployment (Production)

This guide describes how to deploy the Multi Agent Builder stack to an **Azure Virtual Machine (VM)**.

This architecture uses an Azure VM to provide full Linux kernel capabilities (namespaces, `unshare`, `mount`), which are required for the **FileSystem Isolation** features of the Workspace API.

## Architecture

- **Compute**: Azure Virtual Machine (Ubuntu Linux)
- **Orchestration**: Docker Compose running directly on the VM
- **Networking**: Caddy Reverse Proxy (Automatic HTTPS, Domain Routing)
- **Storage**: Local VM Disk (High performance, full POSIX compliance)
- **Database**: Azure Database for PostgreSQL (Managed) or local SQLite

## Prerequisites

1.  **Azure CLI**: Installed and logged in (`az login`).
2.  **Terraform**: Installed (v1.0+).
3.  **SSH Key**: An SSH public key for VM access.
    *   By default, this setup uses `~/.ssh/mcp_azure_key`.
    *   If you don't have it, generate it: `ssh-keygen -t rsa -b 4096 -f ~/.ssh/mcp_azure_key -N ""`

## 1. Provision Infrastructure (Terraform)

Use Terraform to create the Resource Group, Container Registry, Virtual Machine, Network, Public IP, and Security Groups.

```bash
cd deploy/azure/terraform

# Initialize Terraform
terraform init

# Apply Configuration (Pass your SSH public key)
# You can also customize acr_name if the default is taken
terraform apply -var="ssh_public_key=$(cat ~/.ssh/mcp_azure_key.pub)"
```

**Note the Outputs:**
- `public_ip_address`: The IP of your new VM.
- `public_fqdn`: The DNS name (e.g., `mcpagent-vm-v2.swedencentral.cloudapp.azure.com`).
- `acr_name`: The name of your created Azure Container Registry.

## 2. Deploy Application

Use the unified deployment script to build images, push to ACR, configure the VM, and start services.

### Required Environment Variables

Before deploying, you must set `ACR_PASSWORD` so the VM can pull images from ACR.
The script will try to detect `ACR_NAME` from Terraform, but you can also export it.

```bash
# Get ACR name from terraform output (if not already known)
export ACR_NAME=$(terraform -chdir=terraform output -raw acr_name)

# Get ACR password:
export ACR_PASSWORD=$(az acr credential show -n $ACR_NAME --query "passwords[0].value" -o tsv)
```

### Run the Deploy Script

**First Time Only:** Build and push the shared base image (used by Agent and Workspace).

```bash
cd deploy/azure
./deploy.sh base --local
```

**Deploy Application:**

```bash
# Syntax: ./deploy_vm.sh <VM_IP_OR_HOSTNAME> [service]
# Deploy everything:
./deploy_vm.sh <VM_IP_ADDRESS> all

# Deploy only specific services (faster):
./deploy_vm.sh <VM_IP_ADDRESS> agent
./deploy_vm.sh <VM_IP_ADDRESS> frontend
```

The script will:
1.  **Build** Docker images for Agent, Workspace, and Frontend (locally via Docker, cross-compiled for `linux/amd64`).
2.  **Push** them to your Azure Container Registry (ACR).
3.  **SSH** into the VM.
4.  **Copy** `docker-compose.vm.yml` and `Caddyfile`.
5.  **Pull** images (using `ACR_PASSWORD` for authentication) and **Start** containers.

## SSH Key Management

The deployment process uses a dedicated SSH key pair for security:
- **Public Key (`~/.ssh/mcp_azure_key.pub`)**: Uploaded to Azure via Terraform to authorize access to the `appuser` account on the VM.
- **Private Key (`~/.ssh/mcp_azure_key`)**: Stored on your local machine and used by `./deploy_vm.sh` to securely configure the VM.

**If you use a different key:**
Update the `SSH_KEY` variable at the top of `deploy/azure/deploy_vm.sh` to point to your private key path.

## 3. Access the Application

- **Frontend**: `https://<VM_DNS_NAME>`
- **Agent API**: `https://<VM_DNS_NAME>/api/health`
- **Workspace API**: `https://<VM_DNS_NAME>/workspace/health`

HTTPS is automatically provisioned by Caddy (Let's Encrypt) on the first request.

## Configuration

### Environment Variables (.env)
To configure secrets (API keys, Database URL), create a `.env` file on the VM:

1.  SSH into the VM:
    ```bash
    ssh appuser@<VM_IP>
    ```
2.  Create `.env` in `~/mcp-agent/.env`:
    ```bash
    nano ~/mcp-agent/.env
    ```
    Add your secrets:
    ```bash
    OPENAI_API_KEY=sk-...
    DATABASE_URL=postgres://...
    ```
3.  Restart the agent:
    ```bash
    cd ~/mcp-agent
    docker compose restart agent
    ```

### Updates
To deploy code changes:
```bash
./deploy_vm.sh <VM_IP> all
```
To deploy only one service (faster):
```bash
./deploy_vm.sh <VM_IP> frontend
```

## Troubleshooting & Logs

You can debug the running application by SSHing into the VM or running remote commands.

**Convenience Variable:**
For easier typing, set this variable in your terminal:
```bash
export VM_SSH="ssh -i ~/.ssh/mcp_azure_key appuser@<VM_IP> 'cd ~/mcp-agent &&"
# Usage: eval "$VM_SSH docker compose logs'"
```

### Viewing Logs

**1. Watch Real-time Logs (All Services):**
```bash
ssh -i ~/.ssh/mcp_azure_key appuser@<VM_IP> "cd ~/mcp-agent && docker compose logs -f"
```

**2. Watch Specific Service:**
```bash
# Agent (Backend)
ssh -i ~/.ssh/mcp_azure_key appuser@<VM_IP> "cd ~/mcp-agent && docker compose logs -f agent"

# Workspace API
ssh -i ~/.ssh/mcp_azure_key appuser@<VM_IP> "cd ~/mcp-agent && docker compose logs -f workspace-api"

# Caddy (HTTPS/Reverse Proxy)
ssh -i ~/.ssh/mcp_azure_key appuser@<VM_IP> "cd ~/mcp-agent && docker compose logs -f caddy"
```

**3. View Last 100 Lines:**
```bash
ssh -i ~/.ssh/mcp_azure_key appuser@<VM_IP> "cd ~/mcp-agent && docker compose logs --tail 100 agent"
```

### Common Tasks

**Check Container Status:**
```bash
ssh -i ~/.ssh/mcp_azure_key appuser@<VM_IP> "cd ~/mcp-agent && docker compose ps"
```

**Restart a Service:**
```bash
ssh -i ~/.ssh/mcp_azure_key appuser@<VM_IP> "cd ~/mcp-agent && docker compose restart agent"
```

**Reload Caddy Configuration:**
If HTTPS certificates are failing or routing is wrong:
```bash
ssh -i ~/.ssh/mcp_azure_key appuser@<VM_IP> "cd ~/mcp-agent && docker compose restart caddy"
```

## Future Improvements

For a production-at-scale environment, the following enhancements are recommended:

1.  **Centralized Logging:** Integrate the VM with **Azure Monitor (Log Analytics)** using the Azure Monitor Agent (AMA). This will allow you to:
    *   Store logs indefinitely outside the VM.
    *   Create dashboards and alerts for errors.
    *   Search across multiple services/VMs easily in the Azure Portal.
2.  **High Availability:** Migrate the database to a multi-zone configuration (Azure PostgreSQL already supports this).
3.  **Content Delivery:** Use **Azure Front Door** or **Application Gateway** to provide global acceleration and WAF (Web Application Firewall) protection.
4.  **CI/CD Integration:** Migrate the deployment from manual scripts to GitHub Actions or Azure DevOps using service principals.