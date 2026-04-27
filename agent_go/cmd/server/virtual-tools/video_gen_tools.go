package virtualtools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"

	llm "github.com/manishiitg/multi-llm-provider-go"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"mcp-agent-builder-go/agent_go/pkg/fsutil"
	"mcp-agent-builder-go/agent_go/pkg/workspace"
)

var videoProviderModels = map[string][]string{
	"vertex": {
		"veo-3.1-generate-001",
		"veo-3.1-lite-generate-001",
		"veo-3.1-fast-generate-001",
		"veo-3.1-generate-preview",
		"veo-3.1-fast-generate-preview",
	},
}

type videoPricing struct {
	Rate720  float64
	Rate1080 float64
}

var videoModelPricing = map[string]videoPricing{
	"veo-3.1-generate-001":          {Rate720: 0.40, Rate1080: 0.40},
	"veo-3.1-generate-preview":      {Rate720: 0.40, Rate1080: 0.40},
	"veo-3.1-lite-generate-001":     {Rate720: 0.05, Rate1080: 0.08},
	"veo-3.1-fast-generate-001":     {Rate720: 0.10, Rate1080: 0.12},
	"veo-3.1-fast-generate-preview": {Rate720: 0.10, Rate1080: 0.12},
}

type VideoGenExecutorConfig struct {
	Provider        string
	ModelID         string
	APIKey          string
	WorkspaceAPIURL string
	UserID          string
}

const generateVideoToolName = "generate_video"

type videoGenResult struct {
	Model                 string   `json:"model"`
	Prompt                string   `json:"prompt"`
	SavedPaths            []string `json:"saved_paths,omitempty"`
	AbsolutePaths         []string `json:"absolute_paths,omitempty"`
	Count                 int      `json:"count"`
	FilteredCount         int      `json:"filtered_count,omitempty"`
	FilterReasons         []string `json:"filter_reasons,omitempty"`
	DurationSeconds       int      `json:"duration_seconds,omitempty"`
	Resolution            string   `json:"resolution,omitempty"`
	EstimatedCostPerVideo float64  `json:"estimated_cost_per_video,omitempty"`
	EstimatedCostTotal    float64  `json:"estimated_cost_total,omitempty"`
}

const defaultVideoGenProvider = "vertex"

func inferVideoProviderFromModel(modelID string) string {
	modelID = strings.ToLower(strings.TrimSpace(modelID))
	switch {
	case strings.HasPrefix(modelID, "veo-"):
		return "vertex"
	default:
		return ""
	}
}

func normalizeVideoProviderAndModel(provider, modelID string) (string, string, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	modelID = strings.TrimSpace(modelID)

	if modelID == "" {
		return "", "", fmt.Errorf("model_id is required. %s", supportedVideoProviderSummary())
	}
	if provider == "" {
		provider = inferVideoProviderFromModel(modelID)
	}
	if provider == "" {
		provider = defaultVideoGenProvider
	}

	switch provider {
	case "vertex":
		return provider, modelID, nil
	default:
		return "", "", fmt.Errorf("unsupported video generation provider %q. %s", provider, supportedVideoProviderSummary())
	}
}

func supportedVideoProviderSummary() string {
	return "Supported video providers: vertex (Gemini API preview models such as veo-3.1-generate-preview with API-key auth, and Vertex AI models such as veo-3.1-generate-001 / veo-3.1-lite-generate-001 with project-based auth)"
}

func videoModelsSummaryForProvider(provider string) string {
	models := videoProviderModels[strings.ToLower(strings.TrimSpace(provider))]
	if len(models) == 0 {
		return supportedVideoProviderSummary()
	}
	return fmt.Sprintf("Supported models for provider %q: %s", provider, strings.Join(models, ", "))
}

func wrapVideoGenerationSelectionError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf(
		"video generation setup is incomplete: %w. Add workspace provider auth with set_provider_auth(provider=\"vertex\", api_key=\"...\") for Gemini API preview models, or configure Vertex AI auth with GOOGLE_CLOUD_PROJECT / VERTEX_PROJECT_ID and ADC for GA Veo models. %s",
		err,
		supportedVideoProviderSummary(),
	)
}

