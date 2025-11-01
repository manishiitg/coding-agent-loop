package testing

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"mcp-agent/agent_go/internal/llm"
	"mcp-agent/agent_go/pkg/mcpclient"

	"mcp-agent/agent_go/internal/llmtypes"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var anthropicCmd = &cobra.Command{
	Use:   "anthropic",
	Short: "Test Anthropic (Claude) with API key, tool calling, and structured output",
	Run:   runAnthropic,
}

type anthropicTestFlags struct {
	model      string
	apiKey     string
	withTools  bool
	withGitHub bool
	structured bool
	configPath string
}

var anthropicFlags anthropicTestFlags

func init() {
	anthropicCmd.Flags().StringVar(&anthropicFlags.model, "model", "claude-haiku-4-5-20251001", "Claude model to test")
	anthropicCmd.Flags().StringVar(&anthropicFlags.apiKey, "api-key", "", "Anthropic API key (or set ANTHROPIC_API_KEY env var)")
	anthropicCmd.Flags().BoolVar(&anthropicFlags.withTools, "with-tools", false, "enable tool calling")
	anthropicCmd.Flags().BoolVar(&anthropicFlags.withGitHub, "with-github", false, "use GitHub MCP tools for testing")
	anthropicCmd.Flags().BoolVar(&anthropicFlags.structured, "structured", false, "test structured JSON output with JSON mode")
	anthropicCmd.Flags().StringVar(&anthropicFlags.configPath, "config", "configs/mcp_servers_clean_user.json", "MCP config file path")
}

