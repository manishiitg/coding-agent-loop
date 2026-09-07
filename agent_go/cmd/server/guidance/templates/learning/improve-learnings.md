**Saved-code paths:** Read `workflow.json.code_layout_version` first. In this reference, `<script-dir>` means `code/<step-id>` for version 1, or `learnings/<step-id>` for absent/zero (legacy). Resolve the placeholder before using a path; never infer the version from folders or migrate an existing workflow implicitly. Version 1 executes and repairs canonical source directly, with shared helpers under `WORKFLOW_CODE_ROOT`; only legacy workflows copy code into runs and save it back.

# ENGINEERING REVIEW — STORES HEALTH / LEARNINGS LENS

Review whether `learnings/_global/` supports the current plan and objective. This
checklist is passed to a generic read-only reviewer. Do not edit any file, update
any presentation artifact, or call module-result or human-input tools. Any later
wording such as improve, apply, edit, update, remove, merge, or resolve describes
what the **Pulse Fixer** should do after consolidating all reviewer findings; it
is not permission for this reviewer to mutate anything.

EXECUTION

The parent Workshop/Pulse agent first loads `assumption-audit`, then includes
this rendered checklist in the normal Engineering/Ops background executor. The
executor returns compact evidence; the parent consolidates it after the
automatic completion notification. Do not create a dedicated
learning-maintenance agent.

This checklist is one of three store-integrity lenses (learnings, knowledgebase,
DB) the parent may load into Engineering Review — see the `pulse-review-fixer`
contract. Use
the fields below as the learning-lens checkpoint; do not return a separate
module envelope, final artifact, or module state from this turn.

Return only this compact contract:

- `module`: technical_review
- `verdict`: clean | needs_fix | blocked
- `index_shape`: **required, measure it — do not estimate.** Report
  `SKILL.md` line count, how many of those lines are links to `references/`
  versus inline detail, the number of reference files, and the largest two with
  their sizes. Then state whether `SKILL.md` is still functioning as an index.
  Fill this on every review, including a `clean` one.
- `purity_manifest`: **required.** Enumerate every content-bearing Markdown
  file under `learnings/_global/` (the root `SKILL.md` and every Markdown
  reference), state whether it contains only reusable execution HOW, and list
  every section that belongs in another store. Do not sample references and do
  not treat moving non-skill content from `SKILL.md` into `references/` as a
  repair.
- `learning_objective_audit`: **required.** Account for every step whose
  effective `learnings_access` is `read-write`. Classify its objective as
  `valid_how`, `misconfigured`, or `missing_coverage`, with the step ID and
  exact reason. Also identify objectives on read-only/disabled steps that
  should be cleared so legacy defaults cannot silently re-enable writes.
- `ownership_candidates`: **required.** For every misplaced or duplicated
  semantic item, report `item`, `current_location`, `semantic_type`,
  `authoritative_owner`, `duplicate_locations`, `recommended_action`, and
  `verification`. Use the shared Stores ownership contract below; a link or
  summary that repeats the same fact still counts as a duplicate unless it is
  only a stable reference to the authoritative record.
- `findings`: stable `finding_id`, `target_key`, severity, plain-language
  summary, exact problem, and why it matters
- `evidence`: precise paths and relevant step ids
- `recommended_fix`: bounded edits for the Pulse Fixer
- `verification`: exact checks for the Pulse Fixer
- `user_judgment_required`: yes/no with reason
- `next_check`: evidence or cadence condition for another review

`index_shape` is a required field because structure review is otherwise reliably
skipped. Content correctness is more interesting than file shape, so attention
goes there and the index quietly accretes — in one live workflow `SKILL.md`
reached 272 lines and 67 KB while consecutive reviews returned detailed,
correct content findings and never once mentioned its shape.

Use the remaining document only as the learning-health audit checklist.

Read typed Pulse findings and review history for prior context and matching open
findings, but do not mutate them. Use targeted semantic reads only. The parent
agent owns any approved mutation and its typed disposition.

For a claimed learning regression or a repair awaiting proof, compare the
current state with up to three comparable retained runs (same route/group and
materially equivalent configuration). Inspect compact receipts first and open
raw execution traces only for the differing writer or consumer. State an
evidence limitation when fewer comparable runs remain.

Apply the parent-provided `assumption-audit` learnings/skills lens within this command's boundaries. Reusable HOW belongs here; business policy, fixed strategy, architecture preferences, and unverified limitations do not become true because they were written into a skill. Recommend removing misplaced material from the complete skill package, not merely relocating it from `SKILL.md` to a reference. Surface consequential unresolved assumptions for Pulse's Assumptions challenged.

## Skill-content purity contract

The Stores contract is **one semantic item, one authoritative owner**. Soul
owns why, goals, preferences, and hard constraints. Plan/step config owns what
the workflow currently does. Validation owns deterministic proof requirements.
Learnings owns reusable execution HOW. Knowledgebase owns durable domain facts
with provenance. DB owns structured operational state. Pulse owns findings,
diagnosis, attempts, decisions, and fix verification. Other stores may retain a
stable ID or path to the owner; they must not retain a second prose copy.

