package virtualtools

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path"
	"path/filepath"
	"strings"

	llm "github.com/manishiitg/multi-llm-provider-go"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/workspace"
)

const (
	textToSpeechToolName     = "text_to_speech"
	speechToTextToolName     = "speech_to_text"
	defaultAudioGenProvider  = "vertex"
	defaultAudioGenModelID   = "gemini-3.1-flash-tts-preview"
	defaultMiniMaxModelID    = "speech-2.8-turbo"
	defaultElevenLabsModelID = "eleven_multilingual_v2"
	defaultDeepgramModelID   = "aura-2-thalia-en"
	defaultDeepgramSTTModel  = "nova-3"
)

var audioProviderModels = map[string][]string{
	"vertex": {
		"gemini-3.1-flash-tts-preview",
	},
	"minimax": {
		"speech-2.8-turbo",
		"speech-2.8-hd",
		"speech-2.6-turbo",
		"speech-2.6-hd",
		"speech-02-turbo",
		"speech-02-hd",
	},
	"elevenlabs": {
		"eleven_multilingual_v2",
		"eleven_turbo_v2_5",
		"eleven_flash_v2_5",
		"eleven_v3",
	},
	"deepgram": {
		"aura-2-thalia-en",
		"aura-2-luna-en",
		"aura-2-asteria-en",
		"aura-2-apollo-en",
	},
}

type AudioGenExecutorConfig struct {
	Provider        string
	ModelID         string
	APIKey          string
	WorkspaceAPIURL string
	UserID          string
}

type audioGenResult struct {
	Model         string   `json:"model"`
	Provider      string   `json:"provider"`
	Prompt        string   `json:"prompt"`
	VoiceName     string   `json:"voice_name,omitempty"`
	LanguageCode  string   `json:"language_code,omitempty"`
	SavedPaths    []string `json:"saved_paths,omitempty"`
	AbsolutePaths []string `json:"absolute_paths,omitempty"`
	Count         int      `json:"count"`
	MimeType      string   `json:"mime_type,omitempty"`
	CostUnit      string   `json:"cost_unit,omitempty"`
	CostQuantity  float64  `json:"cost_quantity,omitempty"`
	TotalCost     float64  `json:"total_cost_usd,omitempty"`
	CostEstimated bool     `json:"cost_estimated,omitempty"`
}

type audioTranscriptionResult struct {
	Model         string  `json:"model"`
	Provider      string  `json:"provider"`
	AudioPath     string  `json:"audio_path"`
	Transcript    string  `json:"transcript"`
	Confidence    float64 `json:"confidence,omitempty"`
	Duration      float64 `json:"duration,omitempty"`
	MimeType      string  `json:"mime_type,omitempty"`
	CostUnit      string  `json:"cost_unit,omitempty"`
	CostQuantity  float64 `json:"cost_quantity,omitempty"`
	TotalCost     float64 `json:"total_cost_usd,omitempty"`
	CostEstimated bool    `json:"cost_estimated,omitempty"`
}

func inferAudioProviderFromModel(modelID string) string {
	modelID = strings.ToLower(strings.TrimSpace(modelID))
	switch {
	case strings.HasPrefix(modelID, "gemini-"):
		return "vertex"
	case strings.HasPrefix(modelID, "speech-"):
		return "minimax"
	case strings.HasPrefix(modelID, "eleven_"):
		return "elevenlabs"
	case strings.HasPrefix(modelID, "aura-"):
		return "deepgram"
	default:
		return ""
	}
}

func normalizeAudioProviderAndModel(provider, modelID string) (string, string, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	modelID = strings.TrimSpace(modelID)

	if provider == "" && modelID != "" {
		provider = inferAudioProviderFromModel(modelID)
	}
	if provider == "" {
		provider = defaultAudioGenProvider
	}
	switch provider {
	case "vertex":
		if modelID == "" {
			modelID = defaultAudioGenModelID
		}
		return provider, modelID, nil
	case "minimax":
		if modelID == "" {
			modelID = defaultMiniMaxModelID
		}
		return provider, modelID, nil
	case "elevenlabs":
		if modelID == "" {
			modelID = defaultElevenLabsModelID
		}
		return provider, modelID, nil
	case "deepgram":
		if modelID == "" {
			modelID = defaultDeepgramModelID
		}
		return provider, modelID, nil
	default:
		return "", "", fmt.Errorf("unsupported audio generation provider %q. %s", provider, supportedAudioProviderSummary())
	}
}

