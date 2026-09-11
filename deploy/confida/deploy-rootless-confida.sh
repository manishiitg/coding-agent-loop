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
# redeploy path.
#
# Build location: like RTS's deploy-rootless.sh, THIS SCRIPT DOES NOT BUILD
# ANYTHING. It only sends instructions (repo URLs, branch) to confida@HOST,
# which clones fresh and builds natively there (Linux x86_64, matching the
# runtime host exactly -- no cross-compile, and no local go/node/npm
# requirement on whichever machine triggers a deploy). The actual build+
# activate logic lives in server-build-and-activate.sh, INSIDE the repo, so
# it always runs whatever version is on the deployed branch, never a stale
# copy cached on the triggering machine. See server-bootstrap-build.sh for
# the on-server clone step and server-build-and-activate.sh for the build.
#
# Source of truth: like RTS, this builds ONLY from a fresh, depth-1 clone of
# each repo's `main` branch on the git remote -- not whatever happens to be
# sitting in the local working tree (uncommitted or untracked files, or a
# dirty sibling checkout, can never enter a release). Set
# DEPLOY_SOURCE_MODE=local to instead rsync the local working trees to the
# server and build those -- fast iteration only, never for a release you
# intend to keep.
#
# Usage: ./deploy-rootless-confida.sh
# Env overrides: HOST_IP, SSH_PORT, SSH_KEY_PATH, DEPLOY_SOURCE_MODE, DEPLOY_BRANCH

HOST_IP="${HOST_IP:-116.202.210.102}"
SSH_PORT="${SSH_PORT:-2299}"
SSH_KEY_PATH="${SSH_KEY_PATH:-$HOME/.ssh/confida_deploy}"
REMOTE_APP="/srv/confida"
REMOTE_TOOLS="$REMOTE_APP/tools"
REMOTE_RUNTIME_PATH="$REMOTE_TOOLS/bin:$REMOTE_APP/home/.local/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
DEPLOY_SOURCE_MODE="${DEPLOY_SOURCE_MODE:-remote-main}"
DEPLOY_BRANCH="${DEPLOY_BRANCH:-main}"

LOCAL_SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
LOCAL_REPO_ROOT="$(cd "$LOCAL_SCRIPT_DIR/../.." && pwd)"     # mcp-agent-builder-go (local checkout)
LOCAL_WORKSPACE_ROOT="$(cd "$LOCAL_REPO_ROOT/.." && pwd)"    # sibling repos (mcpagent, ...)

[[ "$DEPLOY_SOURCE_MODE" == "remote-main" || "$DEPLOY_SOURCE_MODE" == "local" ]] || {
  echo "DEPLOY_SOURCE_MODE must be remote-main or local" >&2
  exit 1
}
test -d "$LOCAL_WORKSPACE_ROOT/mcpagent/.git" || { echo "Expected sibling checkout: $LOCAL_WORKSPACE_ROOT/mcpagent" >&2; exit 1; }
test -d "$LOCAL_WORKSPACE_ROOT/multi-llm-provider-go/.git" || { echo "Expected sibling checkout: $LOCAL_WORKSPACE_ROOT/multi-llm-provider-go" >&2; exit 1; }

# The triggering machine no longer builds anything, so it no longer needs
# go/node/npm -- only enough to talk to git remotes and to the server.
for cmd in git rsync ssh; do
  command -v "$cmd" >/dev/null || { echo "Missing $cmd" >&2; exit 1; }
done

SSH_OPTS=(-p "$SSH_PORT" -i "$SSH_KEY_PATH" -o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new)
SSH=(ssh "${SSH_OPTS[@]}" "confida@$HOST_IP")
RSYNC_SSH="ssh ${SSH_OPTS[*]}"

# jq is not needed by this script itself -- it's a runtime dependency of
# agent-authored shell scripts, which the MCP bridge guidance (mcp-bridge.md)
# explicitly tells agents to use for encoding JSON tool-call payloads. A
# fresh confida host had no jq at all, so every such script failed. Checked
# (not installed) here: installing it needs root, which this script does not
# assume it has -- see the printed remedy below instead of failing the deploy.
echo "==> Checking for jq on the remote host (required by agent shell scripts, not by this script)"
if "${SSH[@]}" 'command -v jq' >/dev/null 2>&1; then
  echo "    jq is present."
else
  echo "    WARNING: jq is NOT installed on $HOST_IP. Agent shell scripts that rely on it will fail." >&2
  echo "    Install it once as root: ssh -p $SSH_PORT root@$HOST_IP 'apt-get install -y jq'" >&2
fi

