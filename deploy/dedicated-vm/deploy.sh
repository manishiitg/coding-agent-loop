#!/usr/bin/env bash
# Deploy MCP Agent Builder to a dedicated VM.
# Usage: ./deploy.sh <vm-ip> [setup|build|deploy|all]
#
# Stages:
#   setup  - Run setup-server.sh on the VM (first time only)
#   build  - Rsync code + build Docker images on the VM
#   deploy - Start/restart containers
#   all    - All of the above
#
# Examples:
#   ./deploy.sh 138.201.227.99 all       # Full first-time deploy
#   ./deploy.sh 138.201.227.99 build     # Rebuild after code changes
#   ./deploy.sh 138.201.227.99 deploy    # Restart services only

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

VM_HOST="${1:-}"
STAGE="${2:-all}"

if [ -z "$VM_HOST" ]; then
  echo "Usage: ./deploy.sh <vm-ip> [setup|build|deploy|all]"
  exit 1
fi

# SSH config
SSH_USER="${SSH_USER:-root}"
SSH_KEY="${SSH_KEY:-}"
if [ -n "$SSH_KEY" ]; then
  SSH_OPTS="-i $SSH_KEY -o StrictHostKeyChecking=no -o ConnectTimeout=10"
else
  SSH_OPTS="-o StrictHostKeyChecking=no -o ConnectTimeout=10"
fi

REMOTE_DIR="/opt/mcp-agent"

# Helper: run command on remote
remote() {
  ssh $SSH_OPTS "$SSH_USER@$VM_HOST" "$@"
}

# Helper: copy file to remote
remote_cp() {
  scp $SSH_OPTS "$1" "$SSH_USER@$VM_HOST:$2"
}

echo "==> Deploying to $VM_HOST (stage: $STAGE)"

# ===================================================================
# STAGE: setup
# ===================================================================
if [[ "$STAGE" == "setup" || "$STAGE" == "all" ]]; then
  echo ""
  echo "==> [1/3] Setting up server..."
  remote_cp "$SCRIPT_DIR/setup-server.sh" "/tmp/setup-server.sh"
  remote "chmod +x /tmp/setup-server.sh && /tmp/setup-server.sh"
  echo "==> Server setup complete."
fi

