# Basic MCP Agent Example

Simple example showing how to use the MCP Agent with OpenAI LLM and MCP servers.

## Setup

1. Create `.env` file with your OpenAI API key:
   ```
   OPENAI_API_KEY=your-api-key-here
   ```

2. Run from the example directory:
   ```bash
   cd examples/basic
   go run main.go
   ```

## Usage

```bash
# Default question
go run main.go

# Custom question
go run main.go mcp_servers.json "What are the latest AI developments?"
```

## Configuration

Edit `mcp_servers.json` to add/configure MCP servers. The example includes `tavily-search` for web search.

## Requirements

- Go 1.24.4+
- Node.js (for npx-based MCP servers)
- OpenAI API key in `.env` file

