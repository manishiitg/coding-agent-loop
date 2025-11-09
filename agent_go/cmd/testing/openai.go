package testing

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"mcp-agent/agent_go/internal/llm"
	"mcp-agent/agent_go/pkg/mcpclient"

	"mcp-agent/agent_go/internal/llmtypes"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var openaiCmd = &cobra.Command{
	Use:   "openai",
	Short: "Test OpenAI (GPT) with API key, tool calling, structured output, and image input",
	Run:   runOpenAI,
}

type openaiTestFlags struct {
	model      string
	apiKey     string
	withTools  bool
	withGitHub bool
	structured bool
	configPath string
	imagePath  string
	imageURL   string
}

var openaiFlags openaiTestFlags

func init() {
	openaiCmd.Flags().StringVar(&openaiFlags.model, "model", "gpt-4o", "OpenAI model to test")
	openaiCmd.Flags().StringVar(&openaiFlags.apiKey, "api-key", "", "OpenAI API key (or set OPENAI_API_KEY env var)")
	openaiCmd.Flags().BoolVar(&openaiFlags.withTools, "with-tools", false, "enable tool calling")
	openaiCmd.Flags().BoolVar(&openaiFlags.withGitHub, "with-github", false, "use GitHub MCP tools for testing")
	openaiCmd.Flags().BoolVar(&openaiFlags.structured, "structured", false, "test structured JSON output with JSON mode")
	openaiCmd.Flags().StringVar(&openaiFlags.configPath, "config", "configs/mcp_servers_clean_user.json", "MCP config file path")
	openaiCmd.Flags().StringVar(&openaiFlags.imagePath, "with-image", "", "path to image file to test image input (JPEG, PNG, GIF, WebP)")
	openaiCmd.Flags().StringVar(&openaiFlags.imageURL, "image-url", "", "URL of image to test image input")
}

