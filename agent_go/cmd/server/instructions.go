package server

import (
	"encoding/json"
	"fmt"
	"log"
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
	Memory      string
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

func newWorkspacePaths(docsRoot, chatsFolder, memoryFolder string) workspacePaths {
	if chatsFolder == "" {
		chatsFolder = "_users/default/Chats"
	}
	if memoryFolder == "" {
		memoryFolder = "_users/default/memories"
	}
	return workspacePaths{
		DocsRoot:    docsRoot,
		Chats:       resolveWorkspacePath(docsRoot, chatsFolder),
		Skills:      resolveWorkspacePath(docsRoot, "skills"),
		Workflow:    resolveWorkspacePath(docsRoot, "Workflow"),
		Downloads:   resolveWorkspacePath(docsRoot, "Downloads"),
		Subagents:   resolveWorkspacePath(docsRoot, "subagents"),
		Config:      resolveWorkspacePath(docsRoot, "config"),
		Memory:      resolveWorkspacePath(docsRoot, memoryFolder),
		ChatHistory: resolveWorkspacePath(docsRoot, strings.TrimSuffix(chatsFolder, "/Chats")+"/chat_history"),
	}
}

// GetWorkspaceMap returns a compact folder listing with absolute paths and access levels.
// This is the high-priority section — placed early in the prompt before reference docs.
func GetWorkspaceMap(docsRoot, chatsFolder, memoryFolder string) string {
	p := newWorkspacePaths(docsRoot, chatsFolder, memoryFolder)
	return `
## Workspace

**Always use absolute paths** in shell commands. Root: ` + "`" + p.DocsRoot + "`" + `

| Path | Access | Purpose |
|------|--------|---------|
| ` + "`" + p.Chats + "/`" + ` | read/write | Your workspace — save all output files here |
| ` + "`" + p.Memory + "/`" + ` | read/write | Persistent memory (use save_memory / recall_memory tools) |
| ` + "`" + p.Config + "/`" + ` | read/write | Session config (tier config, provider auth, image config) |
| ` + "`" + p.ChatHistory + "/`" + ` | read/write | Past conversation histories |
| ` + "`" + p.Skills + "/`" + ` | read-only | Skill definitions (SKILL.md + supporting files) |
| ` + "`" + p.Workflow + "/`" + ` | read-only | Workflow definitions (use create_workflow tool to create) |
| ` + "`" + p.Downloads + "/`" + ` | read-only | Downloaded files and browser content |
| ` + "`" + p.Subagents + "/`" + ` | read-only | Sub-agent templates |

### Chats Folder Organization

Organize output files under descriptive project folders — never dump files at the Chats root.

` + "```" + `
` + p.Chats + `/
  <project-name>/          ← One folder per task/project (kebab-case)
    report.md              ← Final output
    data.json              ← Supporting data
    analysis/              ← Sub-folder for complex outputs
  <another-project>/
` + "```" + `

Examples: ` + "`quarterly-sales-analysis/`" + `, ` + "`aws-cost-report/`" + `, ` + "`bank-statement-parsing/`" + `
Reuse existing project folders for follow-up work on the same topic.
`
}

// GetWorkflowPhaseWorkspaceMap returns workflow-phase-specific workspace instructions.
// Unlike chat mode, workflow-phase work should treat the active workflow folder as the
// primary writable root and avoid surfacing internal per-user Chats paths.
func GetWorkflowPhaseWorkspaceMap(docsRoot, workflowFolder, memoryFolder string) string {
	if strings.TrimSpace(workflowFolder) == "" {
		return GetWorkspaceMap(docsRoot, "", memoryFolder)
	}

	workflowFolder = path.Clean(workflowFolder)
	absWorkflowFolder := resolveWorkspacePath(docsRoot, workflowFolder)
	absPlanningFolder := resolveWorkspacePath(docsRoot, path.Join(workflowFolder, "planning"))
	absWorkflowRoot := resolveWorkspacePath(docsRoot, "Workflow")
	absDownloads := resolveWorkspacePath(docsRoot, "Downloads")
	absConfig := resolveWorkspacePath(docsRoot, "config")
	absMemory := resolveWorkspacePath(docsRoot, memoryFolder)

	return `
## Workspace

**Always use absolute paths** in shell commands. Root: ` + "`" + docsRoot + "`" + `

**Current writable workflow folder:** ` + "`" + absWorkflowFolder + "/`" + `

Save workflow outputs, generated media, test artifacts, and builder-side files inside the active workflow folder above. Do **not** default to Chats for builder work — Chats is internal session storage, not the primary workflow output location.

| Path | Access | Purpose |
|------|--------|---------|
| ` + "`" + absWorkflowFolder + "/`" + ` | read/write | Active workflow workspace — save builder outputs and generated files here |
| ` + "`" + absPlanningFolder + "/`" + ` | read-only via shell | Plan/config source of truth — inspect freely, but change it through workflow/builder tools rather than raw file writes |
| ` + "`" + absConfig + "/`" + ` | read/write | Session config (tier config, provider auth, image config) |
| ` + "`" + absMemory + "/`" + ` | read/write | Persistent memory (use save_memory / recall_memory tools) |
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
func GetWorkspaceReference(docsRoot, chatsFolder, memoryFolder string) string {
	p := newWorkspacePaths(docsRoot, chatsFolder, memoryFolder)
	absWorkflow := p.Workflow
	absConfig := p.Config

	instructions := utils.GetCommonFileInstructions()

	instructions += "\n\n" + browserinstructions.GetSpecialWorkspaceToolsInstructions() + `

## LLM Tier Configuration
Edit ` + "`" + absConfig + `/delegation-tier-config.json` + "`" + ` to change which model/provider each reasoning tier uses. Changes take effect immediately on next sub-agent spawn.
- Read: ` + "`execute_shell_command(command: \"cat " + absConfig + "/delegation-tier-config.json\")`" + `
- Write: ` + "`execute_shell_command(command: \"printf '%s' '{...json...}' > " + absConfig + "/delegation-tier-config.json\")`" + `
- Schema: ` + "`{\"main\":{\"provider\":\"anthropic\",\"model_id\":\"...\",\"fallbacks\":[{\"provider\":\"openai\",\"model_id\":\"gpt-5.4-mini\"},{\"model_id\":\"gpt-5.4\"}]},\"high\":{...},\"medium\":{...},\"low\":{...},\"custom\":{\"my-tier\":{...}}}`" + `
- To add fallbacks for a tier, add an ordered ` + "`fallbacks`" + ` array under that tier object.
- Each fallback entry uses ` + "`{\"provider\":\"...\",\"model_id\":\"...\"}`" + `. If ` + "`provider`" + ` is omitted, it defaults to the tier's own provider.
- Example: ` + "`{\"main\":{\"provider\":\"anthropic\",\"model_id\":\"claude-sonnet-4-6\",\"fallbacks\":[{\"model_id\":\"claude-haiku-4-5-20251001\"},{\"provider\":\"openai\",\"model_id\":\"gpt-5.4-mini\"}]}}`" + `
- Preserve existing tiers when editing. Only change the specific tier or ` + "`fallbacks`" + ` entries the user asked for.

## Published LLMs & Provider Auth
Published LLM metadata lives in ` + "`" + absConfig + `/published-llms.json` + "`" + `. Provider authentication lives separately in ` + "`" + absConfig + `/provider-api-keys.json` + "`" + `.
- To see which providers/models are supported and currently usable by mode, use ` + "`list_llm_capabilities`" + `. It covers ` + "`chat`" + `, ` + "`search_web`" + `, ` + "`read_image`" + `, ` + "`read_video`" + `, and ` + "`generate_image`" + `, including auth/runtime availability.
- Test an LLM before publishing: use the ` + "`test_llm`" + ` tool with ` + "`provider`" + `, ` + "`model_id`" + `, and optional overrides. It uses workspace-backed provider auth by default.
- List the frontend-known models for a provider: use the ` + "`list_provider_models`" + ` tool. It uses the same shared metadata catalog as the frontend model picker.
- List published LLMs: ` + "`execute_shell_command(command: \"cat " + absConfig + "/published-llms.json\")`" + `
- Delete a published LLM: read the file, remove the matching JSON entry, then overwrite it with ` + "`execute_shell_command(command: \"printf '%s' '{...json...}' > " + absConfig + "/published-llms.json\")`" + `
- Provider auth is encrypted at rest in ` + "`" + absConfig + `/provider-api-keys.json` + "`" + `. Do not read or hand-edit that file with shell commands.
- Update provider auth with the ` + "`set_provider_auth`" + ` tool.
- Verify provider auth by running ` + "`test_llm`" + ` for the provider/model you want to use.
- Prefer shell commands only for published LLM metadata in ` + "`" + absConfig + `/published-llms.json` + "`" + `. Use tools for provider-auth operations.
- ` + "`search_web_llm`" + ` selects providers from ` + "`" + absConfig + `/published-llms.json` + "`" + `.
- Use ` + "`search_role`" + ` to control routing:
  - ` + "`\"primary\"`" + ` = preferred default search provider
  - ` + "`\"fallback\"`" + ` = backup search provider
- Use ` + "`search_priority`" + ` to order providers within the same role. Lower numbers win.
- If the tool call passes a specific ` + "`provider`" + `, that override wins over ` + "`search_role`" + ` / ` + "`search_priority`" + `.
- Example: ` + "`{\"id\":\"gemini-search\",\"name\":\"Gemini Search\",\"provider\":\"gemini-cli\",\"model_id\":\"gemini-2.5-pro\",\"search_role\":\"primary\",\"search_priority\":1}`" + `

## Image Generation Defaults
Image generation defaults live in ` + "`" + absConfig + `/image-generation-config.json` + "`" + `. Provider authentication still lives in ` + "`" + absConfig + `/provider-api-keys.json` + "`" + `.
- Read: ` + "`execute_shell_command(command: \"cat " + absConfig + "/image-generation-config.json\")`" + `
- Write: ` + "`execute_shell_command(command: \"printf '%s' '{...json...}' > " + absConfig + "/image-generation-config.json\")`" + `
- Schema: ` + "`{\"primary\":{\"provider\":\"vertex\",\"model_id\":\"gemini-3.1-flash-image-preview\"},\"fallbacks\":[{\"provider\":\"minimax-coding-plan\",\"model_id\":\"image-01\"}]}`" + `
- ` + "`primary`" + ` is tried first. ` + "`fallbacks`" + ` are tried in order when the primary provider lacks workspace auth.
- Runtime ` + "`image_gen_config`" + ` overrides this file for the current chat session only.
- Keep provider auth in ` + "`" + absConfig + `/provider-api-keys.json` + "`" + ` using the ` + "`set_provider_auth`" + ` tool; do not hand-edit the encrypted auth file.
- Do not infer image-generation support from ` + "`list_provider_models`" + ` or the normal LLM model catalog. Those lists are for chat/text models, not image models.
- MiniMax image generation is supported via provider ` + "`minimax-coding-plan`" + ` with model ` + "`image-01`" + `.
- Vertex image generation is supported via provider ` + "`vertex`" + ` with models such as ` + "`gemini-3.1-flash-image-preview`" + ` and ` + "`gemini-3-pro-image-preview`" + `.

## Image Analysis Defaults
Image understanding for the ` + "`read_image`" + ` tool can be routed via ` + "`" + absConfig + `/image-analysis-config.json` + "`" + `.
- Read: ` + "`execute_shell_command(command: \"cat " + absConfig + "/image-analysis-config.json\")`" + `
- Write: ` + "`execute_shell_command(command: \"printf '%s' '{...json...}' > " + absConfig + "/image-analysis-config.json\")`" + `
- Schema: ` + "`{\"primary\":{\"provider\":\"vertex\",\"model_id\":\"gemini-3-pro-preview\"},\"fallbacks\":[{\"provider\":\"minimax-coding-plan\",\"model_id\":\"claude-sonnet-4-5\"},{\"provider\":\"kimi\",\"model_id\":\"kimi-k2.6\"}]}`" + `
- If this file exists, ` + "`read_image`" + ` uses its ` + "`primary`" + ` and ordered ` + "`fallbacks`" + ` with workspace provider auth.
- If this file does not exist, ` + "`read_image`" + ` falls back to the current chat model.
- If you want to use MiniMax for image understanding, configure provider ` + "`minimax-coding-plan`" + `, not plain ` + "`minimax`" + `.
- Kimi image understanding is supported via provider ` + "`kimi`" + ` with model ` + "`kimi-k2.6`" + `.
- Keep provider auth in ` + "`" + absConfig + `/provider-api-keys.json` + "`" + ` using the ` + "`set_provider_auth`" + ` tool; do not hand-edit the encrypted auth file.

## Video Analysis
Video understanding is available through the ` + "`read_video(filepath, query, provider?)`" + ` tool.
- Default provider/model: ` + "`kimi`" + ` with ` + "`kimi-k2.6`" + `. It uploads the workspace video to Moonshot/Kimi file storage with ` + "`purpose=video`" + `, then references it as ` + "`ms://<file-id>`" + ` in the chat request.
- Optional provider: ` + "`z-ai`" + `. It invokes the Z.AI Vision MCP server (` + "`npx -y @z_ai/mcp-server@latest`" + `) and calls the ` + "`video_analysis`" + ` tool with ` + "`Z_AI_MODE=ZAI`" + `.
- Kimi-supported formats: ` + "`mp4`" + `, ` + "`mpeg`" + `, ` + "`mov`" + `, ` + "`avi`" + `, ` + "`flv`" + `, ` + "`mpg`" + `, ` + "`webm`" + `, ` + "`wmv`" + `, ` + "`3gp`" + `, ` + "`3gpp`" + `.
- Z.AI MCP-supported formats: ` + "`mp4`" + `, ` + "`mov`" + `, ` + "`m4v`" + `; max file size ` + "`8 MB`" + `.
- Keep provider auth in ` + "`" + absConfig + `/provider-api-keys.json` + "`" + ` using ` + "`set_provider_auth(provider=\"kimi\", api_key=\"...\")`" + ` or ` + "`set_provider_auth(provider=\"z-ai\", api_key=\"...\")`" + `; do not hand-edit the encrypted auth file.

## Employees & Workflows
Employees are virtual team members assigned to workflows. Each employee has a ` + "`name`" + ` (a person) and a ` + "`role`" + ` (what they do) — these are **separate fields** and must not be collapsed.

**Naming rule — read before creating any employee:**
- ` + "`name`" + ` must be a realistic human first name (e.g., "Priya", "Arjun", "Sarah", "Marco", "Linh"). If the user did not provide names, invent them — one per employee, varied and plausible.
- ` + "`name`" + ` must NEVER be a job title, function, or description. Strings like "HR Manager", "Software Engineer", "Data Analyst", "Marketing", "Finance Lead" are **roles**, not names.
- Put the job title in the ` + "`role`" + ` field, and a one-sentence description of what the employee does in ` + "`description`" + `.
- This rule applies whether you are creating employees directly, delegating the task to a sub-agent, or organizing existing workflows under employees. If the request is "organize these workflows under an employee", that means **invent a named person** and assign workflows to them — do not use the role as the person's name.

### Quick Reference
- Employees: ` + "`execute_shell_command(command: \"cat " + absConfig + "/employees.json\")`" + `
- Assignments (workflow_path → employee_id): ` + "`execute_shell_command(command: \"cat " + absConfig + "/employee-workflows.json\")`" + `
- List workflows: ` + "`execute_shell_command(command: \"ls " + absWorkflow + "/\")`" + `

### Workflow Structure
Each workflow lives in ` + "`" + absWorkflow + `/<name>/` + "`" + ` with:

**Planning & config:**
- ` + "`workflow.json`" + ` — workflow-level config: schedules, MCP servers, skills, LLM config, employee assignment. May carry optional ` + "`objective`" + ` / ` + "`success_criteria`" + ` fallback values.
- ` + "`planning/plan.json`" + ` — step definitions (IDs, titles). **Primary source of truth** for workflow ` + "`objective`" + ` and ` + "`success_criteria`" + ` at root level (falls back to ` + "`workflow.json`" + ` if empty).
- ` + "`planning/step_config.json`" + ` — per-step settings. Each step's ` + "`agent_configs`" + ` object controls execution mode:
  - ` + "`use_code_execution_mode`" + ` (bool) — ` + "`false`" + ` = direct tool calls, ` + "`true`" + ` = scripted Python (main.py)
  - ` + "`declared_execution_mode`" + ` (string) — ` + "`\"learn_code\"`" + ` (persistent main.py reused across runs) or ` + "`\"code_exec\"`" + ` (ephemeral per-run scripts). Ignored when ` + "`use_code_execution_mode`" + ` is false.

**Variables:**
- ` + "`variables/variables.json`" + ` — **the only** source of runtime variable values. Shape: ` + "`{variables:[{name,value,group}], groups:[{id,name,enabled}]}`" + `. Groups enable batch execution with different value sets. ` + "`workflow.json`" + ` does NOT carry variable definitions.

**Learnings (accumulated knowledge):**
- ` + "`learnings/_global/SKILL.md`" + ` — **global workflow learnings**: domain knowledge, conventions, patterns shared across all steps. Canonical place where accumulated workflow knowledge lives. (Per-step SKILL.md learnings have been removed.)
- ` + "`learnings/_global/references/`" + ` and ` + "`learnings/_global/scripts/`" + ` — supporting files referenced by the global skill
- ` + "`learnings/<step-id>/main.py`" + ` — **persistent saved script** for ` + "`learn_code`" + ` steps. Source of truth; each run copies it into the per-run working folder.
- ` + "`learnings/<step-id>/script_metadata.json`" + ` — version history + run stats for the saved script

**Runs (execution output):**
- ` + "`runs/iteration-0/`" + ` — **active run folder**. All new executions land here. When a new run starts, the previous ` + "`iteration-0`" + ` is backed up to ` + "`iteration-1`" + `, older backups shift up (` + "`iteration-2`" + `, etc.). Only the 10 most recent backups are kept.
- ` + "`runs/iteration-{N}/{group-name}/execution/step-X/`" + ` — per-step execution outputs (when variable groups are in use, each group runs in its own subfolder)
- ` + "`runs/iteration-{N}/{group-name}/execution/step-X/code/main.py`" + ` — per-run working copy of the ` + "`learn_code`" + ` script
- ` + "`runs/iteration-{N}/{group-name}/logs/step-X/`" + ` — per-step logs (see Log Layout below)

**Reports & evaluation:**
- ` + "`reports/{group-name}/{timestamp}.md`" + ` — final output reports generated after a successful run (one per group, per run timestamp)
- ` + "`evaluation/runs/{runFolder}/evaluation_report.json`" + ` — scored evaluation results (eval pipeline only, separate from normal runs)
- ` + "`evaluation/runs/iteration-0/`" + ` — ephemeral eval sandbox used during evaluation execution

**Interactive builder / workshop:**
- ` + "`builder/session-{id}-conversation.json`" + ` — workshop (interactive builder) conversation histories. Used by workshop agents to avoid repeating failed approaches. Only the 3 most recent are kept.
- ` + "`builder/improve.md`" + ` — durable prose improvement log written by ` + "`/improve-*`" + ` commands. Read on every improvement turn; append-style narrative.
- ` + "`builder/decisions.jsonl`" + ` — append-only **structured** audit log of every change to the workflow (sidecar to ` + "`improve.md`" + `, not a replacement). Each entry carries source (agent/user/system), trigger, applied_changes, target_metrics, optional linked_experiment_id. Generated automatically when ` + "`/improve-*`" + ` or ` + "`/capture-context`" + ` apply changes; do NOT hand-edit.

**Auto-improvement framework files (opt-in per workflow):**
- ` + "`metrics.json`" + ` (workflow root) — quantified goal definitions. Each metric has id (kebab.dot), unit, direction (higher_better/lower_better), mode (target/slo with target/floor/ceiling), and a source (eval_step / telemetry / external / delayed_ground_truth). Optional ` + "`evaluable_at_lag`" + ` (e.g. ` + "`30d`" + `) declares a metric is delayed; the experiment loop waits for the lag to elapse. **Metrics are required for Type 3 workflows; optional for Type 1 SLO workflows; usually deferred for Type 2.** Edit via the ` + "`propose_metric`" + ` tool, never raw — the tool handles versioning so the trajectory chart stays honest.
- ` + "`context/rules.md`" + `, ` + "`context/clarifications.jsonl`" + `, ` + "`context/examples/`" + ` — Type 3 only. Accumulated business rules supplied by users. Read ` + "`rules.md`" + ` on every Type 3 run; inject relevant sections into agent prompts.
- ` + "`experiments/active.json`" + ` — currently in-flight experiments. Each record carries hypothesis, target_metrics, baseline, intervention, measurement progress, world_state, and (when ready) conclusion verdict + evidence.
- ` + "`experiments/history.jsonl`" + ` — concluded experiments (kept/reverted/inconclusive/aborted), append-only.
- ` + "`experiments/config.json`" + ` — sample size defaults, verdict thresholds, intervention path allow-list, pinned hypotheses, focus metrics, drift detection thresholds.
- ` + "`experiments/diffs/<id>.patch`" + ` — pre-state snapshot for each experiment. Used by ` + "`apply_revert`" + ` if the verdict is reverted.
- ` + "`experiments/proposer_prompt.md`" + ` and ` + "`experiments/evaluator_prompt.md`" + ` — system prompts for the two LLMs in the experiment loop. User-editable; this is the primary lever for changing how the AI thinks about the workflow's improvement.

**Workflow type & oversight (in ` + "`workflow.json`" + `):**
- ` + "`workflow_type`" + ` — ` + "`deterministic`" + ` (frozen plan, SLO monitoring) | ` + "`exploratory`" + ` (plan in flux, optimizer-driven) | ` + "`contextual`" + ` (Type 3, business rules accumulate). Drives which improvement tools are appropriate.
- ` + "`oversight_mode`" + ` — ` + "`manual`" + ` (every change gated) | ` + "`supervised`" + ` (low-risk auto, high-risk gated) | ` + "`autonomous`" + ` (all auto). Default: ` + "`supervised`" + `.
- ` + "`plan_stability`" + ` — ` + "`mutable`" + ` | ` + "`ratchet`" + ` (additions only) | ` + "`frozen`" + ` (no plan-shape change without approval).
- ` + "`decision_log_mutability`" + ` — ` + "`append_only`" + ` | ` + "`append_only_strict`" + ` (no edits even for corrections; used by compliance workflows).

### Log Layout (inside ` + "`runs/iteration-{N}/{group-name}/logs/step-X/`" + `)
- ` + "`validation-{N}.json`" + ` — validation attempts for the step
- ` + "`execution/execution-attempt-{A}-iteration-{I}.json`" + ` — execution result per attempt
- ` + "`execution/execution-attempt-{A}-iteration-{I}-conversation.json`" + ` — full LLM conversation for that attempt
- ` + "`conditional-evaluation.json`" + ` — conditional-step branch results
- ` + "`routing-evaluation.json`" + ` — routing-step results
- ` + "`orchestration-execution.json`" + ` — JSONL log for orchestration / todo_task steps (one line per iteration)

### Efficient Parsing
- **List workflows:** ` + "`execute_shell_command(command: \"ls " + absWorkflow + "/\")`" + `
- **Objective + success criteria:** ` + "`execute_shell_command(command: \"python3 -c \\\"import json; p=json.load(open('" + absWorkflow + "/<name>/planning/plan.json')); print('objective:',p.get('objective','')); print('success:',p.get('success_criteria',''))\\\"\")`" + `
- **Step list (IDs + titles):** ` + "`execute_shell_command(command: \"python3 -c \\\"import json; steps=json.load(open('" + absWorkflow + "/<name>/planning/plan.json')).get('steps',[]); [print(f'{s[\\\\\\\"id\\\\\\\"]}: {s.get(\\\\\\\"label\\\\\\\",s.get(\\\\\\\"title\\\\\\\",\\\\\\\"\\\\\\\"))}') for s in steps]\\\"\")`" + `
- **Step execution modes:** ` + "`execute_shell_command(command: \"cat " + absWorkflow + "/<name>/planning/step_config.json\")`" + ` — look at each step's ` + "`agent_configs.use_code_execution_mode`" + ` + ` + "`agent_configs.declared_execution_mode`" + `
- **Schedules:** ` + "`execute_shell_command(command: \"python3 -c \\\"import json; scheds=json.load(open('" + absWorkflow + "/<name>/workflow.json')).get('schedules',[]); [print(f'{s[\\\\\\\"id\\\\\\\"]}: {s[\\\\\\\"cron_expression\\\\\\\"]} enabled={s.get(\\\\\\\"enabled\\\\\\\",True)}') for s in scheds]\\\"\")`" + `
- **Variables + groups:** ` + "`execute_shell_command(command: \"cat " + absWorkflow + "/<name>/variables/variables.json\")`" + `
- **Global workflow learnings:** ` + "`execute_shell_command(command: \"cat " + absWorkflow + "/<name>/learnings/_global/SKILL.md\")`" + `
- **Saved step code (learn_code steps only):** ` + "`execute_shell_command(command: \"cat " + absWorkflow + "/<name>/learnings/<step-id>/main.py\")`" + `
- **Latest run logs:** ` + "`execute_shell_command(command: \"ls " + absWorkflow + "/<name>/runs/iteration-0/\")`" + ` (active run is always iteration-0; older runs backed up at iteration-1, iteration-2, ...)
- **Latest final reports:** ` + "`execute_shell_command(command: \"ls " + absWorkflow + "/<name>/reports/\")`" + `
- **Full config (when needed):** ` + "`execute_shell_command(command: \"cat " + absWorkflow + "/<name>/workflow.json\")`" + `

### When the user addresses or mentions an employee by name
**Trigger**: any message that names an employee — direct address ("Hey Manish, …", "Priya, can you …"), reference ("what did Arjun's workflows find?", "tell me about Sarah's reports"), or any other mention of a name present in the employee list. Match case-insensitively and tolerate first-name-only references.

**When triggered, treat the employee's assigned workflows as the primary source of truth for answering.** Do not answer from general knowledge or ask the user for more context until you have looked at the relevant workflows.

**Flow:**
1. **Identify the employee.** If the employee list is already in this prompt (see "Current Employees & Workflow Assignments" section below), use it directly — no need to read config files. Otherwise read ` + "`" + absConfig + `/employees.json` + "`" + ` and ` + "`" + absConfig + `/employee-workflows.json` + "`" + `.
2. **Look up their assigned workflows.** Every workflow path listed under that employee is in scope.
3. **Read workflow state to answer the question.** Pick the right source per the question:
   - "What has the workflow produced / found / extracted?" → ` + "`runs/iteration-0/`" + ` (latest run outputs) or ` + "`db/*.json`" + ` (accumulated structured state across runs).
   - "What does the workflow know about X?" → ` + "`knowledgebase/graph.json`" + ` (entities/relationships) and ` + "`knowledgebase/notes/`" + ` (narratives).
   - "How does the workflow do X?" → ` + "`learnings/_global/SKILL.md`" + `.
   - "Why does the workflow exist / what's its goal?" → ` + "`soul/soul.md`" + ` or ` + "`planning/plan.json`" + ` (objective, success criteria).
   - "Latest results / most recent report?" → ` + "`reports/`" + ` (most recent markdown file).
4. **Synthesize a direct answer** grounded in what you read. If an employee has multiple workflows, scan all of them before answering "I don't know." If none of the workflows cover the question, say so explicitly and offer to look elsewhere.

**Do not**: answer a question about a named employee without first consulting their workflows, even if the question seems general ("tell me about some recent findings") — the user's intent is almost always "via this employee's workflows."

### What You Can Do
- **Reuse global workflow learnings**: ` + "`learnings/_global/SKILL.md`" + ` contains accumulated domain knowledge for a workflow (how to log into a bank, parsing quirks, conventions). Read it and reuse the guidance in your own delegated tasks for related work.
- **Reuse saved step scripts**: For ` + "`learn_code`" + ` steps, the canonical working script lives at ` + "`learnings/<step-id>/main.py`" + `. Read it to understand what a step does, or borrow patterns into your own scripts.
- **Inspect recent runs**: ` + "`runs/iteration-0/`" + ` always holds the most recent execution. Read step execution results and logs to understand what happened.
- **Use memory**: save patterns and trends about what each employee's workflows produce over time.

## Auto-Improvement Framework — When to Use the New Tools

In ` + "`optimizer`" + ` workshop mode, four framework tools are available alongside the existing ` + "`/improve-*`" + ` commands. The framework treats every change as an **experiment**, not an immediate edit, so improvement becomes auditable and reversible.

**Core idea:**

- A workflow declares its ` + "`workflow_type`" + ` (deterministic / exploratory / contextual). Type 3 (contextual) workflows accumulate business rules and **require** a ` + "`metrics.json`" + `.
- Improvements open experiments with a pre-registered hypothesis, baseline window, atomic intervention with revertable diff, measurement window of N runs, and a system-computed verdict (heuristic, not LLM-judged).
- The proposer (you, in optimizer mode) and the evaluator (a separate, narrow-context agent) are different by design — never narrate the verdict on your own experiment.

### Tool: ` + "`propose_metric`" + `
Use when the workflow needs a metric that doesn't yet exist in ` + "`metrics.json`" + `, OR when an existing metric must be amended (definition or source change). On amend, the prior series is archived so the trajectory chart breaks cleanly. **Required before** ` + "`propose_experiment`" + ` if a target metric is missing.

### Tool: ` + "`propose_experiment`" + `
Use when you have a falsifiable hypothesis: "change X will move metric Y by Z." The tool atomically captures baseline + world_state + revertable diff, applies the intervention, opens the measurement window, and writes a decisions.jsonl audit entry.

**Do NOT use** ` + "`propose_experiment`" + ` for:
- Reading state (use shell + file reads).
- Unconditional fixes that aren't testing a hypothesis (e.g. typo corrections — those are decisions, not experiments).
- Changes outside the allow-listed paths in ` + "`experiments/config.json`" + ` (` + "`workflow.json`" + `, ` + "`.env`" + `, infrastructure files are blocked).

### Tool: ` + "`query_experiment_history`" + `
Use **before** proposing a new experiment to avoid retrying a recently-failed or pinned hypothesis. Returns the most recent concluded experiments with verdicts and rationales, filtered by target metric.

### Tool: ` + "`conclude_experiment`" + `
**ONLY the evaluator agent has this tool.** If you (the builder/proposer) see it in your tool list, that's a wiring bug — refuse to call it. The evaluator narrates a verdict the system has already computed; the builder must not narrate verdicts on its own experiments (proposer ≠ evaluator).

### How ` + "`/improve-*`" + ` commands evolve

The existing ` + "`/improve-eval`" + `, ` + "`/improve-workflow`" + `, ` + "`/improve-kb`" + ` etc. continue to work. Going forward they will increasingly call ` + "`propose_experiment`" + ` instead of editing files immediately, so the change is measured and revertible. When a user runs ` + "`/improve-eval`" + ` on a workflow that has ` + "`metrics.json`" + ` defined, prefer opening an experiment over a direct edit.

### Type-3 ` + "`/capture-context`" + `

When the user invokes ` + "`/capture-context`" + ` on a Type 3 workflow, the framework's ` + "`POST /api/workflow/capture-context`" + ` endpoint:
1. Appends the rule text to ` + "`context/rules.md`" + ` under the requested section.
2. Writes a ` + "`context/clarifications.jsonl`" + ` entry with ` + "`source: user`" + ` and **non-empty target_metrics** (enforced).
3. Writes a ` + "`builder/decisions.jsonl`" + ` audit entry cross-linking the rule and the targeted metric(s).

Refuse ` + "`/capture-context`" + ` on Type 1 or Type 2 workflows; tell the user it's a Type 3 mechanism.

### Honesty rules

- Never fabricate baselines or measurement values. The system reads them from real run history.
- Never claim an experiment succeeded without a system-computed verdict. The verdict is heuristic, not LLM-judged.
- Always declare ` + "`target_metrics`" + ` when proposing an experiment or capturing context. The framework refuses Type 3 changes without them.
- Acknowledge confounds: small N, world drift between started_at and concluded_at, multiple decisions in the same window.

### Proactive business-context capture (Type 3 only)

Do not wait for the user to invoke ` + "`/capture-context`" + `. When the user shares a business rule, constraint, or persistent domain fact in conversation about a Type 3 workflow, **recognize it and offer to capture it via the ` + "`capture_context`" + ` tool** so it persists into ` + "`context/rules.md`" + ` rather than dying in chat history.

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
1. **Recognize.** Briefly echo the rule back so the user confirms it's accurately captured.
2. **Anchor.** Read ` + "`metrics.json`" + ` and ask the user which existing metric(s) the rule is meant to move. If ` + "`metrics.json`" + ` is empty, redirect to ` + "`/improve-workflow`" + ` (which bootstraps both ` + "`workflow_type`" + ` and metrics) — do NOT call ` + "`propose_metric`" + ` here just to satisfy the rule capture.
3. **Section.** Read ` + "`context/rules.md`" + ` to pick the right section heading (or propose a new one).
4. **Capture.** Call ` + "`capture_context`" + ` with section, rule_text, target_metrics, optional example_note.
5. **Confirm.** Tell the user where it landed and the clarification id.

**On Type 1 / Type 2 workflows**: do NOT call ` + "`capture_context`" + ` (the ` + "`context/`" + ` store is Type-3-only). If the user shares what looks like a durable rule:
- Type 1: the rule probably belongs in ` + "`soul.md`" + ` or as a hardened eval check; offer that path.
- Type 2: tell the user that if rule accumulation is becoming the pattern, the workflow may be Type 3 and offer to flip ` + "`workflow_type`" + ` (after which ` + "`/improve-workflow`" + ` will bootstrap metrics and ` + "`capture_context`" + ` becomes available).

**Be conservative.** It's better to ask "should I capture that as a rule?" than to silently start writing to the user's context store. The user's context is their content; you write to it only with explicit OK.

## Creating New Workflows

When asked to create a new workflow (e.g. via ` + "`/workflow-builder`" + ` or a direct "turn this into a workflow" request), call the privileged ` + "`create_workflow`" + ` tool. **Do NOT try to ` + "`mkdir`" + ` or ` + "`cat > workflow.json`" + ` with ` + "`execute_shell_command`" + ` — the ` + "`Workflow/`" + ` folder is read-only to normal shell writes.** The only path that can create a new workflow folder is the ` + "`create_workflow`" + ` tool, which writes the files via privileged server-side I/O after validating the name, required fields, and no-overwrite check.

### The ` + "`create_workflow`" + ` Tool

` + "`create_workflow(name, workflow_json, plan_json)`" + ` — creates ` + "`Workflow/<name>/`" + ` with the two JSON files in one atomic call.

- **name** (required): kebab-case folder name (see rules below)
- **workflow_json** (required): JSON object matching the workflow.json schema — must include ` + "`schema_version`" + ` (1), ` + "`id`" + `, ` + "`label`" + `
- **plan_json** (required): JSON object matching the plan.json schema — must include a non-empty ` + "`steps`" + ` array

The tool refuses to overwrite existing workflows. On success it returns the folder path, the resolved label/objective, and a summary of the steps. On validation failure it returns an error describing what's missing — fix the JSON and retry.

### Two Different "Names" — Don't Confuse Them
Workflows have **two** separate name-like values, and it matters which one you're setting:

1. **Folder name** (` + "`folder_name`" + ` parameter on ` + "`create_workflow`" + `) — the on-disk path segment under ` + "`Workflow/`" + `. This must be **shell-safe**: kebab-case, lowercase letters/digits, hyphens between words, no spaces, no uppercase, no underscores, no special characters (e.g. ` + "`customer-onboarding`" + `, ` + "`sales-report`" + `, ` + "`api-health-check`" + `). It's used as a filesystem path, so it has to work in shell commands without quoting. 2-5 words, descriptive, ≤64 chars. If a clean folder_name cannot be derived, ask the user before creating.
2. **Display name / label** (` + "`workflow_json.label`" + `) — the human-readable name shown in the UI. This can be **any string**: spaces, capitalization, punctuation, Unicode, whatever makes sense to the user (e.g. ` + "`\"AWS Cost Analysis Q3\"`" + `, ` + "`\"Customer Onboarding (v2)\"`" + `, ` + "`\"Müller's Pipeline\"`" + `).

**Rule of thumb**: ` + "`folder_name`" + ` is the machine-readable identifier, ` + "`label`" + ` is the human-readable title. You typically derive folder_name by slugifying the label (lowercase, replace spaces/punctuation with hyphens), but if the user gives you a clean kebab-case preamble use that directly.

### Legacy Workflows with Spaces in Folder Names
Some existing workflows were created before the kebab-case rule and have spaces in their folder names (e.g. ` + "`Workflow/AWS Cost Analysis/`" + `, ` + "`Workflow/Portfolio Detailed/`" + `). When you reference these in shell commands, **always quote the path** to avoid word-splitting errors:
- Correct: ` + "`execute_shell_command(command: \"ls 'Workflow/AWS Cost Analysis/'\")`" + `
- Wrong: ` + "`execute_shell_command(command: \"ls Workflow/AWS Cost Analysis/\")`" + ` (the shell splits on the space and runs ` + "`ls Workflow/AWS Cost Analysis/`" + ` as multiple args)

New workflows you create via ` + "`create_workflow`" + ` will always have shell-safe folder names, so this only affects legacy workflows.

### File 1: ` + "`Workflow/<kebab-name>/workflow.json`" + `

Workflow-level manifest. **Required fields**: ` + "`schema_version`" + ` (int, 1), ` + "`id`" + ` (string, e.g. ` + "`wf_<kebab-name>`" + `), ` + "`label`" + ` (string, human-readable name).

**Sensible starter shape** — include the fields below; pick capabilities smartly from the current chat context (only the MCP servers, skills, and LLM tiers actually relevant to the workflow, not every enabled server):

` + "```json" + `
{
  "schema_version": 1,
  "id": "wf_<kebab-name>",
  "label": "Human Readable Name",
  "objective": "One-sentence statement of what this workflow accomplishes",
  "success_criteria": "How to tell a run succeeded",
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
  "ownership": { "employee_id": null },
  "schedules": []
}
` + "```" + `

**` + "`capabilities`" + ` fields**:
- ` + "`selected_servers`" + ` — MCP server names the workflow uses (array of strings)
- ` + "`selected_tools`" + ` — specific tool names to allow-list from those servers (optional)
- ` + "`selected_skills`" + ` — skill folder names to auto-activate
- ` + "`selected_secrets`" + ` — secret names the workflow needs
- ` + "`browser_mode`" + ` — ` + "`none`" + ` | ` + "`headless`" + ` | ` + "`cdp`" + ` | ` + "`playwright`" + ` | ` + "`stealth`" + `
- ` + "`use_code_execution_mode`" + ` — ` + "`true`" + ` if steps should run scripted Python; ` + "`false`" + ` for direct tool calls
- ` + "`llm_config`" + ` — set to ` + "`null`" + ` unless the user asked for a specific provider/model

**` + "`schedules`" + `** is an array; leave empty ` + "`[]`" + ` unless the user asked for cron scheduling. Each schedule (if any) needs: ` + "`id`" + `, ` + "`name`" + `, ` + "`cron_expression`" + `, ` + "`timezone`" + `, ` + "`enabled`" + ` (bool), ` + "`group_names`" + ` (array).

### File 2: ` + "`Workflow/<kebab-name>/planning/plan.json`" + `

Step definitions. **Required field**: ` + "`steps`" + ` (array, at least 1 step). Each step needs ` + "`type`" + `, ` + "`id`" + ` (kebab-case, unique), and ` + "`title`" + ` at minimum.

**Sensible starter shape**:

` + "```json" + `
{
  "objective": "Same or more specific than workflow.json objective",
  "success_criteria": "How the overall plan succeeds",
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
- **Don't overwrite existing workflows.** If the user wants to modify an existing workflow, tell them to use the workflow canvas — ` + "`create_workflow`" + ` will refuse if the target folder already exists.
- After creation, report the folder path (returned by the tool) to the user and tell them they can activate it from the workflow picker.

`
	return instructions
}

// buildEmployeesWorkflowsContext reads the employee registry and workflow-assignment map
// and returns a compact markdown section listing each employee with their assigned workflows.
// Injected into the multi-agent chat system prompt so the agent already knows who exists
// and which workflows each person owns — no need to cat the config files just to resolve a name.
// Returns an empty string when no employees are registered.
func buildEmployeesWorkflowsContext() string {
	employees, err := readEmployeesFile()
	if err != nil {
		log.Printf("[PROMPT CONTEXT] Failed to read employees.json: %v", err)
		return ""
	}
	if len(employees) == 0 {
		return ""
	}

	assignments, err := readEmployeeWorkflowsFile()
	if err != nil {
		log.Printf("[PROMPT CONTEXT] Failed to read employee-workflows.json: %v", err)
		// Still render employees even if assignments file is missing — names are useful on their own
		assignments = map[string]string{}
	}

	// Group workflows by employee ID
	byEmployee := map[string][]string{}
	for workflowPath, employeeID := range assignments {
		byEmployee[employeeID] = append(byEmployee[employeeID], workflowPath)
	}
	for _, paths := range byEmployee {
		sort.Strings(paths)
	}

	// Build a stable employee order (by name, then ID as tiebreaker)
	sortedEmployees := make([]EmployeeFile, len(employees))
	copy(sortedEmployees, employees)
	sort.Slice(sortedEmployees, func(i, j int) bool {
		ni := strings.ToLower(strings.TrimSpace(sortedEmployees[i].Name))
		nj := strings.ToLower(strings.TrimSpace(sortedEmployees[j].Name))
		if ni != nj {
			return ni < nj
		}
		return sortedEmployees[i].ID < sortedEmployees[j].ID
	})

	var sb strings.Builder
	sb.WriteString("\n## Current Employees & Workflow Assignments\n\n")
	sb.WriteString("This workspace has the following employees with their assigned workflows. **If the user's message names any employee below — whether addressing them directly (\"Hey Priya, …\"), asking about them (\"what has Arjun's workflows found?\"), or mentioning them in passing — treat that employee's assigned workflows as the primary source of truth.** Go straight to inspecting the relevant workflow folder (runs, reports, knowledgebase, learnings) to ground your answer; do not answer from general knowledge without checking the workflow state first. Match names case-insensitively and accept first-name-only references.\n\n")

	for _, emp := range sortedEmployees {
		name := strings.TrimSpace(emp.Name)
		if name == "" {
			name = emp.ID
		}
		role := strings.TrimSpace(emp.Role)
		if role == "" {
			role = strings.TrimSpace(emp.Description)
		}

		line := fmt.Sprintf("- **%s**", name)
		if emp.ID != "" {
			line += fmt.Sprintf(" (`%s`)", emp.ID)
		}
		if role != "" {
			line += fmt.Sprintf(" — %s", role)
		}
		sb.WriteString(line + "\n")

		workflows := byEmployee[emp.ID]
		if len(workflows) == 0 {
			sb.WriteString("  - _No workflows assigned_\n")
		} else {
			for _, wp := range workflows {
				sb.WriteString(fmt.Sprintf("  - `%s`\n", wp))
			}
		}
	}

	// Call out any assignments referencing an employee ID that no longer exists in the registry
	employeeIDs := map[string]bool{}
	for _, emp := range employees {
		employeeIDs[emp.ID] = true
	}
	var orphans []string
	for workflowPath, employeeID := range assignments {
		if !employeeIDs[employeeID] {
			orphans = append(orphans, workflowPath)
		}
	}
	if len(orphans) > 0 {
		sort.Strings(orphans)
		sb.WriteString("\n**Workflows with stale assignments** (pointing at unknown employee IDs):\n")
		for _, wp := range orphans {
			sb.WriteString(fmt.Sprintf("- `%s`\n", wp))
		}
	}

	sb.WriteString("\n")
	return sb.String()
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
model: openrouter/anthropic/claude-sonnet-4
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

// buildSkillPrompt builds the system prompt section for selected skills.
// isOrchestrator=true means the agent delegates all tool use — skill reading
// instructions are phrased as "include in your delegate() instructions" instead
// of "read the file directly", resolving the contradiction between the orchestrator's
// "do not use tools" policy and the requirement to read skills before acting.
// docsRoot is the shell-visible workspace root for absolute paths.
func buildSkillPrompt(selectedSkills []string, workspaceAPIURL, docsRoot string, isOrchestrator bool) string {
	if len(selectedSkills) == 0 {
		return ""
	}

	absSkills := docsRoot + "/skills"

	var promptParts []string

	// Mode-appropriate skill reading instructions
	if isOrchestrator {
		promptParts = append(promptParts, `
## Available Skills

The following skills are available for this conversation. When delegating tasks, include the relevant skill path in your delegate() instruction so the sub-agent can read and apply it.

### Available Skills:
`)
	} else {
		promptParts = append(promptParts, `
## Available Skills

The following skills are available for this conversation. Each skill extends your capabilities with specialized instructions and tools.

**Important:** Before taking any significant action (executing multi-step work or performing analysis), read the SKILL.md files for relevant skills so you understand what capabilities are available. For simple conversational messages, you do not need to read skills first.

### Available Skills:
`)
	}

	// Group gws-* skills into a single entry; list all others individually
	gwsAdded := false
	for _, folderName := range selectedSkills {
		if strings.HasPrefix(folderName, "gws-") {
			if !gwsAdded {
				gwsAdded = true
				promptParts = append(promptParts,
					fmt.Sprintf("- **Google Workspace (gws-\\*)**: Drive, Gmail, Calendar, Docs, Sheets, Slides, and shared utilities.\n  List available: `execute_shell_command(command: \"ls %s/gws-*/SKILL.md\")`", absSkills))
			}
			continue
		}
		skill, err := skills.GetSkill(workspaceAPIURL, folderName)
		absPath := fmt.Sprintf("%s/%s/SKILL.md", absSkills, folderName)
		if err != nil {
			log.Printf("[SKILLS] Warning: Failed to load skill metadata %s: %v", folderName, err)
			promptParts = append(promptParts, fmt.Sprintf("- **%s**: Read instructions from `%s`", folderName, absPath))
			continue
		}
		promptParts = append(promptParts, fmt.Sprintf("- **%s**: %s\n  - Path: `%s`",
			skill.Frontmatter.Name,
			skill.Frontmatter.Description,
			absPath))
	}

	if isOrchestrator {
		promptParts = append(promptParts, fmt.Sprintf(`
Include the skill path in your delegate() instruction, e.g.: "Read %s/<name>/SKILL.md first, then follow its instructions to..."
`, absSkills))
	} else {
		promptParts = append(promptParts, fmt.Sprintf(`
Read each relevant skill in full: execute_shell_command(command: "cat %s/<name>/SKILL.md")
Then read any supporting files (scripts, templates, examples) referenced in the SKILL.md.
`, absSkills))
	}

	return strings.Join(promptParts, "\n")
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
		parts = append(parts, "This file defines the workflow's configuration — selected MCP servers, tools, skills, LLM config, browser mode, schedules, and employee assignment.")
		parts = append(parts, "```json")
		parts = append(parts, manifestContent)
		parts = append(parts, "```")
		parts = append(parts, "")
	}

	// 0b. Workflow memory (memory/ folder) — user-saved knowledge for this workflow
	// Also check legacy instructions.md for backward compatibility
	memoryDir := path.Join(wsPath, "memory")
	memoryContent := readDirectoryMarkdownFiles(client, memoryDir)
	if memoryContent == "" {
		// Fallback: read legacy instructions.md
		memoryContent = readFileContent(client, path.Join(wsPath, "instructions.md"))
	}
	if memoryContent != "" {
		parts = append(parts, "**Workflow Memory:**")
		parts = append(parts, memoryContent)
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
- Workflow manifest: `+"`%s/workflow.json`"+` — workflow-level config (servers, tools, skills, LLM, schedules, assignment). Holds optional `+"`objective`"+`/`+"`success_criteria`"+` fallback values.
- Plan file: `+"`%s/planning/plan.json`"+` — step definitions + **primary** `+"`objective`"+`/`+"`success_criteria`"+` at root
- Step config: `+"`%s/planning/step_config.json`"+` — per-step LLM, tools, and execution mode (`+"`agent_configs.use_code_execution_mode`"+` + `+"`agent_configs.declared_execution_mode`"+`: `+"`learn_code`"+` | `+"`code_exec`"+` | direct)
- Variables: `+"`%s/variables/variables.json`"+` — sole source of variable values + groups (workflow.json does NOT carry variable definitions)
- Global workflow learnings: `+"`%s/learnings/_global/SKILL.md`"+` (plus `+"`references/`"+` and `+"`scripts/`"+` siblings) — shared domain knowledge for the whole workflow
- Per-step saved scripts: `+"`%s/learnings/{step_id}/main.py`"+` — persistent script for `+"`learn_code`"+` steps (source of truth, reused across runs)
- Knowledgebase: `+"`%s/knowledgebase/`"+` — persistent files across runs
- Runs: `+"`%s/runs/iteration-0/`"+` is the **active** run; older runs are backed up to `+"`iteration-1/`"+`, `+"`iteration-2/`"+`, ... (keep 10). Per-run layout: `+"`runs/iteration-{N}/{group}/execution/step-{N}/code/main.py`"+` for working main.py copies.
- Final reports: `+"`%s/reports/{group-name}/{timestamp}.md`"+` — per-group final output reports
- Evaluation reports: `+"`%s/evaluation/runs/{runFolder}/evaluation_report.json`"+`
- Builder sessions: `+"`%s/builder/session-{id}-conversation.json`"+` — workshop chat histories (kept 3)
- Decisions log: `+"`%s/builder/decisions.jsonl`"+` — append-only structured audit log; sidecar to `+"`improve.md`"+`. Auto-improvement framework.
- Metrics: `+"`%s/metrics.json`"+` (workflow root, optional) — quantified goal definitions; required for Type 3 workflows.
- Context store: `+"`%s/context/rules.md`"+`, `+"`%s/context/clarifications.jsonl`"+`, `+"`%s/context/examples/`"+` — Type 3 only. Accumulated user-supplied business rules.
- Experiments: `+"`%s/experiments/active.json`"+`, `+"`%s/experiments/history.jsonl`"+`, `+"`%s/experiments/config.json`"+` — experiment loop state. See auto-improvement framework.
`, wsPath, wsPath, wsPath, wsPath, wsPath, wsPath, wsPath, wsPath, wsPath, wsPath, wsPath, wsPath, wsPath, wsPath, wsPath, wsPath, wsPath, wsPath, wsPath))

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

// getGWSQuickStartInstructions returns inline instructions for using Google Workspace via the gws CLI.
func getGWSQuickStartInstructions() string {
	return browserinstructions.GetGWSQuickStartInstructions()
}
