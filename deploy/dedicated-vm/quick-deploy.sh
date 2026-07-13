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
SSH_OPTS="-i $KEY -o StrictHostKeyChecking=no -o ConnectTimeout=10"
SSH="ssh $SSH_OPTS root@$VM"
SCP="scp $SSH_OPTS"
RSYNC_SSH="ssh $SSH_OPTS"
REMOTE="/opt/mcp-agent"
TARGET="${1:-all}"
PUBLIC_URL="${PUBLIC_URL:-https://agents.excellencetechnologies.in}"

check_local_url() {
  local label="$1"
  local url="$2"
  local expect="$3"
  local http_code

  http_code="$(curl -ksS --max-time 20 -o /tmp/mcp-agent-deploy-check.$$ -w '%{http_code}' "$url" || true)"
  if [[ "$http_code" != "$expect" ]]; then
    echo "✗ $label failed: HTTP $http_code from $url"
    echo "---- response ----"
    head -c 500 /tmp/mcp-agent-deploy-check.$$ 2>/dev/null || true
    echo ""
    rm -f /tmp/mcp-agent-deploy-check.$$
    return 1
  fi
  rm -f /tmp/mcp-agent-deploy-check.$$
  echo "✓ $label: HTTP $http_code"
}

echo "==> Deploying [$TARGET] to $VM"
echo "==> Checking SSH connectivity..."
$SSH "echo '    SSH OK: ' \$(hostname)"

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
  rsync -az --delete --exclude='.git' --exclude='*.db' --exclude='.gocache' --exclude='node_modules' --exclude='logs/' --exclude='tmp/' \
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
  # Exclude runtime-config.js — it's only for local dev (dynamic ports). In Docker
  # prod, Caddy routes /api* to the fixed agent port and the frontend uses
  # same-origin relative URLs via VITE build-time envs.
  rsync -az --delete --exclude='runtime-config.js' \
    -e "$RSYNC_SSH" "$REPO_ROOT/frontend/dist/" "root@$VM:$REMOTE/src/frontend-dist/"
  # Write an empty config so index.html's <script src="/runtime-config.js"> doesn't
  # fall through nginx's try_files → /index.html (which would execute HTML as JS).
  $SSH "echo 'window.__APP_RUNTIME_CONFIG__ = {};' > $REMOTE/src/frontend-dist/runtime-config.js"
fi

# Copy run-agent.sh so the server always has the latest version
if [[ "$TARGET" == "all" || "$TARGET" == "agent" ]]; then
  scp -i "$KEY" -o StrictHostKeyChecking=no "$SCRIPT_DIR/run-agent.sh" "root@$VM:/opt/mcp-agent/run-agent.sh"
  ssh -i "$KEY" -o StrictHostKeyChecking=no "root@$VM" "chmod +x /opt/mcp-agent/run-agent.sh"
fi

# Install Supabase keep-alive cron job (daily ping to prevent free-tier pausing)
scp -i "$KEY" -o StrictHostKeyChecking=no "$SCRIPT_DIR/supabase-keepalive.sh" "root@$VM:/opt/mcp-agent/supabase-keepalive.sh"
ssh -i "$KEY" -o StrictHostKeyChecking=no "root@$VM" "
  chmod +x /opt/mcp-agent/supabase-keepalive.sh
  # Add cron job if not already present
  (crontab -l 2>/dev/null | grep -v supabase-keepalive; echo '0 6 * * * /opt/mcp-agent/supabase-keepalive.sh') | crontab -
"

# --- Server-side: fix replaces, rebuild, restart ---
echo "==> Building and restarting on server..."
$SSH bash -s "$TARGET" "$PUBLIC_URL" << 'REMOTE_SCRIPT'
TARGET=$1
PUBLIC_URL=$2
export PATH=$PATH:/usr/local/go/bin:/root/go/bin
export HOME=/root
export GOPATH=/root/go
REMOTE=/opt/mcp-agent

wait_for_url() {
  local label=$1
  local url=$2
  local expect=${3:-200}
  local code
  for _ in $(seq 1 60); do
    code=$(curl -ksS --max-time 3 -o /tmp/mcp-agent-health.$$ -w '%{http_code}' "$url" 2>/dev/null || true)
    if [[ "$code" == "$expect" ]]; then
      rm -f /tmp/mcp-agent-health.$$
      echo "    ✓ $label ready ($code)"
      return 0
    fi
    sleep 2
  done
  echo "    ✗ $label not ready; last HTTP $code from $url"
  echo "    ---- response ----"
  head -c 500 /tmp/mcp-agent-health.$$ 2>/dev/null || true
  echo ""
  rm -f /tmp/mcp-agent-health.$$
  return 1
}

wait_for_frontend_container() {
  for _ in $(seq 1 60); do
    if cd "$REMOTE" && docker compose exec -T frontend wget -qO- http://127.0.0.1/health 2>/dev/null | grep -q '"status":"healthy"'; then
      echo "    ✓ frontend container ready"
      return 0
    fi
    sleep 2
  done
  echo "    ✗ frontend container not ready"
  cd "$REMOTE" && docker compose ps frontend || true
  return 1
}

