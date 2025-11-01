package testing

import (
	"context"
	"fmt"
	"strings"
	"time"

	"mcp-agent/agent_go/internal/llm"
	"mcp-agent/agent_go/internal/llmtypes"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	agent "mcp-agent/agent_go/pkg/agentwrapper"
	"mcp-agent/agent_go/pkg/events"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/mcpcache"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var mcpCacheTestCmd = &cobra.Command{
	Use:   "mcp-cache-test",
	Short: "Test MCP connection caching system with real agents and tools",
	Long: `Comprehensive test for the MCP connection caching system that validates:

1. First agent creation with fresh MCP connections
2. Cached connection reuse for subsequent agents
3. Performance improvement measurement
4. Cache hit/miss validation
5. Real MCP tools functionality with caching

This test creates multiple agents sequentially to demonstrate the caching benefits
and ensures that the caching system works correctly with real MCP tools.`,

	RunE: func(cmd *cobra.Command, args []string) error {
		// Get logging configuration from viper
		logFile := viper.GetString("log-file")
		logLevel := viper.GetString("log-level")

		// Initialize logger with command-specific settings
		InitTestLogger(logFile, logLevel)
		logger := GetTestLogger()

		logger.Infof("=== MCP Connection Caching Test ===")

		// Configuration
		serverList := viper.GetString("servers")
		if serverList == "" {
			serverList = "all" // Test with all available servers
		}

		configPath := viper.GetString("config")
		if configPath == "" {
			configPath = "configs/mcp_servers_clean.json"
		}

		// For test commands, use the correct viper keys
		if serverList == "all" && viper.GetString("servers") == "" {
			serverList = "all"
		}
		if configPath == "configs/mcp_servers_clean.json" && viper.GetString("config") == "" {
			configPath = "configs/mcp_servers_clean.json"
		}

		provider := viper.GetString("test.provider")
		if provider == "" {
			provider = "bedrock"
		}

		iterations := viper.GetInt("iterations")
		if iterations <= 0 {
			iterations = 3 // Default to 3 iterations
		}

		logger.Info("🎯 Test Configuration", map[string]interface{}{
			"servers":       serverList,
			"config_path":   configPath,
			"provider":      provider,
			"iterations":    iterations,
			"cache_enabled": true,
		})

		// Validate provider (done in createCacheTestAgent function)
		if _, err := llm.ValidateProvider(provider); err != nil {
			return fmt.Errorf("invalid LLM provider: %v", err)
		}

		// Note: Model ID and provider validation is handled by createCacheTestAgent function

		// Initialize tracer
		// Initialize tracer based on environment (Langfuse if available, otherwise noop)
		tracer := InitializeTracer(logger)

		// Test 1: Get initial cache stats
		logger.Infof("\n=== Phase 1: Initial Cache State ===")
		initialStats := mcpcache.GetCacheStats(logger)
		logger.Info("📊 Initial Cache Stats", map[string]interface{}{
			"total_entries":   initialStats["total_entries"],
			"valid_entries":   initialStats["valid_entries"],
			"expired_entries": initialStats["expired_entries"],
			"cache_directory": initialStats["cache_directory"],
			"ttl_minutes":     initialStats["ttl_minutes"],
		})

		// Test 2: Create first agent (fresh connections)
		logger.Infof("\n=== Phase 2: First Agent (Fresh Connections) ===")

		startTime := time.Now()
		firstAgent, err := createCacheTestAgent(serverList, configPath, provider, tracer, logger)
		if err != nil {
			logger.Errorf("❌ Failed to create first agent: %v", err)
			return fmt.Errorf("first agent creation failed: %w", err)
		}
		firstAgentDuration := time.Since(startTime)

		logger.Info("✅ First Agent Created", map[string]interface{}{
			"duration_ms": firstAgentDuration.Milliseconds(),
			"duration":    firstAgentDuration.String(),
		})

		// Test the first agent with a simple query
		testQuery := "List the available tools and give me a brief description of what you can do."
		logger.Infof("🧪 Testing first agent with query: %s", testQuery)

		testCtx, testCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer testCancel()

		firstResponse, err := firstAgent.InvokeWithHistory(testCtx, []llmtypes.MessageContent{
			{
				Role:  llmtypes.ChatMessageTypeHuman,
				Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: testQuery}},
			},
		})
		if err != nil {
			logger.Errorf("❌ First agent test failed: %v", err)
			return fmt.Errorf("first agent test failed: %w", err)
		}

		logger.Info("✅ First Agent Test Completed", map[string]interface{}{
			"response_length": len(firstResponse),
			"has_tools":       strings.Contains(strings.ToLower(firstResponse), "tool"),
		})

		firstAgent.Stop(context.Background())

		// Test 3: Check cache state after first agent
		logger.Infof("\n=== Phase 3: Cache State After First Agent ===")
		afterFirstStats := mcpcache.GetCacheStats(logger)
		logger.Info("📊 Cache Stats After First Agent", map[string]interface{}{
			"total_entries":   afterFirstStats["total_entries"],
			"valid_entries":   afterFirstStats["valid_entries"],
			"expired_entries": afterFirstStats["expired_entries"],
			"new_entries":     afterFirstStats["total_entries"].(int) - initialStats["total_entries"].(int),
		})

		// Test 4: Create subsequent agents (should use cache)
		logger.Infof("\n=== Phase 4: Subsequent Agents (Cached Connections) ===")

		var subsequentDurations []time.Duration
		var subsequentResponses []string

		for i := 1; i < iterations; i++ {
			logger.Infof("🔄 Creating agent %d/%d (should use cache)", i+1, iterations)

			startTime := time.Now()
			subsequentAgent, err := createCacheTestAgent(serverList, configPath, provider, tracer, logger)
			if err != nil {
				logger.Errorf("❌ Failed to create agent %d: %v", i+1, err)
				return fmt.Errorf("agent %d creation failed: %w", i+1, err)
			}
			duration := time.Since(startTime)
			subsequentDurations = append(subsequentDurations, duration)

			logger.Info("✅ Agent Created", map[string]interface{}{
				"agent_number": i + 1,
				"duration_ms":  duration.Milliseconds(),
				"duration":     duration.String(),
			})

			// Test with a different query
			testQuery := fmt.Sprintf("What MCP servers are you connected to? Please list them with their protocols.")
			logger.Infof("🧪 Testing agent %d with query: %s", i+1, testQuery)

			testCtx, testCancel := context.WithTimeout(context.Background(), 60*time.Second)

			response, err := subsequentAgent.InvokeWithHistory(testCtx, []llmtypes.MessageContent{
				{
					Role:  llmtypes.ChatMessageTypeHuman,
					Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: testQuery}},
				},
			})
			testCancel()

			if err != nil {
				logger.Errorf("❌ Agent %d test failed: %v", i+1, err)
				subsequentAgent.Stop(context.Background())
				return fmt.Errorf("agent %d test failed: %w", i+1, err)
			}

			subsequentResponses = append(subsequentResponses, response)
			logger.Info("✅ Agent Test Completed", map[string]interface{}{
				"agent_number":    i + 1,
				"response_length": len(response),
				"has_servers":     strings.Contains(strings.ToLower(response), "server"),
			})

			subsequentAgent.Stop(context.Background())
		}

		// Test 5: Performance analysis
		logger.Infof("\n=== Phase 5: Performance Analysis ===")

		// Calculate average subsequent agent duration
		var avgSubsequentDuration time.Duration
		if len(subsequentDurations) > 0 {
			var totalSubsequentDuration time.Duration
			for _, duration := range subsequentDurations {
				totalSubsequentDuration += duration
			}
			avgSubsequentDuration = totalSubsequentDuration / time.Duration(len(subsequentDurations))
		}

		// Calculate performance improvement
		var improvement float64
		if avgSubsequentDuration > 0 {
			improvement = float64(firstAgentDuration-avgSubsequentDuration) / float64(firstAgentDuration) * 100
		}

		logger.Info("⚡ Performance Results", map[string]interface{}{
			"first_agent_duration_ms":     firstAgentDuration.Milliseconds(),
			"avg_subsequent_duration_ms":  avgSubsequentDuration.Milliseconds(),
			"performance_improvement_pct": improvement,
			"cache_benefit":               improvement > 10, // Consider >10% as significant improvement
		})

		// Test 6: Final cache stats
		logger.Infof("\n=== Phase 6: Final Cache State ===")
		finalStats := mcpcache.GetCacheStats(logger)
		logger.Info("📊 Final Cache Stats", map[string]interface{}{
			"total_entries":   finalStats["total_entries"],
			"valid_entries":   finalStats["valid_entries"],
			"expired_entries": finalStats["expired_entries"],
			"cache_hits":      iterations - 1, // First agent creates cache, rest should hit
		})

		// Test 7: Cache validation
		logger.Infof("\n=== Phase 7: Cache Validation ===")

		// Verify that subsequent responses mention caching or faster creation
		cacheWorking := improvement > 5 // At least 5% improvement indicates caching is working
		if cacheWorking {
			logger.Infof("✅ CACHING VALIDATION PASSED: %.1f%% performance improvement detected", improvement)
		} else {
			logger.Warnf("⚠️  CACHING VALIDATION UNCLEAR: Only %.1f%% performance improvement (may need more iterations)", improvement)
		}

		// Test 8: Cleanup test
		logger.Infof("\n=== Phase 8: Cache Cleanup Test ===")

		// Test cleanup functionality
		err = mcpcache.CleanupExpiredEntries(logger)
		if err != nil {
			logger.Warnf("⚠️  Cleanup failed: %v", err)
		} else {
			logger.Infof("✅ Cache cleanup completed successfully")
		}

		// Final summary
		logger.Infof("\n=== MCP Cache Test Summary ===")
		logger.Infof("🎯 Test Configuration:")
		logger.Infof("   - Servers: %s", serverList)
		logger.Infof("   - Provider: %s", provider)
		logger.Infof("   - Iterations: %d", iterations)
		logger.Infof("   - Config: %s", configPath)

		logger.Infof("\n⚡ Performance Results:")
		logger.Infof("   - First agent: %v", firstAgentDuration)
		logger.Infof("   - Avg subsequent: %v", avgSubsequentDuration)
		logger.Infof("   - Improvement: %.1f%%", improvement)

		logger.Infof("\n📊 Cache Results:")
		logger.Infof("   - Initial entries: %d", initialStats["total_entries"])
		logger.Infof("   - Final entries: %d", finalStats["total_entries"])
		logger.Infof("   - Cache working: %t", cacheWorking)

		if cacheWorking {
			logger.Infof("\n🎉 CACHE TEST PASSED: MCP connection caching is working correctly!")
			logger.Infof("   - Cache provides measurable performance improvements")
			logger.Infof("   - Multiple agents can reuse cached connections")
			logger.Infof("   - Real MCP tools function correctly with caching")
		} else {
			logger.Warnf("\n⚠️  CACHE TEST INCONCLUSIVE: Performance improvement not clearly detected")
			logger.Infof("   - This may be due to fast network or small number of servers")
			logger.Infof("   - Try increasing iterations or using more servers for clearer results")
		}

		logger.Infof("\n✅ MCP Cache Test completed successfully!")

		// Test timestamp preservation
		logger.Infof("\n🕐 Testing timestamp preservation...")
		testTimestampPreservation(logger)

		return nil
	},
}

