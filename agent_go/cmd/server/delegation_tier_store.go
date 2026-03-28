package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
)

const delegationTierConfigFilePath = "config/delegation-tier-config.json"

// SaveDelegationTierConfig saves delegation tier config as plain JSON to the workspace.
func SaveDelegationTierConfig(ctx context.Context, config *virtualtools.DelegationTierConfig) error {
	data, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal tier config: %w", err)
	}
	if err := writeFileToWorkspace(ctx, delegationTierConfigFilePath, string(data)); err != nil {
		return fmt.Errorf("failed to write tier config: %w", err)
	}
	return nil
}

// LoadDelegationTierConfig reads delegation tier config from the workspace.
// Returns nil, nil if the file doesn't exist.
func LoadDelegationTierConfig(ctx context.Context) (*virtualtools.DelegationTierConfig, error) {
	content, exists, err := readFileFromWorkspace(ctx, delegationTierConfigFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read tier config: %w", err)
	}
	if !exists {
		return nil, nil
	}
	var cfg virtualtools.DelegationTierConfig
	if err := json.Unmarshal([]byte(content), &cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal tier config: %w", err)
	}
	return &cfg, nil
}

// LoadAndResolveTierConfig loads tier config from the workspace file and merges with a request-level
// override (request config takes priority over file config, file over env vars).
// Use this at delegation spawn time so tier changes written mid-session take effect immediately.
func LoadAndResolveTierConfig(ctx context.Context, requestConfig *virtualtools.DelegationTierConfig) *virtualtools.DelegationTierConfig {
	fileConfig, _ := LoadDelegationTierConfig(ctx)
	if fileConfig == nil && requestConfig == nil {
		return nil
	}
	// Start with file config as base, then overlay request config fields on top
	merged := &virtualtools.DelegationTierConfig{}
	if fileConfig != nil {
		merged.Main = fileConfig.Main
		merged.High = fileConfig.High
		merged.Medium = fileConfig.Medium
		merged.Low = fileConfig.Low
		merged.Custom = fileConfig.Custom
	}
	if requestConfig != nil {
		if requestConfig.Main != nil {
			merged.Main = requestConfig.Main
		}
		if requestConfig.High != nil {
			merged.High = requestConfig.High
		}
		if requestConfig.Medium != nil {
			merged.Medium = requestConfig.Medium
		}
		if requestConfig.Low != nil {
			merged.Low = requestConfig.Low
		}
		if len(requestConfig.Custom) > 0 {
			merged.Custom = requestConfig.Custom
		}
	}
	return merged
}

// handleSaveDelegationTierConfig saves delegation tier config to the workspace filesystem.
// PUT /api/delegation-tier-config
func (api *StreamingAPI) handleSaveDelegationTierConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	var cfg virtualtools.DelegationTierConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if err := SaveDelegationTierConfig(r.Context(), &cfg); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save tier config: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
}

// handleLoadDelegationTierConfig loads delegation tier config from the workspace filesystem.
// GET /api/delegation-tier-config
func (api *StreamingAPI) handleLoadDelegationTierConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	cfg, err := LoadDelegationTierConfig(r.Context())
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to load tier config: %v", err), http.StatusInternalServerError)
		return
	}
	if cfg == nil {
		cfg = &virtualtools.DelegationTierConfig{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cfg)
}