func wrapVideoGenerationInitializationError(provider, modelID string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf(
		"video generation could not start for provider %q and model %q: %w. To fix this, set workspace auth with set_provider_auth(provider=\"vertex\", api_key=\"...\") for preview models, or configure Vertex AI project auth for GA Veo models. %s",
		provider, modelID, err, videoModelsSummaryForProvider(provider),
	)
}

func videoExtensionForMIME(mimeType string) string {
	switch mimeType {
	case "video/webm":
		return "webm"
	case "video/quicktime", "video/mov":
		return "mov"
	default:
		return "mp4"
	}
}

func normalizeWorkspaceDocumentPath(inputPath string) string {
	trimmed := strings.TrimSpace(inputPath)
	if trimmed == "" {
		return ""
	}
	if !filepath.IsAbs(trimmed) {
		return path.Clean(filepath.ToSlash(trimmed))
	}

	cleanAbs := filepath.Clean(trimmed)
	prefixes := []string{
		filepath.Clean(fsutil.WorkspaceDocsRoot()),
		filepath.Clean("/app/workspace-docs"),
		filepath.Clean("/workspace-docs"),
	}
	for _, prefix := range prefixes {
		if prefix == "" {
			continue
		}
		if cleanAbs == prefix {
			return "."
		}
		if strings.HasPrefix(cleanAbs, prefix+string(os.PathSeparator)) {
			rel, err := filepath.Rel(prefix, cleanAbs)
			if err == nil {
				return path.Clean(filepath.ToSlash(rel))
			}
		}
	}
	return path.Clean(filepath.ToSlash(trimmed))
}

func workspaceAbsolutePath(relativePath string) string {
	return filepath.Join(fsutil.WorkspaceDocsRoot(), filepath.FromSlash(path.Clean(relativePath)))
}

func resolveVideoOutputPaths(outputPath string, count int, mimeType string) ([]string, error) {
	cleanPath := path.Clean(strings.TrimSpace(outputPath))
	if cleanPath == "" || cleanPath == "." {
		return nil, fmt.Errorf("output_path is required")
	}
	if strings.HasPrefix(cleanPath, "/") {
		return nil, fmt.Errorf("output_path must be workspace-relative, not absolute")
	}
	if cleanPath == ".." || strings.HasPrefix(cleanPath, "../") {
		return nil, fmt.Errorf("output_path must stay inside the workspace")
	}

	dir := path.Dir(cleanPath)
	if dir == "." || dir == "" {
		return nil, fmt.Errorf("output_path must include a workspace folder, e.g. Chats/... or Workflow/...")
	}

	ext := "." + videoExtensionForMIME(mimeType)
	baseWithoutExt := strings.TrimSuffix(cleanPath, path.Ext(cleanPath))
	if baseWithoutExt == "" {
		return nil, fmt.Errorf("output_path must include a file name")
	}

	if count <= 1 {
		return []string{baseWithoutExt + ext}, nil
	}

	paths := make([]string, 0, count)
	for i := 1; i <= count; i++ {
		paths = append(paths, fmt.Sprintf("%s-%d%s", baseWithoutExt, i, ext))
	}
	return paths, nil
}

func validateGuardedVideoOutputPath(ctx context.Context, cfg VideoGenExecutorConfig, outputPath string) error {
	cleanOutputPath := path.Clean(strings.TrimSpace(outputPath))
	if _, err := resolveVideoOutputPaths(cleanOutputPath, 1, "video/mp4"); err != nil {
		return err
	}
	if cfg.WorkspaceAPIURL == "" {
		return fmt.Errorf("video generation requires a workspace API URL so output_path can be saved in the workspace")
	}

	guardClient := workspace.NewClient(
		cfg.WorkspaceAPIURL,
		workspace.WithUserID(cfg.UserID),
	)
	if !guardClient.HasEffectiveWriteGuard(ctx) {
		return fmt.Errorf("output_path must stay inside the current session's writable folder, but no active session/workflow write guard was found")
	}

	outputDir := path.Dir(cleanOutputPath)
	if err := guardClient.ValidatePathWithContext(ctx, outputDir, true); err != nil {
		return fmt.Errorf("output_path directory is outside the current session's writable folder: %w", err)
	}
	if err := guardClient.ValidatePathWithContext(ctx, cleanOutputPath, true); err != nil {
		return fmt.Errorf("output_path is outside the current session's writable folder: %w", err)
	}
	return nil
}

