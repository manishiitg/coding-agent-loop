package virtualtools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBuildCLIToolEnvironmentPromptUsesProviderSpecificBridgeToolNames(t *testing.T) {
	tests := []struct {
		provider string
		want     string
		forbid   string
	}{
		{
			provider: "claude-code",
			want:     "mcp__api-bridge__execute_shell_command",
			forbid:   "mcp_api-bridge_execute_shell_command",
		},
		{
			provider: "codex-cli",
			want:     "mcp_api-bridge_execute_shell_command",
			forbid:   "mcp__api-bridge__execute_shell_command",
		},
		{
			provider: "gemini-cli",
			want:     "mcp_api-bridge_execute_shell_command",
			forbid:   "mcp__api-bridge__execute_shell_command",
		},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			got := BuildCLIToolEnvironmentPrompt(tt.provider)
			if !strings.Contains(got, tt.want) {
				t.Fatalf("prompt missing provider bridge tool %q:\n%s", tt.want, got)
			}
			if strings.Contains(got, tt.forbid) {
				t.Fatalf("prompt contains wrong provider bridge tool %q:\n%s", tt.forbid, got)
			}
		})
	}
}

func TestBuildCLIToolEnvironmentPromptIncludesLLMConfigCurlRouting(t *testing.T) {
	got := BuildCLIToolEnvironmentPrompt("claude-code")
	for _, want := range []string{
		"LLM config tools",
		"list_published_llms",
		"$MCP_CUSTOM/list_provider_models",
		"Do **NOT** read or edit `config/` files for LLM/provider configuration",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt missing %q:\n%s", want, got)
		}
	}
}

