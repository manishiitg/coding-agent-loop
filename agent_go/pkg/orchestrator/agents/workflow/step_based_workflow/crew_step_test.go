package step_based_workflow

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/workflowtypes"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

var errTestCrewDown = errors.New("crew down")
var errTestFileNotFound = errors.New("file not found")

func testCrewStepJSON() string {
	return `{
		"type": "crew",
		"id": "crew-review",
		"title": "Review with RTS crew",
		"description": "Ask the maintained reviewer crew",
		"crew_profile_id": "work",
		"crew_project_id": "rts",
		"trigger_id": "trig-1",
		"instruction": "Review PR {{pr}}",
		"context_dependencies": ["pr.json"],
		"context_output": "review.md",
		"timeout_seconds": 600,
		"next_step_id": "ship"
	}`
}

func TestCrewPlanStepJSONRoundTrip(t *testing.T) {
	raw := json.RawMessage(testCrewStepJSON())

	parsed, err := parseStepFromJSON(raw, 0, "step")
	if err != nil {
		t.Fatalf("parseStepFromJSON: %v", err)
	}
	crew, ok := parsed.(*CrewPlanStep)
	if !ok {
		t.Fatalf("parsed type = %T, want *CrewPlanStep", parsed)
	}
	if crew.CrewProfileID != "work" || crew.CrewProjectID != "rts" || crew.TriggerID != "trig-1" ||
		crew.Instruction != "Review PR {{pr}}" || crew.TimeoutSeconds != 600 || crew.NextStepID != "ship" {
		t.Fatalf("parsed crew step = %+v", crew)
	}

	var asMap map[string]interface{}
	if err := json.Unmarshal(raw, &asMap); err != nil {
		t.Fatal(err)
	}
	fromMap, err := convertMapToStep(asMap)
	if err != nil {
		t.Fatalf("convertMapToStep: %v", err)
	}
	if _, ok := fromMap.(*CrewPlanStep); !ok {
		t.Fatalf("map type = %T, want *CrewPlanStep", fromMap)
	}

	nested, err := unmarshalStepFromJSON(raw)
	if err != nil {
		t.Fatalf("unmarshalStepFromJSON: %v", err)
	}
	if _, ok := nested.(*CrewPlanStep); !ok {
		t.Fatalf("nested type = %T, want *CrewPlanStep", nested)
	}

	// Marshaling forces the type discriminator even when unset.
	crew.Type = ""
	encoded, err := json.Marshal(crew)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["type"] != "crew" {
		t.Fatalf("marshaled type = %v, want crew", decoded["type"])
	}
}

func TestValidateCrewStepFieldsTyped(t *testing.T) {
	valid := &CrewPlanStep{
		CommonStepFields: CommonStepFields{ID: "crew-1", Title: "Review"},
		CrewProfileID:    "work", CrewProjectID: "rts", TriggerID: "trig-1",
		Instruction: "Review it",
	}
	if err := validateCrewStepFieldsTyped(valid); err != nil {
		t.Fatalf("valid step rejected: %v", err)
	}
	for _, tt := range []struct {
		name   string
		mutate func(*CrewPlanStep)
		want   string
	}{
		{"missing id", func(s *CrewPlanStep) { s.ID = "" }, "ID"},
		{"missing title", func(s *CrewPlanStep) { s.Title = "" }, "title"},
		{"missing profile", func(s *CrewPlanStep) { s.CrewProfileID = "" }, "crew_profile_id"},
		{"missing project", func(s *CrewPlanStep) { s.CrewProjectID = "" }, "crew_project_id"},
		{"missing trigger", func(s *CrewPlanStep) { s.TriggerID = "" }, "trigger_id"},
		{"missing instruction", func(s *CrewPlanStep) { s.Instruction = "  " }, "instruction"},
		{"negative timeout", func(s *CrewPlanStep) { s.TimeoutSeconds = -1 }, "timeout_seconds"},
	} {
		step := *valid
		tt.mutate(&step)
		err := validateCrewStepFieldsTyped(&step)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Fatalf("%s: err = %v, want containing %q", tt.name, err, tt.want)
		}
	}
}

func TestUpdateToolForStepTypeCrew(t *testing.T) {
	if got := updateToolForStepType(StepTypeCrew); got != "update_crew_step" {
		t.Fatalf("update tool = %q, want update_crew_step", got)
	}
}

