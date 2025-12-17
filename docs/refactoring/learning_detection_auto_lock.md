# Learning Detection & Auto-Lock Feature

## Overview

**Automatic Learning Detection** uses an LLM agent to detect if new learnings are being generated after each learning phase. After 2 consecutive iterations with no new learnings detected, the step's learnings are automatically locked to prevent unnecessary learning agent runs.

## Key Concepts

### Learning Detection Agent
- **Purpose**: Semantically compare old vs new learning content to detect genuinely new knowledge
- **Input**: Previous learnings content (combined from all learning files) + Current learnings content (combined from all learning files)
- **Output**: `{has_new_learning: bool, reasoning: string, confidence: float}`
- **LLM Config**: Uses same learning LLM config pattern as other learning agents
- **Note**: Receives actual content (not file paths), supports multiple learning files per step

### Auto-Lock Mechanism
- **Trigger**: 2 consecutive iterations with `has_new_learning = false`
- **Action**: Set `LockLearnings = true` in `step_config.json` for the affected step
- **Config-Based**: Lock state is stored in `step_config.json` via `AgentConfigs.LockLearnings` boolean field
- **Event**: Emits `TodoStepsExtracted` event with `changed_step_ids` metadata to notify frontend of config update

### What Counts as "New Learning"
✅ **New Learning Detected**:
- New code patterns/workflows not seen before
- New failure patterns with root causes
- New optimal path markers (⭐ OPTIMAL)
- New prerequisites/error recovery strategies
- New output file formats documented
- Significant score improvements indicating learning

❌ **NOT New Learning**:
- Minor score updates (e.g., [Runs: 5 → 6] with same pattern)
- Formatting changes or reorganization
- Repetition of existing patterns
- Refinements to existing code without new approaches

## Data Structure

### Learning Detection Response

```go
type LearningDetectionResponse struct {
    HasNewLearning bool    `json:"has_new_learning"`
    Reasoning      string  `json:"reasoning"`
    Confidence     float64 `json:"confidence"` // 0.0 to 1.0
}
```

**Note**: `NewContentSummary` field was removed as it was not required.

### Learning Metadata (per step)

**File**: `learnings/step-{X}/.learning_metadata.json`

```json
{
  "step_id": "step-3",
  "step_path": "step-3",
  "total_iterations": 5,
  "consecutive_no_new_learning": 2,
  "last_learning_detected_at": "2025-01-27T10:30:00Z",
  "last_detection_reasoning": "New optimal path identified",
  "last_detection_confidence": 0.95,
  "last_consolidation_at": "2025-01-27T10:30:00Z",
  "last_consolidation_output": "Consolidated 3 files into step-3_learning.md",
  "detection_history": [
    {
      "iteration": 3,
      "timestamp": "2025-01-27T09:30:00Z",
      "has_new_learning": false,
      "reasoning": "Only score updates, no new patterns",
      "confidence": 0.85
    },
    {
      "iteration": 4,
      "timestamp": "2025-01-27T10:00:00Z",
      "has_new_learning": false,
      "reasoning": "Minor formatting changes only",
      "confidence": 0.90
    },
    {
      "iteration": 5,
      "timestamp": "2025-01-27T10:30:00Z",
      "has_new_learning": true,
      "reasoning": "New optimal path identified",
      "confidence": 0.95
    }
  ],
  "consolidation_history": [
    {
      "iteration": 5,
      "timestamp": "2025-01-27T10:30:00Z",
      "output": "Consolidated 3 files into step-3_learning.md",
      "new_learning_content": "# New learning content from _learning_new.md...",
      "consolidated_file": "step-3_learning.md"
    }
  ]
}
```

**Note**: Metadata file stores:
- Detection history: All previous detection results (limited to last 50 entries)
- Consolidation history: All previous consolidation results with output and new learning content (for tracking what happened over time)
- Counters: Total iterations and consecutive no-new-learning count
- Last detection: Most recent detection result (for quick access)
- Last consolidation: Most recent consolidation result (for quick access)

It does NOT store:
- Full learning content (read directly from files when needed)
- Previous learnings content (read directly from files before extraction agent runs)

## Implementation

### Files Created

1. **`learning_detection_agent.go`**
   - `HumanControlledTodoPlannerLearningDetectionAgent` struct
   - Agent implementation with structured output
   - Prompt for semantic comparison

