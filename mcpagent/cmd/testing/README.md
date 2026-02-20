# Testing Framework

This directory contains integration and comprehensive tests for the mcpagent package.

## Structure

Tests are organized as CLI commands in `cmd/testing/`:

- **Test commands**: `cmd/testing/*/` - Each test in its own folder with Cobra command implementation
- **Test utilities**: `cmd/testing/testutils/` - Shared test utilities (logger, agent, MCP, LLM helpers)
- **Test documentation**: This README and per-test documentation

### Test Utilities Package

The `testutils/` package provides shared utilities for all tests:

- **Logger utilities** (`testutils/logger.go`) - Standardized logger initialization
- **MCP utilities** (`testutils/mcp.go`) - MCP config loading and temporary config creation
- **LLM utilities** (`testutils/llm.go`) - LLM instance creation for tests
- **Agent utilities** (`testutils/agent.go`) - Agent creation helpers and tracer utilities

## Test Utilities API

All tests should use the shared test utilities from `testutils/` package. Here's the complete API documentation:

### Logger Utilities

#### `NewTestLogger(cfg *TestLoggerConfig)`

Creates a new test logger with the specified configuration. If config is nil, it uses viper to get configuration from flags.

```go
import testutils "github.com/manishiitg/mcpagent/cmd/testing/testutils"

logger := testutils.NewTestLogger(nil) // Uses viper config
// or
logger := testutils.NewTestLogger(&testutils.TestLoggerConfig{
    LogFile:  "logs/test.log",
    LogLevel: "debug",
})
```

#### `NewTestLoggerFromViper()`

Convenience function that creates a test logger using viper configuration.

```go
logger := testutils.NewTestLoggerFromViper()
```

### MCP Utilities

#### `LoadTestMCPConfig(path string, logger loggerv2.Logger)`

Loads an MCP configuration file for testing. If path is empty, it tries to get the path from viper config or uses a default.

```go
config, err := testutils.LoadTestMCPConfig("", logger)
```

#### `CreateTempMCPConfig(servers map[string]interface{}, logger loggerv2.Logger)`

Creates a temporary MCP configuration file. Returns the path and a cleanup function.

```go
configPath, cleanup, err := testutils.CreateTempMCPConfig(
    map[string]interface{}{"test-server": nil},
    logger,
)
defer cleanup()
```

#### `GetDefaultTestConfigPath()`

Returns the default path for test MCP configuration, checking common locations.

```go
configPath := testutils.GetDefaultTestConfigPath()
```

### LLM Utilities

#### `CreateTestLLM(cfg *TestLLMConfig)`

Creates a test LLM instance with the specified configuration.

```go
llm, err := testutils.CreateTestLLM(&testutils.TestLLMConfig{
    Provider: string(llm.ProviderOpenAI),
    ModelID:  openai.ModelGPT41Mini,
    Logger:   logger,
})
```

#### `CreateTestLLMFromViper(logger loggerv2.Logger)`

Creates a test LLM using viper configuration.

```go
llm, err := testutils.CreateTestLLMFromViper(logger)
```

### Agent Utilities

#### `CreateTestAgent(ctx context.Context, cfg *TestAgentConfig)`

Creates a test agent with the specified configuration.

```go
agent, err := testutils.CreateTestAgent(ctx, &testutils.TestAgentConfig{
    LLM:       llm,
    ConfigPath: configPath,
    Tracer:    tracer,
    TraceID:   traceID,
    Logger:    logger,
    Options:   []mcpagent.AgentOption{...}, // Optional agent options
})
```

#### `CreateMinimalAgent(ctx, llm, tracer, traceID, logger)`

Creates a minimal test agent with empty MCP config. Useful for tests that don't need MCP servers.

```go
agent, err := testutils.CreateMinimalAgent(ctx, llm, tracer, traceID, logger)
```

#### `CreateAgentWithTracer(ctx, llm, configPath, tracer, traceID, logger, options...)`

Creates a test agent with a specific tracer.

```go
agent, err := testutils.CreateAgentWithTracer(
    ctx, llm, configPath, tracer, traceID, logger,
    mcpagent.WithSelectedTools([]string{"server:tool"}),
)
```

### Tracer Utilities

#### `IsNoopTracer(tracer observability.Tracer)`

Checks if a tracer is a NoopTracer.

