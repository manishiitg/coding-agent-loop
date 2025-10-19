package types

// ExecutionMode represents the execution mode for orchestrator operations
type ExecutionMode string

const (
	// SequentialExecution runs tasks one after another
	SequentialExecution ExecutionMode = "sequential_execution"

	// ParallelExecution runs tasks concurrently
	ParallelExecution ExecutionMode = "parallel_execution"
)

// String returns the string representation of the execution mode
func (em ExecutionMode) String() string {
	return string(em)
}

// IsValid checks if the execution mode is valid
func (em ExecutionMode) IsValid() bool {
	switch em {
	case SequentialExecution, ParallelExecution:
		return true
	default:
		return false
	}
}

// GetLabel returns a human-readable label for the execution mode
func (em ExecutionMode) GetLabel() string {
	switch em {
	case SequentialExecution:
		return "Sequential Execution"
	case ParallelExecution:
		return "Parallel Execution"
	default:
		return "Parallel Execution" // Default fallback
	}
}

// ParseExecutionMode parses a string into an ExecutionMode, returning ParallelExecution as default
func ParseExecutionMode(mode string) ExecutionMode {
	switch mode {
	case string(SequentialExecution):
		return SequentialExecution
	case string(ParallelExecution):
		return ParallelExecution
	default:
		return ParallelExecution // Default fallback
	}
}

// AllExecutionModes returns all available execution modes
func AllExecutionModes() []ExecutionMode {
	return []ExecutionMode{
		SequentialExecution,
		ParallelExecution,
	}
}
