# LLM Provider Testing Guide

## Overview

This testing system provides a **recording and replay** mechanism for testing LLM providers. Instead of making real API calls during every test run, you can:

1. **Record** actual LLM responses once (with real API calls)
2. **Replay** those recorded responses in subsequent test runs (fast, no API costs)
3. **Run a test suite** that automatically discovers and runs all recorded tests

This approach provides:
- ✅ **Fast test execution** (~200-500µs per test vs seconds for real API calls)
- ✅ **No API costs** during test runs
- ✅ **Deterministic results** for consistent testing
- ✅ **Real response validation** using actual LLM outputs
- ✅ **Easy test maintenance** with automatic discovery
- ✅ **Zero production overhead** - recording system has no performance impact when disabled
- ✅ **Multi-provider support** - Works with Anthropic, Bedrock, OpenAI, OpenRouter, and Vertex (Gemini)

---

## Architecture

### Components

1. **Recorder** (`internal/recorder/`)
   - Captures LLM API responses during recording mode
   - Stores responses with request metadata for matching
   - Loads recorded responses during replay mode

2. **Test Functions** (`internal/testing/commands/shared/test_functions.go`)
   - Shared test logic for different test types
   - Assertions for validating responses
   - Support for both live and replay modes

3. **Test Commands** (`internal/testing/commands/`)
   - CLI commands for running specific tests
   - Support for `--record` and `--replay` flags
   - Integration with the recorder system

4. **Test Suite** (`internal/testing/commands/shared/test_suite.go`)
   - Automatically discovers all recorded tests
   - Uses a flexible test registry system for extensibility
   - Runs all registered tests in replay mode
   - Provides comprehensive reporting

### Request-Response Matching

The system uses **request hashing** to match recorded responses with incoming requests:

- Each request (messages, model ID, call options) is hashed using SHA256
- The hash is stored with the recorded response
- During replay, the system computes the request hash and finds the matching recorded response
- This ensures the correct response is returned for each specific input

---

## Usage

### Recording Tests

To record a test (capture real LLM responses):

```bash
# Vertex (Gemini) - Record a plain text test
./bin/llm-test vertex --model gemini-2.5-flash --record

# Vertex (Gemini) - Record a tool call test
./bin/llm-test vertex-tool-call --model gemini-3-pro-preview --record

# OpenAI - Record a plain text test
./bin/llm-test openai --model gpt-4o-mini --record

# OpenAI - Record a tool call test
./bin/llm-test openai-tool-call --model gpt-5.1 --record

# Bedrock - Record a plain text test
./bin/llm-test bedrock --model global.anthropic.claude-sonnet-4-5-20250929-v1:0 --record

# Bedrock - Record a tool call test
./bin/llm-test llm-tool-call --model global.anthropic.claude-sonnet-4-5-20250929-v1:0 --record

# Anthropic - Record a tool call events test
./bin/llm-test anthropic-tool-call-events --model claude-3-5-sonnet-20241022 --record

# Bedrock - Record a tool call events test
./bin/llm-test bedrock-tool-call-events --model global.anthropic.claude-sonnet-4-5-20250929-v1:0 --record

# OpenAI - Record a tool call events test
./bin/llm-test openai-tool-call-events --model gpt-4o-mini --record

# OpenRouter - Record a tool call events test
./bin/llm-test openrouter-tool-call-events --model moonshotai/kimi-k2 --record

# Vertex - Record a tool call events test
./bin/llm-test vertex-tool-call-events --model gemini-2.5-flash --record

# Custom test directory
./bin/llm-test vertex-tool-call --model gemini-2.5-pro --record --test-dir my-testdata
```

**What happens:**
1. The test runs with real API calls to the LLM
2. All responses are captured and stored in JSON files
3. Files are saved to `testdata/{provider}/{test_name}_{model}_{hash}_{timestamp}.json`
4. Each file contains:
   - Request metadata (messages, model, options)
   - Request hash for matching
   - Response data (raw JSON from LLM)
   - Recording timestamp

### Replaying Tests

To replay a recorded test (use recorded responses):

