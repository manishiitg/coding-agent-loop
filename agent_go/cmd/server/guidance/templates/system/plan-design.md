## PLAN DESIGN — From Requirements to Steps (DESIGN phase)

When a user describes what they want to automate, design and create the best-practice workflow structure using the information available. Do not ask broad planning questions or wait for confirmation before adding steps unless the missing answer would materially change behavior, safety, credentials, scheduling, external side effects, or irreversible actions. When you make a reasonable assumption, state it briefly and proceed.

### Step 1: Identify Durable Workflow Boundaries

Modern agents can handle long context and many tool calls. Do not make one workflow step per tool call, screen action, file read, or small transformation. A step is a durable workflow boundary: it has an output contract, validation gate, retry behavior, and persistent-store responsibilities.

Start with **one large `message_sequence` for each coherent shared-context span**. It should complete that span's agentic outcome, then prove and repair it without discarding conversation context. Strengthen the step with machine-checkable evidence/provenance fields, a top-level `validation_schema`, and focused verify-and-repair turns before adding another workflow step. A reviewer must be able to name the context or execution boundary that the extra step protects.

Split into separate steps only when a boundary buys something concrete that cannot live safely inside that large step:
- Distinct durable output or downstream contract
- Independent validation/retry gate that must succeed, fail, or rerun separately
- Independent retry/failure domain
- Different tool, security, credential, or runtime context
- Downstream consumer needs the intermediate artifact
- Different persistent-store contract (learnings HOW, KB WHAT, database structured state, report data)
- Human decision, approval, or routing checkpoint
- Deterministic execution versus agentic judgment: fixed API/SDK calls, CLI commands, data fetching, parsing, normalization, and mechanical writes belong in scripted regular steps; reasoning over their results belongs in a message sequence

Combine actions into one step when they share one objective and output contract, use the same tools/security context, fail and retry together, produce only scratch intermediates, and one validation schema can verify the result.

**Rule of thumb**: one large validated step per shared-context span unless a hard boundary proves otherwise. Many tool calls, internal phases, proof checks, and corrections can belong in that step; contexts that should not be shared or genuinely independent durable contracts may use separate large sequences.

### Step 2: Choose the Right Step Type

| Scenario | Step Type | Why |
|----------|-----------|-----|
| Agent does work, then verifies/fixes it against the success criteria (the common case) | **Message Sequence** (default) | One shared conversation: do → verify → fix, as ordered items. Keeps the agent's full working context instead of handoff artifacts between regular steps |
| One coherent agentic outcome with **no** same-context verify-and-fix follow-up | **Regular** | One agent, one output, one final gate; the outcome may still require many routine actions and tool calls |
| Runtime needs independently delegated tasks with isolated context, tools, retries, or parallel progress | **Todo Task** (sub-workflow/pipeline) with sub-agents | Delegation itself creates value; a known checklist in one shared context stays in one message sequence |
| Need to branch based on prior step output or context | **Routing** | Supported branch primitive — reads `route_selection.json` and picks a route |
| Need user input before proceeding | **Human Input** | Blocks until user responds |
| User already told the builder which fixed branch to run | **Routing** | The builder/caller passes `route_selections` to `run_workflow` / `run_full_workflow`; do not add a `human_input` step just to ask the same choice again. |
| The running workflow must ask a human before it can continue | **Human Input** | Use only when the answer is not already known at launch. If the answer branches, use `option_routes` for a small in-run menu or feed a later deterministic router. |
| Utility/debug tool available but not auto-run | **Orphan** (is_orphan: true) | Not in main flow; manual execution from workshop only |

**Default to one large Message Sequence per shared context.** Modern agents do a lot in a single long-running turn, so begin with one shared-context conversation for each coherent agentic span: `[do the whole span] → [re-open source evidence and prove every criterion] → [repair every gap and double-check the final result]`. Improve its description, proof/evidence contract, top-level `validation_schema`, and verify/repair turns before considering more steps. Multiple large sequences are correct when their contexts should not be shared—for example because they have different credentials/security exposure, independent outputs/retries, clean-room independence, human or routing boundaries, or unrelated context that would distract or contaminate the next agent. The builder must decide this from the workflow semantics and state the boundary. Use **Regular** when one coherent agentic outcome genuinely needs no same-context verify-and-fix follow-up, or when deterministic work must be scripted. Use **Todo Task** only when independent delegation itself is required, and **Routing** only for real fixed branch choices.

