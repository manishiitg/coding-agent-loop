# Test Utilities

This package provides shared utilities for testing the MCP Agent.

## Logger Utilities

### `NewTestLogger(cfg *TestLoggerConfig)`

Creates a new test logger with the specified configuration. If config is nil, it uses viper to get configuration from flags.

```go
import "mcpagent/cmd/testing/testutils"

logger := testutils.NewTestLogger(nil) // Uses viper config
// or
logger := testutils.NewTestLogger(&testutils.TestLoggerConfig{
    LogFile:  "logs/test.log",
    LogLevel: "debug",
})
```

### `NewTestLoggerFromViper()`

Convenience function that creates a test logger using viper configuration.

## MCP Utilities

### `LoadTestMCPConfig(path string, logger loggerv2.Logger)`

Loads an MCP configuration file for testing. If path is empty, it tries to get the path from viper config or uses a default.

```go
config, err := testutils.LoadTestMCPConfig("", logger)
```

### `CreateTempMCPConfig(servers map[string]interface{}, logger loggerv2.Logger)`

Creates a temporary MCP configuration file. Returns the path and a cleanup function.

```go
configPath, cleanup, err := testutils.CreateTempMCPConfig(
    map[string]interface{}{"test-server": nil},
    logger,
)
defer cleanup()
```

### `GetDefaultTestConfigPath()`

Returns the default path for test MCP configuration, checking common locations.

## LLM Utilities

### `CreateTestLLM(cfg *TestLLMConfig)`

Creates a test LLM instance with the specified configuration.

```go
llm, err := testutils.CreateTestLLM(&testutils.TestLLMConfig{
    Provider: "bedrock",
    ModelID:  "us.anthropic.claude-sonnet-4-20250514-v1:0",
    Logger:   logger,
})
```

### `CreateTestLLMFromViper(logger loggerv2.Logger)`

Creates a test LLM using viper configuration.

## Agent Utilities

### `CreateTestAgent(ctx context.Context, cfg *TestAgentConfig)`

Creates a test agent with the specified configuration.

```go
agent, err := testutils.CreateTestAgent(ctx, &testutils.TestAgentConfig{
    LLM:       llm,
    ConfigPath: configPath,
    Tracer:    tracer,
    TraceID:   traceID,
    Logger:    logger,
})
```

### `CreateMinimalAgent(ctx, llm, tracer, traceID, logger)`

Creates a minimal test agent with empty MCP config. Useful for tests that don't need MCP servers.

### `CreateAgentWithTracer(ctx, llm, configPath, tracer, traceID, logger, options...)`

Creates a test agent with a specific tracer.

## Tracer Utilities

### `IsNoopTracer(tracer observability.Tracer)`

Checks if a tracer is a NoopTracer.

### `IsLangfuseTracer(tracer observability.Tracer)`

Checks if a tracer is a LangfuseTracer (not NoopTracer).

### `GetTracerWithLogger(provider string, logger loggerv2.Logger)`

Gets a tracer with the specified provider and logger. Returns the tracer and a boolean indicating if it's a real tracer.

### `GenerateTestTraceID()`

Generates a unique trace ID for testing.

