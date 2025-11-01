# MCP Agent External Package

This package provides a clean, simple interface for using the MCP agent from external applications.

## Features

- **Simple Interface**: Easy-to-use API for agent interactions
- **Multiple Agent Modes**: Support for Simple and ReAct agents
- **Event Handling**: Built-in event system for observability
- **Health Monitoring**: Connection health checks and statistics
- **Multi-turn Conversations**: Support for conversation history
- **Flexible Configuration**: Builder pattern for easy configuration
- **Unified Logging**: Custom logger support with file and console output
- **Tool Timeout Protection**: Configurable tool execution timeouts to prevent hanging tools

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"
    
    "mcp-agent/agent_go/pkg/external"
)

func main() {
    // Create a context with timeout
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
    defer cancel()

    // Create a default configuration
    config := external.DefaultConfig().
        WithAgentMode(external.ReActAgent).
        WithServer("filesystem", "configs/mcp_servers.json").
        WithLLM("bedrock", "anthropic.claude-3-sonnet-20240229-v1:0", 0.2).
        WithMaxTurns(10).
        WithToolTimeout(5 * time.Second) // Set tool timeout to 5 seconds

    // Create the agent
    agent, err := external.NewAgent(ctx, config)
    if err != nil {
        log.Fatalf("Failed to create agent: %v", err)
    }
    defer agent.Close()

    // Ask a question
    answer, err := agent.Ask(ctx, "What is the current weather in New York?")
    if err != nil {
        log.Fatalf("Failed to ask question: %v", err)
    }

    fmt.Printf("Answer: %s\n", answer)
}
```

## Configuration

The package uses a builder pattern for configuration:

```go
config := external.DefaultConfig().
    WithAgentMode(external.ReActAgent).           // Simple or ReAct agent
    WithServer("filesystem", "configs/mcp_servers.json").
    WithLLM("bedrock", "model-id", 0.2).        // Provider, model, temperature
    WithMaxTurns(10).                            // Maximum conversation turns
    WithObservability("console", "").             // Tracing provider
    WithTimeout(5 * time.Minute).                // Request timeout
    WithToolTimeout(10 * time.Second).           // Tool execution timeout
    WithLogger(unifiedLogger)                    // Custom logger (optional)
```

### Timeout Configuration

The external package supports configurable timeouts for both requests and tool execution:

```go
config := external.DefaultConfig().
    WithTimeout(5 * time.Minute).                // Overall request timeout
    WithToolTimeout(5 * time.Minute).           // Tool execution timeout
```

**Timeout Types:**
- **Request Timeout**: Overall timeout for the entire agent interaction
- **Tool Timeout**: Maximum time allowed for individual tool execution (default: 5 minutes)

**Tool Timeout Benefits:**
- Prevents hanging tools from blocking agent conversations
- Configurable per-agent instance
- Graceful error handling with clear timeout messages
- Agent continues conversation after tool timeout

### System Prompt Configuration

The external package supports highly configurable system prompts:

```go
config := external.DefaultConfig().
    WithAgentMode(external.ReActAgent).
    WithServer("filesystem", "configs/mcp_servers.json").
    WithLLM("bedrock", "model-id", 0.2).
    // System prompt configuration
    WithCustomSystemPrompt(customTemplate).       // Use completely custom template
    WithSystemPromptMode("detailed").             // Use predefined mode
    WithAdditionalInstructions("Custom instructions..."). // Add to existing prompt
    WithToolInstructions(false).                  // Disable default tool instructions
    WithLargeOutputInstructions(false)            // Disable large output instructions
```

#### System Prompt Modes

- **`auto`** (default): Automatically selects based on agent mode (simple/react)
- **`simple`**: Standard assistant with tool usage
- **`react`**: ReAct agent with explicit reasoning
- **`minimal`**: Minimal instructions for direct responses
- **`detailed`**: Comprehensive instructions with large output handling
- **`custom`**: Use your own template with placeholders

#### Custom System Prompt Template

You can provide a completely custom template with these placeholders:

```go
customTemplate := `You are a specialized AI assistant.

Available tools:
{{TOOLS}}

{{PROMPTS_SECTION}}

{{RESOURCES_SECTION}}

{{VIRTUAL_TOOLS_SECTION}}

Your custom instructions here...`

config := external.DefaultConfig().
    WithCustomSystemPrompt(customTemplate)
```

**⚠️ Required Placeholders**: All custom templates **MUST** include these placeholders:
- `{{TOOLS}}` - Available tools section
- `{{PROMPTS_SECTION}}` - Available prompts section  
- `{{RESOURCES_SECTION}}` - Available resources section
- `{{VIRTUAL_TOOLS_SECTION}}` - Virtual tools section

**Validation Error Example**:
```go
// This will throw an exception - missing required placeholders
invalidTemplate := `You are a custom assistant. 
Use tools when needed.` // Missing {{TOOLS}}, {{PROMPTS_SECTION}}, etc.

config := external.DefaultConfig().
    WithCustomSystemPrompt(invalidTemplate) // ❌ Will throw validation error

