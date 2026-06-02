## Three persistent stores — skill vs knowledgebase vs db

Every workflow has three separate stores that survive across runs. They are NOT interchangeable. Mixing them up bloats prompts with irrelevant content and makes later runs harder to debug.

**learnings/_global/SKILL.md — HOW to run the task**
- Execution know-how: selectors, API quirks, CLI flags, SDK/tool call patterns, timing, auth flows, output parsing rules, retry/recovery rules, and pitfalls the agent hit before.
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

**db/db.sqlite — workflow state and results**
- A single SQLite database per workflow holding the workflow's actual output data: one table per logical entity (processed records, cursors, cumulative output, per-group tallies). Step code reads/writes it with the `sqlite3` CLI (or Python's `sqlite3`).
- Durable media/file assets live under `db/assets/`. Store images, PDFs, screenshots, audio, generated files, and other binary assets there when they must survive runs or be used by reports/later steps. Keep metadata, provenance, and references in a `db.sqlite` table; do not embed large assets as blobs.
- **Written by (runtime):** step code directly (`sqlite3` / Python). Step-owned during runs — `INSERT ... ON CONFLICT DO UPDATE` (upsert by primary key), never recreate or wholesale-overwrite a table (that destroys rows from other groups/runs).
- **Written by (design time — you):** YOU (the builder) MAY use `sqlite3 db/db.sqlite` directly to create tables, seed initial rows, fix corrupt data, or stage test data for development. Your FolderGuard allows it. Prefer letting steps populate tables during actual runs — your writes are for setup and repair, not ongoing state.
- Read as: step agents query it directly via `sqlite3`; widgets in reports/report_plan.json bind to it via `db: "db/db.sqlite"` + a `sql` query.
- Shape: relational tables with a declared PRIMARY KEY per table, decided by the builder at design time. Nested objects/arrays are stored as JSON-text columns (`json_extract` to read them back).
- Examples: "table `processed_companies` keyed by company_id", "table `monthly_totals` aggregated across all months", "table `cursors` tracking last-processed dates".

**KB shape:** context + notes. User-supplied runtime context lives under `knowledgebase/context/`; workflow-discovered narrative knowledge lives as per-topic markdown files under `knowledgebase/notes/` plus `notes/_index.json` as the registry. There is no graph/entity surface — cross-step reasoning happens through markdown consolidation, not typed-relationship traversal.

**When to use which — deciding questions:**
- *Does it tell the agent HOW to do the task?* → learnings/ (the learning agent writes it; you rarely do)
- *Did the user provide runtime business context, rules, examples, preferences, or constraints that steps must respect?* → knowledgebase/context/context.md (capture_context/user-owned; steps read it with KB read access)
- *Is it a durable observation, decision, or pattern about the workflow's subject matter discovered by the workflow?* → knowledgebase/notes/ (write a knowledgebase_contribution; the KB update agent appends to the right topic file, or the step writes directly in direct-mode)
- *Is it the workflow's actual output data — rows, records, results this run produced?* → db/db.sqlite (the step writes via `sqlite3`; upsert on the primary key, never recreate or wholesale-overwrite the table)
- *Is it a durable image/PDF/audio/download/generated file?* → db/assets/ with a db/db.sqlite metadata row pointing to it

**Rule of thumb on the split:**
- learnings = HOW (methods, patterns, quirks of the target system)
- knowledgebase/context = WHAT the user told us to remember at runtime
- knowledgebase/notes = WHAT the workflow learned about the domain (narrative observations, patterns)
- db = WHAT the workflow produced (state, results, rows in db/db.sqlite tables, plus durable assets under db/assets/)

**Business/runtime context placement:**
When the user gives context that future step agents will need at run time, do not leave it only in chat. Put it in the narrowest durable surface:
- **Workflow-wide goal, policy, or success constraint** -> `soul/soul.md` if it defines what success means for the whole workflow.
- **Step-specific behavior rule** -> the relevant step description via plan modification tools. Example: "never send outreach before human approval" belongs in the send/approval step boundary, not KB.
- **User-provided business/runtime context needed across runs** -> `knowledgebase/context/context.md` plus `knowledgebase_access="read"` on steps that must use it, and an explicit sentence in each affected step description naming the relevant context section/path. Example: customer preferences, market context, account history, domain heuristics, examples, style constraints, approval rules.
- **Workflow-discovered business/domain facts** -> `knowledgebase/notes/` plus `knowledgebase_contribution` on producer steps. Example: patterns discovered from account history, cross-run observations, hypotheses.
- **Structured lookup/context needed by code or reports** -> a `db/db.sqlite` table with schema in `db/README.md`. Example: account rows, scored leads, product catalog, rolling metrics.
- **Durable assets needed by reports or later steps** -> `db/assets/` with metadata/reference rows in a `db/db.sqlite` table. Example: generated images, screenshots, PDFs, downloaded source documents, chart PNGs.
- **User/account-specific values** -> `variables/variables.json` or secrets. Example: account IDs, email addresses, phone numbers, sheet IDs, API endpoints.
- **Execution technique** -> `learnings/_global/SKILL.md`, only when it is reusable HOW-to-run knowledge such as selectors, API quirks, timing, or auth-flow pitfalls.