func supportedAudioProviderSummary() string {
	return "Supported audio providers: vertex (Gemini API TTS model gemini-3.1-flash-tts-preview with API-key auth), minimax (MiniMax T2A models such as speech-2.8-turbo, speech-2.8-hd), elevenlabs (ElevenLabs TTS models such as eleven_multilingual_v2, eleven_turbo_v2_5, eleven_flash_v2_5, eleven_v3), deepgram (Aura TTS models such as aura-2-thalia-en)"
}

func audioModelsSummaryForProvider(provider string) string {
	models := audioProviderModels[strings.ToLower(strings.TrimSpace(provider))]
	if len(models) == 0 {
		return supportedAudioProviderSummary()
	}
	return fmt.Sprintf("Supported models for provider %q: %s", provider, strings.Join(models, ", "))
}

func wrapAudioGenerationSelectionError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf(
		"audio generation setup is incomplete: %w. Add workspace provider auth with set_provider_auth(provider=\"vertex\"|\"minimax\"|\"elevenlabs\"|\"deepgram\", api_key=\"...\") or configure GEMINI_API_KEY / MINIMAX_API_KEY / ELEVENLABS_API_KEY / DEEPGRAM_API_KEY. %s",
		err,
		supportedAudioProviderSummary(),
	)
}

func wrapAudioGenerationInitializationError(provider, modelID string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf(
		"audio generation could not start for provider %q and model %q: %w. To fix this, set workspace auth with set_provider_auth(provider=\"vertex\"|\"minimax\"|\"elevenlabs\"|\"deepgram\", api_key=\"...\") or configure provider env auth. %s",
		provider, modelID, err, audioModelsSummaryForProvider(provider),
	)
}

func audioExtensionForMIME(mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "audio/mpeg", "audio/mp3", "audio/mpeg; charset=utf-8":
		return "mp3"
	case "audio/ogg", "audio/opus", "audio/ogg; codecs=opus":
		return "ogg"
	case "audio/wav", "audio/wave", "audio/x-wav":
		return "wav"
	case "audio/pcm", "audio/l16":
		return "pcm"
	default:
		return "wav"
	}
}

