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
shift # Shift arguments so we can check for --local
USE_LOCAL_BUILD=false

if [[ "$1" == "--local" ]] || [[ "$TARGET" == "--local" ]]; then
  USE_LOCAL_BUILD=true
  # If first arg was --local, default target to all
  if [[ "$TARGET" == "--local" ]]; then
    TARGET="all"
  fi
  echo "==> Mode: LOCAL BUILD (docker build + push)"
else
  echo "==> Mode: REMOTE BUILD (az acr build)"
fi

TAG="v$(date +%Y%m%d-%H%M%S)"
# Shared base image for agent and workspace (build rarely with: ./deploy.sh base)
BASE_IMAGE="$ACR_NAME.azurecr.io/mcp-agent-base:latest"
BASE_IMAGE_ARG="--build-arg BASE_IMAGE=$BASE_IMAGE"

echo "==> Starting deployment: $TAG"
echo "==> ACR: $ACR_NAME"

if [ "$USE_LOCAL_BUILD" = "true" ]; then
  echo "    Logging into ACR..."
  az acr login -n "$ACR_NAME"
fi

# ------------------------------------------------------------------
# Build Functions (Run in background)
# ------------------------------------------------------------------

# Generic build function to handle both local and remote builds
run_build() {
  local image_name="$1"
  local dockerfile="$2"
  local build_args="$3" # Optional build args string
  local context_dir="$4"

  if [ "$USE_LOCAL_BUILD" = "true" ]; then
    # Local Build (tee so you see docker output in terminal and in log file)
    local full_image="$ACR_NAME.azurecr.io/$image_name"
    local logfile="$SCRIPT_DIR/build_${image_name}.log"
    # shellcheck disable=SC2086
    docker build --platform linux/amd64 \
      -t "$full_image:$TAG" \
      -t "$full_image:latest" \
      -f "$dockerfile" \
      $build_args \
      . 2>&1 | tee "$logfile"

    docker push "$full_image:$TAG" 2>&1 | tee -a "$logfile"
    docker push "$full_image:latest" 2>&1 | tee -a "$logfile"
  else
    # Remote Build (Azure ACR) - tee so you see build output in terminal and in log file
    local logfile="$SCRIPT_DIR/build_${image_name}.log"
    # shellcheck disable=SC2086
    az acr build -r "$ACR_NAME" \
      --platform linux/amd64 \
      -t "$image_name:$TAG" \
      -t "$image_name:latest" \
      -f "$dockerfile" \
      $build_args \
      . 2>&1 | tee "$logfile"
  fi
}

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

  if [ -f "$REPO_ROOT/.dockerignore" ]; then
    cp "$REPO_ROOT/.dockerignore" "$TEMP_DIR/.dockerignore"
    echo "    Copied .dockerignore to build context root."
  fi

  # Aggressively prune known heavy folders that are not needed
  echo "    Pruning unnecessary heavy folders..."
  rm -rf "$TEMP_DIR/mcpagent/examples"
  rm -rf "$TEMP_DIR/mcpagent/sdk-node"
  rm -rf "$TEMP_DIR/mcpagent/bin"
  rm -rf "$TEMP_DIR/multi-llm-provider-go/bin"
  rm -rf "$TEMP_DIR/multi-llm-provider-go/examples"
  rm -rf "$TEMP_DIR/mcp-agent-builder-go/frontend/node_modules"
  rm -rf "$TEMP_DIR/mcp-agent-builder-go/frontend/dist"
    
  echo "    Clean context created."
}

