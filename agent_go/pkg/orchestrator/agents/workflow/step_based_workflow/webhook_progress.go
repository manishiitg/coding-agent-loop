package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"time"
)

var webhookProgressMu sync.Mutex

type WebhookProgressEntry struct {
	StepID    string    `json:"step_id"`
	StepPath  string    `json:"step_path,omitempty"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (hcpo *StepBasedWorkflowOrchestrator) persistWebhookProgress(ctx context.Context, step PlanStepInterface, index int, stepPath, status string) {
	opts := hcpo.GetExecutionOptions()
	if opts == nil || opts.WebhookInputFile == "" || step == nil {
		return
	}
	webhookProgressMu.Lock()
	defer webhookProgressMu.Unlock()
	p := filepath.ToSlash(filepath.Join(hcpo.GetWorkspacePath(), "runs", hcpo.selectedRunFolder, "webhook_progress.json"))
	entries := map[string]WebhookProgressEntry{}
	if raw, err := hcpo.ReadWorkspaceFile(ctx, p); err == nil {
		if json.Unmarshal([]byte(raw), &entries) != nil {
			return
		}
	}
	id := step.GetID()
	if id == "" {
		id = fmt.Sprintf("step-%d", index+1)
	}
	key := id + ":" + stepPath
	entries[key] = WebhookProgressEntry{StepID: id, StepPath: stepPath, Title: step.GetTitle(), Status: status, UpdatedAt: time.Now().UTC()}
	raw, err := json.Marshal(entries)
	if err == nil {
		err = hcpo.WriteWorkspaceFile(context.WithoutCancel(ctx), p, string(raw))
	}
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("Unable to persist webhook progress: %v", err))
	}
}