func resolveAudioOutputPaths(outputPath string, count int, mimeType string) ([]string, error) {
	cleanPath := path.Clean(strings.TrimSpace(outputPath))
	if cleanPath == "" || cleanPath == "." {
		return nil, fmt.Errorf("output_path is required")
	}
	if strings.HasPrefix(cleanPath, "/") {
		return nil, fmt.Errorf("output_path must be normalized under the workspace docs root")
	}
	if cleanPath == ".." || strings.HasPrefix(cleanPath, "../") {
		return nil, fmt.Errorf("output_path must stay inside the workspace")
	}

	dir := path.Dir(cleanPath)
	if dir == "." || dir == "" {
		return nil, fmt.Errorf("output_path must include a workspace folder, e.g. Chats/... or Workflow/...")
	}

	ext := "." + audioExtensionForMIME(mimeType)
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

func validateGuardedAudioOutputPath(ctx context.Context, cfg AudioGenExecutorConfig, outputPath string) error {
	cleanOutputPath := path.Clean(strings.TrimSpace(outputPath))
	if _, err := resolveAudioOutputPaths(cleanOutputPath, 1, "audio/wav"); err != nil {
		return err
	}
	if cfg.WorkspaceAPIURL == "" {
		return fmt.Errorf("audio generation requires a workspace API URL so output_path can be saved in the workspace")
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

func applyAudioGenToolArgs(cfg AudioGenExecutorConfig, args map[string]any) AudioGenExecutorConfig {
	if provider, ok := args["provider"].(string); ok && strings.TrimSpace(provider) != "" {
		cfg.Provider = strings.TrimSpace(provider)
		cfg.ModelID = ""
	}
	if modelID, ok := args["model_id"].(string); ok && strings.TrimSpace(modelID) != "" {
		cfg.ModelID = strings.TrimSpace(modelID)
	}
	return cfg
}

func resolveAudioGenerationTarget(ctx context.Context, cfg AudioGenExecutorConfig) (string, string, *llm.ProviderAPIKeys, error) {
	apiKeys := loadWorkspaceProviderAPIKeys(ctx, cfg.WorkspaceAPIURL)
	provider, modelID, err := normalizeAudioProviderAndModel(cfg.Provider, cfg.ModelID)
	return provider, modelID, apiKeys, err
}

func GetTextToSpeechToolDefinition() llmtypes.Tool {
	return llmtypes.Tool{
		Function: &llmtypes.FunctionDefinition{
			Name:        textToSpeechToolName,
			Description: "Generate text-to-speech audio using Gemini TTS, MiniMax, ElevenLabs, or Deepgram. Requires a full absolute output_path under the workspace docs root. Before choosing provider/model_id, call list_llm_capabilities(capability=\"text_to_speech\", include_models=true). If you pass model_id, also pass the matching provider from that capability result; do not pass model_id by itself. Defaults to Gemini gemini-3.1-flash-tts-preview.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"prompt": map[string]interface{}{
						"type":        "string",
						"description": "Text and direction for the speech generation. Include style, pace, tone, accent, and the exact transcript to be spoken. For multi-speaker generation, include speaker names in the prompt that match speaker_1 and speaker_2.",
					},
					"output_path": map[string]interface{}{
						"type":        "string",
						"description": "Required full absolute destination path under the workspace docs root for the generated audio, e.g. '/Users/.../workspace-docs/_users/default/Chats/generated-audio/narration.wav'. Workspace-relative paths are rejected. If multiple audio items are returned, files are saved as '-1', '-2', etc. before the extension.",
					},
					"provider": map[string]interface{}{
						"type":        "string",
						"description": "Optional provider override. Discover usable provider/model pairs with list_llm_capabilities(capability=\"text_to_speech\", include_models=true). Supported values: vertex, minimax, elevenlabs, deepgram. If specifying model_id, pass the matching provider too.",
					},
					"model_id": map[string]interface{}{
						"type":        "string",
						"description": "Optional model id. Use a model from list_llm_capabilities(capability=\"text_to_speech\", include_models=true), and pass the matching provider in the same call. Gemini default: gemini-3.1-flash-tts-preview. MiniMax examples: speech-2.8-turbo, speech-2.8-hd. ElevenLabs examples: eleven_multilingual_v2, eleven_turbo_v2_5. Deepgram examples: aura-2-thalia-en, aura-2-luna-en.",
						"enum":        []interface{}{"gemini-3.1-flash-tts-preview", "speech-2.8-turbo", "speech-2.8-hd", "speech-2.6-turbo", "speech-2.6-hd", "speech-02-turbo", "speech-02-hd", "eleven_multilingual_v2", "eleven_turbo_v2_5", "eleven_flash_v2_5", "eleven_v3", "aura-2-thalia-en", "aura-2-luna-en", "aura-2-asteria-en", "aura-2-apollo-en"},
					},
					"voice_name": map[string]interface{}{
						"type":        "string",
						"description": "Optional voice. For Gemini, this is a prebuilt voice name such as Kore, Puck, Aoede, Charon, Fenrir. For MiniMax and ElevenLabs, this is treated as the voice_id unless voice_id is provided. For Deepgram, an aura-* value overrides model_id.",
					},
					"voice_id": map[string]interface{}{
						"type":        "string",
						"description": "Optional ElevenLabs voice ID. Defaults to ElevenLabs example voice JBFqnCBsd6RMkjVDRZzb when provider=\"elevenlabs\".",
					},
					"language_code": map[string]interface{}{
						"type":        "string",
						"description": "Optional BCP-47 language code such as en-US, hi-IN, ja-JP, or es-US. If omitted, Gemini TTS detects the language from the prompt.",
					},
					"speaker_1": map[string]interface{}{
						"type":        "string",
						"description": "Optional first speaker name for multi-speaker TTS. Must match the speaker label used in the prompt.",
					},
					"speaker_1_voice": map[string]interface{}{
						"type":        "string",
						"description": "Optional voice for speaker_1. Defaults to Kore.",
					},
					"speaker_2": map[string]interface{}{
						"type":        "string",
						"description": "Optional second speaker name for multi-speaker TTS. Must match the speaker label used in the prompt.",
					},
					"speaker_2_voice": map[string]interface{}{
						"type":        "string",
						"description": "Optional voice for speaker_2. Defaults to Puck.",
					},
				},
				"required": []interface{}{"prompt", "output_path"},
			}),
		},
	}
}

