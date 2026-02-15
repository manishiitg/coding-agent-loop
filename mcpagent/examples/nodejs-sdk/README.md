# MCPAgent Node.js HTTP Example

This example demonstrates how to use MCPAgent from Node.js via the HTTP API.

## Quick Start with Docker Compose

The easiest way to run this example is with Docker Compose:

```bash
cd examples/nodejs-http

# Create .env file with your OpenAI API key
echo "OPENAI_API_KEY=your-api-key-here" > .env

# Start the server
docker compose up -d

# Verify the server is running
curl http://localhost:8080/health
# Should return: {"status":"ok"}

# Run the example
npm install
npm run dev
```

## Prerequisites

### Option 1: Docker Compose (Recommended)

The Docker image includes all necessary runtimes for MCP servers:
- Node.js 20.x with npm/npx
- Python 3.11 with uv/uvx
- curl, ca-certificates

Just create a `.env` file with your API key and run `docker compose up -d`.

### Option 2: Run Go Server Directly

From the `mcpagent` root directory:

```bash
# Load environment variables (contains OPENAI_API_KEY)
source examples/basic/.env

# Start the server
go run cmd/server/main.go --port 8080 --config examples/nodejs-http/mcp_servers.json
```

## Running the Examples

```bash
# Install dependencies
npm install

# Run basic example (single questions)
npm run dev

# Run multi-turn conversation example
npm run dev:multi-turn
```

## Examples

### Basic Example (`src/basic.ts`)

Demonstrates:
- Creating an agent with OpenAI gpt-4.1-mini
- Asking single questions
- Using MCP tools (context7 for documentation)
- Getting token usage and costs
- Destroying the agent

### Multi-turn Example (`src/multi-turn.ts`)

Demonstrates:
- Maintaining conversation history
- Context-aware responses across multiple turns
- Session summary with total costs

## API Overview

```typescript
import { MCPAgent } from 'mcpagent';

// Create client
const agent = new MCPAgent('http://localhost:8080');

// Initialize agent
await agent.initialize({
  provider: 'openai',
  modelId: 'gpt-4.1-mini',
});

// Ask questions
const response = await agent.ask('What tools are available?');
console.log(response.response);

// Multi-turn conversation
const messages = [
  { role: 'user', content: 'Hello' },
  { role: 'assistant', content: 'Hi there!' },
  { role: 'user', content: 'What can you do?' },
];
const result = await agent.askWithHistory(messages);

// Get usage stats
const usage = await agent.getTokenUsage();
console.log(`Cost: $${usage.costs.totalCost}`);

// Cleanup
await agent.destroy();
```

## MCP Servers Configuration

The `mcp_servers.json` file configures which MCP servers to connect:

```json
{
  "mcpServers": {
    "context7": {
      "url": "https://mcp.context7.com/mcp",
      "protocol": "http"
    },
    "sequential-thinking": {
      "command": "npx",
      "args": ["--yes", "@modelcontextprotocol/server-sequential-thinking"]
    },
    "ddg-search": {
      "command": "uvx",
      "args": ["duckduckgo-mcp-server"]
    }
  }
}
```

## Configuration

| Environment Variable | Description |
|---------------------|-------------|
| `OPENAI_API_KEY` | Your OpenAI API key (required) |
| `MCPAGENT_URL` | Server URL (default: `http://localhost:8080`) |

## Project Structure

```
nodejs-http/
├── src/
│   ├── basic.ts           # Simple example
│   └── multi-turn.ts      # Conversation example
├── .env                   # API keys (create this)
├── docker-compose.yaml    # Docker Compose config
├── mcp_servers.json       # MCP servers config
├── package.json
├── tsconfig.json
└── README.md
```

## Supported LLM Providers

The HTTP API supports multiple LLM providers:

| Provider | Example Models |
|----------|---------------|
| `openai` | `gpt-4.1-mini`, `gpt-4.1`, `gpt-4o`, `gpt-4o-mini` |
| `anthropic` | `claude-3-5-sonnet-20241022`, `claude-3-opus-20240229` |
| `bedrock` | `anthropic.claude-3-5-sonnet-20241022-v2:0` |
| `openrouter` | Various models via OpenRouter |
| `vertex` | Google Vertex AI models |

## Troubleshooting

### Server not starting
- Check if port 8080 is available
- Verify your `.env` file has a valid `OPENAI_API_KEY`

### MCP servers not connecting
- For `npx` servers: Node.js must be installed in the container
- For `uvx` servers: Python and uv must be installed in the container
- The Docker image includes both runtimes

### Timeout errors
- The first request may take longer as MCP servers initialize
- Subsequent requests will be faster due to connection caching
