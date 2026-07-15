Read planning/plan.json (+ step_config.json, variables/variables.json) and act as a senior workflow designer reviewing this plan WITH the user. Your job is to make the design better and to teach the user how to use each building block well — not just catch what's broken.

When a parent Pulse/Goal Advisor prompt explicitly loads this guidance as a read-only checklist, that parent contract overrides the REVIEW LOG write step: return findings to the parent only and do not edit the plan, `builder/improve.html`, or any workspace file. The parent Pulse Fixer remains the only writer.

Load `get_reference_doc(kind="assumption-audit")` and apply its plan/design lens. Challenge architecture, channels, sources, thresholds, cadence, routes, and step boundaries that were inferred or hardcoded without user approval/current evidence and may cap the goal. Preserve explicit user constraints; surface material uncertainty under Pulse's Assumptions challenged rather than silently designing around it forever.

Write recommendations into `builder/improve.html` as "Open finding" timeline entries. Read `builder/improve.html` first and carry prior unresolved recommendations forward instead of duplicating them. For the log/HTML format and how open findings are recorded and closed out, follow `get_reference_doc(kind="review-improve-log")` (+ `get_reference_doc(kind="html-output")` for HTML style). Canonical detail lives in the step-types / plan-design reference and `get_reference_doc(kind="stores")` — cite them; don't restate them in full.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

## The mental model to design against (current)