# ===================================================================
# STAGE: build
# ===================================================================
if [[ "$STAGE" == "build" || "$STAGE" == "all" ]]; then
  echo ""
  echo "==> [2/3] Syncing code and building images..."

  # Ensure remote directory exists
  remote "mkdir -p $REMOTE_DIR/src"

  # Rsync source code (only what's needed for builds)
  echo "    Syncing agent_go/..."
  rsync -az --delete \
    --exclude='.git' --exclude='node_modules' --exclude='*.db' \
    --exclude='logs/' --exclude='bin/' --exclude='tmp/' \
    -e "ssh $SSH_OPTS" \
    "$REPO_ROOT/agent_go/" "$SSH_USER@$VM_HOST:$REMOTE_DIR/src/agent_go/"

  echo "    Syncing workspace/..."
  rsync -az --delete \
    --exclude='.git' --exclude='node_modules' --exclude='*.db' \
    --exclude='workspace-docs/' --exclude='logs/' \
    -e "ssh $SSH_OPTS" \
    "$REPO_ROOT/workspace/" "$SSH_USER@$VM_HOST:$REMOTE_DIR/src/workspace/"

  echo "    Syncing frontend/..."
  rsync -az --delete \
    --exclude='.git' --exclude='node_modules' --exclude='dist/' \
    -e "ssh $SSH_OPTS" \
    "$REPO_ROOT/frontend/" "$SSH_USER@$VM_HOST:$REMOTE_DIR/src/frontend/"

  # Sync extra local dependencies
  PARENT_DIR="$(cd "$REPO_ROOT/.." && pwd)"
  if [ -d "$PARENT_DIR/mcpagent" ]; then
    echo "    Syncing mcpagent/..."
    rsync -az --delete --exclude='.git' --exclude='*.db' \
      -e "ssh $SSH_OPTS" \
      "$PARENT_DIR/mcpagent/" "$SSH_USER@$VM_HOST:$REMOTE_DIR/src/mcpagent/"
  fi
  if [ -d "$PARENT_DIR/multi-llm-provider-go" ]; then
    echo "    Syncing multi-llm-provider-go/..."
    rsync -az --delete --exclude='.git' --exclude='*.db' \
      -e "ssh $SSH_OPTS" \
      "$PARENT_DIR/multi-llm-provider-go/" "$SSH_USER@$VM_HOST:$REMOTE_DIR/src/multi-llm-provider-go/"
  fi

  # Fix go.mod replace directives (rsync copies local paths that don't match server layout)
  echo "    Fixing go.mod replace directives..."
  remote "export PATH=\$PATH:/usr/local/go/bin && cd $REMOTE_DIR/src/agent_go && \
    go mod edit -dropreplace=github.com/manishiitg/mcpagent 2>/dev/null; \
    go mod edit -dropreplace=github.com/manishiitg/multi-llm-provider-go 2>/dev/null; \
    go mod edit -dropreplace=github.com/manishiitg/mcp-agent-builder-go/workspace 2>/dev/null; \
    go mod edit -replace=github.com/manishiitg/mcpagent=../mcpagent && \
    go mod edit -replace=github.com/manishiitg/multi-llm-provider-go=../multi-llm-provider-go && \
    go mod edit -replace=github.com/manishiitg/mcp-agent-builder-go/workspace=../workspace && \
    cd ../mcpagent && \
    go mod edit -dropreplace=github.com/manishiitg/multi-llm-provider-go 2>/dev/null; \
    go mod edit -dropreplace=github.com/manishiitg/mcp-agent-builder-go/workspace 2>/dev/null; \
    go mod edit -replace=github.com/manishiitg/multi-llm-provider-go=../multi-llm-provider-go && \
    go mod edit -replace=github.com/manishiitg/mcp-agent-builder-go/workspace=../workspace && \
    cd ../multi-llm-provider-go && \
    go mod edit -dropreplace=github.com/manishiitg/mcp-agent-builder-go/workspace 2>/dev/null; \
    go mod edit -replace=github.com/manishiitg/mcp-agent-builder-go/workspace=../workspace"

  # Build mcpbridge (required for CLI provider MCP bridge)
  echo "    Building mcpbridge..."
  remote "export PATH=\$PATH:/usr/local/go/bin:/root/go/bin && export HOME=/root && export GOPATH=/root/go && \
    cd $REMOTE_DIR/src/mcpagent && go install ./cmd/mcpbridge/ 2>&1 | tail -3"

  # Copy deploy configs
  echo "    Copying deploy configs..."
  remote_cp "$SCRIPT_DIR/docker-compose.yml" "$REMOTE_DIR/docker-compose.yml"
  remote_cp "$SCRIPT_DIR/Caddyfile" "$REMOTE_DIR/Caddyfile"
  remote_cp "$SCRIPT_DIR/Dockerfile.base" "$REMOTE_DIR/Dockerfile.base"
  remote_cp "$SCRIPT_DIR/Dockerfile.agent" "$REMOTE_DIR/Dockerfile.agent"
  remote_cp "$SCRIPT_DIR/Dockerfile.workspace" "$REMOTE_DIR/Dockerfile.workspace"

  # Copy MCP config
  if [ -f "$SCRIPT_DIR/mcp_config.json" ]; then
    remote "mkdir -p $REMOTE_DIR/src/agent_go/configs/"
    remote_cp "$SCRIPT_DIR/mcp_config.json" "$REMOTE_DIR/src/agent_go/configs/mcp_servers_clean_user.json"
  fi

  # Copy .env if exists
  if [ -f "$SCRIPT_DIR/.env" ]; then
    remote_cp "$SCRIPT_DIR/.env" "$REMOTE_DIR/.env"
  fi

  # Build base image first, then services
  echo "    Building base image..."
  remote "cd $REMOTE_DIR && docker build -f Dockerfile.base -t mcp-agent-base:latest ."

  echo "    Building all services..."
  remote "cd $REMOTE_DIR && docker compose build --parallel"

  echo "==> Build complete."
fi

# ===================================================================
# STAGE: deploy
# ===================================================================
if [[ "$STAGE" == "deploy" || "$STAGE" == "all" ]]; then
  echo ""
  echo "==> [3/3] Starting services..."

  remote << 'EOF'
    cd /opt/mcp-agent
    docker compose down --remove-orphans 2>/dev/null || true
    docker compose up -d
    echo ""
    echo "==> Container status:"
    docker compose ps
EOF

  echo ""
  echo "==> Deployment complete!"
  echo "    Frontend: http://$VM_HOST"
  echo "    Agent API: http://$VM_HOST/api/health"
  echo "    Workspace: http://$VM_HOST/workspace/health"
fi
