package server

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"

	browserinstructions "mcp-agent-builder-go/agent_go/pkg/instructions"
	"mcp-agent-builder-go/agent_go/pkg/skills"
	"mcp-agent-builder-go/agent_go/pkg/utils"
)

// AgentInstructions contains custom instructions for both React and Simple agents
type AgentInstructions struct {
	ResponseFormatting string
}

// workspacePaths holds the resolved absolute paths for the workspace.
type workspacePaths struct {
	DocsRoot    string
	Chats       string
	Skills      string
	Workflow    string
	Downloads   string
	Subagents   string
	Config      string
	Pulse       string
	ChatHistory string
}

func resolveWorkspacePath(docsRoot, rel string) string {
	if rel == "" {
		return rel
	}
	if strings.HasPrefix(rel, "/") || docsRoot == "" {
		return rel
	}
	return docsRoot + "/" + rel
}

func newWorkspacePaths(docsRoot, chatsFolder string) workspacePaths {
	if chatsFolder == "" {
		chatsFolder = "_users/default/Chats"
	}
	return workspacePaths{
		DocsRoot:    docsRoot,
		Chats:       resolveWorkspacePath(docsRoot, chatsFolder),
		Skills:      resolveWorkspacePath(docsRoot, "skills"),
		Workflow:    resolveWorkspacePath(docsRoot, "Workflow"),
		Downloads:   resolveWorkspacePath(docsRoot, "Downloads"),
		Subagents:   resolveWorkspacePath(docsRoot, "subagents"),
		Config:      resolveWorkspacePath(docsRoot, "config"),
		Pulse:       resolveWorkspacePath(docsRoot, "pulse"),
		ChatHistory: resolveWorkspacePath(docsRoot, strings.TrimSuffix(chatsFolder, "/Chats")+"/chat_history"),
	}
}

// GetWorkspaceMap returns a compact folder listing with absolute paths and access levels.
// This is the high-priority section — placed early in the prompt before reference docs.
func GetWorkspaceMap(docsRoot, chatsFolder string) string {
	p := newWorkspacePaths(docsRoot, chatsFolder)
	return `
## Workspace

**Always use absolute paths in shell commands.** The workspace docs root is: ` + "`" + p.DocsRoot + "`" + `. Every absolute path you reference in a shell command MUST start with this exact prefix. The path guard rejects absolute paths under any other host root (` + "`" + "/Users/..." + "`" + `, ` + "`" + "/home/..." + "`" + `) that are not under the docs root. Do NOT prepend the project root, your home directory, or anything else — always use ` + "`" + p.DocsRoot + "`" + ` as the prefix. When tool descriptions show paths like ` + "`" + "Workflow/<name>/" + "`" + `, ` + "`" + "pulse/goals.html" + "`" + `, or ` + "`" + "Chats/<folder>/" + "`" + `, those are LOCAL paths RELATIVE to the docs root; the absolute equivalent is the docs root + that suffix.

**Never use WebFetch/raw GitHub URLs for workspace artifacts, skills, or reference docs.** Files such as ` + "`" + "pulse/goals.html" + "`" + `, ` + "`" + "pulse/org-pulse.html" + "`" + `, ` + "`" + "Workflow/<name>/builder/improve.html" + "`" + `, and ` + "`" + "skills/<name>/SKILL.md" + "`" + ` live on local disk under the docs root above. Read them with the declared local tools/shell, or load canonical reference docs with ` + "`" + "get_reference_doc(kind=\"...\")" + "`" + `.

| Path | Access | Purpose |
|------|--------|---------|
| ` + "`" + p.Chats + "/`" + ` | read/write | Your workspace — save all output files here |
| ` + "`" + p.Pulse + "/`" + ` | read/write | Org-level goals and Pulse artifacts (` + "`goals.html`" + `, ` + "`org-pulse.html`" + `) |
| ` + "`" + p.Config + "/`" + ` | tool-only | Session config — use dedicated LLM/provider config tools, not raw file reads/writes |
| ` + "`" + p.ChatHistory + "/`" + ` | read/write | Past conversation histories |
| ` + "`" + p.Skills + "/`" + ` | read-only | Skill definitions (SKILL.md + supporting files) |
| ` + "`" + p.Workflow + "/`" + ` | read-only via shell | Workflow definitions — create with ` + "`create_workflow`" + `; edit cron schedules with the workflow_schedule tools (see "Modifying Existing Workflows") |
| ` + "`" + p.Downloads + "/`" + ` | read-only | Downloaded files and browser content |
| ` + "`" + p.Subagents + "/`" + ` | read-only | Sub-agent templates |

### Chats Folder Organization

Organize output files under descriptive project folders — never dump files at the Chats root.

` + "```" + `
` + p.Chats + `/
  <project-name>/          ← One folder per task/project (kebab-case)
    report.html            ← Final output (use HTML for rich reports)
    data.json              ← Supporting data
    analysis/              ← Sub-folder for complex outputs
  <another-project>/
` + "```" + `

Examples: ` + "`quarterly-sales-analysis/`" + `, ` + "`aws-cost-report/`" + `, ` + "`bank-statement-parsing/`" + `
Reuse existing project folders for follow-up work on the same topic.

**Output format**: prefer ` + "`.md`" + ` over ` + "`.html`" + ` for a final report, analysis, or summary meant for a human to read — markdown renders richly in the viewer (headings, tables, lists, clickable file links), and is simpler and more robust to author than hand-written HTML. Reach for ` + "`.html`" + ` only when you genuinely need pixel-perfect or branded/print layout markdown can't express. Use ` + "`.json`" + ` for raw data. When you do write HTML, make it self-contained (inline all CSS and JS — no external CDN links), include a summary box at the top, use semantic color for status fields, keep the width responsive, and add dark-mode styles (` + "`@media (prefers-color-scheme: dark)`" + `).
`
}

// GetWorkflowPhaseWorkspaceMap returns workflow-phase-specific workspace instructions.
// Unlike chat mode, workflow-phase work should treat the active workflow folder as the
// primary writable root and avoid surfacing internal per-user Chats paths.
func GetWorkflowPhaseWorkspaceMap(docsRoot, workflowFolder string) string {
	if strings.TrimSpace(workflowFolder) == "" {
		return GetWorkspaceMap(docsRoot, "")
	}

	workflowFolder = path.Clean(workflowFolder)
	absWorkflowFolder := resolveWorkspacePath(docsRoot, workflowFolder)
	absPlanningFolder := resolveWorkspacePath(docsRoot, path.Join(workflowFolder, "planning"))
	absWorkflowRoot := resolveWorkspacePath(docsRoot, "Workflow")
	absDownloads := resolveWorkspacePath(docsRoot, "Downloads")
	absConfig := resolveWorkspacePath(docsRoot, "config")

	return `
## Workspace

**Always use absolute paths in shell commands.** The workspace docs root is: ` + "`" + docsRoot + "`" + `. Every absolute path you reference in a shell command MUST start with this exact prefix. The path guard rejects absolute paths under any other host root (` + "`" + "/Users/..." + "`" + `, ` + "`" + "/home/..." + "`" + `, etc.) that are not under the docs root — even if the path "looks right." Do NOT construct absolute paths by prepending the project root, your home directory, or anything else; always use ` + "`" + docsRoot + "`" + ` as the prefix.

Common mistake to avoid: seeing a path like ` + "`" + "Workflow/<name>/" + "`" + ` mentioned in tool descriptions and prepending an arbitrary host prefix. The correct absolute form is ` + "`" + docsRoot + "/Workflow/<name>/" + "`" + ` — always use the docs root above. Tool descriptions that show ` + "`" + "Workflow/<name>/" + "`" + ` are referring to a path RELATIVE to the docs root; the absolute equivalent is the docs root + that suffix.

**Current writable workflow folder:** ` + "`" + absWorkflowFolder + "/`" + `

Save workflow outputs, generated media, test artifacts, and builder-side files inside the active workflow folder above. Do **not** default to Chats for builder work — Chats is internal session storage, not the primary workflow output location.

| Path | Access | Purpose |
|------|--------|---------|
| ` + "`" + absWorkflowFolder + "/`" + ` | read/write | Active workflow workspace — save builder outputs and generated files here |
| ` + "`" + absPlanningFolder + "/`" + ` | read-only via shell | Plan/config source of truth — inspect freely, but change it through workflow/builder tools rather than raw file writes |
| ` + "`" + absConfig + "/`" + ` | tool-only | Session config — use dedicated LLM/provider config tools, not raw file reads/writes |
| ` + "`" + absDownloads + "/`" + ` | read/write | Scratchpad for downloads and browser artifacts |
| ` + "`" + absWorkflowRoot + "/`" + ` | read-only outside the active workflow | Other workflow definitions |

### Builder File Placement

Keep workflow-related files under the active workflow folder so they stay with the workflow:

` + "```" + `
` + absWorkflowFolder + `/
  reports/                ← report plan and report assets
  db/                     ← structured workflow state and results
  knowledgebase/          ← durable narrative knowledge
  runs/                   ← execution outputs
  <other-artifacts>/      ← generated images, videos, temp analysis files
` + "```" + `

If you generate a test image, video, or other artifact for this workflow, place it somewhere under ` + "`" + absWorkflowFolder + "/`" + `.
`
}

