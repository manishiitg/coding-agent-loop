# Refactoring Plan: Event Type Discriminated Unions

## 📋 Overview

This plan outlines the refactoring of the event type generation pipeline to produce **TypeScript Discriminated Unions** for the frontend. This change will enforce strict type safety, eliminate unsafe casting, and significantly improve the developer experience when handling events.

**Key Benefits:**
- **Type Safety**: The compiler guarantees that `event.data` matches `event.type`.
- **Zero Casting**: Removes the need for manual `as Type` casts or helper functions.
- **Better IDE Support**: Autocomplete works correctly based on the checked `type`.

---

## 🚨 Problem Statement

Currently, the generated `PollingEvent` type in `frontend/src/generated/events-bridge.ts` is loosely typed:

```typescript
// Current Structure
export interface PollingEventSchema {
  type?: string;          // Generic string
  data?: EventData;       // Object with ALL event types as optional keys
}

export interface EventData {
  tool_call_start?: ToolCallStartEvent;
  tool_call_end?: ToolCallEndEvent;
  // ... 50+ other optional fields
}
```

**Issues:**
1.  **No Link between Type and Data**: Checking `event.type === 'tool_call_start'` does not narrow `event.data`.
2.  **Optional Hell**: `event.data` is typed as an object that *could* contain any event, forcing developers to guess or cast.
3.  **Unsafe Code**: We currently use a helper `extractEventData<T>` that performs an unsafe cast (`as T`), hiding potential bugs.

---

## 🎯 Proposed Solution

We will restructure the generated JSON Schema so that `json-schema-to-typescript` produces a **Discriminated Union**.

**Target TypeScript Structure:**

```typescript
// Target Structure
export type PollingEvent = 
  | ToolCallStartEventWrapper
  | ToolCallEndEventWrapper
  | AgentStartEventWrapper
  // ... etc

export interface ToolCallStartEventWrapper {
  type: 'tool_call_start'; // Literal type
  data: ToolCallStartEvent;
}

export interface ToolCallEndEventWrapper {
  type: 'tool_call_end';   // Literal type
  data: ToolCallEndEvent;
}
```

With this structure, TypeScript automatically narrows the type:

```typescript
if (event.type === 'tool_call_start') {
  // event.data is strictly ToolCallStartEvent
  console.log(event.data.tool_name); 
}
```

---

## 🛠️ Implementation Plan

### Phase 1: Update Schema Generation (Backend)

**File:** [`agent_go/cmd/schema-gen/main.go`](file:///Users/mipl/ai-work/mcp-agent/agent_go/cmd/schema-gen/main.go)

1.  **Define Wrapper Structs**: Create generic wrapper structs in Go that represent the wire format for each event.
    ```go
    type EventWrapper[T any] struct {
        Type string `json:"type" jsonschema:"enum=..."` // Enum will be specific to T
        Data T      `json:"data"`
    }
    ```
    *Note: Since Go generics + reflection might be tricky with `jsonschema`, we might need to manually construct the `oneOf` definition or define explicit wrapper structs for the schema generator.*

2.  **Construct `OneOf` Schema**:
    Instead of reflecting a single `PollingEvent` struct, we will programmatically construct a schema that uses `oneOf`.
    
    ```go
    // Conceptual approach for main.go
    schema := &jsonschema.Schema{
        OneOf: []*jsonschema.Schema{
            generateWrapperSchema("tool_call_start", ToolCallStartEvent{}),
            generateWrapperSchema("tool_call_end", ToolCallEndEvent{}),
            // ... iterate over all events
        },
    }
    ```

3.  **Update Output**: Ensure `schemas/polling-event.schema.json` reflects this new structure.

### Phase 2: Regenerate Frontend Types

**Location:** [`frontend/`](file:///Users/mipl/ai-work/mcp-agent/frontend/)

1.  Run `npm run types:generate`.
2.  Verify that `src/generated/events-bridge.ts` now exports a union type (e.g., `export type PollingEvent = ...`).

### Phase 3: Refactor Frontend Components

**File:** [`frontend/src/components/events/EventDispatcher.tsx`](file:///Users/mipl/ai-work/mcp-agent/frontend/src/components/events/EventDispatcher.tsx)

1.  **Remove Helper**: Delete `extractEventData` and `wrapWithOrchestratorContext` (or update it to be type-safe).
2.  **Update Switch Statement**:
    ```tsx
    // Before
    case 'agent_error':
      return <AgentErrorEventDisplay event={extractEventData<AgentErrorEvent>(event.data)} />

    // After
    case 'agent_error':
      // event.data is automatically narrowed to AgentErrorEvent
      return <AgentErrorEventDisplay event={event.data} />
    ```
3.  **Fix Prop Types**: Update components to accept the specific event data type directly, or the full wrapper if needed.

---

## Phase 4: Fix Context-Aware Events

**File:** [`agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller.go`](file:///Users/mipl/ai-work/mcp-agent/agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller.go)

**Issue:**
Initial setup events (`VariablesExtractedEvent`, `TodoStepsExtractedEvent`) are emitted before the orchestrator context is set, resulting in missing metadata (`orchestrator_phase`, `orchestrator_step`).

**Fix:**
Explicitly set a "planning" or "initialization" context before emitting these events in `CreateTodoList`.

```go
// In CreateTodoList method:

// Set context for initialization events
hcpo.GetContextAwareBridge().SetOrchestratorContext("initialization", 0, 0, "orchestrator")

// Emit events
hcpo.variableManager.emitVariablesExtractedEvent(...)
hcpo.emitTodoStepsExtractedEvent(...)

// Clear context afterwards if needed, or let the execution loop overwrite it
```

---

## 🔍 Verification

1.  **Compile Check**: Ensure `npm run build` in frontend passes without type errors.
2.  **Runtime Check**: Verify events still render correctly in the UI.
3.  **Negative Test**: Temporarily introduce a type mismatch (e.g., accessing `tool_name` on an `agent_start` event) and verify TypeScript catches it.
4.  **Context Check**: Verify that `VariablesExtractedEvent` and `TodoStepsExtractedEvent` now contain `orchestrator_phase: "initialization"` in their metadata.

---

## ⚠️ Constraints & Risks

*   **Backward Compatibility**: The wire format (JSON sent over HTTP/SSE) MUST NOT change. We are only changing the *type definition* used to describe it.
*   **Schema Generator Complexity**: `jsonschema` library might require some custom tuning to generate the exact `const` value for the `type` field.
*   **Large Union Performance**: TypeScript handles large unions well, but we should ensure the generated file doesn't become unwieldy (currently ~50 events, which is fine).
