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
- A coherent scripted boundary inside the **Linear Pipeline** pattern (see `read_skill(skills=[{"name":"builder-reference","path":"references/workflow-patterns.md"}])`), not one step per pipeline action.

If the work fans out over items, branches on a decision, needs several turns that
share one conversation, or needs a specialist that remembers across calls, it is
**not** a regular step — see the redirects below.

## Anatomy

- `description` — the executable instruction/prompt for the step agent, not metadata. Resolved variable values are available as `$VAR_*`.
- `script_parameters` — the optional, typed public input contract when an orchestrator
  calls this script as a predefined route. Each named parameter declares `type`,
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

## Parameterized orchestrator routes

Use parameters—not rewritten code or free-form delegation prose—when one reusable script
needs controlled variation between orchestrator calls. The saved step is the single source
of truth:

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

The builder must make `main.py` parse `json.loads(os.environ["STEP_PARAMS_JSON"])` and
must not hardcode the values from an individual test run. At runtime the orchestrator first
reads the route description, then calls `call_scripted_sub_agent` with `parameters`
matching this contract. That tool has no `instructions` argument. The controller applies defaults and rejects unknown,
missing, or wrongly typed values before starting Python. The same declared contract and
current validated values are included in a repair turn, so repair must preserve the public
interface rather than inventing a second input path. Positional arguments remain reserved
for `context_dependencies`.

The Builder tests the identical contract with
`execute_step(step_id="...", script_parameters={...})`; never simulate it by exporting
`STEP_PARAMS_JSON` in a generic shell. `execute_step` rejects parameters on non-scripted
steps and rejects invalid values before registering background execution.

## When NOT to use (redirects)

- Branching on a decision or run flag → **`branch`** (small in-flow decision) or **`routing`**
  (major, self-contained sub-workflow fork).
- Coordinating ≥2 specialized sub-agents, or dynamic per-item work → **`orchestrator`**.
- Same-context ordered turns, a stateful conversation, self-validation/grounding
  gate, or stepping through a db array row-by-row → **`message_sequence`**
  (incl. its `foreach` item).
- Pausing for a fixed human approval/selection → **`branch` with `route_source="human"`**; capturing a free-form value → **`human_input` (`text`)**.

## Anti-patterns

For deterministic browser tests, direct Python Playwright is supported inside the saved
script. Read `references/playwright-scripted.md` before authoring or repairing the suite.
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
- A regular step that just enumerates a list and processes each item — if the list is
  a db array, use a `foreach` (see `workflow-patterns` #10); if each item needs
  sub-agents, use `orchestrator`.
- Missing or weak `validation_schema` — every step needs one strong enough that a
  bad/absent output fails it.
