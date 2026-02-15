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
export ACR_NAME="mcpagentacr"
REMOTE_DIR="/home/appuser/mcp-agent"
SSH_USER="appuser" # Change if your VM user is different
SSH_KEY="~/.ssh/mcp_azure_key"
SSH_OPTS="-i $SSH_KEY -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null"

echo "==> Deploying to Azure VM: $VM_HOST"

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
scp $SSH_OPTS docker-compose.vm.yml $SSH_USER@$VM_HOST:$REMOTE_DIR/docker-compose.yml
scp $SSH_OPTS Caddyfile $SSH_USER@$VM_HOST:$REMOTE_DIR/Caddyfile

# Copy .env if it exists locally
if [ -f ".env" ]; then
    echo "    Found local .env file, copying to VM..."
    scp $SSH_OPTS .env $SSH_USER@$VM_HOST:$REMOTE_DIR/.env
fi

# 4. Pull and Restart on VM
echo "==> Updating services on VM..."
# Pass ACR password via stdin to avoid logging it
echo "$ACR_PASSWORD" | ssh $SSH_OPTS $SSH_USER@$VM_HOST "cat - | docker login mcpagentacr.azurecr.io -u mcpagentacr --password-stdin"

ssh $SSH_OPTS $SSH_USER@$VM_HOST << EOF
    cd $REMOTE_DIR
    docker compose pull
    docker compose up -d
    docker compose restart caddy
    docker compose ps
EOF

echo "==> SUCCESS! Deployment to VM complete."
echo "Access your frontend at http://$VM_HOST"
