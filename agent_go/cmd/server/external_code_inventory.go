package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"strings"

	wf "github.com/manishiitg/coding-agent-loop/workspace/workflowfiles"
)

type externalStepCodeEntry struct {
	wf.Entry
	StepID    string `json:"step_id,omitempty"`
	StepTitle string `json:"step_title,omitempty"`
	InPlan    bool   `json:"in_plan"`
}

// Step IDs are names of directories below the persisted code root. A code
// folder can outlive a plan step, so in_plan distinguishes saved orphan code.
func externalPlanStepTitles(node any, titles map[string]string) {
	switch value := node.(type) {
	case map[string]any:
		if id, ok := value["id"].(string); ok && id != "" {
			if _, isStep := value["type"].(string); isStep {
				titles[id], _ = value["title"].(string)
			}
		}
		for _, child := range value {
			externalPlanStepTitles(child, titles)
		}
	case []any:
		for _, child := range value {
			externalPlanStepTitles(child, titles)
		}
	}
}

func (api *StreamingAPI) externalListStepCode(w http.ResponseWriter, r *http.Request, workflow DiscoveredWorkflow, args map[string]any) {
	base := "code"
	if workflow.Manifest.CodeLayoutVersion == 0 {
		base = "learnings"
	}
	selected := externalArg(args, "step_id")
	if selected != "" {
		clean, err := wf.CleanRelative(selected)
		if err != nil || clean != selected || clean == "." || strings.Contains(clean, "/") || wf.Private(clean) {
			externalError(w, 400, "invalid_arguments", "step_id must be one public directory name")
			return
		}
	}
	glob := externalArg(args, "glob")
	if glob == "" {
		glob = "**/*.py"
	}
	if err := wf.ValidateGlob(glob); err != nil {
		externalError(w, 400, "invalid_arguments", err.Error())
		return
	}
	directory := base
	if selected != "" {
		directory = path.Join(base, selected)
	}
	result, err := externalFileRequest(r.Context(), wf.Request{Root: workflow.WorkspacePath, Operation: "list", Path: directory, Glob: glob, Depth: 8, Limit: externalInt(args, "limit", 100), Offset: externalInt(args, "offset", 0)})
	if err != nil {
		var upstream *externalUpstreamError
		if !errors.As(err, &upstream) || upstream.status != 404 {
			externalFailure(w, err)
			return
		}
		result.Entries = []wf.Entry{}
	}
	titles := map[string]string{}
	plan, err := externalFileRequest(r.Context(), wf.Request{Root: workflow.WorkspacePath, Operation: "read", Path: "planning/plan.json"})
	if err != nil {
		externalFailure(w, err)
		return
	}
	if plan.Exists {
		var value any
		if err := json.Unmarshal([]byte(plan.Content), &value); err != nil {
			externalFailure(w, err)
			return
		}
		externalPlanStepTitles(value, titles)
	}
	entries := make([]externalStepCodeEntry, 0, len(result.Entries))
	for _, entry := range result.Entries {
		item := externalStepCodeEntry{Entry: entry}
		rel := strings.TrimPrefix(entry.Path, base+"/")
		if segment, _, ok := strings.Cut(rel, "/"); ok {
			item.StepID = segment
			item.StepTitle, item.InPlan = titles[segment]
		}
		entries = append(entries, item)
	}
	externalJSON(w, map[string]any{"workflow_id": workflow.Manifest.ID, "code_layout_version": workflow.Manifest.CodeLayoutVersion, "source_root": base, "entries": entries, "next_offset": result.NextOffset, "truncated": result.Truncated})
}
