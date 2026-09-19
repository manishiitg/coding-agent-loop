package costobserver

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func TestNewWarnsWhenLaunchPathCannotNameItsScope(t *testing.T) {
	var logged bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logged)
	defer log.SetOutput(previous)

	observer := New(nil, "sess-1", "user-1", "simple",
		WithAttribution("", "Workflow/demo", "", "exec-1"),
		WithLaunchPath("somePackage.someLaunchPath"),
	)

	if observer.Scope() != ScopeUnknown {
		t.Fatalf("Scope() = %q, want %q", observer.Scope(), ScopeUnknown)
	}
	output := logged.String()
	if !strings.Contains(output, "somePackage.someLaunchPath") {
		t.Fatalf("an unattributed observer must name its launch path in the log, got: %q", output)
	}
	if !strings.Contains(output, "did not name a cost scope") {
		t.Fatalf("missing unattributed-scope warning, got: %q", output)
	}
}

func TestNewDoesNotWarnForAnAttributedObserver(t *testing.T) {
	var logged bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logged)
	defer log.SetOutput(previous)

	observer := New(nil, "sess-1", "user-1", "simple",
		WithAttribution(ScopePulse, "Workflow/demo", "", "pulse-review-1"),
		WithLaunchPath("somePackage.someLaunchPath"),
	)
	if observer.Scope() != ScopePulse {
		t.Fatalf("Scope() = %q, want %q", observer.Scope(), ScopePulse)
	}
	if observer.ExecutionID() != "pulse-review-1" {
		t.Fatalf("ExecutionID() = %q, want %q", observer.ExecutionID(), "pulse-review-1")
	}
	if logged.Len() != 0 {
		t.Fatalf("attributed observer logged %q", logged.String())
	}
}

func TestInferScope(t *testing.T) {
	tests := []struct {
		agentMode string
		phaseID   string
		want      string
	}{
		{"chat", "", ScopeChat},
		{"simple", "", ScopeChat},
		{"chat", "post_run_monitor", ScopePulse},
		{"chat", "pulse-fixer", ScopePulse},
		{"multi-agent", "", ScopeChat},
		{"workflow", "", ScopeBuilder},
	}
	for _, tc := range tests {
		if got := InferScope(tc.agentMode, tc.phaseID); got != tc.want {
			t.Errorf("InferScope(%q, %q) = %q, want %q", tc.agentMode, tc.phaseID, got, tc.want)
		}
	}
}

func TestInferWorkflowScope(t *testing.T) {
	tests := []struct {
		name         string
		agentMode    string
		hasRunFolder bool
		identifiers  []string
		want         string
	}{
		{
			name:        "pulse reviewer stage",
			agentMode:   "simple",
			identifiers: []string{"pulse-reviewer-stores-health-1785", "Background: Pulse reviewer - stores-health", ""},
			want:        ScopePulse,
		},
		{
			name:         "live workflow step",
			agentMode:    "simple",
			hasRunFolder: true,
			identifiers:  []string{"execution", "execution-agent-step-1", "iteration-0/default"},
			want:         ScopeWorkflowExecution,
		},
		{
			name:        "builder-side background stage",
			agentMode:   "simple",
			identifiers: []string{"generic-agent-refresh-docs-1785", "Background: Generic agent - refresh-docs", ""},
			want:        ScopeBuilder,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := InferWorkflowScope(tc.agentMode, tc.hasRunFolder, tc.identifiers...)
			if got != tc.want {
				t.Fatalf("InferWorkflowScope() = %q, want %q", got, tc.want)
			}
			if got == ScopeUnknown {
				t.Fatalf("InferWorkflowScope() must always name a scope")
			}
		})
	}
}