func GetSpeechToTextToolDefinition() llmtypes.Tool {
	return llmtypes.Tool{
		Function: &llmtypes.FunctionDefinition{
			Name:        speechToTextToolName,
			Description: "Transcribe workspace audio to text using Deepgram speech-to-text. Requires a full absolute audio_path under the workspace docs root. Before choosing provider/model_id, call list_llm_capabilities(capability=\"speech_to_text\", include_models=true). If you pass model_id, also pass the matching provider from that capability result; do not pass model_id by itself. Defaults to Deepgram nova-3.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"audio_path": map[string]interface{}{
						"type":        "string",
						"description": "Required full absolute path under the workspace docs root to the audio file to transcribe, e.g. '/Users/.../workspace-docs/_users/default/Chats/audio/interview.mp3'. Workspace-relative paths are rejected.",
					},
					"provider": map[string]interface{}{
						"type":        "string",
						"description": "Optional provider override. Discover usable provider/model pairs with list_llm_capabilities(capability=\"speech_to_text\", include_models=true). Currently supported: deepgram. If specifying model_id, pass the matching provider too.",
						"enum":        []interface{}{"deepgram"},
					},
					"model_id": map[string]interface{}{
						"type":        "string",
						"description": "Optional Deepgram speech-to-text model id. Use a model from list_llm_capabilities(capability=\"speech_to_text\", include_models=true), and pass provider=\"deepgram\" in the same call. Defaults to nova-3.",
						"enum":        []interface{}{"nova-3", "nova-3-multilingual", "nova-2", "base"},
					},
					"language_code": map[string]interface{}{
						"type":        "string",
						"description": "Optional BCP-47 language code such as en, en-US, hi, ja, or es.",
					},
					"smart_format": map[string]interface{}{
						"type":        "boolean",
						"description": "Optional Deepgram smart formatting toggle. Defaults to true.",
					},
					"punctuate": map[string]interface{}{
						"type":        "boolean",
						"description": "Optional punctuation toggle. Defaults to true.",
					},
					"diarize": map[string]interface{}{
						"type":        "boolean",
						"description": "Optional speaker diarization toggle.",
					},
				},
				"required": []interface{}{"audio_path"},
			}),
		},
	}
}

