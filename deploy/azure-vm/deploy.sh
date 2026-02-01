#!/usr/bin/env bash
# Build and deploy all services to an Azure VM via SCP + SSH.
# Usage: ./deploy.sh [agent|workspace|frontend|all]
# Default: all
#
# Requires: terraform outputs (vm_fqdn, ssh_command), Go, Node.js, zig or docker (for CGO cross-compile)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
BUILD_DIR="$SCRIPT_DIR/.build"

TARGET="${1:-all}"

# ── Config from Terraform ───────────────────────────────────

cd "$SCRIPT_DIR"
VM_FQDN="$(terraform output -raw vm_fqdn 2>/dev/null || true)"
VM_USER="$(terraform output -raw ssh_command 2>/dev/null | awk '{print $2}' | cut -d@ -f1 || echo 'azureuser')"

if [ -z "$VM_FQDN" ]; then
  echo "Error: Could not get VM FQDN from terraform output."
  echo "Run 'terraform init && terraform apply' first."
  exit 1
fi

SSH_TARGET="${VM_USER}@${VM_FQDN}"
SSH_OPTS="-o StrictHostKeyChecking=no -o ConnectTimeout=10"

echo "Deploying to: $SSH_TARGET"
echo "Target: $TARGET"
echo ""

mkdir -p "$BUILD_DIR"

# ── Cross-compile Go binary with CGO (for SQLite) ──────────

BUILDER_IMAGE="go-crossbuild"

ensure_builder_image() {
  if ! docker image inspect "$BUILDER_IMAGE" &>/dev/null; then
    echo "    Building Docker cross-compilation image (one-time)..."
    docker build --platform linux/amd64 \
      -t "$BUILDER_IMAGE" \
      -f "$SCRIPT_DIR/Dockerfile.builder" \
      "$SCRIPT_DIR"
  fi
}

cross_compile_go() {
  local src_dir="$1"
  local output_name="$2"
  local output_path="$BUILD_DIR/$output_name"

  echo "    Compiling $output_name (linux/amd64 with CGO)..."

  # Try zig cc first (fast, no Docker needed)
  if command -v zig &>/dev/null; then
    echo "    Using zig as C cross-compiler"
    cd "$src_dir"
    CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
      CC="zig cc -target x86_64-linux-musl" \
      CXX="zig c++ -target x86_64-linux-musl" \
      go build -ldflags="-w -s -linkmode external -extldflags '-static'" \
      -o "$output_path" .
  # Fallback: docker-based cross-compile with pre-built image
  elif command -v docker &>/dev/null; then
    echo "    Using docker for cross-compilation"
    ensure_builder_image
    cd "$src_dir"
    # Mount parent dir so go.mod replace directives (../../mcpagent etc.) resolve
    PARENT_DIR="$(cd "$REPO_ROOT/.." && pwd)"
    docker run --rm --platform linux/amd64 \
      -v "$PARENT_DIR:/parent" \
      -w "/parent/$(python3 -c "import os; print(os.path.relpath('$src_dir', '$PARENT_DIR'))")" \
      "$BUILDER_IMAGE" \
      sh -c "GOWORK=off CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
             go build -ldflags='-w -s' -o /parent/$(python3 -c "import os; print(os.path.relpath('$REPO_ROOT', '$PARENT_DIR'))")/deploy/azure-vm/.build/$output_name ."
  else
    echo "Error: Need zig or docker for CGO cross-compilation (SQLite requires CGO)."
    echo "Install zig: https://ziglang.org/download/"
    exit 1
  fi

  echo "    Built: $output_path ($(du -h "$output_path" | cut -f1))"
}

# ── Build Functions ─────────────────────────────────────────

build_agent() {
  echo "==> Building agent..."
  cross_compile_go "$REPO_ROOT/agent_go" "mcp-agent"

  # Copy configs
  mkdir -p "$BUILD_DIR/configs"
  cp "$REPO_ROOT/agent_go/configs/mcp_servers_clean.json" "$BUILD_DIR/configs/"

  # Copy migrations if they exist
  if [ -d "$REPO_ROOT/agent_go/migrations" ]; then
    cp -r "$REPO_ROOT/agent_go/migrations" "$BUILD_DIR/migrations"
  fi

  echo "==> Agent built."
}

build_workspace() {
  echo "==> Building workspace-api..."
  cross_compile_go "$REPO_ROOT/workspace" "planner"
  echo "==> Workspace-api built."
}

build_frontend() {
  echo "==> Building frontend..."

  cd "$REPO_ROOT/frontend"

  # Build with VM FQDN as API base URL
  VITE_API_BASE_URL="http://${VM_FQDN}" \
  VITE_WORKSPACE_API_URL="http://${VM_FQDN}/workspace-api" \
  npm ci --silent
  VITE_API_BASE_URL="http://${VM_FQDN}" \
  VITE_WORKSPACE_API_URL="http://${VM_FQDN}/workspace-api" \
  npm run build

  # Copy dist to build dir
  rm -rf "$BUILD_DIR/frontend"
  cp -r "$REPO_ROOT/frontend/dist" "$BUILD_DIR/frontend"

  echo "==> Frontend built."
}

