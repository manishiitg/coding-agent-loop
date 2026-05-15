#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Get script directory and project root (script lives in deploy/k8s/scripts/)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
K8S_DIR="$PROJECT_ROOT/deploy/k8s"
NAMESPACE="prod-mcpagent"

# Configuration
ECR_REGISTRY="414085459896.dkr.ecr.ap-south-1.amazonaws.com"
IMAGE_TAG="${IMAGE_TAG:-latest}"
# Stable builder name (no ${RANDOM}). Reusing one buildkit deployment across runs
# prevents the leak-on-failure pattern where every aborted build orphaned a separate
# buildkit pod in prod-mcpagent. Override with BUILDER_NAME env if you need parallel
# builds.
BUILDER_NAME="${BUILDER_NAME:-k8s-mcpagent-builder}"
# AWS EKS context to use (script will switch to this before deploy). Override with KUBE_CONTEXT env if needed.
KUBE_CONTEXT="${KUBE_CONTEXT:-arn:aws:eks:ap-south-1:414085459896:cluster/app-services}"

# Parse arguments
DEPLOY_SUFFIX="${DEPLOY_SUFFIX:--cs}"
BUILD=false
SERVICES=()
SYNC_WORKFLOWS=()
SYNC_INCLUDE_RUNS=false
SYNC_PORT="${SYNC_PORT:-18080}"
NEXT_IS_SUFFIX=false
NEXT_IS_SYNC_WORKFLOW=false

for arg in "$@"; do
    if [ "$NEXT_IS_SUFFIX" = true ]; then
        DEPLOY_SUFFIX="$arg"
        NEXT_IS_SUFFIX=false
        continue
    fi
    if [ "$NEXT_IS_SYNC_WORKFLOW" = true ]; then
        SYNC_WORKFLOWS+=("$arg")
        NEXT_IS_SYNC_WORKFLOW=false
        continue
    fi
    case "$arg" in
        --build)
            BUILD=true
            ;;
        --suffix)
            NEXT_IS_SUFFIX=true
            ;;
        --suffix=*)
            DEPLOY_SUFFIX="${arg#--suffix=}"
            ;;
        --no-suffix)
            DEPLOY_SUFFIX=""
            ;;
        --sync-workflow)
            NEXT_IS_SYNC_WORKFLOW=true
            ;;
        --sync-workflow=*)
            SYNC_WORKFLOWS+=("${arg#--sync-workflow=}")
            ;;
        --sync-workflow-include-runs)
            SYNC_INCLUDE_RUNS=true
            ;;
        *)
            SERVICES+=("$arg")
            ;;
    esac
done

if [ "$NEXT_IS_SUFFIX" = true ]; then
    echo -e "${RED}Error: --suffix requires a value (e.g. --suffix -cs)${NC}" >&2
    exit 1
fi

if [ "$NEXT_IS_SYNC_WORKFLOW" = true ]; then
    echo -e "${RED}Error: --sync-workflow requires a workflow name (e.g. --sync-workflow citymall-infra)${NC}" >&2
    exit 1
fi

# Default: deploy all services if none specified
if [ ${#SERVICES[@]} -eq 0 ]; then
    SERVICES=("all")
fi

# Function to get image name for a service
get_image_for_service() {
    local service=$1
    case "$service" in
        agent)
            echo "mcpagent-agent"
            ;;
        frontend)
            echo "mcpagent-frontend"
            ;;
        workspace-api)
            echo "mcpagent-workspace-api"
            ;;
        *)
            echo ""
            ;;
    esac
}

# Function to get Dockerfile path for an image (relative to build context)
get_dockerfile_for_image() {
    local image=$1
    case "$image" in
        mcpagent-agent)
            echo "agent_go/Dockerfile"  # Dockerfile built from project root
            ;;
        mcpagent-frontend)
            echo "Dockerfile.prod"
            ;;
        mcpagent-workspace-api)
            echo "Dockerfile"
            ;;
        *)
            echo ""
            ;;
    esac
}

