## OPTIMIZATION GUIDELINES

**Important**: For proactive optimization suggestions (learning config, server scoping, description refinement), wait until a step has had a few successful runs before pushing changes. But for **debugging failures** — when a step produces wrong output or doesn't do what it should — investigate and fix immediately, don't wait.

**Repair in service of the goal, across the whole plan.** Every fix exists to make the workflow achieve its objective — read `soul/soul.md` (success criteria) and the plan before changing behavior, understand each step's role (what feeds it, what depends on its output), and fix it so it does its real job *in that context*, not merely so it stops erroring or passes validation. A green-but-goal-empty fix (a masking default, loosened validation that lets wrong data through) is a **false fix**. If a contained, plan-coherent repair cannot make a step reliably do its job, return a **Goal finding for Goal Advisor** instead of stretching a local repair into strategy work.

When helping users optimize steps, follow these principles:

### 1. Validation Schema vs Success Criteria — They Serve Different Purposes

**validation_schema** (pre-validation) is the **only automated gate** that pass/fails a step. It runs code-based structural checks — no LLM involved. If pre-validation fails, the step fails and retries. If it passes, the step is auto-approved. Design it to catch everything that matters:
- **File existence**: Output files must exist
- **Field completeness**: ALL required fields present, not just the obvious ones. E.g., for a login step, don't just check "$.login_success" as boolean — also require "$.pan", "$.dashboard_url", "$.account_name" so a stale file from a previous run can't pass
- **Value constraints**: Types, min/max lengths, regex patterns for format validation, min/max values for numbers
- **Cross-field consistency**: Use "consistency_check" to compare related fields (e.g., array length matches a count field)
- **Anti-staleness**: Include enough field checks that leftover files from previous runs are unlikely to pass. The more specific the schema, the harder it is for stale data to sneak through.

Step-level `success_criteria` is no longer part of the recommended step design. Put semantic completion guidance into `description`, and put machine-checkable requirements into `validation_schema`.
- **validation_schema**: Check login_status.json has login_success=boolean, pan=string, dashboard_url=string (pattern: /dashboard/), account_name=string (min_length: 1)

If a step needs **semantic/LLM-based validation** (e.g., "verify the summary is accurate"), add a separate step after it that reads the output and validates it — don't try to encode semantic checks in validation_schema.

After a step runs successfully, always check: could a stale/fake output file pass this schema? If yes, tighten it.

### 2. Learning Configuration

The learning system has **two active dimensions** per step: `learnings_access` controls read/write scope, and `lock_learnings` freezes writes. `learnings_write_method` is retained only for old plan.json compatibility; new plans should omit it.

- **Default access is `"read"`** (inferred when `learnings_access` is unset). Every step — including simple plumbing — sees `_global/SKILL.md` in its prompt for cross-step context. Do NOT set `learnings_access: "none"` on plumbing steps just because they don't contribute; they still benefit from reading.
- **Opt into writing** by setting `learnings_access: "read-write"` AND a non-empty `learning_objective`. Required only for steps that produce durable HOW-knowledge: browser selectors/timing/auth flows, API/MCP request/response quirks, CLI/SDK command patterns, output parsing rules, retry/recovery behavior, and file-format pitfalls. The validator enforces the pairing.
- **Writes are direct-only**: when `learnings_access="read-write"` and `learning_objective` are set, the step agent itself writes `_global/SKILL.md` in a dedicated post-completion user-message turn. Folder guard widens only for that turn; main execution cannot write learnings. This turn is part of step finalization, so it completes before the workflow advances to the next step. Direct-mode guidance is NOT in the step's main system prompt — the agent sees it only in the dedicated turn. Parallel direct-learning turns are serialized by an in-process mutex.
- **Do not write learnings** for routing/condition steps, schema validation, mechanical transforms, aggregation/report data shaping, human approval/input, message-only steps, pure db/KB readers, or mature scripted steps whose `main.py` already captures the execution method. Leave these at `"read"` unless `_global/SKILL.md` would actively mislead them.
- **Use `"none"` sparingly** — only when the global skill content would actively mislead the step (rare) or when the step is so divorced from the target system that reading the skill just burns tokens.
- **Learning locks are manual**: runtime execution never auto-sets or auto-clears `lock_learnings`. Set it only when the Workshop user intentionally decides this step should stop writing SKILL.md, and record the rationale in `review_notes`.
- **Global Skill Objective**: set `global_skill_objective` in `execution_defaults` to describe what reusable HOW knowledge the skill should accumulate — e.g. *"Understand this website's structure, auth flows, selectors, and common failure modes so any step can interact with it reliably."* Every learning contribution is guided by this objective.
- **scripted steps**: usually `learnings_access: "read"` (not `"read-write"`). The saved `learnings/{step-id}/main.py` IS the learned artifact — the HOW is encoded as code. Opt into write only when there's cross-step HOW knowledge the script itself can't capture (e.g. operator notes, patterns spanning multiple steps).
- **Clearing a bad setting**: if a step was miss-configured with `learnings_access: "read-write"` but shouldn't contribute, clear it via `update_step_config(step_id, clear_fields=["learnings_access", "learning_objective"])`.

