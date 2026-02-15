# @mcpagent/node

Node.js SDK for MCPAgent - AI agents with MCP (Model Context Protocol) server support.

## Features

- **Auto-managed Go server** - Server starts/stops automatically with your Node.js process
- **Multi-provider LLM support** - OpenAI, Anthropic, AWS Bedrock, OpenRouter, Vertex AI
- **MCP server integration** - Connect to any MCP-compatible server (filesystem, github, etc.)
- **Custom tools** - Register JavaScript functions as tools for the LLM
- **Multi-turn conversations** - Maintain context across conversation turns
- **Token tracking** - Monitor usage and costs across providers
- **Unix socket communication** - Secure, no network exposure

## Installation

```bash
npm install @mcpagent/node
```

### Prerequisites

- Node.js >= 18.0.0
- Go >= 1.21 (for the backend server)
- MCPAgent Go server (included in the monorepo)

## Quick Start

```typescript
import { MCPAgent } from '@mcpagent/node';

const agent = new MCPAgent();

// Initialize with your preferred LLM provider
await agent.initialize({
  provider: 'openai',
  modelId: 'gpt-4o',
  selectedServers: ['filesystem']
});

// Ask questions - the agent will use available tools
const response = await agent.ask('What files are in the current directory?');
console.log(response.response);

// Always clean up when done
await agent.destroy();
```

## Configuration

### Agent Configuration

```typescript
await agent.initialize({
  // LLM Provider (required)
  provider: 'openai',           // 'openai' | 'anthropic' | 'bedrock' | 'openrouter' | 'vertex'
  modelId: 'gpt-4o',           // Model ID for the provider

  // Optional settings
  temperature: 0.7,             // 0.0 - 1.0
  maxTurns: 100,               // Maximum conversation turns
  systemPrompt: 'You are...',  // Custom system prompt

  // MCP Server configuration
  mcpConfigPath: './mcp_servers.json',  // Path to MCP config
  selectedServers: ['filesystem'],       // Filter to specific servers
  selectedTools: ['filesystem:read_file'], // Filter to specific tools

  // Advanced features
  enableContextSummarization: true,  // Auto-summarize long conversations
  enableContextOffloading: true,     // Save large outputs to files
  enableStreaming: false             // Enable streaming responses
});
```

### Environment Variables

Set API keys for your LLM providers:

```bash
# OpenAI
export OPENAI_API_KEY=sk-...

# Anthropic
export ANTHROPIC_API_KEY=sk-ant-...

# AWS Bedrock
export AWS_REGION=us-east-1
export AWS_ACCESS_KEY_ID=...
export AWS_SECRET_ACCESS_KEY=...
```

## Custom Tools

Register JavaScript functions as tools that the LLM can call:

```typescript
const agent = new MCPAgent();

// Register tools BEFORE initialize()
agent.registerTool(
  'get_weather',
  'Get current weather for a location',
  {
    type: 'object',
    properties: {
      city: { type: 'string', description: 'City name' }
    },
    required: ['city']
  },
  async (args) => {
    const response = await fetch(`https://api.weather.com/${args.city}`);
    return await response.text();
  },
  { timeoutMs: 10000 }
);

await agent.initialize({ provider: 'openai', modelId: 'gpt-4o' });

// The LLM can now call your custom tool
const response = await agent.ask('What is the weather in Tokyo?');
```

## Multi-turn Conversations

Maintain context across multiple turns:

```typescript
import { Message } from '@mcpagent/node';

const messages: Message[] = [];

// First turn
messages.push({ role: 'user', content: 'List files in src/' });
let result = await agent.askWithHistory(messages);
messages.push(...result.updatedMessages.slice(messages.length));

// Follow-up turn (has context from previous turn)
messages.push({ role: 'user', content: 'Show me the first file' });
result = await agent.askWithHistory(messages);
```

## Token Usage & Costs

Track usage and costs:

```typescript
const usage = await agent.getTokenUsage();

