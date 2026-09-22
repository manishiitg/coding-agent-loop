package server

import (
	"archive/zip"
	"bytes"
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
// The skill carries the server origin but never a credential: the connector
// URL already holds the token, and skill text gets stored in connector
// configs and shared across workspaces.

// hostedSkillDescription is the SKILL.md frontmatter description: what the
// skill does and when to use it. Keep it under 1024 chars with no XML
// brackets (frontmatter constraints shared by the upload scanners).
const hostedSkillDescription = "Read and run AgentWorks workflows over MCP (list workflows, read files, plans, runs, guidance, and knowledge; execute steps, workflows, and schedules). Use when the task touches an AgentWorks workflow or when agentworks tools are available."

// buildHostedSkillMarkdown renders the hosted SKILL.md. It must stay
// self-contained: ChatGPT delivers tools only (no MCP prompts, resources, or
// initialize instructions), so a hosted agent sees exactly this text plus
// tool schemas. It shares sections with
// agent_go/pkg/agentworksclient/skills/agentworks/SKILL.md, but the dispatch
// differs: the remote surface exposes only get_api_spec and call_tool, so
// every tool below runs through call_tool.
func buildHostedSkillMarkdown(origin string) string {
	server := strings.TrimRight(strings.TrimSpace(origin), "/")
	var body strings.Builder
	body.WriteString("---\nname: agentworks\ndescription: " + hostedSkillDescription + "\n---\n")
	fmt.Fprintf(&body, `
# AgentWorks

You are connected to an AgentWorks server at %s via MCP. This connection exposes exactly two tools and reads and runs through them: tools read, and run-mode tools execute in pinned Run-mode sessions. Nothing creates, edits, or authors. Workflows below are identified by workflow ID, never by filesystem path.

## First step

Call `+"`get_api_spec`"+` with no arguments to list every available tool. Call `+"`get_api_spec`"+` again with names for their JSON schemas. Execute everything with `+"`call_tool`"+`, passing the tool name and its arguments — never call a listed tool directly, only these two tools exist. Discover workflow IDs with `+"`list_workflows`"+` first — IDs are never filesystem paths. Call `+"`get_agent_context`"+` for token capabilities and the guidance version.

## Guidance per task

List topics with `+"`list_guidance_topics`"+` and load only relevant ones via `+"`get_guidance_topic`"+`. Inspect workflow knowledge with `+"`list_workflow_knowledge`"+` / `+"`read_workflow_knowledge`"+` (learnings, knowledgebase notes, workspace skills, skill wiring). Use `+"`get_file_link`"+` for preview/download URLs.

## Run

To run: call a run-mode tool such as `+"`execute_step`"+` — the reply carries `+"`session_id`"+` — then poll `+"`run_status`"+` for completion. Steer live work with `+"`send_step_message`"+`, stop it with `+"`stop_step`"+` / `+"`stop_all_executions`"+`, and read run evidence with `+"`list_runs`"+`, `+"`get_run`"+`, and `+"`get_logs`"+`. Schedules: `+"`list_schedules`"+`, `+"`get_schedule_runs`"+`, `+"`trigger_schedule`"+`.

## Chat

`+"`chat`"+` asks the workflow assistant anything — analysis, explanations, follow-ups — in a pinned Run-mode session. Pass `+"`session_id`"+` to continue the conversation; sessions are shared with the run tools, so one conversation can ask, run, and ask about the run. Read replies with `+"`run_status`"+`, and answer waiting human-input steps with `+"`run_reply_input`"+`.

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
