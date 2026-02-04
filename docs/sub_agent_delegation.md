# Sub-Agent Delegation (Spawn Mode)

## Overview

Sub-agent delegation allows the main agent to spawn independent sub-agents to handle tasks in parallel. This is particularly useful for:

- Breaking down complex tasks into smaller, independent pieces
- Running multiple operations simultaneously
- Delegating focused subtasks that can be handled autonomously

When delegation mode is enabled, the agent has access to a `delegate` tool that spawns sub-agents with the same tool access as the parent agent.

---

## Quick Start

### Enabling Delegation Mode

Delegation mode is **enabled by default** for new chat sessions. You can toggle it using slash commands:

| Command | Action |
|---------|--------|
| `/spawn` | Enable sub-agent delegation |
| `/nospawn` | Disable sub-agent delegation |

### UI Indicator

When delegation mode is enabled, a small purple **GitBranch icon** appears next to the send button in the chat input. Hovering over it shows: "Sub-agent delegation enabled (/nospawn to disable)"

---

## How It Works

### Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    Parent Agent                          │
│  ┌─────────────────────────────────────────────────┐    │
│  │ Has access to: delegate tool + all other tools   │    │
│  └─────────────────────────────────────────────────┘    │
│                         │                                │
│                         ▼                                │
│              delegate(instruction)                       │
│                         │                                │
│         ┌───────────────┼───────────────┐               │
│         ▼               ▼               ▼               │
│   ┌──────────┐   ┌──────────┐   ┌──────────┐           │
│   │Sub-Agent │   │Sub-Agent │   │Sub-Agent │           │
│   │  Depth 1 │   │  Depth 1 │   │  Depth 1 │           │
│   │          │   │          │   │          │           │
│   │ Same     │   │ Same     │   │ Same     │           │
│   │ tools as │   │ tools as │   │ tools as │           │
│   │ parent   │   │ parent   │   │ parent   │           │
│   └──────────┘   └──────────┘   └──────────┘           │
└─────────────────────────────────────────────────────────┘
```

### The `delegate` Tool

When delegation mode is enabled, the agent receives a `delegate` tool:

```json
{
  "name": "delegate",
  "description": "Delegate a task to a sub-agent. The sub-agent will have access to the same tools as you and will execute the task independently.",
  "parameters": {
    "type": "object",
    "properties": {
      "instruction": {
        "type": "string",
        "description": "Clear, detailed instructions for what the sub-agent should accomplish."
      }
    },
    "required": ["instruction"]
  }
}
```

### Sub-Agent Behavior

- **Tool Access**: Sub-agents inherit all tools from the parent agent (MCP servers, workspace tools, etc.)
- **Isolation**: Each sub-agent has its own conversation context (no access to parent's history)
- **Recursion Limit**: Maximum depth of 3 to prevent infinite delegation chains
- **No Delegation**: Sub-agents do NOT receive the `delegate` tool themselves (v1 design choice)

---

## When to Use Delegation

### Good Use Cases

| Scenario | Example |
|----------|---------|
| **Parallel independent tasks** | "Implement feature A" + "Implement feature B" simultaneously |
| **Research while working** | Delegate "Research library X" while continuing other work |
| **Multi-file operations** | Process multiple unrelated files in parallel |
| **Complex subtasks** | Break down a large task into focused, autonomous pieces |

### When NOT to Delegate

| Scenario | Reason |
|----------|--------|
| **Simple single operations** | Just do it directly - delegation overhead not worth it |
| **Sequential dependencies** | Each step depends on previous results |
| **Context-dependent tasks** | Sub-agent has no access to parent's conversation history |
| **Quick lookups** | Faster to do directly |

---

## Example Usage

### Parallel Feature Implementation

```
User: Implement login and signup forms for the app

Agent: I'll delegate these independent tasks to sub-agents:

[Calls delegate tool]
instruction: "Implement a login form component in src/components/LoginForm.tsx with email and password fields, validation, and submit handler that calls the /api/auth/login endpoint"

[Calls delegate tool]
instruction: "Implement a signup form component in src/components/SignupForm.tsx with email, password, confirm password fields, validation, and submit handler that calls the /api/auth/signup endpoint"
```

### Research and Implementation

```
User: Add caching to our API using the best library for our stack

Agent: I'll delegate the research while I prepare the integration points:

[Calls delegate tool]
instruction: "Research the best caching libraries for Go APIs. Compare redis-go, go-cache, and bigcache. Recommend one based on our use case of caching API responses with TTL support."

[Meanwhile, agent continues analyzing the codebase for integration points]
```

---

## Technical Details

### Key Files

| Component | File Path | Description |
|-----------|-----------|-------------|
| **Delegation Tools** | `agent_go/cmd/server/virtual-tools/delegation_tools.go` | Tool definition and executor |
| **Server Integration** | `agent_go/cmd/server/server.go` | `executeDelegatedTask()` method |
| **Event Observer** | `agent_go/internal/events/event_observer.go` | `DelegationEventObserver` for sub-agent events |
| **Frontend Toggle** | `frontend/src/components/ChatInput.tsx` | `/spawn` and `/nospawn` handlers |
| **Store** | `frontend/src/stores/useAppStore.ts` | `enableDelegationMode` state |

### Event Hierarchy

Sub-agent events are tagged with metadata for hierarchical display:

```json
{
  "component": "delegation-0",      // Identifies delegation depth
  "hierarchy_level": 1,             // Depth + 1
  "correlation_id": "delegation-0-123456789",  // Groups related events
  "parent_id": "session_delegation_start_..."   // Links to parent event
}
```

### Streaming Behavior

- **Parent agent**: Streaming text shown in real-time
- **Sub-agents**: Streaming filtered out to prevent UI conflicts
- Sub-agent results are returned via the `delegate` tool response

### Recursion Prevention

```go
const MaxDelegationDepth = 3

// Checked before each delegation
if currentDepth >= MaxDelegationDepth {
    return "", fmt.Errorf("maximum delegation depth (%d) reached", MaxDelegationDepth)
}
```

---

## Configuration

### Default State

Delegation mode is **enabled by default** for new sessions. This can be changed in:

```typescript
// frontend/src/stores/useAppStore.ts
enableDelegationMode: true, // Default to enabled
```

### Per-Session Toggle

Users can toggle delegation mode at any time during a chat session using `/spawn` or `/nospawn`. The setting persists for the session.

---

## Limitations

1. **No Sub-Agent Delegation**: Sub-agents cannot spawn their own sub-agents (prevents runaway chains)
2. **No Conversation Context**: Sub-agents start fresh with no access to parent's history
3. **Same Session**: All events flow to the same session ID (sub-agent events are tagged for identification)
4. **Timeout**: Sub-agent execution has a 30-minute timeout

---

## Future Enhancements

- Parallel delegation execution (multiple sub-agents running simultaneously)
- Progress streaming from sub-agents to UI
- Sub-agent task queuing and management
- Predefined sub-agent specializations (researcher, coder, reviewer)
