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
- Each branch uses one large `message_sequence` per shared-context span, coherent scripted deterministic boundaries where needed, or a `todo_task` only for independent delegation
- Optional small terminate `routing` near the end of each branch to converge or end cleanly

**When to use**: one logical workflow has distinct entry modes (dry-run vs apply, learn vs execute, daily vs weekly). Avoids two near-duplicate plans.

**Pitfalls**:
	- Routing on a signal that isn't actually in context yet — add a prior `regular` probe step that writes `route_selection.json`, then have the routing step read that file.
- Branches drift apart over time — keep shared steps (login, KB read) outside the router so both branches benefit.
- Using a Phase Router when a single linear plan would do — only use it when the branches are genuinely different.

---

### 2. Scoped Investigation

**Industry alignment**: Anthropic *Orchestrator-Workers* with a human-input seed (HITL scope-setting).

**Trigger phrases**: "investigate X", "do an RCA", "audit Y", "recon this system", "find the root cause".

**Layout**:
- Use scope, target, and hypotheses already supplied by the user, launch variables, or upstream durable context. Add `human_input` only when the running workflow genuinely cannot proceed without missing scope.
- Use one large `message_sequence` to investigate and produce the proof-bearing deliverable when the work shares one context.
- Use a `todo_task` only when independently delegated sources, hypotheses, or attack surfaces need isolated contexts, parallel progress, or independent retries; then feed their durable outputs to one large final `message_sequence` that consolidates, proves each finding, and repairs gaps.

**When to use**: the scope cannot be known at design time and the investigation has to fan out across angles the orchestrator decides at runtime.

**Pitfalls**:
- Pre-defining too many routes in the `todo_task` — for investigations, the generic agent often handles dynamic sub-tasks better than fixed routes.
- Asking for scope again when it is already present in the request or variables. If required scope is truly absent at runtime, use `human_input`; never let the orchestrator invent it.
- Putting report-writing inside the `todo_task` instead of after it — splits the report across runs and breaks consolidation.

---

### 3. Linear Pipeline

**Industry alignment**: Anthropic *Prompt Chaining*.

**Trigger phrases**: "login, then download, then parse, then upload", "step-by-step automation", "fetch and process", "scrape and update sheet".

**Layout**:
- One coherent scripted `regular` owns related deterministic credential/API/CLI/fetch/parse/transform/persist work under one source/auth/retry/output contract
- If judgment is required, its validated output feeds one large `message_sequence` that completes the semantic outcome, proves it from evidence, and repairs gaps
- No `routing`, no `todo_task`, no fan-out
- Each producing step has a strict `validation_schema`; the script or sequence performs its own evidence-based double-check before completion

**When to use**: sequential, deterministic automation where each step depends on the previous and there is no real branching. Common for bank statements, document parsing, GST/tax audits, scheduled imports.

**Pitfalls**:
- Splitting login, fetch, parse, transform, and persistence merely because they are separate actions even though they share one deterministic execution contract.
- Creating a separate verify step when the owning script or message sequence can re-read the authoritative system and prove the result in the same retry domain — see pattern #5.
- Forcing a `todo_task` when the work is genuinely linear — adds orchestration overhead with no gain.

---

### 4. Fan-out & Consolidate

**Industry alignment**: Anthropic *Orchestrator-Workers* / *Parallelization* (with a central LLM orchestrator).

**Trigger phrases**: "process every item", "for each section / source / team", "research multiple angles", "test every component", "run X for each Y".

**Layout**:
- One or more `todo_task` steps with predefined routes per item type (or generic agent for unknowns)
- Followed by one large `message_sequence` consolidator/synthesizer that reads every route output, proves coverage and consistency, and repairs the final deliverable
- The consolidator owns its verification and top-level validation unless an independent clean-room boundary is required

**When to use**: N independent sub-tasks share the same orchestrator goal and need to be combined into one deliverable.