// Error: "custom template is missing required placeholders: [{{TOOLS}} {{PROMPTS_SECTION}} {{RESOURCES_SECTION}} {{VIRTUAL_TOOLS_SECTION}}]"
```

#### Additional Instructions

Add custom instructions to any system prompt:

```go
config := external.DefaultConfig().
    WithSystemPromptMode("simple").
    WithAdditionalInstructions(`
IMPORTANT: When analyzing data:
1. Always verify the source
2. Provide confidence levels
3. Suggest follow-up actions`)
```

#### Disabling Default Instructions

You can disable default tool and large output instructions:

```go
config := external.DefaultConfig().
    WithToolInstructions(false).                  // Disable default tool instructions
    WithLargeOutputInstructions(false).           // Disable large output handling
    WithAdditionalInstructions("Your custom instructions only...")
```

### Agent Modes

- **SimpleAgent**: Direct tool usage without explicit reasoning
- **ReActAgent**: Explicit reasoning with step-by-step thinking

### LLM Providers

- **bedrock**: AWS Bedrock (requires AWS credentials)
- **openai**: OpenAI (requires OPENAI_API_KEY)

## API Reference

### Agent Interface

```go
type Agent interface {
    // Ask sends a single question and returns the answer
    Ask(ctx context.Context, question string) (string, error)
    
    // AskWithHistory sends a conversation with history and returns the answer
    AskWithHistory(ctx context.Context, messages []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error)
    
    // GetServerNames returns the list of connected server names
    GetServerNames() []string
    
    // CheckHealth performs health checks on all MCP connections
    CheckHealth(ctx context.Context) map[string]error
    
    // GetStats returns statistics about all MCP connections
    GetStats() map[string]interface{}
    
    // Close closes all underlying MCP client connections
    Close()
    
    // AddEventListener adds an event listener for agent events
    AddEventListener(listener AgentEventListener)
    
    // EmitEvent emits an event to all listeners
    EmitEvent(ctx context.Context, eventType AgentEventType, data map[string]interface{})
}
```

### Event Handling

```go
type AgentEventListener interface {
    HandleEvent(ctx context.Context, event *AgentEvent) error
    Name() string
}

type AgentEvent struct {
    Type      AgentEventType
    Data      map[string]interface{}
    Timestamp time.Time
}
```

### Event Types

- `EventSystemPrompt`: System prompt events
- `EventUserMessage`: User message events
- `EventLLMGeneration`: LLM generation events
- `EventToolCall`: Tool call events
- `EventConversationEnd`: Conversation end events
- `EventAgentEnd`: Agent end events
- `EventError`: Error events

## Multi-turn Conversations

```go
// Create conversation history
messages := []llmtypes.MessageContent{
    {
        Role: llmtypes.ChatMessageTypeHuman,
        Parts: []llmtypes.ContentPart{
            llmtypes.TextContent{Text: "Hello, how are you?"},
        },
    },
}

// Continue the conversation
answer, updatedMessages, err := agent.AskWithHistory(ctx, messages)
if err != nil {
    log.Fatalf("Failed to continue conversation: %v", err)
}

// Add a follow-up question
updatedMessages = append(updatedMessages, llmtypes.MessageContent{
    Role: llmtypes.ChatMessageTypeHuman,
    Parts: []llmtypes.ContentPart{
        llmtypes.TextContent{Text: "Can you help me with a task?"},
    },
})

// Get the response
answer, _, err = agent.AskWithHistory(ctx, updatedMessages)
```

## Health Monitoring

```go
// Check agent health
health := agent.CheckHealth(ctx)
for server, err := range health {
    if err != nil {
        fmt.Printf("Server %s: %v\n", server, err)
    } else {
        fmt.Printf("Server %s: healthy\n", server)
    }
}

// Get connection stats
stats := agent.GetStats()
fmt.Printf("Connection stats: %+v\n", stats)
```

## Custom Logging

The external package supports custom loggers that implement our `utils.ExtendedLogger` interface. This allows you to:

- **File Logging**: Write logs to files for persistence
- **Console Logging**: Display logs in the terminal
- **Automatic Default Logger**: Creates a default logger with custom filename when none provided
- **Agent Integration**: Logs from all agent components (conversation, tools, etc.)

### Logger Usage

#### With Custom Logger
```go
import (
    "mcp-agent/agent_go/pkg/logger"
)

// Create your own logger
customLogger, err := logger.CreateLogger("my-app.log", "info", "text", true)
if err != nil {
    log.Fatalf("Failed to create logger: %v", err)
}
defer customLogger.Close()

// Use with agent configuration
config := external.DefaultConfig().
    WithAgentMode(external.ReActAgent).
    WithServer("filesystem", "configs/mcp_servers.json").
    WithLLM("bedrock", "anthropic.claude-3-sonnet-20240229-v1:0", 0.2).
    WithLogger(customLogger) // Pass your custom logger

agent, err := external.NewAgent(ctx, config)
```

#### With Default Logger (Nil Logger)
```go
// No logger specified - agent will create a default logger automatically
config := external.DefaultConfig().
    WithAgentMode(external.SimpleAgent).
    WithServer("filesystem", "configs/mcp_servers.json").
    WithLLM("bedrock", "anthropic.claude-3-sonnet-20240229-v1:0", 0.2)
    // No WithLogger() call - uses default logger

