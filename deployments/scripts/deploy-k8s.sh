#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Get script directory and project root
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
K8S_DIR="$PROJECT_ROOT/deployments/k8s"
NAMESPACE="prod-mcpagent"

# Configuration
ECR_REGISTRY="414085459896.dkr.ecr.ap-south-1.amazonaws.com"
IMAGE_TAG="${IMAGE_TAG:-latest}"

# Parse arguments
DEPLOY_SUFFIX="${DEPLOY_SUFFIX:--cs}"
BUILD=false
SERVICES=()
NEXT_IS_SUFFIX=false

for arg in "$@"; do
    if [ "$NEXT_IS_SUFFIX" = true ]; then
        DEPLOY_SUFFIX="$arg"
        NEXT_IS_SUFFIX=false
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
        *)
            SERVICES+=("$arg")
            ;;
    esac
done

if [ "$NEXT_IS_SUFFIX" = true ]; then
    echo -e "${RED}Error: --suffix requires a value (e.g. --suffix -cs)${NC}" >&2
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

    # Ensure buildx builder exists
    if ! docker buildx inspect multiarch-builder &>/dev/null; then
        echo -e "${BLUE}Creating buildx builder...${NC}"
        docker buildx create --name multiarch-builder --use --bootstrap 2>/dev/null || true
    else
        docker buildx use multiarch-builder 2>/dev/null || true
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

# Deploy shared resources first
echo -e "${GREEN}[1] Deploying shared resources...${NC}"
if [ -f "$K8S_DIR/shared/configmap.yaml" ]; then
    kubectl apply -f "$K8S_DIR/shared/configmap.yaml"
    echo -e "${GREEN}✓ ConfigMap applied${NC}"
fi

# Function to extract value from .env file (handles spaces and quotes)
extract_env_value() {
    local key=$1
    local env_file=$2
    grep "^${key}" "$env_file" | sed 's/^[^=]*[[:space:]]*=[[:space:]]*//' | sed "s/^['\"]//;s/['\"]$//" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//' | head -1
}

# Create/update secret from deployments/k8s/.env
SECRET_NAME="prod-mcpagent-secret"
ENV_FILE="$K8S_DIR/.env"

if [ -f "$ENV_FILE" ]; then
    echo -e "${BLUE}Reading secrets from deployments/k8s/.env for ${SECRET_NAME}...${NC}"

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

    if kubectl get secret "$SECRET_NAME" -n "$NAMESPACE" &> /dev/null; then
        echo -e "${YELLOW}Secret ${SECRET_NAME} exists. Updating...${NC}"
        kubectl delete secret "$SECRET_NAME" -n "$NAMESPACE" 2>/dev/null || true
    else
        echo -e "${YELLOW}Creating secret ${SECRET_NAME}...${NC}"
    fi

    # Build kubectl create secret command with available keys
    SECRET_CMD="kubectl create secret generic \"$SECRET_NAME\" -n \"$NAMESPACE\""

    [ -n "$GEMINI_API_KEY" ] && SECRET_CMD="$SECRET_CMD --from-literal=GEMINI_API_KEY=\"$GEMINI_API_KEY\""
    # Vertex/Gemini: LLM provider expects VERTEX_API_KEY or GOOGLE_API_KEY; use GEMINI_API_KEY for both so keys work
    [ -n "$GEMINI_API_KEY" ] && SECRET_CMD="$SECRET_CMD --from-literal=VERTEX_API_KEY=\"$GEMINI_API_KEY\""
    [ -n "$GEMINI_API_KEY" ] && SECRET_CMD="$SECRET_CMD --from-literal=GOOGLE_API_KEY=\"$GEMINI_API_KEY\""
    [ -n "$VERTEX_PROJECT_ID" ] && SECRET_CMD="$SECRET_CMD --from-literal=VERTEX_PROJECT_ID=\"$VERTEX_PROJECT_ID\""
    [ -n "$VERTEX_LOCATION_ID" ] && SECRET_CMD="$SECRET_CMD --from-literal=VERTEX_LOCATION_ID=\"$VERTEX_LOCATION_ID\""
    [ -n "$OPENROUTER_API_KEY" ] && SECRET_CMD="$SECRET_CMD --from-literal=OPENROUTER_API_KEY=\"$OPENROUTER_API_KEY\""
    [ -n "$DATABASE_URL" ] && SECRET_CMD="$SECRET_CMD --from-literal=DATABASE_URL=\"$DATABASE_URL\""
    [ -n "$LANGFUSE_PUBLIC_KEY" ] && SECRET_CMD="$SECRET_CMD --from-literal=LANGFUSE_PUBLIC_KEY=\"$LANGFUSE_PUBLIC_KEY\""
    [ -n "$LANGFUSE_SECRET_KEY" ] && SECRET_CMD="$SECRET_CMD --from-literal=LANGFUSE_SECRET_KEY=\"$LANGFUSE_SECRET_KEY\""
    [ -n "$LANGFUSE_HOST" ] && SECRET_CMD="$SECRET_CMD --from-literal=LANGFUSE_HOST=\"$LANGFUSE_HOST\""
    [ -n "$GITHUB_TOKEN" ] && SECRET_CMD="$SECRET_CMD --from-literal=GITHUB_TOKEN=\"$GITHUB_TOKEN\""
    [ -n "$GITHUB_REPO" ] && SECRET_CMD="$SECRET_CMD --from-literal=GITHUB_REPO=\"$GITHUB_REPO\""

    eval $SECRET_CMD
    echo -e "${GREEN}✓ Secret ${SECRET_NAME} created/updated${NC}"
else
    if ! kubectl get secret "$SECRET_NAME" -n "$NAMESPACE" &> /dev/null; then
        echo -e "${YELLOW}No deployments/k8s/.env and secret ${SECRET_NAME} not found. Create it manually:${NC}"
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

    # Agent: sync MCP ConfigMap from deployments/k8s/agent/mcp_config.json if present
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

# Show status
echo -e "${GREEN}=== Deployment Status ===${NC}"
kubectl get deployments -n "$NAMESPACE" 2>/dev/null || echo "No deployments found"
echo ""
kubectl get pods -n "$NAMESPACE" 2>/dev/null || echo "No pods found"

echo ""
echo -e "${GREEN}=== Deployment Complete ===${NC}"
if [ "$BUILD" = true ]; then
    echo -e "Images built and pushed with tag: ${YELLOW}${IMAGE_TAG}${NC}"
fi
echo -e "View logs: ${YELLOW}kubectl logs -f deployment/<service-name> -n $NAMESPACE${NC}"
echo -e "Check status: ${YELLOW}kubectl get pods -n $NAMESPACE${NC}"
echo -e "Port forward agent: ${YELLOW}kubectl port-forward svc/mcpagent-agent-cs 8000:80 -n $NAMESPACE${NC}"