### Direct-Work Grounding Rule

When you do workflow work yourself instead of delegating to a normal step, first ground yourself in the workflow's own operating memory. Read `learnings/_global/SKILL.md` when it exists; read relevant `knowledgebase/context/`, `knowledgebase/notes/_index.json` + targeted notes, `db/` contracts/data, and recent `runs/iteration-0/` artifacts as needed. For `scripted` steps, read the canonical `learnings/{step-id}/main.py` when it is relevant to the task. Then use those patterns while acting directly. Do not improvise a fresh approach when the workflow has already generated a skill, script, KB context, or prior run evidence that explains how to do it.

If a step needs business context while running, explicitly wire it in BOTH places: set `knowledgebase_access="read"` for KB context, and update the step description to say which `knowledgebase/context/context.md` section or rule family it must read and apply. Also add the right `context_dependencies` for prior run outputs, reference the `db/README.md` contract for db reads/writes, or use variables/placeholders for group-specific values. A step should not depend on untracked chat memory.

**Step config knobs for KB (use update_step_config):**
- knowledgebase_access — one of read / write / read-write / none. **Defaults to 'none' — KB is opt-in per step.** Set to 'read' on steps that consume KB notes, 'read-write' (or 'write') on steps that produce KB narrative via knowledgebase_contribution. Leave unset for steps that have nothing to do with KB.
- knowledgebase_contribution — natural-language instruction: what to contribute to notes/ from this step (which topic file(s), what observations). In direct-write-method it's the contract for the step agent's self-review turn; in agent-write-method it's the instruction handed to a separate post-step KB update agent. If empty, NO KB writes happen regardless of access.
- knowledgebase_write_method — `direct` OR `agent`. Picks WHO writes. **Set `direct` explicitly whenever the step writes KB.** Direct means the step captures its KB contribution inline with tight provenance, then self-reviews once after completion. Do not choose `agent` just because the output is long, messy, or analytical. Use `agent` only when the user explicitly asks for a separate post-step KB writer/reviewer. If omitted, the runtime fallback may be `agent`, so do not omit it for new KB-writing steps.

### Forward-pipe vs persistent state — context_output vs db/

Every non-trivial step has a `context_output` file (e.g. `extracted_data.json`). That's the forward-pipe to the next step and the target of `validation_schema`. It lives under `runs/{iteration}/{group}/execution/{step-id}/` and is **volatile** — deleted on re-execution.

`db/db.sqlite` is different: workspace-level, persistent across runs and groups, and the default source for data-backed report widgets (`table`, `cards`, `chart`, `stat`, `alert`, `pivot`) — they bind via `db: "db/db.sqlite"` + a `sql` query. Artifact report widgets are the exception: `kind: "file"` and `kind: "file-list"` use `source` to render durable files from `db/`, `knowledgebase/`, or `docs/`. Do not point report widgets at volatile `runs/...` paths.

**When to introduce a db/db.sqlite table:**
- (a) You want (or might plausibly want) structured data to appear in the Report UI — a db.sqlite table is the default durable option; migrating later means rewriting step code + schema notes, so lean toward it up front. For human-readable documents or media artifacts, use durable `docs/`, `knowledgebase/`, or `db/assets/` paths with `file` / `file-list` widgets.
- (b) Cross-run persistence matters — cursors ("last-processed date"), processed-ID sets for dedup, cumulative rows that grow across runs.
- (c) Cross-group aggregation matters — combined tallies, per-group rows unified into one view.

**When NOT to use db/:**
- Data is pure forward-pipe between consecutive steps within one run → `context_output` alone is correct.
- Data is durable **narrative knowledge about the subject matter** (observations, decisions, patterns) → that belongs in the knowledgebase via `knowledgebase_contribution`, not in `db/`.

**A step often writes both:**
- Full data → a `db/db.sqlite` table via `INSERT ... ON CONFLICT DO UPDATE` (preserves rows from other groups and prior runs).
- Lightweight pointer/summary → `context_output` (status, count, maybe a path reference). This keeps validation precise, downstream dependencies wired, and the heavy payload out of the volatile per-run folder.

