// Package guidance owns the canonical guided-flow text for every workflow
// slash command in the workshop UI. The text lives as embedded markdown
// templates so it can be rendered with focus/iteration/run_folder params and
// returned to the agent — the agent then follows the rendered prose verbatim.
// Focus is the conversation-derived instruction or context that caused the
// command, not just a narrow keyword. Slash-command wrappers should pass the
// user's surrounding/preceding request into focus when available.
//
// The same prose is reachable from three contexts:
//
//  1. A user typed a slash command. The slash command's frontend onSubmit
//     collapses to one line: "Call get_workflow_command_guidance(kind=<name>,
//     ...) and follow the returned instructions."
//  2. A user described the same intent in chat ("help me improve this
//     workflow"). The agent recognizes the intent and calls the tool.
//  3. A scheduled fire (e.g. /auto-improve's recurring optimizer
//     message) calls the tool to get the same canonical flow.
//
// One source of truth, three callers.
package guidance

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"text/template"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

//go:embed templates/builder/*.md templates/review/*.md templates/improve/*.md templates/report/*.md templates/kb/*.md templates/learning/*.md templates/db/*.md templates/system/*.md
var templatesFS embed.FS

// kindMeta captures everything we know about a guided flow at registration
// time: which file holds its template, which workshop modes are allowed to
// invoke it, and a one-line description used in the tool-list enum.
type kindMeta struct {
	Group       string // builder | review | improve | report | kb | learning | db
	Description string // shown to the agent in the kind enum
	Modes       []string
}

// allKinds is the canonical registry of guided flows. Adding a new kind:
//
//  1. Drop a markdown file in templates/<group>/<kind>.md.
//  2. Add an entry here with description + allowed modes.
//  3. Update the slash command's onSubmit to call this tool with the new kind.
//
// Modes match interactive_workshop_manager.go's switch. "workshop" is the
// canonical merged mode (was builder + optimizer in older releases);
// "builder" and "optimizer" are kept as legacy aliases so persisted sessions
// that pre-date the merge still resolve correctly. "run" is constrained
// runtime; "reporting" is the report-only surface. The tool refuses kinds
// not allowed in the caller's mode and tells the agent to suggest a mode
// switch.
var allKinds = map[string]kindMeta{
	// Design audits (only meaningful before run evidence accumulates)
	"design-flow":       {Group: "builder", Description: "Inspect context dependency / handoff design between steps", Modes: []string{"workshop"}},
	"ready-to-optimize": {Group: "builder", Description: "Pre-optimizer readiness checklist (objective, success criteria, runs, validation, etc.)", Modes: []string{"workshop"}},

	// Reviews — recommend, don't apply; appends to builder/review.md
	"review-plan":           {Group: "review", Description: "Comprehensive workflow audit: plan, step descriptions, learnings, KB, db/*.json, reports, variables, and eval wiring", Modes: []string{"workshop", "run"}},
	"review-speed":          {Group: "review", Description: "Latency analysis with safe-speedup recommendations", Modes: []string{"workshop"}},
	"review-cost":           {Group: "review", Description: "Cost analysis with safe-reduction recommendations", Modes: []string{"workshop"}},
	"review-code":           {Group: "review", Description: "Saved main.py vs current step descriptions drift check", Modes: []string{"workshop"}},
	"review-artifact-drift": {Group: "review", Description: "Audit plan changelog entries against dependent artifacts: learnings, main.py, KB, db, reports, and eval wiring", Modes: []string{"workshop"}},

	// Knowledgebase maintenance — applies targeted or cross-step KB cleanup
	"improve-knowledge": {Group: "kb", Description: "Improve knowledgebase/notes with targeted cleanup or cross-step consolidation", Modes: []string{"workshop"}},

	// Learning maintenance — applies targeted/cross-step cleanup to learnings/_global
	"improve-learnings": {Group: "learning", Description: "Improve learnings/_global with targeted cleanup or current-plan consolidation", Modes: []string{"workshop"}},

	// DB maintenance — applies guarded schema/contract cleanup to db/*.json
	"improve-data": {Group: "db", Description: "Improve db/*.json contracts, schemas, and report compatibility", Modes: []string{"workshop"}},

	// Improvements — metric-driven harden/replan flows
	"define-success":     {Group: "improve", Description: "One-time bootstrap of optimization success criteria (Workflow Profile + metrics)", Modes: []string{"workshop"}},
	"improve-workflow":   {Group: "improve", Description: "Unified metric-driven workflow improvement: harden or replan from run/eval evidence", Modes: []string{"workshop"}},
	"improve-evaluation": {Group: "improve", Description: "Evaluation plan changes and metric-source health checks", Modes: []string{"workshop"}},
	"auto-improve":       {Group: "improve", Description: "Set up recurring run + optimizer schedules", Modes: []string{"workshop"}},
	"improve-report":     {Group: "report", Description: "Report layout / color / density improvements", Modes: []string{"workshop"}},
}