```bash
# Vertex (Gemini) - Replay a plain text test
./bin/llm-test vertex --model gemini-2.5-flash --replay

# Vertex (Gemini) - Replay a tool call test
./bin/llm-test vertex-tool-call --model gemini-3-pro-preview --replay

# OpenAI - Replay a plain text test
./bin/llm-test openai --model gpt-4o-mini --replay

# OpenAI - Replay a tool call test
./bin/llm-test openai-tool-call --model gpt-5.1 --replay

# Bedrock - Replay a plain text test
./bin/llm-test bedrock --model global.anthropic.claude-sonnet-4-5-20250929-v1:0 --replay

# Bedrock - Replay a tool call test
./bin/llm-test llm-tool-call --model global.anthropic.claude-sonnet-4-5-20250929-v1:0 --replay

# Anthropic - Replay a tool call events test
./bin/llm-test anthropic-tool-call-events --model claude-3-5-sonnet-20241022 --replay

# Bedrock - Replay a tool call events test
./bin/llm-test bedrock-tool-call-events --model global.anthropic.claude-sonnet-4-5-20250929-v1:0 --replay

# OpenAI - Replay a tool call events test
./bin/llm-test openai-tool-call-events --model gpt-4o-mini --replay

# OpenRouter - Replay a tool call events test
./bin/llm-test openrouter-tool-call-events --model moonshotai/kimi-k2 --replay

# Vertex - Replay a tool call events test
./bin/llm-test vertex-tool-call-events --model gemini-2.5-flash --replay

# Custom test directory
./bin/llm-test vertex-tool-call --model gemini-2.5-pro --replay --test-dir my-testdata
```

**What happens:**
1. The system computes the request hash
2. Finds the matching recorded response file
3. Loads and replays the recorded response
4. All assertions run against the replayed response
5. No API calls are made (fast and free)

### Running the Test Suite

To run all recorded tests automatically:

```bash
# Run all recorded tests
./bin/llm-test test-suite

# Verbose output
./bin/llm-test test-suite --verbose

# Custom test directory
./bin/llm-test test-suite --test-dir my-testdata
```

**What happens:**
1. Initializes the test registry with all available test types
2. Scans `testdata/` directory for all recorded test files
3. Groups tests by test type and model
4. Runs each test in replay mode using the registered test runners
5. Provides comprehensive summary report

**Supported Test Types (automatically registered via test registry):**
- `plain_text` - Basic text generation tests
- `tool_call` - Tool calling tests
- `tool_call_events` - Tool call event tests (all providers: Anthropic, Bedrock, OpenAI, OpenRouter, Vertex)
- `token_usage` - Basic token usage tests
- `token_usage_cache` - Token usage with cache tests
- `simple_reasoning` - Reasoning token tests (gpt-5.1)

All test types are registered in the `initTestRegistry()` function and automatically discovered by the test suite.

**Example Output:**
```
🚀 Test Suite: Running 9 recorded test scenarios
📁 Test directory: testdata
📋 Registered test types: [plain_text simple_reasoning token_usage token_usage_cache tool_call tool_call_events]

📊 TEST SUITE SUMMARY
======================================================================

📋 Results by Test Type:

  PLAIN_TEXT:
    ✅ Passed: 2
      ✅ gemini-2.5-flash (997.166µs)
      ✅ gemini-3-pro-preview (586.458µs)

  TOOL_CALL:
    ✅ Passed: 3
      ✅ gemini-2.5-flash (2.398792ms)
      ✅ gemini-2.5-pro (2.618541ms)
      ✅ gemini-3-pro-preview (2.886625ms)

  TOOL_CALL_EVENTS:
    ✅ Passed: 1
      ✅ gpt-4o-mini (180.959µs)

  TOKEN_USAGE:
    ✅ Passed: 2
      ✅ gpt-4.1-mini (1.234567ms)
      ✅ gpt-5.1 (1.456789ms)

  SIMPLE_REASONING:
    ✅ Passed: 1
      ✅ gpt-5.1 (2.123456ms)

📈 Overall Statistics:
   Total tests: 9
   ✅ Passed: 9
   Total duration: 12.868541ms
   Average duration: 1.429838ms
   Success rate: 100.0%

🎉 All tests passed!
```

---

## Test Types

The test suite supports the following test types (all automatically discovered):

### Plain Text Tests

Tests basic text generation without tools:

```bash
# Vertex (Gemini)
./bin/llm-test vertex --model gemini-2.5-flash --record

# OpenAI
./bin/llm-test openai --model gpt-4o-mini --record

# Bedrock
./bin/llm-test bedrock --model global.anthropic.claude-sonnet-4-5-20250929-v1:0 --record
```

