package server

import (
	"context"
	"log"
	"path"
)

// chiefOfStaffRecommendationWriteFolders returns the only workflow-internal
// files Chief of Staff may write through filesystem tools: each workflow's
// builder/improve.html recommendation ledger. Everything else under Workflow/
// remains read-only in multi-agent chat.
func chiefOfStaffRecommendationWriteFolders(ctx context.Context) []string {
	workflows, err := DiscoverWorkflowManifests(ctx)
	if err != nil {
		log.Printf("[CHIEF_OF_STAFF_RECOMMENDATIONS] Failed to discover workflow improve logs: %v", err)
		return nil
	}

	folders := make([]string, 0, len(workflows))
	for _, workflow := range workflows {
		if workflow.WorkspacePath == "" {
			continue
		}
		folders = append(folders, path.Join(workflow.WorkspacePath, "builder", "improve.html"))
	}
	return folders
}
