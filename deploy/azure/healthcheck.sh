#!/usr/bin/env bash
# Health check for deployed Container Apps. Run from deploy/azure after terraform apply.
# Usage: ./healthcheck.sh [frontend_url] [agent_url] [workspace_api_url]
# If no args, reads URLs from terraform output.
# Frontend is a Container App (nginx serving built app); GET /health returns JSON.

set -e

if [ $# -ge 3 ]; then
  FRONTEND_URL="$1"
  AGENT_URL="$2"
  WORKSPACE_API_URL="$3"
else
  SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  cd "$SCRIPT_DIR"
  FRONTEND_URL="$(terraform output -raw frontend_fqdn 2>/dev/null || true)"
  AGENT_URL="$(terraform output -raw agent_fqdn 2>/dev/null || true)"
  WORKSPACE_API_URL="$(terraform output -raw workspace_api_fqdn 2>/dev/null || true)"
  if [ -z "$FRONTEND_URL" ] || [ -z "$AGENT_URL" ] || [ -z "$WORKSPACE_API_URL" ]; then
    echo "Run from deploy/azure after 'terraform apply', or pass three URLs:"
    echo "  $0 <frontend_url> <agent_url> <workspace_api_url>"
    exit 1
  fi
fi

echo "Health checks (expect HTTP 200 and healthy status):"
echo ""

echo "1. Frontend GET / (container) - fetch website"
BODY=$(curl -sS -m 30 -w "\n%{http_code}" "$FRONTEND_URL" 2>/dev/null) || { echo "   Failed to reach frontend"; BODY=""; }
if [ -n "$BODY" ]; then
  CODE=$(echo "$BODY" | tail -1)
  HTML=$(echo "$BODY" | sed '$d' | head -30)
  echo "   HTTP $CODE"
  echo "   Response (first 30 lines):"
  echo "$HTML" | sed 's/^/   /'
  if echo "$HTML" | grep -q -E '<!DOCTYPE|<html'; then
    echo "   OK: website (HTML) received"
  fi
else
  echo "   (no response)"
fi
echo ""

echo "2. Agent GET /api/health"
curl -sS -w "\n   HTTP %{http_code}\n" "$AGENT_URL/api/health" | head -10
echo ""

echo "3. Workspace API GET /health"
curl -sS -w "\n   HTTP %{http_code}\n" "$WORKSPACE_API_URL/health" | head -5
echo ""

echo "Done."