func runAnthropic(cmd *cobra.Command, args []string) {
	// Load .env file if present
	if err := godotenv.Load("agent_go/.env"); err == nil {
		// Environment loaded successfully
	} else if err := godotenv.Load(".env"); err == nil {
		// Environment loaded successfully
	} else if err := godotenv.Load("../.env"); err == nil {
		// Environment loaded successfully
	}
	// Note: If .env file not found, continue with system environment variables

	logFile := viper.GetString("log-file")
	logLevel := viper.GetString("log-level")
	InitTestLogger(logFile, logLevel)
	logger := GetTestLogger()

	// Get API key from environment or flag
	apiKey := anthropicFlags.apiKey
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	if apiKey == "" {
		log.Fatal("API key required: set --api-key flag or ANTHROPIC_API_KEY environment variable")
	}

	// Set API key as environment variable for internal LLM provider to pick up
	os.Setenv("ANTHROPIC_API_KEY", apiKey)

	ctx := context.Background()

	testType := "plain generation"
	if anthropicFlags.withTools {
		testType = "tool calling"
	} else if anthropicFlags.structured {
		testType = "structured output"
	}
	logger.Info(fmt.Sprintf("🚀 Testing Anthropic Claude (%s)", testType))

	// Set default model if not specified
	modelID := anthropicFlags.model
	if modelID == "" {
		modelID = "claude-haiku-4-5-20251001"
	}

	// Initialize Anthropic LLM using internal provider
	llmInstance, err := llm.InitializeLLM(llm.Config{
		Provider:    llm.ProviderAnthropic,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
	})
	if err != nil {
		log.Fatalf("Failed to initialize Anthropic LLM: %v", err)
	}

	var tools []llmtypes.Tool
	var messages []llmtypes.MessageContent

	if anthropicFlags.structured {
		// Test structured output with JSON mode
		logger.Info("📋 Setting up structured output test with JSON mode...")

		messages = []llmtypes.MessageContent{
			llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "List a few popular cookie recipes, and include the amounts of ingredients. Return as a JSON array where each recipe has 'recipeName' (string) and 'ingredients' (array of strings)."),
		}

		logger.Info("✅ Structured output test configured")
		logger.Info("   Schema: JSON array of objects with recipeName (string) and ingredients (array of strings)")
	} else if anthropicFlags.withGitHub {
		// Load GitHub MCP tools
		logger.Info("🔗 Connecting to GitHub MCP server...")
		config, err := mcpclient.LoadMergedConfig(anthropicFlags.configPath, logger)
		if err != nil {
			log.Fatalf("Failed to load MCP config: %v", err)
		}

		githubConfig, err := config.GetServer("github")
		if err != nil {
			log.Fatalf("GitHub server not found in config: %v", err)
		}

		// Create client and connect
		client := mcpclient.New(githubConfig, logger)
		if err := client.Connect(ctx); err != nil {
			log.Fatalf("Failed to connect to GitHub MCP: %v", err)
		}
		defer client.Close()

		// List tools
		mcpTools, err := client.ListTools(ctx)
		if err != nil {
			log.Fatalf("Failed to list GitHub tools: %v", err)
		}

		logger.Info(fmt.Sprintf("✅ Loaded %d tools from GitHub MCP", len(mcpTools)))

		// Convert to LLM tools
		llmTools, err := mcpclient.ToolsAsLLM(mcpTools)
		if err != nil {
			log.Fatalf("Failed to convert tools: %v", err)
		}

		tools = llmTools

		logger.Info(fmt.Sprintf("✅ Converted %d tools for Anthropic", len(tools)))
		messages = []llmtypes.MessageContent{
			llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "List my GitHub repositories"),
		}
	} else if anthropicFlags.withTools {
		// Define a simple weather tool
		weatherTool := llmtypes.Tool{
			Type: "function",
			Function: &llmtypes.FunctionDefinition{
				Name:        "get_weather",
				Description: "Get current weather for a location",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"location": map[string]any{
							"type":        "string",
							"description": "City name",
						},
					},
					"required": []string{"location"},
				},
			},
		}
		tools = []llmtypes.Tool{weatherTool}
		messages = []llmtypes.MessageContent{
			llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "What's the weather in Tokyo?"),
		}
	} else {
		messages = []llmtypes.MessageContent{
			llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "Hello! Can you introduce yourself?"),
		}
	}

	// Call with or without tools
	var resp *llmtypes.ContentResponse
	if len(tools) > 0 {
		logger.Info(fmt.Sprintf("📤 Sending %d tools to Claude...", len(tools)))

		// DEBUG: Marshal tools to JSON to see what's actually being sent
		toolsJSON, _ := json.MarshalIndent(tools, "", "  ")
		jsonLen := len(toolsJSON)
		if jsonLen > 2000 {
			jsonLen = 2000
		}
		logger.Info(fmt.Sprintf("🔍 Tools JSON structure (first 2000 chars):\n%s", string(toolsJSON[:jsonLen])))

		resp, err = llmInstance.GenerateContent(ctx, messages,
			llmtypes.WithModel(modelID),
			llmtypes.WithTools(tools))
	} else {
		// For structured output, enable JSON mode
		if anthropicFlags.structured {
			resp, err = llmInstance.GenerateContent(ctx, messages,
				llmtypes.WithModel(modelID),
				llmtypes.WithJSONMode())
		} else {
			resp, err = llmInstance.GenerateContent(ctx, messages, llmtypes.WithModel(modelID))
		}
	}

	if err != nil {
		log.Fatalf("❌ Error: %v", err)
	}

	if len(resp.Choices) == 0 {
		log.Fatal("❌ No choices returned")
	}

	choice := resp.Choices[0]

	// Display token usage if available
	if choice.GenerationInfo != nil {
		logger.Info("📊 Token Usage:")
		if inputTokens, ok := choice.GenerationInfo["input_tokens"]; ok {
			logger.Info(fmt.Sprintf("   Input tokens: %v", inputTokens))
		}
		if outputTokens, ok := choice.GenerationInfo["output_tokens"]; ok {
			logger.Info(fmt.Sprintf("   Output tokens: %v", outputTokens))
		}
		if totalTokens, ok := choice.GenerationInfo["total_tokens"]; ok {
			logger.Info(fmt.Sprintf("   Total tokens: %v", totalTokens))
		}
		// Check for cache tokens
		if cacheRead, ok := choice.GenerationInfo["cache_read_input_tokens"]; ok {
			logger.Info(fmt.Sprintf("   Cache read tokens: %v", cacheRead))
		}
		if cacheCreate, ok := choice.GenerationInfo["cache_creation_input_tokens"]; ok {
			logger.Info(fmt.Sprintf("   Cache creation tokens: %v", cacheCreate))
		}
	}

	// Check for tool calls
	if len(choice.ToolCalls) > 0 {
		logger.Info(fmt.Sprintf("✅ Success! Detected %d tool call(s)", len(choice.ToolCalls)))
		for i, toolCall := range choice.ToolCalls {
			logger.Info(fmt.Sprintf("🔧 Tool #%d", i+1), map[string]interface{}{
				"name":      toolCall.FunctionCall.Name,
				"arguments": toolCall.FunctionCall.Arguments,
			})
		}
	} else if len(choice.Content) > 0 {
		if anthropicFlags.structured {
			// Validate structured output
			logger.Info("📋 Validating structured JSON output...")

			// Try to parse as JSON - first try array, then object with array property
			var recipes []map[string]interface{}
			if err := json.Unmarshal([]byte(choice.Content), &recipes); err != nil {
				// Try parsing as object with "cookies" or similar property
				var obj map[string]interface{}
				if err2 := json.Unmarshal([]byte(choice.Content), &obj); err2 == nil {
					// Look for array properties
					for key, value := range obj {
						if arr, ok := value.([]interface{}); ok {
							recipes = make([]map[string]interface{}, 0, len(arr))
							for _, item := range arr {
								if recipe, ok := item.(map[string]interface{}); ok {
									recipes = append(recipes, recipe)
								}
							}
							logger.Info(fmt.Sprintf("   Found %d recipes in '%s' property", len(recipes), key))
							break
						}
					}
				}
				if len(recipes) == 0 {
					logger.Warn(fmt.Sprintf("⚠️ Response is not valid JSON array: %v", err))
					logger.Info("Response content (first 500 chars):", map[string]interface{}{
						"content_preview": func() string {
							if len(choice.Content) > 500 {
								return choice.Content[:500] + "..."
							}
							return choice.Content
						}(),
					})
				}
			}
			if len(recipes) > 0 {
				logger.Info(fmt.Sprintf("✅ Valid JSON array with %d recipe(s)", len(recipes)))

				// Validate structure
				for i, recipe := range recipes {
					hasRecipeName := false
					hasIngredients := false

					if name, ok := recipe["recipeName"]; ok && name != nil {
						hasRecipeName = true
						logger.Info(fmt.Sprintf("   Recipe %d: %s", i+1, name))
					}

					if ingredients, ok := recipe["ingredients"]; ok && ingredients != nil {
						if ingArray, ok := ingredients.([]interface{}); ok {
							hasIngredients = true
							logger.Info(fmt.Sprintf("      Ingredients (%d): %v", len(ingArray), ingArray))
						}
					}

					if !hasRecipeName {
						logger.Warn(fmt.Sprintf("   ⚠️ Recipe %d missing 'recipeName' field", i+1))
					}
					if !hasIngredients {
						logger.Warn(fmt.Sprintf("   ⚠️ Recipe %d missing 'ingredients' field", i+1))
					}
				}

				// Pretty print the full JSON response
				prettyJSON, _ := json.MarshalIndent(recipes, "", "  ")
				logger.Info("📄 Full structured response:")
				fmt.Println(string(prettyJSON))
			}
		} else {
			logger.Info("✅ Success! Response received", map[string]interface{}{
				"content": choice.Content,
			})
		}
	}
}
