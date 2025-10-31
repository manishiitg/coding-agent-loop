package testing

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"mcp-agent/agent_go/internal/llm"
	"mcp-agent/agent_go/pkg/mcpclient"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/tmc/langchaingo/llms"
)

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

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
	configPath string
}

var vertexFlags vertexTestFlags

func init() {
	vertexCmd.Flags().StringVar(&vertexFlags.model, "model", "gemini-2.5-flash", "Gemini model to test")
	vertexCmd.Flags().StringVar(&vertexFlags.apiKey, "api-key", "", "Google API key (or set VERTEX_API_KEY env var)")
	vertexCmd.Flags().BoolVar(&vertexFlags.withTools, "with-tools", false, "enable tool calling")
	vertexCmd.Flags().BoolVar(&vertexFlags.withGitHub, "with-github", false, "use GitHub MCP tools for testing")
	vertexCmd.Flags().StringVar(&vertexFlags.configPath, "config", "configs/mcp_servers_clean_user.json", "MCP config file path")
}

func runVertex(cmd *cobra.Command, args []string) {
	logFile := viper.GetString("log-file")
	logLevel := viper.GetString("log-level")
	InitTestLogger(logFile, logLevel)
	logger := GetTestLogger()

	// Get API key
	apiKey := vertexFlags.apiKey
	if apiKey == "" {
		if key := os.Getenv("VERTEX_API_KEY"); key != "" {
			apiKey = key
		} else if key := os.Getenv("GOOGLE_API_KEY"); key != "" {
			apiKey = key
		}
	}
	if apiKey == "" {
		log.Fatal("API key required: set --api-key flag or VERTEX_API_KEY/GOOGLE_API_KEY environment variable")
	}

	// Set API key as environment variable for internal LLM provider to pick up
	os.Setenv("VERTEX_API_KEY", apiKey)

	ctx := context.Background()

	testType := "plain generation"
	if vertexFlags.withTools {
		testType = "tool calling"
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

	var tools []llms.Tool
	var messages []llms.MessageContent

	if vertexFlags.withGitHub {
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
		messages = []llms.MessageContent{
			llms.TextParts(llms.ChatMessageTypeHuman, "List my GitHub repositories"),
		}
	} else if vertexFlags.withTools {
		// Define a simple weather tool
		weatherTool := llms.Tool{
			Type: "function",
			Function: &llms.FunctionDefinition{
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
		tools = []llms.Tool{weatherTool}
		messages = []llms.MessageContent{
			llms.TextParts(llms.ChatMessageTypeHuman, "What's the weather in Tokyo?"),
		}
	} else {
		messages = []llms.MessageContent{
			llms.TextParts(llms.ChatMessageTypeHuman, "Hello! Can you introduce yourself?"),
		}
	}

	// Call with or without tools
	var resp *llms.ContentResponse
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
				if params, ok := tool.Function.Parameters.(map[string]interface{}); ok {
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

		resp, err = llmInstance.GenerateContent(ctx, messages,
			llms.WithModel(modelID),
			llms.WithTools(tools))
	} else {
		resp, err = llmInstance.GenerateContent(ctx, messages, llms.WithModel(modelID))
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
		logger.Info("✅ Success! Response received", map[string]interface{}{
			"content": choice.Content,
		})
	}
}