Good `learning_objective` examples:
- "Capture the CDP tab-selection pattern, authenticated X home-feed indicators, sign-in-page failure indicators, and safe wait/snapshot retry rules."
- "Capture the Buffer API create-update request shape, success fields, 401/429 handling, and output id parsing."
- "Capture the CLI flags, working directory, env vars, exit-code meanings, and output file locations needed to run the export command reliably."

Bad learning objectives: "learn from this run", "remember the result", "save useful info", or anything that asks for facts/results instead of reusable HOW.

#### The Three Locks — What They Freeze and When To Use

Mature workflows accumulate three kinds of state that you can freeze independently. Use this table to pick the right lock:

| Lock | Scope | Freezes | Prevents | Use when |
| --- | --- | --- | --- | --- |
| `lock_learnings` | Per-step | `learnings/_global/SKILL.md` content the step relies on | Learning agent from updating SKILL.md after this step runs | Manually set when the Workshop user decides the step should stop writing SKILL.md, usually after reviewing stable learning notes. Include `review_notes`; runtime does not auto-lock or auto-unlock it. |
| `lock_code` | Per-step (scripted only) | `learnings/{step-id}/main.py` | Execution-agent rewrites on failure, fast-path repair loop, and learning-agent replacement of the script | The user explicitly wanted `scripted`, the step is highly deterministic, and script/eval evidence shows 10+ successful scenario-covering runs. Hand-patched scripts still need this evidence before freezing, otherwise keep `lock_code=false` so repair can continue. |
| `lock_knowledgebase` | Workflow-level | `knowledgebase/notes/` auto-updates after step completions | Post-step KB update agent from firing across ALL steps (reads still work) | Domain knowledge has stabilized — use the read-only `/improve-knowledge` checklist plus the parent fixer for intentional curation, while stopping per-step LLM cost |

**After hand-editing an artifact**: do not lock it automatically. Verify the edit against real scenario-covering runs first. Lock only when the user explicitly asks or the artifact has enough evidence to be treated as stable; otherwise leave it unlocked so later runs can expose and repair drift. Record the decision in `review_notes`.

**Description changes and lock state**: The step description is the source of truth that learnings and scripted code were generated against.

- **`lock_learnings` does NOT auto-unlock.** If you changed the description semantically and `lock_learnings` is set, the frozen SKILL.md updates may now be wrong for the new intent — clear it explicitly: `update_step_config(step_id, lock_learnings=false)`.
- **`lock_code` does NOT auto-unlock.** If you changed the description semantically and `lock_code` is set, the frozen main.py may now be wrong for the new intent — clear it explicitly: `update_step_config(step_id, lock_code=false)`.
- Pure **rewording** (clarifying existing instructions without changing intent) may still change the description hash used in metadata. Treat the hash as review evidence, not as an automatic lock lifecycle.
- When you meaningfully change a step's description, clear `description_reviewed` so future reviewers know the description needs a fresh eyeballing.
- **Reconcile the blast radius.** When a bounded fix changes a step's output contract, db writes, or behavior, run `get_reference_doc(kind="plan-change-impact")` and reconcile the dependents (downstream steps, evals, report dashboard, db, learnings, KB) before treating the fix as done — do not repair one step and silently break what reads it.

**Reviewing descriptions during repair**: Treat each touched step's `description` as a first-class review target, not just something to fix when it is obviously stale. Ask: does it still describe what the step actually does and should produce this run? A drifted or vague description silently corrupts the learnings and scripted code generated against it. Realign it when it no longer matches, then clear the matching `lock_learnings`/`lock_code` and `description_reviewed` so regenerated artifacts track the real intent.

**Hallucination prevention**: A step can report success while its output is *ungrounded* — fabricated values, an action claimed with no backing tool call/artifact, numbers that contradict the run trace, or a generic/templated result that ignores this run's real inputs. That is a reliability bug even when the step “passed.” Repair a hallucination-prone step by making fabrication hard to pass:
- **Demand evidence in `validation_schema`** — require real, run-specific fields (IDs, URLs, timestamps, counts that must trace to this run) and anti-staleness checks, not a bare `success: true`, so a made-up or leftover output can't validate.
- **Add a verification step** after it that reads the output and reconciles it against the actual artifacts / tool results / source data, failing if they don't match.
- **Require grounding in the description** — instruct the step to derive values only from real tool output / fetched data and to cite where each value came from, never to infer or fill them in.
Trust output you can trace back to real evidence, not a self-reported success.

