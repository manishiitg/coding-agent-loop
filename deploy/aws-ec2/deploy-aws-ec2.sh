#!/usr/bin/env bash
set -euo pipefail

AWS_PROFILE_NAME="${AWS_PROFILE_NAME:-RTS}"
AWS_REGION="${AWS_REGION:-us-west-2}"
STACK_NAME="${STACK_NAME:-video-studio-prod}"
DOMAIN_NAME="${DOMAIN_NAME:-video.realtrainingsys.com}"
PUBLIC_HOSTED_ZONE_ID="${PUBLIC_HOSTED_ZONE_ID:-Z02500993JAB1GQYSENG0}"
INSTANCE_TYPE="${INSTANCE_TYPE:-t3.xlarge}"
ACCESS_PASSWORD="${ACCESS_PASSWORD:-}"
SSH_KEY_NAME="${SSH_KEY_NAME:-video-studio-deploy}"
SSH_KEY_PATH="${SSH_KEY_PATH:-/Users/mipl/.ssh/id_ed25519}"
SSH_INGRESS_CIDR="${SSH_INGRESS_CIDR:-$(curl -4 -fsSL https://checkip.amazonaws.com | tr -d '\n')/32}"
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
GLOBAL_SECRETS_FILE="${GLOBAL_SECRETS_FILE:-$SCRIPT_DIR/global-secrets.env}"
GLOBAL_SECRETS_SECRET_ID="${GLOBAL_SECRETS_SECRET_ID:-video-studio/global-secrets}"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
WORKSPACE_ROOT="$(cd "$REPO_ROOT/.." && pwd)"

aws_rts() { aws --profile "$AWS_PROFILE_NAME" --region "$AWS_REGION" "$@"; }
stack_output() {
  aws_rts cloudformation describe-stacks --stack-name "$STACK_NAME" \
    --query "Stacks[0].Outputs[?OutputKey==\`$1\`].OutputValue | [0]" --output text
}

for command in aws go npm tar rsync ssh jq; do
  command -v "$command" >/dev/null || { echo "Missing required command: $command" >&2; exit 1; }
done

AMI_ID="${AMI_ID:-$(aws_rts ec2 describe-images --owners 099720109477 --filters 'Name=name,Values=ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-amd64-server-*' 'Name=state,Values=available' --query 'reverse(sort_by(Images,&CreationDate))[0].ImageId' --output text)}"
test "$AMI_ID" != "None" && test -n "$AMI_ID"

aws_rts cloudformation validate-template --template-body "file://$SCRIPT_DIR/template.yaml" >/dev/null
stack_status="$(aws_rts cloudformation describe-stacks --stack-name "$STACK_NAME" --query 'Stacks[0].StackStatus' --output text 2>/dev/null || true)"
if [ "$stack_status" = "ROLLBACK_COMPLETE" ]; then
  aws_rts cloudformation delete-stack --stack-name "$STACK_NAME"
  aws_rts cloudformation wait stack-delete-complete --stack-name "$STACK_NAME"
fi
aws_rts cloudformation deploy \
  --stack-name "$STACK_NAME" \
  --template-file "$SCRIPT_DIR/template.yaml" \
  --capabilities CAPABILITY_IAM \
  --no-fail-on-empty-changeset \
  --parameter-overrides \
    DomainName="$DOMAIN_NAME" PublicHostedZoneId="$PUBLIC_HOSTED_ZONE_ID" \
    AmiId="$AMI_ID" InstanceType="$INSTANCE_TYPE" KeyName="$SSH_KEY_NAME" SshIngressCidr="$SSH_INGRESS_CIDR"

INSTANCE_ID="$(stack_output InstanceId)"

HOST_IP="$(stack_output ElasticIp)"
echo "Waiting for $INSTANCE_ID to accept restricted SSH connections..."
for _ in $(seq 1 90); do
  ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new -o ConnectTimeout=5 -i "$SSH_KEY_PATH" "ubuntu@$HOST_IP" true 2>/dev/null && break
  sleep 10