```go
if testutils.IsNoopTracer(tracer) {
    // Tracer is not active
}
```

#### `IsLangfuseTracer(tracer observability.Tracer)`

Checks if a tracer is a LangfuseTracer (not NoopTracer).

```go
if testutils.IsLangfuseTracer(tracer) {
    // Langfuse tracing is active
}
```

#### `GetTracerWithLogger(provider string, logger loggerv2.Logger)`

Gets a tracer with the specified provider and logger. Returns the tracer and a boolean indicating if it's a real tracer.

```go
tracer, isReal := testutils.GetTracerWithLogger("langfuse", logger)
```

#### `GenerateTestTraceID()`

Generates a unique trace ID for testing.

```go
traceID := testutils.GenerateTestTraceID()
```

### Complete Example

Here's a complete example using all test utilities:

```go
import testutils "github.com/manishiitg/mcpagent/cmd/testing/testutils"

// Initialize logger
logger := testutils.NewTestLoggerFromViper()

// Load MCP config
config, err := testutils.LoadTestMCPConfig("", logger)
if err != nil {
    return fmt.Errorf("failed to load config: %w", err)
}

// Create LLM
llm, err := testutils.CreateTestLLM(&testutils.TestLLMConfig{
    Provider: string(llm.ProviderOpenAI),
    Logger:   logger,
})
if err != nil {
    return fmt.Errorf("failed to create LLM: %w", err)
}

// Get tracer
tracer, _ := testutils.GetTracerWithLogger("langfuse", logger)
traceID := testutils.GenerateTestTraceID()

// Create agent
agent, err := testutils.CreateTestAgent(ctx, &testutils.TestAgentConfig{
    LLM:       llm,
    ConfigPath: configPath,
    Tracer:    tracer,
    TraceID:   traceID,
    Logger:    logger,
})
```

## Test Criteria Documentation

**Important**: These tests don't use traditional asserts. Instead, logs are analyzed (manually or by LLM) to verify test success.

Each test folder should contain a `criteria.md` file that documents:
- What the test does
- How to run the test
- **Detailed log analysis criteria** - what to check in logs to verify success
- Expected test outcomes
- Troubleshooting guide

The `criteria.md` file serves as the definitive guide for analyzing test results and determining if a test passed or failed.

## Existing Tests

### `tool-filter` - Tool Filter Testing

**Folder**: `cmd/testing/tool-filter/`  
**Files**: 
- `tool-filter-test.go` - Test implementation with Cobra command
- `criteria.md` - Log analysis criteria (if applicable)
**Command**: `mcpagent-test test tool-filter`

Tests the unified `ToolFilter` system that ensures consistency between:
- LLM tool registration (what tools the LLM can actually call)
- Discovery results (what tools appear in system prompt)

#### Test Coverage

1. **Comprehensive Filter Scenarios** (`TestComprehensiveFilterScenarios`)
   - Priority conflicts (selectedServers vs selectedTools)
   - Package name format normalization (hyphens vs underscores)
   - Custom tools filtering with system categories
   - Virtual tools always included
   - Wildcard patterns (`server:*`)

2. **Discovery Simulation** (`TestDiscoverySimulation`)
   - Simulates what `code_execution_tools.go` does during discovery
   - Tests package name passing from discovery to filter
   - Validates directory name vs config name handling

3. **System Categories** (`TestSystemCategoriesIncludedByDefault`)
   - `workspace_tools` and `human_tools` included by default
   - Specific tool selection overrides default behavior
   - MCP tool filtering doesn't affect system categories

4. **Integration Tests** (require MCP config + LLM)
   - Normal mode integration
   - Code execution mode integration
   - Filter consistency between modes

#### Running

```bash
# Run via Cobra CLI command
mcpagent-test test tool-filter

# With custom log file
mcpagent-test test tool-filter --log-file logs/my-test.log

# With debug logging
mcpagent-test test tool-filter --log-level debug

# With custom MCP config for integration tests
mcpagent-test test tool-filter --config configs/mcp_servers_simple.json
```

#### Logs

- Default: stdout (no file logging unless `--log-file` is specified)
- Override with `--log-file` flag

---

### `agent-mcp` - Agent MCP Integration Testing

**Folder**: `cmd/testing/agent-mcp/`  
**Files**: 
- `agent-mcp-test.go` - Test implementation with Cobra command
- `criteria.md` - Log analysis criteria
**Command**: `mcpagent-test test agent-mcp`