# ── Deploy to VM ────────────────────────────────────────────

deploy_to_vm() {
  echo "==> Deploying files to VM..."

  # Upload binaries and configs
  if [ "$TARGET" = "all" ] || [ "$TARGET" = "agent" ]; then
    echo "    Uploading agent..."
    ssh $SSH_OPTS "$SSH_TARGET" "sudo mkdir -p /opt/mcpagent/configs"
    scp $SSH_OPTS "$BUILD_DIR/mcp-agent" "$SSH_TARGET:/tmp/mcp-agent"
    ssh $SSH_OPTS "$SSH_TARGET" "sudo mv /tmp/mcp-agent /opt/mcpagent/mcp-agent && sudo chmod +x /opt/mcpagent/mcp-agent"
    scp $SSH_OPTS -r "$BUILD_DIR/configs/" "$SSH_TARGET:/tmp/configs/"
    ssh $SSH_OPTS "$SSH_TARGET" "sudo cp /tmp/configs/* /opt/mcpagent/configs/ && rm -rf /tmp/configs"
    if [ -d "$BUILD_DIR/migrations" ]; then
      scp $SSH_OPTS -r "$BUILD_DIR/migrations/" "$SSH_TARGET:/tmp/migrations/"
      ssh $SSH_OPTS "$SSH_TARGET" "sudo cp -r /tmp/migrations /opt/mcpagent/migrations && rm -rf /tmp/migrations"
    fi
    ssh $SSH_OPTS "$SSH_TARGET" "sudo chown -R appuser:appgroup /opt/mcpagent"
  fi

  if [ "$TARGET" = "all" ] || [ "$TARGET" = "workspace" ]; then
    echo "    Uploading workspace-api..."
    ssh $SSH_OPTS "$SSH_TARGET" "sudo mkdir -p /opt/workspace-api"
    scp $SSH_OPTS "$BUILD_DIR/planner" "$SSH_TARGET:/tmp/planner"
    ssh $SSH_OPTS "$SSH_TARGET" "sudo mv /tmp/planner /opt/workspace-api/planner && sudo chmod +x /opt/workspace-api/planner"
  fi

  if [ "$TARGET" = "all" ] || [ "$TARGET" = "frontend" ]; then
    echo "    Uploading frontend..."
    ssh $SSH_OPTS "$SSH_TARGET" "sudo mkdir -p /opt/frontend"
    scp $SSH_OPTS -r "$BUILD_DIR/frontend/." "$SSH_TARGET:/tmp/frontend/"
    ssh $SSH_OPTS "$SSH_TARGET" "sudo rm -rf /opt/frontend/* && sudo cp -r /tmp/frontend/* /opt/frontend/ && rm -rf /tmp/frontend"
  fi

  # Upload systemd units and nginx config
  echo "    Uploading service configs..."
  scp $SSH_OPTS "$SCRIPT_DIR/files/mcpagent.service" "$SSH_TARGET:/tmp/mcpagent.service"
  scp $SSH_OPTS "$SCRIPT_DIR/files/workspace-api.service" "$SSH_TARGET:/tmp/workspace-api.service"
  scp $SSH_OPTS "$SCRIPT_DIR/files/nginx-mcpagent.conf" "$SSH_TARGET:/tmp/nginx-mcpagent.conf"

  ssh $SSH_OPTS "$SSH_TARGET" bash -s <<'REMOTE'
    sudo mv /tmp/mcpagent.service /etc/systemd/system/mcpagent.service
    sudo mv /tmp/workspace-api.service /etc/systemd/system/workspace-api.service
    sudo mv /tmp/nginx-mcpagent.conf /etc/nginx/sites-available/mcpagent.conf
    sudo ln -sf /etc/nginx/sites-available/mcpagent.conf /etc/nginx/sites-enabled/mcpagent.conf
    sudo rm -f /etc/nginx/sites-enabled/default

    sudo systemctl daemon-reload
    sudo systemctl enable mcpagent workspace-api nginx
    sudo systemctl restart workspace-api
    sudo systemctl restart mcpagent
    sudo nginx -t && sudo systemctl restart nginx

    echo "    Services restarted."
REMOTE

  echo "==> Deploy complete."
}

# ── Main ────────────────────────────────────────────────────

case "$TARGET" in
  agent)
    build_agent
    deploy_to_vm
    ;;
  workspace)
    build_workspace
    deploy_to_vm
    ;;
  frontend)
    build_frontend
    deploy_to_vm
    ;;
  all)
    build_agent &
    PID_AGENT=$!
    build_workspace &
    PID_WS=$!
    build_frontend

    wait $PID_AGENT || { echo "Agent build failed"; exit 1; }
    wait $PID_WS || { echo "Workspace-api build failed"; exit 1; }

    deploy_to_vm
    ;;
  *)
    echo "Usage: $0 [agent|workspace|frontend|all]"
    exit 1
    ;;
esac

# Health checks
echo ""
echo "==> Waiting for services to start..."
sleep 5
"$SCRIPT_DIR/healthcheck.sh"

echo ""
echo "Done. Frontend: http://${VM_FQDN}"
