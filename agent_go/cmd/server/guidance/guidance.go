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

	// Tools names the runtime tools this doc explains. It is the selection key
	// for surfaces that choose references by capability instead of by mode —
	// today that is step execution (see MaterializeStepExecutionReferenceSkill).
	//
	// PLAT-125: a step agent holding eight tools was handed the workshop chat's
	// 41-doc builder bundle, including llm-provider-config, and duly called
	// list_published_llms — which its session never registered. Left empty, a
	// doc is simply never selected by capability, which is the safe default for
	// the builder- and Pulse-facing docs that no runtime tool corresponds to.
	Tools []string
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
	"design-plan": {Group: "builder", Description: "Comprehensive workflow plan and dependent-artifact review with better design recommendations", Modes: []string{"workshop"}},

	// Reviews — recommend, don't apply; persist their typed result through Pulse.
	// TEMPORARY (PLAT-259): manual live-reverify diagnostic for the `branch`
	// step type. Remove this entry, templates/review/verify-branch-step.md,
	// and the /verify-branch-step frontend command once confirmed working.
	"verify-branch-step": {Group: "review", Description: "TEMPORARY (PLAT-259): manually verify a real branch step persists, executes, logs, and navigates correctly", Modes: []string{"workshop"}},
	// TEMPORARY (PLAT-259): migrates a pre-split plan's routing steps onto
	// the new routing/branch semantics. Remove this entry, templates/review/
	// migrate-routing-to-branch.md, and the /migrate-routing-to-branch
	// frontend command once confirmed working.
	"migrate-routing-to-branch": {Group: "review", Description: "TEMPORARY (PLAT-259): reclassify existing routing steps as branch where appropriate and check route best practices", Modes: []string{"workshop"}},
	"review-artifact-drift":     {Group: "review", Description: "Manual on-demand equivalent of the scheduled plan_drift_review pass (same candidate collector, repair contract, and completion writer — including deletion-coverage audits), plus a read-only checklist for everything it doesn't cover: schedule drift, learnings, main.py, KB, db, reports, and eval wiring", Modes: []string{"workshop"}},
	"ops-review":                {Group: "review", Description: "One-off agentic read-only review of cost, timing, tool/runtime reliability, model routing, setup, and plan-design hygiene", Modes: []string{"workshop"}},
	"strategy-auditor":          {Group: "review", Description: "One-off read-only cross-run diagnosis of whether the current plan can achieve the goal; does not run Goal Advisor or change the plan", Modes: []string{"workshop"}},

	// Knowledgebase maintenance — applies targeted or cross-step KB cleanup
	"improve-knowledge": {Group: "kb", Description: "Read-only knowledgebase/notes health review with targeted or cross-step fixer recommendations", Modes: []string{"workshop"}},

	// Learning maintenance — read-only targeted/cross-step review of learnings/_global
	"improve-learnings": {Group: "learning", Description: "Read-only learnings/_global health review with targeted or current-plan fixer recommendations", Modes: []string{"workshop"}},

	// DB maintenance — read-only guarded schema/contract review of db/db.sqlite
	"improve-database": {Group: "db", Description: "Read-only db/db.sqlite contract, schema, integrity, and report-compatibility review", Modes: []string{"workshop"}},

	// Improvements — evidence-driven reliability and strategy flows
	"define-success":      {Group: "improve", Description: "Confirm the workflow Goal, success criteria, and operating model", Modes: []string{"workshop"}},
	"improve-evaluation":  {Group: "improve", Description: "Read-only evaluation coverage and correctness review with fixer recommendations", Modes: []string{"workshop"}},
	"engineering-review":  {Group: "improve", Description: "Run one read-only Engineering and LLM/Ops review sequence, classify workflow observations, and persist a canonical repair queue", Modes: []string{"workshop"}},
	"pulse-fixer":         {Group: "improve", Description: "Apply and verify an agent-selected bounded repair batch from canonical Pulse issues; retained Technical Review+Fix normally handles this in one task", Modes: []string{"workshop"}},
	"goal-advisor":        {Group: "improve", Description: "Strategy-first expert advisor: identify the current strategy ceiling, challenge it with one materially different high-leverage thesis, and advance one approval-gated strategy experiment through typed Pulse records from proposal through measured outcome; operational repairs remain with Pulse maintenance modules", Modes: []string{"workshop"}},
	"specialize-advisors": {Group: "improve", Description: "Propose owner-approved workflow-specific lenses for Strategy Auditor and Goal Advisor without changing their canonical roles", Modes: []string{"workshop"}},
	"design-reporting-ui": {Group: "report", Description: "Design the reporting UI as one workflow-owned db/reports/index.html experience: live data, report-owned approval buttons, and user-reviewed requests to an existing or new workflow chat via window.report.sendChatMessage.", Modes: []string{"workshop"}},
	"improve-report":      {Group: "report", Description: "Read-only report dashboard accuracy, goal tracking, live-data, layout, and responsive-design review", Modes: []string{"workshop"}},
}