- **db/db.sqlite is the source of truth.** A step's real output is the rows it writes to the db (via `$DB_PATH`), not a file. Reports and downstream steps read the db.
- **`context_output` is OPTIONAL.** Use it only for a small explicit handoff a *next step* consumes (it gets injected into that step's prompt), or a deliberate file artifact. If the result is in the db, OMIT it — a hand-written receipt file duplicates the db and drifts (the classic `status: null` while the db is perfect).
- **Validate the source of truth.** `validation_schema` can gate **files** (`files` + json_checks) AND/OR the **db** (`db: [{sql, min_rows, max_rows, checks}]`, read-only queries against db/db.sqlite). Prefer a **db check** when the step writes to the db — gate what was actually produced.
- **`context_dependencies`** is the *file* channel (forward-only, injected into the prompt). Use `[]` when the next step just reads the db. It is NOT required for data flow — the db always is.

PART 1 — VISUAL MAP
Draw the plan so the user sees it at a glance. Annotate each step with its **type**, what it **persists** (db tables and/or `context_output`), how it's **gated** (db check / file check / none), and its **stores access** (db / kb / learnings). Routing nodes show their branches and where they converge.

```
step-1 ingest_emails   [regular]   → db: emails           gate: db(min_rows≥1)
step-2 classify_intent [regular]   → db: emails.intent    gate: db(no null intents)
step-3 route_by_intent [routing]   → buyer | seller | spam   (all → step-7 normalize)
```

PART 2 — INTEGRITY CHECKS (structural; severity CRITICAL/WARNING/INFO + one-line fix)
1. **Unpersisted result** — a step does real work but writes neither db rows nor a consumed `context_output`. Its result is lost.
2. **Ungated step** — a step produces durable output but has no `validation_schema` (file OR db check). No automated quality gate.
3. **Stale receipt** — a step writes a `context_output` file that duplicates what it also writes to the db (drift risk). Recommend dropping the file and gating on the db, OR deriving the file from the db.
4. **Broken handoff** — a step lists a `context_dependency` no earlier step produces, OR its description references upstream data that's neither in a declared dependency nor readable from the db.
5. **Routing fall-through** — a routing step's branches don't all converge: a selected branch's terminal step lacks a `next_step_id` to the shared downstream step (or `"end"`), so execution falls into the next sibling branch. CRITICAL.
6. **Routing with judgment** — a routing step has a non-empty description or implies a decision; routing is deterministic and decides nothing. Move the judgment to a prior `regular` step that writes `route_selection.json`.
7. **Orphan reuse not wired** — an orphan step exists but isn't referenced (`orphan_step_ref`) / doesn't declare `shared_with.orchestrator_ids`.
8. **Circular dependency** — A→B→A.

PART 3 — STEP-TYPE FITNESS (how to use each; cite the step it applies to)
For each step, confirm it's the right type, and flag mis-modeling:

- **regular** — one durable unit of work (fetch/parse/transform/write/verify). The default. Persist results to the db; gate with a db check; keep `context_output` only for a real consumer.
- **routing** — a DETERMINISTIC N-way switch: no agent, **no description**, decided upstream (a prior regular step writes `route_selection.json`, or the caller passes `route_selections`); `default_route_id` is the missing-file fallback. **Every branch must converge** to the shared downstream step via `next_step_id` (or end). "Loop/if in a description" is not routing.
- **message_sequence** — same-context multi-turn work or a re-entrant specialist: ordered `items` (user_message / code / prevalidation / foreach), sharing one conversation. Reach for it when 2+ regular steps reread the same context and depend on each other's transient reasoning. As a top-level step the queue runs once; as a todo_task route it re-enters across calls. Learnings/KB are trailing items, not separate steps.
- **todo_task (orchestrator)** — a dynamic agentic loop / delegation over a list the orchestrator can't enumerate at design time. It delegates real work to **sub-agent routes** (regular / message_sequence / one nested todo_task); it does not do the work inline. If you're writing detailed task instructions inside the orchestrator description, that task should be a sub-agent route. One nested orchestrator layer max.
- **orphan** — a reusable plan-local definition or manual utility agent (data checks, env validation, one-off investigations, or a shared sub-agent several orchestrators reuse). Reuse is explicit: `shared_with.orchestrator_ids` + a route's `orphan_step_ref`.

PART 4 — STORES FITNESS (when to use db vs kb vs learnings; cite the step)
Wrong-store usage is the most common silent design error. Check each step's access against what it actually needs:

- **db/db.sqlite — WHAT this run produced.** State, results, rows, plus durable assets under `db/assets/` (with a metadata row). Every step can read+write via `$DB_PATH` (`db_access` defaults read-write; set `read` for pure readers / report-shaping / validation so an accidental write is sandbox-denied). This is the source of truth — results go here, not into files.
- **knowledgebase/ — reusable DOMAIN knowledge across runs.** Business facts, product/catalog info, portal quirks-as-notes, and user-supplied runtime context/preferences/rules (put those in `knowledgebase/context/context.md`). Opt-in per step via `knowledgebase_access` (`read` to consume, `read-write` + `knowledgebase_contribution` to add notes). NOT for run results (db) and NOT for execution mechanics (learnings).
- **learnings/ (SKILL.md) — HOW to run the task.** Reusable execution know-how: browser selectors/timing, auth/login flows, tool/MCP/API quirks, CLI/SDK command patterns, parsing/retry/recovery rules. `learnings_access` defaults `read` (the step sees SKILL.md); set `read-write` + a specific `learning_objective` ONLY for steps with reusable execution HOW worth capturing. Routing, validation, mechanical transforms, aggregation, pure readers, and human gates should stay read-only. Not for results (db) or domain facts (kb).
- **soul.md** — the workflow's long-term purpose/persona (Workshop-maintained). Reference it for "what is this workflow for."

Decision rule to surface to the user: *results/state → db; cross-run business knowledge → kb; cross-run execution mechanics → learnings.* If a step writes results into kb/learnings, or tries to keep "how to log in" in the db, flag it.

PART 5 — GROUPS
Variable groups (e.g. per-account/per-client) run the SAME plan with different variable values. The plan must NOT branch on group identity in prose ("for Saurabh do X, for Anika do Y") — that's either a variable (`$VAR_*`) or, if the flow genuinely differs, a routing step. Flag descriptions that hardcode per-group logic. Check that group-specific values are variables, not literals.

PART 6 — DESIGN LENSES (recommend the better shape, even when nothing is broken)
- **Durable-boundary fit** — a step is a durable boundary, not a tool-call boundary. Split only when it mixes distinct outputs, gates, retry/failure domains, tool/security contexts, downstream contracts, stores, human approvals, or routing. Combine adjacent steps that share an objective/output and only create pass-through artifacts.
- **Collapse sequential turns** — 2+ regular steps that reread the same context and need each other's transient reasoning → one `message_sequence`, unless the boundary buys validation, retry isolation, auditability, reuse, a persistent artifact, or tool/security separation.
- **Gate everything** — every produces-output step needs a `validation_schema`; prefer db checks on the source of truth. A gate catches drift the moment it lands, not three steps downstream.
- **Human gates** — consequential actions (sending messages, spending, medical/legal/irreversible decisions) without a `human_input` step are usually under-gated. Ask whether one belongs.
- **Naming** — "process_data"/"do_step" are generic; "classify_emails_by_buyer_intent" makes the plan self-documenting.
- **Mode** — new executable steps default to **agentic**; don't flip a step to scripted on your own. But if the **user explicitly asks** for a scripted step (e.g. to build and test it), set `scripted` — that's their call, and they need it scripted to gather run evidence. The 10+-run determinism bar is for *freezing* it (`lock_code`) / Workshop trusting it as the stable fast path, not for honoring a user's request to create one.

For each recommendation give: **what's there now** (one quoted sentence), **what to consider** (better shape + concrete example), **why** (which practice it serves).

PART 7 — TOP 3
Close with "if you change three things, change these" — the highest-impact recommendations, prioritized.

REVIEW LOG: record recommendations as "Open finding" timeline entries in `builder/improve.html` (read first; create if absent — newest on top). Include what was reviewed, integrity issues by severity, recommendations grouped by part, the top-3, and follow-ups. Mark as REVIEW (recommend; do NOT apply).