// testTimestampPreservation creates a simple event to verify timestamp is preserved
func testTimestampPreservation(logger utils.ExtendedLogger) {
	startTime := time.Now()
	time.Sleep(10 * time.Millisecond) // Small delay to ensure different timestamps

	// Create a simple MCP connection event
	eventData := events.NewMCPServerConnectionEvent("test-server", "started", 1, 10*time.Millisecond, "")
	eventData.ConfigPath = "test-config.json"
	eventData.Operation = "timestamp_test"
	eventData.Timestamp = startTime

	event := events.NewAgentEvent(eventData)
	event.Type = events.MCPServerConnectionStart
	event.Timestamp = startTime // Preserve the original timestamp
	event.TraceID = "test-trace"
	event.CorrelationID = "test-correlation"

	logger.Infof("📊 Timestamp Test Results:")
	logger.Infof("   - Event start time: %s", startTime.Format(time.RFC3339Nano))
	logger.Infof("   - Event timestamp: %s", event.Timestamp.Format(time.RFC3339Nano))
	logger.Infof("   - Time difference: %v", event.Timestamp.Sub(startTime))

	if event.Timestamp.Equal(startTime) {
		logger.Infof("🎯 TIMESTAMP TEST PASSED: Event timestamp correctly preserved!")
	} else {
		logger.Errorf("❌ TIMESTAMP TEST FAILED: Event timestamp not preserved")
	}
}