func applyVideoGenToolArgs(cfg VideoGenExecutorConfig, args map[string]any) VideoGenExecutorConfig {
	if provider, ok := args["provider"].(string); ok && strings.TrimSpace(provider) != "" {
		cfg.Provider = strings.TrimSpace(provider)
		cfg.ModelID = ""
	}
	if modelID, ok := args["model_id"].(string); ok && strings.TrimSpace(modelID) != "" {
		cfg.ModelID = strings.TrimSpace(modelID)
	}
	return cfg
}

func resolveVideoGenerationTarget(ctx context.Context, cfg VideoGenExecutorConfig) (string, string, *llm.ProviderAPIKeys, error) {
	apiKeys := loadWorkspaceProviderAPIKeys(ctx, cfg.WorkspaceAPIURL)
	provider, modelID, err := normalizeVideoProviderAndModel(cfg.Provider, cfg.ModelID)
	return provider, modelID, apiKeys, err
}

func getVideoGenToolDefinition(toolName string) llmtypes.Tool {
	return llmtypes.Tool{
		Function: &llmtypes.FunctionDefinition{
			Name:        toolName,
			Description: "Generate videos using AI from a text prompt. Requires an output_path inside the workspace and a model_id (the model determines the Google backend). Veo 3 models include native audio in the output by default. Supports image-to-video generation, aspect ratio, resolution, duration, number of videos, and negative prompt.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"prompt": map[string]interface{}{
						"type":        "string",
						"description": "Text prompt describing the video to generate. Veo 3 also generates audio from this same prompt — there is no separate audio parameter. Use these conventions to direct the soundstage: (1) Dialogue: put speech in double quotes, e.g. A woman says, \"We have to leave now.\" Parentheticals before the quote shape delivery, e.g. (whispering), (voice tight with fear); they are not spoken. (2) Sound effects: prefix with 'SFX:', e.g. SFX: thunder cracks in the distance. (3) Ambient noise: prefix with 'Ambient noise:', e.g. Ambient noise: the quiet hum of a starship bridge. Place audio cues next to the visual beat they sync with rather than only at the top. For multi-shot prompts, bracket each beat with timestamps like [00:02-00:04] and place SFX/dialogue inside that beat. To minimize unwanted audio, put words like 'silence', 'no music', or 'no dialogue' in negative_prompt — there is no off switch on the Gemini API path.",
					},
					"output_path": map[string]interface{}{
						"type":        "string",
						"description": "Required destination path for the generated video inside the workspace. Can be workspace-relative like 'Chats/generated-videos/scene.mp4' or an absolute workspace path like '/app/workspace-docs/Chats/generated-videos/scene.mp4'. If multiple videos are returned, this path is used as the base name and files are saved as '-1', '-2', etc. before the extension.",
					},
					"provider": map[string]interface{}{
						"type":        "string",
						"description": "Optional provider override. Supported values: vertex.",
					},
					"model_id": map[string]interface{}{
						"type":        "string",
						"description": "Required Veo model id. The model determines the Google backend. Gemini API backend (requires GEMINI_API_KEY/VERTEX_API_KEY): veo-3.1-generate-preview, veo-3.1-fast-generate-preview. Vertex AI backend (requires GOOGLE_CLOUD_PROJECT + ADC): veo-3.1-generate-001, veo-3.1-lite-generate-001, veo-3.1-fast-generate-001. All Veo 3 models include native audio in the output by default.",
						"enum":        []interface{}{"veo-3.1-generate-001", "veo-3.1-lite-generate-001", "veo-3.1-fast-generate-001", "veo-3.1-generate-preview", "veo-3.1-fast-generate-preview"},
					},
					"input_image": map[string]interface{}{
						"type":        "string",
						"description": "Optional base64-encoded image to use as the first frame for image-to-video generation.",
					},
					"input_image_path": map[string]interface{}{
						"type":        "string",
						"description": "Optional workspace image path to use as the first frame for image-to-video generation. Can be workspace-relative like 'Chats/images/start.png' or an absolute workspace path like '/app/workspace-docs/Chats/images/start.png'.",
					},
					"input_image_mime_type": map[string]interface{}{
						"type":        "string",
						"description": "MIME type of the input image (e.g. 'image/png', 'image/jpeg'). Defaults to 'image/png'.",
					},
					"aspect_ratio": map[string]interface{}{
						"type":        "string",
						"description": "The aspect ratio for the generated video.",
						"enum":        []interface{}{"16:9", "9:16"},
					},
					"resolution": map[string]interface{}{
						"type":        "string",
						"description": "Output video resolution. Model-dependent.",
						"enum":        []interface{}{"720p", "1080p"},
					},
					"duration_seconds": map[string]interface{}{
						"type":        "integer",
						"description": "Requested video duration in seconds. Supported values depend on the selected Veo model.",
						"minimum":     4,
						"maximum":     8,
					},
					"number_of_videos": map[string]interface{}{
						"type":        "integer",
						"description": "Number of videos to generate. Supported range depends on the selected Veo model.",
						"minimum":     1,
						"maximum":     4,
					},
					"negative_prompt": map[string]interface{}{
						"type":        "string",
						"description": "Optional description of what to exclude from the generated videos.",
					},
					"person_generation": map[string]interface{}{
						"type":        "string",
						"description": "Optional people-generation safety policy.",
						"enum":        []interface{}{"allow_adult", "dont_allow"},
					},
					"seed": map[string]interface{}{
						"type":        "integer",
						"description": "Optional deterministic seed.",
						"minimum":     0,
					},
				},
				"required": []interface{}{"prompt", "output_path", "model_id"},
			}),
		},
	}
}

