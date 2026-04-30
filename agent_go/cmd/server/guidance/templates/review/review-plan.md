Critical audit of the workflow plan — the comprehensive review. Where /design-flow asks "what would a designer make better," this asks "what's wrong, weak, risky, or unjustified, and which steps need attention." Findings go to builder/review.md as recommendations; nothing is applied here.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

The audit has three phases. Run each in order. Skip Phase 3's orchestrator block when the workflow has no todo_task steps.

PHASE 1 — STRUCTURAL ANALYSIS

1. Call review_plan() — the server-side review tool. It analyzes plan structure: step boundaries, step types, execution modes, context flow integrity, validation coverage, portability, and whether choices are justified by the objective + success_criteria from soul.md.
2. Read its output carefully. Group findings by severity: CRITICAL (broken structure, missing required fields, contradictions vs soul.md), WARNING (questionable choices that need defense), INFO (style/minor).
3. Compare against soul.md's objective + success_criteria explicitly: for each weak structural choice, name which criterion it fails or under-serves.

PHASE 2 — PER-STEP DESCRIPTION AUDIT

For every step in plan.json, read the step's description and (if present) the step's SKILL.md / learnings. Apply each lens; skip a lens when it doesn't fire.

LENS A — Description vs Skill Confusion
- **Description contains runtime learnings**: the description should be an *instruction* (what to do), not a *retrospective* (what worked last time). "Use batch mode because single inserts timeout", "avoid X which caused failures", or specific tool parameter values discovered at runtime belong in SKILL.md, not the description.
- **Skill contains task instructions**: SKILL.md should capture *reusable patterns and pitfalls discovered during execution*, not restate what the step is supposed to do. If the skill reads like a task description, it's confused.
- **Duplication**: same guidance appearing in both description and skill — pick one home.
- **Description defers to skill**: phrases like "follow the skill" or "see learnings" instead of giving clear instructions.

LENS B — Hardcoded Values
- **Hardcoded paths**: absolute paths like `/app/workspace-docs/...`, `/Users/...`, `/home/...`, or specific local paths. Should use workspace-relative or workspace-rooted paths instead.
- **Hardcoded run/iteration paths**: references to `runs/iteration-0/...`, `execution/step-3/...`, or hardcoded group names like `group-1`. These break across runs and groups — the orchestrator resolves these via context_dependencies at runtime.
- **Hardcoded credentials/secrets**: API keys, tokens, passwords, auth headers. Should reference `SECRET_*` environment variables.
- **Hardcoded IDs/URLs/user-specific values**: spreadsheet IDs, database names, API endpoints, user IDs, email addresses, phone numbers, account numbers. Should use variable placeholders (e.g., `{USER_ID}`, `{SHEET_ID}`, `{EMAIL}`) in descriptions, with actual values in `variables.json` / variable groups.

LENS C — Browser Anti-Patterns (only for steps that use playwright/browser/agent_browser)
- **Prescribes browser_evaluate for interactions**: description tells the LLM to use `browser_evaluate`/`eval` to click, fill, or navigate. Should say "take a snapshot, find the element, click/type using its ref" instead.
- **Prescribes CSS selectors**: patterns like `browser_click({'selector': '...'})` or `browser_type({'selector': '...'})`. Use ref-based interaction from snapshots.
- **Prescribes hardcoded element references**: specific DOM selectors, iframe indices, or element IDs that may change. Describe intent ("find the password field", "click the login button") and let the LLM discover elements via snapshot.
- **Over-specifies implementation**: description dictates exact tool calls and parameters instead of describing WHAT to accomplish. For learn_code steps, the description should focus on the goal and let the LLM figure out the implementation using `get_api_spec` and snapshots.

LENS D — Missing Pre-Validation Schema
- **No validation_schema**: every step that produces a context_output should have a `validation_schema` defined. Without it, there's no automated quality gate — a step can produce garbage and downstream steps will blindly consume it. Check that `validation_schema` exists, has file checks matching the context_output filename, and includes meaningful `json_checks` (not just `must_exist`).

