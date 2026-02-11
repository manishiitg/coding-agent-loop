#!/usr/bin/env bash
# Test Grafana MCP via the agent API.
# Usage: ./scripts/test_grafana_mcp_api.sh [BASE_URL] [AUTH_TOKEN]
# Example: ./scripts/test_grafana_mcp_api.sh http://localhost:8000
#          ./scripts/test_grafana_mcp_api.sh http://localhost:8000 "Bearer <jwt>"

set -e
BASE_URL="${1:-http://localhost:8000}"
AUTH_TOKEN="${2:-}"
API="${BASE_URL}/api"

echo "=== Testing Grafana MCP via API (base: $API) ==="

CURL_AUTH=()
if [[ -n "$AUTH_TOKEN" ]]; then
  CURL_AUTH=(-H "Authorization: $AUTH_TOKEN")
  echo "(Using provided auth token)"
else
  echo "(No token: using single-user mode or unauthenticated)"
fi

echo ""
echo "1. GET /api/tools — list all MCP servers (check for 'grafana')"
curl -s "${CURL_AUTH[@]}" "$API/tools" | jq -r '.[] | select(.name == "grafana") | "\(.name): status=\(.status), tools=\(.toolsEnabled)"' 2>/dev/null || curl -s "${CURL_AUTH[@]}" "$API/tools" | jq '.[] | select(.name == "grafana")'

echo ""
echo "2. GET /api/tools/detail?server_name=grafana — discover Grafana MCP tools (timeout 90s)"
DETAIL=$(curl -s --max-time 90 "${CURL_AUTH[@]}" "$API/tools/detail?server_name=grafana")
if echo "$DETAIL" | jq -e '.' >/dev/null 2>&1; then
  if echo "$DETAIL" | jq -e '.error' >/dev/null 2>&1; then
    echo "Error from server: $(echo "$DETAIL" | jq -r '.error')"
  else
    echo "Status: $(echo "$DETAIL" | jq -r '.status')"
    echo "Tools: $(echo "$DETAIL" | jq -r '.function_names | join(", ")')"
  fi
else
  echo "Response (not JSON): ${DETAIL:0:300}"
fi

echo ""
echo "3. POST /api/mcp/execute — run list_datasources (Grafana MCP, timeout 60s)"
EXEC=$(curl -s --max-time 60 -X POST "${CURL_AUTH[@]}" -H "Content-Type: application/json" "$API/mcp/execute" \
  -d '{"server":"grafana","tool":"list_datasources","args":{}}')
if echo "$EXEC" | jq -e '.error' >/dev/null 2>&1; then
  echo "Execution error: $(echo "$EXEC" | jq -r '.error')"
  echo "$EXEC" | jq .
else
  echo "Result (first 500 chars):"
  echo "$EXEC" | jq -r '.content[0].text // .result // .' 2>/dev/null | head -c 500
  echo ""
  echo ""
  echo "Full response:"
  echo "$EXEC" | jq .
fi

echo ""
echo "=== Done ==="