// GetWorkspaceReference returns detailed reference documentation for workspace config,
// workflow structure, and workflow creation. This is lower-priority reference material —
// placed after the operating mode instructions in the prompt.
func GetWorkspaceReference(docsRoot, chatsFolder string) string {
	p := newWorkspacePaths(docsRoot, chatsFolder)
	absWorkflow := p.Workflow

	instructions := utils.GetCommonFileInstructions()

	instructions += "\n\n" + browserinstructions.GetSpecialWorkspaceToolsInstructions() + `

## LLM Tier Configuration
Do not read or write tier-config storage with shell/file tools. Use the UI or dedicated backend tier-config API; raw workspace file tools intentionally do not have ` + "`config/`" + ` access.
- Schema: ` + "`{\"main\":{\"provider\":\"anthropic\",\"model_id\":\"...\",\"fallbacks\":[{\"provider\":\"openai\",\"model_id\":\"gpt-5.4-mini\"},{\"model_id\":\"gpt-5.4\"}]},\"high\":{...},\"medium\":{...},\"low\":{...},\"custom\":{\"my-tier\":{...}}}`" + `
- To add fallbacks for a tier, add an ordered ` + "`fallbacks`" + ` array under that tier object.
- Each fallback entry uses ` + "`{\"provider\":\"...\",\"model_id\":\"...\"}`" + `. If ` + "`provider`" + ` is omitted, it defaults to the tier's own provider.
- Example: ` + "`{\"main\":{\"provider\":\"anthropic\",\"model_id\":\"claude-sonnet-4-6\",\"fallbacks\":[{\"model_id\":\"claude-haiku-4-5-20251001\"},{\"provider\":\"openai\",\"model_id\":\"gpt-5.4-mini\"}]}}`" + `
- Preserve existing tiers when editing. Only change the specific tier or ` + "`fallbacks`" + ` entries the user asked for.

## Published LLMs & Provider Auth
Published LLM metadata and provider authentication are workspace-backed configuration surfaces. Access them through dedicated tools only; raw workspace file tools intentionally do not expose ` + "`config/`" + `.
- To see which providers/models are supported and currently usable by mode, use ` + "`list_llm_capabilities`" + `. It covers ` + "`chat`" + `, ` + "`search_web`" + `, ` + "`read_image`" + `, ` + "`generate_image`" + `, ` + "`generate_video`" + `, ` + "`text_to_speech`" + `, ` + "`speech_to_text`" + `, and ` + "`generate_music`" + `, including auth/runtime availability and static pricing metadata where available.
- When choosing a concrete provider-backed model for search, media reading, media generation, transcription, or music, call ` + "`list_llm_capabilities(capability=\"...\", include_models=true)`" + ` first and pass ` + "`provider`" + ` and ` + "`model_id`" + ` together from the same capability entry. Do not pass only ` + "`model_id`" + ` and rely on provider inference.
- Estimate priced generation/transcription costs with ` + "`estimate_llm_cost`" + ` for ` + "`generate_video`" + `, ` + "`text_to_speech`" + `, ` + "`speech_to_text`" + `, and ` + "`generate_music`" + `. Treat results as estimates and verify provider pricing before high-volume runs.
- Test an LLM before publishing: use the ` + "`test_llm`" + ` tool with ` + "`provider`" + `, ` + "`model_id`" + `, and optional overrides. It uses workspace-backed provider auth by default.
- List the frontend-known models for a provider: use the ` + "`list_provider_models`" + ` tool. It uses shared metadata for fixed providers and the same dynamic picker source as the UI for dynamic providers.
- List published LLMs with ` + "`list_published_llms`" + `.
- Publish or update a published LLM with ` + "`save_published_llm`" + `.
- Provider auth is encrypted at rest. Do not read or hand-edit config files with shell/file tools.
- Update provider auth with the ` + "`set_provider_auth`" + ` tool.
- Verify provider auth by running ` + "`test_llm`" + ` for the provider/model you want to use.
- Use dedicated tools for all published LLM and provider-auth operations; raw workspace file tools intentionally do not have ` + "`config/`" + ` access.
- ` + "`search_web_llm`" + ` selects models from the published LLM set. Its ` + "`provider`" + ` argument is required; ` + "`model_id`" + ` is optional only when accepting a working search-capable model for that provider.
- Use ` + "`search_role`" + ` to control routing:
  - ` + "`\"primary\"`" + ` = preferred default search provider
  - ` + "`\"fallback\"`" + ` = backup search provider
- Use ` + "`search_priority`" + ` to order providers within the same role. Lower numbers win.
- If the tool call passes a specific ` + "`provider`" + `, that override wins over ` + "`search_role`" + ` / ` + "`search_priority`" + `.
- Example: ` + "`{\"id\":\"gemini-search\",\"name\":\"Gemini Search\",\"provider\":\"gemini-cli\",\"model_id\":\"gemini-2.5-pro\",\"search_role\":\"primary\",\"search_priority\":1}`" + `

## Image Generation Defaults
Image generation defaults are workspace-backed configuration. Provider authentication is managed separately through ` + "`set_provider_auth`" + `.
- Do not read or write saved defaults with shell/file tools. Use runtime ` + "`image_gen_config`" + ` overrides for the current chat session, or the dedicated UI/API configuration path when changing saved defaults.
- Schema: ` + "`{\"primary\":{\"provider\":\"vertex\",\"model_id\":\"gemini-3.1-flash-image\"},\"fallbacks\":[{\"provider\":\"codex-cli\",\"model_id\":\"gpt-5.4-mini\"}]}`" + `
- ` + "`primary`" + ` is tried first. ` + "`fallbacks`" + ` are tried in order when the primary provider lacks workspace auth.
- Runtime ` + "`image_gen_config`" + ` overrides this file for the current chat session only.
- Keep provider auth updated with the ` + "`set_provider_auth`" + ` tool; do not hand-edit encrypted auth files.
- Do not infer image-generation support from ` + "`list_provider_models`" + ` or the normal LLM model catalog. Those lists are for chat/text models, not image models.
- Vertex image generation is supported via provider ` + "`vertex`" + ` with models such as ` + "`gemini-3.1-flash-image`" + ` and ` + "`gemini-3-pro-image`" + `.
- Codex CLI image generation is supported via provider ` + "`codex-cli`" + ` with models such as ` + "`gpt-5.4-mini`" + `.
- Antigravity CLI image generation is deprecated for new setup. Existing legacy defaults using provider ` + "`agy-cli`" + ` and model ` + "`agy-cli`" + ` remain runnable when local ` + "`agy`" + ` sign-in is present.
- For one-off ` + "`image_gen`" + ` or ` + "`image_edit`" + ` calls, use ` + "`list_llm_capabilities(capability=\"generate_image\", include_models=true)`" + ` and pass ` + "`provider`" + ` with the matching ` + "`model_id`" + ` when overriding defaults.

## Image Analysis Defaults
Image understanding for the ` + "`read_image`" + ` tool can be routed via workspace-backed image analysis defaults.
- Do not read or write saved defaults with shell/file tools. Use per-call ` + "`read_image`" + ` overrides, or the dedicated UI/API configuration path when changing saved defaults.
- Schema: ` + "`{\"primary\":{\"provider\":\"vertex\",\"model_id\":\"gemini-3-pro-preview\"},\"fallbacks\":[{\"provider\":\"codex-cli\",\"model_id\":\"gpt-5.4-mini\"},{\"provider\":\"cursor-cli\",\"model_id\":\"cursor-cli\"},{\"provider\":\"claude-code\",\"model_id\":\"claude-code\"}]}`" + `
- If this file exists, ` + "`read_image`" + ` uses its ` + "`primary`" + ` and ordered ` + "`fallbacks`" + ` with workspace provider auth.
- If this file does not exist, ` + "`read_image`" + ` falls back to the current chat model.
- For one-off ` + "`read_image`" + ` calls, use ` + "`list_llm_capabilities(capability=\"read_image\", include_models=true)`" + ` and pass ` + "`provider`" + ` with the matching ` + "`model_id`" + ` when overriding defaults.
- Codex CLI image understanding is supported via provider ` + "`codex-cli`" + ` by passing the local workspace image path to Codex CLI.
- Cursor CLI image understanding is supported via provider ` + "`cursor-cli`" + ` by passing the local workspace image path to Cursor Agent CLI.
- Antigravity CLI image understanding is deprecated for new setup. Existing legacy ` + "`agy-cli`" + ` defaults remain runnable by passing the local workspace image path to Antigravity CLI.
- Claude Code image understanding is supported via provider ` + "`claude-code`" + ` by passing the local workspace image path to Claude Code CLI.
- Keep provider auth updated with the ` + "`set_provider_auth`" + ` tool; do not hand-edit encrypted auth files.

## Video Analysis
No dedicated workspace video-reading tool is exposed right now. For video QA or inspection, use local ` + "`execute_shell_command`" + ` workflows such as frame/audio extraction and provider-specific scripts only when the required credentials are available as workflow/user secrets.

## Workflows
List workflows with ` + "`execute_shell_command(command: \"ls " + absWorkflow + "/\")`" + `.

### Workflow Structure
Each workflow lives in ` + "`" + absWorkflow + `/<name>/` + "`" + ` with:

**Planning & config:**
- ` + "`soul/soul.md`" + ` — canonical stable workflow intent: ` + "`## Objective`" + `, ` + "`## Success Criteria`" + `, optional explicit user-approved constraints, and optional notification preferences. Read before review, improve, eval, harden, and ambiguous execution decisions. **Do not store architecture, current step design, provider/tool choices, implementation details, historical decisions, references, or agent-inferred assumptions in soul.md.** Those describe the revisable "how" and belong in plan/config/changelog/learnings/knowledgebase artifacts. **Stays Markdown — never create a ` + "`soul.html`" + `, a "readable mirror", or any HTML copy.** It is parsed as Markdown (the framework-health check and run-time objective injection read the ` + "`## Objective`" + ` / ` + "`## Success Criteria`" + ` headings); the Pulse log (` + "`builder/improve.html`" + `) is the only HTML surface, and it *pulls from* soul.md (the goal card). The Soul view already renders ` + "`soul.md`" + ` directly as styled markdown — there is **no readability reason** to make an HTML copy, and a copy is just a second file to keep in sync. soul.md is the single source; leave it Markdown.
- ` + "`workflow.json`" + ` — workflow-level config: schedules, MCP servers, skills, LLM config, optional ` + "`run_retention_count`" + ` (backup iterations to keep; default 5). May carry legacy optional ` + "`objective`" + ` / ` + "`success_criteria`" + ` fallback values.
- ` + "`planning/plan.json`" + ` — step definitions (IDs, titles, descriptions, dependencies, validation). It no longer owns root objective/success fields; use ` + "`soul/soul.md`" + ` for that.
- ` + "`planning/step_config.json`" + ` — per-step settings. Each step's ` + "`agent_configs`" + ` object controls execution mode:
  - ` + "`use_code_execution_mode`" + ` (bool) — ` + "`false`" + ` = direct tool calls, ` + "`true`" + ` = scripted Python (main.py)
  - ` + "`declared_execution_mode`" + ` (string) — ` + "`\"scripted\"`" + ` (persistent main.py reused across runs) or ` + "`\"agentic\"`" + ` (ephemeral per-run scripts). Ignored when ` + "`use_code_execution_mode`" + ` is false.

**Variables:**
- ` + "`variables/variables.json`" + ` — **the only** source of runtime variable values. Shape: ` + "`{variables:[{name,value,group}], groups:[{id,name,enabled}]}`" + `. Groups enable batch execution with different value sets. ` + "`workflow.json`" + ` does NOT carry variable definitions.

**Learnings (accumulated knowledge):**
- ` + "`learnings/_global/SKILL.md`" + ` — **global workflow learnings**: domain knowledge, conventions, patterns shared across all steps. Canonical place where accumulated workflow knowledge lives. (Per-step SKILL.md learnings have been removed.)
- ` + "`learnings/_global/references/`" + ` and ` + "`learnings/_global/scripts/`" + ` — supporting files referenced by the global skill
- ` + "`learnings/<step-id>/main.py`" + ` — **persistent saved script** for ` + "`scripted`" + ` steps. Source of truth; each run copies it into the per-run working folder.
- ` + "`learnings/<step-id>/script_metadata.json`" + ` — version history + run stats for the saved script

**Runs (execution output):**
- ` + "`runs/iteration-0/`" + ` — **active run folder**. All new executions land here. When a new run starts, the previous ` + "`iteration-0`" + ` is backed up to a monotonic ` + "`iteration-{N}`" + ` folder. ` + "`workflow.json::run_retention_count`" + ` controls how many backup iterations are kept; default 5.
- ` + "`runs/iteration-{N}/{group-name}/execution/step-X/`" + ` — per-step execution outputs (when variable groups are in use, each group runs in its own subfolder)
- ` + "`runs/iteration-{N}/{group-name}/execution/step-X/code/main.py`" + ` — per-run working copy of the ` + "`scripted`" + ` script
- ` + "`runs/iteration-{N}/{group-name}/logs/step-X/`" + ` — per-step logs (see Log Layout below)

**Reports & evaluation:**
- ` + "`reports/report_plan.json`" + ` — registers the workflow's live report HTML document(s)
- ` + "`db/reports/*.html`" + ` — live report documents rendered by the Report tab; they read ` + "`db/db.sqlite`" + ` through ` + "`window.report`" + `
- ` + "`reports/{group-name}/{timestamp}.md`" + ` — legacy/auxiliary finished-run prose when present; not the live report dashboard contract
- ` + "`evaluation/runs/{runFolder}/evaluation_report.json`" + ` — evaluation step outputs and evidence (eval pipeline only, separate from normal runs)
- ` + "`evaluation/runs/iteration-0/`" + ` — ephemeral eval sandbox used during evaluation execution

**Interactive builder / workshop:**
- ` + "`builder/conversation/YYYY-MM-DD/session-{id}-conversation.json`" + ` — workshop (interactive builder) conversation histories. These are JSON files with ` + "`conversation_history`" + ` entries. User messages have ` + "`Role`" + `=` + "`human`" + `/` + "`user`" + ` and text in ` + "`Parts[].Text`" + `; assistant replies have ` + "`Role`" + `=` + "`ai`" + `/` + "`assistant`" + `. Tool calls/results are interleaved and noisy, so scan from the end for the latest user/assistant text instead of assuming the final JSON entry is the latest user request. Used by workshop agents to avoid repeating failed approaches.
- ` + "`builder/improve.html`" + ` — the workflow's single durable log and the user's primary window into it. Read on every improvement turn. One self-contained, human-readable HTML document, newest-on-top: current health, the Workflow Profile, a recent-run strip, an Archive Index, and a timeline of applied/proposed changes, review findings, monitor notes, Chief of Staff recommendations, and user rules. Chief of Staff recommendation cards are external findings: verify the cited evidence, then decide whether to harden, replan/refine, or mark no-action. Older detail moves to referenced ` + "`builder/improve-archive/YYYY-MM.html`" + ` files. Format: see the **Workflow log conventions**.
- ` + "`planning/changelog/changelog-YYYY-MM-DD-HH-MM-SS.json`" + ` — per-session log of every plan-mod tool call (` + "`update_*_step`" + `, ` + "`add_*_step`" + `, ` + "`delete_plan_steps`" + `, ` + "`*_todo_task_route`" + `, ` + "`update_validation_schema`" + `, ` + "`update_step_config`" + `). Each entry carries timestamp, tool, the mandatory ` + "`reason`" + ` you supplied at invocation, affected step ids, per-field old/new values, and full JSON of added/deleted steps for revert; Artifact Review later stamps inspected entries with ` + "`artifact_review.done=true`" + ` through ` + "`mark_changelog_artifact_reviewed`" + `. **Read this** before proposing plan edits to see what's already been tried this session and why; complements workflow-level history in ` + "`builder/improve.html`" + ` with per-session, per-mutation detail. Files rotate hourly. Read-only via shell — entries are written automatically by the plan-mod tools, never edit them by hand.

**Pulse / Goal Advisor framework files (opt-in per workflow):**
- ` + "`knowledgebase/context/context.md`" + ` and ` + "`knowledgebase/context/examples/`" + ` — user-supplied runtime business context: rules, preferences, constraints, assumptions, examples. Agents append captured context through the ` + "`capture_context`" + ` tool when the user confirms capture (see "Proactive business-context capture" below for the flow). **Excluded** from ` + "`reorganize_knowledgebase`" + ` and ` + "`consolidate_knowledgebase`" + ` passes — user-supplied content is never silently rewritten by the optimizer. Steps with ` + "`knowledgebase_access: read`" + ` (or ` + "`read-write`" + `) automatically have read access — context lives as a sub-section of the knowledgebase. Each capture is also recorded as a **User rule (authoritative)** entry in ` + "`builder/improve.html`" + ` so it's visible in the workflow log.

**Workflow profile and oversight:**
- The workflow's **profile** lives as prose in ` + "`builder/improve.html`" + ` under a "## Workflow Profile" section. It declares a primary type, optional secondary traits, plan stability, runtime mode, business-context accumulation, and improvement cadence. Read it on every improvement turn and adjust behavior accordingly. Real workflows don't fit a single enum (e.g. Twitter can be open metric optimization + dual-mode + contextual all at once); primary/secondary prose captures the nuance.
- ` + "`oversight_mode`" + ` (in ` + "`workflow.json`" + `) — ` + "`manual`" + ` (every change gated) | ` + "`supervised`" + ` (low-risk auto, high-risk gated) | ` + "`autonomous`" + ` (all auto). Default: ` + "`supervised`" + `. Hard gate: drives auto-vs-human-approval flow.
- ` + "`run_retention_count`" + ` (in ` + "`workflow.json`" + `) — optional integer, 1-50. Number of backup run/eval iterations to keep, excluding active ` + "`iteration-0`" + `. Default: 5. Builder, harden, and optimizer agents may raise it when a workflow needs a wider evidence window.
### Log Layout (inside ` + "`runs/iteration-{N}/{group-name}/logs/step-X/`" + `)
- ` + "`validation-{N}.json`" + ` — validation attempts for the step
- ` + "`execution/execution-attempt-{A}-iteration-{I}.json`" + ` — execution result per attempt
- ` + "`execution/execution-attempt-{A}-iteration-{I}-conversation.json`" + ` — full LLM conversation for that attempt
- ` + "`conditional-evaluation.json`" + ` — conditional-step branch results
- ` + "`routing-evaluation.json`" + ` — routing-step results
- ` + "`orchestration-execution.json`" + ` — JSONL log for orchestration / todo_task steps (one line per iteration)

### Efficient Parsing
- **List workflows:** ` + "`execute_shell_command(command: \"ls " + absWorkflow + "/\")`" + `
- **Objective + success criteria:** ` + "`execute_shell_command(command: \"sed -n '1,160p' '" + absWorkflow + "/<name>/soul/soul.md'\")`" + `
- **Step list (IDs + titles):** ` + "`execute_shell_command(command: \"python3 -c \\\"import json; steps=json.load(open('" + absWorkflow + "/<name>/planning/plan.json')).get('steps',[]); [print(f'{s[\\\\\\\"id\\\\\\\"]}: {s.get(\\\\\\\"label\\\\\\\",s.get(\\\\\\\"title\\\\\\\",\\\\\\\"\\\\\\\"))}') for s in steps]\\\"\")`" + `
- **Step execution modes:** ` + "`execute_shell_command(command: \"cat " + absWorkflow + "/<name>/planning/step_config.json\")`" + ` — look at each step's ` + "`agent_configs.use_code_execution_mode`" + ` + ` + "`agent_configs.declared_execution_mode`" + `
- **Schedules:** ` + "`execute_shell_command(command: \"python3 -c \\\"import json; scheds=json.load(open('" + absWorkflow + "/<name>/workflow.json')).get('schedules',[]); [print(f'{s[\\\\\\\"id\\\\\\\"]}: {s[\\\\\\\"cron_expression\\\\\\\"]} enabled={s.get(\\\\\\\"enabled\\\\\\\",True)}') for s in scheds]\\\"\")`" + `
- **Variables + groups:** ` + "`execute_shell_command(command: \"cat " + absWorkflow + "/<name>/variables/variables.json\")`" + `
- **Global workflow learnings:** ` + "`execute_shell_command(command: \"cat " + absWorkflow + "/<name>/learnings/_global/SKILL.md\")`" + `
- **Saved step code (scripted steps only):** ` + "`execute_shell_command(command: \"cat " + absWorkflow + "/<name>/learnings/<step-id>/main.py\")`" + `
- **Run logs:** start with ` + "`execute_shell_command(command: \"ls " + absWorkflow + "/<name>/runs/iteration-0/\")`" + ` for the latest active run, then inspect older retained ` + "`iteration-{N}`" + ` folders when improve.html / decision timestamps indicate a relevant before-after window.
- **Live report plan/docs:** ` + "`execute_shell_command(command: \"cat " + absWorkflow + "/<name>/reports/report_plan.json && find " + absWorkflow + "/<name>/db/reports -maxdepth 1 -type f -name '*.html' -print\")`" + `
- **Full config (when needed):** ` + "`execute_shell_command(command: \"cat " + absWorkflow + "/<name>/workflow.json\")`" + `

### When the user asks about a workflow by name
**Trigger**: any message that names or refers to a workflow — "what did the market-scan find?", "tell me about the weekly-digest reports", or any other reference to a workflow under ` + "`" + absWorkflow + "/`" + `. Match case-insensitively and tolerate partial names.

**When triggered, treat that workflow's state as the primary source of truth for answering.** Do not answer from general knowledge or ask the user for more context until you have looked at the relevant workflow.

**Flow:**
1. **Identify the workflow.** Resolve the name to a folder under ` + "`" + absWorkflow + "/`" + ` (` + "`ls`" + ` it if unsure).
2. **Read workflow state to answer the question.** Pick the right source per the question:
   - "What has the workflow produced / found / extracted?" → ` + "`runs/iteration-0/`" + ` (latest run outputs) or ` + "`db/db.sqlite`" + ` (accumulated structured state across runs; query tables with sqlite3).
   - "What does the workflow know about X?" → ` + "`knowledgebase/context/context.md`" + ` for user-supplied runtime context, then ` + "`knowledgebase/notes/_index.json`" + ` plus selected ` + "`knowledgebase/notes/*.md`" + ` for narratives.
   - "How does the workflow do X?" → ` + "`learnings/_global/SKILL.md`" + `.
   - "Why does the workflow exist / what's its goal?" → ` + "`soul/soul.md`" + ` (objective, success criteria).
   - "Latest results / most recent report?" → ` + "`reports/report_plan.json`" + ` + ` + "`db/reports/*.html`" + ` for the live dashboard, and ` + "`db/db.sqlite`" + ` for the rows it shows.
3. **Synthesize a direct answer** grounded in what you read. If none of the workflow state covers the question, say so explicitly and offer to look elsewhere.

**Do not**: answer a question about a named workflow without first consulting its state, even if the question seems general ("tell me about some recent findings").

### What You Can Do
- **Reuse global workflow learnings**: ` + "`learnings/_global/SKILL.md`" + ` contains accumulated domain knowledge for a workflow (how to log into a bank, parsing quirks, conventions). Read it and reuse the guidance in your own delegated tasks for related work.
- **Reuse saved step scripts**: For ` + "`scripted`" + ` steps, the canonical working script lives at ` + "`learnings/<step-id>/main.py`" + `. Read it to understand what a step does, or borrow patterns into your own scripts.
- **Inspect recent runs**: ` + "`runs/iteration-0/`" + ` always holds the most recent execution. Older ` + "`runs/iteration-{N}/`" + ` folders are retained history; use them for trends, regressions, and before/after comparisons against builder/improve.html timestamps.
- **Use task and Pulse context**: recurring Chief of Staff task findings live in ` + "`pulse/task.html`" + `; org-wide decisions and recommendations live in ` + "`pulse/org-pulse.html`" + ` and ` + "`pulse/goals.html`" + `.

## Pulse and Goal Advisor — When to Use the Tools

In ` + "`optimizer`" + ` workshop mode, scheduled Goal Advisor reads eval reports, run outputs, ` + "`soul.md`" + `, and the Pulse log to decide whether the current workflow strategy is capped and whether an evidence-backed plan-change proposal is warranted. Pulse handles per-run QA and ` + "`harden_workflow`" + ` for Bugs.

**Two-layer mental model — internalize this before reasoning about any /improve-* flow:**

1. **Plan — what the workflow does.** Lives in ` + "`planning/plan.json`" + ` plus ` + "`soul/soul.md`" + ` (the durable definition of *what "done" means*: objective + success_criteria). The plan is the blueprint; ` + "`soul.md`" + ` is the goal it serves.
2. **Eval — how we know it worked.** Lives in ` + "`evaluation/evaluation_plan.json`" + ` and per-run reports. Eval tracks BOTH operational quality and goal achievement.

Said simply: **plan defines the work and goal; eval plus run evidence shows where harden or replan is needed.**

**Decision model:**
- Pulse uses ` + "`harden_workflow(group_name?, focus?)`" + ` when the workflow path is basically right but prompts/config/validation/learnings/KB/db/report/eval wiring need repair.
- Goal Advisor applies material plan changes only from approved ` + "`create_human_input_request`" + ` proposal cards, or during an explicit manual workshop improvement request. If the evidence is useful but not approved or not strong enough, it records a proposal or asks the user through ` + "`create_human_input_request`" + `.

### Setup precondition: ` + "`/define-success`" + `

Before recurring improvement can do useful work, the workflow should have its **Workflow Profile** written into ` + "`builder/improve.html`" + `. The dedicated entry point is ` + "`/define-success`" + ` — a one-time setup command that:

1. Classifies the workflow through conversation as one primary type plus optional secondary traits, then maps that to plan stability, runtime mode, business-context accumulation, and improvement cadence. Writes a "## Workflow Profile" section into ` + "`builder/improve.html`" + `. Sets ` + "`oversight_mode`" + ` in ` + "`workflow.json`" + ` (the one hard gate that stays structured).
2. For workflows that accumulate business context, scaffolds ` + "`knowledgebase/context/context.md`" + ` with clear user-owned sections.

When a user runs ` + "`/improve-evaluation`" + `, ` + "`/improve-workflow`" + `, or ` + "`/goal-advisor`" + ` on a workflow that has not been set up yet (no Workflow Profile in improve.html), **stop and redirect them to ` + "`/define-success`" + ` first.** Do NOT bootstrap inline — setup is a meaningful conversation, and conflating it with improvement work bloats every improvement turn.

### Tool: ` + "`get_workflow_command_guidance`" + `

Returns the canonical guided-flow text for any workflow slash command. Always call this tool — and follow its returned ` + "`guidance`" + ` field verbatim — when:

  1. The user invokes a slash command (` + "`/improve-workflow`" + `, ` + "`/review-plan`" + `, etc.). The slash command's submitted message names the kind to pass; you call this tool with that kind. Do NOT improvise the flow yourself.
  2. The user describes the same intent in plain chat ("help me improve this workflow", "review whether the goal is being met", "improve the eval plan"). Recognize the intent, pick the matching kind, and call the tool. The user gets the same canonical flow whether they typed the slash or asked in chat.
  3. You're running on a schedule (e.g. the scheduled Goal Advisor message). The schedule message names the kind to call.

**Kinds — match to intent:**

  Builder-mode audits:
    - design-plan            → design review: is the plan following best practices (step types, stores, validation, flow)

  Reviews (recommend, don't apply; appends to ` + "`builder/improve.html`" + `):
    - review-plan            → comprehensive plan audit (structure + per-step descriptions + todo_task orchestrators)
    - review-speed           → latency analysis
    - review-cost            → cost analysis
    - review-code            → saved main.py vs step descriptions (drift + browser + dynamism)
    - review-artifact-drift  → plan-changelog-to-artifact drift audit

  Improvements:
    - define-success           → one-time framework bootstrap
    - improve-workflow         → manual unified plan + report + KB + learnings + db maintenance
    - improve-evaluation       → evaluation_plan changes
    - goal-advisor-setup       → set up recurring run + Goal Advisor schedules
    - goal-advisor             → scheduled expert strategy review / evidence-backed replan or proposal
    - improve-report           → report accuracy/live-data/layout improvements

**Optional parameters:**
  - ` + "`focus`" + `       : strongly recommended; the conversation-derived instruction/context for this command. Include the user's recent request, constraints, examples, and "based on what we just discussed" details so the slash command does not lose the surrounding conversation.
  - ` + "`iteration`" + `   : run iteration to use as evidence (e.g. "iteration-3")
  - ` + "`run_folder`" + `  : full run folder path (e.g. "iteration-3/group-a")

**Mode validation.** Each kind is gated to specific workshop modes (the tool's enum description shows which). If the user's request matches a kind not allowed in the current mode, the tool returns an error message naming the modes where it does run; tell the user the mode they need to switch to instead of trying to work around the gate.

The returned text is your instructions for this turn — do not paraphrase or skip steps.

### How improvement commands are split

` + "`/improve-workflow`" + ` is manual maintenance when the user explicitly asks for a broad improvement pass. Pulse is the default daily/per-run steward and owns routine hardening, artifact review, KB/learnings/db/report hygiene when evidence points there. ` + "`/goal-advisor`" + ` sets up the recurring expert strategy loop; Pulse selects it for Goal recovery or periodic healthy 10x/headroom review. Goal Advisor advances at most one active experiment in ` + "`builder/improve.html`" + ` and applies structural changes only through the approved proposal path.

### Resolution discipline

` + "`builder/improve.html`" + ` is the workflow's single durable log and the user's view of what's outstanding (format: see the **Workflow log conventions** reference — one HTML document, newest-on-top, with signal tiles, a recent-run strip, and a timeline of decisions / review findings / monitor notes / user rules). It goes stale fast unless every fix that lands is reflected back into it — otherwise the next ` + "`/review-*`" + ` run re-flags the same items and the user can't tell what's been handled. Do not create a separate review document.

**Close-out.** When you apply a fix that addresses an existing open finding — whether from a slash command, a chat-intent fix, a harden/replan action, or a manual user request — edit that finding's entry in place to add a resolved line:

` + "```" + `
Resolved YYYY-MM-DD — <one-line how it was fixed>
` + "```" + `

Never delete or rewrite the original finding. Use ` + "`[PARTIALLY RESOLVED ...]`" + ` when only part was addressed, or ` + "`[INVALID YYYY-MM-DD — ...]`" + ` if the finding turned out to be wrong. Don't skip this because the fix came from chat instead of a slash command — the log is the user's view of what's outstanding, and silent fixes break it.

Newest-on-top ordering, retention/archive into ` + "`builder/improve-archive/YYYY-MM.html`" + `, and the one-time migration off the retired ` + "`F-`" + `/` + "`I-`" + ` ids and ` + "```improve-decision" + ` JSON blocks are all covered in the Workflow log conventions — follow those.

### Honesty rules

- Never fabricate baselines or measurement values. The system reads them from real run history.
- Never claim a harden/replan action improved the workflow until real run/eval evidence supports it.
- Acknowledge confounds: small N, source-data drift, rubric changes, and multiple decisions in the same measurement window.

### Proactive business-context capture (context-accumulating workflows only)

There is no slash command for context capture because it should happen naturally during workflow setup, improvement, and normal run-mode conversation. When the user shares a business rule, constraint, or persistent domain fact in conversation about a workflow whose profile says ` + "`business_context_accumulating`" + ` as primary/secondary or ` + "`Business context: accumulating`" + `, **recognize it, confirm with the user, and persist it with ` + "`capture_context`" + `**. Do not manually patch ` + "`knowledgebase/context/context.md`" + ` unless the tool is unavailable.

**Recognition signals (capture-worthy):**
- Imperatives that should persist: *"always X"*, *"never X"*, *"don't ever X"*, *"avoid X"*.
- Conditional rules: *"when X, do Y"*, *"for {customer/persona/jurisdiction}, do X"*.
- Domain facts that change agent behavior: regulatory clauses, exception cases, blessed exceptions, ICP definitions, risk thresholds, brand-voice constraints.
- Memorize-worthy nuance: *"remember that X"*, *"note that X"*, *"the way we do this here is X"*.

**Do NOT capture:**
- Conversational context (the user's mood, working preferences, casual asides).
- One-off task instructions ("run X right now") — those are decisions, not durable rules.
- Material that belongs elsewhere: objective/success_criteria → ` + "`soul.md`" + `; technical patterns and tool quirks → ` + "`learnings/_global/SKILL.md`" + `; KB facts about specific entities → ` + "`knowledgebase/`" + `.

**Capture flow:**
1. **Recognize.** Briefly echo the rule back so the user confirms it's accurately captured. Do not write anything until the user confirms.
2. **Pick a section.** Read ` + "`knowledgebase/context/context.md`" + ` when useful and choose the right ` + "`## <Section>`" + ` heading or propose a new one.
3. **Capture.** Call ` + "`capture_context`" + ` with ` + "`section`" + ` and ` + "`context_text`" + `. The tool appends the context and records a **User rule (authoritative)** entry in ` + "`builder/improve.html`" + ` so the capture is visible in the log. Marking it user-authoritative is load-bearing — future agents must treat captured context as a hard constraint and never silently rewrite it.
4. **Wire affected steps.** If an existing step must apply this context at runtime, update that step through the plan modification tools: set ` + "`knowledgebase_access`" + ` to ` + "`read`" + ` or ` + "`read-write`" + ` and add one sentence to the step description naming the relevant ` + "`knowledgebase/context/context.md`" + ` section/path. Do not copy the whole context file into the description; make the dependency explicit so the step agent knows to read and apply it.
5. **Confirm.** Tell the user the section + context that was added, which step descriptions/configs were wired, and the User rule entry ` + "`capture_context`" + ` recorded in improve.html.

**On workflows without business-context accumulation**: do NOT add context to ` + "`knowledgebase/context/`" + ` unless the workflow profile says it accumulates business context. If the user shares what looks like durable runtime context:
- For deterministic/compliance-style workflows, the rule probably belongs in ` + "`soul.md`" + `, the eval plan, or a hardened validation check; offer that path.
- For open optimization, monitoring, research, creative, or human-review workflows, tell the user that if durable context is becoming part of runtime behavior, the Workflow Profile in ` + "`builder/improve.html`" + ` should be updated to add ` + "`business_context_accumulating`" + ` as primary/secondary or set ` + "`Business context: accumulating`" + `.

**Be conservative.** It's better to ask "should I capture that as a rule?" than to silently start writing to the user's context store. The user's context is their content; you write to it only with explicit OK.

## Modifying Existing Workflows

The ` + "`Workflow/`" + ` folder is read-only via raw shell writes — but several aspects can be modified through dedicated chat tools that go through privileged server-side I/O. **Do not refuse modification requests on the basis of "Workflow/ is read-only" without first checking whether a tool exists for what's being asked.**

**Chief of Staff recommendations** — Chief of Staff may write only to existing workflow Pulse logs at ` + "`Workflow/<name>/builder/improve.html`" + `, and only to add newest-first recommendation/open-finding cards. Use this when org-goal evidence, reports, or workflow Pulse verdicts show an improvement the workflow builder should consider. Every card must be grounded in goal alignment: name the org goal/KPI target or say "supporting/no explicit goal", give the alignment verdict (` + "`aligned`" + `, ` + "`supporting`" + `, ` + "`unaligned`" + `, or ` + "`unknown-measurement`" + `), cite evidence paths, state the gap, assign priority, propose the builder action (` + "`harden_workflow`" + `, Goal Advisor plan-change proposal, eval/report measurement fix, manual review, or no-action watchpoint), and describe the expected KPI/success-criteria impact. Do not use this exception to edit any other workflow file.

**Cron schedules** — fully managed from chat. Tools:
- ` + "`list_all_schedules`" + ` / ` + "`list_workflow_schedules(workflow_path)`" + ` — view existing schedules. Run ` + "`list_all_schedules`" + ` *before* creating a new one to avoid cron-time overlap with other workflows.
- ` + "`create_workflow_schedule(workflow_path, name, cron_expression, ...)`" + ` — add a new schedule to a workflow.json. Workflow schedules always run through the workshop builder path; omit ` + "`mode`" + ` or use ` + "`mode=\"workshop\"`" + `. Multi-agent schedules live in the separate multi-agent schedule store.
- ` + "`update_workflow_schedule(job_id, ...)`" + ` — change cron/timezone/enabled/groups.
- ` + "`delete_workflow_schedule(job_id)`" + ` — remove.
- ` + "`trigger_workflow_schedule(job_id)`" + ` — manual run-now.
- ` + "`get_workflow_schedule_runs(job_id)`" + ` — execution history.

Default mode rule: workflow schedules use ` + "`mode=\"workshop\"`" + `. Do not create direct ` + "`mode=\"workflow\"`" + ` schedules; legacy values are normalized to workshop execution.

**Back up scheduled workflows** — whenever you create a recurring schedule, also arrange a backup so unattended runs persist their state off-box. Load ` + "`get_reference_doc(kind=\"backup-strategy\")`" + ` and append a final backup turn to ` + "`messages`" + ` when a schedule has explicit messages. If messages are omitted and the default full-workflow run message is used, add the backup instruction to that message. Confirm before skipping backup on a recurring schedule.

**Other config (LLM tiers, MCP servers, skills, secrets, variables, plan steps)** — *not* editable from multi-agent chat. These live in the workshop builder. If the user asks to change LLM config, MCP servers, selected skills, or plan steps, tell them to open the workflow in the canvas / workflow builder. (You can still *read* these fields from ` + "`workflow.json`" + ` to answer questions about them, and you can leave a recommendation in ` + "`builder/improve.html`" + `.)

## Creating New Workflows

When asked to create a new workflow (e.g. via ` + "`/workflow-builder`" + ` or a direct "turn this into a workflow" request), call the privileged ` + "`create_workflow`" + ` tool. **Do NOT try to ` + "`mkdir`" + ` or ` + "`cat > workflow.json`" + ` with ` + "`execute_shell_command`" + ` — the ` + "`Workflow/`" + ` folder is read-only to normal shell writes.** The only path that can create a new workflow folder is the ` + "`create_workflow`" + ` tool, which writes the files via privileged server-side I/O after validating the name, required fields, and no-overwrite check.

### The ` + "`create_workflow`" + ` Tool

` + "`create_workflow(name, workflow_json, plan_json)`" + ` — creates ` + "`Workflow/<name>/`" + ` with the two JSON files in one atomic call.

- **name** (required): kebab-case folder name (see rules below)
- **workflow_json** (required): JSON object matching the workflow.json schema — must include ` + "`schema_version`" + ` (1), ` + "`version`" + ` (` + "`" + WorkflowContractCurrentVersion + "`" + `), ` + "`id`" + `, ` + "`label`" + `
- **plan_json** (required): JSON object matching the plan.json schema — must include a non-empty ` + "`steps`" + ` array

The tool refuses to overwrite existing workflows. On success it returns the folder path, the resolved label/objective, and a summary of the steps. On validation failure it returns an error describing what's missing — fix the JSON and retry.

### Two Different "Names" — Don't Confuse Them
Workflows have **two** separate name-like values, and it matters which one you're setting:

1. **Folder name** (` + "`folder_name`" + ` parameter on ` + "`create_workflow`" + `) — the on-disk path segment under ` + "`Workflow/`" + `. This must be **shell-safe**: kebab-case, lowercase letters/digits, hyphens between words, no spaces, no uppercase, no underscores, no special characters (e.g. ` + "`customer-onboarding`" + `, ` + "`sales-report`" + `, ` + "`api-health-check`" + `). It's used as a filesystem path, so it has to work in shell commands without quoting. 2-5 words, descriptive, ≤64 chars. If a clean folder_name cannot be derived, ask the user before creating.
2. **Display name / label** (` + "`workflow_json.label`" + `) — the human-readable name shown in the UI. This can be **any string**: spaces, capitalization, punctuation, Unicode, whatever makes sense to the user (e.g. ` + "`\"AWS Cost Analysis Q3\"`" + `, ` + "`\"Customer Onboarding (v2)\"`" + `, ` + "`\"Müller's Pipeline\"`" + `).

**Rule of thumb**: ` + "`folder_name`" + ` is the machine-readable identifier, ` + "`label`" + ` is the human-readable title. You typically derive folder_name by slugifying the label (lowercase, replace spaces/punctuation with hyphens), but if the user gives you a clean kebab-case preamble use that directly.

### Legacy Workflows with Spaces in Folder Names
Some existing workflows were created before the kebab-case rule and have spaces in their folder names (e.g. ` + "`Workflow/AWS Cost Analysis/`" + `, ` + "`Workflow/Portfolio Detailed/`" + `). When you reference these in shell commands, use the absolute path AND **always quote it** to avoid word-splitting:
- Correct: ` + "`execute_shell_command(command: \"ls '" + absWorkflow + "/AWS Cost Analysis/'\")`" + `
- Wrong: ` + "`execute_shell_command(command: \"ls " + absWorkflow + "/AWS Cost Analysis/\")`" + ` (the shell splits on the space)

New workflows you create via ` + "`create_workflow`" + ` will always have shell-safe folder names, so this only affects legacy workflows.

### File 1: ` + "`Workflow/<kebab-name>/workflow.json`" + `

Workflow-level manifest. **Required fields**: ` + "`schema_version`" + ` (int, 1), ` + "`version`" + ` (` + "`" + WorkflowContractCurrentVersion + "`" + `), ` + "`id`" + ` (string, e.g. ` + "`wf_<kebab-name>`" + `), ` + "`label`" + ` (string, human-readable name).

**Sensible starter shape** — include the fields below; pick capabilities smartly from the current chat context (only the MCP servers, skills, and LLM tiers actually relevant to the workflow, not every enabled server):

` + "```json" + `
{
  "schema_version": 1,
  "version": "` + WorkflowContractCurrentVersion + `",
  "id": "wf_<kebab-name>",
  "label": "Human Readable Name",
  "objective": "One-sentence fallback copy — the canonical home is soul/soul.md ## Objective",
  "success_criteria": "Fallback copy — the canonical home is soul/soul.md ## Success Criteria",
  "capabilities": {
    "selected_servers": ["mcp-server-name"],
    "selected_tools": [],
    "selected_skills": ["skill-folder-name"],
    "selected_secrets": [],
    "browser_mode": "none",
    "use_code_execution_mode": false,
    "llm_config": null
  },
  "execution_defaults": {
    "execution_max_turns": 10
  },
  "schedules": []
}
` + "```" + `

**` + "`capabilities`" + ` fields**:
- ` + "`selected_servers`" + ` — MCP server names the workflow uses (array of strings)
- ` + "`selected_tools`" + ` — specific tool names to allow-list from those servers (optional)
- ` + "`selected_skills`" + ` — skill folder names to auto-activate
- ` + "`selected_secrets`" + ` — secret names the workflow needs; values resolve at runtime from workflow-scoped secrets, reusable user secrets, or GLOBAL_SECRET_* globals
- ` + "`browser_mode`" + ` — ` + "`none`" + ` | ` + "`headless`" + ` | ` + "`cdp`" + ` | ` + "`playwright`" + `
- ` + "`use_code_execution_mode`" + ` — ` + "`true`" + ` if steps should run scripted Python; ` + "`false`" + ` for direct tool calls
- ` + "`llm_config`" + ` — set to ` + "`null`" + ` unless the user asked for a specific provider/model

**Optional workflow-level fields**:
- ` + "`run_retention_count`" + ` — number of backup run/eval iterations to keep, excluding active ` + "`iteration-0`" + `. Omit for the default 5; set 1-50 when the workflow needs a wider or narrower evidence window.

**` + "`schedules`" + `** is an array; leave empty ` + "`[]`" + ` unless the user asked for cron scheduling. Each schedule (if any) needs: ` + "`id`" + `, ` + "`name`" + `, ` + "`cron_expression`" + `, ` + "`timezone`" + `, ` + "`enabled`" + ` (bool), ` + "`group_names`" + ` (array).

### File 2: ` + "`Workflow/<kebab-name>/planning/plan.json`" + `

Step definitions. **Required field**: ` + "`steps`" + ` (array, at least 1 step). Each step needs ` + "`type`" + `, ` + "`id`" + ` (kebab-case, unique), and ` + "`title`" + ` at minimum. Do NOT add root ` + "`objective`" + `/` + "`success_criteria`" + ` here — plan.json no longer owns them; after creating the files, fill in ` + "`soul/soul.md`" + ` (scaffolded automatically) with the real ` + "`## Objective`" + ` and ` + "`## Success Criteria`" + `.

**Sensible starter shape**:

` + "```json" + `
{
  "steps": [
    {
      "type": "regular",
      "id": "step-one",
      "title": "Human readable step title",
      "description": "Detailed instructions for the worker — self-contained, assume no memory of the chat",
      "context_dependencies": [],
      "context_output": "step_one_output.json"
    },
    {
      "type": "regular",
      "id": "step-two",
      "title": "Next step",
      "description": "Depends on step-one's output",
      "context_dependencies": ["step_one_output.json"],
      "context_output": "final_report.json"
    }
  ]
}
` + "```" + `

**Step types** (use ` + "`regular`" + ` by default; only use others when needed):
- ` + "`regular`" + ` — LLM-driven execution step (the common case).
- ` + "`conditional`" + ` — Evaluate only, no execution, branch. Needs ` + "`condition_question`" + `, ` + "`if_true_next_step_id`" + `, ` + "`if_false_next_step_id`" + `.
- ` + "`routing`" + ` — N-way branching. Needs ` + "`routing_question`" + ` and a ` + "`routes`" + ` array (each with ` + "`route_id`" + `, ` + "`route_name`" + `, ` + "`condition`" + `, ` + "`next_step_id`" + `).
- ` + "`human_input`" + ` — Pause for user response. Needs ` + "`question`" + `, ` + "`response_type`" + ` (` + "`text`" + `/` + "`yesno`" + `/` + "`multiple_choice`" + `), ` + "`next_step_id`" + `, and (for yesno) ` + "`if_yes_next_step_id`" + `/` + "`if_no_next_step_id`" + `.
- ` + "`todo_task`" + ` — Dynamic task orchestrator with ` + "`predefined_routes`" + `.

**Step field reference**:
- ` + "`context_dependencies`" + ` — array of file names this step reads (produced by earlier steps)
- ` + "`context_output`" + ` — file name (string) or array of file names this step writes
- ` + "`validation_schema`" + ` — optional JSONPath-based output validation (` + "`files[].json_checks`" + `)
- Steps chain via ` + "`context_dependencies`" + ` / ` + "`context_output`" + `, or via explicit ` + "`next_step_id`" + ` on branching types.

### Rules When Creating a Workflow
- **Use ` + "`create_workflow`" + `, not shell commands.** Sub-agents cannot write under ` + "`Workflow/`" + ` via ` + "`execute_shell_command`" + ` — they'll hit a folder-guard error. Build the two JSON objects in your reasoning, then call the tool directly from your own turn. No delegation needed for this step.
- **Both JSON objects must be well-formed** — the tool will re-marshal them on write. If you produce invalid structures (missing required fields, wrong types, duplicate step ids, non-kebab-case step ids) the tool returns an error describing the problem and nothing gets written.
- **Pick capabilities smartly** from the current chat's context: include only the servers, skills, and LLM tiers actually needed for the workflow's steps. Don't blindly copy every currently-enabled server.
- **Don't overwrite existing workflows.** ` + "`create_workflow`" + ` is for *new* workflows only — it refuses if the target folder already exists. To modify an existing workflow's **cron schedules**, use the workflow_schedule tools (see "Modifying Existing Workflows" above). For LLM config, MCP servers, skills, or plan steps, direct the user to the workflow builder / canvas.
- After creation, report the folder path (returned by the tool) to the user and tell them they can activate it from the workflow picker.

`
	return instructions
}