PHASE 3 — TODO_TASK ORCHESTRATOR AUDIT (skip if no todo_task steps)

For every step where `step_type == "todo_task"`, read its description and ALL its `predefined_routes` (sub-agent descriptions). Apply each lens.

LENS E — Orchestrator Description Quality
- **Missing objective/intent**: the orchestrator description must clearly state WHAT we are trying to achieve — the overall goal. Without this, the orchestrator can't make intelligent decisions when things go wrong or unexpected situations arise. A good orchestrator description answers: "Why do these sub-agents exist together? What outcome are we after?"
- **Reduced to a sequencer**: if the description is just "run route A, then route B, then route C" or a fixed checklist, the orchestrator is being wasted. It's a capable LLM — its description should enable reasoning, not just list steps. If all it does is follow a fixed order, these should be regular steps in sequence instead.
- **No edge case / failure guidance**: the description should explain how to handle failures, retries, partial results, missing data, or unexpected sub-agent states. The orchestrator's core value is making decisions when things don't go as planned.
- **No routing criteria**: the description doesn't explain WHEN or WHY to pick each route. The orchestrator needs to know what conditions, inputs, or states map to which sub-agent.

LENS F — Orchestrator vs Sub-Agent Boundary
- **Inline execution logic**: detailed task instructions for a specific sub-task written inside the orchestrator description. Each distinct task should be its own route with its own description, learnings, and tools. Orchestrator dispatches; sub-agents execute.
- **Duplicates sub-agent descriptions**: orchestrator restates what sub-agents already describe. Orchestrator should focus on coordination and decision-making.
- **Sub-agent descriptions too vague**: route descriptions that are too thin because all the detail is in the orchestrator. Each sub-agent should be self-contained — a junior agent reading only its own description should know exactly what to do.

LENS G — Sub-Agent Hardcoded Values
- Same hardcoded-value checks from Lens B applied to sub-agent route descriptions (paths, run/iteration paths, credentials, IDs/URLs).

OUTPUT FORMAT

For each step, produce a per-step report:

```
### step-id: <name> (type: <regular|todo_task|routing|human_input|orphan>)
**Description summary:** <one-line>
**Lens A — Description vs Skill:** <findings or "clean">
**Lens B — Hardcoded:** <findings or "clean">
**Lens C — Browser:** <findings or "n/a (no browser capability)" or "clean">
**Lens D — Validation:** <findings or "clean">
**Lens E — Orchestrator description:** <findings, or "n/a (not a todo_task)" or "clean">
**Lens F — Orchestrator/sub-agent boundary:** <findings or "n/a" or "clean">
**Lens G — Sub-agent hardcoded:** <findings or "n/a" or "clean">
**Severity verdict:** CRITICAL / WARNING / INFO / clean
**Top recommendation:** <single highest-value fix>
```

Then a cross-step summary:

- **Phase 1 structural findings** (from review_plan tool): list by severity.
- **Steps with description issues** (Lens A/B/C/D): per-step, which lenses fired.
- **Todo_task steps with orchestrator issues** (Lens E/F/G): per-step, which lenses fired.
- **Steps that look clean across all phases.**
- **Top 5 issues to fix first** (highest-impact across all phases).

REVIEW LOG: append a dated entry to builder/review.md (read it first if it exists, create it if it does not). Include: what was reviewed, the structural findings (Phase 1), the description findings grouped by lens (Phase 2), the orchestrator findings (Phase 3), the cross-step summary, the top-5 list, items flagged for follow-up. Mark this as REVIEW (recommend; do NOT apply — fixes go through optimizer-mode tools or the experiment loop).

**Finding IDs.** Every distinct finding (whether from Phase 1, 2, or 3) gets a stable id of the form `F-YYYY-MM-DD-NNN` — today's date plus a 3-digit sequence that restarts at `001` per day. Scan the file for today's highest existing sequence and continue from there; never reuse an id. Format each finding line as `- [F-YYYY-MM-DD-NNN] <severity>: <step-id or "structural"> — <finding>` so the close-out edits performed later by `/improve-*` (or by chat-driven fixes) can target the exact entry.
