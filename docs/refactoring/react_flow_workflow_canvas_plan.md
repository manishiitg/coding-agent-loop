# React Flow Workflow Canvas - Implementation Plan

## Overview

Replace the current workflow mode UI with a React Flow-based canvas that serves as the command center for all workflow operations. The ChatArea appears on the right side only when a workflow phase is started.

## Current Status: ✅ Phase 7 Complete (Frontend Execution Controls & Single Step Execution)

### Completed Features
- ✅ React Flow canvas with visual plan representation
- ✅ Left-to-right (LR) layout using Dagre algorithm
- ✅ Custom nodes: StepNode, ConditionalNode, LoopNode, StartNode, EndNode
- ✅ Dynamic phase selector dropdown in toolbar (loads all phases from backend)
- ✅ Phase descriptions shown in dropdown
- ✅ Step sidebar for editing individual steps (replaces popup panel)
- ✅ StepEditPanel integration for advanced step configuration
- ✅ Theme-aware colors (light, dark, dark-plus support)
- ✅ Dependency edges toggle (show/hide context dependencies)
- ✅ ChatArea appears on right when phase is started
- ✅ EventViewer for streaming events during execution
- ✅ **Auto-refresh on plan updates** (detects `todo_steps_extracted` events)
- ✅ **Change highlighting** (added=green, updated=blue, deleted=red)
- ✅ **Auto-clear highlights** after 4 seconds
- ✅ **Minimized TodoStepsExtractedEvent** in workflow mode (collapsed by default)
- ✅ **Frontend execution controls** - All execution options moved to UI (no backend prompts)
- ✅ **3-Dropdown execution system** - Iteration, Execution Mode, Start Point
- ✅ **LocalStorage persistence** - Execution preferences saved per workflow
- ✅ **Run single step** - Execute only one step and stop
- ✅ **Progress-based step enabling** - Run buttons disabled if previous steps incomplete
- ✅ **Run from step button** - Direct play button on each step node
- ✅ **Compact toolbar** - Reduced font and button sizes for better space usage

### Remaining Work
- 🔲 Test full workflow execution flow end-to-end
- 🔲 Verify all execution strategies work correctly
- 🔲 Test single step execution in various scenarios
- 🔲 Phase options support (if phases have configurable options)
- 🔲 Test LocalStorage persistence across browser sessions

---

## Architecture

### Layout Structure (Current Implementation)

```
┌────────────────────────────────────────────────────────────────────────────────┐
│  Sidebar     │                    WORKFLOW MODE                        │ WS    │
│  (72/288px)  │                                                         │ (48px)│
├──────────────┼─────────────────────────────────────────────────────────┼───────┤
│              │  ┌───────────────────────────────────────────────────┐  │       │
│  • Presets   │  │  [Header: Workflow Mode | Preset Name | Settings] │  │  [>]  │
│  • Sessions  │  ├───────────────────────────────────────────────────┤  │       │
│              │  │  Toolbar: [Iteration▼] [Mode▼] [Start▼] [Execute] │ [Phase▼] [Deps] [Zoom] │  │       │
│              │  ├─────────────────────────┬─────────────────────────┤  │       │
│              │  │                         │                         │  │       │
│              │  │   REACT FLOW CANVAS     │    ChatArea/Events      │  │       │
│              │  │   [Left-to-Right Flow]  │    (appears when        │  │       │
│              │  │                         │     phase started)      │  │       │
│              │  │  [Start] → [Step 1] →   │                         │  │       │
│              │  │     [Condition?] → ...  │    • Running: Planning  │  │       │
│              │  │                         │    • Tool calls         │  │       │
│              │  │  [Step Sidebar]         │    • LLM messages       │  │       │
│              │  │  (on right when step    │    • Events stream      │  │       │
│              │  │   selected)             │                         │  │       │
│              │  │                         │                         │  │       │
│              │  └─────────────────────────┴─────────────────────────┘  │       │
└────────────────────────────────────────────────────────────────────────────────┘
```

### Component Hierarchy (Implemented)

```
App.tsx
├── Sidebar (existing WorkspaceSidebar)
├── EventModeProvider (wraps workflow mode)
│   └── WorkflowLayout.tsx ✅
│       ├── ChatHeader.tsx (mode selector, preset name)
│       └── Main Content (flex)
│           ├── WorkflowCanvas.tsx ✅ (flex-1, shrinks when ChatArea shown)
│           │   ├── WorkflowToolbar.tsx ✅
│           │   │   └── Phase Dropdown (loads ALL phases from backend)
│           │   ├── ReactFlow ✅
│           │   │   ├── StepNode.tsx ✅
│           │   │   ├── ConditionalNode.tsx ✅ (hexagon shape)
│           │   │   ├── LoopNode.tsx ✅ (dashed border, iteration counter)
│           │   │   ├── StartNode.tsx ✅
│           │   │   └── EndNode.tsx ✅
│           │   └── StepSidebar.tsx ✅ (replaces NodeDetailPanel)
│           │       └── StepEditPanel.tsx (advanced config)
│           └── ChatArea Panel ✅ (appears on right when phase started)
│               └── EventViewer.tsx ✅
├── ChatArea.tsx (hidden in workflow mode, provides execution infrastructure)
└── Workspace.tsx (right side, minimizable)
```

