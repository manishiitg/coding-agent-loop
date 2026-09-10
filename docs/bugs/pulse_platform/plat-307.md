[← Pulse platform index](../pulse_platform_issue_register.md)

# PLAT-307 — A manual workflow run could search, install, or remove MCP servers mid-execution

| Coordination | Value |
|---|---|
| Assigned agent | Claude |
| Ticket state | Implemented, regression-tested locally — deployment pending |
| Last synchronized | 2026-09-10 |
| Priority | P2 — unattended runtime capability creep, not a live incident; no evidence of it being exploited or triggered |

## Problem

`registerMultiAgentMCPServerTools` (`search_mcp_catalog`, `inspect_mcp_server`,
`install_mcp_server`, `add_mcp_server`, `edit_mcp_server`, `remove_mcp_server`,
`get_mcp_server_logs`, `trigger_mcp_discovery`) is meant to be a workflow-builder
/chat-time capability only — installing, removing, or discovering MCP servers
is a configuration decision, not something a running automation should do to
itself.

The actual gate (`server.go:5495`, `isToolBackedChat := !isWorkflowPhase`, where
`isWorkflowPhase := req.AgentMode == "workflow_phase"`) only excludes the
literal string `"workflow_phase"`. There are two separate "running a workflow"
code paths, and only one of them uses that string:

- **Scheduled/cron runs** (`scheduler.go:4204`, `buildWorkshopRequest`) set
  `agent_mode: "workflow_phase"` (distinguished internally from real builder
  chat by `execution_options.workshop_mode: "run"`) — correctly excluded.
- **A manual Run-button click** (`frontend/src/components/workflow/hooks/useWorkflowExecution.ts:185,256`,
  the "Execute workflow for preset..." / "Execute step..." requests) sets
  `agent_mode: 'workflow'` — a different string, not `"workflow_phase"`. This
  fell through `isWorkflowPhase == false` → `isToolBackedChat == true`, so the
  full MCP server management toolset was registered for a manual run.

Net effect: triggering a workflow manually gave that run's agent the ability to
search GitHub's MCP Registry/Smithery, install a new (unvetted) MCP server with
an OAuth flow or API key, or delete an existing one from the account-wide
config — all live, during what's supposed to be execution of an already-built
automation, with no human at the builder UI to review any of it. Scheduled runs
of the identical workflow never had this exposure, purely because of which
internal request-builder path constructs the query.

## Fix

Added `mcpServerToolsEligible(isToolBackedChat, agentMode)` (`workflow_manifest_routes.go`) —
`isToolBackedChat && strings.TrimSpace(agentMode) != "workflow"` — and use it in
place of the bare `isToolBackedChat` check that gated
`registerMultiAgentMCPServerTools`'s call site (`server.go`, inside the
`isToolBackedChat` block that also registers secret-management tools).

Deliberately scoped to just the MCP-server-tools registration, not a redefinition
of `isToolBackedChat`/`isWorkflowPhase` themselves — those two flags also gate
LLM discovery tools, skill tools, and secret-management tools across ~30 other
call sites in the same function, and this fix isn't making a determination about
run-mode eligibility for any of those; only MCP server management was reported
and verified as the concern.

## Verification

- `TestMCPServerToolsEligibleExcludesManualWorkflowRun` (`workflow_manifest_routes_test.go`) —
  table test covering multi-agent chat (eligible), empty/legacy agent-mode
  aliases (eligible), `workflow` with and without surrounding whitespace
  (excluded), and `workflow_phase` (excluded, matching prior behavior).
- `go build ./...` and `go vet ./cmd/server/...` clean.
- Related, separate fix in the same pass: `remove_mcp_server` now scans every
  workflow visible to the user for a dangling reference to the server being
  removed (`workflowsReferencingMCPServer`) and reports affected workflows in
  its response, since removal is account-wide and previously left no trace for
  a workflow that still had the server selected.

## Deployment

Pending. Not yet deployed to any server; implemented and tested locally only.