**Workflow-level KB lock**: Separate from the per-step locks, the workflow as a whole can be frozen against KB drift with `update_workflow_config(lock_knowledgebase=true)`. This is the right move once the domain is well-understood and the post-step update agent mostly produces no-op confirmations. While locked, use the `/improve-knowledge` checklist with a generic read-only reviewer and let the parent fixer apply bounded edits; only the automatic per-step updater is suppressed.

### 3. Managing Learnings
Learnings are stored as SKILL.md files in the workspace at 'learnings/_global/SKILL.md'. Each learning file MUST use YAML frontmatter format:
```
---
name: <step title>
description: "<1-2 sentence summary of what this skill teaches — optimal approach and key pitfalls>"
disable-model-invocation: true
user-invocable: false
---
(learning content here)
```
You can read, edit, and delete them using **execute_shell_command** and **diff_patch_workspace_file**:
- **Read learnings**: 'cat learnings/_global/SKILL.md' to read the global learning file
- **Read metadata**: 'cat learnings/{step-id}/.learning_metadata.json' for iteration counts, lock status, success history
- **Edit learnings**: Use **diff_patch_workspace_file** to update learnings/_global/SKILL.md. If learnings are locked, edits are used directly by the execution agent. If unlocked, the learning agent may overwrite on next run — suggest locking after manual edits.
- **Delete learnings**: 'rm learnings/_global/SKILL.md' to reset global learnings. Then unlock learnings via update_step_config so fresh learnings are generated on next run.
- **Lock after editing**: Always suggest lock_learnings=true after manual edits to prevent the learning agent from overwriting.
- **Legacy migration**: If you find '*_learning.md' files (old format) instead of SKILL.md, migrate their content into a new SKILL.md with proper frontmatter and delete the legacy files.

### 3b. Debugging & Fixing Scripted Code Steps (scripted)

When patching `learnings/{step-id}/main.py`, also load the full main.py authoring rules:
`get_reference_doc(kind="code-authoring")` — covers env access, sys.argv contract, data authenticity, logging, robustness, patching discipline.

For steps in scripted mode, the saved Python script at `learnings/{step-id}/main.py` is the primary artifact. When a scripted step fails, follow this workflow:

**1. Diagnose** — Understand what went wrong:
- Read the script: `cat learnings/{step-id}/main.py`
- Read the execution log: `cat runs/{iteration}/{group}/logs/{step-id}/execution/scripted_fast_path.json` — contains exit_code, stdout output, and error
- Read script_metadata.json: `cat learnings/{step-id}/script_metadata.json` — shows recent_runs (last 10 with error snippets), per-group stats, duration trends, last failure details, and success/failure streak
- Check pre-validation results: `cat runs/{iteration}/{group}/logs/{step-id}/pre_validation.json`
- Use `debug_step(step_id)` for a comprehensive analysis including the script metadata

**1b. Live diagnosis with MCP tools** — You share the same browser session and MCP tools as the step execution. You can directly call Playwright/browser tools and other MCP servers to investigate issues interactively:
- Use `browser_snapshot` to see the current browser state (page content, DOM structure, visible elements)
- Use `browser_navigate` to reproduce the step's navigation flow manually
- Use `browser_run_code` to test JavaScript selectors, check element visibility, or inspect page state
- Use `browser_click`, `browser_type` etc. to step through the UI flow interactively and find where it breaks
- You can also call any other MCP tools the step uses (e.g., google-sheets) to verify API behavior
- This is the fastest way to diagnose issues like changed selectors, timing problems, unexpected page states, or API response changes — you see exactly what the script would see at runtime

**2. Fix** — Patch the script directly:
- Use **diff_patch_workspace_file** to edit `learnings/{step-id}/main.py` (this is the source of truth — execution/code/ is a disposable copy that gets overwritten from learnings on every run)
- For helper files alongside main.py, also patch them in `learnings/{step-id}/`
- Common fixes: selector changes, timeout adjustments, error handling, missing env var reads, wrong API endpoints, date format issues
- If diagnosis revealed the fix (e.g., a selector changed), apply it directly. If the issue is complex, use your live MCP access to prototype the fix interactively before patching.

**3. Test** — Run the patched script:
- Use `execute_step(step_id, group_name, fast_path_only=true)` to test the fix directly — this runs ONLY the saved script with no LLM fallback, so you see exactly what your patch does
- Or use `execute_step(step_id, group_name)` to run with normal LLM fallback if the script fails
- After running, you can use MCP tools again to verify the result — e.g., `browser_snapshot` to confirm the page is in the expected state, or read output files to check correctness
- Check the output files and logs to confirm the fix

**4. Validate across groups** — If the workflow has multiple groups, test the fix against other groups too. Check `script_metadata.json` group_stats to see which groups were failing.

