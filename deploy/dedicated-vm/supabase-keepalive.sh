#!/usr/bin/env bash
# Ping Supabase daily to prevent free-tier project pausing (pauses after 7 days of inactivity).
# Installed as a cron job by quick-deploy.sh when AUTH_PROVIDERS includes "supabase".

ENV_FILE="/opt/mcp-agent/.env"

if [ ! -f "$ENV_FILE" ]; then
  echo "$(date): .env not found, skipping" >> /data/logs/supabase-keepalive.log
  exit 0
fi

source "$ENV_FILE"

if [[ "$AUTH_PROVIDERS" != *"supabase"* ]] || [ -z "$SUPABASE_URL" ] || [ -z "$SUPABASE_ANON_KEY" ]; then
  exit 0
fi

STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
  "$SUPABASE_URL/auth/v1/health" \
  -H "apikey: $SUPABASE_ANON_KEY")

echo "$(date): Supabase ping → $STATUS" >> /data/logs/supabase-keepalive.log

if [ "$STATUS" != "200" ]; then
  echo "$(date): WARNING - unexpected status $STATUS" >&2
fi
