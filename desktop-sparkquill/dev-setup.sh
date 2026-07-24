#!/usr/bin/env bash
# Build everything the Electron shell needs into resources/, then install its
# own deps. Run this once before `npm start`, and again after changing the Go
# server or the frontend.
#
# CI does the same steps inline (see .github/workflows/sparkquill-desktop.yml);
# this exists so a local run is one command and stays in sync with it.
set -euo pipefail

cd "$(dirname "$0")"
ROOT="$(cd .. && pwd)"

echo "==> Building the frontend"
(cd "$ROOT/frontend/learning-app" && npm ci --silent && npm run build)

echo "==> Staging the frontend into resources/web"
rm -rf resources/web
mkdir -p resources/web
cp -R "$ROOT/frontend/learning-app/dist/." resources/web/

echo "==> Building family-server"
(cd "$ROOT/agent_go" && go build -o "$OLDPWD/resources/family-server" ./cmd/family-server/)

echo "==> Installing Electron deps"
npm install --silent

echo
echo "Ready. Start it with:  npm start"
