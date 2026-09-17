#!/usr/bin/env bash
# Invoked only inside a fresh $PRODUCT@<host> checkout by bootstrap-build.sh.
# Everything below runs directly on the target host -- there is no
# cross-compile and no artifact upload, matching
# deploy/aws-ec2/server/build-and-activate.sh's (video-studio's) and
# deploy/cf/server-build-and-activate.sh's (confida's) on-server build shape.
# This is the generalized, parameterized version of the latter: per-product
# facts (ports, provider/model, CLI list, extra env, runtime-config.js,
# mcp-servers.json) live under deploy/rootless-linux/products/$PRODUCT/ and
# are read from the checkout on the deployed branch, never from the
# triggering machine.
set -euo pipefail
[[ "$(uname -sm)" == "Linux x86_64" ]] || { echo "Build must run on Linux x86_64" >&2; exit 1; }
WORKSPACE_ROOT="$1"
PRODUCT="$2"
REPO_ROOT="$WORKSPACE_ROOT/mcp-agent-builder-go"
SCRIPT_DIR="$REPO_ROOT/deploy/rootless-linux"
PRODUCT_DIR="$SCRIPT_DIR/products/$PRODUCT"
REMOTE_APP="/srv/$PRODUCT"
export GOMAXPROCS=4 GOFLAGS=-p=4 NODE_OPTIONS=--max-old-space-size=2048
export PATH="$REMOTE_APP/tools/node/bin:$REMOTE_APP/tools/bin:$PATH"

test -f "$PRODUCT_DIR/product.env" || { echo "No such product: $PRODUCT_DIR/product.env" >&2; exit 1; }
# shellcheck disable=SC1091
source "$PRODUCT_DIR/product.env"
[[ "$PRODUCT" == "$(basename "$PRODUCT_DIR")" ]] || { echo "product.env PRODUCT=$PRODUCT does not match directory $(basename "$PRODUCT_DIR")" >&2; exit 1; }

for command in git go gcc npm python3; do command -v "$command" >/dev/null || { echo "Missing $command" >&2; exit 1; }; done
PRODUCT="$PRODUCT" EXPECTED_PUBLIC_URL="${EXPECTED_PUBLIC_URL:-}" python3 "$SCRIPT_DIR/deployment_checks.py" preflight

builder_revision="$(git -C "$REPO_ROOT" rev-parse HEAD)"
test -n "$builder_revision"
RELEASE_ID="${PRODUCT}-${builder_revision:0:8}-$(date +%Y%m%d%H%M%S)"
REMOTE_RELEASE="$REMOTE_APP/releases/$RELEASE_ID"
BUILD_DIR="$REMOTE_RELEASE"
MIGRATION_STOPPED_AGENT=0
mkdir -p "$BUILD_DIR/bin" "$BUILD_DIR/frontend" "$BUILD_DIR/configs"
touch "$BUILD_DIR/.deploying"
cleanup_build() {
  if [[ "$MIGRATION_STOPPED_AGENT" == 1 ]]; then
    systemctl --user restart "$PRODUCT-agent" >/dev/null 2>&1 || true
  fi
  if [[ "$(readlink -f "$REMOTE_APP/current")" != "$BUILD_DIR" ]]; then rm -rf "$BUILD_DIR"; fi
}
trap cleanup_build EXIT

# Build exactly the requested checkout while resolving the shared sibling
# modules from the declared workspace root, rather than trusting whatever
# checked-in go.work happens to be sitting in the cloned repo.
DEPLOY_GOWORK="$BUILD_DIR/go.work"
(cd "$BUILD_DIR" && go work init "$REPO_ROOT/agent_go" "$REPO_ROOT/workspace" "$WORKSPACE_ROOT/mcpagent" "$WORKSPACE_ROOT/multi-llm-provider-go")
{
  printf 'mcp-agent-builder-go=%s\n' "$builder_revision"
  printf 'mcpagent=%s\n' "$(git -C "$WORKSPACE_ROOT/mcpagent" rev-parse HEAD)"
  printf 'multi-llm-provider-go=%s\n' "$(git -C "$WORKSPACE_ROOT/multi-llm-provider-go" rev-parse HEAD)"
} > "$BUILD_DIR/SOURCE_REVISIONS"

