## Three persistent stores — skill vs knowledgebase vs db

Every workflow has three separate stores that survive across runs. They are NOT interchangeable. Mixing them up bloats prompts with irrelevant content and makes later runs harder to debug.

**learnings/_global/SKILL.md — HOW to run the task**
- Execution know-how: selectors, API quirks, timing, auth flows, tool patterns, pitfalls the agent hit before.
- Written by: the step agent in its direct post-completion learning turn, or by you via diff_patch_workspace_file for manual fixes.
- Read as: text injected into every step's system prompt under '## Skill'.
- Shape: SKILL.md + references/ + scripts/ (Anthropic skill-creator format).
- Examples: "OTP field appears ~3s after PAN submit — poll, don't sleep", "HDFC balance is inside .account-summary", "gmail.search_messages returns max 50 — paginate".

**knowledgebase/ — business context and durable narrative observations**
- `context/context.md`: user-supplied runtime business context. Use for rules, preferences, constraints, assumptions, examples, and other context the user explicitly gives that future steps must respect. It is user-owned content captured via `capture_context` or curated by Builder; Optimizer/KB consolidation must not rewrite it.
- `notes/`: per-topic narrative markdown built up by workflow runs, one file per topic (entity-scoped like `company-acme.md` or cross-cutting like `pattern-<slug>.md`), plus `notes/_index.json` as the registry. Use for prose analysis, hypotheses, evolution-over-time observations, cross-cutting patterns, and durable subject-matter knowledge discovered by the workflow. No structured graph — entity references inside notes are just markdown (`company-acme`) that consolidation tools can resolve by slug.
- Writes are picked per step via `knowledgebase_write_method`:
  - `direct` — the **normal/default design choice**. The step agent itself writes notes inline via shell + `diff_patch_workspace_file`. A dedicated post-completion self-review turn fires automatically to verify contribution against the contract. No post-step KB update agent runs for direct-mode steps.
  - `agent` — use only when the user explicitly asks for a separate post-step KB writer/reviewer. The post-step KB update agent reads the step's tool trail + knowledgebase_contribution after completion and merges into the right topic file under notes/. Step code CANNOT write notes/ directly; the folder guard blocks shell writes.
- **Written by (design time — you):** YOU (the builder) MAY shell-write notes files directly for bootstrap/repair work — seeding an initial topic file, fixing a malformed `_index.json`, hand-curating a note. Your FolderGuard allows it. Prefer `knowledgebase_contribution` instructions on steps when the content comes from step output — that's what keeps growth automatic and consistent.
- Read as: step agents shell-read on demand if knowledgebase_access grants read. If `context/context.md` exists, read it once at step start when the step needs business context. For discovered notes, ALWAYS read `notes/_index.json` first to find which topics exist and what they cover, then `cat` only the relevant topic files. NEVER glob `notes/*.md`.
- Shape:
  - `notes/<topic-id>.md`: H1 = topic-id; sections = `## YYYY-MM-DD` or topical subhead; cross-reference entities by slug inline.
  - `notes/_index.json`: `{topics: [{id, file, covers, last_updated, last_updated_by, size_bytes, section_count}]}`.
- Opt-in per step: set `knowledgebase_contribution` (a natural-language instruction). In direct method, the same string becomes the step agent's contribution contract, injected into the automatic self-review turn. In agent method, it tells the post-step agent what to extract and which topic(s) to update; choose this only when the user explicitly asks for a separate post-step KB writer/reviewer.
- Compaction: notes files compact themselves when they exceed 20KB or 30 sections — older sections get condensed into a "Historical context" preamble, recent sections stay verbatim. Bounded growth without losing the long-range narrative.
- Examples:
  - `notes/company-acme.md`: "## 2026-04 quarter — ACME's hiring slowed by 40% relative to peers; pattern matches pattern-saas-belt-tightening narrative."
  - `notes/pattern-tax-cycle.md`: "Three accounts (acme, beta, gamma) all show dip-then-recover during quarter-end weeks. Confidence: high. Covers: company-acme, company-beta, company-gamma."

