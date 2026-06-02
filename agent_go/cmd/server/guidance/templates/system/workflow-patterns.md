## WORKFLOW COMPOSITION PATTERNS

These are the recurring shapes that real workflows in this system take. When designing a new plan or restructuring an existing one, find the closest pattern and start from its layout — don't invent novel structures unless the workflow genuinely doesn't fit.

Each pattern lists: industry-alignment line (for users with prior vocabulary), trigger phrases, primitive layout, common pitfalls.

The builder's primitives referenced below: `regular`, `todo_task`, `routing`, `human_input`, `message_sequence`.

---

### 1. Phase Router

**Industry alignment**: Anthropic *Routing*, applied at the top of the plan to host multiple sub-flows in one workflow.

**Trigger phrases**: "support multiple modes", "do X or Y depending on the run flag", "the same workflow needs to handle review and execute", "one of N flows".

**Layout**:
- Top-level `routing` step on a mode/flag (e.g., `$RUN_MODE`) → 2–N branches
- Each branch is its own subgraph (chain of `regular`, or a `todo_task`, or another routing)
- Optional small terminate `routing` near the end of each branch to converge or end cleanly

**When to use**: one logical workflow has distinct entry modes (dry-run vs apply, learn vs execute, daily vs weekly). Avoids two near-duplicate plans.

**Pitfalls**:
- Routing on a signal that isn't actually in context yet — set `description` on the routing step (execute-then-route) to produce the signal first.
- Branches drift apart over time — keep shared steps (login, KB read) outside the router so both branches benefit.
- Using a Phase Router when a single linear plan would do — only use it when the branches are genuinely different.

---

### 2. Scoped Investigation

**Industry alignment**: Anthropic *Orchestrator-Workers* with a human-input seed (HITL scope-setting).

**Trigger phrases**: "investigate X", "do an RCA", "audit Y", "recon this system", "find the root cause".

**Layout**:
- `human_input` (text or multiple_choice) collects the scope, target, or hypothesis
- `todo_task` orchestrator investigates — sub-agents per source / per hypothesis / per attack surface
- Final `regular` step composes the deliverable (report, root-cause doc, audit findings)

**When to use**: the scope cannot be known at design time and the investigation has to fan out across angles the orchestrator decides at runtime.

**Pitfalls**:
- Pre-defining too many routes in the `todo_task` — for investigations, the generic agent often handles dynamic sub-tasks better than fixed routes.
- Skipping the `human_input` and letting the orchestrator guess the scope — produces unfocused output.
- Putting report-writing inside the `todo_task` instead of after it — splits the report across runs and breaks consolidation.

---

### 3. Linear Pipeline

**Industry alignment**: Anthropic *Prompt Chaining*.

**Trigger phrases**: "login, then download, then parse, then upload", "step-by-step automation", "fetch and process", "scrape and update sheet".

**Layout**:
- Pure `regular` chain: credential read → login → fetch/download → parse → transform → upload → verify
- No `routing`, no `todo_task`, no fan-out
- Each step has a strict `validation_schema` so failures stop the chain

**When to use**: sequential, deterministic automation where each step depends on the previous and there is no real branching. Common for bank statements, document parsing, GST/tax audits, scheduled imports.

**Pitfalls**:
- Merging too many actions into one step — split at durable-output boundaries (different file, different store, different failure domain).
- Skipping the verify step before the next mutation — see pattern #5 (Verification Gate).
- Forcing a `todo_task` when the work is genuinely linear — adds orchestration overhead with no gain.

---

### 4. Fan-out & Consolidate

**Industry alignment**: Anthropic *Orchestrator-Workers* / *Parallelization* (with a central LLM orchestrator).

**Trigger phrases**: "process every item", "for each section / source / team", "research multiple angles", "test every component", "run X for each Y".

**Layout**:
- One or more `todo_task` steps with predefined routes per item type (or generic agent for unknowns)
- Followed by a `regular` consolidator/synthesizer step that reads each route's output
- Often paired with #5 (Verification Gate) on the consolidator

**When to use**: N independent sub-tasks share the same orchestrator goal and need to be combined into one deliverable.

**Pitfalls**:
- Inlining detailed per-item instructions in the orchestrator description — that detail belongs in the route's `sub_agent_step.description`.
- No consolidator step — leaves N orphan outputs and no synthesis.
- Routes with different output schemas — consolidator can't merge them; align route outputs first.

---

### 5. Verification Gate

**Industry alignment**: Lite *Evaluator-Optimizer* — evaluator without the optimizer loop (hard gate, not iterative).

**Trigger phrases**: "verify the upload succeeded", "check the data landed", "confirm before continuing", "make sure X is right before doing Y".

**Layout**:
- `regular`(action) → `regular`(verify with strict `validation_schema`) → next step
- The verify step typically re-reads the system of record (sheet, db, API) and asserts the action's effect
- Verify step's `validation_schema` is strong enough that a stale or absent write fails it

**When to use**: after any mutation that downstream steps depend on (upload, write, publish, submit). Cheaper than discovering corruption three steps later.

**Pitfalls**:
- Using the action step's own output to "verify" itself — that just confirms the step ran, not that the mutation landed. Re-read from the system of record.
- Weak `validation_schema` (just file-existence) — must check that values match what was written.
- Adding verification gates everywhere — only gate the steps whose effects others depend on.

---

### 6. Pre-flight Probe

**Industry alignment**: No direct industry name; community calls this "guard step" or "precondition check". Loosely related to *Routing*.

**Trigger phrases**: "check if logged in first", "make sure CDP is up", "verify access before running", "abort if X is not ready".