2. **`controller_learning_detection.go`**
   - `detectNewLearningWithLLM()` - Run detection agent (compares combined content of all learning files)
   - `updateLearningMetadata()` - Update metadata file with detection results
   - `updateConsolidationMetadata()` - Update metadata file with consolidation results
   - `autoLockStepLearningsInConfig()` - Set `LockLearnings = true` in `step_config.json` and emit event
   - `unlockStepLearningsInConfig()` - Set `LockLearnings = false` in `step_config.json`
   - `unlockStepLearningsAndResetMetadata()` - Unlock in config and reset metadata counter (for plan modifications)
   - `savePreviousLearningsToMetadata()` - Save combined content of learning files to metadata before execution
   - `createUnlockLearningsFunction()` - Creates unlock function for planning agent executors
   - `createUnlockLearningsFunctionFromBase()` - Creates unlock function using base orchestrator (for plan improvement agent)
   - `readStepLearningFiles()` - Reads all learning files (excluding metadata) from step folder
   - `formatStepLearningFilesAsHistory()` - Formats learning files into combined content string

3. **`learning_consolidation_agent.go`** (✅ NEW)
   - `HumanControlledTodoPlannerLearningPhaseConsolidationAgent` struct
   - Agent implementation for consolidating, scoring, and optimizing learnings
   - Handles multiple learning files and folders
   - Deletes temporary files and irrelevant folders

4. **`controller_learning_consolidation.go`** (✅ NEW)
   - `runLearningConsolidationPhase()` - Orchestrates consolidation agent execution
   - Reads `_learning_new.md` before consolidation
   - Captures consolidation output and stores in metadata

### Integration Points

**File**: `controller_learning.go`

After `runSuccessLearningPhase()` or `runFailureLearningPhase()` completes:
```go
// 1. Read previous learnings BEFORE learning phase runs
previousLearningsContent := ...

// 2. Execute extraction agent (writes to _learning_new.md)
extractionOutput, _, err := learningAgent.Execute(...)
newLearningFilePath := filepath.Join(..., "_learning_new.md")

// 3. Run consolidation agent (reads _learning_new.md, writes consolidated file)
if err := hcpo.runLearningConsolidationPhase(
    ctx, stepIndex, stepPath, learningPathIdentifier, step, 
    newLearningFilePath, isCodeExecutionMode,
); err != nil {
    // Handle error
}

// 4. Run detection agent (compares previous vs current)
hasNewLearning, reasoning, confidence, err := hcpo.detectNewLearningWithLLM(
    ctx, stepIndex, stepPath, learningPathIdentifier, 
    step.AgentConfigs, previousLearningsContent, step,
)

// 5. Update metadata and check auto-lock
if err == nil {
    shouldAutoLock, err := hcpo.updateLearningMetadata(
        ctx, stepIndex, stepPath, hasNewLearning, reasoning, confidence,
    )
    if shouldAutoLock {
        err := hcpo.autoLockStepLearnings(ctx, stepIndex, step.ID)
    }
}
```

### Agent Factory

**File**: `controller_agent_factory.go`

```go
func (hcpo *HumanControlledTodoPlannerOrchestrator) createLearningDetectionAgent(
    ctx context.Context,
    agentName string,
    stepConfig *AgentConfigs,
) (agents.OrchestratorAgent, error)
```

- Uses same LLM config pattern as learning agents
- No MCP servers (pure LLM analysis)
- Structured output format
- Read-only access to learnings folder

## Execution Flow

```
1. Before Learning Phase
   - Read current learning files from step folder (captures state BEFORE learning agents run)
   - Combine into single content string (previousLearningsContent)
   ↓
2. Learning Extraction Agent Runs
   - Analyzes execution history and validation results
   - Extracts task-specific success and failure patterns
   - Filters out general programming errors (NOT learnings)
   - Writes raw learning content to: _learning_new.md (temporary file)
   ↓
3. Learning Consolidation Agent Runs
   - Reads new learning content from: _learning_new.md
   - Reads all existing learning files
   - Consolidates multiple files into single {StepTitle}_learning.md
   - Updates scores [Runs: X | Success: Y%]
   - Marks ⭐ OPTIMAL paths and ⚠️ UNRELIABLE paths
   - Deletes temporary file _learning_new.md (MANDATORY)
   - Deletes old/duplicate files and irrelevant folders
   - Stores consolidation output in .learning_metadata.json
   ↓
4. Run Learning Detection Agent
   - Read current learning files (all .md files in step folder, excluding metadata)
   - Combine current files into single content string
   - If no previous learnings: skip LLM, return true (first iteration)
   - If no current learnings: skip LLM, return false (no new learning)
   - LLM compares previous (from step 1) vs current (from step 3) content semantically
   ↓
5. Update Metadata
   - If new learning: reset consecutive_no_new_learning = 0
   - If no new learning: increment consecutive_no_new_learning
   - Store detection result in detection_history (without content)
   - Store consolidation result in consolidation_history (with output and new learning content)
   ↓
6. Check Auto-Lock Condition
   - If consecutive_no_new_learning >= 2:
     → Set LockLearnings = true in step_config.json
     → Emit TodoStepsExtracted event with changed_step_ids metadata
```

