#!/usr/bin/env bash
# Runs on confida@116.202.210.102 only, inside a scratch job dir shipped by
# deploy-rootless-confida.sh. Mirrors deploy/aws-ec2/server/bootstrap-build.sh
# (RTS): the local machine sends only instructions (repo URLs / branch), this
# clones fresh on the target host, then hands off to the actual build script
# living INSIDE that fresh checkout -- so the build logic itself always tracks
# whatever is on the deployed branch, not a stale copy shipped from the caller.
set -euo pipefail
JOB="$1"
REMOTE_APP="/srv/confida"
export PATH="$HOME/.local/go/bin:$HOME/.local/bin:/usr/local/bin:/usr/bin:/bin"
[[ "$(uname -sm)" == "Linux x86_64" ]] || { echo "confida build must run on Linux x86_64" >&2; exit 1; }

# Serialize complete deployments (clone + build + activate + prune). This box
# also runs RTS and Dominion under their own separate accounts/locks; this
# lock only ever guards concurrent runs of THIS script against itself.
exec 9>"$REMOTE_APP/deploy.lock"
flock -n 9 || { echo 'Another confida deployment is already running.' >&2; exit 1; }
trap 'rm -rf "$JOB"' EXIT

if ! command -v go >/dev/null; then
  echo 'Installing the pinned Go toolchain on the server'
  curl --fail --location --silent --show-error https://go.dev/dl/go1.27.1.linux-amd64.tar.gz -o "$JOB/go.tar.gz"
  curl --fail --location --silent --show-error 'https://go.dev/dl/?mode=json&include=all' -o "$JOB/go-releases.json"
  python3 - "$JOB" <<'PY'
import hashlib,json,pathlib,sys
p=pathlib.Path(sys.argv[1])
expected=next(f['sha256'] for v in json.loads((p/'go-releases.json').read_text()) for f in v['files'] if f['filename']=='go1.27.1.linux-amd64.tar.gz')
assert hashlib.sha256((p/'go.tar.gz').read_bytes()).hexdigest()==expected, 'Go checksum mismatch'
PY
  mkdir -p "$HOME/.local"
  tar -xzf "$JOB/go.tar.gz" -C "$HOME/.local"
  rm -f "$JOB/go.tar.gz" "$JOB/go-releases.json"
fi

BRANCH="$(cat "$JOB/branch")"
mkdir -p "$JOB/source"
if [[ -f "$JOB/repos" ]]; then
  index=0
  for repo in mcp-agent-builder-go mcpagent multi-llm-provider-go; do
    index=$((index+1))
    url="$(sed -n "${index}p" "$JOB/repos")"
    echo "Cloning $repo/$BRANCH on $(hostname)"
    GIT_TERMINAL_PROMPT=0 git clone --quiet --depth 1 --single-branch --branch "$BRANCH" "$url" "$JOB/source/$repo"
  done
else
  # DEPLOY_SOURCE_MODE=local: the caller already rsynced local working trees
  # into "$JOB/source/<repo>" -- fast iteration only, never a release to keep.
  echo "Using rsynced local working trees as source (DEPLOY_SOURCE_MODE=local)"
  for repo in mcp-agent-builder-go mcpagent multi-llm-provider-go; do
    test -d "$JOB/source/$repo" || { echo "Expected rsynced source at $JOB/source/$repo" >&2; exit 1; }
  done
fi

bash "$JOB/source/mcp-agent-builder-go/deploy/confida/server-build-and-activate.sh" "$JOB/source"
