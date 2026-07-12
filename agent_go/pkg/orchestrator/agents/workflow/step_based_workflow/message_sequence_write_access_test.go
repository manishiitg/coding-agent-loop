package step_based_workflow

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
)

// TestMessageSequenceAbsPath_IncludesWorkflowRoot guards the forward-pipe bug:
// the absolute path the message_sequence agent is handed (StepExecutionPath,
// item/code dirs) MUST include the workflow root (GetWorkspacePath, e.g.
// "Workflow/social-media"). Without it the agent writes to <docsRoot>/runs/...,
// outside its workflow folder, where downstream context_dependencies can't see
// the file.
func TestMessageSequenceAbsPath_IncludesWorkflowRoot(t *testing.T) {
	docsRoot := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", docsRoot)

	base, err := orchestrator.NewBaseOrchestrator(
		loggerv2.NewNoop(), nil, orchestrator.OrchestratorTypeWorkflow, "", 0, "",
		[]string{"test-server"}, nil, false, &orchestrator.LLMConfig{}, 1, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("NewBaseOrchestrator: %v", err)
	}
	base.SetWorkspacePath("Workflow/social-media")
	hcpo := &StepBasedWorkflowOrchestrator{BaseOrchestrator: base, selectedRunFolder: "iteration-0"}

	stepExecRel := hcpo.messageSequenceExecutionRelPath("step-5", "step-report") // runs/iteration-0/execution/step-report
	got := hcpo.messageSequenceAbsPath(stepExecRel)
	want := filepath.Join(docsRoot, "Workflow/social-media", "runs", "iteration-0", "execution", "step-report")
	if got != want {
		t.Fatalf("messageSequenceAbsPath = %q, want %q (must include docsRoot + workflow root)", got, want)
	}
	if !strings.Contains(filepath.ToSlash(got), "Workflow/social-media") {
		t.Fatalf("agent-facing path is missing the workflow root: %q", got)
	}
}

func TestMessageSequenceExecutionRelPath_UsesNormalStepFolder(t *testing.T) {
	hcpo := &StepBasedWorkflowOrchestrator{selectedRunFolder: "iteration-0"}
	for _, tc := range []struct{ stepPath, stepID string }{
		{"step-5", "step-run-intent-orchestrator"},
		{"step-3-sub-login", "login-specialist"},
	} {
		got := hcpo.messageSequenceExecutionRelPath(tc.stepPath, tc.stepID)
		// Must equal the folder every other step writes to (execution/<stepID>) —
		// the folder downstream context_dependencies resolve against.
		want := filepath.Join("runs", "iteration-0", "execution", getArtifactFolderName(tc.stepID, tc.stepPath))
		if got != want {
			t.Fatalf("messageSequenceExecutionRelPath(%q,%q) = %q, want normal step folder %q", tc.stepPath, tc.stepID, got, want)
		}
		if strings.Contains(got, "message_sequences") {
			t.Fatalf("sequence still writes to isolated message_sequences folder: %q", got)
		}
	}
}

func TestMessageSequenceRuntimeSessionIDStableForSequence(t *testing.T) {
	hcpo := &StepBasedWorkflowOrchestrator{
		selectedRunFolder: "iteration-0",
		currentGroupName:  "Acme Group",
	}

	gotA := hcpo.messageSequenceRuntimeSessionID("step-5", "review-specialist")
	gotB := hcpo.messageSequenceRuntimeSessionID("step-5", "review-specialist")
	if gotA != gotB {
		t.Fatalf("runtime session id changed between sequence items: %q vs %q", gotA, gotB)
	}
	if !strings.Contains(gotA, "iteration-0") || !strings.Contains(gotA, "acme-group") || !strings.Contains(gotA, "review-specialist") {
		t.Fatalf("runtime session id missing scope parts: %q", gotA)
	}
	if strings.Contains(gotA, "item") {
		t.Fatalf("runtime session id should not include sanitizer fallback for non-empty scope: %q", gotA)
	}
}

