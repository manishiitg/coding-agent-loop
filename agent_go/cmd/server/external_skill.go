package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Hosted skill endpoints. ChatGPT and Claude Cowork connect over remote MCP
// and have no skill directory, so the Connect tab offers the AgentWorks skill
// as a downloadable zip (Plugins/Skills upload) plus the same text for
// pasting into Custom Instructions / connector instructions on plans without
// Skills support. The zip follows the Agent Skills SKILL.md layout that
// ChatGPT, Claude, and Cowork all accept for upload.
//
// The skill carries the server origin but never a credential. Hosted clients
// obtain access through OAuth; skill text can be shared across workspaces.

// hostedSkillDescription is the SKILL.md frontmatter description: what the
// skill does and when to use it. Keep it under 1024 chars with no XML
// brackets (frontmatter constraints shared by the upload scanners).
const hostedSkillDescription = "Discover, ask, and call AgentWorks Crews and workflows over MCP, and read their files, plans, runs, and guidance. Use when the task touches an AgentWorks Crew or workflow or when AgentWorks MCP tools are available."

// buildHostedSkillMarkdown renders the hosted SKILL.md. It must stay
// self-contained: ChatGPT delivers tools only (no MCP prompts, resources, or
// initialize instructions), so a hosted agent sees exactly this text plus
// tool schemas. It shares sections with
// agent_go/pkg/agentworksclient/skills/agentworks/SKILL.md. Direct calls use
// the four advertised tools; less common and legacy operations use call_tool.
func buildHostedSkillMarkdown(origin string) string {
	server := strings.TrimRight(strings.TrimSpace(origin), "/")
	var body strings.Builder
	body.WriteString("---\nname: agentworks\ndescription: " + hostedSkillDescription + "\n---\n")
	fmt.Fprintf(&body, `
# AgentWorks

You are connected to an AgentWorks server at %s via MCP. The server advertises `+"`list_agents`"+`, `+"`ask`"+`, `+"`call_function`"+`, `+"`get_call`"+`, `+"`get_api_spec`"+`, and `+"`call_tool`"+` when your scopes permit them. Tools read or run; they do not author workflows. IDs are never filesystem paths.

## First step

Use `+"`list_agents`"+` to find visible Crews and workflows; call them by the returned ID. Use `+"`get_api_spec`"+` for other permitted operations and their schemas, then `+"`call_tool`"+` to execute those operations by name. Call `+"`get_agent_context`"+` through `+"`call_tool`"+` if you need token capabilities and the guidance version.

## Ask and call

Use `+"`ask(target, message)`"+` for a plain-language request. This MCP connection has one continuing conversation with each target. The workflow assistant may choose a typed function or a raw Run-mode action. When the function and inputs are known, use `+"`call_function(target, function, args)`"+` for checked inputs before execution. Omitted declared defaults are applied; explicit values override them. An invalid call is refused with problems and the effective schema; supply missing values before retrying. If the result is still working, poll `+"`get_call(call_id)`"+` for progress and the outcome. Another token owned by the same user has a separate conversation and cannot poll this call.

## Guidance per task

Use `+"`list_workflows`"+` through `+"`call_tool`"+` when you need workflow-only inventory or metadata. List topics with `+"`list_guidance_topics`"+` and load only relevant ones via `+"`get_guidance_topic`"+`. Inspect workflow knowledge with `+"`list_workflow_knowledge`"+` / `+"`read_workflow_knowledge`"+` (learnings, knowledgebase notes, workspace skills, skill wiring). Use `+"`get_file_link`"+` for preview/download URLs.

## Run

To run: call a run-mode tool such as `+"`execute_step`"+` — the reply carries `+"`session_id`"+` — then poll `+"`run_status`"+` for completion. Steer live work with `+"`send_step_message`"+`, stop it with `+"`stop_step`"+` / `+"`stop_all_executions`"+`, and read run evidence with `+"`list_runs`"+`, `+"`get_run`"+`, and `+"`get_logs`"+`. Schedules: `+"`list_schedules`"+`, `+"`get_schedule_runs`"+`, `+"`trigger_schedule`"+`.

## Chat

`+"`chat`"+` asks the workflow assistant anything — analysis, explanations, follow-ups — in a pinned Run-mode session. Pass `+"`session_id`"+` to continue the conversation; sessions are shared with the run tools, so one conversation can ask, run, and ask about the run. Read replies with `+"`run_status`"+`, and answer waiting human-input steps with `+"`run_reply_input`"+`.

## Crews

Workflows expose typed functions defined by their Builder. Direct `+"`call_function`"+` returns the run outcome; direct `+"`ask`"+` reaches the workflow's Run-mode assistant. The older `+"`list_workflow_functions`"+`, `+"`call_workflow_function`"+`, and `+"`get_workflow_function_call`"+` operations remain available through `+"`call_tool`"+` for existing scripts. Calls need `+"`runs:execute`"+` and edit access to the workflow.

Crews are persistent AgentWorks agents. `+"`get_crew`"+` shows identity, model, and functions; `+"`list_crew_files`"+` / `+"`read_crew_file`"+` read shared project files. The older `+"`list_crews`"+`, `+"`call_crew_function`"+`, `+"`ask_crew`"+`, and `+"`get_crew_function_call`"+` operations remain available through `+"`call_tool`"+` for existing scripts. Those calls keep their legacy user-scoped conversation. New direct calls use the connection-scoped conversation. Calls need `+"`crews:run`"+` on a token that includes the Crew.

## Answer from reading

If the task needs a change, say so instead of attempting one — authoring is not exposed.
`, server)
	return body.String()
}