func TestHandleDelegatePrefersAsyncBackgroundDelegate(t *testing.T) {
	tests := []struct {
		name      string
		wrapValue func(BackgroundDelegateFunc) interface{}
	}{
		{
			name: "named background delegate func",
			wrapValue: func(fn BackgroundDelegateFunc) interface{} {
				return fn
			},
		},
		{
			name: "plain compatible function",
			wrapValue: func(fn BackgroundDelegateFunc) interface{} {
				return func(ctx context.Context, name, instruction string) (string, error) {
					return fn(ctx, name, instruction)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			started := make(chan struct{}, 1)
			executeCalled := make(chan struct{}, 1)

			bgDelegate := BackgroundDelegateFunc(func(_ context.Context, name, instruction string) (string, error) {
				if name != "bg-contract-check" {
					t.Fatalf("unexpected background agent name: %q", name)
				}
				if instruction != "sleep briefly" {
					t.Fatalf("unexpected instruction: %q", instruction)
				}
				started <- struct{}{}
				return "bg-agent-123", nil
			})

			ctx := context.WithValue(context.Background(), BackgroundDelegateKey, tt.wrapValue(bgDelegate))
			ctx = context.WithValue(ctx, ExecuteDelegatedTaskKey, ExecuteDelegatedTaskFunc(func(context.Context, string) (string, error) {
				executeCalled <- struct{}{}
				time.Sleep(time.Second)
				return "blocking result", nil
			}))

			result, err := handleDelegate(ctx, map[string]interface{}{
				"name":            "bg-contract-check",
				"instruction":     "sleep briefly",
				"reasoning_level": "low",
			})
			if err != nil {
				t.Fatalf("handleDelegate returned error: %v", err)
			}

			select {
			case <-started:
			default:
				t.Fatalf("background delegate was not invoked")
			}
			select {
			case <-executeCalled:
				t.Fatalf("blocking delegated task executor was called despite async delegate being available")
			default:
			}

			var parsed struct {
				Async   bool   `json:"async"`
				AgentID string `json:"agent_id"`
				Status  string `json:"status"`
			}
			if err := json.Unmarshal([]byte(result), &parsed); err != nil {
				t.Fatalf("result is not JSON: %v\n%s", err, result)
			}
			if !parsed.Async || parsed.AgentID != "bg-agent-123" || parsed.Status != "running" {
				t.Fatalf("unexpected async delegate result: %+v", parsed)
			}
		})
	}
}

// TestHandleDelegateBuildsChildSpec verifies that delegate(...) translates its
// tool args into the SubAgentSpec the sub-agent creation path consumes:
// explicit-pass skills, server restriction, browser isolation, tier, and the
// incremented depth. If this propagation breaks, sub-agents silently get the
// defaults with no diagnostic — this test catches it at the boundary.
func TestHandleDelegateBuildsChildSpec(t *testing.T) {
	var captured SubAgentSpec

	// The async background-delegate path captures the ctx the handler builds.
	bgDelegate := BackgroundDelegateFunc(func(ctx context.Context, name, instruction string) (string, error) {
		captured = SubAgentSpecFromContext(ctx)
		return "agent-xyz", nil
	})

	parentSpec := SubAgentSpec{Depth: 1, ShareBrowser: true}
	ctx := context.WithValue(context.Background(), BackgroundDelegateKey, bgDelegate)
	ctx = WithSubAgentSpec(ctx, parentSpec)

	_, err := handleDelegate(ctx, map[string]interface{}{
		"name":            "test-spec-pass",
		"instruction":     "do the thing",
		"reasoning_level": "low",
		"skills":          []interface{}{"pdf-extract", "agent-browser"},
		"servers":         []interface{}{"playwright"},
		"share_browser":   false,
	})
	if err != nil {
		t.Fatalf("handleDelegate returned error: %v", err)
	}

	if len(captured.Skills) != 2 || captured.Skills[0] != "pdf-extract" || captured.Skills[1] != "agent-browser" {
		t.Errorf("expected skills [pdf-extract agent-browser], got %v", captured.Skills)
	}
	if len(captured.Servers) != 1 || captured.Servers[0] != "playwright" {
		t.Errorf("expected servers [playwright], got %v", captured.Servers)
	}
	if captured.ShareBrowser {
		t.Error("share_browser=false should produce ShareBrowser=false in the child spec")
	}
	if captured.ReasoningLevel != "low" {
		t.Errorf("expected reasoning level low, got %q", captured.ReasoningLevel)
	}
	if captured.Depth != parentSpec.Depth+1 {
		t.Errorf("expected child depth %d, got %d", parentSpec.Depth+1, captured.Depth)
	}
}

// TestHandleDelegateNoSkillsArgMeansNoInheritance is the corollary:
// when the parent omits skills=[...], the child spec must carry no
// skills (so server.go's attach loop is a no-op and the sub-agent
// starts clean). Phase 6 explicit-pass.
func TestHandleDelegateNoSkillsArgMeansNoInheritance(t *testing.T) {
	var captured SubAgentSpec

	bgDelegate := BackgroundDelegateFunc(func(ctx context.Context, _, _ string) (string, error) {
		captured = SubAgentSpecFromContext(ctx)
		return "agent-xyz", nil
	})

	ctx := context.WithValue(context.Background(), BackgroundDelegateKey, bgDelegate)

	_, err := handleDelegate(ctx, map[string]interface{}{
		"name":            "no-skills",
		"instruction":     "do the thing",
		"reasoning_level": "low",
		// no "skills" key
	})
	if err != nil {
		t.Fatalf("handleDelegate returned error: %v", err)
	}

	if len(captured.Skills) != 0 {
		t.Errorf("expected no skills in child spec when args has no skills; got %v", captured.Skills)
	}
	if !captured.ShareBrowser {
		t.Error("ShareBrowser should default to true when share_browser arg is omitted")
	}
}

// TestHandleDelegateEnforcesMaxDepth verifies the recursion guard reads the
// depth from the SubAgentSpec.
func TestHandleDelegateEnforcesMaxDepth(t *testing.T) {
	bgDelegate := BackgroundDelegateFunc(func(context.Context, string, string) (string, error) {
		t.Fatal("delegate must not run at max depth")
		return "", nil
	})
	ctx := context.WithValue(context.Background(), BackgroundDelegateKey, bgDelegate)
	ctx = WithSubAgentSpec(ctx, SubAgentSpec{Depth: MaxDelegationDepth, ShareBrowser: true})

	_, err := handleDelegate(ctx, map[string]interface{}{
		"name":            "too-deep",
		"instruction":     "do the thing",
		"reasoning_level": "low",
	})
	if err == nil || !strings.Contains(err.Error(), "maximum delegation depth") {
		t.Fatalf("expected max-depth error, got: %v", err)
	}
}

// TestSubAgentSpecContextRoundTrip locks the spec accessor contract: defaults
// when absent (root depth, shared browser), exact round-trip when set, and
// WithBackgroundAgentID preserving other fields.
func TestSubAgentSpecContextRoundTrip(t *testing.T) {
	def := SubAgentSpecFromContext(context.Background())
	if def.Depth != 0 || !def.ShareBrowser || def.ReasoningLevel != "" || def.BackgroundAgentID != "" {
		t.Errorf("unexpected default spec: %+v", def)
	}

	want := SubAgentSpec{
		Depth:          2,
		ReasoningLevel: "high",
		AgentTemplate:  "researcher",
		Servers:        []string{"playwright"},
		Skills:         []string{"pdf-extract"},
		ShareBrowser:   false,
	}
	ctx := WithSubAgentSpec(context.Background(), want)
	got := SubAgentSpecFromContext(ctx)
	if got.Depth != want.Depth || got.ReasoningLevel != want.ReasoningLevel ||
		got.AgentTemplate != want.AgentTemplate || got.ShareBrowser != want.ShareBrowser ||
		len(got.Servers) != 1 || len(got.Skills) != 1 {
		t.Errorf("spec round-trip mismatch: got %+v, want %+v", got, want)
	}

	linked := SubAgentSpecFromContext(WithBackgroundAgentID(ctx, "bg-42"))
	if linked.BackgroundAgentID != "bg-42" {
		t.Errorf("expected background agent ID bg-42, got %q", linked.BackgroundAgentID)
	}
	if linked.ReasoningLevel != "high" || linked.Depth != 2 {
		t.Errorf("WithBackgroundAgentID must preserve other fields, got %+v", linked)
	}
}

// TestGetMultiAgentDelegationInstructionsLazyLoadsScheduleAndSecret locks in
// the prompt refactor that moved Schedule and Secret management deep docs
// into templates/system/{schedule-management,secret-management}.md. The
// inline prompt should keep brief cheat sheets + get_reference_doc pointers
// — not the old ~80-line JSON file format / detailed tool description
// blocks that every chat turn used to carry.
func TestGetMultiAgentDelegationInstructionsLazyLoadsScheduleAndSecret(t *testing.T) {
	out := GetMultiAgentDelegationInstructionsWithUser("Chats", "default")

	// Cheat-sheet headers must remain so the agent still knows the
	// capabilities exist without loading the deep docs.
	mustContain := []string{
		"## Schedule Management (brief)",
		"## Secret Management (brief)",
		// Brief Schedule cheat sheet keeps the file path + workflow.
		"multiagent-schedules.json",
		`mode: "multi-agent"`,
		// Brief Secret cheat sheet keeps the three buckets + safety rule.
		"workflow",
		"never echo / print / log a plaintext secret",
		// Pointers to the reference docs — agent needs these to know to
		// load the deep guide before scheduling / managing secrets.
		`get_reference_doc(kind="schedule-management")`,
		`get_reference_doc(kind="secret-management")`,
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("delegation prompt missing required cheat-sheet content: %q", s)
		}
	}

	// Content that moved to reference docs MUST NOT appear inline anymore.
	// Each marker below is a distinctive string from the old long-form
	// inline blocks. If any one of these slips back into the inline prompt,
	// the migration regressed.
	mustNotContain := []struct {
		marker string
		hint   string
	}{
		{
			marker: "### File Format",
			hint:   "JSON file-format block belongs in schedule-management.md",
		},
		{
			marker: "**Cron expression examples:**",
			hint:   "cron examples belong in schedule-management.md",
		},
		{
			marker: "first day of each month at midnight",
			hint:   "cron example detail belongs in schedule-management.md",
		},
		{
			marker: "### When users ask to schedule something",
			hint:   "step-by-step schedule flow belongs in schedule-management.md",
		},
		{
			marker: "### Tools",
			hint:   "secret-tools listing belongs in secret-management.md (cheat sheet lists tools inline as a sentence, not as a separate ### section)",
		},
		{
			marker: "delete_workflow_secret(name)",
			hint:   "per-tool detail belongs in secret-management.md",
		},
		{
			marker: "Globals cannot be deleted",
			hint:   "global-bucket detail belongs in secret-management.md",
		},
	}
	for _, c := range mustNotContain {
		if strings.Contains(out, c.marker) {
			t.Errorf("delegation prompt still inlines migrated content %q — %s", c.marker, c.hint)
		}
	}
}

