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
- ✅ **Zustand store architecture** - Centralized workflow state management
- ✅ **Single API call for phases** - Promise-based deduplication prevents redundant calls
- ✅ **Phase dropdown fix** - Works correctly even when in execution phase
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
- ✅ **Clickable file names** - Click context input/output files to open in workspace

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

frontend/src/stores/
├── useWorkflowStore.ts           ✅ Centralized workflow state (phases, progress, execution options)
└── index.ts                      ✅ Store initialization & exports

frontend/src/constants/
└── workflow.ts                   ✅ Static constants only (WORKFLOW_MESSAGES, EXECUTION_PHASE_ID)
```

---

## Key Implementation Details

### 1. Phase Selector (WorkflowToolbar.tsx)

**Dynamic Phase Loading via Zustand Store:**
- Phases loaded once on app initialization via `useWorkflowStore`
- Centralized state management in `stores/useWorkflowStore.ts`
- Promise-based deduplication prevents multiple API calls
- Shows dropdown with phase titles AND descriptions
- Numbered badges (1, 2, 3, etc.)
- Current phase highlighted with checkmark
- Disabled during execution (shows spinner)
- **Fixed:** Dropdown now works even when `currentPhase === 'execution'` (users can switch phases)

**State Management:**
```typescript
// Zustand store manages all workflow state
useWorkflowStore.getState().loadPhases()  // Loads from API once
const phases = useWorkflowStore(state => state.phases)
const isLoadingPhases = useWorkflowStore(state => state.isLoadingPhases)

// Phases loaded from: GET /api/workflow/constants
interface WorkflowPhase {
  id: string           // e.g., "variable-extraction"
  title: string        // e.g., "Variable Extraction"
  description: string  // Full description shown in dropdown
  options?: WorkflowPhaseOption[]
}
```

**Store Architecture:**
- `useWorkflowStore` - Centralized Zustand store for all workflow state
- Phases loaded on app initialization (`App.tsx` calls `initializeStores()`)
- Components use individual selectors for proper reactivity
- Removed deprecated functions from `constants/workflow.ts`

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
- **Clickable file names** - Context input/output files are clickable
  - Click to open file in workspace (same mechanism as workspace sidebar)
  - File path: `{workspacePath}/runs/{selectedRunFolder}/execution/{filename}`
  - Only enabled when valid iteration folder is selected (not 'new')
  - Visual feedback: hover effects (underline, background color change, cursor pointer)
  - Handles file not found errors gracefully with user-friendly messages
  - Processes file content (JSON formatting, image support, escaped characters)

**ConditionalNode:**
- Hexagon shape using CSS clip-path
- Yes/No labels only on edge connectors (removed internal labels)
- Full text display (no truncation/ellipsis)
- Dynamically centered input handle

**OrchestratorNode:**
- Displays all orchestration routes including sub-agent routes
- Shows "End Workflow" as a special route option (always available)
- Routes displayed in a blue info box with route names and conditions
- "End" route shown with red indicator to distinguish from sub-agent routes
- Output handles for each route (blue) plus "end" handle (red)
- Edge to "end" node uses red color and "End" label when orchestrator chooses to terminate

**LoopNode:**
- Dashed border with cyan accent
- Loop icon badge (top-left)
- Iteration counter badge (top-right): "2/10" or "×10"
- Progress bar during execution
- Full dark mode support
- **Clickable file names** - Same file opening functionality as StepNode
  - Context input/output files are clickable
  - Opens files in workspace with same processing as workspace sidebar

### 4. File Opening from Nodes

**Clickable Context Files:**
- Context input/output file names displayed in StepNode and LoopNode are clickable
- Clicking a file name opens it in the workspace panel (same as clicking in workspace sidebar)
- **File Path Construction:**
  - Full path: `{workspacePath}/runs/{selectedRunFolder}/execution/{filename}`
  - Uses `workspacePath` and `selectedRunFolder` from workflow store
  - Only enabled when valid iteration folder is selected (not 'new')

**File Opening Process:**
1. Constructs full file path from workspace path, selected run folder, and filename
2. Fetches file content using `agentApi.getPlannerFileContent(filePath)`
3. Processes content (handles JSON formatting, images, escaped characters)
4. Opens file in workspace panel with proper formatting
5. Highlights file in workspace sidebar
6. Shows error message if file doesn't exist (user-friendly with file path)

**Error Handling:**
- Clears previous errors before attempting to load
- Shows user-friendly error messages: "File not found: {filename}"
- Includes full file path for debugging
- Doesn't open file panel if file doesn't exist
- Errors displayed in workspace panel (consistent with workspace sidebar)

**Visual Feedback:**
- Hover effects: underline, background color change
- Cursor changes to pointer on hover
- Tooltip shows "Click to open: {full path}" when enabled
- Disabled state when no valid iteration folder selected

### 5. Step Sidebar (StepSidebar.tsx)

**Replaces the old popup NodeDetailPanel:**
- Fixed position on right side (600px width)
- Shows step details: title, description, success criteria
- Inline editing mode for basic fields
- Integrates StepEditPanel for advanced configuration:
  - Agent configs
  - Server selection
  - LLM settings
- Run/Edit/Delete actions

### 5.1. Step Legend (StepLegend.tsx)

**Collapsible step list at bottom-left of canvas:**
- Shows **all steps** from plan including branch steps (regular, conditional, loop, and nested branch steps)
- **Includes conditional steps** - Displays with purple GitBranch icon and "Conditional" label
- **Includes loop steps** - Displays with cyan Repeat icon and "Loop" label
- **Includes branch steps** - Steps inside conditional branches (if_true_steps, if_false_steps)
  - Displayed with indentation to show hierarchy
  - Shows "Yes Branch" or "No Branch" badge with green/red colors
  - Shows "Y" or "N" indicator instead of step number
  - Indented based on nesting depth
- Excludes validation nodes and learning nodes
- Click any step to navigate to it on the canvas
- Shows step status icons (pending, running, completed, failed)
- Shows code execution mode icon when enabled
- Displays step number (for top-level), branch indicator (for branch steps), title, and type badge
- Collapsible to save space (collapsed by default)

### 6. ChatArea Integration & Observer ID Management

**Single Source of Truth for Observer ID:**
- `useChatStore.observerId` is the single source of truth
- ChatArea registers observer on mount → stores in `useChatStore` + syncs to API module
- `useWorkflowExecution` uses `useChatStore` selectors (no local state)
- No localStorage for observer ID - uses module-level variable in `api.ts`

**Observer ID Flow:**
```
ChatArea mounts
  └── Registers observer via agentApi.registerObserver()
  └── Stores in useChatStore.observerId
  └── Calls setCurrentObserverId() to sync to API module
  └── Polls with useChatStore.observerId → events to useChatStore.events