---

## File Structure (Current)

```
frontend/src/components/workflow/
├── canvas/
│   ├── WorkflowCanvas.tsx        ✅ Main React Flow canvas
│   ├── WorkflowToolbar.tsx       ✅ Phase dropdown, zoom controls
│   └── StepSidebar.tsx           ✅ Step details & edit panel (NEW)
├── nodes/
│   ├── StepNode.tsx              ✅ Regular step (theme-aware)
│   ├── ConditionalNode.tsx       ✅ Hexagon shape, Yes/No branches
│   ├── LoopNode.tsx              ✅ Dashed border, iteration badge
│   ├── StartEndNodes.tsx         ✅ Start/End markers (LR handles)
│   └── index.ts                  ✅ Export all node types
├── hooks/
│   ├── usePlanData.ts            ✅ Read/write plan.json
│   ├── usePlanToFlow.ts          ✅ Convert plan → nodes/edges (LR layout)
│   └── useWorkflowExecution.ts   ✅ Workflow API calls
├── EventViewer.tsx               ✅ Read-only event stream
├── WorkflowLayout.tsx            ✅ Main layout with ChatArea integration
├── WorkflowPhaseHandler.tsx      (deprecated - phases now in toolbar)
└── index.ts                      ✅ Exports
```

---

## Key Implementation Details

### 1. Phase Selector (WorkflowToolbar.tsx)

**Dynamic Phase Loading:**
- Loads ALL phases from backend via `getWorkflowPhases()`
- Shows dropdown with phase titles AND descriptions
- Numbered badges (1, 2, 3, etc.)
- Current phase highlighted with checkmark
- Disabled during execution (shows spinner)

```typescript
// Phases loaded from: GET /api/workflow/constants
interface WorkflowPhase {
  id: string           // e.g., "variable-extraction"
  title: string        // e.g., "Variable Extraction"
  description: string  // Full description shown in dropdown
  options?: WorkflowPhaseOption[]
}
```

### 1.1. Execution Controls (WorkflowToolbar.tsx)

**3-Dropdown Execution System:**
All execution options are now collected in the frontend UI, eliminating backend interactive prompts.

**Dropdown 1: Iteration Selector**
- "New Run" - Creates a new iteration folder (e.g., `iteration-1`, `iteration-2`)
- Existing iterations - Lists all available run folders sorted by number (newest first)
- Progress badge - Shows `✓ X/Y` when existing run with progress is selected
- LocalStorage persistence - Selected iteration saved per workflow

**Dropdown 2: Execution Mode**
- **With Human Approval** - Pause for feedback at each step (default)
- **Fast Execution** - Execute all steps without pausing
- **With Learning** - Human approval + capture learnings
- LocalStorage persistence - Selected mode saved per workflow

**Dropdown 3: Start Point**
- **Start from Beginning** - Execute all steps from start
- **Resume from Step X** - Dynamically generated based on completed steps
  - Shows next step after last completed
  - Can resume from any valid step (all previous steps must be completed)
- LocalStorage persistence - Selected start point saved per workflow

**Execute Button:**
- Combines all three selections into `ExecutionOptions`
- Sends to backend via `/api/agent/query` with `execution_options` payload
- Backend uses options directly, bypassing all interactive prompts

**Backend Integration:**
```typescript
interface ExecutionOptions {
  run_mode: 'use_same_run' | 'create_new_runs_always'
  selected_run_folder?: string
  execution_strategy: string  // Maps to backend ExecutionStrategy constants
  resume_from_step?: number   // 1-based step number
}
```

**Strategy Mapping:**
| Frontend Selection | Backend Strategy |
|---|---|
| Human Approval + Beginning | `start_from_beginning` |
| Human Approval + Resume | `resume_from_step` |
| Fast Execution + Beginning | `fast_execute_all` |
| Fast Execution + Resume | `fast_resume_from_step` |
| With Learning + Beginning | `start_from_beginning_no_human` |
| With Learning + Resume | `resume_from_step_no_human` |
| Single Step (from node) | `run_single_step` |

**Toolbar Size:**
- Compact design with reduced padding (`px-3 py-1.5`)
- Smaller fonts (`text-xs` instead of `text-sm`)
- Smaller icons (`w-3.5 h-3.5` instead of `w-4 h-4`)
- Reduced button padding for better space efficiency

