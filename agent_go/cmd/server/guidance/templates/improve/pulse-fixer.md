# PULSE FIX PHASE

Run only after Technical Review has classified current workflow evidence. This
may be an explicit standalone `/pulse-fixer` command or the caller-supplied
later Fix message in one retained Review+Fix sequence. For that sequence,
confirm the completed `technical_review` receipt for this conversation first;
if it is missing or incomplete, report the limitation and do not apply repairs.
Do not rerun Technical Review, Strategic Review, or broad
discovery. Do not treat raw workflow observations as repair issues.{{if .Focus}}

Focus especially on: {{.Focus}}. The focus affects priority only; it cannot
promote an observation or bypass evidence.{{end}}{{if .RunFolder}}

Use `{{.RunFolder}}` as the primary retained run when it is relevant to the
selected issue's proof boundary.{{end}}

1. Load `read_skill(skills=[{"name":"builder-reference","path":"references/pulse-fixer-practices.md"},{"name":"builder-reference","path":"references/fix-verification.md"}])`.
2. Use `pulse_run_id="current"`. Call
   `list_approved_fixer_decisions(workspace_path=<this workflow>)` exactly once
   before ordinary queue selection. Each returned candidate is an **explicit
   operator-approved mandatory Fixer handoff**: it overrides normal
   `repair_eligible` filtering. Read the exact `get_human_input_request` record
   and then its returned public PUL issue with `get_pulse_state(...,
   detail="full", issue_ids=[...])`. Apply only its `approved_scope`, checks,
   proof boundary, and failure policy; never consume it when that bounded repair
   cannot be proved. A candidate with no linked PUL id is a lifecycle defect:
   preserve it and state that it could not be safely repaired, rather than
   selecting unrelated work.
3. Read the saved Gate worklist and
   `get_pulse_state(view="backlog", detail="compact")` exactly once. If there
   was an approved candidate, it is the first repair bundle. Otherwise select a
   bounded repair batch from this index, then request `detail="full"` only for
   its exact issue IDs. Read their typed review records, attempts, verification,
   and the current Technical Review checkpoint when this run has one. Manual
   slash-command sequences may have only typed review state and no Markdown
   checkpoint; do not treat that absence as missing review. The compact backlog's `issues` feed
   is the ordinary repair queue. Workflow observations are evidence
   from reviewers and must not be repaired unless a reviewer already promoted them.
4. Select a bounded canonical **repair batch**. Start with the highest-value
   coherent repair bundle; it may include several issue IDs when they share one
   root cause, compatible targets, and one proof boundary. Then add another
   independent bundle only when it is low-risk, needs no broad rediscovery,
   has a separately clear proof boundary, and can be completed with the
   evidence/context already loaded or a targeted read. Do not use a fixed issue
   count. Defer a bundle that needs a different route, public action, user
   decision, broad investigation, or a new context window. Preserve every
   unselected issue unchanged.
5. Apply each selected bundle and perform only proportional immediate checks
   before starting the next. Use the
   smallest complete safe repair with normal Workflow Builder tools. Record
   exact changed targets, attempts, dispositions, and proportional post-change
   checks per bundle. A successfully applied fix closes now. Use
   `changed_unverified` when stronger runtime proof is unavailable; do not wait
   for a producing run, schedule verification work, or keep it in the queue.
   A failed mutation or a check reproducing the defect remains active.
6. Record the repair attempt and each issue disposition, but do not write or
   replace either reviewer's terminal receipt. Review completion and repair
   outcome are separate facts. If no safe canonical objective exists, preserve
   the truthful terminal module results without manufacturing a repair. When this is
   the caller-supplied Fix message of a combined manual Review+Fix sequence,
   also call `record_pulse_result` exactly once for `technical_review` with
   `pulse_run_id="current"` and the truthful repair/no-safe-repair outcome.
7. Finish with a concise statement of the selected repair batch: bundles and
   issue IDs, changes and proof per bundle, lifecycle outcomes, and the
   remaining canonical queue with defer reasons.