**DB schema discipline — declare BEFORE you write.** Every table in `db/db.sqlite` is shared across groups and runs. The PRIMARY KEY plus an explicit `ON CONFLICT` upsert is what keeps a step's write from clobbering rows another group just wrote. Treat the schema (DDL) as a contract, not a convention.

**Where the contract lives: `db/README.md`** (you create and maintain it — FolderGuard allows builder shell-writes). One section per table, in this shape:

```markdown
## table: processed_companies
- **ddl**: `CREATE TABLE processed_companies (company_id TEXT PRIMARY KEY, name TEXT, industry TEXT, scored_at TEXT, score REAL, updated_at TEXT)`
- **primary_key**: `company_id` (stable across runs)
- **upsert**: `INSERT ... ON CONFLICT(company_id) DO UPDATE SET ...`; newer `updated_at` wins; never DELETE rows
- **indexes**: `CREATE INDEX idx_processed_companies_score ON processed_companies(score)`
- **writers**: step-extract-companies (insert/update), step-score-companies (update score column only)
- **used by**: report widget `companies-table` in report_plan.json (`SELECT ... FROM processed_companies`); step-rank-companies reads it
```

**Before you create or edit any step that writes to `db/db.sqlite`:**
1. Check `db/README.md` for an entry matching the table. If missing, add one FIRST (DDL, PK, upsert rule, indexes, writers, consumers) and `CREATE TABLE IF NOT EXISTS` it.
2. If multiple steps write the same table, each writer must be listed — and they must agree on the upsert rule (e.g. one step inserts rows, another only updates specific columns, never rewrites the whole row).
3. Reference the entry in the step's description: *"Writes table `processed_companies` per schema in `db/README.md` — upsert on company_id."* This way the step agent, reviewers, and future you all read from the same contract.

**Upsert mechanics the step agent must follow:** use a single `INSERT ... ON CONFLICT(<pk>) DO UPDATE SET ...` statement. Do NOT `DROP`/`CREATE` a table to "refresh" it, and do not delete-then-insert the whole table — that destroys rows written by other groups / prior runs. This is the single most common db bug and it shows up as "the report was fine yesterday, now it's only showing this group's rows."

### Deciding which steps opt in to learning and KB — your call, per step

Learning writes and KB access/writes are **opt-in** for every step. Global learning read is on by default (`learnings_access="read"`), but writing to SKILL.md or knowledgebase/notes/ is YOUR deliberate decision, not a passive default — and you are expected to justify both the opt-in and the opt-out. The runtime will flatly refuse writes when the required opt-in fields are empty, so these aren't advisory flags; they're the on/off switch.

**For each step, ask yourself three questions:**

1. **Should this step build up SKILL.md?** — Every step by default READS `learnings/_global/SKILL.md` into its prompt (learnings_access defaults to `"read"`). The question is whether it should also WRITE. Only if the step has HOW-to-run knowledge worth capturing across runs. If yes, set `learnings_access: "read-write"` AND `learning_objective` to a concrete instruction naming exactly what SKILL.md should capture. The step agent then writes SKILL.md itself during a dedicated post-completion turn using shell + `diff_patch_workspace_file`; no separate learning agent runs. (`learnings_write_method` is no longer needed — omit it from new plans.) For steps that do not discover reusable HOW, leave access at `"read"` so they can still consume shared guidance. Use `"none"` only when `_global/SKILL.md` would actively mislead the step or token isolation is important.

### Learning write decision matrix

Use `learnings_access="read-write"` only when the step is expected to discover reusable execution technique:
- **Browser/UI automation**: stable selectors, tab/session rules, login/auth indicators, upload/download quirks, wait/re-snapshot timing, safe CDP vs headless behavior.
- **APIs/MCP tools**: exact request shape, pagination cursors, response fields that prove success, retry/rate-limit behavior, idempotency keys, error envelopes, required call order.
- **CLIs/SDKs**: command flags, working directory, required env vars, exit-code meanings, output parsing rules, generated file locations, commands to avoid.
- **Unstable external systems**: recovery steps for known failures, temporary-state handling, deterministic checks that separate "loaded" from "actually usable".
- **Unknown formats/parsing**: PDF/table/CSV/HTML quirks, schema variations, safe merge/read patterns discovered from real runs.

Keep the step **read-only** (`learnings_access` unset or `"read"`) when it is mainly executing an already-known contract:
- routing/condition steps,
- validation/preflight checks that only inspect known fields,
- mechanical transforms, aggregation, dedupe, formatting, and report-widget data shaping,
- human input/approval/message-only steps,
- pure db/KB consumers that do not interact with an external system,
- mature scripted steps where `learnings/{step-id}/main.py` already encodes the HOW.