### 2. Layout Direction (usePlanToFlow.ts)

**Changed from TB (top-to-bottom) to LR (left-to-right):**
```typescript
const DAGRE_CONFIG = {
  rankdir: 'LR',      // Left-to-Right flow
  nodesep: 150,       // Horizontal spacing between nodes
  ranksep: 180,       // Vertical spacing between ranks
  marginx: 50,
  marginy: 50
}
```

**Handle Positions Updated:**
- Input handles: `Position.Left`
- Output handles: `Position.Right`

### 3. Node Designs

**StepNode:**
- Status-based colors (pending, running, completed, failed)
- Shows title, description, success criteria
- Theme-aware (dark mode support)
- **Run button** - Play icon button in node header
  - Runs only that single step (uses `run_single_step` strategy)
  - Disabled if previous steps not completed (`canRun` prop)
  - Tooltip shows "Run step X only" or "Complete previous steps first"
  - Disabled during execution (`isExecuting` prop)

**ConditionalNode:**
- Hexagon shape using CSS clip-path
- Yes/No labels only on edge connectors (removed internal labels)
- Full text display (no truncation/ellipsis)
- Dynamically centered input handle

**LoopNode:**
- Dashed border with cyan accent
- Loop icon badge (top-left)
- Iteration counter badge (top-right): "2/10" or "×10"
- Progress bar during execution
- Full dark mode support

### 4. Step Sidebar (StepSidebar.tsx)

**Replaces the old popup NodeDetailPanel:**
- Fixed position on right side (600px width)
- Shows step details: title, description, success criteria
- Inline editing mode for basic fields
- Integrates StepEditPanel for advanced configuration:
  - Agent configs
  - Server selection
  - LLM settings
- Run/Edit/Delete actions

### 5. ChatArea Integration

**ChatArea provides execution infrastructure:**
- Hidden in workflow mode (`<div className="hidden">`)
- Exposes methods via ref:
  - `submitQuery(query)` - Start phase execution
  - `getEvents()` - Get event stream
  - `isStreaming` - Check if running

**WorkflowLayout uses ChatAreaRef:**
```typescript
const handleStartPhase = async (phaseId: string) => {
  setShowChatArea(true)  // Show ChatArea panel on right
  await chatAreaRef.current?.submitQuery(`Start ${phaseId} phase`)
}
```

### 6. Auto-Refresh & Change Highlighting

**Plan Change Detection (`usePlanData.ts`):**
- Tracks previous plan state for comparison
- Detects added, updated, and deleted steps by ID
- Returns `PlanChanges` object with change metadata

```typescript
interface PlanChanges {
  added: string[]      // Step IDs that were added
  updated: string[]    // Step IDs that were updated
  deleted: string[]    // Step IDs that were deleted
  hasChanges: boolean
}
```

**Visual Highlighting:**
- Added steps: Emerald ring + shadow + pulse animation
- Updated steps: Blue ring + shadow + pulse animation
- Deleted steps: Red ring + shadow + reduced opacity
- Badge showing change type (top-right of node)

**Auto-Refresh Flow:**
1. WorkflowLayout listens for `todo_steps_extracted` events
2. On detection, calls `canvasRef.current.refresh()`
3. `usePlanData` compares old vs new plan
4. `usePlanToFlow` applies `changeType` to nodes
5. Node components render highlights
6. Highlights auto-clear after 4 seconds via `clearChanges()`

**Minimized TodoStepsExtractedEvent:**
- In workflow mode, shows collapsed summary by default
- Header displays step count and type badges
- Click to expand for full step list
- Hint: "(view in React Flow canvas)"

### 7. Execution Progress & Step Enabling

**Progress Tracking:**
- Reads `steps_done.json` from selected iteration folder
- Tracks completed step indices (0-based)
- Progress badge shown in toolbar when existing run selected
- Progress data passed to `usePlanToFlow` hook

**Step Enabling Logic:**
- Each step node receives `canRun` prop based on completion status
- Step N can run only if steps 0 through N-1 are all completed
- First step (index 0) can always run
- Run button disabled with tooltip if step cannot run yet

**API Endpoints:**
```
GET /api/workflow/run-folders?workspace_path={path}
Response: { folders: string[] }  // e.g., ["iteration-1", "iteration-2"]

GET /api/workflow/progress?workspace_path={path}&run_folder={folder}
Response: { exists: boolean, progress: StepProgress | null }
```

**StepProgress Interface:**
```typescript
interface StepProgress {
  total_steps: number
  completed_step_indices: number[]  // 0-based indices
  last_updated: string
  branch_steps?: Record<number, BranchStepProgress>
}
```

**Single Step Execution:**
- Clicking run button on node sends `execution_strategy: 'run_single_step'`
- Backend executes only the target step and stops immediately
- Progress saved normally after step completion
- Execution loop breaks after single step completes

