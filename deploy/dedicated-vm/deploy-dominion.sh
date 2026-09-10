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

# This script runs over a plain non-interactive `ssh host 'cmd'` exec, which
# does not source .bashrc/.profile (those only run for interactive/login
# shells) and never loads /srv/dominion/.env's PATH= line either -- that file
# is only read by systemd via EnvironmentFile=. So /srv/dominion/tools/bin
# (where claude/gog/gws/surge/agent-browser all install to) is not on PATH
# here unless this script puts it there itself. 2026-09-09: `command -v
# agent-browser` failed right after a successful install for exactly this
# reason.
export PATH="/srv/dominion/tools/bin:$PATH"

REPO="$SRC_ROOT/mcp-agent-builder-go"
MLP="$SRC_ROOT/multi-llm-provider-go"
MCPAGENT="$SRC_ROOT/mcpagent"

ACTIVATE=0
command -v python3 >/dev/null || { echo 'Missing python3 (required for release cleanup)' >&2; exit 1; }
if [[ "${1:-}" == "--activate" ]]; then
  ACTIVATE=1
fi

if [[ ! -x "$GO_BIN" ]]; then
  echo "FATAL: Go toolchain not found at $GO_BIN — install it first:" >&2
  echo "  curl -fsSL -o /tmp/go.tar.gz https://go.dev/dl/go1.27.1.linux-amd64.tar.gz" >&2
  echo "  tar -C /srv/dominion/tools -xzf /tmp/go.tar.gz" >&2
  exit 1
fi

echo "==> Ensuring agent-browser is installed (preview_report and every browser-automation tool shell out to it by name; nothing checks it's on PATH until it fails at runtime)"
# Dominion's default npm prefix is /usr (root-owned; confirmed 2026-09-08 via
# `npm config get prefix`), which the rootless `dominion` user cannot write to
# -- npm install -g without --prefix fails with EACCES. /srv/dominion/tools is
# the box's established per-user tool prefix (already on PATH via .env, same
# place claude/gog/gws/surge are symlinked from).
if ! npm install --prefix /srv/dominion/tools -g agent-browser@latest 2>&1 | tail -5; then
  echo "FATAL: npm install -g agent-browser@latest failed" >&2
  exit 1
fi
if ! command -v agent-browser >/dev/null 2>&1; then
  echo "FATAL: agent-browser still not on PATH after install — check npm's global bin dir is on PATH for the dominion-agent service user" >&2
  exit 1
fi
echo "    agent-browser: $(agent-browser --version 2>&1)"

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

# This script's own on-disk copy at /srv/dominion/deploy-dominion.sh is what
# actually runs -- it does NOT auto-update from the repo just synced above.
# 2026-09-09: a real deploy silently ran a copy that predated agent-browser
# provisioning, the static/report-preview fix, and the CDP-disable work by a
# full day, because nothing ever re-copied it after those changes landed.
# Replace the file atomically: Bash may still be reading this run's original
# inode. The next invocation gets the new script without changing this run.
SELF_SOURCE="$REPO/deploy/dedicated-vm/deploy-dominion.sh"
SELF_TARGET="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/$(basename "${BASH_SOURCE[0]}")"
if ! cmp -s "$SELF_SOURCE" "$SELF_TARGET"; then
  echo "==> Updating this script's own copy at $SELF_TARGET from the repo just synced (takes effect next run)"
  cp "$SELF_SOURCE" "$SELF_TARGET.next"
  chmod +x "$SELF_TARGET.next"
  mv "$SELF_TARGET.next" "$SELF_TARGET"
fi
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
touch "$RELEASE_DIR/.deploying"
trap 'rm -f "$RELEASE_DIR/.deploying"' EXIT
cp "$REPO/deploy/common/prune-releases.py" "$RELEASE_DIR/prune-releases.py"
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

# frontend's build:report-preview step (part of `npm run build` above) writes
# report-preview.js to agent_go/cmd/server/static/ in the SOURCE checkout, not
# into the release. dominion-agent runs with WorkingDirectory=$CURRENT_LINK
# and resolves staticFrontendDir()'s default ("./static/") against that cwd,
# so without this copy preview_report always 503s with "Report preview
# runtime is missing" no matter how many times the frontend gets built.
mkdir -p "$RELEASE_DIR/static"
cp -R "$REPO/agent_go/cmd/server/static/." "$RELEASE_DIR/static/"

echo "==> Restoring the hand-maintained runtime-config.js from the current release (see lesson #2 above)"
if [[ -f "$CURRENT_LINK/frontend/runtime-config.js" ]]; then
  cp "$CURRENT_LINK/frontend/runtime-config.js" "$RELEASE_DIR/frontend/runtime-config.js"
  echo "    restored from $CURRENT_LINK/frontend/runtime-config.js"
else
  echo "WARNING: no existing runtime-config.js found at $CURRENT_LINK — this release ships whatever npm run build produced. Verify it by hand before activating." >&2
