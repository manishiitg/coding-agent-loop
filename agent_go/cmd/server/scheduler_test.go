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
			name: "builtin memory schedule is excluded",
			sctx: &ScheduleContext{
				SourceType: "multi-agent",
				Schedule:   WorkflowSchedule{ID: builtinAutoEnrichMemoryID, Name: "Auto-enrich memory"},
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
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("task report update message missing %q:\n%s", want, msg)
		}
	}
}

func TestPostRunMonitorUsesSeparateLLMCostTimeReportStep(t *testing.T) {
	steps := postRunMonitorSteps()
	if got := len(steps); got != 8 {
		t.Fatalf("postRunMonitorSteps() length = %d, want 8", got)
	}
	for i, want := range []string{"triage", "fix", "artifact", "report", "cadence", "backup", "publish", "notify"} {
		if got := steps[i].label; got != want {
			t.Fatalf("postRunMonitorSteps()[%d].label = %q, want %q", i, got, want)
		}
	}

	var triage string
	var fix string
	var artifact string
	var report string
	var cadence string
	var backup string
	var notify string
	for _, step := range steps {
		if step.label == "triage" {
			triage = step.query
		}
		if step.label == "fix" {
			fix = step.query
		}
		if step.label == "artifact" {
			artifact = step.query
		}
		if step.label == "report" {
			report = step.query
		}
		if step.label == "cadence" {
			cadence = step.query
		}
		if step.label == "backup" {
			backup = step.query
		}
		if step.label == "notify" {
			notify = step.query
		}
	}
	if triage == "" {
		t.Fatal("triage step not found")
	}
	if fix == "" {
		t.Fatal("fix step not found")
	}
	if artifact == "" {
		t.Fatal("artifact step not found")
	}
	if report == "" {
		t.Fatal("report step not found")
	}
	if cadence == "" {
		t.Fatal("cadence step not found")
	}
	if backup == "" {
		t.Fatal("backup step not found")
	}
	if notify == "" {
		t.Fatal("notify step not found")
	}
	if strings.Contains(triage, "LLM/COST/TIME REPORT") || strings.Contains(triage, "costs/execution") {
		t.Fatalf("triage step should not include report-only LLM/cost/time audit:\n%s", triage)
	}
	if !strings.Contains(triage, "Triage is diagnosis/verdict only") {
		t.Fatalf("triage step should clarify that hardening is separate:\n%s", triage)
	}
	for _, want := range []string{
		"pending Chief of Staff recommendation cards",
		"mark_cos_recommendation_status",
		"queued_auto_improve",
		"CoS rec_ids processed",
	} {
		if !strings.Contains(triage, want) {
			t.Fatalf("triage step missing Chief of Staff handoff text %q:\n%s", want, triage)
		}
	}
	if !strings.Contains(fix, "FIX / HARDEN") {
		t.Fatalf("fix step should be the harden step:\n%s", fix)
	}
	for _, want := range []string{
		`get_reference_doc(kind="optimize-playbook")`,
		"harden_workflow",
		"This turn does not improvise manual workflow edits",
		"Decision - Pulse harden",
		"Bug fix",
		"Report fix",
		"Eval fix",
		"mark_cos_recommendation_status",
	} {
		if !strings.Contains(fix, want) {
			t.Fatalf("fix step missing %q:\n%s", want, fix)
		}
	}
	for _, want := range []string{
		"ARTIFACT REVIEW",
		`get_workflow_command_guidance(kind="review-artifact-drift"`,
		"review_artifact_sync",
		"mark_changelog_artifact_reviewed",
		"not part of harden",
		"Artifact drift",
	} {
		if !strings.Contains(artifact, want) {
			t.Fatalf("artifact step missing %q:\n%s", want, artifact)
		}
	}

	for _, want := range []string{
		"LLM/COST/TIME REPORT",
		"after artifact review",
		"costs/execution",
		"costs/evaluation",
		"costs/phase/token_usage.json",
		"timing summaries",
		"by plan step and by agent/sub-agent",
		"builder/card.cost.html",
		"data-axis='cost'",
		"normal|elevated|missing",
		`get_reference_doc(kind="report-plan")`,
		"window.report.get",
		"do NOT change model tiers",
		"Cost/time",
		"Report fix",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("report step missing %q:\n%s", want, report)
		}
	}
	for _, want := range []string{
		"once every run",
		"steady healthy",
		"Bug/Goal state",
		"cost card is elevated/missing",
		"builder/card.cost.html",
	} {
		if !strings.Contains(notify, want) {
			t.Fatalf("notify step missing %q:\n%s", want, notify)
		}
	}
	for _, want := range []string{
		"AUTO-IMPROVE CADENCE",
		"cron-only",
		"list_schedules",
		"update_schedule",
		"never edit workflow.json directly",
		"weekly",
		"twice-weekly",
		"daily-until-recovered",
		"biweekly-over-time",
		"Decision - Auto-improve cadence",
	} {
		if !strings.Contains(cadence, want) {
			t.Fatalf("cadence step missing %q:\n%s", want, cadence)
		}
	}
	if !strings.Contains(backup, "BACK UP FINAL STATE") || !strings.Contains(backup, "before publish") {
		t.Fatalf("backup step should snapshot final state before publish:\n%s", backup)
	}
}