done
ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new -o ConnectTimeout=5 -i "$SSH_KEY_PATH" "ubuntu@$HOST_IP" true

# Keep the shared login stable across ordinary releases. A password is generated
# only for the first deployment (or when explicitly supplied by the operator).
if [ -z "$ACCESS_PASSWORD" ]; then
  ACCESS_PASSWORD="$(ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new -i "$SSH_KEY_PATH" "ubuntu@$HOST_IP" "sudo sed -n 's/^ACCESS_PASSWORD=//p' /opt/video-studio/.env 2>/dev/null | head -n 1" || true)"
fi
if [ -z "$ACCESS_PASSWORD" ]; then
  ACCESS_PASSWORD="$(openssl rand -base64 24 | tr -d '\n' | tr '/+' 'AB')"
  GENERATED_ACCESS_PASSWORD=true
else
  GENERATED_ACCESS_PASSWORD=false
fi
ACCESS_PASSWORD_B64="$(printf %s "$ACCESS_PASSWORD" | base64)"

RELEASE_ID="$(git -C "$REPO_ROOT" rev-parse --short HEAD)-$(date +%Y%m%d%H%M%S)"
BUILD_DIR="$(mktemp -d)"
ARCHIVE_PATH="$(mktemp /tmp/video-studio-release.XXXXXX)"
trap 'rm -rf "$BUILD_DIR" "$ARCHIVE_PATH"' EXIT
mkdir -p "$BUILD_DIR/bin" "$BUILD_DIR/frontend" "$BUILD_DIR/configs" "$BUILD_DIR/server"

(cd "$WORKSPACE_ROOT" && GOWORK="$WORKSPACE_ROOT/go.work" GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "$BUILD_DIR/bin/video-studio-agent" ./mcp-agent-builder-go/agent_go)
(cd "$WORKSPACE_ROOT" && GOWORK="$WORKSPACE_ROOT/go.work" GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "$BUILD_DIR/bin/video-studio-workspace" ./mcp-agent-builder-go/workspace)
# Claude Code communicates with the hosted agent only through this local
# stdio-to-HTTP bridge. Ship it alongside the agent instead of depending on a
# separate Go installation or a user's PATH on the EC2 instance.
(cd "$WORKSPACE_ROOT" && GOWORK="$WORKSPACE_ROOT/go.work" GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "$BUILD_DIR/bin/mcpbridge" ./mcpagent/cmd/mcpbridge)
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "$BUILD_DIR/bin/video-studio-gateway" "$SCRIPT_DIR/server/auth-gateway.go"
(cd "$REPO_ROOT/frontend" && VITE_API_BASE_URL='' VITE_WORKSPACE_API_URL=/api/wp npm run build)
cp -R "$REPO_ROOT/frontend/dist/." "$BUILD_DIR/frontend/"
node "$REPO_ROOT/frontend/scripts/check-release-assets.mjs" "$BUILD_DIR/frontend"
cp "$REPO_ROOT/frontend/scripts/check-release-assets.mjs" "$BUILD_DIR/check-release-assets.mjs"
cp "$REPO_ROOT/deploy/common/prune-releases.py" "$BUILD_DIR/prune-releases.py"
# The shared public runtime-config.js is rewritten by local AgentWorks dev
# sessions. Replace it after every build so this browser deployment always
# uses Caddy's same-origin routes rather than a visitor's localhost ports.
install -m 0644 "$SCRIPT_DIR/server/runtime-config.js" "$BUILD_DIR/frontend/runtime-config.js"
# This EC2 product is intentionally isolated from the developer workstation's
# broad MCP catalog. Product tools are provided through the bundled mcpbridge;
# start with no external MCP servers and add only deployment-approved ones.
install -m 0644 "$SCRIPT_DIR/server/mcp_servers_video_studio.json" "$BUILD_DIR/configs/mcp_servers_video_studio.json"
cp -R "$SCRIPT_DIR/server/." "$BUILD_DIR/server/"
chmod +x "$BUILD_DIR/server/install-release.sh"
REMOTE_DIR="/tmp/video-studio-release-$RELEASE_ID"
SSH=(ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new -i "$SSH_KEY_PATH" "ubuntu@$HOST_IP")
rsync -az -e "ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new -i $SSH_KEY_PATH" "$BUILD_DIR/" "ubuntu@$HOST_IP:$REMOTE_DIR/"