func TestCrewStepNextStepIDReferenceValidation(t *testing.T) {
	good := &PlanningResponse{Steps: []PlanStepInterface{
		&CrewPlanStep{
			Type:             StepTypeCrew,
			CommonStepFields: CommonStepFields{ID: "crew-1", Title: "Review"},
			CrewProfileID:    "work", CrewProjectID: "rts", TriggerID: "trig-1",
			Instruction: "Review it", NextStepID: "ship",
		},
		&RegularPlanStep{CommonStepFields: CommonStepFields{ID: "ship", Title: "Ship"}},
	}}
	if err := ValidatePlanStructure(good); err != nil {
		t.Fatalf("valid crew next_step_id rejected: %v", err)
	}
	bad := &PlanningResponse{Steps: []PlanStepInterface{
		&CrewPlanStep{
			Type:             StepTypeCrew,
			CommonStepFields: CommonStepFields{ID: "crew-1", Title: "Review"},
			CrewProfileID:    "work", CrewProjectID: "rts", TriggerID: "trig-1",
			Instruction: "Review it", NextStepID: "ghost",
		},
	}}
	if err := ValidatePlanStructure(bad); err == nil {
		t.Fatal("dangling crew next_step_id accepted")
	}
}

func TestPopulateRuntimeFieldsCrew(t *testing.T) {
	step := &CrewPlanStep{
		CommonStepFields: CommonStepFields{ID: "crew-1", Title: "Review"},
	}
	if err := populateRuntimeFields(step, nil); err != nil {
		t.Fatalf("populateRuntimeFields: %v", err)
	}
}

func TestWebhookStepIndexCrew(t *testing.T) {
	steps := []PlanStepInterface{
		&RegularPlanStep{CommonStepFields: CommonStepFields{ID: "first", Title: "First"}},
		&CrewPlanStep{CommonStepFields: CommonStepFields{ID: "crew-1", Title: "Review"}},
	}
	idx, err := WebhookStepIndex(steps, "crew-1")
	if err != nil || idx != 1 {
		t.Fatalf("webhook index = %d err=%v, want 1", idx, err)
	}
}

func testCrewToolFiles(seedPlan string) (map[string]string, func(context.Context, string) (string, error), func(context.Context, string, string) error) {
	files := map[string]string{}
	readFile := func(_ context.Context, path string) (string, error) {
		if strings.HasSuffix(path, "plan.json") {
			if seedPlan == "" {
				return "", errTestFileNotFound
			}
			return seedPlan, nil
		}
		if content, ok := files[path]; ok {
			return content, nil
		}
		return "", errTestFileNotFound
	}
	writeFile := func(_ context.Context, path, content string) error {
		files[path] = content
		return nil
	}
	return files, readFile, writeFile
}

func TestAddCrewStepTool(t *testing.T) {
	seed := `{"objective": "test", "steps": [{"type": "regular", "id": "first", "title": "First"}]}`
	files, readFile, writeFile := testCrewToolFiles(seed)
	moveFile := func(context.Context, string, string) error { return nil }
	add := createAddCrewStepExecutor("Workflow/test", loggerv2.NewNoop(), readFile, writeFile, moveFile)
	out, err := add(context.Background(), map[string]interface{}{
		"id": "crew-1", "title": "Review", "crew_profile_id": "work",
		"crew_project_id": "rts", "trigger_id": "trig-1", "instruction": "Review it",
		"context_dependencies": []interface{}{}, "insert_after_step_id": "first", "reason": "test",
	})
	if err != nil {
		t.Fatalf("add_crew_step: %v", err)
	}
	if !strings.Contains(out, "crew-1") {
		t.Fatalf("add output = %q", out)
	}
	var written string
	for path, content := range files {
		if strings.HasSuffix(path, "plan.json") {
			written = content
		}
	}
	var plan PlanningResponse
	if err := json.Unmarshal([]byte(written), &plan); err != nil {
		t.Fatalf("written plan: %v", err)
	}
	if len(plan.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(plan.Steps))
	}
	crew, ok := plan.Steps[1].(*CrewPlanStep)
	if !ok {
		t.Fatalf("step type = %T, want *CrewPlanStep", plan.Steps[1])
	}
	if crew.CrewProjectID != "rts" || crew.TriggerID != "trig-1" || crew.Instruction != "Review it" {
		t.Fatalf("crew step = %+v", crew)
	}
}

