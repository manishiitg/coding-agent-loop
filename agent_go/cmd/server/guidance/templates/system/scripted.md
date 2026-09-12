## scripted — Deterministic Worker

A `regular` step is the scripted boundary for one deterministic unit of work. It owns one
coherent output and deterministic final gate and runs through the saved `main.py` path.
Do not create an agentic regular step: every new conversational or judgment-heavy step uses
**`message_sequence`**, even when it needs only one work turn. Persisted non-scripted regular
steps are normalized to a one-turn message sequence at runtime; they never use the removed
direct regular-agent path. See `read_skill(skills=[{"name":"builder-reference","path":"references/message-sequence.md"}])`.
Use the others for branching (`branch` for a small in-flow decision, `routing` for a major
sub-workflow fork), sub-agent coordination (`orchestrator`), or operator input (`human_input`).

## When to use

- Deterministic, self-contained work: fixed API/SDK calls, CLI commands, data fetching, known pagination, parse, normalize, transform, write, and mechanically verify. Declare these steps `scripted` from initial design and batch related calls that share one source/auth/retry/output contract.
- One clear deterministic objective expressible as a `description` plus a `validation_schema`.
- Batch related deterministic actions behind one input/output and retry contract; use `references/plan-design.md` for composing that script with agentic work.

If selecting further work requires agentic judgment, or the task needs conversational
memory, use the redirects below. A deterministic script may process many records;
task count alone does not make it agentic.

## Calling scripts from a message sequence

Create a reusable definition with `add_scripted_step(is_orphan=true, ...)`, then
reference its ID and typed parameters from a sequence's `scripted` batch item.
The runtime runs saved code, validates outputs, waits for the batch, and passes
result paths to the sequence's next conversational turn. No child LLM, automatic
code repair, or automatic retry is started by this path. Script failures stop the
sequence after remaining batch calls settle. Keep source fixes in Workshop.

This is deterministic script execution, not an agentic sub-agent. The sequence's
own conversation handles reasoning and reporting. See `references/message-sequence.md`
for the exact item schema, permissions, parallelism, Stop behavior, and limits.

## Anatomy

- `description` — the executable instruction/prompt for the step agent, not metadata. Resolved variable values are available as `$VAR_*`.
- `script_parameters` — the optional, typed public input contract for direct execution,
  orchestrator routes, or message-sequence scripted batches. Each named parameter declares `type`,
  `description`, and optionally `required`, `default`, and `enum`. These are non-secret
  per-call values; credentials still belong in Secrets. The builder defines this contract
  with the step, and `main.py` reads the validated object from `STEP_PARAMS_JSON`.
- `context_dependencies` → `context_output` — forward-only context flow between steps.
- `validation_schema` — **required**; gates the step. Checks **files** (file_checks +
  json_checks) AND/OR the **db** (`db: [{sql, min_rows, max_rows, checks}]` — read-only
  queries against `db/db.sqlite`). On failure the agent retries with the failed-check
  feedback. Prefer **db checks** when the step writes its results to the db: they gate on
  the source of truth, so you don't need a hand-written output file just to validate (a
  duplicated summary file drifts from the db — e.g. a `status` that ends up null).
- Stores: reads soul / db / knowledgebase / learnings per access; writes its own step
  folder + `db/`, plus knowledgebase notes / learnings when access is read-write
  (learnings writes happen in a dedicated post-step turn — see `read_skill(skills=[{"name":"builder-reference","path":"references/stores.md"}])`).

## Execution mode

- **Scripted / code-execution mode** is the only mode for new regular steps. Create one with `add_scripted_step`; the internal plan type remains `regular`. The builder authors a `main.py` saved under
  `code/{step-id}/` when workflow.json has `code_layout_version: 1`; absent/0 stays at `learnings/{step-id}/`. Prefer an explicit migration to `code/` for legacy scripted workflows using the "Deliberate migration to code/" procedure in `references/code-authoring.md`; never silently move files or change the layout flag. Test using `execute_step(fast_path_only=true)` so the actual runner supplies the selected group's environment, inputs, permissions and working directory. New-layout source and shared helpers are edited in place, not copied into a run. Use for
  deterministic, repeatable execution. No run-history threshold is required to declare an obviously deterministic step scripted; 10+ representative successful runs are required only before `lock_code=true` freezes it. See `read_skill(skills=[{"name":"builder-reference","path":"references/code-authoring.md"}])`.
