# Structured Output Tests

This directory contains tests for the agent's structured output generation capabilities. The agent supports **two different models** for generating structured output from LLM responses.

## Two Models for Structured Output

### Model 1: Text Conversion Model (`conversion/`)

**Location**: `structured-output/conversion/`  
**Command**: `mcpagent-test test structured-output-conversion`

**How it works:**
1. Agent gets a text response using `AskWithHistory`
2. Text response is converted to JSON using a **second LLM call** with JSON mode
3. JSON is parsed into the target struct

**Methods:**
- `AskStructured[T](agent, ctx, question, schema, schemaString) (T, error)`
- `AskWithHistoryStructured[T](agent, ctx, messages, schema, schemaString) (T, []MessageContent, error)`

**Pros:**
- ✅ **Always works** - Reliable fallback to text
- ✅ **Better for complex schemas** - Explicit conversion step
- ✅ **More predictable** - Guaranteed structured output

**Cons:**
- ❌ **2 LLM calls** - Slower and more expensive
- ❌ **May lose context** - Conversion is separate from conversation

**Use cases:**
- Critical applications requiring guaranteed structured output
- Complex nested schemas
- Batch processing where reliability > speed

---

### Model 2: Tool-Based Model (`tool/`)

**Location**: `structured-output/tool/`  
**Command**: `mcpagent-test test structured-output-tool`

**How it works:**
1. Dynamically registers a custom tool with the schema as parameters
2. LLM calls the tool during conversation
3. Structured data is extracted directly from tool call arguments
4. Conversation breaks immediately when tool is called

**Methods:**
- `AskWithHistoryStructuredViaTool[T](agent, ctx, messages, toolName, toolDescription, schema) (StructuredOutputResult[T], error)`

**Result type:**
```go
type StructuredOutputResult[T any] struct {
    HasStructuredOutput bool   // true if tool was called
    StructuredResult    T      // structured data from tool arguments
    TextResponse        string // text response if tool not called
    Messages            []MessageContent
}
```

**Pros:**
- ✅ **Single LLM call** - Faster and cheaper
- ✅ **Preserves context** - Structured data from conversation
- ✅ **More efficient** - No conversion step needed

**Cons:**
- ⚠️ **LLM may not call tool** - Graceful fallback to text response
- ⚠️ **Less predictable** - Depends on LLM cooperation

**Use cases:**
- Interactive applications where speed matters
- High-volume APIs where cost is a concern
- When LLM cooperation is likely (clear instructions)

---

## Quick Comparison

| Aspect | Model 1 (Conversion) | Model 2 (Tool) |
|--------|---------------------|----------------|
| **LLM Calls** | 2 (text + conversion) | 1 (tool call) |
| **Reliability** | ✅ Always works | ⚠️ May not call tool |
| **Speed** | ❌ Slower | ✅ Faster |
| **Cost** | ❌ Higher (2x calls) | ✅ Lower (1 call) |
| **Context** | ⚠️ May lose | ✅ Preserved |
| **Complexity** | Simple API | Requires tool registration |
| **Best For** | Critical/complex | Interactive/efficient |

---

## Running the Tests

**Important:** Always run tests with `--log-file` to avoid cluttering terminal output. Tests use OpenAI by default.

### Test Model 1 (Text Conversion)

```bash
# Run from mcpagent directory (where .env file is located)
cd /path/to/mcpagent
./cmd/testing/mcpagent-test structured-output-conversion --log-file logs/conversion-test.log

# The test will:
# - Load .env file automatically
# - Use OpenAI provider (default)
# - Log all output to logs/conversion-test.log
# - Show no terminal output (clean)
```

**Tests:**
1. Simple Person struct (AskStructured)
2. TodoList with conversation history (AskWithHistoryStructured)
3. Complex Project with nested arrays (AskStructured)

---

### Test Model 2 (Tool-Based)

```bash
# Run from mcpagent directory
cd /path/to/mcpagent
./cmd/testing/mcpagent-test structured-output-tool --log-file logs/tool-test.log
```

**Tests:**
1. Simple Person via submit_person_profile tool
2. Complex Order with nested items via submit_order tool
3. Tool not called scenario (graceful fallback)

---

## Test Structure

```
structured-output/
├── README.md                          # This file - overview of both models
├── conversion/                        # Model 1: Text Conversion Model
│   ├── structured-output-conversion-test.go
│   └── criteria.md                    # Log analysis criteria for Model 1
└── tool/                              # Model 2: Tool-Based Model
    ├── structured-output-tool-test.go
    └── criteria.md                    # Log analysis criteria for Model 2
```

---

## Which Model Should I Use?

