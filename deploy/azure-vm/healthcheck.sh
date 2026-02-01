#!/usr/bin/env bash
# Health check for services running on the Azure VM (through nginx).
# Usage: ./healthcheck.sh [base_url]
# If no args, reads FQDN from terraform output.

set -e

if [ $# -ge 1 ]; then
  BASE_URL="$1"
else
  SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  cd "$SCRIPT_DIR"
  VM_FQDN="$(terraform output -raw vm_fqdn 2>/dev/null || true)"
  if [ -z "$VM_FQDN" ]; then
    echo "Run from deploy/azure-vm after 'terraform apply', or pass base URL:"
    echo "  $0 <base_url>"
    exit 1
  fi
  BASE_URL="http://${VM_FQDN}"
fi

echo "Health checks against: $BASE_URL"
echo "(expect HTTP 200 and healthy status)"
echo ""

PASS=0
FAIL=0

check() {
  local label="$1"
  local url="$2"
  local expect_in_body="${3:-}"

  echo -n "$label: "
  RESP=$(curl -sS -m 15 -w "\n%{http_code}" "$url" 2>/dev/null) || { echo "FAIL (connection error)"; FAIL=$((FAIL+1)); return; }
  CODE=$(echo "$RESP" | tail -1)
  BODY=$(echo "$RESP" | sed '$d')

  if [ "$CODE" = "200" ]; then
    if [ -n "$expect_in_body" ] && ! echo "$BODY" | grep -q "$expect_in_body"; then
      echo "FAIL (HTTP $CODE but missing '$expect_in_body' in body)"
      FAIL=$((FAIL+1))
    else
      echo "OK (HTTP $CODE)"
      PASS=$((PASS+1))
    fi
  else
    echo "FAIL (HTTP $CODE)"
    FAIL=$((FAIL+1))
  fi
}

check "1. Nginx health" "$BASE_URL/health" "healthy"
check "2. Frontend (HTML)" "$BASE_URL" "<!DOCTYPE"
check "3. Agent API health" "$BASE_URL/api/health" ""
check "4. Workspace API health" "$BASE_URL/workspace-api/health" ""

echo ""
echo "Results: $PASS passed, $FAIL failed"

if [ "$FAIL" -gt 0 ]; then
  echo ""
  echo "Troubleshooting:"
  echo "  SSH into VM: $(cd "$SCRIPT_DIR" 2>/dev/null && terraform output -raw ssh_command 2>/dev/null || echo 'ssh azureuser@<vm>')"
  echo "  Agent logs:       journalctl -u mcpagent -f"
  echo "  Workspace logs:   journalctl -u workspace-api -f"
  echo "  Nginx logs:       journalctl -u nginx -f"
  echo "  Cloud-init:       cat /var/log/cloud-init-output.log"
  exit 1
fi
