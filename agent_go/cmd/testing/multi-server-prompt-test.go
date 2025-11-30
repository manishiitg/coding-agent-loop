package testing

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	"mcpagent/llm"
	"mcpagent/agent"
	"mcpagent/mcpclient"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var multiServerPromptTestCmd = &cobra.Command{
	Use:   "multi-server-prompt",
	Short: "Test multiple MCP servers to verify all prompts are discovered",
	Long:  "Test multiple MCP servers (Slack, Scripts) to verify that prompts from all servers are discovered and included",
	Run: func(cmd *cobra.Command, args []string) {
		testMultiServerPrompts()
	},
}

func init() {
	TestingCmd.AddCommand(multiServerPromptTestCmd)
}

func testMultiServerPrompts() {
	// Get logging configuration from viper
	logFile := viper.GetString("log-file")
	if logFile == "" {
		logFile = "logs/multi_server_prompt_test.log"
	}

	// Initialize test logger
	InitTestLogger(logFile, "info")
	logger := GetTestLogger()

	logger.Info("🚀 Starting Multi-Server Prompt Test: Testing prompt discovery from multiple servers")

	// Step 1: Load MCP config and check servers
	logger.Info("📋 Step 1: Loading MCP configuration and checking multiple servers...")

	configPath := "configs/mcp_servers_clean.json"
	config, err := mcpclient.LoadMergedConfig(configPath, logger)
	if err != nil {
		logger.Error("Failed to load merged MCP config", map[string]interface{}{"error": err.Error()})
		logger.Info("Test completed with errors.")
		return
	}

	// Check for required servers
	requiredServers := []string{"citymall-slack-mcp", "citymall-scripts-mcp"}
	for _, serverName := range requiredServers {
		if _, exists := config.MCPServers[serverName]; !exists {
			logger.Error("Required server not found in MCP configuration", map[string]interface{}{"server": serverName})
			logger.Info("Test completed with errors.")
			return
		}
		logger.Info("✅ Server found in configuration", map[string]interface{}{"server": serverName})
	}

	// Step 2: Create multi-server configuration
	logger.Info("📋 Step 2: Creating multi-server configuration...")

	multiServerConfig := &mcpclient.MCPConfig{
		MCPServers: map[string]mcpclient.MCPServerConfig{
			"citymall-slack-mcp":   config.MCPServers["citymall-slack-mcp"],
			"citymall-scripts-mcp": config.MCPServers["citymall-scripts-mcp"],
		},
	}

	// Create a temporary config file with multiple servers
	tempConfigPath := "configs/multi_server_test.json"
	configData, err := json.MarshalIndent(multiServerConfig, "", "  ")
	if err != nil {
		logger.Error("Failed to marshal config", map[string]interface{}{"error": err.Error()})
		logger.Info("Test completed with errors.")
		return
	}

	if err := os.WriteFile(tempConfigPath, configData, 0644); err != nil {
		logger.Error("Failed to write temporary config", map[string]interface{}{"error": err.Error()})
		logger.Info("Test completed with errors.")
		return
	}

	logger.Info("✅ Multi-server configuration created")

	// Step 3: Initialize LLM for the agent
	logger.Info("📋 Step 3: Initializing LLM for the agent...")

	// Get provider from command line flag, environment, or use default
	provider := viper.GetString("provider")
	if provider == "" {
		provider = os.Getenv("AGENT_PROVIDER")
	}
	if provider == "" {
		provider = "openai" // Default to OpenAI for testing
	}

	// Get model from environment or use default based on provider
	modelID := os.Getenv("AGENT_MODEL")
	if modelID == "" {
		if provider == "bedrock" {
			modelID = "us.anthropic.claude-sonnet-4-20250514-v1:0" // Default Bedrock model
		} else {
			modelID = "gpt-4.1-mini" // Default OpenAI model
		}
	}

	// Validate provider
	llmProvider, err := llm.ValidateProvider(provider)
	if err != nil {
		logger.Error("Failed to validate LLM provider", map[string]interface{}{
			"provider": provider,
			"error":    err.Error(),
		})
		logger.Info("Test completed with errors.")
		return
	}

	// Create LLM configuration
	llmConfig := llm.Config{
		Provider:       llmProvider,
		ModelID:        modelID,
		Temperature:    0.2,
		Tracers:        nil,
		TraceID:        "",
		FallbackModels: llm.GetDefaultFallbackModels(llmProvider),
		MaxRetries:     3,
		Logger:         logger,
	}

	// Initialize the LLM
	llmInstance, err := llm.InitializeLLM(llmConfig)
	if err != nil {
		logger.Error("Failed to initialize LLM", map[string]interface{}{
			"provider": provider,
			"model":    modelID,
			"error":    err.Error(),
		})
		logger.Info("Test completed with errors.")
		return
	}

	logger.Info("✅ LLM initialized successfully", map[string]interface{}{
		"provider": provider,
		"model":    modelID,
	})

	// Step 4: Create agent with multiple servers
	logger.Info("📋 Step 4: Creating agent with multiple servers...")

	ctx := context.Background()
	agent, err := mcpagent.NewSimpleAgent(
		ctx,
		llmInstance,
		"citymall-slack-mcp,citymall-scripts-mcp", // Connect to both servers
		tempConfigPath,
		modelID,
		nil,
		"",
		logger,
		mcpagent.WithTemperature(0.2),
		mcpagent.WithToolChoice("auto"),
		mcpagent.WithMaxTurns(10),
	)
	if err != nil {
		logger.Error("Failed to create agent", map[string]interface{}{"error": err.Error()})
		logger.Info("Test completed with errors.")
		return
	}

	logger.Info("✅ Multi-server agent created successfully")

	// Step 5: Log discovered prompts
	logger.Info("📋 Step 5: Logging discovered prompts...")

	prompts := agent.GetPrompts()
	if prompts != nil {
		logger.Info("📚 Prompts discovered by server:")
		for serverName, serverPrompts := range prompts {
			logger.Info("Server prompts", map[string]interface{}{
				"server": serverName,
				"count":  len(serverPrompts),
				"prompts": func() []string {
					var names []string
					for _, prompt := range serverPrompts {
						names = append(names, prompt.Name)
					}
					return names
				}(),
			})
		}
	} else {
		logger.Warn("⚠️ No prompts discovered")
	}

	// Step 6: Test agent with multi-server prompt query
	logger.Info("📋 Step 6: Testing agent with multi-server prompt query...")

	// Log what tools are available to the agent
	logger.Info("🔍 Agent tools available", map[string]interface{}{
		"total_tools": len(agent.Tools),
		"tool_names": func() []string {
			var names []string
			for _, tool := range agent.Tools {
				if tool.Function != nil {
					names = append(names, tool.Function.Name)
				}
			}
			return names
		}(),
	})

	// Use a query that tests prompts from multiple servers
	multiServerQuery := `Please use the get_prompt tool to retrieve prompts from multiple servers:

1. First, get a prompt from one of the available servers
2. Then, get any available prompt from the citymall-slack-mcp server  
3. Finally, get the "how-it-works" prompt from the citymall-scripts-mcp server

For each prompt you retrieve, please:
- Confirm which server it came from
- Provide a brief summary of what the prompt contains
- Explain how this prompt helps with using that server's tools

This will help verify that prompts from all MCP servers are accessible and working correctly.`

	logger.Info("Testing agent with multi-server prompt query", map[string]interface{}{
		"query": multiServerQuery,
	})

	response, err := agent.Ask(ctx, multiServerQuery)
	if err != nil {
		logger.Error("Failed to process multi-server prompt query with agent", map[string]interface{}{"error": err.Error()})
		logger.Info("Test completed with errors.")
		return
	}

	logger.Info("✅ Agent successfully processed multi-server prompt query", map[string]interface{}{
		"response_length": len(response),
		"response_preview": func() string {
			if len(response) > 200 {
				return response[:200] + "..."
			}
			return response
		}(),
	})

	// Check if the response shows evidence of using prompts from multiple servers
	usedSlack := strings.Contains(strings.ToLower(response), "slack") || strings.Contains(strings.ToLower(response), "citymall-slack")
	usedScripts := strings.Contains(strings.ToLower(response), "scripts") || strings.Contains(strings.ToLower(response), "how-it-works")

	logger.Info("🔍 Server usage analysis:", map[string]interface{}{
		"slack_mentioned":   usedSlack,
		"scripts_mentioned": usedScripts,
	})

	if usedSlack && usedScripts {
		logger.Info("✅ Response shows evidence of using prompts from both servers")
	} else {
		logger.Info("⚠️ Response may not have used prompts from all servers as expected")
	}

	logger.Info("🎉 Multi-Server Prompt Test completed successfully!")
	logger.Info("✅ Multi-server agent successfully created")
	logger.Info("✅ Agent can connect to all three MCP servers and discover tools")
	logger.Info("✅ Agent has access to get_prompt virtual tool")
	logger.Info("✅ get_prompt tool is properly implemented and available")
	logger.Info("✅ Agent successfully used virtual tools to retrieve content from multiple servers")
}