**Validates:**
- Response has choices
- Content is non-empty
- Token usage is available

### Tool Call Tests

Tests function calling capabilities:

```bash
# Vertex (Gemini)
./bin/llm-test vertex-tool-call --model gemini-3-pro-preview --record

# OpenAI
./bin/llm-test openai-tool-call --model gpt-5.1 --record

# Bedrock
./bin/llm-test llm-tool-call --model global.anthropic.claude-sonnet-4-5-20250929-v1:0 --record
```

**Validates:**
- Tool calls are present
- Tool call IDs are non-empty and unique
- Function names are correct
- Arguments are valid JSON
- Required parameters are present with correct values
- Parallel tool calls are handled correctly

### Tool Call Events Tests

Tests tool call event emission for all providers:

```bash
# Anthropic
./bin/llm-test anthropic-tool-call-events --model claude-3-5-sonnet-20241022 --record

# Bedrock
./bin/llm-test bedrock-tool-call-events --model global.anthropic.claude-sonnet-4-5-20250929-v1:0 --record

# OpenAI
./bin/llm-test openai-tool-call-events --model gpt-4o-mini --record

# OpenRouter
./bin/llm-test openrouter-tool-call-events --model moonshotai/kimi-k2 --record

# Vertex (Gemini)
./bin/llm-test vertex-tool-call-events --model gemini-2.5-flash --record
```

**Validates:**
- Tool call events are emitted correctly
- Event count matches tool call count
- Event structure matches expected format (ID, name, arguments)
- Events are properly captured by TestEventEmitter

### Token Usage Tests

Tests token usage extraction and reporting:

```bash
# Basic token usage
./bin/llm-test token-usage --provider openai --record

# OpenAI-specific token usage tests
./bin/llm-test openai-token-usage --record
```

**Test Types:**
- `token_usage` - Basic token usage validation
- `token_usage_cache` - Token usage with caching (OpenRouter)
- `simple_reasoning` - Reasoning token tests (gpt-5.1 with reasoning_effort/verbosity)

**Validates:**
- Token usage is extracted correctly
- Input/output/total tokens are available
- Reasoning tokens are captured (for reasoning models)
- Cache tokens are tracked (for OpenRouter)

---

## File Structure

### Recorded Test Files

Recorded responses are stored in JSON files with the following structure:

```
testdata/
├── vertex/
│   ├── plain_text_gemini-2.5-flash_d84fd823_20251128_194115.json
│   ├── plain_text_gemini-3-pro-preview_fa9ec0bd_20251128_200059.json
│   ├── tool_call_gemini-2.5-flash_abc12345_20251128_200000.json
│   ├── tool_call_gemini-2.5-pro_def67890_20251128_200100.json
│   └── tool_call_gemini-3-pro-preview_b8bb768b_20251128_202056.json
├── openai/
│   ├── plain_text_gpt-4o-mini_084d803f_20251128_205313.json
│   ├── plain_text_gpt-5.1_911bf63f_20251128_205312.json
│   ├── tool_call_gpt-4o-mini_abc12345_20251128_200000.json
│   └── tool_call_gpt-5.1_def67890_20251128_200100.json
└── bedrock/
    ├── plain_text_global_anthropic_claude-sonnet-4-5-20250929-v10_659f4fbf_20251128_213719.json
    └── tool_call_global_anthropic_claude-sonnet-4-5-20250929-v10_207908d8_20251128_214944.json
```

**Filename Format:**
```
{test_name}_{model_id}_{hash8chars}_{timestamp}.json
```

**File Contents:**
```json
{
  "provider": "vertex",
  "model_id": "gemini-2.5-flash",
  "test_name": "tool_call",
  "recorded_at": "2025-11-28T19:41:15Z",
  "request_hash": "d84fd823abc12345...",
  "request": {
    "messages": [...],
    "model_id": "gemini-2.5-flash",
    "options": {...}
  },
  "response_data": [...],
  "chunk_count": 15
}
```

---

## Adding New Tests

### Step 1: Create Test Command

Create a new test command in the appropriate provider directory:

