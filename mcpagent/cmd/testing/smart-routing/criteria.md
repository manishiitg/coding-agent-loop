# Smart Routing Test - Log Analysis Criteria

This test validates the agent's smart routing feature that filters tools based on conversation context. **These tests don't use traditional asserts** - instead, logs are analyzed (manually or by LLM) to verify test success.

## Running the Test

```bash
# Basic test (uses OpenAI by default, thresholds: 5 tools, 2 servers)
mcpagent-test test smart-routing --log-file logs/smart-routing-test.log

# Custom thresholds
mcpagent-test test smart-routing --max-tools-threshold 10 --max-servers-threshold 3 --log-file logs/smart-routing-test.log

# Custom smart routing config
mcpagent-test test smart-routing --temperature 0.1 --max-tokens 1000 --log-file logs/smart-routing-test.log

# With debug logging
mcpagent-test test smart-routing --log-level debug --log-file logs/smart-routing-test.log
```

## What This Test Does

1. **Agent Setup**: Creates an agent with multiple MCP servers (sequential-thinking, context7, aws-pricing) to exceed smart routing thresholds.
2. **Smart Routing Configuration**: Enables smart routing with configurable thresholds (default: 5 tools, 2 servers).
3. **Threshold Check**: Verifies that smart routing eligibility is correctly determined based on tool count and server count.
4. **Scenario 1 - AWS-Focused Question**: Asks a question about AWS EC2 pricing to test if smart routing filters tools to relevant servers (aws-pricing).
5. **Scenario 2 - Multi-Turn Conversation**: Tests smart routing with a multi-turn conversation to verify context building and dynamic tool filtering.
6. **Event Verification**: Checks logs for smart routing events (start, end, token usage).

## Log Analysis Criteria

To verify test success, check the log file (`logs/smart-routing-test.log`) for the following:

### Success Indicators

- `=== Smart Routing Test ===`
- `--- Test: Smart Routing Feature ---`
- `✅ LLM initialized` (check `model_id`)
- `✅ Created temporary MCP config` (check `path` and `server_count` - should be 3+)
- `✅ Smart routing thresholds set` (check `max_tools` and `max_servers`)
- `✅ Smart routing config set` (check `temperature` and `max_tokens`)
- `Smart routing eligibility check` (check `should_use` - should be `true` if thresholds exceeded)
- `--- Scenario 1: Testing smart routing with AWS-focused question ---`
- `Running agent with question to generate large JSON output...`
- `✅ Agent executed successfully` (check `response_preview`, `duration`, `response_length`)
- `Tool filtering results` (check `total_tools` vs `filtered_tools` - filtered should be less if smart routing active)
- `--- Scenario 2: Testing smart routing with multi-turn conversation ---`
- `Turn 1: AWS question` and `✅ Turn 1 completed`
- `Turn 2: Sequential thinking question` and `✅ Turn 2 completed`
- `--- Scenario 3: Verifying smart routing behavior ---`
- `✅ Smart routing test scenarios completed`
- Look for `[SMART_ROUTING_START]` event with:
  - `total_tools` count
  - `server_count`
  - `max_tools_threshold`
  - `max_servers_threshold`
  - `llm_prompt` (server selection prompt)
  - `user_query` (conversation context)
- Look for `[SMART_ROUTING_END]` event with:
  - `filtered_tools_count` (should be less than `total_tools` if routing successful)
  - `selected_servers` (array of relevant server names)
  - `reasoning` (explanation of server selection)
  - `success` (should be `true`)
  - `llm_response` (raw LLM response)
- Look for `[TOKEN_USAGE]` events with `operation=smart_routing`
- Look for `🔄 Rebuilding system prompt with filtered servers` (system prompt should be rebuilt after routing)
- `✅ System prompt rebuilt with filtered servers` (check `filtered_prompts_count`, `filtered_resources_count`)

### Failure Indicators

- `❌ Smart Routing test failed:`
- `failed to create temp MCP config:`
- `failed to initialize LLM:`
- `failed to create agent:`
- `agent execution failed:`
- `⚠️ Smart routing will not be used - thresholds not exceeded` (if thresholds too high)
- `Smart routing failed, using all tools` (fallback to all tools)
- `[SMART_ROUTING_END]` event with `success=false` and `error` field
- Missing expected smart routing events
- `filtered_tools_count` equals `total_tools` (routing didn't filter anything)

### Troubleshooting

- **Smart Routing Not Triggered**: If you see "Smart routing will not be used", the thresholds are too high. Lower `--max-tools-threshold` and `--max-servers-threshold` values (e.g., `--max-tools-threshold 3 --max-servers-threshold 1`).
- **No Tool Filtering**: If `filtered_tools_count` equals `total_tools`, smart routing may have selected all servers. This is expected behavior when the conversation context requires tools from multiple servers.
- **LLM Errors**: If smart routing LLM call fails, check for errors in `[SMART_ROUTING_END]` event. Ensure your OpenAI API key is valid and the model is available.
- **Missing Servers**: Ensure the required MCP servers (sequential-thinking, context7, aws-pricing) are available and properly configured in your environment.
- **System Prompt Rebuild Failures**: Check for warnings about system prompt rebuild failures. This doesn't fail the test but may affect tool availability.

### Expected Behavior

1. **Threshold Check**: Smart routing should be enabled when:
   - `EnableSmartRouting = true`
   - `total_tools > max_tools_threshold`
   - `server_count > max_servers_threshold`

2. **Tool Filtering**: After smart routing:
   - Tools from non-relevant servers should be filtered out
   - Custom tools (virtual tools) should always be included
   - Filtered tool count should be ≤ total tool count

3. **System Prompt**: System prompt should be rebuilt with only prompts/resources from relevant servers after smart routing completes.

4. **Events**: All smart routing events should be emitted with complete information for observability.