The entire `learnings/_global/` package is a skill. `references/` is progressive
disclosure for detailed skill instructions; it is not an archive or an escape
hatch for content that fails the skill boundary. Classify every content block
using this routing table:

| Content | Canonical home | Skill treatment |
|---|---|---|
| Reusable target-system procedure, selector strategy, target-service auth/API/CLI quirk, parsing/retry/recovery rule, or stable failure signature | `learnings/_global/` | Keep; concise index in `SKILL.md`, detail in a focused reference |
| Business identity, entity fact, or durable subject-matter fact | `knowledgebase/context/` when user-supplied; `knowledgebase/notes/` when workflow-discovered | Remove from the skill; never copy user context or invent a fact |
| Run output, current metric/value/status, action/result row, or time-varying observation | `db/db.sqlite` or run artifacts | Remove from the skill; never perform a speculative data mutation |
| Owner goal, preference, cap, threshold, or safety constraint | `soul/soul.md` | Remove the duplicate from the skill; never change the owner value here |
| Current workflow strategy, cadence, allocation, routing, or step behavior | `planning/plan.json` / `planning/step_config.json` | Remove from the skill; use the current plan as authority |
| Incident narrative, dated provenance, action/run IDs, fix history, operator decision receipt, or forensic proof | Pulse review/finding/decision history and run evidence | Remove from the skill after the durable Pulse review retains the exact source pointer and evidence |
| Shared AgentWorks bridge/auth variables or envelopes, api-bridge routing, Folder Guard internals, managed workflow-DB tool syntax, `get_api_spec` workarounds, coding-agent tmux/native-session plumbing, platform architecture/history, or product documentation | Platform prompt/tool schema/documentation | Remove from the workflow skill. Do not retain a shortened copy; the runtime already supplies the authoritative instruction. |

A date, ID, handle, metric, or product name is not automatically invalid: it may
be part of a stable executable instruction. Judge its role. The test is whether
a future agent needs the content to perform the task, not whether it once helped
explain why the rule was learned.

## Reconcile against unreviewed plan changes

The parent may pass `plan_change_backlog` from `get_pulse_state(view="module")`: plan-mod
calls whose knock-on effects nobody has traced yet, each with its reason,
affected step ids and changed field names.

Treat that list as evidence, never as a verdict. Most edits invalidate nothing —
a `review_notes` touch or a typo fix moves the same timestamps as a rewritten step
description, and only reading the diff against the actual learning tells you which
happened. Do not report an item as stale merely because it predates an edit.

For each edit that plausibly changes how a step runs, open the learnings it could
affect and check the specific claim:

- the learning describes a flow, selector, path, or rule the edit changed → report
  it with the exact contradicting text and the edit that supersedes it
- the learning restates a value the owner has since changed (a cap, limit, or
  threshold) → report it; constraint values belong in `soul.md` and must never be
  copied into a learning, so the fix is to remove the number, not update it
- the learning still holds → say so explicitly, so the reviewer's silence is not
  mistaken for "not checked"

If a plan edit removed or renamed a step, its learnings are orphaned: recommend
retiring them rather than leaving them to be matched by a step that no longer
exists.

This command maintains reusable HOW-to-run knowledge such as selectors, tool/API patterns, auth quirks, timing/wait strategies, file-format pitfalls, reusable recovery steps, and common failure signatures.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

BOUNDARIES

1. Return a concrete recommended instruction and optional focus for the Pulse Fixer; there is no separate learning-maintenance tool.
2. The reviewer is read-only everywhere. Its learning-content mutation scope recommendation is `learnings/_global/`; it may also recommend `update_step_config` changes for bad `learning_objective` / `learnings_access` settings and routed KB/DB follow-up through the matching Stores lens. Never recommend editing per-step `<script-dir>/main.py` as content cleanup. Never edit or delete `learnings/_global/_freshness.json` — it is a code-owned freshness ledger written by the runtime; read it, do not touch it.
3. If you discover stale per-step scripts, bad `learning_objective`, wrong `learnings_access`, or code-lock issues, record/recommend them for the parent Pulse Fixer or an explicit manual fix. Eval rubric, coverage, or scoring issues belong to `/improve-evaluation`, not here.
4. Keep WHAT-the-workflow-discovered out of the entire skill package. User-supplied runtime context belongs in `knowledgebase/context/`; workflow-discovered subject-matter facts belong in `knowledgebase/notes/` or `db/db.sqlite`, not in either `SKILL.md` or its references.
5. Enforce a lean index shape. `learnings/_global/SKILL.md` is an **index**: frontmatter, a short scope note, and links to focused files under `learnings/_global/references/`. Detailed selectors, API quirks, auth flows, file-format notes, retry patterns, and step-specific HOW guidance belong in reference files, not in the root `SKILL.md`. Keep it as lean as the content allows; there is no line quota to fill, and a mostly-links index of any length is healthier than a short one stuffed with detail.

   **Anti-pattern to watch for in your own findings:** recommending an edit *inside* a detailed section of `SKILL.md` accepts that the detail belongs there and maintains the bloat. If the prose is reusable HOW, move it into a reference and leave a link. If it is not skill content, remove it from the complete skill package and route it according to the purity table. Moving non-skill content into `references/` is laundering, not repair.

