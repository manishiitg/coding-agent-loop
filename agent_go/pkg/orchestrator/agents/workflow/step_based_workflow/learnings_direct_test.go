package step_based_workflow

import (
	"context"
	"strings"
	"testing"

	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/agents"
)

type fakeDirectLearningAgent struct {
	cfg *agents.OrchestratorAgentConfig
}

func (f *fakeDirectLearningAgent) Execute(context.Context, map[string]string, []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	return "", nil, nil
}

func (f *fakeDirectLearningAgent) GetType() string { return "fake" }

func (f *fakeDirectLearningAgent) GetConfig() *agents.OrchestratorAgentConfig { return f.cfg }

func (f *fakeDirectLearningAgent) Initialize(context.Context) error { return nil }

func (f *fakeDirectLearningAgent) Close() error { return nil }

func (f *fakeDirectLearningAgent) GetBaseAgent() *agents.BaseAgent { return nil }

func TestBuildLearningsContributionTurnRequiresPatchToolForAllWrites(t *testing.T) {
	targetPath := "/tmp/workspace-docs/Workflow/social-media/learnings/_global"
	msg := BuildLearningsContributionTurnWithTarget("unfollow-cleanup", "Unfollow stale X accounts.", "Capture the unfollow flow.", false, targetPath)
	for _, want := range []string{
		"Use `diff_patch_workspace_file` for every write",
		"including creating a new `/tmp/workspace-docs/Workflow/social-media/learnings/_global/references/<topic>.md` file",
		"Do not use shell redirection, heredocs, tee, Python",
		"`execute_shell_command` for read-only inspection",
		"cat '/tmp/workspace-docs/Workflow/social-media/learnings/_global/SKILL.md'",
		"do not rely on your shell working directory",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("learning prompt missing %q:\n%s", want, msg)
		}
	}
	for _, forbidden := range []string{
		"heredoc creation",
		"diff_patch_workspace_file` (for updating existing files)",
	} {
		if strings.Contains(msg, forbidden) {
			t.Fatalf("learning prompt still contains stale write guidance %q:\n%s", forbidden, msg)
		}
	}
}

func TestPrepareDirectLearningTurnKeepsWorkingDirAndRestoresSession(t *testing.T) {
	base, err := orchestrator.NewBaseOrchestrator(
		loggerv2.NewNoop(), nil, orchestrator.OrchestratorTypeWorkflow, "", 0, "",
		[]string{"test-server"}, nil, false, &orchestrator.LLMConfig{}, 1, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("NewBaseOrchestrator: %v", err)
	}
	base.SetWorkspacePath("Workflow/social-media")
	hcpo := &StepBasedWorkflowOrchestrator{BaseOrchestrator: base}

	sessionID := "direct-learning-test-session"
	defer common.ClearSessionShellConfig(sessionID)
	originalRead := []string{"Workflow/social-media/runs/iteration-0/execution"}
	originalWrite := []string{"Workflow/social-media/runs/iteration-0/execution/step-unfollow"}
	originalWorkingDir := "Workflow/social-media/runs/iteration-0/execution"
	common.SetSessionFolderGuard(sessionID, originalRead, originalWrite)
	common.SetSessionWorkingDir(sessionID, originalWorkingDir)

	cfg := &agents.OrchestratorAgentConfig{
		MCPSessionID:          sessionID,
		FolderGuardReadPaths:  append([]string{}, originalRead...),
		FolderGuardWritePaths: append([]string{}, originalWrite...),
	}
	agent := &fakeDirectLearningAgent{cfg: cfg}
	globalLearningsPath := "Workflow/social-media/learnings/_global"

	restore := hcpo.prepareDirectLearningTurn(agent, []string{globalLearningsPath})

	sessionCfg := common.GetSessionShellConfig(sessionID)
	if sessionCfg == nil {
		t.Fatal("expected session config to exist")
	}
	if sessionCfg.WorkingDir != originalWorkingDir {
		t.Fatalf("direct-learning changed cwd = %q, want unchanged %q", sessionCfg.WorkingDir, originalWorkingDir)
	}
	if !testContainsString(sessionCfg.ReadPaths, globalLearningsPath) || !testContainsString(sessionCfg.WritePaths, globalLearningsPath) {
		t.Fatalf("session guard missing global learnings path: read=%v write=%v", sessionCfg.ReadPaths, sessionCfg.WritePaths)
	}
	if !testContainsString(cfg.FolderGuardReadPaths, globalLearningsPath) || !testContainsString(cfg.FolderGuardWritePaths, globalLearningsPath) {
		t.Fatalf("agent guard missing global learnings path: read=%v write=%v", cfg.FolderGuardReadPaths, cfg.FolderGuardWritePaths)
	}

	restore()

	sessionCfg = common.GetSessionShellConfig(sessionID)
	if sessionCfg.WorkingDir != originalWorkingDir {
		t.Fatalf("restored cwd = %q, want %q", sessionCfg.WorkingDir, originalWorkingDir)
	}
	if strings.Join(sessionCfg.ReadPaths, "\x00") != strings.Join(originalRead, "\x00") {
		t.Fatalf("restored session read paths = %v, want %v", sessionCfg.ReadPaths, originalRead)
	}
	if strings.Join(sessionCfg.WritePaths, "\x00") != strings.Join(originalWrite, "\x00") {
		t.Fatalf("restored session write paths = %v, want %v", sessionCfg.WritePaths, originalWrite)
	}
	if strings.Join(cfg.FolderGuardReadPaths, "\x00") != strings.Join(originalRead, "\x00") {
		t.Fatalf("restored agent read paths = %v, want %v", cfg.FolderGuardReadPaths, originalRead)
	}
	if strings.Join(cfg.FolderGuardWritePaths, "\x00") != strings.Join(originalWrite, "\x00") {
		t.Fatalf("restored agent write paths = %v, want %v", cfg.FolderGuardWritePaths, originalWrite)
	}
}

func TestPrepareDirectLearningTurnCreatesTemporarySessionConfig(t *testing.T) {
	base, err := orchestrator.NewBaseOrchestrator(
		loggerv2.NewNoop(), nil, orchestrator.OrchestratorTypeWorkflow, "", 0, "",
		[]string{"test-server"}, nil, false, &orchestrator.LLMConfig{}, 1, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("NewBaseOrchestrator: %v", err)
	}
	base.SetWorkspacePath("Workflow/social-media")
	hcpo := &StepBasedWorkflowOrchestrator{BaseOrchestrator: base}

	sessionID := "direct-learning-new-session"
	common.ClearSessionShellConfig(sessionID)
	cfg := &agents.OrchestratorAgentConfig{MCPSessionID: sessionID}
	agent := &fakeDirectLearningAgent{cfg: cfg}
	globalLearningsPath := "Workflow/social-media/learnings/_global"

	restore := hcpo.prepareDirectLearningTurn(agent, []string{globalLearningsPath})
	sessionCfg := common.GetSessionShellConfig(sessionID)
	if sessionCfg == nil {
		t.Fatal("expected temporary session config to be created")
	}
	if sessionCfg.WorkingDir != "" {
		t.Fatalf("temporary cwd = %q, want empty/unchanged", sessionCfg.WorkingDir)
	}
	if !testContainsString(sessionCfg.WritePaths, globalLearningsPath) {
		t.Fatalf("temporary session write guard missing global learnings path: %v", sessionCfg.WritePaths)
	}

	restore()
	if got := common.GetSessionShellConfig(sessionID); got != nil {
		t.Fatalf("temporary session config was not cleared: %+v", got)
	}
}

func testContainsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
