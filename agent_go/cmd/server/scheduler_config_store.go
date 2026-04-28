package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

const schedulerConfigFilePath = "config/scheduler.json"

// SchedulerConfig stores workspace-level scheduler settings in config/scheduler.json.
// Schedule definitions remain in each workflow manifest.
type SchedulerConfig struct {
	GloballyPaused   bool       `json:"globally_paused"`
	PausedAt         *time.Time `json:"paused_at,omitempty"`
	PausedBy         string     `json:"paused_by,omitempty"`
	UpdatedAt        *time.Time `json:"updated_at,omitempty"`
	ExecutionEnabled bool       `json:"execution_enabled"`
	DisabledViaEnv   bool       `json:"disabled_via_env,omitempty"`
	DisabledReason   string     `json:"disabled_reason,omitempty"`

	// Per-workflow env filter — populated from SCHEDULER_ALLOWED_WORKFLOWS /
	// SCHEDULER_BLOCKED_WORKFLOWS so the UI can show which crons run on this
	// machine when sharing workspace files across multiple machines.
	AllowedWorkflows []string `json:"allowed_workflows,omitempty"`
	BlockedWorkflows []string `json:"blocked_workflows,omitempty"`

	// Per-user multi-agent env filter — populated from SCHEDULER_ALLOWED_USERS /
	// SCHEDULER_BLOCKED_USERS for multi-agent schedules under _users/{userID}/.
	AllowedUsers []string `json:"allowed_users,omitempty"`
	BlockedUsers []string `json:"blocked_users,omitempty"`
}

func sanitizeSchedulerConfig(cfg *SchedulerConfig) *SchedulerConfig {
	if cfg == nil {
		return &SchedulerConfig{}
	}

	sanitized := &SchedulerConfig{
		GloballyPaused: cfg.GloballyPaused,
		PausedBy:       strings.TrimSpace(cfg.PausedBy),
	}
	if cfg.GloballyPaused && cfg.PausedAt != nil {
		pausedAt := cfg.PausedAt.UTC()
		sanitized.PausedAt = &pausedAt
	}
	if cfg.UpdatedAt != nil {
		updatedAt := cfg.UpdatedAt.UTC()
		sanitized.UpdatedAt = &updatedAt
	}
	return sanitized
}

func applySchedulerRuntimeState(cfg *SchedulerConfig) *SchedulerConfig {
	if cfg == nil {
		cfg = &SchedulerConfig{}
	}

	cfg.ExecutionEnabled = true
	cfg.DisabledViaEnv = false
	cfg.DisabledReason = ""

	if strings.EqualFold(strings.TrimSpace(os.Getenv("SCHEDULER_ENABLED")), "false") {
		cfg.ExecutionEnabled = false
		cfg.DisabledViaEnv = true
		cfg.DisabledReason = "Automatic cron execution is disabled on this server because SCHEDULER_ENABLED=false. Manual runs still work."
	}

	envFilter := loadSchedulerWorkflowFilter()
	cfg.AllowedWorkflows = envFilter.rawAllow
	cfg.BlockedWorkflows = envFilter.rawBlock
	cfg.AllowedUsers = envFilter.rawAllowUsers
	cfg.BlockedUsers = envFilter.rawBlockUsers

	return cfg
}

func SaveSchedulerConfig(ctx context.Context, cfg *SchedulerConfig) error {
	sanitized := sanitizeSchedulerConfig(cfg)
	now := time.Now().UTC()
	if sanitized.GloballyPaused && sanitized.PausedAt == nil {
		sanitized.PausedAt = &now
	}
	if !sanitized.GloballyPaused {
		sanitized.PausedAt = nil
		sanitized.PausedBy = ""
	}
	sanitized.UpdatedAt = &now

	data, err := json.Marshal(sanitized)
	if err != nil {
		return fmt.Errorf("failed to marshal scheduler config: %w", err)
	}
	if err := writeFileToWorkspace(ctx, schedulerConfigFilePath, string(data)); err != nil {
		return fmt.Errorf("failed to write scheduler config: %w", err)
	}
	return nil
}

func LoadSchedulerConfig(ctx context.Context) (*SchedulerConfig, error) {
	content, exists, err := readFileFromWorkspace(ctx, schedulerConfigFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read scheduler config: %w", err)
	}
	if !exists {
		return applySchedulerRuntimeState(&SchedulerConfig{}), nil
	}

	var cfg SchedulerConfig
	if err := json.Unmarshal([]byte(content), &cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal scheduler config: %w", err)
	}
	return applySchedulerRuntimeState(sanitizeSchedulerConfig(&cfg)), nil
}

func (s *SchedulerService) IsGloballyPaused(ctx context.Context) (bool, *SchedulerConfig, error) {
	cfg, err := LoadSchedulerConfig(ctx)
	if err != nil {
		return false, nil, err
	}
	return cfg.GloballyPaused, cfg, nil
}

func getSchedulerConfigHandler(svc *SchedulerService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		cfg, err := LoadSchedulerConfig(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(cfg)
	}
}

func updateSchedulerConfigHandler(svc *SchedulerService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		var req SchedulerConfig
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		if err := SaveSchedulerConfig(r.Context(), &req); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		cfg, err := LoadSchedulerConfig(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(cfg)
	}
}
