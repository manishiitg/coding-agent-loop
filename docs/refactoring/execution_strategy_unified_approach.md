# Execution Strategy Unified Approach - Comprehensive Analysis

## Overview

This document provides a detailed analysis of all execution strategies, their current implementation, and a unified approach to fix the identified issues.

---

## All Execution Strategies

### Strategy Constants (from `controller_types.go`)

```go
// Fresh start strategies
ExecutionStrategyStartFromBeginning        = "start_from_beginning"          // Normal execution with learning and human feedback
ExecutionStrategyFastExecuteAll            = "fast_execute_all"              // Fast execute all steps (skip learning and human feedback)
ExecutionStrategyStartFromBeginningNoHuman = "start_from_beginning_no_human" // Without human feedback (learning enabled)

// Resume strategies
ExecutionStrategyResumeFromStep        = "resume_from_step"          // Resume from specific step (normal mode)
ExecutionStrategyFastResumeFromStep    = "fast_resume_from_step"     // Fast resume from step
ExecutionStrategyResumeFromStepNoHuman = "resume_from_step_no_human" // Resume without human
ExecutionStrategyFastExecuteRange      = "fast_execute_range"        // Fast execute 0 to step X

// Single step execution
ExecutionStrategyRunSingleStep = "run_single_step" // Run only the specified step and stop
```

---

## Frontend UI Mapping

### 3-Dropdown System

**Dropdown 1: Iteration Selector**
- "New Run" → `run_mode: 'create_new_runs_always'`
- Existing iteration → `run_mode: 'use_same_run'`, `selected_run_folder: 'iteration-X'`

**Dropdown 2: Execution Mode**
| UI Option | ID |
|-----------|-----|
| With Human Approval | `human_approval` |
| Fast Execution | `fast_execution` |
| With Learning | `with_learning` |

**Dropdown 3: Start Point**
| UI Option | `selectedStartPoint` |
|-----------|---------------------|
| Start from Beginning | `0` |
| Resume from Step X | `X` (1-based) |

### Strategy Mapping (from `useWorkflowStore.ts`)

```typescript
// isResuming = selectedStartPoint > 0

if (selectedExecutionMode === 'fast_execution') {
  executionStrategy = isResuming ? FAST_RESUME_FROM_STEP : FAST_EXECUTE_ALL
} else if (selectedExecutionMode === 'with_learning') {
  executionStrategy = isResuming ? RESUME_FROM_STEP_NO_HUMAN : START_FROM_BEGINNING_NO_HUMAN
} else { // human_approval
  executionStrategy = isResuming ? RESUME_FROM_STEP : START_FROM_BEGINNING
}
```

### Combined Matrix

| Execution Mode | Start from Beginning | Resume from Step X |
|----------------|---------------------|-------------------|
| Human Approval | `start_from_beginning` | `resume_from_step` |
| Fast Execution | `fast_execute_all` | `fast_resume_from_step` |
| With Learning | `start_from_beginning_no_human` | `resume_from_step_no_human` |

**Additional strategies:**
- `run_single_step` - Used when clicking play button on individual step node
- `fast_execute_range` - Used interactively (not in current frontend UI)

---

## Detailed Strategy Analysis

### 1. `start_from_beginning` (Fresh Start - Normal)

**Context:** No existing progress OR user selects "Start from Beginning"

**Implementation (line 555-557, 846-859):**
```go
// Fresh start context (no progress)
case ExecutionStrategyStartFromBeginning:
    hcpo.GetLogger().Infof("✅ Frontend chose normal execution from beginning")
    // Defaults are correct (startFromStep=0, no fast mode)

// With existing progress context
case ExecutionStrategyStartFromBeginning:
    hcpo.GetLogger().Infof("🔄 Frontend chose to start from beginning")
    if err := hcpo.deleteStepProgress(ctx); err != nil { ... }
    if err := hcpo.cleanupExecutionArtifactsForFreshStart(...); err != nil { ... }
    existingProgress = nil
    startFromStep = 0
```