echo "==> [$RELEASE_ID] Building binaries (native linux/amd64, on $(hostname))"
# Compile the agent with cgo enabled (it links sherpa-onnx for voice/STT) and
# an $ORIGIN/lib rpath, staging the native libraries beside the binary. The
# remaining Go services stay static CGO_ENABLED=0 builds.
bash "$REPO_ROOT/deploy/aws-ec2/build/build-linux-agent.sh" "$BUILD_DIR" "$REPO_ROOT/agent_go" "$WORKSPACE_ROOT"
mv "$BUILD_DIR/bin/video-studio-agent" "$BUILD_DIR/bin/$PRODUCT-agent"
(cd "$WORKSPACE_ROOT" && GOWORK="$DEPLOY_GOWORK" GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "$BUILD_DIR/bin/$PRODUCT-workspace" "$REPO_ROOT/workspace")
# Literal filename required: workspace/security/landlock_policy.go resolves
# its sandbox launcher by this exact name regardless of which product runs.
(cd "$WORKSPACE_ROOT" && GOWORK="$DEPLOY_GOWORK" GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "$BUILD_DIR/bin/video-studio-landlock-runner" "$REPO_ROOT/workspace/cmd/landlock-runner")
(cd "$WORKSPACE_ROOT" && GOWORK="$DEPLOY_GOWORK" GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "$BUILD_DIR/bin/mcpbridge" ./mcpagent/cmd/mcpbridge)
GOWORK="$DEPLOY_GOWORK" GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "$BUILD_DIR/bin/$PRODUCT-gateway" "$REPO_ROOT/deploy/aws-ec2/server/auth-gateway.go"

echo "==> [$RELEASE_ID] Building frontend"
(cd "$REPO_ROOT/frontend" && npm ci)
(cd "$REPO_ROOT/frontend" && VITE_API_BASE_URL='' VITE_WORKSPACE_API_URL=/api/wp npm run build)
cp -R "$REPO_ROOT/frontend/dist/." "$BUILD_DIR/frontend/"
cp "$REPO_ROOT/frontend/scripts/check-release-assets.mjs" "$BUILD_DIR/check-release-assets.mjs"
cp "$REPO_ROOT/deploy/common/prune-releases.py" "$BUILD_DIR/prune-releases.py"
cp "$SCRIPT_DIR/deployment_checks.py" "$BUILD_DIR/deployment_checks.py"
# frontend's build:report-preview step (part of `npm run build` above) writes
# report-preview.js to agent_go/cmd/server/static/ in the source checkout,
# never into the release. $PRODUCT-agent, like every other product on this
# pattern, runs with WorkingDirectory=.../current and resolves
# staticFrontendDir()'s default ("./static/") against that cwd, so without
# this copy preview_report always 503s with "Report preview runtime is
# missing" regardless of how many times the frontend gets built.
mkdir -p "$BUILD_DIR/static"
cp -R "$REPO_ROOT/agent_go/cmd/server/static/." "$BUILD_DIR/static/"
install -m 0644 "$PRODUCT_DIR/runtime-config.js" "$BUILD_DIR/frontend/runtime-config.js"
install -m 0644 "$PRODUCT_DIR/mcp-servers.json" "$BUILD_DIR/configs/mcp_servers_$PRODUCT.json"
node "$BUILD_DIR/check-release-assets.mjs" "$BUILD_DIR/frontend"

if [[ "${COPY_PLAYBOOKS:-false}" == "true" ]]; then
  python3 "$REPO_ROOT/playbooks/scripts/validate_playbooks.py"
  mkdir -p "$BUILD_DIR/playbooks"
  cp -R "$REPO_ROOT/playbooks/." "$BUILD_DIR/playbooks/"
  python3 "$BUILD_DIR/playbooks/scripts/validate_playbooks.py"
fi

