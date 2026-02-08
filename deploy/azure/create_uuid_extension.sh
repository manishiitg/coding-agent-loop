#!/usr/bin/env bash
# Create uuid-ossp extension in the mcpagent database (Step 2 after allowlisting via Azure CLI).
# Run from deploy/azure. Requires psql and Terraform output (postgres_fqdn).
# Password: set TF_VAR_postgres_admin_password, or you will be prompted.

set -e
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

FQDN="$(terraform output -raw postgres_fqdn 2>/dev/null)" || {
  echo "Error: could not get postgres_fqdn. Run from deploy/azure after 'terraform apply'." >&2
  exit 1
}

if [[ -z "${TF_VAR_postgres_admin_password:-}" ]]; then
  echo -n "Database admin password (postgres_admin_password): "
  read -rs PGPASSWORD
  echo
  export PGPASSWORD
fi

PGPASSWORD="${PGPASSWORD:-$TF_VAR_postgres_admin_password}" psql \
  "host=${FQDN} port=5432 dbname=mcpagent user=pgadmin sslmode=require" \
  -c 'CREATE EXTENSION IF NOT EXISTS "uuid-ossp";'

echo "Done. Restart the agent (new revision or ./deploy.sh agent --local) so migrations run."
