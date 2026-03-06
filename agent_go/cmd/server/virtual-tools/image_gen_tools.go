package virtualtools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	llm "github.com/manishiitg/multi-llm-provider-go"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"mcp-agent-builder-go/agent_go/pkg/workspace"
)

// imageGenModelCosts maps model IDs to cost-per-image in USD
var imageGenModelCosts = map[string]float64{
	"gemini-3.1-flash-image-preview": 0.067,  // $0.045/0.5K · $0.067/1K · $0.101/2K · $0.151/4K
	"gemini-3-pro-image-preview":     0.134,  // $0.134/1K-2K image · $0.24/4K image
	"gemini-2.5-flash-image":         0.039,  // $0.039/image flat
	"image-01":                       0.0035, // MiniMax Image-01
}

// ImageGenExecutorConfig holds configuration for the image generation executor
type ImageGenExecutorConfig struct {
	Provider        string // e.g. "vertex"
	ModelID         string // e.g. "imagen-4.0-generate-001"
	APIKey          string // optional; falls back to GEMINI_API_KEY env var on the server
	WorkspaceAPIURL string // workspace API base URL for saving generated images
	UserID          string // user ID for per-user workspace isolation
}

// GetImageGenToolCategory returns the category name for the image gen tool
func GetImageGenToolCategory() string {
	return "workspace_image_gen"
}

// GetImageGenToolDefinition returns the workspace_image_gen tool definition
func GetImageGenToolDefinition() llmtypes.Tool {
	return llmtypes.Tool{
		Function: &llmtypes.FunctionDefinition{
			Name:        "workspace_image_gen",
			Description: "Generate images using AI from a text prompt. Saves results to the workspace (Chats/generated-images/) and displays them inline in the chat. Supports aspect ratio, resolution, number of images, and negative prompt options.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"prompt": map[string]interface{}{
						"type":        "string",
						"description": "Text prompt describing the image to generate, or the edit instruction when input_image is provided.",
					},
					"input_image": map[string]interface{}{
						"type":        "string",
						"description": "Optional base64-encoded image to edit. When provided, the model modifies this image according to the prompt instead of generating from scratch.",
					},
					"input_image_mime_type": map[string]interface{}{
						"type":        "string",
						"description": "MIME type of the input image (e.g. 'image/png', 'image/jpeg'). Defaults to 'image/png'.",
					},
					"aspect_ratio": map[string]interface{}{
						"type":        "string",
						"description": "The aspect ratio for the generated image.",
						"enum":        []interface{}{"1:1", "2:3", "3:2", "3:4", "4:3", "9:16", "16:9", "21:9"},
					},
					"resolution": map[string]interface{}{
						"type":        "string",
						"description": "Output image resolution. Defaults to '1K'.",
						"enum":        []interface{}{"1K", "2K", "4K"},
					},
					"number_of_images": map[string]interface{}{
						"type":        "integer",
						"description": "Number of images to generate (1-4). Defaults to 1.",
						"minimum":     1,
						"maximum":     4,
					},
					"negative_prompt": map[string]interface{}{
						"type":        "string",
						"description": "Optional description of what to exclude from the generated images.",
					},
				},
				"required": []interface{}{"prompt"},
			}),
		},
	}
}

// imageGenResult is the JSON structure returned to the LLM
type imageGenResult struct {
	// Images is only populated when workspace saving is unavailable (fallback)
	Images       []imageGenResultImage `json:"images,omitempty"`
	Model        string                `json:"model"`
	CostPerImage float64               `json:"cost_per_image"`
	Prompt       string                `json:"prompt"`
	SavedPaths   []string              `json:"saved_paths,omitempty"`
	Count        int                   `json:"count"`
	Note         string                `json:"note,omitempty"`
}

type imageGenResultImage struct {
	Data     string `json:"data"`      // base64-encoded
	MIMEType string `json:"mime_type"` // e.g. "image/png"
}