# agent-browser is a mandatory runtime dependency: preview_report and every
# browser-automation tool shell out to it by name with no PATH check of their
# own, so a missing install surfaces only as an opaque "exit code 127:
# agent-browser: not found" deep inside a tool call. Unlike jq, it needs no
# root (a per-user npm global prefix is enough). Confida owns a stable tools
# prefix outside releases so the CLI survives release pruning and every
# deploy can safely ensure/update it. A missing mandatory browser runtime is
# fatal: activating a release that cannot execute browser tools is not a
# successful deployment.
echo "==> Ensuring agent-browser is installed on the remote host"
"${SSH[@]}" "set -e
  install -d -m 0755 '$REMOTE_TOOLS'
  npm install -g --prefix '$REMOTE_TOOLS' agent-browser@latest >/dev/null
  export PATH='$REMOTE_TOOLS/bin':\"\$PATH\"
  command -v agent-browser >/dev/null
  agent-browser --version"

JOB="confida-deploy-$(date +%Y%m%d%H%M%S)-$$"
REMOTE_JOB="$REMOTE_APP/builds/$JOB"
STAGING="$(mktemp -d)"
chmod 700 "$STAGING"
cleanup() { rm -rf "$STAGING"; "${SSH[@]}" "rm -rf '$REMOTE_JOB'" >/dev/null 2>&1 || true; }
trap cleanup EXIT

cp "$LOCAL_SCRIPT_DIR/server-bootstrap-build.sh" "$STAGING/bootstrap-build.sh"
printf '%s\n' "$DEPLOY_BRANCH" > "$STAGING/branch"
"${SSH[@]}" "install -d -m 0700 '$REMOTE_JOB'"

# The confida account has no SSH deploy key or github.com host-key trust
# set up for git@github.com -- cloning over the origin SSH remote failed
# outright ("Host key verification failed", live 2026-09-11). All three repos
# are public, so rewrite to an anonymous HTTPS URL instead (matching RTS's
# deploy-rootless.sh intent), handling both the `git@host:owner/repo.git`
# shorthand and the `ssh://git@host/owner/repo.git` form this repo actually
# uses.
to_https_url() {
  local url="$1"
  url="${url/ssh:\/\/git@github.com\//https://github.com/}"
  url="${url/git@github.com:/https://github.com/}"
  url="${url%.git}"
  [[ "$url" =~ ^https://github\.com/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || { echo "Unsupported repository URL: $1" >&2; exit 1; }
  printf '%s\n' "$url"
}

if [[ "$DEPLOY_SOURCE_MODE" == "remote-main" ]]; then
  echo "==> Resolving git remotes for $DEPLOY_BRANCH (mcp-agent-builder-go, mcpagent, multi-llm-provider-go)"
  {
    to_https_url "$(git -C "$LOCAL_REPO_ROOT" remote get-url origin)"
    to_https_url "$(git -C "$LOCAL_WORKSPACE_ROOT/mcpagent" remote get-url origin)"
    to_https_url "$(git -C "$LOCAL_WORKSPACE_ROOT/multi-llm-provider-go" remote get-url origin)"
  } > "$STAGING/repos"
  rsync -az -e "$RSYNC_SSH" "$STAGING/" "confida@$HOST_IP:$REMOTE_JOB/"
else
  echo "==> WARNING: shipping local working trees to the server (DEPLOY_SOURCE_MODE=local) -- not for a release you intend to keep"
  rsync -az -e "$RSYNC_SSH" "$STAGING/" "confida@$HOST_IP:$REMOTE_JOB/"
  "${SSH[@]}" "mkdir -p '$REMOTE_JOB/source'"
  for repo in mcp-agent-builder-go mcpagent multi-llm-provider-go; do
    case "$repo" in
      mcp-agent-builder-go) local_dir="$LOCAL_REPO_ROOT" ;;
      *) local_dir="$LOCAL_WORKSPACE_ROOT/$repo" ;;
    esac
    "${SSH[@]}" "mkdir -p '$REMOTE_JOB/source/$repo'"
    rsync -az --filter=':- .gitignore' --exclude .git -e "$RSYNC_SSH" "$local_dir/" "confida@$HOST_IP:$REMOTE_JOB/source/$repo/"
  done
fi

echo "==> Building on confida@$HOST_IP: cloning/using $DEPLOY_BRANCH and building natively"
# Throttled well below the box's 16 cores / 62G RAM (confirmed 2026-09-11):
# this box also runs RTS and Dominion, each under its own account, and a full
# go+npm build must not starve their live services while it runs.
"${SSH[@]}" "systemd-run --user --quiet --wait --pipe --unit='$JOB' -p MemoryMax=6G -p CPUQuota=300% -p Nice=10 bash '$REMOTE_JOB/bootstrap-build.sh' '$REMOTE_JOB'"

echo "==> Verifying"
curl -fsSI "https://confida.agentworkshq.com/login" | head -1
"${SSH[@]}" "set -e
  export PATH='$REMOTE_RUNTIME_PATH'
  command -v agent-browser >/dev/null
  for unit in confida-agent confida-workspace; do
    pid=\$(systemctl --user show \"\$unit\" -p MainPID --value)
    test \"\$pid\" -gt 0
    tr '\\0' '\\n' < \"/proc/\$pid/environ\" | grep -Fqx 'PATH=$REMOTE_RUNTIME_PATH'
  done"

echo "==> Done."