build_agent() {
  local CONTEXT_DIR="/tmp/mcp-agent-build-ctx-$$/agent"
  
  if [ "$USE_LOCAL_BUILD" = "true" ]; then
     # For local build, we can just use the selective copy approach we established
     # It's faster than rsyncing everything for local docker context too
     echo "    Creating selective build context in $CONTEXT_DIR..."
     mkdir -p "$CONTEXT_DIR/mcp-agent-builder-go"
     cp -r "$REPO_ROOT/agent_go" "$CONTEXT_DIR/mcp-agent-builder-go/"
     cp -r "$REPO_ROOT/workspace" "$CONTEXT_DIR/mcp-agent-builder-go/"
     # Local multi-llm-provider-go (streaming fix) - must be inside context for Dockerfile
     cp -r "$PARENT_DIR/multi-llm-provider-go" "$CONTEXT_DIR/mcp-agent-builder-go/multi-llm-provider-go"
     # Local mcpagent - must be inside context for Dockerfile (same as multi-llm-provider-go)
     cp -r "$PARENT_DIR/mcpagent" "$CONTEXT_DIR/mcp-agent-builder-go/mcpagent"
     # Copy sibling dependencies for local build
     cp -r "$PARENT_DIR/mcpagent" "$CONTEXT_DIR/mcpagent"
     cp -r "$PARENT_DIR/multi-llm-provider-go" "$CONTEXT_DIR/multi-llm-provider-go"
  else 
     # Use the same selective context for remote build to be consistent
     echo "    Creating selective build context in $CONTEXT_DIR..."
     mkdir -p "$CONTEXT_DIR/mcp-agent-builder-go"
     cp -r "$REPO_ROOT/agent_go" "$CONTEXT_DIR/mcp-agent-builder-go/"
     cp -r "$REPO_ROOT/workspace" "$CONTEXT_DIR/mcp-agent-builder-go/"
     # Local multi-llm-provider-go (streaming fix) - must be inside context for Dockerfile
     cp -r "$PARENT_DIR/multi-llm-provider-go" "$CONTEXT_DIR/mcp-agent-builder-go/multi-llm-provider-go"
     # Local mcpagent - must be inside context for Dockerfile (same as multi-llm-provider-go)
     cp -r "$PARENT_DIR/mcpagent" "$CONTEXT_DIR/mcp-agent-builder-go/mcpagent"
     # Copy sibling dependencies for remote build
     cp -r "$PARENT_DIR/mcpagent" "$CONTEXT_DIR/mcpagent"
     cp -r "$PARENT_DIR/multi-llm-provider-go" "$CONTEXT_DIR/multi-llm-provider-go"
  fi

  # MCP config: use deploy/azure/mcp_config.json if present (like K8s deploy/k8s/agent/mcp_config.json); else use repo default
  if [ -f "$SCRIPT_DIR/mcp_config.json" ]; then
    cp "$SCRIPT_DIR/mcp_config.json" "$CONTEXT_DIR/mcp-agent-builder-go/agent_go/configs/mcp_servers_clean_user.json"
  else
    cp "$REPO_ROOT/agent_go/configs/mcp_servers_clean_user.json" "$CONTEXT_DIR/mcp-agent-builder-go/agent_go/configs/mcp_servers_clean_user.json"
  fi

  echo "    [Agent] Building..."
  (
    cd "$CONTEXT_DIR/mcp-agent-builder-go" || exit 1
    run_build "mcp-agent" "$SCRIPT_DIR/Dockerfile.agent" "$BASE_IMAGE_ARG" "$CONTEXT_DIR/mcp-agent-builder-go"
  )
    
  echo "    [Agent] Build complete."
  rm -rf "$CONTEXT_DIR"
}

build_workspace() {
  local CONTEXT_DIR="/tmp/mcp-agent-build-ctx-$$/workspace"
  create_clean_context "$CONTEXT_DIR"

  echo "    [Workspace] Building..."
  (
    cd "$CONTEXT_DIR/mcp-agent-builder-go" || exit 1
    run_build "workspace-api" "$SCRIPT_DIR/Dockerfile.workspace" "$BASE_IMAGE_ARG" "$CONTEXT_DIR/mcp-agent-builder-go"
  )
    
  echo "    [Workspace] Build complete."
  rm -rf "$CONTEXT_DIR"
}

# Build and push shared base image (agent + workspace). Run once or when base deps change.
build_base() {
  echo "    [Base] Building and pushing shared base (agent + workspace)..."
  (
    cd "$SCRIPT_DIR" || exit 1
    local logfile="$SCRIPT_DIR/build_base.log"
    if [ "$USE_LOCAL_BUILD" = "true" ]; then
      docker build --platform linux/amd64 -f "$SCRIPT_DIR/Dockerfile.base" -t "$BASE_IMAGE" . 2>&1 | tee "$logfile"
      docker push "$BASE_IMAGE" 2>&1 | tee -a "$logfile"
    else
      az acr build -r "$ACR_NAME" --platform linux/amd64 -f "$SCRIPT_DIR/Dockerfile.base" -t "mcp-agent-base:latest" . 2>&1 | tee "$logfile"
    fi
  )
  echo "    [Base] Done. Use ./deploy.sh agent|workspace|all [--local] to build app images."
}

build_frontend() {
  local CONTEXT_DIR="/tmp/mcp-agent-build-ctx-$$/frontend"
  create_clean_context "$CONTEXT_DIR"

  echo "    [Frontend] Building..."
  local AGENT_URL="$(terraform output -raw agent_fqdn 2>/dev/null)"
  local WORKSPACE_URL="$(terraform output -raw workspace_api_fqdn 2>/dev/null)"
  
  (
    cd "$CONTEXT_DIR/mcp-agent-builder-go/frontend" || exit 1
    run_build "mcp-agent-frontend" "Dockerfile.prod" "--build-arg VITE_API_BASE_URL=$AGENT_URL --build-arg VITE_WORKSPACE_API_URL=$WORKSPACE_URL" "$CONTEXT_DIR/mcp-agent-builder-go/frontend"
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
  base)
    build_base
    exit 0
    ;;
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
  *)
    echo "Unknown target: $TARGET. Use: agent | workspace | frontend | all | base"
    exit 1
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
