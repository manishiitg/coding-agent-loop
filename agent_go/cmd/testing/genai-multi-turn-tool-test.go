package testing

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"

	"mcp-agent/agent_go/internal/llm"
	"mcp-agent/agent_go/internal/llmtypes"
)

var genaiMultiTurnToolTestCmd = &cobra.Command{
	Use:   "genai-multi-turn-tool",
	Short: "Test Google GenAI multi-turn conversations with tool calling",
	Long: `Test Google GenAI multi-turn conversations with multiple tools.

This test simulates a real conversation flow where:
1. User asks a question requiring multiple tool calls
2. LLM makes tool calls
3. Tools are executed and results returned
4. LLM uses tool results to continue conversation
5. Multiple rounds of tool calls in sequence

Examples:
  go run main.go test genai-multi-turn-tool
  go run main.go test genai-multi-turn-tool --model gemini-2.5-flash`,
	Run: runGenAIMultiTurnToolTest,
}

type genaiMultiTurnToolTestFlags struct {
	model    string
	maxTurns int
	verbose  bool
}

var genaiMultiTurnToolFlags genaiMultiTurnToolTestFlags

func init() {
	genaiMultiTurnToolTestCmd.Flags().StringVar(&genaiMultiTurnToolFlags.model, "model", "gemini-2.5-flash", "Google GenAI model to test")
	genaiMultiTurnToolTestCmd.Flags().IntVar(&genaiMultiTurnToolFlags.maxTurns, "max-turns", 5, "Maximum conversation turns")
	genaiMultiTurnToolTestCmd.Flags().BoolVar(&genaiMultiTurnToolFlags.verbose, "verbose", false, "Enable verbose logging")
}

func runGenAIMultiTurnToolTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	modelID := genaiMultiTurnToolFlags.model
	maxTurns := genaiMultiTurnToolFlags.maxTurns
	verbose := genaiMultiTurnToolFlags.verbose

	log.Printf("🚀 Testing Google GenAI Multi-Turn Tool Calling with %s", modelID)
	log.Printf("   Max Turns: %d", maxTurns)

	// Check for API key
	if os.Getenv("VERTEX_API_KEY") == "" && os.Getenv("GOOGLE_API_KEY") == "" {
		log.Printf("❌ VERTEX_API_KEY or GOOGLE_API_KEY environment variable is required")
		return
	}

	// Create Google GenAI LLM using our adapter
	logger := GetTestLogger()
	genaiLLM, err := llm.InitializeLLM(llm.Config{
		Provider:    llm.ProviderVertex,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
	})
	if err != nil {
		log.Printf("❌ Failed to create Google GenAI LLM: %v", err)
		return
	}

	// Define multiple test tools (same as OpenAI test)
	tools := []llmtypes.Tool{
		{
			Type: "function",
			Function: &llmtypes.FunctionDefinition{
				Name:        "calculate_math",
				Description: "Perform basic arithmetic calculations",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"expression": map[string]interface{}{
							"type":        "string",
							"description": "Mathematical expression to evaluate (e.g., '15*23')",
						},
					},
					"required": []string{"expression"},
				},
			},
		},
		{
			Type: "function",
			Function: &llmtypes.FunctionDefinition{
				Name:        "get_weather",
				Description: "Get current weather information for a location",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"location": map[string]interface{}{
							"type":        "string",
							"description": "City or location name",
						},
					},
					"required": []string{"location"},
				},
			},
		},
		{
			Type: "function",
			Function: &llmtypes.FunctionDefinition{
				Name:        "search_knowledge",
				Description: "Search for information on a given topic",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"query": map[string]interface{}{
							"type":        "string",
							"description": "Search query",
						},
					},
					"required": []string{"query"},
				},
			},
		},
	}

	log.Printf("\n" + strings.Repeat("=", 80))
	log.Printf("🧪 TEST 1: Sequential Tool Calls")
	log.Printf(strings.Repeat("=", 80) + "\n")
	testSequentialToolCallsGenAI(genaiLLM, tools, maxTurns, verbose)

	log.Printf("\n" + strings.Repeat("=", 80))
	log.Printf("🧪 TEST 2: Parallel Tool Calls (Multiple Tools in One Turn)")
	log.Printf(strings.Repeat("=", 80) + "\n")
	testParallelToolCallsGenAI(genaiLLM, tools, maxTurns, verbose)

	log.Printf("\n" + strings.Repeat("=", 80))
	log.Printf("🧪 TEST 3: Multi-Step Reasoning with Tools")
	log.Printf(strings.Repeat("=", 80) + "\n")
	testMultiStepReasoningGenAI(genaiLLM, tools, maxTurns, verbose)

	log.Printf("\n🎯 All Google GenAI multi-turn tool calling tests completed!")
}