if [[ "$TARGET" == "all" || "$TARGET" == "agent" ]]; then
  # Keep CLI tools used by the bare-metal agent up to date. The agent shells out
  # to these, so if any is missing the call fails with "not found" (code 127).
  # Running on every agent deploy ensures they're installed and fresh.
  if ! command -v tmux >/dev/null 2>&1; then
    echo "    Installing tmux for Claude Code interactive provider..."
    apt-get update >/dev/null
    apt-get install -y --no-install-recommends tmux >/dev/null
  fi
  echo "    Updating bare-metal CLI tools (agent-browser, claude, pi)..."
  npm install -g agent-browser@latest @anthropic-ai/claude-code@latest @earendil-works/pi-coding-agent@latest 2>&1 | tail -3

  # Fix go.mod replace directives
  cd $REMOTE/src/agent_go
  go mod edit -dropreplace=github.com/manishiitg/mcpagent 2>/dev/null; go mod edit -dropreplace=github.com/manishiitg/multi-llm-provider-go 2>/dev/null; go mod edit -dropreplace=github.com/manishiitg/coding-agent-loop/workspace 2>/dev/null
  go mod edit -replace=github.com/manishiitg/mcpagent=../mcpagent -replace=github.com/manishiitg/multi-llm-provider-go=../multi-llm-provider-go -replace=github.com/manishiitg/coding-agent-loop/workspace=../workspace

  cd $REMOTE/src/mcpagent
  go mod edit -dropreplace=github.com/manishiitg/multi-llm-provider-go 2>/dev/null; go mod edit -dropreplace=github.com/manishiitg/coding-agent-loop/workspace 2>/dev/null
  go mod edit -replace=github.com/manishiitg/multi-llm-provider-go=../multi-llm-provider-go -replace=github.com/manishiitg/coding-agent-loop/workspace=../workspace

  cd $REMOTE/src/multi-llm-provider-go
  go mod edit -dropreplace=github.com/manishiitg/coding-agent-loop/workspace 2>/dev/null
  go mod edit -replace=github.com/manishiitg/coding-agent-loop/workspace=../workspace

  # Rebuild mcpbridge
  cd $REMOTE/src/mcpagent && go install ./cmd/mcpbridge/ 2>&1 | tail -1

  systemctl restart mcp-agent
  echo "    Agent restarting (~60s to compile)"
  systemctl is-active --quiet mcp-agent
  wait_for_url "agent API" "http://127.0.0.1:8000/api/health" 200
fi

if [[ "$TARGET" == "all" || "$TARGET" == "frontend" ]]; then
  cat > /tmp/Dockerfile.frontend-prebuilt << 'DOCKERFILE'
FROM nginx:alpine
COPY . /usr/share/nginx/html
RUN printf 'gzip on;\ngzip_types text/plain text/css application/json application/javascript text/xml application/xml text/javascript image/svg+xml;\ngzip_min_length 256;\ngzip_vary on;\nserver {\n    listen 80;\n    root /usr/share/nginx/html;\n    index index.html;\n    location / {\n        try_files $uri $uri/ /index.html;\n    }\n    location = /index.html {\n        add_header Cache-Control "no-cache, no-store, must-revalidate";\n    }\n    location /assets/ {\n        expires 1y;\n        add_header Cache-Control "public, immutable";\n    }\n    location /health {\n        access_log off;\n        default_type application/json;\n        return 200 "{\"status\":\"healthy\",\"service\":\"frontend\"}";\n    }\n}\n' > /etc/nginx/conf.d/default.conf
CMD ["nginx", "-g", "daemon off;"]
DOCKERFILE
  cd $REMOTE/src/frontend-dist
  docker build -f /tmp/Dockerfile.frontend-prebuilt -t mcp-agent-frontend:latest . 2>&1 | tail -1
  cd $REMOTE && docker compose up -d --force-recreate frontend
  echo "    Frontend deployed"
  cd $REMOTE && docker compose ps frontend
  wait_for_frontend_container
fi

if [[ "$TARGET" == "all" || "$TARGET" == "workspace" ]]; then
  systemctl restart mcp-workspace
  echo "    Workspace restarting (~30s to compile)"
fi

if [[ "$TARGET" == "all" ]]; then
  cd $REMOTE && docker compose restart caddy
  wait_for_url "public frontend" "$PUBLIC_URL/?deploy_check=$(date +%s)" 200
fi

echo "==> Done!"
REMOTE_SCRIPT

echo "==> Verifying public site..."
check_local_url "frontend" "$PUBLIC_URL/?deploy_check=$(date +%s)" 200
check_local_url "runtime config" "$PUBLIC_URL/runtime-config.js?deploy_check=$(date +%s)" 200
check_local_url "agent API proxy" "$PUBLIC_URL/api/health" 200
echo "==> Deploy checks passed."
