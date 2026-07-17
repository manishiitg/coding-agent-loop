# MANUAL ONE-OFF PULSE

Run one complete Pulse now against retained workflow evidence. This is the
manual equivalent of the post-run Pulse pass. It must not create, edit, enable,
or disable a schedule, change `post_run_monitor`, or run the workflow itself.
Use `/pulse-setup` when recurring Pulse needs configuration.{{if .Focus}}

User focus: {{.Focus}}.{{end}}{{if .RunFolder}}

Use `{{.RunFolder}}` as the primary run folder.{{end}}

## Canonical contract

1. Call `get_reference_doc(kind="post-run-monitor")` and
   `get_reference_doc(kind="review-improve-log")`. Follow the same Gate,
   reviewed-baseline cadence, parallel read-only reviewer, single Pulse Fixer,
   module-result, dashboard, backup, publish, and notify contracts.
2. Select the latest meaningful retained run when no run folder was supplied.
   State the selected run before reviewing it. Do not generate fresh workflow
   evidence merely to make Pulse run.
3. Create a unique `pulse_run_id` beginning with `manual-` and the current UTC
   timestamp. Read `get_pulse_module_state`, then perform the progressive Gate
   scan and call `record_pulse_worklist` exactly once with one decision for all
   ten canonical modules.
4. Run every module Gate marks due. Issue one independent `call_generic_agent`
   reviewer call per due module in the same parallel batch. Never select only a
   "top 3" subset. If a provider limit requires multiple batches, cover all due
   modules in the fewest consecutive batches. Every reviewer is read-only. Goal
   Advisor uses a separate read-only critic after its strategy draft.
5. The current parent remains the only Pulse Fixer. Consolidate reviewer
   results, apply bounded safe fixes sequentially, verify them, process exact
   approved decisions where applicable, and update `builder/improve.html` once
   atomically with one explicitly attributed result card per due module.
   Read-only maintenance-review results are **Signals / Kizuki**. Add Reflection
   / Hansei for run interpretation, cadence, and answered questions; add
   Improvements / Kaizen for verified Pulse Fixer work or Goal Advisor
   proposals/decisions. Never
   let a reviewer mutate the workflow.
6. Call `mark_pulse_module_result` exactly once for every due module, including
   clean, changed, blocked, and failed reviews. A failed reviewer fails only its
   own module; continue independent safe work.
7. Confirm all due modules have terminal results, then run the normal ordered
   finalizer. Track dashboard, backup, publish, and notify with
   `mark_pulse_final_command_result`. Respect configured disabled/unverified
   states and record them as skipped rather than silently omitting them.

## Manual-run boundary

- Do not call schedule create/update/delete/trigger tools.
- Do not call `run_full_workflow` or `execute_step`.
- Do not enable Pulse as a side effect.
- Do not turn an operational correctness repair into a user decision.
- Do not apply a new strategy, business-semantic, or LLM/provider change without
  the existing exact approval flow.

Finish with one concise summary: evidence/run reviewed, modules due and skipped,
fixes made, unresolved decisions, finalizer statuses, and the next evidence
checkpoint.
