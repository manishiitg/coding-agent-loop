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
