# Structured Output Conversion Test - Criteria

**Model 1: Text Conversion** - Gets text response → Converts to JSON via second LLM call

## Success Criteria

### Test 1: Simple Person
- ✅ "AskStructured successful"
- ✅ Person has name, age, email populated
- ✅ Valid JSON output

### Test 2: TodoList with History
- ✅ "AskWithHistoryStructured successful"
- ✅ TodoList has 3 tasks
- ✅ Each task has id, title, status, priority
- ✅ Message history updated

### Test 3: Complex Project
- ✅ "Complex nested structure test successful"
- ✅ Project has 3 members, 4 milestones
- ✅ All nested fields populated
- ✅ Valid nested JSON

## Expected Output

```
=== Structured Output Conversion Test Complete ===
📊 Tests passed: 3, Tests failed: 0
```

## Performance
- ~12-22 seconds total (2 LLM calls per test)