func CreateAudioGenExecutor(cfg AudioGenExecutorConfig) func(ctx context.Context, args map[string]any) (string, error) {
	return func(ctx context.Context, args map[string]any) (string, error) {
		cfg = applyAudioGenToolArgs(cfg, args)

		prompt, _ := args["prompt"].(string)
		if strings.TrimSpace(prompt) == "" {
			return "", fmt.Errorf("prompt is required")
		}
		outputPath, _ := args["output_path"].(string)
		normalizedOutputPath, err := normalizeRequiredAbsoluteWorkspaceDocumentPath(outputPath, "output_path")
		if err != nil {
			return "", err
		}
		outputPath = normalizedOutputPath
		if err := validateGuardedAudioOutputPath(ctx, cfg, outputPath); err != nil {
			return "", err
		}

		provider, modelID, workspaceAPIKeys, err := resolveAudioGenerationTarget(ctx, cfg)
		if err != nil {
			return "", wrapAudioGenerationSelectionError(err)
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
			if provider == "elevenlabs" {
				providerAPIKeys.ElevenLabs = apiKeyPtr
			} else if provider == "minimax" {
				providerAPIKeys.MiniMax = apiKeyPtr
			} else if provider == "deepgram" {
				providerAPIKeys.Deepgram = apiKeyPtr
			} else {
				providerAPIKeys.Vertex = apiKeyPtr
			}
		}

		audioGenCfg := llm.Config{
			Provider: llm.Provider(provider),
			ModelID:  modelID,
			APIKeys:  providerAPIKeys,
			Context:  ctx,
		}

		log.Printf("[AUDIO_GEN] Initializing model: provider=%s model=%s apiKeyProvided=%v workspaceURL=%q userID=%q",
			provider, modelID, cfg.APIKey != "", cfg.WorkspaceAPIURL, cfg.UserID)

		model, err := llm.InitializeAudioGenerationModel(audioGenCfg)
		if err != nil {
			log.Printf("[AUDIO_GEN] Failed to initialize audio generation model: %v", err)
			return "", wrapAudioGenerationInitializationError(provider, modelID, err)
		}

		var opts []llmtypes.AudioGenerationOption
		voiceName, _ := args["voice_name"].(string)
		if voiceID, _ := args["voice_id"].(string); strings.TrimSpace(voiceID) != "" {
			voiceName = voiceID
		}
		if strings.TrimSpace(voiceName) != "" {
			opts = append(opts, llmtypes.WithAudioVoiceName(voiceName))
		}
		languageCode, _ := args["language_code"].(string)
		if strings.TrimSpace(languageCode) != "" {
			opts = append(opts, llmtypes.WithAudioLanguageCode(languageCode))
		}
		if speakers := audioSpeakerConfigsFromArgs(args); len(speakers) > 0 {
			opts = append(opts, llmtypes.WithAudioSpeakerVoiceConfigs(speakers))
		}

		resp, err := model.GenerateAudio(ctx, prompt, opts...)
		if err != nil {
			return "", fmt.Errorf("audio generation failed: %w", err)
		}
		if len(resp.Audio) == 0 {
			return "", fmt.Errorf("audio generation returned no audio")
		}

		var savedPaths []string
		var absolutePaths []string
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
				log.Printf("[AUDIO_GEN] %s folder already exists, proceeding", outputDir)
			} else {
				return "", fmt.Errorf("failed to prepare output folder %q: %w", outputDir, err)
			}
		}

		for i, audio := range resp.Audio {
			if len(audio.Data) == 0 {
				return "", fmt.Errorf("generated audio %d returned no bytes", i+1)
			}
			mimeType := audio.MimeType
			if mimeType == "" {
				mimeType = "audio/wav"
			}
			if resultMimeType == "" {
				resultMimeType = mimeType
			}

			targetPaths, pathErr := resolveAudioOutputPaths(outputPath, len(resp.Audio), mimeType)
			if pathErr != nil {
				return "", pathErr
			}
			targetPath := targetPaths[i]
			folderPath := path.Dir(targetPath)
			fileName := path.Base(targetPath)
			savedPath, saveErr := wsClient.UploadBinary(ctx, folderPath, fileName, audio.Data)
			if saveErr != nil {
				return "", fmt.Errorf("failed to save generated audio %d to workspace path %q: %w", i+1, targetPath, saveErr)
			}
			savedPaths = append(savedPaths, savedPath)
			absolutePaths = append(absolutePaths, workspaceAbsolutePath(savedPath))
		}

		costUnit, costQuantity, totalCost, costEstimated := textToSpeechCost(provider, modelID, prompt, len(resp.Audio))
		if totalCost > 0 {
			recordPricedToolCost(ctx, cfg.WorkspaceAPIURL, cfg.UserID, pricedToolCost{
				ToolName:    textToSpeechToolName,
				Capability:  textToSpeechToolName,
				Provider:    provider,
				ModelID:     modelID,
				Unit:        costUnit,
				Quantity:    costQuantity,
				Count:       len(resp.Audio),
				TotalCost:   totalCost,
				Estimated:   costEstimated,
				OutputPaths: savedPaths,
				Metadata: map[string]interface{}{
					"characters":    len([]rune(prompt)),
					"voice_name":    voiceName,
					"language_code": languageCode,
				},
			})
		}

		result := audioGenResult{
			Model:         modelID,
			Provider:      provider,
			Prompt:        prompt,
			VoiceName:     voiceName,
			LanguageCode:  languageCode,
			SavedPaths:    savedPaths,
			AbsolutePaths: absolutePaths,
			Count:         len(resp.Audio),
			MimeType:      resultMimeType,
			CostUnit:      costUnit,
			CostQuantity:  costQuantity,
			TotalCost:     totalCost,
			CostEstimated: costEstimated,
		}
		resultJSON, err := json.Marshal(result)
		if err != nil {
			return "", fmt.Errorf("failed to marshal audio generation result: %w", err)
		}

		return string(resultJSON), nil
	}
}

