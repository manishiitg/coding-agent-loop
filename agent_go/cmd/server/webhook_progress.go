package server

import (
	"encoding/json"
	stepworkflow "github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow"
	"io/fs"
	"os"
	"sort"
	"strings"
)

type webhookProgressEntry struct {
	stepworkflow.WebhookProgressEntry
	Group string `json:"group"`
}

func collectWebhookProgress(root *os.Root) []webhookProgressEntry {
	result := []webhookProgressEntry{}
	groups, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return result
	}
	for _, g := range groups {
		if !g.IsDir() || strings.HasPrefix(g.Name(), ".") {
			continue
		}
		raw, err := root.ReadFile(g.Name() + "/webhook_progress.json")
		if err != nil || len(raw) > 2*1024*1024 {
			continue
		}
		var entries map[string]stepworkflow.WebhookProgressEntry
		if json.Unmarshal(raw, &entries) != nil {
			continue
		}
		for _, entry := range entries {
			result = append(result, webhookProgressEntry{WebhookProgressEntry: entry, Group: g.Name()})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Group+result[i].StepID+result[i].StepPath < result[j].Group+result[j].StepID+result[j].StepPath
	})
	return result
}
