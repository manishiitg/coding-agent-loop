package step_based_workflow

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
)

func TestCreateRunFolderStructureDoesNotCreateGitignore(t *testing.T) {
	var (
		mu             sync.Mutex
		createdFolders []string
		writtenFiles   []string
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/folders":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			var req struct {
				FolderPath string `json:"folder_path"`
			}
			if err := json.Unmarshal(body, &req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			mu.Lock()
			createdFolders = append(createdFolders, req.FolderPath)
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodPut && r.URL.Path != "":
			mu.Lock()
			writtenFiles = append(writtenFiles, r.URL.Path)
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("WORKSPACE_API_URL", server.URL)

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
		&orchestrator.LLMConfig{},
		1,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewBaseOrchestrator returned error: %v", err)
	}
	base.SetWorkspacePath("Workflow/test")

	hcpo := &StepBasedWorkflowOrchestrator{
		BaseOrchestrator: base,
	}

	runPath := "Workflow/test/runs/iteration-1"
	if err := hcpo.createRunFolderStructure(context.Background(), runPath); err != nil {
		t.Fatalf("createRunFolderStructure returned error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	expectedFolders := []string{
		runPath,
		runPath + "/execution",
		runPath + "/execution/Downloads",
	}
	if len(createdFolders) != len(expectedFolders) {
		t.Fatalf("expected %d folder creation requests, got %d (%v)", len(expectedFolders), len(createdFolders), createdFolders)
	}
	for i, expected := range expectedFolders {
		if createdFolders[i] != expected {
			t.Fatalf("expected folder %q at index %d, got %q", expected, i, createdFolders[i])
		}
	}
	if len(writtenFiles) != 0 {
		t.Fatalf("expected no file writes while creating run folder, got %v", writtenFiles)
	}
}