// executeMockTool executes a mock tool and returns a result
func executeMockToolGenAI(toolName string, argumentsJSON string) string {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
		return fmt.Sprintf("Error parsing arguments: %v", err)
	}

	switch toolName {
	case "calculate_math":
		expr := args["expression"].(string)
		// Simple mock calculation - just return the expression result as mock
		if strings.Contains(expr, "*") {
			parts := strings.Split(expr, "*")
			if len(parts) == 2 {
				return fmt.Sprintf("Calculation result for %s: [mock result]", expr)
			}
		}
		if strings.Contains(expr, "15*23") {
			return "345"
		}
		if strings.Contains(expr, "42 * 18") || strings.Contains(expr, "42*18") {
			return "Calculation result for 42 * 18: [mock result]"
		}
		if strings.Contains(expr, "500*3") || strings.Contains(expr, "500 * 3") {
			return "1500"
		}
		return fmt.Sprintf("Calculation result for %s: [mock result]", expr)
	case "get_weather":
		location := args["location"].(string)
		weatherMap := map[string]string{
			"New York":      "Weather in New York: Sunny, 72°F, light breeze",
			"Tokyo":         "Weather in Tokyo: Partly cloudy, 68°F, calm",
			"San Francisco": "Weather in San Francisco: Foggy, 58°F, light wind",
			"Seattle":       "Weather in Seattle: Rainy, 55°F, moderate wind",
		}
		if weather, ok := weatherMap[location]; ok {
			return weather
		}
		return fmt.Sprintf("Weather in %s: Clear, 70°F, calm", location)
	case "search_knowledge":
		query := args["query"].(string)
		if strings.Contains(strings.ToLower(query), "go") {
			return "Go (Golang) is a statically typed, compiled programming language designed at Google. It's known for its simplicity, concurrency support, and fast compilation."
		}
		if strings.Contains(strings.ToLower(query), "python") {
			return "Python is a high-level, interpreted programming language known for its simplicity and readability. It's widely used in web development, data science, and automation."
		}
		return fmt.Sprintf("Information about %s: [general knowledge]", query)
	default:
		return fmt.Sprintf("Mock result for %s with args: %s", toolName, argumentsJSON)
	}
}

// testSequentialToolCalls tests multiple tools called sequentially
func testSequentialToolCallsGenAI(llm llmtypes.Model, tools []llmtypes.Tool, maxTurns int, verbose bool) {
	ctx := context.Background()
	messages := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman,
			"First, calculate 15 * 23. Then, get the weather for New York. Finally, search for information about Go programming language."),
	}

	log.Printf("📝 User: First, calculate 15 * 23. Then, get the weather for New York. Finally, search for information about Go programming language.")

	var totalTokens int
	startTime := time.Now()

	for turn := 0; turn < maxTurns; turn++ {
		if verbose {
			log.Printf("\n--- Turn %d ---", turn+1)
		}

		resp, err := llm.GenerateContent(ctx, messages, llmtypes.WithTools(tools), llmtypes.WithToolChoiceString("auto"))
		if err != nil {
			log.Printf("❌ Turn %d: Error: %v", turn+1, err)
			return
		}

		if len(resp.Choices) == 0 {
			log.Printf("❌ Turn %d: No response", turn+1)
			return
		}

		choice := resp.Choices[0]

		// Track token usage
		if choice.GenerationInfo != nil {
			if input, ok := choice.GenerationInfo["input_tokens"].(int); ok {
				if output, ok := choice.GenerationInfo["output_tokens"].(int); ok {
					totalTokens += input + output
				}
			}
		}

		if len(choice.ToolCalls) > 0 {
			log.Printf("🔧 Turn %d: LLM made %d tool call(s):", turn+1, len(choice.ToolCalls))

			// Append assistant message
			assistantParts := []llmtypes.ContentPart{}
			if choice.Content != "" {
				assistantParts = append(assistantParts, llmtypes.TextContent{Text: choice.Content})
			}
			for _, tc := range choice.ToolCalls {
				assistantParts = append(assistantParts, tc)
			}
			messages = append(messages, llmtypes.MessageContent{
				Role:  llmtypes.ChatMessageTypeAI,
				Parts: assistantParts,
			})

			// Execute tools and append results
			for i, tc := range choice.ToolCalls {
				result := executeMockToolGenAI(tc.FunctionCall.Name, tc.FunctionCall.Arguments)
				log.Printf("   [%d] Tool: %s", i+1, tc.FunctionCall.Name)
				log.Printf("       Args: %s", tc.FunctionCall.Arguments)
				log.Printf("       Result: %s", truncateString(result, 100))

				// Append tool result to conversation
				messages = append(messages, llmtypes.MessageContent{
					Role: llmtypes.ChatMessageTypeTool,
					Parts: []llmtypes.ContentPart{
						llmtypes.ToolCallResponse{
							ToolCallID: tc.ID,
							Name:       tc.FunctionCall.Name,
							Content:    result,
						},
					},
				})
			}
			log.Printf("   Waiting for LLM to process tool results...\n")
		} else {
			// No tool calls - conversation complete
			log.Printf("\n✅ Turn %d: Final Response (no more tool calls)", turn+1)
			log.Printf("📝 Assistant: %s", choice.Content)
			duration := time.Since(startTime)
			log.Printf("\n📊 Test Summary:")
			log.Printf("   Total Turns: %d", turn+1)
			log.Printf("   Duration: %v", duration)
			log.Printf("   Total Tokens: %d", totalTokens)
			return
		}
	}

	log.Printf("⚠️  Reached max turns (%d) without completion", maxTurns)
}

