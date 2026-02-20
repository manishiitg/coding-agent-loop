# Fixed Structured Output Example

This example demonstrates **Model 1: Text Conversion Model** for structured output using `AskStructured`.

## How It Works

1. Agent gets a text response using `AskWithHistory`
2. Text response is converted to JSON using a **second LLM call** with JSON mode
3. JSON is parsed into the target struct

## Methods Demonstrated

- `AskStructured[T](agent, ctx, question, schema, schemaString) (T, error)`

## Pros & Cons

**Pros:**
- ✅ **Always works** - Reliable fallback to text
- ✅ **Better for complex schemas** - Explicit conversion step
- ✅ **More predictable** - Guaranteed structured output

**Cons:**
- ❌ **2 LLM calls** - Slower and more expensive
- ❌ **May lose context** - Conversion is separate from conversation

## Setup

1. Create `.env` file with your OpenAI API key:
   ```
   OPENAI_API_KEY=your-api-key-here
   ```

2. Ensure Docker is running (for the fetch MCP server)

3. Run from the example directory:
   ```bash
   cd examples/structured_output/fixed
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

### Example 1: Simple Person Profile
- Creates a `Person` struct with name, age, and email
- Demonstrates basic structured output conversion

### Example 2: Complex Project
- Creates a `Project` struct with nested members and milestones
- Demonstrates complex nested structures with arrays and objects

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

Use the **Text Conversion Model** when:
- ✅ You need **guaranteed structured output**
- ✅ Working with **complex nested schemas**
- ✅ Reliability is more important than speed
- ✅ Cost is not a primary concern
- ✅ Batch processing or background jobs

## Output

The example will:
1. Create a simple person profile and display it as JSON
2. Create a complex project with team members and milestones
3. Show formatted JSON output for both examples

Note: This method uses **2 LLM calls** per structured output request (text response + JSON conversion).