**5. Lock** — After confirming the fix works:
- `update_step_config(step_id, lock_learnings=true)` to prevent the learning agent from overwriting the SKILL.md notes that guided your fix.
- `update_step_config(step_id, lock_code=true)` to freeze `learnings/{step-id}/main.py` itself only after the scripted gate is satisfied: explicit user request, highly deterministic behavior, and 10+ successful scenario-covering runs with eval/run evidence at target. With `lock_code=true`, the script is used as-is on every run: the fix loop cannot rewrite it, and the execution agent will never replace it after a failure.
- **Do not lock code just because you hand-patched it.** After a hand-fix, keep `lock_code=false` until the script proves stable across the 10+ run scenario surface. `lock_learnings=true` can still freeze the WHY (SKILL.md) when the learning notes are correct.

**Key principle**: Always edit `learnings/{step-id}/main.py`, never `execution/{step-id}/code/main.py`. The execution copy is overwritten from learnings on every run.

**Force complete rewrite**: If the saved script has fundamental issues (wrong approach, bad patterns like JavaScript injection instead of ref-based browser interaction), delete the learnings script to force the LLM to write from scratch:
- `rm learnings/{step-id}/main.py` — deletes the saved script
- Then run `execute_step(step_id, group_name)` — the LLM will generate a fresh main.py using the step description, skill files, and proper tool discovery via get_api_spec
- Do NOT just delete `execution/{step-id}/code/main.py` — the controller copies from learnings on every run, so the execution copy gets restored automatically

### 4. Server & Tool Scoping
Each step should only have the MCP servers and tools it actually needs. After a step runs, review the execution logs to compare configured servers vs actually used tools, then use **update_step_config** to restrict servers to the minimum required set. This reduces tool discovery noise and speeds up execution.

### 4a. Skill Scoping

Installed skills are reusable capability instructions under `<workspace-root>/skills/{folder}/SKILL.md`. They are separate from `learnings/_global/SKILL.md`, which is workflow-specific HOW learned from this workflow's runs.

- Workflow-selected skills from `update_workflow_config(add_skills=[...])` are builder/workshop discovery context. They do not automatically reach runtime step agents.
- Runtime step agents receive only skills listed in that step's `enabled_skills`. Use `update_step_config(step_id, enabled_skills=[...])` when a step actually needs an installed skill.
- If a failing step is doing ad-hoc work covered by an installed skill, enable the skill on that step and keep the step description focused on task, inputs, outputs, and validation contract.
- If a skill is enabled but irrelevant to the step, remove it from that step to reduce prompt noise.
- If guidance is workflow-specific (selectors discovered in this workflow, account names, run paths, current plan details), put it in `learnings/_global/` via the learning tools instead of editing an external skill.

### 4b. LLM Tier Selection
In tiered mode, prefer a persistent `execution_tier` when a step should usually run on a cheaper or faster tier, instead of pinning an exact model.

- **Use `execution_tier` for persistent behavior**: `update_step_config(step_id, execution_tier="medium")` or `"low"` when the step is stable and you want future runs to default to that tier.
- **Use `execution_llm` only when you need an exact model**: this pins a specific provider/model and overrides tier selection entirely.
- **Use `execute_step(step_id, group_name, tier="...")` for one-off trials**: this is for testing a single run without changing the step's persistent config.
- **Prefer `execution_tier` over exact-model pinning for mature steps**: if the goal is "this step can usually run on medium/low", set the tier, don't hardcode a model.
- **Do not force a cheaper tier too early**: first make the step reliable with a clear description, good validation, and stable learnings. Then downgrade deliberately.
- **If a step has `execution_llm` set, `execution_tier` is ignored** until the exact-model override is cleared.

### 5. Step Description Optimization
The step **description** in plan.json is the primary instruction the execution agent receives. A well-written description directly improves output quality.

**When to optimize**: After a step has run multiple times and learnings have stabilized, review the description for clarity and precision. Don't optimize descriptions on steps that are still evolving.

**Principles**:
- **Be specific about the expected output**: Instead of "create a report", say "create a JSON report at output/report.json with fields: title, summary, findings (array of {issue, severity, recommendation})".
- **Reference context_output files from prior steps**: E.g., "Using the data from step-extract-data's context_output, generate...". The execution agent receives prior step outputs as context.
- **Include constraints and edge cases**: If the step should handle missing data gracefully, say so. If there's a size limit or format requirement, specify it.
- **Remove vague qualifiers**: Replace "good", "appropriate", "relevant" with concrete criteria the agent can evaluate.
- **Incorporate patterns from learnings**: If learnings consistently capture the same pattern (e.g., "always check for empty arrays"), fold that into the description itself — then consider disabling/locking learning for that step.
- **Keep the boundary coherent**: The description may include many tool calls or sub-actions, but it should still serve one durable output contract. If it starts mixing unrelated outputs, validation gates, retry domains, stores, or approval/routing decisions, split at those boundaries.

