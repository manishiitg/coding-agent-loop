# Multi-Turn Conversation Example

This example demonstrates how to use the MCP Agent for multi-turn conversations where the agent maintains context across multiple questions.

## Features

- **Conversation History**: Uses `AskWithHistory()` to maintain context between turns
- **Context Awareness**: Follow-up questions can reference previous answers
- **Flexible Questions**: Supports default questions or custom questions via command line

## Setup

1. Create `.env` file with your OpenAI API key:
   ```
   OPENAI_API_KEY=your-api-key-here
   ```

2. Run from the example directory:
   ```bash
   cd examples/multi-turn
   go mod init multi-turn-example
   go mod edit -replace mcpagent=../..
   go mod tidy
   ```

## Usage

```bash
# Default multi-turn conversation
go run main.go

# Custom questions
go run main.go mcp_servers.json "What is React?" "Tell me more about hooks" "Show me an example"
```

## Example Conversation

The default example demonstrates a 3-turn conversation:

1. **Turn 1**: "Get me the documentation for React library"
2. **Turn 2**: "What are the main features mentioned in that documentation?"
3. **Turn 3**: "Can you give me a code example from it?"

The agent maintains context across all turns, so follow-up questions can reference previous answers without needing to repeat information.

## How It Works

1. Initialize the agent (same as basic example)
2. Start with an empty conversation history
3. For each question:
   - Add the user message to conversation history
   - Call `agent.AskWithHistory()` with the full history
   - Update the history with the agent's response
   - Continue to the next question

The `AskWithHistory()` method returns both the answer and the updated conversation history, which includes all previous messages and tool calls, allowing the agent to maintain full context.

## Requirements

- Go 1.24.4+
- OpenAI API key in `.env` file
- MCP server configuration in `mcp_servers.json`

