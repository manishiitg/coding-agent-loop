package server

import (
	"context"
	"testing"
	"time"
)

func TestScheduleExpectsWorkflowRun(t *testing.T) {
	plain := &ScheduleContext{Schedule: WorkflowSchedule{ID: "daily", CronExpression: "0 9 * * *"}}
	if !scheduleExpectsWorkflowRun(plain) {
		t.Fatal("a plain workflow schedule exists to run the workflow")
	}
	calendar := &ScheduleContext{Schedule: WorkflowSchedule{ID: "cal", ScheduleType: "calendar", CalendarItems: []CalendarScheduleItem{{Date: "2026-10-01", Time: "09:00"}}}}
	if !scheduleExpectsWorkflowRun(calendar) {
		t.Fatal("a calendar schedule also exists to run the workflow")
	}
	for name, sctx := range map[string]*ScheduleContext{
		"direct messages": {Schedule: WorkflowSchedule{ID: "m", Messages: []string{"Run only step-send-email-outreach"}}},
		"query":           {Schedule: WorkflowSchedule{ID: "q", Query: "summarize"}},
		"execution mode":  {Schedule: WorkflowSchedule{ID: "c", ExecutionMode: "close_only"}},
		"pulse only":      {Schedule: WorkflowSchedule{ID: "p"}, PulseOnly: true},
	} {
		if scheduleExpectsWorkflowRun(sctx) {
			t.Errorf("%s schedule may legitimately run no workflow", name)
		}
	}
}

func TestScheduleRunHealthShowsSchedulesThatRanNothing(t *testing.T) {
	_, _ = newScheduleRunWorkspaceStub(t)
	ctx := context.Background()
	ws := "Workflow/test"
	base := time.Date(2026, 9, 20, 7, 30, 0, 0, time.UTC)
	add := func(id, schedule string, at time.Time, ran *bool) {
		t.Helper()
		if err := AppendScheduleRun(ctx, ws, &ScheduleRunEntry{ID: id, ScheduleID: schedule, Status: "success", StartedAt: at}); err != nil {
			t.Fatal(err)
		}
		if ran != nil {
			if err := SetScheduleRunRanWorkflow(ctx, ws, id, *ran); err != nil {
				t.Fatal(err)
			}
		}
	}
	yes, no := true, false
	add("r1", "email", base, &yes)
	add("r2", "email", base.Add(24*time.Hour), &no)
	add("r3", "email", base.Add(48*time.Hour), &no)
	add("r4", "email", base.Add(72*time.Hour), nil)
	add("r5", "digest", base, &no)

	manifest := &WorkflowManifest{Schedules: []WorkflowSchedule{
		{ID: "email", Name: "Email outreach send", Enabled: true, Messages: []string{"Run only step-send-email-outreach"}},
		{ID: "digest", Name: "Daily digest", Enabled: true, Messages: []string{"Summarize"}},
		{ID: "off", Name: "Disabled", Enabled: false},
	}}
	health := collectScheduleRunHealth(ctx, ws, manifest)
	if len(health) != 2 {
		t.Fatalf("health = %+v, want the two enabled schedules", health)
	}
	email := health[0]
	if email.ScheduleID != "email" || email.RecentRuns != 4 || email.RanWorkflow != 1 || email.RanNothing != 2 || email.Unknown != 1 || !email.CustomMessages {
		t.Fatalf("email health = %+v", email)
	}
	if email.LastRanWorkflowAt != base.Format(time.RFC3339) || email.LastStartedAt != base.Add(72*time.Hour).Format(time.RFC3339) {
		t.Fatalf("email last-ran / last-started = %+v", email)
	}
}