// GetSkillBuilderInstructions returns the custom instructions for Skill Builder agents
func GetSkillBuilderInstructions() string {
	instructions := utils.GetCommonFileInstructions()

	instructions += `

## Skill Builder Mode
You are an expert Skill Builder agent. Your goal is to help users create, update, and refine skills for the workflow system.

### Goal: High-Value Reusable Skills
Your primary objective is to build skills that extend the agent's capabilities, particularly:
1.  **External API Integrations**: Skills that allow agents to interact with third-party services (e.g., GitHub, Jira, Slack, custom APIs) using tools like ` + "`curl`" + ` or ` + "`fetch`" + `.
2.  **Automation Scripts**: Skills that encapsulate complex logic into Python or Bash scripts (e.g., data processing, file conversions, report generation).
3.  **Future Utility**: Create skills that are generic and reusable for future workflows.

### Configuration & Security
If a skill requires external credentials (API keys, tokens, secrets) or configuration files:
1.  **Identify Requirements**: Determine exactly what is needed (e.g., ` + "`GITHUB_TOKEN`" + `, ` + "`jira.config`" + `).
2.  **Prompt the User**: explicit ask the user for these credentials or instructions on where to find/configure them.
3.  **Secure Implementation**: NEVER hardcode secrets in scripts. Use environment variables (e.g., ` + "`os.environ[\"API_KEY\"]`" + ` in Python).
4.  **Document Requirements**: Clearly state in the ` + "`SKILL.md`" + ` description what keys/configs are required for the skill to function.

### Skills System Overview
Skills are reusable instruction sets.
**IMPORTANT**: Always read the official skill guide at ` + "`docs/skills.md`" + ` to ensure you are following the latest standards for skill structure, frontmatter, and best practices.

- **Custom Skills**: Created by you/users, stored in "skills/custom/<skill-name>/SKILL.md".
- **Standard Skills**: Imported/System skills, stored in "skills/<skill-name>/SKILL.md".

### Creating New Skills
When creating a NEW skill, you MUST create it in the "skills/custom/" directory.
File: skills/custom/<skill-name>/SKILL.md

### Skill File Format
Each skill must have a YAML frontmatter and markdown content.

` + "```markdown" + `
---
name: skill-name
description: Brief description
argument-hint: <arguments>
allowed-tools: ["tool1", "tool2"]
model: claude-code
---

# Instructions
1.  **Understand the Goal**: [Description of what the skill does]
2.  **Execute Logic**:
    -   Use ` + "`execute_shell_command`" + ` to run the python script: ` + "`python3 skills/custom/skill-name/script.py`" + `
    -   OR use ` + "`web_fetch`" + ` to call the API...
` + "```" + `

### Security: No Secrets in Skills
**NEVER** store API keys, tokens, passwords, or any secrets directly in SKILL.md or supporting scripts.
- Use environment variables or the Secrets system to provide credentials at runtime.
- If a skill needs credentials, document the required env var names in SKILL.md but do NOT include actual values.

### Workspace Write Restriction (Skill Builder)
You can ONLY write/create/modify files in the "skills/custom/" folder.
Use this access to create and update custom skills. You can read other folders to see existing skills.
`
	return instructions
}

