#!/bin/bash
# Schema drift check — Go is the canonical source of truth for the report
# plan and event schemas. The frontend types are auto-generated from JSON
# Schema artifacts emitted by `agent_go/cmd/schema-gen` and compiled by
# `frontend/scripts/generate-event-types.mjs`.
#
# This check fails commits that:
#   - edit Go schema sources without regenerating, or
#   - hand-edit the generated TS / JSON Schema files.
#
# It only runs when staged files plausibly affect the generated artifacts —
# editing unrelated files imposes zero overhead.
#
# Triggered by .git/hooks/pre-commit; safe to invoke directly.

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

REPO_ROOT="$(git rev-parse --show-toplevel)"

# Paths whose changes might affect the generated schemas / types.
RELEVANT_PATTERN='^(agent_go/cmd/schema-gen/|agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/report_plan_helpers\.go|agent_go/pkg/(events|orchestrator/events)/|frontend/scripts/generate-event-types\.mjs|agent_go/schemas/|frontend/src/generated/)'

STAGED=$(git diff --cached --name-only --diff-filter=ACMR | grep -E "$RELEVANT_PATTERN" || true)
if [ -z "$STAGED" ]; then
    exit 0
fi

echo -e "${BLUE}🔁 Checking schema drift (staged schema sources detected)…${NC}"

# Generated artifacts the check verifies.
GENERATED_FILES=(
    "agent_go/schemas/report-plan.schema.json"
    "agent_go/schemas/polling-event.schema.json"
    "agent_go/schemas/unified-events-complete.schema.json"
    "frontend/src/generated/report-plan.ts"
    "frontend/src/generated/events.ts"
    "frontend/src/generated/events-bridge.ts"
)

# Snapshot current generated content to a temp dir so we can compare after
# regeneration.
SNAPSHOT_DIR=$(mktemp -d)
trap "rm -rf $SNAPSHOT_DIR" EXIT
for f in "${GENERATED_FILES[@]}"; do
    src="$REPO_ROOT/$f"
    if [ -f "$src" ]; then
        mkdir -p "$SNAPSHOT_DIR/$(dirname "$f")"
        cp "$src" "$SNAPSHOT_DIR/$f"
    fi
done

# Regenerate.
if ! (cd "$REPO_ROOT/agent_go" && GOWORK=off go run ./cmd/schema-gen >/tmp/schema-gen.log 2>&1); then
    echo -e "${RED}❌ schema-gen failed. See /tmp/schema-gen.log${NC}"
    tail -30 /tmp/schema-gen.log
    exit 1
fi

if [ ! -d "$REPO_ROOT/frontend/node_modules" ]; then
    echo -e "${YELLOW}⚠️  frontend/node_modules missing — skipping TS regen, schema diff will be incomplete.${NC}"
else
    if ! (cd "$REPO_ROOT/frontend" && npm run types:generate >/tmp/types-generate.log 2>&1); then
        echo -e "${RED}❌ npm run types:generate failed. See /tmp/types-generate.log${NC}"
        tail -30 /tmp/types-generate.log
        exit 1
    fi
fi

# Compare snapshot to regenerated. Any diff means the staged tree doesn't
# match what regen would produce — either Go was edited without regenerating,
# or a generated file was hand-edited.
DRIFT_FILES=()
for f in "${GENERATED_FILES[@]}"; do
    snap="$SNAPSHOT_DIR/$f"
    live="$REPO_ROOT/$f"
    if [ -f "$live" ]; then
        if [ ! -f "$snap" ] || ! diff -q "$snap" "$live" >/dev/null 2>&1; then
            DRIFT_FILES+=("$f")
        fi
    fi
done

if [ ${#DRIFT_FILES[@]} -gt 0 ]; then
    echo -e "${RED}❌ Schema drift detected in:${NC}"
    for f in "${DRIFT_FILES[@]}"; do
        echo "   - $f"
    done
    echo ""
    echo "Either Go schema sources were edited without regenerating, or a"
    echo "generated file was hand-edited."
    echo ""
    echo "To fix: run 'cd frontend && npm run types:generate', stage the"
    echo "regenerated files, and commit again."
    exit 1
fi

echo -e "${GREEN}✅ Schemas in sync.${NC}"
