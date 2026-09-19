#!/usr/bin/env bash
set -euo pipefail

# Repeatable redeploy script for any fixed-workspace product running as its
# own isolated Linux account on a shared rootless-systemd Hetzner box.
#
# Generalized from the original Confida deployer. Confida and SparkQuill now
# both use this parameterized pipeline; target-specific settings live under
# products/<product>/ instead of being duplicated in deployment scripts.
#
# This host also runs other products, each under its own account and its own
# deploy path. This script only ever touches /srv/$PRODUCT and the
# $PRODUCT-* systemd --user units for the product named on the command line.
#
# Build location: like confida's script, THIS SCRIPT DOES NOT BUILD ANYTHING.
# It only sends instructions (repo URLs, branch, product name) to
# $PRODUCT@$HOST_IP, which clones fresh and builds natively there (Linux
# x86_64, matching the runtime host exactly). The actual build+activate logic
# lives in build-and-activate.sh, INSIDE the repo, so it always runs whatever
# version is on the deployed branch, never a stale copy cached on the
# triggering machine.
#
# Source of truth: builds ONLY from a fresh, depth-1 clone of each repo's
# `main` branch on the git remote -- not whatever happens to be sitting in
# the local working tree.
#
# Usage: ./deploy.sh <product>            # e.g. ./deploy.sh sparkquill
# Env overrides (see products/<product>/product.env for defaults):
#   HOST_IP, SSH_PORT, SSH_KEY_PATH, DEPLOY_BRANCH

PRODUCT="${1:?Usage: deploy.sh <product> (a directory under deploy/rootless-linux/products/)}"

LOCAL_SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
LOCAL_REPO_ROOT="$(cd "$LOCAL_SCRIPT_DIR/../.." && pwd)"     # mcp-agent-builder-go (local checkout)
LOCAL_WORKSPACE_ROOT="$(cd "$LOCAL_REPO_ROOT/.." && pwd)"    # sibling repos (mcpagent, ...)
PRODUCT_DIR="$LOCAL_SCRIPT_DIR/products/$PRODUCT"

test -f "$PRODUCT_DIR/product.env" || { echo "No such product: $PRODUCT_DIR/product.env not found" >&2; exit 1; }
# shellcheck disable=SC1091
source "$PRODUCT_DIR/product.env"
[[ "$PRODUCT" == "$(basename "$PRODUCT_DIR")" ]] || { echo "product.env PRODUCT=$PRODUCT does not match directory $(basename "$PRODUCT_DIR")" >&2; exit 1; }

DEPLOY_BRANCH="${DEPLOY_BRANCH:-main}"
REMOTE_APP="/srv/$PRODUCT"
REMOTE_TOOLS="$REMOTE_APP/tools"
REMOTE_RUNTIME_PATH="$REMOTE_TOOLS/node/bin:$REMOTE_TOOLS/bin:$REMOTE_APP/home/.local/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

test -d "$LOCAL_WORKSPACE_ROOT/mcpagent/.git" || { echo "Expected sibling checkout: $LOCAL_WORKSPACE_ROOT/mcpagent" >&2; exit 1; }
test -d "$LOCAL_WORKSPACE_ROOT/multi-llm-provider-go/.git" || { echo "Expected sibling checkout: $LOCAL_WORKSPACE_ROOT/multi-llm-provider-go" >&2; exit 1; }

# The triggering machine no longer builds anything, so it no longer needs
# go/node/npm -- only enough to talk to git remotes and to the server.
for cmd in git scp ssh; do
  command -v "$cmd" >/dev/null || { echo "Missing $cmd" >&2; exit 1; }
done

SSH_OPTS=(-p "$SSH_PORT" -i "$SSH_KEY_PATH" -o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new)
SSH=(ssh "${SSH_OPTS[@]}" "$PRODUCT@$HOST_IP")
SCP=(scp -P "$SSH_PORT" -i "$SSH_KEY_PATH" -o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new)

echo "==> [$PRODUCT] Checking deployment configuration"
"${SSH[@]}" "PRODUCT=$PRODUCT EXPECTED_PUBLIC_URL=${EXPECTED_PUBLIC_URL:-} python3 - preflight" < "$LOCAL_SCRIPT_DIR/deployment_checks.py"

echo "==> [$PRODUCT] Checking for jq on the remote host (required by agent shell scripts)"
if "${SSH[@]}" 'command -v jq' >/dev/null 2>&1; then
  echo "    jq is present."
else
  echo "    WARNING: jq is NOT installed on $HOST_IP. Agent shell scripts that rely on it will fail." >&2
  echo "    Install it once as root: ssh -p $SSH_PORT root@$HOST_IP 'apt-get install -y jq'" >&2
fi

