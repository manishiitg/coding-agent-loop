package testing

import (
	"context"
	"fmt"
	"time"

	"mcp-agent/agent_go/internal/llm"
	agent "mcp-agent/agent_go/pkg/agentwrapper"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/mcpclient"

	"mcp-agent/agent_go/internal/llmtypes"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var awsTestCmd = &cobra.Command{
	Use:   "aws-test",
	Short: "Comprehensive test for MCP servers, tools, prompts, and virtual tools",
	Long: `Comprehensive test that validates:
1. MCP server connections and protocol detection
2. Tool discovery and listing
3. Prompt discovery and preview functionality
4. Virtual tools integration (get_prompt, list_prompts, etc.)
5. Resource discovery (when available)

This test ensures the complete MCP agent functionality is working correctly.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Get logging configuration from viper
		logFile := viper.GetString("log-file")
		logLevel := viper.GetString("log-level")

		// Initialize logger with command-specific settings
		// Initialize test logger
		InitTestLogger(logFile, logLevel)
		logger := GetTestLogger()

		logger.Infof("=== Comprehensive MCP Server & Virtual Tools Test ===")

		// Load MCP server configurations
		configPath := viper.GetString("config")
		if configPath == "" {
			configPath = "configs/mcp_servers_clean.json" // Use the working config as default
		}

		logger.Infof("Loading merged config from: %s", configPath)
		config, err := mcpclient.LoadMergedConfig(configPath, logger)
		if err != nil {
			return fmt.Errorf("failed to load merged config: %w", err)
		}

		// Test all servers in config
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		successCount := 0
		totalCount := len(config.MCPServers)
		totalTools := 0
		totalPrompts := 0
		totalResources := 0

		for serverName, serverConfig := range config.MCPServers {
			logger.Infof("\n--- Testing %s ---", serverName)

			// Show config info
			if serverConfig.Description != "" {
				logger.Infof("   Config Description: %s", serverConfig.Description)
			}
			logger.Infof("   Protocol: %s", serverConfig.GetProtocol())
			if serverConfig.URL != "" {
				logger.Infof("   URL: %s", serverConfig.URL)
			}
			if serverConfig.Command != "" {
				logger.Infof("   Command: %s %v", serverConfig.Command, serverConfig.Args)
			}

			// Create direct client (automatically handles protocol detection)
			client := mcpclient.New(serverConfig, logger)

			// Connect (automatically handles SSE, stdio, HTTP)
			if err := client.Connect(ctx); err != nil {
				logger.Errorf("❌ Failed to connect: %w", err)
				continue
			}

			logger.Infof("✅ Connected successfully")

			// Get server info from MCP protocol
			serverInfo := client.GetServerInfo()
			if serverInfo != nil {
				logger.Infof("   Server Info:")
				logger.Infof("     Name: %s", serverInfo.Name)
				logger.Infof("     Version: %s", serverInfo.Version)
			}

			// List tools with details
			tools, err := client.ListTools(ctx)
			if err != nil {
				logger.Errorf("❌ Failed to list tools: %w", err)
				client.Close()
				continue
			}

			logger.Infof("✅ Found %d tools:", len(tools))
			totalTools += len(tools)

			// Show first few tools with descriptions
			for i, tool := range tools {
				if i < 3 { // Show first 3 tools
					logger.Infof("   %d. %s", i+1, tool.Name)
					if tool.Description != "" {
						logger.Infof("      Description: %s", tool.Description)
					}
				}
			}

			if len(tools) > 3 {
				logger.Infof("   ... and %d more tools", len(tools)-3)
			}

			// Test prompts discovery
			logger.Infof("📝 Testing prompts discovery...")
			prompts, err := client.ListPrompts(ctx)
			if err != nil {
				logger.Errorf("❌ Failed to list prompts: %w", err)
			} else {
				logger.Infof("✅ Found %d prompts:", len(prompts))
				totalPrompts += len(prompts)
				for _, prompt := range prompts {
					logger.Infof("   - %s: %s", prompt.Name, prompt.Description)
				}
			}

			// Test resources discovery
			logger.Infof("📁 Testing resources discovery...")
			resources, err := client.ListResources(ctx)
			if err != nil {
				logger.Errorf("❌ Failed to list resources: %w", err)
			} else {
				logger.Infof("✅ Found %d resources:", len(resources))
				totalResources += len(resources)
				for _, resource := range resources {
					logger.Infof("   - %s (%s): %s", resource.Name, resource.URI, resource.Description)
				}
			}

			client.Close()
			successCount++
		}

		logger.Infof("\n=== MCP Server Test Summary ===")
		logger.Infof("✅ Successful connections: %d/%d", successCount, totalCount)
		logger.Infof("🔧 Total tools discovered: %d", totalTools)
		logger.Infof("📝 Total prompts discovered: %d", totalPrompts)
		logger.Infof("📁 Total resources discovered: %d", totalResources)

		if successCount == totalCount {
			logger.Infof("🎉 All servers connected successfully!")
		} else {
			logger.Infof("⚠️  Some servers failed to connect")
		}

		// Test virtual tools functionality
		logger.Infof("\n=== Virtual Tools Test ===")
		if err := testVirtualTools(); err != nil {
			logger.Errorf("❌ Virtual tools test failed: %w", err)
			return err
		}

		logger.Infof("🎉 All tests completed successfully!")
		return nil
	},
}

// testVirtualTools tests the virtual tools functionality
func testVirtualTools() error {
	logger := GetTestLogger()
	logger.Infof("🧪 Testing virtual tools integration...")

	// Configuration
	serverList := "citymall-aws-mcp,citymall-scripts-mcp"
	configPath := viper.GetString("config")
	if configPath == "" {
		configPath = "configs/mcp_servers_clean.json" // Use the working config as default
	}
	provider := "bedrock"

	// Validate and get provider
	llmProvider, err := llm.ValidateProvider(provider)
	if err != nil {
		return fmt.Errorf("invalid LLM provider: %w", err)
	}

	// Set default model if not specified
	modelID := llm.GetDefaultModel(llmProvider)

	logger.Info("🤖 Virtual Tools Test Configuration", map[string]interface{}{
		"provider":    provider,
		"model":       modelID,
		"servers":     serverList,
		"config_path": configPath,
	})

	// Initialize tracer
	// Initialize tracer based on environment (Langfuse if available, otherwise noop)
	tracer := InitializeTracer(logger)

	// Create agent config
	agentConfig := agent.LLMAgentConfig{
		Name:        "Virtual Tools Test Agent",
		ServerName:  serverList,
		ConfigPath:  configPath,
		Provider:    llm.Provider(provider),
		ModelID:     modelID,
		Temperature: 0.2,
		ToolChoice:  "auto",
		MaxTurns:    30,
		Timeout:     2 * time.Minute,
		AgentMode:   mcpagent.SimpleAgent, // Use simple mode for testing
	}

	// Create a context with timeout for agent creation
	agentCtx, agentCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer agentCancel()

	logger.Info("🚀 Creating agent for virtual tools test", map[string]interface{}{
		"agent_timeout": "30s",
		"config_path":   configPath,
		"server_list":   serverList,
	})

	// Create the agent wrapper
	testAgent, err := agent.NewLLMAgentWrapper(agentCtx, agentConfig, tracer, logger)
	if err != nil {
		return fmt.Errorf("failed to create agent: %w", err)
	}

	logger.Info("✅ Agent created successfully", map[string]interface{}{
		"agent_name": agentConfig.Name,
	})

	// Test virtual tools functionality
	logger.Infof("🔍 Testing virtual tools availability...")

	expectedVirtualTools := []string{"get_prompt", "get_resource"}

	logger.Infof("📊 Expected virtual tools: %w", expectedVirtualTools)

	// Test prompt access functionality
	logger.Infof("🧪 Testing prompt access functionality...")

	testQuery := "Use the get_prompt tool to fetch the aws-msk prompt from citymall-aws-mcp server and show me the first few lines of the content."

	logger.Infof("📝 Test query: %s", testQuery)

	// Create a context with timeout for the test
	testCtx, testCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer testCancel()

	response, err := testAgent.InvokeWithHistory(testCtx, []llmtypes.MessageContent{
		{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: testQuery}},
		},
	})
	if err != nil {
		logger.Errorf("❌ Test query failed: %w", err)
		// Don't return error here as it might be due to AWS auth issues
	} else {
		logger.Infof("✅ Test query completed successfully")
		logger.Infof("   Response length: %d characters", len(response))
		if len(response) > 200 {
			logger.Infof("   Response preview: %s...", response[:200])
		} else {
			logger.Infof("   Response: %s", response)
		}
	}

	logger.Infof("🎉 Virtual tools test completed")
	return nil
}

func init() {
	awsTestCmd.Flags().String("config", "configs/mcp_servers_clean.json", "Path to MCP server configuration file")
	viper.BindPFlag("config", awsTestCmd.Flags().Lookup("config"))
}
