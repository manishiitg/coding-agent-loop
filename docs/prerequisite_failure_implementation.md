# Prerequisite Failure Detection - Implementation Guide

## Overview

**Status**: ✅ **COMPLETED**

Per-step manual configuration via UI. Users enable prerequisite detection for specific steps and configure **prerequisite rules** - each rule specifies one step dependency and one description of when to detect prerequisite failures. When validation fails due to a missing prerequisite (e.g., expired login session, missing config file), the validation agent evaluates all rules and navigates back to the matching prerequisite step instead of retrying the current step.

**Key Design**: Each prerequisite rule has **one** step dependency and **one** description, ensuring clear, unambiguous prerequisite detection logic.

---

## Data Model

### Step Configuration (`step_config.json`)

```json
{
  "steps": [{
    "id": "step-2",
    "agent_configs": {
      "enable_prerequisite_detection": true,  // Default: false
      "prerequisite_rules": [
        {
          "depends_on_step": "step-0",  // Step ID this rule depends on
          "description": "If login session is missing or expired, go back to step 0"  // User description for this specific step
        },
        {
          "depends_on_step": "step-1",
          "description": "If config file is missing, go back to step 1"
        }
      ]
    }
  }]
}
```

**Key Points**:
- Each rule has **one** step dependency and **one** description
- Multiple rules can be configured for different prerequisite scenarios
- The validation agent evaluates all rules and uses the first matching one

### Backend Types

**`planning_agent.go`**:
```go
type PrerequisiteRule struct {
    DependsOnStep string `json:"depends_on_step"` // Step ID this rule depends on
    Description   string `json:"description"`     // User description for this specific step
}

type AgentConfigs struct {
    EnablePrerequisiteDetection *bool              `json:"enable_prerequisite_detection,omitempty"`
    PrerequisiteRules           []PrerequisiteRule `json:"prerequisite_rules,omitempty"` // Array of prerequisite rules
}
```

**`validation_agent.go`**:
```go
type ValidationResponse struct {
    FailureType         string `json:"failure_type,omitempty"`  // "prerequisite" | "execution"
    ShouldRetryFromStep *int   `json:"should_retry_from_step,omitempty"`  // 0-based index
    RetryReason         string `json:"retry_reason,omitempty"`
}
```

---

## Frontend

### 1. Prerequisite Config Panel

**Component**: `PrerequisiteConfigPanel.tsx`

**UI**:
```
┌─────────────────────────────────┐
│ ☑ Enable prerequisite detection │
│                                 │
│ Prerequisite Rules:             │
│ ┌─────────────────────────────┐ │
│ │ Rule 1                    [×]│ │
│ │ Depends on step:            │ │
│ │ [Select a step...]          │ │
│ │ Detection description:      │ │
│ │ ┌─────────────────────────┐ │ │
│ │ │ If login session is     │ │ │
│ │ │ missing or expired, go │ │ │
│ │ │ back to step 0          │ │ │
│ │ └─────────────────────────┘ │ │
│ └─────────────────────────────┘ │
│ ┌─────────────────────────────┐ │
│ │ Rule 2                    [×]│ │
│ │ Depends on step:            │ │
│ │ [Select a step...]          │ │
│ │ Detection description:      │ │
│ │ ┌─────────────────────────┐ │ │
│ │ │ If config file is       │ │ │
│ │ │ missing, go back to    │ │ │
│ │ │ step 1                 │ │ │
│ │ └─────────────────────────┘ │ │
│ └─────────────────────────────┘ │
│ [+ Add prerequisite rule]        │
└─────────────────────────────────┘
```

**Key Features**:
- Each rule has its own step selector and description field
- Users can add multiple rules for different prerequisite scenarios
- Each rule is independent - one step dependency per rule

**Integration**: Add to `StepSidebar.tsx` in StepEditPanel section.

### 2. React Flow Visualization

**Changes to `usePlanToFlow.ts`**:
- Add `prerequisite` edge type (dashed, orange/purple)
- Toggle to show/hide (like context dependencies)
- Highlight navigation path on events

---

## Backend

### 1. Gather Prerequisite Info

**Function**: `gatherPrerequisiteInfo()` in `controller_execution.go`

**Logic**:
- Check if `enable_prerequisite_detection` is true
- Get `prerequisite_rules` array from config
- For each prerequisite rule:
  - Get the `depends_on_step` and `description` from the rule
  - Check dependency step completion status
  - Check context output file status
  - Get validation status
  - Build `PrerequisiteRuleInfo` with step info and description