**Deterministic fetcher → agentic processor is the default data architecture.** Put fixed API/SDK requests, CLI commands, pagination with known rules, parsing, normalization, and mechanical database/file writes in one or a few `regular` steps declared `scripted`. Batch related calls when they share credentials, retry policy, source, and output contract; do not create one step per endpoint or command. Give each fetcher an explicit authoritative output (prefer canonical rows in `db/db.sqlite`, otherwise a compact JSON artifact), provenance/freshness fields, fail-closed error handling, idempotency where relevant, and deterministic validation. Then let one large `message_sequence` read those persisted results and perform the judgment-heavy analysis, synthesis, critique, and repair. Do not spend an LLM turn reissuing a known request or parsing a stable response shape.

If selecting the next call genuinely requires live judgment, keep the decision agentic but isolate deterministic execution: the message sequence produces an explicit request/specification, a scripted regular step executes it, and a later message sequence interprets the result. Browser/UI navigation remains agentic unless it is genuinely stable and the user explicitly wants a scripted browser path. Human approval still precedes consequential side effects even when the approved API/CLI action itself is deterministic.

**For recurring workflow shapes** (Phase Router, Scoped Investigation, coherent scripted pipeline, Fan-out & Consolidate, In-Context Verification Gate, Pre-flight Probe, Human Checkpoint, Critique Loop, Persistence Tail, SQL-driven foreach), call `get_reference_doc(kind="workflow-patterns")` — load when starting a new plan or restructuring an existing one.

**Whenever you change an existing plan** — add / remove / reorder a step, or change a step's output contract, db writes, or behavior — run `get_reference_doc(kind="plan-change-impact")` before treating it as done and reconcile the blast radius (downstream steps, evals, report dashboard, db, learnings, KB). A step change ripples into everything that reads it; don't leave silent breakage behind.

### Step 3: Design Context Flow

Every step reads from prior steps and writes for downstream steps:
- **description** is executable for agentic steps. For `regular`, it is the main step prompt. For `message_sequence`, it is the opening instruction prepended to the first item. For `todo_task`, it is the orchestrator's first turn. For `routing`, leave it empty because routing never runs an agent.
- **context_dependencies**: Files from prior steps this step needs (e.g., ["login_status.json"])
- **context_output**: The file this step produces (e.g., "extracted_data.json")
- **Flow must be forward-only** — no circular dependencies
- Use JSON for structured data consumed by downstream steps. Keep output files < 100KB. For a final human-readable report or analysis, **prefer `.md`** — markdown renders richly in the file viewer (headings, tables, lists) and, unlike HTML, gets clickable workspace file links; it's also simpler and more robust to author. Reach for a standalone `.html` file only when you genuinely need rich/branded layout markdown can't express — and for a real dashboard, use the report system (`reports/report_plan.json`) rather than hand-rolling HTML. For prose appended into learnings/KB, use Markdown.

### Step 4: When to Use Orchestrator (Sub-Workflow / Pipeline) with Sub-Agents

**Note:** Users may refer to todo_task steps as "Orchestrators", "orchestrators", "sub-workflows", or "pipelines", and to the routes/sub-agent steps within them as "sub-agents". These are all the same concept — the internal type name is todo_task.

**Use todo_task only when the step must manage independently delegated tasks**, especially when:
- Runtime evidence determines which or how many independent tasks must run
- Different tasks need **different tools or servers** (e.g., one sub-agent uses browser, another uses API)
- Tasks benefit from **independent learning** — each sub-agent accumulates its own patterns
- You need **progress tracking** — todo_task shows which tasks are done, pending, failed

**Create predefined sub-agents (routes)** for tasks that are:
- **Predictable** — same pattern every run, even if inputs change
- **Self-contained** — clear inputs/outputs, can be validated independently
- **Worth optimizing** — complex enough that accumulated learnings improve reliability

Route sub-agents can be `regular` for stateless one-off work, `message_sequence` for a stateful specialist conversation, or `todo_task` for one nested orchestration layer.

Use a `message_sequence` route when the parent orchestrator should be able to call the same specialist repeatedly with memory. Normal repeated calls reuse the route session and send the new instructions as the re-entry user message. Use `message_sequence_restart=true` only when the orchestrator intentionally needs a clean rerun that archives the existing route session and replays the configured queue from the beginning.

**Use the generic agent** (no predefined route) for tasks that are:
- **Dynamic** — unpredictable at design time
- **Trivial** — too simple for a dedicated sub-agent

**Non-example**: A known list such as "process the income, deductions, and credits pages" is not enough to justify three sub-agents. Keep it in one large message sequence unless those pages require independent tools, retry domains, outputs, or runtime delegation.

