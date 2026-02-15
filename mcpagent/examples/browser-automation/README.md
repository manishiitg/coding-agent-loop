# Browser Automation Example

This example demonstrates how to use the MCP Agent with browser automation capabilities to perform web research and analysis tasks.

## Features

- **Browser Automation**: Uses `@browsermcp/mcp` for web browsing and data extraction
- **Web Research**: Automatically navigates websites, searches, and extracts information
- **Analysis Tasks**: Performs complex research and analysis using browser tools
- **Default Task**: Analyzes the last 10 popular IPOs in India and finds patterns

## Setup

1. Create `.env` file with your OpenAI API key:
   ```
   OPENAI_API_KEY=your-api-key-here
   ```

2. Initialize the Go module:
   ```bash
   cd examples/browser-automation
   go mod init browser-automation-example
   go mod edit -replace mcpagent=../..
   go mod tidy
   ```

3. Ensure Node.js is installed (required for `npx` to run the browser MCP server):
   ```bash
   node --version  # Should be v18 or higher
   ```

## Usage

```bash
# Run with default IPO analysis task
go run main.go

# Run with custom config file
go run main.go mcp_servers.json

# Run with custom task
go run main.go mcp_servers.json "Search for the latest AI news and summarize the top 5 articles"
```

## Default Task

The default task performs a comprehensive analysis of the last 10 popular IPOs in India:

1. **Search**: Uses browser automation to search for recent Indian IPOs
2. **Data Collection**: Navigates to relevant websites and extracts IPO information
3. **Analysis**: Identifies patterns such as:
   - Sector distribution
   - Pricing trends
   - Performance metrics
   - Market timing patterns
   - Investor response patterns
4. **Reporting**: Presents findings in a structured format

## How It Works

1. The agent initializes with the browser MCP server (`browsermcp`)
2. Browser tools are available for:
   - Navigating to URLs
   - Searching the web
   - Extracting page content
   - Taking snapshots
   - Interacting with web elements
3. The LLM orchestrates browser actions to complete the research task
4. Results are compiled and presented as a comprehensive analysis

## Example Use Cases

- **Market Research**: Analyze trends in specific markets or industries
- **Competitive Analysis**: Research competitors and market positioning
- **Data Collection**: Gather information from multiple sources
- **Content Analysis**: Extract and analyze content from websites
- **Real-time Information**: Get up-to-date information from the web

## Configuration

The `mcp_servers.json` file configures the browser MCP server:

```json
{
  "mcpServers": {
    "browsermcp": {
      "command": "npx",
      "args": ["@browsermcp/mcp@latest"]
    }
  }
}
```

## Requirements

- Go 1.24.4+
- Node.js 18+ (for npx-based MCP servers)
- OpenAI API key in `.env` file
- Internet connection (for web browsing)

## Notes

- Browser automation tasks may take longer than simple API calls
- The default timeout is set to 15 minutes to allow for complex research
- The browser MCP server will automatically handle web interactions
- Some websites may have rate limiting or require authentication