func runOpenAI(cmd *cobra.Command, args []string) {
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
	apiKey := openaiFlags.apiKey
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if apiKey == "" {
		log.Fatal("API key required: set --api-key flag or OPENAI_API_KEY environment variable")
	}

	// Set API key as environment variable for internal LLM provider to pick up
	os.Setenv("OPENAI_API_KEY", apiKey)

	ctx := context.Background()

	testType := "image input (default)"
	if openaiFlags.withTools {
		testType = "tool calling"
	} else if openaiFlags.structured {
		testType = "structured output"
	} else if openaiFlags.imagePath != "" || openaiFlags.imageURL != "" {
		testType = "image input"
	}
	logger.Info(fmt.Sprintf("🚀 Testing OpenAI GPT (%s)", testType))

	// Set default model if not specified
	modelID := openaiFlags.model
	if modelID == "" {
		modelID = "gpt-4o"
	}

	// Initialize OpenAI LLM using internal provider
	llmInstance, err := llm.InitializeLLM(llm.Config{
		Provider:    llm.ProviderOpenAI,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
	})
	if err != nil {
		log.Fatalf("Failed to initialize OpenAI LLM: %v", err)
	}

	var tools []llmtypes.Tool
	var messages []llmtypes.MessageContent

	if openaiFlags.structured {
		// Test structured output with JSON mode
		logger.Info("📋 Setting up structured output test with JSON mode...")

		messages = []llmtypes.MessageContent{
			llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "List a few popular cookie recipes, and include the amounts of ingredients. Return as a JSON array where each recipe has 'recipeName' (string) and 'ingredients' (array of strings)."),
		}

		logger.Info("✅ Structured output test configured")
		logger.Info("   Schema: JSON array of objects with recipeName (string) and ingredients (array of strings)")
	} else if openaiFlags.withGitHub {
		// Load GitHub MCP tools
		logger.Info("🔗 Connecting to GitHub MCP server...")
		config, err := mcpclient.LoadMergedConfig(openaiFlags.configPath, logger)
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

		logger.Info(fmt.Sprintf("✅ Converted %d tools for OpenAI", len(tools)))
		messages = []llmtypes.MessageContent{
			llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "List my GitHub repositories"),
		}
	} else if openaiFlags.withTools {
		// Define a simple weather tool
		weatherTool := llmtypes.Tool{
			Type: "function",
			Function: &llmtypes.FunctionDefinition{
				Name:        "get_weather",
				Description: "Get current weather for a location",
				Parameters: llmtypes.NewParameters(map[string]interface{}{
					"type": "object",
					"properties": map[string]any{
						"location": map[string]any{
							"type":        "string",
							"description": "City name",
						},
					},
					"required": []string{"location"},
				}),
			},
		}
		tools = []llmtypes.Tool{weatherTool}
		messages = []llmtypes.MessageContent{
			llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "What's the weather in Tokyo?"),
		}
	} else if openaiFlags.imagePath != "" || openaiFlags.imageURL != "" {
		// Test image input
		logger.Info("🖼️ Setting up image input test...")

		var imageParts []llmtypes.ContentPart

		if openaiFlags.imagePath != "" {
			// Load and encode image file
			logger.Info(fmt.Sprintf("📁 Loading image from file: %s", openaiFlags.imagePath))
			imageData, err := os.ReadFile(openaiFlags.imagePath)
			if err != nil {
				log.Fatalf("Failed to read image file: %v", err)
			}

			// Detect MIME type from file extension
			ext := strings.ToLower(filepath.Ext(openaiFlags.imagePath))
			mediaType := mime.TypeByExtension(ext)
			if mediaType == "" {
				// Fallback to common types
				switch ext {
				case ".jpg", ".jpeg":
					mediaType = "image/jpeg"
				case ".png":
					mediaType = "image/png"
				case ".gif":
					mediaType = "image/gif"
				case ".webp":
					mediaType = "image/webp"
				default:
					log.Fatalf("Unsupported image format: %s. Supported: JPEG, PNG, GIF, WebP", ext)
				}
			}

			// Encode to base64
			base64Data := base64.StdEncoding.EncodeToString(imageData)
			logger.Info(fmt.Sprintf("✅ Image loaded: %d bytes, MIME type: %s", len(imageData), mediaType))

			imageParts = append(imageParts, llmtypes.ImageContent{
				SourceType: "base64",
				MediaType:  mediaType,
				Data:       base64Data,
			})
		} else {
			// Use image URL (from flag or default test URL)
			imageURL := openaiFlags.imageURL
			if imageURL == "" {
				// Default test image URL - Vertex AI logo
				imageURL = "https://cdn.prod.website-files.com/657639ebfb91510f45654149/67cef0fb78a461a1580d3c5a_667f5f1018134e3c5a8549c2_AD_4nXfn52WaKNUy839wUllpITpaj7mvuOTR6AOzDk3SypLHLgO-_n8zgt7QJ7rxcLOfOJRWAShjk1dIZRmwuKYLCYFD4qgOq1SCiGFIYbnhDLjD1E0zTdb8cgnCBceLMy7lmCZ3qDUce-gCfJjofiZ9ftDF2m4.webp"
			}
			logger.Info(fmt.Sprintf("🌐 Using image URL: %s", imageURL))
			imageParts = append(imageParts, llmtypes.ImageContent{
				SourceType: "url",
				MediaType:  "", // Not needed for URL
				Data:       imageURL,
			})
		}

		// Create message with text and image
		parts := []llmtypes.ContentPart{
			llmtypes.TextContent{Text: "What is the text written in this image?"},
		}
		parts = append(parts, imageParts...)

		messages = []llmtypes.MessageContent{
			{
				Role:  llmtypes.ChatMessageTypeHuman,
				Parts: parts,
			},
		}

		logger.Info("✅ Image input test configured")
	} else {
		// Default test: image input with Vertex AI logo
		logger.Info("🖼️ Running default image input test...")
		
		// Default test image URL - Vertex AI logo
		testImageURL := "https://cdn.prod.website-files.com/657639ebfb91510f45654149/67cef0fb78a461a1580d3c5a_667f5f1018134e3c5a8549c2_AD_4nXfn52WaKNUy839wUllpITpaj7mvuOTR6AOzDk3SypLHLgO-_n8zgt7QJ7rxcLOfOJRWAShjk1dIZRmwuKYLCYFD4qgOq1SCiGFIYbnhDLjD1E0zTdb8cgnCBceLMy7lmCZ3qDUce-gCfJjofiZ9ftDF2m4.webp"
		
		parts := []llmtypes.ContentPart{
			llmtypes.TextContent{Text: "What is the text written in this image?"},
			llmtypes.ImageContent{
				SourceType: "url",
				MediaType:  "",
				Data:       testImageURL,
			},
		}

		messages = []llmtypes.MessageContent{
			{
				Role:  llmtypes.ChatMessageTypeHuman,
				Parts: parts,
			},
		}
		
		logger.Info(fmt.Sprintf("✅ Default image test configured with URL: %s", testImageURL))
	}

	// Call with or without tools
	var resp *llmtypes.ContentResponse
	if len(tools) > 0 {
		logger.Info(fmt.Sprintf("📤 Sending %d tools to GPT...", len(tools)))

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
		if openaiFlags.structured {
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
		info := choice.GenerationInfo
		logger.Info("📊 Token Usage:")
		if info.InputTokens != nil {
			logger.Info(fmt.Sprintf("   Input tokens: %v", *info.InputTokens))
		}
		if info.OutputTokens != nil {
			logger.Info(fmt.Sprintf("   Output tokens: %v", *info.OutputTokens))
		}
		if info.TotalTokens != nil {
			logger.Info(fmt.Sprintf("   Total tokens: %v", *info.TotalTokens))
		}
		// Check for cache tokens in Additional map
		if info.Additional != nil {
			if cacheRead, ok := info.Additional["cache_read_input_tokens"]; ok {
				logger.Info(fmt.Sprintf("   Cache read tokens: %v", cacheRead))
			}
			if cacheCreate, ok := info.Additional["cache_creation_input_tokens"]; ok {
				logger.Info(fmt.Sprintf("   Cache creation tokens: %v", cacheCreate))
			}
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
		if openaiFlags.structured {
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

