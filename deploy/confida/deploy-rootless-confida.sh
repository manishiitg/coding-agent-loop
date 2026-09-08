#!/usr/bin/env bash
set -euo pipefail

# Redeploy script for the confida AgentWorks instance ONLY.
#
# Target: Hetzner host 116.202.210.102, isolated `confida` system account,
# rootless systemd --user services on loopback ports 22000 (agent) / 22001
# (workspace) / 22080 (gateway), fronted by the box's existing shared Caddy
# at confida.agentworkshq.com. Default LLM is Gemini 3.8 Flash via the
# `vertex` provider (GEMINI_API_KEY already set in the remote .env).
#
# This host also runs RTS (Video Studio) and Dominion, each managed by its
# own separate tooling/session. This script must never be pointed at, nor
# merged into, deploy-rootless.sh (RTS) or any Dominion deploy path -- it
# only ever touches /srv/confida and the confida-* systemd units.
#
# One-time account/systemd-unit/Caddy-site bootstrap is NOT part of this
# script (that was done by hand once). This script is the repeatable
# redeploy path, mirroring deploy-rootless.sh's shape for RTS.
#
# Source of truth: like RTS's deploy-rootless.sh, this builds ONLY from a
# fresh, depth-1 clone of each repo's `main` branch on the git remote -- not
# whatever happens to be sitting in the local working tree (uncommitted or
# untracked files, or a dirty sibling checkout, can never enter a release).
# Set DEPLOY_SOURCE_MODE=local to build from the local checkouts instead,
# for fast iteration only -- never for a release you intend to keep.
#
# Usage: ./deploy-rootless-confida.sh
# Env overrides: HOST_IP, SSH_PORT, SSH_KEY_PATH, DEPLOY_SOURCE_MODE, DEPLOY_BRANCH

HOST_IP="${HOST_IP:-116.202.210.102}"
SSH_PORT="${SSH_PORT:-2299}"
SSH_KEY_PATH="${SSH_KEY_PATH:-$HOME/.ssh/confida_deploy}"
REMOTE_APP="/srv/confida"
DEPLOY_SOURCE_MODE="${DEPLOY_SOURCE_MODE:-remote-main}"
DEPLOY_BRANCH="${DEPLOY_BRANCH:-main}"

LOCAL_SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
LOCAL_REPO_ROOT="$(cd "$LOCAL_SCRIPT_DIR/../.." && pwd)"     # mcp-agent-builder-go (local checkout)
LOCAL_WORKSPACE_ROOT="$(cd "$LOCAL_REPO_ROOT/.." && pwd)"    # sibling repos (mcpagent, ...)

for cmd in go node npm rsync ssh git; do
  command -v "$cmd" >/dev/null || { echo "Missing $cmd" >&2; exit 1; }
done

# jq is not needed by this script itself -- it's a runtime dependency of
# agent-authored shell scripts, which the MCP bridge guidance (mcp-bridge.md)
# explicitly tells agents to use for encoding JSON tool-call payloads. A
# fresh confida host had no jq at all, so every such script failed. Checked
# (not installed) here: installing it needs root, which this script does not
# assume it has -- see the printed remedy below instead of failing the deploy.
echo "==> Checking for jq on the remote host (required by agent shell scripts, not by this script)"
if ssh -p "$SSH_PORT" -i "$SSH_KEY_PATH" -o ConnectTimeout=10 "confida@$HOST_IP" 'command -v jq' >/dev/null 2>&1; then
  echo "    jq is present."
else
  echo "    WARNING: jq is NOT installed on $HOST_IP. Agent shell scripts that rely on it will fail." >&2
  echo "    Install it once as root: ssh -p $SSH_PORT root@$HOST_IP 'apt-get install -y jq'" >&2
fi

# agent-browser is a mandatory runtime dependency: preview_report and every
# browser-automation tool shell out to it by name with no PATH check of their
# own, so a missing install surfaces only as an opaque "exit code 127:
# agent-browser: not found" deep inside a tool call. Unlike jq, it needs no
# root (a per-user npm global prefix is enough), so attempt the install here
# rather than only warning -- non-fatal, since this script cannot know this
# host's exact prefix/PATH convention in advance.
echo "==> Ensuring agent-browser is installed on the remote host"
if ssh -p "$SSH_PORT" -i "$SSH_KEY_PATH" -o ConnectTimeout=10 "confida@$HOST_IP" 'command -v agent-browser' >/dev/null 2>&1; then
  echo "    agent-browser is present."
