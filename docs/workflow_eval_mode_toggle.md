# Workflow Mode Toggle: Plan+Execution vs Eval Mode

## Overview

Add a segmented control toggle to the WorkflowToolbar that switches between two distinct canvas modes:
1. **Plan+Exec Mode** (default): Main workflow with Planning and Execution phases
2. **Eval Mode**: Evaluation steps with Eval Designer, Eval Execution, and Evaluation Debugger phases

When toggling modes, the canvas content completely replaces (not side-by-side).

---

## Architecture

### Current State

The workflow currently shows the main plan steps (`planning/plan.json`) on the canvas with phases like Planning and Execution. Evaluation phases exist but are separate actions that don't change the canvas view.

### Proposed State

Two distinct canvas modes:
- **Plan+Exec Mode**: Shows main workflow steps from `planning/plan.json`
- **Eval Mode**: Shows evaluation steps from `evaluation/evaluation_plan.json`

Each mode has its own:
- Plan data
- Step configurations
- Layout persistence
- Available phases

---

## Critical Files to Modify

| File | Changes |
|------|---------|
| `frontend/src/stores/useWorkflowStore.ts` | Add `workflowMode` state and actions |
| `frontend/src/services/api-types.ts` | Add `EvaluationStep`, `EvaluationPlan` types |
| `frontend/src/components/workflow/canvas/WorkflowToolbar.tsx` | Add mode toggle UI, filter phases by mode |
| `frontend/src/components/workflow/canvas/WorkflowCanvas.tsx` | Conditional rendering based on mode |
| `frontend/src/components/workflow/canvas/StepSidebar.tsx` | Support editing eval step configs |
| `agent_go/pkg/orchestrator/types/workflow_orchestrator.go` | Register `evaluation-debugger` phase |
| `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/evaluation_debugger_manager.go` | Implement Evaluation Debugger |
| `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/step_config.go` | Support separate config file for evaluation |

## New Files to Create

| File | Purpose |
|------|---------|
| `frontend/src/components/workflow/hooks/useEvaluationPlanData.ts` | Load/save evaluation_plan.json and step_config |
| `frontend/src/components/workflow/hooks/useEvaluationPlanToFlow.ts` | Convert eval plan to React Flow nodes |

---

## Implementation Details

### 1. State Management (useWorkflowStore.ts)

Add new state fields to the Zustand store:

```typescript
// New state fields
workflowMode: 'plan' | 'eval'  // default: 'plan'
evaluationPlan: EvaluationPlan | null
evaluationStepProgress: StepProgress | null
isLoadingEvaluationPlan: boolean

// New actions
setWorkflowMode: (mode: 'plan' | 'eval') => void
setEvaluationPlan: (plan: EvaluationPlan | null) => void
loadEvaluationPlan: (workspacePath: string) => Promise<void>
```

### 2. Types (api-types.ts)

Add types matching backend `evaluation_types.go`:

```typescript
// Evaluation step (matches backend EvaluationStep)
export interface EvaluationStep {
  id: string
  title: string
  description: string
  pre_validation?: ValidationSchema
  success_criteria: string
  agent_configs?: AgentConfigs
}

// Evaluation plan structure
export interface EvaluationPlan {
  steps: EvaluationStep[]
}

// Evaluation step config (parallel to step_config.json for main plan)
export interface EvaluationStepConfig {
  id: string
  agent_configs: AgentConfigs
}
```

### 3. Mode Toggle UI (WorkflowToolbar.tsx)

Add segmented control at left side of toolbar:

```tsx
<div className="flex items-center gap-1 bg-gray-100 dark:bg-gray-800 rounded-md p-1 mr-2 border border-gray-200 dark:border-gray-700">
  <button
    onClick={() => setWorkflowMode('plan')}
    className={`px-3 py-1 rounded-md text-xs font-medium transition-all ${
      workflowMode === 'plan'
        ? 'bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 shadow-sm'
        : 'text-gray-500 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-200'
    }`}
  >
    Plan+Exec
  </button>
  <button
    onClick={() => setWorkflowMode('eval')}
    className={`px-3 py-1 rounded-md text-xs font-medium transition-all ${
      workflowMode === 'eval'
        ? 'bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 shadow-sm'
        : 'text-gray-500 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-200'
    }`}
  >
    Eval
  </button>
</div>
```