// testParallelToolCalls tests multiple tools called in parallel
func testParallelToolCallsGenAI(llm llmtypes.Model, tools []llmtypes.Tool, maxTurns int, verbose bool) {
	ctx := context.Background()
	messages := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman,
			"Get the weather for Tokyo, calculate 42 * 18, and search for information about Python programming - all at once please."),
	}

	log.Printf("📝 User: Get the weather for Tokyo, calculate 42 * 18, and search for information about Python programming - all at once please.")

	var totalTokens int
	startTime := time.Now()

	for turn := 0; turn < maxTurns; turn++ {
		if verbose {
			log.Printf("\n--- Turn %d ---", turn+1)
		}

		resp, err := llm.GenerateContent(ctx, messages, llmtypes.WithTools(tools), llmtypes.WithToolChoiceString("auto"))
		if err != nil {
			log.Printf("❌ Turn %d: Error: %v", turn+1, err)
			return
		}

		if len(resp.Choices) == 0 {
			log.Printf("❌ Turn %d: No response", turn+1)
			return
		}

		choice := resp.Choices[0]

		// Track token usage
		if choice.GenerationInfo != nil {
			if input, ok := choice.GenerationInfo["input_tokens"].(int); ok {
				if output, ok := choice.GenerationInfo["output_tokens"].(int); ok {
					totalTokens += input + output
				}
			}
		}

		if len(choice.ToolCalls) > 0 {
			log.Printf("🔧 Turn %d: LLM made %d parallel tool call(s):", turn+1, len(choice.ToolCalls))

			// Append assistant message
			assistantParts := []llmtypes.ContentPart{}
			if choice.Content != "" {
				assistantParts = append(assistantParts, llmtypes.TextContent{Text: choice.Content})
			}
			for _, tc := range choice.ToolCalls {
				assistantParts = append(assistantParts, tc)
			}
			messages = append(messages, llmtypes.MessageContent{
				Role:  llmtypes.ChatMessageTypeAI,
				Parts: assistantParts,
			})

			// Execute all tools in parallel
			for i, tc := range choice.ToolCalls {
				result := executeMockToolGenAI(tc.FunctionCall.Name, tc.FunctionCall.Arguments)
				log.Printf("   [%d] Tool: %s", i+1, tc.FunctionCall.Name)
				log.Printf("       Args: %s", tc.FunctionCall.Arguments)
				log.Printf("       Result: %s", truncateString(result, 100))

				// Append tool result to conversation
				messages = append(messages, llmtypes.MessageContent{
					Role: llmtypes.ChatMessageTypeTool,
					Parts: []llmtypes.ContentPart{
						llmtypes.ToolCallResponse{
							ToolCallID: tc.ID,
							Name:       tc.FunctionCall.Name,
							Content:    result,
						},
					},
				})
			}
			log.Printf("   All tool results returned, waiting for LLM response...")
		} else {
			// No tool calls - conversation complete
			log.Printf("\n✅ Turn %d: Final Response", turn+1)
			log.Printf("📝 Assistant: %s", choice.Content)
			duration := time.Since(startTime)
			log.Printf("\n📊 Test Summary:")
			log.Printf("   Total Turns: %d", turn+1)
			log.Printf("   Duration: %v", duration)
			log.Printf("   Total Tokens: %d", totalTokens)
			return
		}
	}

	log.Printf("⚠️  Reached max turns (%d) without completion", maxTurns)
}

