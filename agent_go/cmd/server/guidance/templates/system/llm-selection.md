## LLM Selection — picking & pinning the model that runs a step

This doc is about choosing which LLM executes workflow work. There are **two levels**: the workflow-wide tiered allocation, and per-step overrides. Default to the tiered allocation; reach for per-step pins only when a specific step needs it.

### The tiered model (workflow-level, the default)

A workflow runs in **tiered mode**: instead of hardcoding a model per step, you define three tiers once and the scheduler picks a tier per run based on step maturity.

- **tier_1 — high reasoning**: first-time execution and initial learning extraction (a step's early, unproven runs).
- **tier_2 — medium**: execution once learnings exist, and learning refinement.
- **tier_3 — low**: fast/cheap, for deterministic or file-shape work.
- **phase_llm**: the model used for planning, eval design, debugging phases, and normal builder chat helpers (not step execution).
- **auto_improve_llm**: optional model used by expensive background maintenance/advisor agents, including Goal Advisor, harden/review agents, KB consolidation/reorganization, and DB improvement. Leave unset to use the provider maintenance default when available (Claude Code defaults to `claude-opus-4-8` with high reasoning).
- **pulse_llm**: optional model used by the lightweight Pulse coordinator turns and routine scheduled Pulse/post-run QA messages. Leave unset to use the provider Pulse default when available (Claude Code defaults to `claude-sonnet-5` with high reasoning).

Each tier takes a `provider` + `model_id` (both required to set a tier) and an optional ordered `fallbacks` list (tried if the primary fails).

**Tools:**
- `list_published_llms` — the models available to this workflow (start here; never guess provider/model_id).
- `list_provider_models` — models exposed by a configured provider.
- `test_llm` — smoke-test a provider/model before committing to it.
- `set_workflow_llm_config(tier_1, tier_2, tier_3, phase_llm?, auto_improve_llm?, pulse_llm?)` — save the tiered config to `workflow.json` → `capabilities.llm_config`. Do NOT edit `workflow.json` by hand.
- `get_llm_config` / `get_workflow_config` — inspect the current tiered config, phase LLM, and any per-step overrides.

### Per-step overrides (use sparingly)

Set via `update_step_config(step_id, ...)`:

- **`execution_tier`** (`"high"` | `"medium"` | `"low"`) — a *persistent* tier override for one step in tiered mode. Use **high** for subjective/ambiguous judgment, **medium** for normal checks, **low** for deterministic/file-shape checks. Prefer this over pinning an exact model when the intent is just "this step can usually run cheaper/faster".
- **`execution_llm`** (`{provider, model_id, fallbacks?}`) — pins an *exact* model for one step. Use only when a specific model is genuinely required (a capability only that model has).
- **`validation_llm`** — same shape, overrides the model used for that step's validation. Learning model selection is handled by tiered allocation; there is no separate `learning_llm` setting.

**Precedence (highest wins):**
1. `execute_step(step_id, group_name, tier="...")` — a one-off tier for a single trial run; changes nothing persistent.
2. `execution_llm` — if set, it pins the model and **`execution_tier` is ignored** until the exact-model override is cleared.
3. `execution_tier` — persistent tier for the step.
4. Maturity-based default tier selection — what tiered mode does on its own.

**Clearing an override:** `update_step_config(step_id, clear=["execution_llm"])` (or `["execution_tier"]`) removes the override so the step inherits tiered/default behavior again.

### Choosing — a short decision framework

1. **Start with tiers, not pins.** Set sensible tier_1/2/3 once via `set_workflow_llm_config` and let maturity selection do the rest. Most steps never need a per-step override.
2. **Don't force a cheaper tier before reliability is proven.** Drop a step to `medium`/`low` only after it's stable with eval/run evidence at target — premature downgrading trades correctness for cost.
3. **Use `execution_tier` for "usually cheaper/faster", `execution_llm` for "must be this exact model".** Don't hardcode a model when a tier expresses the intent.
4. **Trial before committing.** Use `execute_step(..., tier="...")` or `test_llm` to validate a model on a real run before making it persistent.
5. **Match tier to the work.** Subjective/ambiguous judgment → high; routine checks → medium; deterministic/file-shape → low.

### Cost awareness

- `review_workflow_costs(iteration?, group_name?, focus?)` — read-only cost analysis with safe-reduction recommendations (which steps could drop a tier).
- `get_cost_summary` — current spend snapshot.
- `estimate_llm_cost(...)` — estimate priced (media) generation before high-volume runs.

### Installing / authorizing providers

- Only models in the workspace's published set surfaced by `list_published_llms` are routable for chat/text execution.
- To add credentials for a provider, use `set_provider_auth(provider, api_key?, region?, endpoint?, ...)` — never paste API keys into shell or config files.
- Provider-backed **media** capabilities (image/video/audio/text generation, transcription, web search) are a separate surface with their own provider/model contracts and discovery — see `get_reference_doc(kind="workspace-media-tools")`. This doc covers the LLM that *executes agent steps*, not media generation.
