package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

const publishedLLMsFilePath = "config/published-llms.json"

// StoredPublishedLLM is the workspace-backed published LLM record.
// Secrets are intentionally not stored here; provider auth lives in config/provider-api-keys.json.
type StoredPublishedLLM struct {
	ID                        string                 `json:"id"`
	Name                      string                 `json:"name"`
	Provider                  string                 `json:"provider"`
	ModelID                   string                 `json:"model_id"`
	ModelName                 string                 `json:"model_name,omitempty"`
	AuthMethod                string                 `json:"auth_method,omitempty"`
	ContextWindow             *int                   `json:"context_window,omitempty"`
	InputCostPer1M            *float64               `json:"input_cost_per_1m,omitempty"`
	OutputCostPer1M           *float64               `json:"output_cost_per_1m,omitempty"`
	ReasoningCostPer1M        *float64               `json:"reasoning_cost_per_1m,omitempty"`
	CachedInputCostPer1M      *float64               `json:"cached_input_cost_per_1m,omitempty"`
	CachedInputCostWritePer1M *float64               `json:"cached_input_cost_write_per_1m,omitempty"`
	Options                   map[string]interface{} `json:"options,omitempty"`
	Temperature               *float64               `json:"temperature,omitempty"`
	CreatedAt                 string                 `json:"created_at,omitempty"`
}

func sanitizePublishedLLM(entry StoredPublishedLLM) (StoredPublishedLLM, bool) {
	entry.ID = strings.TrimSpace(entry.ID)
	entry.Name = strings.TrimSpace(entry.Name)
	entry.Provider = strings.TrimSpace(entry.Provider)
	entry.ModelID = strings.TrimSpace(entry.ModelID)
	entry.ModelName = strings.TrimSpace(entry.ModelName)
	entry.AuthMethod = strings.TrimSpace(entry.AuthMethod)
	entry.CreatedAt = strings.TrimSpace(entry.CreatedAt)

	if entry.Provider == "" || entry.ModelID == "" || entry.Name == "" {
		return StoredPublishedLLM{}, false
	}

	if entry.ID == "" {
		entry.ID = fmt.Sprintf("%s:%s:%d", entry.Provider, entry.ModelID, time.Now().UnixNano())
	}
	if entry.CreatedAt == "" {
		entry.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if entry.Options != nil && len(entry.Options) == 0 {
		entry.Options = nil
	}

	return entry, true
}

// SavePublishedLLMs saves published LLMs as plain JSON in the workspace config folder.
func SavePublishedLLMs(ctx context.Context, llms []StoredPublishedLLM) error {
	log.Printf("[PUBLISHED_LLM] SavePublishedLLMs called with %d entries", len(llms))
	sanitized := make([]StoredPublishedLLM, 0, len(llms))
	for _, entry := range llms {
		clean, ok := sanitizePublishedLLM(entry)
		if !ok {
			log.Printf("[PUBLISHED_LLM] Dropping invalid entry during save: id=%q name=%q provider=%q model_id=%q", entry.ID, entry.Name, entry.Provider, entry.ModelID)
			continue
		}
		sanitized = append(sanitized, clean)
	}
	log.Printf("[PUBLISHED_LLM] Sanitized save payload contains %d entries", len(sanitized))

	data, err := json.Marshal(sanitized)
	if err != nil {
		return fmt.Errorf("failed to marshal published llms: %w", err)
	}
	if err := writeFileToWorkspace(ctx, publishedLLMsFilePath, string(data)); err != nil {
		log.Printf("[PUBLISHED_LLM] Failed writing %s: %v", publishedLLMsFilePath, err)
		return fmt.Errorf("failed to write published llms: %w", err)
	}
	log.Printf("[PUBLISHED_LLM] Wrote %d entries to %s", len(sanitized), publishedLLMsFilePath)
	return nil
}

// LoadPublishedLLMs reads published LLMs from the workspace config folder.
// Returns nil, nil if the file doesn't exist.
func LoadPublishedLLMs(ctx context.Context) ([]StoredPublishedLLM, error) {
	content, exists, err := readFileFromWorkspace(ctx, publishedLLMsFilePath)
	if err != nil {
		log.Printf("[PUBLISHED_LLM] Failed reading %s: %v", publishedLLMsFilePath, err)
		return nil, fmt.Errorf("failed to read published llms: %w", err)
	}
	if !exists {
		log.Printf("[PUBLISHED_LLM] %s does not exist", publishedLLMsFilePath)
		return nil, nil
	}
	log.Printf("[PUBLISHED_LLM] Loaded raw content from %s (%d bytes)", publishedLLMsFilePath, len(content))

	var llms []StoredPublishedLLM
	if err := json.Unmarshal([]byte(content), &llms); err != nil {
		log.Printf("[PUBLISHED_LLM] Failed unmarshalling %s: %v", publishedLLMsFilePath, err)
		return nil, fmt.Errorf("failed to unmarshal published llms: %w", err)
	}

	sanitized := make([]StoredPublishedLLM, 0, len(llms))
	for _, entry := range llms {
		clean, ok := sanitizePublishedLLM(entry)
		if !ok {
			log.Printf("[PUBLISHED_LLM] Dropping invalid entry during load: id=%q name=%q provider=%q model_id=%q", entry.ID, entry.Name, entry.Provider, entry.ModelID)
			continue
		}
		sanitized = append(sanitized, clean)
	}
	log.Printf("[PUBLISHED_LLM] Returning %d published LLM entries from %s", len(sanitized), publishedLLMsFilePath)

	return sanitized, nil
}

// handleSavePublishedLLMs saves published LLMs to the workspace config folder.
// PUT /api/published-llms
func (api *StreamingAPI) handleSavePublishedLLMs(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	var llms []StoredPublishedLLM
	if err := json.NewDecoder(r.Body).Decode(&llms); err != nil {
		log.Printf("[PUBLISHED_LLM] Invalid request body for save: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	log.Printf("[PUBLISHED_LLM] HTTP save request received with %d entries", len(llms))

	if err := SavePublishedLLMs(r.Context(), llms); err != nil {
		log.Printf("[PUBLISHED_LLM] Save request failed: %v", err)
		http.Error(w, fmt.Sprintf("Failed to save published llms: %v", err), http.StatusInternalServerError)
		return
	}
	log.Printf("[PUBLISHED_LLM] Save request completed successfully")

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
}

// handleLoadPublishedLLMs loads published LLMs from the workspace config folder.
// GET /api/published-llms
func (api *StreamingAPI) handleLoadPublishedLLMs(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	log.Printf("[PUBLISHED_LLM] HTTP load request received")

	llms, err := LoadPublishedLLMs(r.Context())
	if err != nil {
		log.Printf("[PUBLISHED_LLM] Load request failed: %v", err)
		http.Error(w, fmt.Sprintf("Failed to load published llms: %v", err), http.StatusInternalServerError)
		return
	}
	if llms == nil {
		llms = []StoredPublishedLLM{}
	}
	log.Printf("[PUBLISHED_LLM] Load request returning %d entries", len(llms))

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(llms)
}
