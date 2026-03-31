package step_based_workflow

import (
	"strings"
	"testing"

	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
)

func TestSwitchWorkshopGroupSessionCachesPerGroup(t *testing.T) {
	t.Setenv("MCP_API_URL", "http://example.test")

	base, err := orchestrator.NewBaseOrchestrator(
		loggerv2.NewDefault(),
		nil,
		orchestrator.OrchestratorTypeWorkflow,
		"",
		0,
		"",
		nil,
		nil,
		false,
		false,
		nil,
		nil,
		1,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewBaseOrchestrator returned error: %v", err)
	}

	envRef := map[string]string{
		"MCP_API_TOKEN":  "test-token",
		"MCP_API_URL":    "http://example.test/s/original-session",
		"MCP_SESSION_ID": "original-session",
	}
	base.SetWorkspaceEnvRef(envRef)

	hcpo := &StepBasedWorkflowOrchestrator{
		BaseOrchestrator:        base,
		workshopGroupSessionIDs: make(map[string]string),
	}

	if err := hcpo.switchWorkshopGroupSession("group-1"); err != nil {
		t.Fatalf("switchWorkshopGroupSession(group-1) returned error: %v", err)
	}
	group1Session := hcpo.GetMCPSessionID()
	if !strings.Contains(group1Session, "session-group-group-1-") {
		t.Fatalf("expected group-1 session id, got %q", group1Session)
	}
	if envRef["MCP_SESSION_ID"] != group1Session {
		t.Fatalf("expected workspace env MCP_SESSION_ID %q, got %q", group1Session, envRef["MCP_SESSION_ID"])
	}

	if err := hcpo.switchWorkshopGroupSession("group-1"); err != nil {
		t.Fatalf("switchWorkshopGroupSession(group-1) second call returned error: %v", err)
	}
	if hcpo.GetMCPSessionID() != group1Session {
		t.Fatalf("expected group-1 session to be reused, got %q want %q", hcpo.GetMCPSessionID(), group1Session)
	}

	if err := hcpo.switchWorkshopGroupSession("group-2"); err != nil {
		t.Fatalf("switchWorkshopGroupSession(group-2) returned error: %v", err)
	}
	group2Session := hcpo.GetMCPSessionID()
	if !strings.Contains(group2Session, "session-group-group-2-") {
		t.Fatalf("expected group-2 session id, got %q", group2Session)
	}
	if group2Session == group1Session {
		t.Fatalf("expected different session ids for different groups, both were %q", group2Session)
	}

	if len(hcpo.workshopGroupSessionIDs) != 2 {
		t.Fatalf("expected 2 cached group sessions, got %d", len(hcpo.workshopGroupSessionIDs))
	}
}
