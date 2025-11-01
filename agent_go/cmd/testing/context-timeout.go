package testing

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"mcp-agent/agent_go/internal/llm"
	"mcp-agent/agent_go/internal/utils"
	agent "mcp-agent/agent_go/pkg/agentwrapper"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/mcpclient"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var contextTimeoutTestCmd = &cobra.Command{
	Use:   "context-timeout",
	Short: "Test MCP timeout server with simple agent via stdio",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Get logging configuration from viper
		logFile := viper.GetString("log-file")
		logLevel := viper.GetString("log-level")

		// Initialize test logger
		InitTestLogger(logFile, logLevel)
		logger := GetTestLogger()

		logger.Info("=== MCP Timeout Server Test ===")

		// Create timeout server configuration for stdio using compiled binary
		timeoutConfig := mcpclient.MCPServerConfig{
			Command:  "/Users/mipl/ai-work/mcp-agent/bin/timeout-server",
			Args:     []string{},
			Protocol: mcpclient.ProtocolStdio,
		}

		logger.Info("Starting timeout server test with stdio transport")

		// Create direct connection to timeout server
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		client := mcpclient.New(timeoutConfig, logger)

		if err := client.Connect(ctx); err != nil {
			return fmt.Errorf("failed to connect to timeout server: %w", err)
		}
		defer client.Close()

		logger.Info("✅ Connected to timeout server successfully")

		// Test 1: List available tools
		logger.Info("\n--- Test 1: List Tools ---")
		if err := testListTimeoutTools(ctx, client, logger); err != nil {
			return fmt.Errorf("list tools test failed: %w", err)
		}

		// Test 2: Use simple agent to call mock_timeout tool
		logger.Info("\n--- Test 2: Simple Agent with Timeout Tool ---")
		if err := testSimpleAgentWithTimeout(ctx, timeoutConfig, logger); err != nil {
			return fmt.Errorf("mock timeout tool test failed: %w", err)
		}

		logger.Info("\n✅ All timeout server tests passed!")
		return nil
	},
}

func testListTimeoutTools(ctx context.Context, client *mcpclient.Client, logger utils.ExtendedLogger) error {
	// List tools with details
	tools, err := client.ListTools(ctx)
	if err != nil {
		return fmt.Errorf("failed to list tools: %w", err)
	}

	logger.Infof("Found %d tools:", len(tools))
	for i, tool := range tools {
		logger.Infof("  %d. %s", i+1, tool.Name)
		if tool.Description != "" {
			logger.Infof("     Description: %s", tool.Description)
		}
	}

	// Check for expected tool
	expectedTool := "mock_timeout"
	found := false
	for _, tool := range tools {
		if tool.Name == expectedTool {
			found = true
			logger.Infof("✅ Found expected tool: %s", expectedTool)
			break
		}
	}

	if !found {
		logger.Infof("❌ Expected tool '%s' not found", expectedTool)
		return fmt.Errorf("expected tool '%s' not found", expectedTool)
	}

	return nil
}

func testSimpleAgentWithTimeout(ctx context.Context, timeoutConfig mcpclient.MCPServerConfig, logger utils.ExtendedLogger) error {
	logger.Info("Creating simple agent to call mock_timeout tool...")

	// Create a temporary config file for the timeout server
	tempConfigPath := "configs/timeout_test_config.json"
	timeoutConfigData := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"timeout-server": map[string]interface{}{
				"command":  timeoutConfig.Command,
				"args":     timeoutConfig.Args,
				"protocol": "stdio",
			},
		},
	}

	// Write config to file
	configJSON, err := json.MarshalIndent(timeoutConfigData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(tempConfigPath, configJSON, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	defer os.Remove(tempConfigPath)

	// Initialize tracer
	// Initialize tracer based on environment (Langfuse if available, otherwise noop)
	tracer := InitializeTracer(logger)

	// Create agent configuration
	agentConfig := agent.LLMAgentConfig{
		Name:        "Timeout Test Agent",
		ServerName:  "timeout-server",
		ConfigPath:  tempConfigPath,
		Provider:    llm.Provider("openai"),
		ModelID:     "gpt-4o-mini",
		Temperature: 0.2,
		ToolChoice:  "auto",
		MaxTurns:    5,
		Timeout:     30 * time.Second,
		ToolTimeout: 5 * time.Second,      // Set tool timeout to 5 seconds for testing
		AgentMode:   mcpagent.SimpleAgent, // Use simple mode
	}

	// Create the agent wrapper
	timeoutAgent, err := agent.NewLLMAgentWrapper(ctx, agentConfig, tracer, logger)
	if err != nil {
		return fmt.Errorf("failed to create agent: %w", err)
	}
	defer func() {
		if err := timeoutAgent.Stop(context.Background()); err != nil {
			logger.Warn("⚠️ Error stopping timeout agent", map[string]interface{}{"error": err.Error()})
		}
	}()

	// User message that prompts the agent to use the timeout tool
	userMessage := "Please call the mock_timeout tool to test timeout functionality. This tool will sleep for 30 seconds."

	logger.Info("Starting agent conversation...")
	logger.Infof("User message: %s", userMessage)

	// Record start time
	startTime := time.Now()

	// Run the agent
	result, err := timeoutAgent.Invoke(ctx, userMessage)
	if err != nil {
		return fmt.Errorf("agent failed: %w", err)
	}

	// Record end time
	endTime := time.Now()
	duration := endTime.Sub(startTime)

	logger.Infof("✅ Agent conversation completed in %v", duration)
	logger.Infof("Final response: %s", result)

	// Verify the duration is reasonable (should be around 5 seconds due to tool timeout + processing time)
	if duration < 4*time.Second {
		return fmt.Errorf("conversation completed too quickly: %v (expected ~5+ seconds due to tool timeout)", duration)
	}

	if duration > 15*time.Second {
		return fmt.Errorf("conversation took too long: %v (expected ~5-10 seconds due to tool timeout)", duration)
	}

	logger.Infof("✅ Duration validation passed: %v", duration)
	return nil
}

func init() {
	TestingCmd.AddCommand(contextTimeoutTestCmd)
}
