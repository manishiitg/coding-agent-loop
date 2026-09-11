#!/usr/bin/env bash
# Invoked only inside a fresh confida@116.202.210.102 checkout by
# server-bootstrap-build.sh. Everything below runs directly on the target
# host -- there is no cross-compile and no artifact upload, matching
# deploy/aws-ec2/server/build-and-activate.sh's (RTS's) on-server build shape.
#
# This host also runs RTS (Video Studio) and Dominion, each under its own
# separate system account/tooling. This script must never be pointed at, nor
# merged into, deploy-rootless.sh (RTS) or any Dominion deploy path -- it only
# ever touches /srv/confida and the confida-* systemd --user units.
set -euo pipefail
[[ "$(uname -sm)" == "Linux x86_64" ]] || { echo "Build must run on Linux x86_64" >&2; exit 1; }
WORKSPACE_ROOT="$1"
REPO_ROOT="$WORKSPACE_ROOT/mcp-agent-builder-go"
SCRIPT_DIR="$REPO_ROOT/deploy/confida"
REMOTE_APP="/srv/confida"
export GOMAXPROCS=4 GOFLAGS=-p=4 NODE_OPTIONS=--max-old-space-size=2048

for command in git go gcc npm rsync python3; do command -v "$command" >/dev/null || { echo "Missing $command" >&2; exit 1; }; done

# Config overlay files are read from THIS checkout (i.e. from whatever is on
# the deployed branch), not from wherever the deploy was triggered -- keeping
# the "always builds from main" contract consistent for deployment config,
# not just application source.
grep -Fq 'cdpEnabled: false' "$SCRIPT_DIR/runtime-config.js" || {
  echo "confida runtime config must display CDP as disabled" >&2
  exit 1
}

RELEASE_ID="confida-$(git -C "$REPO_ROOT" rev-parse --short HEAD)-$(date +%Y%m%d%H%M%S)"
REMOTE_RELEASE="$REMOTE_APP/releases/$RELEASE_ID"
BUILD_DIR="$REMOTE_RELEASE"
mkdir -p "$BUILD_DIR/bin" "$BUILD_DIR/frontend" "$BUILD_DIR/configs"
touch "$BUILD_DIR/.deploying"
cleanup_build() {
  if [[ "$(readlink -f "$REMOTE_APP/current")" != "$BUILD_DIR" ]]; then rm -rf "$BUILD_DIR"; fi
}
trap cleanup_build EXIT

# Build exactly the requested checkout while resolving the shared sibling
# modules from the declared workspace root, rather than trusting whatever
# checked-in go.work happens to be sitting in the cloned repo.
DEPLOY_GOWORK="$BUILD_DIR/go.work"
(cd "$BUILD_DIR" && go work init "$REPO_ROOT/agent_go" "$REPO_ROOT/workspace" "$WORKSPACE_ROOT/mcpagent" "$WORKSPACE_ROOT/multi-llm-provider-go")
{
  printf 'mcp-agent-builder-go=%s\n' "$(git -C "$REPO_ROOT" rev-parse HEAD)"
  printf 'mcpagent=%s\n' "$(git -C "$WORKSPACE_ROOT/mcpagent" rev-parse HEAD)"
  printf 'multi-llm-provider-go=%s\n' "$(git -C "$WORKSPACE_ROOT/multi-llm-provider-go" rev-parse HEAD)"
} > "$BUILD_DIR/SOURCE_REVISIONS"