**Pitfalls**:
- Inlining detailed per-item instructions in the orchestrator description — that detail belongs in the route's `sub_agent_step.description`.
- No consolidator step — leaves N orphan outputs and no synthesis.
- Routes with different output schemas — consolidator can't merge them; align route outputs first.

---

### 5. In-Context Verification Gate

**Industry alignment**: Lite *Evaluator-Optimizer* — evaluator without the optimizer loop (hard gate, not iterative).

**Trigger phrases**: "verify the upload succeeded", "check the data landed", "confirm before continuing", "make sure X is right before doing Y".

**Layout**:
- Agentic work: one `message_sequence` with `[perform the action] → [re-read the system of record and prove the effect] → [repair or retry, then double-check]`
- Deterministic work: one scripted `regular` performs the mutation and independently re-reads/asserts the authoritative state before returning
- The owning step's `validation_schema` requires run-specific proof/provenance so a stale or absent write fails it

**When to use**: after any mutation that downstream steps depend on (upload, write, publish, submit). Cheaper than discovering corruption three steps later.

**Pitfalls**:
- Using the action step's own output to "verify" itself — that just confirms the step ran, not that the mutation landed. Re-read from the system of record.
- Weak `validation_schema` (just file-existence) — must check that values match what was written.
- Adding a separate verifier merely to get a double-check. Keep it in the same sequence/script unless different credentials/tools, clean-room independence, or an independently rerunnable failure domain is required.

---

### 6. Pre-flight Probe

**Industry alignment**: No direct industry name; community calls this "guard step" or "precondition check". Loosely related to *Routing*.

**Trigger phrases**: "check if logged in first", "make sure CDP is up", "verify access before running", "abort if X is not ready".

**Layout**:
- Use one coherent probe boundary: a scripted `regular` for deterministic API/CLI/DB/auth/connectivity checks, or a `message_sequence` for adaptive browser/session inspection that needs same-context remediation and proof
- Immediately followed by a `routing` step that aborts / re-auths / continues based on the probe's output
- The probe writes a small validated status or `route_selection.json` contract with run-specific freshness/provenance; the routing step consumes that declared output

**When to use**: when the rest of the plan is expensive or destructive and an early environmental failure would waste a run. Critical for browser-driven flows, cloud SSO, third-party APIs.

**Pitfalls**:
- Probe step doing too much — it should be cheap and fail fast.
- No bailout path — if the probe just logs failure and continues, downstream steps will fail confusingly.
- Adding a probe when the owning step's `validation_schema` can fail directly and no branch decision is needed. A probe earns a separate boundary only when its result controls routing, avoids material cost/risk, or requires isolated credentials/runtime.

---

### 7. Human Checkpoint

**Industry alignment**: Human-in-the-loop (HITL) — standard term across frameworks.

**Trigger phrases**: "let me approve before publishing", "I want to pick the topic", "ask me before doing X", "confirm with the operator", "let me review the draft".

**Layout**:
- One large `message_sequence` drafts/proposes, re-opens evidence, proves the approval package, and repairs it before the human boundary
- `human_input` approves, selects, or edits; this is an intentional context boundary because new external information enters the run
- After approval, use a scripted `regular` for a fixed API/CLI publish/execute action with authoritative read-back verification, or another large `message_sequence` only when adaptive post-approval judgment needs its own shared context
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
- Default: one large `message_sequence` owns `[execute] → [re-open evidence and critique with an explicit rubric] → [repair and double-check]`
- When genuine clean-room independence matters, use separate large maker and reviewer sequences with intentionally isolated context, then an explicit repair handoff
- A todo_task variant is reserved for independently delegated maker/reviewer work, not ordinary self-checking

**When to use**: outputs where quality matters and the critic can spot issues the maker missed (reports, code, strategy proposals, content).

**Pitfalls**:
- Assuming every critic needs a new step. Same-context proof and correction belong in the owning sequence; isolate the reviewer only when independence is a real requirement, then give it an independent rubric and tools.
- Infinite loop — bound iterations or require the critique to converge (e.g., "if score ≥ 8 or iteration ≥ 3, exit").
- Using a critique loop when a `validation_schema` would do — schemas catch structural problems for free; reserve the critic for semantic judgment.

