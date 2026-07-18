package server

import (
	"strings"
	"testing"
)

func TestBuiltinOrgPulseSequenceIncludesReadOnlyLLMCostAudit(t *testing.T) {
	sched, ok := FindDefaultBuiltinSchedule(builtinOrgPulseID)
	if !ok {
		t.Fatal("builtin org pulse schedule not found")
	}
	if got := len(sched.Messages); got != 6 {
		t.Fatalf("builtin org pulse messages = %d, want 6", got)
	}

	audit := sched.Messages[2]
	if !strings.Contains(audit, "LLM + COST AUDIT") {
		t.Fatalf("third org pulse message should be LLM/cost audit, got: %s", audit)
	}
	for _, want := range []string{
		"complete high/medium/low LLM tier setup",
		"complete, missing-tier, override-mismatch, over-tiered, under-tiered, or unknown",
		"high/medium/low",
		"where tier evidence is missing",
		"cost evidence is missing",
		"do NOT change workflow.json",
		"do NOT run optimizers or fixes",
	} {
		if !strings.Contains(audit, want) {
			t.Fatalf("LLM/cost audit message missing %q:\n%s", want, audit)
		}
	}

	final := sched.Messages[len(sched.Messages)-1]
	if !strings.Contains(final, "LLM/model tier + cost audit") || !strings.Contains(final, "task findings/promotions") || !strings.Contains(final, "Report the log entry, LLM/cost summary") {
		t.Fatalf("final org pulse message should require LLM/cost reporting in pulse and final report:\n%s", final)
	}
	for _, want := range []string{
		"DAILY DIGEST",
		"one daily Org Pulse digest",
		"steady healthy day still gets a calm all-healthy digest",
		"email is the default in-depth rendering",
		"notification result",
	} {
		if !strings.Contains(final, want) {
			t.Fatalf("final org pulse message missing daily digest requirement %q:\n%s", want, final)
		}
	}
	for _, want := range []string{
		"daily Org Pulse digest",
		"Send a calm all-healthy digest on steady days",
		"set email_cc",
	} {
		if !strings.Contains(builtinOrgPulseQuery, want) {
			t.Fatalf("single-turn Org Pulse fallback missing daily digest requirement %q:\n%s", want, builtinOrgPulseQuery)
		}
	}
	for _, forbidden := range []string{
		"If nothing has changed, write nothing and stop",
		"STOP the whole pass",
		"Org idle since last pulse",
		"harvest what's worth keeping into your memory",
		"Harvest what's worth keeping into your shared memory",
	} {
		if strings.Contains(builtinOrgPulseQuery, forbidden) {
			t.Fatalf("single-turn Org Pulse fallback should not include skip/no-op gate %q:\n%s", forbidden, builtinOrgPulseQuery)
		}
		for _, msg := range sched.Messages {
			if strings.Contains(msg, forbidden) {
				t.Fatalf("Org Pulse message should not include skip/no-op gate %q:\n%s", forbidden, msg)
			}
		}
	}
}

func TestBuiltinOrgPulseRecommendationLifecycleHandoff(t *testing.T) {
	sched, ok := FindDefaultBuiltinSchedule(builtinOrgPulseID)
	if !ok {
		t.Fatal("builtin org pulse schedule not found")
	}
	if len(sched.Messages) < 4 {
		t.Fatalf("builtin org pulse messages = %d, want at least 4", len(sched.Messages))
	}

	recStep := sched.Messages[3]
	for _, want := range []string{
		"First read existing org-level recommendation cards",
		"workflow-level Chief of Staff cards",
		"data-cos-rec-id",
		"queued_goal_advisor",
		"update/follow up instead of duplicating",
		`data-status="proposed"`,
		"stale open decisions",
	} {
		if !strings.Contains(recStep, want) {
			t.Fatalf("Org Pulse recommendation step missing %q:\n%s", want, recStep)
		}
	}
	if !strings.Contains(builtinOrgPulseQuery, "follow up on existing recommendations before creating new ones") {
		t.Fatalf("single-turn Org Pulse fallback missing recommendation lifecycle follow-up:\n%s", builtinOrgPulseQuery)
	}
}

