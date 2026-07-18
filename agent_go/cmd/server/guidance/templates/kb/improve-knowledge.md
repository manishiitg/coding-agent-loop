# READ-ONLY KNOWLEDGEBASE HEALTH REVIEW

Review whether workflow knowledgebase notes support the current plan and
objective. This checklist is passed to a generic read-only reviewer. Do not edit
any file, update `builder/improve.html`, or call module-result or human-input
tools. Any later wording such as improve, apply, edit, update, merge, rename,
compact, or resolve describes a recommendation for the **Pulse Fixer**, not an
action for this reviewer.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

EXECUTION

The parent Workshop/Pulse agent first loads `assumption-audit`, then passes its
relevant lens and this rendered checklist to
`call_generic_agent` in an instruction beginning with `READ-ONLY REVIEW` and
waits for its synchronous result. The parent then validates and applies any
bounded safe edit. Do not create a dedicated KB-maintenance agent or use
`run_in_background` for this review.

Return only: `module=knowledgebase_health`, `verdict`, `next_check`, and ordered
`findings`. Every finding includes stable `finding_id`, `target_key`, severity,
plain-language summary, precise `evidence`, a bounded `recommended_fix`, exact
`verification`, and `user_judgment_required` with reason. Use the remaining
document only as the KB-health audit checklist.

Read `builder/improve.html` for prior context and matching open findings, but do
not write it. Use targeted semantic reads only; do not inspect CSS, load HTML
style/skeleton guidance, migrate markup, or format cards. The Pulse Fixer owns
the consolidated log update.

Apply the parent-provided `assumption-audit` KB-notes lens within this command's boundaries. A note that merely repeats the current plan's tactic, architecture, fixed source/channel, or unverified belief is not durable domain knowledge. Keep user-owned `knowledgebase/context/` untouched; surface a consequential unresolved restriction for Pulse's Assumptions challenged instead of copying it into more notes.

BOUNDARIES

1. Work only on `knowledgebase/notes/` and `knowledgebase/notes/_index.json`.
2. Never read or write `knowledgebase/context/`. That folder is user-owned runtime business context, not maintenance-owned notes.
3. Do not edit planning files, eval files, report files, learnings, or db files unless the user explicitly asks outside this command.
4. This review is available in Workshop because KB shape can be part of workflow design or Pulse cleanup. It is not available in Run mode.

READ FIRST

1. Read `soul/soul.md` if present to understand the workflow objective and success criteria.
2. Read `builder/improve.html` if present. Use unresolved KB/db/report findings, prior failed cleanup attempts, recent Pulse fixes or Goal Advisor actions, and plan changes as context.
3. Read `planning/plan.json` and `planning/step_config.json` if present so the KB improvement is aligned with the current plan.
4. Read `knowledgebase/notes/_index.json` before opening topic files.
5. Read only topic markdown files relevant to the requested cleanup or consolidation. Do not glob or load every `knowledgebase/notes/*.md` file.
6. If the focus is broad, names a step, or says to optimize for the plan, inspect the matching plan step(s) and recent iteration-0 outputs enough to understand what durable knowledge was produced.

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

1. Convert the user's request into one concrete instruction. If the focus is empty, base the instruction on `soul/soul.md`, `planning/plan.json`, unresolved findings and recent entries in `builder/improve.html`, and the KB index.
2. Return the instruction and mode as `recommended_fix`; there is no separate KB-maintenance tool.
3. Name affected topics, contradictions, pattern-note opportunities, index
   corrections, and remaining uncertainty.
4. Identify matching open findings only as evidence. The Pulse Fixer owns every
   file mutation and `builder/improve.html` update.
