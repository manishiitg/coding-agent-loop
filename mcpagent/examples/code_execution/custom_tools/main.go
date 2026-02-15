package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joho/godotenv"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/mcpagent/executor"
	"github.com/manishiitg/mcpagent/llm"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"

	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/openai"
)

func main() {
	// Load .env file if it exists
	if _, err := os.Stat(".env"); err == nil {
		if err := godotenv.Load(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Could not load .env file: %v\n", err)
		}
	}

	// Step 1: Get OpenAI API key from environment
	openAIKey := os.Getenv("OPENAI_API_KEY")
	if openAIKey == "" {
		fmt.Fprintf(os.Stderr, "Please set OPENAI_API_KEY environment variable\n")
		os.Exit(1)
	}

	// Step 2: Set up file loggers
	// Create logs directory if it doesn't exist
	logDir := "logs"
	if err := os.MkdirAll(logDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create logs directory: %v\n", err)
		os.Exit(1)
	}

	// Define log file paths
	llmLogFile := filepath.Join(logDir, "llm.log")
	agentLogFile := filepath.Join(logDir, "custom-tools-code-execution.log")

	// Clear existing log files to start fresh for this run
	if err := os.Truncate(llmLogFile, 0); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Warning: Failed to clear LLM log file: %v\n", err)
	}
	if err := os.Truncate(agentLogFile, 0); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Warning: Failed to clear agent log file: %v\n", err)
	}

	// Create logger for LLM operations
	llmLogger, err := loggerv2.New(loggerv2.Config{
		Level:      "info",
		Format:     "text",
		Output:     llmLogFile,
		EnableFile: false,
		FilePath:   "",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create LLM logger: %v\n", err)
		os.Exit(1)
	}
	defer llmLogger.Close()

	fmt.Printf("LLM logging to: %s (cleared)\n", llmLogFile)

	// Step 3: Initialize OpenAI LLM with file logger
	llmModel, err := llm.InitializeLLM(llm.Config{
		Provider:    llm.ProviderOpenAI,
		ModelID:     openai.ModelGPT41,
		Temperature: 0.7,
		Logger:      llmLogger,
		APIKeys: &llm.ProviderAPIKeys{
			OpenAI: &openAIKey,
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize LLM: %v\n", err)
		os.Exit(1)
	}

	// Step 4: Create logger for agent operations
	agentLogger, err := loggerv2.New(loggerv2.Config{
		Level:      "info",
		Format:     "text",
		Output:     agentLogFile,
		EnableFile: false,
		FilePath:   "",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create agent logger: %v\n", err)
		os.Exit(1)
	}
	defer agentLogger.Close()

	fmt.Printf("Agent logging to: %s (cleared)\n", agentLogFile)

	// Step 5: Set up MCP server configuration path
	configPath := "mcp_servers.json"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	// Step 6: Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	// Step 7: Start HTTP server for code execution
	// The agent will generate Go code that makes HTTP calls to this server
	serverPort := os.Getenv("MCP_API_URL")
	if serverPort == "" {
		serverPort = "8000" // Default port
	}

	// Extract port number if MCP_API_URL includes protocol
	portNum := serverPort
	if strings.HasPrefix(serverPort, "http://") || strings.HasPrefix(serverPort, "https://") {
		parts := strings.Split(serverPort, ":")
		if len(parts) >= 3 {
			portNum = parts[len(parts)-1]
		}
	}

	serverAddr := fmt.Sprintf("127.0.0.1:%s", portNum)

	// Create executor handlers
	handlers := executor.NewExecutorHandlers(configPath, agentLogger)

	// Create HTTP mux and register handlers
	mux := http.NewServeMux()
	mux.HandleFunc("/api/mcp/execute", handlers.HandleMCPExecute)
	mux.HandleFunc("/api/custom/execute", handlers.HandleCustomExecute)
	mux.HandleFunc("/api/virtual/execute", handlers.HandleVirtualExecute)

	server := &http.Server{
		Addr:    serverAddr,
		Handler: mux,
	}

	// Start server in a goroutine
	go func() {
		fmt.Printf("Starting HTTP server for code execution on %s\n", serverAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "HTTP server error: %v\n", err)
		}
	}()

	// Give server a moment to start
	time.Sleep(100 * time.Millisecond)

	// Ensure server is shut down on exit
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "Error shutting down HTTP server: %v\n", err)
		}
	}()

	// Step 8: Create the agent with code execution mode enabled
	agent, err := mcpagent.NewAgent(
		ctx,
		llmModel,
		configPath,
		mcpagent.WithLogger(agentLogger),
		mcpagent.WithCodeExecutionMode(true), // Enable code execution mode
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create agent: %v\n", err)
		os.Exit(1)
	}

	// Step 9: Register custom tool
	// In code execution mode, custom tools are accessible via generated Go code
	// The agent will write Go code that calls these tools through the HTTP API
	fmt.Println("=== Registering Custom Tool ===")
	fmt.Println()

	// Weather Simulator - data category
	weatherParams := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"location": map[string]interface{}{
				"type":        "string",
				"description": "The location to get weather for",
			},
			"unit": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"celsius", "fahrenheit"},
				"description": "Temperature unit",
			},
		},
		"required": []string{"location"},
	}

	err = agent.RegisterCustomTool(
		"get_weather",
		"Gets simulated weather data for a given location. Returns temperature, condition, and humidity.",
		weatherParams,
		weatherTool,
		"data",
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to register get_weather tool: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✓ Registered get_weather tool (category: data)")

	fmt.Println()
	fmt.Println("=== Code Execution Mode with Custom Tool ===")
	fmt.Println("The agent will automatically write and execute Go code that uses the custom weather tool.")
	fmt.Println("Custom tools are accessible via generated Go code through the HTTP API.")
	fmt.Println()

	// Step 10: Example questions that will trigger code execution with custom tool
	questions := []string{
		"Get the weather for San Francisco in fahrenheit",
		"Get the weather for New York in celsius",
	}

	// Step 11: Use AskWithHistory for multi-turn conversations
	conversationHistory := []llm.MessageContent{}

	for i, question := range questions {
		fmt.Printf("\n=== Question %d/%d ===\n", i+1, len(questions))
		fmt.Printf("Q: %s\n\n", question)

		// Add user message to conversation history
		userMessage := llm.MessageContent{
			Role:  llm.ChatMessageTypeHuman,
			Parts: []llm.ContentPart{llm.TextContent{Text: question}},
		}
		conversationHistory = append(conversationHistory, userMessage)

		// Get answer with conversation history
		answer, updatedHistory, err := agent.AskWithHistory(ctx, conversationHistory)
		if err != nil {
			agentLogger.Error("Failed to get answer from agent", err)
			fmt.Fprintf(os.Stderr, "Failed to get answer: %v\n", err)
			continue
		}

		// Update conversation history
		conversationHistory = updatedHistory

		// Print the answer
		fmt.Println("=== Agent Response ===")
		fmt.Println(answer)
		fmt.Println("=====================")
		fmt.Println()
	}

	fmt.Println("=== Example Complete ===")
	fmt.Println("Custom weather tool was used via generated Go code in code execution mode.")
	fmt.Println("Check the logs directory for detailed execution logs.")
	agentLogger.Info("Custom tool code execution example completed")
}

// weatherTool simulates weather data
func weatherTool(ctx context.Context, args map[string]interface{}) (string, error) {
	location, ok := args["location"].(string)
	if !ok {
		return "", fmt.Errorf("location must be a string")
	}

	unit := "celsius"
	if u, ok := args["unit"].(string); ok {
		unit = u
	}

	// Always return hardcoded temperature of 50
	temp := 50.0
	unitSymbol := "°C"
	if unit == "fahrenheit" {
		unitSymbol = "°F"
	}

	return fmt.Sprintf("Weather for %s: %.1f%s, sunny, humidity 50%%", location, temp, unitSymbol), nil
}
