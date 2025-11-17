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
	"mcp-agent/agent_go/internal/llm/vertex"
	"mcp-agent/agent_go/internal/llmtypes"
	"mcp-agent/agent_go/pkg/mcpclient"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"google.golang.org/genai"
)

var vertexCmd = &cobra.Command{
	Use:   "vertex",
	Short: "Test Vertex AI (Gemini) with API key and tool calling",
	Run:   runVertex,
}

type vertexTestFlags struct {
	model      string
	apiKey     string
	withTools  bool
	withGitHub bool
	structured bool
	configPath string
	imagePath  string
	imageURL   string
}

var vertexFlags vertexTestFlags

func init() {
	vertexCmd.Flags().StringVar(&vertexFlags.model, "model", "gemini-2.5-flash", "Gemini model to test")
	vertexCmd.Flags().StringVar(&vertexFlags.apiKey, "api-key", "", "Google API key (or set VERTEX_API_KEY env var)")
	vertexCmd.Flags().BoolVar(&vertexFlags.withTools, "with-tools", false, "enable tool calling")
	vertexCmd.Flags().BoolVar(&vertexFlags.withGitHub, "with-github", false, "use GitHub MCP tools for testing")
	vertexCmd.Flags().BoolVar(&vertexFlags.structured, "structured", false, "test structured JSON output with ResponseSchema")
	vertexCmd.Flags().StringVar(&vertexFlags.configPath, "config", "configs/mcp_servers_clean_user.json", "MCP config file path")
	vertexCmd.Flags().StringVar(&vertexFlags.imagePath, "with-image", "", "path to image file to test image input (JPEG, PNG, GIF, WebP)")
	vertexCmd.Flags().StringVar(&vertexFlags.imageURL, "image-url", "", "URL of image to test image input")
}