func TestAddCrewStepToolRejectsInvalid(t *testing.T) {
	seed := `{"objective": "test", "steps": [{"type": "regular", "id": "first", "title": "First"}]}`
	_, readFile, writeFile := testCrewToolFiles(seed)
	moveFile := func(context.Context, string, string) error { return nil }
	add := createAddCrewStepExecutor("Workflow/test", loggerv2.NewNoop(), readFile, writeFile, moveFile)
	_, err := add(context.Background(), map[string]interface{}{
		"id": "crew-1", "title": "Review", "crew_profile_id": "work",
		"crew_project_id": "rts", "instruction": "Review it",
		"context_dependencies": []interface{}{}, "insert_after_step_id": "first", "reason": "test",
	})
	if err == nil || !strings.Contains(err.Error(), "trigger_id") {
		t.Fatalf("missing trigger err = %v, want trigger_id complaint", err)
	}
}

func TestUpdateCrewStepTool(t *testing.T) {
	seed := `{"objective": "test", "steps": [{
		"type": "crew", "id": "crew-review", "title": "Review with RTS crew",
		"crew_profile_id": "work", "crew_project_id": "rts", "trigger_id": "trig-1",
		"instruction": "Review it", "context_dependencies": []
	}]}`
	files, readFile, writeFile := testCrewToolFiles(seed)
	update := createUpdateCrewStepExecutor("Workflow/test", loggerv2.NewNoop(), readFile, writeFile)
	out, err := update(context.Background(), map[string]interface{}{
		"existing_step_id": "crew-review", "instruction": "Review PR carefully",
		"timeout_seconds": float64(300), "reason": "test",
	})
	if err != nil {
		t.Fatalf("update_crew_step: %v", err)
	}
	if !strings.Contains(out, "crew-review") {
		t.Fatalf("update output = %q", out)
	}
	var written string
	for path, content := range files {
		if strings.HasSuffix(path, "plan.json") {
			written = content
		}
	}
	var plan PlanningResponse
	if err := json.Unmarshal([]byte(written), &plan); err != nil {
		t.Fatalf("written plan: %v", err)
	}
	crew, ok := plan.Steps[0].(*CrewPlanStep)
	if !ok {
		t.Fatalf("step type = %T", plan.Steps[0])
	}
	if crew.Instruction != "Review PR carefully" || crew.TimeoutSeconds != 300 || crew.TriggerID != "trig-1" {
		t.Fatalf("merged crew step = %+v", crew)
	}
}

func TestUpdateCrewStepToolRejectsNonCrew(t *testing.T) {
	seed := `{"objective": "test", "steps": [{"type": "regular", "id": "first", "title": "First"}]}`
	_, readFile, writeFile := testCrewToolFiles(seed)
	update := createUpdateCrewStepExecutor("Workflow/test", loggerv2.NewNoop(), readFile, writeFile)
	_, err := update(context.Background(), map[string]interface{}{
		"existing_step_id": "first", "instruction": "x", "reason": "test",
	})
	if err == nil || !strings.Contains(err.Error(), "not a crew step") {
		t.Fatalf("non-crew update err = %v", err)
	}
}

// fakeCrewRunner captures the request a crew step sends and replays a canned
// outcome, so executor tests never touch Crew services.
type fakeCrewRunner struct {
	req    CrewStepRequest
	result CrewStepResult
	err    error
}

func (f *fakeCrewRunner) RunCrewStep(_ context.Context, req CrewStepRequest) (CrewStepResult, error) {
	f.req = req
	return f.result, f.err
}

