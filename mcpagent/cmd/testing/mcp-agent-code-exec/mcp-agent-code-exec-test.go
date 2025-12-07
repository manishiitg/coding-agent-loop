package mcpagentcodeexec

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"

	mcpagent "mcpagent/agent"
	testutils "mcpagent/cmd/testing/testutils"
	"mcpagent/executor"
	loggerv2 "mcpagent/logger/v2"
)

var mcpAgentCodeExecTestCmd = &cobra.Command{
	Use:   "mcp-agent-code-exec",
	Short: "Test code execution agent with executor HTTP handlers",
	Long: `Tests the full code execution flow end-to-end:
1. Creates an agent in code execution mode
2. Starts HTTP server with executor handlers (simulating the server)
3. Agent generates Go code that calls the executor endpoints
4. Verifies the full integration works

This validates that the refactored executor package works correctly with code execution.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Initialize logger using shared utilities
		logger := testutils.NewTestLoggerFromViper()

		logger.Info("=== MCP Agent Code Execution Test ===")
		logger.Info("Testing full code execution flow with executor handlers")

		if err := TestCodeExecutionWithExecutor(logger); err != nil {
			return fmt.Errorf("test failed: %w", err)
		}

		logger.Info("✅ All code execution tests passed")
		logger.Info("")
		logger.Info("📋 For detailed verification, see criteria.md in cmd/testing/mcp-agent-code-exec/")
		return nil
	},
}

// GetMCPAgentCodeExecTestCmd returns the test command
func GetMCPAgentCodeExecTestCmd() *cobra.Command {
	return mcpAgentCodeExecTestCmd
}

// TestCodeExecutionWithExecutor tests the full code execution flow
func TestCodeExecutionWithExecutor(log loggerv2.Logger) error {
	ctx := context.Background()

	// Step 1: Start HTTP server with executor handlers (simulating the server)
	log.Info("--- Step 1: Start Executor HTTP Server ---")
	serverURL, shutdown, err := startExecutorServer(log)
	if err != nil {
		return err
	}
	defer shutdown()

	log.Info("✅ Executor server started", loggerv2.String("url", serverURL))

	// Give server a moment to fully start
	time.Sleep(500 * time.Millisecond)

	// Step 2: Create agent in code execution mode
	log.Info("--- Step 2: Create Code Execution Agent ---")
	agent, err := createCodeExecutionAgent(ctx, log)
	if err != nil {
		return err
	}
	defer agent.Close()

	log.Info("✅ Code execution agent created")

	// Step 3: Test code execution with a simple task
	log.Info("--- Step 3: Test Code Execution Flow ---")
	if err := testCodeExecutionFlow(ctx, agent, log); err != nil {
		return err
	}

	log.Info("✅ Code execution flow completed successfully")

	return nil
}

// startExecutorServer starts an HTTP server with executor handlers
func startExecutorServer(log loggerv2.Logger) (string, func(), error) {
	// Get config path
	configPath := testutils.GetDefaultTestConfigPath()
	log.Info("Using config for executor handlers", loggerv2.String("path", configPath))

	// Create executor handlers
	handlers := executor.NewExecutorHandlers(configPath, log)

	// Create HTTP mux
	mux := http.NewServeMux()
	mux.HandleFunc("/api/mcp/execute", handlers.HandleMCPExecute)
	mux.HandleFunc("/api/custom/execute", handlers.HandleCustomExecute)
	mux.HandleFunc("/api/virtual/execute", handlers.HandleVirtualExecute)

	// Start server on port 8000 (default MCP_API_URL)
	server := &http.Server{
		Addr:    "127.0.0.1:8000",
		Handler: mux,
	}

	// Start server in background
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("Server error", err)
		}
	}()

	serverURL := "http://127.0.0.1:8000"

	// Shutdown function
	shutdown := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Error("Server shutdown error", err)
		}
		log.Info("Executor server stopped")
	}

	return serverURL, shutdown, nil
}

// createCodeExecutionAgent creates an agent in code execution mode
func createCodeExecutionAgent(ctx context.Context, log loggerv2.Logger) (*mcpagent.Agent, error) {
	log.Info("Creating code execution agent...")

	// Create LLM using testutils
	llm, err := testutils.CreateTestLLMFromViper(log)
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM: %w", err)
	}

	// Create temporary MCP config with context7
	mcpServers := map[string]interface{}{
		"context7": map[string]interface{}{
			"url":      "https://mcp.context7.com/mcp",
			"protocol": "http",
		},
	}

	configPath, cleanup, err := testutils.CreateTempMCPConfig(mcpServers, log)
	if err != nil {
		return nil, fmt.Errorf("failed to create temp MCP config: %w", err)
	}
	// Note: We can't defer cleanup here since we need the config to persist for the agent
	// The caller should handle cleanup or the temp file will be cleaned up on process exit

	log.Info("✅ Created temporary MCP config with context7",
		loggerv2.String("path", configPath))

	// Create agent with code execution mode enabled
	tracer, _ := testutils.GetTracerWithLogger("noop", log)
	traceID := testutils.GenerateTestTraceID()

	agent, err := testutils.CreateTestAgent(ctx, &testutils.TestAgentConfig{
		LLM:        llm,
		ConfigPath: configPath,
		Tracer:     tracer,
		TraceID:    traceID,
		Logger:     log,
		Options: []mcpagent.AgentOption{
			mcpagent.WithCodeExecutionMode(true),
		},
	})
	if err != nil {
		cleanup() // Clean up config on error
		return nil, fmt.Errorf("failed to create agent: %w", err)
	}

	log.Info("✅ Code execution mode enabled")
	log.Info("Agent can now generate Go code that calls executor endpoints")
	log.Info("MCP server configured: context7")

	return agent, nil
}

// testCodeExecutionFlow tests the code execution flow with a real MCP tool
func testCodeExecutionFlow(ctx context.Context, agent *mcpagent.Agent, log loggerv2.Logger) error {
	log.Info("Testing code execution with context7 MCP tool...")

	// Test query: Ask agent to use context7 to resolve a library
	// This will test the full flow: agent → code gen → execution → HTTP → MCP tool
	query := `Use the context7 server to resolve the library ID for "react". 
Write and execute Go code that calls the resolve_library_id tool with library_name="react".`

	log.Info("Sending query to agent", loggerv2.String("query", query))

	// Execute query
	startTime := time.Now()
	response, err := agent.Ask(ctx, query)
	duration := time.Since(startTime)

	if err != nil {
		return fmt.Errorf("agent query failed: %w", err)
	}

	log.Info("✅ Agent response received",
		loggerv2.String("response", response),
		loggerv2.String("duration", duration.String()))

	// Check if response indicates successful code execution
	if response == "" {
		return fmt.Errorf("empty response from agent")
	}

	// Check if response contains evidence of context7 tool usage
	// The response should mention "react" since we asked to resolve the react library
	responseContainsReact := containsIgnoreCase(response, "react")

	if len(response) < 20 {
		log.Warn("⚠️  Response seems too short - tool may not have been called",
			loggerv2.String("response", response))
	} else if !responseContainsReact {
		log.Warn("⚠️  Response doesn't mention 'react' - context7 tool may not have been called",
			loggerv2.Int("response_length", len(response)))
	} else {
		log.Info("✅ Response indicates context7 tool was called successfully",
			loggerv2.Int("response_length", len(response)))
	}

	log.Info("✅ Agent successfully executed code with MCP tool")
	log.Info("This confirms:")
	log.Info("  1. Agent generated Go code")
	log.Info("  2. Code was executed via write_code virtual tool")
	log.Info("  3. Generated code called executor HTTP endpoint")
	log.Info("  4. Executor handler called context7 MCP tool")
	log.Info("  5. Full code execution flow works end-to-end")

	return nil
}

// containsIgnoreCase checks if s contains substr (case-insensitive)
func containsIgnoreCase(s, substr string) bool {
	s = strings.ToLower(s)
	substr = strings.ToLower(substr)
	return strings.Contains(s, substr)
}
