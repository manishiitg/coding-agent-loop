# Workflow Shell Working Directory

## Overview

When a workflow execution agent runs `execute_shell_command`, the working directory
it defaults to is determined by a session-scoped config map. This doc explains
how that plumbing works and where the working directory is set at each stage.

---

## Two Session IDs

There are two distinct session IDs in the system:

| ID | Created | Stored |
|----|---------|--------|
| **HTTP session ID** | From `X-Session-ID` request header | `sessionID` in `server.go` |
| **MCP session ID** | `workflow-session-{timestamp}` in `workflow_orchestrator.go` | `hcpo.sessionID` |

The session working dir config map (`common.sessionShellConfigs`) is keyed by the **HTTP session ID**.

---

## How Session Working Dir Is Looked Up

`execute_shell_command.go` reads `ChatSessionIDKey` from context to find the session:

```
ctx → ChatSessionIDKey → sessionID → GetSessionShellConfig(sessionID) → WorkingDir
```

Priority order when no explicit `working_directory` arg is passed:
1. `sessionCfg.WorkingDir` (session map, keyed by `ChatSessionIDKey` in context)
2. `c.DefaultWorkingDir` (workspace client field — typically empty)
3. `c.ExtraEnv["_DEFAULT_WORKING_DIR"]`
4. Empty → workspace root

---

## workflowCtx Setup (`server.go`)

The workflow execution runs in a fresh context (`context.Background()`):

```go
workflowCtx = context.WithCancel(context.Background())
workflowCtx = context.WithValue(workflowCtx, common.UserIDKey, currentUserID)
workflowCtx = context.WithValue(workflowCtx, common.ChatSessionIDKey, sessionID)  // injects HTTP session ID
```

The `ChatSessionIDKey` injection (third line) is critical — without it, all execution
agent shell commands would fall through to workspace root as the working directory,
ignoring whatever was set via `SetSessionWorkingDir`.

**Note:** The main chat agent also injects `ChatSessionIDKey` at `server.go:5138` into its
own `agentCtx`. Phase chat agents use this same path.

---

## Working Directory Lifecycle During Execution

### On workflow start (`server.go:2771`)

```go
workspace.SetSessionWorkingDir(sessionID, workflowWorkspacePath)
// e.g. "Workflow/HRMS"
```

All shell commands at this point default to `Workflow/HRMS`.

### On run folder selection (`controller.go` / `execution_manager.go`)

When a run folder is resolved (either from frontend options or interactive selection),
the session working dir is updated to the run's execution folder:

```go
// controller.go (frontend path, line ~549)
common.SetSessionWorkingDir(hcpo.httpSessionID, "Workflow/HRMS/runs/run-001/execution")

// controller.go (interactive path, line ~605)
common.SetSessionWorkingDir(hcpo.httpSessionID, "Workflow/HRMS/runs/run-001/execution")

// execution_manager.go (batch execution path, applyExecutionContext)
common.SetSessionWorkingDir(orch.httpSessionID, "Workflow/HRMS/runs/run-001/execution")
```

After this point, all shell commands (execution agents, learning agents, and todo-task agents)
default to `Workflow/HRMS/runs/run-001/execution`.

---

## Folder Guard vs Working Directory

These are separate mechanisms:

- **Working directory**: where relative paths and `./` resolve for shell commands
- **Folder guard**: which paths an agent is allowed to read/write (enforced via Isolator)

The folder guard for execution agents is set per-step via `SetWorkspacePathForFolderGuard`
(snapshotted at agent creation time in `WrapWorkspaceToolsWithFolderGuard`). It is narrower
than the working directory — e.g., write access restricted to `execution/step-3/`.

The working directory is broader — the whole `execution/` folder — so the agent can read
previous step outputs with relative paths like `../step-2/output.json`.

---

## Workshop Builder Phase

The interactive workshop (workflow builder) also runs in `workflowCtx` and inherits the
`ChatSessionIDKey`. At build time, `selectedRunFolder` is not yet set, so the working
directory remains `Workflow/HRMS` — correct for a builder that reads/writes planning files.

---

## Summary Table

| Stage | Working Dir | How set |
|-------|-------------|---------|
| Workflow start | `Workflow/HRMS` | `SetSessionWorkingDir` in `server.go:2771` |
| After run folder selected | `Workflow/HRMS/runs/run-001/execution` | `common.SetSessionWorkingDir` in `controller.go` / `execution_manager.go` |
| Chat agent | `Chats/` | `DefaultWorkingDirKey` context injection in `tool_setup.go` |
| Phase chat agent | `phaseWorkspacePath` | `SetSessionWorkingDir` in `server.go:4472` |
| Plans agent | `Chats/` | `SetSessionWorkingDir` in `server.go:3756` |