func TestExecuteCrewStep(t *testing.T) {
	files := map[string]string{
		"Workflow/instagram/runs/iteration-0/execution/step-pr/pr.json": `{"summary": "looks good"}`,
	}
	fake := &fakeCrewRunner{result: CrewStepResult{CrewRunID: "run-9", SessionID: "sess-9", Status: "success", FinalResponse: "Approved"}}
	hcpo := &StepBasedWorkflowOrchestrator{
		BaseOrchestrator:  newFakeWorkspaceAPIWithContent(t, files),
		selectedRunFolder: "iteration-0",
		currentGroupName:  "production",
		workflowID:        "wf-1",
		variableValues:    map[string]string{"pr": "87"},
		executionOptions:  &ExecutionOptions{CrewRunner: fake},
	}
	producer := &RegularPlanStep{CommonStepFields: CommonStepFields{ID: "step-pr", Title: "Prepare", ContextOutput: "pr.json"}}
	crew := &CrewPlanStep{
		Type:             StepTypeCrew,
		CommonStepFields: CommonStepFields{ID: "crew-1", Title: "Review", ContextDependencies: []string{"pr.json"}},
		CrewProfileID:    "work",
		CrewProjectID:    "rts",
		TriggerID:        "trig-1",
		Instruction:      "Review PR {{pr}}",
	}
	allSteps := []PlanStepInterface{producer, crew}
	result, updated, err := hcpo.executeCrewStep(context.Background(), crew, 1, &StepProgress{}, nil, &ExecutionContext{}, allSteps)
	if err != nil {
		t.Fatalf("executeCrewStep: %v", err)
	}
	if result.FinalResponse != "Approved" || result.CrewRunID != "run-9" {
		t.Fatalf("result = %+v", result)
	}
	if len(updated) != 1 || updated[0] != "response.md" {
		t.Fatalf("context files = %v, want [response.md]", updated)
	}
	req := fake.req
	if req.WorkflowID != "wf-1" || req.WorkflowRunFolder != "iteration-0" || req.StepID != "crew-1" || req.Group != "production" {
		t.Fatalf("request identity = %+v", req)
	}
	if req.Instruction != "Review PR 87" {
		t.Fatalf("instruction = %q, want rendered variables", req.Instruction)
	}
	if req.TimeoutSeconds != DefaultCrewStepTimeoutSeconds {
		t.Fatalf("timeout = %d, want default %d", req.TimeoutSeconds, DefaultCrewStepTimeoutSeconds)
	}
	pr, ok := req.Inputs["pr"].(map[string]interface{})
	if !ok || pr["summary"] != "looks good" {
		t.Fatalf("inputs = %+v, want parsed pr.json", req.Inputs)
	}
	if files["Workflow/instagram/runs/iteration-0/execution/crew-1/response.md"] != "Approved" {
		t.Fatalf("response.md not persisted: %v", files)
	}
	var record map[string]interface{}
	if err := json.Unmarshal([]byte(files["Workflow/instagram/runs/iteration-0/execution/crew-1/crew-run.json"]), &record); err != nil {
		t.Fatalf("crew-run.json: %v", err)
	}
	if record["crew_run_id"] != "run-9" || record["status"] != "success" || record["step_id"] != "crew-1" || record["group"] != "production" {
		t.Fatalf("record = %v", record)
	}
}

func TestExecuteCrewStepRejectsWithoutRunner(t *testing.T) {
	hcpo := &StepBasedWorkflowOrchestrator{
		BaseOrchestrator: newFakeWorkspaceAPIWithContent(t, map[string]string{}),
		executionOptions: &ExecutionOptions{},
	}
	crew := &CrewPlanStep{
		CommonStepFields: CommonStepFields{ID: "crew-1", Title: "Review"},
		CrewProfileID:    "work", CrewProjectID: "rts", TriggerID: "trig-1", Instruction: "x",
	}
	_, _, err := hcpo.executeCrewStep(context.Background(), crew, 0, &StepProgress{}, nil, &ExecutionContext{}, []PlanStepInterface{crew})
	if err == nil || !strings.Contains(err.Error(), "does not bind a Crew runner") {
		t.Fatalf("nil runner err = %v", err)
	}
}

func TestExecuteCrewStepFailsMissingInput(t *testing.T) {
	fake := &fakeCrewRunner{}
	hcpo := &StepBasedWorkflowOrchestrator{
		BaseOrchestrator:  newFakeWorkspaceAPIWithContent(t, map[string]string{}),
		selectedRunFolder: "iteration-0",
		executionOptions:  &ExecutionOptions{CrewRunner: fake},
	}
	crew := &CrewPlanStep{
		CommonStepFields: CommonStepFields{ID: "crew-1", Title: "Review", ContextDependencies: []string{"ghost.json"}},
		CrewProfileID:    "work", CrewProjectID: "rts", TriggerID: "trig-1", Instruction: "x",
	}
	_, _, err := hcpo.executeCrewStep(context.Background(), crew, 0, &StepProgress{}, nil, &ExecutionContext{}, []PlanStepInterface{crew})
	if err == nil || !strings.Contains(err.Error(), "crew input file not found") {
		t.Fatalf("missing input err = %v", err)
	}
	if fake.req.StepID != "" {
		t.Fatal("runner must not be invoked when inputs are missing")
	}
}