Use `learnings_access="none"` rarely:
- when shared HOW would confuse an isolated deterministic step,
- when the step intentionally must not see target-system operating guidance,
- or when token budget is critical and the step is completely divorced from the workflow's external systems.

A good `learning_objective` is concrete: "Capture the Buffer API create-update request shape, success fields, 401/429 handling, and output id parsing." Bad: "learn from this step."

Learning content should answer **"how should this step operate next time?"** It should not record facts/results such as leads found, current prices, user preferences, status history, or credentials. Put facts/results in `db/` or KB as appropriate; never put secret values in learnings.
2. **Should this step read user-provided business context?** — If the step must respect durable user-supplied context from `knowledgebase/context/context.md`, set `knowledgebase_access` to `read` or `read-write` AND update the step description to name the relevant context section/path, e.g. *"Before deciding, read and apply `knowledgebase/context/context.md` section `ICP Filters`."* Do not copy the whole context file into the description; describe the dependency and wire read access instead. A step with KB read access but no description-level context mention is under-specified.
3. **Should this step contribute to knowledgebase/notes/?** — Only if the step produces durable narrative knowledge about the workflow's subject matter (observations, decisions, patterns, cross-run findings). If yes, set `knowledgebase_access` to `write` or `read-write` AND set `knowledgebase_contribution` to a concrete instruction naming the topic(s) and what to record. Then set `knowledgebase_write_method: "direct"` so the step agent writes notes/ inline and self-reviews once after completion. Choose `"agent"` only when the user explicitly asks for a separate post-step KB writer/reviewer. Do not choose agent merely because the output is long, messy, or analytical. Access without a contribution is a validation error.
4. **Should this step write to `db/db.sqlite` or `db/assets/`?** — Only if the step produces rows or durable assets the workflow will persist across runs/groups or bind to the Report UI. If yes, **before you set the step's description or code**, ensure `db/README.md` has an entry for the target table declaring its DDL, primary_key, upsert rule, indexes, and writers. For assets, store files under `db/assets/` and write metadata/reference rows in a `db/db.sqlite` table. Reference that schema in the step description so the step agent reads the same contract you wrote. Skip db/ for pure forward-pipe data — use `context_output` instead. KB ≠ db: facts about the subject go through `knowledgebase_contribution`, not `db/`.

**Record your reasoning.** When you set `learning_objective` or `knowledgebase_contribution`, or designate the step as a `db/` writer, also update `review_notes` with one sentence explaining WHY — future hardening passes and other LLM reviewers will read it. Example: *"Opted into learning: ICICI login selectors change quarterly so auth-flow drift must be captured. Opted into KB: account nicknames surface here and nowhere else. Writes table `accounts` in db/db.sqlite (PK=account_id, upsert latest-wins) per schema in db/README.md — consumed by the balances widget."*

**Symmetric rules for opt-OUT:** if most steps in a workflow shouldn't learn or contribute, that's fine — just leave the fields empty. Don't set either field "because the others have it" — that accumulates noise. If you unset a step (via `clear_fields`), explain in `review_notes` why the step no longer deserves the overhead.

**Cheap heuristics to use while deciding:**
- **Step writes a brand-new `db/db.sqlite` table, `db/assets/` asset, or consumes a db table**: likely worth KB too if there are narrative domain facts alongside the persistent rows/assets. Likely NOT worth learning (db schema is stable; selectors aren't).
- **Step drives a UI / browser / third-party API with fussy selectors or timing**: worth learning. Probably NOT worth KB (selectors are HOW, not WHAT). For execution mode, keep these steps on `agentic` in Builder; Optimizer can consider later migration only if the user explicitly asks for scripted execution and 10+ scenario-covering successful runs prove the flow is deterministic enough to freeze.
- **Step is pure data transformation, math, or file IO**: neither. Leave both empty.
- **Step calls an LLM for analysis/classification**: worth KB (facts discovered) if outputs are domain facts; not worth learning (the LLM prompt is stable and doesn't need SKILL.md tips).
- **Step uses `declared_execution_mode = "scripted"`** (Optimizer): generally leave `learning_objective` empty. The saved `learnings/{step-id}/main.py` script IS the captured HOW — running a separate learning pass on top of it just duplicates work and risks drift between the script and SKILL.md. Only opt in if there's HOW-knowledge the script itself can't encode (e.g. out-of-band operator notes, cross-step patterns that belong in the shared `_global/` skill).
