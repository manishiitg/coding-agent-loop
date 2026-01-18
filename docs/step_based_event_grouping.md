# Step-Based Event Grouping in EventHierarchy

## Objective
Add mode-aware event grouping: agent sessions in chat mode, step-based grouping in workflow mode.

## Current State
- Agent session grouping implemented (orchestrator_agent_start/end)
- Chat mode: 1-5 sessions expanded, 6+ collapse all except newest
- Workflow mode: No step-based grouping yet

## Implementation Plan

### 1. Mode Detection
```typescript
import { useModeStore } from '../../stores/useModeStore';
const { selectedModeCategory } = useModeStore();
const isWorkflowMode = selectedModeCategory === 'workflow';
```

### 2. Step Key Extraction (~30 lines)
Create `getStepKey` function similar to `getAgentSessionKey`:
- Extract `step_id` from `step_execution_start` and `step_execution_end` events
- Handle nested data structures (event.data.data, event.data.step_execution_start, etc.)
- Return `step_session:${step_id}` or null

### 3. Step Event Grouping (~40 lines)
Create `findEventsBetweenStepStartEnd` memo:
- Map step_id -> Set of event IDs between start/end
- Similar pattern to `findEventsBetweenStartEnd` for agent sessions
- Only process `step_execution_start` and `step_execution_end` events

### 4. State Management (~10 lines)
Add step collapse state:
```typescript
const [collapsedSteps, setCollapsedSteps] = useState<Set<string>>(new Set());
const [manuallyExpandedSteps, setManuallyExpandedSteps] = useState<Set<string>>(new Set());
```

### 5. Step Toggle Function (~10 lines)
Create `toggleStep` function:
- Toggle collapsedSteps state
- Track manually expanded steps
- Similar to `toggleAgentSession`

### 6. Filtering Logic (~30 lines)
Update `eventTree` memo:
- Check `isWorkflowMode` flag
- If workflow mode: filter events in collapsed steps (keep start/end visible)
- If chat mode: use existing agent session filtering
- Both can coexist (different collapse states)

### 7. UI Integration (~20 lines)
Update `renderEventNode`:
- For `step_execution_start` events in workflow mode:
  - Add collapse/expand button (similar to agent sessions)
  - Show event count when collapsed
  - Pass `isCollapsed`, `eventCount`, `onToggleCollapse` to EventDispatcher

### 8. Auto-Collapse Logic (Optional, ~50 lines)
For workflow mode:
- Keep current step (most recent step_execution_start) expanded
- Collapse completed steps (have step_execution_end)
- Track step completion order
- Respect manually expanded steps

## Key Files
- `frontend/src/components/events/EventHierarchy.tsx` - Main implementation
- `frontend/src/components/events/EventDispatcher.tsx` - Pass step collapse props
- `frontend/src/components/events/orchestrator/StepExecutionEvent.tsx` - Display collapse button

## Data Structure
Step events have:
- `step_id`: string (required identifier)
- `step_index`: number (0-based)
- `step_title`: string
- `step_path`: string (e.g., "step-1" or "step-1-if-true-0")

## Testing Checklist
- [ ] Step grouping works in workflow mode
- [ ] Agent session grouping still works in chat mode
- [ ] Mode switching preserves collapse states
- [ ] Step collapse/expand button appears in workflow mode
- [ ] Event count shows correctly when step collapsed
- [ ] Auto-collapse keeps current step expanded (if implemented)

## Effort Estimate
- Basic implementation (items 1-7): ~140 lines, 1-2 hours
- With auto-collapse (item 8): ~190 lines, 2-3 hours

