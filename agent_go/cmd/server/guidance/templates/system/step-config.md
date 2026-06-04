## STEP CONFIG — planning/step_config.json (per-step tuning)

Every step has an optional config entry that overrides defaults for that step. All of it is set with **`update_step_config(step_id, ...)`** and removed with **`update_step_config(step_id, clear=[...])`** (clearing returns the field to its default). Never hand-edit `planning/step_config.json`. This doc is the one-stop map of the knobs; load the linked deep-dives when a knob needs more than the summary.

### Store access — who can touch the three persistent stores

A step's access to each store is independent and defaults differently. Grant the least access the step actually needs.

| Field | Values | Default | Grant when… |
|---|---|---|---|
| `learnings_access` | `read` · `read-write` · `none` | `read` | `read-write` only for reusable execution HOW (browser selectors/timing/auth, API/MCP quirks, CLI/SDK patterns, parsing/retry/recovery) — also requires a concrete `learning_objective`. `none` only when shared SKILL.md would mislead the step. |
| `knowledgebase_access` | `read` · `write` · `read-write` · `none` | `none` | KB is opt-in. `read` to consume business context/notes (covers `knowledgebase/context/`); `write`/`read-write` to contribute — also needs a non-empty `knowledgebase_contribution`, and `knowledgebase_write_method` (`direct` = step writes notes/ itself, default; `agent` = separate post-step writer, only if the user asks). |
| `db_access` | `read` · `read-write` | `read-write` | db/ is the shared structured-state surface, so read+write is the default. Set `read` for least-privilege steps that must never mutate the db — pure readers, report-shaping/aggregation, validation/preflight: db/ stays readable but is removed from the step's write paths, so an accidental write is sandbox-denied. |

- **Path contract:** steps reach the db via the absolute **`$DB_PATH`** env var (`os.environ['DB_PATH']`), NOT a relative `db/db.sqlite` — a step's working directory is its execution folder, not the workflow root.
- **Rule of thumb:** routing, validation, mechanical transforms, aggregation/report-shaping, human approval, pure db/KB readers, and mature scripted steps should stay read-only on learnings (and often `db_access: read`).
- Deep dive on what belongs in each store and the write contracts: `get_reference_doc(kind="stores")`.

### The three locks

| Lock | Scope | Effect | Set when |
|---|---|---|---|
| `lock_learnings` | per-step | Freezes `learnings/{step}/SKILL.md` — still read, no new writes | deliberate builder/user decision; never a runtime side effect |
| `lock_code` | per-step (scripted) | Freezes `learnings/{step}/main.py`, skips the fix loop | only after user-explicit scripted + 10+ scenario-covering runs |
| `lock_knowledgebase` | workflow-wide | Freezes `knowledgebase/notes/` auto-updates | when KB is curated and should stop auto-evolving |

Only pass a lock field when you are explicitly changing it — passing `lock_learnings:false` while editing other fields resets a previously set value.

### Execution mode + which model runs the step

- **`declared_execution_mode`**: `agentic` (default for workshop-created steps) vs `scripted`. Promote agentic→scripted only with an explicit user ask, deterministic behavior, and 10+ scenario-covering successful runs. Set `declared_execution_mode_reason` when you do.
- **`use_code_execution_mode`**: per-step override of the preset's code-execution toggle (nil = inherit).
- **Model selection**: `execution_tier` (`high`/`medium`/`low`) maps to the workflow's tiered allocation; `execution_llm` / `validation_llm` / `learning_llm` pin a specific published model for that role. Prefer tiers over hard pins. Full framework: `get_reference_doc(kind="llm-selection")`.

### Other common fields

- **`validation_schema`** — the only automated gate (set via `update_validation_schema`); catches stale files, missing fields, constraint violations. Every step needs one.
- **`enabled_skills`** — step-level skill selection (step execution does NOT inherit workflow-selected skills; set explicitly). `enabled_custom_tools`, `selected_servers`, `selected_tools` — narrow the step's tool surface.
- **`review_notes`** — one-line WHY for non-obvious config (future hardening passes + other reviewers read it). Record it whenever you set learning/KB access or designate a db writer.
- **`description_reviewed`**, `coding_agent_tmux_lifecycle`, `transport`, `disable_parallel_tool_execution` — situational.

### Workflow

1. `update_step_config(step_id, <field>=<value>, ...)` to set; `update_step_config(step_id, clear=["<field>", ...])` to revert to default.
2. Pair access grants with their prerequisite (`learnings_access: read-write` ⇒ `learning_objective`; KB write ⇒ `knowledgebase_contribution`).
3. For the harden/replan decision-making that drives most config changes: `get_reference_doc(kind="optimize-playbook")`.
