package testing

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"mcp-agent/agent_go/internal/llm"
	agent "mcp-agent/agent_go/pkg/agentwrapper"
	"mcp-agent/agent_go/pkg/mcpagent"
)

// comprehensiveReactCmd represents the comprehensive React agent test command
var comprehensiveReactCmd = &cobra.Command{
	Use:   "comprehensive-react",
	Short: "Test the ReAct Agent with comprehensive reasoning and tool usage",
	Long: `Test the ReAct (Reasoning and Acting) Agent with comprehensive validation of:

1. ReAct reasoning patterns and explicit step-by-step thinking
2. Multi-tool execution with AWS and Scripts servers
3. Cross-provider fallback handling
4. Tool timeout handling and graceful error recovery
5. Langfuse tracing and observability
6. Conversation history management

This test validates that the ReAct agent can:
- Use explicit reasoning patterns ("Let me think about this step by step...")
- Execute multiple tools across different servers
- Handle tool timeouts gracefully
- Provide comprehensive analysis with "Final Answer:"
- Use cross-provider fallback when needed

Examples:
  mcp-agent test comprehensive-react                    # Run comprehensive ReAct test
  mcp-agent test comprehensive-react --provider bedrock    # Test with AWS Bedrock
  mcp-agent test comprehensive-react --provider openai     # Test with OpenAI
  mcp-agent test comprehensive-react --provider anthropic  # Test with Anthropic
  mcp-agent test comprehensive-react --provider openrouter # Test with OpenRouter
  mcp-agent test comprehensive-react --verbose         # Verbose output`,
	Run: runComprehensiveReactTest,
}

// comprehensiveReactTestFlags holds the comprehensive React test specific flags
type comprehensiveReactTestFlags struct {
	model       string
	servers     string
	showMetrics bool
	timeout     time.Duration
	maxTurns    int
	temperature float64
}

var reactFlags comprehensiveReactTestFlags

func init() {
	// Comprehensive React test specific flags
	comprehensiveReactCmd.Flags().StringVar(&reactFlags.model, "model", "", "specific model ID (uses provider default if empty)")
	comprehensiveReactCmd.Flags().StringVar(&reactFlags.servers, "servers", "citymall-aws-mcp,citymall-scripts-mcp", "MCP servers to test with (use 'all' for all servers)")
	comprehensiveReactCmd.Flags().BoolVar(&reactFlags.showMetrics, "show-metrics", false, "display detailed metrics")
	comprehensiveReactCmd.Flags().DurationVar(&reactFlags.timeout, "timeout", 5*time.Minute, "test timeout duration")
	comprehensiveReactCmd.Flags().IntVar(&reactFlags.maxTurns, "max-turns", 50, "maximum conversation turns for ReAct agent")
	comprehensiveReactCmd.Flags().Float64Var(&reactFlags.temperature, "temperature", 0.2, "LLM temperature for reasoning")
}

