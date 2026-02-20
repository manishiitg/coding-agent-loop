# Large Tool Output Test - Log Analysis Criteria

This test validates the large tool output handling feature. **These tests don't use traditional asserts** - instead, logs are analyzed (manually or by LLM) to verify test success.

## Running the Test

```bash
# Basic run (logs to stdout, uses OpenAI by default)
mcpagent-test test large-tool-output

# With log file (recommended for analysis)
mcpagent-test test large-tool-output --log-file logs/large-tool-output-test.log

# With custom threshold (default: 1000 tokens)
mcpagent-test test large-tool-output --threshold 2000 --log-file logs/large-tool-output-test.log

# Test with text output instead of JSON
mcpagent-test test large-tool-output --output-type text --log-file logs/large-tool-output-test.log

# Custom output size
mcpagent-test test large-tool-output --output-size 5000 --log-file logs/large-tool-output-test.log

# With debug logging
mcpagent-test test large-tool-output --log-level debug --log-file logs/large-tool-output-test.log
```

## What This Test Does

1. **Creates a custom tool** (`generate_large_output`) that can generate large JSON or text output
2. **Sets a lower threshold** (default: 1000 tokens) for testing large output detection
3. **Tests large output detection**: Calls the tool with output that exceeds the threshold
4. **Verifies file writing**: Checks that large outputs are written to `tool_output_folder`
5. **Tests virtual tools**: Tests `read_large_output`, `search_large_output`, and `query_large_output`

## Test Scenarios

### Test 1: Large JSON Output
- Agent is asked to call `generate_large_output` with JSON type
- Output size exceeds the threshold
- **Expected**: Output is written to a file, agent receives file message with preview

### Test 2: Large Text Output
- Agent is asked to call `generate_large_output` with text type
- Output size exceeds the threshold
- **Expected**: Output is written to a file, agent receives file message with preview

### Test 3: Virtual Tool - read_large_output
- Agent is asked to read from the large output file
- **Expected**: Virtual tool successfully reads the specified character range

### Test 4: Virtual Tool - search_large_output (JSON only)
- Agent is asked to search for patterns in the JSON file
- **Expected**: Virtual tool successfully searches and returns matches

### Test 5: Virtual Tool - query_large_output (JSON only)
- Agent is asked to execute a jq query on the JSON file
- **Expected**: Virtual tool successfully executes the query and returns results

## Log Analysis Criteria

### ✅ Success Indicators

#### 1. Tool Registration
Look for:
```
✅ Registered custom tool: generate_large_output
🔧 Registered custom tool tool=generate_large_output category=custom
```

#### 2. Large Output Detection
Look for event logs indicating large output was detected:
```
[LargeToolOutputDetectedEvent] or similar event logs
```

Key fields to check:
- `tool`: Should be `generate_large_output`
- `size`: Should be greater than the threshold
- `output_folder`: Should be `tool_output_folder` or similar

#### 3. File Writing
Look for event logs indicating file was written:
```
[LargeToolOutputFileWrittenEvent] or similar event logs
```

Key fields to check:
- `file_path`: Path to the written file (e.g., `tool_output_folder/tool_YYYYMMDD_HHMMSS_generate_large_output.json`)
- `size`: Size of the written content
- `preview`: First 100 characters of the content

#### 4. File Message Creation
In the agent's response, look for:
- File path mentioned in the response
- Preview content (first 50% of threshold characters)
- Instructions about using virtual tools
- References to `read_large_output`, `search_large_output`, `query_large_output`

Example response should contain:
```
The tool output was too large and has been saved to: tool_output_folder/...
FIRST X CHARACTERS OF OUTPUT (50% of threshold):
[preview content here]
...
Make sure to use the virtual tools next to read contents of this file...
```

#### 5. Virtual Tools Execution
For `read_large_output`:
- Look for successful file reads
- Check that the correct character range was read
- Verify the content matches the original file

For `search_large_output` (JSON only):
- Look for search results
- Verify pattern matching works
- Check that results are formatted correctly