// referenceKinds is the registry of system reference docs — content that
// used to live inline in the workshop system prompt and is now loaded on
// demand by the agent via mcpagent's intrinsic read_skill tool. These are not procedural
// flows (those live in allKinds); they are reference material the agent reads
// before performing certain actions (e.g. read "code-authoring" before
// patching main.py). Which docs to read is the agent's judgment call: every
// kind is also materialized into the projected reference skill, so the agent
// can reach the same content by reading references/<kind>.md from the attached
// builder-reference bundle.
//
// Adding a new reference doc:
//
//  1. Drop a markdown file in templates/system/<kind>.md.
//  2. Add an entry here with description + allowed modes.
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
	"code-authoring":        {Group: "system", Description: "Detailed main.py authoring rules and patterns (env access, sys.argv contract, data authenticity, patching discipline)", Modes: []string{"workshop"}},
	"stores":                {Group: "system", Description: "Persistent store design contract: skill vs knowledgebase vs db, when to write to which", Modes: []string{"multi-agent", "workshop"}, Tools: []string{"query_workflow_db", "mutate_workflow_db", "apply_workflow_db_migration"}},
	"assumption-audit":      {Group: "system", Description: "Bounded cross-artifact check that separates explicit user constraints and verified external facts from revisable design choices and agent-inferred assumptions; prevents plan/eval/report/KB/learnings/DB/code from freezing an outdated approach", Modes: []string{"workshop"}},
	"pulse-gate":            {Group: "system", Description: "Focused scheduler Pulse Gate contract: progressive evidence scan, concerns and cadence classification, complete three-module worklist, compact durable handoff, and no review/fix/finalizer work.", Modes: []string{"workshop"}},
	"pulse-review-fixer":    {Group: "system", Description: "Agent-owned retained Review+Fix contract: classify observations, deduplicate roots, apply a bounded compatible repair when safe, and persist typed lifecycle state in one task.", Modes: []string{"workshop"}},
	"pulse-fixer-practices": {Group: "system", Description: "Canonical Pulse Fixer engineering practices: root-cause bundling, evidence hierarchy, complete blast-radius repair, and focused playbooks for schema/artifact contracts, databases, tool/path/permission failures, scheduler lifecycle, evaluations, and reports. Load before every Fixer mutation pass.", Modes: []string{"workshop"}},
	"pulse-finalizer":       {Group: "system", Description: "Focused scheduler Pulse finalizer contract: require terminal due-module results, then run backup, publish, and notify in one ordered truthfully recorded turn. The Pulse popup is the sole presentation.", Modes: []string{"workshop"}},
	"plan-drift-review":     {Group: "system", Description: "Focused scheduler plan_drift_review contract, a review-and-fix module like technical_review: for every step flagged needs_review or with no drift_review record, reconcile Go-precomputed deterministic checks with the remaining judgment checks (description accuracy, learnings/KB content staleness and access appropriateness, DB normalization, and for routing-typed steps only: no shared exclusive interior steps between sibling routes (explicit convergence is allowed), and complete route coverage when an eval plan exists), apply and verify safe workflow-owned fixes directly, route only genuine human decisions or platform-owned boundaries elsewhere, and persist the merged per-step result.", Modes: []string{"workshop"}},
	"pulse-bug-review":      {Group: "system", Description: "The Technical Review runtime/logic evidence pack: Exploratory QA behavioral-contract and risk-matrix method, control-path reachability checks (wrong_store_write / shadow_store_drift / dead_configuration), observable execution-trace review, and finding classifications (correctness_bug, efficiency_or_coaching, no_issue, insufficient_evidence). Gate decides whether technical_review is due and records that in the durable worklist. Load when the selected technical focus needs runtime or logic evidence.", Modes: []string{"workshop"}},
	"strategy-auditor":      {Group: "system", Description: "The current-strategy audit phase of Strategic Review: reconstruct the goal-to-action-to-observation causal chain, verify matured prior strategic work, and detect feedback loops, proxy optimization, concentration, bias, contamination, local optima, saturation, and missing causal stages before the sequence conditionally explores materially different approaches.", Modes: []string{"workshop"}},
	"fix-verification":      {Group: "system", Description: "The single contract for verifying that a bounded repair actually worked, shared by scheduled Pulse, /pulse-fixer, and approved measurement changes: the post-change evidence boundary (baseline artifacts are never proof), what counts as valid verification (a side-effect-free deterministic check through the real runtime consumer path, or a fresh post-mutation run/eval/report artifact with matching provenance — a successful write, file existence, or mtime alone is not proof), and the changed_unverified close-and-reopen rule when stronger proof needs a later normal run. Load before applying any fix.", Modes: []string{"workshop"}},
	"message-sequence":      {Group: "system", Description: "Message-sequence patterns — when same-context ordered turns should share one conversation, route patterns (stateful specialist, test/fix loop, maker+reviewer, panel, clean-room retry, HITL re-entry, scripted conversation), and single-step quality patterns (self-validation/interrogation gate, compute-then-reason, citation/grounding gate, self-healing script). Load when multiple regular steps may collapse into message_sequence, when using message_sequence as a todo_task route, or when a standalone step should self-check its own work.", Modes: []string{"workshop"}},
	"routing":               {Group: "system", Description: "Routing step design: when to use routing vs todo_task/message_sequence/human_input, deterministic route_selection.json contract, route_selections for builder-selected fixed branches, route structure (route_id/condition/next_step_id/default_route_id), anti-patterns. Routing is now the \"route\" (major sub-workflow fork) concept; for a small in-flow decision use a branch step instead.", Modes: []string{"workshop"}},
	"branch":                {Group: "system", Description: "Branch step design: a small in-flow next-step decision, same deterministic route_selection.json/routes[] mechanics as routing but without the major-fork implications -- when to use branch vs routing (now the \"route\"/major-fork concept) vs todo_task/message_sequence/human_input, branch_question requirement, anti-patterns.", Modes: []string{"workshop"}},
	"orchestrator":          {Group: "system", Description: "orchestrator step design (plan type `orchestrator`; `todo_task` is the legacy alias; users also say sub-workflow / pipeline): when to use vs routing / message_sequence / regular, anatomy (todo_task_step + predefined_routes), inline sub_agent_step vs orphan_step_ref, nested-todo_task 1-level limit, variables and group_name handling, messages as ordinary sequence items (the orchestrator runs on the message_sequence executor), anti-patterns. Load before adding or restructuring a todo_task step.", Modes: []string{"workshop"}},
	"human-input":           {Group: "system", Description: "human_input step design: free-form text for new steps; yesno/multiple_choice are legacy only. Use a human-decided branch for new fixed choices, route_selections for known route/branch answers, and human_inputs for value steps. Covers unattended runs, downstream validation, and anti-patterns. Load before adding or editing a human_input step.", Modes: []string{"workshop"}},
	"scripted":              {Group: "system", Description: "scripted step design: use only as the deterministic API/CLI/data boundary; covers anatomy, required validation_schema, store access, and anti-patterns. The internal plan type remains regular for compatibility. Conversational work always uses message_sequence. Load before adding a scripted step or when unsure which step type fits.", Modes: []string{"workshop"}},
	"playwright-scripted":   {Group: "system", Description: "Direct Python Playwright harness rules for scripted browser suites", Modes: []string{"workshop", "run"}},
	"workflow-patterns":     {Group: "system", Description: "Recurring workflow composition patterns: routing, shared-context investigation, coherent scripted pipelines, independent fan-out, in-context verification, pre-flight probes, human checkpoints, critique, durable persistence, and SQL-driven foreach. Each pattern follows one large message_sequence per shared-context span. Load when starting a new plan or restructuring an existing one.", Modes: []string{"workshop"}},
	"optimize-playbook":     {Group: "system", Description: "Optimizer deep-dive: harden vs replan decision tree, eval, and the Pulse/Strategic Review framework", Modes: []string{"workshop"}},
	"step-config":           {Group: "system", Description: "Per-step config reference (planning/step_config.json via update_step_config): store-access modes (including db_access mapped to managed query/mutation tools, with $DB_PATH only for saved scripted compatibility), locks, execution mode, model selection, validation_schema, skills/tools, and clearing fields. Load before tuning a step's access, locks, mode, or model.", Modes: []string{"workshop"}},
	"file-layout":           {Group: "system", Description: "Workspace file layout reference and path discipline", Modes: []string{"multi-agent", "workshop", "run"}},
	"workspace-views":       {Group: "system", Description: "What each workspace view on the workflow page shows and when to open or refresh it for the user (open_workspace_view / refresh_workspace_view)", Modes: []string{"workshop", "run"}, Tools: []string{"open_workspace_view", "refresh_workspace_view"}},
	"plan-design":           {Group: "system", Description: "Plan-design playbook: step boundaries, step-type selection, context flow, validation/failure design, anti-patterns, step-types reference. Load when designing a new plan or restructuring an existing one in DESIGN phase.", Modes: []string{"workshop"}},
	"step-description":      {Group: "system", Description: "How to write an optimized step description and validation_schema: earn every word, let validation_schema (not description prose) name the object shape since it's shown to the agent proactively, keep validation_schema light and load-bearing rather than exhaustive, use the description for WHAT to achieve and binding constraints, skills/learnings for reusable HOW-to-execute guidance, and validation_schema for output structure; reference shared stores instead of restating their contents, and never duplicate the same paragraph across steps. A self-check before finalizing any description or schema. Load before writing or editing any step's description.", Modes: []string{"workshop"}},
	"plan-change-impact":    {Group: "system", Description: "Plan-change impact analysis: when a step changes (add/remove/reorder, output contract, db writes, or behavior) trace and reconcile the blast radius across downstream steps, evals, the report dashboard, db, learnings, and KB — by searching the workspace for references to the change's surface, then fixing the clear ones and flagging the judgment calls. The planning/changelog is the work-list; record an impact summary and let review-artifact-drift be the audit backstop. After related edits, do one combined compatibility check in the current agent before the targeted test; reserve a full drift audit for Pulse or an explicit user request. Load before treating a plan change as done.", Modes: []string{"workshop"}},
	"evaluation-plan":       {Group: "system", Description: "Evaluation plan rules + writing-a-good-eval best practices: required fields, route gating, ID collision discipline, TARGET_RUN_PATH placeholder, step config (prefer scripted/deterministic evals; execution_mode on the plan entry + execution_tier), anti-placeholder/anti-gaming + outcome-grounded scoring, validate/run workflow. Load before editing evaluation/evaluation_plan.json.", Modes: []string{"workshop"}},
	"llm-provider-config":   {Group: "system", Description: "Model Library and provider-auth management for multi-agent chat and workflow workshop: discover provider models, validate candidates, optionally save reusable provider/model/options configurations, preserve reasoning_effort, and never read or edit config/ files directly. Load when the user asks which LLMs exist, wants a reusable saved configuration, or needs provider auth.", Modes: []string{"multi-agent", "workshop"}},
	"llm-selection":         {Group: "system", Description: "Choosing the LLM that runs workflow work: provider-profile vs explicit Builder/Pulse/high/medium/low roles via set_workflow_llm_config, per-step overrides (execution_tier, execution_llm, validation_llm), precedence rules, cost review tools, and provider auth. Load when picking, pinning, or changing which model executes a workflow step (not media generation — see workspace-media-tools for that).", Modes: []string{"workshop"}},
	"skill-management":      {Group: "system", Description: "Install skills and wire them into workflows: find (list_skills/search_skills), install (install_skill/import_skill), select for workflow/builder context (update_workflow_config add_skills), enable at runtime per step (update_step_config enabled_skills), the no-cascade attachment model, learnings/_global/SKILL.md as shared-know-how home, remove/uninstall, and troubleshooting. Load before installing a skill or wiring skills onto a workflow or step.", Modes: []string{"multi-agent", "workshop"}},

	// Multi-agent and Workshop reference docs for secret management. Run can
	// inspect available secret names but cannot create, replace, or delete them.
	"secret-management": {Group: "system", Description: "Manage workflow / user / global secrets via list_secrets, set_workflow_secret, set_user_secret, delete_workflow_secret, delete_user_secret — buckets, naming rules, attach-after-store discipline", Modes: []string{"multi-agent", "workshop"}},

	// Cross-mode operational reference docs (browser and code-execution bridge).
	// Currently duplicated in the always-on system-prompt sections;
	// adding them as skills lets the agent load deep details on-demand and
	// sets up the eventual prompt-trim.
	"html-output":           {Group: "system", Description: "High-quality self-contained HTML report guide: when to use HTML vs JSON vs Markdown, layout baseline with dark-mode styles, summary box, sticky nav, inline bar chart (no CDN), badge classes for pass/fail/warn, quality checklist. Load before writing any .html output file.", Modes: []string{"multi-agent", "workshop"}},
	"browser-usage":         {Group: "system", Description: "Browser automation deep guide: agent_browser HTTP API, CDP vs headless modes, macOS CDP installation and additional port/profile setup, snapshot/click/fill workflow, tab management, file uploads, session limits, common mistakes. Load when installing or driving a CDP browser, scraping pages, automating logins, or uploading files via a web form.", Modes: []string{"multi-agent", "workshop", "run"}, Tools: []string{"agent_browser"}},
	"mcp-bridge":            {Group: "system", Description: "MCP HTTP bridge mechanics: $MCP_API_URL / $MCP_API_TOKEN env vars, curl pattern for calling MCP tools, response envelope, $VAR_* / $SECRET_* variable rules, single-call discipline. Load before writing scripts that call MCP tools via the bridge, or when debugging bridge errors.", Modes: []string{"multi-agent", "workshop", "run"}},
	"workflow-tools":        {Group: "system", Description: "Full reference for workshop / workflow tools: step execution & inspection (execute_step, query_step, debug_step, run_full_workflow), step config and read-only review tools, plan modification (add/update/delete step tools, todo_task routes, versioning), Goal Advisor proposal workflow, variables & MCP server config, shell, skills, and secrets. Schedule management now lives in its own \"schedules\" reference doc. Load when you need a tool's exact signature, parameters, or when-to-use rules and the inline cheat sheet doesn't suffice.", Modes: []string{"workshop", "run"}},
	"schedules":             {Group: "system", Description: "Full schedule management reference: list/create/update/delete/trigger_schedule and get_schedule_runs signatures and entry shape, cron vs calendar schedules (create_calendar_schedule payload, choosing between them), how workflow schedules execute (mode=\"workshop\" always, mandatory per-schedule pulse_mode and pulse_mode_reason: off, basic, or full; legacy-only inheritance and deliberate route review coverage), the backup-on-schedule requirement, and writing messages for unattended runs (route-backed vs direct-sequence mode, why messages must never require human input). Load before creating, editing, or reasoning about any schedule.", Modes: []string{"workshop"}},
	"workspace-media-tools": {Group: "system", Description: "Active workspace LLM tools: generate_text_llm and search_web_llm, plus provider-auth discipline. Provider media tools are deprecated and hidden from agents while text/search testing is the focus.", Modes: []string{"multi-agent", "workshop", "run"}, Tools: []string{"search_web_llm", "generate_text_llm"}},
	"execution-policy":      {Group: "system", Description: "Per-group sequential execution policy for run_full_workflow on multi-group workflows: why per-group by default (cleaner failure signal, fixes propagate forward, avoids resource contention, earlier abort, correct iteration rotation), the recipe pattern, exceptions where parallel is appropriate, and how to handle ambiguous 'run the workflow' requests. Load before kicking off a multi-group run or when the user asks about parallel/sequential execution.", Modes: []string{"workshop", "run"}},
	"deployed-channel":      {Group: "system", Description: "Deployed channel runtime: handling Slack/WhatsApp/bot-channel-routed workflow requests — group identification from message, runtime context grounding (soul.md/learnings/KB/db), direct answer vs run_full_workflow vs execute_step decision, channel-context plumbing through human_inputs, in-channel result summarization, and Run-vs-Workshop boundary rules. Load when a chat or message arrives via a deployed channel route.", Modes: []string{"workshop", "run"}},
	"runtime-context":       {Group: "system", Description: "Ground direct runtime work in workflow learnings, business context, DB contracts, current results and costs; capture explicit durable user rules without crossing Run-mode edit boundaries.", Modes: []string{"workshop", "run"}},
	"human-in-the-loop":     {Group: "system", Description: "Choose how a workflow gets human input: chat, planned checkpoints, urgent response cards, durable Pulse decisions, report-owned approvals, report-to-chat requests, and live execution corrections. Load when designing or handling human review, approval, feedback, or a human-in-the-loop handoff; distinguish saved answers, queued requests, and applied work.", Modes: []string{"workshop", "run"}, Tools: []string{"human_feedback", "create_human_input_request", "answer_human_input_request", "get_human_input_request", "list_approved_fixer_decisions", "send_step_message"}},
	"reporting-policy":      {Group: "system", Description: "HTML report contract: db/reports/index.html, live data and field writes, approval-save then sendChatMessage to an existing/new workflow chat, queued/cancelled receipts, authoring, refresh, preview, and Run-mode boundaries. Load for reports, dashboards, report action buttons, agent chat handoffs, themes, or layouts.", Modes: []string{"workshop", "run"}},
	"running-steps":         {Group: "system", Description: "Step execution mechanics: iterations & groups (always iteration-0 in workshop builder, read variables.json for group names), the 6-step execution procedure (determine group → execute_step → handle human_input → wait/notification → success/failure handling → always follow up), auto-notification system (no polling, system-generated [AUTO-NOTIFICATION] prefix, may be delayed), and stopping tasks (stop_all_executions / stop_step are required, text alone does NOT stop). Load before calling execute_step / run_full_workflow or when a user asks how to stop/cancel.", Modes: []string{"workshop", "run"}},
	"planning-steps":        {Group: "system", Description: "Workshop plan composition: take-action-by-default discipline, one large message_sequence per shared-context span with proof/double-check/repair turns, intelligent separation when contexts should not be shared, scripted deterministic boundaries, required validation_schema, forward-only context flow, step types, fixed-branch routing, and deeper references. Load before adding/editing plan steps.", Modes: []string{"workshop"}},
	"workshop-mode-flow":    {Group: "system", Description: "Workshop mode operating playbook: foundation checks, the core run → eval → classify → review → fix → verify loop, Pulse Bug Review/Fixer versus Goal Advisor proposal, optimization workflow steps, and mode redirects. Load when choosing between reliability repair, plan-change proposal, eval improvement, or no action.", Modes: []string{"workshop"}},
	"debugging-flow":        {Group: "system", Description: "Debugging failed/stuck workflow steps: read-only investigation, Pulse Bug Review/Fixer, bounded manual fixes, run-mode inspection, root-cause mapping, and retry versus design-change decisions. Load when a step fails or behaves unexpectedly.", Modes: []string{"multi-agent", "workshop", "run"}},
	"publish-strategy":      {Group: "system", Description: "Publish workflow report dashboards or org pulse/goals.html + pulse/org-pulse.html to a public URL on any static host — the share-twin of backup. Provider-agnostic + agentic (no per-provider code): three universal deploy paths (provider CLI like netlify/vercel/wrangler/gh-pages/surge/firebase; git-push-to-deploy; object-store/rclone/rsync sync), auth from a named secret. Includes the static-dashboard snapshot procedure (run the report's window.report.query SQL against db.sqlite, inline the results as JSON + a shim, deploy static — never ship the DB), a privacy/scope confirmation before exposing data, the configure->verify->auto flow, workflow publish/status.json, and org pulse/publish.json + pulse/publish/status.json. Load when the user asks to publish/share/host a workflow report or org Goals/Pulse pages, or to set up a publish destination.", Modes: []string{"multi-agent", "workshop"}},
	"backup-strategy":       {Group: "system", Description: "Workflow and org backup playbook: when to commit to git vs push to a large-file backend, what never to back up (secrets, transient state), workflow status in backup/status.json, org config/status in pulse/backup.json + pulse/backup/status.json, git commit/pull/push discipline (atomic commits, --force-with-lease, JSON merge handling, hook bypass policy), and a comparison of large-file backends — HuggingFace Hub, Cloudflare R2, Backblaze B2, AWS S3, Google Cloud Storage, Azure Blob, and rclone. Includes CLI commands, auth env-var convention, and a decision matrix by content type. Load when the user asks about backup/versioning, how to push a workflow folder, org goals/pulse backup, or setting up a storage destination.", Modes: []string{"multi-agent", "workshop"}},
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