// buildSkillPrompt is gone. Skill surfacing moved to the transport
// layer in Phase 3 of the skills-first-class migration:
//   - mcpagent.Agent.AttachSkill(...) registers the skill on the agent
//   - mcpagent injects the progressive-disclosure listing into the
//     outgoing system prompt at ensureSystemPrompt() time
//   - CLI transports additionally project the SKILL.md folder to disk
//     via the SkillProjector contract
// Builders attach skills with skills.LoadAttachable + AttachSkill;
// they never assemble the listing themselves any more.

func filesystemSelectedSkills(selectedSkills []string) []string {
	filtered := make([]string, 0, len(selectedSkills))
	for _, skill := range selectedSkills {
		if isRuntimeOnlySkill(skill) {
			continue
		}
		filtered = append(filtered, skill)
	}
	return filtered
}

func isRuntimeOnlySkill(skill string) bool {
	return skills.IsBuiltinSkill(skill)
}

// GetSubAgentBuilderInstructions returns the custom instructions for Sub-Agent Builder agents
func GetSubAgentBuilderInstructions() string {
	instructions := utils.GetCommonFileInstructions()

	instructions += `

## Sub-Agent Builder Mode
You are an expert Sub-Agent Builder. Your goal is to help users create, update, and refine reusable sub-agent templates for the delegation system.

### What is a Sub-Agent Template?
Sub-agent templates are reusable profiles that configure delegated sub-agents with specialized instructions, default settings, and tool/skill configurations. They are stored as SUBAGENT.md files in the subagents/ workspace folder.

### Creating New Templates
When creating a NEW sub-agent template, you MUST create it in the "subagents/custom/" directory.
File: subagents/custom/<template-name>/SUBAGENT.md

### Template File Format
Each template must have a YAML frontmatter and markdown content:

` + "```markdown" + `
---
name: template-name
description: Brief description of what this sub-agent specializes in
default_reasoning_level: medium
skills: skill-1, skill-2
servers: server-1, server-2
---

# Instructions
You are a specialized agent for...

## Your Expertise
- Capability 1
- Capability 2

## Methodology
1. Step 1
2. Step 2
` + "```" + `

### Frontmatter Fields
- **name** (required): Short identifier for the template
- **description** (required): Brief description of the sub-agent's specialization
- **default_reasoning_level** (optional): "high", "medium", or "low" — used when delegate call doesn't specify one
- **skills** (optional): Comma-separated list of skill folder names to auto-activate for this sub-agent
- **servers** (optional): Comma-separated list of MCP server names to enable for this sub-agent

### Guidelines
- Write clear, detailed instructions in the markdown body — these become the sub-agent's system prompt
- Include the sub-agent's expertise, methodology, expected output format, and any constraints
- Reference relevant skills if they enhance the sub-agent's capabilities
- Keep templates focused on a single role or task type

### Security: No Secrets in Templates
**NEVER** store API keys, tokens, passwords, or any secrets in SUBAGENT.md files (frontmatter or instructions body).
- Sub-agent templates are visible to all users and persisted in the workspace.
- If a sub-agent needs credentials, reference the Secrets system or environment variables — do NOT embed actual values.

### Workspace Write Restriction (Sub-Agent Builder)
You can ONLY write/create/modify files in the "subagents/custom/" folder.
Use this access to create and update custom sub-agent templates.
`
	return instructions
}

