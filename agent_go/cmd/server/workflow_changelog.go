package server

import (
	"encoding/json"
	"net/http"
	"path"
	"sort"
	"strings"
)

// planChangelogFieldChange mirrors PlanFieldChange written by the planning agent
// (planning_agent.go). Defined locally so the server package stays decoupled from
// the orchestrator package; the JSON tags are the contract.
type planChangelogFieldChange struct {
	StepID   string      `json:"step_id"`
	Field    string      `json:"field"`
	OldValue interface{} `json:"old_value"`
	NewValue interface{} `json:"new_value"`
}

// planChangelogEntry mirrors PlanChangelogEntry. One entry per successful
// plan-mod tool call, appended to planning/changelog/changelog-*.json.
type planChangelogEntry struct {
	Timestamp string                     `json:"timestamp"`
	Tool      string                     `json:"tool"`
	Reason    string                     `json:"reason"`
	StepIDs   []string                   `json:"step_ids,omitempty"`
	Changes   []planChangelogFieldChange `json:"changes,omitempty"`
	// Source changelog file this entry came from (added by the server, not on disk).
	File string `json:"file,omitempty"`
}

type planChangelogFile struct {
	Entries []planChangelogEntry `json:"entries"`
}

// PlanChangelogResponse is the JSON shape of GET /api/workflow/plan-changelog.
type PlanChangelogResponse struct {
	Success bool                 `json:"success"`
	Entries []planChangelogEntry `json:"entries"`
	Count   int                  `json:"count"`
	Error   string               `json:"error,omitempty"`
}

// planChangelogMaxEntries caps how many entries the feed returns so a long-lived
// workflow doesn't ship thousands of rows to the History view in one response.
const planChangelogMaxEntries = 300

// handleGetPlanChangelog serves the merged, newest-first plan-edit audit trail
// from planning/changelog/*.json — the granular "Plan edits" feed of the
// History view. Each entry carries the agent's mandatory reason plus per-field
// old→new diffs.
func (api *StreamingAPI) handleGetPlanChangelog(w http.ResponseWriter, r *http.Request) {
	if !setupCORS(w, r, http.MethodGet) {
		return
	}
	workspacePath, ok := requireWorkspacePath(w, r)
	if !ok {
		return
	}

	folder := path.Join(strings.Trim(workspacePath, "/"), "planning", "changelog")
	listing, exists, err := listWorkspaceFolder(r.Context(), folder, 1)
	if err != nil {
		writeAIJSON(w, PlanChangelogResponse{Success: false, Error: err.Error()})
		return
	}
	if !exists {
		// No changelog folder yet — an empty feed is a valid, non-error state.
		writeAIJSON(w, PlanChangelogResponse{Success: true, Entries: []planChangelogEntry{}, Count: 0})
		return
	}

	var filePaths []string
	collectWorkspaceFilePaths(listing, &filePaths)

	entries := make([]planChangelogEntry, 0, 64)
	for _, fullPath := range filePaths {
		if !strings.HasSuffix(strings.ToLower(fullPath), ".json") {
			continue
		}
		content, fileExists, readErr := readFileFromWorkspace(r.Context(), fullPath)
		if readErr != nil || !fileExists || strings.TrimSpace(content) == "" {
			continue
		}
		var parsed planChangelogFile
		if err := json.Unmarshal([]byte(content), &parsed); err != nil {
			continue // skip malformed changelog files rather than failing the whole feed
		}
		fileName := path.Base(fullPath)
		for _, entry := range parsed.Entries {
			entry.File = fileName
			entries = append(entries, entry)
		}
	}

	// Newest first. Timestamps are RFC3339 UTC, so a lexical descending sort
	// is chronological.
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Timestamp > entries[j].Timestamp
	})

	total := len(entries)
	if len(entries) > planChangelogMaxEntries {
		entries = entries[:planChangelogMaxEntries]
	}

	writeAIJSON(w, PlanChangelogResponse{Success: true, Entries: entries, Count: total})
}
