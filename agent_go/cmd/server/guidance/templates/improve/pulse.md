# MANUAL ONE-OFF PULSE

Run one standalone Pulse review now against retained workflow evidence. This is
for testing and investigation; it does not participate in the scheduler's
durable worklist, cadence, timeout, or final-command state machine. Use the
Pulse toolbar Run action for the full scheduler-equivalent workflow + Pulse
pipeline. This command must not create, edit, enable, or disable a schedule,
change `post_run_monitor`, or run the workflow itself. Use `/pulse-setup` when
recurring Pulse needs configuration.{{if .Focus}}

User focus: {{.Focus}}.{{end}}{{if .RunFolder}}

Use `{{.RunFolder}}` as the primary run folder.{{end}}

## Canonical contract

1. Call `get_reference_doc(kind="post-run-monitor")` and
   `get_reference_doc(kind="review-improve-log")`. Follow the same evidence
   review, parallel read-only reviewer, single Pulse Fixer, and HTML contracts.
2. Select the latest meaningful retained run when no run folder was supplied.
   State the selected run before reviewing it. Do not generate fresh workflow
   evidence merely to make Pulse run.
3. Read `get_pulse_module_state` only as historical context. Decide which
   modules are useful for this standalone review, but do not call
   `record_pulse_worklist`, `mark_pulse_module_result`, or
   `mark_pulse_final_command_result`; those tools are reserved for an active
   scheduler-issued Pulse run.
4. Run every module this standalone review selects. Issue one independent
   `call_generic_agent` reviewer call per due module in parallel batches of at
   most four. Never select only a "top 3" subset. Cover all selected modules in
   the fewest consecutive batches. Every reviewer is read-only. Goal Advisor
   uses a separate read-only critic after its strategy draft.
5. The current parent remains the only Pulse Fixer. Consolidate reviewer
   results, apply bounded safe fixes sequentially, verify them, process exact
   approved decisions where applicable, and update `builder/improve.html` once
   atomically with one explicitly attributed result card per due module.
   Read-only maintenance-review results are **Signals / Kizuki**. Add Reflection
   / Hansei for run interpretation, cadence, and answered questions; add
   Improvements / Kaizen for verified Pulse Fixer work or Goal Advisor
   proposals/decisions. Never
   let a reviewer mutate the workflow.
6. Record every selected module's clean, changed, blocked, or failed outcome in
   `builder/improve.html`. A failed reviewer fails only its own module; continue
   independent safe work.
7. Do not run the scheduler finalizer from this standalone command. Do not
   publish or notify. Back up before any local mutation, and report backup
   status in the final summary.

## Manual-run boundary

- Do not call schedule create/update/delete/trigger tools.
- Do not call `run_full_workflow` or `execute_step`.
- Do not enable Pulse as a side effect.
- Do not turn an operational correctness repair into a user decision.
- Do not apply a new strategy, business-semantic, or LLM/provider change without
  the existing exact approval flow.

Finish with one concise summary: evidence/run reviewed, modules selected and
omitted, fixes made, unresolved decisions, backup status, and the next evidence
checkpoint.
