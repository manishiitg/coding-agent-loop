#!/usr/bin/env bash
# Deploy all services to Azure Container Apps using ACR Build (native amd64).
# Optimized for speed (parallel builds) and safety (zero-downtime updates).
# Usage: ./deploy.sh [agent|workspace|frontend|all]
# Default: all

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
PARENT_DIR="$(cd "$REPO_ROOT/.." && pwd)"

cd "$SCRIPT_DIR"

# Get config from terraform
ACR_NAME="$(terraform output -raw acr_name 2>/dev/null)"
AGENT_APP="$(terraform output -raw agent_container_app_name 2>/dev/null || echo "mcpagent-agent")"
WORKSPACE_APP="$(terraform output -raw workspace_api_container_app_name 2>/dev/null || echo "mcpagent-workspace-api")"
FRONTEND_APP="$(terraform output -raw frontend_container_app_name 2>/dev/null || echo "mcpagent-frontend")"

# Fallback for app names if terraform output missing
PROJECT_NAME="mcpagent" # Update if you changed var.project_name
RG_NAME="code-analysis-phase-1" # Update if you changed resource group

if [ -z "$ACR_NAME" ]; then
  echo "Error: Could not get ACR name from terraform output. Run 'terraform init && terraform apply' first."
  exit 1
fi

TARGET="${1:-all}"
TAG="v$(date +%Y%m%d-%H%M%S)"

echo "==> Starting deployment: $TAG"
echo "==> ACR: $ACR_NAME"

# ------------------------------------------------------------------
# Build Functions (Run in background)
# ------------------------------------------------------------------

# Create a clean build context to speed up uploads
create_clean_context() {
  local TEMP_DIR="$1"
  echo "    Creating clean build context in $TEMP_DIR..."
  mkdir -p "$TEMP_DIR"
  
  # rsync from PARENT_DIR to TEMP_DIR with exclusions
  # We use a filter file or explicit excludes to avoid copying garbage
  rsync -avq "$PARENT_DIR/" "$TEMP_DIR/" \
    --exclude='.git' \
    --exclude='node_modules' \
    --exclude='workspace-docs' \
    --exclude='workflow-docs' \
    --exclude='logs' \
    --exclude='bin' \
    --exclude='dist' \
    --exclude='build' \
    --exclude='.terraform' \
    --exclude='tmp' \
    --exclude='*.db' \
    --exclude='__pycache__'
    
  echo "    Clean context created."
}

build_agent() {
  local CONTEXT_DIR="/tmp/mcp-agent-build-ctx-$$/agent"
  create_clean_context "$CONTEXT_DIR"
  
  echo "    [Agent] Building..."
  # Build from the CLEAN context root (simulating the 'ai-work' parent dir)
  (
    cd "$CONTEXT_DIR" || exit 1
    az acr build -r "$ACR_NAME" \
      --platform linux/amd64 \
      -t "mcp-agent:$TAG" \
      -t "mcp-agent:latest" \
      -f mcp-agent-builder-go/agent_go/Dockerfile.localdeps \
      . > /dev/null
  )
    
  echo "    [Agent] Build complete."
  rm -rf "$CONTEXT_DIR"
}

build_workspace() {
  local CONTEXT_DIR="/tmp/mcp-agent-build-ctx-$$/workspace"
  create_clean_context "$CONTEXT_DIR"

  echo "    [Workspace] Building..."
  (
    cd "$CONTEXT_DIR/mcp-agent-builder-go/workspace" || exit 1
    az acr build -r "$ACR_NAME" \
      --platform linux/amd64 \
      -t "workspace-api:$TAG" \
      -t "workspace-api:latest" \
      -f Dockerfile \
      . > /dev/null
  )
    
  echo "    [Workspace] Build complete."
  rm -rf "$CONTEXT_DIR"
}

