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

	"llm-providers/llmtypes"
	"mcp-agent/agent_go/internal/llm"
)

var genaiMultiToolComplexTestCmd = &cobra.Command{
	Use:   "genai-multi-tool-complex",
	Short: "Test Gemini 3 Pro with complex multi-tool scenarios",
	Long: `Test Gemini 3 Pro with complex scenarios involving multiple tools in sequence.
This test specifically validates thought signature handling across multiple tool calls.

Examples:
  go run main.go test genai-multi-tool-complex --model gemini-3-pro-preview`,
	Run: runGenAIMultiToolComplexTest,
}

type genaiMultiToolComplexTestFlags struct {
	model    string
	maxTurns int
	verbose  bool
}

var genaiMultiToolComplexFlags genaiMultiToolComplexTestFlags

func init() {
	genaiMultiToolComplexTestCmd.Flags().StringVar(&genaiMultiToolComplexFlags.model, "model", "gemini-3-pro-preview", "Gemini model to test")
	genaiMultiToolComplexTestCmd.Flags().IntVar(&genaiMultiToolComplexFlags.maxTurns, "max-turns", 10, "Maximum conversation turns")
	genaiMultiToolComplexTestCmd.Flags().BoolVar(&genaiMultiToolComplexFlags.verbose, "verbose", false, "Enable verbose logging")
	TestingCmd.AddCommand(genaiMultiToolComplexTestCmd)
}

func runGenAIMultiToolComplexTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	modelID := genaiMultiToolComplexFlags.model
	maxTurns := genaiMultiToolComplexFlags.maxTurns
	verbose := genaiMultiToolComplexFlags.verbose

	log.Printf("🚀 Testing Gemini 3 Pro Complex Multi-Tool Scenarios with %s", modelID)
	log.Printf("   Max Turns: %d", maxTurns)

	// Check for API key
	apiKey := os.Getenv("VERTEX_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("GOOGLE_API_KEY")
	}
	if apiKey == "" {
		log.Printf("❌ VERTEX_API_KEY or GOOGLE_API_KEY environment variable is required")
		return
	}
	os.Setenv("VERTEX_API_KEY", apiKey)
	os.Unsetenv("GOOGLE_GENAI_USE_VERTEXAI")
	os.Unsetenv("GOOGLE_CLOUD_PROJECT")
	os.Unsetenv("VERTEX_PROJECT_ID")

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

	// Define multiple tools for complex scenarios
	tools := []llmtypes.Tool{
		{
			Type: "function",
			Function: &llmtypes.FunctionDefinition{
				Name:        "get_weather",
				Description: "Get current weather information for a location",
				Parameters: llmtypes.NewParameters(map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"location": map[string]interface{}{
							"type":        "string",
							"description": "City or location name",
						},
					},
					"required": []string{"location"},
				}),
			},
		},
		{
			Type: "function",
			Function: &llmtypes.FunctionDefinition{
				Name:        "calculate_math",
				Description: "Perform basic arithmetic calculations",
				Parameters: llmtypes.NewParameters(map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"expression": map[string]interface{}{
							"type":        "string",
							"description": "Mathematical expression to evaluate (e.g., '15*23')",
						},
					},
					"required": []string{"expression"},
				}),
			},
		},
		{
			Type: "function",
			Function: &llmtypes.FunctionDefinition{
				Name:        "search_knowledge",
				Description: "Search for information on a given topic",
				Parameters: llmtypes.NewParameters(map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"query": map[string]interface{}{
							"type":        "string",
							"description": "Search query",
						},
					},
					"required": []string{"query"},
				}),
			},
		},
		{
			Type: "function",
			Function: &llmtypes.FunctionDefinition{
				Name:        "get_stock_price",
				Description: "Get current stock price for a ticker symbol",
				Parameters: llmtypes.NewParameters(map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"ticker": map[string]interface{}{
							"type":        "string",
							"description": "Stock ticker symbol (e.g., 'AAPL', 'GOOGL')",
						},
					},
					"required": []string{"ticker"},
				}),
			},
		},
		{
			Type: "function",
			Function: &llmtypes.FunctionDefinition{
				Name:        "convert_currency",
				Description: "Convert amount from one currency to another",
				Parameters: llmtypes.NewParameters(map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"amount": map[string]interface{}{
							"type":        "number",
							"description": "Amount to convert",
						},
						"from": map[string]interface{}{
							"type":        "string",
							"description": "Source currency code (e.g., 'USD', 'EUR')",
						},
						"to": map[string]interface{}{
							"type":        "string",
							"description": "Target currency code (e.g., 'USD', 'EUR')",
						},
					},
					"required": []string{"amount", "from", "to"},
				}),
			},
		},
	}

	log.Printf("\n" + strings.Repeat("=", 80))
	log.Printf("🧪 COMPLEX TEST: Multi-Tool Sequential Execution with Thought Signatures")
	log.Printf(strings.Repeat("=", 80) + "\n")

	ctx := context.Background()
	messages := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman,
			"I need you to: 1) Get weather for Paris, 2) Calculate 125 * 8, 3) Search for information about machine learning, 4) Get stock price for AAPL, and 5) Convert 1000 USD to EUR. Please execute all these tools in sequence."),
	}

	log.Printf("📝 User: I need you to: 1) Get weather for Paris, 2) Calculate 125 * 8, 3) Search for information about machine learning, 4) Get stock price for AAPL, and 5) Convert 1000 USD to EUR. Please execute all these tools in sequence.")

	var totalTokens int
	startTime := time.Now()
	thoughtSignatureCount := 0
	toolCallCount := 0

	for turn := 0; turn < maxTurns; turn++ {
		if verbose {
			log.Printf("\n--- Turn %d ---", turn+1)
		}

		resp, err := genaiLLM.GenerateContent(ctx, messages, llmtypes.WithTools(tools), llmtypes.WithToolChoiceString("auto"))
		if err != nil {
			log.Printf("❌ Turn %d: Error: %v", turn+1, err)
			if strings.Contains(err.Error(), "thought_signature") {
				log.Printf("❌❌❌ THOUGHT SIGNATURE ERROR DETECTED! This means thought signatures are not being passed back correctly.")
			}
			return
		}

		if len(resp.Choices) == 0 {
			log.Printf("❌ Turn %d: No response", turn+1)
			return
		}

		choice := resp.Choices[0]

		// Track token usage
		if choice.GenerationInfo != nil {
			var input, output int
			if choice.GenerationInfo.InputTokens != nil {
				input = *choice.GenerationInfo.InputTokens
			}
			if choice.GenerationInfo.OutputTokens != nil {
				output = *choice.GenerationInfo.OutputTokens
			}
			if input > 0 || output > 0 {
				totalTokens += input + output
			}
		}

		if len(choice.ToolCalls) > 0 {
			toolCallCount += len(choice.ToolCalls)
			log.Printf("🔧 Turn %d: LLM made %d tool call(s):", turn+1, len(choice.ToolCalls))

			// Append assistant message with tool calls
			assistantParts := []llmtypes.ContentPart{}
			if choice.Content != "" {
				assistantParts = append(assistantParts, llmtypes.TextContent{Text: choice.Content})
			}
			for i, tc := range choice.ToolCalls {
				assistantParts = append(assistantParts, tc)
				// Check and log thought signature status
				if tc.ThoughtSignature != "" {
					thoughtSignatureCount++
					log.Printf("   ✅ [%d] Tool: %s (HAS thought signature, length: %d)", i+1, tc.FunctionCall.Name, len(tc.ThoughtSignature))
				} else {
					log.Printf("   ⚠️  [%d] Tool: %s (MISSING thought signature)", i+1, tc.FunctionCall.Name)
				}
				if verbose {
					log.Printf("       Args: %s", tc.FunctionCall.Arguments)
				}
			}
			messages = append(messages, llmtypes.MessageContent{
				Role:  llmtypes.ChatMessageTypeAI,
				Parts: assistantParts,
			})

			// Execute tools and append results
			for i, tc := range choice.ToolCalls {
				result := executeMockToolComplex(tc.FunctionCall.Name, tc.FunctionCall.Arguments)
				if verbose {
					log.Printf("   [%d] Result: %s", i+1, truncateString(result, 100))
				}

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

			// Calculate coverage safely, avoiding division by zero
			var coverage float64
			if toolCallCount > 0 {
				coverage = float64(thoughtSignatureCount) / float64(toolCallCount) * 100
			} else {
				coverage = 0.0
			}

			log.Printf("\n📊 Test Summary:")
			log.Printf("   Total Turns: %d", turn+1)
			log.Printf("   Total Tool Calls: %d", toolCallCount)
			log.Printf("   Tool Calls with Thought Signatures: %d", thoughtSignatureCount)
			log.Printf("   Thought Signature Coverage: %.1f%%", coverage)
			log.Printf("   Duration: %v", duration)
			log.Printf("   Total Tokens: %d", totalTokens)
			if thoughtSignatureCount == toolCallCount && toolCallCount > 0 {
				log.Printf("\n✅✅✅ SUCCESS: All tool calls had thought signatures!")
			} else if toolCallCount > 0 {
				log.Printf("\n⚠️  WARNING: Some tool calls were missing thought signatures")
			}
			return
		}
	}

	log.Printf("⚠️  Reached max turns (%d) without completion", maxTurns)
}

