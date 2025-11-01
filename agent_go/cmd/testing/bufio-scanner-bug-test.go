package testing

import (
	"context"
	"fmt"
	"strings"
	"time"

	"mcp-agent/agent_go/internal/llm"
	"mcp-agent/agent_go/internal/utils"
	agent "mcp-agent/agent_go/pkg/agentwrapper"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/mcpclient"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var bufioScannerBugTestCmd = &cobra.Command{
	Use:   "bufio-scanner-bug",
	Short: "Test to reproduce bufio.Scanner token too long bug with Playwright MCP server",
	Long: `Test to reproduce the bufio.Scanner: token too long bug that occurs when MCP stdio servers 
output large content (e.g., browser automation tools like Playwright).

This test specifically:
1. Uses Playwright MCP server (stdio protocol)
2. Performs browser automation that generates large outputs
3. Tests MoneyControl mutual funds search scenario
4. Captures and reports the bufio.Scanner error when it occurs

The bug occurs in the github.com/mark3labs/mcp-go library's NewStdioMCPClient() function
which uses bufio.Scanner with default 64KB buffer limit.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Get logging configuration from viper
		logFile := viper.GetString("log-file")
		logLevel := viper.GetString("log-level")

		// Initialize test logger
		InitTestLogger(logFile, logLevel)
		logger := GetTestLogger()

		logger.Info("=== Bufio.Scanner Token Too Long Bug Reproduction Test ===")
		logger.Info("This test attempts to reproduce the bufio.Scanner: token too long error")
		logger.Info("that occurs when MCP stdio servers output large content.")

		// Load MCP server configurations
		configPath := viper.GetString("config")
		if configPath == "" {
			configPath = "configs/mcp_servers_clean_user.json" // Use the config with Playwright
		}

		logger.Infof("Loading config from: %s", configPath)
		config, err := mcpclient.LoadMergedConfig(configPath, logger)
		if err != nil {
			return fmt.Errorf("failed to load merged config: %v", err)
		}

		// Test 1: Direct Playwright MCP Server Connection Test
		logger.Info("\n--- Test 1: Direct Playwright MCP Server Connection ---")
		if err := testDirectPlaywrightConnection(config, logger); err != nil {
			logger.Errorf("❌ Direct Playwright connection test failed: %v", err)
			// Continue with other tests even if this fails
		}

		// Test 2: Agent-based Playwright Test with Large Output Scenario
		logger.Info("\n--- Test 2: Agent-based Playwright Test (MoneyControl Scenario) ---")
		if err := testAgentPlaywrightScenario(config, logger); err != nil {
			logger.Errorf("❌ Agent-based Playwright test failed: %v", err)
			// Continue with other tests even if this fails
		}

		// Test 3: Large Output Stress Test
		logger.Info("\n--- Test 3: Large Output Stress Test ---")
		if err := testLargeOutputStress(config, logger); err != nil {
			logger.Errorf("❌ Large output stress test failed: %v", err)
			// Continue with other tests even if this fails
		}

		logger.Info("\n=== Bug Reproduction Test Summary ===")
		logger.Info("If you see 'bufio.Scanner: token too long' errors above,")
		logger.Info("the bug has been successfully reproduced!")
		logger.Info("The fix requires updating the MCP library to use larger buffers.")

		return nil
	},
}

// testDirectPlaywrightConnection tests direct connection to Playwright MCP server
func testDirectPlaywrightConnection(config *mcpclient.MCPConfig, logger utils.ExtendedLogger) error {
	// Find Playwright configuration
	var playwrightConfig *mcpclient.MCPServerConfig
	for serverName, serverConfig := range config.MCPServers {
		if strings.Contains(strings.ToLower(serverName), "playwright") {
			playwrightConfig = &serverConfig
			logger.Infof("Found Playwright config: %s", serverName)
			break
		}
	}

	if playwrightConfig == nil {
		return fmt.Errorf("no Playwright configuration found in config")
	}

	logger.Infof("Playwright config: Command=%s, Args=%v", playwrightConfig.Command, playwrightConfig.Args)
	logger.Infof("Protocol: %s", playwrightConfig.GetProtocol())

	// Create direct connection to Playwright MCP server
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	client := mcpclient.New(*playwrightConfig, logger)

	logger.Info("🔧 Attempting to connect to Playwright MCP server...")
	if err := client.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to Playwright MCP server: %v", err)
	}
	defer client.Close()

	logger.Info("✅ Connected to Playwright MCP server successfully")

	// List tools to see what's available
	logger.Info("🔍 Listing available Playwright tools...")
	tools, err := client.ListTools(ctx)
	if err != nil {
		return fmt.Errorf("failed to list Playwright tools: %v", err)
	}

	logger.Infof("Found %d Playwright tools:", len(tools))
	for i, tool := range tools {
		logger.Infof("  %d. %s", i+1, tool.Name)
		if tool.Description != "" {
			logger.Infof("     Description: %s", tool.Description)
		}
	}

	// Test a simple tool call that might generate large output
	logger.Info("🧪 Testing Playwright tool call that might generate large output...")

	// Try to navigate to a page (this might generate large HTML output)
	navigateResult, err := client.CallTool(ctx, "mcp_playwright_browser_navigate", map[string]interface{}{
		"url": "https://www.moneycontrol.com",
	})
	if err != nil {
		logger.Errorf("❌ Navigate tool call failed: %v", err)
		// This might be the bufio.Scanner error we're looking for
		if strings.Contains(err.Error(), "bufio.Scanner") && strings.Contains(err.Error(), "token too long") {
			logger.Error("🎯 SUCCESS! Reproduced bufio.Scanner: token too long error!")
			logger.Errorf("   Error details: %v", err)
		}
	} else {
		logger.Info("✅ Navigate tool call completed")
		logger.Infof("   Result: %s", mcpclient.ToolResultAsString(navigateResult, logger))
	}

	return nil
}

// testAgentPlaywrightScenario tests Playwright through the agent with MoneyControl scenario
func testAgentPlaywrightScenario(config *mcpclient.MCPConfig, logger utils.ExtendedLogger) error {
	// Configuration for agent test
	serverList := "playwright"
	configPath := viper.GetString("config")
	if configPath == "" {
		configPath = "configs/mcp_servers_clean_user.json"
	}
	provider := "bedrock"

	// Validate and get provider
	llmProvider, err := llm.ValidateProvider(provider)
	if err != nil {
		return fmt.Errorf("invalid LLM provider: %v", err)
	}

	// Set default model if not specified
	modelID := llm.GetDefaultModel(llmProvider)

	logger.Info("🤖 Agent Playwright Test Configuration", map[string]interface{}{
		"provider":    provider,
		"model":       modelID,
		"servers":     serverList,
		"config_path": configPath,
	})

	// Initialize tracer
	tracer := InitializeTracer(logger)

	// Create agent config
	agentConfig := agent.LLMAgentConfig{
		Name:        "Playwright Bug Test Agent",
		ServerName:  serverList,
		ConfigPath:  configPath,
		Provider:    llm.Provider(provider),
		ModelID:     modelID,
		Temperature: 0.2,
		ToolChoice:  "auto",
		MaxTurns:    10,
		Timeout:     3 * time.Minute,
		AgentMode:   mcpagent.SimpleAgent, // Use simple mode for testing
	}

	// Create a context with timeout for agent creation
	agentCtx, agentCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer agentCancel()

	logger.Info("🚀 Creating agent for Playwright bug test", map[string]interface{}{
		"agent_timeout": "30s",
		"config_path":   configPath,
		"server_list":   serverList,
	})

	// Create the agent wrapper
	testAgent, err := agent.NewLLMAgentWrapper(agentCtx, agentConfig, tracer, logger)
	if err != nil {
		return fmt.Errorf("failed to create agent: %v", err)
	}

	logger.Info("✅ Agent created successfully", map[string]interface{}{
		"agent_name": agentConfig.Name,
	})

	// Test MoneyControl mutual funds search scenario
	logger.Info("🧪 Testing MoneyControl mutual funds search scenario...")

	testQuery := `Use Playwright to navigate to MoneyControl website (https://www.moneycontrol.com) and search for mutual funds from Motilal Oswal. Take a screenshot of the results page and provide details about the mutual funds found. This should generate large HTML content and potentially trigger the bufio.Scanner token too long error.`

	logger.Infof("📝 Test query: %s", testQuery)

	// Create a context with timeout for the test
	testCtx, testCancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer testCancel()

	logger.Info("🔧 Executing test query (this might trigger the bufio.Scanner error)...")
	response, err := testAgent.InvokeWithHistory(testCtx, []llmtypes.MessageContent{
		{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: testQuery}},
		},
	})
	if err != nil {
		logger.Errorf("❌ Test query failed: %v", err)
		// Check if this is the bufio.Scanner error we're looking for
		if strings.Contains(err.Error(), "bufio.Scanner") && strings.Contains(err.Error(), "token too long") {
			logger.Error("🎯 SUCCESS! Reproduced bufio.Scanner: token too long error in agent!")
			logger.Errorf("   Error details: %v", err)
		}
		return err
	}

	logger.Info("✅ Test query completed successfully")
	logger.Infof("   Response length: %d characters", len(response))
	if len(response) > 500 {
		logger.Infof("   Response preview: %s...", response[:500])
	} else {
		logger.Infof("   Response: %s", response)
	}

	return nil
}

