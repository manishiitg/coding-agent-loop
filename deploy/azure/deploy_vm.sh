#!/bin/bash

# Deployment script for Azure VM
# Usage: ./deploy_vm.sh <vm-ip-or-hostname> [services]
# Example: ./deploy_vm.sh 20.91.186.189 all

set -e

VM_HOST=$1
SERVICES=$2

if [ -z "$VM_HOST" ]; then
    echo "Usage: ./deploy_vm.sh <vm-ip-or-hostname> [services]"
    exit 1
fi

# Configuration
cd terraform
export ACR_NAME="${ACR_NAME:-$(terraform output -raw acr_name 2>/dev/null || echo "mcpagentacr")}"
cd ..

REMOTE_DIR="/home/appuser/mcp-agent"
SSH_USER="appuser" # Change if your VM user is different
SSH_KEY="~/.ssh/mcp_azure_key"
SSH_OPTS="-i $SSH_KEY -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null"

echo "==> Deploying to Azure VM: $VM_HOST"
echo "==> Using ACR: $ACR_NAME"

if [ -z "$ACR_PASSWORD" ]; then
    echo "Error: ACR_PASSWORD environment variable is not set."
    echo "Run: export ACR_PASSWORD=\$(az acr credential show -n \$ACR_NAME --query \"passwords[0].value\" -o tsv)"
    exit 1
fi

# 1. Build and push images to ACR (Optional - using local build context)
if [[ "$SERVICES" == "all" || "$SERVICES" == "" ]]; then
    echo "==> Building and pushing all images..."
    # Set relative URLs for VM deployment (Caddy reverse proxy)
    export FRONTEND_API_URL=""
    export FRONTEND_WORKSPACE_URL="/workspace"
    ./deploy.sh all --local --build-only
else
    echo "==> Building and pushing specific service: $SERVICES..."
    # Set relative URLs for VM deployment
    export FRONTEND_API_URL=""
    export FRONTEND_WORKSPACE_URL="/workspace"
    ./deploy.sh "$SERVICES" --local --build-only
fi

# 2. Prepare remote directory
echo "==> Preparing remote directory on VM..."
ssh $SSH_OPTS $SSH_USER@$VM_HOST "mkdir -p $REMOTE_DIR"

# 3. Copy configuration and environment
echo "==> Copying configuration files..."
# Create a temporary Caddyfile with the actual VM host
sed "s/{DOMAIN_OR_IP}/$VM_HOST/g" Caddyfile > Caddyfile.tmp
scp $SSH_OPTS docker-compose.vm.yml $SSH_USER@$VM_HOST:$REMOTE_DIR/docker-compose.yml
scp $SSH_OPTS Caddyfile.tmp $SSH_USER@$VM_HOST:$REMOTE_DIR/Caddyfile
rm Caddyfile.tmp

# Create/Update .env on VM with ACR_NAME
echo "==> Setting ACR_NAME on VM..."
ssh $SSH_OPTS $SSH_USER@$VM_HOST "echo \"ACR_NAME=$ACR_NAME\" > $REMOTE_DIR/.env"

# Copy local .env if it exists (append to remote .env)
if [ -f ".env" ]; then
    echo "    Found local .env file, appending to VM .env..."
    # Filter out ACR_NAME if it exists in local .env to avoid duplicates
    grep -v "ACR_NAME=" .env | ssh $SSH_OPTS $SSH_USER@$VM_HOST "cat >> $REMOTE_DIR/.env"
fi

# 4. Pull and Restart on VM
echo "==> Updating services on VM..."
# Pass ACR password via stdin to avoid logging it
echo "$ACR_PASSWORD" | ssh $SSH_OPTS $SSH_USER@$VM_HOST "cat - | docker login $ACR_NAME.azurecr.io -u $ACR_NAME --password-stdin"

ssh $SSH_OPTS $SSH_USER@$VM_HOST << EOF
    cd $REMOTE_DIR
    docker compose pull
    docker compose up -d
    docker compose restart caddy
    docker compose ps
EOF

echo "==> SUCCESS! Deployment to VM complete."
echo "Access your frontend at http://$VM_HOST"
