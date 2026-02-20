# Custom Tools Example

This example demonstrates how to register and use **custom tools** with the MCP agent. Custom tools allow you to extend the agent's capabilities with your own functions that can be called by the LLM during conversations.

## What Are Custom Tools?

Custom tools are user-defined functions that:
- Are registered with the agent using `RegisterCustomTool()`
- Can be called by the LLM during conversations
- Work alongside MCP server tools
- Support JSON schema parameters
- Can be organized into categories

## How It Works

1. **Register Custom Tools**: Use `agent.RegisterCustomTool()` to register your functions
2. **LLM Calls Tools**: During conversations, the LLM can call your custom tools
3. **Tool Execution**: Your execution function runs and returns results
4. **Response**: Results are returned to the LLM and included in the response

## Methods Demonstrated

- `agent.RegisterCustomTool(name, description, parameters, executionFunc, category)`

## Custom Tools in This Example

### 1. **calculator** (utility category)
Performs mathematical operations:
- Operations: `add`, `subtract`, `multiply`, `divide`, `power`, `sqrt`
- Parameters: `operation` (string), `a` (number), `b` (number, optional for sqrt)

### 2. **format_text** (utility category)
Formats text in various ways:
- Formats: `uppercase`, `lowercase`, `reverse`, `title_case`
- Parameters: `text` (string), `format` (string)

### 3. **get_weather** (data category)
Simulates weather data for locations:
- Parameters: `location` (string), `unit` (string, optional: "celsius" or "fahrenheit")

### 4. **count_text** (utility category)
Counts elements in text:
- Count types: `characters`, `words`, `sentences`, `paragraphs`
- Parameters: `text` (string), `count_type` (string)

## Tool Categories

Tools must be registered with a **category** (required). Categories help organize tools and can be used for:
- Filtering tools
- Code generation (in code execution mode)
- Tool discovery

Common categories:
- `utility` - General utility tools
- `data` - Data retrieval/processing tools
- `workspace` - Workspace/file operations
- `human` - Human interaction tools
- `structured_output` - Structured output tools
- Or any custom category name

## Setup

1. Create `.env` file with your OpenAI API key:
   ```
   OPENAI_API_KEY=your-api-key-here
   ```

2. Install dependencies:
   ```bash
   go mod tidy
   ```

3. Run the example:
   ```bash
   go run main.go
   ```

4. Or with custom questions:
   ```bash
   go run main.go mcp_servers.json "Calculate 100 divided by 5" "Format 'hello world' to uppercase"
   ```

## Example Usage

The example demonstrates several scenarios:

1. **Mathematical Operations**: "Calculate 15 multiplied by 23"
2. **Text Formatting**: "Format the text 'Hello World' to uppercase"
3. **Weather Data**: "What's the weather like in San Francisco?"
4. **Text Analysis**: "Count the number of words in 'The quick brown fox jumps over the lazy dog'"
5. **Complex Operations**: "Get weather for New York in fahrenheit and format the location name to title case"

## Tool Registration Pattern

```go
// Define parameters as JSON schema
params := map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
        "param1": map[string]interface{}{
            "type":        "string",
            "description": "Description of param1",
        },
    },
    "required": []string{"param1"},
}

// Register the tool
err := agent.RegisterCustomTool(
    "tool_name",                    // Tool name
    "Tool description",             // Description for LLM
    params,                         // JSON schema parameters
    toolExecutionFunction,          // Execution function
    "category",                     // Category (required)
)
```

## Execution Function Signature

```go
func toolExecutionFunction(ctx context.Context, args map[string]interface{}) (string, error) {
    // Extract and validate arguments
    param1, ok := args["param1"].(string)
    if !ok {
        return "", fmt.Errorf("param1 must be a string")
    }
    
    // Perform operation
    result := doSomething(param1)
    
    // Return result as string
    return fmt.Sprintf("Result: %s", result), nil
}
```

## Key Features

- ✅ **Multiple Categories**: Tools can be organized into different categories
- ✅ **JSON Schema Parameters**: Full support for JSON schema parameter definitions
- ✅ **Error Handling**: Proper error handling in tool execution
- ✅ **Works with MCP Tools**: Custom tools work alongside MCP server tools
- ✅ **LLM Integration**: LLM automatically decides when to use custom tools
- ✅ **Logging**: Full logging support for debugging

## Logs

The example creates log files in the `logs/` directory:
- `llm.log` - LLM API calls and token usage
- `custom-tools.log` - Agent operations, tool registrations, and executions

## Notes

- **Category is Required**: All tools must have a category
- **Parameter Validation**: Always validate parameters in your execution function
- **Error Messages**: Return clear error messages for better LLM understanding
- **String Results**: Tool execution functions must return `(string, error)`
- **Context Support**: Execution functions receive a context for cancellation/timeout

## Next Steps

- Add more custom tools for your specific use case
- Create custom categories for better organization
- Combine custom tools with MCP server tools
- Use custom tools in multi-turn conversations
- Integrate with external APIs or services