## Lock Mechanism (Step Config Based)

**Implementation**: Lock state is stored in `step_config.json` via the `LockLearnings` boolean field in `AgentConfigs`.

### Lock State Location
- **Storage**: `planning/step_config.json`
- **Field**: `AgentConfigs.LockLearnings` (boolean pointer: `true` = locked, `false` = unlocked, `nil` = default/unlocked)
- **Scope**: Per-step configuration

### Lock Check Implementation
```go
// Check if learnings are locked (config-based)
func isStepLearningsLocked(stepConfig *AgentConfigs) bool {
    return stepConfig != nil && 
           stepConfig.LockLearnings != nil && 
           *stepConfig.LockLearnings == true
}
```

### Lock/Unlock Operations
```go
// Auto-lock: Set LockLearnings = true in step_config.json
func (hcpo *HumanControlledTodoPlannerOrchestrator) autoLockStepLearningsInConfig(
    ctx context.Context,
    stepID string,
    reasoning string,
) error

// Unlock: Set LockLearnings = false in step_config.json
func (hcpo *HumanControlledTodoPlannerOrchestrator) unlockStepLearningsInConfig(
    ctx context.Context,
    stepID string,
) error
```

### Event Emission
When auto-locking occurs, the system emits a `TodoStepsExtracted` event with:
- `changed_step_ids`: Array containing the locked step ID
- `config_update_only`: `true` (indicates only config was updated, not plan structure)
- This allows the frontend to dynamically update the React Flow node without full plan reload

## Edge Cases

- **First iteration**: No previous learnings in metadata → skip LLM, return `true` (treat as new learning)
- **No current learnings**: No learning files found → skip LLM, return `false` (no new learning)
- **Multiple learning files**: All `.md` files (and `.go` files in `code/` subfolder) are combined for comparison
- **Metadata missing**: Create new metadata, treat as "new learning" (first iteration)
- **Detection agent fails**: Log warning, don't update metadata, don't auto-lock
- **Branch steps**: Track separately per branch path (`step-3-true-0`)
- **Config missing**: If step config doesn't exist, create it when locking
- **Metadata file exclusion**: `.learning_metadata.json` is explicitly ignored when reading learnings for execution agent

## Plan Modification Agents Integration

### Planning Agent & Plan Improvement Agent

**When plan is modified** (add/delete/update steps), these agents must:
1. **Unlock affected steps**: Delete `.lock` file for modified steps
2. **Reset learning metadata**: Reset `consecutive_no_new_learning = 0` in metadata
3. **Handle step reordering**: When steps are renumbered, unlock and reset metadata for affected steps

### Integration Points

**File**: `planning_agent.go` - Plan modification tool executors

After plan modification tools update `plan.json`:
- `createUpdatePlanStepsExecutor()` - After updating steps
- `createDeletePlanStepsExecutor()` - After deleting steps  
- `createSingleStepAdder()` - After adding steps
- `createConvertStepToConditionalExecutor()` - After converting steps
- Branch step tools - After modifying branch steps

**File**: `plan_opt_improvement_agent.go` - Plan improvement agent

After plan modification via improvement agent:
- After any plan modification tool call
- Unlock and reset metadata for affected steps

### Unlock Logic

```go
// Unlock learnings for a step (set LockLearnings = false in config and reset metadata)
func (hcpo *HumanControlledTodoPlannerOrchestrator) unlockStepLearningsAndResetMetadata(
    ctx context.Context,
    stepID string,
    stepIndex int,
    stepPath string,
    learningPathIdentifier string,
) error {
    // Unlock in step config (set LockLearnings = false)
    if err := hcpo.unlockStepLearningsInConfig(ctx, stepID); err != nil {
        hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to unlock learnings in config: %v", err))
        // Continue to reset metadata even if config unlock fails
    }
    
    // Reset metadata: consecutive_no_new_learning = 0
    metadataPath := filepath.Join(baseWorkspacePath, "learnings", learningPathIdentifier, ".learning_metadata.json")
    // Read, update, write metadata
    // ...
}
```

**Helper Functions**:
- `createUnlockLearningsFunction()` - Creates unlock function for planning agent (uses full orchestrator)
- `createUnlockLearningsFunctionFromBase()` - Creates unlock function for plan improvement agent (uses base orchestrator)

### When to Unlock

**Unlock learnings when**:
- Step is updated (title, description, success_criteria, etc.)
- Step is deleted (cleanup lock file)
- Step is added (no lock should exist)
- Step is reordered (step number changed - unlock old and new positions)
- Step is converted (regular ↔ conditional ↔ decision)
- Branch steps are added/updated/deleted

