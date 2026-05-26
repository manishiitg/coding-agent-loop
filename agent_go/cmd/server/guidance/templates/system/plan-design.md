## PLAN DESIGN — From Requirements to Steps (DESIGN phase)

When a user describes what they want to automate, follow this process to design the plan. **Present the plan to the user and get explicit confirmation before creating any steps.** The user may be exploring or testing ideas — do not assume they are ready to commit to a workflow structure.

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
| Agent performs a task and writes output | **Regular** | Simplest type — one agent, one output |
| Task has multiple known sub-tasks that repeat | **Todo Task** (sub-workflow/pipeline) with sub-agents | Each sub-task gets its own learning, validation, and tools |
| Need to branch based on prior step output or context | **Routing** | Supported branch primitive — evaluates context and picks a route |
| Need user input before proceeding | **Human Input** | Blocks until user responds |
| User input determines the path | **Human Input** → **Routing** | Collect input first, then LLM routes based on it |
| Utility/debug tool available but not auto-run | **Orphan** (is_orphan: true) | Not in main flow; manual execution from workshop only |

**Default to Regular** unless the task clearly needs branching, iteration, or sub-agents.

**For recurring multi-step shapes** (Phase Router, Scoped Investigation, Linear Pipeline, Fan-out & Consolidate, Verification Gate, Pre-flight Probe, Human Checkpoint, Critique Loop, Persistence Tail), call `get_reference_doc(kind="workflow-patterns")` — load when starting a new plan or restructuring an existing one.

### Step 3: Design Context Flow

Every step reads from prior steps and writes for downstream steps:
- **context_dependencies**: Files from prior steps this step needs (e.g., ["login_status.json"])
- **context_output**: The file this step produces (e.g., "extracted_data.json")
- **Flow must be forward-only** — no circular dependencies
- Use JSON for structured data. Keep output files < 100KB. For large content, write a separate .md file and reference it from the JSON.

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

Use `message_sequence` only when the user explicitly wants one persistent agent conversation with a known ordered queue of user messages.

- Break one large task into small user_message items: one instruction per turn.
- Add learning / knowledgebase / db update items as user messages at the exact point they should happen.
- Add explicit reference-check, hallucination-check, critique, or self-validation items when reliability needs it.
- Reads for KB, db, and learnings are always available. Writes are item-scoped through `write_access`.
- Python `code` items are for deterministic parsing/transforms. On success, the next user_message gets script path, output paths, and summarized logs as prepended context.
- As a todo_task predefined route, a message_sequence behaves like a reusable specialist sub-agent: reuse the same route for critique, test feedback, validation feedback, or follow-up work that should keep prior context; restart only when the prior conversation is stale, wrong, or contaminated.

### Step 6: When to Use Routing (brief)

Use `routing` when the next step depends on a decision that needs **LLM judgment** to pick **exactly one of N mutually exclusive paths** (e.g., "did login succeed, hit MFA, or fail?"). For deterministic checks, use validation_schema + retry, not routing. For running every sub-task, use todo_task. For a linear conversation, use message_sequence. Pair `human_input` → `routing` when the user's answer determines the path.

Two modes: **pure routing** (omit `description`, route on prior context) or **execute-then-route** (provide `description`, perform a probe, then route on the result). Each `routes` entry needs a stable `route_id`, a `condition` the router matches against `routing_question`, and a `next_step_id` that points to another step in the plan (routing routes do **not** define inline sub-agents — they branch to existing steps); set `default_route_id` for the fallback.

For full route structure, mode trade-offs, and anti-patterns, call `get_reference_doc(kind="routing")` — load before designing or hardening any routing step.

### Step 7: Design Validation

Every step MUST have a **validation_schema** — the automated gate that pass/fails the step:
- Check file existence, required fields, value types, patterns, and lengths
- Include enough checks that stale/leftover files from previous runs can't pass
- For todo_task steps: validation passing IS the completion signal

Step-level `success_criteria` is deprecated. Rely on a strong `description` plus `validation_schema` instead.

### Step 8: Think About Failure Modes

- If a step might fail due to external factors (login, API), add clear error handling in the description
- If a step's output needs semantic validation (not just structural), add a separate validation step after it
- If a step is flaky, add explicit retry/polling instructions inside the step or split the unstable part into a dedicated regular step with strong validation

### Design Anti-Patterns to Avoid

- **Monster boundaries**: A single step owns unrelated durable outputs, validation gates, failure domains, or persistent stores — split at those boundaries. Many tool calls alone are not a reason to split.
- **Trivial steps**: A step that just reads a file and passes it through — merge with the consumer
- **Missing validation**: No validation_schema means no automated quality gate
- **Vague descriptions**: "Process the data appropriately" — be specific about WHAT, HOW, and WHERE
- **Over-sequencing**: Steps that don't depend on each other can potentially run in parallel via independent step groups
- **Inline sub-tasks in todo_task**: If you're writing detailed instructions for a specific task inside the orchestrator description, that task should be a sub-agent route instead

### Step Types Reference

- **Regular** (type: "regular"): Standard task. Executes an agent that produces a context_output file.
- **Orchestrator / Todo Task / Sub-Workflow** (type: "todo_task"): Also called "orchestrator" by users. Manages a dynamic todo list. Has a **todo_task_step** (orchestrator) and **predefined_routes**. Each route can either define an inline **sub_agent_step** or reuse a plan-local orphan definition via **orphan_step_ref**. Route sub-agents are usually **regular** steps, can be **message_sequence** when the route needs persistent specialist memory with re-entry/restart behavior, and can be another **todo_task** (nested orchestrator) when that route needs its own nested orchestration. Only one nested todo_task layer is allowed: top-level todo_task -> nested todo_task is valid, but a nested todo_task must not contain another nested todo_task.
- **Message Sequence** (type: "message_sequence"): Persistent single-agent conversation with ordered `items`. Use short user_message items, optional prevalidation items, and optional Python code items. The sequence can resume and receive a new user message without replaying the queue.
- **Routing** (type: "routing"): N-way LLM-based branching. Has a **routing_question** and **routes[]**, each with **route_id**, **condition**, and **next_step_id** (pointer to an existing step). Optional **default_route_id** is the fallback. Two modes: pure routing (no `description`, routes on prior context) or execute-then-route (with `description`, executes first then routes on its own output).
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