---

## API Integration

### Workflow Constants (Phases)
```
GET /api/workflow/constants
Response: { phases: WorkflowPhase[] }
```

### Plan Data
```
GET /api/documents/{workspacePath}/planning/plan.json
PUT /api/documents/{workspacePath}/planning/plan.json
```

### Workflow Execution
```
POST /api/agent/query
Request Body: {
  query: string,
  agent_mode: 'workflow',
  execution_options?: ExecutionOptions  // Frontend-provided options
}

ExecutionOptions: {
  run_mode: 'use_same_run' | 'create_new_runs_always',
  selected_run_folder?: string,
  execution_strategy: string,  // See strategy constants below
  resume_from_step?: number    // 1-based step number
}
```

### Run Folders & Progress
```
GET /api/workflow/run-folders?workspace_path={path}
Response: { folders: string[] }

GET /api/workflow/progress?workspace_path={path}&run_folder={folder}
Response: { exists: boolean, progress: StepProgress | null }
```

**Execution Strategy Constants:**
- `start_from_beginning` - Normal execution with human feedback
- `fast_execute_all` - Fast execution without pausing
- `start_from_beginning_no_human` - Without human feedback (learning enabled)
- `resume_from_step` - Resume from specific step (normal mode)
- `fast_resume_from_step` - Fast resume from step
- `resume_from_step_no_human` - Resume without human feedback
- `run_single_step` - Run only the specified step and stop

---

## Styling

### Theme Support
All components use CSS variables for theme-aware colors:
- `bg-background`, `text-foreground`
- `bg-muted`, `text-muted-foreground`
- `border-border`
- `bg-primary`, `text-primary-foreground`

### Node Status Colors
| Status | Light Mode | Dark Mode |
|--------|------------|-----------|
| Pending | `bg-gray-50` | `bg-gray-800` |
| Running | `bg-blue-50 animate-pulse` | `bg-blue-900/30` |
| Completed | `bg-green-50` | `bg-green-900/30` |
| Failed | `bg-red-50` | `bg-red-900/30` |

### Edge Styles
- Sequential flow: `stroke: #6b7280`, solid
- Conditional Yes: `stroke: #22c55e` with "Yes" label
- Conditional No: `stroke: #ef4444` with "No" label
- Dependencies: `stroke: #8b5cf6`, dashed (toggleable)

---

## Dependencies

```json
{
  "@xyflow/react": "^12.0.0",
  "dagre": "^0.8.5",
  "@types/dagre": "^0.7.52"
}
```

---

## Next Steps

### Immediate
1. Test phase execution flow end-to-end
2. Verify events stream correctly in ChatArea panel
3. Add phase options support (some phases have configurable options)

### Future Enhancements
1. Drag-and-drop step reordering
2. Step duplication
3. Visual step dependencies editing
4. Execution progress indicators on nodes (real-time status updates)
5. Mini-map for large plans
6. Batch step execution (run multiple selected steps)
7. Step execution history/undo

---

## Migration Notes

1. **ChatArea.tsx** - Hidden in workflow mode, provides execution engine
2. **WorkflowPhaseHandler.tsx** - Deprecated, phases moved to toolbar dropdown
3. **NodeDetailPanel.tsx** - Replaced by StepSidebar.tsx
4. **EventDisplay.tsx** - Reused in EventViewer component
5. **Execution Phase ID** - Changed from `'pre-verification'` to `'execution'` (frontend & backend)
6. **Iteration Folders** - Removed `'iteration-same'` option, now uses numbered iterations only
7. **Backend Prompts** - All execution prompts moved to frontend UI, backend uses `ExecutionOptions` struct

---

## Success Criteria

| Criteria | Status |
|----------|--------|
| Plan visualizes as flow diagram | ✅ |
| Left-to-right layout | ✅ |
| All phases load from backend | ✅ |
| Phase descriptions visible | ✅ |
| Run phases from toolbar | ✅ |
| Run individual steps | ✅ |
| Edit steps in sidebar | ✅ |
| Theme-aware (dark mode) | ✅ |
| Events stream in panel | ✅ |
| ChatArea appears on phase start | ✅ |
| Dependency edges toggleable | ✅ |
| Auto-refresh on plan updates | ✅ |
| Highlight added steps (green) | ✅ |
| Highlight updated steps (blue) | ✅ |
| Highlight deleted steps (red) | ✅ |
| Auto-clear highlights (4s timeout) | ✅ |
| Minimized plan event in workflow mode | ✅ |
| Frontend execution controls (3 dropdowns) | ✅ |
| LocalStorage persistence for execution options | ✅ |
| Run single step from node button | ✅ |
| Progress-based step enabling | ✅ |
| Progress badge in toolbar | ✅ |
| Compact toolbar design | ✅ |
| No backend interactive prompts | ✅ |
