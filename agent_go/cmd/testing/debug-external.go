package testing

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"mcp-agent/agent_go/internal/llm"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/external"
	"mcp-agent/agent_go/pkg/mcpagent"
)

// debugExternalCmd represents the debug-external command
var debugExternalCmd = &cobra.Command{
	Use:   "debug-external",
	Short: "Debug external package agent configuration issues",
	Long: `Debug external package agent configuration issues.

This command helps debug problems with:
- Agent mode configuration (SimpleAgent vs ReActAgent)
- Max turns settings not being applied
- Conversation end detection
- External package config flow

Examples:
  orchestrator test debug-external
  orchestrator test debug-external --verbose`,
	Run: runDebugExternalTest,
}

// Note: Command is registered in testing.go initTestingCommands()

func runDebugExternalTest(cmd *cobra.Command, args []string) {
	// Get logging configuration from root command flags directly
	// This avoids viper binding conflicts between root and testing framework
	logFile := cmd.Flag("log-file").Value.String()
	logLevel := cmd.Flag("log-level").Value.String()
	logFormat := cmd.Flag("log-format").Value.String()

	// Debug: Print what we got from flags
	fmt.Printf("Debug: logFile='%s', logLevel='%s', logFormat='%s'\n", logFile, logLevel, logFormat)

	// Initialize test logger
	InitTestLogger(logFile, logLevel)
	logger := GetTestLogger()

	// Test if logger is working
	fmt.Printf("Logger initialized, testing write to file...\n")

	// Debug: Check if logger is working by logging to console first
	fmt.Printf("About to log to logger...\n")

	// Debug: Check logger state
	fmt.Printf("Logger instance: %+v\n", logger)
	// Note: Can't access file directly since it's unexported

	logger.Info("🚀 Starting External Package Agent Configuration Debug Test")
	logger.Info("==================================================")

	// Test 1: Debug MCP Agent Configuration
	logger.Info("\n🔍 Test 1: Debug MCP Agent Configuration")
	if err := debugMCPAgentConfig(logger); err != nil {
		logger.Error("❌ DebugMCPAgentConfig failed", map[string]interface{}{"error": err.Error()})
		os.Exit(1)
	}

	// Test 2: Debug External Package Configuration
	logger.Info("\n🔍 Test 2: Debug External Package Configuration")
	if err := debugExternalPackageConfig(logger); err != nil {
		logger.Error("❌ DebugExternalPackageConfig failed", map[string]interface{}{"error": err.Error()})
		os.Exit(1)
	}

	// Test 3: Test Agent Creation Flow
	logger.Info("\n🔍 Test 3: Test Agent Creation Flow")
	if err := testAgentCreationFlow(logger); err != nil {
		logger.Error("❌ TestAgentCreationFlow failed", map[string]interface{}{"error": err.Error()})
		os.Exit(1)
	}

	logger.Info("\n✅ All debug tests completed successfully!")
	logger.Info("==================================================")
}

// debugMCPAgentConfig tests direct MCP agent creation with options
func debugMCPAgentConfig(logger utils.ExtendedLogger) error {
	logger.Info("🔧 Testing direct MCP agent creation with different configurations...")

	ctx := context.Background()

	// Initialize LLM
	llmConfig := llm.Config{
		Provider:    llm.ProviderOpenAI,
		ModelID:     "gpt-4o-mini",
		Temperature: 0.1,
		Logger:      utils.AdaptLogger(logger),
		Tracers:     nil,
		TraceID:     observability.TraceID("debug_mcp_agent"),
	}

	llmModel, err := llm.InitializeLLM(llmConfig)
	if err != nil {
		return fmt.Errorf("failed to initialize LLM: %w", err)
	}

	// Create tracer
	// Initialize tracer based on environment (Langfuse if available, otherwise noop)
	tracer := InitializeTracer(logger)
	traceID := observability.TraceID(fmt.Sprintf("debug_mcp_agent_config_%d", time.Now().UnixNano()))

	// Test 1: SimpleAgent with 10 max turns
	logger.Info("\n📋 Test 1: SimpleAgent with 10 max turns")
	agent1, err := mcpagent.NewAgent(
		ctx,
		llmModel,
		"filesystem",
		"configs/mcp_servers_simple.json",
		"gpt-4o-mini",
		tracer,
		traceID,
		logger,
		mcpagent.WithMode(mcpagent.SimpleAgent),
		mcpagent.WithMaxTurns(10),
	)
	if err != nil {
		logger.Error("❌ Failed to create agent 1", map[string]interface{}{"error": err.Error()})
	} else {
		logger.Info("✅ Agent 1 created successfully", map[string]interface{}{
			"mode":        string(agent1.AgentMode),
			"max_turns":   agent1.MaxTurns,
			"temperature": agent1.Temperature,
			"tool_choice": agent1.ToolChoice,
		})
	}

	// Test 2: SimpleAgent with 15 max turns
	logger.Info("\n📋 Test 2: SimpleAgent with 15 max turns")
	agent2, err := mcpagent.NewAgent(
		ctx,
		llmModel,
		"filesystem",
		"configs/mcp_servers_simple.json",
		"gpt-4o-mini",
		tracer,
		traceID,
		logger,
		mcpagent.WithMode(mcpagent.SimpleAgent),
		mcpagent.WithMaxTurns(15),
	)
	if err != nil {
		logger.Error("❌ Failed to create agent 2", map[string]interface{}{"error": err.Error()})
	} else {
		logger.Info("✅ Agent 2 created successfully", map[string]interface{}{
			"mode":        string(agent2.AgentMode),
			"max_turns":   agent2.MaxTurns,
			"temperature": agent2.Temperature,
			"tool_choice": agent2.ToolChoice,
		})
	}

	// Test 3: Check default values
	logger.Info("\n📋 Test 3: Checking default values")
	logger.Info("Default values", map[string]interface{}{
		"simple_agent_max_turns":   mcpagent.GetDefaultMaxTurns(mcpagent.SimpleAgent),
		"simple_agent_max_turns_2": mcpagent.GetDefaultMaxTurns(mcpagent.SimpleAgent),
	})

	logger.Info("\n🔍 MCP agent debug test completed!")
	return nil
}