# Function to get build context for an image
get_context_for_image() {
    local image=$1
    case "$image" in
        mcpagent-agent)
            echo "."  # Build from project root to access workspace module
            ;;
        mcpagent-frontend)
            echo "frontend"
            ;;
        mcpagent-workspace-api)
            echo "workspace"
            ;;
        *)
            echo ""
            ;;
    esac
}

# Function to get build args for an image
get_build_args_for_image() {
    local image=$1
    case "$image" in
        mcpagent-frontend)
            echo "--build-arg VITE_API_BASE_URL=https://analytics-agent.citymall.live --build-arg VITE_WORKSPACE_API_URL=https://analytics-agent.citymall.live/workspace"
            ;;
        mcpagent-agent)
            # Install @google/gemini-cli in the k8s agent image so the `gemini-cli` LLM
            # provider adapter works on prod. Other deploy targets skip this to stay lean.
            echo "--build-arg INSTALL_GEMINI_CLI=true"
            ;;
        *)
            echo ""
            ;;
    esac
}

echo -e "${GREEN}=== MCP Agent Kubernetes Deployment ===${NC}\n"
if [ -n "$DEPLOY_SUFFIX" ]; then
    echo -e "${YELLOW}Deployment name suffix: ${DEPLOY_SUFFIX} (targeting e.g. mcpagent-agent${DEPLOY_SUFFIX})${NC}\n"
fi

# Check prerequisites
if ! command -v kubectl &> /dev/null; then
    echo -e "${RED}Error: kubectl is not installed or not in PATH${NC}"
    exit 1
fi

# Ensure we're on the AWS EKS context (default: app-services)
echo -e "${BLUE}Switching kubectl context to: $KUBE_CONTEXT${NC}"
if ! kubectl config use-context "$KUBE_CONTEXT" 2>/dev/null; then
    echo -e "${RED}Error: failed to switch to context '$KUBE_CONTEXT'. Ensure the AWS context (e.g. app-services) exists (e.g. aws eks update-kubeconfig --name <cluster>).${NC}" >&2
    exit 1
fi
echo -e "${GREEN}✓ Using context: $KUBE_CONTEXT${NC}\n"

if [ "$BUILD" = true ]; then
    if ! command -v docker &> /dev/null; then
        echo -e "${RED}Error: docker is not installed or not in PATH${NC}"
        exit 1
    fi
    if ! command -v aws &> /dev/null; then
        echo -e "${RED}Error: aws CLI is not installed or not in PATH${NC}"
        exit 1
    fi
fi


# Function to build and push Docker image
build_and_push_image() {
    local image_name=$1
    local dockerfile_path=$2
    local build_context=$3
    local build_args=$4
    local full_image="${ECR_REGISTRY}/${image_name}:${IMAGE_TAG}"

    echo -e "${BLUE}Building ${image_name}...${NC}"

    cd "$PROJECT_ROOT/$build_context"

    # Use full dockerfile path for project root context, basename for subdirectory contexts
    local dockerfile_name
    if [ "$build_context" = "." ]; then
        dockerfile_name="$dockerfile_path"
    else
        dockerfile_name=$(basename "$dockerfile_path")
    fi

    # Login to ECR first (needed for buildx push)
    echo -e "${BLUE}Logging into ECR...${NC}"
    if ! aws ecr get-login-password --region ap-south-1 | \
        docker login --username AWS --password-stdin "$ECR_REGISTRY" 2>/dev/null; then
        echo -e "${RED}✗ ECR login failed. Make sure AWS credentials are configured.${NC}"
        return 1
    fi

    # Ensure buildx builder exists (kubernetes driver offloads builds to cluster)
    if ! docker buildx inspect "$BUILDER_NAME" &>/dev/null; then
        echo -e "${BLUE}Creating Kubernetes buildx builder ($BUILDER_NAME)...${NC}"
        docker buildx create --name "$BUILDER_NAME" --driver kubernetes \
            --driver-opt namespace=$NAMESPACE \
            --use --bootstrap 2>/dev/null || true
    else
        docker buildx use "$BUILDER_NAME" 2>/dev/null || true
    fi

    # Build and push using buildx with linux/amd64 platform (for Kubernetes nodes)
    echo -e "${BLUE}Building for linux/amd64 platform...${NC}"
    local build_cmd="docker buildx build --platform linux/amd64 -t \"$full_image\" -f \"$dockerfile_name\" --push"
    if [ -n "$build_args" ]; then
        build_cmd="$build_cmd $build_args"
    fi
    build_cmd="$build_cmd ."

    if ! eval $build_cmd; then
        echo -e "${RED}✗ Docker build failed for ${image_name}${NC}"
        return 1
    fi

    echo -e "${GREEN}✓ ${image_name} built and pushed to ECR (linux/amd64)${NC}\n"
}

