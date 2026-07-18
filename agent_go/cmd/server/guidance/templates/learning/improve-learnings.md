# READ-ONLY LEARNING HEALTH REVIEW

Review whether `learnings/_global/` supports the current plan and objective. This
checklist is passed to a generic read-only reviewer. Do not edit any file, update
`builder/improve.html`, or call module-result or human-input tools. Any later
wording such as improve, apply, edit, update, remove, merge, or resolve describes
what the **Pulse Fixer** should do after consolidating all reviewer findings; it
is not permission for this reviewer to mutate anything.

EXECUTION

The parent Workshop/Pulse agent first loads `assumption-audit`, then passes its
relevant lens and this rendered checklist to
`call_generic_agent` in an instruction beginning with `READ-ONLY REVIEW` and
waits for its synchronous result. The parent then validates and applies any
bounded safe edit. Do not create a dedicated learning-maintenance agent or use
`run_in_background` for this review.

Return only this compact contract:

- `module`: learning_health
- `verdict`: clean | needs_fix | blocked
- `findings`: stable `finding_id`, `target_key`, severity, plain-language
  summary, exact problem, and why it matters
- `evidence`: precise paths and relevant step ids
- `recommended_fix`: bounded edits for the Pulse Fixer
- `verification`: exact checks for the Pulse Fixer
- `user_judgment_required`: yes/no with reason
- `next_check`: evidence or cadence condition for another review

Use the remaining document only as the learning-health audit checklist.

Read `builder/improve.html` for prior context and matching open findings, but do
not write it. Use targeted semantic reads only; do not inspect CSS, load HTML
style/skeleton guidance, migrate markup, or format cards. The Pulse Fixer owns
the consolidated log update.

Apply the parent-provided `assumption-audit` learnings/skills lens within this command's boundaries. Reusable HOW belongs here; business policy, fixed strategy, architecture preferences, and unverified limitations do not become true because they were written into a skill. Recommend removing or qualifying stale assumptions and surface consequential unresolved ones for Pulse's Assumptions challenged.

This command maintains reusable HOW-to-run knowledge such as selectors, tool/API patterns, auth quirks, timing/wait strategies, file-format pitfalls, reusable recovery steps, and common failure signatures.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

BOUNDARIES

1. Return a concrete recommended instruction and optional focus for the Pulse Fixer; there is no separate learning-maintenance tool.
2. Work on `learnings/_global/` only. Do not edit `planning/`, `evaluation/`, `reports/`, `db/`, `knowledgebase/`, or per-step `learnings/{step-id}/main.py` from this command.
3. If you discover stale per-step scripts, bad `learning_objective`, wrong `learnings_access`, or lock issues, record/recommend them for the parent Pulse Fixer or an explicit manual fix. Eval rubric, coverage, or scoring issues belong to `/improve-evaluation`, not here.
4. Keep WHAT-the-workflow-discovered out of learnings. User-supplied runtime context belongs in `knowledgebase/context/`; workflow-discovered subject-matter facts belong in `knowledgebase/notes/` or `db/db.sqlite`, not `learnings/_global/`.
5. Enforce a lean index shape: `learnings/_global/SKILL.md` should stay under roughly 80-100 lines and act as an overview plus links to focused files under `learnings/_global/references/`. Detailed selectors, API quirks, auth flows, file-format notes, retry patterns, and step-specific HOW guidance belong in reference files, not in the root `SKILL.md`.

READ FIRST

1. Read `soul/soul.md` if present to understand the workflow objective and success criteria.
2. Read `planning/plan.json` and `planning/step_config.json` if present. Use them to understand current steps, `learnings_access`, `learning_objective`, `lock_learnings`, and `lock_code` decisions.
3. Read `builder/improve.html` if present. Carry unresolved learning/code findings, prior cleanup attempts, recent Pulse fixes or Goal Advisor actions, and recent plan changes into the instruction.
4. Read `learnings/_global/SKILL.md` and relevant files under `learnings/_global/references/`. Do not blindly load every large reference file; use the index and file names to pick relevant files.

WHEN TO USE EACH MODE

Use `mode="targeted"` when the operation is known file hygiene:

- make `SKILL.md` a short index with links to focused reference files
- merge or split specific reference files
- remove stale selectors/tool patterns after site or API changes
- compact bloated browser/API/file-format guidance
- repair links between `SKILL.md` and references
- remove or replace stale HOW guidance that no longer matches current step descriptions

Use `mode="cross_step"` when improving learnings requires the plan or multiple step declarations:

- optimize learnings for the current workflow plan
- repeated lessons appear across multiple step objectives
- step-specific HOW knowledge should be promoted into shared references
- declared `learning_objective` values are not reflected in the global skill
- recent plan or step-description changes mean old HOW guidance needs reconciliation against the new objective

If unsure, use `mode="auto"` or omit mode. Broad instructions like "optimize learnings for this plan" should resolve to current-plan consolidation.

REVIEW OUTPUT

1. Build one concrete instruction. It must mention the objective from `soul.md` or `planning/plan.json`, the user's focus if provided, and any unresolved learning-related findings from `builder/improve.html`.
   - Always include this invariant in the instruction: keep `learnings/_global/SKILL.md` lean as an index/overview; move detailed HOW-to-run content into `learnings/_global/references/<topic>.md` and link those files from `SKILL.md`.
   - Always include this stale-content rule: compare learnings against current `planning/plan.json` step descriptions and `planning/step_config.json` learning objectives; remove or replace HOW guidance that belongs to old step behavior, obsolete selectors/API paths, removed dependencies, or previous descriptions.
2. Return that instruction as `recommended_fix`; do not execute it.
3. Name the exact files that would change, stale or duplicate HOW content to
   remove, reference files to create or reorganize, and learning objectives that
   still lack matching content.
4. Identify matching open findings only as evidence. The Pulse Fixer owns any
   `builder/improve.html` update or close-out.