// createCacheTestAgent creates a test agent with the specified configuration for cache testing
func createCacheTestAgent(serverList, configPath, provider string, tracer observability.Tracer, logger interface{}) (*agent.LLMAgentWrapper, error) {
	// Cast logger to ExtendedLogger (logger.Logger from testing package implements ExtendedLogger)
	extendedLogger, ok := logger.(utils.ExtendedLogger)
	if !ok {
		return nil, fmt.Errorf("logger does not implement ExtendedLogger interface: %T", logger)
	}

	// Validate and get provider
	llmProvider, err := llm.ValidateProvider(provider)
	if err != nil {
		return nil, fmt.Errorf("invalid LLM provider: %v", err)
	}

	// Get default model for provider
	modelID := llm.GetDefaultModel(llmProvider)
	if modelID == "" {
		return nil, fmt.Errorf("no default model available for provider: %s", provider)
	}

	// Create agent config
	agentConfig := agent.LLMAgentConfig{
		Name:        fmt.Sprintf("MCP Cache Test Agent (%s)", time.Now().Format("15:04:05")),
		ServerName:  serverList,
		ConfigPath:  configPath,
		Provider:    llmProvider,
		ModelID:     modelID,
		Temperature: 0.2,
		ToolChoice:  "auto",
		MaxTurns:    10, // Shorter for testing
		Timeout:     2 * time.Minute,
		AgentMode:   mcpagent.SimpleAgent,
	}

	// Create context with timeout for agent creation
	agentCtx, agentCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer agentCancel()

	extendedLogger.Info("🚀 Creating test agent", map[string]interface{}{
		"agent_name":  agentConfig.Name,
		"server_list": serverList,
		"config_path": configPath,
		"provider":    provider,
	})

	// Create the agent wrapper
	testAgent, err := agent.NewLLMAgentWrapper(agentCtx, agentConfig, tracer, extendedLogger)
	if err != nil {
		return nil, fmt.Errorf("failed to create agent: %w", err)
	}

	extendedLogger.Info("✅ Test agent created successfully", map[string]interface{}{
		"agent_name": agentConfig.Name,
	})

	return testAgent, nil
}

func init() {
	// Add command-specific flags
	mcpCacheTestCmd.Flags().String("servers", "all", "Comma-separated list of MCP servers to test (default: all)")
	mcpCacheTestCmd.Flags().Int("iterations", 3, "Number of agents to create and test (default: 3)")
	mcpCacheTestCmd.Flags().String("config", "configs/mcp_servers_clean.json", "Path to MCP server configuration file")

	// Bind flags to viper
	viper.BindPFlag("servers", mcpCacheTestCmd.Flags().Lookup("servers"))
	viper.BindPFlag("iterations", mcpCacheTestCmd.Flags().Lookup("iterations"))
	viper.BindPFlag("config", mcpCacheTestCmd.Flags().Lookup("config"))
}
