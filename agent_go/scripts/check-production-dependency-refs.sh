#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
dockerfile="$repo_root/agent_go/Dockerfile"

grep -q '^ARG MCPAGENT_REF=main$' "$dockerfile"
grep -q '^ARG MULTI_LLM_PROVIDER_REF=main$' "$dockerfile"
grep -q 'github.com/manishiitg/mcpagent@${MCPAGENT_REF}' "$dockerfile"
grep -q 'github.com/manishiitg/multi-llm-provider-go@${MULTI_LLM_PROVIDER_REF}' "$dockerfile"

echo 'Production coding-agent dependencies default to main.'
