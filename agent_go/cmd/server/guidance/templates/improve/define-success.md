Set up outcome goals and measurable progress for this workflow. This is the
shared flow for /setup-goals, legacy /define-success, and first-time Pulse setup.
{{if .Focus}}
User context: {{.Focus}}
{{end}}

## Inspect before asking
Read soul/soul.md, the current plan/config, reports and relevant existing data.
Call get_goal_metrics(workspace_path=...) for current definitions and history.
Use known user intent and prior answers. Do not ask the user to repeat them.

## Agree on outcomes and measurement
Present a short proposal: outcome goal bullets, one primary metric, supporting
metrics, a target or "establish a baseline first", and explicit boundaries.
Ask only about unresolved priorities, targets or material changes to meaning.
For X, distinguish followers from paid subscribers; use received engagement,
not actions performed, to describe audience response. Activity metrics can
support an outcome but do not prove it improved.

Keep the confirmed outcome bullets under ## Objective in soul/soul.md. Preserve
user constraints under ## Constraints. Keep nonnumeric acceptance conditions
under ## Success Criteria. Keep this required section even when all acceptance is numeric: refer to the configured goal metric targets and agreed boundaries without duplicating their numbers. Metric targets belong in typed definitions, not a
second manually maintained metric table. Move implementation details to the
appropriate plan/config through its managed tools, preserving behavior and
explicit approvals. Do not discard existing success commitments during migration.

## Configure once
Call configure_goal_metrics with the COMPLETE active list (omitted metrics are
retired, not deleted). Exactly one metric is primary. Each definition has:
- id and criterion_id: stable IDs, reusing comparable existing observation IDs;
- name, role (primary/supporting), unit, direction (increase/decrease/maintain);
- definition: precise calculation/aggregation including denominator where relevant;
- source: exact existing query, API, output field or collector location;
- window: instant, trailing 7 days, post age 24 hours, etc.;
- route and environment: explicit comparable scope (empty means only unscoped records);
- collection_frequency and freshness_hours: how often to collect and when data is stale;
- optional target and target_date (YYYY-MM-DD): use user-agreed targets only.
Do not invent measurements, targets or a universal success percentage.
Changing definition/unit/window/source/scope/direction requires a NEW metric ID.
Reruns reuse unchanged IDs and preserve historical data; never reset history.

## Connect and verify collection
Add collection to the canonical workflow using managed plan/config tools. Prefer
scripted reads/calculations for deterministic work. Collect independently of
whether Pulse reviews run. Use record_goal_observations with metric=id, the exact
criterion_id/unit/route/environment, source run_id, actual observed_at (RFC3339),
value and evidence. For unavailable measurements omit value and supply status;
never report zero for failed/missing collection. Discover the tool's live schema
and wire it through the granted MCP bridge, not direct SQLite writes.
Schedule a delayed refresh when results mature later (e.g. post age 24 hours or
7 days); honor existing scheduling and external-action boundaries. Never create
a recurring external action merely to collect a metric.

For existing workflows, backfill only verifiable, comparable source history,
using original run IDs and observation timestamps. Keep incompatible old records
available; don't merge them into the new series. Verify a collected value against
its source. A saved definition alone does not prove collection works. If source
access or instrumentation is missing, report exactly what is needed and retain
"Measurement setup needed"; do not claim completion.

If this was invoked to enable Pulse, after the goal/definitions are agreed call
update_workflow_config(pulse_enabled=true). Existing runs must not be disabled
while measurement is being configured. Baseline collection may continue after
Pulse is enabled; no fabricated baseline or target is required to enable it.

## Finish
Summarize the goal, primary/supporting metrics, sources, verified collection,
backfill coverage and any outstanding decisions. No separate Markdown review,
HTML dashboard or manual chart: Pulse renders the typed data automatically.