- Return `PrerequisiteInfo` JSON (includes description)

### 2. Pass to Validation Agent

**In `executeSingleStep()`**, before validation:
```go
prerequisiteInfo, _ := hcpo.gatherPrerequisiteInfo(...)
if prerequisiteInfo != nil {
    prerequisiteInfoJSON, _ := json.Marshal(prerequisiteInfo)
    validationTemplateVars["PrerequisiteInfo"] = string(prerequisiteInfoJSON)
    validationTemplateVars["EnablePrerequisiteDetection"] = "true"
}
```

**PrerequisiteInfo Structure**:
```go
type PrerequisiteInfo struct {
    CurrentStepID               string                 `json:"current_step_id"`
    CurrentStepIndex            int                    `json:"current_step_index"`
    EnablePrerequisiteDetection bool                   `json:"enable_prerequisite_detection"`
    PrerequisiteRules           []PrerequisiteRuleInfo `json:"prerequisite_rules"`
}

type PrerequisiteRuleInfo struct {
    DependsOnStep      string             `json:"depends_on_step"`
    Description        string             `json:"description"`
    DependencyStepInfo DependencyStepInfo `json:"dependency_step_info"`
}
```

### 3. Validation Agent Prompt

**Add conditional section** (only when enabled):
- **Parse Prerequisite Rules**: The prerequisite information contains an array of prerequisite rules
- **Each Rule Contains**:
  - `depends_on_step`: The step ID this rule depends on
  - `description`: User description of when to detect prerequisite failures for this specific step
  - `dependency_step_info`: Information about the dependency step (step index, title, completion status, context output)
- **Evaluate Each Rule**:
  - Analyze the description to understand when to detect prerequisite failures
  - Parse the description to extract condition (e.g., "login is missing") and target step
  - Analyze execution history to check if condition is met
  - Check dependency step info (completion status, context output existence)
- **Decision**:
  - If ANY rule's condition is met → PREREQUISITE FAILURE → set `should_retry_from_step` to the matching rule's `dependency_step_info.step_index`
  - If NO rule's condition is met → EXECUTION FAILURE → retry current step
- **Examples**:
  - Rule 1: "If login session is missing or expired, go back to step 0" → If "session expired" detected → navigate to step 0
  - Rule 2: "If config file is missing, go back to step 1" → If "config file not found" → navigate to step 1

### 4. Navigation Logic

**After validation**:
```go
if validationResponse.ShouldRetryFromStep != nil {
    targetStepIndex := *validationResponse.ShouldRetryFromStep
    // Validate target (not before start, not in different branch)
    // Clean up progress from target step onward
    // Emit prerequisite_navigation event
    // Return navigation error (handled by caller)
}
```

### 5. Progress Cleanup

**Function**: `cleanupProgressFromStep()`
- Remove completed indices from target step onward
- Clean up branch steps
- Save updated progress

---

## Event System

### Event Type

**`events/types.go`**: Add `PrerequisiteNavigation = "prerequisite_navigation"`

**`events/data.go`**:
```go
type PrerequisiteNavigationEvent struct {
    BaseEventData
    FromStepIndex int
    ToStepIndex   int
    Reason        string
    FailureType   string
}
```

### Frontend Handling

**In `WorkflowCanvas.tsx`**:
- Listen for `prerequisite_navigation` events
- Highlight navigation path
- Show notification

---

## Implementation Status

✅ **All phases completed**

1. ✅ **Data Model**: Added `PrerequisiteRule` type and `prerequisite_rules` array to `AgentConfigs`, `ValidationResponse` with failure detection fields
2. ✅ **Backend Gathering**: Implemented `gatherPrerequisiteInfo()` in `controller_execution.go` to process multiple prerequisite rules
3. ✅ **Backend Navigation**: Implemented navigation logic + `cleanupProgressFromStep()` in `controller_progress.go`
4. ✅ **Frontend UI**: Created `PrerequisiteConfigPanel` with rule-based UI (each rule has one step + one description) + integrated into `StepEditPanel`
5. ✅ **Frontend Viz**: Added prerequisite edges to React Flow in `usePlanToFlow.ts` (one edge per rule)
6. ✅ **Events**: Added `PrerequisiteNavigation` event type + frontend handling in `useWorkflowExecution.ts`
7. ✅ **Node Visualization**: Added prerequisite badge and rule display in `StepNode.tsx`
8. ✅ **Validation Agent**: Updated prompt to handle multiple prerequisite rules, evaluating each rule independently