// executeMockToolComplex executes mock tools for complex test scenarios
func executeMockToolComplex(toolName string, argumentsJSON string) string {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
		return fmt.Sprintf("Error parsing arguments: %v", err)
	}

	switch toolName {
	case "get_weather":
		location := args["location"].(string)
		weatherMap := map[string]string{
			"Paris":         "Weather in Paris: Cloudy, 65°F, light breeze",
			"New York":      "Weather in New York: Sunny, 72°F, light breeze",
			"Tokyo":         "Weather in Tokyo: Partly cloudy, 68°F, calm",
			"San Francisco": "Weather in San Francisco: Foggy, 58°F, light wind",
			"Seattle":       "Weather in Seattle: Rainy, 55°F, moderate wind",
		}
		if weather, ok := weatherMap[location]; ok {
			return weather
		}
		return fmt.Sprintf("Weather in %s: Clear, 70°F, calm", location)
	case "calculate_math":
		expr := args["expression"].(string)
		if strings.Contains(expr, "125*8") || strings.Contains(expr, "125 * 8") {
			return "1000"
		}
		if strings.Contains(expr, "15*23") || strings.Contains(expr, "15 * 23") {
			return "345"
		}
		return fmt.Sprintf("Calculation result for %s: [mock result]", expr)
	case "search_knowledge":
		query := args["query"].(string)
		if strings.Contains(strings.ToLower(query), "machine learning") {
			return "Machine learning is a subset of artificial intelligence that enables systems to learn and improve from experience without being explicitly programmed. It uses algorithms to analyze data, identify patterns, and make predictions or decisions."
		}
		return fmt.Sprintf("Information about %s: [general knowledge]", query)
	case "get_stock_price":
		ticker := args["ticker"].(string)
		stockPrices := map[string]string{
			"AAPL":  "$175.50",
			"GOOGL": "$142.30",
			"MSFT":  "$378.90",
		}
		if price, ok := stockPrices[ticker]; ok {
			return fmt.Sprintf("Stock price for %s: %s", ticker, price)
		}
		return fmt.Sprintf("Stock price for %s: $100.00", ticker)
	case "convert_currency":
		amount := args["amount"].(float64)
		from := args["from"].(string)
		to := args["to"].(string)
		if from == "USD" && to == "EUR" {
			return fmt.Sprintf("Converted %.2f %s to %.2f %s (rate: 0.92)", amount, from, amount*0.92, to)
		}
		return fmt.Sprintf("Converted %.2f %s to %.2f %s", amount, from, amount, to)
	default:
		return fmt.Sprintf("Mock result for %s with args: %s", toolName, argumentsJSON)
	}
}
