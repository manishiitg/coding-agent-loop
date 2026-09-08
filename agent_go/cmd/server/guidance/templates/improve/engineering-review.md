# TECHNICAL REVIEW PHASE

Run the operational Review contract directly in this continuing Workflow Builder conversation.
Do not modify implementation files and do not run Pulse Gate,
Strategy Auditor, Goal Advisor, Dashboard, publish, or notify.{{if .Focus}}

Focus especially on: {{.Focus}}. The focus sets priority; it does not suppress
a lightweight scan for critical technical evidence.{{end}}{{if .RunFolder}}

Use `{{.RunFolder}}` as the primary retained run.{{end}}

1. Load `read_skill(skills=[{"name":"builder-reference","path":"references/pulse-review-fixer.md"},{"name":"workflow-commands","path":"references/ops-review.md"}])`.
   Treat `ops-review.md` as the canonical guide to focused operations investigation.
   Its diagnostic checks are conditional on the selected question, not a
   mandatory whole-workflow checklist. This continuing Review command overrides only that reference's
   Standalone Operations Review dispatch and read-only return wrapper: do not launch its
   standalone wrapper. Apply only its relevant checks inside this conversation,
   persist evidence-backed findings, and leave implementation changes to a
   later Fix phase. The caller may supply that phase as the next
   message in this retained conversation, or the operator may invoke
   `/pulse-fixer` separately; neither possibility grants mutation authority in
   this review turn.
2. Use `pulse_run_id="current"`, which resolves to this current Workflow Builder
   chat. This is a manual Technical Review, not a scheduled Pulse Gate pass:
   call `record_pulse_module_due(workspace_path=<this workflow>,
   pulse_run_id="current", module="technical_review", reason=<the explicit
   command/focus that requested this review>)` exactly once. Do not call
   `record_pulse_worklist`, do not select among Plan Drift, Technical Review,
   and Strategic Review, and do not change another module's cadence. If the
   due claim is refused because a scheduled Pulse pass is already reviewing
   `technical_review`, stop and report that collision instead of retrying or
   overwriting its state. Then read
   `get_pulse_state(view="focus_agenda", module="technical_review", route_scope=<relevant route>)`, perform a
   lightweight scan for critical regressions, reproduced defects, answered
   decisions, plan routes, and retained run selectors, then choose the smallest
   sufficient route-aware technical focus set using priority plus durable
   rotation history. Route size is evidence, not a mechanical quota. Then read the retained active backlog and newly reproduced defects,
   `get_pulse_state(view="backlog", detail="compact")` exactly once, plus the
   latest meaningful outputs and summaries. Inspect affected plan/store state
   and cost/runtime evidence only as needed. Rank issues by impact on required
   outcomes and useful improvements, not tool-error counts. Small recovered
   tool failures with correct outputs and negligible overhead do not merit an
   issue or deeper investigation. Expand only for a material concern; unrelated
   checks may be skipped without creating follow-up work.
   **Navigate the Pulse store deliberately:** `issues` are the canonical repair
   register; work from those roots first. `closed_issues` are prior roots to
   reuse when new evidence is semantically the same. Historical `observations`
   are audit-only and are not a review queue. Inspect the retained run artifacts
   and deterministic runtime/validation receipts directly, then use judgment to
   create, reopen, or reject a canonical issue only when that evidence supports
   it. Never treat a raw historical observation count as either a clean system
   or a backlog of confirmed bugs. Then request
   `detail="full"` only for those exact IDs (at most 20 per call). Never reload
   the complete compact index merely to filter or confirm a small ID set.
3. Own the review yourself. Use a specialist child only when independent focused
   analysis is genuinely useful; wait for its automatic completion and
   consolidate it before persistence. For every selected workflow observation,
   link it to an existing issue, promote it with evidence, or reject it as a
   non-issue. Persist typed findings and any reproduced failures as they are
   established. Applied fixes stay closed unless the defect is reproduced;
   missing stronger proof is not a reason to review them again. Do not create a Markdown review report. For an exceptional
   repair that genuinely requires operator judgment, create or refresh one
   `create_human_input_request(source="technical_review", input_id="technical-decision-...", options=[approve,reject,defer])`
   before filing it with `recommended_route="decision_required"`, and pass the
   returned id as `human_input_id` to `record_pulse_finding`. Never leave a
   decision-required finding without a real pending question. Normal safe
   engineering repairs use `fixer_handoff` and do not consume operator attention.
4. Deduplicate by root cause and leave one compact, ordered canonical repair
   queue. Do not apply repairs. Record the chosen focus exactly once, then call
   `record_pulse_result(module="technical_review", result="done", ...)` exactly
   once with the truthful review outcome and evidence. The same retained Review+Fix task may later add a supplemental changed result with repair
   dispositions; it must not invent a separate completion handshake.
5. Finish with a concise summary of what was reviewed, promoted, linked,
   rejected, already verified, awaiting evidence, or blocked. State whether at
   least one safe canonical issue is actionable for the later Fix phase; do not
   assume whether the caller supplied that phase or the operator
   will invoke `/pulse-fixer` separately.