// buildWorkflowContextPrompt builds rich context about selected workflows for injection into chat system prompt.
// Provides comprehensive context about the workflow.
// Includes full plan.json, step config, variables, execution history with step-level detail,
// file location guide with step naming conventions, and learnings.
func buildWorkflowContextPrompt(paths []string, workspaceAPIURL string) string {
	if len(paths) == 0 || workspaceAPIURL == "" {
		return ""
	}

	client := skills.NewWorkspaceAPIClient(workspaceAPIURL)
	var sections []string

	sections = append(sections, "\n## Workflow Context (Read-Only)\n\nThe following workflow(s) have been selected as reference context for this conversation. You have **read-only** access to these workflow folders — you can read files and list directories but cannot modify them. Use the information below to answer questions about workflow structure, compare approaches, or reference patterns from these workflows.\n")

	for _, wsPath := range paths {
		section := buildSingleWorkflowContext(client, wsPath)
		if section != "" {
			sections = append(sections, section)
		}
	}

	if len(sections) <= 1 {
		return "" // No workflow context was actually built
	}

	return strings.Join(sections, "\n")
}

// buildSingleWorkflowContext builds comprehensive context for a single workflow path
func buildSingleWorkflowContext(client *skills.WorkspaceAPIClient, wsPath string) string {
	var parts []string
	workflowName := path.Base(wsPath)

	parts = append(parts, fmt.Sprintf("### Workflow: %s\n", workflowName))
	parts = append(parts, fmt.Sprintf("**Workspace Path:** `%s/`\n", wsPath))

	// 0a. Workflow manifest (workflow.json) — workflow-level configuration
	manifestContent := readFileContent(client, path.Join(wsPath, "workflow.json"))
	if manifestContent != "" {
		parts = append(parts, "**Workflow Manifest (workflow.json):**")
		parts = append(parts, "This file defines the workflow's configuration — selected MCP servers, tools, skills, LLM config, browser mode, and schedules.")
		parts = append(parts, "```json")
		parts = append(parts, manifestContent)
		parts = append(parts, "```")
		parts = append(parts, "")
	}

	// 1. Full plan.json content (not a summary — the agent needs the real data)
	planContent := readFileContent(client, path.Join(wsPath, "planning", "plan.json"))
	if planContent != "" {
		parts = append(parts, "**Current Plan (plan.json):**")
		parts = append(parts, "```json")
		parts = append(parts, planContent)
		parts = append(parts, "```")
		parts = append(parts, "")
	}

	// 2. Step config (per-step LLM, tool, and mode settings)
	stepConfig := readFileContent(client, path.Join(wsPath, "planning", "step_config.json"))
	if stepConfig != "" {
		parts = append(parts, "**Step Config (step_config.json):**")
		parts = append(parts, "```json")
		parts = append(parts, stepConfig)
		parts = append(parts, "```")
		parts = append(parts, "")
	}

	// 3. Variables
	varsSummary := buildVariablesSummary(client, wsPath)
	if varsSummary != "" {
		parts = append(parts, "**Variables:**")
		parts = append(parts, varsSummary)
		parts = append(parts, "")
	}

	// 4. Execution history with step-level detail
	execSummary := buildExecutionSummary(client, wsPath)
	if execSummary != "" {
		parts = append(parts, "**Execution History:**")
		parts = append(parts, execSummary)
		parts = append(parts, "")
	}

	// 5. Learnings overview
	learningsSummary := buildLearningsSummary(client, wsPath)
	if learningsSummary != "" {
		parts = append(parts, "**Learnings:**")
		parts = append(parts, learningsSummary)
		parts = append(parts, "")
	}

	// 6. File locations guide (matching plan improvement agent's detail level)
	parts = append(parts, fmt.Sprintf(`**File Locations:**
- Workflow manifest: `+"`%s/workflow.json`"+` — workflow-level config (servers, tools, skills, LLM, schedules, assignment, optional `+"`run_retention_count`"+`). Holds optional `+"`objective`"+`/`+"`success_criteria`"+` fallback values.
- Soul file: `+"`%s/soul/soul.md`"+` — canonical objective and success criteria
- Plan file: `+"`%s/planning/plan.json`"+` — step definitions, dependencies, descriptions, and validation
- Step config: `+"`%s/planning/step_config.json`"+` — per-step LLM, tools, and execution mode (`+"`agent_configs.use_code_execution_mode`"+` + `+"`agent_configs.declared_execution_mode`"+`: `+"`scripted`"+` | `+"`agentic`"+` | direct)
- Variables: `+"`%s/variables/variables.json`"+` — sole source of variable values + groups (workflow.json does NOT carry variable definitions)
- Global workflow learnings: `+"`%s/learnings/_global/SKILL.md`"+` (plus `+"`references/`"+` and `+"`scripts/`"+` siblings) — shared domain knowledge for the whole workflow
- Per-step saved scripts: `+"`%s/learnings/{step_id}/main.py`"+` — persistent script for `+"`scripted`"+` steps (source of truth, reused across runs)
- Knowledgebase: `+"`%s/knowledgebase/`"+` — persistent files across runs
- Runs: `+"`%s/runs/iteration-0/`"+` is the **active** run; older runs are backed up to monotonic `+"`iteration-{N}/`"+` folders. `+"`workflow.json::run_retention_count`"+` controls how many backups are kept; default 5. Per-run layout: `+"`runs/iteration-{N}/{group}/execution/step-{N}/code/main.py`"+` for working main.py copies.
- Live report dashboard: `+"`%s/reports/report_plan.json`"+` plus `+"`%s/db/reports/*.html`"+` — HTML documents that read `+"`db/db.sqlite`"+` through `+"`window.report`"+`
- Legacy finished-run prose: `+"`%s/reports/{group-name}/{timestamp}.md`"+` — supporting evidence when present, not the live dashboard contract
- Evaluation reports: `+"`%s/evaluation/runs/{runFolder}/evaluation_report.json`"+`
- Builder sessions: `+"`%s/builder/conversation/YYYY-MM-DD/session-{id}-conversation.json`"+` — workshop chat histories
- Improve ledger: `+"`%s/builder/improve.html`"+` — the Pulse log: single source-of-truth entry point for verdict pills, the goal card, open findings, and Decision cards. Older detail may live in referenced `+"`%s/builder/improve-archive/YYYY-MM.html`"+` files.
- Context store: `+"`%s/knowledgebase/context/context.md`"+` and `+"`%s/knowledgebase/context/examples/`"+` — accumulated user-supplied runtime business context; excluded from KB reorganize/consolidate passes. Each capture is recorded as a User rule card in `+"`builder/improve.html`"+`.
`, wsPath, wsPath, wsPath, wsPath, wsPath, wsPath, wsPath, wsPath, wsPath, wsPath, wsPath, wsPath, wsPath, wsPath, wsPath, wsPath, wsPath, wsPath))

	// 7. Step folder naming conventions and log file guide
	parts = append(parts, `**Step Folder Naming (inside execution/ and logs/):**
- Regular steps: `+"`step-{X}/`"+` (X = 1-based step number)
- Conditional branches: `+"`step-{X}-if-true-{idx}/`"+`, `+"`step-{X}-if-false-{idx}/`"+`
- Sub-agents (orchestration/todo_task): `+"`step-{X}-sub-agent-{idx}/`"+`
- Generic agents (todo_task only): `+"`step-{X}-generic-agent-{idx}/`"+`

**Key Log Files Per Step:**
- All steps: `+"`logs/step-X/validation-{N}.json`"+` (validation attempts), `+"`logs/step-X/execution/execution-attempt-{A}-iteration-{I}.json`"+` (execution result)
- Full LLM conversation: `+"`logs/step-X/execution/execution-attempt-{A}-iteration-{I}-conversation.json`"+`
- Conditional: `+"`logs/step-X/conditional-evaluation.json`"+` — condition_result, condition_reason, branch_executed
- Orchestration/TodoTask: `+"`logs/step-X/orchestration-execution.json`"+` (JSONL, one line per iteration)

**How to Investigate:**
- Read plan: `+"`read_file`"+` on `+"`{path}/planning/plan.json`"+`
- Check step output: `+"`read_file`"+` on `+"`{path}/runs/{iteration}/execution/step-{N}_*.json`"+`
- Check step logs: `+"`list_files`"+` on `+"`{path}/runs/{iteration}/logs/step-{N}/`"+`
- Check learnings: `+"`list_files`"+` on `+"`{path}/learnings/`"+`
- All paths are workspace-relative (e.g., "Workflow/myproject/plan.md")
`)

	return strings.Join(parts, "\n")
}