// standaloneReviewLensKinds are guidance kinds whose template text is a
// read-only Engineering Review lens ("do not record findings yourself, the
// parent agent records after all reviewers return"). That contract assumes
// the kind was loaded INSIDE ops-review's Technical Review turn, alongside
// sibling lenses, with a parent turn recording their combined findings —
// ops-review reaches these templates only through materialize.go's
// read_skill bundle (see renderKind's other call site), never through this
// tool. So any call that reaches this handler for one of these kinds is
// always a genuine standalone/top-level invocation (a slash command or
// matched chat intent) with no parent turn — without this notice the
// reviewer would generate findings exactly as instructed, then discard them
// when the turn ends, contradicting the kind's own "never a standalone
// reviewer result" contract.
var standaloneReviewLensKinds = map[string]bool{
	"improve-report":     true,
	"improve-knowledge":  true,
	"improve-database":   true,
	"improve-learnings":  true,
	"improve-evaluation": true,
}

const standaloneReviewLensRecordingNotice = `

STANDALONE MODE. This checklist is normally loaded as one lens inside a larger Engineering Review turn (ops-review), which records every lens's findings once they all return. You were called directly — there is no such parent turn. Record your own findings before finishing this turn: call record_pulse_review_focus(workspace_path=..., pulse_run_id="current", module="technical_review", focus_key=..., priority_class=..., selection_reason=...) once for the focus you investigated, then record_pulse_finding for each finding above. Do this even though the checklist text above told you not to record — that instruction assumes a parent turn that does not exist here.`