func GetGenerateVideoToolDefinition() llmtypes.Tool {
	return getVideoGenToolDefinition(generateVideoToolName)
}

func CreateVideoGenExecutor(cfg VideoGenExecutorConfig) func(ctx context.Context, args map[string]any) (string, error) {
	return func(ctx context.Context, args map[string]any) (string, error) {
		cfg = applyVideoGenToolArgs(cfg, args)

		prompt, _ := args["prompt"].(string)
		if prompt == "" {
			return "", fmt.Errorf("prompt is required")
		}
		outputPath, _ := args["output_path"].(string)
		outputPath = normalizeWorkspaceDocumentPath(outputPath)
		if strings.TrimSpace(outputPath) == "" {
			return "", fmt.Errorf("output_path is required")
		}
		if rawModelID, _ := args["model_id"].(string); strings.TrimSpace(rawModelID) == "" {
			return "", fmt.Errorf("model_id is required. %s", supportedVideoProviderSummary())
		}
		if err := validateGuardedVideoOutputPath(ctx, cfg, outputPath); err != nil {
			return "", err
		}

		provider, modelID, workspaceAPIKeys, err := resolveVideoGenerationTarget(ctx, cfg)
		if err != nil {
			return "", wrapVideoGenerationSelectionError(err)
		}

		var apiKeyPtr *string
		if cfg.APIKey != "" {
			k := cfg.APIKey
			apiKeyPtr = &k
		}

		providerAPIKeys := workspaceAPIKeys
		if providerAPIKeys == nil {
			providerAPIKeys = &llm.ProviderAPIKeys{}
		}
		if apiKeyPtr != nil {
			providerAPIKeys.Vertex = apiKeyPtr
		}

		videoGenCfg := llm.Config{
			Provider: llm.Provider(provider),
			ModelID:  modelID,
			APIKeys:  providerAPIKeys,
			Context:  ctx,
		}

		log.Printf("[VIDEO_GEN] Initializing model: provider=%s model=%s apiKeyProvided=%v workspaceURL=%q userID=%q",
			provider, modelID, cfg.APIKey != "", cfg.WorkspaceAPIURL, cfg.UserID)

		model, err := llm.InitializeVideoGenerationModel(videoGenCfg)
		if err != nil {
			log.Printf("[VIDEO_GEN] Failed to initialize video generation model: %v", err)
			return "", wrapVideoGenerationInitializationError(provider, modelID, err)
		}

		var opts []llmtypes.VideoGenerationOption
		durationSeconds := 0
		resolution := ""
		normalizedInputImagePath := ""
		if ar, ok := args["aspect_ratio"].(string); ok && ar != "" {
			opts = append(opts, llmtypes.WithVideoAspectRatio(ar))
		}
		if res, ok := args["resolution"].(string); ok && res != "" {
			resolution = res
			opts = append(opts, llmtypes.WithVideoResolution(res))
		}
		if n, ok := args["number_of_videos"].(float64); ok && n >= 1 {
			opts = append(opts, llmtypes.WithVideoNumberOfVideos(int(n)))
		}
		if np, ok := args["negative_prompt"].(string); ok && np != "" {
			opts = append(opts, llmtypes.WithVideoNegativePrompt(np))
		}
		if d, ok := args["duration_seconds"].(float64); ok && d >= 1 {
			durationSeconds = int(d)
			opts = append(opts, llmtypes.WithVideoDurationSeconds(durationSeconds))
		}
		if policy, ok := args["person_generation"].(string); ok && policy != "" {
			opts = append(opts, llmtypes.WithVideoPersonGeneration(policy))
		}
		if seed, ok := args["seed"].(float64); ok && seed >= 0 {
			opts = append(opts, llmtypes.WithVideoSeed(int32(seed)))
		}
		if inputImagePath, ok := args["input_image_path"].(string); ok && strings.TrimSpace(inputImagePath) != "" {
			normalizedInputImagePath = normalizeWorkspaceDocumentPath(inputImagePath)
		}
		if inputImageB64, ok := args["input_image"].(string); ok && inputImageB64 != "" && normalizedInputImagePath != "" {
			return "", fmt.Errorf("provide either input_image or input_image_path, not both")
		}
		if normalizedInputImagePath != "" {
			if cfg.WorkspaceAPIURL == "" {
				return "", fmt.Errorf("workspace API URL is required for input_image_path")
			}
			wsReadClient := workspace.NewClient(
				cfg.WorkspaceAPIURL,
				workspace.WithUserID(cfg.UserID),
			)
			if err := wsReadClient.ValidatePathWithContext(ctx, normalizedInputImagePath, false); err != nil {
				return "", fmt.Errorf("input_image_path is outside the current session's readable folder: %w", err)
			}
			imgBytes, err := wsReadClient.DownloadFile(ctx, normalizedInputImagePath)
			if err != nil {
				return "", fmt.Errorf("failed to fetch input image from workspace: %w", err)
			}
			mimeType, _ := args["input_image_mime_type"].(string)
			if mimeType == "" {
				switch strings.ToLower(path.Ext(normalizedInputImagePath)) {
				case ".jpg", ".jpeg":
					mimeType = "image/jpeg"
				case ".webp":
					mimeType = "image/webp"
				default:
					mimeType = "image/png"
				}
			}
			opts = append(opts, llmtypes.WithVideoInputImage(imgBytes, mimeType))
		} else if inputImageB64, ok := args["input_image"].(string); ok && inputImageB64 != "" {
			imgBytes, err := base64.StdEncoding.DecodeString(inputImageB64)
			if err != nil {
				return "", fmt.Errorf("invalid input_image base64: %w", err)
			}
			mimeType, _ := args["input_image_mime_type"].(string)
			if mimeType == "" {
				mimeType = "image/png"
			}
			opts = append(opts, llmtypes.WithVideoInputImage(imgBytes, mimeType))
		}

		resp, err := model.GenerateVideos(ctx, prompt, opts...)
		if err != nil {
			return "", fmt.Errorf("video generation failed: %w", err)
		}
		if len(resp.Videos) == 0 {
			return "", fmt.Errorf("video generation returned no videos (prompt may have been filtered by safety filters)")
		}

		var savedPaths []string
		var absolutePaths []string
		cleanOutputPath := path.Clean(strings.TrimSpace(outputPath))
		outputDir := path.Dir(cleanOutputPath)
		wsClient := workspace.NewClient(
			cfg.WorkspaceAPIURL,
			workspace.WithUserID(cfg.UserID),
			workspace.WithFolderGuard(&workspace.FolderGuardConfig{
				Enabled:    true,
				WritePaths: []string{outputDir + "/"},
			}),
		)
		if err := wsClient.CreateFolder(ctx, outputDir); err != nil {
			errStr := err.Error()
			if strings.Contains(errStr, "409") || strings.Contains(errStr, "already exists") {
				log.Printf("[VIDEO_GEN] %s folder already exists, proceeding", outputDir)
			} else {
				return "", fmt.Errorf("failed to prepare output folder %q: %w", outputDir, err)
			}
		}

		for i, video := range resp.Videos {
			if len(video.Data) == 0 {
				return "", fmt.Errorf("generated video %d returned no downloadable bytes", i+1)
			}
			mimeType := video.MimeType
			if mimeType == "" {
				mimeType = "video/mp4"
			}

			targetPaths, pathErr := resolveVideoOutputPaths(outputPath, len(resp.Videos), mimeType)
			if pathErr != nil {
				return "", pathErr
			}
			targetPath := targetPaths[i]
			folderPath := path.Dir(targetPath)
			fileName := path.Base(targetPath)
			savedPath, saveErr := wsClient.UploadBinary(ctx, folderPath, fileName, video.Data)
			if saveErr != nil {
				return "", fmt.Errorf("failed to save generated video %d to workspace path %q: %w", i+1, targetPath, saveErr)
			}
			savedPaths = append(savedPaths, savedPath)
			absolutePaths = append(absolutePaths, workspaceAbsolutePath(savedPath))
		}

		estimatedCostPerVideo, estimatedCostTotal := estimateVideoGenerationCost(modelID, resolution, durationSeconds, len(resp.Videos))

		result := videoGenResult{
			Model:                 modelID,
			Prompt:                prompt,
			SavedPaths:            savedPaths,
			AbsolutePaths:         absolutePaths,
			Count:                 len(resp.Videos),
			FilteredCount:         resp.FilteredCount,
			FilterReasons:         resp.FilterReasons,
			DurationSeconds:       durationSeconds,
			Resolution:            resolution,
			EstimatedCostPerVideo: estimatedCostPerVideo,
			EstimatedCostTotal:    estimatedCostTotal,
		}

		resultJSON, err := json.Marshal(result)
		if err != nil {
			return "", fmt.Errorf("failed to marshal video generation result: %w", err)
		}

		return string(resultJSON), nil
	}
}

