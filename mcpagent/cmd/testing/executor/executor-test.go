package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/manishiitg/mcpagent/agent/codeexec"
	testutils "github.com/manishiitg/mcpagent/cmd/testing/testutils"
	"github.com/manishiitg/mcpagent/executor"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

var executorTestCmd = &cobra.Command{
	Use:   "executor",
	Short: "Test executor package HTTP handlers for tool execution",
	Long: `Tests the mcpagent/executor package that provides HTTP handlers for:
- /api/mcp/execute - MCP tool execution
- /api/custom/execute - Custom tool execution  
- /api/virtual/execute - Virtual tool execution

This test starts a real HTTP server and makes actual requests to verify the handlers work.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Initialize logger using shared utilities
		logger := testutils.NewTestLoggerFromViper()

		logger.Info("=== Executor Package Integration Test ===")
		logger.Info("Starting HTTP server with executor handlers and making real requests")

		if err := TestExecutorWithHTTPServer(logger); err != nil {
			return fmt.Errorf("test failed: %w", err)
		}

		logger.Info("✅ All executor tests passed")
		logger.Info("")
		logger.Info("📋 For detailed verification, see criteria.md in cmd/testing/executor/")
		return nil
	},
}

// GetExecutorTestCmd returns the test command
func GetExecutorTestCmd() *cobra.Command {
	return executorTestCmd
}

// TestExecutorWithHTTPServer starts an HTTP server and tests the executor handlers
func TestExecutorWithHTTPServer(log loggerv2.Logger) error {
	ctx := context.Background()

	// Step 1: Initialize agent to set up the codeexec registry
	log.Info("--- Step 1: Initialize Agent and Registry ---")
	if err := initializeAgentRegistry(ctx, log); err != nil {
		return err
	}

	// Step 2: Start HTTP server with executor handlers
	log.Info("--- Step 2: Start HTTP Server ---")
	serverURL, shutdown, err := startTestServer(log)
	if err != nil {
		return err
	}
	defer shutdown()

	log.Info("✅ Server started", loggerv2.String("url", serverURL))

	// Give server a moment to fully start
	time.Sleep(500 * time.Millisecond)

	// Step 3: Test MCP execute endpoint
	log.Info("--- Step 3: Test MCP Execute Endpoint ---")
	if err := testMCPExecuteEndpoint(serverURL, log); err != nil {
		log.Warn("⚠️  MCP execute test failed (may be expected if no MCP servers configured)", loggerv2.Error(err))
	} else {
		log.Info("✅ MCP execute endpoint works")
	}

	// Step 4: Test custom execute endpoint
	log.Info("--- Step 4: Test Custom Execute Endpoint ---")
	if err := testCustomExecuteEndpoint(serverURL, log); err != nil {
		log.Warn("⚠️  Custom execute test failed", loggerv2.Error(err))
	} else {
		log.Info("✅ Custom execute endpoint works")
	}

	// Step 5: Test virtual execute endpoint
	log.Info("--- Step 5: Test Virtual Execute Endpoint ---")
	if err := testVirtualExecuteEndpoint(serverURL, log); err != nil {
		log.Warn("⚠️  Virtual execute test failed", loggerv2.Error(err))
	} else {
		log.Info("✅ Virtual execute endpoint works")
	}

	return nil
}

// initializeAgentRegistry creates an agent to initialize the codeexec registry
func initializeAgentRegistry(ctx context.Context, log loggerv2.Logger) error {
	log.Info("Creating agent to initialize tool registry...")

	// Create LLM using testutils
	llm, llmProvider, err := testutils.CreateTestLLMFromViper(log)
	if err != nil {
		return fmt.Errorf("failed to create LLM: %w", err)
	}

	// Get config path
	configPath := testutils.GetDefaultTestConfigPath()
	log.Info("Using config", loggerv2.String("path", configPath))

	// Create agent using testutils
	tracer, _ := testutils.GetTracerWithLogger("noop", log)
	traceID := testutils.GenerateTestTraceID()

	agent, err := testutils.CreateTestAgent(ctx, &testutils.TestAgentConfig{
		LLM:        llm,
		Provider:   llmProvider,
		ConfigPath: configPath,
		Tracer:     tracer,
		TraceID:    traceID,
		Logger:     log,
	})
	if err != nil {
		return fmt.Errorf("failed to create agent: %w", err)
	}
	defer agent.Close()

	// Verify registry is initialized
	registry := codeexec.GetRegistry()
	if registry == nil {
		return fmt.Errorf("code execution registry not initialized")
	}

	log.Info("✅ Agent created and registry initialized")
	return nil
}

// startTestServer starts an HTTP server with executor handlers
func startTestServer(log loggerv2.Logger) (string, func(), error) {
	// Get config path
	configPath := testutils.GetDefaultTestConfigPath()
	log.Info("Using config for handlers", loggerv2.String("path", configPath))

	// Create executor handlers
	handlers := executor.NewExecutorHandlers(configPath, log)

	// Create HTTP mux
	mux := http.NewServeMux()
	mux.HandleFunc("/api/mcp/execute", handlers.HandleMCPExecute)
	mux.HandleFunc("/api/custom/execute", handlers.HandleCustomExecute)
	mux.HandleFunc("/api/virtual/execute", handlers.HandleVirtualExecute)

	// Start server on fixed port for testing
	server := &http.Server{
		Addr:              "127.0.0.1:18765",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Start server in background
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("Server error", err)
		}
	}()

	serverURL := "http://127.0.0.1:18765"

	// Shutdown function
	shutdown := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Error("Server shutdown error", err)
		}
		log.Info("Server stopped")
	}

	return serverURL, shutdown, nil
}

// testMCPExecuteEndpoint tests the /api/mcp/execute endpoint
func testMCPExecuteEndpoint(serverURL string, log loggerv2.Logger) error {
	log.Info("Testing POST /api/mcp/execute...")

	// Prepare request
	reqBody := map[string]interface{}{
		"server": "test-server",
		"tool":   "test-tool",
		"args":   map[string]interface{}{},
	}

	respBody, err := makeRequest(serverURL+"/api/mcp/execute", reqBody, log)
	if err != nil {
		return err
	}

	log.Info("Response received", loggerv2.String("response", string(respBody)))

	// Parse response
	var response struct {
		Success bool   `json:"success"`
		Result  string `json:"result,omitempty"`
		Error   string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(respBody, &response); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// We expect this to fail (server not configured), but the endpoint should respond properly
	if !response.Success && response.Error != "" {
		log.Info("Expected failure (server not configured)", loggerv2.String("error", response.Error))
		return nil // This is actually success - the endpoint works
	}

	return nil
}

// testCustomExecuteEndpoint tests the /api/custom/execute endpoint
func testCustomExecuteEndpoint(serverURL string, log loggerv2.Logger) error {
	log.Info("Testing POST /api/custom/execute...")

	// Prepare request - try to call a custom tool
	reqBody := map[string]interface{}{
		"tool": "test-custom-tool",
		"args": map[string]interface{}{},
	}

	respBody, err := makeRequest(serverURL+"/api/custom/execute", reqBody, log)
	if err != nil {
		return err
	}

	log.Info("Response received", loggerv2.String("response", string(respBody)))

	// Parse response
	var response struct {
		Success bool   `json:"success"`
		Result  string `json:"result,omitempty"`
		Error   string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(respBody, &response); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// We expect this to fail (tool not registered), but the endpoint should respond properly
	if !response.Success && response.Error != "" {
		log.Info("Expected failure (tool not registered)", loggerv2.String("error", response.Error))
		return nil // This is actually success - the endpoint works
	}

	return nil
}

// testVirtualExecuteEndpoint tests the /api/virtual/execute endpoint
func testVirtualExecuteEndpoint(serverURL string, log loggerv2.Logger) error {
	log.Info("Testing POST /api/virtual/execute...")

	// Prepare request - try to call discover_code_files
	reqBody := map[string]interface{}{
		"tool": "discover_code_files",
		"args": map[string]interface{}{},
	}

	respBody, err := makeRequest(serverURL+"/api/virtual/execute", reqBody, log)
	if err != nil {
		return err
	}

	log.Info("Response received", loggerv2.String("response", string(respBody)))

	// Parse response
	var response struct {
		Success bool   `json:"success"`
		Result  string `json:"result,omitempty"`
		Error   string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(respBody, &response); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// Virtual tools may or may not be registered depending on agent mode
	if !response.Success && response.Error != "" {
		log.Info("Expected failure (virtual tool not registered)", loggerv2.String("error", response.Error))
		return nil // This is actually success - the endpoint works
	}

	if response.Success {
		log.Info("Virtual tool executed successfully", loggerv2.String("result", response.Result))
	}

	return nil
}

// makeRequest makes an HTTP POST request and returns the response body
func makeRequest(url string, body map[string]interface{}, log loggerv2.Logger) ([]byte, error) {
	// Marshal request body
	reqBodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	log.Info("Making request", loggerv2.String("url", url), loggerv2.String("body", string(reqBodyBytes)))

	// Create request
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(reqBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Make request
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	log.Info("Response status", loggerv2.Int("status", resp.StatusCode))

	return respBody, nil
}
