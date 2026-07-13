package guidance

import (
	"context"
	"strings"
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
)

// --- DocReadTracker behavior ---

func TestDocReadTracker_FreshSessionHasNoLoads(t *testing.T) {
	tracker := NewDocReadTracker()

	if tracker.HasLoaded("sess-1", "code-authoring") {
		t.Errorf("fresh session should report no loads")
	}
	missing := tracker.MissingFor("sess-1", []string{"code-authoring", "stores"})
	if len(missing) != 2 {
		t.Errorf("expected both kinds missing, got %v", missing)
	}
}

func TestDocReadTracker_MarkLoadedAndCheck(t *testing.T) {
	tracker := NewDocReadTracker()

	tracker.MarkLoaded("sess-1", "code-authoring")

	if !tracker.HasLoaded("sess-1", "code-authoring") {
		t.Errorf("after MarkLoaded, HasLoaded should return true")
	}
	if tracker.HasLoaded("sess-1", "stores") {
		t.Errorf("HasLoaded for unloaded kind should return false")
	}
}

func TestDocReadTracker_SessionsAreIsolated(t *testing.T) {
	tracker := NewDocReadTracker()

	tracker.MarkLoaded("sess-a", "code-authoring")

	if !tracker.HasLoaded("sess-a", "code-authoring") {
		t.Errorf("session a should see its own load")
	}
	if tracker.HasLoaded("sess-b", "code-authoring") {
		t.Errorf("session b should not see session a's load")
	}
}

func TestDocReadTracker_EmptySessionIsNoOp(t *testing.T) {
	tracker := NewDocReadTracker()

	// MarkLoaded with empty sessionID should be a no-op, not panic.
	tracker.MarkLoaded("", "code-authoring")
	if tracker.HasLoaded("", "code-authoring") {
		t.Errorf("HasLoaded with empty sessionID should return false (no key to track)")
	}
}

func TestDocReadTracker_MissingForReportsExactGap(t *testing.T) {
	tracker := NewDocReadTracker()
	tracker.MarkLoaded("sess-1", "code-authoring")
	tracker.MarkLoaded("sess-1", "stores")

	missing := tracker.MissingFor("sess-1", []string{"code-authoring", "stores", "optimize-playbook"})
	if len(missing) != 1 || missing[0] != "optimize-playbook" {
		t.Errorf("expected only optimize-playbook missing, got %v", missing)
	}

	missing = tracker.MissingFor("sess-1", []string{"code-authoring"})
	if len(missing) != 0 {
		t.Errorf("expected nothing missing when all kinds loaded, got %v", missing)
	}
}

// --- WithDocPrecondition gate behavior ---

func TestWithDocPrecondition_RejectsWhenDocNotLoaded(t *testing.T) {
	tracker := NewDocReadTracker()
	called := false
	inner := func(ctx context.Context, args map[string]interface{}) (string, error) {
		called = true
		return "inner-result", nil
	}

	wrapped := WithDocPrecondition([]string{"optimize-playbook"}, tracker, inner)

	ctx := context.WithValue(context.Background(), common.ChatSessionIDKey, "sess-1")
	out, err := wrapped(ctx, nil)

	if err != nil {
		t.Fatalf("wrapper should never return an error (always teaching-string), got %v", err)
	}
	if called {
		t.Errorf("inner handler must NOT be called when precondition fails")
	}
	if !strings.Contains(out, "precondition_not_met") {
		t.Errorf("output should contain precondition_not_met marker, got: %s", out)
	}
	if !strings.Contains(out, `get_reference_doc(kind="optimize-playbook")`) {
		t.Errorf("output should suggest the exact load call, got: %s", out)
	}
	if !strings.Contains(out, `"required_kinds": ["optimize-playbook"]`) {
		t.Errorf("output should include required_kinds JSON field, got: %s", out)
	}
}

func TestWithDocPrecondition_AllowsWhenDocLoaded(t *testing.T) {
	tracker := NewDocReadTracker()
	tracker.MarkLoaded("sess-1", "optimize-playbook")

	called := false
	inner := func(ctx context.Context, args map[string]interface{}) (string, error) {
		called = true
		return "inner-result", nil
	}
	wrapped := WithDocPrecondition([]string{"optimize-playbook"}, tracker, inner)

	ctx := context.WithValue(context.Background(), common.ChatSessionIDKey, "sess-1")
	out, err := wrapped(ctx, nil)

	if err != nil {
		t.Fatalf("inner handler errored: %v", err)
	}
	if !called {
		t.Errorf("inner handler should be called when precondition met")
	}
	if out != "inner-result" {
		t.Errorf("output should be inner's return, got: %s", out)
	}
}

