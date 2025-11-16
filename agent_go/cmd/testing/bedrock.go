package testing

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"mcp-agent/agent_go/internal/llm"
	"mcp-agent/agent_go/internal/llmtypes"
)

var bedrockCmd = &cobra.Command{
	Use:   "bedrock",
	Short: "Test Bedrock tool calling",
	Long:  "Test AWS Bedrock LLM with tool calling capabilities",
	Run:   runBedrock,
}

// bedrockTestFlags holds the bedrock test specific flags
type bedrockTestFlags struct {
	model        string
	region       string
	verbose      bool
	showResponse bool
	imagePath    string
	imageURL     string
}

var bedrockFlags bedrockTestFlags

func init() {
	// Bedrock test specific flags
	bedrockCmd.Flags().StringVar(&bedrockFlags.model, "model", "global.anthropic.claude-sonnet-4-5-20250929-v1:0", "Bedrock model to test")
	bedrockCmd.Flags().StringVar(&bedrockFlags.region, "region", "us-east-1", "AWS region for Bedrock")
	bedrockCmd.Flags().BoolVar(&bedrockFlags.verbose, "verbose", false, "enable verbose output")
	bedrockCmd.Flags().BoolVar(&bedrockFlags.showResponse, "show-response", true, "show full response")
	bedrockCmd.Flags().StringVar(&bedrockFlags.imagePath, "with-image", "", "path to image file to test image input (JPEG, PNG, GIF, WebP)")
	bedrockCmd.Flags().StringVar(&bedrockFlags.imageURL, "image-url", "", "URL of image to test image input")
}

func runBedrock(cmd *cobra.Command, args []string) {
	// Get logging configuration from viper
	logFile := viper.GetString("log-file")
	logLevel := viper.GetString("log-level")

	// Initialize test logger
	InitTestLogger(logFile, logLevel)
	logger := GetTestLogger()

	testType := ""
	if bedrockFlags.imagePath != "" || bedrockFlags.imageURL != "" {
		testType = "image input"
	} else {
		// Default test: image input with Vertex AI logo
		testType = "image input (default)"
	}
	logger.Info(fmt.Sprintf("🚀 Testing Bedrock (%s)", testType))

	// Use model ID from flags (default is already set to the new model)
	modelID := bedrockFlags.model
	if modelID == "" {
		// Fallback to the new model if not specified
		modelID = "global.anthropic.claude-sonnet-4-5-20250929-v1:0"
	}

	// Create Bedrock LLM using new adapter
	llm, err := llm.InitializeLLM(llm.Config{
		Provider:    llm.ProviderBedrock,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
	})
	if err != nil {
		log.Fatalf("Failed to create Bedrock LLM: %v", err)
	}

	var messages []llmtypes.MessageContent
	var tools []llmtypes.Tool

	if bedrockFlags.imagePath != "" || bedrockFlags.imageURL != "" {
		// Test image input
		logger.Info("🖼️ Setting up image input test...")

		var imageParts []llmtypes.ContentPart

		if bedrockFlags.imagePath != "" {
			// Load and encode image file
			logger.Info(fmt.Sprintf("📁 Loading image from file: %s", bedrockFlags.imagePath))
			imageData, err := os.ReadFile(bedrockFlags.imagePath)
			if err != nil {
				log.Fatalf("Failed to read image file: %v", err)
			}

			// Detect MIME type from file extension
			ext := strings.ToLower(filepath.Ext(bedrockFlags.imagePath))
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
			imageURL := bedrockFlags.imageURL
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

	ctx := context.Background()

	// Call with or without tools
	var resp *llmtypes.ContentResponse
	if len(tools) > 0 {
		logger.Info("📞 Calling Bedrock with tool...")
		resp, err = llm.GenerateContent(ctx, messages,
			llmtypes.WithTools(tools),
			llmtypes.WithToolChoiceString("required"),
		)
	} else {
		logger.Info("📞 Calling Bedrock...")
		resp, err = llm.GenerateContent(ctx, messages)
	}
	if err != nil {
		logger.Fatal("❌ Call failed", map[string]interface{}{"error": err.Error()})
	}

	if len(resp.Choices) == 0 {
		logger.Fatal("❌ No choices returned")
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
		logger.Info("✅ Response received", map[string]interface{}{
			"response": choice.Content,
		})
	}
}
