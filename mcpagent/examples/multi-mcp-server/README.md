# Multi-MCP Server Example

This example demonstrates how to use the MCP Agent with multiple MCP servers working together to perform complex tasks that require different capabilities.

## Features

- **Multiple MCP Servers**: Uses 4 different MCP servers simultaneously:
  - **Playwright**: Browser automation for web browsing and data extraction
  - **Sequential Thinking**: Advanced reasoning and step-by-step analysis
  - **Context7**: Library documentation and code reference access
  - **AWS Knowledge MCP**: AWS knowledge base and documentation access
- **Cross-Server Collaboration**: Demonstrates how different servers can work together
- **Complex Task Execution**: Performs tasks that require multiple capabilities
- **Default Task**: Researches cloud computing trends, analyzes findings, and checks AWS pricing

## Setup

1. Create `.env` file with your OpenAI API key:
   ```
   OPENAI_API_KEY=your-api-key-here
   ```

2. Initialize the Go module:
   ```bash
   cd examples/multi-mcp-server
   go mod init multi-mcp-server-example
   go mod edit -replace mcpagent=../..
   go mod tidy
   ```

3. Ensure required tools are installed:
   - **Node.js** (v18 or higher) - required for `npx` to run Playwright and Sequential Thinking servers
   ```bash
   node --version  # Should be v18 or higher
   ```

## Usage

```bash
# Run with default cloud computing research task
go run main.go

# Run with custom config file
go run main.go mcp_servers.json

# Run with custom task
go run main.go mcp_servers.json "Research AI trends, analyze them, and check relevant AWS services"
```

## Default Task

The default task demonstrates multi-server collaboration:

1. **Browser Research** (Playwright): Uses browser automation to search for recent cloud computing trends
2. **Analysis** (Sequential Thinking): Breaks down and analyzes the information using advanced reasoning
3. **AWS Knowledge** (AWS Knowledge MCP): Accesses AWS documentation and knowledge base
4. **Reporting**: Presents findings in a structured format with key insights

## How It Works

1. The agent initializes with all configured MCP servers
2. Each server provides different tools:
   - **Playwright**: Browser navigation, page content extraction, web interactions
   - **Sequential Thinking**: Step-by-step reasoning, problem decomposition
   - **Context7**: Library documentation lookup, code examples
   - **AWS Knowledge MCP**: AWS documentation, knowledge base, service information
3. The LLM orchestrates tools from different servers to complete the task
4. Results are compiled from multiple sources and presented as a comprehensive analysis

## Configuration

The `mcp_servers.json` file configures all MCP servers:

```json
{
  "mcpServers": {
    "playwright": {
      "command": "npx",
      "args": ["@playwright/mcp@latest"]
    },
    "sequential-thinking": {
      "command": "npx",
      "args": ["--yes", "@modelcontextprotocol/server-sequential-thinking"]
    },
    "context7": {
      "command": "",
      "args": null,
      "protocol": "http",
      "url": "https://mcp.context7.com/mcp"
    },
    "aws-knowledge-mcp": {
      "url": "https://knowledge-mcp.global.api.aws"
    }
  }
}
```

## Example Use Cases

- **Research & Analysis**: Use browser automation to gather data, sequential thinking to analyze it, and AWS Knowledge MCP to access AWS documentation
- **Development Workflows**: Use Context7 for library docs, Playwright for testing, and AWS Knowledge MCP for AWS service information
- **Market Research**: Combine web research with analytical reasoning and cloud cost analysis
- **Technical Analysis**: Gather information from multiple sources and synthesize insights

## Requirements

- Go 1.24.4+
- Node.js 18+ (for npx-based MCP servers)
- OpenAI API key in `.env` file
- Internet connection (for web browsing and API access)

## Notes

- Multi-server tasks may take longer than single-server operations
- The default timeout is set to 15 minutes to allow for complex multi-step operations
- Each server runs independently and can be used in parallel when appropriate
- Some servers may require additional setup or API keys
- The agent automatically routes requests to the appropriate server based on the task

## Server Details

### Playwright
- Provides browser automation capabilities
- Can navigate websites, extract content, interact with elements
- Useful for web scraping and research

### Sequential Thinking
- Provides advanced reasoning capabilities
- Breaks down complex problems into steps
- Useful for analysis and problem-solving

### Context7
- Provides access to library documentation
- Can search and retrieve code examples
- Useful for development and technical reference

### AWS Knowledge MCP
- Provides access to AWS knowledge base and documentation
- Can query AWS service information, best practices, and technical documentation
- No credentials required - public knowledge base access
- Accessible via HTTPS endpoint

