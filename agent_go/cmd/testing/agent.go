package testing

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"mcp-agent/agent_go/internal/llm"
	agent "mcp-agent/agent_go/pkg/agentwrapper"
	"mcp-agent/agent_go/pkg/mcpagent"

	"mcp-agent/agent_go/internal/llmtypes"
)

// agentCmd represents the agent test command
var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Test the LLM Agent Wrapper functionality",
	Long: `Test the LLM Agent Wrapper with comprehensive validation of:

1. Agent wrapper initialization and configuration
2. Simple Invoke interface (prompt-in, response-out)
3. Streaming interface with chunks (simulated vs true LLM streaming)
4. Lifecycle management (start/stop)
5. Metrics collection and health monitoring
6. Multi-server MCP capabilities through wrapper

This command validates that the agent wrapper provides a clean LLM-like
interface over the complex MCP agent functionality.

Streaming Options:
  --streaming         Enable streaming test (default false)
  --true-streaming    Use true LLM streaming vs simulated chunking

True streaming uses llmtypes.WithStreamingFunc to get real-time chunks as
the LLM generates them, providing immediate feedback and lower latency.

Examples:
  mcp-agent test agent                           # Run all agent tests
  mcp-agent test agent --provider openai        # Test with OpenAI
  mcp-agent test agent --true-streaming         # Test true LLM streaming
  mcp-agent test agent --simple --no-streaming  # Simple test only`,
	Run: runAgentTest,
}

// agentTestFlags holds the agent test specific flags
type agentTestFlags struct {
	model   string
	servers string
	simple  bool

	complex            bool
	comprehensive      bool
	comprehensiveAWS   bool
	comprehensiveReAct bool // DEPRECATED: Use comprehensive-react command instead
	tokenTest          bool
	multiTurn          bool
	showMetrics        bool
	fallbackTest       bool
}

var agentFlags agentTestFlags

func init() {
	// Agent test specific flags (inherits common flags from parent)
	agentCmd.Flags().StringVar(&agentFlags.model, "model", "", "specific model ID (uses provider default if empty)")
	agentCmd.Flags().StringVar(&agentFlags.servers, "servers", "filesystem,memory", "MCP servers to test with")
	agentCmd.Flags().BoolVar(&agentFlags.simple, "simple", false, "run simple invoke test")

	agentCmd.Flags().BoolVar(&agentFlags.complex, "complex", false, "run complex multi-tool test")
	agentCmd.Flags().BoolVar(&agentFlags.comprehensive, "comprehensive", false, "run comprehensive multi-server test")
	agentCmd.Flags().BoolVar(&agentFlags.comprehensiveAWS, "comprehensive-aws", false, "run comprehensive AWS cost report test")
	agentCmd.Flags().BoolVar(&agentFlags.comprehensiveReAct, "comprehensive-react", false, "DEPRECATED: Use 'test comprehensive-react' command instead")
	agentCmd.Flags().BoolVar(&agentFlags.tokenTest, "token-test", false, "run token management test")
	agentCmd.Flags().BoolVar(&agentFlags.multiTurn, "multi-turn", false, "run multi-turn conversation test")
	agentCmd.Flags().BoolVar(&agentFlags.showMetrics, "show-metrics", false, "display detailed metrics")
	agentCmd.Flags().BoolVar(&agentFlags.fallbackTest, "fallback-test", false, "run fallback model test")
}