// handleExternalSkillMD serves the hosted SKILL.md text for pasting into
// Custom Instructions / connector instructions.
func (api *StreamingAPI) handleExternalSkillMD(w http.ResponseWriter, r *http.Request) {
	if GetUserFromContext(r.Context()) == nil {
		externalError(w, http.StatusUnauthorized, "unauthorized", "Sign in to AgentWorks.")
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	_, _ = io.WriteString(w, buildHostedSkillMarkdown(getBaseURL(r)))
}

// handleExternalSkillZIP serves the hosted skill as a zip for the
// Plugins/Skills upload flow (ChatGPT, Cowork).
func (api *StreamingAPI) handleExternalSkillZIP(w http.ResponseWriter, r *http.Request) {
	if GetUserFromContext(r.Context()) == nil {
		externalError(w, http.StatusUnauthorized, "unauthorized", "Sign in to AgentWorks.")
		return
	}
	var buf bytes.Buffer
	archive := zip.NewWriter(&buf)
	entry, err := archive.Create("agentworks/SKILL.md")
	if err != nil {
		externalError(w, http.StatusInternalServerError, "skill_unavailable", err.Error())
		return
	}
	if _, err := io.WriteString(entry, buildHostedSkillMarkdown(getBaseURL(r))); err != nil {
		externalError(w, http.StatusInternalServerError, "skill_unavailable", err.Error())
		return
	}
	if err := archive.Close(); err != nil {
		externalError(w, http.StatusInternalServerError, "skill_unavailable", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="agentworks-skill.zip"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

// handleExternalPlugin packages the hosted skill with a Cowork remote MCP
// connector. The OAuth client discovers authorization from the public MCP URL;
// no credential or client secret belongs in the downloadable archive.
func (api *StreamingAPI) handleExternalPlugin(w http.ResponseWriter, r *http.Request) {
	if GetUserFromContext(r.Context()) == nil {
		externalError(w, http.StatusUnauthorized, "unauthorized", "Sign in to AgentWorks.")
		return
	}
	origin, resource, ok := mcpOAuthURLs()
	if !ok || !strings.HasPrefix(strings.ToLower(origin), "https://") {
		externalError(w, http.StatusServiceUnavailable, "plugin_unavailable", "The Cowork plugin requires a configured public HTTPS URL.")
		return
	}
	manifest, err := json.MarshalIndent(map[string]any{
		"name": "agentworks", "version": "0.1.0",
		"description": "Read AgentWorks workflow knowledge and test code, inspect runs, and run permitted workflows.",
		"author":      map[string]string{"name": "AgentWorks"},
	}, "", "  ")
	if err != nil {
		externalError(w, http.StatusInternalServerError, "plugin_unavailable", err.Error())
		return
	}
	connector, err := json.MarshalIndent(map[string]any{
		"mcpServers": map[string]any{"agentworks": map[string]string{"type": "http", "url": resource}},
	}, "", "  ")
	if err != nil {
		externalError(w, http.StatusInternalServerError, "plugin_unavailable", err.Error())
		return
	}
	files := []struct{ name, content string }{
		{".claude-plugin/plugin.json", string(manifest) + "\n"},
		{".mcp.json", string(connector) + "\n"},
		{"skills/agentworks/SKILL.md", buildHostedSkillMarkdown(origin)},
		{"README.md", "# AgentWorks for Claude Cowork\n\nInstall this plugin in Customize > Plugins, then connect AgentWorks and approve access in your browser. The connector uses OAuth; this package contains no credential.\n"},
	}
	var buf bytes.Buffer
	archive := zip.NewWriter(&buf)
	for _, file := range files {
		entry, err := archive.Create(file.name)
		if err == nil {
			_, err = io.WriteString(entry, file.content)
		}
		if err != nil {
			externalError(w, http.StatusInternalServerError, "plugin_unavailable", err.Error())
			return
		}
	}
	if err := archive.Close(); err != nil {
		externalError(w, http.StatusInternalServerError, "plugin_unavailable", err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="agentworks.plugin"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}