build_frontend() {
  local CONTEXT_DIR="/tmp/mcp-agent-build-ctx-$$/frontend"
  create_clean_context "$CONTEXT_DIR"

  echo "    [Frontend] Building..."
  local AGENT_URL="$(terraform output -raw agent_fqdn 2>/dev/null)"
  local WORKSPACE_URL="$(terraform output -raw workspace_api_fqdn 2>/dev/null)"
  
  (
    cd "$CONTEXT_DIR/mcp-agent-builder-go/frontend" || exit 1
    az acr build -r "$ACR_NAME" \
      --platform linux/amd64 \
      -t "mcp-agent-frontend:$TAG" \
      -t "mcp-agent-frontend:latest" \
      -f Dockerfile.prod \
      --build-arg VITE_API_BASE_URL="$AGENT_URL" \
      --build-arg VITE_WORKSPACE_API_URL="$WORKSPACE_URL" \
      . > /dev/null
  )
    
  echo "    [Frontend] Build complete."
  rm -rf "$CONTEXT_DIR"
}

# ------------------------------------------------------------------
# Git Safety Checks (Temporarily disabled by user request)
# ------------------------------------------------------------------

echo "==> Git safety checks: SKIPPED (user request)"

# CURRENT_BRANCH=$(git rev-parse --abbrev-ref HEAD)
# if [ "$CURRENT_BRANCH" != "main" ]; then
#   echo "Error: You are on branch '$CURRENT_BRANCH'. You must be on 'main' to deploy."
#   echo "Run: git checkout main"
#   exit 1
# fi

# if ! git diff-index --quiet HEAD --; then
#   echo "Error: You have uncommitted changes. Please commit or stash them before deploying."
#   echo "This ensures you only deploy code that is tracked in the repository."
#   exit 1
# fi

# echo "    Checking sync status with origin..."
# git fetch origin main > /dev/null
# LOCAL_HASH=$(git rev-parse HEAD)
# REMOTE_HASH=$(git rev-parse origin/main)

# if [ "$LOCAL_HASH" != "$REMOTE_HASH" ]; then
#   echo "Error: Your local 'main' is not in sync with 'origin/main'."
#   echo "Please push or pull your changes first."
#   exit 1
# fi

# ------------------------------------------------------------------
# Execution Logic
# ------------------------------------------------------------------

pids=()

case "$TARGET" in
  agent)
    build_agent & pids+=($!)
    ;;
  workspace)
    build_workspace & pids+=($!)
    ;;
  frontend)
    build_frontend & pids+=($!)
    ;;
  all)
    build_agent & pids+=($!)
    build_workspace & pids+=($!)
    build_frontend & pids+=($!)
    ;;
esac

echo "==> Builds running in parallel. Waiting..."
for pid in "${pids[@]}"; do
  wait "$pid"
done
echo "==> All builds finished successfully."

# ------------------------------------------------------------------
# Update Container Apps (Zero-Downtime)
# ------------------------------------------------------------------

update_app() {
  local app_name="$1"
  local image="$2"
  echo "    [$app_name] Updating to $image..."
  az containerapp update \
    --name "$app_name" \
    --resource-group "$RG_NAME" \
    --image "$ACR_NAME.azurecr.io/$image" \
    --output none
  echo "    [$app_name] Update complete."
}

echo "==> Updating Container Apps..."

# Reset PIDs for update phase
pids=()

if [[ "$TARGET" == "agent" || "$TARGET" == "all" ]]; then
  update_app "${PROJECT_NAME}-agent" "mcp-agent:$TAG" & pids+=($!)
fi

if [[ "$TARGET" == "workspace" || "$TARGET" == "all" ]]; then
  update_app "${PROJECT_NAME}-workspace-api" "workspace-api:$TAG" & pids+=($!)
fi

if [[ "$TARGET" == "frontend" || "$TARGET" == "all" ]]; then
  update_app "${PROJECT_NAME}-frontend" "mcp-agent-frontend:$TAG" & pids+=($!)
fi

for pid in "${pids[@]}"; do
  wait "$pid"
done

echo ""
echo "SUCCESS! Deployed version: $TAG"
echo "Frontend: $(terraform output -raw frontend_fqdn 2>/dev/null)"
