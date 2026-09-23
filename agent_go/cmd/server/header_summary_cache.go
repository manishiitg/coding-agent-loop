package server

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// The schedule half of /api/header-summary resolved every schedule's runtime
// state (reconcile + two schedule-runs.json reads + a durable-run lookup) on
// each 5s poll from every open tab. Its answer only changes when a scheduled
// run starts or finishes, or a manifest changes; those paths bump the
// generation below. The safety TTL bounds drift from anything unhooked.
const scheduleSummarySafetyTTL = 30 * time.Second

var scheduleSummaryGeneration atomic.Uint64

func noteScheduleSummaryChange() { scheduleSummaryGeneration.Add(1) }

type scheduleSummaryCache struct {
	mu    sync.Mutex
	value WorkflowScheduleSummary
	gen   uint64
	at    time.Time
	ok    bool
}

var workflowScheduleSummaryCache scheduleSummaryCache

// CachedWorkflowScheduleSummary serialises recomputation so concurrent pollers
// share one computation instead of each paying for it.
func (svc *SchedulerService) CachedWorkflowScheduleSummary(ctx context.Context) (WorkflowScheduleSummary, error) {
	c := &workflowScheduleSummaryCache
	c.mu.Lock()
	defer c.mu.Unlock()
	gen := scheduleSummaryGeneration.Load()
	if c.ok && c.gen == gen && time.Since(c.at) < scheduleSummarySafetyTTL {
		return c.value, nil
	}
	summary, err := svc.SummarizeWorkflowSchedules(ctx)
	if err != nil {
		return WorkflowScheduleSummary{}, err
	}
	c.value, c.gen, c.at, c.ok = summary, gen, time.Now(), true
	return summary, nil
}