**Behavior:**
- `startFromStep = 0`
- `existingProgress = nil` (cleared)
- `fastExecuteMode = false`
- `skipHumanInput = false`

**Execution Loop:** Executes all steps from 0 ✅

---

### 2. `fast_execute_all` (Fresh Start - Fast)

**Context:** User selects "Fast Execution" + "Start from Beginning"

**Implementation (line 558-562, 880-889):**
```go
// Fresh start context
case ExecutionStrategyFastExecuteAll:
    hcpo.GetLogger().Infof("⚡ Frontend chose fast execute mode for all steps")
    fastExecuteMode = true
    fastExecuteEndStep = len(breakdownSteps) - 1

// With existing progress context
case ExecutionStrategyFastExecuteAll:
    hcpo.GetLogger().Infof("⚡ Frontend chose fast execute mode for all steps")
    executionDir := fmt.Sprintf("%s/execution", hcpo.GetWorkspacePath())
    if err := hcpo.CleanupDirectory(ctx, executionDir, "execution"); err != nil { ... }
    fastExecuteMode = true
    fastExecuteEndStep = len(breakdownSteps) - 1
    startFromStep = 0
    existingProgress.CompletedStepIndices = []int{}  // CLEARS ALL
```

**Behavior:**
- `startFromStep = 0`
- `existingProgress.CompletedStepIndices = []int{}` (cleared)
- `fastExecuteMode = true`
- `fastExecuteEndStep = totalSteps - 1`

**Execution Loop:** Executes all steps in fast mode ✅

---

### 3. `start_from_beginning_no_human` (Fresh Start - Learning Mode)

**Context:** User selects "With Learning" + "Start from Beginning"

**Implementation (line 563-565, 907-921):**
```go
// Fresh start context
case ExecutionStrategyStartFromBeginningNoHuman:
    hcpo.GetLogger().Infof("⚡ Frontend chose to start from beginning without human input")
    skipHumanInput = true

// With existing progress context
case ExecutionStrategyStartFromBeginningNoHuman:
    hcpo.GetLogger().Infof("🔄 Frontend chose to start from beginning without human input")
    if err := hcpo.deleteStepProgress(ctx); err != nil { ... }
    if err := hcpo.cleanupExecutionArtifactsForFreshStart(...); err != nil { ... }
    existingProgress = nil
    startFromStep = 0
    skipHumanInput = true
```

**Behavior:**
- `startFromStep = 0`
- `existingProgress = nil` (cleared)
- `fastExecuteMode = false`
- `skipHumanInput = true`

**Execution Loop:** Executes all steps without human approval ✅

---

### 4. `resume_from_step` (Resume - Normal) ❌ BUG

**Context:** User selects "Human Approval" + "Resume from Step X"

**Implementation (line 839-845):**
```go
case ExecutionStrategyResumeFromStep:
    resumeStep := execOpts.ResumeFromStep
    if resumeStep <= 0 {
        resumeStep = nextIncompleteStep
    }
    startFromStep = resumeStep - 1 // Convert to 0-based
    hcpo.GetLogger().Infof("✅ Frontend chose to resume from step %d", resumeStep)
```

**Behavior:**
- `startFromStep = resumeStep - 1`
- `existingProgress.CompletedStepIndices` → **NO CHANGE** ❌
- `fastExecuteMode = false`
- `skipHumanInput = false`

**Execution Loop:**
1. Line 1170-1174: Skips steps before `startFromStep` ✅
2. Line 1176-1196: Checks if step is in completed list → **SKIPS IF COMPLETED** ❌

**Problem:** If user selects "Resume from Step 3" and Step 3 is completed, it gets skipped!

---

### 5. `fast_resume_from_step` (Resume - Fast) ❌ BUG

**Context:** User selects "Fast Execution" + "Resume from Step X"