// referenceKinds is the registry of system reference docs — content that
// used to live inline in the workshop system prompt and is now loaded on
// demand by the agent via get_reference_doc(kind). These are not procedural
// flows (those live in allKinds); they are gated reference material the
// agent reads before performing certain actions (e.g. read "code-authoring"
// before patching main.py).
//
// Adding a new reference doc:
//
//  1. Drop a markdown file in templates/system/<kind>.md.
//  2. Add an entry here with description + allowed modes.
//  3. Optionally wire a precondition gate on the tool that should require it.
//
// Modes use the same workshop mode strings as allKinds. "workshop" is the
// merged builder+optimizer mode; "run" is constrained runtime; "reporting"
// is the report-only surface.
// Reference docs are content that used to be inlined in the workshop system
// prompt and is now loaded on demand. We intentionally do NOT migrate tool
// catalogs (TOOLS REFERENCE, Special Workspace Tools / media-tools, Browser
// Automation) because the LLM only sees tools through the MCP bridge — the
// prose catalog IS the agent's primary tool-discovery surface, and lazy-loading
// would create a bootstrap problem.
var referenceKinds = map[string]kindMeta{
	// Workflow-scoped reference docs (workshop / run modes).
	"code-authoring":    {Group: "system", Description: "Detailed main.py authoring rules and patterns (env access, sys.argv contract, data authenticity, patching discipline)", Modes: []string{"workshop"}},
	"stores":            {Group: "system", Description: "Persistent store design contract: skill vs knowledgebase vs db, when to write to which", Modes: []string{"workshop", "run"}},
	"message-sequence":  {Group: "system", Description: "Message-sequence route patterns (stateful specialist, test/fix loop, maker+reviewer, panel, clean-room retry, HITL re-entry, scripted conversation)", Modes: []string{"workshop"}},
	"routing":           {Group: "system", Description: "Routing step design: when to use routing vs todo_task/message_sequence, pure vs execute-then-route modes, route structure (route_id/condition/sub_agent_step/default_route_id), human_input→routing pairing, anti-patterns", Modes: []string{"workshop"}},
	"todo-task":         {Group: "system", Description: "todo_task (orchestrator / sub-workflow / pipeline) step design: when to use vs routing / message_sequence / regular, anatomy (todo_task_step + predefined_routes), inline sub_agent_step vs orphan_step_ref, nested-todo_task 1-level limit, variables and group_name handling, anti-patterns, scripted-mode fast path. Load before adding or restructuring a todo_task step.", Modes: []string{"workshop"}},
	"human-input":       {Group: "system", Description: "human_input step design: text vs yesno vs multiple_choice input types, pairing with a routing step for user-driven branching, schedule (unattended) considerations and the human_inputs run_full_workflow arg, downstream validation, anti-patterns. Load before adding or editing a human_input step.", Modes: []string{"workshop"}},
	"workflow-patterns": {Group: "system", Description: "Recurring workflow composition patterns extracted from real plans: Phase Router, Scoped Investigation, Linear Pipeline, Fan-out & Consolidate, Verification Gate, Pre-flight Probe, Human Checkpoint, Critique Loop, Persistence Tail. Load when starting a new plan or restructuring an existing one.", Modes: []string{"workshop"}},
	"optimize-playbook": {Group: "system", Description: "Optimizer deep-dive: harden vs replan decision tree, eval, metrics, auto-improvement framework", Modes: []string{"workshop"}},
	"file-layout":       {Group: "system", Description: "Workspace file layout reference and path discipline", Modes: []string{"workshop", "run"}},
	"plan-design":       {Group: "system", Description: "Plan-design playbook: step boundaries, step-type selection, context flow, validation/failure design, anti-patterns, step-types reference. Load when designing a new plan or restructuring an existing one in DESIGN phase.", Modes: []string{"workshop"}},
	"report-plan":       {Group: "system", Description: "Report plan toolchain: get/upsert/move/toggle/remove widgets, JSONata multi-source binding, section layouts and tabs, per-report themes, validate/preview, populating missing db sources. Load before authoring or editing reports/report_plan.json.", Modes: []string{"workshop"}},
	"evaluation-plan":   {Group: "system", Description: "Evaluation plan rules: required fields, route gating, ID collision discipline, TARGET_RUN_PATH placeholder, step config (declared_execution_mode + execution_tier), validate/run workflow. Load before editing evaluation/evaluation_plan.json.", Modes: []string{"workshop"}},

	// Multi-agent chat reference docs (rare-path topics — schedule/secret
	// management — that don't warrant always-loaded prompt space).
	"schedule-management": {Group: "system", Description: "Schedule cron, edit, update, or remove multi-agent scheduled tasks via _users/<id>/multiagent-schedules.json", Modes: []string{"multi-agent"}},
	"secret-management":   {Group: "system", Description: "Manage workflow / user / global secrets via list_secrets, set_workflow_secret, set_user_secret, delete_workflow_secret, delete_user_secret — buckets, naming rules, attach-after-store discipline", Modes: []string{"multi-agent", "workshop"}},

	// Cross-mode operational reference docs (browser, memory, code-execution
	// bridge). Currently duplicated in the always-on system-prompt sections;
	// adding them as skills lets the agent load deep details on-demand and
	// sets up the eventual prompt-trim.
	"html-output":    {Group: "system", Description: "High-quality self-contained HTML report guide: when to use HTML vs JSON vs Markdown, layout baseline with dark-mode styles, summary box, sticky nav, inline bar chart (no CDN), badge classes for pass/fail/warn, quality checklist. Load before writing any .html output file.", Modes: []string{"multi-agent", "workshop", "run"}},
	"browser-usage":  {Group: "system", Description: "Browser automation deep guide: agent_browser HTTP API, CDP vs headless vs Playwright modes, snapshot/click/fill workflow, tab management, file uploads, session limits, common mistakes. Load when driving a browser, scraping pages, automating logins, or uploading files via a web form.", Modes: []string{"multi-agent", "workshop", "run"}},
	"memory-usage":   {Group: "system", Description: "Persistent cross-session memory: save_memory, recall_memory, enrich_memory tools; the user-model philosophy (what to save vs not save); storage layout and recall guidelines. Load when the user asks to remember something, references past work, or you need to consolidate chat history.", Modes: []string{"multi-agent", "workshop"}},
	"mcp-bridge":     {Group: "system", Description: "MCP HTTP bridge mechanics: $MCP_API_URL / $MCP_API_TOKEN env vars, curl pattern for calling MCP tools, response envelope, $VAR_* / $SECRET_* variable rules, single-call discipline. Load before writing scripts that call MCP tools via the bridge, or when debugging bridge errors.", Modes: []string{"multi-agent", "workshop", "run"}},
	"workflow-tools":        {Group: "system", Description: "Full reference for workshop / workflow tools: step execution & inspection (execute_step, query_step, debug_step, run_full_workflow), step config & analysis (update_step_config, harden_workflow, replan_workflow_from_results, review_workflow_*), plan modification (add/update/delete step tools, todo_task routes, versioning), variables & MCP server config, schedule management (cron, modes, message authoring, optimizer best practices), shell, skills install/manage, and secrets two-step flow. Load when you need a tool's exact signature, parameters, or when-to-use rules and the inline cheat sheet doesn't suffice.", Modes: []string{"workshop"}},
	"workspace-media-tools": {Group: "system", Description: "Workspace-level provider-backed tools: text generation (generate_text_llm, search_web_llm), image generation + editing (image_gen, image_edit), video generation (generate_video — Vertex AI vs Gemini API model routing), audio + music (text_to_speech, speech_to_text, generate_music), media reading (read_image, read_video, read_pdf), capability discovery (list_llm_capabilities, estimate_llm_cost, set_provider_auth). Full path / provider / model_id contracts, default providers per capability, provider-setup discipline. Load before generating media, reading non-text files, or wiring provider auth.", Modes: []string{"multi-agent", "workshop", "run"}},
	"workshop-mode-flow":    {Group: "system", Description: "Workshop mode operating playbook: foundation checks (soul/soul.md objective + success criteria), the core run → eval → classify → act → verify loop, when to call harden_workflow vs replan_workflow_from_results, optimization workflow steps, progressive hardening loop across groups, and the when-to-redirect-to-other-mode decision tree. Load when in workshop mode and choosing between harden / replan / eval-improvement / metric-cleanup / no-action, or when running a multi-group hardening loop.", Modes: []string{"workshop"}},
	"debugging-flow":        {Group: "system", Description: "Debugging failed/stuck workflow steps: workshop investigation workflow (harden_workflow vs replan_workflow_from_results decision, manual fix tools), run-mode investigation workflow (query_step, debug_step, list_executions, read-only review), root cause → fix mapping table (description gaps, validation issues, context wiring), workshop vs run mode fix options. Load when a step fails or behaves unexpectedly, when the user asks 'why is it stuck' / 'what happened', or before deciding to retry vs fix the underlying step.", Modes: []string{"multi-agent", "workshop", "run"}},
	"backup-strategy":       {Group: "system", Description: "Per-workflow backup playbook: when to commit to the workflow's git repo vs push to a large-file backend, what never to back up (secrets, transient state), git commit/pull/push discipline (atomic commits, --force-with-lease, JSON merge handling, hook bypass policy), and a comparison of large-file backends — HuggingFace Hub (datasets / models), Cloudflare R2 (zero-egress S3-compatible), Backblaze B2 (cheap cold storage), AWS S3, Google Cloud Storage, Azure Blob, and rclone (one CLI for all). Includes CLI commands, auth env-var convention, and a decision matrix by content type. Load when the user asks about backup / versioning / where to store artifacts / how to push a workflow folder, or when setting up a new workflow's storage destination.", Modes: []string{"multi-agent", "workshop", "run"}},
}