# Function to get deployment name from service
get_deployment_name() {
    local service=$1
    echo "mcpagent-${service}${DEPLOY_SUFFIX}"
}

# Build images if --build flag is set
if [ "$BUILD" = true ]; then
    echo -e "${GREEN}[0] Building Docker images...${NC}\n"

    # Collect unique images needed
    images_to_build=""

    if [ "${SERVICES[0]}" = "all" ]; then
        # Build all images
        images_to_build="mcpagent-agent mcpagent-frontend mcpagent-workspace-api"
    else
        # Build only images for specified services
        for service in "${SERVICES[@]}"; do
            image=$(get_image_for_service "$service")
            if [ -n "$image" ] && [[ ! " $images_to_build " =~ " $image " ]]; then
                images_to_build="$images_to_build $image"
            fi
        done
        # Remove leading space
        images_to_build=$(echo "$images_to_build" | sed 's/^ //')
    fi

    # Build each image
    for image_name in $images_to_build; do
        dockerfile=$(get_dockerfile_for_image "$image_name")
        context=$(get_context_for_image "$image_name")
        build_args=$(get_build_args_for_image "$image_name")
        if [ -n "$dockerfile" ] && [ -n "$context" ]; then
            build_and_push_image "$image_name" "$dockerfile" "$context" "$build_args"
        fi
    done
fi

# Check if namespace exists
if ! kubectl get namespace "$NAMESPACE" &> /dev/null; then
    echo -e "${YELLOW}Namespace $NAMESPACE does not exist. Creating...${NC}"
    kubectl create namespace "$NAMESPACE"
fi

# Function to extract value from .env file (handles spaces and quotes)
extract_env_value() {
    local key=$1
    local env_file=$2
    grep "^${key}" "$env_file" | sed 's/^[^=]*[[:space:]]*=[[:space:]]*//' | sed "s/^['\"]//;s/['\"]$//" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//' | head -1
}

# Deploy shared resources first
echo -e "${GREEN}[1] Deploying shared resources...${NC}"
if [ -f "$K8S_DIR/shared/configmap.yaml" ]; then
    kubectl apply -f "$K8S_DIR/shared/configmap.yaml"
    if [ -f "$K8S_DIR/.env" ]; then
        DESKTOP_APP_ONLY_UI_OVERRIDE=$(extract_env_value "DESKTOP_APP_ONLY_UI" "$K8S_DIR/.env")
        if [ "$DESKTOP_APP_ONLY_UI_OVERRIDE" = "true" ] || [ "$DESKTOP_APP_ONLY_UI_OVERRIDE" = "false" ]; then
            kubectl patch configmap mcpagent-shared-config -n "$NAMESPACE" --type merge -p "{\"data\":{\"DESKTOP_APP_ONLY_UI\":\"$DESKTOP_APP_ONLY_UI_OVERRIDE\"}}"
            echo -e "${GREEN}✓ DESKTOP_APP_ONLY_UI set from deploy/k8s/.env: ${DESKTOP_APP_ONLY_UI_OVERRIDE}${NC}"
        fi
    fi
    echo -e "${GREEN}✓ ConfigMap applied${NC}"
fi