# Carry the previous release's hashed frontend assets into the new one. A tab
# opened before the swap still lazy-imports chunks by their old hashed names
# on its next navigation; without these files it fails with "Failed to fetch
# dynamically imported module". Hashed names never collide, so only missing
# files are copied (-n), mtimes are preserved (-p), and anything carried for
# more than 14 days is dropped so the directory cannot grow forever.
prev="$REMOTE_APP/current/frontend/assets"
next="$BUILD_DIR/frontend/assets"
if [[ -d "$prev" && -d "$next" ]]; then
  cp -pn "$prev"/* "$next"/ 2>/dev/null || true
  find "$next" -type f -mtime +14 -delete
fi

echo "==> [$RELEASE_ID] Activating release and restarting services"
# Check again after the build, before switching current or restarting services.
PRODUCT="$PRODUCT" EXPECTED_PUBLIC_URL="${EXPECTED_PUBLIC_URL:-}" python3 "$SCRIPT_DIR/deployment_checks.py" preflight
chmod +x "$BUILD_DIR"/bin/*
ln -sfn "$REMOTE_APP/logs" "$BUILD_DIR/logs"

runtime_path="$REMOTE_APP/tools/node/bin:$REMOTE_APP/tools/bin:$REMOTE_APP/home/.local/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

# EnvironmentFile= values are applied after Environment= and therefore win
# for duplicate variables -- a systemd drop-in setting PATH looks correct in
# `systemctl show` while the executed process still receives whatever PATH
# is (or is not) in .env. Replace just that one line atomically so the
# managed PATH always wins regardless of what the drop-in also says, and
# preserve every other line in .env untouched (confida hit this live,
# 2026-09-11: see deploy/cf/server-build-and-activate.sh).
env_file="$REMOTE_APP/.env"
if [[ -f "$env_file" ]]; then
  env_next="$(mktemp "$REMOTE_APP/.env.runtime.XXXXXX")"
  awk -v managed_path="$runtime_path" '
    BEGIN { wrote_path = 0 }
    /^PATH=/ { if (!wrote_path) { print "PATH=" managed_path; wrote_path = 1 }; next }
    { print }
    END { if (!wrote_path) print "PATH=" managed_path }
  ' "$env_file" > "$env_next"
  chmod 0600 "$env_next"
  mv "$env_next" "$env_file"
fi

mkdir -p "$HOME/.config/systemd/user/$PRODUCT-agent.service.d" "$HOME/.config/systemd/user/$PRODUCT-workspace.service.d"
{
  echo '[Service]'
  echo "Environment=PATH=$runtime_path"
  for entry in "${EXTRA_ENV[@]:-}"; do
    [[ -n "$entry" ]] && echo "Environment=$entry"
  done
} > "$HOME/.config/systemd/user/$PRODUCT-agent.service.d/zz-deploy-managed.conf"
cp "$HOME/.config/systemd/user/$PRODUCT-agent.service.d/zz-deploy-managed.conf" \
   "$HOME/.config/systemd/user/$PRODUCT-workspace.service.d/zz-deploy-managed.conf"

# One-time migration of legacy Workflow Builder chats, if this product opted
# in. Runs after `current` points at code that understands the new nested
# layout, but before starting the new agent, so no turn can rewrite a
# transcript while it moves. The marker lives outside releases and makes all
# later deployments a no-op.
if [[ "${RUN_WORKFLOW_BUILDER_MIGRATION:-false}" == "true" ]]; then
  migration_state="$REMOTE_APP/state/migrations"
  migration_marker="$migration_state/workflow-builder-chats-v1.done"
  install -d -m 0700 "$migration_state"
  if [[ ! -f "$migration_marker" ]]; then
    echo "==> [$RELEASE_ID] Migrating legacy Workflow Builder chats"
    mkdir -p "$BUILD_DIR/migrations"
    install -m 0755 "$REPO_ROOT/scripts/migrate_workflow_builder_chats.py" "$BUILD_DIR/migrations/migrate_workflow_builder_chats.py"
    install -m 0644 "$SCRIPT_DIR/workflow-builder-chat-owners-v1.json" "$BUILD_DIR/migrations/workflow-builder-chat-owners-v1.json"
    MIGRATION_STOPPED_AGENT=1
    systemctl --user stop "$PRODUCT-agent"
    python3 "$BUILD_DIR/migrations/migrate_workflow_builder_chats.py" \
      --workspace-root "$REMOTE_APP/data/docs" \
      --owner-map "$BUILD_DIR/migrations/workflow-builder-chat-owners-v1.json" \
      --apply
    marker_tmp="$(mktemp "$migration_state/.workflow-builder-chats-v1.XXXXXX")"
    printf 'release=%s\ncompleted_at=%s\n' "$RELEASE_ID" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > "$marker_tmp"
    chmod 0600 "$marker_tmp"
    mv "$marker_tmp" "$migration_marker"
  fi
fi

ln -sfn "$BUILD_DIR" "$REMOTE_APP/current"

# Drain before the restart: restarting while a turn is running hands the user
# a 502 mid-message. Poll the agent's /health "drain" block until it is idle,
# up to DRAIN_TIMEOUT_SECONDS (default 5 min); new turns can still start
# during the wait, so this is best-effort, and an agent without the field
# (older release, or not yet running) drains immediately.
DRAIN_TIMEOUT_SECONDS="${DRAIN_TIMEOUT_SECONDS:-300}"
deadline=$(($(date +%s) + DRAIN_TIMEOUT_SECONDS))
while :; do
  h="$(curl -s --max-time 5 "http://127.0.0.1:$AGENT_PORT/api/health" || true)"
  idle="$(printf '%s' "$h" | python3 -c 'import json,sys
try:
  d=json.load(sys.stdin)
  print("false" if d.get("drain",{}).get("idle") is False else "true")
except Exception:
  print("true")' 2>/dev/null || echo true)"
  [[ "$idle" == "true" ]] && { echo "drain: agent idle, restarting"; break; }
  if [[ $(date +%s) -ge $deadline ]]; then
    echo "drain: still busy after ${DRAIN_TIMEOUT_SECONDS}s; restarting anyway" >&2
    break
  fi
  echo "drain: waiting for in-flight turns"
  sleep 5
done

systemctl --user daemon-reload
systemctl --user restart "$PRODUCT-workspace"
sleep 2
systemctl --user restart "$PRODUCT-agent"
MIGRATION_STOPPED_AGENT=0
sleep 2
systemctl --user restart "$PRODUCT-gateway"
sleep 2
systemctl --user is-active "$PRODUCT-workspace" "$PRODUCT-agent" "$PRODUCT-gateway"

# Verify what the services actually received at exec time. `systemctl show`
# reports Environment= assignments but does not expose a later PATH override
# from EnvironmentFile=, which is how a broken deploy can otherwise pass.
for unit in "$PRODUCT-agent" "$PRODUCT-workspace"; do
  pid="$(systemctl --user show "$unit" -p MainPID --value)"
  [[ "$pid" -gt 0 ]]
  tr '\0' '\n' < "/proc/$pid/environ" | grep -Fqx "PATH=$runtime_path"
  for entry in "${EXTRA_ENV[@]:-}"; do
    [[ -n "$entry" ]] && { tr '\0' '\n' < "/proc/$pid/environ" | grep -Fqx "$entry" || { echo "$unit did not receive $entry" >&2; exit 1; }; }
  done
done

echo "==> [$RELEASE_ID] Verifying"
PRODUCT="$PRODUCT" EXPECTED_PUBLIC_URL="${EXPECTED_PUBLIC_URL:-}" python3 "$BUILD_DIR/deployment_checks.py" running
curl -fsS -o /dev/null -w "agent  /api/health: %{http_code}\n" "http://127.0.0.1:$AGENT_PORT/api/health"
curl -fsS "http://127.0.0.1:$WORKSPACE_PORT/health"; echo
# Not `curl -f`: whether /api/health is reachable through the gateway without
# auth depends on its gate model (GATEWAY_DISABLE_PASSWORD_GATE in .env) --
# see the matching comment in deploy.sh. Only a connection failure or a 5xx
# means the gateway itself is broken.
public_code="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 10 "https://$DOMAIN/api/health")"
echo "public /api/health: $public_code"
[[ "$public_code" -lt 500 ]] || { echo "https://$DOMAIN/api/health returned $public_code" >&2; exit 1; }

rm -f "$BUILD_DIR/.deploying"
python3 "$BUILD_DIR/prune-releases.py" "$REMOTE_APP" --apply \
  --health-url "http://127.0.0.1:$AGENT_PORT/api/health" \
  --health-url "http://127.0.0.1:$WORKSPACE_PORT/health"

echo "==> Done. Release $RELEASE_ID is live at https://$DOMAIN"