elif ssh -p "$SSH_PORT" -i "$SSH_KEY_PATH" -o ConnectTimeout=10 "confida@$HOST_IP" \
    'npm install -g agent-browser@latest >/dev/null 2>&1 && command -v agent-browser' >/dev/null 2>&1; then
  echo "    agent-browser installed."
else
  echo "    WARNING: agent-browser is NOT installed on $HOST_IP and the default-prefix install failed" >&2
  echo "    (likely a permissions error on the system npm prefix). Install it once with a writable" >&2
  echo "    --prefix and put that prefix's bin/ on PATH: ssh -p $SSH_PORT confida@$HOST_IP" >&2
  echo "      npm install --prefix <writable-dir> -g agent-browser@latest" >&2
fi

SOURCE_ROOT=""
if [[ "$DEPLOY_SOURCE_MODE" == "remote-main" ]]; then
  echo "==> Cloning $DEPLOY_BRANCH from git remotes (mcp-agent-builder-go, mcpagent, multi-llm-provider-go)"
  SOURCE_ROOT="$(mktemp -d)"
  git clone --quiet --depth 1 --single-branch --branch "$DEPLOY_BRANCH" "$(git -C "$LOCAL_REPO_ROOT" remote get-url origin)" "$SOURCE_ROOT/mcp-agent-builder-go"
  git clone --quiet --depth 1 --single-branch --branch "$DEPLOY_BRANCH" "$(git -C "$LOCAL_WORKSPACE_ROOT/mcpagent" remote get-url origin)" "$SOURCE_ROOT/mcpagent"
  git clone --quiet --depth 1 --single-branch --branch "$DEPLOY_BRANCH" "$(git -C "$LOCAL_WORKSPACE_ROOT/multi-llm-provider-go" remote get-url origin)" "$SOURCE_ROOT/multi-llm-provider-go"
  REPO_ROOT="$SOURCE_ROOT/mcp-agent-builder-go"
  WORKSPACE_ROOT="$SOURCE_ROOT"
elif [[ "$DEPLOY_SOURCE_MODE" == "local" ]]; then
  echo "==> WARNING: building from local working tree (DEPLOY_SOURCE_MODE=local) -- not for a release you intend to keep"
  REPO_ROOT="$LOCAL_REPO_ROOT"
  WORKSPACE_ROOT="$LOCAL_WORKSPACE_ROOT"
else
  echo "DEPLOY_SOURCE_MODE must be remote-main or local" >&2
  exit 1
fi
test -f "$WORKSPACE_ROOT/mcpagent/cmd/mcpbridge/main.go" || { echo "Expected sibling checkout: $WORKSPACE_ROOT/mcpagent" >&2; exit 1; }

RELEASE_ID="confida-$(git -C "$REPO_ROOT" rev-parse --short HEAD 2>/dev/null || echo local)-$(date +%Y%m%d%H%M%S)"
BUILD_DIR="$(mktemp -d)"
trap 'rm -rf "$BUILD_DIR" "${SOURCE_ROOT:-}"' EXIT
mkdir -p "$BUILD_DIR/bin" "$BUILD_DIR/frontend" "$BUILD_DIR/configs"

# Build exactly the requested checkout while resolving the shared sibling
# modules from the declared workspace root, rather than trusting whatever
# checked-in go.work happens to be sitting in the cloned/local repo.
DEPLOY_GOWORK="$BUILD_DIR/go.work"
(cd "$BUILD_DIR" && go work init "$REPO_ROOT/agent_go" "$REPO_ROOT/workspace" "$WORKSPACE_ROOT/mcpagent" "$WORKSPACE_ROOT/multi-llm-provider-go")

SSH_OPTS=(-p "$SSH_PORT" -i "$SSH_KEY_PATH" -o BatchMode=yes -o StrictHostKeyChecking=accept-new)
SSH=(ssh "${SSH_OPTS[@]}" "confida@$HOST_IP")
RSYNC_SSH="ssh ${SSH_OPTS[*]}"