# Create/update secret from deploy/k8s/.env
SECRET_NAME="prod-mcpagent-secret"
ENV_FILE="$K8S_DIR/.env"

if [ -f "$ENV_FILE" ]; then
    echo -e "${BLUE}Reading secrets from deploy/k8s/.env for ${SECRET_NAME}...${NC}"

    # Gemini/Vertex Configuration
    GEMINI_API_KEY=$(extract_env_value "GEMINI_API_KEY" "$ENV_FILE")
    VERTEX_PROJECT_ID=$(extract_env_value "VERTEX_PROJECT_ID" "$ENV_FILE")
    VERTEX_LOCATION_ID=$(extract_env_value "VERTEX_LOCATION_ID" "$ENV_FILE")

    # OpenRouter Configuration
    OPENROUTER_API_KEY=$(extract_env_value "OPENROUTER_API_KEY" "$ENV_FILE")

    # Database and Observability
    DATABASE_URL=$(extract_env_value "DATABASE_URL" "$ENV_FILE")
    LANGFUSE_PUBLIC_KEY=$(extract_env_value "LANGFUSE_PUBLIC_KEY" "$ENV_FILE")
    LANGFUSE_SECRET_KEY=$(extract_env_value "LANGFUSE_SECRET_KEY" "$ENV_FILE")
    LANGFUSE_HOST=$(extract_env_value "LANGFUSE_HOST" "$ENV_FILE")

    # GitHub Sync
    GITHUB_TOKEN=$(extract_env_value "GITHUB_TOKEN" "$ENV_FILE")
    GITHUB_REPO=$(extract_env_value "GITHUB_REPO" "$ENV_FILE")

    # Authentication (required for MULTI_USER_MODE=true)
    AUTH_SECRET=$(extract_env_value "AUTH_SECRET" "$ENV_FILE")

    # Global secrets (JSON values passed as env vars)
    GLOBAL_SECRET_GRAFANA=$(extract_env_value "GLOBAL_SECRET_GRAFANA" "$ENV_FILE")
    GLOBAL_SECRET_AWS_KEYS=$(extract_env_value "GLOBAL_SECRET_AWS_KEYS" "$ENV_FILE")
    GLOBAL_SECRET_GITHUB=$(extract_env_value "GLOBAL_SECRET_GITHUB" "$ENV_FILE")
    GLOBAL_SECRET_CLICKHOUSE=$(extract_env_value "GLOBAL_SECRET_CLICKHOUSE" "$ENV_FILE")

    if kubectl get secret "$SECRET_NAME" -n "$NAMESPACE" &> /dev/null; then
        echo -e "${YELLOW}Secret ${SECRET_NAME} exists. Updating...${NC}"
        kubectl delete secret "$SECRET_NAME" -n "$NAMESPACE" 2>/dev/null || true
    else
        echo -e "${YELLOW}Creating secret ${SECRET_NAME}...${NC}"
    fi

    # Build secret from env file to avoid eval/quoting issues with special characters
    SECRET_ENV_FILE=$(mktemp)
    trap "rm -f $SECRET_ENV_FILE" EXIT

    [ -n "$GEMINI_API_KEY" ] && printf '%s=%s\n' "GEMINI_API_KEY" "$GEMINI_API_KEY" >> "$SECRET_ENV_FILE"
    # Vertex/Gemini: LLM provider expects VERTEX_API_KEY or GOOGLE_API_KEY; use GEMINI_API_KEY for both so keys work
    [ -n "$GEMINI_API_KEY" ] && printf '%s=%s\n' "VERTEX_API_KEY" "$GEMINI_API_KEY" >> "$SECRET_ENV_FILE"
    [ -n "$GEMINI_API_KEY" ] && printf '%s=%s\n' "GOOGLE_API_KEY" "$GEMINI_API_KEY" >> "$SECRET_ENV_FILE"
    [ -n "$VERTEX_PROJECT_ID" ] && printf '%s=%s\n' "VERTEX_PROJECT_ID" "$VERTEX_PROJECT_ID" >> "$SECRET_ENV_FILE"
    [ -n "$VERTEX_LOCATION_ID" ] && printf '%s=%s\n' "VERTEX_LOCATION_ID" "$VERTEX_LOCATION_ID" >> "$SECRET_ENV_FILE"
    [ -n "$OPENROUTER_API_KEY" ] && printf '%s=%s\n' "OPENROUTER_API_KEY" "$OPENROUTER_API_KEY" >> "$SECRET_ENV_FILE"
    [ -n "$DATABASE_URL" ] && printf '%s=%s\n' "DATABASE_URL" "$DATABASE_URL" >> "$SECRET_ENV_FILE"
    [ -n "$LANGFUSE_PUBLIC_KEY" ] && printf '%s=%s\n' "LANGFUSE_PUBLIC_KEY" "$LANGFUSE_PUBLIC_KEY" >> "$SECRET_ENV_FILE"
    [ -n "$LANGFUSE_SECRET_KEY" ] && printf '%s=%s\n' "LANGFUSE_SECRET_KEY" "$LANGFUSE_SECRET_KEY" >> "$SECRET_ENV_FILE"
    [ -n "$LANGFUSE_HOST" ] && printf '%s=%s\n' "LANGFUSE_HOST" "$LANGFUSE_HOST" >> "$SECRET_ENV_FILE"
    [ -n "$GITHUB_TOKEN" ] && printf '%s=%s\n' "GITHUB_TOKEN" "$GITHUB_TOKEN" >> "$SECRET_ENV_FILE"
    [ -n "$GITHUB_REPO" ] && printf '%s=%s\n' "GITHUB_REPO" "$GITHUB_REPO" >> "$SECRET_ENV_FILE"
    [ -n "$AUTH_SECRET" ] && printf '%s=%s\n' "AUTH_SECRET" "$AUTH_SECRET" >> "$SECRET_ENV_FILE"
    [ -n "$GLOBAL_SECRET_GRAFANA" ] && printf '%s=%s\n' "GLOBAL_SECRET_GRAFANA" "$GLOBAL_SECRET_GRAFANA" >> "$SECRET_ENV_FILE"
    [ -n "$GLOBAL_SECRET_AWS_KEYS" ] && printf '%s=%s\n' "GLOBAL_SECRET_AWS_KEYS" "$GLOBAL_SECRET_AWS_KEYS" >> "$SECRET_ENV_FILE"
    [ -n "$GLOBAL_SECRET_GITHUB" ] && printf '%s=%s\n' "GLOBAL_SECRET_GITHUB" "$GLOBAL_SECRET_GITHUB" >> "$SECRET_ENV_FILE"
    [ -n "$GLOBAL_SECRET_CLICKHOUSE" ] && printf '%s=%s\n' "GLOBAL_SECRET_CLICKHOUSE" "$GLOBAL_SECRET_CLICKHOUSE" >> "$SECRET_ENV_FILE"

    kubectl create secret generic "$SECRET_NAME" -n "$NAMESPACE" --from-env-file="$SECRET_ENV_FILE"
    rm -f "$SECRET_ENV_FILE"
    echo -e "${GREEN}✓ Secret ${SECRET_NAME} created/updated${NC}"
