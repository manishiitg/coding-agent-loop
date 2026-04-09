# MCP Session ID Propagation: Workspace Env Fix

## Problem

In workflow/code execution mode, every Playwright tool call opens a **new browser** instead of reusing the existing one. The root cause is a **session ID mismatch** between the `MCP_API_URL` baked into the workspace executor's env map and the actual MCP session ID used by the agent.

### Symptom in logs

```
⚠️ [SESSION MISS] Falling back to mcpcache - will create NEW connection (new browser for Playwright!)
  server=playwright session_id=workflow-session-XXXX

🔍 [SESSION DEBUG] Available sessions in registry
  all_sessions="[session-group-group-1-YYYY]"
  requested_session_id=workflow-session-XXXX
```

The requested session (`workflow-session-XXXX`) doesn't match the registered session (`session-group-group-1-YYYY`).

## Root Cause

### Object hierarchy

```
WorkflowOrchestrator
  └── *BaseOrchestrator (A)  ← SetWorkspaceEnvRef called here ✓
        └── workspaceEnvRef = env map

StepBasedWorkflowOrchestrator (created inside WorkflowOrchestrator)
  └── *BaseOrchestrator (B)  ← SetMCPSessionID(groupSessionID) called here
        └── workspaceEnvRef = nil  ✗ WAS NOT PROPAGATED
```

`WorkflowOrchestrator` and `StepBasedWorkflowOrchestrator` each have their **own separate `BaseOrchestrator`** instance. When `server.go` calls `SetWorkspaceEnvRef(env)`, it sets it on `BaseOrchestrator A`. But when `controller_batch_execution.go` calls `SetMCPSessionID(groupSessionID)`, it runs on `BaseOrchestrator B` — which has `workspaceEnvRef = nil`. The env map never gets updated with the group session ID.

### Timeline (before fix)

```
1. server.go:3013     → Creates env map with MCP_API_URL = .../s/{UI_session}
2. server.go:3022     → workflowOrchestrator.SetWorkspaceEnvRef(env)  [on BaseOrchestrator A]
3. getSessionID()     → SetMCPSessionID(workflow-session-XXX) on A → env updated to .../s/workflow-session-XXX ✓
4. NewStepBased...    → Creates BaseOrchestrator B (workspaceEnvRef = nil)
5. todoPlannerAgent.SetMCPSessionID(workflow-session-XXX)  [on B, env NOT updated — nil ref]
6. batch_execution    → hcpo.SetMCPSessionID(session-group-group-1-YYY)  [on B, env NOT updated — nil ref]
7. execute_shell_cmd  → shell gets MCP_API_URL=.../s/workflow-session-XXX (STALE!)
8. Playwright call    → /s/workflow-session-XXX/tools/mcp/playwright/... → SESSION MISS
```

## Fix

### Reference chain (Go maps are reference types)

```
getMCPExtraEnv() creates env map
        ↓ same pointer
WithExtraEnv(env) → Client.ExtraEnv = env  (direct assignment, no copy)
        ↓ same pointer
factory returns env → stored as workspaceEnv in server.go
        ↓ same pointer
SetWorkspaceEnvRef(env) → BaseOrchestrator.workspaceEnvRef
        ↓ mutates same map in-place
SetMCPSessionID() writes env["MCP_API_URL"] and env["MCP_SESSION_ID"]
        ↓ next call reads updated values
ExecuteShellCommand reads Client.ExtraEnv at call time → shell gets correct URL
```

### Changes

#### 1. Factory returns env map reference

**File: `virtual-tools/workspace_advanced_tools.go`**

`CreateWorkspaceAdvancedToolExecutorsWithSession` and `...WithSessionAndEnv` now return `(executors, envMap)`. The `envMap` is the same Go map stored as `Client.ExtraEnv`.

#### 2. BaseOrchestrator stores and propagates env ref

**File: `orchestrator/base_orchestrator.go`**