agent, err := external.NewAgent(ctx, config)
```

The default logger will:
- **Filename**: `external-file-{date}-{time}.log` (e.g., `external-file-2025-01-27-14-30-25.log`)
- **Level**: Info
- **Format**: Text
- **Output**: Both file and console

### Custom Logger Implementation

You can implement your own logger by satisfying the `utils.ExtendedLogger` interface:

```go
import "mcp-agent/agent_go/internal/utils"

type MyCustomLogger struct {
    logFile *os.File
}

func (l *MyCustomLogger) Infof(format string, v ...any) {
    message := fmt.Sprintf("INFO: "+format, v...)
    // Write to file and console
    l.logFile.WriteString(message + "\n")
    fmt.Print(message + "\n")
}

func (l *MyCustomLogger) Errorf(format string, v ...any) {
    message := fmt.Sprintf("ERROR: "+format, v...)
    // Write to file and console
    l.logFile.WriteString(message + "\n")
    fmt.Print(message + "\n")
}

// Implement other required methods...
func (l *MyCustomLogger) Info(args ...interface{}) { /* ... */ }
func (l *MyCustomLogger) Error(args ...interface{}) { /* ... */ }
func (l *MyCustomLogger) Debug(args ...interface{}) { /* ... */ }
func (l *MyCustomLogger) Debugf(format string, args ...interface{}) { /* ... */ }
func (l *MyCustomLogger) Warn(args ...interface{}) { /* ... */ }
func (l *MyCustomLogger) Warnf(format string, args ...interface{}) { /* ... */ }
func (l *MyCustomLogger) Fatal(args ...interface{}) { /* ... */ }
func (l *MyCustomLogger) Fatalf(format string, args ...interface{}) { /* ... */ }
func (l *MyCustomLogger) WithField(key string, value interface{}) *logrus.Entry { /* ... */ }
func (l *MyCustomLogger) WithFields(fields logrus.Fields) *logrus.Entry { /* ... */ }
func (l *MyCustomLogger) WithError(err error) *logrus.Entry { /* ... */ }
func (l *MyCustomLogger) Close() error { /* ... */ }

// Use your custom logger
config := external.DefaultConfig().
    WithLogger(&MyCustomLogger{logFile: myFile})
```

### Logger Benefits

- **Flexible Logger Support**: Use your own logger or let the agent create a default one
- **Automatic Default Logger**: No need to worry about logger setup - just omit `WithLogger()`
- **Custom Filename Pattern**: Default logger uses descriptive filenames with timestamps
- **File Persistence**: Logs are saved to files for debugging
- **Console Output**: Real-time logging in the terminal
- **Agent Integration**: Logs from all agent components (conversation, tools, etc.)

## Event Listeners

```go
type MyEventListener struct{}

func (e *MyEventListener) HandleEvent(ctx context.Context, event *external.AgentEvent) error {
    fmt.Printf("Event: %s at %s\n", event.Type, event.Timestamp.Format(time.RFC3339))
    if event.Data != nil {
        fmt.Printf("  Data: %+v\n", event.Data)
    }
    return nil
}

func (e *MyEventListener) Name() string {
    return "my-listener"
}

// Add the event listener
agent.AddEventListener(&MyEventListener{})
```

## Environment Variables

For detailed environment variable setup, see [ENV_GUIDE.md](ENV_GUIDE.md).

### Quick Setup

#### AWS Bedrock (Recommended)
```bash
export AWS_REGION=us-east-1
export AWS_ACCESS_KEY_ID=your_access_key_here
export AWS_SECRET_ACCESS_KEY=your_secret_key_here
export BEDROCK_PRIMARY_MODEL=anthropic.claude-3-sonnet-20240229-v1:0
```

#### OpenAI
```bash
export OPENAI_API_KEY=your_openai_api_key_here
```

#### Observability (Optional)
```bash
export TRACING_PROVIDER=console  # console, langfuse, noop
export LANGFUSE_PUBLIC_KEY=your_public_key
export LANGFUSE_SECRET_KEY=your_secret_key
```

## Quick Reference

### Logger Usage

```go
// With custom logger
customLogger, err := logger.CreateLogger("my-app.log", "info", "text", true)
config := external.DefaultConfig().
    WithLogger(customLogger)

// With default logger (no WithLogger call)
config := external.DefaultConfig().
    WithAgentMode(external.SimpleAgent)

agent, err := external.NewAgent(ctx, config)
```

### Logger Interface

```go
type ExtendedLogger interface {
    Infof(format string, v ...any)
    Errorf(format string, v ...any)
    Info(args ...interface{})
    Error(args ...interface{})
    Debug(args ...interface{})
    // ... and more methods
}
```

## Examples

See `example.go` for complete usage examples including:
- Basic agent usage
- Multi-turn conversations
- Event handling
- Health monitoring
- Configuration examples
- Custom logger implementation (`ExampleCustomLogger`)
- File logging (`ExampleFileLogger`)
- Default logger usage (no logger specified) 