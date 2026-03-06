# Agent Memory System

Persistent memory for the multi-agent chat mode. Memories survive across sessions and are stored as markdown files in the workspace.

## Overview

The memory system provides two tools — `save_memory` and `recall_memory` — available only in **Multi-Agent Chat mode** (delegation mode: `plan`). Each tool spawns a background sub-agent that reads/writes memory files using shell commands. The manager agent is notified when the background agent completes.

## Tools

### save_memory

| Parameter | Required | Description |
|-----------|----------|-------------|
| `content` | Yes | The information to save. Should be specific and self-contained. |
| `context` | No | Additional context about why this is being saved. |

Returns immediately with an `agent_id`. A background agent:
1. Creates the current month folder (`Plans/memories/YYYY-MM/`)
2. Checks existing files for duplicates
3. Appends (or creates) a category file with a timestamped heading

### recall_memory

| Parameter | Required | Description |
|-----------|----------|-------------|
| `query` | Yes | Topic, keyword, or question to search for. |

Returns immediately with an `agent_id`. A background agent:
1. Lists all month folders
2. Searches across all months using `grep -ri`
3. Reads matching files and synthesizes a summary

## Storage Structure

Two parallel structures for different access patterns:

```
Plans/memories/
  prompt.md              ← User-editable custom instructions (optional)
  entities.md            ← Entity registry (list of known entity names)
  entities/              ← Per-entity knowledge files (fast lookup)
    auth-service.md      ← Everything known about "auth-service"
    postgresql.md        ← Everything known about "postgresql"
    {entity-name}.md     ← Lowercase, hyphenated
  2026-03-05/            ← Date folders (chronological log)
    general.md
    decisions.md
    preferences.md
    {custom}.md
  2026-02-18/
    ...
```

### Entity Files

Entity files group all knowledge about a specific named thing (project, system, technology, person, feature). They are updated by the `save_memory` agent when it identifies relevant entities in the content being saved.

**entities.md** registry:
```markdown
# Entity Registry

Known entities (each has a file in entities/):
- auth-service
- postgresql
- user-preferences
```

**entities/auth-service.md**:
```markdown
# Auth Service

## 2026-03-05 14:30
Chose JWT for authentication. Uses HS256 algorithm.
Refresh tokens stored in Redis with 24h TTL.

## 2026-03-05 16:00
Updated auth middleware to handle token expiry gracefully.
```

### Date Files (Chronological Log)

Full chronological record of all memories, regardless of entity:

```markdown
# Decisions

## 2026-03-05 14:30
User confirmed all API endpoints require JWT authentication.

## 2026-03-05 15:45
Chose PostgreSQL over MongoDB for relational data model.
```

### Why Both Structures

| | Entity Files | Date Files |
|---|---|---|
| Best for | "What do I know about X?" | "What happened recently?" |
| Lookup | O(1) — direct file read | O(n) — grep across folders |
| Content | Curated, deduplicated | Full chronological record |
| Updated by | `save_memory` (if entity extracted) | `save_memory` (always) |
| Compressed by | `compress_memory` | `compress_memory` |

## Custom Instructions (prompt.md)

Users can customize memory agent behavior by creating `Plans/memories/prompt.md`. This file is read by the handler before spawning the sub-agent and prepended to the agent's instructions.

Example `prompt.md`:
```markdown
- Always save memories in bullet-point format
- Categorize by project name (e.g., auth-service.md, data-pipeline.md)
- Never save code snippets — only decisions and rationale
- Use Spanish for all memory entries
```

The handler reads `prompt.md` via the workspace API client (`planWorkspaceClient`). If the file doesn't exist, the default instructions are used without modification.

## Architecture

### Execution Flow

```
Manager Agent
  ↓ calls save_memory(content: "...")
  ↓
handleSaveMemory (handler function)
  ├── Reads Plans/memories/prompt.md via workspace API (if exists)
  ├── Builds instruction string (custom prompt + save instructions + content)
  ├── Calls BackgroundDelegateFunc(ctx, "Save Memory", instruction)
  └── Returns immediately with { agent_id, status: "running" }
        ↓
Background Sub-Agent (async)
  ├── Gets workspace_advanced tools (shell, image, web fetch, PDF, diff_patch)
  ├── Uses execute_shell_command for all file operations
  ├── mkdir -p, ls, cat, grep, append via heredoc
  ├── Writes to Plans/memories/YYYY-MM/{category}.md
  └── Manager notified on completion
```

### Context Injection

The memory tool executors receive these context values from the `wrappedExecutor` in `server.go`:

| Context Key | Value | Purpose |
|-------------|-------|---------|
| `WorkspaceClientKey` | `planWorkspaceClient` | Reading prompt.md |
| `PlanEventEmitterKey` | `planEventEmitter` | Sidebar file events |
| `BackgroundDelegateKey` | `bgDelegateFunc` | Spawning background agents |
| `BGAgentRegistryKey` | `bgQuerier` | Agent status queries |
| `BGAgentSessionIDKey` | `sessionID` | Session identification |

### Sub-Agent Configuration

Memory sub-agents inherit the standard delegation infrastructure:
- **Reasoning level**: `low` (hardcoded — memory ops are simple read/write)
- **Write restriction**: `PlanFolderKey` set to `"Plans"` (FolderGuard enforced)
- **Tools**: workspace_advanced (shell, image, web fetch, PDF, diff_patch) + any MCP servers from parent
- **Async**: Always background agents via `BackgroundDelegateFunc`

## System Prompt

The manager agent receives memory instructions via `GetMemoryInstructions()`, appended to the system prompt in plan delegation mode. This tells the manager when and how to use `save_memory` / `recall_memory`.

## Availability

Memory tools are registered **only** when `delegationMode == "plan"` (Multi-Agent Chat mode). They do NOT appear in:
- Regular Chat mode
- Spawn delegation mode
- Workflow mode

## Files

| File | Purpose |
|------|---------|
| `agent_go/cmd/server/virtual-tools/memory_tools.go` | Tool definitions, executors, instructions |
| `agent_go/cmd/server/server.go` (~line 3971) | Tool registration in plan mode |
| `agent_go/cmd/server/server.go` (~line 4186) | System prompt injection |
| `agent_go/cmd/server/instructions.go` | Plans/ folder description update |
