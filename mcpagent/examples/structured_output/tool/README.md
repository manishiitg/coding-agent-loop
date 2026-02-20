# Structured Output as Tool Example

This example demonstrates **Model 2: Tool-Based Model** for structured output using `AskWithHistoryStructuredViaTool`.

## How It Works

1. Dynamically registers a custom tool with the schema as parameters
2. LLM calls the tool during conversation
3. Structured data is extracted directly from tool call arguments
4. Conversation breaks immediately when tool is called

## Methods Demonstrated

- `AskWithHistoryStructuredViaTool[T](agent, ctx, messages, toolName, toolDescription, schema) (StructuredOutputResult[T], error)`

## Result Type

```go
type StructuredOutputResult[T any] struct {
    HasStructuredOutput bool   // true if tool was called
    StructuredResult    T      // structured data from tool arguments
    TextResponse        string // text response if tool not called
    Messages            []MessageContent
}
```

## Pros & Cons

**Pros:**
- ✅ **Single LLM call** - Faster and cheaper
- ✅ **Preserves context** - Structured data from conversation
- ✅ **More efficient** - No conversion step needed

**Cons:**
- ⚠️ **LLM may not call tool** - Graceful fallback to text response
- ⚠️ **Less predictable** - Depends on LLM cooperation

## Setup

1. Create `.env` file with your OpenAI API key:
   ```
   OPENAI_API_KEY=your-api-key-here
   ```

2. Ensure Docker is running (for the fetch MCP server)

3. Run from the example directory:
   ```bash
   cd examples/structured_output/tool
   go run main.go
   ```

## Usage

```bash
# Default configuration
go run main.go

# Custom MCP server config
go run main.go mcp_servers.json
```

## Examples Included

### Example 1: Simple Person Profile via Tool
- Registers `submit_person_profile` tool
- Extracts person data from tool call arguments
- Shows graceful fallback if tool is not called

### Example 2: Complex Order via Tool
- Registers `submit_order` tool with nested items
- Extracts order data from tool call arguments
- Demonstrates complex nested structures via tool

## Configuration

The `mcp_servers.json` file configures the fetch MCP server:

```json
{
  "mcpServers": {
    "fetch": {
      "command": "docker",
      "args": ["run", "-i", "--rm", "mcp/fetch"]
    }
  }
}
```

## Requirements

- Go 1.24.4+
- Docker (for fetch MCP server)
- OpenAI API key in `.env` file

## When to Use This Model

Use the **Tool-Based Model** when:
- ✅ You need **fast response times**
- ✅ Cost efficiency is important (high-volume APIs)
- ✅ Interactive applications (chatbots, UIs)
- ✅ LLM cooperation is likely (clear instructions)
- ✅ You can handle graceful fallback to text

## Output

The example will:
1. Attempt to extract person profile from tool call
2. Attempt to extract order data from tool call
3. Show structured output if tool was called, or text response if not

Note: This method uses **1 LLM call** per structured output request (if tool is called).

## Important Notes

- **Tool not called is NOT an error** - The LLM may choose a conversational response instead
- Make instructions explicit about using the tool (e.g., "Use the submit_person_profile tool")
- The tool is registered with category "structured_output" so it's always available
- Conversation breaks immediately when tool is called to extract structured data

