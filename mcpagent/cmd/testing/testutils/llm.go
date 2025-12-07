package testutils

import (
	"fmt"

	"mcpagent/llm"
	loggerv2 "mcpagent/logger/v2"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"github.com/spf13/viper"
)

// TestLLMConfig holds configuration for test LLM initialization
type TestLLMConfig struct {
	Provider    string  // LLM provider (bedrock, openai, anthropic, etc.)
	ModelID     string  // Model ID (optional, will use default if empty)
	Temperature float64 // Temperature (optional, defaults to 0.2)
	Logger      loggerv2.Logger
}

// CreateTestLLM creates a test LLM instance with the specified configuration.
// If config is nil or empty, it uses viper to get configuration from flags.
func CreateTestLLM(cfg *TestLLMConfig) (llmtypes.Model, error) {
	// Use viper if config is not provided
	if cfg == nil {
		cfg = &TestLLMConfig{}
	}

	// Get provider from config or viper
	provider := cfg.Provider
	if provider == "" {
		// Try both "provider" and "test.provider" (flag binding)
		provider = viper.GetString("provider")
		if provider == "" {
			provider = viper.GetString("test.provider")
		}
		if provider == "" {
			provider = "openai" // Default provider
		}
	}

	// Validate provider
	llmProvider, err := llm.ValidateProvider(provider)
	if err != nil {
		return nil, fmt.Errorf("invalid LLM provider %s: %w", provider, err)
	}

	// Get model ID
	modelID := cfg.ModelID
	if modelID == "" {
		modelID = llm.GetDefaultModel(llmProvider)
		if modelID == "" {
			return nil, fmt.Errorf("no default model available for provider: %s", provider)
		}
	}

	// Get temperature
	temperature := cfg.Temperature
	if temperature == 0 {
		temperature = 0.2 // Default temperature
	}

	// Get logger
	logger := cfg.Logger
	if logger == nil {
		logger = loggerv2.NewNoop()
	}

	// Create LLM config
	llmConfig := llm.Config{
		Provider:    llmProvider,
		ModelID:     modelID,
		Temperature: temperature,
		Logger:      logger,
	}

	// Initialize LLM
	model, err := llm.InitializeLLM(llmConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize LLM: %w", err)
	}

	return model, nil
}

// CreateTestLLMFromViper creates a test LLM using viper configuration.
// This is a convenience function that calls CreateTestLLM(nil).
func CreateTestLLMFromViper(logger loggerv2.Logger) (llmtypes.Model, error) {
	return CreateTestLLM(&TestLLMConfig{
		Logger: logger,
	})
}