// tmplData is the typed context passed to every guidance template. Focus is
// the conversation-derived instruction/context for this command. New fields
// require updating the markdown templates that consume them.
type tmplData struct {
	Focus        string
	Iteration    string
	RunFolder    string
	WorkshopMode string
}

const pathDisciplineGuidance = `PATH DISCIPLINE
Use absolute workspace paths for shell commands when the prompt or env exposes them (` + "`AbsWorkspacePath`" + `, ` + "`VAR_WORKSPACE_PATH`" + `, ` + "`STEP_OUTPUT_DIR`" + `, ` + "`STEP_EXECUTION_DIR`" + `). For file tools that expect workspace paths, use workflow-root-qualified paths. If the current workflow path is ` + "`Workflow/<name>`" + `, read ` + "`Workflow/<name>/runs/...`" + `, ` + "`Workflow/<name>/evaluation/...`" + `, ` + "`Workflow/<name>/planning/...`" + `, and ` + "`Workflow/<name>/db/...`" + ` rather than bare ` + "`runs/...`" + `, ` + "`evaluation/...`" + `, ` + "`planning/...`" + `, or ` + "`db/...`" + `. Bare examples in this guidance are shorthand for paths under the current workflow root. Do not use host paths outside workspace-docs.

`

// renderKind loads templates/<group>/<kind>.md, renders it with the supplied
// params, and returns the rendered text. Returns an error if the kind isn't
// known or its template is malformed.
func renderKind(kind string, data tmplData) (string, error) {
	return renderFromRegistry(kind, data, allKinds)
}