**Implementation (line 890-898):**
```go
case ExecutionStrategyFastResumeFromStep:
    resumeStep := execOpts.ResumeFromStep
    if resumeStep <= 0 {
        resumeStep = nextIncompleteStep
    }
    hcpo.GetLogger().Infof("⚡ Frontend chose fast resume mode from step %d", resumeStep)
    fastExecuteMode = true
    fastExecuteEndStep = len(breakdownSteps) - 1
    startFromStep = resumeStep - 1
```

**Behavior:**
- `startFromStep = resumeStep - 1`
- `existingProgress.CompletedStepIndices` → **NO CHANGE** ❌
- `fastExecuteMode = true`
- `fastExecuteEndStep = totalSteps - 1`

**Execution Loop:** Same as `resume_from_step` - **SKIPS IF COMPLETED** ❌

---

### 6. `resume_from_step_no_human` (Resume - Learning Mode) ❌ BUG

**Context:** User selects "With Learning" + "Resume from Step X"

**Implementation (line 899-906):**
```go
case ExecutionStrategyResumeFromStepNoHuman:
    resumeStep := execOpts.ResumeFromStep
    if resumeStep <= 0 {
        resumeStep = nextIncompleteStep
    }
    startFromStep = resumeStep - 1
    skipHumanInput = true
    hcpo.GetLogger().Infof("✅ Frontend chose to resume from step %d without human input", resumeStep)
```

**Behavior:**
- `startFromStep = resumeStep - 1`
- `existingProgress.CompletedStepIndices` → **NO CHANGE** ❌
- `skipHumanInput = true`

**Execution Loop:** Same as `resume_from_step` - **SKIPS IF COMPLETED** ❌

---

### 7. `fast_execute_range` (Re-execute Range)

**Context:** Interactive mode only (re-execute steps 0 to X)

**Implementation (line 860-879):**
```go
case ExecutionStrategyFastExecuteRange:
    endStep := execOpts.FastExecuteEndStep
    if endStep <= 0 {
        endStep = max(existingProgress.CompletedStepIndices)
    }
    hcpo.GetLogger().Infof("⚡ Frontend chose fast execute mode (0 to %d)", endStep)
    executionDir := fmt.Sprintf("%s/execution", hcpo.GetWorkspacePath())
    if err := hcpo.CleanupDirectory(ctx, executionDir, "execution"); err != nil { ... }
    fastExecuteMode = true
    fastExecuteEndStep = endStep
    startFromStep = 0
    // Remove completed indices for steps to be re-executed
    var newCompletedIndices []int
    for _, idx := range existingProgress.CompletedStepIndices {
        if idx > fastExecuteEndStep {
            newCompletedIndices = append(newCompletedIndices, idx)
        }
    }
    existingProgress.CompletedStepIndices = newCompletedIndices  // REMOVES steps ≤ endStep
```

**Behavior:**
- `startFromStep = 0`
- `existingProgress.CompletedStepIndices` → **REMOVES** steps ≤ endStep ✅
- `fastExecuteMode = true`
- `fastExecuteEndStep = endStep`

**Execution Loop:** Re-executes steps 0 to endStep ✅

---

### 8. `run_single_step` (Single Step Only) ✅ WORKS

**Context:** User clicks play button on specific step node

**Implementation (line 566-584, 922-944):**
```go
case ExecutionStrategyRunSingleStep:
    targetStep := execOpts.ResumeFromStep
    if targetStep <= 0 {
        targetStep = nextIncompleteStep  // or 1 for fresh start
    }
    hcpo.GetLogger().Infof("🎯 Frontend chose to run single step %d only", targetStep)
    startFromStep = targetStep - 1 // Convert to 0-based
    hcpo.SetRunSingleStepMode(true, startFromStep)
    // Remove target step from completed list to force re-execution
    if existingProgress != nil {
        var newCompletedIndices []int
        for _, idx := range existingProgress.CompletedStepIndices {
            if idx != startFromStep {
                newCompletedIndices = append(newCompletedIndices, idx)
            }
        }
        existingProgress.CompletedStepIndices = newCompletedIndices  // REMOVES target step
        hcpo.GetLogger().Infof("🔄 Removed step %d from completed list to force re-execution", targetStep)
    }
```