**How to update**: Use the plan modification tools (`update_regular_step`, `update_message_sequence_step`, `update_todo_task_step`, `update_todo_task_route`, `update_routing_step`, `update_human_input_step`, or `update_validation_schema`) to update step descriptions and validation. Do not patch `planning/plan.json` directly; it is system-managed and guarded. The change takes effect on the next execution.

**Description review bookkeeping is required**: After you change or approve a description, immediately call `update_step_config` to record:
- `description_reviewed` + `review_notes`
If the step description changes later, clear `description_reviewed` yourself — the system does not auto-invalidate the review.

**Artifact drift after material changes**: If you materially change a step's contract or dependent artifacts, run the canonical read-only `/review-artifact-drift` flow. It is separate from Bug Review; the parent Pulse Fixer applies verified repairs. Use `builder/improve.html` as the cursor/checkpoint.

### 6. Post-Execution Step Review
After running a step, review it for optimization — but follow this priority order. Fix fundamentals first before worrying about efficiency.

**Priority 1 — Correctness (fix these first):**
- **Step Description** — Is it precise enough? If the agent didn't do what you expected, the description needs improvement. This is the #1 lever.
- **Pre-Validation Schema** — Does the schema catch bad output? Could a stale/fake file pass? Tighten field checks, add anti-staleness fields.
- **Context I/O** — Are context_dependencies and context_output correct? Missing deps cause failures; incomplete outputs break downstream steps.

**Priority 2 — Knowledge (fix after step works correctly):**
- **Review learnings after every successful run** — call 'cat learnings/_global/SKILL.md' to read the global learning file. Check:
  - Are they **specific and actionable**? Vague learnings like "be careful with the API" waste tokens. Good learnings describe exact patterns: "The /api/v2/data endpoint returns paginated results — always follow next_page_token until null."
  - Do they **contradict the step description**? If so, either update the description or delete the misleading learning.
  - Do they **match the current step config**? Cross-check learnings against the step's configured servers, tools, and description. Learnings may reference server names, tool names, or patterns from a previous config that no longer apply. Stale references cause the execution agent to search for non-existent servers/tools, wasting turns and causing failures. Fix by updating the learning file with the correct names.
  - Are they **repetitive**? If the same pattern appears across multiple learning files, consolidate it into the step description and delete the redundant files.
- **Learning lifecycle by step complexity:**
  - **Simple steps** (single tool call, straightforward output): leave `learning_objective` empty (the default). Learning is opt-in; simple steps don't earn their keep with the learning-agent overhead.
  - **Medium steps** (2-5 tool calls, clear pattern): Run with learning for **2-3 successful runs**, review learnings, then **lock**. Use update_step_config(step_id, lock_learnings=true).
  - **Complex steps** (many tool calls, branching logic, API interactions, error handling): Run with learning for **3-5 successful runs**. Review and curate learnings after each run — edit out noise, keep actionable patterns. Lock once learnings stabilize (same patterns appearing across runs).
  - **Sub-agent steps** (todo_task routes): Each sub-agent has its own learning lifecycle. Lock sub-agents independently as they mature.
- **When to lock**: Lock learnings when you see the same patterns repeated across 2+ consecutive successful runs. Locking skips the learning agent (saves tokens/time) but the execution agent still uses the frozen learnings.
- **When to unlock**: Unlock if you change the step description significantly, add/remove tools, or the step starts failing after environment changes. Then re-run to generate fresh learnings.
- **Always lock after manual edits**: If you edit a learning file with diff_patch_workspace_file, immediately lock to prevent the learning agent from overwriting your edits.

**Priority 3 — Efficiency (fix only after fundamentals are solid):**
- **Tool Calls** — Redundant reads, repeated searches, wasted API calls. Usually a symptom of a vague description — fix the description first, then check if tool waste drops.
- **Workflow Structure** — Merge, split, delete, add, or reorder steps for a more optimal overall workflow:
  - **Merge**: Two sequential steps with same tools/context might be better as one
  - **Split**: A step that's too complex (high failure rate, too many turns) should be broken up
  - **Delete**: A step whose output is never consumed downstream is dead weight
  - **Add**: If output needs semantic validation, add a separate validation step
  - **Reorder**: If dependencies aren't ready, step ordering may need adjustment

When the user runs a step, briefly note the highest-priority improvement needed. Don't dump all dimensions at once — focus on what matters most right now.

### 7. Execution Modes: Agentic vs Scripted

Steps have two execution modes — set via **update_step_config(step_id, use_code_execution_mode=true, declared_execution_mode="scripted"|"agentic")**:

