package server

const upgradeEvalRetirement = `WORKFLOW CONTRACT UPGRADE: RETIRE LEGACY EVALUATION WITHOUT MANDATING MEASUREMENT TOPOLOGY.

Do only this one-time migration. The evaluation executor and its tools are retired. Outcome measurement now belongs to ordinary producing steps, route-local producers, scheduled collectors, engine telemetry, and the goal-observation ledger. Do not run the workflow, invoke a retired evaluation tool, or perform any public action.

The workflow path is {{WORKSPACE_PATH}}. First inventory legacy dependencies before deciding whether any change is required:

1. Check whether evaluation/evaluation_plan.json exists. If it does, parse it and treat it only as a read-only description of measurements the workflow used to perform. Never edit, delete, move, validate, or execute it. Its presence alone does not require a new step or route.
2. Read soul/soul.md, planning/plan.json, planning/step_config.json, and the producing steps' current output/database contracts. Call get_goal_metrics(workspace_path={{WORKSPACE_PATH}}) and inspect the complete configured definitions and recent observations.
3. Inspect every schedule with list_schedules for instructions that invoke a retired evaluation tool or promise/wait for an evaluation pass. Inspect db/reports/index.html when present for active reads of eval_results, evaluation reports, or other retired evaluation artifacts.

For each still-relevant success-criterion measurement described by the legacy plan, choose the smallest safe disposition from evidence:

- If an existing ordinary step, route-local step, scheduled collector, producer-owned database row, or engine telemetry already supplies equivalent run-scoped evidence, reuse it. Do not change plan topology merely for uniformity and do not copy the value into a second metrics store.
- If the value should appear in Pulse's durable goal-progress history, make the existing producer or collector call record_goal_observations with the configured metric identity, immutable source run ID, actual observation time, and evidence. Reviewers may continue to query producer-owned tables directly when trend history is unnecessary.
- If a required measurement has no safe existing owner, add an ordinary producer step only when it is genuinely shared workflow behavior, or place it in an existing route when it is route-specific. A dedicated step is a last resort, not a default. Use normal typed plan/config tools and preserve route, group, side-effect, approval, and failure semantics.
- If a configured goal metric's source depends on retired evaluation output, replace only that affected definition through configure_goal_metrics while preserving the complete unaffected active list, goal meaning, unit, target, route, environment, and observation history. Follow the tool's immutable-series rule when a source change requires a new metric ID. Do not invent a target or fabricate/backfill observations.
- Rewrite only affected report queries and schedule wording. Reports should read producer-owned outputs or workflow_goal_metrics/pulse_goal_observations; schedules must not invoke, promise, or wait for a retired evaluation pass. Do not add measurement to a schedule that did not already own a genuinely required collector.

An evaluation check that merely duplicated current producer validation, operational health checks, or already-sufficient stored outcomes needs no replacement. If the legacy plan is absent, or it exists but no current success criterion, configured goal metric, report, or schedule depends on it, this migration is a verified no-op: do not create any step, route, table, collector, or goal metric.

Validate every changed plan/config with the normal typed validation flow and every changed report with validate_report_html. Re-read the plan, goal definitions, report, and schedules that changed. Confirm that active behavior no longer depends on retired evaluation execution and that all unrelated behavior is unchanged. Legacy evaluation files, reports, score ledgers, and database rows remain read-only history.

If equivalence is ambiguous, a required source cannot be identified, or a safe plan change cannot be made with the available typed tools, create one precise human-input request describing the unresolved mapping and do not stamp. Otherwise, after either verified changes or a verified no-op, call set_workflow_contract_version(version="1.0.43") and stop.`
