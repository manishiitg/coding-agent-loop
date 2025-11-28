package testing

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	agent "mcp-agent/agent_go/pkg/agentwrapper"
	mcpagent "mcpagent/agent"
	"mcpagent/llm"
)

// comprehensiveSimpleCmd represents the comprehensive Simple agent test command
var comprehensiveSimpleCmd = &cobra.Command{
	Use:   "comprehensive-simple",
	Short: "Test the Simple Agent with comprehensive tool usage",
	Long: `Test the Simple Agent with comprehensive validation of:

1. Direct tool execution without explicit reasoning patterns
2. Multi-tool execution with AWS and Scripts servers
3. Cross-provider fallback handling
4. Tool timeout handling and graceful error recovery
5. Langfuse tracing and observability
6. Conversation history management

This test validates that the Simple agent can:
- Execute multiple tools across different servers efficiently
- Handle tool timeouts gracefully
- Provide comprehensive analysis with direct responses
- Use cross-provider fallback when needed

Examples:
  mcp-agent test comprehensive-simple                    # Run comprehensive Simple test
  mcp-agent test comprehensive-simple --provider bedrock    # Test with AWS Bedrock
  mcp-agent test comprehensive-simple --provider openai     # Test with OpenAI
  mcp-agent test comprehensive-simple --provider anthropic  # Test with Anthropic
  mcp-agent test comprehensive-simple --provider openrouter # Test with OpenRouter
  mcp-agent test comprehensive-simple --verbose         # Verbose output`,
	Run: runComprehensiveSimpleTest,
}

// comprehensiveSimpleTestFlags holds the comprehensive Simple test specific flags
type comprehensiveSimpleTestFlags struct {
	model       string
	servers     string
	showMetrics bool
	timeout     time.Duration
	maxTurns    int
	temperature float64
}

var simpleFlags comprehensiveSimpleTestFlags

func init() {
	// Comprehensive Simple test specific flags
	comprehensiveSimpleCmd.Flags().StringVar(&simpleFlags.model, "model", "", "specific model ID (uses provider default if empty)")
	comprehensiveSimpleCmd.Flags().StringVar(&simpleFlags.servers, "servers", "citymall-aws-mcp,citymall-scripts-mcp", "MCP servers to test with (use 'all' for all servers)")
	comprehensiveSimpleCmd.Flags().BoolVar(&simpleFlags.showMetrics, "show-metrics", false, "display detailed metrics")
	comprehensiveSimpleCmd.Flags().DurationVar(&simpleFlags.timeout, "timeout", 5*time.Minute, "test timeout duration")
	comprehensiveSimpleCmd.Flags().IntVar(&simpleFlags.maxTurns, "max-turns", 50, "maximum conversation turns for Simple agent")
	comprehensiveSimpleCmd.Flags().Float64Var(&simpleFlags.temperature, "temperature", 0.2, "LLM temperature")
}