// renderReferenceKind is the same as renderKind but resolves against the
// reference-doc registry (templates/system/*.md). Both registries share
// rendering internals so behavior stays consistent (path discipline header,
// template params, trailing newline handling).
func renderReferenceKind(kind string, data tmplData) (string, error) {
	return renderFromRegistry(kind, data, referenceKinds)
}

// renderFromRegistry is the shared rendering core. It looks up `kind` in the
// supplied registry, reads templates/<group>/<kind>.md from the embedded FS,
// executes it as a Go template with the supplied data, and prepends the
// shared path-discipline preamble.
func renderFromRegistry(kind string, data tmplData, registry map[string]kindMeta) (string, error) {
	meta, ok := registry[kind]
	if !ok {
		return "", fmt.Errorf("unknown kind %q", kind)
	}
	rel := path.Join("templates", meta.Group, kind+".md")
	body, err := templatesFS.ReadFile(rel)
	if err != nil {
		return "", fmt.Errorf("read template %s: %w", rel, err)
	}
	tmpl, err := template.New(kind).Parse(string(body))
	if err != nil {
		return "", fmt.Errorf("parse template %s: %w", rel, err)
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template %s: %w", rel, err)
	}
	return pathDisciplineGuidance + strings.TrimRight(buf.String(), "\n") + "\n", nil
}