**Layout**:
- First `regular` step is a cheap probe (auth status, connectivity, access token, browser session, DB reachable)
- Immediately followed by a `routing` step that aborts / re-auths / continues based on the probe's output
- Probe's `context_output` is small (just a status JSON) and read by the routing step

**When to use**: when the rest of the plan is expensive or destructive and an early environmental failure would waste a run. Critical for browser-driven flows, cloud SSO, third-party APIs.

**Pitfalls**:
- Probe step doing too much — it should be cheap and fail fast.
- No bailout path — if the probe just logs failure and continues, downstream steps will fail confusingly.
- Probing things the validation_schema could check — only probe what can't be expressed as a schema.

---

### 7. Human Checkpoint

**Industry alignment**: Human-in-the-loop (HITL) — standard term across frameworks.

**Trigger phrases**: "let me approve before publishing", "I want to pick the topic", "ask me before doing X", "confirm with the operator", "let me review the draft".

**Layout**:
- `regular`(draft / propose / select) → `human_input`(approve / pick / edit) → `regular`(publish / execute)
- Or `human_input` directly inside a `todo_task` route when the orchestrator should pause per item
- Different from pattern #2's seed: this `human_input` sits mid-pipeline, not at the start

**When to use**: irreversible actions (publish, submit, send), creative judgment (topic, tone), or contested decisions (which lead to pursue).

**Pitfalls**:
- Asking too many checkpoint questions — fatigue makes the user rubber-stamp.
- Putting the checkpoint after a costly step rather than before — the cost is sunk by approval time.
- Free-text `human_input` when a `multiple_choice` would do — choices reduce ambiguity and route cleanly.

---

### 8. Critique Loop

**Industry alignment**: Anthropic *Evaluator-Optimizer* / LangGraph *Reflection*.

**Trigger phrases**: "review and improve", "self-correct", "critique the draft", "iterate until good", "find issues in the output".

**Layout**:
- `regular`(execute) → `regular`(critique with explicit rubric) → optional `routing` back to execute, or forward to publish
- The critique step has its own learnings/KB so review standards accumulate over runs
- A todo_task variant: maker route + reviewer route, with the orchestrator alternating between them

**When to use**: outputs where quality matters and the critic can spot issues the maker missed (reports, code, strategy proposals, content).

**Pitfalls**:
- Critic and maker share the same context — the critic just rubber-stamps. Give the critic an independent rubric and isolated tools.
- Infinite loop — bound iterations or require the critique to converge (e.g., "if score ≥ 8 or iteration ≥ 3, exit").
- Using a critique loop when a `validation_schema` would do — schemas catch structural problems for free; reserve the critic for semantic judgment.

---

### 9. Persistence Tail

**Industry alignment**: No direct industry name; closest is "post-processing" or "bookkeeping step".

**Trigger phrases**: "update the dashboard", "log to db", "save findings to KB", "record the run", "sync to the report".

**Layout**:
- Last 1–2 `regular` steps in the plan, decoupled from the main work
- Typical writes: `db/db.sqlite` table upsert, KB SKILL.md update, dashboard score, report widget data
- Each tail step has its own `validation_schema` and is independently re-runnable (idempotent)

**When to use**: when the workflow has downstream consumers (dashboards, weekly reports, accumulated learnings) that need to be updated after every run.

**Pitfalls**:
- Tail step couples to the main work — if the tail fails, the user thinks the workflow failed. Keep tail steps idempotent and clearly named.
- Writing to too many stores in one tail step — split per store so failures are localized.
- Forgetting `db/README.md` schema declaration before writing to `db/` — see `get_reference_doc(kind="stores")`.

---

### 10. Data-Driven Iteration (foreach)

**Industry alignment**: *Map* over a dataset — deterministic iteration driven by data, not by an LLM enumerating items.

**Trigger phrases**: "for every row a prior step found", "process each record in the db", "one pass per item in the list", "drain the queue".

**Layout**:
- A producer step (any type) writes a JSON array to `db/<file>.json`
- A consumer step with a `foreach` item/entry over that file: `message_sequence` foreach (one agent handles each row) or `todo_task` foreach (orchestrator can delegate per row). The runtime sends one turn per row; the row is bound to `.` in the message template.
- Optional verify/`prevalidation` gate after the loop

**When to use**: an earlier step produces a list and **every** row must be processed. The loop lives in code, so nothing is skipped — the key advantage over telling an orchestrator "process all rows" (which can miss some). This is the reliable form of producer/consumer fan-out when the item set is already materialized in `db/`.

**Pitfalls**:
- Reaching for #4 (Fan-out & Consolidate) when the items are already a db array — `foreach` enumerates deterministically; an orchestrator might not.
- Unbounded rows blowing up cost — set `max_iterations`; the shared conversation is bounded by auto-summarization but each row is still an LLM turn.
- Producer/consumer path mismatch — the consumer's `source` must point at exactly where the producer wrote.

---

## Notes on the pattern set

**Why message_sequence is rarely referenced here**: only 2 of 28 real plans use `message_sequence` at the top level. It's still the right choice for stateful specialist conversations (see the Message Sequence brief and `get_reference_doc(kind="message-sequence")`), but most workflows decompose cleanly into the patterns above.

**Industry patterns not used here**:
- *Swarm* / *Shared Scratchpad* (LangGraph) — this builder does not support peer agents with shared memory. Use #2 (Scoped Investigation) or #4 (Fan-out) instead.
- *Long-running autonomous agent* — use `message_sequence` if this is genuinely what the user needs.

**When in doubt**: a workflow is almost always **#3 Linear Pipeline** with one or two of #5, #6, #7, #9 attached. Start there. Reach for #1, #2, #4, #8 only when the structure demands it.