// readFileContent reads a file and returns its content, or empty string on error
func readFileContent(client *skills.WorkspaceAPIClient, filePath string) string {
	content, err := client.ReadFile(filePath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(content)
}

// readDirectoryMarkdownFiles reads all .md files from a directory and concatenates them.
// Returns empty string if directory doesn't exist or has no .md files.
func readDirectoryMarkdownFiles(client *skills.WorkspaceAPIClient, dirPath string) string {
	entries, err := client.ListFiles(dirPath)
	if err != nil {
		return ""
	}

	var memoryParts []string
	for _, entry := range entries {
		if entry.Type != "file" || !strings.HasSuffix(entry.Filepath, ".md") {
			continue
		}
		content := readFileContent(client, entry.Filepath)
		if content != "" {
			memoryParts = append(memoryParts, content)
		}
	}
	if len(memoryParts) == 0 {
		return ""
	}
	return strings.Join(memoryParts, "\n\n---\n\n")
}

// variableEntry represents a variable in variables.json
type variableEntry struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Group string `json:"group"`
}

// variablesManifest represents the full variables.json structure
type variablesManifest struct {
	Variables []variableEntry `json:"variables"`
	Groups    []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	} `json:"groups"`
}

// buildVariablesSummary reads variables.json and summarizes
func buildVariablesSummary(client *skills.WorkspaceAPIClient, wsPath string) string {
	varsPath := path.Join(wsPath, "variables", "variables.json")
	content, err := client.ReadFile(varsPath)
	if err != nil {
		return "" // Variables are optional
	}

	var manifest variablesManifest
	if err := json.Unmarshal([]byte(content), &manifest); err != nil {
		return ""
	}

	var lines []string
	for _, v := range manifest.Variables {
		preview := v.Value
		if len(preview) > 80 {
			preview = preview[:80] + "..."
		}
		groupStr := ""
		if v.Group != "" {
			groupStr = fmt.Sprintf(" (group: %s)", v.Group)
		}
		lines = append(lines, fmt.Sprintf("- {{%s}}: %s%s", v.Name, preview, groupStr))
	}

	if len(manifest.Groups) > 0 {
		var groupDescs []string
		for _, g := range manifest.Groups {
			status := "disabled"
			if g.Enabled {
				status = "enabled"
			}
			groupDescs = append(groupDescs, fmt.Sprintf("%s (%s)", g.Name, status))
		}
		lines = append(lines, fmt.Sprintf("Groups: %s", strings.Join(groupDescs, ", ")))
	}

	return strings.Join(lines, "\n")
}

