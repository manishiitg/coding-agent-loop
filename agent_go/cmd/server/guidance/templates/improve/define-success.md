Set up outcome goals and measurable progress for this workflow. This is the
shared flow for /setup-goals, legacy /define-success, and first-time Pulse setup.
{{if .Focus}}
User context: {{.Focus}}
{{end}}

## Inspect before asking
Read soul/soul.md, the current plan/config, reports and relevant existing data.
Call get_goal_metrics(workspace_path=...) for current definitions and history.
Use known user intent and prior answers. Do not ask the user to repeat them.

## Choose workflow boundaries by the work
A workflow groups work that needs to be planned, executed, and evaluated together.
It can have multiple goals and primary metrics. Keep shared investigations,
actions and routine outcome tradeoffs together. Consider splitting independently
operating work, especially when owners or permission boundaries differ. Sharing
a server alone does not justify grouping; different schedules alone do not justify
splitting because routes can have separate schedules.
Practical test: would separate workflows repeatedly coordinate the same investigation
or change? Keep that work together. If they mostly exchange results, suggest separate
workflows with shared evidence and guardrails. Explain the recommendation using the
actual routes and let the user choose. Never force or execute a split because of the
number of goals, routes or primary metrics; preserve existing boundaries unless the
user authorizes restructuring.

## Agree on outcomes and measurement
Present a short proposal: outcome goal bullets, one or more primary metrics per goal, their supporting
measurements, a target or "establish a baseline first", and explicit boundaries.
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
retired, not deleted). At least one metric is primary; multiple goals and primary metrics are supported. Each definition has:
- id and criterion_id: stable IDs, reusing comparable existing observation IDs;
- name, role (primary/supporting), unit, direction (increase/decrease/maintain);
- goal_id and goal_name together on primary metrics to group them by outcome;
  reuse the same goal_id/name for metrics measuring the same goal. Keep goal IDs
  separate from immutable historical criterion_id values;
- supports on every supporting metric: IDs of the primaries it explains or constrains;
  one supporting measurement may serve several primaries and inherits their goals;
- support_kind: breakdown (same measurement by cohort), diagnostic (explains movement),
  or guardrail (a constraint). Preserve unclassified legacy supporting measurements
  until their relationship is understood; do not automatically promote them;
- optional dimensions: fixed key/value slice such as language=English or endpoint=/sessions.
  Each slice uses its own stable metric ID and exact scoped collector query. A changed
  slice requires a new ID. Existing named slice metrics keep their IDs and definitions
  without requiring dimensions metadata to be backfilled. Never average percentiles
  or sum component medians to manufacture an overall metric;
- definition: precise calculation/aggregation including denominator where relevant;
- source: exact existing query, API, output field or collector location;
- window: instant, trailing 7 days, post age 24 hours, etc.;
- route and environment: explicit comparable scope (empty means only unscoped records);
- collection_frequency and freshness_hours: how often to collect and when data is stale;
- optional target and target_date (YYYY-MM-DD): use user-agreed targets only.
Do not invent measurements, targets or a universal success percentage.
Changing definition/unit/window/source/scope/direction/dimensions requires a NEW metric ID.
Changing role, goal grouping or supporting links preserves measurement history.
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
Summarize the goals, primary metrics and their supporting measurements, sources, verified collection,
backfill coverage and any outstanding decisions. No separate Markdown review,
HTML dashboard or manual chart: Pulse renders the typed data automatically.