func TestExecuteCrewStepSurfacesRunnerError(t *testing.T) {
	fake := &fakeCrewRunner{err: errTestCrewDown}
	hcpo := &StepBasedWorkflowOrchestrator{
		BaseOrchestrator:  newFakeWorkspaceAPIWithContent(t, map[string]string{}),
		selectedRunFolder: "iteration-0",
		executionOptions:  &ExecutionOptions{CrewRunner: fake},
	}
	crew := &CrewPlanStep{
		CommonStepFields: CommonStepFields{ID: "crew-1", Title: "Review"},
		CrewProfileID:    "work", CrewProjectID: "rts", TriggerID: "trig-1", Instruction: "x",
	}
	_, _, err := hcpo.executeCrewStep(context.Background(), crew, 0, &StepProgress{}, nil, &ExecutionContext{}, []PlanStepInterface{crew})
	if err == nil || !strings.Contains(err.Error(), "crew step") {
		t.Fatalf("runner error = %v", err)
	}
}

func TestExecuteCrewStepRecordsFailedRun(t *testing.T) {
	files := map[string]string{}
	completed := time.Now().UTC()
	fake := &fakeCrewRunner{
		result: CrewStepResult{CrewRunID: "run-9", SessionID: "sess-9", Status: "error", Error: "boom", CompletedAt: &completed},
		err:    errTestCrewDown,
	}
	hcpo := &StepBasedWorkflowOrchestrator{
		BaseOrchestrator:  newFakeWorkspaceAPIWithContent(t, files),
		selectedRunFolder: "iteration-0",
		executionOptions:  &ExecutionOptions{CrewRunner: fake},
	}
	crew := &CrewPlanStep{
		CommonStepFields: CommonStepFields{ID: "crew-1", Title: "Review"},
		CrewProfileID:    "work", CrewProjectID: "rts", TriggerID: "trig-1", Instruction: "x",
	}
	_, _, err := hcpo.executeCrewStep(context.Background(), crew, 0, &StepProgress{}, nil, &ExecutionContext{}, []PlanStepInterface{crew})
	if err == nil {
		t.Fatal("failed run accepted")
	}
	var record map[string]interface{}
	if err := json.Unmarshal([]byte(files["Workflow/instagram/runs/iteration-0/execution/crew-1/crew-run.json"]), &record); err != nil {
		t.Fatalf("failed-run record: %v", err)
	}
	if record["crew_run_id"] != "run-9" || record["status"] != "error" || record["error"] != "boom" {
		t.Fatalf("record = %v", record)
	}
	if _, ok := files["Workflow/instagram/runs/iteration-0/execution/crew-1/response.md"]; ok {
		t.Fatal("failed run must not write a response file")
	}
}

func TestExecuteSingleStepRejectsNestedCrew(t *testing.T) {
	hcpo := &StepBasedWorkflowOrchestrator{BaseOrchestrator: newFakeWorkspaceAPIWithContent(t, map[string]string{})}
	crew := &CrewPlanStep{CommonStepFields: CommonStepFields{ID: "crew-1", Title: "Review"}}
	_, _, err := hcpo.executeSingleStep(context.Background(), crew, 0, "step-1-sub", 1, 0, nil, &StepProgress{}, true, &ExecutionContext{}, []PlanStepInterface{crew}, true, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "only as a top-level step") {
		t.Fatalf("nested crew err = %v", err)
	}
}