echo "==> [$RELEASE_ID] Building binaries (native linux/amd64, on $(hostname))"
# Match RTS's production voice build: compile only the agent with cgo enabled,
# embed an $ORIGIN/lib rpath, and stage sherpa-onnx plus ONNX Runtime beside
# the binary. The remaining Go services stay static CGO_ENABLED=0 builds.
bash "$REPO_ROOT/deploy/aws-ec2/build/build-linux-agent.sh" "$BUILD_DIR" "$REPO_ROOT/agent_go" "$WORKSPACE_ROOT"
mv "$BUILD_DIR/bin/video-studio-agent" "$BUILD_DIR/bin/confida-agent"
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
cp "$REPO_ROOT/deploy/common/prune-releases.py" "$BUILD_DIR/prune-releases.py"
# frontend's build:report-preview step (part of `npm run build` above) writes
# report-preview.js to agent_go/cmd/server/static/ in the source checkout,
# never into the release. confida-agent, like RTS's video-studio-agent and
# Dominion's dominion-agent, runs with WorkingDirectory=.../current and
# resolves staticFrontendDir()'s default ("./static/") against that cwd, so
# without this copy preview_report always 503s with "Report preview runtime
# is missing" regardless of how many times the frontend gets built.
mkdir -p "$BUILD_DIR/static"
cp -R "$REPO_ROOT/agent_go/cmd/server/static/." "$BUILD_DIR/static/"
install -m 0644 "$SCRIPT_DIR/runtime-config.js" "$BUILD_DIR/frontend/runtime-config.js"
# The shared public MCP catalog (Notion, Linear, Sentry, Exa, etc. via the
# already-registered agentworkshq.com OAuth client metadata) -- confida is a
# generic AgentWorks instance, not a locked single-purpose product profile
# like Dominion, so it gets the same catalog Video Studio ships with.
install -m 0644 "$SCRIPT_DIR/mcp_servers_confida.json" "$BUILD_DIR/configs/mcp_servers_confida.json"
node "$BUILD_DIR/check-release-assets.mjs" "$BUILD_DIR/frontend"

# Carry the previous release's hashed frontend assets into the new one. A tab
# opened before the swap still lazy-imports chunks by their old hashed names
# on its next navigation; without these files it fails with "Failed to fetch
# dynamically imported module" (RTS hit this three times in one afternoon,
# 2026-09-03). Hashed names never collide, so only missing files are copied
# (-n), mtimes are preserved (-p), and anything carried for more than 14 days
# is dropped so the directory cannot grow forever.
prev="$REMOTE_APP/current/frontend/assets"
next="$BUILD_DIR/frontend/assets"
if [[ -d "$prev" && -d "$next" ]]; then
  cp -pn "$prev"/* "$next"/ 2>/dev/null || true
  find "$next" -type f -mtime +14 -delete
fi

echo "==> [$RELEASE_ID] Activating release and restarting services"
chmod +x "$BUILD_DIR"/bin/*
ln -sfn "$REMOTE_APP/logs" "$BUILD_DIR/logs"
ln -sfn "$BUILD_DIR" "$REMOTE_APP/current"

export XDG_RUNTIME_DIR="/run/user/$(id -u)"
mkdir -p "$HOME/.config/systemd/user/confida-agent.service.d"
printf '%s\n' '[Service]' 'Environment=AGENT_BROWSER_CDP_ENABLED=false' > "$HOME/.config/systemd/user/confida-agent.service.d/20-disable-cdp.conf"
mkdir -p "$HOME/.config/systemd/user/confida-workspace.service.d"
printf '%s\n' '[Service]' 'Environment=AGENT_BROWSER_CDP_ENABLED=false' > "$HOME/.config/systemd/user/confida-workspace.service.d/20-disable-cdp.conf"
systemctl --user daemon-reload
systemctl --user restart confida-workspace
sleep 2
systemctl --user restart confida-agent
sleep 2
systemctl --user restart confida-gateway
sleep 2
systemctl --user is-active confida-workspace confida-agent confida-gateway

echo "==> [$RELEASE_ID] Verifying"
curl -fsS -o /dev/null -w "agent  /api/health: %{http_code}\n" http://127.0.0.1:22000/api/health
curl -fsS http://127.0.0.1:22001/health; echo

rm -f "$BUILD_DIR/.deploying"
python3 "$BUILD_DIR/prune-releases.py" "$REMOTE_APP" --apply --health-url http://127.0.0.1:22000/api/health --health-url http://127.0.0.1:22001/health

echo "==> Done. Release $RELEASE_ID is live at https://confida.agentworkshq.com"
