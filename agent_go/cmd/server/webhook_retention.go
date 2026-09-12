package server

import (
	"context"
	"encoding/json"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/schedulerstate"
	"io/fs"
	"os"
	"sort"
	"strings"
)

const webhookRetainedRuns = 10

func (s *SchedulerService) pruneWebhookRuns(workspace string) {
	if err := s.pruneWebhookRunsChecked(workspace); err != nil {
		scheduleLogf("[WEBHOOK_RETENTION] cleanup failed for %s: %v", workspace, err)
	}
}
func (s *SchedulerService) pruneWebhookRunsChecked(workspace string) error {
	lock := scheduleRunFileLock(workspace + "/hook-allocation")
	lock.Lock()
	defer lock.Unlock()
	root, err := webhookWorkspaceRoot(workspace)
	if err != nil {
		return err
	}
	defer root.Close()
	entries, err := fs.ReadDir(root.FS(), "runs")
	if err != nil {
		return err
	}
	completed := []schedulerstate.Run{}
	for _, entry := range entries {
		if !entry.IsDir() || !webhookFolderPattern.MatchString(entry.Name()) {
			continue
		}
		raw, e := root.ReadFile("runs/" + entry.Name() + "/.webhook-run-id")
		if e != nil {
			continue
		}
		run, e := s.existingWebhookRun(context.Background(), string(raw))
		if e != nil {
			return e
		}
		if run.TriggerSource == "webhook" && run.ScopeID == workspace && run.RunFolder == entry.Name() && run.CompletedAt != nil {
			completed = append(completed, run)
		}
	}
	sort.Slice(completed, func(i, j int) bool { return completed[i].CompletedAt.After(*completed[j].CompletedAt) })
	if len(completed) <= webhookRetainedRuns {
		return nil
	}
	if err := root.MkdirAll("webhooks/expired", 0700); err != nil {
		return err
	}
	for _, run := range completed[webhookRetainedRuns:] {
		// Persist expiration before removing artifacts; a crash can safely retry cleanup.
		b, _ := json.Marshal(map[string]string{"run_id": run.RunID, "run_folder": run.RunFolder})
		if err := root.WriteFile("webhooks/expired/"+run.RunID+".json", b, 0600); err != nil {
			return err
		}
		if err := root.RemoveAll("runs/" + run.RunFolder); err != nil {
			return err
		}
		if err := root.RemoveAll("evaluation/runs/" + run.RunFolder); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
func webhookArtifactsExpired(workspace, runID string) bool {
	if strings.ContainsAny(runID, "/\\") {
		return false
	}
	root, e := webhookWorkspaceRoot(workspace)
	if e != nil {
		return false
	}
	defer root.Close()
	_, e = root.Stat("webhooks/expired/" + runID + ".json")
	return e == nil
}