Tests agent functionality with multiple MCP servers (sequential-thinking, context7, aws-pricing).

**See `criteria.md` in the agent-mcp folder for detailed log analysis criteria.**

#### Running

```bash
# Run via Cobra CLI command (uses OpenAI by default)
mcpagent-test test agent-mcp --log-file logs/agent-mcp-test.log

# With specific model
mcpagent-test test agent-mcp --model gpt-4.1-mini --log-file logs/agent-mcp-test.log
```

---

### `large-tool-output` - Large Tool Output Handling Testing

**Folder**: `cmd/testing/large-tool-output/`  
**Files**: 
- `large-tool-output-test.go` - Test implementation with Cobra command
- `criteria.md` - Log analysis criteria
**Command**: `mcpagent-test test large-tool-output`

Tests the large tool output handling feature:
- Large output detection and file writing
- File message creation with previews
- Virtual tools: `read_large_output`, `search_large_output`, `query_large_output`

**See `criteria.md` in the large-tool-output folder for detailed log analysis criteria.**

#### Running

```bash
# Run via Cobra CLI command (uses OpenAI by default)
mcpagent-test test large-tool-output --log-file logs/large-tool-output-test.log

# With custom threshold (default: 1000 tokens)
mcpagent-test test large-tool-output --threshold 2000 --log-file logs/large-tool-output-test.log

# Test with text output instead of JSON
mcpagent-test test large-tool-output --output-type text --log-file logs/large-tool-output-test.log
```

---

### `smart-routing` - Smart Routing Testing

**Folder**: `cmd/testing/smart-routing/`  
**Files**: 
- `smart-routing-test.go` - Test implementation with Cobra command
- `criteria.md` - Log analysis criteria
**Command**: `mcpagent-test test smart-routing`

Tests the agent's smart routing feature that filters tools based on conversation context.

**See `criteria.md` in the smart-routing folder for detailed log analysis criteria.**

#### Running

```bash
# Basic test (uses OpenAI by default, thresholds: 5 tools, 2 servers)
mcpagent-test test smart-routing --log-file logs/smart-routing-test.log

# Custom thresholds
mcpagent-test test smart-routing --max-tools-threshold 10 --max-servers-threshold 3 --log-file logs/smart-routing-test.log

# Custom smart routing config
mcpagent-test test smart-routing --temperature 0.1 --max-tokens 1000 --log-file logs/smart-routing-test.log
```

---

### `structured-output` - Structured Output Testing

**Folder**: `cmd/testing/structured-output/`  
**Files**: 
- `conversion/conversion-test.go` - Model 1: Text Conversion Model test
- `tool/tool-test.go` - Model 2: Tool-Based Model test
- `conversion/criteria.md` - Log analysis criteria for Model 1
- `tool/criteria.md` - Log analysis criteria for Model 2
- `README.md` - Comprehensive documentation of both models
**Commands**: 
- `mcpagent-test test structured-output-conversion` - Test Model 1 (Text Conversion)
- `mcpagent-test test structured-output-tool` - Test Model 2 (Tool-Based)

Tests the agent's structured output generation capabilities using two different models:

#### Model 1: Text Conversion Model (`structured-output-conversion`)
- **How it works**: Agent gets text response → Second LLM call converts to JSON → Parse into struct
- **Methods**: `AskStructured`, `AskWithHistoryStructured`
- **Pros**: Always works, better for complex schemas, more predictable
- **Cons**: 2 LLM calls (slower, more expensive)

#### Model 2: Tool-Based Model (`structured-output-tool`)
- **How it works**: Dynamically registers custom tool → LLM calls tool → Extract from arguments
- **Methods**: `AskWithHistoryStructuredViaTool`
- **Pros**: Single LLM call (faster, cheaper), preserves context
- **Cons**: LLM may not call tool (graceful fallback to text)

**See `structured-output/README.md` for detailed comparison and usage guide.**

#### Test Coverage

**Model 1 (Conversion) Tests:**
1. Simple Person struct (`AskStructured`)
2. TodoList with conversation history (`AskWithHistoryStructured`)
3. Complex Project with nested arrays (`AskStructured`)

