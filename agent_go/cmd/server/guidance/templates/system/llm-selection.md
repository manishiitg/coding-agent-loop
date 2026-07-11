## LLM Selection — picking & pinning the model that runs a step

This doc is about choosing which LLM executes workflow work. There are **two levels**: workflow-wide role allocation and per-step overrides. Default to workflow roles; reach for per-step pins only when a specific step needs one.

### Workflow roles

A workflow always resolves these roles:

- **Builder**: planning, eval design, debugging, and normal workflow-builder chat.
- **High execution**: first-time, ambiguous, or difficult step work.
- **Medium execution**: established work with useful context and learnings.
- **Low execution**: deterministic validation and mature routine work.
- **Maintenance**: expensive Pulse modules such as Goal Advisor, Bug Review, report/eval improvement, KB health, and DB health.
- **Pulse**: the gate, worklist, report update, and notification coordinator turns.

The config has two modes:

- **`provider_profile` (simple)** stores only a coding-agent `provider`. The provider package supplies current defaults for every role at runtime, so an app update can improve those defaults without rewriting the workflow.
- **`explicit` (advanced)** pins `builder_llm`, `maintenance_llm`, `pulse_llm`, and all three entries under `tiered_config`. Every entry has a direct `provider` + `model_id`, optional provider `options` such as `reasoning_effort`, and optional ordered `fallbacks`.

Saved configurations are reusable shortcuts for exact provider/model/options combinations. They are not required before a provider or model can be selected.

**Tools:**
- `list_provider_models` — authoritative models and supported options exposed by a provider. Use this before choosing a direct model.
- `list_published_llms` — optional saved model configurations that may be reused as shortcuts.
- `test_llm` — smoke-test a provider/model before committing to it.
- `set_workflow_llm_config(mode="provider_profile", provider="...")` — follow the provider's current role defaults.
- `set_workflow_llm_config(mode="explicit", builder_llm=..., maintenance_llm=..., pulse_llm=..., tier_1=..., tier_2=..., tier_3=...)` — pin the complete advanced role configuration. Do NOT edit `workflow.json` by hand.
- `get_llm_config` / `get_workflow_config` — inspect the current workflow roles and per-step overrides.

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

1. **Start with a provider profile, not pins.** Let the coding-agent provider supply sensible role defaults. Use explicit mode only when the workflow needs a deliberate cross-provider or model-specific allocation.
2. **Don't force a cheaper tier before reliability is proven.** Drop a step to `medium`/`low` only after it's stable with eval/run evidence at target — premature downgrading trades correctness for cost.
3. **Use `execution_tier` for "usually cheaper/faster", `execution_llm` for "must be this exact model".** Don't hardcode a model when a tier expresses the intent.
4. **Trial before committing.** Use `execute_step(..., tier="...")` or `test_llm` to validate a model on a real run before making it persistent.
5. **Match tier to the work.** Subjective/ambiguous judgment → high; routine checks → medium; deterministic/file-shape → low.

### Cost awareness

- `review_workflow_costs(iteration?, group_name?, focus?)` — read-only cost analysis with safe-reduction recommendations (which steps could drop a tier).
- `get_cost_summary` — current spend snapshot.
- `estimate_llm_cost(...)` — estimate priced (media) generation before high-volume runs.

### Installing / authorizing providers

- Provider-manifest models are directly selectable. A saved configuration is needed only when the same exact provider/model/options combination should be named and reused.
- To add credentials for a provider, use `set_provider_auth(provider, api_key?, region?, endpoint?, ...)` — never paste API keys into shell or config files.
- Provider-backed **media** capabilities (image/video/audio/text generation, transcription, web search) are a separate surface with their own provider/model contracts and discovery — see `get_reference_doc(kind="workspace-media-tools")`. This doc covers the LLM that *executes agent steps*, not media generation.