**Reason**: Plan changes may invalidate existing learnings, so steps should re-learn from scratch.

### Step Reordering Handling

**When steps are reordered** (e.g., step 3 becomes step 2):
1. **Before reordering**: Identify old step number from step ID
2. **After reordering**: Identify new step number from updated plan
3. **Unlock both positions**: 
   - Delete `.lock` at old path: `learnings/step-{oldNumber}/.lock`
   - Delete `.lock` at new path: `learnings/step-{newNumber}/.lock`
4. **Reset metadata**: Reset metadata for both old and new positions
5. **Note**: Learning folder rename (via `syncLearningsFolders`) happens separately - unlock should happen after rename

**Implementation**: Use step ID to track step across reordering, then determine both old and new step numbers.

### Implementation in Plan Tools

**Integration**: All plan modification tool executors now accept an optional `unlockLearningsFunc` parameter that is called after successful plan modifications.

**Example**: `createUpdatePlanStepsExecutor()` in `planning_agent.go`

```go
// After plan is written successfully
if unlockLearningsFunc != nil {
    for _, stepID := range updatedStepIDs {
        // Find the step index in the updated plan
        stepIndex := -1
        for i, s := range plan.Steps {
            if s.ID == stepID {
                stepIndex = i
                break
            }
        }
        if stepIndex >= 0 {
            if err := unlockLearningsFunc(ctx, stepID, stepIndex); err != nil {
                logger.Warn(fmt.Sprintf("⚠️ Failed to unlock learnings for updated step %s: %v", stepID, err))
            } else {
                logger.Info(fmt.Sprintf("🔓 Unlocked learnings for updated step %s (plan was modified)", stepID))
            }
        }
    }
}
```

**Similar pattern implemented in**:
- `createDeletePlanStepsExecutor()` - Unlocks deleted steps (uses old step indices before deletion)
- `createSingleStepAdder()` - Unlocks newly added steps (finds step index in new plan)
- All step adder variants (regular, conditional, decision, loop)

**Registration**: `registerPlanModificationTools()` accepts `unlockLearningsFunc` parameter and passes it to all executor creators.

## User Control

- **Manual lock**: User sets `LockLearnings = true` in `step_config.json` (via frontend or directly)
- **Manual unlock**: User sets `LockLearnings = false` in `step_config.json` (via frontend or directly)
- **Unlock resets counter**: When user unlocks, reset `consecutive_no_new_learning = 0` in metadata (can be implemented in frontend)
- **Plan modification unlocks**: When planning/improvement agents modify plan, affected steps are automatically unlocked
- **Frontend display**: Show lock status based on `LockLearnings` field in `step_config.json`
  - Read `AgentConfigs.LockLearnings` to determine lock state
  - Display lock status in React Flow node (dynamically updated via events)
- **Reasoning display**: Show last detection reasoning in UI (from metadata)

## Events

### `learning_detection_completed`
```json
{
  "step_id": "step-3",
  "step_index": 2,
  "has_new_learning": false,
  "reasoning": "Only score updates, no new patterns",
  "confidence": 0.85,
  "consecutive_no_new_learning": 2
}
```

### `TodoStepsExtracted` (with auto-lock metadata)
When a step is auto-locked, a `TodoStepsExtracted` event is emitted with:
```json
{
  "steps": [{
    "id": "step-3",
    "title": "Step Title",
    // ... other step fields
  }],
  "metadata": {
    "changed_step_ids": ["step-3"],
    "config_update_only": true
  }
}
```

This allows the frontend to:
- Dynamically update the React Flow node without full plan reload
- Show lock status in the UI
- Update step config display

## Configuration

- **Threshold**: 2 consecutive iterations (hardcoded, can be made configurable)
- **LLM Model**: Uses step-specific `LearningLLM` config or preset default
- **Performance**: Fast/cheap model recommended (e.g., GPT-4o-mini, Claude Haiku)

## Implementation Details

### Lock Mechanism: Step Config Based
- **Storage**: `LockLearnings` boolean field in `AgentConfigs` struct (in `step_config.json`)
- **Check**: `step.AgentConfigs.LockLearnings != nil && *step.AgentConfigs.LockLearnings == true`
- **Auto-lock**: Sets `LockLearnings = true` in `step_config.json` when threshold reached
- **Unlock**: Sets `LockLearnings = false` in `step_config.json` (manual or via plan modification)

