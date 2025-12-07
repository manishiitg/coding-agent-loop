# Refactoring Plan: Event Type Discriminated Unions

## ✅ IMPLEMENTATION COMPLETED (December 2024)

This refactoring has been fully implemented. The event type system now provides type-safe discriminated unions for all events.

---

## 📋 Overview

This plan outlines the refactoring of the event type generation pipeline to produce **TypeScript Discriminated Unions** for the frontend. This change enforces strict type safety, eliminates unsafe casting, and significantly improves the developer experience when handling events.

**Key Benefits:**
- **Type Safety**: The compiler guarantees that `event.data` matches `event.type`.
- **Zero Casting**: Removes the need for manual `as Type` casts or helper functions.
- **Better IDE Support**: Autocomplete works correctly based on the checked `type`.

---

## ✅ Implementation Summary

### What Was Done

1. **Added Missing Event Types** (`mcpagent/events/data.go`)
   - `Variable` struct
   - `VariablesExtractedEvent`
   - `IndependentStepsSelectedEvent`
   - `StructuredOutputStartEvent`, `StructuredOutputEndEvent`, `StructuredOutputErrorEvent`

2. **Updated Schema Generation** (`agent_go/cmd/schema-gen/main.go`)
   - Schema now matches actual wire format: `Event → AgentEvent → EventData`
   - Added all 50+ event types to the schema
   - Proper nested structure: `event.data.data` contains typed event

3. **Created Type-Safe TypeScript Utilities** (`frontend/src/generated/event-types.ts`)
   - `EventTypeToDataMap`: Maps event type strings to typed data
   - `TypedAgentEvent<T>`: Generic typed event wrapper
   - `AgentEventUnion`: Union of all typed events
   - `isEventOfType()`: Type guard for narrowing
   - `getTypedEventData()`: Type-safe data extraction
   - `extractEventData<T>()`: Backward-compatible helper

4. **Updated Frontend Components**
   - `EventDispatcher.tsx`: Now uses type-safe `extractEventData` from `event-types.ts`
   - `VariablesIcon.tsx`: Uses typed `VariablesExtractedEvent`
   - `StepSidebar.tsx`: Uses typed event extraction

---

## 📊 Wire Format (Actual Structure)

```
PollingEvent (event_store.go)
├── id: string
├── type: "tool_call_start"          ← Event type discriminator
├── timestamp: ISO string
├── session_id?: string
├── error?: string
└── data: AgentEvent
    ├── type: "tool_call_start"      ← Same as parent
    ├── timestamp: ISO string
    ├── event_index: number
    ├── trace_id?: string
    ├── hierarchy_level: number
    └── data: ToolCallStartEvent     ← Actual typed event data
        ├── tool_name: string
        ├── tool_params: object
        └── server_name: string
```

**Key Insight**: Event data is at `event.data.data`, not `event.data`.

---

## 🔧 How to Use

### Type-Safe Event Handling

```typescript
import { isEventOfType, getTypedEventData, extractEventData } from '../../generated/event-types';
import type { ToolCallStartEvent } from '../../generated/event-types';

// Option 1: Type guard with automatic narrowing
if (isEventOfType(event, 'tool_call_start')) {
  // event.data.data is automatically typed as ToolCallStartEvent
  console.log(event.data.data.tool_name);
}

// Option 2: Direct extraction with type parameter
const data = getTypedEventData(event, 'tool_call_start');
if (data) {
  console.log(data.tool_name);  // TypeScript knows the type
}

// Option 3: Legacy-compatible extraction (in switch statements)
const eventData = extractEventData<ToolCallStartEvent>(event.data);
console.log(eventData?.tool_name);
```

### In EventDispatcher.tsx

```tsx
case 'tool_call_start':
  return <ToolCallStartDisplay event={extractEventData<ToolCallStartEvent>(event.data)} />
```

---

## 📁 Key Files

| File | Description |
|------|-------------|
| `mcpagent/events/data.go` | All event struct definitions |
| `mcpagent/events/types.go` | EventType constants |
| `agent_go/cmd/schema-gen/main.go` | JSON Schema generator |
| `agent_go/schemas/polling-event.schema.json` | Generated schema |
| `frontend/src/generated/events-bridge.ts` | Generated TypeScript (from json-schema-to-typescript) |
| `frontend/src/generated/event-types.ts` | Type utilities and discriminated unions |

---

## 🔄 Regenerating Types

```bash
# 1. Generate JSON schema (from Go)
cd agent_go
go build -o ../bin/schema-gen ./cmd/schema-gen
../bin/schema-gen

# 2. Generate TypeScript types (from JSON Schema)
cd ../frontend
npm run types:events-bridge
```

---

## ⚠️ Important Notes

1. **Wire Format Unchanged**: The JSON sent over HTTP/SSE has not changed. Only the TypeScript type definitions have been improved.

2. **Backward Compatibility**: The `extractEventData` function is still available for backward compatibility with existing code.

3. **Nested Data**: Remember that actual event data is at `event.data.data`, not directly at `event.data`.

---

## 🧪 Verification Checklist

- [x] Backend compiles: `cd agent_go && go build ./...`
- [x] Frontend compiles: `cd frontend && npx tsc --noEmit`
- [x] Schema generates: `../bin/schema-gen`
- [x] TypeScript types generate: `npm run types:events-bridge`
- [x] All 50+ event types included in schema
- [x] Type guards work correctly with narrowing

---

## 📋 Original Problem Statement (Historical)

The original `PollingEvent` type was loosely typed:

```typescript
// Old problematic structure
export interface PollingEventSchema {
  type?: string;          // Generic string - no narrowing
  data?: EventData;       // Object with ALL event types as optional keys
}

export interface EventData {
  tool_call_start?: ToolCallStartEvent;
  tool_call_end?: ToolCallEndEvent;
  // ... 50+ other optional fields
}
```

**Issues Solved:**
1. ✅ No link between type and data → Now uses discriminated unions
2. ✅ Optional hell → Now has direct typed access
3. ✅ Unsafe casting → Now has type-safe helpers