### Step 5: When to Use Message Sequence

`message_sequence` is the **default** step type (see Step 2). Use it whenever ordered agent turns share the same working context and build on each other, and the boundary between turns is not a durable workflow boundary — which is most agentic work, since the natural shape is **do the task, then verify and fix it in follow-up items of the same conversation**. This is better than several regular steps that re-read the same files, need each other's transient reasoning, and produce only one final output.

- **Give the work turn the complete outcome.** Let the agent perform all routine sub-actions, tool calls, and internal checks needed for that outcome in one substantial turn; do not create one item per checklist line, source, or tool call.
- **Consume deterministic evidence; do not fetch it conversationally.** Fixed API/SDK/CLI acquisition and stable parsing belong in an upstream scripted regular step. The sequence reads the resulting DB rows/artifacts and spends its turns on judgment, synthesis, semantic validation, and repair.
- **Make verification a follow-up item, not a separate step.** A typical sequence is `[complete the outcome] → [re-open the evidence and verify every success criterion; identify anything unsupported or incomplete] → [repair every verified gap and recheck]`. The verifier turn has the full context of what was just done, so it catches more than a fresh regular step reconstructing from artifacts.
- Add a separate sequence item only when it changes the agent's job: validation or critique, correction from observed evidence, a real intermediate gate, new external input, or a deliberate role/perspective change. A sequence of tiny routine instructions recreates the same fragmentation as too many regular steps.
- Prefer it when turns share the same inputs, tools, credentials, runtime, and security context; fail/retry together; and can be validated as one unit.
- Keep separate regular steps only when an item has its own durable artifact, independently rerunnable validation/failure domain, tool/security context, or downstream consumer. A desire to double-check the same final output is not a separate-step reason.
- Add learning / knowledgebase / db update items as user messages at the exact point they should happen.
- Add explicit reference-check, hallucination-check, critique, or self-validation items when reliability needs it.
- Plain items inherit the step-level KB, DB, and learnings permissions, just like regular steps. Use a non-empty `write_access` object or `kind` only to narrow a particular turn; an item can never escalate beyond the step configuration. See `get_reference_doc(kind="message-sequence")`.
- Deterministic execution is never a sequence item. Put API/CLI/SDK calls, data fetching, parsing, transforms, and mechanical writes in standalone scripted regular steps with explicit inputs, outputs, and validation.
- As a todo_task predefined route, a message_sequence behaves like a reusable specialist sub-agent: reuse the same route for critique, test feedback, validation feedback, or follow-up work that should keep prior context; restart only when the prior conversation is stale, wrong, or contaminated.
- For row/item iteration, use a `foreach` item inside message_sequence when one shared conversation should process every row. Use todo_task when each item needs independent sub-agent delegation.

### Step 6: When to Use Routing (brief)

Use `routing` when the next step must be **exactly one of N mutually exclusive paths** (e.g., "did login succeed, hit MFA, or fail?"). Routing is deterministic: a caller or prior step must provide `route_selection.json` (or `route_selections`) with the selected route. For running every sub-task, use todo_task. For a linear conversation, use message_sequence.

Routing has one mode: leave `description` and `context_output` empty, read an existing route file/source, then switch. The common case is that the builder/caller selects the fixed branch from the user's request with `route_selections`. If an agent/probe/judgment is needed, add a prior `regular` step that writes `route_selection.json` and have the routing step consume it with `route_source_file` or `context_dependencies: ["route_selection.json"]`. Each `routes` entry needs a stable `route_id`, a `condition` explaining when that route should be selected, and a `next_step_id` that points to another step in the plan (routing routes do **not** define inline sub-agents — they branch to existing steps); set `default_route_id` only as a missing-file fallback.

For full route structure, file contract, and anti-patterns, call `get_reference_doc(kind="routing")` — load before designing or repairing any routing step.

### Step 7: Design Validation

Every step MUST have a **validation_schema** — the automated gate that pass/fails the step:
- Check file existence, required fields, value types, patterns, and lengths
- Include enough checks that stale/leftover files from previous runs can't pass
- For todo_task steps: validation passing IS the completion signal
- For message_sequence steps: the runtime automatically runs the step-level schema after the final work turn and repairs failures in the same conversation. Add explicit prevalidation items only for intermediate gates.

Step-level `success_criteria` is deprecated. Rely on a strong `description` plus `validation_schema` instead.

### Step 8: Think About Failure Modes