func runVertex(cmd *cobra.Command, args []string) {
	logFile := viper.GetString("log-file")
	logLevel := viper.GetString("log-level")
	InitTestLogger(logFile, logLevel)
	logger := GetTestLogger()

	// Get API key (optional - will use OAuth if not provided)
	apiKey := vertexFlags.apiKey
	if apiKey == "" {
		if key := os.Getenv("VERTEX_API_KEY"); key != "" {
			apiKey = key
		} else if key := os.Getenv("GOOGLE_API_KEY"); key != "" {
			apiKey = key
		}
	}

	// Set API key as environment variable if provided (for API key auth)
	if apiKey != "" {
		os.Setenv("VERTEX_API_KEY", apiKey)
		logger.Info("🔑 Using API key authentication")
	} else {
		logger.Info("🔐 No API key provided - will use OAuth authentication (gcloud/ADC)")

		// Ensure required environment variables are set for OAuth
		projectID := os.Getenv("GOOGLE_CLOUD_PROJECT")
		if projectID == "" {
			projectID = os.Getenv("VERTEX_PROJECT_ID")
		}
		if projectID == "" {
			log.Fatal("For OAuth authentication, GOOGLE_CLOUD_PROJECT or VERTEX_PROJECT_ID environment variable is required")
		}

		location := os.Getenv("GOOGLE_CLOUD_LOCATION")
		if location == "" {
			location = os.Getenv("VERTEX_LOCATION_ID")
		}
		if location == "" {
			location = "us-central1"
			logger.Info(fmt.Sprintf("⚠️ GOOGLE_CLOUD_LOCATION not set, using default: %s", location))
		}
		// Vertex AI doesn't support "global" location
		if location == "global" {
			location = "us-central1"
			logger.Info(fmt.Sprintf("⚠️ Location 'global' is not valid for Vertex AI, using: %s", location))
		}

		// Set environment variables for OAuth
		os.Setenv("GOOGLE_CLOUD_PROJECT", projectID)
		os.Setenv("GOOGLE_CLOUD_LOCATION", location)
		os.Setenv("GOOGLE_GENAI_USE_VERTEXAI", "true")
		logger.Info(fmt.Sprintf("🔧 Using Vertex AI project: %s, location: %s", projectID, location))
	}

	ctx := context.Background()

	testType := "image input (default)"
	if vertexFlags.withTools {
		testType = "tool calling"
	} else if vertexFlags.structured {
		testType = "structured output"
	} else if vertexFlags.imagePath != "" || vertexFlags.imageURL != "" {
		testType = "image input"
	}
	logger.Info(fmt.Sprintf("🚀 Testing Vertex AI (%s)", testType))

	// Set default model if not specified
	modelID := vertexFlags.model
	if modelID == "" {
		modelID = "gemini-2.5-flash"
	}

	// Initialize Vertex AI LLM using internal provider
	// The internal provider automatically uses vertex.New() which switches to BackendGeminiAPI with API key
	llmInstance, err := llm.InitializeLLM(llm.Config{
		Provider:    llm.ProviderVertex,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
		Context:     ctx,
	})
	if err != nil {
		log.Fatalf("Failed to initialize Vertex LLM: %v", err)
	}

	var tools []llmtypes.Tool
	var messages []llmtypes.MessageContent
	var responseSchema *genai.Schema

	if vertexFlags.structured {
		// Test structured output with ResponseSchema (recipe example from user)
		logger.Info("📋 Setting up structured output test with ResponseSchema...")

		// Create the ResponseSchema matching the user's example
		responseSchema = &genai.Schema{
			Type: genai.TypeArray,
			Items: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"recipeName": {
						Type: genai.TypeString,
					},
					"ingredients": {
						Type: genai.TypeArray,
						Items: &genai.Schema{
							Type: genai.TypeString,
						},
					},
				},
				PropertyOrdering: []string{"recipeName", "ingredients"},
			},
		}

		// Set context with ResponseSchema
		ctx = vertex.WithResponseSchema(ctx, responseSchema)

		messages = []llmtypes.MessageContent{
			llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "List a few popular cookie recipes, and include the amounts of ingredients."),
		}

		logger.Info("✅ Structured output test configured")
		logger.Info("   Schema: Array of objects with recipeName (string) and ingredients (array of strings)")
	} else if vertexFlags.withGitHub {
		// Load GitHub MCP tools
		logger.Info("🔗 Connecting to GitHub MCP server...")
		config, err := mcpclient.LoadMergedConfig(vertexFlags.configPath, logger)
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

		// Normalize tools
		logger.Info("🔧 Normalizing tools for Gemini compatibility...")
		mcpclient.NormalizeLLMTools(llmTools)
		tools = llmTools

		logger.Info(fmt.Sprintf("✅ Normalized %d tools for Gemini", len(tools)))
		messages = []llmtypes.MessageContent{
			llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "List my GitHub repositories"),
		}
	} else if vertexFlags.withTools {
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
	} else if vertexFlags.imagePath != "" || vertexFlags.imageURL != "" {
		// Test image input
		logger.Info("🖼️ Setting up image input test...")

		var imageParts []llmtypes.ContentPart

		if vertexFlags.imagePath != "" {
			// Load and encode image file
			logger.Info(fmt.Sprintf("📁 Loading image from file: %s", vertexFlags.imagePath))
			imageData, err := os.ReadFile(vertexFlags.imagePath)
			if err != nil {
				log.Fatalf("Failed to read image file: %v", err)
			}

			// Detect MIME type from file extension
			ext := strings.ToLower(filepath.Ext(vertexFlags.imagePath))
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
			imageURL := vertexFlags.imageURL
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
		logger.Info(fmt.Sprintf("📤 Sending %d tools to Gemini...", len(tools)))

		// DEBUG: Marshal tools to JSON to see what's actually being sent
		toolsJSON, _ := json.MarshalIndent(tools, "", "  ")
		jsonLen := len(toolsJSON)
		if jsonLen > 2000 {
			jsonLen = 2000
		}
		logger.Info(fmt.Sprintf("🔍 Tools JSON structure (first 2000 chars):\n%s", string(toolsJSON[:jsonLen])))

		// Check specific problematic tools
		for i, tool := range tools {
			if tool.Function != nil {
				// Check for array parameters without items
				if tool.Function.Parameters != nil {
					// Convert Parameters to map for compatibility
					paramsBytes, err := json.Marshal(tool.Function.Parameters)
					var params map[string]interface{}
					if err == nil {
						json.Unmarshal(paramsBytes, &params)
					}
					if params != nil {
						if props, ok := params["properties"].(map[string]interface{}); ok {
							for propName, propValue := range props {
								if propMap, ok := propValue.(map[string]interface{}); ok {
									if propType, ok := propMap["type"].(string); ok && propType == "array" {
										hasItems := propMap["items"] != nil
										if !hasItems {
											logger.Info(fmt.Sprintf("⚠️ Tool %d (%s) has array param %s WITHOUT items!", i, tool.Function.Name, propName))
										} else {
											logger.Info(fmt.Sprintf("✅ Tool %d (%s) has array param %s WITH items", i, tool.Function.Name, propName))
										}
									}
								}
							}
						}
					}
				}
			}
		}

		resp, err = llmInstance.GenerateContent(ctx, messages,
			llmtypes.WithModel(modelID),
			llmtypes.WithTools(tools))
	} else {
		// For structured output, also enable JSON mode
		if vertexFlags.structured {
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
		if vertexFlags.structured {
			// Validate structured output
			logger.Info("📋 Validating structured JSON output...")

			// Try to parse as JSON array
			var recipes []map[string]interface{}
			if err := json.Unmarshal([]byte(choice.Content), &recipes); err != nil {
				logger.Warn(fmt.Sprintf("⚠️ Response is not valid JSON array: %v", err))
				logger.Info("Response content:", map[string]interface{}{
					"content": choice.Content,
				})
			} else {
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