// testMultiStepReasoning tests multi-step reasoning using tool results
func testMultiStepReasoningGenAI(llm llmtypes.Model, tools []llmtypes.Tool, maxTurns int, verbose bool) {
	ctx := context.Background()
	messages := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman,
			"I need to plan a trip. First, check the weather in San Francisco and Seattle. Based on the weather, recommend which city is better for a trip. Then calculate the total cost if the trip costs $500 per day for 3 days."),
	}

	log.Printf("📝 User: I need to plan a trip. First, check the weather in San Francisco and Seattle. Based on the weather, recommend which city is better for a trip. Then calculate the total cost if the trip costs $500 per day for 3 days.")

	var totalTokens int
	startTime := time.Now()

	for turn := 0; turn < maxTurns; turn++ {
		if verbose {
			log.Printf("\n--- Turn %d ---", turn+1)
		}

		resp, err := llm.GenerateContent(ctx, messages, llmtypes.WithTools(tools), llmtypes.WithToolChoiceString("auto"))
		if err != nil {
			log.Printf("❌ Turn %d: Error: %v", turn+1, err)
			return
		}

		if len(resp.Choices) == 0 {
			log.Printf("❌ Turn %d: No response", turn+1)
			return
		}

		choice := resp.Choices[0]

		// Track token usage
		if choice.GenerationInfo != nil {
			if input, ok := choice.GenerationInfo["input_tokens"].(int); ok {
				if output, ok := choice.GenerationInfo["output_tokens"].(int); ok {
					totalTokens += input + output
				}
			}
		}

		if len(choice.ToolCalls) > 0 {
			log.Printf("🔧 Turn %d: LLM made %d tool call(s):", turn+1, len(choice.ToolCalls))

			// Append assistant message
			assistantParts := []llmtypes.ContentPart{}
			if choice.Content != "" {
				assistantParts = append(assistantParts, llmtypes.TextContent{Text: choice.Content})
			}
			for _, tc := range choice.ToolCalls {
				assistantParts = append(assistantParts, tc)
			}
			messages = append(messages, llmtypes.MessageContent{
				Role:  llmtypes.ChatMessageTypeAI,
				Parts: assistantParts,
			})

			// Execute tools and append results
			for i, tc := range choice.ToolCalls {
				result := executeMockToolGenAI(tc.FunctionCall.Name, tc.FunctionCall.Arguments)
				log.Printf("   [%d] Tool: %s", i+1, tc.FunctionCall.Name)
				log.Printf("       Args: %s", tc.FunctionCall.Arguments)
				log.Printf("       Result: %s", truncateString(result, 100))

				// Append tool result to conversation
				messages = append(messages, llmtypes.MessageContent{
					Role: llmtypes.ChatMessageTypeTool,
					Parts: []llmtypes.ContentPart{
						llmtypes.ToolCallResponse{
							ToolCallID: tc.ID,
							Name:       tc.FunctionCall.Name,
							Content:    result,
						},
					},
				})
			}
			log.Printf("   Waiting for LLM to analyze results and continue...")
		} else {
			// No tool calls - conversation complete
			log.Printf("\n✅ Turn %d: Final Response", turn+1)
			log.Printf("📝 Assistant: %s", choice.Content)
			duration := time.Since(startTime)
			log.Printf("\n📊 Test Summary:")
			log.Printf("   Total Turns: %d", turn+1)
			log.Printf("   Duration: %v", duration)
			log.Printf("   Total Tokens: %d", totalTokens)
			return
		}
	}

	log.Printf("⚠️  Reached max turns (%d) without completion", maxTurns)
}