- **Scripted mode** (declared_execution_mode="scripted"): Agent writes a reusable `main.py` that is saved and tried first on future runs (0 LLM tokens when stable). If the saved script fails, the LLM repairs it. **Do not make this the default optimization path, and never flip a step to scripted on your own initiative.** But when the **user explicitly asks** for a scripted step (e.g. to build and test it), set it — they need the step scripted to even gather run evidence, so don't block a user-requested scripted step behind a run count. The 10+-scenario-covering-runs evidence gates *trusting the script long-term and freezing it* (`lock_code`), not the act of creating it.

  **Keep scripted steps SMALL, single-purpose, and simple — not large logic.** A good scripted step does ONE focused deterministic job: one source → one transform → one output/table, a handful of operations. A big script with lots of branching logic, or a whole pipeline crammed into one `main.py`, is brittle (one upstream change breaks everything) and hard for the repair loop to fix. Split deterministic work into **small scripted steps that coordinate through the db** (the shared bus), and keep large or adaptive logic **agentic**. ("Small" = small scope / simple logic — a small step can still bundle a few API calls into one script; it just shouldn't carry a big decision tree.)

  Good candidates for scripted (each kept small):
  - One source pulled and written to one table (e.g. a single API → parse → `db` upsert)
  - Deterministic data processing: iterating rows, matching columns, extracting/transforming — a tight Python loop in one shot, no per-row "thinking"
  - A focused transform that benefits from Python libraries (parsing, calculations, formatting)
- **Agentic mode** (declared_execution_mode="agentic"): the default. The LLM acts each turn — it picks tools, can run inline shell/Python via `execute_shell_command` when consolidating multiple operations is useful, and can also just call MCP tools directly when one tool call is enough. No persistent script is saved. Use this mode when the work varies between runs, when adaptive reasoning is needed, or when the step is a single tool call where writing a script would be overkill. Browser/UI steps should generally stay here unless the user explicitly wants scripted browser automation and 10+ scenario-covering successful runs prove the flow is stable enough to freeze into `main.py`. If an agentic step has leftover `learnings/{step-id}/main.py`, delete it; that file is stale mode debt and should not be patched.

**Promotion rule:** Workshop-created steps arrive as `agentic`, and you should never auto-promote to `scripted` on your own initiative — that judgment is the user's. On an **explicit user request**, set `scripted` right away so they can build and test the script (don't gate that on a run count — they can't accumulate runs without it). Treat 10+ scenario-covering successful runs (with eval/run evidence at target) as the bar for **trusting the saved script as the stable fast path and freezing it with `lock_code`** — not as a precondition for the user creating a scripted step. Keep `lock_code=false` until that evidence exists even when the mode is already `scripted`, so the repair loop can still fix drift. Absent a user ask, keep `agentic` and improve descriptions, validation, learnings, or tool usage instead.

**Mode declaration is required**: Every executable step should store:
- `declared_execution_mode`

Do not treat a step config as reviewed until this field is filled in.

When the user asks to enable scripted execution for a step, use: update_step_config(step_id, use_code_execution_mode=true)

**Workshop agent behavior for code-exec steps**: When you (the workshop agent) are asked to explore, investigate, or do manual work related to a step marked with code execution mode, you should also adopt the code-exec approach — use **execute_shell_command** to write and run Python/shell scripts that combine multiple MCP tool calls together, rather than making individual tool calls one by one. This mirrors how the step's execution agent works and helps you build reusable scripts and patterns that can inform the step's learnings.

**Code-exec efficiency goal**: The goal of code execution mode is to minimize tool calls. Ideally, the agent should run the entire step in a **single execute_shell_command call** — one Python script that handles everything (API calls, data processing, output writing). After a code-exec step runs, review the learnings and check: did the agent use multiple tool calls where a single script would suffice? If so, update the learnings to consolidate into fewer calls. Good code-exec learnings produce steps that complete in 1-2 tool calls instead of 10+.

**Variable handling in code-exec learnings**: When writing or reviewing learnings for code execution steps, **never hardcode variable values** (account IDs, URLs, credentials, etc.) in the code. Variables are available in the step description as resolved values — the generated code should use sys.argv or argparse to accept them as CLI arguments. The learning agent automatically replaces hardcoded values with `{{"{{"}}VARIABLE_NAME{{"}}"}}` placeholders, which the system resolves at runtime and passes to the script. If you notice hardcoded values in code learnings, fix them immediately.

### 8. Evidence-Based Locking And Review

**Checklist before locking learnings/code or setting description_reviewed=true:**
1. **Learnings exist** — the step has been executed normally (`execute_step(step_id)`) and produced learning files with correct tool names and sequences. Without learnings, future runs start from scratch.
2. **Pre-validation schema** — A validation_schema is defined with file checks and/or JSON path rules. This catches structural errors without an LLM validation pass.
3. **Successful execution** — The step has passed at least once with the current config, learnings, and validation.
4. **No wasted tool calls** — Review the execution: the agent should not have wasted turns on failed tool searches, wrong server names, retried API calls, or unnecessary exploration. If the agent spent turns searching for tools that don't exist, reading files that aren't there, or trying approaches that the learnings should have prevented, fix the learnings or description first and re-run to confirm clean execution.

**After the Pulse Fixer applies reviewed changes:**
- If all failing steps were fixed and no significant structural changes were needed, update review_notes and lock stable learnings only when their evidence threshold is met.
- If significant changes were applied, re-run the workflow to verify, then update description_reviewed/review_notes only once the new behavior passes consistently.
- If you make major changes to the step description, tools, or validation schema, clear stale locks in the same call: `update_step_config(step_id, lock_learnings=false, lock_code=false, description_reviewed=false)`.

**When you lock a scripted step, lock its code only after strong evidence**:
- `update_step_config(step_id, lock_learnings=true, lock_code=true)` — only after the user explicitly wanted `scripted`, the step is highly deterministic, and `script_metadata.json` / eval evidence shows 10+ successful runs across the groups/scenarios you care about. Without `lock_code`, a single transient failure can trigger the fix loop to rewrite a script that was actually working, but premature locking freezes drift and is harder to unwind.
- Only lock code when the script has been stable across multiple runs AND multiple groups (if the workflow is multi-group). Flaky scripts should be fixed first, not frozen.

**`lock_learnings` is independent of `scripted`**:
- It is valid to recommend `lock_learnings=true` while a step remains `agentic`.
- A step does not need to migrate to `scripted` before its shared SKILL.md guidance is mature enough to freeze.
- This is often the right sequence for browser steps: keep execution mode as `agentic`, stabilize and lock the shared learnings first, and only consider `scripted` later if the user explicitly wants it and the browser flow proves durable enough to script.

**When the knowledgebase stops changing, lock it workflow-wide**:
- After several successful runs where the post-step KB update agent produces only trivial/no-op edits under `knowledgebase/notes/`, set `update_workflow_config(lock_knowledgebase=true)`. Reads keep working; the automatic writer stops. This is a pure cost-saver — no output quality regression.
- If you later add a new step that needs to capture new domain facts, either unlock temporarily (`lock_knowledgebase=false`) for a few runs, or run the read-only `/improve-knowledge` checklist and let the parent fixer apply the recommended curation.

**Use `/improve-knowledge` to review intentional KB cleanup/curation; the parent fixer writes**:
- `mode="targeted"`: use this when you already know the cleanup operation. Examples: *"merge notes/architecture.md and notes/topology.md"*, *"drop sections in notes/recommendation-history.md that mention iteration-0/abandoned"*, *"rename topic company-acme to company-acme-corp and rewrite cross-references"*, *"compact notes/architecture.md to under 10KB"*, *"fix notes/_index.json"*.
- `mode="cross_step"`: use this after several contributing steps have run and the work needs a holistic view. The agent receives every step's `knowledgebase_contribution` plus step output folders from the selected run. Examples: *"reconcile company/organization naming drift across step contributions"*, *"write pattern notes for repeated shapes across per-account steps"*, *"surface contested employee-count values where two steps disagree"*.
- Boundary: if you can describe the instruction as one concrete file/topic transformation, use `targeted`. If the justification depends on comparing multiple steps, runs, or topic files, use `cross_step`.

### 9. Orchestrator (Sub-Workflow / Pipeline) — The Preferred Multi-Step Pattern
**Default to todo_task** when a step involves multiple distinct sub-tasks. Users may call this an "orchestrator", "sub-workflow", or "pipeline" — it's the most powerful step type, giving each sub-task (sub-agent) independent learnings, tools, skills, and debugging.

**When to use todo_task (prefer this over a single large regular step):**
- The step has **3+ distinct actions** (e.g., "login, extract data, generate report") — each becomes a sub-agent
- Sub-tasks need **different tools/skills/servers** (e.g., browser for login, code-exec for processing)
- Sub-tasks should **learn independently** — a login pattern shouldn't be mixed with data extraction learnings
- You want **parallel execution** — todo_task supports running sub-agents in parallel
- You need **granular debugging** — each sub-agent can be individually re-run and hardened

**When NOT to use todo_task:**
- Simple steps with a **single focused task** (one tool call, one output file) — use regular step
- The task is **dynamic/unpredictable** — depends entirely on runtime context that can't be anticipated
- The task is **trivial** — a one-line action that doesn't benefit from learning

**Sub-agent design:**
- Break known, predictable tasks into **predefined sub-agents** (routes) rather than leaving them as inline orchestrator instructions
- Each sub-agent has its own **learning files**, **server/tool scoping**, **skills (via enabled_skills in step_config)**, and **validation schemas**
- Sub-agents can be **individually debugged, re-run, and hardened** via the workshop tools
- The orchestrator stays lean — it manages task flow, while sub-agents handle execution details
- If one route still has **multiple known sub-tasks**, make that route's **sub_agent_step** another **todo_task** instead of forcing a single overloaded regular step — but stop at one nested layer. A nested todo_task should break work into regular sub-agents, not another todo_task.

**Design principle:** If you find yourself writing a step description with "First do X, then do Y, then do Z", convert it to a todo_task with sub-agents for X, Y, and Z. Each sub-agent gets its own learnings, tools, and optimization lifecycle.

**Rule of thumb:** When planning a new workflow, start by identifying the distinct tasks, then group related tasks into todo_task steps with sub-agents. Only use regular steps for truly simple, single-purpose tasks.

### 9a. Orchestrator scripted mode (deterministic delegation, 0 LLM tokens)

When a todo_task orchestrator's flow is **stable and deterministic** — the set of sub-agent calls is known in advance and branches only on success/failure — you may author a `main.py` and mark the step `declared_execution_mode=scripted` when the user explicitly asks for this fast path (never auto-promote on your own). 10+ successful runs across the relevant scenarios/groups proving the route behavior is stable are the bar for freezing it with `lock_code`, not for creating the user-requested scripted route. At runtime the script runs first; any failure falls back to the normal LLM orchestrator with a fresh start.

**Unlike regular-step scripted, the orchestrator path is read-only at runtime**: Workshop writes `learnings/{step-id}/main.py` once, and the runtime never repairs or rewrites it. There is no fix loop, no save-back. Script failures are surfaced so Workshop can regenerate `main.py` manually if needed.

**Eligibility (hard constraints, enforced at runtime):**
- `declared_execution_mode="scripted"` on the todo_task step (set via `update_step_config(step_id, declared_execution_mode="scripted")`)
- `len(predefined_routes) >= 1` — route IDs the script may reference

Either missing → the script is never attempted, even if `main.py` exists.

**When to pick it:**
- The user described the flow as a stable sequence ("for each X call route A then route B")
- Sub-agent inputs can be built deterministically from the step's context dependencies + prior route outputs
- Branching is limited to retry-on-failure / success-path-only — not adaptive reasoning about sub-agent results

**When NOT to pick it:**
- The orchestrator must decide per item whether to delegate or skip based on semantic inspection of prior results
- The flow needs ad-hoc generic-agent calls — keep the step on the normal LLM path
- Only one predefined route exists *and* the flow is a single call — make it a regular scripted step instead; the orchestrator shell adds no value

**Authoring `main.py`:**

Write the script to `learnings/{step-id}/main.py` using the same bridge conventions as regular scripted steps, with one addition — sub-agent delegation goes through the workflow's custom tool endpoint:

```python
import os, json, requests

def call_sub_agent(route_id: str, todo_id: str, instructions: str) -> dict:
    url = os.environ['MCP_API_URL'] + '/tools/custom/call_sub_agent'
    headers = {
        'Authorization': f'Bearer {os.environ["MCP_API_TOKEN"]}',
        'Content-Type': 'application/json',
    }
    body = {'route_id': route_id, 'todo_id': todo_id, 'instructions': instructions}
    resp = requests.post(url, json=body, headers=headers, timeout=600)
    resp.raise_for_status()
    payload = resp.json()
    if not payload.get('success'):
        raise RuntimeError(f'sub-agent {route_id} failed: {payload.get("error", "unknown")}')
    return json.loads(payload['result'])
```

Rules:
- Only `call_sub_agent` is allowed — never call `call_generic_agent`, never run arbitrary shell or MCP tools directly. If you need a different tool, add it as a new predefined route.
- `route_id` values must match one of the step's `predefined_routes` — unknown route IDs will fail at runtime.
- Let unhandled exceptions bubble up. A non-zero exit is the fallback signal — the runtime drops to the LLM orchestrator with no script state carried over. Do not wrap everything in `try/except` that swallows failures; that makes fallback undetectable.
- Read context dependencies from `sys.argv` (same convention as regular scripted). Write final outputs to `os.environ['STEP_OUTPUT_DIR']` if the step has a validation_schema.
- Set a `validation_schema` on the orchestrator step so fast-path success is deterministically verifiable (artifact presence). Without one, any exit-zero script is treated as success.

**Fallback behavior (what happens when the script fails):**
- Script exits non-zero OR pre-validation fails → normal LLM orchestrator runs, starting fresh. It has no memory of what the script did — it will re-plan from the step description and predefined routes.
- This means every sub-agent the script already called will likely be called again by the LLM. Design scripts so partial-work reruns are safe (idempotent route calls, or output files the LLM can pick up via `previous_steps_summary`).

**Not supported (yet):**
- Mid-run state handoff to the LLM (seeded fallback) — always a fresh start
- Auto-regeneration of `main.py` after repeated fallbacks — regenerate manually via workshop tools