// modeAllowed reports whether a kind can be invoked from a given workshop
// mode. The caller passes their current mode (builder / optimizer / workshop
// / run / reporting); the kind's allow-list is checked.
func modeAllowed(kind, mode string) bool {
	return modeAllowedIn(kind, mode, allKinds)
}

// modeAllowedIn is the registry-parameterized form of modeAllowed. Used by
// both the procedural-guidance tool (allKinds) and the reference-doc tool
// (referenceKinds).
func modeAllowedIn(kind, mode string, registry map[string]kindMeta) bool {
	meta, ok := registry[kind]
	if !ok {
		return false
	}
	for _, m := range meta.Modes {
		if m == mode {
			return true
		}
	}
	return false
}

// kindEnum returns sorted kind names — used to populate the tool schema's
// enum and for diagnostic error messages.
func kindEnum() []string {
	return kindEnumFrom(allKinds)
}

// kindEnumFrom returns sorted kind names for any registry.
func kindEnumFrom(registry map[string]kindMeta) []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// kindEnumWithDescriptions formats the kind list for the tool description so
// the agent can see, in one place, every guided flow available to it.
func kindEnumWithDescriptions() string {
	return kindEnumWithDescriptionsFrom(allKinds)
}

// kindEnumWithDescriptionsFrom formats the kind list for any registry.
func kindEnumWithDescriptionsFrom(registry map[string]kindMeta) string {
	type row struct {
		k     string
		d     string
		modes []string
	}
	rows := make([]row, 0, len(registry))
	for k, v := range registry {
		rows = append(rows, row{k: k, d: v.Description, modes: v.Modes})
	}
	sort.Slice(rows, func(i, j int) bool {
		gi, gj := registry[rows[i].k].Group, registry[rows[j].k].Group
		if gi != gj {
			return gi < gj
		}
		return rows[i].k < rows[j].k
	})
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "  %s — %s [modes: %s]\n", r.k, r.d, strings.Join(r.modes, ", "))
	}
	return b.String()
}