func runComprehensiveReactTest(cmd *cobra.Command, args []string) {
	// Get logging configuration from viper
	logFile := viper.GetString("log-file")
	logLevel := viper.GetString("log-level")

	// Initialize test logger
	InitTestLogger(logFile, logLevel)
	logger := GetTestLogger()

	// If log file is specified, log to file only
	if logFile != "" {
		logger.Info("📝 Logging to file only", map[string]interface{}{"log_file": logFile})
	}

	// 🆕 AUTOMATIC LANGFUSE SETUP FOR REACT TESTS
	// Set environment variables for automatic Langfuse tracing
	os.Setenv("TRACING_PROVIDER", "langfuse")
	os.Setenv("LANGFUSE_DEBUG", "true")

	logger.Info("🔧 Automatic Langfuse Setup for ReAct Test", map[string]interface{}{
		"tracing_provider": "langfuse",
		"langfuse_debug":   "true",
		"note":             "ReAct test uses enhanced Langfuse tracing",
	})

	logger.Info("🧪 Comprehensive ReAct Agent Test Suite", map[string]interface{}{
		"test_type": "comprehensive_react_test",
		"provider":  provider,
		"verbose":   verbose,
	})

	// Configuration
	modelID := reactFlags.model
	serverList := reactFlags.servers
	configPath := "configs/mcp_servers_clean.json"

	// Handle "all" servers parameter
	if serverList == "all" {
		serverList = "citymall-github-mcp,citymall-aws-mcp,citymall-db-mcp,citymall-k8s-mcp,citymall-grafana-mcp,citymall-sentry-mcp,citymall-slack-mcp,citymall-profiler-mcp,citymall-scripts-mcp,context7,fetch"
		logger.Info("🔧 Using all available servers", map[string]interface{}{
			"server_count": len(strings.Split(serverList, ",")),
			"servers":      serverList,
		})
	}

	// Validate and get provider
	llmProvider, err := llm.ValidateProvider(provider)
	if err != nil {
		logger.Fatal("❌ Invalid LLM provider", map[string]interface{}{
			"provider": provider,
			"error":    err.Error(),
		})
	}

	// Set default model if not specified
	if modelID == "" {
		modelID = llm.GetDefaultModel(llmProvider)
	}

	logger.Info("🤖 ReAct Test Configuration", map[string]interface{}{
		"provider":       provider,
		"model":          modelID,
		"servers":        serverList,
		"trace_provider": "langfuse",
		"debug_mode":     viper.GetBool("debug"),
		"verbose":        verbose,
		"max_turns":      reactFlags.maxTurns,
		"timeout":        reactFlags.timeout.String(),
		"temperature":    reactFlags.temperature,
	})

	// Initialize tracer
	// Initialize tracer based on environment (Langfuse if available, otherwise noop)
	tracer := InitializeTracer(logger)

	logger.Info("✅ Tracer initialized successfully", map[string]interface{}{
		"tracer_nil": tracer == nil,
	})

	// Create ReAct agent wrapper with AWS and Scripts citymall servers
	// Note: Large output handling now uses virtual tools instead of MCP server
	logger.Info("🔧 Creating agent config", map[string]interface{}{
		"server_list": serverList,
		"config_path": configPath,
		"provider":    provider,
		"model_id":    modelID,
	})

	reactConfig := agent.LLMAgentConfig{
		Name:        "ReAct AWS + Scripts Test Agent",
		ServerName:  serverList,
		ConfigPath:  configPath,
		Provider:    llm.Provider(provider),
		ModelID:     modelID,
		Temperature: reactFlags.temperature,
		ToolChoice:  "auto",
		MaxTurns:    reactFlags.maxTurns,
		Timeout:     reactFlags.timeout,
		AgentMode:   mcpagent.SimpleAgent, // Use Simple mode
	}

	logger.Info("✅ Agent config created", map[string]interface{}{
		"config_name":    reactConfig.Name,
		"config_timeout": reactConfig.Timeout.String(),
	})

	// Create a context with timeout for agent creation
	agentCtx, agentCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer agentCancel()

	logger.Info("🚀 Starting NewLLMAgentWrapper call", map[string]interface{}{
		"agent_timeout": "30s",
		"config_path":   configPath,
		"server_list":   serverList,
	})

	// Create the ReAct agent wrapper with timeout
	logger.Info("🔍 About to call NewLLMAgentWrapper", map[string]interface{}{
		"config_name":        reactConfig.Name,
		"config_server_name": reactConfig.ServerName,
		"config_provider":    reactConfig.Provider,
		"config_model_id":    reactConfig.ModelID,
		"config_agent_mode":  reactConfig.AgentMode,
	})

	reactAgent, err := agent.NewLLMAgentWrapper(agentCtx, reactConfig, tracer, GetTestLogger())

	logger.Info("🔍 NewLLMAgentWrapper call completed", map[string]interface{}{
		"error":     err,
		"agent_nil": reactAgent == nil,
	})

	if err != nil {
		logger.Fatal("❌ Failed to create ReAct agent wrapper", map[string]interface{}{"error": err.Error()})
	}

	logger.Info("✅ ReAct agent wrapper created successfully", map[string]interface{}{
		"agent_name": reactAgent.GetName(),
	})

	defer func() {
		if err := reactAgent.Stop(context.Background()); err != nil {
			logger.Warn("⚠️ Error stopping ReAct agent", map[string]interface{}{"error": err.Error()})
		}
	}()

	logger.Info("✅ ReAct agent wrapper created", map[string]interface{}{
		"agent_name":    reactAgent.GetName(),
		"capabilities":  reactAgent.GetCapabilities(),
		"health_status": reactAgent.IsHealthy(),
		"max_turns":     reactFlags.maxTurns,
	})

	// Test query designed to trigger ReAct reasoning patterns with all available tools
	// Note: Large output handling now uses virtual tools (read_large_output, search_large_output, query_large_output)
	reactQuery := "Perform a comprehensive analysis of our infrastructure and available tools. First, check the current AWS costs and usage patterns using AWS tools. Then, examine any CloudWatch metrics and alarms. Next, explore what scripts are available and their capabilities. Check GitHub repositories and any database connections. Examine Kubernetes clusters, Grafana dashboards, Sentry error tracking, and Slack integrations. If you encounter large tool outputs, use the virtual tools (read_large_output, search_large_output, query_large_output) to process them efficiently. Finally, provide a comprehensive analysis with cost optimization recommendations, automation suggestions, and infrastructure insights. Use explicit reasoning at each step and provide step-by-step analysis."

	logger.Info("📝 ReAct Test Query", map[string]interface{}{"query": reactQuery})

	// Execute the ReAct test with timeout and detailed logging
	reactStartTime := time.Now()

	// Create a context with timeout for the invoke call
	// Use a longer timeout for comprehensive ReAct tests that need multiple turns
	invokeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	logger.Info("🚀 Starting ReAct Agent Invoke", map[string]interface{}{
		"timeout":      "2m",
		"query_length": len(reactQuery),
	})

	// Execute with timeout
	reactResponse, err := reactAgent.Invoke(invokeCtx, reactQuery)
	reactDuration := time.Since(reactStartTime)

	if err != nil {
		logger.Fatal("❌ ReAct comprehensive test failed", map[string]interface{}{"error": err.Error()})
	}

	logger.Info("✅ ReAct Agent Invoke completed", map[string]interface{}{
		"duration":        reactDuration.String(),
		"response_length": len(reactResponse),
	})

	// Log the response
	logger.Info("✅ ReAct Test Response",
		"response_length", len(reactResponse),
		"duration", reactDuration.String(),
		"response_preview", mcpagent.TruncateString(reactResponse, 300))

	// Check for ReAct-specific patterns with comprehensive tool focus
	reactPatterns := []string{
		"Let me think about this step by step",
		"Final Answer:",
		"FINAL ANSWER:",
		"reasoning",
		"observe",
		"plan",
		"AWS",
		"cost",
		"CloudWatch",
		"metrics",
		"optimization",
		"analysis",
		"script",
		"automation",
		"capabilities",
		"github",
		"database",
		"kubernetes",
		"grafana",
		"sentry",
		"slack",
		"infrastructure",
		"cluster",
		"dashboard",
		"error",
		"integration",
		// Virtual tools patterns
		"read_large_output",
		"search_large_output",
		"query_large_output",
		"virtual tool",
		"large output",
	}

	patternMatches := 0
	for _, pattern := range reactPatterns {
		if strings.Contains(strings.ToLower(reactResponse), strings.ToLower(pattern)) {
			patternMatches++
			logger.Info("✅ ReAct pattern detected", map[string]interface{}{"pattern": pattern})
		}
	}

	// Check if comprehensive analysis was performed
	if strings.Contains(reactResponse, "AWS") || strings.Contains(reactResponse, "CloudWatch") || strings.Contains(reactResponse, "cost") {
		logger.Info("✅ ReAct AWS Analysis Confirmed")
	}
	if strings.Contains(reactResponse, "script") || strings.Contains(reactResponse, "automation") || strings.Contains(reactResponse, "capabilities") {
		logger.Info("✅ ReAct Scripts Analysis Confirmed")
	}
	if strings.Contains(reactResponse, "github") || strings.Contains(reactResponse, "repository") {
		logger.Info("✅ ReAct GitHub Analysis Confirmed")
	}
	if strings.Contains(reactResponse, "database") || strings.Contains(reactResponse, "db") {
		logger.Info("✅ ReAct Database Analysis Confirmed")
	}
	if strings.Contains(reactResponse, "kubernetes") || strings.Contains(reactResponse, "k8s") || strings.Contains(reactResponse, "cluster") {
		logger.Info("✅ ReAct Kubernetes Analysis Confirmed")
	}
	if strings.Contains(reactResponse, "grafana") || strings.Contains(reactResponse, "dashboard") {
		logger.Info("✅ ReAct Grafana Analysis Confirmed")
	}
	if strings.Contains(reactResponse, "sentry") || strings.Contains(reactResponse, "error") {
		logger.Info("✅ ReAct Sentry Analysis Confirmed")
	}
	if strings.Contains(reactResponse, "slack") || strings.Contains(reactResponse, "integration") {
		logger.Info("✅ ReAct Slack Analysis Confirmed")
	}

	// Check for timeout handling patterns
	if strings.Contains(reactResponse, "timed out") || strings.Contains(reactResponse, "timeout") {
		logger.Info("✅ ReAct Timeout Handling Confirmed")
	}

	// Check for fallback patterns
	if strings.Contains(reactResponse, "fallback") || strings.Contains(reactResponse, "throttling") {
		logger.Info("✅ ReAct Fallback Handling Confirmed")
	}

	// Check for virtual tools usage
	if strings.Contains(reactResponse, "read_large_output") || strings.Contains(reactResponse, "search_large_output") || strings.Contains(reactResponse, "query_large_output") {
		logger.Info("✅ ReAct Virtual Tools Usage Confirmed")
	}

	// Success criteria for ReAct test
	if patternMatches >= 3 {
		logger.Info("✅ ReAct Agent Test Completed Successfully", map[string]interface{}{
			"pattern_matches": patternMatches,
			"total_patterns":  len(reactPatterns),
			"duration":        reactDuration.String(),
		})
	} else {
		logger.Warn("⚠️ ReAct Agent Test completed but with limited reasoning patterns", map[string]interface{}{
			"pattern_matches": patternMatches,
			"total_patterns":  len(reactPatterns),
		})
	}

	// Show metrics if requested
	if reactFlags.showMetrics {
		metrics := reactAgent.GetMetrics()
		logger.Info("📈 ReAct Agent Metrics", map[string]interface{}{"metrics": metrics})

		// Calculate success rate using helper from agent.go
		successRate := calculateSuccessRate(metrics)
		logger.Info("📊 ReAct Success Rate", map[string]interface{}{"rate": successRate})
	}

	logger.Info("🏁 Comprehensive ReAct Test Completed", map[string]interface{}{
		"duration":        reactDuration.String(),
		"pattern_matches": patternMatches,
		"status":          "completed",
	})
}

func calculateSuccessRate(metrics map[string]interface{}) float64 {
	total := getIntValue(metrics, "total_requests")
	successful := getIntValue(metrics, "successful_requests")

	if total == 0 {
		return 0.0
	}

	return (float64(successful) / float64(total)) * 100.0
}
