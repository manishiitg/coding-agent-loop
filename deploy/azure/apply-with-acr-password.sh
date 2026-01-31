#!/usr/bin/env bash
# Run terraform apply with ACR admin password from Azure (no need to put password in tfvars).
# Use when skip_acr_managed_identity = true. Requires: az CLI logged in, terraform init already run.
# Usage: ./apply-with-acr-password.sh [terraform apply args...]

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

ACR_NAME="$(terraform output -raw acr_name 2>/dev/null || grep -E '^\s*acr_name\s*=' terraform.tfvars 2>/dev/null | sed -E 's/.*=\s*["]?([^"]+)["]?.*/\1/' | tr -d ' ')"
if [ -z "$ACR_NAME" ]; then
  echo "Could not get acr_name from terraform output or terraform.tfvars. Run 'terraform init' and ensure ACR exists, or pass acr name: ACR_NAME=myacr $0"
  exit 1
fi

echo "Getting ACR password for $ACR_NAME..."
export TF_VAR_acr_admin_password
TF_VAR_acr_admin_password="$(az acr credential show -n "$ACR_NAME" -o tsv --query 'passwords[0].value' 2>/dev/null)" || {
  echo "Failed to get ACR password. Run 'az login' and ensure you have access to ACR $ACR_NAME."
  exit 1
}

echo "Running terraform apply..."
exec terraform apply "$@"