func TestPostRunMonitorPrependsWorkflowVersionUpgradeForOldManifest(t *testing.T) {
	steps := postRunMonitorStepsForManifest(&WorkflowManifest{Version: "1.0.0"})
	if got := len(steps); got != 11 {
		t.Fatalf("postRunMonitorStepsForManifest(old) length = %d, want 11", got)
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
	if got := steps[3].label; got != "triage" {
		t.Fatalf("fourth step label = %q, want triage", got)
	}
}

func TestPostRunMonitorPrependsWorkflowVersionUpgradeForMissingVersion(t *testing.T) {
	steps := postRunMonitorStepsForManifest(&WorkflowManifest{})
	if got := len(steps); got != 11 {
		t.Fatalf("postRunMonitorStepsForManifest(missing version) length = %d, want 11", got)
	}
	if !strings.Contains(steps[0].query, `Current workflow.json version seen by scheduler: "1.0.0"`) {
		t.Fatalf("missing version should be treated as 1.0.0:\n%s", steps[0].query)
	}
}

func TestPostRunMonitorPrependsPublishGateUpgradeForVersion101Manifest(t *testing.T) {
	steps := postRunMonitorStepsForManifest(&WorkflowManifest{Version: "1.0.1"})
	if got := len(steps); got != 10 {
		t.Fatalf("postRunMonitorStepsForManifest(1.0.1) length = %d, want 10", got)
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
	if got := steps[2].label; got != "triage" {
		t.Fatalf("third step label = %q, want triage", got)
	}
}

func TestPostRunMonitorPrependsHTMLReportUpgradeForVersion102Manifest(t *testing.T) {
	steps := postRunMonitorStepsForManifest(&WorkflowManifest{Version: "1.0.2"})
	if got := len(steps); got != 9 {
		t.Fatalf("postRunMonitorStepsForManifest(1.0.2) length = %d, want 9", got)
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
	if got := steps[1].label; got != "triage" {
		t.Fatalf("second step label = %q, want triage", got)
	}
}

func TestPostRunMonitorDoesNotPrependWorkflowVersionUpgradeForCurrentManifest(t *testing.T) {
	steps := postRunMonitorStepsForManifest(&WorkflowManifest{Version: WorkflowContractCurrentVersion})
	if got := len(steps); got != 8 {
		t.Fatalf("postRunMonitorStepsForManifest(current) length = %d, want 8", got)
	}
	if got := steps[0].label; got != "triage" {
		t.Fatalf("first step label = %q, want triage", got)
	}
	if got := steps[2].label; got != "artifact" {
		t.Fatalf("third step label = %q, want artifact", got)
	}
	for _, want := range []string{
		"STEP 3",
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

func TestOptimizerScheduleMessagesIgnoresStoredMessagesAndInjectsCanonicalTurns(t *testing.T) {
	stored := []string{`Do not ask for confirmation. Call get_workflow_command_guidance(kind="improve-workflow", focus="scheduled improve fire").`}

	got := optimizerScheduleMessages(stored, []string{"prod"})
	if len(got) != 5 {
		t.Fatalf("optimizerScheduleMessages() length = %d, want 5", len(got))
	}

	checks := []struct {
		idx  int
		want string
	}{
		{0, "STEP 1/5 - PRE-BACKUP"},
		{1, "STEP 2/5 - IMPROVE"},
		{1, "CANONICAL AUTO IMPROVE MESSAGE"},
		{1, "group_names=prod"},
		{1, `get_workflow_command_guidance(kind="improve-workflow"`},
		{1, "critical evidence review"},
		{1, "hallucinations, unsupported claims, bugs, misreporting"},
		{1, "expert-advisor scan"},
		{1, "out-of-plan opportunities"},
		{1, "report dashboard in best possible shape"},
		{1, "measure and track the workflow goal"},
		{1, "Decision - Auto-improve - Applied"},
		{1, "entry decision major"},
		{1, "Improvement"},
		{1, "Advisor idea"},
		{1, "Report fix"},
		{1, "Eval fix"},
		{1, "Artifact drift"},
		{1, "Cost/time"},
		{1, "Why now, Evidence, Change, Expected impact, Files touched, and Risk / gap"},
		{1, "Do not call notify_user"},
		{2, "STEP 3/5 - BACKUP FINAL STATE"},
		{3, "STEP 4/5 - PUBLISH"},
		{4, "STEP 5/5 - NOTIFY"},
	}
	for _, check := range checks {
		if !strings.Contains(got[check.idx], check.want) {
			t.Fatalf("optimizerScheduleMessages()[%d] missing %q:\n%s", check.idx, check.want, got[check.idx])
		}
	}
	if strings.Contains(got[1], stored[0]) {
		t.Fatalf("optimizerScheduleMessages() should ignore stored optimizer messages, got:\n%s", got[1])
	}
}

func TestOptimizerScheduleMessagesDefaultsToImproveWhenNoStoredMessage(t *testing.T) {
	got := optimizerScheduleMessages(nil, []string{"group-a"})
	if len(got) != 5 {
		t.Fatalf("optimizerScheduleMessages(nil) length = %d, want 5", len(got))
	}
	for _, want := range []string{
		`get_workflow_command_guidance(kind="improve-workflow"`,
		"group_names=group-a",
		"report/dashboard misstatements",
		"out-of-plan opportunities",
		"Advisor idea",
		"Chief of Staff recommendation cards",
		"mark_cos_recommendation_status",
		"success-criteria status, tracked signals, trend/delta",
		"Decision - Auto-improve - Applied",
		"do NOT call harden_workflow",
		"do NOT call notify_user",
	} {
		if !strings.Contains(got[1], want) {
			t.Fatalf("default improve message missing %q:\n%s", want, got[1])
		}
	}
}

func TestOptimizerScheduleMessagesReplacesLegacyExplicitQueue(t *testing.T) {
	legacy := []string{
		"STEP 1/5 — PRE-BACKUP",
		"STEP 2/5 — IMPROVE",
		"STEP 3/5 — BACKUP FINAL STATE",
		"STEP 4/5 — PUBLISH",
		"STEP 5/5 — NOTIFY",
	}

	got := optimizerScheduleMessages(legacy, nil)
	if len(got) != 5 {
		t.Fatalf("optimizerScheduleMessages() length = %d, want 5", len(got))
	}
	if strings.Contains(strings.Join(got, "\n"), strings.Join(legacy, "\n")) {
		t.Fatalf("optimizerScheduleMessages() should replace legacy stored queues, got:\n%s", strings.Join(got, "\n"))
	}
	for _, want := range []string{
		"STEP 1/5 - PRE-BACKUP",
		"STEP 2/5 - IMPROVE",
		"STEP 3/5 - BACKUP FINAL STATE",
		"STEP 4/5 - PUBLISH",
		"STEP 5/5 - NOTIFY",
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
			name:         "optimizer schedule uses auto improve override",
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

func TestResolveChiefOfStaffLLMForMemoryScheduleUsesPulseOverride(t *testing.T) {
	sctx := &ScheduleContext{
		SourceType: "multi-agent",
		Schedule:   WorkflowSchedule{ID: builtinAutoEnrichMemoryID},
		Capabilities: WorkflowCapabilities{
			LLMConfig: &workflowtypes.PresetLLMConfig{
				Provider: "claude-code",
				ModelID:  "claude-code",
				PulseLLM: &workflowtypes.AgentLLMConfig{
					Provider: "codex-cli",
					ModelID:  "gpt-5.4",
				},
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
	if got.Provider != "codex-cli" || got.ModelID != "gpt-5.4" {
		t.Fatalf("resolveChiefOfStaffLLMForSchedule() = %+v, want codex-cli/gpt-5.4", got)
	}
}

func TestResolveChiefOfStaffLLMForMemoryScheduleUsesCodingAgentPulseDefault(t *testing.T) {
	sctx := &ScheduleContext{
		SourceType: "multi-agent",
		Schedule:   WorkflowSchedule{ID: builtinAutoEnrichMemoryID},
		Capabilities: WorkflowCapabilities{
			LLMConfig: &workflowtypes.PresetLLMConfig{
				Provider:          "claude-code",
				ModelID:           "claude-code",
				LLMAllocationMode: workflowtypes.LLMAllocationModeCodingAgent,
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
	if got.Provider != "claude-code" || got.ModelID != "claude-sonnet-5" {
		t.Fatalf("resolveChiefOfStaffLLMForSchedule() = %+v, want claude-code/claude-sonnet-5", got)
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

func TestResolveMemoryLLMFromDelegationConfigUsesProviderPulseDefault(t *testing.T) {
	got := resolveMemoryLLMFromDelegationConfig(&virtualtools.DelegationTierConfig{
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
		t.Fatal("resolveMemoryLLMFromDelegationConfig() = nil")
	}
	if got.Provider != "claude-code" || got.ModelID != "claude-sonnet-5" {
		t.Fatalf("resolveMemoryLLMFromDelegationConfig() = %+v, want claude-code/claude-sonnet-5", got)
	}
	if got.Options["reasoning_effort"] != "high" {
		t.Fatalf("reasoning_effort = %#v, want high", got.Options["reasoning_effort"])
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