func TestGetMultiAgentDelegationInstructionsAnchorsOrgPulseToLocalWorkspace(t *testing.T) {
	out := GetMultiAgentDelegationInstructionsWithUser("_users/default/Chats", "default")

	mustContain := []string{
		"Org goals live in the local workspace file `pulse/goals.html`",
		"Org Pulse lives in local `pulse/org-pulse.html`",
		"Never WebFetch raw GitHub URLs for these files or reference docs",
		"if local `pulse/goals.html` exists under the docs root",
		"Org-level goals live in local `pulse/goals.html`",
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Fatalf("delegation prompt missing local org artifact guardrail %q", s)
		}
	}
}

// TestGetMultiAgentDelegationInstructionsSize ensures the refactored prompt
// stays under a reasonable size ceiling. The pre-refactor version was
// ~10KB; the cheat-sheet rewrite targets ~8KB or less. The ceiling here
// allows small natural growth but trips if a big content block is added
// back without a corresponding lazy-load migration.
func TestGetMultiAgentDelegationInstructionsSize(t *testing.T) {
	out := GetMultiAgentDelegationInstructionsWithUser("Chats", "default")
	size := len(out)
	estTokens := size / 4

	// Always log so CI shows the trend.
	t.Logf("GetMultiAgentDelegationInstructionsWithUser: %d bytes (~%d tokens)", size, estTokens)

	const maxBytes = 9_000 // ~2.25k tokens; current is ~7.8k bytes.
	const minBytes = 4_000 // floor catches accidental gutting.

	if size > maxBytes {
		t.Errorf("delegation prompt %d bytes exceeds ceiling %d (~%d tokens) — move new content to templates/system/*.md and reference via get_reference_doc",
			size, maxBytes, estTokens)
	}
	if size < minBytes {
		t.Errorf("delegation prompt %d bytes below floor %d — orchestrator / capabilities content likely missing",
			size, minBytes)
	}
}
