#!/usr/bin/env bash
set -euo pipefail

: "${RELEASE_DIR:?}"
node "$RELEASE_DIR/check-release-assets.mjs" "$RELEASE_DIR/frontend"
: "${DOMAIN_NAME:?}"
: "${ACCESS_PASSWORD_B64:?}"
ACCESS_PASSWORD="$(printf %s "$ACCESS_PASSWORD_B64" | base64 --decode)"
test -n "$ACCESS_PASSWORD"

if ! id -u video-studio >/dev/null 2>&1; then
  useradd --system --create-home --home-dir /var/lib/video-studio --shell /usr/sbin/nologin video-studio
fi

runuser -u video-studio -- env HOME=/var/lib/video-studio npx --yes hyperframes@0.8.6 browser ensure >/dev/null
browser_path="$(runuser -u video-studio -- env HOME=/var/lib/video-studio npx --yes hyperframes@0.8.6 browser path | tail -n 1)"
case "$browser_path" in
  /var/lib/video-studio/.cache/hyperframes/chrome/*/chrome-headless-shell) ;;
  *) echo "Unexpected HyperFrames browser path: $browser_path" >&2; exit 1 ;;
esac
browser_wrapper="$(dirname "$browser_path")/agentworks-chrome-headless"
install -m 0755 "$RELEASE_DIR/server/chrome-headless-wrapper.sh" "$browser_wrapper"

install -d -m 0755 /opt/video-studio /data/video-studio/{docs,workspace-db,agent-db,logs,caddy-data,caddy-config}
install -d -o video-studio -g video-studio -m 0755 /var/lib/video-studio
# Releases contain executable code and static configuration only. Make the
# active release traversable/readable to the unprivileged application account;
# credentials remain separately stored in the root-owned 0600 environment file.
chmod -R a+rX "$RELEASE_DIR"
# The agent needs to read the encrypted provider token and write its own
# working state. These paths belong only to this isolated Video Studio host.
chown -R video-studio:video-studio /data/video-studio
chown -R 10001:10001 /data/video-studio/caddy-data /data/video-studio/caddy-config
ln -sfn "$RELEASE_DIR" /opt/video-studio/current
auth_secret="$(sed -n 's/^AUTH_SECRET=//p' /opt/video-studio/.env 2>/dev/null | head -n 1 || true)"
if [ -z "$auth_secret" ]; then
  auth_secret="$(openssl rand -hex 32)"
fi

# Preserve the previous deployment-managed global secrets unless this release
# explicitly supplies a replacement file. This file never enters a release
# directory or app-readable path; it is merged only into root-owned .env.
global_secrets_tmp="$(mktemp)"
trap 'rm -f "$global_secrets_tmp"' EXIT
if [ -n "${GLOBAL_SECRETS_FILE:-}" ]; then
  test -r "$GLOBAL_SECRETS_FILE"
  if grep -Ev '^[[:space:]]*(#|$)|^GLOBAL_SECRET_[A-Z0-9_]+=.+$' "$GLOBAL_SECRETS_FILE" | grep -q .; then
    echo "Invalid global secret file. Use GLOBAL_SECRET_UPPER_SNAKE_CASE=value entries only." >&2
    exit 1
  fi
  grep '^GLOBAL_SECRET_' "$GLOBAL_SECRETS_FILE" > "$global_secrets_tmp" || true
else
  sed -n '/^GLOBAL_SECRET_/p' /opt/video-studio/.env 2>/dev/null > "$global_secrets_tmp" || true
fi

printf '%s\n' \
  'MULTI_USER_MODE=false' \
  'WORKSPACE_API_URL=http://127.0.0.1:8080' \
  'WORKSPACE_DOCS_PATH=/data/video-studio/docs' \
  'DB_PATH=/data/video-studio/workspace-db/workspace.db' \
  'DOCS_DIR=/data/video-studio/docs' \
  "AGENT_BROWSER_EXECUTABLE_PATH=$browser_wrapper" \
  'AGENT_PROVIDER=openai' \
  'AGENT_MODEL=gpt-5.2' \
  "AUTH_SECRET=$auth_secret" \
  "ACCESS_PASSWORD=$ACCESS_PASSWORD" \
  > /opt/video-studio/.env
cat "$global_secrets_tmp" >> /opt/video-studio/.env
chmod 0600 /opt/video-studio/.env

sed "s|__DOMAIN__|$DOMAIN_NAME|g" "$RELEASE_DIR/server/Caddyfile" > /opt/video-studio/Caddyfile
cp "$RELEASE_DIR/server/docker-compose.yml" /opt/video-studio/docker-compose.yml
cp "$RELEASE_DIR/server/Dockerfile.caddy" /opt/video-studio/Dockerfile.caddy
cp "$RELEASE_DIR/server/video-studio-agent.service" /etc/systemd/system/video-studio-agent.service
cp "$RELEASE_DIR/server/video-studio-workspace.service" /etc/systemd/system/video-studio-workspace.service
cp "$RELEASE_DIR/server/video-studio-gateway.service" /etc/systemd/system/video-studio-gateway.service

install -m 0644 "$RELEASE_DIR/server/video-studio-userns.apparmor" /etc/apparmor.d/video-studio-userns
apparmor_parser -r /etc/apparmor.d/video-studio-userns

docker build -t video-studio-frontend:latest -f "$RELEASE_DIR/server/Dockerfile.frontend" "$RELEASE_DIR"
cd /opt/video-studio
docker compose up -d --force-recreate
systemctl daemon-reload
systemctl enable video-studio-workspace video-studio-agent video-studio-gateway
systemctl restart video-studio-workspace video-studio-agent video-studio-gateway