else
    if ! kubectl get secret "$SECRET_NAME" -n "$NAMESPACE" &> /dev/null; then
        echo -e "${YELLOW}No deploy/k8s/.env and secret ${SECRET_NAME} not found. Create it manually:${NC}"
        echo -e "  kubectl create secret generic ${SECRET_NAME} \\"
        echo -e "    --from-literal=GEMINI_API_KEY=\"<key>\" \\"
        echo -e "    --from-literal=OPENROUTER_API_KEY=\"<key>\" \\"
        echo -e "    --from-literal=DATABASE_URL=\"<url>\" \\"
        echo -e "    -n $NAMESPACE"
    else
        echo -e "${GREEN}✓ Using existing secret ${SECRET_NAME} (no .env to update)${NC}"
    fi
fi

# Set when MCP config is updated so we restart agent to pick it up
MCP_CONFIG_UPDATED=false

# Function to deploy a service
deploy_service() {
    local service=$1
    local service_dir="$K8S_DIR/$service"

    if [ ! -d "$service_dir" ]; then
        echo -e "${RED}✗ Service directory not found: $service_dir${NC}"
        return 1
    fi

    echo -e "${BLUE}Deploying $service...${NC}"

    # Agent: sync MCP ConfigMap from deploy/k8s/agent/mcp_config.json if present
    if [ "$service" = "agent" ] && [ -f "$K8S_DIR/agent/mcp_config.json" ]; then
        echo -e "${BLUE}Updating MCP config from agent/mcp_config.json...${NC}"
        if kubectl create configmap mcpagent-agent-config \
            --from-file=mcp_servers.json="$K8S_DIR/agent/mcp_config.json" \
            -n "$NAMESPACE" \
            --dry-run=client -o yaml | kubectl apply -f - 2>/dev/null; then
            echo -e "${GREEN}✓ MCP ConfigMap updated${NC}"
            MCP_CONFIG_UPDATED=true
        fi
    fi

    # Apply all YAML files in the service directory
    for yaml in "$service_dir"/*.yaml; do
        if [ -f "$yaml" ]; then
            kubectl apply -f "$yaml"
        fi
    done

    # Ensure deployment image tag matches IMAGE_TAG (prevents hardcoded tag drift in YAML)
    local image_name
    image_name=$(get_image_for_service "$service")
    if [ -n "$image_name" ]; then
        local deployment_name
        deployment_name=$(get_deployment_name "$service")
        local full_image="${ECR_REGISTRY}/${image_name}:${IMAGE_TAG}"
        kubectl set image "deployment/${deployment_name}" "${service}=${full_image}" -n "$NAMESPACE" 2>/dev/null || true
    fi

    echo -e "${GREEN}✓ $service deployed${NC}\n"
}

# Deploy services
if [ "${SERVICES[0]}" = "all" ]; then
    echo -e "${GREEN}[2] Deploying all services...${NC}\n"
    deploy_service "workspace-api"
    deploy_service "agent"
    deploy_service "frontend"
    DEPLOYED_SERVICES=("workspace-api" "agent" "frontend")
else
    echo -e "${GREEN}[2] Deploying specified services: ${SERVICES[*]}${NC}\n"
    DEPLOYED_SERVICES=("${SERVICES[@]}")
    for service in "${SERVICES[@]}"; do
        deploy_service "$service"
    done
fi

# Restart deployments to pull new images if --build was used, or if MCP config was updated
if [ "$BUILD" = true ] || [ "$MCP_CONFIG_UPDATED" = true ]; then
    echo -e "\n${BLUE}[3] Restarting deployments to pull new images / pick up config...${NC}\n"
    # When MCP config updated, only restart agent; when BUILD, restart all deployed services
    if [ "$MCP_CONFIG_UPDATED" = true ] && [ "$BUILD" != true ]; then
        DEPLOYED_SERVICES=("agent")
    fi
    for service in "${DEPLOYED_SERVICES[@]}"; do
        deployment_name=$(get_deployment_name "$service")

        echo -e "${BLUE}Rolling out ${deployment_name}...${NC}"
        if kubectl rollout restart deployment "$deployment_name" -n "$NAMESPACE" 2>/dev/null; then
            echo -e "${GREEN}✓ ${deployment_name} restarted${NC}"

            # Wait for rollout to complete
            echo -e "${YELLOW}Waiting for rollout to complete...${NC}"
            if kubectl rollout status deployment "$deployment_name" -n "$NAMESPACE" 2>/dev/null; then
                echo -e "${GREEN}✓ ${deployment_name} rollout complete${NC}\n"
            else
                echo -e "${YELLOW}⚠ ${deployment_name} rollout failed${NC}\n"
            fi
        else
            echo -e "${YELLOW}⚠ Could not restart ${deployment_name} (may not exist)${NC}\n"
        fi
    done
fi

# Sync workflows from local workspace-docs/Workflow/<name>/ to the prod workspace-api PVC.
# Uses the workspace-api /api/workspace/import endpoint via a short-lived port-forward.
# Clean-replace semantics: deletes the remote folder first, then uploads a zip of the local folder.
sync_workflow() {
    local workflow_name=$1
    local src_dir="$PROJECT_ROOT/workspace-docs/Workflow/$workflow_name"
    local svc_name="mcpagent-workspace-api${DEPLOY_SUFFIX}"
    local zip_path="/tmp/sync-workflow-${workflow_name}-$$.zip"

    echo -e "${BLUE}Syncing workflow '${workflow_name}' to ${svc_name}...${NC}"

    if [ ! -d "$src_dir" ]; then
        echo -e "${RED}✗ Local workflow not found: $src_dir${NC}"
        return 1
    fi
    if ! command -v zip &> /dev/null; then
        echo -e "${RED}✗ 'zip' command not found — install it (macOS: preinstalled; Debian: apt install zip)${NC}"
        return 1
    fi
    if ! command -v curl &> /dev/null; then
        echo -e "${RED}✗ 'curl' command not found${NC}"
        return 1
    fi

    # Build the zip. Import extracts entries into workspace_path=Workflow, so entries
    # must be prefixed with the workflow folder name (zip -r <name> from Workflow/ does that).
    local zip_excludes=("*.DS_Store" "*__pycache__*" "*.pyc")
    if [ "$SYNC_INCLUDE_RUNS" != true ]; then
        zip_excludes+=("${workflow_name}/runs/*")
    fi
    rm -f "$zip_path"
    (cd "$PROJECT_ROOT/workspace-docs/Workflow" && zip -rq "$zip_path" "$workflow_name" -x "${zip_excludes[@]}")
    if [ ! -s "$zip_path" ]; then
        echo -e "${RED}✗ Failed to create zip archive at $zip_path${NC}"
        return 1
    fi
    local zip_size
    zip_size=$(du -h "$zip_path" | awk '{print $1}')
    if [ "$SYNC_INCLUDE_RUNS" = true ]; then
        echo -e "${BLUE}  Archive: $zip_size (runs/ included)${NC}"
    else
        echo -e "${BLUE}  Archive: $zip_size (runs/ excluded — use --sync-workflow-include-runs to include)${NC}"
    fi

    # Start port-forward in background; ensure it's cleaned up on any exit path
    local pf_log
    pf_log=$(mktemp)
    kubectl port-forward -n "$NAMESPACE" "svc/${svc_name}" "${SYNC_PORT}:80" >"$pf_log" 2>&1 &
    local pf_pid=$!
    # shellcheck disable=SC2064
    trap "kill $pf_pid 2>/dev/null; rm -f '$zip_path' '$pf_log'" RETURN

    # Wait up to 10s for the port-forward to become reachable
    local ready=false
    for _ in 1 2 3 4 5 6 7 8 9 10; do
        if curl -fsS "http://localhost:${SYNC_PORT}/health" >/dev/null 2>&1; then
            ready=true
            break
        fi
        sleep 1
    done
    if [ "$ready" != true ]; then
        echo -e "${RED}✗ Port-forward to ${svc_name} did not become ready. Log:${NC}"
        cat "$pf_log"
        return 1
    fi

    # Clean-replace: delete existing remote folder (404 is fine — first-time sync)
    local del_http
    del_http=$(curl -s -o /tmp/sync-del-$$.json -w '%{http_code}' -X DELETE \
        "http://localhost:${SYNC_PORT}/api/folders/Workflow/${workflow_name}?confirm=true&commit_message=sync-workflow%20replace%20${workflow_name}")
    rm -f /tmp/sync-del-$$.json
    if [ "$del_http" = "200" ]; then
        echo -e "${GREEN}  ✓ Removed existing remote Workflow/${workflow_name}${NC}"
    elif [ "$del_http" = "404" ]; then
        echo -e "${YELLOW}  ℹ No existing remote Workflow/${workflow_name} (first-time sync)${NC}"
    else
        echo -e "${RED}  ✗ Delete failed (HTTP $del_http) — aborting sync${NC}"
        return 1
    fi

    # Upload the zip
    local resp
    resp=$(curl -s -X POST "http://localhost:${SYNC_PORT}/api/workspace/import" \
        -F "workspace_path=Workflow" \
        -F "overwrite=true" \
        -F "file=@${zip_path}")
    local ok files
    ok=$(echo "$resp" | python3 -c "import sys,json;d=json.load(sys.stdin);print(d.get('success'))" 2>/dev/null || echo "")
    files=$(echo "$resp" | python3 -c "import sys,json;d=json.load(sys.stdin);print(d.get('data',{}).get('files_extracted',''))" 2>/dev/null || echo "")
    if [ "$ok" != "True" ]; then
        echo -e "${RED}  ✗ Import failed. Response: $resp${NC}"
        return 1
    fi
    echo -e "${GREEN}  ✓ Imported ${files} files into Workflow/${workflow_name}${NC}\n"
    return 0
}

# Run any requested workflow syncs after deploy + any BUILD rollout have settled
if [ ${#SYNC_WORKFLOWS[@]} -gt 0 ]; then
    echo -e "${GREEN}[4] Syncing workflows from local workspace-docs/...${NC}\n"
    sync_failures=0
    for wf in "${SYNC_WORKFLOWS[@]}"; do
        if ! sync_workflow "$wf"; then
            sync_failures=$((sync_failures + 1))
        fi
    done
    if [ "$sync_failures" -gt 0 ]; then
        echo -e "${YELLOW}⚠ ${sync_failures} workflow sync(s) failed${NC}\n"
    fi
fi

# Clean up stale pods (Evicted, Failed, ContainerStatusUnknown) left behind by dead nodes
echo -e "${BLUE}Cleaning up stale pods...${NC}"
kubectl delete pods --field-selector=status.phase=Failed -n "$NAMESPACE" 2>/dev/null && echo -e "${GREEN}✓ Cleaned failed/evicted pods${NC}" || true
kubectl delete pods --field-selector=status.phase=Succeeded -n "$NAMESPACE" 2>/dev/null && echo -e "${GREEN}✓ Cleaned completed pods${NC}" || true
# Clean ContainerStatusUnknown pods (orphaned by dead nodes)
for pod in $(kubectl get pods -n "$NAMESPACE" --no-headers 2>/dev/null | grep -i "unknown\|ContainerStatusUnknown" | awk '{print $1}'); do
    kubectl delete pod "$pod" -n "$NAMESPACE" --grace-period=0 --force 2>/dev/null && echo -e "${GREEN}✓ Force-deleted orphan pod $pod${NC}" || true
done

# Show status
echo -e "${GREEN}=== Deployment Status ===${NC}"
kubectl get deployments -n "$NAMESPACE" 2>/dev/null || echo "No deployments found"
echo ""
kubectl get pods -n "$NAMESPACE" 2>/dev/null || echo "No pods found"

echo ""
echo -e "${GREEN}=== Deployment Complete ===${NC}"
if [ "$BUILD" = true ]; then
    echo -e "Images built and pushed with tag: ${YELLOW}${IMAGE_TAG}${NC}"
    echo -e "${BLUE}Reusing buildx builder '${BUILDER_NAME}' across runs (no auto-cleanup).${NC}"
    echo -e "${BLUE}To remove manually: ${YELLOW}docker buildx rm ${BUILDER_NAME}${NC}"
fi
echo -e "View logs: ${YELLOW}kubectl logs -f deployment/<service-name> -n $NAMESPACE${NC}"
echo -e "Check status: ${YELLOW}kubectl get pods -n $NAMESPACE${NC}"
echo -e "Port forward agent: ${YELLOW}kubectl port-forward svc/mcpagent-agent-cs 8000:80 -n $NAMESPACE${NC}"
