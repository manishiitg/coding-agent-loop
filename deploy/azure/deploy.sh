#!/usr/bin/env bash
# Deploy all services to Azure Container Apps using ACR Build (native amd64, no local Docker needed).
# Usage: ./deploy.sh [agent|workspace|frontend|all]
# Default: all

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
PARENT_DIR="$(cd "$REPO_ROOT/.." && pwd)"

cd "$SCRIPT_DIR"

# Get config from terraform
ACR_NAME="$(terraform output -raw acr_name 2>/dev/null)"
AGENT_FQDN="$(terraform output -raw agent_fqdn 2>/dev/null)"
WORKSPACE_FQDN="$(terraform output -raw workspace_api_fqdn 2>/dev/null)"

if [ -z "$ACR_NAME" ]; then
  echo "Error: Could not get ACR name from terraform output. Run 'terraform init && terraform apply' first."
  exit 1
fi

TARGET="${1:-all}"

build_agent() {
  echo "==> Building agent on ACR..."
  cd "$PARENT_DIR"
  az acr build -r "$ACR_NAME" \
    --platform linux/amd64 \
    -t mcp-agent:latest \
    -f mcp-agent-builder-go/agent_go/Dockerfile.localdeps \
    .
  echo "==> Agent built and pushed."
}

build_workspace() {
  echo "==> Building workspace-api on ACR..."
  cd "$REPO_ROOT/workspace"
  az acr build -r "$ACR_NAME" \
    --platform linux/amd64 \
    -t workspace-api:latest \
    -f Dockerfile \
    .
  echo "==> Workspace-api built and pushed."
}

build_frontend() {
  echo "==> Building frontend on ACR..."
  cd "$REPO_ROOT/frontend"
  az acr build -r "$ACR_NAME" \
    --platform linux/amd64 \
    -t mcp-agent-frontend:latest \
    -f Dockerfile.prod \
    --build-arg VITE_API_BASE_URL="$AGENT_FQDN" \
    --build-arg VITE_WORKSPACE_API_URL="$WORKSPACE_FQDN" \
    .
  echo "==> Frontend built and pushed."
}

restart_app() {
  local app_name="$1"
  local revision
  revision="$(az containerapp revision list -n "$app_name" -g code-analysis-phase-1 --query '[0].name' -o tsv)"
  az containerapp revision restart -n "$app_name" -g code-analysis-phase-1 --revision "$revision" -o none
  echo "    Restarted $app_name"
}

restart_apps() {
  echo "==> Restarting container apps..."
  case "$TARGET" in
    agent)    restart_app "mcpagent-agent" ;;
    workspace) restart_app "mcpagent-workspace-api" ;;
    frontend) restart_app "mcpagent-frontend" ;;
    all)
      restart_app "mcpagent-agent"
      restart_app "mcpagent-workspace-api"
      restart_app "mcpagent-frontend"
      ;;
  esac
}

# Build
case "$TARGET" in
  agent)
    build_agent
    ;;
  workspace)
    build_workspace
    ;;
  frontend)
    build_frontend
    ;;
  all)
    # Build workspace and frontend in parallel, agent sequentially (large context)
    build_workspace &
    PID_WS=$!
    build_frontend &
    PID_FE=$!
    build_agent

    wait $PID_WS || { echo "Workspace-api build failed"; exit 1; }
    wait $PID_FE || { echo "Frontend build failed"; exit 1; }
    ;;
  *)
    echo "Usage: $0 [agent|workspace|frontend|all]"
    exit 1
    ;;
esac

# Restart
restart_apps

# Health checks
echo ""
echo "==> Waiting for services to start..."
sleep 15
"$SCRIPT_DIR/healthcheck.sh"

echo ""
echo "Done. Frontend: $(terraform output -raw frontend_fqdn)"