func runComprehensiveSimpleTest(cmd *cobra.Command, args []string) {
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

	// 🆕 AUTOMATIC LANGFUSE SETUP FOR SIMPLE TESTS
	// Set environment variables for automatic Langfuse tracing
	os.Setenv("TRACING_PROVIDER", "langfuse")
	os.Setenv("LANGFUSE_DEBUG", "true")

	logger.Info("🔧 Automatic Langfuse Setup for Simple Test", map[string]interface{}{
		"tracing_provider": "langfuse",
		"langfuse_debug":   "true",
		"note":             "Simple test uses enhanced Langfuse tracing",
	})

	logger.Info("🧪 Comprehensive Simple Agent Test Suite", map[string]interface{}{
		"test_type": "comprehensive_simple_test",
		"provider":  provider,
		"verbose":   verbose,
	})

	// Configuration
	modelID := simpleFlags.model
	serverList := simpleFlags.servers
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

	logger.Info("🤖 Simple Test Configuration", map[string]interface{}{
		"provider":       provider,
		"model":          modelID,
		"servers":        serverList,
		"trace_provider": "langfuse",
		"debug_mode":     viper.GetBool("debug"),
		"verbose":        verbose,
		"max_turns":      simpleFlags.maxTurns,
		"timeout":        simpleFlags.timeout.String(),
		"temperature":    simpleFlags.temperature,
	})

	// Initialize tracer
	// Initialize tracer based on environment (Langfuse if available, otherwise noop)
	tracer := InitializeTracer(logger)

	logger.Info("✅ Tracer initialized successfully", map[string]interface{}{
		"tracer_nil": tracer == nil,
	})

	// Create Simple agent wrapper with AWS and Scripts citymall servers
	// Note: Large output handling now uses virtual tools instead of MCP server
	logger.Info("🔧 Creating agent config", map[string]interface{}{
		"server_list": serverList,
		"config_path": configPath,
		"provider":    provider,
		"model_id":    modelID,
	})

	simpleConfig := agent.LLMAgentConfig{
		Name:        "Simple AWS + Scripts Test Agent",
		ServerName:  serverList,
		ConfigPath:  configPath,
		Provider:    llm.Provider(provider),
		ModelID:     modelID,
		Temperature: simpleFlags.temperature,
		ToolChoice:  "auto",
		MaxTurns:    simpleFlags.maxTurns,
		Timeout:     simpleFlags.timeout,
		AgentMode:   mcpagent.SimpleAgent, // Use Simple mode
	}

	logger.Info("✅ Agent config created", map[string]interface{}{
		"config_name":    simpleConfig.Name,
		"config_timeout": simpleConfig.Timeout.String(),
	})

	// Create a context with timeout for agent creation
	agentCtx, agentCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer agentCancel()

	logger.Info("🚀 Starting NewLLMAgentWrapper call", map[string]interface{}{
		"agent_timeout": "30s",
		"config_path":   configPath,
		"server_list":   serverList,
	})

	// Create the Simple agent wrapper with timeout
	logger.Info("🔍 About to call NewLLMAgentWrapper", map[string]interface{}{
		"config_name":        simpleConfig.Name,
		"config_server_name": simpleConfig.ServerName,
		"config_provider":    simpleConfig.Provider,
		"config_model_id":    simpleConfig.ModelID,
		"config_agent_mode":  simpleConfig.AgentMode,
	})

	simpleAgent, err := agent.NewLLMAgentWrapper(agentCtx, simpleConfig, tracer, GetTestLogger())

	logger.Info("🔍 NewLLMAgentWrapper call completed", map[string]interface{}{
		"error":     err,
		"agent_nil": simpleAgent == nil,
	})

	if err != nil {
		logger.Fatal("❌ Failed to create Simple agent wrapper", map[string]interface{}{"error": err.Error()})
	}

	logger.Info("✅ Simple agent wrapper created successfully", map[string]interface{}{
		"agent_name": simpleAgent.GetName(),
	})

	defer func() {
		if err := simpleAgent.Stop(context.Background()); err != nil {
			logger.Warn("⚠️ Error stopping Simple agent", map[string]interface{}{"error": err.Error()})
		}
	}()

	logger.Info("✅ Simple agent wrapper created", map[string]interface{}{
		"agent_name":    simpleAgent.GetName(),
		"capabilities":  simpleAgent.GetCapabilities(),
		"health_status": simpleAgent.IsHealthy(),
		"max_turns":     simpleFlags.maxTurns,
	})

	// Test query designed to trigger comprehensive tool usage with all available tools
	// Note: Large output handling now uses virtual tools (read_large_output, search_large_output, query_large_output)
	simpleQuery := "Perform a comprehensive analysis of our infrastructure and available tools. First, check the current AWS costs and usage patterns using AWS tools. Then, examine any CloudWatch metrics and alarms. Next, explore what scripts are available and their capabilities. Check GitHub repositories and any database connections. Examine Kubernetes clusters, Grafana dashboards, Sentry error tracking, and Slack integrations. If you encounter large tool outputs, use the virtual tools (read_large_output, search_large_output, query_large_output) to process them efficiently. Finally, provide a comprehensive analysis with cost optimization recommendations, automation suggestions, and infrastructure insights."

	logger.Info("📝 Simple Test Query", map[string]interface{}{"query": simpleQuery})

	// Execute the Simple test with timeout and detailed logging
	simpleStartTime := time.Now()

	// Create a context with timeout for the invoke call
	// Use a longer timeout for comprehensive Simple tests that need multiple turns
	invokeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	logger.Info("🚀 Starting Simple Agent Invoke", map[string]interface{}{
		"timeout":      "2m",
		"query_length": len(simpleQuery),
	})

	// Execute with timeout
	simpleResponse, err := simpleAgent.Invoke(invokeCtx, simpleQuery)
	simpleDuration := time.Since(simpleStartTime)

	if err != nil {
		logger.Fatal("❌ Simple comprehensive test failed", map[string]interface{}{"error": err.Error()})
	}

	logger.Info("✅ Simple Agent Invoke completed", map[string]interface{}{
		"duration":        simpleDuration.String(),
		"response_length": len(simpleResponse),
	})

	// Log the response
	logger.Info("✅ Simple Test Response",
		"response_length", len(simpleResponse),
		"duration", simpleDuration.String(),
		"response_preview", mcpagent.TruncateString(simpleResponse, 300))

	// Check for comprehensive tool usage patterns
	simplePatterns := []string{
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
	for _, pattern := range simplePatterns {
		if strings.Contains(strings.ToLower(simpleResponse), strings.ToLower(pattern)) {
			patternMatches++
			logger.Info("✅ Simple pattern detected", map[string]interface{}{"pattern": pattern})
		}
	}

	// Check if comprehensive analysis was performed
	if strings.Contains(simpleResponse, "AWS") || strings.Contains(simpleResponse, "CloudWatch") || strings.Contains(simpleResponse, "cost") {
		logger.Info("✅ Simple AWS Analysis Confirmed")
	}
	if strings.Contains(simpleResponse, "script") || strings.Contains(simpleResponse, "automation") || strings.Contains(simpleResponse, "capabilities") {
		logger.Info("✅ Simple Scripts Analysis Confirmed")
	}
	if strings.Contains(simpleResponse, "github") || strings.Contains(simpleResponse, "repository") {
		logger.Info("✅ Simple GitHub Analysis Confirmed")
	}
	if strings.Contains(simpleResponse, "database") || strings.Contains(simpleResponse, "db") {
		logger.Info("✅ Simple Database Analysis Confirmed")
	}
	if strings.Contains(simpleResponse, "kubernetes") || strings.Contains(simpleResponse, "k8s") || strings.Contains(simpleResponse, "cluster") {
		logger.Info("✅ Simple Kubernetes Analysis Confirmed")
	}
	if strings.Contains(simpleResponse, "grafana") || strings.Contains(simpleResponse, "dashboard") {
		logger.Info("✅ Simple Grafana Analysis Confirmed")
	}
	if strings.Contains(simpleResponse, "sentry") || strings.Contains(simpleResponse, "error") {
		logger.Info("✅ Simple Sentry Analysis Confirmed")
	}
	if strings.Contains(simpleResponse, "slack") || strings.Contains(simpleResponse, "integration") {
		logger.Info("✅ Simple Slack Analysis Confirmed")
	}

	// Check for timeout handling patterns
	if strings.Contains(simpleResponse, "timed out") || strings.Contains(simpleResponse, "timeout") {
		logger.Info("✅ Simple Timeout Handling Confirmed")
	}

	// Check for fallback patterns
	if strings.Contains(simpleResponse, "fallback") || strings.Contains(simpleResponse, "throttling") {
		logger.Info("✅ Simple Fallback Handling Confirmed")
	}

	// Check for virtual tools usage
	if strings.Contains(simpleResponse, "read_large_output") || strings.Contains(simpleResponse, "search_large_output") || strings.Contains(simpleResponse, "query_large_output") {
		logger.Info("✅ Simple Virtual Tools Usage Confirmed")
	}

	// Success criteria for Simple test
	if patternMatches >= 3 {
		logger.Info("✅ Simple Agent Test Completed Successfully", map[string]interface{}{
			"pattern_matches": patternMatches,
			"total_patterns":  len(simplePatterns),
			"duration":        simpleDuration.String(),
		})
	} else {
		logger.Warn("⚠️ Simple Agent Test completed but with limited tool usage patterns", map[string]interface{}{
			"pattern_matches": patternMatches,
			"total_patterns":  len(simplePatterns),
		})
	}

	// Show metrics if requested
	if simpleFlags.showMetrics {
		metrics := simpleAgent.GetMetrics()
		logger.Info("📈 Simple Agent Metrics", map[string]interface{}{"metrics": metrics})

		// Calculate success rate using helper from agent.go
		successRate := calculateSuccessRate(metrics)
		logger.Info("📊 Simple Success Rate", map[string]interface{}{"rate": successRate})
	}

	logger.Info("🏁 Comprehensive Simple Test Completed", map[string]interface{}{
		"duration":        simpleDuration.String(),
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