### Learning Content Handling
- **Multiple files**: Supports multiple learning files per step (all `.md` files and `.go` files in `code/` subfolder)
- **Content combination**: All learning files are combined into a single content string for comparison
- **Metadata exclusion**: `.learning_metadata.json` is explicitly ignored when reading learnings
- **Previous learnings**: Read directly from files BEFORE the learning phase runs (not stored in metadata)
- **Current learnings**: Read from step folder AFTER consolidation completes and combined using `formatStepLearningFilesAsHistory()`
- **Detection history in metadata**: Metadata file stores all previous detection results (iteration, timestamp, has_new_learning, reasoning, confidence) - limited to last 50 entries
- **Consolidation history in metadata**: Metadata file stores all previous consolidation results (iteration, timestamp, output, new_learning_content, consolidated_file) - for tracking what happened over time
- **No full learning content in metadata**: Metadata file does NOT store the full learning content, only detection results, consolidation results, and counters

### Temporary File Handling
- **Extraction output**: Extraction agent writes to `_learning_new.md` (temporary file, no step title prefix)
- **Consolidation input**: Consolidation agent reads from `_learning_new.md`
- **Consolidation output**: Consolidation agent writes to `{StepTitle}_learning.md` (final consolidated file)
- **Cleanup**: Consolidation agent MUST delete `_learning_new.md` after successful consolidation (MANDATORY)
- **New learning content storage**: Content from `_learning_new.md` is stored in consolidation metadata before deletion (for history tracking)

### Folder Consolidation and Cleanup
- **Multiple learning files**: Consolidation agent consolidates multiple `*_learning.md` files into single `{StepTitle}_learning.md`
- **Scripts folder**: Consolidation agent consolidates multiple `*.py`/`*.sh` files in `scripts/` folder into single best/merged script file
- **Code folder**: Consolidation agent consolidates multiple `*.go` files in `code/` folder into single best/merged code file
- **Execution mode cleanup**: Consolidation agent deletes irrelevant folders based on execution mode:
  - **Code execution mode**: Deletes `scripts/` folder (not used in code execution mode)
  - **Simple mode**: Deletes `code/` folder (not used in simple mode)
- **Duplicate cleanup**: Consolidation agent deletes old/duplicate files after consolidation

### Detection Logic
- **First iteration**: If no previous learnings exist, skip LLM call and return `true` (new learning)
- **No current learnings**: If no learning files found, skip LLM call and return `false` (no new learning)
- **Content comparison**: LLM receives combined content strings (not file paths) for semantic comparison
- **Variable naming**: Template variables use `PreviousLearningsContent` and `CurrentLearningsContent` (not "File")

## Multi-Agent Learning Architecture (✅ IMPLEMENTED)

### Problem Solved
The original learning agent performed **12+ distinct tasks**, leading to:
- Cognitive overload (too many responsibilities in one prompt)
- Quality issues (rushed consolidation, inconsistent pattern matching)
- Maintenance difficulties (large, complex prompts)
- Conflicting priorities (extraction vs consolidation vs scoring)

### Implemented Multi-Agent Architecture

#### **Agent 1: Learning Extraction Agent** (✅ IMPLEMENTED)
**Purpose**: Extract patterns from execution history (both success and failure)
**Responsibilities**:
- Analyze execution history and validation results
- Extract MCP tool calls, Python scripts, output file formats
- Extract **success patterns** (what worked and why)
- Extract **failure patterns** (what failed, root causes, and how to avoid) - **TASK-SPECIFIC ONLY**
- Handle variable replacement ({{VARIABLE_NAME}} placeholders)
- Generate new learning content (workflows or tool recipes)
- Learn from both successful and failed executions
- **Filter out general programming errors** (syntax errors, unused variables, etc.) - these are NOT learnings
- **Output**: Raw learning content to temporary file `_learning_new.md` (not yet merged/scored)

**Input**: Execution history, validation results, step context
**Output**: New learning content written to `_learning_new.md` (temporary file)

**Key Features**:
- Single agent handles both success and failure analysis
- Focuses ONLY on task-specific learnings (excludes general programming knowledge)
- Does NOT merge with existing files (consolidation agent handles that)
- Does NOT update scores or optimal paths (consolidation agent handles that)

**Files**: `learning_agent.go`, `learning_agent_code_execution.go`

#### **Agent 2: Learning Consolidation Agent** (✅ IMPLEMENTED)
**Purpose**: Merge, consolidate, score, and optimize learning files
**Responsibilities**:
- Read new learning content from `_learning_new.md` (temporary file)
- Read all existing learning files (*_learning.md, *.go in code/, *.py/.sh in scripts/)
- **Consolidate multiple learning files** into single `{StepTitle}_learning.md` file
- **Consolidate scripts/ folder**: Merge multiple *.py/.sh files into single best/merged script file
- **Consolidate code/ folder**: Merge multiple *.go files into single best/merged code file
- Merge duplicate patterns (combine run counts, recalculate success rates)
- Keep latest/best patterns (by score and recency)
- **Pattern matching** (normalize to {{VARS}} for comparison)
- **Score calculation** ([Runs: X | Success: Y%])
- **Score updates** (increment runs, recalculate success rates)
- **Identify optimal path** (highest score combination)
- **Mark optimal path** with ⭐ OPTIMAL tag
- **Deprecate unreliable paths** (<50% success) as ⚠️ UNRELIABLE
- Write consolidated file `{StepTitle}_learning.md`
- **Delete temporary file** `_learning_new.md` (MANDATORY cleanup)
- **Delete old/duplicate files** that were consolidated
- **Delete irrelevant folders** based on execution mode:
  - Code execution mode: Delete `scripts/` folder (not used)
  - Simple mode: Delete `code/` folder (not used)