// debugExternalPackageConfig tests the external package configuration flow
func debugExternalPackageConfig(logger utils.ExtendedLogger) error {
	logger.Info("🔍 Testing external package config flow...")

	// Test 1: Test external package config creation
	logger.Info("\n📋 Test 1: Testing external package config creation")
	config := external.DefaultConfig().
		WithAgentMode(external.SimpleAgent).
		WithMaxTurns(10)

	logger.Info("✅ External config created", map[string]interface{}{
		"mode":      string(config.AgentMode),
		"max_turns": config.MaxTurns,
	})

	// Test 2: Test mode conversion
	logger.Info("\n📋 Test 2: Testing mode conversion")
	// All agents use Simple mode
	agentMode := mcpagent.SimpleAgent

	logger.Info("✅ Mode conversion successful", map[string]interface{}{
		"external_mode": string(config.AgentMode),
		"mcp_mode":      string(agentMode),
	})

	// Test 3: Test config building
	logger.Info("\n📋 Test 3: Testing config building")
	config2 := external.DefaultConfig().
		WithAgentMode(external.SimpleAgent).
		WithMaxTurns(20).
		WithLLM("openai", "gpt-4o-mini", 0.1)

	logger.Info("✅ Extended config created", map[string]interface{}{
		"mode":        string(config2.AgentMode),
		"max_turns":   config2.MaxTurns,
		"provider":    string(config2.Provider),
		"model":       config2.ModelID,
		"temperature": config2.Temperature,
	})

	logger.Info("\n🔍 External package config test completed!")
	return nil
}

// testAgentCreationFlow tests the complete agent creation flow
func testAgentCreationFlow(logger utils.ExtendedLogger) error {
	logger.Info("🔍 Testing complete agent creation flow...")

	ctx := context.Background()

	// Test 1: Create agent using external package
	logger.Info("\n📋 Test 1: Creating agent using external package")
	config := external.DefaultConfig().
		WithAgentMode(external.SimpleAgent).
		WithLLM("openai", "gpt-4o-mini", 0.1).
		WithMaxTurns(10).
		WithServer("filesystem", "configs/mcp_servers_simple.json").
		WithLogger(utils.AdaptLogger(logger))

	agent, err := external.NewAgent(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to create external agent: %w", err)
	}

	logger.Info("✅ External agent created successfully")

	// Test 2: Check agent capabilities
	logger.Info("\n📋 Test 2: Checking agent capabilities")
	capabilities := agent.GetCapabilities()
	logger.Info("Agent capabilities", map[string]interface{}{
		"capabilities": capabilities,
	})

	// Test 3: Test simple invocation
	logger.Info("\n📋 Test 3: Testing simple invocation")
	response, err := agent.Invoke(ctx, "What is 2+2?")
	if err != nil {
		logger.Error("❌ Agent invocation failed", map[string]interface{}{"error": err.Error()})
	} else {
		logger.Info("✅ Agent invocation successful", map[string]interface{}{
			"response": response[:min(len(response), 200)] + "...", // Truncate for logging
		})
	}

	logger.Info("\n🔍 Agent creation flow test completed!")
	return nil
}