### Use Model 1 (Text Conversion) when:
- ✅ You need **guaranteed structured output**
- ✅ Working with **complex nested schemas**
- ✅ Reliability is more important than speed
- ✅ Cost is not a primary concern
- ✅ Batch processing or background jobs

### Use Model 2 (Tool-Based) when:
- ✅ You need **fast response times**
- ✅ Cost efficiency is important (high-volume APIs)
- ✅ Interactive applications (chatbots, UIs)
- ✅ LLM cooperation is likely (clear instructions)
- ✅ You can handle graceful fallback to text

### Example: When to use each

**E-commerce checkout (critical)** → Use Model 1
- Need guaranteed order data
- Complex nested structure (items, shipping, payment)
- Can't afford to miss structured data

**Chatbot feedback collection (interactive)** → Use Model 2
- Fast response needed
- Simple schema (rating, comments)
- Can fall back to text if needed

**Financial report generation (complex)** → Use Model 1
- Complex nested metrics
- Reliability critical
- Batch processing acceptable

**User profile updates (simple)** → Use Model 2
- Simple schema (name, email, age)
- Interactive UI
- Fast response needed

---

## Implementation Examples

### Model 1: Text Conversion

```go
// Simple struct
type Person struct {
    Name  string `json:"name"`
    Age   int    `json:"age"`
    Email string `json:"email"`
}

schema := `{
    "type": "object",
    "properties": {
        "name": {"type": "string"},
        "age": {"type": "integer"},
        "email": {"type": "string"}
    },
    "required": ["name", "age", "email"]
}`

// Single question
person, err := mcpagent.AskStructured(
    agent, ctx,
    "Create a profile for John Doe, age 30, email john@example.com",
    Person{}, schema,
)

// With conversation history
person, messages, err := mcpagent.AskWithHistoryStructured(
    agent, ctx, conversationHistory,
    Person{}, schema,
)
```

### Model 2: Tool-Based

```go
// Same Person struct and schema

messages := []llmtypes.MessageContent{
    {
        Role: llmtypes.ChatMessageTypeHuman,
        Parts: []llmtypes.ContentPart{
            llmtypes.TextContent{
                Text: "Submit profile for John Doe, age 30, email john@example.com. Use the submit_profile tool.",
            },
        },
    },
}

result, err := mcpagent.AskWithHistoryStructuredViaTool[Person](
    agent, ctx, messages,
    "submit_profile",
    "Submit a person profile",
    schema,
)

if result.HasStructuredOutput {
    // Tool was called - use structured data
    person := result.StructuredResult
    fmt.Printf("Name: %s, Age: %d\n", person.Name, person.Age)
} else {
    // Tool not called - use text response
    fmt.Printf("Response: %s\n", result.TextResponse)
}
```

---

## Log Analysis

Both tests use **log-based validation** rather than traditional asserts. After running tests, analyze the log files using the criteria documented in each test's `criteria.md` file.

**Model 1 criteria**: `structured-output/conversion/criteria.md`  
**Model 2 criteria**: `structured-output/tool/criteria.md`

---

## Performance Expectations

### Model 1 (Text Conversion)
- **Simple struct**: ~3-5 seconds (2 LLM calls)
- **Nested struct**: ~4-7 seconds (2 LLM calls)
- **Complex nested**: ~5-10 seconds (2 LLM calls)
- **Total LLM calls**: 6 calls across 3 tests

### Model 2 (Tool-Based)
- **Simple tool call**: ~2-4 seconds (1 LLM call if tool called)
- **Complex tool call**: ~3-6 seconds (1 LLM call if tool called)
- **Tool not called**: ~2-3 seconds (1 LLM call)
- **Total LLM calls**: 3 calls across 3 tests (if all tools called)

**Cost comparison**: Model 2 is ~50% cheaper (half the LLM calls)

---

## Common Issues

### Model 1 Issues

**"failed to convert to structured output"**
- LLM didn't return valid JSON in conversion step
- Check debug logs for "JSON validation failed"
- Try with different LLM provider

**Empty fields in output**
- Schema too complex for conversion
- Increase max_tokens
- Simplify schema

### Model 2 Issues

**"Tool was not called"**
- This is **not an error** - it's acceptable behavior
- LLM chose conversational response
- Make instructions more explicit
- Check if prompt clearly mentions the tool

**Tool registered but not found**
- Check tool registration logs
- Verify category is "structured_output"
- Check agent's custom tools list

---

## Next Steps

1. **Run both tests** to understand the differences
2. **Review criteria.md** files for detailed log analysis
3. **Choose the right model** for your use case
4. **Implement in your application** using the examples above

For questions or issues, see the troubleshooting sections in each test's `criteria.md` file.