**Model 2 (Tool) Tests:**
1. Simple Person via `submit_person_profile` tool
2. Complex Order with nested items via `submit_order` tool
3. Tool not called scenario (graceful fallback)

#### Running

```bash
# Test Model 1: Text Conversion (uses OpenAI by default)
mcpagent-test test structured-output-conversion --log-file logs/conversion-test.log

# Test Model 2: Tool-Based (uses OpenAI by default)
mcpagent-test test structured-output-tool --log-file logs/tool-test.log

# With custom model
mcpagent-test test structured-output-conversion --model gpt-4o-mini --log-file logs/conversion-test.log
```

#### Logs

- Default: stdout (no file logging unless `--log-file` is specified)
- Override with `--log-file` flag
- **Important**: Always use `--log-file` to avoid cluttering terminal output

---

### `token-tracking` - Token Tracking Testing

**Folder**: `cmd/testing/token-tracking/`  
**Files**: 
- `token-tracking-test.go` - Test implementation with Cobra command
- `criteria.md` - Log analysis criteria
**Command**: `mcpagent-test test token-tracking`

Tests the agent's cumulative token usage tracking feature:
- Token accumulation across multiple LLM calls
- Cache tokens tracking (if supported by provider)
- Reasoning tokens tracking (if supported by model)
- LLM call count and cache-enabled call count
- Multi-turn conversation token accumulation

**See `criteria.md` in the token-tracking folder for detailed log analysis criteria.**

#### Test Coverage

1. **Initial State Check** (`TestInitialTokenUsage`)
   - Verifies token usage starts at zero
   - All metrics should be zero before any calls

2. **Single Call Token Accumulation** (`TestSingleCall`)
   - Makes one agent call
   - Verifies tokens are tracked after call
   - Verifies LLM call count increments

3. **Multiple Calls Cumulative Accumulation** (`TestMultipleCalls`)
   - Makes multiple agent calls
   - Verifies cumulative totals increase with each call
   - Verifies no token counts decrease (only accumulate)

4. **Multi-Turn Conversation** (`TestMultiTurnConversation`)
   - Tests token tracking across conversation with context
   - Verifies tokens accumulate across turns
   - Calculates tokens per call

5. **Final Summary** (`TestFinalSummary`)
   - Provides comprehensive token usage statistics
   - Calculates averages per call
   - Analyzes cache tokens and reasoning tokens (if present)

#### Running

```bash
# Basic test (uses OpenAI by default, 3 calls)
mcpagent-test test token-tracking --log-file logs/token-tracking-test.log

# With custom number of calls
mcpagent-test test token-tracking --num-calls 5 --log-file logs/token-tracking-test.log

# With specific model
mcpagent-test test token-tracking --model gpt-4.1 --log-file logs/token-tracking-test.log
```

#### Logs

- Default: stdout (no file logging unless `--log-file` is specified)
- Override with `--log-file` flag
- **Important**: Always use `--log-file` to avoid cluttering terminal output

---

### `human-feedback-code-exec` - Human Feedback Tool in Code Execution Mode Testing

**Folder**: `cmd/testing/human-feedback-code-exec/`  
**Files**: 
- `human-feedback-code-exec-test.go` - Test implementation with Cobra command
- `criteria.md` - Log analysis criteria
**Command**: `mcpagent-test test human-feedback-code-exec`

Tests that the `human_feedback` tool is available as a normal tool in code execution mode, even though other custom tools are excluded.

**Key Feature**: In code execution mode, most tools are excluded from direct LLM access. However, tools with category "human" (like `human_feedback`) are an exception and remain available as normal tools because they require event bridge access for frontend UI.

**See `criteria.md` in the human-feedback-code-exec folder for detailed log analysis criteria.**

#### Test Coverage

1. **Initial State Check** (`TestInitialState`)
   - Verifies only code execution virtual tools are present initially
   - Checks for `discover_code_files` and `write_code`

2. **Regular Tool Exclusion** (`TestRegularToolExclusion`)
   - Registers a regular custom tool (category "custom")
   - Verifies it is NOT in Tools array in code exec mode

3. **Human Tool Availability** (`TestHumanToolAvailability`)
   - Registers `human_feedback` tool with category "human"
   - Verifies it IS in Tools array in code exec mode

4. **Tool Count Verification** (`TestToolCounts`)
   - Verifies final tool count (expected: 3 tools)
   - Verifies breakdown: 2 code exec tools + 1 human tool

