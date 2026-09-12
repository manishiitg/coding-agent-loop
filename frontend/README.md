# AI Agent Streaming Frontend

A React-based frontend for real-time AI agent conversations with multi-server MCP support.

## Features

- **Real-time Event Streaming**: Live display of agent activities and tool calls
- **Multi-Server MCP Support**: Connect to multiple MCP servers simultaneously
- **Event Mode Toggle**: Switch between Basic and Advanced event display modes
- **Tool Management**: Enable/disable specific tools from different MCP servers
- **Agent Modes**: Support for Simple and ReAct agent modes
- **Preset Queries**: Quick access to common query templates
- **Settings Panel**: Configure agent parameters and tool preferences

## Event Display Modes

### Basic Mode (Default)
Shows only essential events for a cleaner experience:
- `tool_call_start` - When a tool begins execution
- `tool_call_end` - When a tool completes execution
- `llm_generation_start` - When LLM generation begins
- `llm_generation_end` - When LLM generation completes
- `react_reasoning_end` - When ReAct reasoning completes
- `conversation_end` - When conversation ends
- `agent_end` - When agent completes
- `conversation_error` - When conversation encounters an error
- `agent_error` - When agent encounters an error
- `large_tool_output_detected` - When large tool outputs are detected

### Advanced Mode
Shows all events for detailed debugging and monitoring:
- All basic mode events
- Debug events
- Performance metrics
- Token usage details
- Detailed spans
- System prompts
- User messages
- And more...

## Getting Started

1. Install dependencies:
```bash
npm install
```

2. Start the development server:
```bash
npm run dev
```

3. Open your browser to `http://localhost:5173`

## Usage

1. **Configure Settings**: Click "Show Settings" to configure agent mode, max turns, and tool preferences
2. **Toggle Event Mode**: Use the "Basic/Advanced" toggle to switch between event display modes
3. **Select Preset Queries**: Choose from predefined query templates for common tasks
4. **Start Conversation**: Enter your query and click "Send" to begin the conversation
5. **Monitor Events**: Watch real-time events in the event stream
6. **View Final Response**: See the completed response in the prominent final response section

## Development

- **Build**: `npm run build`
- **Type Check**: `npm run type-check`
- **Lint**: `npm run lint`

## Architecture

- **Event-Driven**: Real-time event streaming from the backend
- **Context-Based**: React Context for event mode management
- **Component-Based**: Modular components for different event types
- **TypeScript**: Full type safety throughout the application
