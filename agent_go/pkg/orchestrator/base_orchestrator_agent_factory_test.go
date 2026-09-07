package orchestrator

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/agents"
	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/observability"
)

type failedSetupAgent struct {
	agents.OrchestratorAgent
	err    error
	closed int
}

func (a *failedSetupAgent) Initialize(context.Context) error { return a.err }
func (a *failedSetupAgent) Close() error                     { a.closed++; return nil }

func TestAgentFactoriesClosePartiallyInitializedAgent(t *testing.T) {
	bo, err := NewBaseOrchestrator(loggerv2.NewNoop(), nil, OrchestratorTypeWorkflow, "", 0, "", nil, nil, false, &LLMConfig{}, 1, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	bo.SetMCPSessionID("factory-cleanup-test")
	for _, withConfig := range []bool{false, true} {
		failure := errors.New("fixture initialization failed")
		partial := &failedSetupAgent{err: failure}
		factory := func(*agents.OrchestratorAgentConfig, loggerv2.Logger, observability.Tracer, mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return partial
		}
		var got agents.OrchestratorAgent
		if withConfig {
			config := agents.NewOrchestratorAgentConfig("fixture")
			got, err = bo.CreateAndSetupStandardAgentWithConfig(context.Background(), config, "execution", 0, 0, "step", factory, nil, nil, false)
		} else {
			got, err = bo.CreateAndSetupStandardAgent(context.Background(), "fixture", "execution", 0, 0, "step", 1, agents.OutputFormatStructured, factory, nil, nil)
		}
		if got != nil || !errors.Is(err, failure) || partial.closed != 1 {
			t.Fatalf("withConfig=%v: failed setup returned agent=%v err=%v close count=%d", withConfig, got, err, partial.closed)
		}
	}
}

func TestSyncCodingAgentWorkingDirUsesShellSessionWorkingDir(t *testing.T) {
	docsRoot := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", docsRoot)

	sessionID := "workflow-step-session"
	common.SetSessionWorkingDir(sessionID, "Workflow/testing/runs/iteration-0/execution")
	defer common.ClearSessionShellConfig(sessionID)

	config := agents.NewOrchestratorAgentConfig("step-agent")
	config.MCPSessionID = sessionID
	config.CodingAgentWorkingDir = filepath.Join(docsRoot, "Workflow", "testing")

	syncCodingAgentWorkingDirWithShellSession(config)

	want := filepath.Join(docsRoot, "Workflow", "testing", "runs", "iteration-0", "execution")
	if config.CodingAgentWorkingDir != want {
		t.Fatalf("CodingAgentWorkingDir = %q, want %q", config.CodingAgentWorkingDir, want)
	}
}
