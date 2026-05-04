package step_based_workflow

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
)

func TestValidateFilePath(t *testing.T) {
	stepExecutionPath := "Workflow/linkedin/runs/iteration-0/default/execution/step-global-research"

	testCases := []struct {
		name      string
		fileName  string
		want      string
		wantError bool
	}{
		{
			name:     "step local bare file",
			fileName: "auth_context.json",
			want:     "Workflow/linkedin/runs/iteration-0/default/execution/step-global-research/auth_context.json",
		},
		{
			name:     "workflow knowledgebase path",
			fileName: "knowledgebase/research/global_trends.json",
			want:     "Workflow/linkedin/knowledgebase/research/global_trends.json",
		},
		{
			name:     "already workflow scoped relative path",
			fileName: "Workflow/linkedin/knowledgebase/research/global_trends.json",
			want:     "Workflow/linkedin/knowledgebase/research/global_trends.json",
		},
		{
			name:     "absolute path inside current workflow",
			fileName: "/app/workspace-docs/Workflow/linkedin/knowledgebase/research/global_trends.json",
			want:     "Workflow/linkedin/knowledgebase/research/global_trends.json",
		},
		{
			name:      "absolute path outside current workflow",
			fileName:  "/app/workspace-docs/Workflow/social-media/knowledgebase/research/global_trends.json",
			wantError: true,
		},
		{
			name:      "relative path outside current workflow",
			fileName:  "Workflow/social-media/knowledgebase/research/global_trends.json",
			wantError: true,
		},
		{
			name:      "path traversal rejected",
			fileName:  "../knowledgebase/research/global_trends.json",
			wantError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := validateFilePath(stepExecutionPath, tc.fileName)
			if tc.wantError {
				if err == nil {
					t.Fatalf("expected error, got path %q", got)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestDeriveAlternateValidationPath(t *testing.T) {
	stepExecutionPath := "Workflow/linkedin/runs/iteration-0/default/execution/step-global-hn"

	testCases := []struct {
		name         string
		fileName     string
		resolvedPath string
		want         string
	}{
		{
			name:         "workflow root file gets step local alternate",
			fileName:     "knowledgebase/research/current/hn_raw.json",
			resolvedPath: "Workflow/linkedin/knowledgebase/research/current/hn_raw.json",
			want:         "Workflow/linkedin/runs/iteration-0/default/execution/step-global-hn/knowledgebase/research/current/hn_raw.json",
		},
		{
			name:         "step local file gets workflow root alternate",
			fileName:     "knowledgebase/research/current/hn_raw.json",
			resolvedPath: "Workflow/linkedin/runs/iteration-0/default/execution/step-global-hn/knowledgebase/research/current/hn_raw.json",
			want:         "Workflow/linkedin/knowledgebase/research/current/hn_raw.json",
		},
		{
			name:         "bare step file gets workflow root alternate",
			fileName:     "auth_context.json",
			resolvedPath: "Workflow/linkedin/runs/iteration-0/default/execution/step-global-hn/auth_context.json",
			want:         "Workflow/linkedin/auth_context.json",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := deriveAlternateValidationPath(stepExecutionPath, tc.fileName, tc.resolvedPath)
			if got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestBuildValidationPathHint(t *testing.T) {
	got := buildValidationPathHint(
		"Workflow/linkedin/knowledgebase/research/current/hn_raw.json",
		"Workflow/linkedin/runs/iteration-0/default/execution/step-global-hn/knowledgebase/research/current/hn_raw.json",
		true,
	)
	if got == "" || !strings.Contains(got, "Another copy also exists") || !strings.Contains(got, "validation read the other") {
		t.Fatalf("unexpected hint: %q", got)
	}
}

func TestRunPreValidationAllowsBinaryFileWhenOnlyMustExist(t *testing.T) {
	const stepPath = "Workflow/instagram/runs/iteration-0/test-run/execution/route-generate-voiceover"
	const voiceoverPath = stepPath + "/voiceover.mp3"

	var binaryReads int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/documents/"+voiceoverPath {
			atomic.AddInt32(&binaryReads, 1)
			fmt.Fprint(w, `{"success":true,"message":"Document retrieved successfully","data":{"filepath":"`+voiceoverPath+`","content":"","is_binary":true,"size":3,"mime_type":"audio/mpeg"}}`)
			return
		}
		fmt.Fprint(w, `{"success":true,"message":"File does not exist","data":{},"error":"File not found"}`)
	}))
	defer server.Close()

	t.Setenv("WORKSPACE_API_URL", server.URL)
	base, err := orchestrator.NewBaseOrchestrator(
		loggerv2.NewNoop(),
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

	result, err := RunPreValidation(context.Background(), &ValidationSchema{
		Files: []FileValidationRule{{
			FileName:  "voiceover.mp3",
			MustExist: true,
		}},
	}, stepPath, base)
	if err != nil {
		t.Fatalf("RunPreValidation returned error: %v", err)
	}
	if !result.OverallPass {
		t.Fatalf("expected validation to pass, got errors: %#v", result.Summary.Errors)
	}
	if result.Summary.TotalChecks != 1 || result.Summary.PassedChecks != 1 || result.Summary.FailedChecks != 0 {
		t.Fatalf("unexpected summary: %#v", result.Summary)
	}
	if got := atomic.LoadInt32(&binaryReads); got != 1 {
		t.Fatalf("expected exactly one binary metadata read for existence, got %d", got)
	}
	if len(result.FilesChecked) != 1 || len(result.FilesChecked[0].JSONChecks) != 0 {
		t.Fatalf("binary must-exist validation should not add JSON/text read checks: %#v", result.FilesChecked)
	}
}
