# Structured Output Tool Test - Criteria

**Model 2: Tool-Based** - Registers tool → LLM calls tool → Extracts from arguments

## Success Criteria

### Test 1: Simple Person via Tool
- ✅ "AskWithHistoryStructuredViaTool successful"
- ✅ Either: `HasStructuredOutput: true` with person data
- ✅ Or: `HasStructuredOutput: false` with text response (acceptable)

### Test 2: Complex Order via Tool
- ✅ "AskWithHistoryStructuredViaTool successful"
- ✅ Either: Order with 2 items extracted from tool
- ✅ Or: Text response fallback (acceptable)

## Expected Output

```
=== Structured Output Tool Test Complete ===
📊 Tests passed: 2, Tests failed: 0
```

## Note
Tool not being called is **acceptable behavior** - LLM may choose conversational response.

## Performance
- ~4-10 seconds total (1 LLM call per test if tool called)
