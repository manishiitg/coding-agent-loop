package server

import (
	"encoding/json"
	"net/http"

	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/utils"
)

// handleGetModelMetadata returns metadata for all available models across providers
func (api *StreamingAPI) handleGetModelMetadata(w http.ResponseWriter, r *http.Request) {
	// Get all model metadata from the utility function
	models := utils.GetAllModelMetadata()

	response := map[string]interface{}{
		"models": models,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}
