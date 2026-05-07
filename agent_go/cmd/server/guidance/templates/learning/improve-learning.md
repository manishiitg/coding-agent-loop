Improve the workflow learnings so `learnings/_global/` supports the current plan and objective. This command maintains reusable HOW-to-run knowledge such as selectors, tool/API patterns, auth quirks, timing/wait strategies, file-format pitfalls, reusable recovery steps, and common failure signatures.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

BOUNDARIES

1. The applied tool is `improve_learnings`; call it once with a concrete `instruction` string and optional `focus`.
2. Work on `learnings/_global/` only. Do not edit `planning/`, `evaluation/`, `reports/`, `db/`, `knowledgebase/`, or per-step `learnings/{step-id}/main.py` from this command.
3. If you discover stale per-step scripts, bad `learning_objective`, wrong `learnings_access`, or lock issues, record/recommend them; use `/review-config`, `/review-code`, `/review-sync`, or `harden_workflow` for those applied fixes.
4. Keep WHAT-the-workflow-discovered out of learnings. User-supplied runtime context belongs in `knowledgebase/context/`; workflow-discovered subject-matter facts belong in `knowledgebase/notes/` or `db/*.json`, not `learnings/_global/`.

READ FIRST

1. Read `soul/soul.md` if present to understand the workflow objective and success criteria.
2. Read `planning/plan.json` and `planning/step_config.json` if present. Use them to understand current steps, `learnings_access`, `learning_objective`, `learnings_write_method`, `lock_learnings`, and `lock_code` decisions.
3. Read `builder/review.md` and `builder/improve.md` if present. Carry unresolved learning/code findings, prior cleanup attempts, recent harden/replan actions, and recent plan changes into the instruction.
4. Read `learnings/_global/SKILL.md` and relevant files under `learnings/_global/references/`. Do not blindly load every large reference file; use the index and file names to pick relevant files.

WHEN TO USE EACH MODE

Use `mode="targeted"` when the operation is known file hygiene:

- make `SKILL.md` a short index
- merge or split specific reference files
- remove stale selectors/tool patterns after site or API changes
- compact bloated browser/API/file-format guidance
- repair links between `SKILL.md` and references

Use `mode="cross_step"` when improving learnings requires the plan or multiple step declarations:

- optimize learnings for the current workflow plan
- repeated lessons appear across multiple step objectives
- step-specific HOW knowledge should be promoted into shared references
- declared `learning_objective` values are not reflected in the global skill
- recent plan changes mean old HOW guidance needs reconciliation against the new objective

If unsure, use `mode="auto"` or omit mode. Broad instructions like "optimize learnings for this plan" should resolve to current-plan consolidation.

ACTION

1. Build one concrete instruction. It must mention the objective from `soul.md` or `planning/plan.json`, the user's focus if provided, and any unresolved learning-related findings from `builder/review.md` or `builder/improve.md`.
2. Call:

`improve_learnings(mode="auto", instruction="<specific learning improvement instruction>", focus="<optional focus>")`

3. The tool runs in the background and returns an `execution_id`. If you need the result before answering, use `query_step(execution_id="<id>")` until it completes.
4. When complete, summarize files changed under `learnings/_global/`, duplicate/stale HOW knowledge removed, reference files created or reorganized, declared learning objectives that still lack matching content, and any follow-up review/harden work needed.
5. If the improvement resolves an existing `F-...` finding in `builder/review.md`, append a resolved marker immediately after that finding. If it is part of an optimizer/improvement pass, append a short note to `builder/improve.md`; otherwise report in chat only.