if [[ -n "${PIN_NODE_VERSION:-}" ]]; then
  echo "==> [$PRODUCT] Ensuring pinned Node $PIN_NODE_VERSION is installed"
  test -n "${PIN_NODE_SHA256:-}" || { echo "PIN_NODE_VERSION is set but PIN_NODE_SHA256 is not" >&2; exit 1; }
  "${SSH[@]}" "set -e
    install -d -m 0755 '$REMOTE_TOOLS'
    node_release='node-v$PIN_NODE_VERSION-linux-x64'
    node_dir='$REMOTE_TOOLS/node-v$PIN_NODE_VERSION'
    if [ ! -x \"\$node_dir/bin/node\" ]; then
      node_stage=\$(mktemp -d '$REMOTE_TOOLS/.node-install.XXXXXX')
      trap 'rm -rf \"\$node_stage\"' EXIT
      curl -fsSL \"https://nodejs.org/dist/v$PIN_NODE_VERSION/\$node_release.tar.xz\" -o \"\$node_stage/\$node_release.tar.xz\"
      printf '%s  %s\\n' '$PIN_NODE_SHA256' \"\$node_stage/\$node_release.tar.xz\" | sha256sum -c -
      tar -xJf \"\$node_stage/\$node_release.tar.xz\" -C \"\$node_stage\"
      mv \"\$node_stage/\$node_release\" \"\$node_dir\"
      rm -f \"\$node_stage/\$node_release.tar.xz\"
      rmdir \"\$node_stage\"
      trap - EXIT
    fi
    ln -sfn \"\$node_dir\" '$REMOTE_TOOLS/node'
    export PATH='$REMOTE_RUNTIME_PATH'
    test \"\$(node --version)\" = 'v$PIN_NODE_VERSION'
    npm --version"
fi

# WhatsApp voice notes arrive as Ogg/Opus. The shared speech engine consumes
# PCM WAV, so every rootless product needs an audio converter even when the
# host administrator has not installed the distro ffmpeg package. Keep the
# pinned binary outside releases so it survives normal deploy/prune cycles.
echo "==> [$PRODUCT] Ensuring ffmpeg is available for voice-note transcription"
if "${SSH[@]}" "export PATH='$REMOTE_RUNTIME_PATH'; command -v ffmpeg" >/dev/null 2>&1; then
  echo "    ffmpeg is present."
else
  "${SSH[@]}" "REMOTE_APP='$REMOTE_APP' python3 -" < "$LOCAL_SCRIPT_DIR/install-ffmpeg.py"
  "${SSH[@]}" "export PATH='$REMOTE_RUNTIME_PATH'; ffmpeg -version | head -n 1"
fi

# Browser automation and every advertised coding provider are installation
# dependencies, not something an end user is expected to install over SSH.
# Keep the binaries in stable, service-owned paths outside releases so
# credentials and CLI availability survive release pruning.
#
# Built as a plain variable, not a case statement nested inside $(...) inside
# an outer double-quoted string: bash's paren-matching for a case pattern's
# bare `)` breaks down in exactly that nesting, misreading the case body as
# closing the command substitution early.
# Install the pinned backend Slack CLI in the same persistent tools prefix.
"${SSH[@]}" "bash -s -- '$REMOTE_TOOLS'" < "$LOCAL_REPO_ROOT/agent_go/scripts/install-slack-cli.sh"
if [[ "${#CLI_TOOLS[@]}" -gt 0 ]]; then
  cli_install_cmd() {
    case "$1" in
      claude) printf "npm install -g --prefix '%s' @anthropic-ai/claude-code@latest >/dev/null" "$REMOTE_TOOLS" ;;
      codex)  printf "npm install -g --prefix '%s' @openai/codex@latest >/dev/null" "$REMOTE_TOOLS" ;;
      pi)     printf "npm install -g --prefix '%s' @earendil-works/pi-coding-agent@latest >/dev/null" "$REMOTE_TOOLS" ;;
      cursor) printf "HOME='%s/home' curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' --tlsv1.2 https://cursor.com/install | HOME='%s/home' bash" "$REMOTE_APP" "$REMOTE_APP" ;;
      muse)   printf "HOME='%s/home' MUSE_INSTALL_DIR='%s/home/.local/bin' MUSE_NO_MODIFY_PATH=1 bash -c \"curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' --tlsv1.2 https://dev.meta.ai/install.sh | bash\"" "$REMOTE_APP" "$REMOTE_APP" ;;
      *) echo "Unknown CLI_TOOLS entry: $1" >&2; exit 1 ;;
    esac
  }
  cli_bin_name() { [[ "$1" == cursor ]] && echo cursor-agent || echo "$1"; }

  install_lines=""
  check_lines=""
  for cli in "${CLI_TOOLS[@]}"; do
    install_lines+="$(cli_install_cmd "$cli")"$'\n'
    check_lines+="command -v '$(cli_bin_name "$cli")' >/dev/null"$'\n'
  done

  echo "==> [$PRODUCT] Installing server CLI dependencies (agent-browser, ${CLI_TOOLS[*]})"
  "${SSH[@]}" "set -euo pipefail
    install -d -m 0755 '$REMOTE_TOOLS' '$REMOTE_APP/home/.local/bin'
    export PATH='$REMOTE_RUNTIME_PATH'
    npm install -g --prefix '$REMOTE_TOOLS' --allow-scripts=agent-browser agent-browser@latest >/dev/null
    $install_lines
    export PATH='$REMOTE_TOOLS/bin':\"\$PATH\"
    export PATH='$REMOTE_APP/home/.local/bin':\"\$PATH\"
    command -v agent-browser >/dev/null
    command -v slack >/dev/null
    $check_lines
    agent-browser --version"
