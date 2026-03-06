package minimax

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/manishiitg/multi-llm-provider-go/interfaces"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

const (
	MiniMaxImageEndpoint = "https://api.minimax.io/v1/image_generation"

	// Image generation models
	ModelMiniMaxImage01 = "image-01"
)

// minimaxImageModels holds pricing metadata for MiniMax image generation models
var minimaxImageModels = map[string]llmtypes.ModelMetadata{
	ModelMiniMaxImage01: {
		ModelID:      ModelMiniMaxImage01,
		ModelName:    "MiniMax Image 01",
		Provider:     "minimax",
		CostPerImage: 0.0035,
	},
}

// MiniMaxImageAdapter implements llmtypes.ImageGenerationModel for MiniMax.
// Uses POST https://api.minimax.io/v1/image_generation
type MiniMaxImageAdapter struct {
	apiKey  string
	modelID string
	logger  interfaces.Logger
	client  *http.Client
}

// GetModelMetadata returns pricing metadata for the given MiniMax image model.
func (a *MiniMaxImageAdapter) GetModelMetadata(modelID string) (*llmtypes.ModelMetadata, error) {
	if modelID == "" {
		modelID = a.modelID
	}
	if meta, ok := minimaxImageModels[modelID]; ok {
		return &meta, nil
	}
	return nil, fmt.Errorf("no metadata found for MiniMax image model: %s", modelID)
}

// NewMiniMaxImageAdapter creates a new MiniMax image generation adapter.
func NewMiniMaxImageAdapter(apiKey, modelID string, logger interfaces.Logger) *MiniMaxImageAdapter {
	if modelID == "" {
		modelID = ModelMiniMaxImage01
	}
	return &MiniMaxImageAdapter{
		apiKey:  apiKey,
		modelID: modelID,
		logger:  logger,
		client:  &http.Client{},
	}
}

// imageSubjectReference is a reference image for subject-based editing.
// ImageFile is a URL string pointing to the reference image.
type imageSubjectReference struct {
	Type      string `json:"type"`
	ImageFile string `json:"image_file"`
}

// imageGenRequest is the request body for MiniMax image generation
type imageGenRequest struct {
	Model            string                  `json:"model"`
	Prompt           string                  `json:"prompt"`
	N                int                     `json:"n,omitempty"`
	AspectRatio      string                  `json:"aspect_ratio,omitempty"`
	ResponseFormat   string                  `json:"response_format,omitempty"`
	PromptOptimizer  bool                    `json:"prompt_optimizer,omitempty"`
	SubjectReference []imageSubjectReference `json:"subject_reference,omitempty"`
}

// imageGenResponse is the response body from MiniMax image generation.
// With response_format="url": data.image_urls contains signed URLs.
type imageGenResponse struct {
	ID   string `json:"id"`
	Data struct {
		ImageURLs []string `json:"image_urls"`
	} `json:"data"`
	Metadata *struct {
		SuccessCount string `json:"success_count"`
		FailedCount  string `json:"failed_count"`
	} `json:"metadata,omitempty"`
	BaseResp *struct {
		StatusCode int    `json:"status_code"`
		StatusMsg  string `json:"status_msg"`
	} `json:"base_resp,omitempty"`
}

// GenerateImages implements llmtypes.ImageGenerationModel
func (a *MiniMaxImageAdapter) GenerateImages(ctx context.Context, prompt string, options ...llmtypes.ImageGenerationOption) (*llmtypes.ImageGenerationResponse, error) {
	opts := &llmtypes.ImageGenerationOptions{
		NumberOfImages: 1,
		AspectRatio:    "1:1",
	}
	for _, opt := range options {
		opt(opts)
	}

	reqBody := imageGenRequest{
		Model:           a.modelID,
		Prompt:          prompt,
		N:               opts.NumberOfImages,
		AspectRatio:     opts.AspectRatio,
		ResponseFormat:  "url",
		PromptOptimizer: true,
	}

	// Support image editing via subject_reference when a reference image URL is provided
	if opts.InputImageURL != "" {
		reqBody.SubjectReference = []imageSubjectReference{
			{
				Type:      "character",
				ImageFile: opts.InputImageURL,
			},
		}
		if a.logger != nil {
			a.logger.Infof("[MINIMAX IMAGE] Using subject_reference URL for image editing")
		}
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("minimax image gen: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, MiniMaxImageEndpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("minimax image gen: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")

	if a.logger != nil {
		a.logger.Infof("[MINIMAX IMAGE] Generating image model=%s aspect_ratio=%s", a.modelID, opts.AspectRatio)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("minimax image gen: HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("minimax image gen: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("minimax image gen: HTTP %d: %s", resp.StatusCode, string(respBytes))
	}

	var genResp imageGenResponse
	if err := json.Unmarshal(respBytes, &genResp); err != nil {
		return nil, fmt.Errorf("minimax image gen: unmarshal response: %w", err)
	}

	if genResp.BaseResp != nil && genResp.BaseResp.StatusCode != 0 {
		return nil, fmt.Errorf("minimax image gen API error %d: %s", genResp.BaseResp.StatusCode, genResp.BaseResp.StatusMsg)
	}

	result := &llmtypes.ImageGenerationResponse{
		Images: make([]llmtypes.GeneratedImage, 0, len(genResp.Data.ImageURLs)),
	}

	for i, imgURL := range genResp.Data.ImageURLs {
		imgBytes, mimeType, dlErr := downloadImage(ctx, imgURL)
		if dlErr != nil {
			if a.logger != nil {
				a.logger.Errorf("[MINIMAX IMAGE] Failed to download image %d: %v", i, dlErr)
			}
			continue
		}
		if mimeType == "" {
			mimeType = "image/jpeg"
		}
		result.Images = append(result.Images, llmtypes.GeneratedImage{
			Data:     imgBytes,
			MimeType: mimeType,
		})
	}

	if a.logger != nil {
		a.logger.Infof("[MINIMAX IMAGE] Generated %d image(s) successfully", len(result.Images))
	}

	return result, nil
}

// downloadImage fetches image bytes from a URL
func downloadImage(ctx context.Context, url string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}

	mimeType := resp.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "image/jpeg"
	}
	return data, mimeType, nil
}
