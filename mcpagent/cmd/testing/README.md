# Testing Commands

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

See `testutils/README.md` for detailed documentation on using these utilities.

## Tests

### `tool-filter` - Tool Filter Testing

**Folder**: `cmd/testing/tool-filter/`  
**Files**: 
- `tool-filter-test.go` - Test implementation with Cobra command
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

## Adding New Tests

When adding a new test, document it here following this format:

### `your-test-name` - Test Description

**Folder**: `cmd/testing/your-test-name/`  
**Files**: 
- `your-test-name-test.go` - Test implementation with Cobra command
**Command**: `mcpagent-test test your-test-name`

Brief description of what this test validates.

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

## Adding New Tests

### 1. Create Test Folder

Create `cmd/testing/your-feature/` with:
- `your-feature-test.go` - Test implementation with Cobra command

### 2. Test File Structure

```go
package yourfeature

import (
    "fmt"
    "github.com/spf13/cobra"
    loggerv2 "mcpagent/logger/v2"
    testutils "mcpagent/cmd/testing/testutils"
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

// Export test functions for reuse
func TestMainFeature(log loggerv2.Logger) error {
    // Test implementation
    return nil
}
```

### 3. Register Command

Add to `cmd/testing/testing.go`:

```go
func initTestingCommands() {
    TestingCmd.AddCommand(toolfilter.GetToolFilterTestCmd())
    TestingCmd.AddCommand(yourfeature.GetYourFeatureTestCmd()) // Add your command
}
```

### 4. Test Format

- **Focus on complex scenarios** - Simple unit tests go in `*_test.go` files next to source
- **Export test functions** - Use `Test` prefix for exported test functions
- **Use table-driven tests** - Easier to add cases and maintain
- **Descriptive error messages** - Include context in failure messages
- **Handle missing dependencies** - Warn and skip integration tests gracefully
- **Log everything** - Use structured logging for debugging
- **Use shared utilities** - Always use `testutils/` package for common operations

## Test Utilities

### Using Test Utilities

All tests should use the shared test utilities from `testutils/` package:

```go
import testutils "mcpagent/cmd/testing/testutils"

// Initialize logger
logger := testutils.NewTestLoggerFromViper()

// Load MCP config
config, err := testutils.LoadTestMCPConfig("", logger)

// Create LLM
llm, err := testutils.CreateTestLLM(&testutils.TestLLMConfig{
    Provider: "bedrock",
    Logger:   logger,
})

// Create agent
agent, err := testutils.CreateTestAgent(ctx, &testutils.TestAgentConfig{
    LLM:       llm,
    ConfigPath: configPath,
    Tracer:    tracer,
    TraceID:   traceID,
    Logger:    logger,
})
```

See `testutils/README.md` for complete documentation.

### Common Flags

All test commands support:
- `--log-file`: Override log file path
- `--log-level`: Set log level (debug, info, warn, error)
- `--verbose`: Enable verbose output
- `--config`: MCP config file for integration tests

### Configuration

All configuration is done via Cobra flags (bound to viper):
- `--log-file`: Log file path (optional, defaults to stdout only)
- `--log-level`: Log level (debug, info, warn, error, default: `info`)
- `--config`: MCP config file for integration tests
- `--verbose`: Enable verbose output

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
```

