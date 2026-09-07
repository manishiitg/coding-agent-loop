## Writing Optimized Step Descriptions (Prompt Engineering)

A step's `description` is the prompt an execution agent actually runs on. Every word costs context and dilutes the ones that matter. This is a different concern from step-type selection or validation design — it is about writing quality: does this description say exactly what is needed, once, and nothing else. Read this before writing or editing any step's `description`.

### Description defines WHAT; skills and learnings carry HOW

Write the description as an execution prompt focused on the step's objective: what result to achieve, which inputs or evidence to use, the scope and business rules, what success means, and where the result belongs. Keep binding constraints here, including approval requirements and actions outside the step's authority. These define the task even when the implementation changes.

Put reusable execution methods in the step's skills and learnings: tool usage, selectors, API/authentication sequences, troubleshooting, and techniques verified in prior runs. Reference the relevant guidance and configure the step's skill/learning access so it can actually read it. Do not assume a skill exists or that a new step has already learned a procedure; provide needed guidance there, or let the execution agent choose a method within the task's constraints and retain verified reusable know-how through the configured learning flow.

Keep the output structure and automated checks in `validation_schema`. Descriptions, skills/learnings, and schemas should complement one another without repeating the same content.

### Earn every word

Before finalizing a description, cut anything that:
- Restates what the model already knows from training, its system prompt, or an already-loaded skill — do not re-explain what JSON is, what "verify" means, or what a tool already documents about itself.
- Restates content already reachable through `context_dependencies`, `learnings_access`, `knowledgebase_access`, or managed DB tools — reference the store ("read `learnings/_global/SKILL.md` for the login selectors") instead of copying its contents inline.
- Repeats the same constraint from a different angle, hoping one phrasing lands — state it once, precisely.
- Hedges with qualifiers that add no checkable meaning ("carefully," "thoroughly," "properly," "make sure to") — state the actual verifiable criterion instead.

### Let `validation_schema` name the shape — don't restate it in prose

`validation_schema` is shown to the executing agent on its opening turn for every step type (`regular`, `message_sequence`, `orchestrator`) — not only reactively after a failed attempt. So once a step has a schema, the description does not need to also spell out the object's keys; doing so is exactly the duplication the earlier sections warn against, and the two copies drift the moment one is edited. Define the schema, then let the description state the outcome and any context the schema itself can't carry (why the field matters, where the source data comes from) — not the field list.

If an output needs a defined structure and no schema exists, add a light `validation_schema` or reference an accessible authoritative contract. Do not use the description as a substitute schema. Free-form output does not need an invented object structure.

### Keep `validation_schema` light — not exhaustive

Because the schema now renders into the prompt on every attempt, its size is a prompt-bloat cost like any other section, and an over-specified schema is a common failure mode in practice — it's easy to check everything the output happens to contain instead of only what actually matters. Validate the load-bearing contract points: does the file exist, do the handful of fields a downstream step, evaluator, or user genuinely depends on have the right shape and value. Do not add a check for every field just because the field exists, re-validate structure the model already got right by construction (e.g. re-checking a field it copied verbatim from an upstream file), or assert on free-form/optional content where a cosmetic variation is not actually wrong. Each check should answer "what real failure does this catch" — if the answer is "none, it's just thorough," cut it. A schema that mirrors the entire output document duplicates work the model already did and turns harmless variation into a spurious validation failure and retry.

### State the outcome, not a micromanaged procedure — unless the task genuinely needs one

Prefer: "Verify every listed API endpoint returns a 2xx status and its response body matches the declared schema; record failures in `api_failures.json`."

Over: "First, open the API testing tool. Then, for each endpoint, carefully send a request. Check the response. If it looks wrong, note it down. Make sure to be thorough."

The first names one precise, checkable outcome. The second spends four sentences restating the same instruction with decreasing specificity, and its vagueness ("looks wrong," "be thorough") gives the model nothing an evaluator could verify against.

When a fixed procedure is necessary (browser selectors, a multi-stage authentication flow, or an ordered API call chain), put it in a skill or learning and reference it. Keep any task-specific ordering constraint in the description when it defines correctness or authority—for example, approval must precede publishing—without copying the operational procedure.

### Do not duplicate across steps

If two steps in the same plan describe the same policy, contract, or procedure, that is not two descriptions — it is one description and a reference. Move the shared text to `learnings/_global/SKILL.md` (durable HOW-to-operate knowledge), the knowledgebase (domain facts), or a validation schema (structural contract), and have each step reference it by name. A copy-pasted paragraph across steps is a maintenance liability — a fix to one copy silently leaves the others stale — as much as it is a length problem.

### A rough self-check before finalizing

Could someone who has never seen the workflow read the description and understand the intended result, evidence, constraints, and success criteria? Do the separately supplied schema and referenced skills/learnings provide the output contract and execution guidance without contradictions? The description should not need to reconstruct the schema or teach the procedure. Could you delete a sentence without losing information the model needs? If yes, delete it — regardless of how short the description already is. Could you delete a check from `validation_schema` without losing the ability to catch a real failure? If yes, delete it too — a schema is a contract, not a transcript of the output.

### Related

- `references/plan-design.md` for step-type selection, context flow, and validation design.
- `references/workflow-patterns.md` for reusable multi-step shapes.
- The Standalone Technical Review's Prompt-contract health check (`/ops-review`) audits existing plans against these same principles after the fact. Applying them while authoring is cheaper than fixing a finding later.