---

### 9. Persistence Tail

**Industry alignment**: No direct industry name; closest is "post-processing" or "bookkeeping step".

**Trigger phrases**: "update the dashboard", "log to db", "save findings to KB", "record the run", "sync to the report".

**Layout**:
- Prefer one coherent scripted `regular` persistence step for related deterministic `db/db.sqlite` upserts and `db/assets/` writes that share one transaction/retry contract. HTML reports read report-facing rows live with `window.report.query`.
- Workflow-discovered domain knowledge belongs in `knowledgebase/notes/` through the owning agentic step's declared knowledgebase contribution; reusable execution HOW belongs in `learnings/_global/SKILL.md` through the dedicated learning flow. Neither is generic report-data persistence.
- Split tails only when stores have genuinely independent permissions, transactions, retries, or failure semantics; every producing tail has a strict `validation_schema` and is idempotent

**When to use**: when downstream workflows or live reports need durable structured rows/assets updated after every run, and those writes should be independently rerunnable from the main agentic result.

**Pitfalls**:
- Tail step couples to the main work — if the tail fails, the user thinks the workflow failed. Keep tail steps idempotent and clearly named.
- Splitting per store automatically even when the writes form one atomic contract; conversely, do not merge stores whose permissions or failure semantics must remain isolated.
- Forgetting `db/README.md` schema declaration before writing to `db/` — see `get_reference_doc(kind="stores")`.

---

### 10. Data-Driven Iteration (foreach)

**Industry alignment**: *Map* over a dataset — deterministic iteration driven by data, not by an LLM enumerating items.

**Trigger phrases**: "for every row a prior step found", "process each record in the db", "one pass per item in the list", "drain the queue".

**Layout**:
- A producer step writes canonical rows to `db/db.sqlite`, normally with an idempotent scripted upsert and a documented table contract
- A consumer uses a `foreach` entry with read-only `source_sql`: `message_sequence.items[]` when one agent should retain shared context across rows, or `todo_task.messages[]` when the orchestrator should decide independent delegation. The runtime sends one turn per SQL result row, bound to `.` in the message template.
- The owning step's top-level `validation_schema` proves complete processing. Add an explicit `prevalidation` only when an intermediate aggregate must pass before later items run.

**When to use**: an earlier step materializes rows and **every** selected row must receive a conversational turn. Runtime SQL expansion enumerates the result deterministically instead of trusting an LLM to remember the list. Validate processed-versus-selected counts so caps, failures, or filtered rows cannot look complete.

**Pitfalls**:
- Reaching for #4 (Fan-out & Consolidate) when the rows already exist in SQLite and one shared-context sequence can process them with `source_sql`.
- Unbounded rows blowing up cost — set `max_iterations`, but remember that excess rows are skipped and logged; the final validation must fail when the workflow promised to process every row.
- Producer/consumer schema mismatch — the consumer's `source_sql` must query the documented table/columns and group/run scope that the producer actually wrote.

---

## Notes on the pattern set

**Why message_sequence is referenced differently here**: it is a step-internal conversation pattern, not a whole-workflow topology. Prefer it inside these patterns whenever several ordered turns need the same context and one durable validation/output boundary.

**Industry patterns not used here**:
- *Swarm* / *Shared Scratchpad* (LangGraph) — this builder does not support peer agents with a shared scratchpad. Use #2 (Scoped Investigation) or #4 (Fan-out) instead.
- *Long-running autonomous agent* — use `message_sequence` if this is genuinely what the user needs.

**When in doubt**: start with one large `message_sequence` per shared-context span and put proof, double-checking, and repair inside it. Add coherent scripted deterministic boundaries where needed. Reach for routing, human gates, fan-out, isolated critics, or persistence tails only when the workflow semantics create those boundaries.