// CreateImageGenExecutor returns an executor that calls InitializeImageGenerationModel,
// then saves the generated images to the workspace (Chats/generated-images/).
func CreateImageGenExecutor(cfg ImageGenExecutorConfig) func(ctx context.Context, args map[string]any) (string, error) {
	return func(ctx context.Context, args map[string]any) (string, error) {
		prompt, _ := args["prompt"].(string)
		if prompt == "" {
			return "", fmt.Errorf("prompt is required")
		}

		provider := cfg.Provider
		if provider == "" {
			provider = "vertex"
		}
		modelID := cfg.ModelID
		if modelID == "" {
			modelID = "gemini-2.5-flash-image"
		}

		var apiKeyPtr *string
		if cfg.APIKey != "" {
			k := cfg.APIKey
			apiKeyPtr = &k
		}

		providerAPIKeys := &llm.ProviderAPIKeys{}
		if provider == "minimax" {
			providerAPIKeys.MiniMax = apiKeyPtr
		} else {
			providerAPIKeys.Vertex = apiKeyPtr
		}

		imageGenCfg := llm.Config{
			Provider: llm.Provider(provider),
			ModelID:  modelID,
			APIKeys:  providerAPIKeys,
			Context:  ctx,
		}

		log.Printf("[IMAGE_GEN] Initializing model: provider=%s model=%s apiKeyProvided=%v workspaceURL=%q userID=%q",
			provider, modelID, cfg.APIKey != "", cfg.WorkspaceAPIURL, cfg.UserID)

		model, err := llm.InitializeImageGenerationModel(imageGenCfg)
		if err != nil {
			log.Printf("[IMAGE_GEN] Failed to initialize image generation model: %v", err)
			return "", fmt.Errorf("failed to initialize image generation model: %w", err)
		}
		log.Printf("[IMAGE_GEN] Model initialized successfully")

		// Build options from args
		var opts []llmtypes.ImageGenerationOption
		if ar, ok := args["aspect_ratio"].(string); ok && ar != "" {
			log.Printf("[IMAGE_GEN] aspect_ratio=%s", ar)
			opts = append(opts, llmtypes.WithAspectRatio(ar))
		}
		if res, ok := args["resolution"].(string); ok && res != "" {
			log.Printf("[IMAGE_GEN] resolution=%s", res)
			opts = append(opts, llmtypes.WithResolution(res))
		}
		if n, ok := args["number_of_images"].(float64); ok && n >= 1 {
			log.Printf("[IMAGE_GEN] number_of_images=%d", int(n))
			opts = append(opts, llmtypes.WithNumberOfImages(int(n)))
		}
		if np, ok := args["negative_prompt"].(string); ok && np != "" {
			log.Printf("[IMAGE_GEN] negative_prompt set (%d chars)", len(np))
			opts = append(opts, llmtypes.WithNegativePrompt(np))
		}
		if inputImageB64, ok := args["input_image"].(string); ok && inputImageB64 != "" {
			imgBytes, err := base64.StdEncoding.DecodeString(inputImageB64)
			if err != nil {
				return "", fmt.Errorf("invalid input_image base64: %w", err)
			}
			mimeType, _ := args["input_image_mime_type"].(string)
			if mimeType == "" {
				mimeType = "image/png"
			}
			opts = append(opts, llmtypes.WithInputImage(imgBytes, mimeType))
			log.Printf("[IMAGE_GEN] Edit mode: input image %d bytes, mime=%s", len(imgBytes), mimeType)
		}

		mode := "generate"
		if _, hasInput := args["input_image"]; hasInput {
			mode = "edit"
		}
		log.Printf("[IMAGE_GEN] %s image: prompt=%q model=%s", mode, prompt, modelID)
		resp, err := model.GenerateImages(ctx, prompt, opts...)
		if err != nil {
			log.Printf("[IMAGE_GEN] GenerateImages failed: %v", err)
			return "", fmt.Errorf("image generation failed: %w", err)
		}

		if len(resp.Images) == 0 {
			return "", fmt.Errorf("image generation returned no images (prompt may have been filtered by safety filters)")
		}

		log.Printf("[IMAGE_GEN] Generated %d image(s) with model=%s", len(resp.Images), modelID)

		var savedPaths []string
		// fallbackImages holds base64 data only when workspace saving is unavailable
		var fallbackImages []imageGenResultImage

		timestamp := time.Now().Format("20060102-150405")

		// Workspace client for saving files (optional — skip if no URL configured)
		var wsClient *workspace.Client
		if cfg.WorkspaceAPIURL != "" {
			wsClient = workspace.NewClient(
				cfg.WorkspaceAPIURL,
				workspace.WithUserID(cfg.UserID),
				workspace.WithFolderGuard(&workspace.FolderGuardConfig{
					Enabled:    true,
					WritePaths: []string{"Chats/"},
				}),
			)
			// Ensure the generated-images folder exists (ignore 409 — folder already exists is fine)
			if err := wsClient.CreateFolder(ctx, "Chats/generated-images"); err != nil {
				errStr := err.Error()
				if strings.Contains(errStr, "409") || strings.Contains(errStr, "already exists") {
					log.Printf("[IMAGE_GEN] Chats/generated-images folder already exists, proceeding")
				} else {
					log.Printf("[IMAGE_GEN] Warning: failed to create Chats/generated-images folder: %v", err)
					wsClient = nil // genuine error — proceed without saving
				}
			}
		}

		for i, img := range resp.Images {
			mimeType := img.MimeType
			if mimeType == "" {
				mimeType = "image/png"
			}

			if wsClient != nil && len(img.Data) > 0 {
				// Save to workspace — don't include base64 in response
				ext := "png"
				if mimeType == "image/jpeg" {
					ext = "jpg"
				} else if mimeType == "image/webp" {
					ext = "webp"
				}
				fileName := fmt.Sprintf("image-%s-%d.%s", timestamp, i+1, ext)
				savedPath, saveErr := wsClient.UploadBinary(ctx, "Chats/generated-images", fileName, img.Data)
				if saveErr != nil {
					log.Printf("[IMAGE_GEN] Warning: failed to save image %d to workspace: %v", i+1, saveErr)
					// Fall back to base64 for this image
					fallbackImages = append(fallbackImages, imageGenResultImage{
						Data:     base64.StdEncoding.EncodeToString(img.Data),
						MIMEType: mimeType,
					})
				} else {
					log.Printf("[IMAGE_GEN] Saved image %d to workspace: %s", i+1, savedPath)
					savedPaths = append(savedPaths, savedPath)
				}
			} else {
				// No workspace client — return base64 so the LLM/UI can still show the image
				fallbackImages = append(fallbackImages, imageGenResultImage{
					Data:     base64.StdEncoding.EncodeToString(img.Data),
					MIMEType: mimeType,
				})
			}
		}

		costPerImage := imageGenModelCosts[modelID]
		result := imageGenResult{
			Images:       fallbackImages, // nil (omitempty) when all images were saved to workspace
			Model:        modelID,
			CostPerImage: costPerImage,
			Prompt:       prompt,
			SavedPaths:   savedPaths,
			Count:        len(resp.Images),
			Note:         "",
		}
		log.Printf("[IMAGE_GEN] Done: saved=%d fallback=%d costPerImage=$%.4f", len(savedPaths), len(fallbackImages), costPerImage)

		resultJSON, err := json.Marshal(result)
		if err != nil {
			return "", fmt.Errorf("failed to marshal image generation result: %w", err)
		}

		return string(resultJSON), nil
	}
}

