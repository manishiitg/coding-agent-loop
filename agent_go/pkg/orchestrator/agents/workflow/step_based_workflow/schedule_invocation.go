package step_based_workflow

import "time"

// ScheduleInvocation is a trusted server-created binding between one saved
// schedule occurrence and its immutable workflow run folder. It is never
// populated from LLM tool arguments or public execution JSON.
type ScheduleInvocation struct {
	RunID         string
	ScheduleID    string
	RunFolder     string
	TriggerSource string
	ScheduledFor  time.Time
	AllowParallel bool
}
