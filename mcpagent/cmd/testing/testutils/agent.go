package testutils

import (
	"context"
	"fmt"
	"os"
	"time"

	mcpagent "mcpagent/agent"
	loggerv2 "mcpagent/logger/v2"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// TestAgentConfig holds configuration for test agent creation
type TestAgentConfig struct {
	LLM        llmtypes.Model
	ServerName string
	ConfigPath string
	ModelID    string
	Tracer     observability.Tracer
	TraceID    observability.TraceID
	Logger     loggerv2.Logger
	Options    []mcpagent.AgentOption
}

// CreateTestAgent creates a test agent with the specified configuration.
func CreateTestAgent(ctx context.Context, cfg *TestAgentConfig) (*mcpagent.Agent, error) {
	if cfg == nil {
		return nil, fmt.Errorf("agent config cannot be nil")
	}

	if cfg.LLM == nil {
		return nil, fmt.Errorf("LLM cannot be nil")
	}

	if cfg.Logger == nil {
		cfg.Logger = loggerv2.NewNoop()
	}

	// Create agent
	agent, err := mcpagent.NewAgent(
		ctx,
		cfg.LLM,
		cfg.ServerName,
		cfg.ConfigPath,
		cfg.ModelID,
		cfg.Tracer,
		cfg.TraceID,
		cfg.Logger,
		cfg.Options...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create agent: %w", err)
	}

	return agent, nil
}

// CreateMinimalAgent creates a minimal test agent with empty MCP config.
// Useful for tests that don't need MCP servers.
func CreateMinimalAgent(ctx context.Context, llm llmtypes.Model, tracer observability.Tracer, traceID observability.TraceID, logger loggerv2.Logger) (*mcpagent.Agent, error) {
	// Create temporary minimal config
	tempConfig := "/tmp/minimal-mcp-config.json"
	minimalConfig := `{"mcpServers": {}}`
	if err := os.WriteFile(tempConfig, []byte(minimalConfig), 0644); err != nil {
		return nil, fmt.Errorf("failed to create temp config: %w", err)
	}

	// Cleanup function
	defer func() {
		os.Remove(tempConfig)
	}()

	cfg := &TestAgentConfig{
		LLM:        llm,
		ConfigPath: tempConfig,
		ModelID:    "test-model",
		Tracer:     tracer,
		TraceID:    traceID,
		Logger:     logger,
	}

	return CreateTestAgent(ctx, cfg)
}

// CreateAgentWithTracer creates a test agent with a specific tracer.
func CreateAgentWithTracer(ctx context.Context, llm llmtypes.Model, configPath string, tracer observability.Tracer, traceID observability.TraceID, logger loggerv2.Logger, options ...mcpagent.AgentOption) (*mcpagent.Agent, error) {
	cfg := &TestAgentConfig{
		LLM:        llm,
		ConfigPath: configPath,
		ModelID:    "test-model",
		Tracer:     tracer,
		TraceID:    traceID,
		Logger:     logger,
		Options:    options,
	}

	return CreateTestAgent(ctx, cfg)
}

// IsNoopTracer checks if a tracer is a NoopTracer.
// This is useful for determining if tracing is actually enabled.
func IsNoopTracer(tracer observability.Tracer) bool {
	if tracer == nil {
		return true
	}
	_, ok := tracer.(observability.NoopTracer)
	return ok
}

// IsLangfuseTracer checks if a tracer is a LangfuseTracer (not NoopTracer).
// This is useful for determining if Langfuse tracing is enabled.
func IsLangfuseTracer(tracer observability.Tracer) bool {
	if tracer == nil {
		return false
	}
	// Check if it's NOT a NoopTracer
	_, isNoop := tracer.(observability.NoopTracer)
	if isNoop {
		return false
	}
	// If it's not a NoopTracer and not nil, assume it's a LangfuseTracer
	// (or another real tracer implementation)
	return true
}

// GetTracerWithLogger gets a tracer with the specified provider and logger.
// Returns the tracer and a boolean indicating if it's a real tracer (not NoopTracer).
func GetTracerWithLogger(provider string, logger loggerv2.Logger) (observability.Tracer, bool) {
	tracer := observability.GetTracerWithLogger(provider, logger)
	isReal := IsLangfuseTracer(tracer)
	return tracer, isReal
}

// GenerateTestTraceID generates a unique trace ID for testing.
func GenerateTestTraceID() observability.TraceID {
	return observability.TraceID(fmt.Sprintf("test-trace-%d", time.Now().UnixNano()))
}
