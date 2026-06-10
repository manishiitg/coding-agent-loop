package server

import (
	"strings"
	"testing"
	"time"

	agent "mcp-agent-builder-go/agent_go/pkg/agentwrapper"
	"mcp-agent-builder-go/agent_go/pkg/workflowtypes"
)

func boolPtr(b bool) *bool { return &b }

// clearTuningEnv unsets every env var the shared tuning reads so tests see
// pure defaults regardless of the developer's shell.
func clearTuningEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"TOOL_EXECUTION_TIMEOUT",
		"ENABLE_CONTEXT_SUMMARIZATION",
		"SUMMARIZE_ON_TOKEN_THRESHOLD",
		"TOKEN_THRESHOLD_PERCENT",
		"SUMMARIZE_ON_FIXED_TOKEN_THRESHOLD",
		"FIXED_TOKEN_THRESHOLD",
		"SUMMARY_KEEP_LAST_MESSAGES",
		"ENABLE_CONTEXT_EDITING",
		"CONTEXT_EDITING_THRESHOLD",
		"CONTEXT_EDITING_TURN_THRESHOLD",
		"LARGE_OUTPUT_THRESHOLD",
		"ENABLE_PARALLEL_TOOL_EXECUTION",
	} {
		t.Setenv(name, "")
	}
}

func TestApplySharedLLMAgentTuningDefaults(t *testing.T) {
	clearTuningEnv(t)
	var cfg agent.LLMAgentConfig
	applySharedLLMAgentTuning(&cfg, &QueryRequest{}, nil)

	if !cfg.EnableContextSummarization {
		t.Error("context summarization should default to enabled")
	}
	if !cfg.SummarizeOnTokenThreshold || !cfg.SummarizeOnFixedTokenThreshold {
		t.Error("summarization thresholds should default to enabled")
	}
	if cfg.TokenThresholdPercent != 0.8 {
		t.Errorf("TokenThresholdPercent default = %v, want 0.8", cfg.TokenThresholdPercent)
	}
	if cfg.FixedTokenThreshold != 200000 {
		t.Errorf("FixedTokenThreshold default = %d, want 200000", cfg.FixedTokenThreshold)
	}
	if cfg.SummaryKeepLastMessages != 4 {
		t.Errorf("SummaryKeepLastMessages default = %d, want 4", cfg.SummaryKeepLastMessages)
	}
	if cfg.EnableContextEditing {
		t.Error("context editing should default to disabled")
	}
	if cfg.ContextEditingThreshold != 0 || cfg.ContextEditingTurnThreshold != 0 || cfg.LargeOutputThreshold != 0 {
		t.Error("editing/offload thresholds should default to 0 (library defaults)")
	}
	if !cfg.EnableParallelToolExecution {
		t.Error("parallel tool execution should default to enabled")
	}
	if cfg.ToolTimeout != 0 {
		t.Errorf("ToolTimeout default = %v, want 0", cfg.ToolTimeout)
	}
}

func TestApplySharedLLMAgentTuningPriority(t *testing.T) {
	clearTuningEnv(t)
	// Env layer
	t.Setenv("ENABLE_CONTEXT_SUMMARIZATION", "false")
	t.Setenv("TOKEN_THRESHOLD_PERCENT", "0.5")
	t.Setenv("FIXED_TOKEN_THRESHOLD", "100000")
	t.Setenv("ENABLE_CONTEXT_EDITING", "true")
	t.Setenv("ENABLE_PARALLEL_TOOL_EXECUTION", "false")
	t.Setenv("TOOL_EXECUTION_TIMEOUT", "90s")

	// Env beats defaults.
	var envCfg agent.LLMAgentConfig
	applySharedLLMAgentTuning(&envCfg, &QueryRequest{}, nil)
	if envCfg.EnableContextSummarization {
		t.Error("env should disable summarization")
	}
	if envCfg.TokenThresholdPercent != 0.5 || envCfg.FixedTokenThreshold != 100000 {
		t.Errorf("env thresholds not applied: %v / %d", envCfg.TokenThresholdPercent, envCfg.FixedTokenThreshold)
	}
	if !envCfg.EnableContextEditing {
		t.Error("env should enable context editing")
	}
	if envCfg.EnableParallelToolExecution {
		t.Error("env should disable parallel tool execution")
	}
	if envCfg.ToolTimeout != 90*time.Second {
		t.Errorf("ToolTimeout = %v, want 90s", envCfg.ToolTimeout)
	}

	// Preset beats env (root chat agents only).
	preset := &workflowtypes.PresetLLMConfig{
		EnableContextSummarization: boolPtr(true),
		EnableContextEditing:       boolPtr(false),
	}
	var presetCfg agent.LLMAgentConfig
	applySharedLLMAgentTuning(&presetCfg, &QueryRequest{}, preset)
	if !presetCfg.EnableContextSummarization {
		t.Error("preset should override env for summarization")
	}
	if presetCfg.EnableContextEditing {
		t.Error("preset should override env for context editing")
	}

	// Request beats preset and env.
	req := &QueryRequest{
		EnableContextSummarization: boolPtr(false),
		EnableContextEditing:       boolPtr(true),
		TokenThresholdPercent:      0.9,
		FixedTokenThreshold:        300000,
	}
	var reqCfg agent.LLMAgentConfig
	applySharedLLMAgentTuning(&reqCfg, req, preset)
	if reqCfg.EnableContextSummarization {
		t.Error("request should override preset for summarization")
	}
	if !reqCfg.EnableContextEditing {
		t.Error("request should override preset for context editing")
	}
	if reqCfg.TokenThresholdPercent != 0.9 || reqCfg.FixedTokenThreshold != 300000 {
		t.Errorf("request thresholds not applied: %v / %d", reqCfg.TokenThresholdPercent, reqCfg.FixedTokenThreshold)
	}
}

