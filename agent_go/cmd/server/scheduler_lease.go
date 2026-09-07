package server

import (
	"context"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/schedulerstate"
	"time"
)

// The timer belongs to runJob, not an LLM turn. Canceling it does not release
// ownership; the existing terminal transition does that after execution exits.
func (s *SchedulerService) maintainRunLease(ctx context.Context, runID string) func() {
	leaseCtx, stop := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		lastRenewed := time.Now()
		for {
			select {
			case <-leaseCtx.Done():
				return
			case <-ticker.C:
				callCtx, cancel := context.WithTimeout(leaseCtx, 5*time.Second)
				s.stateStoreMu.RLock()
				var err error
				if s.stateStore != nil {
					err = s.stateStore.RenewLease(callCtx, runID, time.Now().UTC())
				}
				s.stateStoreMu.RUnlock()
				cancel()
				if err == nil {
					lastRenewed = time.Now()
					continue
				}
				scheduleLogf("[SCHEDULER_LEASE] renewal failed run=%s: %v", runID, err)
				if time.Since(lastRenewed) >= schedulerstate.LeaseDuration {
					s.cancelScheduleRunContext(runID)
					return
				}
			}
		}
	}()
	return stop
}