```go
// internal/testing/commands/vertex/my-new-test.go
package vertex

var MyNewTestCmd = &cobra.Command{
    Use:   "my-new-test",
    Short: "Test my new feature",
    Run:   runMyNewTest,
}

type myNewTestFlags struct {
    model   string
    record  bool
    replay  bool
    testDir string
}

var myNewTestFlags myNewTestFlags

func init() {
    MyNewTestCmd.Flags().StringVar(&myNewTestFlags.model, "model", "", "Model to test")
    MyNewTestCmd.Flags().BoolVar(&myNewTestFlags.record, "record", false, "Record LLM responses")
    MyNewTestCmd.Flags().BoolVar(&myNewTestFlags.replay, "replay", false, "Replay recorded responses")
    MyNewTestCmd.Flags().StringVar(&myNewTestFlags.testDir, "test-dir", "testdata", "Test directory")
}

func runMyNewTest(cmd *cobra.Command, args []string) {
    logger := testing.GetTestLogger()
    
    ctx := context.Background()
    var rec *recorder.Recorder
    
    if myNewTestFlags.record || myNewTestFlags.replay {
        recConfig := recorder.RecordingConfig{
            Enabled:  myNewTestFlags.record,
            TestName: "my_new_test", // Unique test name
            Provider: "vertex",
            ModelID:  myNewTestFlags.model,
            BaseDir:  myNewTestFlags.testDir,
        }
        rec = recorder.NewRecorder(recConfig)
        if myNewTestFlags.replay {
            rec.SetReplayMode(true)
        }
        ctx = recorder.WithRecorder(ctx, rec)
    }
    
    llmInstance, err := llmproviders.InitializeLLM(llmproviders.Config{
        Provider: llmproviders.ProviderVertex,
        ModelID:  myNewTestFlags.model,
        Logger:   logger,
        Context:  ctx,
    })
    
    // Run your test function
    shared.RunMyNewTestWithContext(ctx, llmInstance, myNewTestFlags.model)
}
```

### Step 2: Create Test Function

Add your test function to `internal/testing/commands/shared/test_functions.go`:

```go
func RunMyNewTestWithContext(ctx context.Context, llm llmtypes.Model, modelID string) {
    log.Printf("🧪 Testing my new feature with model: %s", modelID)
    
    messages := []llmtypes.MessageContent{
        llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "Your test prompt here"),
    }
    
    resp, err := llm.GenerateContent(ctx, messages,
        llmtypes.WithModel(modelID),
    )
    
    if err != nil {
        log.Printf("❌ Test failed: %v", err)
        return
    }
    
    // ASSERTIONS
    if len(resp.Choices) == 0 {
        log.Printf("❌ Test failed - no choices in response")
        return
    }
    
    if resp.Choices[0].Content == "" {
        log.Printf("❌ Test failed - empty content")
        return
    }
    
    log.Printf("✅ Test passed!")
    log.Printf("   Response: %s", resp.Choices[0].Content)
}
```

### Step 3: Register Command

Add your command to `cmd/llm-test/main.go`:

```go
rootCmd.AddCommand(vertexcmd.MyNewTestCmd)
```

### Step 4: Register Test in Test Suite (Required)

To include your test in the test suite, register it in the `initTestRegistry()` function in `test_suite.go`:

```go
// In initTestRegistry() function
registerTest("my_new_test", func(ctx context.Context, llm llmtypes.Model, modelID string, provider string, logger interfaces.Logger) (bool, string) {
    RunMyNewTestWithContext(ctx, llm, modelID)
    return true, ""
})
```

**Example: Tool Call Events Test Registration**

The `tool_call_events` test is registered with provider-specific LLM initialization:

```go
registerTest("tool_call_events", func(ctx context.Context, llm llmtypes.Model, modelID string, provider string, logger interfaces.Logger) (bool, string) {
    // Create test event emitter to capture events
    testEmitter := NewTestEventEmitter()
    // Re-initialize LLM with event emitter based on provider
    var err error
    if provider == "openai" {
        llm, err = llmproviders.InitializeLLM(llmproviders.Config{
            Provider:     llmproviders.ProviderOpenAI,
            ModelID:      modelID,
            Temperature:  0.7,
            Logger:       logger,
            EventEmitter: testEmitter,
            Context:      ctx,
        })
    } else if provider == "bedrock" {
        // ... similar for other providers
    }
    if err != nil {
        return false, fmt.Sprintf("Failed to re-initialize LLM: %v", err)
    }
    RunToolCallEventTestWithContext(ctx, llm, modelID, testEmitter)
    return true, ""
})
```