#### Running

```bash
# Basic test (uses OpenAI by default, gpt-4.1)
mcpagent-test test human-feedback-code-exec --log-file logs/human-feedback-code-exec-test.log

# With specific model
mcpagent-test test human-feedback-code-exec --model gpt-4.1 --log-file logs/human-feedback-code-exec-test.log
```

#### Logs

- Default: stdout (no file logging unless `--log-file` is specified)
- Override with `--log-file` flag
- **Important**: Always use `--log-file` to avoid cluttering terminal output

---

## Test Plan

This section outlines planned tests to be implemented for comprehensive agent feature coverage.

### High Priority Tests

#### 1. `smart-routing` - Smart Routing Testing
**Status**: ✅ Completed  
**Feature**: `EnableSmartRouting`, `SmartRoutingThreshold`, `SmartRoutingConfig`  
**What to Test**: Tool filtering based on conversation context, threshold behavior, routing decisions  
**Complexity**: High (requires conversation context)

#### 3. `structured-output` - Structured Output Testing
**Status**: ✅ Completed  
**Feature**: `AskStructured`, `AskWithHistoryStructured`, `AskWithHistoryStructuredViaTool`  
**What to Test**: JSON schema extraction, structured data conversion, tool-based structured output  
**Complexity**: Medium

#### 4. `custom-tools` - Custom Tools Registration Testing
**Status**: 📋 Planned  
**Feature**: `RegisterCustomTool`, `GetCustomToolsByCategory`, `UpdateCodeExecutionRegistry`  
**What to Test**: Tool registration, category handling, code generation, registry updates  
**Complexity**: Medium

#### 6. `token-tracking` - Token Tracking Testing
**Status**: ✅ Completed  
**Feature**: `GetTokenUsage`, cumulative token tracking  
**What to Test**: Token accumulation, cache tokens, reasoning tokens, cumulative metrics  
**Complexity**: Low-Medium

### Medium Priority Tests

#### 5. `system-prompt` - System Prompt Management Testing
**Status**: 📋 Planned  
**Feature**: `SetSystemPrompt`, `AppendSystemPrompt`, `RebuildSystemPromptWithFilteredServers`  
**What to Test**: Custom prompts, prompt appending, filtered server rebuilding  
**Complexity**: Medium

#### 9. `resource-discovery` - Resource/Prompt Discovery Testing
**Status**: 📋 Planned  
**Feature**: `DiscoverResource`, `DiscoverPrompt`  
**What to Test**: System prompt inclusion/exclusion of resources and prompts  
**Complexity**: Low

#### 10. `tool-timeout` - Tool Timeout Testing
**Status**: 📋 Planned  
**Feature**: `ToolTimeout`  
**What to Test**: Timeout behavior for long-running tools  
**Complexity**: Medium (requires slow tool simulation)

#### 11. `event-system` - Event System Testing
**Status**: 📋 Planned  
**Feature**: `EmitTypedEvent`, `AddEventListener`, `SubscribeToEvents`  
**What to Test**: Event emission, listener handling, event streaming  
**Complexity**: Medium

---

## Adding New Tests

When adding a new test, document it in the "Existing Tests" section above following this format:

### `your-test-name` - Test Description

**Folder**: `cmd/testing/your-test-name/`  
**Files**: 
- `your-test-name-test.go` - Test implementation with Cobra command
- `criteria.md` - Log analysis criteria (required for integration tests)
**Command**: `mcpagent-test test your-test-name`

Brief description of what this test validates.

**Note**: Create a `criteria.md` file in the test folder documenting what to check in logs to verify test success.

#### Test Coverage

1. **Test Scenario 1** (`TestFunctionName`)
   - What it tests
   - Key validations

2. **Test Scenario 2** (`TestFunctionName2`)
   - What it tests
   - Key validations

#### Running

```bash
# Run via Cobra CLI command
mcpagent-test test your-test-name

# With custom log file
mcpagent-test test your-test-name --log-file logs/my-test.log

# With debug logging
mcpagent-test test your-test-name --log-level debug
```

#### Logs

- Default: stdout (no file logging unless `--log-file` is specified)
- Override with `--log-file` flag

---

### Step-by-Step Guide

#### 1. Create Test Folder

