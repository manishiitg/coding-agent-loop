package browser

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
)

func TestHandleAgentBrowserBrokersNamedScreenshot(t *testing.T) {
	var got ShellExecuteRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		stdout := `{"success":true}`
		if got.ArtifactTransfer != nil {
			stdout = `{"success":true,"path":"` + got.ArtifactTransfer.SourcePath + `"}`
		}
		_ = json.NewEncoder(w).Encode(APIResponse{Success: true, Data: ShellExecuteResponse{Stdout: stdout, ExitCode: 0}})
	}))
	defer server.Close()

	ctx := context.WithValue(context.Background(), common.ChatSessionIDKey, "screenshot-owner")
	ctx = context.WithValue(ctx, common.FolderGuardAllowedWriteFolderKey, []string{"Workflow/demo/evidence"})
	wantPath := "Workflow/demo/evidence/login.png"
	result, err := NewExecutor(NewClient(server.URL)).HandleAgentBrowser(ctx, map[string]interface{}{
		"command": "screenshot",
		"args":    []string{wantPath},
		"session": "screenshot-session",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ArtifactTransfer == nil || got.ArtifactTransfer.DestinationPath != wantPath || got.ArtifactTransfer.Kind != "screenshot" {
		t.Fatalf("screenshot transfer = %#v", got.ArtifactTransfer)
	}
	if strings.Contains(got.Command, wantPath) || !strings.Contains(got.Command, got.ArtifactTransfer.SourcePath) {
		t.Fatalf("browser command did not use only staged path: %q", got.Command)
	}
	if !strings.Contains(result, wantPath) || strings.Contains(result, got.ArtifactTransfer.SourcePath) {
		t.Fatalf("result did not expose requested destination: %q", result)
	}
}

func TestHandleAgentBrowserBrokersRecordingAcrossStartAndStop(t *testing.T) {
	var mu sync.Mutex
	var requests []ShellExecuteRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got ShellExecuteRequest
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		mu.Lock()
		requests = append(requests, got)
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(APIResponse{Success: true, Data: ShellExecuteResponse{Stdout: `{"success":true}`, ExitCode: 0}})
	}))
	defer server.Close()

	ctx := context.WithValue(context.Background(), common.ChatSessionIDKey, "recording-owner")
	ctx = context.WithValue(ctx, common.FolderGuardAllowedWriteFolderKey, []string{"Workflow/demo/evidence"})
	executor := NewExecutor(NewClient(server.URL))
	wantPath := "Workflow/demo/evidence/run.webm"
	if _, err := executor.HandleAgentBrowser(ctx, map[string]interface{}{
		"command": "record",
		"args":    []string{"start", wantPath},
		"session": "recording-session",
	}); err != nil {
		t.Fatalf("record start: %v", err)
	}
	if _, err := executor.HandleAgentBrowser(ctx, map[string]interface{}{
		"command": "record",
		"args":    []string{"stop"},
		"session": "recording-session",
	}); err != nil {
		t.Fatalf("record stop: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("request count = %d, want 2", len(requests))
	}
	start, stop := requests[0], requests[1]
	if start.ArtifactTransfer == nil || start.ArtifactTransfer.Finalize {
		t.Fatalf("record start did not carry a stage-only transfer: %#v", start.ArtifactTransfer)
	}
	if strings.Contains(start.Command, wantPath) {
		t.Fatalf("record start used workflow destination directly: %q", start.Command)
	}
	if stop.ArtifactTransfer == nil || !stop.ArtifactTransfer.Finalize || stop.ArtifactTransfer.DestinationPath != wantPath || stop.ArtifactTransfer.Kind != "video" {
		t.Fatalf("record stop transfer = %#v", stop.ArtifactTransfer)
	}
	if !strings.Contains(start.Command, stop.ArtifactTransfer.SourcePath) {
		t.Fatalf("recording lease source %q missing from start command %q", stop.ArtifactTransfer.SourcePath, start.Command)
	}
}

func TestHandleAgentBrowserBrokersUploadInputs(t *testing.T) {
	var got ShellExecuteRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(APIResponse{Success: true, Data: ShellExecuteResponse{Stdout: `{"success":true}`, ExitCode: 0}})
	}))
	defer server.Close()

	ctx := context.WithValue(context.Background(), common.ChatSessionIDKey, "upload-owner")
	ctx = context.WithValue(ctx, common.FolderGuardAllowedWriteFolderKey, []string{"Workflow/demo"})
	source := "Workflow/demo/input/report.pdf"
	_, err := NewExecutor(NewClient(server.URL)).HandleAgentBrowser(ctx, map[string]interface{}{
		"command": "upload",
		"args":    []string{"#file", source},
		"session": "upload-session",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.UploadTransfers) != 1 || got.UploadTransfers[0].SourcePath != source {
		t.Fatalf("upload transfers = %#v", got.UploadTransfers)
	}
	if strings.Contains(got.Command, source) || !strings.Contains(got.Command, got.UploadTransfers[0].StagedPath) {
		t.Fatalf("upload command did not use staged input: %q", got.Command)
	}
}

func TestHandleAgentBrowserBrokersExplicitDownload(t *testing.T) {
	var got ShellExecuteRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		stdout := `{"success":true}`
		if got.ArtifactTransfer != nil {
			stdout = `{"success":true,"path":"` + got.ArtifactTransfer.SourcePath + `"}`
		}
		_ = json.NewEncoder(w).Encode(APIResponse{Success: true, Data: ShellExecuteResponse{Stdout: stdout, ExitCode: 0}})
	}))
	defer server.Close()

	ctx := context.WithValue(context.Background(), common.ChatSessionIDKey, "download-owner")
	ctx = context.WithValue(ctx, common.FolderGuardAllowedWriteFolderKey, []string{"Workflow/demo/Downloads"})
	destination := "Workflow/demo/Downloads/report.csv"
	result, err := NewExecutor(NewClient(server.URL)).HandleAgentBrowser(ctx, map[string]interface{}{
		"command": "download",
		"args":    []string{"#export", destination},
		"session": "download-session",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ArtifactTransfer == nil || got.ArtifactTransfer.Kind != "download" || got.ArtifactTransfer.DestinationPath != destination {
		t.Fatalf("download transfer = %#v", got.ArtifactTransfer)
	}
	if strings.Contains(got.Command, destination) || !strings.Contains(got.Command, got.ArtifactTransfer.SourcePath) {
		t.Fatalf("download command did not use staged output: %q", got.Command)
	}
	if !strings.Contains(result, destination) || strings.Contains(result, got.ArtifactTransfer.SourcePath) {
		t.Fatalf("download result did not expose requested destination: %q", result)
	}
}
