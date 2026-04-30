package virtualtools

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path"
	"strings"

	llm "github.com/manishiitg/multi-llm-provider-go"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"mcp-agent-builder-go/agent_go/pkg/workspace"
)

const (
	generateMusicToolName        = "generate_music"
	defaultMusicGenProvider      = "elevenlabs"
	defaultElevenLabsMusicModel  = "music_v1"
	defaultMiniMaxMusicModel     = "music-2.6"
	defaultElevenLabsMusicFormat = "mp3_44100_128"
)

var musicProviderModels = map[string][]string{
	"elevenlabs": {
		"music_v1",
	},
	"minimax": {
		"music-2.6",
		"music-2.6-free",
		"music-cover",
		"music-cover-free",
	},
}

type musicGenResult struct {
	Model         string           `json:"model"`
	Provider      string           `json:"provider"`
	Prompt        string           `json:"prompt,omitempty"`
	SavedPaths    []string         `json:"saved_paths,omitempty"`
	AbsolutePaths []string         `json:"absolute_paths,omitempty"`
	Count         int              `json:"count"`
	MimeType      string           `json:"mime_type,omitempty"`
	Metadata      []map[string]any `json:"metadata,omitempty"`
}

func inferMusicProviderFromModel(modelID string) string {
	modelID = strings.ToLower(strings.TrimSpace(modelID))
	switch {
	case modelID == "music_v1":
		return "elevenlabs"
	case strings.HasPrefix(modelID, "music-"):
		return "minimax"
	default:
		return ""
	}
}

func normalizeMusicProviderAndModel(provider, modelID string) (string, string, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	modelID = strings.TrimSpace(modelID)

	if provider == "" && modelID != "" {
		provider = inferMusicProviderFromModel(modelID)
	}
	if provider == "" {
		provider = defaultMusicGenProvider
	}
	switch provider {
	case "elevenlabs":
		if modelID == "" {
			modelID = defaultElevenLabsMusicModel
		}
		return provider, modelID, nil
	case "minimax":
		if modelID == "" {
			modelID = defaultMiniMaxMusicModel
		}
		return provider, modelID, nil
	default:
		return "", "", fmt.Errorf("unsupported music generation provider %q. %s", provider, supportedMusicProviderSummary())
	}
}

func supportedMusicProviderSummary() string {
	return "Supported music providers: elevenlabs (ElevenLabs Music model music_v1), minimax (MiniMax Music models music-2.6, music-2.6-free, music-cover, music-cover-free)"
}

func musicModelsSummaryForProvider(provider string) string {
	models := musicProviderModels[strings.ToLower(strings.TrimSpace(provider))]
	if len(models) == 0 {
		return supportedMusicProviderSummary()
	}
	return fmt.Sprintf("Supported music models for provider %q: %s", provider, strings.Join(models, ", "))
}

func wrapMusicGenerationSelectionError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf(
		"music generation setup is incomplete: %w. Add workspace provider auth with set_provider_auth(provider=\"elevenlabs\"|\"minimax\", api_key=\"...\") or configure ELEVENLABS_API_KEY / MINIMAX_API_KEY. %s",
		err,
		supportedMusicProviderSummary(),
	)
}

func wrapMusicGenerationInitializationError(provider, modelID string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf(
		"music generation could not start for provider %q and model %q: %w. To fix this, set workspace auth with set_provider_auth(provider=\"elevenlabs\"|\"minimax\", api_key=\"...\") or configure provider env auth. %s",
		provider, modelID, err, musicModelsSummaryForProvider(provider),
	)
}

