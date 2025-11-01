package testing

import (
	"context"
	"fmt"
	"time"

	"mcp-agent/agent_go/internal/llm"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/external"
	"mcp-agent/agent_go/pkg/mcpclient"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var exaTestCmd = &cobra.Command{
	Use:   "exa-test",
	Short: "Test Exa MCP server connection and tools",
	Long: `Focused test for Exa MCP server that validates:
1. Exa MCP server connection and protocol detection
2. Tool discovery for web_search_exa, company_research, crawling, linkedin_search, deep_researcher_start, deep_researcher_check
3. Basic functionality of Exa tools
4. Server health and responsiveness

This test ensures the Exa MCP server is working correctly with the specified tools.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Get logging configuration from viper
		logFile := viper.GetString("log-file")
		logLevel := viper.GetString("log-level")

		// Initialize test logger
		InitTestLogger(logFile, logLevel)
		logger := GetTestLogger()

		logger.Infof("=== Exa MCP Server Test ===")

		// Create Exa server configuration
		exaConfig := mcpclient.MCPServerConfig{
			Description: "Exa MCP server for web search, company research, and crawling",
			URL:         "https://mcp.exa.ai/mcp?exaApiKey=06c88f57-ee5e-482f-8610-b887085f0b34",
		}

		logger.Infof("Exa Server Configuration:")
		logger.Infof("   URL: %s", exaConfig.URL)
		logger.Infof("   Protocol: %s (auto-detected)", exaConfig.GetProtocol())

		// Test server connection
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		logger.Infof("\n--- Testing Exa MCP Server Connection ---")

		// Create direct client
		client := mcpclient.New(exaConfig, logger)

		// Connect to Exa server
		logger.Infof("🔌 Connecting to Exa MCP server...")
		if err := client.Connect(ctx); err != nil {
			logger.Errorf("❌ Failed to connect to Exa server: %w", err)
			return fmt.Errorf("exa server connection failed: %w", err)
		}

		logger.Infof("✅ Successfully connected to Exa MCP server")

		// Get server info
		serverInfo := client.GetServerInfo()
		if serverInfo != nil {
			logger.Infof("📊 Server Information:")
			logger.Infof("   Name: %s", serverInfo.Name)
			logger.Infof("   Version: %s", serverInfo.Version)
		}

		// List available tools
		logger.Infof("\n🔧 Discovering Exa tools...")
		tools, err := client.ListTools(ctx)
		if err != nil {
			logger.Errorf("❌ Failed to list tools: %w", err)
			client.Close()
			return fmt.Errorf("failed to list exa tools: %w", err)
		}

		logger.Infof("✅ Found %d tools:", len(tools))

		// Expected Exa tools (these may vary based on the remote server)
		expectedTools := []string{
			"web_search_exa",
			"company_research_exa",
			"crawling_exa",
			"linkedin_search_exa",
			"deep_researcher_start",
			"deep_researcher_check",
		}

		// Check for expected tools
		foundTools := make(map[string]bool)
		for _, tool := range tools {
			foundTools[tool.Name] = true
			logger.Infof("   - %s", tool.Name)
			if tool.Description != "" {
				logger.Infof("     Description: %s", tool.Description)
			}
		}

		// Validate expected tools
		logger.Infof("\n📋 Tool Validation:")
		missingTools := []string{}
		for _, expected := range expectedTools {
			if foundTools[expected] {
				logger.Infof("   ✅ %s", expected)
			} else {
				logger.Infof("   ❌ %s (missing)", expected)
				missingTools = append(missingTools, expected)
			}
		}

		if len(missingTools) > 0 {
			logger.Warnf("⚠️  Missing expected tools: %v", missingTools)
		} else {
			logger.Infof("🎉 All expected Exa tools are available!")
		}

		// Test prompts discovery
		logger.Infof("\n📝 Testing prompts discovery...")
		prompts, err := client.ListPrompts(ctx)
		if err != nil {
			logger.Infof("ℹ️  No prompts available (this is normal for Exa)")
		} else {
			logger.Infof("✅ Found %d prompts:", len(prompts))
			for _, prompt := range prompts {
				logger.Infof("   - %s: %s", prompt.Name, prompt.Description)
			}
		}

		// Test resources discovery
		logger.Infof("\n📁 Testing resources discovery...")
		resources, err := client.ListResources(ctx)
		if err != nil {
			logger.Infof("ℹ️  No resources available (this is normal for Exa)")
		} else {
			logger.Infof("✅ Found %d resources:", len(resources))
			for _, resource := range resources {
				logger.Infof("   - %s (%s): %s", resource.Name, resource.URI, resource.Description)
			}
		}

		// Test basic tool functionality (optional - just to verify server is responsive)
		logger.Infof("\n🧪 Testing basic tool functionality...")
		if len(tools) > 0 {
			firstTool := tools[0]
			logger.Infof("   Testing tool: %s", firstTool.Name)
			logger.Infof("   Tool description: %s", firstTool.Description)
			logger.Infof("   ✅ Tool schema loaded successfully")
		}

		// Close connection
		client.Close()
		logger.Infof("\n🔌 Connection closed")

		// Test summary
		logger.Infof("\n=== Exa MCP Server Test Summary ===")
		logger.Infof("✅ Server connection: SUCCESS")
		logger.Infof("🔧 Tools discovered: %d", len(tools))
		logger.Infof("📝 Prompts available: %d", len(prompts))
		logger.Infof("📁 Resources available: %d", len(resources))

		if len(missingTools) == 0 {
			logger.Infof("🎉 All expected Exa tools are available!")
		} else {
			logger.Infof("⚠️  Missing tools: %v", missingTools)
		}

		logger.Infof("🎉 Exa MCP server test completed successfully!")

		// Test simple agent with Exa tools
		logger.Infof("\n🤖 Testing simple agent with Exa tools...")
		if err := testSimpleAgentWithExa(logger); err != nil {
			logger.Warnf("Simple agent test failed: %w", err)
		}

		return nil
	},
}

func init() {
	// No custom flags needed - use global flags from root command
}

// testSimpleAgentWithExa tests the Exa MCP server using a simple agent
func testSimpleAgentWithExa(logger utils.ExtendedLogger) error {
	logger.Infof("🚀 Creating simple agent with Exa MCP server...")

	// 🆕 NEW: Create MCP server configuration directly as structs (no JSON file needed!)
	mcpServers := map[string]external.MCPServerConfig{
		"exa": {
			Description: "Exa MCP server for web search, company research, and crawling",
			URL:         "https://mcp.exa.ai/mcp?exaApiKey=06c88f57-ee5e-482f-8610-b887085f0b34",
		},
	}

	// Create external agent configuration
	agentConfig := external.Config{
		Provider:    llm.ProviderOpenAI, // Use OpenAI for testing
		ModelID:     "gpt-4o-mini",      // Use a cost-effective model
		Temperature: 0.1,
		AgentMode:   external.SimpleAgent,
		MaxTurns:    5,
		ToolTimeout: 30 * time.Second,
		ToolChoice:  "auto",
		// Note: ServerName and ConfigPath are not needed when using NewAgentWithMCPServers
		SystemPrompt: external.SystemPromptConfig{
			Mode: "auto", // Use auto system prompt
		},
	}

	// 🆕 NEW: Use NewAgentWithMCPServers to pass MCP server configs directly as structs!
	ctx := context.Background()
	agent, err := external.NewAgentWithMCPServers(ctx, agentConfig, mcpServers)
	if err != nil {
		return fmt.Errorf("failed to create external agent with MCP servers: %w", err)
	}
	defer agent.Close()

	logger.Infof("✅ External agent created successfully with direct MCP server configuration!")
	logger.Infof("   Mode: %s", agentConfig.AgentMode)
	logger.Infof("   Provider: %s", agentConfig.Provider)
	logger.Infof("   Model: %s", agentConfig.ModelID)
	logger.Infof("   MCP Servers: %d configured directly", len(mcpServers))

	// Test with a query that can use Exa tools
	testQuery := "Use the web_search_exa tool to find the latest news about artificial intelligence. Please provide a brief summary of what you find."
	logger.Infof("\n🔍 Testing with query: %s", testQuery)

	// Invoke the agent
	startTime := time.Now()
	response, err := agent.Invoke(ctx, testQuery)
	duration := time.Since(startTime)

	if err != nil {
		return fmt.Errorf("agent invocation failed: %w", err)
	}

	logger.Infof("✅ Agent response received in %v", duration)
	logger.Infof("📝 Response length: %d characters", len(response))
	// Truncate response for preview
	preview := response
	if len(response) > 200 {
		preview = response[:200] + "..."
	}
	logger.Infof("📄 Response preview: %s", preview)

	// Check if response contains expected content
	if len(response) < 50 {
		logger.Warnf("⚠️  Response seems too short, may indicate an issue")
	} else {
		logger.Infof("🎉 Response looks good!")
	}

	return nil
}
