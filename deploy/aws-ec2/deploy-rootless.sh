#!/usr/bin/env bash
# Send only deployment instructions/secrets. Clone and build all repos on Linux.
set -euo pipefail
AWS_PROFILE_NAME="${AWS_PROFILE_NAME:-RTS}"
AWS_REGION="${AWS_REGION:-us-west-2}"
STACK_NAME="${STACK_NAME:-video-studio-prod}"
SSH_KEY_PATH="${SSH_KEY_PATH:-$HOME/.ssh/id_ed25519}"
GLOBAL_SECRETS_SECRET_ID="${GLOBAL_SECRETS_SECRET_ID:-video-studio/global-secrets}"
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
[[ "${DEPLOY_BRANCH:-main}" == main && "${DEPLOY_SOURCE_MODE:-remote-main}" == remote-main ]] || { echo 'Production deployment requires main from all three repositories.' >&2; exit 1; }
for command in aws git jq rsync ssh; do command -v "$command" >/dev/null || { echo "Missing $command" >&2; exit 1; }; done
aws_rts() { aws --profile "$AWS_PROFILE_NAME" --region "$AWS_REGION" "$@"; }
HOST_IP="$(aws_rts cloudformation describe-stacks --stack-name "$STACK_NAME" --query 'Stacks[0].Outputs[?OutputKey==`ElasticIp`].OutputValue | [0]' --output text)"
JOB="deploy-$(date +%Y%m%d%H%M%S)-$$"
REMOTE_JOB="/var/lib/video-studio/video-studio/builds/$JOB"
STAGING="$(mktemp -d)"
chmod 700 "$STAGING"
SSH=(ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new -i "$SSH_KEY_PATH" "video-studio@$HOST_IP")
cleanup() { rm -rf "$STAGING"; "${SSH[@]}" "rm -rf '$REMOTE_JOB'" >/dev/null 2>&1 || true; }
trap cleanup EXIT
for repo in mcp-agent-builder-go mcpagent multi-llm-provider-go; do
  url="$(git -C "$REPO_ROOT/../$repo" remote get-url origin)"
  url="${url/git@github.com:/https://github.com/}"
  [[ "$url" =~ ^https://github.com/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || { echo "Unsupported repository URL for $repo" >&2; exit 1; }
  printf '%s\n' "$url" >> "$STAGING/repos"
done
aws_rts secretsmanager get-secret-value --secret-id "$GLOBAL_SECRETS_SECRET_ID" --query SecretString --output text \
 | jq -er 'to_entries[] | select(.key | test("^[A-Z0-9_]+$")) | select(.value | type == "string" and length > 0) | if .key == "CLAUDE_CODE_OAUTH_TOKEN" or .key == "CURSOR_API_KEY" then "\(.key)=\(.value)" else "GLOBAL_SECRET_\(.key)=\(.value)" end' > "$STAGING/globals"
chmod 600 "$STAGING/globals"
cp "$SCRIPT_DIR/server/bootstrap-build.sh" "$STAGING/bootstrap-build.sh"
printf '%s\n' "${DRAIN_TIMEOUT_SECONDS:-3600}" > "$STAGING/drain-timeout"
"${SSH[@]}" "install -d -m 0700 '$REMOTE_JOB'"
rsync -az -e "ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new -i $SSH_KEY_PATH" "$STAGING/" "video-studio@$HOST_IP:$REMOTE_JOB/"
echo 'Server cloning main from all three repositories and building the release locally.'
# Keep builds below half the host's RAM and two CPU cores while tests keep running.
"${SSH[@]}" "systemd-run --user --quiet --wait --pipe --unit='$JOB' -p MemoryMax=6G -p CPUQuota=200% -p Nice=10 bash '$REMOTE_JOB/bootstrap-build.sh' '$REMOTE_JOB'"