func audioSpeakerConfigsFromArgs(args map[string]any) []llmtypes.AudioSpeakerVoiceConfig {
	speaker1, _ := args["speaker_1"].(string)
	speaker2, _ := args["speaker_2"].(string)
	speaker1Voice, _ := args["speaker_1_voice"].(string)
	speaker2Voice, _ := args["speaker_2_voice"].(string)
	var speakers []llmtypes.AudioSpeakerVoiceConfig
	if strings.TrimSpace(speaker1) != "" {
		if strings.TrimSpace(speaker1Voice) == "" {
			speaker1Voice = "Kore"
		}
		speakers = append(speakers, llmtypes.AudioSpeakerVoiceConfig{Speaker: speaker1, VoiceName: speaker1Voice})
	}
	if strings.TrimSpace(speaker2) != "" {
		if strings.TrimSpace(speaker2Voice) == "" {
			speaker2Voice = "Puck"
		}
		speakers = append(speakers, llmtypes.AudioSpeakerVoiceConfig{Speaker: speaker2, VoiceName: speaker2Voice})
	}
	return speakers
}

func audioMIMETypeForPath(filePath string) string {
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".mp3", ".mpeg", ".mpga":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".m4a", ".mp4":
		return "audio/mp4"
	case ".webm":
		return "audio/webm"
	case ".ogg", ".oga":
		return "audio/ogg"
	case ".flac":
		return "audio/flac"
	default:
		return "application/octet-stream"
	}
}

func normalizeAudioTranscriptionProviderAndModel(provider, modelID string) (string, string, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	modelID = strings.TrimSpace(modelID)
	if provider == "" {
		provider = "deepgram"
	}
	if provider != "deepgram" {
		return "", "", fmt.Errorf("unsupported speech-to-text provider %q. Supported speech-to-text providers: deepgram", provider)
	}
	if modelID == "" {
		modelID = defaultDeepgramSTTModel
	}
	return provider, modelID, nil
}