**Behavior:**
- `startFromStep = targetStep - 1`
- `existingProgress.CompletedStepIndices` → **REMOVES** target step ✅
- `runSingleStepOnly = true`
- `singleStepTarget = startFromStep`

**Execution Loop (line 1180-1183):**
```go
if hcpo.runSingleStepOnly && i == hcpo.singleStepTarget {
    forceExecution = true  // Forces execution even if completed
}
```

**Result:** Always executes the target step ✅

---

## Execution Loop Analysis

```go
// Line 1170-1174: Skip steps before startFromStep
if i < startFromStep {
    continue  // Skip - this is correct
}

// Line 1176-1196: Check if step is completed
isCompleted := false
forceExecution := false
if hcpo.runSingleStepOnly && i == hcpo.singleStepTarget {
    forceExecution = true  // Only for run_single_step
} else {
    for _, completedIdx := range progress.CompletedStepIndices {
        if completedIdx == i {
            isCompleted = true
            break
        }
    }
}
if isCompleted && !forceExecution {
    continue  // Skip completed steps - THIS IS THE PROBLEM
}
```

---

## Bug Summary

| Strategy | Sets startFromStep | Modifies Completed List | Force Execution | Works? |
|----------|-------------------|------------------------|-----------------|--------|
| `start_from_beginning` | `0` | Clears all | N/A | ✅ |
| `fast_execute_all` | `0` | Clears all (`[]`) | N/A | ✅ |
| `start_from_beginning_no_human` | `0` | Clears all | N/A | ✅ |
| `resume_from_step` | `resumeStep - 1` | **No change** | **No** | ❌ |
| `fast_resume_from_step` | `resumeStep - 1` | **No change** | **No** | ❌ |
| `resume_from_step_no_human` | `resumeStep - 1` | **No change** | **No** | ❌ |
| `fast_execute_range` | `0` | Removes ≤ endStep | N/A | ✅ |
| `run_single_step` | `targetStep - 1` | Removes target | **Yes** | ✅ |

---

## Solution Options

### Option A: Remove from Completed List (Like `run_single_step`)

For each resume strategy, remove the target step from completed list:

```go
case ExecutionStrategyResumeFromStep:
    resumeStep := execOpts.ResumeFromStep
    if resumeStep <= 0 {
        resumeStep = nextIncompleteStep
    }
    startFromStep = resumeStep - 1
    hcpo.GetLogger().Infof("✅ Frontend chose to resume from step %d", resumeStep)
    
    // NEW: Remove target step from completed list if explicitly selected
    if execOpts.ResumeFromStep > 0 && existingProgress != nil {
        var newCompletedIndices []int
        for _, idx := range existingProgress.CompletedStepIndices {
            if idx != startFromStep {
                newCompletedIndices = append(newCompletedIndices, idx)
            }
        }
        existingProgress.CompletedStepIndices = newCompletedIndices
        hcpo.GetLogger().Infof("🔄 Removed step %d from completed list to force re-execution", resumeStep)
    }
```

**Pros:**
- Simple, matches existing `run_single_step` pattern
- Consistent behavior

**Cons:**
- Modifies progress state before execution

---

### Option B: Extend Force Execution Flag

Add a new field to track explicitly selected resume step:

```go
// In controller struct
resumeFromStepTarget int  // 0-based, -1 if not set

// In strategy handling
case ExecutionStrategyResumeFromStep:
    resumeStep := execOpts.ResumeFromStep
    if resumeStep <= 0 {
        resumeStep = nextIncompleteStep
        hcpo.resumeFromStepTarget = -1  // Not explicitly selected
    } else {
        hcpo.resumeFromStepTarget = resumeStep - 1  // Explicit selection
    }
    startFromStep = resumeStep - 1

// In execution loop
if (hcpo.runSingleStepOnly && i == hcpo.singleStepTarget) ||
   (hcpo.resumeFromStepTarget >= 0 && i == hcpo.resumeFromStepTarget) {
    forceExecution = true
}
```