---

## Safety Limits

- **Max Navigation Distance**: 10 steps
- **Max Navigation Attempts**: 3 per step
- **Validation**: Can't navigate to future steps, different branches, or before step 0

---

## Success Criteria

- ✅ Per-step enable/disable via UI
- ✅ Configure multiple prerequisite rules per step (each rule has one step dependency + one description)
- ✅ Visual edges in React Flow (orange dashed edges, one per rule)
- ✅ Detection + navigation works (validation agent evaluates all rules, uses first matching one)
- ✅ Safety limits prevent loops (max 10 steps distance)
- ✅ Edge cases handled
- ✅ Node visualization shows prerequisite badge and all rules
- ✅ Event system emits `prerequisite_navigation` events
- ✅ Progress cleanup resets state from target step onward
- ✅ Rule-based structure ensures one description per step dependency (no ambiguity)

---

## Key Files

**Backend**:
- `planning_agent.go` - `PrerequisiteRule` struct and `AgentConfigs` with `prerequisite_rules` array
- `validation_agent.go` - `ValidationResponse` with `FailureType`, `ShouldRetryFromStep`, `RetryReason` + updated prompt to handle multiple rules
- `controller_execution.go` - `gatherPrerequisiteInfo()` processes multiple rules, navigation logic in `executeSingleStep()`, error handling in `runExecutionPhase()`
- `controller_progress.go` - `cleanupProgressFromStep()` function
- `mcpagent/events/types.go` - `PrerequisiteNavigation` event type
- `mcpagent/events/data.go` - `PrerequisiteNavigationEvent` struct

**Frontend**:
- `frontend/src/components/workflow/canvas/PrerequisiteConfigPanel.tsx` - Configuration UI component
- `frontend/src/components/events/orchestrator/StepEditPanel.tsx` - Integration point
- `frontend/src/components/workflow/canvas/StepSidebar.tsx` - Passes `planSteps` to `StepEditPanel`
- `frontend/src/components/workflow/nodes/StepNode.tsx` - Prerequisite badge and dependency display
- `frontend/src/components/workflow/hooks/usePlanToFlow.ts` - `createPrerequisiteEdges()` function
- `frontend/src/components/workflow/hooks/useWorkflowExecution.ts` - Event handling for `prerequisite_navigation`
- `frontend/src/utils/stepConfigMatching.ts` - `AgentConfigs` interface with prerequisite fields

## How It Works

### User Configuration

1. User enables prerequisite detection for a specific step via the UI
2. User selects which previous steps this step depends on
3. User provides a natural language description (e.g., "If login session is missing or expired, go back to step 0")

### Execution Flow

1. **During Validation**: The validation agent receives:
   - Prerequisite detection description
   - Information about dependency steps (completion status, context outputs, etc.)
   - Execution history

2. **Failure Analysis**: The validation agent analyzes:
   - Whether the condition in the description is met
   - If it's a prerequisite failure (missing dependency) vs execution failure (retry needed)

3. **Navigation Decision**:
   - If `failure_type == "prerequisite"` → Navigate to `should_retry_from_step`
   - If `failure_type == "execution"` → Retry current step (no navigation)

4. **Navigation Process**:
   - Validate target step (must be before current step, within max distance)
   - Clean up progress from target step onward
   - Emit `prerequisite_navigation` event
   - Restart execution from target step

### Safety Features

- **Max Navigation Distance**: 10 steps (prevents excessive backtracking)
- **Validation**: Target step must be before current step
- **Explicit Check**: Only navigates when `failure_type == "prerequisite"`
- **Progress Cleanup**: Resets completed steps and branch steps from target onward

## Example Use Cases

1. **Login Session Expiration**:
   - Step 0: Login to website
   - Step 1: Scrape data
   - If Step 1 fails with "session expired" → Navigate back to Step 0

2. **Config File Missing**:
   - Step 0: Generate config file
   - Step 1: Read and use config
   - If Step 1 fails with "config file not found" → Navigate back to Step 0

3. **Database Connection Lost**:
   - Step 0: Establish database connection
   - Step 1: Query data
   - If Step 1 fails with "connection lost" → Navigate back to Step 0
