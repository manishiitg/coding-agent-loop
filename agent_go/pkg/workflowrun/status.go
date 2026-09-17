// Package workflowrun defines the persisted workflow-run outcome contract.
//
// A chat turn, scheduler job, and Pulse pass each have their own lifecycle.
// None of them may reinterpret or overwrite the workflow execution outcome.
package workflowrun

import "strings"

type Status string

const (
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusCanceled  Status = "canceled"
)

// CanonicalStatus normalizes values written by older binaries. New writers
// must persist one of the four Status constants above and record non-fatal
// conditions as warnings instead of inventing another outcome.
func CanonicalStatus(raw string) Status {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "running":
		return StatusRunning
	case "completed", "success", "completed_with_persistence_error":
		return StatusCompleted
	case "failed", "error":
		return StatusFailed
	case "canceled", "cancelled", "stopped":
		return StatusCanceled
	default:
		return ""
	}
}

func IsSuccessful(raw string) bool {
	return CanonicalStatus(raw) == StatusCompleted
}

func IsTerminal(raw string) bool {
	switch CanonicalStatus(raw) {
	case StatusCompleted, StatusFailed, StatusCanceled:
		return true
	default:
		return false
	}
}
