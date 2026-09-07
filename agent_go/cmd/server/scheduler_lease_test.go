package server

import (
	"context"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/schedulerstate"
	"path/filepath"
	"testing"
	"time"
)

func TestScheduleLeaseTimerRenewsWithoutAgentCalls(t *testing.T) {
	if testing.Short() {
		t.Skip("checks the real 20-second timer")
	}
	store, err := schedulerstate.Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	start := time.Now().UTC()
	if err := store.BeginRun(ctx, schedulerstate.Run{RunID: "timer", ScopeType: "workflow", ScopeID: "demo", LockKey: "demo", ScheduleID: "daily", StartedAt: start}); err != nil {
		t.Fatal(err)
	}
	s := &SchedulerService{stateStore: store}
	stop := s.maintainRunLease(ctx, "timer")
	defer stop()
	deadline := time.NewTimer(25 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(100 * time.Millisecond)
	defer poll.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatal("server did not renew lease")
		case <-poll.C:
			r, err := store.GetRun(ctx, "timer")
			if err != nil {
				t.Fatal(err)
			}
			if r.UpdatedAt.After(start.Add(15 * time.Second)) {
				return
			}
		}
	}
}
