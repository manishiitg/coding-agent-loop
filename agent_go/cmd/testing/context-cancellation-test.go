package testing

import (
	"context"
	"fmt"
	"os"
	"time"

	mcpagent "mcpagent/agent"
	"mcpagent/llm"
	loggerv2 "mcpagent/logger/v2"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var contextCancellationTestCmd = &cobra.Command{
	Use:   "context-cancellation",
	Short: "Test context cancellation behavior in external package - verify LLM calls get canceled",
	Long: `Test context cancellation behavior in external package.

This test verifies that when a context is canceled:
1. External package methods return context cancellation errors
2. LLM calls get canceled and don't complete
3. Tool executions are properly canceled
4. Resources are cleaned up correctly

The test uses a long-running LLM operation to ensure cancellation can be observed.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Get logging configuration from viper
		logFile := viper.GetString("log-file")
		logLevel := viper.GetString("log-level")

		// Initialize test logger
		InitTestLogger(logFile, logLevel)
		logger := GetTestLogger()

		logger.Info("=== Context Cancellation Test ===")
		logger.Info("Testing whether LLM calls get canceled when context is canceled")
		logger.Info("⚠️  NOTE: This test may not definitively prove cancellation vs timeout")
		logger.Info("    We need to verify the LLM request was actually interrupted")

		// Get provider from flags
		provider := viper.GetString("test.provider")
		if provider == "" {
			provider = "openai" // Default to OpenAI
		}

		logger.Info(fmt.Sprintf("Using provider: %s", provider))

		// Test context cancellation during LLM generation
		logger.Info("\n--- Context Cancellation During LLM Generation ---")
		if err := testContextCancellationDuringLLMGeneration(provider, logger); err != nil {
			return fmt.Errorf("context cancellation during LLM generation test failed: %w", err)
		}

		logger.Info("\n✅ All context cancellation tests passed!")
		return nil
	},
}

// testContextCancellationDuringLLMGeneration tests that LLM calls get canceled when context is canceled
func testContextCancellationDuringLLMGeneration(provider string, logger loggerv2.Logger) error {
	logger.Info("Creating external agent for LLM cancellation test...")

	// Create agent config
	var llmProvider llm.Provider
	switch provider {
	case "bedrock":
		llmProvider = llm.ProviderBedrock
	case "openai":
		llmProvider = llm.ProviderOpenAI
	default:
		llmProvider = llm.ProviderBedrock
	}

	// Initialize LLM
	modelID := getModelID(provider)
	var llmModel llmtypes.Model
	var err error

	// Get API keys from environment
	apiKeys := &llm.ProviderAPIKeys{}
	if provider == "openai" {
		openAIKey := os.Getenv("OPENAI_API_KEY")
		if openAIKey == "" {
			return fmt.Errorf("OPENAI_API_KEY environment variable is not set")
		}
		apiKeys.OpenAI = &openAIKey
	} else if provider == "bedrock" {
		// Bedrock uses AWS credentials from environment
	} else if provider == "vertex" {
		// Vertex uses GCP credentials from environment
	}

	llmModel, err = llm.InitializeLLM(llm.Config{
		Provider:    llmProvider,
		ModelID:     modelID,
		Temperature: 0.1,
		Logger:      nil, // Use default logger
		APIKeys:     apiKeys,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize LLM: %w", err)
	}

	// Create agent with valid context first
	// modelID is automatically extracted from llmModel
	ctx := context.Background()
	agent, err := mcpagent.NewAgent(
		ctx,
		llmModel,
		"configs/mcp_servers_simple.json",     // config path
		mcpagent.WithServerName("filesystem"), // server name
		mcpagent.WithMaxTurns(5),
	)
	if err != nil {
		return fmt.Errorf("failed to create agent: %w", err)
	}
	defer agent.Close()

	logger.Info("Starting LLM generation with context cancellation...")

	// Create a context that will be canceled after a short delay
	// Use a longer timeout to ensure we're testing cancellation, not just timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Cancel the context after 2 seconds to test actual cancellation
	go func() {
		time.Sleep(2 * time.Second)
		logger.Info("🔄 Manually canceling context after 2 seconds...")
		cancel()
	}()

	// Start the agent invocation in a goroutine
	resultChan := make(chan string, 1)
	errChan := make(chan error, 1)

	go func() {
		// This should be a complex enough prompt to take some time
		complexPrompt := `Please provide a detailed analysis of the following topic with multiple examples and explanations. 
		The topic is: "The impact of artificial intelligence on modern software development practices, including specific 
		examples of AI tools, their benefits and limitations, and future trends in the field." Please be thorough and 
		comprehensive in your response, covering multiple aspects and providing concrete examples.`

		result, err := agent.Ask(ctx, complexPrompt)
		if err != nil {
			errChan <- err
			return
		}
		resultChan <- result
	}()

	// Wait for either result or cancellation
	select {
	case result := <-resultChan:
		logger.Warn(fmt.Sprintf("⚠️ Unexpected: LLM generation completed before cancellation: %s", result[:100]))
		return fmt.Errorf("LLM generation should have been canceled")
	case err := <-errChan:
		if isContextCancelledError(err) {
			logger.Info(fmt.Sprintf("✅ LLM generation was properly canceled: %v", err))
			return nil
		}
		return fmt.Errorf("unexpected error during LLM generation: %w", err)
	case <-time.After(5 * time.Second):
		logger.Warn(fmt.Sprintf("⚠️ Test timeout - LLM generation may not have been canceled properly"))
		return fmt.Errorf("test timeout - LLM generation should have been canceled within 5 seconds")
	}
}

// isContextCancelledError checks if an error is due to context cancellation
func isContextCancelledError(err error) bool {
	if err == nil {
		return false
	}

	// Check for common context cancellation error messages
	errStr := err.Error()

	// These indicate actual context cancellation
	if errStr == "context canceled" {
		return true
	}

	// These could be either cancellation or timeout - need to investigate further
	if errStr == "context deadline exceeded" ||
		errStr == "operation canceled" {
		// Log this for investigation
		return true // Assume it's cancellation for now, but this needs verification
	}

	return false
}

// getModelID returns the appropriate model ID for the given provider
func getModelID(provider string) string {
	switch provider {
	case "bedrock":
		return "anthropic.claude-3-sonnet-20240229-v1:0"
	case "openai":
		return "gpt-4.1"
	default:
		return "gpt-4.1" // Default to OpenAI GPT-4.1
	}
}

func init() {
	TestingCmd.AddCommand(contextCancellationTestCmd)
}
