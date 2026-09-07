# ENGINEERING REVIEW — STORES HEALTH / KNOWLEDGEBASE LENS

Review whether workflow knowledgebase notes support the current plan and
objective. This checklist is passed to a generic read-only reviewer. Do not edit
any file, update a presentation artifact, or call module-result or human-input
tools. Any later wording such as improve, apply, edit, update, merge, rename,
compact, or resolve describes a recommendation for the **Pulse Fixer**, not an
action for this reviewer.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

EXECUTION

The parent Workshop/Pulse agent first loads `assumption-audit`, then includes
this rendered checklist in the normal Engineering/Ops background executor. The
executor returns compact evidence; the parent consolidates it after the
automatic completion notification. Do not create a dedicated KB-maintenance
agent.

This checklist is one of three store-integrity lenses (learnings, knowledgebase,
DB) that Engineering Review may load together — see the `pulse-review-fixer`
contract. Its
output is evidence for the one Engineering Review result, never a standalone
reviewer result.

Return only: `module=technical_review`, `verdict`, `note_shape`,
`kb_purity_manifest`, `ownership_candidates`, `next_check`, and ordered
`findings`. Every finding includes stable `finding_id`, `target_key`,
severity, plain-language summary, precise `evidence`, a bounded
`recommended_fix`, exact `verification`, and `user_judgment_required` with
reason. Use the remaining document only as the KB-health audit checklist.

`note_shape` is **required — measure it, do not estimate.** Report the number of
topic files, the largest two with their sizes, whether any note is dominated by
repeated near-identical dated entries, and whether `notes/_index.json` matches
what is actually on disk. Fill it on every review, including a `clean` one.

It is required because shape review is otherwise reliably skipped: content
correctness is more interesting than file shape, so attention goes there while
notes quietly accrete. In one live workflow a single topic note reached ~100 KB
across 147 near-duplicate sections before a human compacted it by hand. A note
that repeats the same conclusion every run is not accumulating knowledge — it is
accumulating restatements, and the fix is condensation into
`## Historical context`, not another appended entry.

`kb_purity_manifest` is **required.** Enumerate every content-bearing Markdown
note on disk and classify its sections as durable domain knowledge with
provenance or content owned by Soul, Plan, Validation, Learnings, DB, or Pulse.
For unusually large files, route bounded reads by headings and searches, but do
not omit a file. No content-bearing note file may be omitted.
`ownership_candidates` records each misplaced or duplicated
semantic item with `item`, `current_location`, `semantic_type`,
`authoritative_owner`, `duplicate_locations`, `recommended_action`, and exact
`verification`.

Read typed Pulse findings and review history for prior context and matching open
findings, but do not mutate them. Use targeted semantic reads only. The parent
agent owns any approved mutation and its typed disposition.

For a claimed knowledgebase regression or a repair awaiting proof, compare the
current state with up to three comparable retained runs (same route/group and
materially equivalent configuration). Inspect compact receipts first and open
raw execution traces only for the differing writer or consumer. State an
evidence limitation when fewer comparable runs remain.

Apply the parent-provided `assumption-audit` KB-notes lens within this command's boundaries. A note that merely repeats the current plan's tactic, architecture, fixed source/channel, or unverified belief is not durable domain knowledge. Keep user-owned `knowledgebase/context/` untouched; surface a consequential unresolved restriction for Pulse's Assumptions challenged instead of copying it into more notes.

## Knowledgebase purity and ownership contract

Knowledgebase notes own durable workflow-discovered domain facts and patterns
with source/provenance. They do not own:

- owner goals, preferences, thresholds, or safety constraints (Soul);
- current strategy, cadence, routing, or step behavior (Plan/step config);
- deterministic acceptance checks (Validation);
- selectors, tool quirks, retry rules, or execution recipes (Learnings);
- run metrics, current status, queues, actions, or timestamps (DB/run evidence);
- incident narratives, findings, attempts, decisions, or fix history (Pulse).
- shared AgentWorks bridge/auth, Folder Guard, managed-tool invocation,
  tool-discovery, or coding-session mechanics (platform prompt/tool schema).