func TestBuiltinOrgPulseUpdatesGoalsScorecard(t *testing.T) {
	sched, ok := FindDefaultBuiltinSchedule(builtinOrgPulseID)
	if !ok {
		t.Fatal("builtin org pulse schedule not found")
	}
	if len(sched.Messages) < 2 {
		t.Fatalf("builtin org pulse messages = %d, want at least 2", len(sched.Messages))
	}

	evidenceAndGoals := sched.Messages[1]
	for _, want := range []string{
		`get_reference_doc(kind="org-html")`,
		"pulse/task.html",
		"task findings used",
		"update pulse/goals.html as the durable current scorecard",
		"status, latest evidence, confidence, freshness/last-reviewed, or history",
		"whether pulse/goals.html was updated",
	} {
		if !strings.Contains(evidenceAndGoals, want) {
			t.Fatalf("evidence/goals message missing %q:\n%s", want, evidenceAndGoals)
		}
	}

	if !strings.Contains(builtinOrgPulseQuery, "update pulse/goals.html as the durable current scorecard") {
		t.Fatalf("single-turn Org Pulse fallback should update goals.html scorecard:\n%s", builtinOrgPulseQuery)
	}
}

func TestMergeBuiltinSchedulesRefreshesOrgPulseOverrideMessages(t *testing.T) {
	resume := true
	stale := WorkflowSchedule{
		ID:             builtinOrgPulseID,
		Name:           "Custom Org Pulse",
		Description:    "User cadence override for org pulse",
		ScheduleType:   "calendar",
		CronExpression: "15 9 * * *",
		Timezone:       "Asia/Kolkata",
		Enabled:        true,
		Mode:           "multi-agent",
		Query:          "old org-pulse query",
		Messages:       []string{"old step"},
		ResumePrevious: &resume,
		CalendarItems: []CalendarScheduleItem{
			{ID: "one", Date: "2026-07-01", Time: "09:00", Messages: []string{"old item step"}},
		},
	}

	merged := MergeBuiltinSchedules([]WorkflowSchedule{stale})
	var got *WorkflowSchedule
	for i := range merged {
		if merged[i].ID == builtinOrgPulseID {
			got = &merged[i]
			break
		}
	}
	if got == nil {
		t.Fatal("merged schedules missing org pulse override")
	}

	if got.Name != stale.Name || got.Description != stale.Description {
		t.Fatalf("org pulse user-visible fields not preserved: %#v", got)
	}
	if got.ScheduleType != stale.ScheduleType || got.CronExpression != stale.CronExpression || got.Timezone != stale.Timezone || !got.Enabled || !got.ShouldResumePrevious() {
		t.Fatalf("org pulse scheduling knobs not preserved: %#v", got)
	}
	if got.Query == stale.Query || len(got.Messages) != 6 {
		t.Fatalf("org pulse content was not refreshed: query=%q messages=%d", got.Query, len(got.Messages))
	}
	if !strings.Contains(got.Messages[2], "LLM + COST AUDIT") {
		t.Fatalf("org pulse override did not receive LLM/cost audit step: %v", got.Messages)
	}
	if len(got.CalendarItems) != 1 || len(got.CalendarItems[0].Messages) != 0 {
		t.Fatalf("calendar item messages should not shadow product-managed org pulse steps: %#v", got.CalendarItems)
	}
}

func TestMergeBuiltinSchedulesRefreshesDuplicateOrgPulseMessages(t *testing.T) {
	duplicate := WorkflowSchedule{
		ID:             "user-created-org-pulse",
		Name:           "Org Pulse",
		Description:    "Daily org-pulse duplicate",
		CronExpression: "30 7 * * *",
		Timezone:       "UTC",
		Enabled:        true,
		Mode:           "multi-agent",
		Query:          "legacy org pulse",
		Messages:       []string{"old step"},
	}

	merged := MergeBuiltinSchedules([]WorkflowSchedule{duplicate})
	var got *WorkflowSchedule
	for i := range merged {
		if merged[i].ID == duplicate.ID {
			got = &merged[i]
			break
		}
	}
	if got == nil {
		t.Fatal("merged schedules missing duplicate org pulse")
	}
	if got.ID != duplicate.ID || got.CronExpression != duplicate.CronExpression || !got.Enabled {
		t.Fatalf("duplicate org pulse identity/schedule not preserved: %#v", got)
	}
	if len(got.Messages) != 6 || !strings.Contains(got.Messages[2], "LLM + COST AUDIT") {
		t.Fatalf("duplicate org pulse did not receive current builtin sequence: %#v", got.Messages)
	}
}

func TestIsOrgPulseScheduleDoesNotMatchIncidentalText(t *testing.T) {
	schedule := WorkflowSchedule{
		ID:          "publish-org-report",
		Name:        "Publish reporting dashboard",
		Description: "Post the org-pulse.html summary after publishing.",
		Query:       "Read the Org Pulse output and publish it.",
		Mode:        "multi-agent",
	}
	if IsOrgPulseSchedule(schedule) {
		t.Fatal("schedule with incidental Org Pulse text was classified as the managed Org Pulse")
	}
}
