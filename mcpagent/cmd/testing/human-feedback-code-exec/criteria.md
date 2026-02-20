# Human Feedback Code Execution Test - Log Analysis Criteria

This test validates that the `human_feedback` tool is available as a normal tool in code execution mode, even though other custom tools are excluded. **These tests don't use traditional asserts** - instead, logs are analyzed (manually or by LLM) to verify test success.

## Running the Test

```bash
# Basic test (uses OpenAI by default, gpt-4.1)
mcpagent-test test human-feedback-code-exec --log-file logs/human-feedback-code-exec-test.log

# With specific model
mcpagent-test test human-feedback-code-exec --model gpt-4.1 --log-file logs/human-feedback-code-exec-test.log
```

## What This Test Does

1. **Creates agent in code execution mode**: Verifies initial state with only code execution virtual tools (`discover_code_files`, `write_code`)
2. **Registers regular custom tool**: Tests that regular custom tools are excluded in code exec mode
3. **Registers human_feedback tool**: Tests that human tools (category "human") are available in code exec mode
4. **Verifies tool availability**: Checks that human_feedback is in Tools array while regular tools are not
5. **Verifies tool counts**: Ensures only expected tools are present (3 total: 2 code exec + 1 human)

## Background

In code execution mode, most tools are excluded from direct LLM access. However, tools with category "human" (like `human_feedback`) are an exception and remain available because they require event bridge access for frontend UI and cannot work via generated code.

## Log Analysis Criteria

### Success Indicators

- `=== Human Feedback Code Execution Test ===`
- `✅ Agent created in code execution mode`
- `Initial tools in code execution mode count=2` (should be 2: `discover_code_files`, `write_code`)
- `✅ Regular custom tool correctly excluded from Tools array in code exec mode`
- `✅ Human feedback tool registered with category 'human'`
- `✅ Human feedback tool found in Tools array - correctly available in code exec mode!`
- `Final tools in code execution mode count=3` (should be 3: 2 code exec + 1 human)
- `Tool breakdown code_exec_tools=2 human_tools=1 other_tools=0 total=3`
- `human_feedback_available=true regular_tool_excluded=true` in summary

### Failure Indicators

- `⚠️  Regular custom tool found in Tools array` (should be excluded)
- `⚠️  Human feedback tool NOT found in Tools array` (should be available)
- Wrong tool counts (expected: 2 initial, 3 final)
- Missing expected log messages

### Troubleshooting

- **Human feedback not found**: Check that tool is registered with category "human" (not "custom" or other)
- **Regular tool found**: This is a bug - regular tools should be excluded in code exec mode
- **Wrong tool count**: Check for other virtual tools that may be present (usually normal)

## Expected Behavior

1. **Initial state**: Only 2 code execution virtual tools (`discover_code_files`, `write_code`)
2. **After regular tool registration**: Still 2 tools (regular tool excluded)
3. **After human tool registration**: 3 tools total (2 code exec + 1 human tool)
4. **Final verification**: `human_feedback` available, regular tool excluded