// GetImageEditToolCategory returns the category name for the image edit tool
func GetImageEditToolCategory() string {
	return "workspace_image_edit"
}

// GetImageEditToolDefinition returns the workspace_image_edit tool definition
func GetImageEditToolDefinition() llmtypes.Tool {
	return llmtypes.Tool{
		Function: &llmtypes.FunctionDefinition{
			Name:        "workspace_image_edit",
			Description: "Edit an existing image from the workspace using a text instruction. Provide the workspace path of a previously generated image and a prompt describing what to change. Saves the edited image to the workspace and displays it inline.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"image_path": map[string]interface{}{
						"type":        "string",
						"description": "Workspace path of the image to edit (e.g. 'Chats/generated-images/image-20260305-223629-1.png'). Use the saved_paths value from a prior workspace_image_gen result.",
					},
					"prompt": map[string]interface{}{
						"type":        "string",
						"description": "Instruction describing how to edit the image. Be explicit — describe the full desired result rather than relative changes.",
					},
					"aspect_ratio": map[string]interface{}{
						"type":        "string",
						"description": "Output aspect ratio. Defaults to the input image's ratio.",
						"enum":        []interface{}{"1:1", "2:3", "3:2", "3:4", "4:3", "9:16", "16:9", "21:9"},
					},
					"resolution": map[string]interface{}{
						"type":        "string",
						"description": "Output resolution. Defaults to '1K'.",
						"enum":        []interface{}{"1K", "2K", "4K"},
					},
				},
				"required": []interface{}{"image_path", "prompt"},
			}),
		},
	}
}

