package step_based_workflow

import (
	"context"
	"fmt"
	"sort"
	"strings"

	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/mcpclient"

	"mcp-agent-builder-go/agent_go/pkg/common"
)

// MissingDependency describes one workflow-required resource that isn't
// configured on the host. We collect every missing item before raising so the
// operator sees a complete punch list, not one-by-one trickling errors as
// each step tries and fails.
type MissingDependency struct {
	Kind       string   // currently always "mcp_server" (room for "secret", "skill" later)
	Name       string   // canonical name as required by the workflow/step
	RequiredBy []string // sorted: "workflow" + step IDs that reference this name
}

// validateWorkflowDependencies walks the workflow's declared MCP server set
// (workflow-level + per-step) and the live merged MCP config, returning a
// list of names that the workflow needs but the server cannot provide.
//
// Returning nil with nil error means everything declared is available — the
// run can proceed. A non-empty slice means the run will fail downstream
// (silently for cursor's --print path, with "Server X not found" warnings
// for the agent-init path), so the caller should fail-fast with a single
// formatted error instead of launching the workflow.
//
// MCP-server lookup tolerates the hyphen↔underscore mismatch the same way
// MCPConfig.GetServer does: a workflow asking for "google_sheets" matches a
// config entry named "google-sheets" and vice versa.
//
// Secrets and skills are intentionally NOT validated here yet — they need
// access to the secret store and skill registry which aren't plumbed through
// to the workshop session today. Extend this function (return additional
// MissingDependency.Kind values) when those are wired up.
func validateWorkflowDependencies(ctx context.Context, session *WorkshopChatSession, mcpConfigPath string, logger loggerv2.Logger) ([]MissingDependency, error) {
	if session == nil || session.controller == nil {
		return nil, fmt.Errorf("preflight: nil session/controller")
	}

	// 1. Collect required servers: workflow-level ∪ all step configs.
	required := map[string]map[string]struct{}{} // server → set of "requiredBy" labels (dedup)
	addRequired := func(server, by string) {
		server = strings.TrimSpace(server)
		if server == "" {
			return
		}
		// Built-in custom/virtual tool categories (workspace_advanced,
		// workspace_browser, human, workflow, …) appear in workflow
		// selected_servers but are provided by the app, not the MCP config — so
		// they're always available and must not be validated as MCP servers.
		if common.IsBuiltinToolCategory(server) {
			return
		}
		if _, ok := required[server]; !ok {
			required[server] = map[string]struct{}{}
		}
		required[server][by] = struct{}{}
	}

	for _, s := range session.controller.GetSelectedServers() {
		addRequired(s, "workflow")
	}

	if stepConfigs, err := session.controller.ReadStepConfigs(ctx); err != nil {
		// step_config.json is optional in early-stage workflows. Log + continue
		// with workflow-level only rather than blocking the run.
		if logger != nil {
			logger.Warn(fmt.Sprintf("preflight: ReadStepConfigs failed (%v) — per-step server requirements will not be validated this run", err))
		}
	} else {
		for _, sc := range stepConfigs {
			if sc.AgentConfigs == nil {
				continue
			}
			for _, s := range sc.AgentConfigs.SelectedServers {
				addRequired(s, sc.ID)
			}
		}
	}

	if len(required) == 0 {
		return nil, nil
	}

	// 2. Build the available-server set from the live merged MCP config.
	cfg, err := mcpclient.LoadMergedConfig(mcpConfigPath, logger)
	if err != nil {
		return nil, fmt.Errorf("preflight: load merged MCP config: %w", err)
	}
	available := map[string]struct{}{}
	for _, name := range cfg.ListServers() {
		available[name] = struct{}{}
		// Mirror MCPConfig.GetServer's normalization: declarations using
		// underscore must match config entries using hyphen and vice versa.
		if strings.Contains(name, "-") {
			available[strings.ReplaceAll(name, "-", "_")] = struct{}{}
		}
		if strings.Contains(name, "_") {
			available[strings.ReplaceAll(name, "_", "-")] = struct{}{}
		}
	}

	// 3. Diff. Sort everything so the report is deterministic.
	var missing []MissingDependency
	names := make([]string, 0, len(required))
	for n := range required {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, ok := available[name]; ok {
			continue
		}
		by := make([]string, 0, len(required[name]))
		for label := range required[name] {
			by = append(by, label)
		}
		sort.Strings(by)
		missing = append(missing, MissingDependency{
			Kind:       "mcp_server",
			Name:       name,
			RequiredBy: by,
		})
	}
	return missing, nil
}

// formatMissingDependencies renders a single user-facing error message
// listing every missing dependency at once, with an actionable fix hint.
// Returned as a plain string so the run_full_workflow tool can surface it
// back to the operator as a tool result.
func formatMissingDependencies(workflowID string, missing []MissingDependency, mcpConfigPath string) string {
	var b strings.Builder
	if strings.TrimSpace(workflowID) != "" {
		b.WriteString(fmt.Sprintf("❌ Workflow %q can't run on this server — required dependencies are missing from %s:\n", workflowID, mcpConfigPath))
	} else {
		b.WriteString(fmt.Sprintf("❌ Workflow can't run on this server — required dependencies are missing from %s:\n", mcpConfigPath))
	}
	for _, m := range missing {
		b.WriteString(fmt.Sprintf("  • %s %q (required by: %s)\n", m.Kind, m.Name, strings.Join(m.RequiredBy, ", ")))
	}
	b.WriteString("\nFix: add the missing entry/entries to your MCP config and restart the orchestrator.\n")
	b.WriteString("On this codebase the user-extensible MCP config lives at agent_go/configs/mcp_servers_clean_user.json — it is merged into the base config at load time.\n")
	return b.String()
}