For `query_large_output` (JSON only):
- Look for jq query execution
- Verify query results are correct
- Check JSON parsing

#### 6. File System Verification
Check that files were actually created:
- Look in `tool_output_folder/` directory
- Files should have format: `tool_YYYYMMDD_HHMMSS_generate_large_output.{json|txt}`
- File content should match the generated output

### ⚠️ Warning Indicators

1. **No file written**: If large output was detected but file write failed
   - Look for `LargeToolOutputFileWriteErrorEvent`
   - Check for permission errors or disk space issues

2. **Virtual tools not available**: If agent tries to use virtual tools but they're not registered
   - Check that `EnableContextOffloading` is true
   - Verify virtual tools are in the agent's tool list

3. **Threshold not exceeded**: If output size is below threshold
   - Verify the threshold setting
   - Check the actual output size in logs

### ❌ Failure Indicators

1. **Tool not registered**: Custom tool registration failed
   - Check for registration errors
   - Verify category is set correctly

2. **Large output not detected**: Output exceeds threshold but wasn't detected
   - Check threshold configuration
   - Verify `toolOutputHandler` is initialized
   - Check token counting logic

3. **File write errors**: File creation or writing failed
   - Check file permissions
   - Verify directory exists
   - Check disk space

4. **Virtual tools fail**: Virtual tools return errors
   - Check file path resolution
   - Verify file exists
   - Check tool parameter validation

## Expected Log Patterns

### Successful Large Output Detection
```
✅ Set large output threshold threshold=1000
✅ Registered custom tool: generate_large_output
[LargeToolOutputDetectedEvent] tool=generate_large_output size=2000
[LargeToolOutputFileWrittenEvent] file_path=tool_output_folder/... size=2000
```

### Successful Virtual Tool Usage
```
[read_large_output] filename=... start=1 end=200
[search_large_output] filename=... pattern=...
[query_large_output] filename=... query=...
```

## Troubleshooting

### Issue: No files created in tool_output_folder
- **Check**: Verify `toolOutputHandler` is initialized
- **Check**: Verify threshold is set correctly
- **Check**: Check file permissions on `tool_output_folder`

### Issue: Virtual tools not found
- **Check**: Verify `EnableContextOffloading` is true
- **Check**: Check agent's tool list includes virtual tools
- **Check**: Verify virtual tools are registered in the agent

### Issue: Token counting seems incorrect
- **Check**: Verify model ID is set correctly (affects token counting)
- **Check**: Check token encoding (should use o200k_base)
- **Check**: Compare character count vs token count

### Issue: File path issues with virtual tools
- **Check**: Verify session ID handling
- **Check**: Check path resolution logic
- **Check**: Verify file path format (full path vs filename)

## Test Configuration

### Threshold
- **Default**: 1000 tokens
- **Purpose**: Lower than production (20000) to make testing easier
- **Adjust**: Use `--threshold` flag to change

### Output Size
- **Default**: 2x threshold (e.g., 2000 for threshold 1000)
- **Purpose**: Ensure output exceeds threshold
- **Adjust**: Use `--output-size` flag to change

### Output Type
- **Options**: `json` or `text`
- **Default**: `json`
- **Purpose**: Test both JSON and text handling
- **Adjust**: Use `--output-type` flag to change

## Files to Check

After running the test, check:
1. **Log file** (if `--log-file` was used): Contains all test execution logs
2. **tool_output_folder/**: Contains generated large output files
3. **tool_output_folder/{trace-id}/**: If session ID is used, files may be in subdirectory

## Success Criteria Summary

✅ **Test passes if**:
1. Custom tool is registered successfully
2. Large output is detected when threshold is exceeded
3. Files are written to `tool_output_folder`
4. Agent receives file messages with previews
5. Virtual tools can read/search/query the files
6. All events are logged correctly

❌ **Test fails if**:
1. Tool registration fails
2. Large output is not detected
3. Files are not written
4. Virtual tools fail to execute
5. File paths are incorrect
6. Token counting is incorrect

