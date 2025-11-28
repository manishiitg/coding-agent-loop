package external

import (
	"mcpagent/llm"
	"llm-providers/llmtypes"
	"mcp-agent/agent_go/internal/utils"
)

// initializeLLM creates and configures an LLM based on the provider
func initializeLLM(provider llm.Provider, modelID string, temperature float64, logger utils.ExtendedLogger) (llmtypes.Model, error) {
	// Use the internal llm package Config structure
	config := llm.Config{
		Provider:    provider,
		ModelID:     modelID,
		Temperature: temperature,
		Logger:      logger,
	}
	return llm.InitializeLLM(config)
}
