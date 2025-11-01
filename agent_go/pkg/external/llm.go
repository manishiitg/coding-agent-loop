package external

import (
	"mcp-agent/agent_go/internal/llm"
	"mcp-agent/agent_go/internal/llmtypes"
)

// initializeLLM creates and configures an LLM based on the provider
func initializeLLM(provider llm.Provider, modelID string, temperature float64) (llmtypes.Model, error) {
	// Use the internal llm package Config structure
	config := llm.Config{
		Provider:    provider,
		ModelID:     modelID,
		Temperature: temperature,
	}
	return llm.InitializeLLM(config)
}