- If a step might fail due to external factors (login, API), add clear error handling in the description
- If a step's output needs semantic validation (not just structural), add proof, verification, and repair items inside its `message_sequence` — use a separate validation sequence only when the context must be isolated for clean-room independence, different permissions/tools, or its own rerunnable artifact/failure domain
- If a step is flaky, first add explicit retry/polling and proof checks inside the step; split the unstable part only when it needs independent retries or isolation

### Design Anti-Patterns to Avoid

- **Monster boundaries**: A single step owns unrelated durable outputs, validation gates, failure domains, or persistent stores — split at those boundaries. Many tool calls alone are not a reason to split.
- **Trivial steps**: A step that just reads a file and passes it through — merge with the consumer
- **Over-splitting same-context turns**: Several regular steps mostly reread the same context and depend on each other's transient reasoning. Collapse into one `message_sequence`; verification, critique, double-checking, and repair belong inside it unless a check truly needs an independent durable artifact, retry domain, or tool/security context.
- **Missing validation**: No validation_schema means no automated quality gate
- **Vague descriptions**: "Process the data appropriately" — be specific about WHAT, HOW, and WHERE
- **Over-sequencing**: Steps that don't depend on each other can potentially run in parallel via independent step groups
- **Inline sub-tasks in todo_task**: If you're writing detailed instructions for a specific task inside the orchestrator description, that task should be a sub-agent route instead

### Step Types Reference

- **Message Sequence** (type: "message_sequence") — **the default for conversational work**: a single-agent ordered conversation with `items`. Do the whole coherent job, then **verify and fix it in focused follow-up user_message items** in the same context. Supports foreach turns and intermediate prevalidation gates. Its top-level validation_schema is automatically enforced as the final gate with same-conversation repair retries. Deterministic code is always a separate regular scripted step with explicit file dependencies and outputs. As a top-level step the queue runs once; as a todo_task route it can be re-entered during the same workflow run and receive new instructions without replaying the queue.
- **Regular** (type: "regular"): one coherent agentic outcome with no same-context verify-and-fix follow-up. It may perform many routine actions and tool calls before producing its durable output and passing its final gate.
- **Orchestrator / Todo Task / Sub-Workflow** (type: "todo_task"): Also called "orchestrator" by users. Manages a dynamic todo list. Has a **todo_task_step** (orchestrator) and **predefined_routes**. Each route can either define an inline **sub_agent_step** or reuse a plan-local orphan definition via **orphan_step_ref**. Route sub-agents default to **message_sequence** (specialist conversation with re-entry/restart memory), can be a **regular** step for stateless one-off work, and can be another **todo_task** (nested orchestrator) when that route needs its own nested orchestration. Only one nested todo_task layer is allowed: top-level todo_task -> nested todo_task is valid, but a nested todo_task must not contain another nested todo_task.
- **Routing** (type: "routing"): N-way deterministic branching. Reads `route_selection.json` (or caller `route_selections`) and picks exactly one **routes[]** entry. Each route has **route_id**, **condition**, and **next_step_id** (pointer to an existing step). Optional **default_route_id** is a missing-file fallback. Optional **route_source_file** points at a prior step's route file.
- **Human Input** (type: "human_input"): Asks a question to the user and blocks until response. Supports: 'text', 'yesno', 'multiple_choice'. Can route based on response.
- **Orphan** (is_orphan: true): Not part of the main execution flow. Orphan steps are plan-local reusable definitions and manual utility agents. Use them for data checks, environment validation, one-off investigations, or shared sub-agent definitions that multiple orchestrators in the same plan may reuse. Reuse is explicit: an orphan step must declare `shared_with.orchestrator_ids`, and a todo_task route must point to it with `orphan_step_ref`. Do not assume every orphan step is shared with every orchestrator.

### Inner Steps

Inner steps live inside todo_task `predefined_routes[].sub_agent_step` (and nested todo_task containers). They have their own step IDs and can be individually executed and configured via **execute_step**, **update_step_config** using the inner step ID. Routing steps do not have inner steps — their `routes[].next_step_id` points to existing steps elsewhere in the plan.

### Reusable Orphan Route Pattern

When a todo_task route should reuse an orphan step:
- Put the reusable step definition in `orphan_steps[]`.
- On that orphan step, set `shared_with.orchestrator_ids` to the IDs of the todo_task orchestrators allowed to reuse it.
- On the route, set `orphan_step_ref` to the orphan step ID instead of embedding an inline `sub_agent_step`.
- Use inline `sub_agent_step` only when the route needs its own dedicated definition.