// appendStandaloneReviewLensNotice appends standaloneReviewLensRecordingNotice
// to text when kind is one of standaloneReviewLensKinds, otherwise returns
// text unchanged.
func appendStandaloneReviewLensNotice(kind, text string) string {
	if !standaloneReviewLensKinds[kind] {
		return text
	}
	return text + standaloneReviewLensRecordingNotice
}

// RegisterGuidanceTool exposes get_workflow_command_guidance to the agent.
// The tool returns the rendered prompt for any kind in allKinds. Mode is
// validated against the kind's allow-list — calling a kind from the wrong
// mode returns an error message instructing the agent to suggest a mode
// switch.
type DefinitionToolRegistrar interface {
	RegisterCustomTool(string, string, map[string]interface{}, func(context.Context, map[string]interface{}) (string, error), string) error
}

func RegisterGuidanceTool(agent DefinitionToolRegistrar, currentMode string, logger loggerv2.Logger) {
	desc := "Get the canonical guided-flow text for any workflow command. " +
		"Call this tool — and follow the returned instructions verbatim — when (1) the user invokes a slash command " +
		"like /design-plan or /improve-evaluation — for most commands the slash name IS the kind, but a focused Pulse " +
		"review alias (e.g. /pulse-review-validation-contract, /pulse-review-execution-health) maps to a DIFFERENT " +
		"kind and a specific focus; when the dispatch message explicitly states kind=... and focus=... (as these " +
		"aliases do), use those exact literal values — never derive kind from the alias text itself. Pass the " +
		"surrounding conversation/request into focus when available, (2) the user describes " +
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
				"description": "Optional. Full run folder path (e.g. \"iteration-3/group-a\"). Used by ops-review / improve-evaluation-style flows that anchor on a specific run.",
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
		text = appendStandaloneReviewLensNotice(kind, text)
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
// the MCP bridge, get_api_spec for tool discovery, read_skill for deeper
// system docs, and get_workflow_command_guidance for
// procedural flows. The skill enumerates the reference-doc kinds that
// are allowed in the given mode so the agent knows which bundled files it can
// read.
//
// Why a meta-skill rather than one skill per reference doc: copying
// every reference-doc body into a skill folder per session duplicates
// content and risks drift. Instead this small skill points at the
// attached bundle so the agent loads detail on demand through mcpagent.
//
// An empty mode returns nil (no skill to attach).
func BuildSystemToolsSkill(mode string) *llmtypes.Skill {
	if strings.TrimSpace(mode) == "" {
		return nil
	}

	// The per-kind catalog (name + description) is deliberately NOT inlined
	// here: the builder-reference mega-skill's TOC
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
		kindList = "The full catalog is the `references/` list in the `" + refSkillName + "` skill. Read a topic with `read_skill(skills=[{\"name\":\"" + refSkillName + "\",\"path\":\"references/<kind>.md\"}])`.\n"
	}

	configAccess := buildConfigurationAccessGuidance(mode)
	referenceExamples := "`pulse-gate` for Pulse Gate, `pulse-review-fixer` for review/fix work, `code-authoring` before authoring `main.py`, or `llm-selection` before changing workflow models"
	proceduralGuidance := "- `get_workflow_command_guidance(kind, focus?)` — canonical procedural flows (design-plan, improve-evaluation, goal-advisor, define-success, etc.). The returned text is your instructions for that turn; follow it verbatim.\n"
	if strings.EqualFold(strings.TrimSpace(mode), "run") {
		referenceExamples = "`runtime-context` before answering from workflow state, `running-steps` before execution, or `human-in-the-loop` when work needs a user decision"
		proceduralGuidance = ""
	}

	body := `This skill is a quick guide to the system tools available in this session. Use it as your map for discovery and deep documentation.

## Tool / API discovery

- ` + "`get_api_spec(server_name, tool_name)`" + ` — when you do not know an MCP tool's parameters or response shape, call this first.
- ` + "`read_skill(skills=[{\"name\":\"builder-reference\",\"path\":\"references/<kind>.md\"}])`" + ` — load system reference docs before deep actions (for example ` + referenceExamples + `). This intrinsic mcpagent tool works on API and coding-CLI transports; native CLI skill files are the same bundle.
` + proceduralGuidance + `
## Configuration access

` + configAccess + `

### Reference doc kinds available in this mode

` + kindList + `
## MCP bridge — only in code-execution mode

When you are running scripts via ` + "`execute_shell_command`" + ` (code-execution mode), call MCP tools through HTTP:

` + "```bash" + `
payload='{"arg":"value"}'
curl --fail-with-body -sS --json "$payload" -H "$MCP_AUTH" "$MCP_MCP/{server_name}/{tool_name}"
` + "```" + `

` + "`$MCP_AUTH`" + ` is already the complete ` + "`Authorization: Bearer ...`" + ` header. Never prepend another header or Bearer prefix. ` + "`--json`" + ` already selects POST and Content-Type, so do not add ` + "`-X POST`" + `, another Content-Type header, or ` + "`--data`" + `. Keep the call unpiped so curl's nonzero HTTP-failure status reaches ` + "`execute_shell_command`" + `.

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
		Description: "How to use the MCP bridge, tool discovery (get_api_spec), attached references (read_skill), and workflow command guidance in this session.",
		Content:     body,
		Source:      llmtypes.SkillSource{Origin: "builtin"},
	}
}

func buildConfigurationAccessGuidance(mode string) string {
	var parts []string

	// The prohibition is a guardrail and holds in every mode. The tool
	// recommendation must not: `llm-provider-config`'s registry entry decides
	// which modes manage provider configuration, and only those modes register
	// list_published_llms/list_provider_models/test_llm/save_published_llm/
	// set_provider_auth. Naming them unconditionally told `run`-mode sessions —
	// every scheduled workflow run and every Pulse pass — to call tools that
	// mode never registers, producing `tools_unavailable` and sending agents off
	// to guess provider names instead.
	parts = append(parts, "LLM/provider configuration is tool-managed. Do not read or edit `config/` files with shell or file tools.")
	if modeAllowedIn("llm-provider-config", mode, referenceKinds) {
		parts = append(parts, "For published chat models and provider auth, use `list_published_llms`, `list_provider_models`, `test_llm`, `save_published_llm`, and `set_provider_auth` as appropriate; load `read_skill(skills=[{\"name\":\"builder-reference\",\"path\":\"references/llm-provider-config.md\"}])` before publishing or changing provider auth.")
	}

	if modeAllowedIn("llm-selection", mode, referenceKinds) {
		parts = append(parts, "For workflow execution tiers and per-step model choices, use `get_llm_config` and `set_workflow_llm_config`; load `read_skill(skills=[{\"name\":\"builder-reference\",\"path\":\"references/llm-selection.md\"}])` before changing workflow execution models.")
	}
	if modeAllowedIn("workspace-media-tools", mode, referenceKinds) {
		parts = append(parts, "For media/search provider tools, load `read_skill(skills=[{\"name\":\"builder-reference\",\"path\":\"references/workspace-media-tools.md\"}])`.")
	}

	return strings.Join(parts, " ")
}

// RenderSystemDoc renders the named reference doc with no caller context,
// stripping the path-discipline preamble. Intended for production code that
// needs system-doc content inline (for example deterministic server-side
// assembly that is not itself an agent turn). Agent-facing access uses the
// attached builder-reference skill through mcpagent's read_skill tool.
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