- Judgment, adaptive discovery, ambiguous live evidence, and browser/UI work use `message_sequence`.

Preferred data shape: `regular scripted fetcher(s) → message_sequence processor`. Fetchers own credentials, calls, retries/rate limits, provenance, freshness, idempotency, response parsing, and authoritative DB/file output. The message sequence reads that output and owns semantic analysis, synthesis, critique, and repair.

## Parameterized script calls

Use parameters—not rewritten code or free-form delegation prose—when one reusable script
needs controlled variation between calls. Direct execution, sequence batches, and
orchestrator routes share the saved step's parameter contract:

```json
"script_parameters": {
  "market": {
    "type": "string",
    "description": "Market whose deterministic checks should run",
    "required": true,
    "enum": ["india", "usa", "dubai"]
  },
  "limit": {
    "type": "integer",
    "description": "Maximum rows to fetch",
    "default": 100
  }
}
```

The builder must make `main.py` parse `json.loads(os.environ["STEP_PARAMS_JSON"])`
and must not hardcode values from an individual test run. The runtime applies
defaults and rejects unknown parameters, missing required values, wrong types,
and enum violations before starting Python. Positional arguments remain reserved
for `context_dependencies`.

- **Message sequence:** put literal values in each `scripted_steps[].parameters`
  object in the authored `scripted` item. The sequence agent does not generate or
  modify those values at runtime; no template/environment-variable expansion or
  binding from earlier conversation results is implemented. There is no child
  LLM or automatic repair for sequence script calls.
- **Orchestrator:** read the route description, then call `call_scripted_sub_agent`
  with `parameters` matching this contract. That tool has no `instructions`
  argument. If this route enters its existing repair path, preserve the declared
  parameter interface and current validated values.

The Builder tests the identical contract with
`execute_step(step_id="...", script_parameters={...})`; never simulate it by exporting
`STEP_PARAMS_JSON` in a generic shell. `execute_step` rejects parameters on non-scripted
steps and rejects invalid values before registering background execution.

## When NOT to use (redirects)

- Branching on a decision or run flag → **`branch`** (small in-flow decision) or **`routing`**
  (major, self-contained sub-workflow fork).
- Owning an adaptive strategy that interprets evidence and chooses subsequent work → **`orchestrator`**. A known script batch belongs in a message sequence; worker count alone does not justify an orchestrator.
- Same-context ordered turns, a stateful conversation, self-validation/grounding
  gate, or stepping through a db array row-by-row → **`message_sequence`**
  (incl. its `foreach` item).
- Pausing for a fixed human approval/selection → **`branch` with `route_source="human"`**; capturing a free-form value → **`human_input` (`text`)**.

## Anti-patterns

For saved `main.py` browser tests, existing Python Playwright harnesses remain supported.
Attach Python contexts with `agentworks-playwright` for live viewing. JS/TS suites
use `@agentworks/playwright`;
they use the Playwright runner and do not replace the saved Python entry point. Read `references/playwright-scripted.md` before authoring or repairing the suite.
The suite owns its Playwright objects, case isolation, timeouts, evidence, and cleanup;
`agent_browser` remains the conversational browser tool and is not a test-harness API.
Use `execute_step(fast_path_only=true)` for acceptance testing; direct builder shell runs
do not prove the selected step's permissions, variables, or output validation.
Inspect `debug_step` for captured output and validation; use `log_offset` for more output.
Each saved-script attempt is retained in the execution log directory as `scripted-*.json`;
`scripted_fast_path.json` is the latest-result alias.

- Cramming multiple durable outputs into one step — split at output / store /
  failure-domain boundaries.
- Narrative branching in the description ("if X do A else B") — use a `branch` or `routing` step.
- Choosing an agent merely because there are many records. Deterministic row processing
  can stay in a saved script. Use SQL `foreach` in `references/message-sequence.md`
  when every selected row needs a conversational turn; use `references/orchestrator.md`
  when the parent must reason about the investigation or strategy.
- Missing or weak `validation_schema` — every step needs one strong enough that a
  bad/absent output fails it.