useWorkflowExecution (on Execute)
  └── Gets observerId from useChatStore (same as ChatArea's)
  └── agentApi.startQuery uses same observerId in X-Observer-ID header
  └── Backend stores events with SAME observer ID
  └── ChatArea polling receives events → useChatStore.events
  └── WorkflowLayout detects 'todo_steps_extracted' events
```

**useWorkflowExecution - Single Source of Truth:**
- Uses `useChatStore` selectors: `observerId`, `events`, `isStreaming`, `isCompleted`
- No local polling - relies on ChatArea's polling
- Derives `status` directly from store states (no redundant event scanning):
  - `isStreaming` → `'running'` (source of truth for all execution paths)
  - `isCompleted` → `'completed'`
  - `manualStatus` → for stop/pause overrides
- Works for ALL execution paths: toolbar Execute, run-from-step button, any agent

**WorkflowLayout - Uses useWorkflowStore:**
- `activePhase` and `showChatArea` from `useWorkflowStore` (not local state)
- Single source of truth for workflow UI state

**ChatArea provides execution infrastructure:**
- Hidden in workflow mode (`<div className="hidden">`)
- Manages observer registration and event polling
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

### 7. Auto-Refresh & Change Highlighting

**Plan Change Detection (`usePlanData.ts`):**
- Tracks previous plan state for comparison (merged plan.json + step_config.json)
- Detects added, updated, and deleted steps by ID
- Returns `PlanChanges` object with change metadata
- Compares merged states to detect changes in both files

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

**Backend Event Emission (`planning_management.go`):**
All agents that modify `plan.json` or `step_config.json` emit `todo_steps_extracted` event:
- **Planning Agent**: Emits when plan is created/updated in planning phase
- **Plan Improvement Agent**: Uses `CheckAndEmitPlanUpdateEvent()` helper after execution
- **Plan Tool Optimization Agent**: Uses `CheckAndEmitPlanUpdateEvent()` helper after execution

The shared helper `CheckAndEmitPlanUpdateEvent()`:
1. Extracts tool calls from conversation history
2. Checks for plan modification tools (`update_plan_steps`, `delete_plan_steps`, `add_plan_steps`, etc.)
3. Checks for step_config modification tools (`update_step_config_tools`)
4. If any modification tool was called, reads plan.json and emits event
5. Frontend then fetches and merges both plan.json and step_config.json

**Auto-Refresh Flow:**
1. WorkflowLayout listens for `todo_steps_extracted` events
2. On detection, calls `canvasRef.current.refresh()`
3. `usePlanData` fetches plan.json + step_config.json and merges them
4. Compares old vs new merged plan
5. `usePlanToFlow` applies `changeType` to nodes
6. Node components render highlights
7. Highlights auto-clear after 4 seconds via `clearChanges()`

**Minimized TodoStepsExtractedEvent:**
- In workflow mode, shows collapsed summary by default
- Header displays step count and type badges
- Click to expand for full step list
- Hint: "(view in React Flow canvas)"

### 8. Execution Progress & Step Enabling

**Progress Tracking:**
- Reads `steps_done.json` from selected iteration folder
- Tracks completed step indices (0-based)
- Progress badge shown in toolbar when existing run selected
- Progress data passed to `usePlanToFlow` hook

**Real-Time Progress Updates (`step_progress_updated` event):**
Backend emits `step_progress_updated` event whenever `steps_done.json` is updated:
- Triggered after each step completion (regular, branch, or conditional steps)
- Triggered during fast→normal mode transitions
- Contains full progress state (completed indices, branch steps, total steps)

```typescript
// Event payload
interface StepProgressUpdatedEvent {
  completed_step_indices: number[]  // 0-based indices of completed steps
  total_steps: number               // Total steps in plan
  workspace_path: string            // For file operations
  run_folder: string                // e.g., "iteration-1"
  last_completed_step: number       // Most recently completed step (-1 if unknown)
  branch_steps?: {                  // Branch progress for conditional steps
    [stepIndex: number]: {
      branch_executed: string       // "if_true" or "if_false"
      completed_steps: string[]     // e.g., ["step-3-if-true-0"]
    }
  }
}
```

Frontend can listen for this event to dynamically update:
- Step completion status in dropdown and node UI
- Progress badges in toolbar
- "Completed" visual styling on step nodes

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
4. ✅ **Completed:** Zustand store refactoring for workflow state management
5. ✅ **Completed:** Fixed phase dropdown loading and reactivity issues

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
8. **State Management** - Migrated from scattered local state to centralized `useWorkflowStore` (Zustand)
9. **Phase Loading** - Removed deprecated `getWorkflowPhases()` and `getDefaultWorkflowPhase()` functions
10. **Constants Cleanup** - `constants/workflow.ts` now only contains static constants, no API calls
11. **Component Refactoring** - All workflow components now use Zustand selectors for proper reactivity
12. **Observer ID Management** - Removed localStorage, uses `useChatStore.observerId` as single source of truth
13. **useWorkflowExecution Refactor** - Uses `useChatStore` selectors (`isStreaming`, `isCompleted`), no redundant event scanning
14. **WorkflowLayout Refactor** - Uses `useWorkflowStore` for `activePhase`/`showChatArea` (removed local state)
15. **Execution Status** - `isStreaming` from ChatArea is source of truth for all execution paths

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
| Zustand store for workflow state | ✅ |
| Single API call for phases (deduplication) | ✅ |
| Phase dropdown works in execution phase | ✅ |
| Removed deprecated phase functions | ✅ |
| Clickable file names in nodes | ✅ |
| File opening from workflow nodes | ✅ |
| Error handling for missing files | ✅ |
| Single observer ID (useChatStore) | ✅ |
| No localStorage for observer ID | ✅ |
| Stop button works for all execution paths | ✅ |
| No redundant state (single source of truth) | ✅ |
| "End" route displayed in OrchestratorNode | ✅ |
| "End" handle on orchestrator nodes (red) | ✅ |
| Routes list shows "End Workflow" option | ✅ |
| Edge to "end" uses red color and label | ✅ |