func CreateSpeechToTextExecutor(cfg AudioGenExecutorConfig) func(ctx context.Context, args map[string]any) (string, error) {
	return func(ctx context.Context, args map[string]any) (string, error) {
		audioPath, _ := args["audio_path"].(string)
		normalizedAudioPath, err := normalizeRequiredAbsoluteWorkspaceDocumentPath(audioPath, "audio_path")
		if err != nil {
			return "", err
		}
		audioPath = normalizedAudioPath
		cleanAudioPath := path.Clean(strings.TrimSpace(audioPath))
		if cleanAudioPath == "." || cleanAudioPath == ".." || strings.HasPrefix(cleanAudioPath, "../") || strings.HasPrefix(cleanAudioPath, "/") {
			return "", fmt.Errorf("audio_path must be normalized under the workspace docs root")
		}
		if cfg.WorkspaceAPIURL == "" {
			return "", fmt.Errorf("speech_to_text requires a workspace API URL so audio_path can be read from the workspace")
		}

		providerArg, _ := args["provider"].(string)
		modelIDArg, _ := args["model_id"].(string)
		provider, modelID, err := normalizeAudioTranscriptionProviderAndModel(providerArg, modelIDArg)
		if err != nil {
			return "", err
		}

		guardClient := workspace.NewClient(
			cfg.WorkspaceAPIURL,
			workspace.WithUserID(cfg.UserID),
		)
		if err := guardClient.ValidatePathWithContext(ctx, cleanAudioPath, false); err != nil {
			return "", fmt.Errorf("audio_path is outside the current session's readable folders: %w", err)
		}
		audioBytes, err := guardClient.DownloadFile(ctx, cleanAudioPath)
		if err != nil {
			return "", fmt.Errorf("failed to read audio_path %q from workspace: %w", cleanAudioPath, err)
		}
		if len(audioBytes) == 0 {
			return "", fmt.Errorf("audio_path %q is empty", cleanAudioPath)
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
			providerAPIKeys.Deepgram = apiKeyPtr
		}

		transcriptionCfg := llm.Config{
			Provider: llm.Provider(provider),
			ModelID:  modelID,
			APIKeys:  providerAPIKeys,
			Context:  ctx,
		}

		log.Printf("[SPEECH_TO_TEXT] Initializing model: provider=%s model=%s apiKeyProvided=%v workspaceURL=%q userID=%q",
			provider, modelID, cfg.APIKey != "", cfg.WorkspaceAPIURL, cfg.UserID)

		model, err := llm.InitializeAudioTranscriptionModel(transcriptionCfg)
		if err != nil {
			return "", fmt.Errorf("speech-to-text could not start for provider %q and model %q: %w", provider, modelID, err)
		}

		var opts []llmtypes.AudioTranscriptionOption
		if languageCode, _ := args["language_code"].(string); strings.TrimSpace(languageCode) != "" {
			opts = append(opts, llmtypes.WithTranscriptionLanguageCode(languageCode))
		}
		if smartFormat, ok := args["smart_format"].(bool); ok {
			opts = append(opts, llmtypes.WithTranscriptionSmartFormat(smartFormat))
		}
		if punctuate, ok := args["punctuate"].(bool); ok {
			opts = append(opts, llmtypes.WithTranscriptionPunctuate(punctuate))
		}
		if diarize, ok := args["diarize"].(bool); ok {
			opts = append(opts, llmtypes.WithTranscriptionDiarize(diarize))
		}

		mimeType := audioMIMETypeForPath(cleanAudioPath)
		resp, err := model.TranscribeAudio(ctx, audioBytes, mimeType, opts...)
		if err != nil {
			return "", fmt.Errorf("speech-to-text failed: %w", err)
		}

		costUnit, costQuantity, totalCost, costEstimated := speechToTextCost(provider, modelID, resp.Duration)
		if totalCost > 0 {
			recordPricedToolCost(ctx, cfg.WorkspaceAPIURL, cfg.UserID, pricedToolCost{
				ToolName:    speechToTextToolName,
				Capability:  speechToTextToolName,
				Provider:    provider,
				ModelID:     modelID,
				Unit:        costUnit,
				Quantity:    costQuantity,
				Count:       1,
				TotalCost:   totalCost,
				Estimated:   costEstimated,
				OutputPaths: []string{cleanAudioPath},
				Metadata: map[string]interface{}{
					"duration_seconds": resp.Duration,
					"mime_type":        mimeType,
				},
			})
		}

		result := audioTranscriptionResult{
			Model:         modelID,
			Provider:      provider,
			AudioPath:     cleanAudioPath,
			Transcript:    resp.Transcript,
			Confidence:    resp.Confidence,
			Duration:      resp.Duration,
			MimeType:      mimeType,
			CostUnit:      costUnit,
			CostQuantity:  costQuantity,
			TotalCost:     totalCost,
			CostEstimated: costEstimated,
		}
		resultJSON, err := json.Marshal(result)
		if err != nil {
			return "", fmt.Errorf("failed to marshal speech-to-text result: %w", err)
		}
		return string(resultJSON), nil
	}
}

func CreateWorkspaceAudioTools() []llmtypes.Tool {
	return []llmtypes.Tool{
		GetTextToSpeechToolDefinition(),
		GetSpeechToTextToolDefinition(),
	}
}

func CreateWorkspaceAudioToolExecutors(cfg AudioGenExecutorConfig) map[string]func(ctx context.Context, args map[string]any) (string, error) {
	textToSpeechExecutor := CreateAudioGenExecutor(cfg)
	speechToTextExecutor := CreateSpeechToTextExecutor(cfg)
	return map[string]func(ctx context.Context, args map[string]any) (string, error){
		GetTextToSpeechToolDefinition().Function.Name: textToSpeechExecutor,
		GetSpeechToTextToolDefinition().Function.Name: speechToTextExecutor,
	}
}

func MergeAudioToolExecutors(cfg AudioGenExecutorConfig, executors map[string]func(ctx context.Context, args map[string]any) (string, error), categories map[string]string) {
	cat := GetWorkspaceAdvancedToolCategory()
	for name, exec := range CreateWorkspaceAudioToolExecutors(cfg) {
		executors[name] = exec
		if categories != nil {
			categories[name] = cat
		}
	}
}

func MergeAudioToolExecutorsUntyped(cfg AudioGenExecutorConfig, executors map[string]any, categories map[string]string) {
	cat := GetWorkspaceAdvancedToolCategory()
	for name, exec := range CreateWorkspaceAudioToolExecutors(cfg) {
		executors[name] = exec
		if categories != nil {
			categories[name] = cat
		}
	}
}