func GetGenerateMusicToolDefinition() llmtypes.Tool {
	return llmtypes.Tool{
		Function: &llmtypes.FunctionDefinition{
			Name:        generateMusicToolName,
			Description: "Generate music using ElevenLabs Music or MiniMax Music. Requires a workspace-relative output_path. Defaults to ElevenLabs music_v1; pass provider=\"minimax\" to use MiniMax music-2.6.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"prompt": map[string]interface{}{
						"type":        "string",
						"description": "Music generation prompt describing genre, instrumentation, mood, structure, scenario, or lyrics direction. Required unless composition_plan is provided for ElevenLabs.",
					},
					"output_path": map[string]interface{}{
						"type":        "string",
						"description": "Required destination path for the generated music inside the workspace, e.g. 'Chats/generated-music/theme.mp3'. If multiple music items are returned, files are saved as '-1', '-2', etc. before the extension.",
					},
					"provider": map[string]interface{}{
						"type":        "string",
						"description": "Optional provider override. Supported values: elevenlabs, minimax.",
						"enum":        []interface{}{"elevenlabs", "minimax"},
					},
					"model_id": map[string]interface{}{
						"type":        "string",
						"description": "Optional model id. ElevenLabs default: music_v1. MiniMax examples: music-2.6, music-2.6-free.",
						"enum":        []interface{}{"music_v1", "music-2.6", "music-2.6-free", "music-cover", "music-cover-free"},
					},
					"duration_ms": map[string]interface{}{
						"type":        "number",
						"description": "Optional target duration in milliseconds. ElevenLabs supports 3000 to 600000 ms with prompt mode. MiniMax music duration is provider-controlled.",
					},
					"instrumental": map[string]interface{}{
						"type":        "boolean",
						"description": "Optional instrumental toggle. For ElevenLabs, true forces instrumental output. For MiniMax, omitted defaults to instrumental true.",
					},
					"lyrics": map[string]interface{}{
						"type":        "string",
						"description": "Optional lyrics for MiniMax non-instrumental generation.",
					},
					"lyrics_optimizer": map[string]interface{}{
						"type":        "boolean",
						"description": "Optional MiniMax toggle to generate or optimize lyrics from the prompt.",
					},
					"seed": map[string]interface{}{
						"type":        "number",
						"description": "Optional seed for deterministic generation when supported.",
					},
					"output_format": map[string]interface{}{
						"type":        "string",
						"description": "Optional provider-specific output format. ElevenLabs examples: mp3_44100_128, mp3_44100_192. MiniMax currently saves MP3 from hex output.",
					},
					"composition_plan": map[string]interface{}{
						"type":        "object",
						"description": "Optional ElevenLabs composition plan object. When provided, it is sent instead of prompt/music_length_ms.",
					},
				},
				"required": []interface{}{"output_path"},
			}),
		},
	}
}