Create `cmd/testing/your-feature/` with:
- `your-feature-test.go` - Test implementation with Cobra command
- `criteria.md` - Log analysis criteria (required for integration tests that analyze logs)

#### 2. Test File Structure

```go
package yourfeature

import (
    "fmt"
    "github.com/spf13/cobra"
    loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
    testutils "github.com/manishiitg/mcpagent/cmd/testing/testutils"
)

var yourFeatureTestCmd = &cobra.Command{
    Use:   "your-feature",
    Short: "Test your feature description",
    RunE: func(cmd *cobra.Command, args []string) error {
        // Initialize logger using shared utilities
        logger := testutils.NewTestLoggerFromViper()
        
        logger.Info("=== Your Feature Test ===")
        if err := TestMainFeature(logger); err != nil {
            return fmt.Errorf("test failed: %w", err)
        }
        return nil
    },
}

// GetYourFeatureTestCmd returns the test command
func GetYourFeatureTestCmd() *cobra.Command {
    return yourFeatureTestCmd
}

// Export test functions for reuse
func TestMainFeature(log loggerv2.Logger) error {
    // Test implementation
    return nil
}
```

#### 3. Register Command

Add to `cmd/testing/testing.go`:

```go
func initTestingCommands() {
    TestingCmd.AddCommand(toolfilter.GetToolFilterTestCmd())
    TestingCmd.AddCommand(yourfeature.GetYourFeatureTestCmd()) // Add your command
}
```

#### 4. Create Criteria Documentation

For integration tests that analyze logs (rather than using traditional asserts), create a `criteria.md` file in the test folder:

```markdown
# Your Feature Test - Log Analysis Criteria

This test validates [feature]. **These tests don't use traditional asserts** - instead, logs are analyzed (manually or by LLM) to verify test success.

## Running the Test

[Commands to run the test]

## What This Test Does

[Description of test steps]

## Log Analysis Checklist

After running the test, analyze the log file to verify:

### ✅ Category 1
- [ ] Check item 1
- [ ] Check item 2

**What to look for in logs:**
```
specific log patterns
```

### ✅ Category 2
- [ ] Check item 1
- [ ] Check item 2

## Expected Test Outcome

[What a successful test should show]

## Troubleshooting

[Common issues and solutions]
```

#### 5. Test Format Guidelines

- **Focus on complex scenarios** - Simple unit tests go in `*_test.go` files next to source
- **Export test functions** - Use `Test` prefix for exported test functions
- **Use table-driven tests** - Easier to add cases and maintain
- **Descriptive error messages** - Include context in failure messages
- **Handle missing dependencies** - Warn and skip integration tests gracefully
- **Log everything** - Use structured logging for debugging
- **Use shared utilities** - Always use `testutils/` package for common operations
- **Reference criteria.md** - In test output, mention the criteria.md file for log analysis guidance

## Configuration & Flags

### Common Flags

All test commands support:
- `--log-file`: Override log file path
- `--log-level`: Set log level (debug, info, warn, error)
- `--verbose`: Enable verbose output
- `--config`: MCP config file for integration tests
- `--provider`: LLM provider for tests (default: openai)
- `--timeout`: Test timeout duration (default: 5m)

### Configuration

All configuration is done via Cobra flags (bound to viper):
- `--log-file`: Log file path (optional, defaults to stdout only)
- `--log-level`: Log level (debug, info, warn, error, default: `info`)
- `--config`: MCP config file for integration tests
- `--verbose`: Enable verbose output
- `--provider`: LLM provider (openai, bedrock, anthropic, etc.) (default: openai)
- `--timeout`: Test timeout duration

## Running Tests

### Via Cobra CLI Command

```bash
# Basic test run
mcpagent-test test your-feature

# With debug logging
mcpagent-test test your-feature --log-level debug

# With custom log file
mcpagent-test test your-feature --log-file logs/my-test.log

# With custom MCP config for integration tests
mcpagent-test test your-feature --config configs/mcp_servers_simple.json

# With specific LLM provider
mcpagent-test test your-feature --provider openai --model gpt-4o-mini
```

### Test Execution

Tests are run as standalone CLI commands, making them suitable for:
- Integration testing with real MCP servers
- End-to-end testing with LLM providers
- Complex scenarios requiring full agent setup
- CI/CD pipelines

Simple unit tests should still be written as `*_test.go` files next to the source code they test.