#### Phase Filtering by Mode

```typescript
const { executionPhase, evalPhases, planPhases, otherPhases, visiblePhases } = useMemo(() => {
  // ...
  const evalPhaseIds = ['evaluation-designer', 'evaluation-execution', 'evaluation-debugger']
  const planPhaseIds = ['planning']
  
  // Note: 'plan-improvement' (Plan Debugger) is now in otherPhases for Plan+Exec mode
  
  // In Eval mode: Show Eval phases
  // In Plan mode: Show Planning phases + Others (excluding execution which has its own button)
  // ...
}, [phases, isLoadingPhases, workflowMode])
```

### 4. Evaluation Plan Data Hook (NEW: useEvaluationPlanData.ts)

Follow pattern of `usePlanData.ts`:

```typescript
export interface UseEvaluationPlanDataReturn {
  evaluationPlan: EvaluationPlan | null
  loading: boolean
  error: string | null
  loadEvaluationPlan: () => Promise<void>
  saveEvaluationPlan: (plan: EvaluationPlan) => Promise<void>
  saveEvaluationStepConfig: (stepId: string, agentConfigs: AgentConfigs | undefined) => Promise<void>
  updateEvaluationStep: (stepIndex: number, updates: Partial<EvaluationStep>) => Promise<void>
  refresh: () => Promise<void>
}
```

Key differences from usePlanData:
- Loads from `evaluation/evaluation_plan.json` (not `planning/plan.json`)
- Loads step configs from `evaluation/step_config.json`
- Simpler structure (no conditional/decision/orchestration steps)

### 5. Evaluation Plan to Flow Hook (NEW: useEvaluationPlanToFlow.ts)

Convert evaluation plan to React Flow nodes:

```typescript
export interface EvaluationStepNodeData extends Record<string, unknown> {
  // ...
  isEvaluationStep: boolean
}
```

This hook will:
- Reuse `StepNode` component (evaluation steps have similar structure)
- Create simpler linear flow (no branching)
- Generate nodes for each evaluation step
- Connect with sequential edges

### 6. Canvas Rendering (WorkflowCanvas.tsx)

Add conditional rendering based on workflow mode:

```typescript
const workflowMode = useWorkflowStore(state => state.workflowMode)

// Use appropriate hook based on mode
const planData = usePlanData(workflowMode === 'plan' ? workspacePath : null)
const evaluationData = useEvaluationPlanData(workflowMode === 'eval' ? workspacePath : null)

// Convert to flow based on mode
const planFlow = usePlanToFlow(planData.plan, /* options */)
const evalFlow = useEvaluationPlanToFlow(evaluationData.evaluationPlan, /* options */)

// Use appropriate flow
const { nodes: initialNodes, edges: initialEdges } = workflowMode === 'plan' ? planFlow : evalFlow
```

### 7. Step Sidebar for Eval Steps (StepSidebar.tsx)

Detect eval mode and show appropriate config options:

```typescript
const isEvaluationStep = useMemo(() => {
  return (node?.data as any)?.isEvaluationStep === true
}, [node])

// Show different sections for eval steps:
// - Title, Description, Success Criteria (editable)
// - Agent configs (LLM selection, etc.)
// - Hide irrelevant options (code exec mode, MCP servers, Phase dropdown, Run button)
```

Save to `evaluation/step_config.json` via backend API (`updatePlannerFile`).

### 8. Layout Persistence

Separate layout files for each mode:

```typescript
const getLayoutFilePath = React.useCallback(() => {
  if (!workspacePath) return null
  return workflowMode === 'plan'
    ? `${workspacePath}/planning/workflow_layout.json`
    : `${workspacePath}/evaluation/eval_layout.json`
}, [workspacePath, workflowMode])
```