func CreateMusicGenExecutor(cfg AudioGenExecutorConfig) func(ctx context.Context, args map[string]any) (string, error) {
	return func(ctx context.Context, args map[string]any) (string, error) {
		providerArg, _ := args["provider"].(string)
		modelIDArg, _ := args["model_id"].(string)
		provider, modelID, err := normalizeMusicProviderAndModel(providerArg, modelIDArg)
		if err != nil {
			return "", wrapMusicGenerationSelectionError(err)
		}

		prompt, _ := args["prompt"].(string)
		compositionPlan, _ := args["composition_plan"].(map[string]any)
		if strings.TrimSpace(prompt) == "" && len(compositionPlan) == 0 {
			return "", fmt.Errorf("prompt is required unless composition_plan is provided")
		}
		if len(compositionPlan) > 0 && provider != "elevenlabs" {
			return "", fmt.Errorf("composition_plan is only supported by provider \"elevenlabs\"")
		}

		outputPath, _ := args["output_path"].(string)
		outputPath = normalizeWorkspaceDocumentPath(outputPath)
		if strings.TrimSpace(outputPath) == "" {
			return "", fmt.Errorf("output_path is required")
		}
		if err := validateGuardedAudioOutputPath(ctx, cfg, outputPath); err != nil {
			return "", err
		}

		workspaceAPIKeys := loadWorkspaceProviderAPIKeys(ctx, cfg.WorkspaceAPIURL)
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
			if provider == "elevenlabs" {
				providerAPIKeys.ElevenLabs = apiKeyPtr
			} else {
				providerAPIKeys.MiniMax = apiKeyPtr
			}
		}

		musicGenCfg := llm.Config{
			Provider: llm.Provider(provider),
			ModelID:  modelID,
			APIKeys:  providerAPIKeys,
			Context:  ctx,
		}

		log.Printf("[MUSIC_GEN] Initializing model: provider=%s model=%s apiKeyProvided=%v workspaceURL=%q userID=%q",
			provider, modelID, cfg.APIKey != "", cfg.WorkspaceAPIURL, cfg.UserID)

		model, err := llm.InitializeMusicGenerationModel(musicGenCfg)
		if err != nil {
			log.Printf("[MUSIC_GEN] Failed to initialize music generation model: %v", err)
			return "", wrapMusicGenerationInitializationError(provider, modelID, err)
		}

		var opts []llmtypes.MusicGenerationOption
		if durationMS := int(numberFromAny(args["duration_ms"])); durationMS > 0 {
			opts = append(opts, llmtypes.WithMusicDurationMS(durationMS))
		}
		if instrumental, ok := args["instrumental"].(bool); ok {
			opts = append(opts, llmtypes.WithMusicInstrumental(instrumental))
		}
		if lyrics, _ := args["lyrics"].(string); strings.TrimSpace(lyrics) != "" {
			opts = append(opts, llmtypes.WithMusicLyrics(lyrics))
		}
		if lyricsOptimizer, ok := args["lyrics_optimizer"].(bool); ok {
			opts = append(opts, llmtypes.WithMusicLyricsOptimizer(lyricsOptimizer))
		}
		if seed := int(numberFromAny(args["seed"])); seed > 0 {
			opts = append(opts, llmtypes.WithMusicSeed(seed))
		}
		if outputFormat, _ := args["output_format"].(string); strings.TrimSpace(outputFormat) != "" {
			opts = append(opts, llmtypes.WithMusicOutputFormat(outputFormat))
		} else if provider == "elevenlabs" {
			opts = append(opts, llmtypes.WithMusicOutputFormat(defaultElevenLabsMusicFormat))
		}
		if len(compositionPlan) > 0 {
			opts = append(opts, llmtypes.WithMusicCompositionPlan(compositionPlan))
		}

		resp, err := model.GenerateMusic(ctx, prompt, opts...)
		if err != nil {
			return "", fmt.Errorf("music generation failed: %w", err)
		}
		if len(resp.Music) == 0 {
			return "", fmt.Errorf("music generation returned no audio")
		}

		var savedPaths []string
		var absolutePaths []string
		var metadata []map[string]any
		var resultMimeType string
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
				log.Printf("[MUSIC_GEN] %s folder already exists, proceeding", outputDir)
			} else {
				return "", fmt.Errorf("failed to prepare output folder %q: %w", outputDir, err)
			}
		}

		for i, music := range resp.Music {
			if len(music.Data) == 0 {
				return "", fmt.Errorf("generated music %d returned no bytes", i+1)
			}
			mimeType := music.MimeType
			if mimeType == "" {
				mimeType = "audio/mpeg"
			}
			if resultMimeType == "" {
				resultMimeType = mimeType
			}

			targetPaths, pathErr := resolveAudioOutputPaths(outputPath, len(resp.Music), mimeType)
			if pathErr != nil {
				return "", pathErr
			}
			targetPath := targetPaths[i]
			folderPath := path.Dir(targetPath)
			fileName := path.Base(targetPath)
			savedPath, saveErr := wsClient.UploadBinary(ctx, folderPath, fileName, music.Data)
			if saveErr != nil {
				return "", fmt.Errorf("failed to save generated music %d to workspace path %q: %w", i+1, targetPath, saveErr)
			}
			savedPaths = append(savedPaths, savedPath)
			absolutePaths = append(absolutePaths, workspaceAbsolutePath(savedPath))
			if music.Metadata != nil {
				metadata = append(metadata, music.Metadata)
			}
		}

		result := musicGenResult{
			Model:         modelID,
			Provider:      provider,
			Prompt:        prompt,
			SavedPaths:    savedPaths,
			AbsolutePaths: absolutePaths,
			Count:         len(resp.Music),
			MimeType:      resultMimeType,
			Metadata:      metadata,
		}
		resultJSON, err := json.Marshal(result)
		if err != nil {
			return "", fmt.Errorf("failed to marshal music generation result: %w", err)
		}

		return string(resultJSON), nil
	}
}

func numberFromAny(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		f, _ := v.Float64()
		return f
	default:
		return 0
	}
}

func CreateWorkspaceMusicTools() []llmtypes.Tool {
	return []llmtypes.Tool{
		GetGenerateMusicToolDefinition(),
	}
}

func CreateWorkspaceMusicToolExecutors(cfg AudioGenExecutorConfig) map[string]func(ctx context.Context, args map[string]any) (string, error) {
	generateMusicExecutor := CreateMusicGenExecutor(cfg)
	return map[string]func(ctx context.Context, args map[string]any) (string, error){
		GetGenerateMusicToolDefinition().Function.Name: generateMusicExecutor,
	}
}

func MergeMusicToolExecutors(cfg AudioGenExecutorConfig, executors map[string]func(ctx context.Context, args map[string]any) (string, error), categories map[string]string) {
	cat := GetWorkspaceAdvancedToolCategory()
	for name, exec := range CreateWorkspaceMusicToolExecutors(cfg) {
		executors[name] = exec
		if categories != nil {
			categories[name] = cat
		}
	}
}

func MergeMusicToolExecutorsUntyped(cfg AudioGenExecutorConfig, executors map[string]any, categories map[string]string) {
	cat := GetWorkspaceAdvancedToolCategory()
	for name, exec := range CreateWorkspaceMusicToolExecutors(cfg) {
		executors[name] = exec
		if categories != nil {
			categories[name] = cat
		}
	}
}