// testLargeOutputStress tests with scenarios that are likely to generate very large outputs
func testLargeOutputStress(config *mcpclient.MCPConfig, logger utils.ExtendedLogger) error {
	logger.Info("🧪 Testing large output stress scenarios...")

	// Find Playwright configuration
	var playwrightConfig *mcpclient.MCPServerConfig
	for serverName, serverConfig := range config.MCPServers {
		if strings.Contains(strings.ToLower(serverName), "playwright") {
			playwrightConfig = &serverConfig
			break
		}
	}

	if playwrightConfig == nil {
		return fmt.Errorf("no Playwright configuration found for stress test")
	}

	// Create direct connection
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	client := mcpclient.New(*playwrightConfig, logger)

	if err := client.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect for stress test: %v", err)
	}
	defer client.Close()

	// Test scenarios that might generate large outputs
	stressScenarios := []struct {
		name        string
		toolName    string
		parameters  map[string]interface{}
		description string
	}{
		{
			name:        "Navigate to large page",
			toolName:    "mcp_playwright_browser_navigate",
			parameters:  map[string]interface{}{"url": "https://www.moneycontrol.com/mutual-funds/"},
			description: "Navigate to MoneyControl mutual funds page (likely to have large HTML)",
		},
		{
			name:        "Take full page screenshot",
			toolName:    "mcp_playwright_browser_take_screenshot",
			parameters:  map[string]interface{}{"fullPage": true},
			description: "Take full page screenshot (large image output)",
		},
		{
			name:        "Get page content",
			toolName:    "mcp_playwright_browser_evaluate",
			parameters:  map[string]interface{}{"function": "() => document.documentElement.outerHTML"},
			description: "Get full HTML content (very large output)",
		},
	}

	for i, scenario := range stressScenarios {
		logger.Infof("\n--- Stress Test %d: %s ---", i+1, scenario.name)
		logger.Infof("Description: %s", scenario.description)

		logger.Infof("🔧 Executing %s...", scenario.toolName)
		result, err := client.CallTool(ctx, scenario.toolName, scenario.parameters)
		if err != nil {
			logger.Errorf("❌ %s failed: %v", scenario.name, err)
			// Check if this is the bufio.Scanner error
			if strings.Contains(err.Error(), "bufio.Scanner") && strings.Contains(err.Error(), "token too long") {
				logger.Error("🎯 SUCCESS! Reproduced bufio.Scanner: token too long error in stress test!")
				logger.Errorf("   Scenario: %s", scenario.name)
				logger.Errorf("   Error details: %v", err)
			}
		} else {
			logger.Infof("✅ %s completed", scenario.name)
			resultStr := mcpclient.ToolResultAsString(result, logger)
			logger.Infof("   Result length: %d characters", len(resultStr))
			if len(resultStr) > 1000 {
				logger.Infof("   Result preview: %s...", resultStr[:1000])
			} else {
				logger.Infof("   Result: %s", resultStr)
			}
		}
	}

	return nil
}

func init() {
	bufioScannerBugTestCmd.Flags().String("config", "configs/mcp_servers_clean_user.json", "Path to MCP server configuration file")
	viper.BindPFlag("config", bufioScannerBugTestCmd.Flags().Lookup("config"))
	TestingCmd.AddCommand(bufioScannerBugTestCmd)
}
