#!/usr/bin/env bash
# Fetch Azure Container App agent logs and highlight known LLM/API errors.
# Run from deploy/azure with: ./agent-logs.sh [--follow] [--tail N]
set -e
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

if ! command -v terraform &>/dev/null || ! command -v az &>/dev/null; then
  echo "Need terraform and az CLI. Run from deploy/azure after 'terraform init' and 'az login'."
  exit 1
fi

RG="$(terraform output -raw resource_group_name 2>/dev/null)" || { echo "Run terraform init and apply first."; exit 1; }
AGENT_APP="$(terraform output -raw agent_container_app_name 2>/dev/null)" || AGENT_APP=""

if [[ -z "$AGENT_APP" ]]; then
  echo "Could not get agent_container_app_name. Ensure Terraform has been applied."
  exit 1
fi

FOLLOW=""
TAIL="200"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --follow) FOLLOW="--follow"; shift ;;
    --tail)   TAIL="$2"; shift 2 ;;
    *)        echo "Usage: $0 [--follow] [--tail N]"; exit 1 ;;
  esac
done

echo "=== Agent app: $AGENT_APP (RG: $RG) ==="
echo "Looking for: response is nil | choice.Content is empty | DeploymentNotFound | 404 | all LLMs failed"
echo ""

if [[ -n "$FOLLOW" ]]; then
  az containerapp logs show --name "$AGENT_APP" --resource-group "$RG" --tail "$TAIL" $FOLLOW
else
  az containerapp logs show --name "$AGENT_APP" --resource-group "$RG" --tail "$TAIL" 2>/dev/null | tee /tmp/agent-logs.txt
  echo ""
  echo "--- Matches for known errors ---"
  grep -E "response is nil|choice\.Content is empty|DeploymentNotFound|all LLMs failed|responses API error \(status 404\)" /tmp/agent-logs.txt 2>/dev/null || true
fi
