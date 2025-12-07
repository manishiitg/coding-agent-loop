package main

import (
	"os"

	"github.com/spf13/cobra"

	// Import command packages to access their command variables
	"llm-providers/internal/testing/commands/anthropic"
	"llm-providers/internal/testing/commands/bedrock"
	"llm-providers/internal/testing/commands/openai"
	"llm-providers/internal/testing/commands/openrouter"
	"llm-providers/internal/testing/commands/shared"
	"llm-providers/internal/testing/commands/vertex"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "llm-test",
		Short: "Test LLM providers with comprehensive test suites",
		Long:  "Test LLM providers (OpenAI, Anthropic, Bedrock, Vertex, OpenRouter) with standardized test suites",
	}

	// Register all commands from each provider package
	// Commands are exported variables from each package
	rootCmd.AddCommand(openai.OpenAICmd)
	rootCmd.AddCommand(anthropic.AnthropicCmd)
	rootCmd.AddCommand(bedrock.BedrockCmd)
	rootCmd.AddCommand(vertex.VertexCmd)
	rootCmd.AddCommand(openrouter.OpenRouterCmd)
	rootCmd.AddCommand(shared.TokenUsageTestCmd)

	// Add all test-specific commands
	// OpenRouter commands
	rootCmd.AddCommand(openrouter.OpenRouterToolCallTestCmd)
	rootCmd.AddCommand(openrouter.OpenRouterStructuredOutputTestCmd)
	rootCmd.AddCommand(openrouter.OpenRouterImageTestCmd)
	rootCmd.AddCommand(openrouter.OpenRouterTokenUsageTestCmd)
	rootCmd.AddCommand(openrouter.OpenRouterParallelToolResponseTestCmd)
	rootCmd.AddCommand(openrouter.OpenRouterMultiTurnTestCmd)
	// OpenAI commands
	rootCmd.AddCommand(openai.OpenAIToolCallTestCmd)
	rootCmd.AddCommand(openai.OpenAIStreamingToolCallTestCmd)
	rootCmd.AddCommand(openai.OpenAIStreamingContentTestCmd)
	rootCmd.AddCommand(openai.OpenAIStreamingMixedTestCmd)
	rootCmd.AddCommand(openai.OpenAIStreamingParallelTestCmd)
	rootCmd.AddCommand(openai.OpenAIStreamingFuncTestCmd)
	rootCmd.AddCommand(openai.OpenAIStreamingMultiTurnTestCmd)
	rootCmd.AddCommand(openai.OpenAIStreamingCancellationTestCmd)
	rootCmd.AddCommand(openai.OpenAIParallelToolResponseTestCmd)

	// Anthropic commands
	rootCmd.AddCommand(anthropic.AnthropicToolCallTestCmd)
	rootCmd.AddCommand(anthropic.AnthropicStreamingContentTestCmd)
	rootCmd.AddCommand(anthropic.AnthropicStreamingMixedTestCmd)
	rootCmd.AddCommand(anthropic.AnthropicStreamingParallelTestCmd)
	rootCmd.AddCommand(anthropic.AnthropicStreamingFuncTestCmd)
	rootCmd.AddCommand(anthropic.AnthropicStreamingMultiTurnTestCmd)
	rootCmd.AddCommand(anthropic.AnthropicStreamingCancellationTestCmd)
	rootCmd.AddCommand(anthropic.AnthropicParallelToolResponseTestCmd)

	// Bedrock commands
	rootCmd.AddCommand(bedrock.BedrockStreamingContentTestCmd)
	rootCmd.AddCommand(bedrock.BedrockStreamingMixedTestCmd)
	rootCmd.AddCommand(bedrock.BedrockStreamingParallelTestCmd)
	rootCmd.AddCommand(bedrock.BedrockStreamingFuncTestCmd)
	rootCmd.AddCommand(bedrock.BedrockStreamingMultiTurnTestCmd)
	rootCmd.AddCommand(bedrock.BedrockStreamingCancellationTestCmd)
	rootCmd.AddCommand(bedrock.BedrockStreamingToolCallHistoryTestCmd)
	rootCmd.AddCommand(bedrock.BedrockParallelToolResponseTestCmd)

	// Vertex commands
	rootCmd.AddCommand(vertex.VertexToolCallTestCmd)
	rootCmd.AddCommand(vertex.VertexStreamingContentTestCmd)
	rootCmd.AddCommand(vertex.VertexStreamingMixedTestCmd)
	rootCmd.AddCommand(vertex.VertexStreamingMultiTurnTestCmd)
	rootCmd.AddCommand(vertex.VertexStreamingCancellationTestCmd)
	rootCmd.AddCommand(vertex.VertexParallelToolResponseTestCmd)

	// Shared test commands
	rootCmd.AddCommand(shared.TypeConversionTestCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
