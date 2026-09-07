#!/usr/bin/env bash
# Build-and-deploy Dominion (Hetzner) natively on the box itself.
#
# Run this AS the `dominion` user, ON the Dominion server — it clones/pulls
# the three public source repos there and builds with the box's own native
# Go toolchain (linux/amd64), instead of cross-compiling on a laptop and
# scp-ing the result over. That avoids two classes of mistake a manual
# cross-compiled deploy hit on 2026-09-07 (see below).
#
# Usage:
#   deploy-dominion.sh              # clone/pull, build, stage a new release. Does NOT touch `current` or restart anything.
#   deploy-dominion.sh --activate   # also flip `current` to the just-built release and restart dominion-agent, with a health-check and automatic rollback.
#
# Lessons baked in from a bad manual deploy this session:
#   1. agent_go's actual `go build` target is the module root (agent_go/,
#      package main in main.go) — NOT ./cmd/server, which is a *library*
#      package. `go build -o file ./cmd/server` does not error on a
#      non-main package; it silently writes an `ar` archive (.a format)
#      instead of an ELF executable, which then fails at exec() time with
#      a cryptic systemd `status=203/EXEC`. Building the module root, then
#      independently verifying the output is a real executable (`file` +
#      an actual --help invocation) before touching anything live, is not
#      optional.
#   2. frontend/public/runtime-config.js is a hand-maintained, per-deployment
#      file (Dominion's routes through dominion-gateway, sets
#      enabledProductSurfaces, etc.). `npm run build`'s own dist/ output
#      ships a *different*, generic dev-pointing runtime-config.js
#      (api/workspace URLs pointing at a developer's own 127.0.0.1 ports).
#      That generic one must never reach a release — always overwrite it
#      with the current release's copy after building.
#   3. Never flip `current` or restart a service on an unverified binary.
#      A 200 from `file` + `go build` exit 0 is not proof of a working
#      executable — actually running it is.
set -euo pipefail

SRC_ROOT="/srv/dominion/src"
GO_BIN="/srv/dominion/tools/go/bin/go"
RELEASES_ROOT="/srv/dominion/releases"
CURRENT_LINK="/srv/dominion/current"

REPO="$SRC_ROOT/mcp-agent-builder-go"
MLP="$SRC_ROOT/multi-llm-provider-go"
MCPAGENT="$SRC_ROOT/mcpagent"

ACTIVATE=0
if [[ "${1:-}" == "--activate" ]]; then
  ACTIVATE=1
fi

if [[ ! -x "$GO_BIN" ]]; then
  echo "FATAL: Go toolchain not found at $GO_BIN — install it first:" >&2
  echo "  curl -fsSL -o /tmp/go.tar.gz https://go.dev/dl/go1.27.1.linux-amd64.tar.gz" >&2
  echo "  tar -C /srv/dominion/tools -xzf /tmp/go.tar.gz" >&2
  exit 1
fi

mkdir -p "$SRC_ROOT"

echo "==> Syncing source repos (all public, no credentials needed)"
sync_repo() {
  local name="$1" dir="$2"
  if [[ -d "$dir/.git" ]]; then
    git -C "$dir" fetch origin main
    git -C "$dir" reset --hard origin/main
  else
    git clone --depth 50 "https://github.com/manishiitg/${name}.git" "$dir"
  fi
  echo "    $name -> $(git -C "$dir" rev-parse --short HEAD)"
}
sync_repo "coding-agent-loop" "$REPO"
sync_repo "multi-llm-provider-go" "$MLP"
sync_repo "mcpagent" "$MCPAGENT"

# workspace/ and mcpagent/'s own go.mod carry no `replace` directives (only
# agent_go/go.mod does), so without a go.work tying all three siblings
# together, building them here would resolve multi-llm-provider-go/mcpagent
# from the public Go module proxy instead of the exact commits just synced
# above. Mirrors deploy/aws-ec2/deploy-rootless.sh's approach.
GOWORK_FILE="$SRC_ROOT/go.work"
rm -f "$GOWORK_FILE" # regenerate fresh each run so it can never point at a stale sibling state
(cd "$SRC_ROOT" && "$GO_BIN" work init "$REPO/agent_go" "$REPO/workspace" "$MCPAGENT" "$MLP")

RELEASE_ID="$(git -C "$REPO" rev-parse --short HEAD)-$(date +%Y%m%d%H%M%S)"
RELEASE_DIR="$RELEASES_ROOT/$RELEASE_ID"
mkdir -p "$RELEASE_DIR/bin" "$RELEASE_DIR/configs" "$RELEASE_DIR/frontend"
ln -sfn /srv/dominion/logs "$RELEASE_DIR/logs"
echo "==> Staging release $RELEASE_ID at $RELEASE_DIR"