func TestWithDocPrecondition_ListsAllMissingKinds(t *testing.T) {
	tracker := NewDocReadTracker()
	tracker.MarkLoaded("sess-1", "code-authoring") // partial: only one of three loaded

	inner := func(ctx context.Context, args map[string]interface{}) (string, error) {
		return "ok", nil
	}
	wrapped := WithDocPrecondition(
		[]string{"code-authoring", "optimize-playbook", "stores"},
		tracker,
		inner,
	)

	ctx := context.WithValue(context.Background(), common.ChatSessionIDKey, "sess-1")
	out, _ := wrapped(ctx, nil)

	// Both missing kinds should appear in the suggested next calls.
	if !strings.Contains(out, `get_reference_doc(kind="optimize-playbook")`) {
		t.Errorf("expected optimize-playbook in suggestion: %s", out)
	}
	if !strings.Contains(out, `get_reference_doc(kind="stores")`) {
		t.Errorf("expected stores in suggestion: %s", out)
	}
	// The already-loaded one should NOT appear in the missing list.
	if strings.Contains(out, `"required_kinds": [.*code-authoring`) {
		t.Errorf("already-loaded kind should not appear in required_kinds: %s", out)
	}
}

func TestWithDocPrecondition_NoSessionIDPasses(t *testing.T) {
	// Production paths stamp ChatSessionIDKey. Test/internal paths may not.
	// In that case the wrapper should NOT block (defensive default).
	tracker := NewDocReadTracker()

	called := false
	inner := func(ctx context.Context, args map[string]interface{}) (string, error) {
		called = true
		return "ok", nil
	}
	wrapped := WithDocPrecondition([]string{"optimize-playbook"}, tracker, inner)

	// No ChatSessionIDKey on ctx.
	out, err := wrapped(context.Background(), nil)

	if err != nil {
		t.Fatalf("wrapper errored: %v", err)
	}
	if !called {
		t.Errorf("with no session ID, inner should be called (do not block)")
	}
	if out != "ok" {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestWithDocPrecondition_NilTrackerUsesDefault(t *testing.T) {
	// Passing nil tracker should fall back to DefaultTracker, not panic.
	inner := func(ctx context.Context, args map[string]interface{}) (string, error) {
		return "ok", nil
	}
	// Use a unique kind so it doesn't accidentally match prior test state in
	// the default tracker if other tests have loaded common kinds.
	wrapped := WithDocPrecondition([]string{"_unit-test-kind-that-noone-loads_"}, nil, inner)

	ctx := context.WithValue(context.Background(), common.ChatSessionIDKey, "sess-defaulttracker-test")
	out, _ := wrapped(ctx, nil)
	if !strings.Contains(out, "precondition_not_met") {
		t.Errorf("default tracker should still gate (kind never loaded in this session)")
	}
}

func TestWithDocPrecondition_EmptyRequiredIsPassthrough(t *testing.T) {
	tracker := NewDocReadTracker()
	called := false
	inner := func(ctx context.Context, args map[string]interface{}) (string, error) {
		called = true
		return "ok", nil
	}
	wrapped := WithDocPrecondition(nil, tracker, inner)

	ctx := context.WithValue(context.Background(), common.ChatSessionIDKey, "sess-1")
	_, _ = wrapped(ctx, nil)
	if !called {
		t.Errorf("empty requiredKinds should be a passthrough (no gate)")
	}
}

func TestSessionIDFromContext(t *testing.T) {
	if got := SessionIDFromContext(nil); got != "" {
		t.Errorf("nil ctx should return empty, got %q", got)
	}
	if got := SessionIDFromContext(context.Background()); got != "" {
		t.Errorf("ctx without key should return empty, got %q", got)
	}
	ctx := context.WithValue(context.Background(), common.ChatSessionIDKey, "abc-123")
	if got := SessionIDFromContext(ctx); got != "abc-123" {
		t.Errorf("expected abc-123, got %q", got)
	}
}