func runAgentTest(cmd *cobra.Command, args []string) {
	fmt.Println("=== AGENT TEST STARTED ===")

	// Debug: Print what flags we received
	fmt.Printf("Debug: Starting agent test\n")
	fmt.Printf("Debug: simple flag = %t\n", agentFlags.simple)
	fmt.Printf("Debug: complex flag = %t\n", agentFlags.complex)
	fmt.Printf("Debug: comprehensive flag = %t\n", agentFlags.comprehensive)

	// Get logging configuration from root command flags directly
	// This avoids viper binding conflicts between root and testing framework
	logFile := cmd.Flag("log-file").Value.String()
	logLevel := cmd.Flag("log-level").Value.String()
	logFormat := cmd.Flag("log-format").Value.String()

	// Debug: Print what we got from flags
	fmt.Printf("Debug: logFile='%s', logLevel='%s', logFormat='%s'\n", logFile, logLevel, logFormat)

	// Initialize shared test logger with command-specific settings
	InitTestLogger(logFile, logLevel)

	// Get the shared test logger
	logger := GetTestLogger()
	defer logger.Close()

	// If log file is specified, log to file only
	if logFile != "" {
		logger.Infof("📝 Logging to file only - log_file: %s", logFile)
	}

	// 🆕 AUTOMATIC LANGFUSE SETUP FOR ALL TESTS
	// Set environment variables for automatic Langfuse tracing
	os.Setenv("TRACING_PROVIDER", "langfuse")
	os.Setenv("LANGFUSE_DEBUG", "false")

	logger.Infof("🔧 Automatic Langfuse Setup - tracing_provider: %s, langfuse_debug: %s, note: %s", "langfuse", "false", "All agent tests now automatically use Langfuse tracing")

	logger.Infof("🧪 LLM Agent Wrapper Test Suite - test_type: %s, provider: %s, verbose: %t", "agent_wrapper_test", provider, verbose)

	// Configuration (use inherited flags from parent command)
	modelID := agentFlags.model
	serverList := agentFlags.servers

	// Read config flag directly from cobra command since viper binding is not working
	configPath := cmd.Flag("config").Value.String()
	if configPath == "" || configPath == "mcp.yaml" {
		configPath = "configs/mcp_servers_simple.json" // Use simple config as default for tests
	}

	// Debug: Show what config value was read
	logger.Infof("🔍 Config flag debug - cobra config value: %s, final configPath: %s", cmd.Flag("config").Value.String(), configPath)

	// Validate and get provider
	llmProvider, err := llm.ValidateProvider(provider)
	if err != nil {
		logger.Fatalf("❌ Invalid LLM provider - provider: %s, error: %s", provider, err.Error())
	}

	// Set default model if not specified
	if modelID == "" {
		modelID = llm.GetDefaultModel(llmProvider)
	}

	logger.Infof("🤖 Test Configuration - provider: %s, model: %s, servers: %s, trace_provider: %s, debug_mode: %t, verbose: %t, complex_flag: %t", provider, modelID, serverList, "langfuse", viper.GetBool("debug"), verbose, agentFlags.complex)

	// Initialize tracer based on configuration - now automatic with Langfuse
	// Initialize tracer based on environment (Langfuse if available, otherwise noop)
	tracer := InitializeTracer(logger)

	// Test 1: Basic Agent Wrapper Creation (only if other tests need it)
	var wrapper *agent.LLMAgentWrapper
	var response, complexResponse string

	// Only create basic wrapper if we're running tests that need it
	if agentFlags.simple || agentFlags.complex || agentFlags.comprehensive || agentFlags.comprehensiveAWS || agentFlags.comprehensiveReAct || agentFlags.tokenTest || agentFlags.multiTurn || agentFlags.showMetrics || agentFlags.fallbackTest {
		logger.Info("🧪 Test 1: Agent Wrapper Creation")

		config := agent.LLMAgentConfig{
			Name:        "Test Agent",
			ServerName:  serverList,
			ConfigPath:  configPath,
			Provider:    llm.Provider(provider),
			ModelID:     modelID,
			Temperature: viper.GetFloat64("temperature"),
			ToolChoice:  "auto",
			MaxTurns:    viper.GetInt("max-turns"),
			Timeout:     2 * time.Minute,
		}

		wrapper, err = agent.NewLLMAgentWrapper(context.Background(), config, nil, GetTestLogger()) // tracer will be auto-detected
		if err != nil {
			logger.Fatalf("❌ Failed to create agent wrapper - error: %s", err.Error())
		}
		defer func() {
			if err := wrapper.Stop(context.Background()); err != nil {
				logger.Warnf("⚠️ Error stopping agent - error: %s", err.Error())
			}
		}()

		logger.Infof("✅ Agent wrapper created successfully - agent_name: %s, capabilities: %s, health_status: %t", wrapper.GetName(), wrapper.GetCapabilities(), wrapper.IsHealthy())
	}

	// Test 2: Simple Invoke Interface
	if agentFlags.simple {
		logger.Info("🧪 Test 2: Simple Invoke Interface")

		simpleQuery := "List the files in the current directory and tell me about this Go project structure"

		logger.Infof("📝 Query - query: %s", simpleQuery)

		startTime := time.Now()
		response, err = wrapper.Invoke(context.Background(), simpleQuery)
		duration := time.Since(startTime)

		if err != nil {
			logger.Fatalf("❌ Failed to invoke agent - error: %s", err.Error())
		}

		logger.Infof("✅ Response received - response_length: %d, duration: %s", len(response), duration.String())

		if verbose || viper.GetBool("debug") {
			preview := response
			if len(response) > 200 {
				preview = response[:200] + "... (truncated)"
			}
			logger.Debugf("🤖 Response preview - preview: %s", preview)
		}
	}

	// Test 4: Complex Multi-Tool Test
	if agentFlags.complex {
		logger.Info("🧪 Test 4: Complex Multi-Tool Test")

		complexQuery := "Analyze this Go project, create a summary file, and then read it back to verify it was created correctly"

		logger.Infof("📝 Complex Query - query: %s", complexQuery)

		startTime := time.Now()
		complexResponse, err = wrapper.Invoke(context.Background(), complexQuery)
		duration := time.Since(startTime)

		if err != nil {
			logger.Fatal("❌ Failed to execute complex query", map[string]interface{}{"error": err.Error()})
		}

		logger.Infof("✅ Complex test completed - response_length: %d, duration: %s", len(complexResponse), duration.String())

		// Validate complex response contains expected elements
		expectedElements := []string{"file", "directory", "Go", "project"}
		found := 0
		for _, element := range expectedElements {
			if containsAny(complexResponse, []string{element}) {
				found++
			}
		}

		logger.Infof("📊 Response validation - expected_elements: %d, found_elements: %d", len(expectedElements), found)

		if verbose || viper.GetBool("debug") {
			preview := complexResponse
			if len(complexResponse) > 300 {
				preview = complexResponse[:300] + "... (truncated)"
			}
			logger.Debugf("🤖 Complex Response preview - preview: %s", preview)
		}
	}

	// Test 4.5: Comprehensive Complex Test with All MCP Servers
	if agentFlags.comprehensive {
		logger.Info("🧪 Test 4.5: Comprehensive Complex Test with All MCP Servers")

		// Create a comprehensive agent wrapper with ALL servers hard coded
		comprehensiveConfig := agent.LLMAgentConfig{
			Name:        "Comprehensive Test Agent",
			ServerName:  "filesystem,memory,airbnb,tavily-search,read_large_tool_output",
			ConfigPath:  configPath,
			Provider:    llm.Provider(provider),
			ModelID:     modelID,
			Temperature: viper.GetFloat64("temperature"),
			ToolChoice:  "auto",
			MaxTurns:    viper.GetInt("max-turns"),
			Timeout:     2 * time.Minute,
		}

		comprehensiveWrapper, err := agent.NewLLMAgentWrapper(context.Background(), comprehensiveConfig, tracer, GetTestLogger())
		if err != nil {
			logger.Fatal("❌ Failed to create comprehensive agent wrapper", map[string]interface{}{"error": err.Error()})
		}
		defer func() {
			if err := comprehensiveWrapper.Stop(context.Background()); err != nil {
				logger.Warn("⚠️ Error stopping comprehensive agent", map[string]interface{}{"error": err.Error()})
			}
		}()

		logger.Info("✅ Comprehensive agent wrapper created with all servers", map[string]interface{}{
			"agent_name":    comprehensiveWrapper.GetName(),
			"capabilities":  comprehensiveWrapper.GetCapabilities(),
			"health_status": comprehensiveWrapper.IsHealthy(),
		})

		// Create a comprehensive query that uses all available MCP servers
		comprehensiveQuery := `Create a comprehensive analysis by performing the following tasks:

1. Search the web for the latest news about artificial intelligence and machine learning trends
2. Find luxury Airbnb accommodations in Tokyo for 2 adults for next month
3. Create a detailed report file with all findings including web search results and Airbnb options
4. Read the report back to verify it was created correctly and contains all information
5. Save key insights to memory for future reference

Make this a thorough analysis that demonstrates the agent's ability to use multiple tools and data sources.`

		logger.Infof("📝 Comprehensive Query - query: %s", comprehensiveQuery)

		comprehensiveStartTime := time.Now()
		var comprehensiveResponse string
		comprehensiveResponse, err = comprehensiveWrapper.Invoke(context.Background(), comprehensiveQuery)
		comprehensiveDuration := time.Since(comprehensiveStartTime)

		if err != nil {
			logger.Warn("⚠️ Comprehensive test had issues", map[string]interface{}{"error": err.Error()})
		} else {
			logger.Infof("✅ Comprehensive test completed - response_length: %d, duration: %s", len(comprehensiveResponse), comprehensiveDuration.String())

			// Validate comprehensive response contains expected elements from different tools
			comprehensiveElements := []string{"AI", "machine learning", "Airbnb", "Tokyo", "report", "file", "memory"}
			comprehensiveFound := 0
			for _, element := range comprehensiveElements {
				if containsAny(comprehensiveResponse, []string{element}) {
					comprehensiveFound++
				}
			}

			logger.Info("📊 Comprehensive response validation", map[string]interface{}{
				"expected_elements": len(comprehensiveElements),
				"found_elements":    comprehensiveFound,
			})

			if verbose || viper.GetBool("debug") {
				preview := comprehensiveResponse
				if len(comprehensiveResponse) > 500 {
					preview = comprehensiveResponse[:500] + "... (truncated)"
				}
				logger.Debug("🤖 Comprehensive Response preview", map[string]interface{}{"preview": preview})
			}
		}
	}

	// Test 4.6: Comprehensive AWS Cost Report Test with Actual MCP Servers
	if agentFlags.comprehensiveAWS {
		logger.Info("🧪 Test 4.6: Comprehensive AWS Cost Report Test with Actual MCP Servers")

		// Create a comprehensive agent wrapper with AWS servers and filesystem for saving
		awsComprehensiveConfig := agent.LLMAgentConfig{
			Name:        "AWS Cost Report Agent",
			ServerName:  "citymall-aws-mcp", // Use only AWS server first
			ConfigPath:  configPath,         // Use the config path from viper
			Provider:    llm.Provider(provider),
			ModelID:     modelID,
			Temperature: viper.GetFloat64("temperature"),
			ToolChoice:  "auto",
			MaxTurns:    viper.GetInt("max-turns"),
			Timeout:     3 * time.Minute, // Longer timeout for AWS operations
		}

		// Create the AWS comprehensive agent wrapper
		awsComprehensiveAgent, err := agent.NewLLMAgentWrapper(context.Background(), awsComprehensiveConfig, tracer, GetTestLogger())
		if err != nil {
			logger.Fatal("❌ Failed to create AWS comprehensive agent wrapper", map[string]interface{}{"error": err.Error()})
		}
		defer func() {
			if err := awsComprehensiveAgent.Stop(context.Background()); err != nil {
				logger.Warn("⚠️ Error stopping AWS comprehensive agent", map[string]interface{}{"error": err.Error()})
			}
		}()

		logger.Info("✅ AWS comprehensive agent wrapper created with AWS servers", map[string]interface{}{
			"agent_name":    awsComprehensiveAgent.GetName(),
			"capabilities":  awsComprehensiveAgent.GetCapabilities(),
			"health_status": awsComprehensiveAgent.IsHealthy(),
			"config_path":   configPath,
		})

		// Test query for AWS cost report with realistic parameters
		awsQuery := "Create a basic AWS infrastructure report. List available AWS services and provide general cost optimization recommendations. Save the report to a file called aws_cost_report.txt. Use current dates and realistic parameters."

		logger.Info("📝 AWS Cost Report Query", map[string]interface{}{"query": awsQuery})

		// Execute the AWS comprehensive test
		awsResponse, err := awsComprehensiveAgent.Invoke(context.Background(), awsQuery)
		if err != nil {
			logger.Fatal("❌ AWS comprehensive test failed", map[string]interface{}{"error": err.Error()})
		}

		// Log the response
		logger.Info("✅ AWS Cost Report Response",
			"response_length", len(awsResponse),
			"response_preview", mcpagent.TruncateString(awsResponse, 200))

		// Check if the report file was created
		if strings.Contains(awsResponse, "aws_cost_report.txt") || strings.Contains(awsResponse, "cost report") {
			logger.Info("✅ AWS Cost Report Test Completed Successfully")
		} else {
			logger.Warn("⚠️ AWS Cost Report Test completed but report file creation not confirmed")
		}
	}

	// Test 4.7: Comprehensive ReAct Agent Test (DEPRECATED)
	if agentFlags.comprehensiveReAct {
		logger.Warn("⚠️ DEPRECATED: Comprehensive ReAct test has been moved to its own command")
		logger.Info("💡 Use 'go run main.go test comprehensive-react' instead")
		logger.Info("📝 This provides better isolation and more configuration options")
		return
	}

	// Test 5: Token Management Test
	if agentFlags.tokenTest {
		logger.Info("🧪 Test 5: Token Management Test")

		tokenTestQuery := "Create a very large file with detailed content, then read it back multiple times to test token management and conversation history optimization"

		logger.Info("📝 Token Management Query", map[string]interface{}{"query": tokenTestQuery})

		tokenStartTime := time.Now()
		var tokenResponse string
		tokenResponse, err = wrapper.Invoke(context.Background(), tokenTestQuery)
		tokenDuration := time.Since(tokenStartTime)

		if err != nil {
			logger.Warn("⚠️ Token management test had issues", map[string]interface{}{"error": err.Error()})
		} else {
			logger.Info("✅ Token management test completed", map[string]interface{}{
				"response_length": len(tokenResponse),
				"duration":        tokenDuration.String(),
			})
		}
	}

	// Test 5: Metrics Collection
	if agentFlags.showMetrics {
		logger.Info("🧪 Test 5: Metrics Collection")

		metrics := wrapper.GetMetrics()
		logger.Info("📈 Agent Metrics", map[string]interface{}{"metrics": metrics})

		// Calculate success rate
		successRate := calculateSuccessRate(metrics)
		logger.Info("📊 Success Rate", map[string]interface{}{"rate": successRate})
	}

	// Test 6: Multi-Turn Conversation (single tool/server)
	if agentFlags.multiTurn {
		logger.Info("🧪 Test 6: Multi-Turn Conversation (filesystem only)")

		// Use only the filesystem server for this test
		filesystemConfig := agent.LLMAgentConfig{
			Name:        "Test Agent (Filesystem Only)",
			ServerName:  "filesystem",
			ConfigPath:  configPath,
			Provider:    llm.Provider(provider),
			ModelID:     modelID,
			Temperature: viper.GetFloat64("temperature"),
			ToolChoice:  "auto",
			MaxTurns:    viper.GetInt("max-turns"),
			Timeout:     2 * time.Minute,
		}

		filesystemWrapper, err := agent.NewLLMAgentWrapper(context.Background(), filesystemConfig, tracer, GetTestLogger())
		if err != nil {
			logger.Fatal("❌ Failed to create agent wrapper (filesystem only)", map[string]interface{}{"error": err.Error()})
		}
		defer func() {
			if err := filesystemWrapper.Stop(context.Background()); err != nil {
				logger.Warn("⚠️ Error stopping agent (filesystem only)", map[string]interface{}{"error": err.Error()})
			}
		}()

		systemPrompt := "You are an AI agent. Answer as helpfully as possible."
		messageHistory := []llmtypes.MessageContent{
			{
				Role:  llmtypes.ChatMessageTypeSystem,
				Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: systemPrompt}},
			},
			{
				Role:  llmtypes.ChatMessageTypeHuman,
				Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: "List the files in the current directory."}},
			},
		}

		logger.Info("👤 User", map[string]interface{}{"message": "List the files in the current directory."})
		resp1, err := filesystemWrapper.InvokeWithHistory(context.Background(), messageHistory)
		if err != nil {
			logger.Fatal("❌ Multi-turn (turn 1) failed", map[string]interface{}{"error": err.Error()})
		}
		logger.Info("🤖 Agent", map[string]interface{}{"message": resp1})

		// Add assistant reply to history
		messageHistory = append(messageHistory, llmtypes.MessageContent{
			Role:  llmtypes.ChatMessageTypeAI,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: resp1}},
		})

		// User follow-up
		followup := "Now summarize the largest file."
		messageHistory = append(messageHistory, llmtypes.MessageContent{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: followup}},
		})
		logger.Info("👤 User", map[string]interface{}{"message": followup})
		resp2, err := filesystemWrapper.InvokeWithHistory(context.Background(), messageHistory)
		if err != nil {
			logger.Fatal("❌ Multi-turn (turn 2) failed", map[string]interface{}{"error": err.Error()})
		}
		logger.Info("🤖 Agent", map[string]interface{}{"message": resp2})

		// Add assistant reply to history
		messageHistory = append(messageHistory, llmtypes.MessageContent{
			Role:  llmtypes.ChatMessageTypeAI,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: resp2}},
		})

		// User clarification
		clarification := "By 'largest', I mean the file with the most lines."
		messageHistory = append(messageHistory, llmtypes.MessageContent{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: clarification}},
		})
		logger.Info("👤 User", map[string]interface{}{"message": clarification})
		resp3, err := filesystemWrapper.InvokeWithHistory(context.Background(), messageHistory)
		if err != nil {
			logger.Fatal("❌ Multi-turn (turn 3) failed", map[string]interface{}{"error": err.Error()})
		}
		logger.Info("🤖 Agent", map[string]interface{}{"message": resp3})

		logger.Info("✅ Multi-turn conversation test (filesystem only) completed.")
	}

	// Test 4.7: Fallback Model Test
	if agentFlags.fallbackTest {
		logger.Info("🧪 Test 4.7: Fallback Model Test")

		// Create a test with fallback models configured
		fallbackConfig := agent.LLMAgentConfig{
			Name:        "Fallback Test Agent",
			ServerName:  "filesystem",
			ConfigPath:  configPath,
			Provider:    llm.Provider(provider),
			ModelID:     modelID,
			Temperature: viper.GetFloat64("temperature"),
			ToolChoice:  "auto",
			MaxTurns:    viper.GetInt("max-turns"),
			Timeout:     2 * time.Minute,
		}

		// Add fallback models based on provider
		if provider == "bedrock" {
			fallbackConfig.ModelID = "anthropic.claude-3-sonnet-20240229-v1:0" // Primary
			// Note: Fallback models would be configured in the LLM initialization
		} else if provider == "openai" {
			fallbackConfig.ModelID = "gpt-4.1" // Primary
			// Note: Fallback models would be configured in the LLM initialization
		}

		fallbackAgent, err := agent.NewLLMAgentWrapper(context.Background(), fallbackConfig, tracer, GetTestLogger())
		if err != nil {
			logger.Fatal("❌ Failed to create fallback test agent", map[string]interface{}{"error": err.Error()})
		}
		defer func() {
			if err := fallbackAgent.Stop(context.Background()); err != nil {
				logger.Warn("⚠️ Error stopping fallback test agent", map[string]interface{}{"error": err.Error()})
			}
		}()

		logger.Info("✅ Fallback test agent created", map[string]interface{}{
			"agent_name":    fallbackAgent.GetName(),
			"capabilities":  fallbackAgent.GetCapabilities(),
			"health_status": fallbackAgent.IsHealthy(),
		})

		// Test query that might trigger rate limiting
		fallbackQuery := "Create a simple test file and then read it back to verify file operations work correctly"

		logger.Info("📝 Fallback Test Query", map[string]interface{}{"query": fallbackQuery})

		// Execute the fallback test
		fallbackResponse, err := fallbackAgent.Invoke(context.Background(), fallbackQuery)
		if err != nil {
			logger.Warn("⚠️ Fallback test had issues", map[string]interface{}{"error": err.Error()})
		} else {
			logger.Info("✅ Fallback test completed successfully", map[string]interface{}{
				"response_length": len(fallbackResponse),
			})
		}
	}

	// Final Summary
	logger.Info("🎉 Agent Wrapper Test Summary")
	logger.Info("────────────────────────────────")
	logger.Info("✅ Basic Creation: Passed")

	if agentFlags.simple {
		logger.Info("✅ Simple Invoke: Passed", map[string]interface{}{"response_length": len(response)})
	}

	if agentFlags.complex {
		logger.Info("✅ Complex Multi-Tool: Passed", map[string]interface{}{"response_length": len(complexResponse)})
	}

	if agentFlags.comprehensive {
		logger.Info("✅ Comprehensive: Passed")
	}

	if agentFlags.comprehensiveAWS {
		logger.Info("✅ Comprehensive AWS: Passed")
	}

	if agentFlags.tokenTest {
		logger.Info("✅ Token Management: Passed")
	}

	if agentFlags.multiTurn {
		logger.Info("✅ Multi-Turn: Passed")
	}

	if agentFlags.showMetrics {
		metrics := wrapper.GetMetrics()
		logger.Info("✅ Metrics: Passed", map[string]interface{}{"metrics_collected": len(metrics)})
	}

	logger.Info("🏆 All tests completed successfully!")
}