- **Store consolidation output** in `.learning_metadata.json` for history tracking

**Input**: New learning content from `_learning_new.md` + existing learning files
**Output**: Consolidated learning file `{StepTitle}_learning.md` with updated scores and optimal markers

**Key Features**:
- Combines consolidation, scoring, and optimization in single agent
- Handles multiple learning files and folders
- Cleans up temporary files and irrelevant folders
- Stores consolidation history in metadata

**Files**: `learning_consolidation_agent.go`, `controller_learning_consolidation.go`

#### **Agent 3: Learning Detection Agent** (✅ IMPLEMENTED)
**Purpose**: Detect if genuinely new learning occurred
**Responsibilities**:
- Read old learning file (before current run)
- Read new learning file (after consolidation/scoring)
- Semantic comparison (LLM-based)
- Determine if new learning occurred
- Update metadata (consecutive_no_new_learning counter)
- Trigger auto-lock when threshold reached

**Input**: Old learning file + New learning file
**Output**: `{has_new_learning: bool, reasoning: string, confidence: float}`

### Execution Flow (✅ IMPLEMENTED)

```
Execution Completes
    ↓
1. Before Learning Phase
   - Read current learning files from step folder (captures state BEFORE learning agents run)
   - Combine into single content string (previousLearningsContent)
   ↓
2. Learning Extraction Agent
   - Analyzes execution history and validation results
   - Extracts task-specific success and failure patterns
   - Filters out general programming errors (NOT learnings)
   - Writes raw learning content to: _learning_new.md (temporary file)
   - Does NOT merge with existing files
   - Does NOT update scores or optimal paths
   ↓
3. Learning Consolidation Agent
   - Reads new learning content from: _learning_new.md
   - Reads all existing learning files (*_learning.md, *.go in code/, *.py/.sh in scripts/)
   - Consolidates multiple learning files into single {StepTitle}_learning.md
   - Consolidates scripts/ folder (if simple mode): Merge multiple *.py/.sh into single file
   - Consolidates code/ folder (if code exec mode): Merge multiple *.go into single file
   - Merges duplicate patterns, updates scores [Runs: X | Success: Y%]
   - Identifies and marks ⭐ OPTIMAL paths
   - Marks ⚠️ UNRELIABLE paths (<50% success)
   - Writes consolidated file: {StepTitle}_learning.md
   - Deletes temporary file: _learning_new.md (MANDATORY)
   - Deletes old/duplicate files that were consolidated
   - Deletes irrelevant folders (scripts/ in code exec mode, code/ in simple mode)
   - Stores consolidation output in .learning_metadata.json
   ↓
4. Learning Detection Agent
   - Reads current learning files (all .md files in step folder, excluding metadata)
   - Combines current files into single content string
   - If no previous learnings: skip LLM, return true (first iteration)
   - If no current learnings: skip LLM, return false (no new learning)
   - LLM compares previous (from step 1) vs current (from step 3) content semantically
   - Determines if genuinely new learning occurred
   ↓
5. Update Metadata
   - If new learning: reset consecutive_no_new_learning = 0
   - If no new learning: increment consecutive_no_new_learning
   - Store detection result in detection_history (without content)
   - Store consolidation result in consolidation_history (with output and new learning content)
   ↓
6. Check Auto-Lock Condition
   - If consecutive_no_new_learning >= 2:
     → Set LockLearnings = true in step_config.json
     → Emit TodoStepsExtracted event with changed_step_ids metadata
```

### Key Differences Between Agents

#### **Extraction vs Consolidation**

| Aspect | Extraction | Consolidation |
|--------|-----------|----------------|
| **Input** | Execution history, validation results | New learning content + existing learning files |
| **Output** | Raw learning patterns to `_learning_new.md` | Consolidated file with scores and optimal markers |
| **Process** | Analysis & creation | Merging, scoring, optimization, cleanup |
| **Focus** | "What happened? What worked? What failed?" | "Merge, score, optimize, and clean up" |
| **Data Type** | Content (text, patterns) | Content + Numbers (scores) + Tags (⭐, ⚠️) |
| **LLM Complexity** | High (needs to understand execution) | Medium (pattern matching + math + comparison) |
| **Handles** | Both success and failure patterns (task-specific only) | Consolidation, scoring, optimization, cleanup |
| **File Operations** | Writes temporary file | Reads temp file, writes consolidated file, deletes temp and duplicates |
| **Folder Management** | None | Consolidates scripts/ and code/ folders, deletes irrelevant folders |

