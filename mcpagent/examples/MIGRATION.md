# Example Migration Plan

This document tracks examples that need to be migrated from `external_example/` to `mcpagent/examples/`.

## Current Examples in mcpagent/examples/

✅ **basic/** - Basic agent usage with OpenAI LLM  
✅ **multi-turn/** - Multi-turn conversations with history

## Pending Migrations from external_example/

### High Priority

1. **structured-output/** (from `external_example/structured_output_test/`)
   - Demonstrates `AskStructured` function usage
   - Shows JSON schema validation
   - Type-safe structured responses
   - **Status**: Pending

2. **events/** (from `external_example/agent_with_events/`)
   - Event listener implementation
   - Event types and handling
   - Observability patterns
   - **Status**: Pending

3. **custom-logging/** (from `external_example/custom_logging/`)
   - Custom logger implementation
   - Logger adapter pattern
   - Production logging setup
   - **Status**: Pending

### Medium Priority

4. **langfuse/** (from `external_example/langfuse_test/`)
   - Langfuse observability integration
   - Tracing setup
   - Production monitoring
   - **Status**: Pending

5. **agent-modes/** (from `external_example/agent_modes/`)
   - Simple vs ReAct agent comparison
   - When to use each mode
   - Performance differences
   - **Status**: Pending

### Low Priority / Skip

6. **api/** (from `external_example/api/`)
   - SSE API server implementation
   - Too complex for simple examples
   - Consider separate project/repo
   - **Status**: Skip (too complex)

## Migration Notes

When migrating examples:
- Replace `external` package imports with `mcpagent` directly
- Update LLM initialization to use `llm.InitializeLLM()` directly
- Replace builder pattern with `mcpagent.NewAgent()` + functional options
- Update event types from `external.AgentEvent` to `events.AgentEvent`
- Update logger interfaces from `utils.ExtendedLogger` to `loggerv2.Logger`
- Test all examples after migration

## Migration Checklist

- [ ] structured-output example
- [ ] events example
- [ ] custom-logging example
- [ ] langfuse example (optional)
- [ ] agent-modes example (optional)