func TestMessageSequenceRuntimeSessionIDOmitsEmptyScope(t *testing.T) {
	hcpo := &StepBasedWorkflowOrchestrator{}

	got := hcpo.messageSequenceRuntimeSessionID("step-2", "writer")
	if got != "msgseq-step-2-writer" {
		t.Fatalf("runtime session id = %q, want msgseq-step-2-writer", got)
	}
}

func TestMessageSequenceWriteAccess_RejectsPerFilePaths(t *testing.T) {
	var w MessageSequenceWriteAccess
	err := json.Unmarshal([]byte(`{"db": true, "paths": ["db/session_health.json"]}`), &w)
	if err == nil {
		t.Fatal("expected error for per-file paths in write_access, got nil")
	}
	if !strings.Contains(err.Error(), "per-file scoping") {
		t.Fatalf("error should explain per-file scoping is unsupported, got: %v", err)
	}
}

func TestMessageSequenceWriteAccess_FolderBooleansOK(t *testing.T) {
	var w MessageSequenceWriteAccess
	if err := json.Unmarshal([]byte(`{"db": true, "knowledgebase": true}`), &w); err != nil {
		t.Fatalf("folder-level booleans should unmarshal cleanly, got: %v", err)
	}
	if !w.DB || !w.Knowledgebase || w.Learnings {
		t.Fatalf("unexpected decoded write_access: %+v", w)
	}
}

func TestMessageSequenceWriteAccess_EmptyOK(t *testing.T) {
	var w MessageSequenceWriteAccess
	if err := json.Unmarshal([]byte(`{}`), &w); err != nil {
		t.Fatalf("empty write_access should unmarshal cleanly, got: %v", err)
	}
	if w != (MessageSequenceWriteAccess{}) {
		t.Fatalf("empty write_access should be zero value, got: %+v", w)
	}
}

func TestMessageSequenceTemplateVarsReflectItemWriteAccess(t *testing.T) {
	docsRoot := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", docsRoot)

	base, err := orchestrator.NewBaseOrchestrator(
		loggerv2.NewNoop(), nil, orchestrator.OrchestratorTypeWorkflow, "", 0, "",
		[]string{"test-server"}, nil, false, &orchestrator.LLMConfig{}, 1, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("NewBaseOrchestrator: %v", err)
	}
	base.SetWorkspacePath("Workflow/test-flow")
	hcpo := &StepBasedWorkflowOrchestrator{BaseOrchestrator: base, selectedRunFolder: "iteration-0"}
	step := msgSeqStep(MessageSequenceItem{ID: "capture", Type: "user_message"})
	item := MessageSequenceItem{
		ID:          "capture",
		Type:        "user_message",
		WriteAccess: MessageSequenceWriteAccess{DB: true, Knowledgebase: true, Learnings: true},
	}
	readPaths, writePaths := hcpo.setupMessageSequenceFolderGuard("step-1", step.GetID(), getAgentConfigs(step), item.WriteAccess)
	vars := hcpo.buildMessageSequenceTemplateVars(step, item, 0, "step-1", "write the durable notes", readPaths, writePaths, item.WriteAccess)

	if got := vars["KbAccess"]; got != KBAccessReadWrite {
		t.Fatalf("KbAccess = %q, want %q", got, KBAccessReadWrite)
	}
	if note := vars["MessageSequenceAccessNote"]; !strings.Contains(note, "db/") || !strings.Contains(note, "knowledgebase/notes/") || !strings.Contains(note, "learnings/_global/") {
		t.Fatalf("access note does not list item write grants: %q", note)
	}
	if got := vars["KBGuidanceBlock"]; !strings.Contains(got, "Knowledgebase contribution") {
		t.Fatalf("KBGuidanceBlock missing direct-write guidance: %q", got)
	}
	wantNotesPath := filepath.ToSlash(filepath.Join(docsRoot, "Workflow/test-flow/knowledgebase/notes")) + "/"
	if got := vars["KBGuidanceBlock"]; !strings.Contains(got, wantNotesPath) ||
		!strings.Contains(got, "Do not use shell redirection, heredocs, tee, Python") {
		t.Fatalf("KBGuidanceBlock should use absolute notes path and patch-only writes: %q", got)
	}
}