#### **Why This Architecture?**

1. **Extraction Agent** handles both success and failure:
   - Single agent analyzes execution history
   - Extracts what worked (success patterns)
   - Extracts what failed (failure patterns with root causes) - **TASK-SPECIFIC ONLY**
   - Filters out general programming errors (syntax, unused variables, etc.)
   - No need for separate success/failure agents
   - Simpler architecture
   - Focused on extraction only (no consolidation, scoring, or optimization)

2. **Consolidation Agent combines multiple responsibilities**:
   - Consolidation (merging multiple files)
   - Scoring (updating [Runs: X | Success: Y%])
   - Optimization (identifying ⭐ OPTIMAL paths)
   - Cleanup (deleting temp files, duplicates, irrelevant folders)
   - All operations work with existing learning content and scores
   - Can be done in a single pass through the file
   - Reduces complexity (3 agents instead of 4)
   - Still maintains clear separation of concerns

3. **Clear separation**:
   - **Extraction** = Creates new content from execution (fundamentally different)
   - **Consolidation** = Merges, scores, optimizes, and cleans up (file management + metrics)
   - **Detection** = Compares old vs new (semantic analysis)

### Benefits of Multi-Agent Architecture

1. **Focused Prompts**: Each agent has a single, clear responsibility
2. **Better Quality**: Specialized agents produce higher quality outputs
3. **Easier Debugging**: Issues isolated to specific agents
4. **Independent Improvement**: Can optimize each agent separately
5. **Efficiency**: Combined scoring & optimization reduces passes through learning file
6. **Modularity**: Can disable/replace individual agents if needed
7. **Testability**: Each agent can be tested independently

### Implementation Strategy (✅ COMPLETED)

#### Phase 1: Extract Detection Agent (✅ COMPLETED)
- ✅ Create `learning_detection_agent.go`
- ✅ Create `controller_learning_detection.go`
- ✅ Integrate after learning phase completes

#### Phase 2: Extract Consolidation Agent (✅ COMPLETED)
- ✅ Create `learning_consolidation_agent.go`
- ✅ Create `controller_learning_consolidation.go`
- ✅ Move consolidation, scoring, and optimization logic from extraction agent
- ✅ Run after extraction, before detection
- ✅ Handle temporary file `_learning_new.md`
- ✅ Consolidate multiple learning files and folders
- ✅ Delete temporary files and irrelevant folders
- ✅ Store consolidation history in metadata

#### Phase 3: Simplify Extraction Agent (✅ COMPLETED)
- ✅ Remove consolidation logic
- ✅ Remove scoring logic
- ✅ Remove optimization logic
- ✅ Focus only on extraction (both success and failure patterns)
- ✅ Filter out general programming errors (task-specific only)
- ✅ Write to temporary file `_learning_new.md`
- ✅ Cleaner, more focused prompts

### Agent Communication

**Shared State**: Learning files on disk
- Each agent reads previous agent's output
- Each agent writes its output for next agent
- No direct agent-to-agent communication needed

**Error Handling**:
- If extraction fails → Skip all subsequent agents (consolidation, detection)
- If consolidation fails → Log error, detection agent will compare previous learnings with raw extraction output (if it exists)
- If detection fails → Log warning, don't update metadata, don't auto-lock

### Configuration

Each agent can have:
- **Separate LLM configs**: Use cheaper models for simple tasks (consolidation), better models for complex tasks (extraction, detection)
- **Separate max turns**: Consolidation needs fewer turns than extraction
- **Separate timeouts**: Detection can be faster than consolidation

### Task-Specific Learning Filter

**CRITICAL PRINCIPLE**: Only capture learnings that are SPECIFIC to executing this task better. General programming knowledge is NOT a learning.

**Extraction Agent Filters**:
- ✅ **Include**: Task-specific execution failures (wrong tool usage, wrong approach, wrong data format, missing prerequisites)
- ❌ **Exclude**: General programming errors (syntax errors, unused variables, type errors, compilation errors, code quality issues)
- ❌ **Exclude**: Language-specific best practices that are general knowledge

**Why**: General programming errors (like "declared and not used: userId") are general knowledge the LLM already knows, not task-specific learnings about how to execute this particular step better.

## Status