// CreateImageEditExecutor returns an executor that fetches an image from the workspace,
// edits it using the Gemini image model, and saves the result back to the workspace.
func CreateImageEditExecutor(cfg ImageGenExecutorConfig) func(ctx context.Context, args map[string]any) (string, error) {
	return func(ctx context.Context, args map[string]any) (string, error) {
		imagePath, _ := args["image_path"].(string)
		if imagePath == "" {
			return "", fmt.Errorf("image_path is required")
		}
		prompt, _ := args["prompt"].(string)
		if prompt == "" {
			return "", fmt.Errorf("prompt is required")
		}

		provider := cfg.Provider
		if provider == "" {
			provider = "vertex"
		}
		modelID := cfg.ModelID
		if modelID == "" {
			modelID = "gemini-2.5-flash-image"
		}

		var apiKeyPtr *string
		if cfg.APIKey != "" {
			k := cfg.APIKey
			apiKeyPtr = &k
		}

		providerAPIKeys := &llm.ProviderAPIKeys{}
		if provider == "minimax" {
			providerAPIKeys.MiniMax = apiKeyPtr
		} else {
			providerAPIKeys.Vertex = apiKeyPtr
		}

		imageGenCfg := llm.Config{
			Provider: llm.Provider(provider),
			ModelID:  modelID,
			APIKeys:  providerAPIKeys,
			Context:  ctx,
		}

		log.Printf("[IMAGE_EDIT] Initializing model: provider=%s model=%s apiKeyProvided=%v workspaceURL=%q userID=%q",
			provider, modelID, cfg.APIKey != "", cfg.WorkspaceAPIURL, cfg.UserID)

		model, err := llm.InitializeImageGenerationModel(imageGenCfg)
		if err != nil {
			log.Printf("[IMAGE_EDIT] Failed to initialize image generation model: %v", err)
			return "", fmt.Errorf("failed to initialize image generation model: %w", err)
		}
		log.Printf("[IMAGE_EDIT] Model initialized successfully")

		// Fetch the source image from workspace
		if cfg.WorkspaceAPIURL == "" {
			return "", fmt.Errorf("workspace API URL is required for image editing")
		}
		wsClient := workspace.NewClient(
			cfg.WorkspaceAPIURL,
			workspace.WithUserID(cfg.UserID),
		)
		log.Printf("[IMAGE_EDIT] Fetching source image from workspace: %s", imagePath)
		imgBytes, err := wsClient.DownloadFile(ctx, imagePath)
		if err != nil {
			return "", fmt.Errorf("failed to fetch source image from workspace: %w", err)
		}
		log.Printf("[IMAGE_EDIT] Fetched %d bytes from %s", len(imgBytes), imagePath)

		// Detect MIME type from file extension
		mimeType := "image/png"
		if len(imagePath) > 4 {
			switch imagePath[len(imagePath)-4:] {
			case ".jpg", "jpeg":
				mimeType = "image/jpeg"
			case "webp":
				mimeType = "image/webp"
			}
		}

		// Build options
		var opts []llmtypes.ImageGenerationOption
		log.Printf("[IMAGE_EDIT] Source image: %d bytes, detected mime=%s", len(imgBytes), mimeType)
		opts = append(opts, llmtypes.WithInputImage(imgBytes, mimeType))
		if ar, ok := args["aspect_ratio"].(string); ok && ar != "" {
			log.Printf("[IMAGE_EDIT] aspect_ratio=%s", ar)
			opts = append(opts, llmtypes.WithAspectRatio(ar))
		}
		if res, ok := args["resolution"].(string); ok && res != "" {
			log.Printf("[IMAGE_EDIT] resolution=%s", res)
			opts = append(opts, llmtypes.WithResolution(res))
		}

		log.Printf("[IMAGE_EDIT] Editing image: prompt=%q model=%s source=%s", prompt, modelID, imagePath)
		resp, err := model.GenerateImages(ctx, prompt, opts...)
		if err != nil {
			log.Printf("[IMAGE_EDIT] GenerateImages failed: %v", err)
			return "", fmt.Errorf("image editing failed: %w", err)
		}
		if len(resp.Images) == 0 {
			log.Printf("[IMAGE_EDIT] GenerateImages returned no images (filtered or unsupported)")
			return "", fmt.Errorf("image editing returned no images (prompt may have been filtered)")
		}
		log.Printf("[IMAGE_EDIT] Received %d edited image(s) from model", len(resp.Images))

		// Save edited images to workspace
		var savedPaths []string
		var fallbackImages []imageGenResultImage
		timestamp := time.Now().Format("20060102-150405")

		saveClient := workspace.NewClient(
			cfg.WorkspaceAPIURL,
			workspace.WithUserID(cfg.UserID),
			workspace.WithFolderGuard(&workspace.FolderGuardConfig{
				Enabled:    true,
				WritePaths: []string{"Chats/"},
			}),
		)
		if err := saveClient.CreateFolder(ctx, "Chats/generated-images"); err != nil {
			errStr := err.Error()
			if strings.Contains(errStr, "409") || strings.Contains(errStr, "already exists") {
				log.Printf("[IMAGE_EDIT] Chats/generated-images folder already exists, proceeding")
			} else {
				log.Printf("[IMAGE_EDIT] Warning: failed to create output folder: %v", err)
			}
		}

		for i, img := range resp.Images {
			imgMIME := img.MimeType
			if imgMIME == "" {
				imgMIME = "image/png"
			}
			ext := "png"
			if imgMIME == "image/jpeg" {
				ext = "jpg"
			} else if imgMIME == "image/webp" {
				ext = "webp"
			}
			fileName := fmt.Sprintf("edited-%s-%d.%s", timestamp, i+1, ext)
			savedPath, saveErr := saveClient.UploadBinary(ctx, "Chats/generated-images", fileName, img.Data)
			if saveErr != nil {
				log.Printf("[IMAGE_EDIT] Warning: failed to save edited image %d: %v", i+1, saveErr)
				fallbackImages = append(fallbackImages, imageGenResultImage{
					Data:     base64.StdEncoding.EncodeToString(img.Data),
					MIMEType: imgMIME,
				})
			} else {
				log.Printf("[IMAGE_EDIT] Saved edited image %d: %s", i+1, savedPath)
				savedPaths = append(savedPaths, savedPath)
			}
		}

		costPerImage := imageGenModelCosts[modelID]
		result := imageGenResult{
			Images:       fallbackImages,
			Model:        modelID,
			CostPerImage: costPerImage,
			Prompt:       prompt,
			SavedPaths:   savedPaths,
			Count:        len(resp.Images),
			Note:         "",
		}
		log.Printf("[IMAGE_EDIT] Done: saved=%d fallback=%d costPerImage=$%.4f", len(savedPaths), len(fallbackImages), costPerImage)
		resultJSON, err := json.Marshal(result)
		if err != nil {
			return "", fmt.Errorf("failed to marshal result: %w", err)
		}
		return string(resultJSON), nil
	}
}