echo "==> [$RELEASE_ID] Building binaries (linux/amd64)"
(cd "$WORKSPACE_ROOT" && GOWORK="$DEPLOY_GOWORK" GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "$BUILD_DIR/bin/confida-agent" "$REPO_ROOT/agent_go")
(cd "$WORKSPACE_ROOT" && GOWORK="$DEPLOY_GOWORK" GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "$BUILD_DIR/bin/confida-workspace" "$REPO_ROOT/workspace")
# Literal filename required: workspace/security/landlock_policy.go resolves
# its sandbox launcher by this exact name regardless of which product runs.
(cd "$WORKSPACE_ROOT" && GOWORK="$DEPLOY_GOWORK" GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "$BUILD_DIR/bin/video-studio-landlock-runner" "$REPO_ROOT/workspace/cmd/landlock-runner")
(cd "$WORKSPACE_ROOT" && GOWORK="$DEPLOY_GOWORK" GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "$BUILD_DIR/bin/mcpbridge" ./mcpagent/cmd/mcpbridge)
GOWORK="$DEPLOY_GOWORK" GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "$BUILD_DIR/bin/confida-gateway" "$REPO_ROOT/deploy/aws-ec2/server/auth-gateway.go"

echo "==> [$RELEASE_ID] Building frontend"
(cd "$REPO_ROOT/frontend" && npm ci)
(cd "$REPO_ROOT/frontend" && VITE_API_BASE_URL='' VITE_WORKSPACE_API_URL=/api/wp npm run build)
cp -R "$REPO_ROOT/frontend/dist/." "$BUILD_DIR/frontend/"
cp "$REPO_ROOT/frontend/scripts/check-release-assets.mjs" "$BUILD_DIR/check-release-assets.mjs"
# frontend's build:report-preview step (part of `npm run build` above) writes
# report-preview.js to agent_go/cmd/server/static/ in the source checkout,
# never into the release. confida-agent, like RTS's video-studio-agent and
# Dominion's dominion-agent, runs with WorkingDirectory=.../current and
# resolves staticFrontendDir()'s default ("./static/") against that cwd, so
# without this copy preview_report always 503s with "Report preview runtime
# is missing" regardless of how many times the frontend gets built.
mkdir -p "$BUILD_DIR/static"
cp -R "$REPO_ROOT/agent_go/cmd/server/static/." "$BUILD_DIR/static/"
# See runtime-config.js in this directory for why this overwrite is required.
# Sourced from THIS script's own directory, not the cloned repo -- it is
# deployment configuration, not application source.
install -m 0644 "$LOCAL_SCRIPT_DIR/runtime-config.js" "$BUILD_DIR/frontend/runtime-config.js"
node "$BUILD_DIR/check-release-assets.mjs" "$BUILD_DIR/frontend"

# The shared public MCP catalog (Notion, Linear, Sentry, Exa, etc. via the
# already-registered agentworkshq.com OAuth client metadata) -- confida is a
# generic AgentWorks instance, not a locked single-purpose product profile
# like Dominion, so it gets the same catalog Video Studio ships with.
install -m 0644 "$LOCAL_SCRIPT_DIR/mcp_servers_confida.json" "$BUILD_DIR/configs/mcp_servers_confida.json"

REMOTE_RELEASE="$REMOTE_APP/releases/$RELEASE_ID"
echo "==> [$RELEASE_ID] Shipping release to confida@$HOST_IP:$REMOTE_RELEASE"
"${SSH[@]}" "mkdir -p '$REMOTE_RELEASE'"
rsync -az -e "$RSYNC_SSH" "$BUILD_DIR/" "confida@$HOST_IP:$REMOTE_RELEASE/"
"${SSH[@]}" "node '$REMOTE_RELEASE/check-release-assets.mjs' '$REMOTE_RELEASE/frontend'"

echo "==> [$RELEASE_ID] Activating release and restarting services"
"${SSH[@]}" bash -s -- "$REMOTE_RELEASE" "$REMOTE_APP" <<'REMOTE_ACTIVATE'
set -euo pipefail
remote_release="$1"
remote_app="$2"

chmod +x "$remote_release"/bin/*
ln -sfn "$remote_app/logs" "$remote_release/logs"
ln -sfn "$remote_release" "$remote_app/current"

export XDG_RUNTIME_DIR="/run/user/$(id -u)"
systemctl --user daemon-reload
systemctl --user restart confida-workspace
sleep 2
systemctl --user restart confida-agent
sleep 2
systemctl --user restart confida-gateway
sleep 2
systemctl --user is-active confida-workspace confida-agent confida-gateway
REMOTE_ACTIVATE

echo "==> [$RELEASE_ID] Verifying"
"${SSH[@]}" '
  curl -fsS -o /dev/null -w "agent  /api/health: %{http_code}\n" http://127.0.0.1:22000/api/health
  curl -fsS http://127.0.0.1:22001/health; echo
'
curl -fsSI "https://confida.agentworkshq.com/login" | head -1

echo "==> Done. Release $RELEASE_ID is live at https://confida.agentworkshq.com"