READ FIRST

1. Read `soul/soul.md` if present to understand the workflow objective and success criteria.
2. Read `planning/plan.json` and `planning/step_config.json` if present. Use them to understand current steps, `learnings_access`, `learning_objective`, and `lock_code` decisions.
3. Read typed Pulse findings and review history. Carry unresolved learning/code findings, prior cleanup attempts, recent Pulse fixes or Goal Advisor actions, and recent plan changes into the instruction.
4. Inventory the complete `learnings/_global/` tree. Read `SKILL.md` and every
   content-bearing Markdown reference for the purity manifest. For unusually
   large files, inspect them in bounded chunks or use headings/search to route
   the reads, but do not sample away an entire file. Scripts and assets need
   only structural inspection unless the Markdown content depends on them.
5. Read `learnings/_global/_freshness.json` if present (the code-owned confirmation ledger). Its store-level `last_confirmed_run` says how recently a run reviewed the whole store; its `items` map gives each reference file's own `last_confirmed_run` and `confirm_count`, so you can target the specific stale files rather than re-scanning everything. This is the input to the freshness pass below.

FRESHNESS PASS (confirmation recency)

When Gate marked this due on a freshness signal, judge learnings by whether recent runs still exercise them, not only by plan contradiction. HOW that no run has re-confirmed in a long interval is a re-verify candidate: check it against the latest run evidence (selectors/API shapes/auth flows actually used in `runs/iteration-0/`). Then, for each stale item:

- **Refresh** it if the latest run shows the corrected HOW, and let the next run's confirmation re-stamp it.
- **Demote** genuinely superseded HOW into a `## Superseded` section of its reference file (or an archived reference), keeping the last-confirmed context, rather than deleting it — a regression can resurrect it.
- **Retire** only after a re-verify actually fails or the HOW is provably obsolete.

Never delete learnings purely because they are old. Age is a reason to re-verify, not to discard. Confirmation recency, not calendar age, is the signal.

WHEN TO USE EACH MODE

Use `mode="targeted"` when the operation is known file hygiene:

- make `SKILL.md` a short index with links to focused reference files
- merge or split specific reference files
- remove stale selectors/tool patterns after site or API changes
- compact bloated browser/API/file-format guidance
- repair links between `SKILL.md` and references
- remove or replace stale HOW guidance that no longer matches current step descriptions
- remove incident history, run facts, decisions, strategy, constraints, or
  other non-skill content from the whole skill package

Use `mode="cross_step"` when improving learnings requires the plan or multiple step declarations:

- optimize learnings for the current workflow plan
- repeated lessons appear across multiple step objectives
- step-specific HOW knowledge should be promoted into shared references
- declared `learning_objective` values are not reflected in the global skill
- recent plan or step-description changes mean old HOW guidance needs reconciliation against the new objective

If unsure, use `mode="auto"` or omit mode. Broad instructions like "optimize learnings for this plan" should resolve to current-plan consolidation.

REVIEW OUTPUT

1. Build one concrete instruction. It must mention the objective from `soul.md` or `planning/plan.json`, the user's focus if provided, and any unresolved learning-related typed Pulse findings.
   - Always include this invariant in the instruction: keep `learnings/_global/SKILL.md` lean as an index/overview; move detailed HOW-to-run content into `learnings/_global/references/<topic>.md` and link those files from `SKILL.md`.
   - Always include this purity invariant: references are part of the skill; remove non-HOW content from the entire package and route it to its authoritative store rather than moving it behind a link.
   - Always include this stale-content rule: compare learnings against current `planning/plan.json` step descriptions and `planning/step_config.json` learning objectives; remove or replace HOW guidance that belongs to old step behavior, obsolete selectors/API paths, removed dependencies, or previous descriptions.
2. Return that instruction as `recommended_fix`; do not execute it.
3. Name the exact files that would change, stale or duplicate HOW content to
   remove, reference files to create or reorganize, and learning objectives that
   still lack matching content.
4. Identify matching open findings only as evidence. The parent agent owns any
   typed Pulse disposition or close-out.
5. For every proposed removal, name the classification, authoritative
   destination, whether that destination already contains the material, and
   how the current durable Pulse review preserves the evidence. Never discard
   the only copy of a user decision or unresolved fact, and never keep it in the
   skill merely because routing requires a separate follow-up.
