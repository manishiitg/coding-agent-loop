package main

import (
	"os"

	"github.com/spf13/cobra"

	anthropiccmd "github.com/manishiitg/multi-llm-provider-go/internal/testing/commands/anthropic"
	azurecmd "github.com/manishiitg/multi-llm-provider-go/internal/testing/commands/azure"
	bedrockcmd "github.com/manishiitg/multi-llm-provider-go/internal/testing/commands/bedrock"
	openaicmd "github.com/manishiitg/multi-llm-provider-go/internal/testing/commands/openai"
	openroutercmd "github.com/manishiitg/multi-llm-provider-go/internal/testing/commands/openrouter"
	sharedcmd "github.com/manishiitg/multi-llm-provider-go/internal/testing/commands/shared"
	vertexcmd "github.com/manishiitg/multi-llm-provider-go/internal/testing/commands/vertex"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "llm-test",
		Short: "LLM Provider Testing Tool",
		Long:  "Test tool for llm-providers module",
	}

	// Register all test commands
	rootCmd.AddCommand(bedrockcmd.BedrockCmd)
	rootCmd.AddCommand(bedrockcmd.LLMToolCallTestCmd)
	rootCmd.AddCommand(bedrockcmd.BedrockToolCallEventsTestCmd)
	rootCmd.AddCommand(bedrockcmd.BedrockStreamingContentTestCmd)
	rootCmd.AddCommand(bedrockcmd.BedrockStreamingMixedTestCmd)
	rootCmd.AddCommand(bedrockcmd.BedrockStreamingParallelTestCmd)
	rootCmd.AddCommand(bedrockcmd.BedrockStreamingFuncTestCmd)
	rootCmd.AddCommand(bedrockcmd.BedrockStreamingCancellationTestCmd)
	rootCmd.AddCommand(bedrockcmd.BedrockStreamingMultiTurnTestCmd)
	rootCmd.AddCommand(bedrockcmd.BedrockStreamingToolCallHistoryTestCmd)
	rootCmd.AddCommand(bedrockcmd.BedrockStructuredOutputTestCmd)
	rootCmd.AddCommand(bedrockcmd.BedrockTokenUsageTestCmd)
	rootCmd.AddCommand(bedrockcmd.BedrockImageTestCmd)
	rootCmd.AddCommand(bedrockcmd.BedrockEmbeddingTestCmd)
	rootCmd.AddCommand(openaicmd.OpenAICmd)
	rootCmd.AddCommand(openaicmd.OpenAIToolCallTestCmd)
	rootCmd.AddCommand(openaicmd.OpenAIToolCallEventsTestCmd)
	rootCmd.AddCommand(openaicmd.OpenAIStreamingToolCallTestCmd)
	rootCmd.AddCommand(openaicmd.OpenAIStreamingContentTestCmd)
	rootCmd.AddCommand(openaicmd.OpenAIStreamingMixedTestCmd)
	rootCmd.AddCommand(openaicmd.OpenAIStreamingParallelTestCmd)
	rootCmd.AddCommand(openaicmd.OpenAIStreamingFuncTestCmd)
	rootCmd.AddCommand(openaicmd.OpenAIStreamingCancellationTestCmd)
	rootCmd.AddCommand(openaicmd.OpenAIStreamingMultiTurnTestCmd)
	rootCmd.AddCommand(openaicmd.OpenAIStructuredOutputTestCmd)
	rootCmd.AddCommand(openaicmd.OpenAITokenUsageTestCmd)
	rootCmd.AddCommand(openaicmd.OpenAIImageTestCmd)
	rootCmd.AddCommand(openaicmd.OpenAIEmbeddingTestCmd)
	rootCmd.AddCommand(anthropiccmd.AnthropicCmd)
	rootCmd.AddCommand(anthropiccmd.AnthropicToolCallTestCmd)
	rootCmd.AddCommand(anthropiccmd.AnthropicToolCallEventsTestCmd)
	rootCmd.AddCommand(anthropiccmd.AnthropicStreamingContentTestCmd)
	rootCmd.AddCommand(anthropiccmd.AnthropicStreamingMixedTestCmd)
	rootCmd.AddCommand(anthropiccmd.AnthropicStreamingParallelTestCmd)
	rootCmd.AddCommand(anthropiccmd.AnthropicStreamingFuncTestCmd)
	rootCmd.AddCommand(anthropiccmd.AnthropicStreamingCancellationTestCmd)
	rootCmd.AddCommand(anthropiccmd.AnthropicStreamingMultiTurnTestCmd)
	rootCmd.AddCommand(anthropiccmd.AnthropicStructuredOutputTestCmd)
	rootCmd.AddCommand(anthropiccmd.AnthropicTokenUsageTestCmd)
	rootCmd.AddCommand(anthropiccmd.AnthropicImageTestCmd)
	rootCmd.AddCommand(openroutercmd.OpenRouterCmd)
	rootCmd.AddCommand(openroutercmd.OpenRouterToolCallTestCmd)
	rootCmd.AddCommand(openroutercmd.OpenRouterToolCallEventsTestCmd)
	rootCmd.AddCommand(openroutercmd.OpenRouterStructuredOutputTestCmd)
	rootCmd.AddCommand(openroutercmd.OpenRouterTokenUsageTestCmd)
	rootCmd.AddCommand(openroutercmd.OpenRouterImageTestCmd)
	rootCmd.AddCommand(openroutercmd.OpenRouterModelMetadataTestCmd)
	rootCmd.AddCommand(vertexcmd.VertexCmd)
	rootCmd.AddCommand(vertexcmd.VertexAnthropicCmd)
	rootCmd.AddCommand(vertexcmd.VertexToolCallTestCmd)
	rootCmd.AddCommand(vertexcmd.VertexToolCallEventsTestCmd)
	rootCmd.AddCommand(vertexcmd.VertexStreamingContentTestCmd)
	rootCmd.AddCommand(vertexcmd.VertexStreamingMixedTestCmd)
	rootCmd.AddCommand(vertexcmd.VertexStreamingMultiTurnTestCmd)
	rootCmd.AddCommand(vertexcmd.VertexStreamingCancellationTestCmd)
	rootCmd.AddCommand(vertexcmd.VertexStructuredOutputTestCmd)
	rootCmd.AddCommand(vertexcmd.VertexTokenUsageTestCmd)
	rootCmd.AddCommand(vertexcmd.VertexImageTestCmd)
	rootCmd.AddCommand(vertexcmd.VertexEmbeddingTestCmd)
	rootCmd.AddCommand(vertexcmd.VertexGeminiThinkingLevelTestCmd)
	rootCmd.AddCommand(vertexcmd.VertexNestedArrayTestCmd)
	rootCmd.AddCommand(vertexcmd.VertexSchemaValidationTestCmd)
	rootCmd.AddCommand(azurecmd.AzureCmd)
	rootCmd.AddCommand(azurecmd.AzureToolCallTestCmd)
	rootCmd.AddCommand(azurecmd.AzureToolCallEventsTestCmd)
	rootCmd.AddCommand(azurecmd.AzureStructuredOutputTestCmd)
	rootCmd.AddCommand(azurecmd.AzureImageTestCmd)
	rootCmd.AddCommand(azurecmd.AzureTokenUsageTestCmd)
	rootCmd.AddCommand(azurecmd.AzureStreamingContentTestCmd)
	rootCmd.AddCommand(azurecmd.AzureStreamingMixedTestCmd)
	rootCmd.AddCommand(azurecmd.AzureStreamingParallelTestCmd)
	rootCmd.AddCommand(azurecmd.AzureStreamingFuncTestCmd)
	rootCmd.AddCommand(azurecmd.AzureStreamingMultiTurnTestCmd)
	rootCmd.AddCommand(azurecmd.AzureStreamingCancellationTestCmd)
	rootCmd.AddCommand(azurecmd.AzureModelMetadataTestCmd)
	rootCmd.AddCommand(azurecmd.AzureResponsesTestCmd)
	rootCmd.AddCommand(sharedcmd.TokenUsageTestCmd)
	rootCmd.AddCommand(sharedcmd.TestSuiteCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
