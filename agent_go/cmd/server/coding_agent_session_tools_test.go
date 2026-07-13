package server

import (
	"context"
	"errors"
	"strings"
	"testing"

	virtualtools "github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/virtual-tools"
)

// recordingRegister captures RegisterCustomTool calls so tests can assert what
// the shared registrar would register without constructing a real agent.
type recordingRegister struct {
	categories map[string]string // toolName -> category
	descs      map[string]string // toolName -> description
}

func newRecordingRegister() *recordingRegister {
	return &recordingRegister{categories: map[string]string{}, descs: map[string]string{}}
}

func (r *recordingRegister) fn(name, description string, _ map[string]interface{}, _ func(ctx context.Context, args map[string]interface{}) (string, error), category string) error {
	r.categories[name] = category
	r.descs[name] = description
	return nil
}

// Both the fresh query handler and the restore path now register browser tools
// through registerCodingBrowserTools → registerCodingToolGroup. This test pins
// that shared mechanism: every browser tool with an executor is registered under
// the browser category. If the browser tool set changes, both paths change
// together (they share this code) and this test still passes — which is the
// drift guard.
func TestRegisterCodingToolGroup_RegistersBrowserToolSet(t *testing.T) {
	tools := virtualtools.CreateWorkspaceBrowserTools()
	if len(tools) == 0 {
		t.Fatal("expected browser tools from CreateWorkspaceBrowserTools")
	}
	execs := virtualtools.CreateWorkspaceBrowserToolExecutorsWithSession("test-session", 9222)
	category := virtualtools.GetWorkspaceBrowserToolCategory()

	rec := newRecordingRegister()
	if err := registerCodingToolGroup(rec.fn, tools, execs, func(string) string { return category }, nil); err != nil {
		t.Fatalf("registerCodingToolGroup returned error: %v", err)
	}

	registeredCount := 0
	for _, tool := range tools {
		if tool.Function == nil {
			continue
		}
		if _, hasExec := execs[tool.Function.Name]; !hasExec {
			continue
		}
		registeredCount++
		if got := rec.categories[tool.Function.Name]; got != category {
			t.Errorf("tool %q registered under category %q, want %q", tool.Function.Name, got, category)
		}
	}
	if registeredCount == 0 {
		t.Fatal("no browser tools were registered")
	}
}

// An empty category is a programming error — the registrar must surface it rather
// than register an uncategorized tool (the fresh path historically treated this
// as fatal).
func TestRegisterCodingToolGroup_EmptyCategoryErrors(t *testing.T) {
	tools := virtualtools.CreateWorkspaceBrowserTools()
	execs := virtualtools.CreateWorkspaceBrowserToolExecutorsWithSession("test-session", 9222)
	rec := newRecordingRegister()
	err := registerCodingToolGroup(rec.fn, tools, execs, func(string) string { return "" }, nil)
	if err == nil {
		t.Fatal("expected an error when categoryFor returns empty")
	}
}

// Tools without a matching executor are skipped (not registered, no error).
func TestRegisterCodingToolGroup_SkipsToolsWithoutExecutor(t *testing.T) {
	tools := virtualtools.CreateWorkspaceBrowserTools()
	rec := newRecordingRegister()
	if err := registerCodingToolGroup(rec.fn, tools, codingAgentToolExecutors{}, func(string) string { return "x" }, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rec.categories) != 0 {
		t.Errorf("expected no registrations with empty executor map, got %d", len(rec.categories))
	}
}

// The decorator hook (used by the fresh path for description enhancement /
// image-gen wrapping) must be applied to the registered description.
func TestRegisterCodingToolGroup_AppliesDecorator(t *testing.T) {
	tools := virtualtools.CreateWorkspaceBrowserTools()
	execs := virtualtools.CreateWorkspaceBrowserToolExecutorsWithSession("test-session", 9222)
	rec := newRecordingRegister()
	decorate := func(_ string, description string, exec func(ctx context.Context, args map[string]interface{}) (string, error)) (string, func(ctx context.Context, args map[string]interface{}) (string, error)) {
		return "[DECORATED] " + description, exec
	}
	if err := registerCodingToolGroup(rec.fn, tools, execs, func(string) string { return "x" }, decorate); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for name, desc := range rec.descs {
		if !strings.HasPrefix(desc, "[DECORATED] ") {
			t.Errorf("tool %q description not decorated: %q", name, desc)
		}
	}
}

// A nil register func is a no-op (defensive).
func TestRegisterCodingToolGroup_NilRegisterIsNoop(t *testing.T) {
	if err := registerCodingToolGroup(nil, virtualtools.CreateWorkspaceBrowserTools(), codingAgentToolExecutors{}, func(string) string { return "x" }, nil); err != nil {
		t.Fatalf("expected nil register to be a no-op, got %v", err)
	}
}

// Guards against accidentally swallowing register errors.
func TestRegisterCodingToolGroup_PropagatesRegisterError(t *testing.T) {
	tools := virtualtools.CreateWorkspaceBrowserTools()
	execs := virtualtools.CreateWorkspaceBrowserToolExecutorsWithSession("test-session", 9222)
	sentinel := errors.New("boom")
	failing := func(string, string, map[string]interface{}, func(ctx context.Context, args map[string]interface{}) (string, error), string) error {
		return sentinel
	}
	err := registerCodingToolGroup(failing, tools, execs, func(string) string { return "x" }, nil)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected wrapped sentinel error, got %v", err)
	}
}