export GOWORK="$GOWORK_FILE"

echo "==> Building dominion-agent (agent_go/ module root — the actual main package)"
(cd "$REPO/agent_go" && "$GO_BIN" build -o "$RELEASE_DIR/bin/dominion-agent" .)

echo "==> Building dominion-workspace"
(cd "$REPO/workspace" && "$GO_BIN" build -o "$RELEASE_DIR/bin/dominion-workspace" .)

echo "==> Building video-studio-landlock-runner (hardcoded filename regardless of product — workspace/security/landlock_policy.go resolves it by this exact name)"
(cd "$REPO/workspace" && "$GO_BIN" build -o "$RELEASE_DIR/bin/video-studio-landlock-runner" ./cmd/landlock-runner)

echo "==> Building mcpbridge"
(cd "$MCPAGENT" && "$GO_BIN" build -o "$RELEASE_DIR/bin/mcpbridge" ./cmd/mcpbridge)

echo "==> Building dominion-gateway (generic cookie-auth gateway, shared source with Video Studio)"
"$GO_BIN" build -o "$RELEASE_DIR/bin/dominion-gateway" "$REPO/deploy/aws-ec2/server/auth-gateway.go"

echo "==> Verifying every binary is a real, runnable executable (not just 'go build exit 0')"
for bin in dominion-agent dominion-workspace video-studio-landlock-runner mcpbridge dominion-gateway; do
  path="$RELEASE_DIR/bin/$bin"
  chmod +x "$path"
  filetype="$(file -b "$path")"
  if [[ "$filetype" != *"ELF"* ]]; then
    echo "FATAL: $bin is not a valid ELF executable (got: $filetype)" >&2
    exit 1
  fi
done
if ! "$RELEASE_DIR/bin/dominion-agent" --help >/dev/null 2>&1; then
  echo "FATAL: dominion-agent built but --help failed to run — do not deploy this" >&2
  exit 1
fi
echo "    all 5 binaries verified as real, runnable ELF executables"

echo "==> Building frontend"
(cd "$REPO/frontend" && npm ci && VITE_API_BASE_URL='' VITE_WORKSPACE_API_URL=/api/wp npm run build)
cp -R "$REPO/frontend/dist/." "$RELEASE_DIR/frontend/"
node "$REPO/frontend/scripts/check-release-assets.mjs" "$RELEASE_DIR/frontend"

echo "==> Restoring the hand-maintained runtime-config.js from the current release (see lesson #2 above)"
if [[ -f "$CURRENT_LINK/frontend/runtime-config.js" ]]; then
  cp "$CURRENT_LINK/frontend/runtime-config.js" "$RELEASE_DIR/frontend/runtime-config.js"
  echo "    restored from $CURRENT_LINK/frontend/runtime-config.js"
else
  echo "WARNING: no existing runtime-config.js found at $CURRENT_LINK — this release ships whatever npm run build produced. Verify it by hand before activating." >&2
fi

echo "==> Copying configs/ unchanged from the current release"
if [[ -d "$CURRENT_LINK/configs" ]]; then
  cp -a "$CURRENT_LINK/configs/." "$RELEASE_DIR/configs/"
fi

echo ""
echo "Release staged: $RELEASE_DIR"
echo "Currently active: $(readlink -f "$CURRENT_LINK" 2>/dev/null || echo none)"

if [[ "$ACTIVATE" -ne 1 ]]; then
  echo ""
  echo "Not activated (pass --activate to flip + restart). To activate this exact release later:"
  echo "  ln -sfn $RELEASE_DIR $CURRENT_LINK && systemctl --user restart dominion-agent"
  exit 0
fi

PREVIOUS_RELEASE="$(readlink -f "$CURRENT_LINK" 2>/dev/null || true)"
echo ""
echo "==> Activating: flipping $CURRENT_LINK -> $RELEASE_DIR and restarting dominion-agent"
ln -sfn "$RELEASE_DIR" "$CURRENT_LINK"
systemctl --user restart dominion-agent
sleep 3

if curl -fsS -o /dev/null -w '' http://127.0.0.1:21000/api/health; then
  echo "==> Health check passed. Active release: $(readlink -f "$CURRENT_LINK")"
else
  echo "FATAL: health check failed after restart — rolling back to $PREVIOUS_RELEASE" >&2
  if [[ -n "$PREVIOUS_RELEASE" ]]; then
    ln -sfn "$PREVIOUS_RELEASE" "$CURRENT_LINK"
    systemctl --user restart dominion-agent
    sleep 3
    curl -fsS -o /dev/null http://127.0.0.1:21000/api/health && echo "    rollback successful, service healthy again" || echo "    ROLLBACK ALSO FAILED — needs manual intervention" >&2
  fi
  exit 1
fi
