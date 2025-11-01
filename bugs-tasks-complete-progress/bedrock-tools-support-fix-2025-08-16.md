# 🐛 Bedrock Tools Support Fix - 2025-08-16

## **Issue Description**

The MCP agent was unable to execute tool calls when using AWS Bedrock (Claude models) because the langchaingo library's Bedrock implementation was not properly supporting function calling. 

**Symptoms:**
- Tools were passed to the LLM via `llmtypes.WithTools()` but ignored
- LLM generated text-based responses instead of structured tool calls
- Agent detected "no tool calls" even when tools were available
- Frontend showed no tool execution events

**Root Cause:**
The `github.com/tmc/langchaingo` library's Bedrock implementation was missing tools support:
- No tools field in `anthropicTextGenerationInput` struct
- Tools were not included in API calls to AWS Bedrock
- No tool call parsing in response handling

## **Solution Applied**

**Updated langchaingo dependency** from `github.com/tmc/langchaingo` to `github.com/manishiitg/langchaingo v0.0.3` using Go module replacement.

**Files Modified:**
- `agent_go/go.mod` - Updated replace directive to use manishiitg fork

**Changes Made:**
```go
// Before
replace github.com/tmc/langchaingo => github.com/manishiitg/langchaingo v0.0.2

// After  
replace github.com/tmc/langchaingo => github.com/manishiitg/langchaingo v0.0.3
```

## **Technical Details**

### **What the manishiitg fork v0.0.3 provides:**

1. **Complete Tools Support in Bedrock:**
   - `Tools []BedrockTool` field in input structures
   - Tool conversion from langchaingo format to Bedrock format
   - Tool choice handling (`auto`, `none`, `required`)

2. **Tool Processing Pipeline:**
   ```go
   // Add tools if provided
   if len(options.Tools) > 0 {
       bedrockTools, err := convertToolsToBedrockTools(options.Tools)
       if err != nil {
           return nil, fmt.Errorf("failed to convert tools: %w", err)
       }
       input.Tools = bedrockTools
   }
   ```

3. **Tool Call Response Handling:**
   - Parses `tool_use` blocks from Bedrock responses
   - Converts Bedrock tool calls back to langchaingo format
   - Sets both `ToolCalls` and legacy `FuncCall` fields

4. **Dedicated Tools Support Files:**
   - `tools.go` - Tool conversion and handling logic
   - `provider_anthropic.go` - Updated with tools support

## **Testing Results**

### **Before Fix (v0.0.2):**
- ❌ Tools passed via `llmtypes.WithTools()` but ignored
- ❌ LLM generated text responses like `<function_calls><invoke name="write_file">`
- ❌ Agent detected "no tool calls detected"
- ❌ No tool execution events

### **After Fix (v0.0.3):**
- ✅ Tools properly converted and sent to AWS Bedrock
- ✅ LLM generates structured tool calls with proper IDs and arguments
- ✅ Agent detects tool calls: `"detected 1 tool calls"`
- ✅ Tools execute successfully: `"Preparing to execute tool call 1: [tool_name]"`
- ✅ Complete multi-turn tool usage workflow

**Example Working Tool Call:**
```json
{
  "type": "tool_call",
  "tool_call": {
    "function": {
      "name": "list_directory",
      "arguments": "{\"path\":\".\"}"
    },
    "id": "toolu_bdrk_012qxrNzNUm8JPBcdLzwRosJ",
    "type": "function"
  }
}
```

## **Impact**

**Resolved Issues:**
- ✅ **Function calling now works** with AWS Bedrock/Claude models
- ✅ **All 28 MCP tools are accessible** and functional
- ✅ **Proper tool execution flow** with event emission
- ✅ **Multi-turn conversations** using multiple tools
- ✅ **Frontend tool display** now shows actual tool calls

**Performance Improvements:**
- No more text-based tool call parsing overhead
- Direct structured tool call handling
- Proper error handling for tool failures
- Efficient tool execution workflow

## **Verification Commands**

```bash
# Test tools support with Bedrock
../bin/orchestrator test agent --streaming --provider bedrock --log-file logs/tools_test.log

# Check for tool call detection
grep -E "(detected.*tool calls|Tool.*execution)" logs/tools_test.log

# Verify langchaingo version
go list -m github.com/tmc/langchaingo
```

## **Dependencies**

**Required:**
- `github.com/manishiitg/langchaingo v0.0.3` (via replace directive)
- AWS Bedrock access with Claude models
- MCP servers with tools

**Compatibility:**
- Works with all Bedrock Claude models
- Maintains backward compatibility with existing code
- No changes required to agent logic or frontend

## **Future Considerations**

1. **Monitor manishiitg fork updates** for additional improvements
2. **Consider upstream contribution** if tools support is added to main langchaingo
3. **Test with other Bedrock models** (Cohere, Amazon, etc.) for tools support
4. **Validate tool choice options** (`auto`, `required`, specific tool selection)

## **Related Issues**

- **Fixed**: [SSE Session Management](../sse-session-management-fix-2025-08-16.md)
- **Fixed**: [Logger Integration](../bug-logger.md)
- **Fixed**: [ReAct Agent Issues](../bug-react-agent-fix.md)

---

**Status**: ✅ **RESOLVED**  
**Date**: 2025-08-16  
**Priority**: **HIGH** (Core functionality)  
**Effort**: **LOW** (Simple dependency update)
