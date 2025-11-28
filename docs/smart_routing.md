# Smart Routing

Smart Routing is an advanced optimization feature in the MCP Agent that dynamically filters available tools and servers based on the current conversation context. This significantly reduces token usage, improves LLM performance, and prevents context window overflow when working with many MCP servers.

## 🎯 Purpose

When an agent is connected to multiple MCP servers (e.g., AWS, GitHub, Kubernetes, Database), the total number of available tools can easily exceed 100+. Sending all these tool definitions to the LLM in every turn:
1.  **Consumes Tokens**: Wastes thousands of tokens per request.
2.  **Confuses the LLM**: Increases the chance of hallucination or incorrect tool selection.
3.  **Increases Latency**: Larger prompts take longer to process.

Smart Routing solves this by introducing a lightweight "Router" step before the main agent execution.

## ⚙️ Configuration

Smart Routing is disabled by default. You can enable and configure it using functional options when creating the agent.

```go
agent, err := mcpagent.NewAgent(..., 
    // Enable Smart Routing
    mcpagent.WithSmartRouting(true),
    
    // Set thresholds for when routing should kick in
    mcpagent.WithSmartRoutingThresholds(
        20, // MaxTools: Trigger if total tools > 20
        3,  // MaxServers: Trigger if total servers > 3
    ),
    
    // Configure the Router LLM parameters
    mcpagent.WithSmartRoutingConfig(
        0.1,  // Temperature (low for deterministic routing)
        1000, // MaxTokens
        10,   // MaxMessages to consider for context
        500,  // UserMsgLimit (chars)
        1000, // AssistantMsgLimit (chars)
    ),
)
```

## 🔄 How It Works

The Smart Routing process occurs at the beginning of `AskWithHistory` if the tool/server count exceeds the configured thresholds.

1.  **Context Analysis**: The agent aggregates the full conversation history.
2.  **Router Call**: A lightweight LLM call is made with a specialized system prompt:
    > "You are a tool routing assistant. Based on the user's query and conversation context, determine which MCP servers are most relevant."
3.  **Server Selection**: The Router returns a JSON list of relevant servers and a reasoning string.
4.  **Tool Filtering**: The agent filters the `Tools` list to include only:
    *   Tools belonging to the selected servers.
    *   **Always-Available Tools**: Custom tools (e.g., memory) and Virtual tools are never filtered out.
5.  **Prompt Rebuilding**: The system prompt is dynamically rebuilt to include documentation/resources only from the selected servers.
6.  **Execution**: The main LLM call proceeds with the reduced, highly relevant set of tools.

## 📊 Observability

Smart Routing emits specific events for tracking its performance:

- `smart_routing_start`: Triggered when routing begins.
- `smart_routing_end`: Contains the selected servers, reasoning, and duration.
- `token_usage`: Tracks the token cost of the routing step (usually very low compared to the savings).

## 🧩 Implementation Details

The core logic resides in `pkg/mcpagent/smart_routing.go`.

### `filterToolsByRelevance`
This function orchestrates the routing process. It builds the prompt, calls the LLM, and processes the structured response.

### `buildServerSelectionPrompt`
Constructs a prompt listing all available servers and a summary of their tools, asking the LLM to select the most relevant ones for the current user query.

### `RebuildSystemPromptWithFilteredServers`
Crucial for context saving. It regenerates the system prompt to exclude detailed documentation for servers that were filtered out, ensuring the context window is optimized.
