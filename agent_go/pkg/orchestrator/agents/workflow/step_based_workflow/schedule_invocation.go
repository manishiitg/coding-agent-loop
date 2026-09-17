package step_based_workflow

import (
	"context"
	"time"
)

// ScheduleInvocation is a trusted server-created binding between one saved
// schedule occurrence and its immutable workflow run folder. It is never
// populated from LLM tool arguments or public execution JSON.
type ScheduleInvocation = ExternalInvocation

// ExternalInvocation is the shared immutable-run binding for trusted producers.
type ExternalInvocation struct {
	ExecutionID   string
	Next          func(context.Context, string) (*ExternalInvocation, error) `json:"-"`
	Current       func() *ExternalInvocation                                 `json:"-"`
	Kind          string
	RunID         string
	ScheduleID    string
	RunFolder     string
	TriggerSource string
	ScheduledFor  time.Time
	AllowParallel bool
}

func (i *ExternalInvocation) RunKind() string {
	if i.Kind != "" {
		return i.Kind
	}
	return "schedule"
}

func (i *ExternalInvocation) BoundRunFolder() string {
	if i.Current != nil {
		if current := i.Current(); current != nil {
			return current.RunFolder
		}
	}
	return i.RunFolder
}

func (i *ExternalInvocation) BoundRunID() string {
	if i.Current != nil {
		if current := i.Current(); current != nil {
			return current.RunID
		}
	}
	return i.RunID
}