Enforce **one semantic item, one authoritative owner**. A note may reference a
stable record ID/path in another owner, but must not copy its content. Never
move workflow-discovered material into user-owned `knowledgebase/context/`.

BOUNDARIES

1. Return recommendations for `knowledgebase/notes/` and `knowledgebase/notes/_index.json`; the reviewer edits nothing. Never recommend editing or deleting `knowledgebase/_freshness.json` — it is a code-owned freshness ledger written by the runtime; read it for evidence only.
2. Never read or write `knowledgebase/context/`. That folder is user-owned runtime business context, not maintenance-owned notes.
3. Do not edit planning files, eval files, report files, learnings, or db files. Hand off any required changes outside the notes scope to the parent; this review command never becomes a writer.
4. This review is available in Workshop because KB shape can be part of workflow design or Pulse cleanup. It is not available in Run mode.

READ FIRST

1. Read `soul/soul.md` if present to understand the workflow objective and success criteria.
2. Read typed Pulse findings and review history. Use unresolved KB/db/report findings, prior failed cleanup attempts, recent Pulse fixes or Goal Advisor actions, and plan changes as context.
3. Read `planning/plan.json` and `planning/step_config.json` if present so the KB improvement is aligned with the current plan.
4. Read `knowledgebase/notes/_index.json` before opening topic files.
5. Inventory every content-bearing Markdown topic under `knowledgebase/notes/`
   for `kb_purity_manifest`. Read each file completely when bounded; for large
   files use headings/search and bounded chunks, but do not sample away an
   entire file. Target deeper evidence reads to suspicious sections.
6. If the focus is broad, names a step, or says to optimize for the plan, inspect the matching plan step(s) and recent iteration-0 outputs enough to understand what durable knowledge was produced.
7. Read `knowledgebase/_freshness.json` if present (the code-owned confirmation ledger). Its store-level `last_confirmed_run` says how recently a run reviewed the notes store; its `items` map gives each topic note's own `last_confirmed_run` and `confirm_count`, so you can target the specific stale notes rather than re-scanning everything.

FRESHNESS PASS (confirmation recency)

When Gate marked this due on a freshness signal, judge notes by whether recent runs still re-confirm them, not only by plan contradiction. A durable fact no run has re-confirmed in a long interval is a re-verify candidate — check it against the latest run evidence. Then, for each stale note or section:

- **Refresh** it if the latest run shows the fact changed, and let the next run re-stamp confirmation.
- **Demote** a superseded point-in-time observation into the note's `## Historical context` block (the same condensation used for size-based compaction), keeping its date, instead of deleting it.
- **Retire** a topic only when it is provably no longer valid.

Never delete a note purely because it is old. Time-boxed observations decay and get demoted; durable facts persist. Confirmation recency, not calendar age, is the signal.

WHEN TO USE EACH MODE

Use `mode="targeted"` when the operation is a known note hygiene task:

- merge two specific topic files
- rename a topic and rewrite note cross-references
- compact a bloated topic file
- remove stale sections from a bad run
- drop a topic that is no longer valid
- fix `_index.json` / topic-file mismatch

Use `mode="cross_step"` when improving the KB requires the plan or multiple step outputs:

- optimize the KB for the current workflow plan
- two or more steps created different topics for the same entity
- step outputs disagree about the same durable fact
- repeated observations should become a `pattern-*.md` topic
- topic names or coverage are inconsistent across upstream/downstream steps
- recent plan changes mean old KB topics need reconciliation against the new objective

If unsure, use `mode="auto"` or omit mode. Broad instructions like "optimize the KB for this plan" should resolve to cross-step consolidation.

REVIEW OUTPUT

1. Convert the user's request into one concrete instruction. If the focus is empty, base the instruction on `soul/soul.md`, `planning/plan.json`, unresolved typed Pulse findings/review history, and the KB index.
2. Return the instruction and mode as `recommended_fix`; there is no separate KB-maintenance tool.
3. Name affected topics, contradictions, pattern-note opportunities, index
   corrections, and remaining uncertainty.
4. Identify matching open findings only as evidence. The parent agent owns every
   file mutation and typed Pulse disposition.
