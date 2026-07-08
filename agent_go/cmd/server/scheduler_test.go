package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
	"mcp-agent-builder-go/agent_go/internal/terminals"
	"mcp-agent-builder-go/agent_go/pkg/workflowtypes"
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

func TestWorkflowScheduleShouldResumePreviousIsOptIn(t *testing.T) {
	trueValue := true
	falseValue := false

	tests := []struct {
		name           string
		resumePrevious *bool
		want           bool
	}{
		{
			name: "omitted starts fresh",
			want: false,
		},
		{
			name:           "explicit false starts fresh",
			resumePrevious: &falseValue,
			want:           false,
		},
		{
			name:           "explicit true resumes",
			resumePrevious: &trueValue,
			want:           true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sched := WorkflowSchedule{ResumePrevious: tt.resumePrevious}
			if got := sched.ShouldResumePrevious(); got != tt.want {
				t.Fatalf("ShouldResumePrevious() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWorkflowScheduleListExposesWorkshopMode(t *testing.T) {
	workspacePath := "Workflow/social-media"
	manifest := &WorkflowManifest{
		SchemaVersion: WorkflowManifestSchemaVersion,
		ID:            "social-media",
		Label:         "Social Media",
		Schedules: []WorkflowSchedule{
			{
				ID:             "run-schedule",
				Name:           "Daily publish",
				CronExpression: "0 9 * * *",
				Timezone:       "Asia/Kolkata",
				Enabled:        true,
				GroupNames:     []string{"group-1"},
				Mode:           "workshop",
				WorkshopMode:   "run",
			},
			{
				ID:             "optimizer-schedule",
				Name:           "Goal Advisor",
				CronExpression: "0 23 * * 1,4",
				Timezone:       "Asia/Kolkata",
				Enabled:        true,
				GroupNames:     []string{"group-1"},
				Mode:           "workshop",
				WorkshopMode:   "optimizer",
			},
		},
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	workspace := httptest.NewServer(&mockWorkspaceAPI{files: map[string]string{
		workspacePath + "/workflow.json": string(manifestJSON),
	}})
	defer workspace.Close()
	t.Setenv("WORKSPACE_API_URL", workspace.URL)

	callbacks := (&StreamingAPI{}).buildSchedulerCallbacks()
	out, err := callbacks.ListSchedules(context.Background(), workspacePath)
	if err != nil {
		t.Fatalf("ListSchedules() error = %v", err)
	}
	for _, want := range []string{
		"## Schedules (2 found)",
		"### Daily publish",
		"- **Mode**: `workshop`",
		"- **Workshop Mode**: `run`",
		"### Goal Advisor",
		"- **Workshop Mode**: `optimizer`",
		"- **Type**: cron",
		"- **Cron**: `0 23 * * 1,4`",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("schedule list missing %q:\n%s", want, out)
		}
	}
}

func TestShouldUpdateChiefTaskReport(t *testing.T) {
	tests := []struct {
		name string
		sctx *ScheduleContext
		want bool
	}{
		{
			name: "normal chief schedule updates task report",
			sctx: &ScheduleContext{
				SourceType: "multi-agent",
				Schedule: WorkflowSchedule{
					ID:          "weekly-market-review",
					Name:        "Weekly market review",
					Description: "Review three workflows and recommend changes",
					Query:       "Prepare a cross-workflow recommendation report.",
				},
			},
			want: true,
		},
		{
			name: "workflow schedule does not update chief task report",
			sctx: &ScheduleContext{
				SourceType: "workflow",
				Schedule:   WorkflowSchedule{ID: "daily-run", Name: "Daily run"},
			},
			want: false,
		},
		{
			name: "builtin org pulse is excluded",
			sctx: &ScheduleContext{
				SourceType: "multi-agent",
				Schedule:   WorkflowSchedule{ID: builtinOrgPulseID, Name: "Daily Org Pulse"},
			},
			want: false,
		},
		{
			name: "org pulse duplicate is excluded",
			sctx: &ScheduleContext{
				SourceType: "multi-agent",
				Schedule:   WorkflowSchedule{ID: "custom-pulse", Name: "Daily Org Pulse scan"},
			},
			want: false,
		},
		{
			name: "deprecated builtin memory schedule is excluded",
			sctx: &ScheduleContext{
				SourceType: "multi-agent",
				Schedule:   WorkflowSchedule{ID: deprecatedAutoEnrichMemoryID, Name: "Auto-enrich memory"},
			},
			want: false,
		},
		{
			name: "memory-like schedule is excluded",
			sctx: &ScheduleContext{
				SourceType: "multi-agent",
				Schedule: WorkflowSchedule{
					ID:    "custom-memory",
					Name:  "Memory enrichment",
					Query: "Run enrich_memory for recent conversations.",
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldUpdateChiefTaskReport(tt.sctx); got != tt.want {
				t.Fatalf("shouldUpdateChiefTaskReport() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildChiefTaskReportUpdateMessageUsesSingleSharedTaskHTML(t *testing.T) {
	startedAt := time.Date(2026, 7, 4, 10, 15, 0, 0, time.UTC)
	completedAt := startedAt.Add(2 * time.Minute)
	sctx := &ScheduleContext{
		SourceType: "multi-agent",
		Schedule: WorkflowSchedule{
			ID:             "weekly-market-review",
			Name:           "Weekly market review",
			Description:    "Review three workflows and recommend changes",
			Query:          "Prepare a cross-workflow recommendation report.",
			CronExpression: "0 9 * * 1",
			Timezone:       "Asia/Kolkata",
		},
	}

	msg := buildChiefTaskReportUpdateMessage(sctx, "run-123", "success", "", 120000, startedAt, completedAt, "session-abc")
	for _, want := range []string{
		`get_reference_doc(kind="chief-task-report")`,
		"Update the single shared Tasks page at pulse/task.html",
		"Do not create per-task files",
		"Do not edit pulse/org-pulse.html, pulse/goals.html",
		"schedule_id: weekly-market-review",
		"schedule_name: Weekly market review",
		"run_id: run-123",
		"session_id: session-abc",
		"status: success",
		"Prepare a cross-workflow recommendation report.",
		"Prepend one .task-entry",
		"key findings to reuse",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("task report update message missing %q:\n%s", want, msg)
		}
	}
}

func TestWithChiefTaskRunContextAddsPriorTaskReportInstruction(t *testing.T) {
	sctx := &ScheduleContext{
		SourceType: "multi-agent",
		Schedule: WorkflowSchedule{
			ID:    "weekly-market-review",
			Name:  "Weekly market review",
			Query: "Prepare a cross-workflow recommendation report.",
		},
	}

	msg := withChiefTaskRunContext(sctx, sctx.Schedule.Query)
	for _, want := range []string{
		"NORMAL CHIEF OF STAFF TASK RUN",
		"read pulse/task.html if it exists",
		`data-schedule-id="weekly-market-review"`,
		"key findings",
		"durable context",
		"Do not use or update Chief of Staff memory tools/files",
		"Prepare a cross-workflow recommendation report.",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("task run context missing %q:\n%s", want, msg)
		}
	}
}

func TestWithChiefTaskRunContextSkipsOrgPulse(t *testing.T) {
	sctx := &ScheduleContext{
		SourceType: "multi-agent",
		Schedule:   WorkflowSchedule{ID: builtinOrgPulseID, Name: "Daily Org Pulse"},
	}

	const query = "Run Org Pulse."
	if got := withChiefTaskRunContext(sctx, query); got != query {
		t.Fatalf("withChiefTaskRunContext() = %q, want original query", got)
	}
}

func TestPostRunMonitorUsesSeparateLLMCostTimeReportStep(t *testing.T) {
	steps := postRunMonitorSteps()
	if got := len(steps); got != 12 {
		t.Fatalf("postRunMonitorSteps() length = %d, want 12", got)
	}
	for i, want := range []string{"gate", "harden", "artifact", "report-health", "learning-health", "knowledgebase-health", "db-health", "cost-llm-time", "goal-advisor", "backup", "publish", "notify"} {
		if got := steps[i].label; got != want {
			t.Fatalf("postRunMonitorSteps()[%d].label = %q, want %q", i, got, want)
		}
	}

	var gate string
	var harden string
	var artifact string
	var reportHealth string
	var learningHealth string
	var kbHealth string
	var dbHealth string
	var cost string
	var goalAdvisor string
	var backup string
	var notify string
	for _, step := range steps {
		if step.label == "gate" {
			gate = step.query
		}
		if step.label == "harden" {
			harden = step.query
		}
		if step.label == "artifact" {
			artifact = step.query
		}
		if step.label == "report-health" {
			reportHealth = step.query
		}
		if step.label == "learning-health" {
			learningHealth = step.query
		}
		if step.label == "knowledgebase-health" {
			kbHealth = step.query
		}
		if step.label == "db-health" {
			dbHealth = step.query
		}
		if step.label == "cost-llm-time" {
			cost = step.query
		}
		if step.label == "goal-advisor" {
			goalAdvisor = step.query
		}
		if step.label == "backup" {
			backup = step.query
		}
		if step.label == "notify" {
			notify = step.query
		}
	}
	if gate == "" {
		t.Fatal("gate step not found")
	}
	if harden == "" {
		t.Fatal("harden step not found")
	}
	if artifact == "" {
		t.Fatal("artifact step not found")
	}
	if reportHealth == "" {
		t.Fatal("report-health step not found")
	}
	if learningHealth == "" {
		t.Fatal("learning-health step not found")
	}
	if kbHealth == "" {
		t.Fatal("knowledgebase-health step not found")
	}
	if dbHealth == "" {
		t.Fatal("db-health step not found")
	}
	if cost == "" {
		t.Fatal("cost step not found")
	}
	if goalAdvisor == "" {
		t.Fatal("goal-advisor step not found")
	}
	if backup == "" {
		t.Fatal("backup step not found")
	}
	if notify == "" {
		t.Fatal("notify step not found")
	}
	if strings.Contains(gate, "call harden_workflow(") || strings.Contains(gate, "call improve_learnings(") {
		t.Fatalf("gate step should not run selected modules directly:\n%s", gate)
	}
	for _, want := range []string{
		"PULSE GATE / WORKLIST",
		"get_pulse_module_state",
		"record_pulse_worklist exactly once",
	} {
		if !strings.Contains(gate, want) {
			t.Fatalf("gate step missing %q:\n%s", want, gate)
		}
	}
	for _, want := range []string{
		"harden",
		"artifact_review",
		"report_health",
		"learning_health",
		"knowledgebase_health",
		"db_health",
		"cost_llm_time",
		"goal_advisor",
		"lock/unlock decisions",
		"Goal Advisor does not do routine harden/KB/learnings/db cleanup",
	} {
		if !strings.Contains(gate, want) {
			t.Fatalf("gate step missing module/gating text %q:\n%s", want, gate)
		}
	}
	if !strings.Contains(harden, "PULSE MODULE — HARDEN") {
		t.Fatalf("harden step should be the harden module:\n%s", harden)
	}
	for _, want := range []string{
		`get_reference_doc(kind="optimize-playbook")`,
		"harden_workflow",
		"Decision - Pulse harden",
		"Bug fix",
		"Report fix",
		"Eval fix",
		"mark_pulse_module_result",
	} {
		if !strings.Contains(harden, want) {
			t.Fatalf("harden step missing %q:\n%s", want, harden)
		}
	}
	for _, want := range []string{
		"PULSE MODULE — ARTIFACT REVIEW",
		`get_workflow_command_guidance(kind="review-artifact-drift"`,
		"review_artifact_sync",
		"mark_changelog_artifact_reviewed",
		"Artifact drift",
		"mark_pulse_module_result",
	} {
		if !strings.Contains(artifact, want) {
			t.Fatalf("artifact step missing %q:\n%s", want, artifact)
		}
	}
	for _, want := range []string{
		"PULSE MODULE — REPORT HEALTH",
		`get_workflow_command_guidance(kind="improve-report"`,
		"Report fix",
		"mark_pulse_module_result",
	} {
		if !strings.Contains(reportHealth, want) {
			t.Fatalf("report health step missing %q:\n%s", want, reportHealth)
		}
	}
	for _, want := range []string{
		"PULSE MODULE — LEARNING HEALTH",
		`get_reference_doc(kind="optimize-playbook")`,
		`get_reference_doc(kind="step-config")`,
		"lock_learnings",
		"improve_learnings",
		"mark_pulse_module_result",
	} {
		if !strings.Contains(learningHealth, want) {
			t.Fatalf("learning health step missing %q:\n%s", want, learningHealth)
		}
	}
	for _, want := range []string{
		"PULSE MODULE — KNOWLEDGEBASE HEALTH",
		`get_reference_doc(kind="stores")`,
		"improve_kb",
		"Never rewrite knowledgebase/context",
		"mark_pulse_module_result",
	} {
		if !strings.Contains(kbHealth, want) {
			t.Fatalf("knowledgebase health step missing %q:\n%s", want, kbHealth)
		}
	}
	for _, want := range []string{
		"PULSE MODULE — DB HEALTH",
		`get_reference_doc(kind="stores")`,
		"improve_db",
		"db/README.md",
		"mark_pulse_module_result",
	} {
		if !strings.Contains(dbHealth, want) {
			t.Fatalf("db health step missing %q:\n%s", want, dbHealth)
		}
	}
	for _, want := range []string{
		"PULSE MODULE — COST / LLM / TIME",
		"costs/execution",
		"costs/evaluation",
		"costs/phase/token_usage.json",
		"timing summaries",
		"builder/card.cost.html",
		"do NOT change model tiers",
		"mark_pulse_module_result",
	} {
		if !strings.Contains(cost, want) {
			t.Fatalf("cost step missing %q:\n%s", want, cost)
		}
	}
	for _, want := range []string{
		"PULSE MODULE — GOAL ADVISOR",
		`get_workflow_command_guidance(kind="goal-advisor"`,
		"expert strategy advisor",
		"Do not call harden_workflow, improve_kb, improve_learnings, or improve_db",
		"create_human_input_request",
		"builder/card.progress.html",
		"mark_pulse_module_result",
	} {
		if !strings.Contains(goalAdvisor, want) {
			t.Fatalf("goal advisor step missing %q:\n%s", want, goalAdvisor)
		}
	}
	for _, want := range []string{
		"once every run",
		"Bug/Goal state",
		"builder/card.health.html",
		"final post-Pulse health",
		"create_human_input_request",
		"selected modules ran/skipped",
	} {
		if !strings.Contains(notify, want) {
			t.Fatalf("notify step missing %q:\n%s", want, notify)
		}
	}
	if !strings.Contains(backup, "PULSE FINAL BACKUP") || !strings.Contains(backup, "db/db.sqlite pulse_module_state") {
		t.Fatalf("backup step should snapshot final Pulse state:\n%s", backup)
	}
}

func TestPostRunMonitorPrependsWorkflowVersionUpgradeForOldManifest(t *testing.T) {
	steps := postRunMonitorStepsForManifest(&WorkflowManifest{Version: "1.0.0"})
	if got := len(steps); got != 19 {
		t.Fatalf("postRunMonitorStepsForManifest(old) length = %d, want 19", got)
	}
	if got := steps[0].label; got != "upgrade-1.0.1" {
		t.Fatalf("first step label = %q, want upgrade-1.0.1", got)
	}
	for _, want := range []string{
		"WORKFLOW VERSION UPGRADE v1.0.0 -> v1.0.1",
		`workflow.json "version" to "1.0.1"`,
		`get_reference_doc(kind="review-improve-log")`,
		`get_reference_doc(kind="publish-strategy")`,
		"password-protected static publish contract",
		"named secret only",
		"StatiCrypt",
		"Runloop dark password-gate styling",
	} {
		if !strings.Contains(steps[0].query, want) {
			t.Fatalf("upgrade step missing %q:\n%s", want, steps[0].query)
		}
	}
	if got := steps[1].label; got != "upgrade-1.0.2" {
		t.Fatalf("second step label = %q, want upgrade-1.0.2", got)
	}
	if got := steps[2].label; got != "upgrade-1.0.3" {
		t.Fatalf("third step label = %q, want upgrade-1.0.3", got)
	}
	if got := steps[3].label; got != "upgrade-1.0.4" {
		t.Fatalf("fourth step label = %q, want upgrade-1.0.4", got)
	}
	if got := steps[4].label; got != "upgrade-1.0.5" {
		t.Fatalf("fifth step label = %q, want upgrade-1.0.5", got)
	}
	if got := steps[5].label; got != "upgrade-1.0.6" {
		t.Fatalf("sixth step label = %q, want upgrade-1.0.6", got)
	}
	if got := steps[6].label; got != "upgrade-1.0.7" {
		t.Fatalf("seventh step label = %q, want upgrade-1.0.7", got)
	}
	if got := steps[7].label; got != "gate" {
		t.Fatalf("eighth step label = %q, want gate", got)
	}
}

func TestPostRunMonitorPrependsWorkflowVersionUpgradeForMissingVersion(t *testing.T) {
	steps := postRunMonitorStepsForManifest(&WorkflowManifest{})
	if got := len(steps); got != 19 {
		t.Fatalf("postRunMonitorStepsForManifest(missing version) length = %d, want 19", got)
	}
	if !strings.Contains(steps[0].query, `Current workflow.json version seen by scheduler: "1.0.0"`) {
		t.Fatalf("missing version should be treated as 1.0.0:\n%s", steps[0].query)
	}
}

func TestPostRunMonitorPrependsPublishGateUpgradeForVersion101Manifest(t *testing.T) {
	steps := postRunMonitorStepsForManifest(&WorkflowManifest{Version: "1.0.1"})
	if got := len(steps); got != 18 {
		t.Fatalf("postRunMonitorStepsForManifest(1.0.1) length = %d, want 18", got)
	}
	if got := steps[0].label; got != "upgrade-1.0.2" {
		t.Fatalf("first step label = %q, want upgrade-1.0.2", got)
	}
	for _, want := range []string{
		"WORKFLOW VERSION UPGRADE v1.0.1 -> v1.0.2",
		`workflow.json "version" to "1.0.2"`,
		`get_reference_doc(kind="publish-strategy")`,
		"Runloop dark password-gate contract",
		"default green/white StatiCrypt page",
		"normal verified publish turn will republish with the new gate",
	} {
		if !strings.Contains(steps[0].query, want) {
			t.Fatalf("publish gate upgrade step missing %q:\n%s", want, steps[0].query)
		}
	}
	if got := steps[1].label; got != "upgrade-1.0.3" {
		t.Fatalf("second step label = %q, want upgrade-1.0.3", got)
	}
	if got := steps[2].label; got != "upgrade-1.0.4" {
		t.Fatalf("third step label = %q, want upgrade-1.0.4", got)
	}
	if got := steps[3].label; got != "upgrade-1.0.5" {
		t.Fatalf("fourth step label = %q, want upgrade-1.0.5", got)
	}
	if got := steps[4].label; got != "upgrade-1.0.6" {
		t.Fatalf("fifth step label = %q, want upgrade-1.0.6", got)
	}
	if got := steps[5].label; got != "upgrade-1.0.7" {
		t.Fatalf("sixth step label = %q, want upgrade-1.0.7", got)
	}
	if got := steps[6].label; got != "gate" {
		t.Fatalf("seventh step label = %q, want gate", got)
	}
}

func TestPostRunMonitorPrependsHTMLReportUpgradeForVersion102Manifest(t *testing.T) {
	steps := postRunMonitorStepsForManifest(&WorkflowManifest{Version: "1.0.2"})
	if got := len(steps); got != 17 {
		t.Fatalf("postRunMonitorStepsForManifest(1.0.2) length = %d, want 17", got)
	}
	if got := steps[0].label; got != "upgrade-1.0.3" {
		t.Fatalf("first step label = %q, want upgrade-1.0.3", got)
	}
	for _, want := range []string{
		"WORKFLOW VERSION UPGRADE v1.0.2 -> v1.0.3",
		`reports/report_plan.json`,
		`db/reports/`,
		`window.report.query(sql)`,
		`kind "file"`,
		`renderFormat "html"`,
		"Remove legacy widget kinds",
		`workflow.json "version" to "1.0.3"`,
	} {
		if !strings.Contains(steps[0].query, want) {
			t.Fatalf("html report upgrade step missing %q:\n%s", want, steps[0].query)
		}
	}
	if got := steps[1].label; got != "upgrade-1.0.4" {
		t.Fatalf("second step label = %q, want upgrade-1.0.4", got)
	}
	if got := steps[2].label; got != "upgrade-1.0.5" {
		t.Fatalf("third step label = %q, want upgrade-1.0.5", got)
	}
	if got := steps[3].label; got != "upgrade-1.0.6" {
		t.Fatalf("fourth step label = %q, want upgrade-1.0.6", got)
	}
	if got := steps[4].label; got != "upgrade-1.0.7" {
		t.Fatalf("fifth step label = %q, want upgrade-1.0.7", got)
	}
	if got := steps[5].label; got != "gate" {
		t.Fatalf("sixth step label = %q, want gate", got)
	}
}

func TestPostRunMonitorPrependsPulseReadabilityUpgradeForVersion103Manifest(t *testing.T) {
	steps := postRunMonitorStepsForManifest(&WorkflowManifest{Version: "1.0.3"})
	if got := len(steps); got != 16 {
		t.Fatalf("postRunMonitorStepsForManifest(1.0.3) length = %d, want 16", got)
	}
	if got := steps[0].label; got != "upgrade-1.0.4" {
		t.Fatalf("first step label = %q, want upgrade-1.0.4", got)
	}
	for _, want := range []string{
		"WORKFLOW VERSION UPGRADE v1.0.3 -> v1.0.4",
		`builder/improve.html`,
		`get_reference_doc(kind="review-improve-log")`,
		"What matters now",
		"recent runs: metadata row first",
		"full-width second row",
		`<!-- LOG ENTRIES: newest first -->`,
		`workflow.json "version" to "1.0.4"`,
	} {
		if !strings.Contains(steps[0].query, want) {
			t.Fatalf("pulse readability upgrade step missing %q:\n%s", want, steps[0].query)
		}
	}
	if got := steps[1].label; got != "upgrade-1.0.5" {
		t.Fatalf("second step label = %q, want upgrade-1.0.5", got)
	}
	if got := steps[2].label; got != "upgrade-1.0.6" {
		t.Fatalf("third step label = %q, want upgrade-1.0.6", got)
	}
	if got := steps[3].label; got != "upgrade-1.0.7" {
		t.Fatalf("fourth step label = %q, want upgrade-1.0.7", got)
	}
	if got := steps[4].label; got != "gate" {
		t.Fatalf("fifth step label = %q, want gate", got)
	}
}

func TestPostRunMonitorPrependsPulseFilterUpgradeForVersion104Manifest(t *testing.T) {
	steps := postRunMonitorStepsForManifest(&WorkflowManifest{Version: "1.0.4"})
	if got := len(steps); got != 15 {
		t.Fatalf("postRunMonitorStepsForManifest(1.0.4) length = %d, want 15", got)
	}
	if got := steps[0].label; got != "upgrade-1.0.5" {
		t.Fatalf("first step label = %q, want upgrade-1.0.5", got)
	}
	for _, want := range []string{
		"WORKFLOW VERSION UPGRADE v1.0.4 -> v1.0.5",
		`builder/improve.html`,
		`get_reference_doc(kind="review-improve-log")`,
		"Date, Kind, Search, Reset",
		`data-date="YYYY-MM-DD"`,
		`data-kind="run|monitor|artifact|decision|advisor|cos|open|user|note"`,
		`<!-- LOG ENTRIES: newest first -->`,
		`workflow.json "version" to "1.0.5"`,
	} {
		if !strings.Contains(steps[0].query, want) {
			t.Fatalf("pulse filter upgrade step missing %q:\n%s", want, steps[0].query)
		}
	}
	if got := steps[1].label; got != "upgrade-1.0.6" {
		t.Fatalf("second step label = %q, want upgrade-1.0.6", got)
	}
	if got := steps[2].label; got != "upgrade-1.0.7" {
		t.Fatalf("third step label = %q, want upgrade-1.0.7", got)
	}
	if got := steps[3].label; got != "gate" {
		t.Fatalf("fourth step label = %q, want gate", got)
	}
}

func TestPostRunMonitorPrependsRichPulseWidgetUpgradeForVersion105Manifest(t *testing.T) {
	steps := postRunMonitorStepsForManifest(&WorkflowManifest{Version: "1.0.5"})
	if got := len(steps); got != 14 {
		t.Fatalf("postRunMonitorStepsForManifest(1.0.5) length = %d, want 14", got)
	}
	if got := steps[0].label; got != "upgrade-1.0.6" {
		t.Fatalf("first step label = %q, want upgrade-1.0.6", got)
	}
	for _, want := range []string{
		"WORKFLOW VERSION UPGRADE v1.0.5 -> v1.0.6",
		`builder/improve.html`,
		`get_reference_doc(kind="review-improve-log")`,
		"What matters now widget cards",
		"color-coded signal tiles",
		".tile.ok",
		`<!-- LOG ENTRIES: newest first -->`,
		`workflow.json "version" to "1.0.6"`,
	} {
		if !strings.Contains(steps[0].query, want) {
			t.Fatalf("rich pulse widget upgrade step missing %q:\n%s", want, steps[0].query)
		}
	}
	if got := steps[1].label; got != "upgrade-1.0.7" {
		t.Fatalf("second step label = %q, want upgrade-1.0.7", got)
	}
	if got := steps[2].label; got != "gate" {
		t.Fatalf("third step label = %q, want gate", got)
	}
}

func TestPostRunMonitorPrependsLegacyOptimizerCleanupUpgradeForVersion106Manifest(t *testing.T) {
	steps := postRunMonitorStepsForManifest(&WorkflowManifest{Version: "1.0.6"})
	if got := len(steps); got != 13 {
		t.Fatalf("postRunMonitorStepsForManifest(1.0.6) length = %d, want 13", got)
	}
	if got := steps[0].label; got != "upgrade-1.0.7" {
		t.Fatalf("first step label = %q, want upgrade-1.0.7", got)
	}
	for _, want := range []string{
		"WORKFLOW VERSION UPGRADE v1.0.6 -> v1.0.7",
		"remove old separate Auto Improve / Goal Advisor optimizer schedules",
		`workshop_mode is "optimizer"`,
		"messages is missing/empty",
		"STEP 1/5 PRE-BACKUP",
		"Do not remove a schedule by name alone",
		"Preserve explicit custom optimizer jobs",
		"remove it from workflow.json schedules",
		"schedule-runs.json history",
		"post_run_monitor=true",
		`workflow.json "version" to "1.0.7"`,
		"do not publish",
	} {
		if !strings.Contains(steps[0].query, want) {
			t.Fatalf("legacy optimizer cleanup step missing %q:\n%s", want, steps[0].query)
		}
	}
	if got := steps[1].label; got != "gate" {
		t.Fatalf("second step label = %q, want gate", got)
	}
}

func TestPostRunMonitorDoesNotPrependWorkflowVersionUpgradeForCurrentManifest(t *testing.T) {
	steps := postRunMonitorStepsForManifest(&WorkflowManifest{Version: WorkflowContractCurrentVersion})
	if got := len(steps); got != 12 {
		t.Fatalf("postRunMonitorStepsForManifest(current) length = %d, want 12", got)
	}
	if got := steps[0].label; got != "gate" {
		t.Fatalf("first step label = %q, want gate", got)
	}
	if got := steps[2].label; got != "artifact" {
		t.Fatalf("third step label = %q, want artifact", got)
	}
	for _, want := range []string{
		"ARTIFACT REVIEW",
		`get_workflow_command_guidance(kind="review-artifact-drift"`,
		`review_artifact_sync`,
		`mark_changelog_artifact_reviewed`,
		"not part of harden",
		"Artifact drift",
	} {
		if !strings.Contains(steps[2].query, want) {
			t.Fatalf("artifact step missing %q:\n%s", want, steps[2].query)
		}
	}
}

func TestWorkflowHasPendingPlanChangelogArtifactReview(t *testing.T) {
	tests := []struct {
		name      string
		files     map[string]string
		want      bool
		wantError bool
	}{
		{
			name:  "missing changelog folder",
			files: map[string]string{},
			want:  false,
		},
		{
			name: "unreviewed changelog entry",
			files: map[string]string{
				"Workflow/demo/planning/changelog/changelog-2026-07-02-06-12-46.json": `{"entries":[{"timestamp":"2026-07-02T06:12:46Z","tool":"update_regular_step","reason":"test","step_ids":["step-a"]}]}`,
			},
			want: true,
		},
		{
			name: "reviewed changelog entries",
			files: map[string]string{
				"Workflow/demo/planning/changelog/changelog-2026-07-02-06-12-46.json": `{"entries":[{"timestamp":"2026-07-02T06:12:46Z","tool":"update_regular_step","reason":"test","step_ids":["step-a"],"artifact_review":{"done":true,"reviewed_at":"2026-07-02T06:20:00Z","reviewed_by":"review_artifact_sync","result":"clean"}}]}`,
			},
			want: false,
		},
		{
			name: "one unreviewed entry keeps review pending",
			files: map[string]string{
				"Workflow/demo/planning/changelog/changelog-2026-07-02-06-12-46.json": `{"entries":[{"timestamp":"2026-07-02T06:12:46Z","tool":"update_regular_step","reason":"old","artifact_review":{"done":true}},{"timestamp":"2026-07-02T06:13:46Z","tool":"update_step_config","reason":"new"}]}`,
			},
			want: true,
		},
		{
			name: "malformed changelog is pending",
			files: map[string]string{
				"Workflow/demo/planning/changelog/changelog-2026-07-02-06-12-46.json": `{`,
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspace := httptest.NewServer(&mockWorkspaceAPI{files: tt.files})
			defer workspace.Close()
			t.Setenv("WORKSPACE_API_URL", workspace.URL)

			got, err := workflowHasPendingPlanChangelogArtifactReview(context.Background(), "Workflow/demo")
			if tt.wantError && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantError && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("workflowHasPendingPlanChangelogArtifactReview() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSelectedPostRunMonitorModuleStepsUsesGateWorklist(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	workspacePath := "Workflow/demo"
	pulseRunID := "pulse-run-1"

	if _, err := recordPulseWorklist(ctx, workspacePath, pulseRunID, []PulseWorklistDecision{
		{Module: pulseModuleHarden, Due: true, Reason: "A step failed.", Evidence: []string{"runs/latest"}},
		{Module: pulseModuleArtifactReview, Due: false, Reason: "No unreviewed changelog entries."},
		{Module: pulseModuleReportHealth, Due: false, Reason: "Report is fresh."},
		{Module: pulseModuleLearningHealth, Due: false, Reason: "No plan changes."},
		{Module: pulseModuleKnowledgebaseHealth, Due: false, Reason: "KB is current."},
		{Module: pulseModuleDBHealth, Due: false, Reason: "DB contracts match."},
		{Module: pulseModuleCostLLMTime, Due: true, Reason: "Cost summary is required every run."},
		{Module: pulseModuleGoalAdvisor, Due: true, Reason: "Goal drift persisted across runs."},
	}); err != nil {
		t.Fatalf("record worklist: %v", err)
	}

	s := NewSchedulerService(nil)
	steps := s.selectedPostRunMonitorModuleSteps(ctx, &ScheduleContext{WorkspacePath: workspacePath}, pulseRunID)
	got := postRunStepLabels(steps)
	want := []string{"pre-backup", "harden", "cost-llm-time", "goal-advisor", "backup", "publish", "notify"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("selected labels = %#v, want %#v", got, want)
	}
}

func TestSelectedPostRunMonitorModuleStepsFallsBackConservatively(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	workspacePath := "Workflow/demo"
	pulseRunID := "pulse-run-missing-worklist"
	workspace := httptest.NewServer(&mockWorkspaceAPI{files: map[string]string{}})
	defer workspace.Close()
	t.Setenv("WORKSPACE_API_URL", workspace.URL)

	s := NewSchedulerService(nil)
	steps := s.selectedPostRunMonitorModuleSteps(ctx, &ScheduleContext{WorkspacePath: workspacePath}, pulseRunID)
	got := postRunStepLabels(steps)
	want := []string{"pre-backup", "harden", "report-health", "cost-llm-time", "backup", "publish", "notify"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("fallback labels = %#v, want %#v", got, want)
	}
}

func postRunStepLabels(steps []postRunMonitorStep) []string {
	labels := make([]string, 0, len(steps))
	for _, step := range steps {
		labels = append(labels, step.label)
	}
	return labels
}

func TestOptimizerScheduleMessagesKeepsCustomMessages(t *testing.T) {
	stored := []string{`Do not ask for confirmation. Run this custom optimizer audit and stop. Compare with any Auto Improve history already logged.`}

	got := optimizerScheduleMessages(context.Background(), "Workflow/test", stored, []string{"prod"})
	if len(got) != 1 {
		t.Fatalf("optimizerScheduleMessages() length = %d, want 1", len(got))
	}
	if got[0] != stored[0] {
		t.Fatalf("optimizerScheduleMessages() = %#v, want stored custom message", got)
	}
}

func TestLegacyGoalAdvisorMessageQueueIgnoresCustomTopicMentions(t *testing.T) {
	stored := []string{`Run a custom optimizer audit for this workflow. Compare the result with Goal Advisor and Auto Improve history, then stop.`}
	if isLegacyGoalAdvisorMessageQueue(stored) {
		t.Fatalf("custom optimizer prompt was incorrectly classified as legacy: %q", stored[0])
	}
}

func TestOptimizerScheduleMessagesNoopsWhenNoStoredMessage(t *testing.T) {
	got := optimizerScheduleMessages(context.Background(), "Workflow/test", nil, []string{"group-a"})
	if len(got) != 1 {
		t.Fatalf("optimizerScheduleMessages(nil) length = %d, want 1", len(got))
	}
	for _, want := range []string{
		"optimizer schedule is no longer the product Goal Advisor loop",
		"Goal Advisor now runs as a Pulse-selected module",
		"legacy optimizer schedule should be disabled",
	} {
		if !strings.Contains(got[0], want) {
			t.Fatalf("optimizer no-op message missing %q:\n%s", want, got[0])
		}
	}
}

func TestExecuteWorkshopJobDisablesLegacyOptimizerBeforeStartingSession(t *testing.T) {
	ctx := context.Background()
	workspacePath := "Workflow/demo"
	manifest := &WorkflowManifest{
		SchemaVersion: WorkflowManifestSchemaVersion,
		ID:            "demo",
		Label:         "Demo",
		Schedules: []WorkflowSchedule{
			{
				ID:             "legacy-optimizer",
				Name:           "Goal Advisor",
				CronExpression: "0 23 * * *",
				Timezone:       "UTC",
				Enabled:        true,
				GroupNames:     []string{"group-1"},
				Mode:           "workshop",
				WorkshopMode:   "optimizer",
				Messages: []string{
					"STEP 1/5 — PRE-BACKUP",
					"STEP 2/5 — IMPROVE",
				},
			},
		},
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	workspace := httptest.NewServer(&mockWorkspaceAPI{files: map[string]string{
		workspacePath + "/workflow.json": string(manifestJSON),
	}})
	defer workspace.Close()
	t.Setenv("WORKSPACE_API_URL", workspace.URL)

	s := NewSchedulerService(nil)
	_, _, err = s.executeWorkshopJob(ctx, &ScheduleContext{
		WorkspacePath: workspacePath,
		Schedule:      manifest.Schedules[0],
		SourceType:    "workflow",
	}, "")
	if err != nil {
		t.Fatalf("executeWorkshopJob() error = %v", err)
	}

	updated, found, err := ReadWorkflowManifest(ctx, workspacePath)
	if err != nil || !found {
		t.Fatalf("read updated manifest: found=%v err=%v", found, err)
	}
	if len(updated.Schedules) != 1 || updated.Schedules[0].Enabled {
		t.Fatalf("legacy optimizer schedule was not disabled: %+v", updated.Schedules)
	}
}

func TestOptimizerScheduleMessagesReplacesLegacyGoalAdvisorQueue(t *testing.T) {
	legacy := []string{
		"STEP 1/5 — PRE-BACKUP",
		"STEP 2/5 — IMPROVE",
		"STEP 3/5 — BACKUP FINAL STATE",
		"STEP 4/5 — PUBLISH",
		"STEP 5/5 — NOTIFY",
	}

	got := optimizerScheduleMessages(context.Background(), "Workflow/test", legacy, nil)
	if len(got) != 1 {
		t.Fatalf("optimizerScheduleMessages() length = %d, want 1", len(got))
	}
	if strings.Contains(strings.Join(got, "\n"), strings.Join(legacy, "\n")) {
		t.Fatalf("optimizerScheduleMessages() should replace legacy stored queues, got:\n%s", strings.Join(got, "\n"))
	}
	for _, want := range []string{
		"optimizer schedule is no longer the product Goal Advisor loop",
		"Pulse-selected module",
	} {
		if !strings.Contains(strings.Join(got, "\n"), want) {
			t.Fatalf("optimizerScheduleMessages() missing %q:\n%s", want, strings.Join(got, "\n"))
		}
	}
}

func TestApplyLLMAndSecretsToReqMapUsesAutoImproveOverrideOnlyForOptimizer(t *testing.T) {
	baseConfig := &workflowtypes.PresetLLMConfig{
		Provider: "claude-code",
		ModelID:  "claude-opus-4-6",
		AutoImproveLLM: &workflowtypes.AgentLLMConfig{
			Provider: "gemini-cli",
			ModelID:  "gemini-2.5-pro",
		},
	}

	tests := []struct {
		name         string
		workshopMode string
		wantProvider string
		wantModelID  string
	}{
		{
			name:         "normal schedule uses workflow model",
			workshopMode: "run",
			wantProvider: "claude-code",
			wantModelID:  "claude-opus-4-6",
		},
		{
			name:         "optimizer schedule uses Goal Advisor override",
			workshopMode: "optimizer",
			wantProvider: "gemini-cli",
			wantModelID:  "gemini-2.5-pro",
		},
		{
			name:         "optimizer mode is case insensitive",
			workshopMode: " OPTIMIZER ",
			wantProvider: "gemini-cli",
			wantModelID:  "gemini-2.5-pro",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqMap := map[string]interface{}{}
			(&SchedulerService{}).applyLLMAndSecretsToReqMap(context.Background(), reqMap, &ScheduleContext{
				Schedule: WorkflowSchedule{WorkshopMode: tt.workshopMode},
				Capabilities: WorkflowCapabilities{
					LLMConfig: baseConfig,
				},
			})

			llmConfig, ok := reqMap["llm_config"].(map[string]interface{})
			if !ok {
				t.Fatalf("llm_config missing or wrong type: %#v", reqMap["llm_config"])
			}
			primary, ok := llmConfig["primary"].(map[string]interface{})
			if !ok {
				t.Fatalf("llm_config.primary missing or wrong type: %#v", llmConfig["primary"])
			}
			if got := primary["provider"]; got != tt.wantProvider {
				t.Fatalf("provider = %#v, want %q", got, tt.wantProvider)
			}
			if got := primary["model_id"]; got != tt.wantModelID {
				t.Fatalf("model_id = %#v, want %q", got, tt.wantModelID)
			}
			if tt.workshopMode == "run" {
				if _, ok := reqMap["llm_config_source"]; ok {
					t.Fatalf("normal run set llm_config_source: %#v", reqMap["llm_config_source"])
				}
			} else if got := reqMap["llm_config_source"]; got != llmConfigSourceScheduledAutoImprove {
				t.Fatalf("llm_config_source = %#v, want %q", got, llmConfigSourceScheduledAutoImprove)
			}
		})
	}
}

func TestApplyLLMAndSecretsToReqMapUsesCodingAgentAutoImproveDefaultForOptimizer(t *testing.T) {
	reqMap := map[string]interface{}{}
	(&SchedulerService{}).applyLLMAndSecretsToReqMap(context.Background(), reqMap, &ScheduleContext{
		Schedule: WorkflowSchedule{WorkshopMode: "optimizer"},
		Capabilities: WorkflowCapabilities{
			LLMConfig: &workflowtypes.PresetLLMConfig{
				Provider:          "claude-code",
				ModelID:           "claude-code",
				LLMAllocationMode: workflowtypes.LLMAllocationModeCodingAgent,
			},
		},
	})

	llmConfig, ok := reqMap["llm_config"].(map[string]interface{})
	if !ok {
		t.Fatalf("llm_config missing or wrong type: %#v", reqMap["llm_config"])
	}
	primary, ok := llmConfig["primary"].(map[string]interface{})
	if !ok {
		t.Fatalf("llm_config.primary missing or wrong type: %#v", llmConfig["primary"])
	}
	if got := primary["provider"]; got != "claude-code" {
		t.Fatalf("provider = %#v, want claude-code", got)
	}
	if got := primary["model_id"]; got != "claude-opus-4-8" {
		t.Fatalf("model_id = %#v, want claude-opus-4-8", got)
	}
	if got := reqMap["llm_config_source"]; got != llmConfigSourceScheduledAutoImprove {
		t.Fatalf("llm_config_source = %#v, want %q", got, llmConfigSourceScheduledAutoImprove)
	}
}

func TestApplyLLMAndSecretsToReqMapPreservesAutoImproveDefaultOptions(t *testing.T) {
	reqMap := map[string]interface{}{}
	(&SchedulerService{}).applyLLMAndSecretsToReqMap(context.Background(), reqMap, &ScheduleContext{
		Schedule: WorkflowSchedule{WorkshopMode: "optimizer"},
		Capabilities: WorkflowCapabilities{
			LLMConfig: &workflowtypes.PresetLLMConfig{
				Provider:          "codex-cli",
				ModelID:           "codex-cli",
				LLMAllocationMode: workflowtypes.LLMAllocationModeCodingAgent,
			},
		},
	})

	llmConfig, ok := reqMap["llm_config"].(map[string]interface{})
	if !ok {
		t.Fatalf("llm_config missing or wrong type: %#v", reqMap["llm_config"])
	}
	primary, ok := llmConfig["primary"].(map[string]interface{})
	if !ok {
		t.Fatalf("llm_config.primary missing or wrong type: %#v", llmConfig["primary"])
	}
	if got := primary["provider"]; got != "codex-cli" {
		t.Fatalf("provider = %#v, want codex-cli", got)
	}
	if got := primary["model_id"]; got != "gpt-5.5" {
		t.Fatalf("model_id = %#v, want gpt-5.5", got)
	}
	options, ok := primary["options"].(map[string]interface{})
	if !ok {
		t.Fatalf("options missing or wrong type: %#v", primary["options"])
	}
	if got := options["reasoning_effort"]; got != "xhigh" {
		t.Fatalf("reasoning_effort = %#v, want xhigh", got)
	}
	if got := reqMap["llm_config_source"]; got != llmConfigSourceScheduledAutoImprove {
		t.Fatalf("llm_config_source = %#v, want %q", got, llmConfigSourceScheduledAutoImprove)
	}
}

func TestApplyPulseLLMToReqMapUsesPulseOverrideWhenConfigured(t *testing.T) {
	reqMap := map[string]interface{}{}
	sctx := &ScheduleContext{
		Schedule: WorkflowSchedule{WorkshopMode: "run"},
		Capabilities: WorkflowCapabilities{
			LLMConfig: &workflowtypes.PresetLLMConfig{
				Provider: "claude-code",
				ModelID:  "claude-opus-4-6",
				PulseLLM: &workflowtypes.AgentLLMConfig{
					Provider: "codex-cli",
					ModelID:  "gpt-5.5",
					Options:  map[string]interface{}{"reasoning_effort": "high"},
				},
			},
		},
	}

	svc := &SchedulerService{}
	svc.applyLLMAndSecretsToReqMap(context.Background(), reqMap, sctx)
	svc.applyPulseLLMToReqMap(reqMap, sctx, "test-session")

	llmConfig, ok := reqMap["llm_config"].(map[string]interface{})
	if !ok {
		t.Fatalf("llm_config missing or wrong type: %#v", reqMap["llm_config"])
	}
	primary, ok := llmConfig["primary"].(map[string]interface{})
	if !ok {
		t.Fatalf("llm_config.primary missing or wrong type: %#v", llmConfig["primary"])
	}
	if got := primary["provider"]; got != "codex-cli" {
		t.Fatalf("provider = %#v, want codex-cli", got)
	}
	if got := primary["model_id"]; got != "gpt-5.5" {
		t.Fatalf("model_id = %#v, want gpt-5.5", got)
	}
	if got := reqMap["llm_config_source"]; got != llmConfigSourceScheduledPulse {
		t.Fatalf("llm_config_source = %#v, want %q", got, llmConfigSourceScheduledPulse)
	}
	options, ok := primary["options"].(map[string]interface{})
	if !ok {
		t.Fatalf("options missing or wrong type: %#v", primary["options"])
	}
	if got := options["reasoning_effort"]; got != "high" {
		t.Fatalf("reasoning_effort = %#v, want high", got)
	}
	if got := reqMap["llm_config_source"]; got != llmConfigSourceScheduledPulse {
		t.Fatalf("llm_config_source = %#v, want %q", got, llmConfigSourceScheduledPulse)
	}
}

func TestApplyGoalAdvisorLLMToReqMapUsesAdvisorOverrideWhenConfigured(t *testing.T) {
	reqMap := map[string]interface{}{}
	sctx := &ScheduleContext{
		Schedule: WorkflowSchedule{WorkshopMode: "run"},
		Capabilities: WorkflowCapabilities{
			LLMConfig: &workflowtypes.PresetLLMConfig{
				Provider: "claude-code",
				ModelID:  "claude-sonnet-5",
				AutoImproveLLM: &workflowtypes.AgentLLMConfig{
					Provider: "claude-code",
					ModelID:  "claude-opus-4-8",
					Options:  map[string]interface{}{"reasoning_effort": "high"},
				},
				PulseLLM: &workflowtypes.AgentLLMConfig{
					Provider: "claude-code",
					ModelID:  "claude-sonnet-5",
					Options:  map[string]interface{}{"reasoning_effort": "high"},
				},
			},
		},
	}

	svc := &SchedulerService{}
	svc.applyLLMAndSecretsToReqMap(context.Background(), reqMap, sctx)
	svc.applyGoalAdvisorLLMToReqMap(reqMap, sctx, "test-session")

	llmConfig, ok := reqMap["llm_config"].(map[string]interface{})
	if !ok {
		t.Fatalf("llm_config missing or wrong type: %#v", reqMap["llm_config"])
	}
	primary, ok := llmConfig["primary"].(map[string]interface{})
	if !ok {
		t.Fatalf("llm_config.primary missing or wrong type: %#v", llmConfig["primary"])
	}
	if got := primary["provider"]; got != "claude-code" {
		t.Fatalf("provider = %#v, want claude-code", got)
	}
	if got := primary["model_id"]; got != "claude-opus-4-8" {
		t.Fatalf("model_id = %#v, want claude-opus-4-8", got)
	}
	if got := reqMap["llm_config_source"]; got != llmConfigSourceScheduledAutoImprove {
		t.Fatalf("llm_config_source = %#v, want %q", got, llmConfigSourceScheduledAutoImprove)
	}
}

func TestBuildWorkshopRequestDisablesLiveInputDeliveryForSchedulerTurns(t *testing.T) {
	svc := &SchedulerService{}
	sctx := &ScheduleContext{
		WorkflowID:    "wf_test",
		WorkspacePath: "Workflow/test",
		Schedule:      WorkflowSchedule{ID: "daily", Name: "Daily"},
		Capabilities:  WorkflowCapabilities{},
		SourceType:    "workflow",
	}

	reqMap := svc.buildWorkshopRequest(context.Background(), sctx)
	if got := reqMap["disable_live_input_delivery"]; got != true {
		t.Fatalf("disable_live_input_delivery = %#v, want true", got)
	}
}

func TestRefreshSessionTmuxSnapshotsForIdleCheckCapturesFreshPane(t *testing.T) {
	store := terminals.NewStore()
	sessionID := "session-scheduler-refresh"
	tmuxSession := "tmux-scheduler-refresh"
	store.HandleEvent(sessionID, terminalRouteChunkEvent(sessionID, "workflow-step:review-plan", tmuxSession, "old pane", 1))

	oldRunOutput := runTerminalTmuxOutputCommand
	defer func() { runTerminalTmuxOutputCommand = oldRunOutput }()
	var calls [][]string
	runTerminalTmuxOutputCommand = func(ctx context.Context, args ...string) (string, error) {
		calls = append(calls, append([]string(nil), args...))
		return "fresh pane\n❯", nil
	}

	svc := &SchedulerService{api: &StreamingAPI{terminalStore: store}}
	if err := svc.refreshSessionTmuxSnapshotsForIdleCheck(context.Background(), sessionID); err != nil {
		t.Fatalf("refreshSessionTmuxSnapshotsForIdleCheck returned error: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("tmux capture calls = %d, want 1", len(calls))
	}
	snapshots := store.ListMetadata(sessionID)
	if len(snapshots) != 1 {
		t.Fatalf("snapshots = %d, want 1", len(snapshots))
	}
	if got := snapshots[0].Content; !strings.Contains(got, "fresh pane") {
		t.Fatalf("snapshot content = %q, want fresh capture", got)
	}
	if got := snapshots[0].ContentSource; got != "tmux_capture" {
		t.Fatalf("content source = %q, want tmux_capture", got)
	}
}

func TestRefreshSessionTmuxSnapshotsForIdleCheckMarksMissingPaneStale(t *testing.T) {
	store := terminals.NewStore()
	sessionID := "session-scheduler-missing"
	tmuxSession := "tmux-scheduler-missing"
	store.HandleEvent(sessionID, terminalRouteChunkEvent(sessionID, "workflow-step:review-plan", tmuxSession, "old pane", 1))

	oldRunOutput := runTerminalTmuxOutputCommand
	defer func() { runTerminalTmuxOutputCommand = oldRunOutput }()
	runTerminalTmuxOutputCommand = func(ctx context.Context, args ...string) (string, error) {
		return "", errors.New("can't find session: tmux-scheduler-missing")
	}

	svc := &SchedulerService{api: &StreamingAPI{terminalStore: store}}
	if err := svc.refreshSessionTmuxSnapshotsForIdleCheck(context.Background(), sessionID); err != nil {
		t.Fatalf("refreshSessionTmuxSnapshotsForIdleCheck returned error: %v", err)
	}
	snapshots := store.ListMetadata(sessionID)
	if len(snapshots) != 1 {
		t.Fatalf("snapshots = %d, want 1", len(snapshots))
	}
	if snapshots[0].Active {
		t.Fatalf("missing tmux snapshot should be inactive")
	}
	if got := snapshots[0].State; got != "stale" {
		t.Fatalf("state = %q, want stale", got)
	}
	if got := snapshots[0].TmuxSession; got != "" {
		t.Fatalf("tmux session = %q, want cleared", got)
	}
}

func TestWaitForWorkshopIdleRequiresTwoFreshIdleTmuxChecks(t *testing.T) {
	oldInterval := schedulerWorkshopIdlePollInterval
	schedulerWorkshopIdlePollInterval = time.Millisecond
	defer func() { schedulerWorkshopIdlePollInterval = oldInterval }()

	store := terminals.NewStore()
	sessionID := "session-scheduler-idle"
	tmuxSession := "tmux-scheduler-idle"
	store.HandleEvent(sessionID, terminalRouteChunkEvent(sessionID, "workflow-step:review-plan", tmuxSession, "old pane", 1))

	oldRunOutput := runTerminalTmuxOutputCommand
	defer func() { runTerminalTmuxOutputCommand = oldRunOutput }()
	calls := 0
	runTerminalTmuxOutputCommand = func(ctx context.Context, args ...string) (string, error) {
		calls++
		return "done\n❯", nil
	}

	svc := &SchedulerService{api: &StreamingAPI{terminalStore: store}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := svc.waitForWorkshopIdle(ctx, sessionID); err != nil {
		t.Fatalf("waitForWorkshopIdle returned error: %v", err)
	}
	if calls != schedulerWorkshopIdleConsecutiveChecks {
		t.Fatalf("tmux captures = %d, want %d", calls, schedulerWorkshopIdleConsecutiveChecks)
	}
}

func TestWaitForWorkshopIdleTimesOutWhenSessionStaysBusy(t *testing.T) {
	oldInterval := schedulerWorkshopIdlePollInterval
	oldMaxWait := schedulerWorkshopIdleMaxWait
	schedulerWorkshopIdlePollInterval = time.Millisecond
	schedulerWorkshopIdleMaxWait = 5 * time.Millisecond
	defer func() {
		schedulerWorkshopIdlePollInterval = oldInterval
		schedulerWorkshopIdleMaxWait = oldMaxWait
	}()

	sessionID := "session-scheduler-busy-timeout"
	api := &StreamingAPI{terminalStore: terminals.NewStore()}
	api.setSessionBusy(sessionID, true)
	svc := &SchedulerService{api: api}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := svc.waitForWorkshopIdle(ctx, sessionID)
	if err == nil {
		t.Fatal("waitForWorkshopIdle returned nil, want timeout")
	}
	if !strings.Contains(err.Error(), "workshop idle wait timed out") {
		t.Fatalf("error = %v, want timeout", err)
	}
}

func TestRunningWorkflowScheduleInSetLockedFindsOtherRunningSchedule(t *testing.T) {
	states := map[string]*ScheduleRuntimeState{
		"daily":     {LastStatus: "running", LastSessionID: "session-daily"},
		"optimizer": {LastStatus: "success", LastSessionID: "session-optimizer"},
	}

	id, sessionID := runningWorkflowScheduleInSetLocked(states, []string{"current", "daily", "optimizer"}, "current")
	if id != "daily" {
		t.Fatalf("running schedule id = %q, want daily", id)
	}
	if sessionID != "session-daily" {
		t.Fatalf("running schedule session = %q, want session-daily", sessionID)
	}
}

func TestRunningWorkflowScheduleInSetLockedIgnoresCurrentSchedule(t *testing.T) {
	states := map[string]*ScheduleRuntimeState{
		"current": {LastStatus: "running", LastSessionID: "session-current"},
	}

	id, sessionID := runningWorkflowScheduleInSetLocked(states, []string{"current"}, "current")
	if id != "" || sessionID != "" {
		t.Fatalf("running schedule = (%q, %q), want empty", id, sessionID)
	}
}

func TestWaitForLiveInputTurnCompleteRequiresBusyBeforeIdle(t *testing.T) {
	oldInterval := liveInputTurnPollInterval
	oldStableAfter := liveInputTurnNoBusyStableAfter
	liveInputTurnPollInterval = time.Millisecond
	liveInputTurnNoBusyStableAfter = time.Hour
	defer func() {
		liveInputTurnPollInterval = oldInterval
		liveInputTurnNoBusyStableAfter = oldStableAfter
	}()

	store := terminals.NewStore()
	sessionID := "session-live-input-wait"
	tmuxSession := "tmux-live-input-wait"
	store.HandleEvent(sessionID, terminalRouteChunkEvent(sessionID, "main:"+sessionID, tmuxSession, "old pane\n❯", 1))

	oldRunOutput := runTerminalTmuxOutputCommand
	defer func() { runTerminalTmuxOutputCommand = oldRunOutput }()
	outputs := []string{
		"prompt echoed\n❯",
		"thinking\nesc to interrupt",
		"final answer\n❯",
		"final answer\n❯",
	}
	calls := 0
	runTerminalTmuxOutputCommand = func(ctx context.Context, args ...string) (string, error) {
		if calls >= len(outputs) {
			return outputs[len(outputs)-1], nil
		}
		out := outputs[calls]
		calls++
		return out, nil
	}

	api := &StreamingAPI{terminalStore: store}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := api.waitForLiveInputTurnComplete(ctx, nil, sessionID); err != nil {
		t.Fatalf("waitForLiveInputTurnComplete returned error: %v", err)
	}
	if calls < 4 {
		t.Fatalf("tmux captures = %d, want at least 4; initial idle must not complete the live-input turn", calls)
	}
}

func TestApplyPulseLLMToReqMapUsesCodingAgentPulseDefaultWhenUnset(t *testing.T) {
	reqMap := map[string]interface{}{}
	sctx := &ScheduleContext{
		Schedule: WorkflowSchedule{WorkshopMode: "run"},
		Capabilities: WorkflowCapabilities{
			LLMConfig: &workflowtypes.PresetLLMConfig{
				Provider:          "claude-code",
				ModelID:           "claude-code",
				LLMAllocationMode: workflowtypes.LLMAllocationModeCodingAgent,
			},
		},
	}

	svc := &SchedulerService{}
	svc.applyLLMAndSecretsToReqMap(context.Background(), reqMap, sctx)
	svc.applyPulseLLMToReqMap(reqMap, sctx, "test-session")

	llmConfig, ok := reqMap["llm_config"].(map[string]interface{})
	if !ok {
		t.Fatalf("llm_config missing or wrong type: %#v", reqMap["llm_config"])
	}
	primary, ok := llmConfig["primary"].(map[string]interface{})
	if !ok {
		t.Fatalf("llm_config.primary missing or wrong type: %#v", llmConfig["primary"])
	}
	if got := primary["provider"]; got != "claude-code" {
		t.Fatalf("provider = %#v, want claude-code", got)
	}
	if got := primary["model_id"]; got != "claude-sonnet-5" {
		t.Fatalf("model_id = %#v, want claude-sonnet-5", got)
	}
	options, ok := primary["options"].(map[string]interface{})
	if !ok {
		t.Fatalf("options missing or wrong type: %#v", primary["options"])
	}
	if got := options["reasoning_effort"]; got != "high" {
		t.Fatalf("reasoning_effort = %#v, want high", got)
	}
}

func TestApplyPulseLLMToReqMapKeepsWorkflowModelWhenNoProviderDefault(t *testing.T) {
	reqMap := map[string]interface{}{}
	sctx := &ScheduleContext{
		Schedule: WorkflowSchedule{WorkshopMode: "run"},
		Capabilities: WorkflowCapabilities{
			LLMConfig: &workflowtypes.PresetLLMConfig{
				Provider: "openai",
				ModelID:  "gpt-5.4",
			},
		},
	}

	svc := &SchedulerService{}
	svc.applyLLMAndSecretsToReqMap(context.Background(), reqMap, sctx)
	svc.applyPulseLLMToReqMap(reqMap, sctx, "test-session")

	llmConfig, ok := reqMap["llm_config"].(map[string]interface{})
	if !ok {
		t.Fatalf("llm_config missing or wrong type: %#v", reqMap["llm_config"])
	}
	primary, ok := llmConfig["primary"].(map[string]interface{})
	if !ok {
		t.Fatalf("llm_config.primary missing or wrong type: %#v", llmConfig["primary"])
	}
	if got := primary["provider"]; got != "openai" {
		t.Fatalf("provider = %#v, want openai", got)
	}
	if got := primary["model_id"]; got != "gpt-5.4" {
		t.Fatalf("model_id = %#v, want gpt-5.4", got)
	}
}

func TestResolveChiefOfStaffLLMForScheduleUsesExplicitOverride(t *testing.T) {
	sctx := &ScheduleContext{
		SourceType: "multi-agent",
		Capabilities: WorkflowCapabilities{
			LLMConfig: &workflowtypes.PresetLLMConfig{
				Provider: "claude-code",
				ModelID:  "claude-code",
				ChiefOfStaffLLM: &workflowtypes.AgentLLMConfig{
					Provider: "codex-cli",
					ModelID:  "gpt-5.5",
					Options:  map[string]interface{}{"reasoning_effort": "xhigh"},
				},
			},
		},
	}

	got := resolveChiefOfStaffLLMForSchedule(context.Background(), sctx)
	if got == nil {
		t.Fatal("resolveChiefOfStaffLLMForSchedule() = nil")
	}
	if got.Provider != "codex-cli" || got.ModelID != "gpt-5.5" {
		t.Fatalf("resolveChiefOfStaffLLMForSchedule() = %+v, want codex-cli/gpt-5.5", got)
	}
	if got.Options["reasoning_effort"] != "xhigh" {
		t.Fatalf("reasoning_effort = %#v, want xhigh", got.Options["reasoning_effort"])
	}
}

func TestResolveChiefOfStaffLLMForScheduleUsesCodingAgentDefault(t *testing.T) {
	sctx := &ScheduleContext{
		SourceType: "multi-agent",
		Capabilities: WorkflowCapabilities{
			LLMConfig: &workflowtypes.PresetLLMConfig{
				Provider:          "claude-code",
				ModelID:           "claude-code",
				LLMAllocationMode: workflowtypes.LLMAllocationModeCodingAgent,
			},
		},
	}

	got := resolveChiefOfStaffLLMForSchedule(context.Background(), sctx)
	if got == nil {
		t.Fatal("resolveChiefOfStaffLLMForSchedule() = nil")
	}
	if got.Provider != "claude-code" || got.ModelID != "claude-opus-4-8" {
		t.Fatalf("resolveChiefOfStaffLLMForSchedule() = %+v, want claude-code/claude-opus-4-8", got)
	}
	if got.Options["reasoning_effort"] != "high" {
		t.Fatalf("reasoning_effort = %#v, want high", got.Options["reasoning_effort"])
	}
}

func TestResolveChiefOfStaffLLMFromDelegationConfigUsesExplicitScheduledModel(t *testing.T) {
	got := resolveChiefOfStaffLLMFromDelegationConfig(&virtualtools.DelegationTierConfig{
		ChiefOfStaff: &virtualtools.TierModel{
			Provider: "codex-cli",
			ModelID:  "gpt-5.5",
		},
		Main: &virtualtools.TierModel{
			Provider: "claude-code",
			ModelID:  "claude-code",
		},
	})
	if got == nil {
		t.Fatal("resolveChiefOfStaffLLMFromDelegationConfig() = nil")
	}
	if got.Provider != "codex-cli" || got.ModelID != "gpt-5.5" {
		t.Fatalf("resolveChiefOfStaffLLMFromDelegationConfig() = %+v, want codex-cli/gpt-5.5", got)
	}
}

func TestResolveChiefOfStaffLLMFromDelegationConfigUsesProviderDefault(t *testing.T) {
	got := resolveChiefOfStaffLLMFromDelegationConfig(&virtualtools.DelegationTierConfig{
		Main: &virtualtools.TierModel{
			Provider: "claude-code",
			ModelID:  "claude-code",
		},
	})
	if got == nil {
		t.Fatal("resolveChiefOfStaffLLMFromDelegationConfig() = nil")
	}
	if got.Provider != "claude-code" || got.ModelID != "claude-opus-4-8" {
		t.Fatalf("resolveChiefOfStaffLLMFromDelegationConfig() = %+v, want claude-code/claude-opus-4-8", got)
	}
}

func TestMaybeResumeLatestWorkflowThreadUsesPreviousScheduledSessionOnly(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)

	workspacePath := "Workflow/rtslatency"
	scheduleID := "schedule-1"
	writeWorkflowChatRuntime(t, root, workspacePath, "normal-user-chat", "claude-code", true)
	writeWorkflowChatRuntime(t, root, workspacePath, "previous-schedule-chat", "claude-code", true)
	writeScheduleRunsForTest(t, root, workspacePath, []ScheduleRunEntry{
		{
			ID:         "current-run",
			ScheduleID: scheduleID,
			SessionID:  "current-schedule-chat",
			Status:     "running",
			StartedAt:  time.Now().UTC(),
		},
		{
			ID:         "previous-run",
			ScheduleID: scheduleID,
			SessionID:  "previous-schedule-chat",
			Status:     "success",
			StartedAt:  time.Now().Add(-time.Hour).UTC(),
		},
	})

	reqMap := map[string]interface{}{}
	resumed := (&SchedulerService{}).maybeResumeLatestWorkflowThread(context.Background(), resumeTestScheduleContext(workspacePath, scheduleID), reqMap, "current-schedule-chat")
	if resumed != "previous-schedule-chat" {
		t.Fatalf("resumed session = %q, want previous scheduled session", resumed)
	}
	if got := reqMap["restored_conversation_session_id"]; got != "previous-schedule-chat" {
		t.Fatalf("restored_conversation_session_id = %#v, want previous scheduled session", got)
	}
}

func TestMaybeResumeLatestWorkflowThreadIgnoresNormalUserChat(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)

	workspacePath := "Workflow/rtslatency"
	scheduleID := "schedule-1"
	writeWorkflowChatRuntime(t, root, workspacePath, "normal-user-chat", "claude-code", true)
	writeScheduleRunsForTest(t, root, workspacePath, []ScheduleRunEntry{
		{
			ID:         "current-run",
			ScheduleID: scheduleID,
			SessionID:  "current-schedule-chat",
			Status:     "running",
			StartedAt:  time.Now().UTC(),
		},
	})

	reqMap := map[string]interface{}{}
	resumed := (&SchedulerService{}).maybeResumeLatestWorkflowThread(context.Background(), resumeTestScheduleContext(workspacePath, scheduleID), reqMap, "current-schedule-chat")
	if resumed != "" {
		t.Fatalf("resumed session = %q, want empty because normal user chats are not schedule runs", resumed)
	}
	if _, ok := reqMap["restored_conversation_session_id"]; ok {
		t.Fatalf("restored_conversation_session_id was set for a normal user chat: %#v", reqMap)
	}
}

func TestMaybeResumeLatestMultiAgentThreadUsesPreviousScheduledSessionOnly(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)

	userID := "default"
	scheduleID := "schedule-1"
	writeUserChatRuntime(t, root, userID, "normal-user-chat", "claude-code", true)
	writeUserChatRuntime(t, root, userID, "previous-schedule-chat", "claude-code", true)
	writeMultiAgentScheduleRunsForTest(t, root, userID, []ScheduleRunEntry{
		{
			ID:         "current-run",
			ScheduleID: scheduleID,
			SessionID:  "current-schedule-chat",
			Status:     "running",
			StartedAt:  time.Now().UTC(),
		},
		{
			ID:         "previous-run",
			ScheduleID: scheduleID,
			SessionID:  "previous-schedule-chat",
			Status:     "success",
			StartedAt:  time.Now().Add(-time.Hour).UTC(),
		},
	})

	reqMap := map[string]interface{}{}
	resumed := (&SchedulerService{}).maybeResumeLatestMultiAgentThread(context.Background(), resumeTestMultiAgentScheduleContext(userID, scheduleID), reqMap, "current-schedule-chat")
	if resumed != "previous-schedule-chat" {
		t.Fatalf("resumed session = %q, want previous scheduled session", resumed)
	}
	if got := reqMap["restored_conversation_session_id"]; got != "previous-schedule-chat" {
		t.Fatalf("restored_conversation_session_id = %#v, want previous scheduled session", got)
	}
}

func TestMaybeResumeLatestMultiAgentThreadIgnoresNormalUserChat(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)

	userID := "default"
	scheduleID := "schedule-1"
	writeUserChatRuntime(t, root, userID, "normal-user-chat", "claude-code", true)
	writeMultiAgentScheduleRunsForTest(t, root, userID, []ScheduleRunEntry{
		{
			ID:         "current-run",
			ScheduleID: scheduleID,
			SessionID:  "current-schedule-chat",
			Status:     "running",
			StartedAt:  time.Now().UTC(),
		},
	})

	reqMap := map[string]interface{}{}
	resumed := (&SchedulerService{}).maybeResumeLatestMultiAgentThread(context.Background(), resumeTestMultiAgentScheduleContext(userID, scheduleID), reqMap, "current-schedule-chat")
	if resumed != "" {
		t.Fatalf("resumed session = %q, want empty because normal user chats are not schedule runs", resumed)
	}
	if _, ok := reqMap["restored_conversation_session_id"]; ok {
		t.Fatalf("restored_conversation_session_id was set for a normal user chat: %#v", reqMap)
	}
}

func resumeTestScheduleContext(workspacePath, scheduleID string) *ScheduleContext {
	resumePrevious := true
	return &ScheduleContext{
		WorkspacePath: workspacePath,
		UserID:        "default",
		Schedule: WorkflowSchedule{
			ID:             scheduleID,
			ResumePrevious: &resumePrevious,
		},
		Capabilities: WorkflowCapabilities{
			LLMConfig: &workflowtypes.PresetLLMConfig{
				Provider: "claude-code",
				ModelID:  "claude-opus-4-6",
			},
		},
	}
}

func resumeTestMultiAgentScheduleContext(userID, scheduleID string) *ScheduleContext {
	resumePrevious := true
	return &ScheduleContext{
		UserID:     userID,
		SourceType: "multi-agent",
		Schedule: WorkflowSchedule{
			ID:             scheduleID,
			ResumePrevious: &resumePrevious,
		},
		Capabilities: WorkflowCapabilities{
			LLMConfig: &workflowtypes.PresetLLMConfig{
				Provider: "claude-code",
				ModelID:  "claude-opus-4-6",
			},
		},
	}
}

func writeScheduleRunsForTest(t *testing.T, root, workspacePath string, runs []ScheduleRunEntry) {
	t.Helper()
	dir := filepath.Join(root, filepath.FromSlash(workspacePath))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(runs, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "schedule-runs.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeMultiAgentScheduleRunsForTest(t *testing.T, root, userID string, runs []ScheduleRunEntry) {
	t.Helper()
	dir := filepath.Join(root, "_users", userID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(runs, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "multiagent-schedule-runs.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeWorkflowChatRuntime(t *testing.T, root, workspacePath, sessionID, provider string, resumeSupported bool) {
	t.Helper()
	convDir := filepath.Join(root, filepath.FromSlash(workspacePath), "builder", "conversation", "2026-05-20")
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(map[string]interface{}{
		"session_id":    sessionID,
		"agent_mode":    "workflow_phase",
		"workshop_mode": "workshop",
		"runtime": map[string]interface{}{
			"kind":                 "coding_agent",
			"provider":             provider,
			"model_id":             "claude-opus-4-6",
			"external_session_id":  "external-" + sessionID,
			"resume_supported":     resumeSupported,
			"resume_flag":          "--resume",
			"workspace_path":       workspacePath,
			"workshop_mode":        "workshop",
			"agent_session_handle": map[string]interface{}{},
		},
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(convDir, "session-"+sessionID+"-conversation.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeUserChatRuntime(t *testing.T, root, userID, sessionID, provider string, resumeSupported bool) {
	t.Helper()
	convDir := filepath.Join(root, "_users", userID, "chat_history", "2026-05-20")
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(map[string]interface{}{
		"session_id": sessionID,
		"agent_mode": "simple",
		"runtime": map[string]interface{}{
			"kind":                 "coding_agent",
			"provider":             provider,
			"model_id":             "claude-opus-4-6",
			"external_session_id":  "external-" + sessionID,
			"resume_supported":     resumeSupported,
			"resume_flag":          "--resume",
			"agent_session_handle": map[string]interface{}{},
		},
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(convDir, "session-"+sessionID+"-conversation.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}