---

## Mode-Specific Phases

| Mode | Phases Available |
|------|-----------------|
| Plan+Exec | Planning, Execution, Plan Debugger (Plan Improvement), Tool Optimization, etc. |
| Eval | Eval Designer, Eval Execution, Evaluation Debugger |

---

## Backend Integration

**Backend changes implemented**:

- `evaluation-debugger` phase registered in `agent_go/pkg/orchestrator/types/workflow_orchestrator.go`.
- `EvaluationDebuggerManager` implemented to handle `evaluation-debugger` phase.
- `step_config.go` updated to support reading/writing `step_config.json` in either `planning/` or `evaluation/` directory based on mode.
- `StepBasedWorkflowOrchestrator` methods updated to use correct config path based on `isEvaluationMode`.

---

## Core Concepts: Separation of Concerns

The toggle implements a strict separation between **Workflow Execution** and **Outcome Evaluation**.

### 1. Plan Debugger vs. Evaluation Debugger

The system now provides two distinct debugging phases tailored to the specific mode:

*   **Plan Debugger (Plan+Exec Mode)**: 
    *   **Phase ID**: `plan-improvement`
    *   **Focus**: Analyzes why the workflow failed to achieve its objective.
    *   **Outcome**: Modifies `planning/plan.json` to fix logic errors, missing steps, or incorrect tool usage in the main workflow.
*   **Evaluation Debugger (Eval Mode)**:
    *   **Phase ID**: `evaluation-debugger`
    *   **Focus**: Analyzes why the *evaluation* was inaccurate or failed to run.
    *   **Outcome**: Modifies `evaluation/evaluation_plan.json`. It fixes issues where scores are too low/high because success criteria were vague or pointed to the wrong evidence files.

### 2. Context-Aware Step Configuration (`step_config.json`)

Each mode has its own agent configuration file. This allows for granular control over model selection and parameters without interference:

*   **`planning/step_config.json`**: Controls the agents performing the actual work. (e.g., Use a fast model for repetitive coding tasks).
*   **`evaluation/step_config.json`**: Controls the agents performing the quality assessment. (e.g., Use a high-reasoning model like Claude 3.5 Sonnet to ensure grading is objective and accurate).

---

## File Structure Reference

### Main Plan (Plan+Exec Mode)
```
{workspace}/
├── planning/
│   ├── plan.json              # Main workflow plan
│   ├── step_config.json       # Per-step agent configurations
│   └── workflow_layout.json   # Canvas node positions
└── runs/
    └── {iteration}/
        └── execution/         # Workflow execution outputs
```

### Evaluation Plan (Eval Mode)
```
{workspace}/
├── evaluation/
│   ├── evaluation_plan.json   # Evaluation steps
│   ├── step_config.json       # Per-step agent configurations for eval
│   ├── eval_layout.json       # Canvas node positions (NEW)
│   └── runs/
│       └── {targetRunFolder}/
│           └── execution/     # Evaluation step outputs
```

---

## Verification Checklist

- [x] Mode Toggle Works: Toggle between modes, verify canvas content changes
- [x] Eval Plan Loads: In eval mode, verify evaluation_plan.json loads and renders nodes
- [x] Step Config Editing: Click eval step, edit config, verify saves to `evaluation/step_config.json`
- [x] Phase Execution: In eval mode, click "Eval Designer" button, verify phase runs
- [x] Evaluation Debugger: In eval mode, "Evaluation Debugger" phase is available and runs.
- [x] Layout Persistence: Save layout in eval mode, refresh, verify layout restores
- [x] Existing Evaluation Popup: Open evaluation popup from toolbar, verify still works
- [x] Mode Persistence: Refresh page, verify mode resets to default (or persists if desired)

---

## Notes

- Reuse `StepNode` component for eval steps (add optional `isEvaluationStep` flag for styling)
- Existing `EvaluationPopup` and reports continue to work (independent of canvas mode)
- Evaluation step configs mirror main plan step configs structure