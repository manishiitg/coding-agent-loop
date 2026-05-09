package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"mcp-agent-builder-go/agent_go/pkg/fsutil"
)

// schedulerWorkflowFilter is the env-driven allow/block list for cron execution
// per workflow (and per multi-agent user). Lets two machines share the same
// workspace files but only run some crons on each machine.
//
// Configured via:
//   - SCHEDULER_ALLOWED_WORKFLOWS=foo,bar  (workflow allowlist)
//   - SCHEDULER_BLOCKED_WORKFLOWS=baz      (workflow denylist)
//   - SCHEDULER_ALLOWED_USERS=alice,bob    (multi-agent allowlist)
//   - SCHEDULER_BLOCKED_USERS=carol        (multi-agent denylist)
//
// Allowlist wins over denylist when both are set within the same category.
// Workflow identifiers are matched case-insensitively against the workflow's
// manifest ID, label, and workspace folder name (last segment of the workspace
// path). User identifiers match the userID directory name.
type schedulerWorkflowFilter struct {
	allow         map[string]struct{}
	block         map[string]struct{}
	allowUsers    map[string]struct{}
	blockUsers    map[string]struct{}
	rawAllow      []string
	rawBlock      []string
	rawAllowUsers []string
	rawBlockUsers []string
}

// loadSchedulerWorkflowFilter resolves allow/block lists for cron execution.
// Source of truth is config/scheduler.json (allowed_workflows, blocked_workflows,
// allowed_users, blocked_users). Env vars (SCHEDULER_ALLOWED_WORKFLOWS, etc.)
// remain as a fallback when the JSON field is empty/unset, so existing
// deployments using env vars keep working.
func loadSchedulerWorkflowFilter() schedulerWorkflowFilter {
	jsonAllow, jsonBlock, jsonAllowUsers, jsonBlockUsers := readSchedulerListsFromJSON()

	rawAllow := pickList(jsonAllow, splitAndTrim(os.Getenv("SCHEDULER_ALLOWED_WORKFLOWS")))
	rawBlock := pickList(jsonBlock, splitAndTrim(os.Getenv("SCHEDULER_BLOCKED_WORKFLOWS")))
	rawAllowUsers := pickList(jsonAllowUsers, splitAndTrim(os.Getenv("SCHEDULER_ALLOWED_USERS")))
	rawBlockUsers := pickList(jsonBlockUsers, splitAndTrim(os.Getenv("SCHEDULER_BLOCKED_USERS")))

	return schedulerWorkflowFilter{
		allow:         toLowerSet(rawAllow),
		block:         toLowerSet(rawBlock),
		allowUsers:    toLowerSet(rawAllowUsers),
		blockUsers:    toLowerSet(rawBlockUsers),
		rawAllow:      rawAllow,
		rawBlock:      rawBlock,
		rawAllowUsers: rawAllowUsers,
		rawBlockUsers: rawBlockUsers,
	}
}

// pickList returns json values when non-empty, else falls back to env values.
func pickList(jsonVals, envVals []string) []string {
	if len(jsonVals) > 0 {
		return jsonVals
	}
	return envVals
}

// readSchedulerListsFromJSON reads the four list fields out of
// config/scheduler.json on disk. Returns nil slices if the file is missing or
// unreadable — callers fall back to env vars in that case.
func readSchedulerListsFromJSON() (allow, block, allowUsers, blockUsers []string) {
	path := filepath.Join(fsutil.WorkspaceDocsRoot(), "config", "scheduler.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, nil, nil
	}
	var raw struct {
		AllowedWorkflows []string `json:"allowed_workflows"`
		BlockedWorkflows []string `json:"blocked_workflows"`
		AllowedUsers     []string `json:"allowed_users"`
		BlockedUsers     []string `json:"blocked_users"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil, nil, nil
	}
	return trimList(raw.AllowedWorkflows), trimList(raw.BlockedWorkflows), trimList(raw.AllowedUsers), trimList(raw.BlockedUsers)
}

func trimList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func toLowerSet(items []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, item := range items {
		out[strings.ToLower(item)] = struct{}{}
	}
	return out
}

func splitAndTrim(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// IsActive reports whether the user has configured any per-workflow filtering.
func (f schedulerWorkflowFilter) IsActive() bool {
	return len(f.allow) > 0 || len(f.block) > 0
}

// IsUserFilterActive reports whether multi-agent user filtering is configured.
func (f schedulerWorkflowFilter) IsUserFilterActive() bool {
	return len(f.allowUsers) > 0 || len(f.blockUsers) > 0
}

// IsUserAllowed returns true if the multi-agent user's schedules should run on
// this machine. When no user filter env vars are set, all users are allowed.
func (f schedulerWorkflowFilter) IsUserAllowed(userID string) bool {
	if !f.IsUserFilterActive() {
		return true
	}
	key := strings.ToLower(strings.TrimSpace(userID))
	if key == "" {
		return true
	}
	if len(f.allowUsers) > 0 {
		_, ok := f.allowUsers[key]
		return ok
	}
	_, blocked := f.blockUsers[key]
	return !blocked
}

// IsWorkflowAllowed returns true if the workflow's cron schedules should run on
// this machine. When no filter env vars are set, all workflows are allowed.
func (f schedulerWorkflowFilter) IsWorkflowAllowed(workflowID, workflowLabel, workspacePath string) bool {
	if !f.IsActive() {
		return true
	}

	candidates := workflowMatchCandidates(workflowID, workflowLabel, workspacePath)

	if len(f.allow) > 0 {
		for _, c := range candidates {
			if _, ok := f.allow[c]; ok {
				return true
			}
		}
		return false
	}

	for _, c := range candidates {
		if _, ok := f.block[c]; ok {
			return false
		}
	}
	return true
}

// workflowMatchCandidates returns the lowercase identifiers a filter entry can
// match against — manifest ID, manifest label, and workspace folder name.
func workflowMatchCandidates(workflowID, workflowLabel, workspacePath string) []string {
	out := make([]string, 0, 3)
	if id := strings.TrimSpace(workflowID); id != "" {
		out = append(out, strings.ToLower(id))
	}
	if label := strings.TrimSpace(workflowLabel); label != "" {
		out = append(out, strings.ToLower(label))
	}
	if folder := strings.TrimSpace(filepath.Base(strings.TrimRight(workspacePath, "/"))); folder != "" && folder != "." {
		out = append(out, strings.ToLower(folder))
	}
	return out
}
