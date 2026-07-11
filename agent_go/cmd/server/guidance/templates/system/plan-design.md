## PLAN DESIGN — From Requirements to Steps (DESIGN phase)

When a user describes what they want to automate, design and create the best-practice workflow structure using the information available. Do not ask broad planning questions or wait for confirmation before adding steps unless the missing answer would materially change behavior, safety, credentials, scheduling, external side effects, or irreversible actions. When you make a reasonable assumption, state it briefly and proceed.

### Step 1: Identify Durable Workflow Boundaries

Modern agents can handle long context and many tool calls. Do not make one workflow step per tool call, screen action, file read, or small transformation. A step is a durable workflow boundary: it has an output contract, validation gate, retry behavior, and persistent-store responsibilities.

Split into separate steps when a boundary buys something concrete:
- Distinct durable output or downstream contract
- Independent validation gate
- Independent retry/failure domain
- Different tool, security, credential, or runtime context
- Downstream consumer needs the intermediate artifact
- Different persistent-store contract (learnings HOW, KB WHAT, database structured state, report data)
- Human decision, approval, or routing checkpoint

Combine actions into one step when they share one objective and output contract, use the same tools/security context, fail and retry together, produce only scratch intermediates, and one validation schema can verify the result.

**Rule of thumb**: Many tool calls can belong in one step. Many durable contracts should not.

### Step 2: Choose the Right Step Type

| Scenario | Step Type | Why |
|----------|-----------|-----|
| Agent does work, then verifies/fixes it against the success criteria (the common case) | **Message Sequence** (default) | One shared conversation: do → verify → fix, as ordered items. Keeps the agent's full working context instead of handoff artifacts between regular steps |
| One atomic action with **no** verify-and-fix follow-up | **Regular** | Simplest type — one agent, one output; use when there's nothing to check in-context afterward |
| Task has multiple known sub-tasks that repeat | **Todo Task** (sub-workflow/pipeline) with sub-agents | Each sub-task gets its own learning, validation, and tools |
| Need to branch based on prior step output or context | **Routing** | Supported branch primitive — reads `route_selection.json` and picks a route |
| Need user input before proceeding | **Human Input** | Blocks until user responds |
| User already told the builder which fixed branch to run | **Routing** | The builder/caller passes `route_selections` to `run_workflow` / `run_full_workflow`; do not add a `human_input` step just to ask the same choice again. |
| The running workflow must ask a human before it can continue | **Human Input** | Use only when the answer is not already known at launch. If the answer branches, use `option_routes` for a small in-run menu or feed a later deterministic router. |
| Utility/debug tool available but not auto-run | **Orphan** (is_orphan: true) | Not in main flow; manual execution from workshop only |

**Default to Message Sequence.** Modern agents do a lot in a single long-running turn, so the strongest default is one shared-context conversation that does the work and then verifies it in follow-up items (`[do the task] → [verify against the success criteria] → [fix what verification caught]`). Use **Regular** only for a single atomic action with no verify-and-fix follow-up, or when a turn needs its own durable artifact, hard validation gate, retry/failure domain, or different tool/security context. Use **Todo Task** for multiple discrete repeating sub-tasks or sub-agent delegation, and **Routing** for fixed branch choices.

**For recurring multi-step shapes** (Phase Router, Scoped Investigation, Linear Pipeline, Fan-out & Consolidate, Verification Gate, Pre-flight Probe, Human Checkpoint, Critique Loop, Persistence Tail), call `get_reference_doc(kind="workflow-patterns")` — load when starting a new plan or restructuring an existing one.

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

**Use todo_task when the step manages MULTIPLE discrete tasks**, especially when:
- The tasks are **known in advance** and will run each time (e.g., "process each form field", "check each compliance item")
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

**Example**: A step that "processes tax form pages" should have sub-agents for known page types (income, deductions, credits) rather than one generic agent handling all pages.

### Step 5: When to Use Message Sequence

`message_sequence` is the **default** step type (see Step 2). Use it whenever ordered agent turns share the same working context and build on each other, and the boundary between turns is not a durable workflow boundary — which is most agentic work, since the natural shape is **do the task, then verify and fix it in follow-up items of the same conversation**. This is better than several regular steps that re-read the same files, need each other's transient reasoning, and produce only one final output.

- **Make verification a follow-up item, not a separate step.** A typical sequence is `[do the task] → [verify the output against the success criteria, list any gaps] → [fix the gaps]`. The verifier turn has the full context of what was just done, so it catches more than a fresh regular step reconstructing from artifacts.
- Break one large task into small user_message items: one instruction per turn.
- Prefer it when turns share the same inputs, tools, credentials, runtime, and security context; fail/retry together; and can be validated as one unit.
- Keep separate regular steps when each turn has its own durable artifact, validation gate, retry/failure domain, tool/security context, or downstream consumer.
- Add learning / knowledgebase / db update items as user messages at the exact point they should happen.
- Add explicit reference-check, hallucination-check, critique, or self-validation items when reliability needs it.
- Reads for KB, db, and learnings are always available. Writes are off by default, per item — **any item that writes db/KB/learnings must declare it** (`write_access: {"db": true}` etc., or `kind`/`output_files`), or the runtime folder guard blocks the write. See `get_reference_doc(kind="message-sequence")`.
- Python `code` items are for deterministic parsing/transforms. On success, the next user_message gets script path, output paths, and summarized logs as prepended context.
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

Step-level `success_criteria` is deprecated. Rely on a strong `description` plus `validation_schema` instead.

### Step 8: Think About Failure Modes

- If a step might fail due to external factors (login, API), add clear error handling in the description
- If a step's output needs semantic validation (not just structural), add a verification item right after it in the `message_sequence` (the default) — reach for a separate validation step only when the check needs its own durable artifact, different tools, or its own failure domain
- If a step is flaky, add explicit retry/polling instructions inside the step or split the unstable part into a dedicated regular step with strong validation

### Design Anti-Patterns to Avoid

- **Monster boundaries**: A single step owns unrelated durable outputs, validation gates, failure domains, or persistent stores — split at those boundaries. Many tool calls alone are not a reason to split.
- **Trivial steps**: A step that just reads a file and passes it through — merge with the consumer
- **Over-splitting same-context turns**: Several regular steps mostly reread the same context and depend on each other's transient reasoning. Collapse into a `message_sequence` unless separate durable artifacts, validation gates, retry boundaries, or tool/security contexts are needed.
- **Missing validation**: No validation_schema means no automated quality gate
- **Vague descriptions**: "Process the data appropriately" — be specific about WHAT, HOW, and WHERE
- **Over-sequencing**: Steps that don't depend on each other can potentially run in parallel via independent step groups
- **Inline sub-tasks in todo_task**: If you're writing detailed instructions for a specific task inside the orchestrator description, that task should be a sub-agent route instead

### Step Types Reference

- **Message Sequence** (type: "message_sequence") — **the default**: single-agent ordered conversation with `items`. Do the work, then **verify and fix it in follow-up user_message items** — all in one shared context — instead of splitting into several regular steps that hand off via `.md`/`.json` files. Supports short user_message items, optional prevalidation items, and optional Python code items. As a top-level step the queue runs once; as a todo_task route it can be re-entered during the same workflow run and receive new instructions without replaying the queue.
- **Regular** (type: "regular"): one atomic action with no verify-and-fix follow-up. Executes an agent that produces a context_output file.
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