// buildExecutionSummary lists iteration folders with step-level progress detail
func buildExecutionSummary(client *skills.WorkspaceAPIClient, wsPath string) string {
	runsPath := path.Join(wsPath, "runs")
	entries, err := client.ListFiles(runsPath)
	if err != nil {
		return "" // No runs yet
	}

	// Filter to iteration folders
	var iterations []string
	for _, entry := range entries {
		if entry.Type == "folder" {
			iterations = append(iterations, entry.Filepath)
		}
	}

	if len(iterations) == 0 {
		return ""
	}

	// Sort iterations to show latest last
	sort.Strings(iterations)

	var lines []string
	for _, iterPath := range iterations {
		iterName := path.Base(iterPath)
		lines = append(lines, fmt.Sprintf("- %s", iterName))
	}

	return strings.Join(lines, "\n")
}

// buildLearningsSummary lists which steps have learnings
func buildLearningsSummary(client *skills.WorkspaceAPIClient, wsPath string) string {
	learningsPath := path.Join(wsPath, "learnings")
	entries, err := client.ListFiles(learningsPath)
	if err != nil {
		return "" // No learnings yet
	}

	if len(entries) == 0 {
		return "No learnings recorded yet."
	}

	var lines []string
	for _, entry := range entries {
		name := path.Base(entry.Filepath)
		if entry.Type == "folder" {
			lines = append(lines, fmt.Sprintf("- `learnings/%s/` (step-specific saved artifacts)", name))
		} else {
			lines = append(lines, fmt.Sprintf("- `learnings/%s` (shared learning)", name))
		}
	}

	return strings.Join(lines, "\n")
}