### Phase 1: Learning Detection (✅ COMPLETED)
- [x] Create `learning_detection_agent.go`
- [x] Create `controller_learning_detection.go`
- [x] Integrate into `controller_learning.go`
- [x] Add metadata file management
- [x] Implement config-based lock mechanism (`LockLearnings` in `step_config.json`)
- [x] Add `autoLockStepLearningsInConfig()` function
- [x] Add `unlockStepLearningsInConfig()` function
- [x] Add `unlockStepLearningsAndResetMetadata()` function
- [x] Add `createUnlockLearningsFunction()` helper
- [x] Add `createUnlockLearningsFunctionFromBase()` helper
- [x] Add `savePreviousLearningsToMetadata()` function
- [x] Support multiple learning files (combine all `.md` and `.go` files)
- [x] Exclude metadata file when reading learnings for execution agent
- [x] Skip detection if no previous learnings (first iteration)
- [x] Remove `new_content_summary` field from detection response
- [x] Update variable names to reflect content (not files)
- [x] Integrate unlock logic into planning agent tool executors
  - [x] `createUpdatePlanStepsExecutor()` - Unlock updated steps
  - [x] `createDeletePlanStepsExecutor()` - Unlock deleted steps
  - [x] `createSingleStepAdder()` - Unlock newly added steps
  - [x] All step adder variants (regular, conditional, decision, loop)
- [x] Integrate unlock logic into plan improvement agent
  - [x] After plan modification tool calls
  - [x] Unlock affected steps using base orchestrator
- [x] Add event emission for frontend (`TodoStepsExtracted` with `changed_step_ids`)
- [x] Handle edge cases (first iteration, no current learnings, multiple files)
- [x] Update `registerPlanModificationTools()` to accept unlock function parameter

### Phase 2: Learning Consolidation (✅ COMPLETED)
- [x] Create `learning_consolidation_agent.go`
- [x] Create `controller_learning_consolidation.go`
- [x] Move consolidation, scoring, and optimization logic from extraction agent
- [x] Integrate after extraction, before detection
- [x] Handle temporary file `_learning_new.md`
- [x] Consolidate multiple learning files (*_learning.md)
- [x] Consolidate scripts/ folder (multiple *.py/.sh files)
- [x] Consolidate code/ folder (multiple *.go files)
- [x] Delete temporary file after consolidation
- [x] Delete old/duplicate files after consolidation
- [x] Delete irrelevant folders based on execution mode
- [x] Store consolidation output in metadata

### Phase 3: Simplify Extraction Agent (✅ COMPLETED)
- [x] Remove consolidation logic
- [x] Remove scoring logic
- [x] Remove optimization logic
- [x] Focus only on extraction (both success and failure patterns)
- [x] Filter out general programming errors (task-specific only)
- [x] Write to temporary file `_learning_new.md`
- [x] Simplify prompts with better structure
- [x] Add decision criteria for task-specific vs general knowledge
- [x] Ensure it handles both success and failure analysis

## Implementation Status

### ✅ Completed Features

1. **Learning Detection Agent**
   - ✅ Created `learning_detection_agent.go` with structured output
   - ✅ Semantic comparison of previous vs current learnings content
   - ✅ Supports multiple learning files (combines all files for comparison)
   - ✅ Skips LLM call if no previous learnings (first iteration)
   - ✅ Skips LLM call if no current learnings (returns false)

2. **Auto-Lock Mechanism**
   - ✅ Tracks consecutive iterations with no new learning
   - ✅ Auto-locks after 2 consecutive iterations
   - ✅ Uses `LockLearnings` field in `step_config.json`
   - ✅ Emits `TodoStepsExtracted` event for frontend updates

3. **Metadata Management**
   - ✅ Tracks detection history with reasoning and confidence (stores all previous detection results)
   - ✅ Detection history limited to last 50 entries to prevent unbounded growth
   - ✅ Resets counter when new learning detected
   - ✅ Excludes metadata file when reading learnings for execution agent
   - ✅ Previous learnings read directly from files (not stored in metadata)

4. **Plan Modification Integration**
   - ✅ Unlock logic integrated into all plan modification tools
   - ✅ Unlocks steps when updated, deleted, or added
   - ✅ Works with both planning agent and plan improvement agent
   - ✅ Resets metadata counter when unlocking

5. **Event Emission**
   - ✅ Emits `TodoStepsExtracted` event when auto-locking
   - ✅ Includes `changed_step_ids` metadata for granular updates
   - ✅ Frontend can dynamically update React Flow nodes

### 📝 Notes

- Lock mechanism uses `step_config.json` (not `.lock` files) as per user requirement
- Detection agent receives content (not file paths) for comparison
- Multiple learning files are supported and combined for comparison
- Metadata file is excluded from learning content passed to execution agent