// TestRootAndSubAgentTuningMatch locks the invariant that motivated the
// extraction: given the same request, the root chat agent and a delegated
// sub-agent resolve identical tuning (sub-agents just have no preset layer).
func TestRootAndSubAgentTuningMatch(t *testing.T) {
	clearTuningEnv(t)
	t.Setenv("FIXED_TOKEN_THRESHOLD", "150000")
	t.Setenv("SUMMARY_KEEP_LAST_MESSAGES", "6")

	req := &QueryRequest{
		SummarizeOnTokenThreshold:   boolPtr(false),
		ContextEditingThreshold:     42,
		ContextEditingTurnThreshold: 7,
	}

	var rootCfg, subCfg agent.LLMAgentConfig
	applySharedLLMAgentTuning(&rootCfg, req, nil)
	applySharedLLMAgentTuning(&subCfg, req, nil)

	if rootCfg.EnableContextSummarization != subCfg.EnableContextSummarization ||
		rootCfg.SummarizeOnTokenThreshold != subCfg.SummarizeOnTokenThreshold ||
		rootCfg.TokenThresholdPercent != subCfg.TokenThresholdPercent ||
		rootCfg.SummarizeOnFixedTokenThreshold != subCfg.SummarizeOnFixedTokenThreshold ||
		rootCfg.FixedTokenThreshold != subCfg.FixedTokenThreshold ||
		rootCfg.SummaryKeepLastMessages != subCfg.SummaryKeepLastMessages ||
		rootCfg.EnableContextEditing != subCfg.EnableContextEditing ||
		rootCfg.ContextEditingThreshold != subCfg.ContextEditingThreshold ||
		rootCfg.ContextEditingTurnThreshold != subCfg.ContextEditingTurnThreshold ||
		rootCfg.LargeOutputThreshold != subCfg.LargeOutputThreshold ||
		rootCfg.EnableParallelToolExecution != subCfg.EnableParallelToolExecution ||
		rootCfg.ToolTimeout != subCfg.ToolTimeout {
		t.Errorf("root and sub-agent tuning diverged:\nroot: %+v\nsub:  %+v", rootCfg, subCfg)
	}
	if subCfg.FixedTokenThreshold != 150000 || subCfg.SummaryKeepLastMessages != 6 {
		t.Errorf("env values not applied: %d / %d", subCfg.FixedTokenThreshold, subCfg.SummaryKeepLastMessages)
	}
	if subCfg.ContextEditingThreshold != 42 || subCfg.ContextEditingTurnThreshold != 7 {
		t.Errorf("request values not applied: %d / %d", subCfg.ContextEditingThreshold, subCfg.ContextEditingTurnThreshold)
	}
}

func TestBuildSecretNamesPrompt(t *testing.T) {
	type secret = struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if got := buildSecretNamesPrompt(nil); got != "" {
		t.Errorf("empty secrets should produce empty prompt, got %q", got)
	}
	prompt := buildSecretNamesPrompt([]secret{{Name: "API_KEY", Value: "shh"}})
	if !strings.Contains(prompt, "SECRET_API_KEY") {
		t.Errorf("prompt should reference SECRET_API_KEY, got %q", prompt)
	}
	if strings.Contains(prompt, "shh") {
		t.Error("prompt must never contain secret values")
	}
}