fi

JOB="$PRODUCT-deploy-$(date +%Y%m%d%H%M%S)-$$"
REMOTE_JOB="$REMOTE_APP/builds/$JOB"
STAGING="$(mktemp -d)"
chmod 700 "$STAGING"
cleanup() { rm -rf "$STAGING"; "${SSH[@]}" "rm -rf '$REMOTE_JOB'" >/dev/null 2>&1 || true; }
trap cleanup EXIT

cp "$LOCAL_SCRIPT_DIR/bootstrap-build.sh" "$STAGING/bootstrap-build.sh"
printf '%s\n' "$DEPLOY_BRANCH" > "$STAGING/branch"
printf '%s\n' "$PRODUCT" > "$STAGING/product"
"${SSH[@]}" "install -d -m 0700 '$REMOTE_JOB'"

# All three repos are public; use an anonymous HTTPS URL so a fresh account
# with no SSH deploy key for github.com can still clone (confida hit "Host
# key verification failed" on the SSH remote form, 2026-09-11).
to_https_url() {
  local url="$1"
  url="${url/ssh:\/\/git@github.com\//https://github.com/}"
  url="${url/git@github.com:/https://github.com/}"
  url="${url%.git}"
  [[ "$url" =~ ^https://github\.com/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || { echo "Unsupported repository URL: $1" >&2; exit 1; }
  printf '%s\n' "$url"
}

echo "==> [$PRODUCT] Resolving git remotes for $DEPLOY_BRANCH (mcp-agent-builder-go, mcpagent, multi-llm-provider-go)"
{
  to_https_url "$(git -C "$LOCAL_REPO_ROOT" remote get-url origin)"
  to_https_url "$(git -C "$LOCAL_WORKSPACE_ROOT/mcpagent" remote get-url origin)"
  to_https_url "$(git -C "$LOCAL_WORKSPACE_ROOT/multi-llm-provider-go" remote get-url origin)"
} > "$STAGING/repos"
"${SCP[@]}" "$STAGING/bootstrap-build.sh" "$STAGING/branch" "$STAGING/product" "$STAGING/repos" "$PRODUCT@$HOST_IP:$REMOTE_JOB/"

echo "==> [$PRODUCT] Building on $PRODUCT@$HOST_IP: cloning/using $DEPLOY_BRANCH and building natively"
# Throttled below the box's shared core/RAM budget: this box also runs other
# products, each under its own account, and a full go+npm build must not
# starve their live services while it runs.
"${SSH[@]}" "systemd-run --user --quiet --wait --pipe --unit='$JOB' -p MemoryMax=6G -p CPUQuota=300% -p Nice=10 bash '$REMOTE_JOB/bootstrap-build.sh' '$REMOTE_JOB'"

echo "==> [$PRODUCT] Verifying"
"${SSH[@]}" "PRODUCT=$PRODUCT EXPECTED_PUBLIC_URL=${EXPECTED_PUBLIC_URL:-} python3 - running" < "$LOCAL_SCRIPT_DIR/deployment_checks.py"
# Not `curl -f`: whether /api/health is reachable without auth depends on the
# gateway's own gate model (GATEWAY_DISABLE_PASSWORD_GATE in .env) -- a
# per-user-JWT deployment like confida exempts it (200), a shared-password
# deployment like sparkquill does not (401). Either is a live, correctly
# routed gateway; only a connection failure or a 5xx means something is wrong.
public_code="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 10 "https://$DOMAIN/api/health")"
echo "public /api/health: $public_code"
if [[ -n "${PUBLIC_HEALTH_STATUS:-}" ]]; then
  [[ "$public_code" == "$PUBLIC_HEALTH_STATUS" ]] || { echo "https://$DOMAIN/api/health returned $public_code, expected $PUBLIC_HEALTH_STATUS" >&2; exit 1; }
else
  [[ "$public_code" -lt 500 ]] || { echo "https://$DOMAIN/api/health returned $public_code" >&2; exit 1; }
fi
for path in "${PUBLIC_CHECK_PATHS[@]:-}"; do
  [[ -z "$path" ]] || curl -fsS -o /dev/null --max-time 10 "https://$DOMAIN$path"
done

echo "==> [$PRODUCT] Done."