**Benefits of the Registry System:**
- ✅ **Automatic Discovery** - Test suite automatically finds all registered tests
- ✅ **Easy to Extend** - Just add a `registerTest()` call
- ✅ **No Switch Statements** - Clean, maintainable code
- ✅ **Type Safety** - Function signature ensures consistency
- ✅ **Provider Support** - Can handle provider-specific initialization logic

The test suite will automatically discover and run any recorded test files matching your test name.

### Step 5: Record and Test

```bash
# Record the test
./bin/llm-test my-new-test --model gemini-2.5-flash --record

# Replay the test
./bin/llm-test my-new-test --model gemini-2.5-flash --replay

# Run in test suite
./bin/llm-test test-suite
```

---

## Performance

### Zero Production Overhead

The recording system is **production-safe** with zero performance impact when disabled:

- **When recording is disabled** (normal production use):
  - Only a context lookup (~1ns) and nil check (~0.5ns)
  - **Total overhead: <2ns per request**
  - No JSON marshaling, no file I/O, no request info building

- **When recording is enabled** (testing only):
  - JSON marshaling per event (~1-10µs per event)
  - File write at end (~1-5ms, non-blocking)
  - Acceptable overhead for test scenarios

**All providers** (Vertex, OpenAI, Bedrock) are optimized the same way - `buildRequestInfo()` and recording logic only execute when recording/replay is explicitly enabled.

---

## Best Practices

### 1. Test Naming

- Use descriptive test names: `tool_call`, `plain_text`, `structured_output`
- Keep names consistent across providers
- Use snake_case for test names

### 2. Recording Strategy

- **Record once** for each model/test combination
- **Re-record** when:
  - Test logic changes
  - Expected behavior changes
  - New models are added
- **Don't re-record** for every code change (unless behavior changes)

### 3. Request Matching

