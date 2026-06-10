package server

import (
	"os"
	"strconv"
	"strings"
	"time"

	agent "mcp-agent-builder-go/agent_go/pkg/agentwrapper"
	"mcp-agent-builder-go/agent_go/pkg/workflowtypes"
)

// applySharedLLMAgentTuning fills the LLMAgentConfig fields whose resolution
// must be identical for the root chat agent (handleQuery) and delegated
// sub-agents (executeDelegatedTask): tool timeout, context summarization,
// context editing, large-output offloading, and parallel tool execution.
//
// Priority for each field: request > preset (when non-nil) > environment >
// default. Sub-agents pass preset=nil — they inherit from the parent request
// and environment only.
//
// This is the single place to change these defaults. Before this helper the
// same ~200 lines of closures were duplicated in both config literals and had
// already started to drift.
func applySharedLLMAgentTuning(cfg *agent.LLMAgentConfig, req *QueryRequest, preset *workflowtypes.PresetLLMConfig) {
	cfg.ToolTimeout = resolveToolTimeout()

	// Context summarization
	cfg.EnableContextSummarization = func() bool {
		if req.EnableContextSummarization != nil {
			return *req.EnableContextSummarization
		}
		if preset != nil && preset.EnableContextSummarization != nil {
			return *preset.EnableContextSummarization
		}
		return os.Getenv("ENABLE_CONTEXT_SUMMARIZATION") != "false"
	}()
	cfg.SummarizeOnTokenThreshold = func() bool {
		if req.SummarizeOnTokenThreshold != nil {
			return *req.SummarizeOnTokenThreshold
		}
		return os.Getenv("SUMMARIZE_ON_TOKEN_THRESHOLD") != "false"
	}()
	cfg.TokenThresholdPercent = func() float64 {
		if req.TokenThresholdPercent > 0 {
			return req.TokenThresholdPercent
		}
		if envVal := os.Getenv("TOKEN_THRESHOLD_PERCENT"); envVal != "" {
			if threshold, err := strconv.ParseFloat(envVal, 64); err == nil && threshold > 0 && threshold <= 1.0 {
				return threshold
			}
		}
		return 0.8
	}()
	cfg.SummarizeOnFixedTokenThreshold = func() bool {
		if req.SummarizeOnFixedTokenThreshold != nil {
			return *req.SummarizeOnFixedTokenThreshold
		}
		return os.Getenv("SUMMARIZE_ON_FIXED_TOKEN_THRESHOLD") != "false"
	}()
	cfg.FixedTokenThreshold = func() int {
		if req.FixedTokenThreshold > 0 {
			return req.FixedTokenThreshold
		}
		return envPositiveInt("FIXED_TOKEN_THRESHOLD", 200000)
	}()
	cfg.SummaryKeepLastMessages = func() int {
		if req.SummaryKeepLastMessages > 0 {
			return req.SummaryKeepLastMessages
		}
		return envPositiveInt("SUMMARY_KEEP_LAST_MESSAGES", 4)
	}()

	// Context editing
	cfg.EnableContextEditing = func() bool {
		if req.EnableContextEditing != nil {
			return *req.EnableContextEditing
		}
		if preset != nil && preset.EnableContextEditing != nil {
			return *preset.EnableContextEditing
		}
		return os.Getenv("ENABLE_CONTEXT_EDITING") == "true"
	}()
	cfg.ContextEditingThreshold = func() int {
		if req.ContextEditingThreshold > 0 {
			return req.ContextEditingThreshold
		}
		return envPositiveInt("CONTEXT_EDITING_THRESHOLD", 0) // 0 = library default (100)
	}()
	cfg.ContextEditingTurnThreshold = func() int {
		if req.ContextEditingTurnThreshold > 0 {
			return req.ContextEditingTurnThreshold
		}
		return envPositiveInt("CONTEXT_EDITING_TURN_THRESHOLD", 0) // 0 = library default (5)
	}()

	// Context offloading: tool outputs larger than this (tokens) go to the filesystem.
	cfg.LargeOutputThreshold = envPositiveInt("LARGE_OUTPUT_THRESHOLD", 0) // 0 = library default (10000)

	cfg.EnableParallelToolExecution = os.Getenv("ENABLE_PARALLEL_TOOL_EXECUTION") != "false"
}

// resolveToolTimeout reads TOOL_EXECUTION_TIMEOUT; 0 means no per-tool timeout.
func resolveToolTimeout() time.Duration {
	if envVal := os.Getenv("TOOL_EXECUTION_TIMEOUT"); envVal != "" {
		if timeout, err := time.ParseDuration(envVal); err == nil {
			return timeout
		}
	}
	return 0
}

// envPositiveInt returns the env var as an int when it parses positive,
// otherwise the fallback.
func envPositiveInt(name string, fallback int) int {
	if envVal := os.Getenv(name); envVal != "" {
		if v, err := strconv.Atoi(envVal); err == nil && v > 0 {
			return v
		}
	}
	return fallback
}

// buildSecretNamesPrompt renders the system-prompt section listing available
// secret names (never values — those live in env vars for execute_shell_command).
// Shared by the root chat agent and delegated sub-agents so both describe
// secrets to the LLM identically. Returns "" when there are no secrets.
func buildSecretNamesPrompt(secrets []struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}) string {
	if len(secrets) == 0 {
		return ""
	}
	var secretNames []string
	for _, s := range secrets {
		secretNames = append(secretNames, "- `SECRET_"+s.Name+"` → accessible as `os.environ[\"SECRET_"+s.Name+"\"]` in Python or `$SECRET_"+s.Name+"` in bash")
	}
	return "\n## Secrets\n\nThe following secrets are available as environment variables in execute_shell_command. Do NOT ask the user for these values — read them from the environment.\n\n" + strings.Join(secretNames, "\n")
}