func CreateWorkspaceVideoTools() []llmtypes.Tool {
	return []llmtypes.Tool{
		GetGenerateVideoToolDefinition(),
	}
}

func CreateWorkspaceVideoToolExecutors(cfg VideoGenExecutorConfig) map[string]func(ctx context.Context, args map[string]any) (string, error) {
	executor := CreateVideoGenExecutor(cfg)
	return map[string]func(ctx context.Context, args map[string]any) (string, error){
		GetGenerateVideoToolDefinition().Function.Name: executor,
	}
}

func MergeVideoToolExecutors(cfg VideoGenExecutorConfig, executors map[string]func(ctx context.Context, args map[string]any) (string, error), categories map[string]string) {
	cat := GetWorkspaceAdvancedToolCategory()
	for name, exec := range CreateWorkspaceVideoToolExecutors(cfg) {
		executors[name] = exec
		if categories != nil {
			categories[name] = cat
		}
	}
}

func MergeVideoToolExecutorsUntyped(cfg VideoGenExecutorConfig, executors map[string]any, categories map[string]string) {
	cat := GetWorkspaceAdvancedToolCategory()
	for name, exec := range CreateWorkspaceVideoToolExecutors(cfg) {
		executors[name] = exec
		if categories != nil {
			categories[name] = cat
		}
	}
}

func estimateVideoGenerationCost(modelID, resolution string, durationSeconds int, count int) (float64, float64) {
	pricing, ok := videoModelPricing[modelID]
	if !ok || durationSeconds <= 0 || count <= 0 {
		return 0, 0
	}

	resolution = strings.TrimSpace(strings.ToLower(resolution))
	if resolution == "" {
		resolution = "720p"
	}

	var rate float64
	switch resolution {
	case "1080p":
		rate = pricing.Rate1080
	default:
		rate = pricing.Rate720
	}

	perVideo := rate * float64(durationSeconds)
	return perVideo, perVideo * float64(count)
}