- The system automatically matches requests by hash
- Ensure your test uses consistent inputs
- Avoid random values in test prompts (they'll create different hashes)

### 4. Assertions

- Add comprehensive assertions in test functions
- Assertions run in both live and replay modes
- Validate:
  - Response structure
  - Required fields
  - Expected values
  - Error conditions

### 5. Test Data Management

- Keep `testdata/` in version control
- Use descriptive filenames (automatic)
- Organize by provider: `testdata/{provider}/`
- Don't commit sensitive data (API keys, etc.)

### 6. Test Suite Usage

- Run `test-suite` regularly (CI/CD)
- Use `--verbose` for debugging
- Monitor success rates
- Add new tests as features are added
- All registered tests are automatically discovered and run
- Register new test types in `initTestRegistry()` to include them in the suite

---

## Troubleshooting

### Test Not Found During Replay

**Problem:** `no matching recorded response found`

**Solutions:**
1. Ensure you've recorded the test first: `--record`
2. Check the test name matches: `TestName` in config
3. Verify the model ID matches
4. Check request hash matches (same messages/options)

### Wrong Response Returned

**Problem:** Replay returns different response than expected

**Solutions:**
1. Request hash mismatch - check messages/options are identical
2. Multiple recordings for same request - system uses most recent
3. Re-record the test if request changed

### Test Suite Not Finding Tests

**Problem:** Test suite shows 0 tests

**Solutions:**
1. Check `testdata/` directory exists
2. Verify JSON files are valid
3. Check file naming matches expected pattern
4. Ensure `provider`, `test_name`, `model_id` fields are set
5. Verify the test type is registered in `initTestRegistry()` (check output for "Registered test types")

**Problem:** Test suite shows "Unknown test type" error

**Solutions:**
1. Check if the test name matches a registered test type
2. Register the test type in `initTestRegistry()` function
3. Verify the `TestName` in your test command matches the registered name

### Recording Not Working

**Problem:** No files created in `testdata/`

**Solutions:**
1. Check `--record` flag is set
2. Verify recorder is in context (check logs)
3. Ensure directory is writable
4. Check API key is valid (for initial recording)

---

## Advanced Usage

### Custom Test Directory

Use different directories for different test suites:

```bash
./bin/llm-test vertex-tool-call --record --test-dir testdata/regression
./bin/llm-test test-suite --test-dir testdata/regression
```

### Multiple Models

Record tests for multiple models:

```bash
for model in gemini-2.5-flash gemini-2.5-pro gemini-3-pro-preview; do
    ./bin/llm-test vertex-tool-call --model $model --record
done
```

### CI/CD Integration

Add to your CI pipeline:

```yaml
# .github/workflows/test.yml
- name: Run Test Suite
  run: |
    cd llm-providers
    ./bin/llm-test test-suite
```

---

## Implementation Details

### Recorder Integration

The recorder is injected via `context.Context`:

```go
ctx := recorder.WithRecorder(ctx, rec)
llmInstance.GenerateContent(ctx, messages, ...)
```

The adapter checks for recorder in context:

```go
rec, found := recorder.FromContext(ctx)
if rec != nil && rec.IsRecordingEnabled() {
    // Record response
}
if rec != nil && rec.IsReplayEnabled() {
    // Load and replay response
}
```

### Request Hashing

Requests are hashed using SHA256 of canonical JSON:

```go
hashableRequest := struct {
    Messages []llmtypes.MessageContent `json:"messages"`
    ModelID  string                    `json:"model_id"`
    Options  llmtypes.CallOptions      `json:"options"`
}{
    Messages: request.Messages,
    ModelID:  request.ModelID,
    Options:  request.Options,
}
jsonBytes, _ := json.Marshal(hashableRequest)
hash := sha256.Sum256(jsonBytes)
```

### Streaming Responses

For streaming responses (e.g., Vertex AI), chunks are recorded:

```go
var recordedChunks []interface{}
for response := range stream {
    if rec.IsRecordingEnabled() {
        recordedChunks = append(recordedChunks, response)
    }
    // Process response
}
if rec.IsRecordingEnabled() {
    rec.RecordVertexChunks(recordedChunks, requestInfo)
}
```

---

## Examples

### Complete Workflow

```bash
# 1. Record tests for multiple models (Vertex)
./bin/llm-test vertex-tool-call --model gemini-2.5-flash --record
./bin/llm-test vertex-tool-call --model gemini-2.5-pro --record
./bin/llm-test vertex-tool-call --model gemini-3-pro-preview --record

# 1. Record tests for multiple models (OpenAI)
./bin/llm-test openai-tool-call --model gpt-4o-mini --record
./bin/llm-test openai-tool-call --model gpt-5.1 --record

# 1. Record tests for multiple models (Bedrock)
./bin/llm-test llm-tool-call --model global.anthropic.claude-sonnet-4-5-20250929-v1:0 --record

# 2. Verify recordings
ls -lh testdata/vertex/
ls -lh testdata/openai/
ls -lh testdata/bedrock/

# 3. Test replay
./bin/llm-test vertex-tool-call --model gemini-2.5-flash --replay
./bin/llm-test openai-tool-call --model gpt-5.1 --replay
./bin/llm-test llm-tool-call --model global.anthropic.claude-sonnet-4-5-20250929-v1:0 --replay

# 4. Run full test suite (includes all providers)
./bin/llm-test test-suite

# 5. Check results
./bin/llm-test test-suite --verbose
```

### Adding a New Test Type

```bash
# 1. Create test command (see "Adding New Tests" above)
# 2. Record initial test
./bin/llm-test my-new-test --model gemini-2.5-flash --record

# 3. Verify it works
./bin/llm-test my-new-test --model gemini-2.5-flash --replay

# 4. Add to test suite (optional)
# 5. Run test suite
./bin/llm-test test-suite
```

---

## Summary

The recording/replay testing system provides:

- ✅ **Fast execution** - No API calls during test runs (~200-500µs per test)
- ✅ **Cost-effective** - Record once, replay many times
- ✅ **Deterministic** - Consistent results across runs
- ✅ **Comprehensive** - Full response validation
- ✅ **Automatic** - Test suite discovers all tests via flexible registry system
- ✅ **Maintainable** - Easy to add new tests (just register them)
- ✅ **Extensible** - Registry-based architecture for easy expansion
- ✅ **Multi-provider** - Supports Anthropic, Bedrock, OpenAI, OpenRouter, and Vertex (Gemini)
- ✅ **Production-safe** - Zero performance overhead when disabled (<2ns per request)
- ✅ **Complete Coverage** - Supports plain text, tool calls, events, and token usage tests

**Test Registry System:**
- All test types are registered in `initTestRegistry()`
- New tests are automatically discovered when registered
- No need to modify switch statements or core test logic
- Type-safe function signatures ensure consistency

Use this system to build a robust test suite for your LLM provider implementations!