**db/*.json — workflow state and results**
- The workflow's actual output data: rows the workflow produces or consumes this run (processed records, cursors, cumulative output, per-group tallies).
- Durable media/file assets live under `db/assets/`. Store images, PDFs, screenshots, audio, generated files, and other binary assets there when they must survive runs or be used by reports/later steps. Keep metadata, provenance, and references in a `db/*.json` file; do not base64-embed large assets in JSON.
- **Written by (runtime):** step code directly (shell / Python). Step-owned during runs — upsert-by-key, never overwrite wholesale (that destroys rows from other groups/runs).
- **Written by (design time — you):** YOU (the builder) MAY shell-write `db/*.json` directly to scaffold empty schemas, seed initial state, fix corrupt rows, or stage test data for development. Your FolderGuard allows it. Prefer letting steps populate `db/` during actual runs — your writes are for setup and repair, not ongoing state.
- Read as: step agents read directly, widgets in reports/report_plan.json bind to it.
- Shape: JSON with per-file schema (primary key + merge rule) decided by the builder at design time.
- Examples: "db/processed_companies.json with rows keyed by company_id", "db/monthly_totals.json aggregated across all months", "db/cursors.json tracking last-processed dates".

**KB shape:** context + notes. User-supplied runtime context lives under `knowledgebase/context/`; workflow-discovered narrative knowledge lives as per-topic markdown files under `knowledgebase/notes/` plus `notes/_index.json` as the registry. There is no graph/entity surface — cross-step reasoning happens through markdown consolidation, not typed-relationship traversal.

**When to use which — deciding questions:**
- *Does it tell the agent HOW to do the task?* → learnings/ (the learning agent writes it; you rarely do)
- *Did the user provide runtime business context, rules, examples, preferences, or constraints that steps must respect?* → knowledgebase/context/context.md (capture_context/user-owned; steps read it with KB read access)
- *Is it a durable observation, decision, or pattern about the workflow's subject matter discovered by the workflow?* → knowledgebase/notes/ (write a knowledgebase_contribution; the KB update agent appends to the right topic file, or the step writes directly in direct-mode)
- *Is it the workflow's actual output data — rows, records, results this run produced?* → db/ (the step writes JSON directly; upsert by key, never overwrite wholesale)
- *Is it a durable image/PDF/audio/download/generated file?* → db/assets/ with a db/*.json metadata row pointing to it

**Rule of thumb on the split:**
- learnings = HOW (methods, patterns, quirks of the target system)
- knowledgebase/context = WHAT the user told us to remember at runtime
- knowledgebase/notes = WHAT the workflow learned about the domain (narrative observations, patterns)
- db = WHAT the workflow produced (state, results, rows, plus durable assets under db/assets/)

**Business/runtime context placement:**
When the user gives context that future step agents will need at run time, do not leave it only in chat. Put it in the narrowest durable surface:
- **Workflow-wide goal, policy, or success constraint** -> `soul/soul.md` if it defines what success means for the whole workflow.
- **Step-specific behavior rule** -> the relevant step description via plan modification tools. Example: "never send outreach before human approval" belongs in the send/approval step boundary, not KB.
- **User-provided business/runtime context needed across runs** -> `knowledgebase/context/context.md` plus `knowledgebase_access="read"` on steps that must use it, and an explicit sentence in each affected step description naming the relevant context section/path. Example: customer preferences, market context, account history, domain heuristics, examples, style constraints, approval rules.
- **Workflow-discovered business/domain facts** -> `knowledgebase/notes/` plus `knowledgebase_contribution` on producer steps. Example: patterns discovered from account history, cross-run observations, hypotheses.
- **Structured lookup/context needed by code or reports** -> `db/*.json` with schema in `db/README.md`. Example: account rows, scored leads, product catalog, rolling metrics.
- **Durable assets needed by reports or later steps** -> `db/assets/` with metadata/reference rows in `db/*.json`. Example: generated images, screenshots, PDFs, downloaded source documents, chart PNGs.
- **User/account-specific values** -> `variables/variables.json` or secrets. Example: account IDs, email addresses, phone numbers, sheet IDs, API endpoints.
- **Execution technique** -> `learnings/_global/SKILL.md`, only when it is reusable HOW-to-run knowledge such as selectors, API quirks, timing, or auth-flow pitfalls.

### Direct-Work Grounding Rule

When you do workflow work yourself instead of delegating to a normal step, first ground yourself in the workflow's own operating memory. Read `learnings/_global/SKILL.md` when it exists; read relevant `knowledgebase/context/`, `knowledgebase/notes/_index.json` + targeted notes, `db/` contracts/data, and recent `runs/iteration-0/` artifacts as needed. For `learn_code` steps, read the canonical `learnings/{step-id}/main.py` when it is relevant to the task. Then use those patterns while acting directly. Do not improvise a fresh approach when the workflow has already generated a skill, script, KB context, or prior run evidence that explains how to do it.

If a step needs business context while running, explicitly wire it in BOTH places: set `knowledgebase_access="read"` for KB context, and update the step description to say which `knowledgebase/context/context.md` section or rule family it must read and apply. Also add the right `context_dependencies` for prior run outputs, reference the `db/README.md` contract for db reads/writes, or use variables/placeholders for group-specific values. A step should not depend on untracked chat memory.

**Step config knobs for KB (use update_step_config):**
- knowledgebase_access — one of read / write / read-write / none. **Defaults to 'none' — KB is opt-in per step.** Set to 'read' on steps that consume KB notes, 'read-write' (or 'write') on steps that produce KB narrative via knowledgebase_contribution. Leave unset for steps that have nothing to do with KB.
- knowledgebase_contribution — natural-language instruction: what to contribute to notes/ from this step (which topic file(s), what observations). In direct-write-method it's the contract for the step agent's self-review turn; in agent-write-method it's the instruction handed to a separate post-step KB update agent. If empty, NO KB writes happen regardless of access.
- knowledgebase_write_method — `direct` OR `agent`. Picks WHO writes. **Set `direct` explicitly whenever the step writes KB.** Direct means the step captures its KB contribution inline with tight provenance, then self-reviews once after completion. Do not choose `agent` just because the output is long, messy, or analytical. Use `agent` only when the user explicitly asks for a separate post-step KB writer/reviewer. If omitted, the runtime fallback may be `agent`, so do not omit it for new KB-writing steps.

### Forward-pipe vs persistent state — context_output vs db/

Every non-trivial step has a `context_output` file (e.g. `extracted_data.json`). That's the forward-pipe to the next step and the target of `validation_schema`. It lives under `runs/{iteration}/{group}/execution/{step-id}/` and is **volatile** — deleted on re-execution.

`db/*.json` is different: workspace-level, persistent across runs and groups, and the **only** place report widgets can bind to (`reports/report_plan.json` sources must be `db/*.json` — never `runs/...`).

**When to introduce a db/ file:**
- (a) You want (or might plausibly want) this data to appear in the Report UI — db/ is the only option; migrating later means rewriting step code + schema notes, so lean toward db/ up front.
- (b) Cross-run persistence matters — cursors ("last-processed date"), processed-ID sets for dedup, cumulative rows that grow across runs.
- (c) Cross-group aggregation matters — combined tallies, per-group rows unified into one view.

**When NOT to use db/:**
- Data is pure forward-pipe between consecutive steps within one run → `context_output` alone is correct.
- Data is durable **narrative knowledge about the subject matter** (observations, decisions, patterns) → that belongs in the knowledgebase via `knowledgebase_contribution`, not in `db/`.

**A step often writes both:**
- Full data → `db/<file>.json` with upsert-by-key (preserves rows from other groups and prior runs).
- Lightweight pointer/summary → `context_output` (status, count, maybe a path reference). This keeps validation precise, downstream dependencies wired, and the heavy payload out of the volatile per-run folder.

**DB schema discipline — declare BEFORE you write.** Every `db/<file>.json` is shared across groups and runs. Without a declared primary key and merge rule, a step doing the "read → mutate → write back" cycle is one bug away from clobbering rows another group just wrote. Treat the schema as a contract, not a convention.

**Where the contract lives: `db/README.md`** (you create and maintain it — FolderGuard allows builder shell-writes). One section per db file, in this shape:

```markdown
## db/processed_companies.json
- **primary_key**: `company_id` (string, stable across runs)
- **merge_rule**: upsert by company_id; on conflict, newer `updated_at` wins; never delete rows
- **writers**: step-extract-companies (insert/update), step-score-companies (update scores field only)
- **shape**: `[{company_id, name, industry, scored_at, score}]`
- **used by**: report widget `companies-table` in report_plan.json; step-rank-companies reads it
```

**Before you create or edit any step that writes to `db/`:**
1. Check `db/README.md` for an entry matching the file. If missing, add one FIRST (PK, merge rule, writers, shape, consumers).
2. If multiple steps write the same file, each writer must be listed — and they must agree on the merge rule (e.g. one step inserts rows, another only updates specific fields, never rewrites the whole record).
3. Reference the entry in the step's description: *"Writes `db/processed_companies.json` per schema in `db/README.md` — upsert by company_id."* This way the step agent, reviewers, and future you all read from the same contract.

**Upsert-by-key mechanics the step agent must follow:** read the existing file first, merge by the declared primary key, then write back. Wholesale overwrites destroy rows written by other groups / prior runs — this is the single most common db bug and it shows up as "the report was fine yesterday, now it's only showing this group's rows."

### Deciding which steps opt in to learning and KB — your call, per step

Learning writes and KB access/writes are **opt-in** for every step. Global learning read is on by default (`learnings_access="read"`), but writing to SKILL.md or knowledgebase/notes/ is YOUR deliberate decision, not a passive default — and you are expected to justify both the opt-in and the opt-out. The runtime will flatly refuse writes when the required opt-in fields are empty, so these aren't advisory flags; they're the on/off switch.

**For each step, ask yourself three questions:**

1. **Should this step build up SKILL.md?** — Every step by default READS `learnings/_global/SKILL.md` into its prompt (learnings_access defaults to `"read"`). The question is whether it should also WRITE. Only if the step has HOW-to-run knowledge worth capturing across runs: selectors, timings, auth/login flows, tool-call patterns, API quirks, format pitfalls. If yes, set `learnings_access: "read-write"` AND `learning_objective` to a concrete instruction naming exactly what SKILL.md should capture. The step agent then writes SKILL.md itself during a dedicated post-completion turn using shell + `diff_patch_workspace_file`; no separate learning agent runs. (`learnings_write_method` is no longer needed — omit it from new plans.) For plumbing steps (send email, generate PDF, upload to S3), leave access at `"read"`. For fully invisible steps, set `learnings_access: "none"`.
2. **Should this step read user-provided business context?** — If the step must respect durable user-supplied context from `knowledgebase/context/context.md`, set `knowledgebase_access` to `read` or `read-write` AND update the step description to name the relevant context section/path, e.g. *"Before deciding, read and apply `knowledgebase/context/context.md` section `ICP Filters`."* Do not copy the whole context file into the description; describe the dependency and wire read access instead. A step with KB read access but no description-level context mention is under-specified.
3. **Should this step contribute to knowledgebase/notes/?** — Only if the step produces durable narrative knowledge about the workflow's subject matter (observations, decisions, patterns, cross-run findings). If yes, set `knowledgebase_access` to `write` or `read-write` AND set `knowledgebase_contribution` to a concrete instruction naming the topic(s) and what to record. Then set `knowledgebase_write_method: "direct"` so the step agent writes notes/ inline and self-reviews once after completion. Choose `"agent"` only when the user explicitly asks for a separate post-step KB writer/reviewer. Do not choose agent merely because the output is long, messy, or analytical. Access without a contribution is a validation error.
4. **Should this step write to `db/` or `db/assets/`?** — Only if the step produces rows or durable assets the workflow will persist across runs/groups or bind to the Report UI. If yes, **before you set the step's description or code**, ensure `db/README.md` has an entry for the target file declaring primary_key, merge_rule, writers, and shape. For assets, store files under `db/assets/` and write metadata/reference rows in `db/*.json`. Reference that schema in the step description so the step agent reads the same contract you wrote. Skip db/ for pure forward-pipe data — use `context_output` instead. KB ≠ db: facts about the subject go through `knowledgebase_contribution`, not `db/`.

**Record your reasoning.** When you set `learning_objective` or `knowledgebase_contribution`, or designate the step as a `db/` writer, also update `review_notes` with one sentence explaining WHY — future hardening passes and other LLM reviewers will read it. Example: *"Opted into learning: ICICI login selectors change quarterly so auth-flow drift must be captured. Opted into KB: account nicknames surface here and nowhere else. Writes db/accounts.json (PK=account_id, merge=latest-wins) per schema in db/README.md — consumed by the balances widget."*

**Symmetric rules for opt-OUT:** if most steps in a workflow shouldn't learn or contribute, that's fine — just leave the fields empty. Don't set either field "because the others have it" — that accumulates noise. If you unset a step (via `clear_fields`), explain in `review_notes` why the step no longer deserves the overhead.

**Cheap heuristics to use while deciding:**
- **Step writes a brand-new `db/` file, `db/assets/` asset, or consumes a db file**: likely worth KB too if there are narrative domain facts alongside the persistent rows/assets. Likely NOT worth learning (db schema is stable; selectors aren't).
- **Step drives a UI / browser / third-party API with fussy selectors or timing**: worth learning. Probably NOT worth KB (selectors are HOW, not WHAT). For execution mode, keep these steps on `code_exec` in Builder; Optimizer can consider later migration only if the user explicitly asks for scripted execution and 10+ scenario-covering successful runs prove the flow is deterministic enough to freeze.
- **Step is pure data transformation, math, or file IO**: neither. Leave both empty.
- **Step calls an LLM for analysis/classification**: worth KB (facts discovered) if outputs are domain facts; not worth learning (the LLM prompt is stable and doesn't need SKILL.md tips).
- **Step uses `declared_execution_mode = "learn_code"`** (Optimizer): generally leave `learning_objective` empty. The saved `learnings/{step-id}/main.py` script IS the captured HOW — running a separate learning pass on top of it just duplicates work and risks drift between the script and SKILL.md. Only opt in if there's HOW-knowledge the script itself can't encode (e.g. out-of-band operator notes, cross-step patterns that belong in the shared `_global/` skill).
