#!/usr/bin/env bash
# Quick deploy all latest code to the Hetzner dedicated VM.
# Usage: ./quick-deploy.sh [frontend|agent|workspace|all]
# Default: all
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
PARENT_DIR="$(cd "$REPO_ROOT/.." && pwd)"

VM=138.201.227.99
KEY="$HOME/.ssh/hetzner_mcp"
SSH="ssh -i $KEY -o StrictHostKeyChecking=no root@$VM"
SCP="scp -i $KEY -o StrictHostKeyChecking=no"
RSYNC_SSH="ssh -i $KEY -o StrictHostKeyChecking=no"
REMOTE="/opt/mcp-agent"
TARGET="${1:-all}"

echo "==> Deploying [$TARGET] to $VM"

# --- Sync source ---
if [[ "$TARGET" == "all" || "$TARGET" == "agent" ]]; then
  echo "    Syncing agent_go..."
  rsync -az --delete --exclude='.git' --exclude='node_modules' --exclude='*.db' --exclude='logs/' --exclude='bin/' --exclude='tmp/' \
    -e "$RSYNC_SSH" "$REPO_ROOT/agent_go/" "root@$VM:$REMOTE/src/agent_go/" &
fi

if [[ "$TARGET" == "all" || "$TARGET" == "workspace" ]]; then
  echo "    Syncing workspace..."
  rsync -az --delete --exclude='.git' --exclude='node_modules' --exclude='*.db' --exclude='workspace-docs/' --exclude='logs/' \
    -e "$RSYNC_SSH" "$REPO_ROOT/workspace/" "root@$VM:$REMOTE/src/workspace/" &
fi

if [[ "$TARGET" == "all" || "$TARGET" == "agent" ]]; then
  echo "    Syncing mcpagent..."
  rsync -az --delete --exclude='.git' --exclude='*.db' \
    -e "$RSYNC_SSH" "$PARENT_DIR/mcpagent/" "root@$VM:$REMOTE/src/mcpagent/" &
  echo "    Syncing multi-llm-provider-go..."
  rsync -az --delete --exclude='.git' --exclude='*.db' \
    -e "$RSYNC_SSH" "$PARENT_DIR/multi-llm-provider-go/" "root@$VM:$REMOTE/src/multi-llm-provider-go/" &
fi

if [[ "$TARGET" == "all" || "$TARGET" == "frontend" ]]; then
  echo "    Building frontend..."
  (cd "$REPO_ROOT/frontend" && VITE_API_BASE_URL="" VITE_WORKSPACE_API_URL=/api/wp npx vite build 2>&1 | tail -1) &
fi

wait
echo "==> Sync complete"

# --- Sync frontend dist ---
if [[ "$TARGET" == "all" || "$TARGET" == "frontend" ]]; then
  echo "    Syncing frontend dist..."
  rsync -az -e "$RSYNC_SSH" "$REPO_ROOT/frontend/dist/" "root@$VM:$REMOTE/src/frontend-dist/"
fi

# --- Server-side: fix replaces, rebuild, restart ---
echo "==> Building and restarting on server..."
$SSH bash -s "$TARGET" << 'REMOTE_SCRIPT'
TARGET=$1
export PATH=$PATH:/usr/local/go/bin:/root/go/bin
export HOME=/root
export GOPATH=/root/go
REMOTE=/opt/mcp-agent

if [[ "$TARGET" == "all" || "$TARGET" == "agent" ]]; then
  # Fix go.mod replace directives
  cd $REMOTE/src/agent_go
  go mod edit -dropreplace=github.com/manishiitg/mcpagent 2>/dev/null; go mod edit -dropreplace=github.com/manishiitg/multi-llm-provider-go 2>/dev/null; go mod edit -dropreplace=github.com/manishiitg/mcp-agent-builder-go/workspace 2>/dev/null
  go mod edit -replace=github.com/manishiitg/mcpagent=../mcpagent -replace=github.com/manishiitg/multi-llm-provider-go=../multi-llm-provider-go -replace=github.com/manishiitg/mcp-agent-builder-go/workspace=../workspace

  cd $REMOTE/src/mcpagent
  go mod edit -dropreplace=github.com/manishiitg/multi-llm-provider-go 2>/dev/null; go mod edit -dropreplace=github.com/manishiitg/mcp-agent-builder-go/workspace 2>/dev/null
  go mod edit -replace=github.com/manishiitg/multi-llm-provider-go=../multi-llm-provider-go -replace=github.com/manishiitg/mcp-agent-builder-go/workspace=../workspace

  cd $REMOTE/src/multi-llm-provider-go
  go mod edit -dropreplace=github.com/manishiitg/mcp-agent-builder-go/workspace 2>/dev/null
  go mod edit -replace=github.com/manishiitg/mcp-agent-builder-go/workspace=../workspace

  # Rebuild mcpbridge
  cd $REMOTE/src/mcpagent && go install ./cmd/mcpbridge/ 2>&1 | tail -1

  systemctl restart mcp-agent
  echo "    Agent restarting (~60s to compile)"
fi

if [[ "$TARGET" == "all" || "$TARGET" == "frontend" ]]; then
  cd $REMOTE/src/frontend-dist
  docker build -f /tmp/Dockerfile.frontend-prebuilt -t mcp-agent-frontend:latest . 2>&1 | tail -1
  cd $REMOTE && docker compose up -d --force-recreate frontend
  echo "    Frontend deployed"
fi

if [[ "$TARGET" == "all" || "$TARGET" == "workspace" ]]; then
  cd $REMOTE/src
  docker build -f ../Dockerfile.workspace --build-arg BASE_IMAGE=mcp-agent-base:latest -t mcp-agent-workspace-api:latest . 2>&1 | tail -1
  cd $REMOTE && docker compose up -d --force-recreate workspace-api
  echo "    Workspace deployed"
fi

if [[ "$TARGET" == "all" ]]; then
  cd $REMOTE && docker compose restart caddy
fi

echo "==> Done!"
REMOTE_SCRIPT