// RegisterGuidanceTool exposes get_workflow_command_guidance to the agent.
// The tool returns the rendered prompt for any kind in allKinds. Mode is
// validated against the kind's allow-list — calling a kind from the wrong
// mode returns an error message instructing the agent to suggest a mode
// switch.
func RegisterGuidanceTool(agent *mcpagent.Agent, currentMode string, logger loggerv2.Logger) {
	desc := "Get the canonical guided-flow text for any workflow command. " +
		"Call this tool — and follow the returned instructions verbatim — when (1) the user invokes a slash command " +
		"like /improve-workflow or /review-plan (the slash command will name the kind to pass; pass the surrounding " +
		"conversation/request into focus when available), (2) the user describes " +
		"the same intent in plain chat (\"help me improve this workflow\", \"review whether the goal is being met\", " +
		"\"improve the eval plan\") — recognize the intent and pick the matching kind, or (3) you're running on a " +
		"schedule and the message names a kind. The returned text is your instructions for this turn — do not paraphrase " +
		"or skip steps. Available kinds:\n" + kindEnumWithDescriptions() +
		"Mode validation: each kind is gated to specific workshop modes. If the user's request matches a kind not allowed " +
		"in the current mode, tell the user the mode they need to switch to instead of calling the tool."

	params := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"kind": map[string]interface{}{
				"type":        "string",
				"enum":        kindEnum(),
				"description": "The guided-flow to render. See the tool description for the full list of kinds and their per-mode availability.",
			},
			"focus": map[string]interface{}{
				"type":        "string",
				"description": "Optional but strongly recommended. The conversation-derived instruction/context for this command: include the user's recent request, constraints, examples, or focus area that led to the slash command. This is how a slash command carries 'based on the conversation that just happened' into the canonical guidance.",
			},
			"iteration": map[string]interface{}{
				"type":        "string",
				"description": "Optional. Run iteration to use as evidence (e.g. \"iteration-3\"). When set, templates that take an iteration use it as the starting evidence set.",
			},
			"run_folder": map[string]interface{}{
				"type":        "string",
				"description": "Optional. Full run folder path (e.g. \"iteration-3/group-a\"). Used by review-speed / review-cost / improve-evaluation-style flows that anchor on a specific run.",
			},
		},
		"required": []string{"kind"},
	}

	handler := func(ctx context.Context, args map[string]interface{}) (string, error) {
		kind, _ := args["kind"].(string)
		focus, _ := args["focus"].(string)
		iteration, _ := args["iteration"].(string)
		runFolder, _ := args["run_folder"].(string)

		if _, ok := allKinds[kind]; !ok {
			return fmt.Sprintf("error: unknown kind %q. Valid kinds: %s", kind, strings.Join(kindEnum(), ", ")), nil
		}
		if currentMode != "" && !modeAllowed(kind, currentMode) {
			meta := allKinds[kind]
			return fmt.Sprintf(
				"error: kind %q is not available in mode %q. It runs in: %s. Tell the user they need to switch workshop mode before this command can run.",
				kind, currentMode, strings.Join(meta.Modes, ", "),
			), nil
		}

		text, err := renderKind(kind, tmplData{
			Focus:        strings.TrimSpace(focus),
			Iteration:    strings.TrimSpace(iteration),
			RunFolder:    strings.TrimSpace(runFolder),
			WorkshopMode: strings.TrimSpace(currentMode),
		})
		if err != nil {
			return fmt.Sprintf("error rendering guidance for %q: %v", kind, err), nil
		}
		// Wrap the rendered guidance in a JSON envelope so the agent sees a
		// stable shape; the actual prose is the `guidance` field.
		envelope, _ := json.MarshalIndent(map[string]interface{}{
			"kind":     kind,
			"guidance": text,
		}, "", "  ")
		return string(envelope), nil
	}

	if err := agent.RegisterCustomTool("get_workflow_command_guidance", desc, params, handler, "auto_improvement"); err != nil {
		if logger != nil {
			logger.Warn(fmt.Sprintf("Failed to register get_workflow_command_guidance: %v", err))
		}
	}
}

// ReferenceKindNames returns the sorted list of reference-doc kinds known
// to this package. Exported for cross-package tests that need to enumerate
// every doc without depending on the private registry.
func ReferenceKindNames() []string {
	return kindEnumFrom(referenceKinds)
}