# Global deployment secrets use GLOBAL_SECRET_<NAME>=<value> entries. Keep the
# local file out of the release archive (which is intentionally readable by the
# unprivileged app account) and copy it to an ephemeral 0600 upload instead.
REMOTE_GLOBAL_SECRETS_FILE=""
GLOBAL_SECRETS_SOURCE_FILE=""
if aws_rts secretsmanager describe-secret --secret-id "$GLOBAL_SECRETS_SECRET_ID" >/dev/null 2>&1; then
  GLOBAL_SECRETS_SOURCE_FILE="$(mktemp)"
  trap 'rm -rf "$BUILD_DIR" "$ARCHIVE_PATH" "$GLOBAL_SECRETS_SOURCE_FILE"' EXIT
  aws_rts secretsmanager get-secret-value --secret-id "$GLOBAL_SECRETS_SECRET_ID" --query SecretString --output text \
    | jq -er 'if type != "object" then error("secret must be a JSON object") else to_entries[] | select(.key | test("^[A-Z0-9_]+$")) | select(.value | type == "string" and length > 0) | "GLOBAL_SECRET_\(.key)=\(.value)" end' \
    > "$GLOBAL_SECRETS_SOURCE_FILE"
  if [ ! -s "$GLOBAL_SECRETS_SOURCE_FILE" ]; then
    echo "AWS Secrets Manager secret $GLOBAL_SECRETS_SECRET_ID contains no usable global secrets." >&2
    exit 1
  fi
elif [ -f "$GLOBAL_SECRETS_FILE" ]; then
  GLOBAL_SECRETS_SOURCE_FILE="$GLOBAL_SECRETS_FILE"
fi
if [ -n "$GLOBAL_SECRETS_SOURCE_FILE" ]; then
  if grep -Ev '^[[:space:]]*(#|$)|^GLOBAL_SECRET_[A-Z0-9_]+=.+$' "$GLOBAL_SECRETS_SOURCE_FILE" | grep -q .; then
    echo "Invalid global secret file. Use GLOBAL_SECRET_UPPER_SNAKE_CASE=value entries only." >&2
    exit 1
  fi
  REMOTE_GLOBAL_SECRETS_FILE="/tmp/video-studio-global-secrets-$RELEASE_ID.env"
  rsync -az --chmod=ugo=,u=rw -e "ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new -i $SSH_KEY_PATH" "$GLOBAL_SECRETS_SOURCE_FILE" "ubuntu@$HOST_IP:$REMOTE_GLOBAL_SECRETS_FILE"
fi

remote_install="sudo install -d -m 0755 /opt/video-studio/releases && sudo mv '$REMOTE_DIR' '/opt/video-studio/releases/$RELEASE_ID' && sudo env RELEASE_DIR='/opt/video-studio/releases/$RELEASE_ID' DOMAIN_NAME='$DOMAIN_NAME' ACCESS_PASSWORD_B64='$ACCESS_PASSWORD_B64'"
if [ -n "$REMOTE_GLOBAL_SECRETS_FILE" ]; then
  remote_install+=" GLOBAL_SECRETS_FILE='$REMOTE_GLOBAL_SECRETS_FILE'"
fi
remote_install+=" '/opt/video-studio/releases/$RELEASE_ID/server/install-release.sh'"
if [ -n "$REMOTE_GLOBAL_SECRETS_FILE" ]; then
  remote_install+="; status=\$?; rm -f '$REMOTE_GLOBAL_SECRETS_FILE'; exit \$status"
fi
"${SSH[@]}" "$remote_install"

echo "Video Studio deployed: $(stack_output PublicUrl)"
if [ "$GENERATED_ACCESS_PASSWORD" = true ]; then
  echo "Initial Video Studio access password: $ACCESS_PASSWORD"
fi
