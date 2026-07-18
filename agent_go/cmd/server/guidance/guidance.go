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
//  3. A scheduled Pulse module (e.g. Goal Advisor selected by Pulse Gate) calls
//     the tool to get the same canonical flow.
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
// "builder" and "optimizer" resolve to the merged mode. "run" is constrained
// runtime; "reporting" is the report-only surface. The tool refuses kinds
// not allowed in the caller's mode and tells the agent to suggest a mode
// switch.
var allKinds = map[string]kindMeta{
	// Comprehensive plan review: critical artifact audit plus better-shape design guidance.
	"design-plan": {Group: "builder", Description: "Comprehensive workflow plan and dependent-artifact review with better design recommendations", Modes: []string{"workshop", "run"}},

	// Reviews — recommend, don't apply; appends to builder/improve.html
	"review-speed":          {Group: "review", Description: "Latency analysis with safe-speedup recommendations", Modes: []string{"workshop"}},
	"review-cost":           {Group: "review", Description: "Cost analysis with safe-reduction recommendations", Modes: []string{"workshop"}},
	"review-code":           {Group: "review", Description: "Saved main.py vs current step descriptions drift check", Modes: []string{"workshop"}},
	"review-artifact-drift": {Group: "review", Description: "Audit plan changelog entries against dependent artifacts: learnings, main.py, KB, db, reports, and eval wiring", Modes: []string{"workshop"}},
	"bug-review":            {Group: "review", Description: "One-off read-only Pulse QA review for runtime, logic, evidence-chain, and suspicious-success defects", Modes: []string{"workshop"}},
	"llm-ops-review":        {Group: "review", Description: "One-off read-only review of model tiers, cost, latency, fallbacks, backup, publish, notify, and version readiness", Modes: []string{"workshop"}},

	// Knowledgebase maintenance — applies targeted or cross-step KB cleanup
	"improve-knowledge": {Group: "kb", Description: "Read-only knowledgebase/notes health review with targeted or cross-step fixer recommendations", Modes: []string{"workshop"}},

	// Learning maintenance — read-only targeted/cross-step review of learnings/_global
	"improve-learnings": {Group: "learning", Description: "Read-only learnings/_global health review with targeted or current-plan fixer recommendations", Modes: []string{"workshop"}},

	// DB maintenance — read-only guarded schema/contract review of db/db.sqlite
	"improve-database": {Group: "db", Description: "Read-only db/db.sqlite contract, schema, integrity, and report-compatibility review", Modes: []string{"workshop"}},

	// Improvements — evidence-driven reliability and strategy flows
	"define-success":      {Group: "improve", Description: "Confirm the workflow Goal, success criteria, and operating model", Modes: []string{"workshop"}},
	"improve-evaluation":  {Group: "improve", Description: "Read-only evaluation coverage and correctness review with fixer recommendations", Modes: []string{"workshop"}},
	"pulse":               {Group: "improve", Description: "Run one complete manual Pulse against retained evidence without changing schedules or running the workflow", Modes: []string{"workshop"}},
	"pulse-setup":         {Group: "improve", Description: "Enable Pulse and set up the normal recurring workflow run schedule", Modes: []string{"workshop"}},
	"pulse-fixer":         {Group: "improve", Description: "Apply and verify bounded safe fixes from existing Pulse or standalone review findings", Modes: []string{"workshop"}},
	"goal-advisor":        {Group: "improve", Description: "Expert strategy advisor module: recover drifting goals, run periodic healthy 10x/headroom reviews, and advance one approval-gated experiment in builder/improve.html from proposal through measured outcome", Modes: []string{"workshop"}},
	"design-reporting-ui": {Group: "report", Description: "Design the reporting UI from scratch: author HTML document(s) (live data via window.report, single or tabbed per-entity) and register them in reports/report_plan.json", Modes: []string{"workshop"}},
	"improve-report":      {Group: "report", Description: "Read-only report dashboard accuracy, goal tracking, live-data, layout, and responsive-design review", Modes: []string{"workshop"}},
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
// unified editable mode; "run" is constrained runtime; "reporting"
// is the report-only surface.
// Reference docs are content that used to be inlined in the workshop system
// prompt and is now loaded on demand. We intentionally do NOT migrate tool
// catalogs (TOOLS REFERENCE, Special Workspace Tools / media-tools, Browser
// Automation) because the LLM only sees tools through the MCP bridge — the
// prose catalog IS the agent's primary tool-discovery surface, and lazy-loading
// would create a bootstrap problem.
var referenceKinds = map[string]kindMeta{
	// Workflow-scoped reference docs (workshop / run modes).
	"code-authoring":              {Group: "system", Description: "Detailed main.py authoring rules and patterns (env access, sys.argv contract, data authenticity, patching discipline)", Modes: []string{"workshop"}},
	"stores":                      {Group: "system", Description: "Persistent store design contract: skill vs knowledgebase vs db, when to write to which", Modes: []string{"workshop", "run"}},
	"assumption-audit":            {Group: "system", Description: "Bounded cross-artifact check that separates explicit user constraints and verified external facts from revisable design choices and agent-inferred assumptions; prevents plan/eval/report/KB/learnings/DB/code from freezing an outdated approach", Modes: []string{"workshop", "run"}},
	"review-improve-log":          {Group: "system", Description: "The single workflow Pulse log spec: Needs your decision first in Runloop; active Assumptions challenged, Today's outcome, goal progress, recent activity, collapsed technical detail, and a non-duplicative bottom Agent log in builder/improve.html; newest-on-top audit timeline, Bug/Goal verdicts, cadence, archive, and migration rules. Load before writing the log from any /review-* or /improve-* skill or Pulse.", Modes: []string{"workshop", "run"}},
	"review-improve-log-skeleton": {Group: "system", Description: "Copy-paste starter HTML skeleton for builder/improve.html, including the human-first Pulse hierarchy, optional Assumptions challenged block, Today's outcome widgets, collapsed technical details, compact bottom Agent log, mobile-first CSS, filter controls, card examples, and the stable newest-first insertion anchor. Load only when creating a new Pulse log or doing the format upgrade required by review-improve-log.", Modes: []string{"workshop", "run"}},
	"post-run-monitor":            {Group: "system", Description: "The dynamic Pulse playbook: Gate reads run evidence, changelog, eval/report/DB/KB/learnings, human inputs, and Chief of Staff recommendations; writes a plain-language Pulse Worklist to builder/improve.html; records selected modules in db/db.sqlite via record_pulse_worklist; then only due modules run (harden, artifact review, learning/KB/DB health, eval/report health, cost/LLM/time, Goal Advisor) before one ordered dashboard/backup/publish/notify finalizer with real command statuses. Goal Advisor is selected by Pulse Gate, not a separate recurring schedule. Load when running the post-run monitor pass.", Modes: []string{"workshop", "run"}},
	"message-sequence":            {Group: "system", Description: "Message-sequence patterns — when same-context ordered turns should share one conversation, route patterns (stateful specialist, test/fix loop, maker+reviewer, panel, clean-room retry, HITL re-entry, scripted conversation), and single-step quality patterns (self-validation/interrogation gate, compute-then-reason, citation/grounding gate, self-healing script). Load when multiple regular steps may collapse into message_sequence, when using message_sequence as a todo_task route, or when a standalone step should self-check its own work.", Modes: []string{"workshop"}},
	"routing":                     {Group: "system", Description: "Routing step design: when to use routing vs todo_task/message_sequence/human_input, deterministic route_selection.json contract, route_selections for builder-selected fixed branches, route structure (route_id/condition/next_step_id/default_route_id), anti-patterns", Modes: []string{"workshop"}},
	"todo-task":                   {Group: "system", Description: "todo_task (orchestrator / sub-workflow / pipeline) step design: when to use vs routing / message_sequence / regular, anatomy (todo_task_step + predefined_routes), inline sub_agent_step vs orphan_step_ref, nested-todo_task 1-level limit, variables and group_name handling, anti-patterns, scripted-mode fast path. Load before adding or restructuring a todo_task step.", Modes: []string{"workshop"}},
	"human-input":                 {Group: "system", Description: "human_input step design: text vs yesno vs multiple_choice input types, when to ask during a run vs when to use routing with route_selections, schedule (unattended) considerations and the human_inputs run_full_workflow arg, downstream validation, anti-patterns. Load before adding or editing a human_input step.", Modes: []string{"workshop"}},
	"regular":                     {Group: "system", Description: "regular step design: use only as the scripted boundary for deterministic API/CLI/data work; covers anatomy, required validation_schema, store access, and anti-patterns. Conversational work always uses message_sequence. Load before adding a scripted regular step or when unsure which step type fits.", Modes: []string{"workshop"}},
	"workflow-patterns":           {Group: "system", Description: "Recurring workflow composition patterns: routing, shared-context investigation, coherent scripted pipelines, independent fan-out, in-context verification, pre-flight probes, human checkpoints, critique, durable persistence, and SQL-driven foreach. Each pattern follows one large message_sequence per shared-context span. Load when starting a new plan or restructuring an existing one.", Modes: []string{"workshop"}},
	"optimize-playbook":           {Group: "system", Description: "Optimizer deep-dive: harden vs replan decision tree, eval, and the Pulse/Goal Advisor framework", Modes: []string{"workshop"}},
	"step-config":                 {Group: "system", Description: "Per-step config reference (planning/step_config.json via update_step_config): the three store-access modes (learnings_access, knowledgebase_access, db_access + the $DB_PATH contract), the three locks (lock_learnings/lock_code/lock_knowledgebase), execution mode (agentic vs scripted + promotion gates), model selection (execution_tier/execution_llm), validation_schema, skills/tools, and clearing fields. Load before tuning a step's access, locks, mode, or model.", Modes: []string{"workshop"}},
	"file-layout":                 {Group: "system", Description: "Workspace file layout reference and path discipline", Modes: []string{"workshop", "run"}},
	"plan-design":                 {Group: "system", Description: "Plan-design playbook: step boundaries, step-type selection, context flow, validation/failure design, anti-patterns, step-types reference. Load when designing a new plan or restructuring an existing one in DESIGN phase.", Modes: []string{"workshop"}},
	"plan-change-impact":          {Group: "system", Description: "Plan-change impact analysis: when a step changes (add/remove/reorder, output contract, db writes, or behavior) trace and reconcile the blast radius across downstream steps, evals, the report dashboard, db, learnings, and KB — by searching the workspace for references to the change's surface, then fixing the clear ones and flagging the judgment calls. The planning/changelog is the work-list; record an impact summary and let review-artifact-drift be the audit backstop. Load before treating any plan change (builder edit, replan, or harden) as done.", Modes: []string{"workshop"}},
	"report-plan":                 {Group: "system", Description: "Report toolchain: HTML documents plus explicitly user-configured native interaction widgets registered in report_plan.json. HTML reads db/db.sqlite through window.report; interaction answers persist in report_widget_responses for later workflow steps. Covers authoring, widget consumption, design, themes, tabs, validate/preview. Load before authoring or editing reports/report_plan.json.", Modes: []string{"workshop"}},
	"evaluation-plan":             {Group: "system", Description: "Evaluation plan rules + writing-a-good-eval best practices: required fields, route gating, ID collision discipline, TARGET_RUN_PATH placeholder, step config (prefer scripted/deterministic evals; declared_execution_mode + execution_tier), anti-placeholder/anti-gaming + outcome-grounded scoring, validate/run workflow. Load before editing evaluation/evaluation_plan.json.", Modes: []string{"workshop"}},
	"llm-provider-config":         {Group: "system", Description: "Model Library and provider-auth management for multi-agent chat and workflow workshop: discover provider models, validate candidates, optionally save reusable provider/model/options configurations, preserve reasoning_effort, and never read or edit config/ files directly. Load when the user asks which LLMs exist, wants a reusable saved configuration, or needs provider auth.", Modes: []string{"multi-agent", "workshop"}},
	"llm-selection":               {Group: "system", Description: "Choosing the LLM that runs workflow work: provider-profile vs explicit Builder/Maintenance/Pulse/high/medium/low roles via set_workflow_llm_config, per-step overrides (execution_tier, execution_llm, validation_llm), precedence rules, cost review tools, and provider auth. Load when picking, pinning, or changing which model executes a workflow step (not media generation — see workspace-media-tools for that).", Modes: []string{"workshop"}},
	"skill-management":            {Group: "system", Description: "Install skills and wire them into workflows: find (list_skills/search_skills), install (install_skill/import_skill), select for workflow/builder context (update_workflow_config add_skills), enable at runtime per step (update_step_config enabled_skills), the no-cascade attachment model, learnings/_global/SKILL.md as shared-know-how home, remove/uninstall, and troubleshooting. Load before installing a skill or wiring skills onto a workflow or step.", Modes: []string{"multi-agent", "workshop"}},
	"org-goals":                   {Group: "system", Description: "Org goals contract for Chief of Staff mode: how to interview the CEO for real company-style KPI targets (baseline/current value, target value, unit, owner, due date, source of truth), back up org artifacts, write the self-contained pulse/goals.html scorecard, align every workflow to goals (or mark supporting/unaligned), and measure workflow runs against named goal outcomes using Pulse verdicts, reports, db, and run evidence. Load before setting/changing goals, creating workflows from goals, assigning workflows for goals, or reporting workflow performance against goals.", Modes: []string{"multi-agent"}},
	"org-html":                    {Group: "system", Description: "Org HTML design contract for Chief of Staff right-panel artifacts: self-contained, theme-aware, right-panel-first, colorful widget-first structure and starter skeletons for pulse/goals.html and pulse/org-pulse.html. Load before writing or materially changing org goals or org pulse HTML.", Modes: []string{"multi-agent"}},
	"org-pulse":                   {Group: "system", Description: "The Org Pulse playbook (the Chief of Staff's daily heartbeat over the whole org): back up org artifacts first, read the org goals scorecard at pulse/goals.html, gather evidence in one sweep (each workflow's builder/improve.html verdict + reports + knowledgebase + global learnings, recent conversation files, and pulse/task.html findings), measure workflows against the org goals, roll the per-workflow Goal verdicts into an org endgame read, spot promotions (a repeated ad-hoc task -> propose turning it into a workflow), record everything in pulse/org-pulse.html, and publish pulse/goals.html + pulse/org-pulse.html only when org publish is verified/configured. Workflow internals stay read-only except adding goal-aligned recommendation/open-finding cards to builder/improve.html; judge/curate/suggest, never run or fix. Load when running the daily Org Pulse pass.", Modes: []string{"multi-agent"}},
	"chief-task-report":           {Group: "system", Description: "Chief of Staff scheduled task report contract: after a normal non-system multi-agent schedule completes, update the single shared pulse/task.html Tasks page with colorful task-dashboard widgets, run summary, decisions/recommendations, evidence, next action, and key findings to reuse next time. Separate from Org Pulse; never create per-task report files.", Modes: []string{"multi-agent"}},
	"delegation":                  {Group: "system", Description: "Multi-agent chat delegation contract: the four tools (delegate/query_agent/terminate_agent/list_agents), delegate parameters (name, instruction, reasoning_level, agent_template, servers, skills, share_browser), async background execution model, the max-depth-3 hierarchy (root→child→grandchild), explicit-pass skill semantics (sub-agents inherit NO skills), and what subagents/ templates actually apply at runtime (default_reasoning_level + body only — frontmatter skills/servers are NOT applied). Load before delegating work to sub-agents in chat.", Modes: []string{"multi-agent"}},

	// Multi-agent chat reference docs (rare-path topics — schedule/secret
	// management — that don't warrant always-loaded prompt space).
	"schedule-management": {Group: "system", Description: "Schedule cron, edit, update, or remove multi-agent scheduled tasks via _users/<id>/multiagent-schedules.json", Modes: []string{"multi-agent"}},
	"secret-management":   {Group: "system", Description: "Manage workflow / user / global secrets via list_secrets, set_workflow_secret, set_user_secret, delete_workflow_secret, delete_user_secret — buckets, naming rules, attach-after-store discipline", Modes: []string{"multi-agent", "workshop"}},

	// Cross-mode operational reference docs (browser and code-execution bridge).
	// Currently duplicated in the always-on system-prompt sections;
	// adding them as skills lets the agent load deep details on-demand and
	// sets up the eventual prompt-trim.
	"html-output":           {Group: "system", Description: "High-quality self-contained HTML report guide: when to use HTML vs JSON vs Markdown, layout baseline with dark-mode styles, summary box, sticky nav, inline bar chart (no CDN), badge classes for pass/fail/warn, quality checklist. Load before writing any .html output file.", Modes: []string{"multi-agent", "workshop", "run"}},
	"browser-usage":         {Group: "system", Description: "Browser automation deep guide: agent_browser HTTP API, CDP vs headless modes, macOS CDP installation and additional port/profile setup, snapshot/click/fill workflow, tab management, file uploads, session limits, common mistakes. Load when installing or driving a CDP browser, scraping pages, automating logins, or uploading files via a web form.", Modes: []string{"multi-agent", "workshop", "run"}},
	"mcp-bridge":            {Group: "system", Description: "MCP HTTP bridge mechanics: $MCP_API_URL / $MCP_API_TOKEN env vars, curl pattern for calling MCP tools, response envelope, $VAR_* / $SECRET_* variable rules, single-call discipline. Load before writing scripts that call MCP tools via the bridge, or when debugging bridge errors.", Modes: []string{"multi-agent", "workshop", "run"}},
	"workflow-tools":        {Group: "system", Description: "Full reference for workshop / workflow tools: step execution & inspection (execute_step, query_step, debug_step, run_full_workflow), step config and read-only review tools, plan modification (add/update/delete step tools, todo_task routes, versioning), Goal Advisor proposal workflow, variables & MCP server config, schedule management, shell, skills, and secrets. Load when you need a tool's exact signature, parameters, or when-to-use rules and the inline cheat sheet doesn't suffice.", Modes: []string{"workshop"}},
	"workspace-media-tools": {Group: "system", Description: "Workspace-level provider-backed tools: text generation (generate_text_llm, search_web_llm), image generation + editing (image_gen, image_edit), video generation (generate_video — Vertex AI vs Gemini API model routing), audio + music (text_to_speech, speech_to_text, generate_music), image reading (read_image), capability discovery (list_llm_capabilities, estimate_llm_cost, set_provider_auth). Full path / provider / model_id contracts, default providers per capability, provider-setup discipline. Load before generating media, reading non-text files, or wiring provider auth.", Modes: []string{"multi-agent", "workshop", "run"}},
	"execution-policy":      {Group: "system", Description: "Per-group sequential execution policy for run_full_workflow on multi-group workflows: why per-group by default (cleaner failure signal, fixes propagate forward, avoids resource contention, earlier abort, correct iteration rotation), the recipe pattern, exceptions where parallel is appropriate, and how to handle ambiguous 'run the workflow' requests. Load before kicking off a multi-group run or when the user asks about parallel/sequential execution.", Modes: []string{"workshop", "run"}},
	"deployed-channel":      {Group: "system", Description: "Deployed channel runtime: handling Slack/WhatsApp/bot-channel-routed workflow requests — group identification from message, runtime context grounding (soul.md/learnings/KB/db), direct answer vs run_full_workflow vs execute_step decision, channel-context plumbing through human_inputs, in-channel result summarization, and Run-vs-Workshop boundary rules. Load when a chat or message arrives via a deployed channel route.", Modes: []string{"workshop", "run"}},
	"reporting-policy":      {Group: "system", Description: "Live report viewer policy: HTML documents read db/db.sqlite through window.report, while explicitly user-configured native interaction widgets persist answers for later workflow runs. Covers report/dashboard authoring, interaction ownership, tabs, diagnosis, refresh, and the Run-mode boundary. Load when the user mentions reports, dashboards, configured report controls, themes, or layouts.", Modes: []string{"workshop", "run"}},
	"running-steps":         {Group: "system", Description: "Step execution mechanics: iterations & groups (always iteration-0 in workshop builder, read variables.json for group names), the 6-step execution procedure (determine group → execute_step → handle human_input → wait/notification → success/failure handling → always follow up), auto-notification system (no polling, system-generated [AUTO-NOTIFICATION] prefix, may be delayed), and stopping tasks (stop_all_executions / stop_step are required, text alone does NOT stop). Load before calling execute_step / run_full_workflow or when a user asks how to stop/cancel.", Modes: []string{"workshop", "run"}},
	"planning-steps":        {Group: "system", Description: "Workshop plan composition: take-action-by-default discipline, one large message_sequence per shared-context span with proof/double-check/repair turns, intelligent separation when contexts should not be shared, scripted deterministic boundaries, required validation_schema, forward-only context flow, step types, fixed-branch routing, and deeper references. Load before adding/editing plan steps.", Modes: []string{"workshop"}},
	"workshop-mode-flow":    {Group: "system", Description: "Workshop mode operating playbook: foundation checks, the core run → eval → classify → review → fix → verify loop, Pulse Bug Review/Fixer versus Goal Advisor proposal, optimization workflow steps, and mode redirects. Load when choosing between reliability repair, plan-change proposal, eval improvement, or no action.", Modes: []string{"workshop"}},
	"debugging-flow":        {Group: "system", Description: "Debugging failed/stuck workflow steps: read-only investigation, Pulse Bug Review/Fixer, bounded manual fixes, run-mode inspection, root-cause mapping, and retry versus design-change decisions. Load when a step fails or behaves unexpectedly.", Modes: []string{"multi-agent", "workshop", "run"}},
	"publish-strategy":      {Group: "system", Description: "Publish HTML artifacts (workflow dashboard + builder/improve.html, or org pulse/goals.html + pulse/org-pulse.html) to a public URL on any static host — the share-twin of backup. Provider-agnostic + agentic (no per-provider code): three universal deploy paths (provider CLI like netlify/vercel/wrangler/gh-pages/surge/firebase; git-push-to-deploy; object-store/rclone/rsync sync), auth from a named secret. Includes the static-dashboard snapshot procedure (run the report's window.report.query SQL against db.sqlite, inline the results as JSON + a shim, deploy static — never ship the DB), a privacy/scope confirmation before exposing data, the configure->verify->auto flow, workflow publish/status.json, and org pulse/publish.json + pulse/publish/status.json. Load when the user asks to publish/share/host a workflow report/pulse or org Goals/Pulse pages, or to set up a publish destination.", Modes: []string{"multi-agent", "workshop", "run"}},
	"backup-strategy":       {Group: "system", Description: "Workflow and org backup playbook: when to commit to git vs push to a large-file backend, what never to back up (secrets, transient state), workflow status in backup/status.json, org config/status in pulse/backup.json + pulse/backup/status.json, git commit/pull/push discipline (atomic commits, --force-with-lease, JSON merge handling, hook bypass policy), and a comparison of large-file backends — HuggingFace Hub, Cloudflare R2, Backblaze B2, AWS S3, Google Cloud Storage, Azure Blob, and rclone. Includes CLI commands, auth env-var convention, and a decision matrix by content type. Load when the user asks about backup/versioning, how to push a workflow folder, org goals/pulse backup, or setting up a storage destination.", Modes: []string{"multi-agent", "workshop", "run"}},
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
		"like /design-plan or /improve-evaluation (the slash command will name the kind to pass; pass the surrounding " +
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

	// The per-kind catalog (name + description) is deliberately NOT inlined
	// here: the workflow-reference / multiagent-reference mega-skill's TOC
	// already lists every mode-allowed kind, and duplicating the registry in
	// two attached skills costs prompt tokens in every session. This skill
	// just points at that catalog.
	hasKinds := false
	for _, kind := range kindEnumFrom(referenceKinds) {
		if modeAllowedIn(kind, mode, referenceKinds) {
			hasKinds = true
			break
		}
	}
	kindList := "(no reference docs are available in this mode)\n"
	if hasKinds {
		refSkillName := referenceSkillSpecForMode(mode).Name
		kindList = "The full catalog of kinds, each with a description of when to load it, is the `references/` list in the `" + refSkillName + "` skill. Each `references/<kind>.md` file there corresponds to a kind you can pass to `get_reference_doc`.\n"
	}

	configAccess := buildConfigurationAccessGuidance(mode)

	body := `This skill is a quick guide to the system tools available in this session. Use it as your map for discovery and deep documentation.

## Tool / API discovery

- ` + "`get_api_spec(server_name, tool_name)`" + ` — when you do not know an MCP tool's parameters or response shape, call this first.
- ` + "`get_reference_doc(kind, focus?)`" + ` — system reference docs. Load the matching doc before any deep action (e.g. read ` + "`post-run-monitor`" + ` before Pulse review/fix work; read ` + "`code-authoring`" + ` before authoring ` + "`main.py`" + `; read ` + "`llm-selection`" + ` before ` + "`set_workflow_llm_config`" + `). Some tools refuse to run until their precondition doc has been loaded — the error will name the kind.
- ` + "`get_workflow_command_guidance(kind, focus?)`" + ` — canonical procedural flows (design-plan, improve-evaluation, goal-advisor, define-success, etc.). The returned text is your instructions for that turn; follow it verbatim.
- ` + "`run_goal_advisor_review(pulse_run_id?, focus?)`" + ` — when available, spawn Goal Advisor as a dedicated background agent instead of doing expensive strategic review inline in the parent Pulse/workshop turn.

## Configuration access

` + configAccess + `

### Reference doc kinds available in this mode

` + kindList + `
## MCP bridge — only in code-execution mode

When you are running scripts via ` + "`execute_shell_command`" + ` (code-execution mode), call MCP tools through HTTP:

` + "```bash" + `
payload='{"arg":"value"}'
curl -sS --json "$payload" -H "$MCP_AUTH" "$MCP_MCP/{server_name}/{tool_name}" | jq
` + "```" + `

Use ` + "`$MCP_MCP`" + ` only for real MCP servers from the workflow's selected server list. Built-in/custom categories such as ` + "`human_tools`" + `, ` + "`workflow`" + `, ` + "`workspace_advanced`" + `, ` + "`auto_improvement`" + `, and ` + "`knowledgebase_tools`" + ` are not MCP servers; call them as ` + "`$MCP_CUSTOM/{tool_name}`" + ` with no category segment. Example: ` + "`$MCP_CUSTOM/notify_user`" + `, never ` + "`$MCP_MCP/human_tools/notify_user`" + `.

Pre-set environment for scripts:
- ` + "`$MCP_MCP`" + `, ` + "`$MCP_CUSTOM`" + `, ` + "`$MCP_VIRTUAL`" + ` — short bridge endpoint bases
- ` + "`$MCP_AUTH`" + ` — Authorization header value for ` + "`curl -H`" + `
- ` + "`$MCP_API_URL`" + ` + ` + "`$MCP_API_TOKEN`" + ` — full endpoint + token fallback
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

func buildConfigurationAccessGuidance(mode string) string {
	var parts []string
	parts = append(parts, "LLM/provider configuration is tool-managed. For published chat models and provider auth, use `list_published_llms`, `list_provider_models`, `test_llm`, `save_published_llm`, and `set_provider_auth` as appropriate. Do not read or edit `config/` files with shell or file tools; load `get_reference_doc(kind=\"llm-provider-config\")` before publishing or changing provider auth.")

	if modeAllowedIn("llm-selection", mode, referenceKinds) {
		parts = append(parts, "For workflow execution tiers and per-step model choices, use `get_llm_config` and `set_workflow_llm_config`; load `get_reference_doc(kind=\"llm-selection\")` before changing workflow execution models.")
	}
	if modeAllowedIn("workspace-media-tools", mode, referenceKinds) {
		parts = append(parts, "For media/search provider tools, load `get_reference_doc(kind=\"workspace-media-tools\")`.")
	}

	return strings.Join(parts, " ")
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
		"writing to db/kb/skill stores, applying Pulse fixes, or applying material plan changes. " +
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
