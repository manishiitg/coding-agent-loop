package step_based_workflow

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator"
	workspacepkg "github.com/manishiitg/coding-agent-loop/agent_go/pkg/workspace"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

func TestWriteStepConfigsToSubdirUsesFileScopedPlanningAccess(t *testing.T) {
	const (
		sessionID      = "step-config-mutation-write-access"
		workspacePath  = "Workflow/demo"
		stepConfigPath = workspacePath + "/planning/step_config.json"
	)

	wroteStepConfig := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/api/documents/"+stepConfigPath {
			t.Errorf("unexpected write path: %s", r.URL.Path)
		}
		wroteStepConfig = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()

	workspacepkg.SetSessionFolderGuard(sessionID, []string{workspacePath}, []string{workspacePath})
	workspacepkg.SetSessionFolderGuardBlockedWritePaths(sessionID, []string{workspacePath + "/planning"})
	defer workspacepkg.ClearSessionShellConfig(sessionID)

	client := workspacepkg.NewClient(server.URL)
	ctx := context.WithValue(context.Background(), common.ChatSessionIDKey, sessionID)
	if err := client.ValidatePathWithContext(ctx, stepConfigPath, true); err == nil || !strings.Contains(err.Error(), "blocked for writes") {
		t.Fatalf("ordinary file write should remain blocked, got %v", err)
	}

	base, err := orchestrator.NewBaseOrchestrator(
		loggerv2.NewNoop(), nil, orchestrator.OrchestratorTypeWorkflow, "", 0, "",
		nil, nil, false, &orchestrator.LLMConfig{}, 1, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("NewBaseOrchestrator: %v", err)
	}
	base.WorkspaceClient = client
	base.SetWorkspacePath(workspacePath)
	hcpo := &StepBasedWorkflowOrchestrator{BaseOrchestrator: base}

	if err := hcpo.WriteStepConfigsToSubdir(ctx, "planning", []StepConfig{{ID: "step-1"}}); err != nil {
		t.Fatalf("typed step-config write was blocked: %v", err)
	}
	if !wroteStepConfig {
		t.Fatal("typed step-config writer did not reach the workspace API")
	}

	managedCtx := withStepConfigMutationWriteAccess(ctx, workspacePath, "planning")
	if err := client.ValidatePathWithContext(managedCtx, stepConfigPath, true); err != nil {
		t.Fatalf("managed step-config path should be writable: %v", err)
	}
	if err := client.ValidatePathWithContext(managedCtx, workspacePath+"/planning/plan.json", true); err == nil || !strings.Contains(err.Error(), "blocked for writes") {
		t.Fatalf("step-config capability must not unlock plan.json, got %v", err)
	}
	if err := client.ValidatePathWithContext(ctx, stepConfigPath, true); err == nil || !strings.Contains(err.Error(), "blocked for writes") {
		t.Fatalf("step-config capability leaked into the caller context: %v", err)
	}
}