- New field: `workspaceEnvRef map[string]string`
- New methods: `SetWorkspaceEnvRef(env)`, `GetWorkspaceEnvRef()`
- `SetMCPSessionID` now updates `env["MCP_API_URL"]` and `env["MCP_SESSION_ID"]` in-place when `workspaceEnvRef` is set

#### 3. server.go captures and stores env ref

**File: `server.go` (~line 3013)**

```go
sessionAwareExecutors, workspaceEnv := virtualtools.CreateWorkspaceAdvancedToolExecutorsWithSessionAndEnv(...)
workflowOrchestrator.SetWorkspaceEnvRef(workspaceEnv)
```

#### 4. WorkflowOrchestrator propagates env ref to child

**File: `types/workflow_orchestrator.go` (3 call sites)**

Before calling `SetMCPSessionID`, propagate the env ref:

```go
if envRef := wo.GetWorkspaceEnvRef(); envRef != nil {
    todoPlannerAgent.SetWorkspaceEnvRef(envRef)
}
todoPlannerAgent.SetMCPSessionID(wo.getSessionID())
```

This ensures the `StepBasedWorkflowOrchestrator`'s `BaseOrchestrator` has the env ref BEFORE `SetMCPSessionID` is called.

### Timeline (after fix)

```
1. server.go:3013     → Creates env map with MCP_API_URL = .../s/{UI_session}
2. server.go:3022     → workflowOrchestrator.SetWorkspaceEnvRef(env)  [on BaseOrchestrator A]
3. getSessionID()     → SetMCPSessionID(workflow-session-XXX) on A → env updated ✓
4. NewStepBased...    → Creates BaseOrchestrator B
5. wo.GetWorkspaceEnvRef() → todoPlannerAgent.SetWorkspaceEnvRef(envRef)  [on B] ✓ NEW
6. todoPlannerAgent.SetMCPSessionID(workflow-session-XXX)  [on B, env updated ✓]
7. batch_execution    → hcpo.SetMCPSessionID(session-group-group-1-YYY)  [on B, env updated ✓]
8. execute_shell_cmd  → shell gets MCP_API_URL=.../s/session-group-group-1-YYY ✓
9. Playwright call    → /s/session-group-group-1-YYY/tools/mcp/playwright/... → SESSION HIT ✓
```

## Debug Logging

The fix includes detailed logging at every step:

| Log message | When |
|---|---|
| `🔗 Stored workspace env ref (keys: [...], MCP_API_URL=..., MCP_SESSION_ID=...)` | `SetWorkspaceEnvRef` called |
| `🔗 Set MCP session ID for connection sharing: {new} (previous: {old})` | `SetMCPSessionID` called |
| `🔗 Updated workspace env MCP_API_URL: {old} → {new}` | Env map updated successfully |
| `🔗 Updated workspace env MCP_SESSION_ID: {session}` | Env map updated successfully |
| `🔗 No workspace env ref set, skipping workspace env update (workspaceEnvRef is nil)` | Env ref missing (debug level) |
| `🔗 MCP_API_URL env not set, skipping workspace env update` | Base URL not configured (debug level) |

### Verification in logs

After the fix, look for:
```
🔗 Updated workspace env MCP_API_URL: .../s/workflow-session-XXX → .../s/session-group-group-1-YYY
```
This confirms the env was updated when the group session was set. Then look for:
```
✅ Using session registry connection (session-aware)  session_id=session-group-group-1-YYY  server=playwright
```
This confirms Playwright is reusing the existing browser connection.

## Files Changed

| File | Change |
|---|---|
| `agent_go/cmd/server/virtual-tools/workspace_advanced_tools.go` | Return env map from factory functions |
| `agent_go/pkg/orchestrator/base_orchestrator.go` | Add `workspaceEnvRef` field + getter/setter, update `SetMCPSessionID` |
| `agent_go/cmd/server/server.go` | Capture env ref at ~3013, pass to orchestrator; handle new return at ~4001, ~6982 |
| `agent_go/pkg/orchestrator/types/workflow_orchestrator.go` | Propagate env ref at all 3 `NewStepBasedWorkflowOrchestrator` call sites |
