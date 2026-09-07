package schedulerstate

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestLeaseRenewalAndRestartOwnership(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	old := time.Now().UTC().Add(-time.Hour)
	r := Run{RunID: "old", ScopeType: "workflow", ScopeID: "Workflow/demo", LockKey: "workflow:demo", ScheduleID: "daily", StartedAt: old}
	if err := s.BeginRun(ctx, r); err != nil {
		t.Fatal(err)
	}
	// Expiry alone cannot admit a second worker.
	next := r
	next.RunID = "new"
	if err := s.BeginRun(ctx, next); !errors.Is(err, ErrRunAlreadyActive) {
		t.Fatalf("expired lock admitted overlap: %v", err)
	}
	now := time.Now().UTC()
	if err := s.RenewLease(ctx, r.RunID, now); err != nil {
		t.Fatal(err)
	}
	got, err := s.ActiveRunForScope(ctx, "workflow", r.ScopeID)
	if err != nil || got == nil || !got.UpdatedAt.Equal(now) {
		t.Fatalf("renewal: %+v %v", got, err)
	}
	if other, err := s.ActiveRunForScope(ctx, "workflow", "Workflow/other"); err != nil || other != nil {
		t.Fatalf("scope leaked: %+v %v", other, err)
	}
	if _, err := s.InterruptActiveRuns(ctx, "server restarted", now); err != nil {
		t.Fatal(err)
	}
	if err := s.BeginRun(ctx, next); err != nil {
		t.Fatal(err)
	}
	if err := s.RenewLease(ctx, r.RunID, now); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("old owner renewed: %v", err)
	}
	got, err = s.ActiveRunForScope(ctx, "workflow", r.ScopeID)
	if err != nil || got == nil || got.RunID != "new" {
		t.Fatalf("new owner: %+v %v", got, err)
	}
}