// BuildSystemToolsSkill returns a single small "meta" skill whose body
// teaches the agent the system-tool surface available in this session:
// the MCP bridge, get_api_spec for tool discovery, get_reference_doc
// for deeper system docs, and get_workflow_command_guidance for
// procedural flows. The skill enumerates the reference-doc kinds that
// are allowed in the given mode so the agent knows which kinds it can
// actually ask for via get_reference_doc.
//
// Why a meta-skill rather than one skill per reference doc: copying
// every reference-doc body into a skill folder per session duplicates
// content and risks drift. Instead this small skill points at the
// existing tools so the agent loads detail on demand, with progressive
// disclosure handled by the provider when it surfaces the meta-skill
// itself.
//
// An empty mode returns nil (no skill to attach).
func BuildSystemToolsSkill(mode string) *llmtypes.Skill {
	if strings.TrimSpace(mode) == "" {
		return nil
	}

	var kindLines strings.Builder
	for _, kind := range kindEnumFrom(referenceKinds) {
		if !modeAllowedIn(kind, mode, referenceKinds) {
			continue
		}
		meta := referenceKinds[kind]
		fmt.Fprintf(&kindLines, "- `%s` — %s\n", kind, meta.Description)
	}
	kindList := kindLines.String()
	if kindList == "" {
		kindList = "(no reference docs are available in this mode)\n"
	}

	body := `This skill is a quick guide to the system tools available in this session. Use it as your map for discovery and deep documentation.

## Tool / API discovery

- ` + "`get_api_spec(server_name, tool_name)`" + ` — when you do not know an MCP tool's parameters or response shape, call this first.
- ` + "`get_reference_doc(kind, focus?)`" + ` — system reference docs. Load the matching doc before any deep action (e.g. read ` + "`optimize-playbook`" + ` before ` + "`harden_workflow`" + ` or ` + "`replan_workflow_from_results`" + `; read ` + "`code-authoring`" + ` before authoring ` + "`main.py`" + `). Some tools refuse to run until their precondition doc has been loaded — the error will name the kind.
- ` + "`get_workflow_command_guidance(kind, focus?)`" + ` — canonical procedural flows (improve-workflow, review-plan, auto-improve, define-success, etc.). The returned text is your instructions for that turn; follow it verbatim.

### Reference doc kinds available in this mode

` + kindList + `
## MCP bridge — only in code-execution mode

When you are running scripts via ` + "`execute_shell_command`" + ` (code-execution mode), call MCP tools through HTTP:

` + "```bash" + `
curl -sS -X POST "$MCP_API_URL/tools/mcp/{server_name}/{tool_name}" \
  -H "Authorization: Bearer $MCP_API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"arg":"value"}' | jq
` + "```" + `

Pre-set environment for scripts:
- ` + "`$MCP_API_URL`" + ` + ` + "`$MCP_API_TOKEN`" + ` — bridge endpoint + bearer
- ` + "`$STEP_OUTPUT_DIR`" + `, ` + "`$STEP_EXECUTION_DIR`" + ` — write outputs here
- ` + "`$VAR_<NAME>`" + ` — workflow config (e.g. ` + "`$VAR_USER_ID`" + `); reference, never hardcode
- ` + "`$SECRET_<NAME>`" + ` — credentials; never echo to stdout, never write to files

In non-code-execution mode you call tools directly via the LLM tool-call API; the bridge curl pattern is not needed.

## When in doubt

Call the right discovery tool above before guessing. Hallucinated tool names or parameter shapes will fail at the bridge; reading the spec or the reference doc is cheap.
`

	return &llmtypes.Skill{
		Name:        "system-tools",
		Description: "How to use the MCP bridge, tool discovery (get_api_spec), reference docs (get_reference_doc), and workflow command guidance in this session.",
		Content:     body,
		Source:      llmtypes.SkillSource{Origin: "builtin"},
	}
}

// RenderSystemDoc renders the named reference doc with no caller context,
// stripping the path-discipline preamble. Intended for production code that
// needs system-doc content inline (e.g. sub-agent prompts that can't call
// get_reference_doc themselves because they're not in a chat). The returned
// string is identical to what get_reference_doc(kind=...) would produce in
// the `reference` field, minus the path-discipline header (which only makes
// sense as a chat-turn preamble).
//
// Panics on error because the embedded FS is compile-time — if a kind is
// declared in referenceKinds but its .md file is missing or malformed, that
// is a build-time bug, not a runtime condition.
func RenderSystemDoc(kind string) string {
	meta, ok := referenceKinds[kind]
	if !ok {
		panic(fmt.Sprintf("guidance: RenderSystemDoc called with unknown kind %q", kind))
	}
	rel := path.Join("templates", meta.Group, kind+".md")
	body, err := templatesFS.ReadFile(rel)
	if err != nil {
		panic(fmt.Sprintf("guidance: read %s: %v", rel, err))
	}
	tmpl, err := template.New(kind).Parse(string(body))
	if err != nil {
		panic(fmt.Sprintf("guidance: parse %s: %v", rel, err))
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, tmplData{}); err != nil {
		panic(fmt.Sprintf("guidance: execute %s: %v", rel, err))
	}
	return strings.TrimRight(buf.String(), "\n") + "\n"
}

