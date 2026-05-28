Improve the workflow knowledgebase notes so they support the current plan and objective.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

Before writing builder/improve.html, call get_reference_doc(kind="html-output") to load the HTML style guide. Write a self-contained HTML file — not Markdown.

MIGRATION (one-time): Check whether builder/improve.md exists. If it does, read it, extract all unresolved entries, incorporate them into builder/improve.html, then delete builder/improve.md with execute_shell_command.

BOUNDARIES

1. Work only on `knowledgebase/notes/` and `knowledgebase/notes/_index.json`.
2. Never read or write `knowledgebase/context/`. That folder is user-owned runtime business context, not optimizer-maintained notes.
3. Do not edit planning files, eval files, report files, learnings, or db files unless the user explicitly asks outside this command.
4. This command is allowed in Builder and Optimizer because KB shape can be part of workflow design or optimizer cleanup. It is not available in Run mode.

READ FIRST

1. Read `soul/soul.md` if present to understand the workflow objective and success criteria.
2. Read `builder/review.html` and `builder/improve.html` if present. Use unresolved KB/db/report findings, prior failed cleanup attempts, recent harden/replan actions, and plan changes as context.
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

ACTION

1. Convert the user's request into one concrete instruction. If the focus is empty, base the instruction on `soul/soul.md`, `planning/plan.json`, unresolved `builder/review.html` findings, recent `builder/improve.html` entries, and the KB index.
2. Call:

`improve_kb(mode="auto", instruction="<specific KB improvement instruction>", focus="<optional focus>")`

3. After the tool returns, inspect the summary. If it reports no change, explain why. If it reports changes, summarize the affected topics, contradictions surfaced, pattern notes written, and remaining uncertainty.
4. If the improvement resolves an existing `F-...` finding in `builder/review.html`, append a resolved marker immediately after that finding. Otherwise do not create review findings from this command.
5. If this is part of an optimization action or scheduled improvement, append a short note to `builder/improve.html` with the instruction, evidence, mode, and tool result. Otherwise, report in chat only.
