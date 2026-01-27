package step_based_workflow

// ExecutionMode defines the type of execution
type ExecutionMode string

const (
	// ExecutionModeFresh starts from scratch - delete all progress and artifacts
	ExecutionModeFresh ExecutionMode = "fresh"

	// ExecutionModeResume continues from last incomplete step - no cleanup needed
	ExecutionModeResume ExecutionMode = "resume"

	// ExecutionModeResumeFromStep resumes from a specific step N - cleans step N and all after
	ExecutionModeResumeFromStep ExecutionMode = "resume_from_step"

	// ExecutionModeSingleStep runs only one specific step - cleans only that step
	ExecutionModeSingleStep ExecutionMode = "single_step"

	// ExecutionModeFastExecute fast re-executes all steps - delete all and run without human input
	ExecutionModeFastExecute ExecutionMode = "fast_execute"

	// ExecutionModeFastExecuteRange fast executes steps 0 to N - delete those steps
	ExecutionModeFastExecuteRange ExecutionMode = "fast_execute_range"
)

// CleanupScope defines WHAT should be cleaned (decided upfront, executed later)
// This separates the decision-making from the execution
type CleanupScope struct {
	// === Progress file (steps_done.json) ===

	// DeleteProgress deletes the existing steps_done.json file
	DeleteProgress bool

	// InitFreshProgress creates a new steps_done.json with empty completed list
	InitFreshProgress bool

	// UpdateProgress updates existing progress by removing steps >= StartFromStep
	// Used when resuming from a specific step to mark subsequent steps as not done
	UpdateProgress bool

	// === Execution folders ===

	// CleanAllSteps deletes the entire execution/ folder (all step-* subdirectories)
	CleanAllSteps bool

	// CleanFromStep deletes step-{N} through step-{TotalSteps} folders (1-based)
	// 0 means no cleanup. Used when resuming from step N to clean N and all after.
	CleanFromStep int

	// CleanSpecificStep deletes only step-{N} folder (1-based)
	// 0 means no cleanup. Used for single-step execution.
	CleanSpecificStep int

	// === Metadata ===

	// NewTotalSteps is the total number of steps for fresh progress initialization
	NewTotalSteps int
}

// ExecutionSetup contains the fully resolved execution configuration
// This is the output of PrepareExecution - everything needed to run
type ExecutionSetup struct {
	// Mode is the resolved execution mode
	Mode ExecutionMode

	// Context contains immutable execution flags (skipHumanInput, fastExecuteMode, etc.)
	Context *ExecutionContext

	// Cleanup defines what should be cleaned before execution
	Cleanup CleanupScope

	// StartFromStep is the 0-based index to start execution from
	StartFromStep int

	// RunFolder is the target run folder path (e.g., "iteration-1" or "iteration-1/group-1" or "iteration-1/production" for display names)
	RunFolder string

	// GroupID is the current group ID for batch execution (empty for single execution)
	GroupID string

	// VariableValues are the variable values for this execution (for batch: group-specific)
	VariableValues map[string]string

	// SkipExecutionCleanup skips all execution folder cleanup when true
	SkipExecutionCleanup bool
}

// HasCleanup returns true if any cleanup is needed
func (cs CleanupScope) HasCleanup() bool {
	return cs.DeleteProgress || cs.InitFreshProgress || cs.UpdateProgress ||
		cs.CleanAllSteps || cs.CleanFromStep > 0 || cs.CleanSpecificStep > 0
}

// HasFolderCleanup returns true if any folder cleanup is needed
func (cs CleanupScope) HasFolderCleanup() bool {
	return cs.CleanAllSteps || cs.CleanFromStep > 0 || cs.CleanSpecificStep > 0
}

// HasProgressCleanup returns true if any progress cleanup is needed
func (cs CleanupScope) HasProgressCleanup() bool {
	return cs.DeleteProgress || cs.InitFreshProgress || cs.UpdateProgress
}

// Clone creates a deep copy of ExecutionSetup
func (es *ExecutionSetup) Clone() *ExecutionSetup {
	if es == nil {
		return nil
	}

	clone := &ExecutionSetup{
		Mode:                 es.Mode,
		Cleanup:              es.Cleanup, // CleanupScope is a value type, so this copies
		StartFromStep:        es.StartFromStep,
		RunFolder:            es.RunFolder,
		GroupID:              es.GroupID,
		SkipExecutionCleanup: es.SkipExecutionCleanup,
	}

	// Deep copy Context
	if es.Context != nil {
		clone.Context = &ExecutionContext{
			SkipHumanInput:     es.Context.SkipHumanInput,
			FastExecuteMode:    es.Context.FastExecuteMode,
			FastExecuteEndStep: es.Context.FastExecuteEndStep,
			RunSingleStepOnly:  es.Context.RunSingleStepOnly,
			SingleStepTarget:   es.Context.SingleStepTarget,
			IsEvaluationMode:   es.Context.IsEvaluationMode,
		}
	}

	// Deep copy VariableValues
	if es.VariableValues != nil {
		clone.VariableValues = make(map[string]string, len(es.VariableValues))
		for k, v := range es.VariableValues {
			clone.VariableValues[k] = v
		}
	}

	return clone
}