fi
if grep -q 'cdpEnabled:' "$RELEASE_DIR/frontend/runtime-config.js"; then
  sed -i -E 's/cdpEnabled:[[:space:]]*(true|false)/cdpEnabled: false/' "$RELEASE_DIR/frontend/runtime-config.js"
else
  sed -i '/window.__APP_RUNTIME_CONFIG__ = {/a\  cdpEnabled: false,' "$RELEASE_DIR/frontend/runtime-config.js"
fi
grep -Fq 'cdpEnabled: false' "$RELEASE_DIR/frontend/runtime-config.js" || {
  echo "FATAL: Dominion runtime config does not display CDP as disabled" >&2
  exit 1
}

echo "==> Copying configs/ unchanged from the current release"
if [[ -d "$CURRENT_LINK/configs" ]]; then
  cp -a "$CURRENT_LINK/configs/." "$RELEASE_DIR/configs/"
fi

echo ""
echo "Release staged: $RELEASE_DIR"
echo "Currently active: $(readlink -f "$CURRENT_LINK" 2>/dev/null || echo none)"

if [[ "$ACTIVATE" -ne 1 ]]; then
  # Keep this candidate until the next build/activation; repeated staging
  # must not accumulate an archive of every earlier candidate.
  if [[ -d "$CURRENT_LINK" ]]; then
    rm -f "$RELEASE_DIR/.deploying"
    python3 "$RELEASE_DIR/prune-releases.py" /srv/dominion --apply --keep "$RELEASE_ID" \
      --health-url http://127.0.0.1:21000/api/health --health-url http://127.0.0.1:21001/health
  fi
  echo ""
  echo "Not activated (pass --activate to flip + restart). To activate this exact release later:"
  echo "  ln -sfn $RELEASE_DIR $CURRENT_LINK && systemctl --user restart dominion-agent"
  exit 0
fi

PREVIOUS_RELEASE="$(readlink -f "$CURRENT_LINK" 2>/dev/null || true)"
echo ""
echo "==> Activating: flipping $CURRENT_LINK -> $RELEASE_DIR and restarting dominion-workspace, dominion-agent"
ln -sfn "$RELEASE_DIR" "$CURRENT_LINK"
mkdir -p "$HOME/.config/systemd/user/dominion-agent.service.d"
printf '%s\n' '[Service]' 'Environment=AGENT_BROWSER_CDP_ENABLED=false' > "$HOME/.config/systemd/user/dominion-agent.service.d/20-disable-cdp.conf"
mkdir -p "$HOME/.config/systemd/user/dominion-workspace.service.d"
printf '%s\n' '[Service]' 'Environment=AGENT_BROWSER_CDP_ENABLED=false' > "$HOME/.config/systemd/user/dominion-workspace.service.d/20-disable-cdp.conf"
systemctl --user daemon-reload
# dominion-agent depends on dominion-workspace (After=dominion-workspace.service
# in its unit), so restart it first -- and it must actually be restarted here:
# until now this script only ever restarted dominion-agent, so dominion-workspace
# kept running the PREVIOUS release's binary (and the previous env) indefinitely
# after every deploy, CDP-disable drop-in included.
systemctl --user restart dominion-workspace
sleep 2
if ! systemctl --user is-active --quiet dominion-workspace; then
  echo "FATAL: dominion-workspace failed to start on the new release — rolling back to $PREVIOUS_RELEASE" >&2
  if [[ -n "$PREVIOUS_RELEASE" ]]; then
    ln -sfn "$PREVIOUS_RELEASE" "$CURRENT_LINK"
    systemctl --user restart dominion-workspace
    sleep 2
    systemctl --user is-active --quiet dominion-workspace && echo "    rollback successful, dominion-workspace active again" || echo "    ROLLBACK ALSO FAILED — needs manual intervention" >&2
  fi
  exit 1
fi
systemctl --user restart dominion-agent
sleep 3

if curl -fsS -o /dev/null -w '' http://127.0.0.1:21000/api/health; then
  echo "==> Health check passed. Active release: $(readlink -f "$CURRENT_LINK")"
else
  echo "FATAL: health check failed after restart — rolling back to $PREVIOUS_RELEASE" >&2
  if [[ -n "$PREVIOUS_RELEASE" ]]; then
    ln -sfn "$PREVIOUS_RELEASE" "$CURRENT_LINK"
    systemctl --user restart dominion-workspace
    sleep 2
    systemctl --user restart dominion-agent
    sleep 3
    curl -fsS -o /dev/null http://127.0.0.1:21000/api/health && echo "    rollback successful, service healthy again" || echo "    ROLLBACK ALSO FAILED — needs manual intervention" >&2
  fi
  exit 1
fi

rm -f "$RELEASE_DIR/.deploying"
python3 "$RELEASE_DIR/prune-releases.py" /srv/dominion --apply \
  --health-url http://127.0.0.1:21000/api/health --health-url http://127.0.0.1:21001/health