// RenderReferenceKindForTest renders the named reference doc with empty
// caller context. Exported so step_based_workflow's prompt size/coverage
// tests can verify every kind is renderable and reasonably sized without
// depending on internals.
func RenderReferenceKindForTest(kind, mode string) (string, error) {
	return renderReferenceKind(kind, tmplData{WorkshopMode: mode})
}

// ListReferenceKindsForTest is an alias for ReferenceKindNames kept for
// test ergonomics ("ForTest" suffix signals "use from tests only").
func ListReferenceKindsForTest() []string {
	return ReferenceKindNames()
}

// RegisterReferenceDocTool exposes get_reference_doc to the agent. The tool
// returns the rendered text for any kind in referenceKinds — reference
// documentation that used to live inline in the workshop system prompt and
// is now loaded on demand. Same internals as RegisterGuidanceTool but
// scoped to system-doc semantics:
//
//   - allKinds → procedural guided flows ("here is the procedure to follow")
//   - referenceKinds → static reference material ("here are the rules / patterns")
//
// Mode is validated against the kind's allow-list; calling a kind from the
// wrong mode returns a teaching error explaining which modes the doc lives in.
func RegisterReferenceDocTool(agent *mcpagent.Agent, currentMode string, logger loggerv2.Logger) {
	desc := "Get the full reference documentation for a workshop concept. " +
		"Use this when you need detailed rules, patterns, or contracts that aren't fully covered by the system prompt's " +
		"inline cheat sheets — for example before authoring a step's main.py, designing a message_sequence route, " +
		"writing to db/kb/skill stores, or running harden_workflow / replan_workflow_from_results. " +
		"The returned text is reference material; read it, then proceed with the action that required it. " +
		"Available reference docs:\n" + kindEnumWithDescriptionsFrom(referenceKinds) +
		"Mode validation: each doc is gated to specific workshop modes. If a doc is not available in the current mode, " +
		"the tool returns an error naming the modes where it is available."

	params := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"kind": map[string]interface{}{
				"type":        "string",
				"enum":        kindEnumFrom(referenceKinds),
				"description": "The reference doc to load. See the tool description for the full list and per-mode availability.",
			},
			"focus": map[string]interface{}{
				"type":        "string",
				"description": "Optional. A short note about why you are loading this doc — gets included in the rendered output so the agent has caller context for any conditional sections.",
			},
		},
		"required": []string{"kind"},
	}

	handler := func(ctx context.Context, args map[string]interface{}) (string, error) {
		kind, _ := args["kind"].(string)
		focus, _ := args["focus"].(string)

		if _, ok := referenceKinds[kind]; !ok {
			return fmt.Sprintf("error: unknown reference doc %q. Valid kinds: %s", kind, strings.Join(kindEnumFrom(referenceKinds), ", ")), nil
		}
		if currentMode != "" && !modeAllowedIn(kind, currentMode, referenceKinds) {
			meta := referenceKinds[kind]
			return fmt.Sprintf(
				"error: reference doc %q is not available in mode %q. It is available in: %s.",
				kind, currentMode, strings.Join(meta.Modes, ", "),
			), nil
		}

		text, err := renderReferenceKind(kind, tmplData{
			Focus:        strings.TrimSpace(focus),
			WorkshopMode: strings.TrimSpace(currentMode),
		})
		if err != nil {
			return fmt.Sprintf("error rendering reference doc %q: %v", kind, err), nil
		}

		// Record the load so gated tool calls (see WithDocPrecondition) can
		// verify the agent has read the prerequisite docs. Session ID comes
		// from ctx via common.ChatSessionIDKey — if it's missing, MarkLoaded
		// is a no-op (tracker can't key the load to any session).
		DefaultTracker().MarkLoaded(SessionIDFromContext(ctx), kind)

		// Same envelope shape as get_workflow_command_guidance so the agent
		// sees a consistent return contract across both tools.
		envelope, _ := json.MarshalIndent(map[string]interface{}{
			"kind":      kind,
			"reference": text,
		}, "", "  ")
		return string(envelope), nil
	}

	if err := agent.RegisterCustomTool("get_reference_doc", desc, params, handler, "auto_improvement"); err != nil {
		if logger != nil {
			logger.Warn(fmt.Sprintf("Failed to register get_reference_doc: %v", err))
		}
	}
}
