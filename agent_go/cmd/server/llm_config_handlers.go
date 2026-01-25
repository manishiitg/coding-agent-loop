package server

import (
	"encoding/json"
	"net/http"

	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/azure"
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

// AzureDeployedModelsRequest represents the request body for fetching Azure deployed models
type AzureDeployedModelsRequest struct {
	Endpoint string `json:"endpoint"`
	APIKey   string `json:"api_key"`
}

// handleGetAzureDeployedModels returns only the models deployed in the user's Azure resource
func (api *StreamingAPI) handleGetAzureDeployedModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req AzureDeployedModelsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Endpoint == "" || req.APIKey == "" {
		http.Error(w, "endpoint and api_key are required", http.StatusBadRequest)
		return
	}

	// Fetch deployed models from Azure
	models, err := azure.GetAzureDeployedModels(req.Endpoint, req.APIKey)
	if err != nil {
		// Return error response - allows frontend to fall back to manual entry
		response := map[string]interface{}{
			"models": []interface{}{},
			"error":  err.Error(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	response := map[string]interface{}{
		"models": models,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}
