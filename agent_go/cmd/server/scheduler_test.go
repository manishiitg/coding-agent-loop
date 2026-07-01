package server

import (
	"context"
	"strings"
	"testing"
)

func TestBuildScheduleCronExpressionAlwaysSetsTimezone(t *testing.T) {
	tests := []struct {
		name     string
		cronExpr string
		timezone string
		want     string
	}{
		{
			name:     "utc timezone is explicit",
			cronExpr: "0 9 * * *",
			timezone: "UTC",
			want:     "CRON_TZ=UTC 0 9 * * *",
		},
		{
			name:     "empty timezone defaults to UTC",
			cronExpr: "0 9 * * *",
			timezone: "",
			want:     "CRON_TZ=UTC 0 9 * * *",
		},
		{
			name:     "named timezone is preserved",
			cronExpr: "0 18 * * *",
			timezone: "America/New_York",
			want:     "CRON_TZ=America/New_York 0 18 * * *",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildScheduleCronExpression(tt.cronExpr, tt.timezone); got != tt.want {
				t.Fatalf("buildScheduleCronExpression() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateScheduleTimezone(t *testing.T) {
	valid := []string{"UTC", "Asia/Kolkata", "America/New_York"}
	for _, timezone := range valid {
		t.Run("valid "+timezone, func(t *testing.T) {
			if err := ValidateScheduleTimezone(timezone); err != nil {
				t.Fatalf("ValidateScheduleTimezone(%q) returned error: %v", timezone, err)
			}
		})
	}

	invalid := []string{"", "IST", "EST", "Not/AZone"}
	for _, timezone := range invalid {
		t.Run("invalid "+timezone, func(t *testing.T) {
			if err := ValidateScheduleTimezone(timezone); err == nil {
				t.Fatalf("ValidateScheduleTimezone(%q) returned nil error", timezone)
			}
		})
	}
}

func TestBuildWorkshopRequestUsesScheduleWorkshopMode(t *testing.T) {
	tests := []struct {
		name         string
		workshopMode string
		want         string
	}{
		{
			name:         "run schedule stays in run mode",
			workshopMode: "run",
			want:         "run",
		},
		{
			name:         "optimizer schedule maps to merged workshop mode",
			workshopMode: "optimizer",
			want:         "workshop",
		},
		{
			name:         "blank legacy schedule preserves old workshop fallback",
			workshopMode: "",
			want:         "workshop",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &SchedulerService{}
			req := svc.buildWorkshopRequest(context.Background(), &ScheduleContext{
				WorkspacePath: "Workflow/test",
				WorkflowID:    "wf-test",
				Schedule: WorkflowSchedule{
					GroupNames:   []string{"group-1"},
					WorkshopMode: tt.workshopMode,
				},
			})
			execOpts, ok := req["execution_options"].(map[string]interface{})
			if !ok {
				t.Fatalf("execution_options = %#v, want map", req["execution_options"])
			}
			if got := execOpts["workshop_mode"]; got != tt.want {
				t.Fatalf("workshop_mode = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWorkflowScheduleRuntimeMessagesInjectsUpgradeForLegacyWorkshopPrompt(t *testing.T) {
	got := workflowScheduleRuntimeMessages(&ScheduleContext{
		WorkspacePath: "Workflow/test",
		Schedule: WorkflowSchedule{
			ID:             "sched-1",
			Name:           "Legacy harden",
			Mode:           "workshop",
			WorkshopMode:   "optimizer",
			CronExpression: "0 * * * *",
			Timezone:       "UTC",
			GroupNames:     []string{"group-1"},
			Messages:       []string{"Use the old harden prompt."},
		},
	})

	if len(got) != 1 {
		t.Fatalf("runtime messages length = %d, want 1", len(got))
	}
	msg := got[0]
	for _, want := range []string{
		"SCHEDULE PROMPT UPGRADE REQUIRED",
		"get_reference_doc(kind=\"workflow-tools\")",
		"get_workflow_command_guidance(kind=\"auto-improve\")",
		"update_schedule(job_id=\"sched-1\"",
		"Use the old harden prompt.",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("upgrade message missing %q:\n%s", want, msg)
		}
	}
}

func TestWorkflowScheduleRuntimeMessagesLeavesCurrentPromptUnchanged(t *testing.T) {
	want := []string{"Run the current prompt."}
	got := workflowScheduleRuntimeMessages(&ScheduleContext{
		Schedule: WorkflowSchedule{
			Mode:          "workshop",
			PromptVersion: CurrentWorkflowSchedulePromptVersion,
			Messages:      want,
		},
	})

	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("runtime messages = %#v, want %#v", got, want)
	}
}

func TestApplyNewWorkflowScheduleDefaultsCreatesWorkshopRunPulse(t *testing.T) {
	sched := WorkflowSchedule{
		GroupNames: []string{"group-1", "group-2"},
	}

	applyNewWorkflowScheduleDefaults(&sched)
	markWorkflowSchedulePromptCurrent(&sched)

	if sched.Mode != "workshop" {
		t.Fatalf("mode = %q, want workshop", sched.Mode)
	}
	if sched.WorkshopMode != "run" {
		t.Fatalf("workshop mode = %q, want run", sched.WorkshopMode)
	}
	if sched.PromptVersion != CurrentWorkflowSchedulePromptVersion {
		t.Fatalf("prompt version = %d, want %d", sched.PromptVersion, CurrentWorkflowSchedulePromptVersion)
	}
	if len(sched.Messages) != 1 {
		t.Fatalf("messages length = %d, want 1", len(sched.Messages))
	}
	for _, want := range []string{
		`run_full_workflow(group_name="group-1")`,
		`run_full_workflow(group_name="group-2")`,
		"Do not ask for confirmation",
	} {
		if !strings.Contains(sched.Messages[0], want) {
			t.Fatalf("default pulse message missing %q:\n%s", want, sched.Messages[0])
		}
	}
}

func TestScheduleModeOrDefaultPreservesLegacyBlankAsWorkflow(t *testing.T) {
	if got := scheduleModeOrDefault(""); got != "workflow" {
		t.Fatalf("scheduleModeOrDefault(\"\") = %q, want workflow", got)
	}
}

func TestMigrateWorkflowManifestSchedulesConvertsLegacyDirectToPulse(t *testing.T) {
	manifest := &WorkflowManifest{
		Schedules: []WorkflowSchedule{
			{
				ID:         "legacy-direct",
				Name:       "Daily publish",
				GroupNames: []string{"group-1"},
				Mode:       "workflow",
			},
		},
	}

	summary, changed := migrateWorkflowManifestSchedulesToCurrent(manifest)
	if !changed {
		t.Fatal("changed = false, want true")
	}
	if summary.ConvertedDirectToPulse != 1 {
		t.Fatalf("converted = %d, want 1", summary.ConvertedDirectToPulse)
	}
	sched := manifest.Schedules[0]
	if sched.Mode != "workshop" || sched.WorkshopMode != "run" {
		t.Fatalf("mode/workshop_mode = %q/%q, want workshop/run", sched.Mode, sched.WorkshopMode)
	}
	if sched.ScheduleVersion != CurrentWorkflowScheduleVersion {
		t.Fatalf("schedule version = %d, want %d", sched.ScheduleVersion, CurrentWorkflowScheduleVersion)
	}
	if sched.PromptVersion != CurrentWorkflowSchedulePromptVersion {
		t.Fatalf("prompt version = %d, want %d", sched.PromptVersion, CurrentWorkflowSchedulePromptVersion)
	}
	if len(sched.Messages) != 1 || !strings.Contains(sched.Messages[0], `run_full_workflow(group_name="group-1")`) {
		t.Fatalf("messages = %#v, want default group run message", sched.Messages)
	}
}

func TestMigrateWorkflowManifestSchedulesAddsLegacyUIPulseMigrationForUnversionedWorkflow(t *testing.T) {
	manifest := &WorkflowManifest{
		schemaVersionMissing: true,
		Schedules: []WorkflowSchedule{
			{
				ID:         "legacy-direct",
				Name:       "Daily publish",
				GroupNames: []string{"group-1"},
				Mode:       "workflow",
			},
		},
	}

	summary, changed := migrateWorkflowManifestSchedulesToCurrent(manifest)
	if !changed {
		t.Fatal("changed = false, want true")
	}
	if summary.LegacyPulseUIMigrations != 1 {
		t.Fatalf("legacy pulse UI migrations = %d, want 1", summary.LegacyPulseUIMigrations)
	}
	sched := manifest.Schedules[0]
	if len(sched.Messages) != 2 {
		t.Fatalf("messages length = %d, want 2: %#v", len(sched.Messages), sched.Messages)
	}
	for _, want := range []string{
		"LEGACY PULSE UI MIGRATION",
		"get_reference_doc(kind=\"html-output\")",
		"builder/review.html",
		"report-only compatibility section",
		"Do not run the workflow in this migration turn",
		"update_schedule(job_id=\"legacy-direct\"",
		"removed from future fires",
	} {
		if !strings.Contains(sched.Messages[0], want) {
			t.Fatalf("legacy UI migration message missing %q:\n%s", want, sched.Messages[0])
		}
	}
	if !strings.Contains(sched.Messages[1], `run_full_workflow(group_name="group-1")`) {
		t.Fatalf("second message = %q, want default group run message", sched.Messages[1])
	}
	if sched.PromptVersion != CurrentWorkflowSchedulePromptVersion {
		t.Fatalf("prompt version = %d, want %d", sched.PromptVersion, CurrentWorkflowSchedulePromptVersion)
	}
}

func TestMigrateWorkflowManifestSchedulesLeavesVersionedDirectScheduleAlone(t *testing.T) {
	manifest := &WorkflowManifest{
		Schedules: []WorkflowSchedule{
			{
				ID:              "new-explicit-direct",
				Name:            "Legacy direct requested",
				Mode:            "workflow",
				ScheduleVersion: CurrentWorkflowScheduleVersion,
			},
		},
	}

	summary, changed := migrateWorkflowManifestSchedulesToCurrent(manifest)
	if changed {
		t.Fatalf("changed = true, want false")
	}
	if summary.ConvertedDirectToPulse != 0 {
		t.Fatalf("converted = %d, want 0", summary.ConvertedDirectToPulse)
	}
	sched := manifest.Schedules[0]
	if sched.Mode != "workflow" {
		t.Fatalf("mode = %q, want workflow", sched.Mode)
	}
}

func TestMigrateWorkflowManifestSchedulesPreservesStaleWorkshopPrompt(t *testing.T) {
	manifest := &WorkflowManifest{
		Schedules: []WorkflowSchedule{
			{
				ID:           "legacy-auto-improve",
				Name:         "Auto improve",
				Mode:         "workshop",
				WorkshopMode: "optimizer",
				Messages:     []string{"old optimizer message"},
			},
		},
	}

	summary, changed := migrateWorkflowManifestSchedulesToCurrent(manifest)
	if !changed {
		t.Fatal("changed = false, want true")
	}
	if summary.ConvertedDirectToPulse != 0 {
		t.Fatalf("converted = %d, want 0", summary.ConvertedDirectToPulse)
	}
	if summary.StaleWorkshopPrompts != 1 {
		t.Fatalf("stale workshop prompts = %d, want 1", summary.StaleWorkshopPrompts)
	}
	sched := manifest.Schedules[0]
	if sched.Mode != "workshop" || sched.WorkshopMode != "optimizer" {
		t.Fatalf("mode/workshop_mode = %q/%q, want workshop/optimizer", sched.Mode, sched.WorkshopMode)
	}
	if sched.ScheduleVersion != CurrentWorkflowScheduleVersion {
		t.Fatalf("schedule version = %d, want %d", sched.ScheduleVersion, CurrentWorkflowScheduleVersion)
	}
	if sched.PromptVersion != 0 {
		t.Fatalf("prompt version = %d, want 0 so prompt upgrade still runs", sched.PromptVersion)
	}
}

func TestMultiAgentScheduleRuntimeQueryInjectsUpgradeForLegacyChiefOfStaffPrompt(t *testing.T) {
	got := multiAgentScheduleRuntimeQuery(&ScheduleContext{
		UserID: "default",
		Schedule: WorkflowSchedule{
			ID:             "chief-1",
			Name:           "Daily briefing",
			Mode:           "multi-agent",
			CronExpression: "0 9 * * *",
			Timezone:       "UTC",
			Query:          "Prepare the old daily briefing.",
		},
	})

	for _, want := range []string{
		"CHIEF OF STAFF SCHEDULE PROMPT UPGRADE REQUIRED",
		"get_reference_doc(kind=\"schedule-management\")",
		"_users/default/multiagent-schedules.json",
		"schedule_version=1",
		"prompt_version=1",
		"Prepare the old daily briefing.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("upgrade query missing %q:\n%s", want, got)
		}
	}
}

func TestMultiAgentScheduleRuntimeQueryLeavesCurrentPromptUnchanged(t *testing.T) {
	want := "Prepare the current briefing."
	got := multiAgentScheduleRuntimeQuery(&ScheduleContext{
		Schedule: WorkflowSchedule{
			Mode:          "multi-agent",
			Query:         want,
			PromptVersion: CurrentMultiAgentSchedulePromptVersion,
		},
	})

	if got != want {
		t.Fatalf("runtime query = %q, want %q", got, want)
	}
}

func TestMigrateMultiAgentScheduleFileStampsLegacyChiefOfStaffSchedule(t *testing.T) {
	file := &MultiAgentScheduleFile{
		schemaVersionMissing: true,
		Schedules: []WorkflowSchedule{
			{
				ID:             "chief-legacy",
				Name:           "Daily briefing",
				CronExpression: "0 9 * * *",
				Timezone:       "UTC",
				Query:          "Prepare the old daily briefing.",
			},
		},
	}

	summary, changed := migrateMultiAgentScheduleFileToCurrent(file)
	if !changed {
		t.Fatal("changed = false, want true")
	}
	if summary.LegacyFileRoots != 1 {
		t.Fatalf("legacy file roots = %d, want 1", summary.LegacyFileRoots)
	}
	if summary.StampedCurrent != 1 {
		t.Fatalf("stamped schedules = %d, want 1", summary.StampedCurrent)
	}
	if summary.StalePrompts != 1 {
		t.Fatalf("stale prompts = %d, want 1", summary.StalePrompts)
	}
	if file.SchemaVersion != CurrentMultiAgentScheduleFileVersion {
		t.Fatalf("schema version = %d, want %d", file.SchemaVersion, CurrentMultiAgentScheduleFileVersion)
	}
	sched := file.Schedules[0]
	if sched.Mode != "multi-agent" {
		t.Fatalf("mode = %q, want multi-agent", sched.Mode)
	}
	if sched.ScheduleVersion != CurrentMultiAgentScheduleVersion {
		t.Fatalf("schedule version = %d, want %d", sched.ScheduleVersion, CurrentMultiAgentScheduleVersion)
	}
	if sched.PromptVersion != 0 {
		t.Fatalf("prompt version = %d, want 0 so prompt upgrade still runs", sched.PromptVersion)
	}
}

func TestBuildMultiAgentJobResponseIncludesPromptStatus(t *testing.T) {
	resp := buildMultiAgentJobResponse("default", WorkflowSchedule{
		ID:              "chief-stale",
		Name:            "Daily briefing",
		Mode:            "multi-agent",
		Query:           "Prepare the old daily briefing.",
		ScheduleVersion: CurrentMultiAgentScheduleVersion,
	}, ScheduleRuntimeState{})

	if resp.ScheduleVersion != CurrentMultiAgentScheduleVersion {
		t.Fatalf("schedule version = %d, want %d", resp.ScheduleVersion, CurrentMultiAgentScheduleVersion)
	}
	if resp.CurrentPromptVersion != CurrentMultiAgentSchedulePromptVersion {
		t.Fatalf("current prompt version = %d, want %d", resp.CurrentPromptVersion, CurrentMultiAgentSchedulePromptVersion)
	}
	if !resp.PromptStale {
		t.Fatal("prompt stale = false, want true")
	}
}
