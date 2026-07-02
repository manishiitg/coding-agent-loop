package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
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

func TestPostRunMonitorUsesSeparateLLMCostTimeReportStep(t *testing.T) {
	steps := postRunMonitorSteps()
	if got := len(steps); got != 7 {
		t.Fatalf("postRunMonitorSteps() length = %d, want 7", got)
	}
	for i, want := range []string{"triage", "fix", "artifact", "report", "backup", "publish", "notify"} {
		if got := steps[i].label; got != want {
			t.Fatalf("postRunMonitorSteps()[%d].label = %q, want %q", i, got, want)
		}
	}

	var triage string
	var fix string
	var artifact string
	var report string
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
	if !strings.Contains(fix, "FIX / HARDEN") {
		t.Fatalf("fix step should be the harden step:\n%s", fix)
	}
	for _, want := range []string{
		`get_reference_doc(kind="optimize-playbook")`,
		"harden_workflow",
		"This turn does not improvise manual workflow edits",
	} {
		if !strings.Contains(fix, want) {
			t.Fatalf("fix step missing %q:\n%s", want, fix)
		}
	}
	for _, want := range []string{
		"ARTIFACT REVIEW",
		`get_workflow_command_guidance(kind="review-artifact-drift"`,
		"review_artifact_sync",
		"not part of harden",
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
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("report step missing %q:\n%s", want, report)
		}
	}
	for _, want := range []string{
		"cost card is elevated/missing",
		"builder/card.cost.html",
	} {
		if !strings.Contains(notify, want) {
			t.Fatalf("notify step missing %q:\n%s", want, notify)
		}
	}
	if !strings.Contains(backup, "BACK UP FINAL STATE") || !strings.Contains(backup, "before publish") {
		t.Fatalf("backup step should snapshot final state before publish:\n%s", backup)
	}
}

func TestPostRunMonitorPrependsWorkflowVersionUpgradeForOldManifest(t *testing.T) {
	steps := postRunMonitorStepsForManifest(&WorkflowManifest{Version: "1.0.0"})
	if got := len(steps); got != 8 {
		t.Fatalf("postRunMonitorStepsForManifest(old) length = %d, want 8", got)
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
	} {
		if !strings.Contains(steps[0].query, want) {
			t.Fatalf("upgrade step missing %q:\n%s", want, steps[0].query)
		}
	}
	if got := steps[1].label; got != "triage" {
		t.Fatalf("second step label = %q, want triage", got)
	}
}

func TestPostRunMonitorPrependsWorkflowVersionUpgradeForMissingVersion(t *testing.T) {
	steps := postRunMonitorStepsForManifest(&WorkflowManifest{})
	if got := len(steps); got != 8 {
		t.Fatalf("postRunMonitorStepsForManifest(missing version) length = %d, want 8", got)
	}
	if !strings.Contains(steps[0].query, `Current workflow.json version seen by scheduler: "1.0.0"`) {
		t.Fatalf("missing version should be treated as 1.0.0:\n%s", steps[0].query)
	}
}

func TestPostRunMonitorDoesNotPrependWorkflowVersionUpgradeForCurrentManifest(t *testing.T) {
	steps := postRunMonitorStepsForManifest(&WorkflowManifest{Version: WorkflowContractCurrentVersion})
	if got := len(steps); got != 7 {
		t.Fatalf("postRunMonitorStepsForManifest(current) length = %d, want 7", got)
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
		"not part of harden",
	} {
		if !strings.Contains(steps[2].query, want) {
			t.Fatalf("artifact step missing %q:\n%s", want, steps[2].query)
		}
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
		{1, "report dashboard in best possible shape"},
		{1, "measure and track the workflow goal"},
		{1, "Decision - Auto-improve - Applied"},
		{1, "entry decision major"},
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
	if got := primary["model_id"]; got != "claude-fable-5" {
		t.Fatalf("model_id = %#v, want claude-fable-5", got)
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
	options, ok := primary["options"].(map[string]interface{})
	if !ok {
		t.Fatalf("options missing or wrong type: %#v", primary["options"])
	}
	if got := options["reasoning_effort"]; got != "high" {
		t.Fatalf("reasoning_effort = %#v, want high", got)
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
	if got.Provider != "claude-code" || got.ModelID != "claude-fable-5" {
		t.Fatalf("resolveChiefOfStaffLLMForSchedule() = %+v, want claude-code/claude-fable-5", got)
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
	if got.Provider != "claude-code" || got.ModelID != "claude-fable-5" {
		t.Fatalf("resolveChiefOfStaffLLMFromDelegationConfig() = %+v, want claude-code/claude-fable-5", got)
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