func TestMessageSequenceTemplateVarsUseEffectiveWriteAccess(t *testing.T) {
	docsRoot := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", docsRoot)

	base, err := orchestrator.NewBaseOrchestrator(
		loggerv2.NewNoop(), nil, orchestrator.OrchestratorTypeWorkflow, "", 0, "",
		[]string{"test-server"}, nil, false, &orchestrator.LLMConfig{}, 1, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("NewBaseOrchestrator: %v", err)
	}
	base.SetWorkspacePath("Workflow/test-flow")
	hcpo := &StepBasedWorkflowOrchestrator{BaseOrchestrator: base, selectedRunFolder: "iteration-0"}
	step := msgSeqStep(MessageSequenceItem{ID: "capture", Type: "user_message"})
	item := MessageSequenceItem{
		ID:          "capture",
		Type:        "user_message",
		WriteAccess: MessageSequenceWriteAccess{Learnings: true},
	}
	effectiveAccess := MessageSequenceWriteAccess{}
	readPaths, writePaths := hcpo.setupMessageSequenceFolderGuard("step-1", step.GetID(), getAgentConfigs(step), effectiveAccess)
	vars := hcpo.buildMessageSequenceTemplateVars(step, item, 0, "step-1", "write the durable notes", readPaths, writePaths, effectiveAccess)

	if note := vars["MessageSequenceAccessNote"]; strings.Contains(strings.TrimPrefix(note, "Reads are available for execution outputs, soul, builder logs, db/, knowledgebase/, and learnings/_global/. "), "learnings/_global/") {
		t.Fatalf("write access note should reflect effective grants, not raw item grants: %q", note)
	}
	if writes := vars["FolderGuardWritePaths"]; strings.Contains(writes, "learnings/_global") {
		t.Fatalf("folder guard write paths should not include stripped learnings grant: %q", writes)
	}
}

func msgSeqStep(items ...MessageSequenceItem) *MessageSequencePlanStep {
	return &MessageSequencePlanStep{
		CommonStepFields: CommonStepFields{
			ID:          "step-seq",
			Title:       "Sequence",
			Description: "do work",
		},
		Items: items,
	}
}

func TestMessageSequenceItemReportedFailure(t *testing.T) {
	tests := []struct {
		name       string
		summary    string
		wantFailed bool
		wantReason string
	}{
		{name: "failed with reason", summary: "did the work\nSTATUS: FAILED — cannot write db/x.json: no db write access", wantFailed: true, wantReason: "cannot write db/x.json: no db write access"},
		{name: "failed no space", summary: "STATUS:FAILED - blocked by folder guard", wantFailed: true, wantReason: "blocked by folder guard"},
		{name: "completed is not failed", summary: "all done\nSTATUS: COMPLETED", wantFailed: false},
		{name: "prose mention of failed is not the marker", summary: "the previous attempt failed but I recovered", wantFailed: false},
		{name: "no status marker", summary: "wrote the queue and validated it", wantFailed: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reason, failed := messageSequenceItemReportedFailure(tc.summary)
			if failed != tc.wantFailed {
				t.Fatalf("failed=%v, want %v (summary=%q)", failed, tc.wantFailed, tc.summary)
			}
			if tc.wantFailed && reason != tc.wantReason {
				t.Fatalf("reason=%q, want %q", reason, tc.wantReason)
			}
		})
	}
}

func TestMessageSequenceItemCannotEscalateReadOnlyStorePermissions(t *testing.T) {
	hcpo := newMessageSequenceClosingTestOrchestrator(t)
	config := &AgentConfigs{
		DBAccess:            DBAccessRead,
		KnowledgebaseAccess: KBAccessRead,
		LearningsAccess:     LearningsAccessRead,
	}
	_, writePaths := hcpo.setupMessageSequenceFolderGuard("step-1", "readonly", config, MessageSequenceWriteAccess{
		DB: true, Knowledgebase: true, Learnings: true,
	})
	joined := strings.Join(writePaths, "\n")
	for _, forbidden := range []string{"/db", "/knowledgebase/notes", "/learnings/_global"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("read-only step unexpectedly received write access to %s: %v", forbidden, writePaths)
		}
	}
}