func TestPersistCrewStepTokenUsageAttributesStepBucket(t *testing.T) {
	hcpo := &StepBasedWorkflowOrchestrator{BaseOrchestrator: newFakeWorkspaceAPIWithContent(t, map[string]string{})}
	ctx := context.Background()
	usage := &workflowtypes.CrewRunTokenUsage{}
	usage.AddModel("model-a", "prov", 100, 10, 0, 0, 0, 0.5, 1)
	usage.AddModel("model-b", "prov", 50, 5, 0, 0, 0, 0.25, 2)
	if err := hcpo.persistCrewStepTokenUsage(ctx, "iteration-0/default", "crew-1", 0, "exec-1", usage); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("costs", "execution", "default", orchestrator.CostDateKey(time.Now())+".json")
	content, err := hcpo.ReadWorkspaceFile(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	var daily orchestrator.DailyGroupTokenUsageFile
	if err := json.Unmarshal([]byte(content), &daily); err != nil {
		t.Fatal(err)
	}
	entry := daily.Executions["exec-1"]
	if entry == nil || entry.TokenUsage == nil {
		t.Fatalf("executions = %+v, want exec-1", daily.Executions)
	}
	bucket := entry.TokenUsage.ByStepAndModel["execution_only:crew-1"]
	if bucket == nil || bucket["model-a"] == nil || bucket["model-a"].InputTokens != 100 {
		t.Fatalf("step bucket = %+v", entry.TokenUsage.ByStepAndModel)
	}
	if bucket["model-b"] == nil || bucket["model-b"].InputTokens != 50 || bucket["model-b"].LLMCallCount != 2 {
		t.Fatalf("step bucket = %+v", entry.TokenUsage.ByStepAndModel)
	}
	if entry.TokenUsage.ByModel["model-a"] == nil || entry.TokenUsage.ByModel["model-a"].InputTokens != 100 {
		t.Fatalf("run totals = %+v", entry.TokenUsage.ByModel)
	}
}

func TestPersistCrewStepTokenUsageNoopsAndUnknownFallback(t *testing.T) {
	files := map[string]string{}
	hcpo := &StepBasedWorkflowOrchestrator{BaseOrchestrator: newFakeWorkspaceAPIWithContent(t, files)}
	ctx := context.Background()
	if err := hcpo.persistCrewStepTokenUsage(ctx, "iteration-0/default", "crew-1", 0, "exec-2", nil); err != nil {
		t.Fatal(err)
	}
	if err := hcpo.persistCrewStepTokenUsage(ctx, "iteration-0/default", "crew-1", 0, "exec-2", &workflowtypes.CrewRunTokenUsage{}); err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("empty usage wrote %d files", len(files))
	}
	modelLess := &workflowtypes.CrewRunTokenUsage{PromptTokens: 7, CompletionTokens: 1}
	if err := hcpo.persistCrewStepTokenUsage(ctx, "iteration-0/default", "crew-1", 0, "exec-2", modelLess); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("costs", "execution", "default", orchestrator.CostDateKey(time.Now())+".json")
	content, err := hcpo.ReadWorkspaceFile(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	var daily orchestrator.DailyGroupTokenUsageFile
	if err := json.Unmarshal([]byte(content), &daily); err != nil {
		t.Fatal(err)
	}
	bucket := daily.Executions["exec-2"].TokenUsage.ByStepAndModel["execution_only:crew-1"]
	if bucket == nil || bucket["unknown"] == nil || bucket["unknown"].InputTokens != 7 {
		t.Fatalf("fallback bucket = %+v", daily.Executions["exec-2"].TokenUsage.ByStepAndModel)
	}
}

func TestBindCrewRunner(t *testing.T) {
	fake := &fakeCrewRunner{}

	plain := &StepBasedWorkflowOrchestrator{}
	bindCrewRunner(plain, fake)
	opts := plain.GetExecutionOptions()
	if opts == nil || opts.CrewRunner == nil {
		t.Fatal("session without options binds no crew runner")
	}

	scheduled := &StepBasedWorkflowOrchestrator{}
	scheduled.SetExecutionOptions(&ExecutionOptions{SelectedRunFolder: "iteration-0/prod", RunKind: "schedule"})
	bindCrewRunner(scheduled, fake)
	kept := scheduled.GetExecutionOptions()
	if kept == nil || kept.CrewRunner == nil {
		t.Fatal("scheduled session binds no crew runner")
	}
	if kept.SelectedRunFolder != "iteration-0/prod" || kept.RunKind != "schedule" {
		t.Fatalf("binding clobbered schedule options: %+v", kept)
	}

	unbound := &StepBasedWorkflowOrchestrator{}
	bindCrewRunner(unbound, nil)
	if unbound.GetExecutionOptions() != nil {
		t.Fatal("nil runner created execution options")
	}

	bindCrewRunner(nil, fake)
}