**Pros:**
- Doesn't modify progress until execution
- Clear separation of concerns

**Cons:**
- Adds new state field
- More complex

---

### Option C: Unified Helper Function (Recommended)

Create a helper that handles all resume strategies consistently:

```go
// Helper function
func (hcpo *HumanControlledTodoPlannerOrchestrator) handleResumeStrategy(
    resumeStep int,
    nextIncompleteStep int,
    existingProgress *StepProgress,
    isExplicitSelection bool,
) int {
    if resumeStep <= 0 {
        resumeStep = nextIncompleteStep
        isExplicitSelection = false
    }
    
    startFromStep := resumeStep - 1
    
    // If user explicitly selected a step, remove it from completed list
    if isExplicitSelection && existingProgress != nil {
        var newCompletedIndices []int
        for _, idx := range existingProgress.CompletedStepIndices {
            if idx != startFromStep {
                newCompletedIndices = append(newCompletedIndices, idx)
            }
        }
        existingProgress.CompletedStepIndices = newCompletedIndices
        hcpo.GetLogger().Infof("🔄 Removed step %d from completed list to force re-execution", resumeStep)
    }
    
    return startFromStep
}

// Usage in each resume strategy
case ExecutionStrategyResumeFromStep:
    isExplicit := execOpts.ResumeFromStep > 0
    startFromStep = hcpo.handleResumeStrategy(execOpts.ResumeFromStep, nextIncompleteStep, existingProgress, isExplicit)
    hcpo.GetLogger().Infof("✅ Frontend chose to resume from step %d", startFromStep+1)
```

**Pros:**
- DRY - single implementation for all resume strategies
- Consistent behavior
- Easy to test
- Matches existing `run_single_step` pattern

**Cons:**
- Modifies progress state (but this is acceptable - same as `run_single_step`)

---

## Recommended Solution: Option C (Unified Helper)

### Implementation Plan

1. **Add helper function** `handleResumeStrategy()` in `controller.go`

2. **Update all resume strategies** to use the helper:
   - `resume_from_step`
   - `fast_resume_from_step`
   - `resume_from_step_no_human`

3. **Keep `run_single_step` as-is** (already works correctly)

4. **No changes needed** for:
   - Fresh start strategies (clear all progress anyway)
   - `fast_execute_range` (already removes from completed list)

### Key Insight

The fix is simple: when a user **explicitly selects** a step number (`resume_from_step > 0`), remove that step from the completed list. This matches the behavior of `run_single_step` and is the expected user intent.

When `resume_from_step <= 0` (defaulting to `nextIncompleteStep`), the step is already incomplete, so no removal is needed.

---

## Testing Matrix

| Test Case | Strategy | Step Status | Expected | Current |
|-----------|----------|-------------|----------|---------|
| Resume from Step 3 (completed) | `resume_from_step` | Completed | Execute | ❌ Skip |
| Resume from Step 3 (incomplete) | `resume_from_step` | Incomplete | Execute | ✅ Execute |
| Resume from default (next incomplete) | `resume_from_step` | Incomplete | Execute | ✅ Execute |
| Fast resume from Step 3 (completed) | `fast_resume_from_step` | Completed | Execute | ❌ Skip |
| Fast resume from Step 3 (incomplete) | `fast_resume_from_step` | Incomplete | Execute | ✅ Execute |
| Resume no-human from Step 3 (completed) | `resume_from_step_no_human` | Completed | Execute | ❌ Skip |
| Run single step 3 (completed) | `run_single_step` | Completed | Execute | ✅ Execute |

After fix, all "Expected" = "Current" ✅
