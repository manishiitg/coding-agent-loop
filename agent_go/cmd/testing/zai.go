package testing

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/manishiitg/mcpagent/llm"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var zaiTestCmd = &cobra.Command{
	Use:   "zai",
	Short: "Smoke test Z.AI text and optional vision models",
	Long: `Smoke test Z.AI provider integration from the builder repo.

This test:
1. Loads ZAI_API_KEY from .env or the current environment
2. Runs a direct text generation check with a text model
3. Optionally runs a direct vision check with a separate vision model when --image-path is provided

Examples:
  orchestrator test zai
  orchestrator test zai --model glm-4.7
  orchestrator test zai --image-path /absolute/path/to/image.png
  orchestrator test zai --vision-model glm-5v-turbo --image-path Downloads/sample.png`,
	RunE: runZAITest,
}

func init() {
	zaiTestCmd.Flags().String("model", "", "Z.AI text model to use (default: glm-5.1)")
	zaiTestCmd.Flags().String("vision-model", "", "Z.AI vision model to use when --image-path is provided (default: glm-4.6v)")
	zaiTestCmd.Flags().String("image-path", "", "Optional image path for a Z.AI vision smoke test")
	zaiTestCmd.Flags().Bool("skip-text", false, "Skip the text generation smoke test")
	zaiTestCmd.Flags().Bool("skip-vision", false, "Skip the vision smoke test even if --image-path is provided")

	_ = viper.BindPFlag("zai.model", zaiTestCmd.Flags().Lookup("model"))
	_ = viper.BindPFlag("zai.vision-model", zaiTestCmd.Flags().Lookup("vision-model"))
	_ = viper.BindPFlag("zai.image-path", zaiTestCmd.Flags().Lookup("image-path"))
	_ = viper.BindPFlag("zai.skip-text", zaiTestCmd.Flags().Lookup("skip-text"))
	_ = viper.BindPFlag("zai.skip-vision", zaiTestCmd.Flags().Lookup("skip-vision"))
}

func runZAITest(cmd *cobra.Command, args []string) error {
	loadTestDotEnv()

	logFile := viper.GetString("log-file")
	logLevel := viper.GetString("log-level")
	InitTestLogger(logFile, logLevel)
	logger := GetTestLogger()

	textModel := strings.TrimSpace(viper.GetString("zai.model"))
	if textModel == "" {
		textModel = "glm-5.1"
	}

	visionModel := strings.TrimSpace(viper.GetString("zai.vision-model"))
	if visionModel == "" {
		visionModel = "glm-4.6v"
	}

	imagePath := strings.TrimSpace(viper.GetString("zai.image-path"))
	skipText := viper.GetBool("zai.skip-text")
	skipVision := viper.GetBool("zai.skip-vision")

	apiKey := strings.TrimSpace(os.Getenv("ZAI_API_KEY"))
	if apiKey == "" {
		return fmt.Errorf("ZAI_API_KEY environment variable is not set")
	}

	logger.Info("=== Z.AI Smoke Test ===")
	logger.Info(fmt.Sprintf("Text model: %s", textModel))
	if imagePath != "" {
		logger.Info(fmt.Sprintf("Vision model: %s", visionModel))
		logger.Info(fmt.Sprintf("Image path: %s", imagePath))
	}

	var textResp string
	var visionResp string

	if !skipText {
		resp, err := runZAITextSmokeTest(textModel, apiKey)
		if err != nil {
			return err
		}
		textResp = resp
	}

	if imagePath != "" && !skipVision {
		resp, err := runZAIVisionSmokeTest(visionModel, apiKey, imagePath)
		if err != nil {
			return err
		}
		visionResp = resp
	}

	fmt.Println("\nZ.AI smoke test completed successfully.")
	if textResp != "" {
		fmt.Printf("\nText response (%s):\n%s\n", textModel, textResp)
	}
	if visionResp != "" {
		fmt.Printf("\nVision response (%s):\n%s\n", visionModel, visionResp)
	}

	return nil
}

func loadTestDotEnv() {
	if err := godotenv.Load("agent_go/.env"); err == nil {
		return
	}
	if err := godotenv.Load(".env"); err == nil {
		return
	}
	_ = godotenv.Load("../.env")
}

func runZAITextSmokeTest(modelID, apiKey string) (string, error) {
	llmModel, err := llm.InitializeLLM(llm.Config{
		Provider:    llm.ProviderZAI,
		ModelID:     modelID,
		Temperature: 0.1,
		APIKeys: &llm.ProviderAPIKeys{
			ZAI: &apiKey,
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to initialize Z.AI text model %q: %w", modelID, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, err := llmModel.GenerateContent(ctx, []llmtypes.MessageContent{
		llmtypes.TextPart(llmtypes.ChatMessageTypeHuman, "Reply with exactly: ZAI_TEXT_OK"),
	})
	if err != nil {
		return "", fmt.Errorf("Z.AI text smoke test failed for model %q: %w", modelID, err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("Z.AI text smoke test returned no choices for model %q", modelID)
	}

	content := strings.TrimSpace(resp.Choices[0].Content)
	if content == "" {
		return "", fmt.Errorf("Z.AI text smoke test returned an empty response for model %q", modelID)
	}

	return content, nil
}

func runZAIVisionSmokeTest(modelID, apiKey, imagePath string) (string, error) {
	imageBytes, err := os.ReadFile(imagePath)
	if err != nil {
		return "", fmt.Errorf("failed to read image %q: %w", imagePath, err)
	}

	mimeType := detectImageMIMEType(imagePath, imageBytes)
	if !strings.HasPrefix(mimeType, "image/") {
		return "", fmt.Errorf("file %q does not look like an image (detected MIME type %q)", imagePath, mimeType)
	}

	llmModel, err := llm.InitializeLLM(llm.Config{
		Provider:    llm.ProviderZAI,
		ModelID:     modelID,
		Temperature: 0.1,
		APIKeys: &llm.ProviderAPIKeys{
			ZAI: &apiKey,
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to initialize Z.AI vision model %q: %w", modelID, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	resp, err := llmModel.GenerateContent(ctx, []llmtypes.MessageContent{
		{
			Role: llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: "Describe the main visible content of this image in 2 short sentences."},
				llmtypes.ImageContent{
					SourceType: "base64",
					MediaType:  mimeType,
					Data:       base64.StdEncoding.EncodeToString(imageBytes),
				},
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("Z.AI vision smoke test failed for model %q: %w", modelID, err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("Z.AI vision smoke test returned no choices for model %q", modelID)
	}

	content := strings.TrimSpace(resp.Choices[0].Content)
	if content == "" {
		return "", fmt.Errorf("Z.AI vision smoke test returned an empty response for model %q", modelID)
	}

	return content, nil
}

func detectImageMIMEType(imagePath string, imageBytes []byte) string {
	mimeType := http.DetectContentType(imageBytes)
	if strings.HasPrefix(mimeType, "image/") {
		return mimeType
	}

	switch strings.ToLower(filepath.Ext(imagePath)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".bmp":
		return "image/bmp"
	case ".svg":
		return "image/svg+xml"
	default:
		return "image/png"
	}
}