console.log(`Prompt tokens: ${usage.promptTokens}`);
console.log(`Completion tokens: ${usage.completionTokens}`);
console.log(`Total tokens: ${usage.totalTokens}`);
console.log(`LLM calls: ${usage.llmCallCount}`);

// Cost breakdown (USD)
console.log(`Input cost: $${usage.costs.inputCost.toFixed(4)}`);
console.log(`Output cost: $${usage.costs.outputCost.toFixed(4)}`);
console.log(`Total cost: $${usage.costs.totalCost.toFixed(4)}`);
```

## API Reference

### MCPAgent

Main class for interacting with AI agents.

#### Constructor

```typescript
new MCPAgent(options?: MCPAgentOptions)
```

| Option | Type | Description |
|--------|------|-------------|
| `apiKey` | `string` | Optional API key for authentication |
| `serverOptions` | `ServerManagerOptions` | Options for the Go server |
| `callbackSocketPath` | `string` | Custom path for callback socket |

#### Methods

| Method | Description |
|--------|-------------|
| `initialize(config)` | Initialize agent with configuration |
| `ask(question)` | Ask a single question |
| `askWithHistory(messages)` | Multi-turn conversation |
| `getTokenUsage()` | Get usage statistics |
| `registerTool(...)` | Register a custom tool |
| `unregisterTool(name)` | Remove a custom tool |
| `destroy()` | Clean up resources |
| `getAgentId()` | Get current agent ID |
| `getSessionId()` | Get current session ID |
| `getCapabilities()` | Get available tools/servers |
| `isInitialized()` | Check initialization status |

### Types

```typescript
// LLM Providers
type Provider = 'openai' | 'anthropic' | 'bedrock' | 'openrouter' | 'vertex';

// Conversation message
interface Message {
  role: 'user' | 'assistant' | 'system';
  content: string;
}

// Response from ask()
interface AskResponse {
  response: string;
  tokenUsage: TokenUsage;
  durationMs: number;
}

// Token usage statistics
interface TokenUsage {
  promptTokens: number;
  completionTokens: number;
  totalTokens: number;
  cacheTokens?: number;
  reasoningTokens?: number;
  llmCallCount: number;
}
```

## Architecture

```
Node.js Application
    │
    └─► MCPAgent SDK
            │
            ├─► ServerManager (spawns Go server)
            │       └─► go run cmd/server/main.go --socket ...
            │
            └─► CallbackServer (custom tool handlers)
                    └─► HTTP on Unix socket
                            │
                            ▼
                    Go HTTP Server
                    (Unix socket)
                            │
                            ├─► Agent Manager
                            │       └─► LLM Providers
                            │
                            └─► MCP Servers
                                    └─► filesystem, github, etc.
```

### Why Unix Sockets?

- **No port conflicts** - Each process uses unique socket files
- **Security** - No network exposure, filesystem permissions only
- **Performance** - Lower latency than TCP/IP
- **Auto-cleanup** - Sockets removed when process exits

## Error Handling

```typescript
import { MCPAgent, MCPAgentError } from '@mcpagent/node';

try {
  await agent.initialize({ ... });
  await agent.ask('...');
} catch (error) {
  if (error instanceof MCPAgentError) {
    console.error(`Error [${error.code}]: ${error.message}`);
    // Common codes: NOT_INITIALIZED, AGENT_NOT_FOUND, ASK_FAILED
  }
} finally {
  await agent.destroy();
}
```

## Advanced Usage

### Using a Pre-started Server

```typescript
const agent = new MCPAgent({
  serverOptions: {
    socketPath: '/tmp/my-mcpagent.sock'
  }
});

// If server is already running, it will be reused
await agent.initialize({ ... });
```

### List All Active Agents

```typescript
const agents = await MCPAgent.listAgents('/tmp/mcpagent.sock');
console.log(agents);
// [{ agentId: 'agent_123', sessionId: 'session_456', status: 'ready', ... }]
```

## Contributing

See the main [MCPAgent repository](https://github.com/mcpagent/mcpagent) for contribution guidelines.

## License

MIT