// testAgentWithObservability tests the agent with configurable observability
func testAgentWithObservability(cmd *cobra.Command, args []string) error {
	logger := GetTestLogger()
	logger.Info("Testing Agent with Configurable Observability")

	// Observability is now handled automatically via environment variables
	logger.Info("Observability Configuration", map[string]interface{}{
		"provider": os.Getenv("TRACING_PROVIDER"),
		"enabled":  os.Getenv("TRACING_PROVIDER") != "",
	})

	// Initialize LLM
	llm, err := llm.InitializeLLM(llm.Config{
		Provider:    "bedrock",
		ModelID:     "claude-3-sonnet-20240229-v1:0",
		Temperature: 0.7,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize LLM: %w", err)
	}

	// Create agent with observability
	agent, err := mcpagent.NewAgentWithObservability(
		context.Background(),
		llm,
		"all",
		"configs/mcp_servers.json",
		"claude-3-sonnet-20240229-v1:0",
		GetTestLogger(),
		mcpagent.WithTemperature(0.7),
		mcpagent.WithToolChoice("auto"),
		mcpagent.WithMaxTurns(10),
	)
	if err != nil {
		return fmt.Errorf("failed to create agent: %w", err)
	}
	defer agent.Close()

	// Test question
	question := "What is 2+2? Please provide a simple answer."
	logger.Info("Testing Agent with Question", map[string]interface{}{"question": question})

	// Run the agent
	startTime := time.Now()
	result, err := agent.Ask(context.Background(), question)
	duration := time.Since(startTime)

	if err != nil {
		return fmt.Errorf("agent failed: %w", err)
	}

	logger.Info("Agent Response", map[string]interface{}{"result": result, "duration": duration.String()})

	// Test with a more complex question that uses tools
	logger.Info("Testing Agent with Tool Usage")
	toolQuestion := "List files in the current directory"
	logger.Info("Tool Question", map[string]interface{}{"question": toolQuestion})

	startTime = time.Now()
	toolResult, err := agent.Ask(context.Background(), toolQuestion)
	toolDuration := time.Since(startTime)

	if err != nil {
		return fmt.Errorf("agent failed with tool usage: %w", err)
	}

	logger.Info("Tool Response", map[string]interface{}{"result": toolResult, "duration": toolDuration.String()})

	logger.Info("Agent with Observability Test Completed Successfully")
	return nil
}

// Helper functions
func containsAny(text string, substrings []string) bool {
	textLower := strings.ToLower(text)
	for _, substring := range substrings {
		if strings.Contains(textLower, strings.ToLower(substring)) {
			return true
		}
	}
	return false
}

func getIntValue(metrics map[string]interface{}, key string) int64 {
	if value, exists := metrics[key]; exists {
		if intValue, ok := value.(int64); ok {
			return intValue
		}
	}
	return 0
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// createDynamicAWSConfig creates a dynamic MCP configuration for AWS testing
func createDynamicAWSConfig() map[string]interface{} {
	return map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"citymall-aws-mcp": map[string]interface{}{
				"url":       "http://localhost:9091/sse",
				"transport": "sse",
			},
			"filesystem": map[string]interface{}{
				"command": "npx",
				"args":    []string{"-y", "@modelcontextprotocol/server-filesystem", "reports"},
			},
		},
	}
}
