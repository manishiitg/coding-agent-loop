# Outcome goals and measurable progress

Pulse starts with outcome goals, then one primary metric and supporting metrics.
Acceptance conditions and explicit user boundaries remain available as expandable
sections. The current soul document stays canonical for intent; SQLite owns metric
definitions and observations. There is no extra reviewer-authored report.

## Setup

- New workflow: enabling Pulse checks whether metrics are configured. If not, it
  opens the shared setup conversation before enabling Pulse. The builder presents
  outcomes, primary/supporting metrics, targets or baseline collection, and known
  boundaries. It asks only unresolved questions, then enables Pulse after agreement.
- Existing workflow: `/setup-goals`, its `/define-success` alias, or the Pulse
  "Set up goals & metrics" button runs the same flow. Existing runs are not stopped.
- Setup is idempotent. Existing targets/constraints are preserved unless the user
  agrees to change them. Historical goal prose is not automatically rewritten.

## Data contract

`configure_goal_metrics` saves the complete active list to
`workflow_goal_metrics` in the workflow database. Exactly one metric is primary.
Definitions include stable ID, criterion, name, unit, direction, calculation,
source, window, route/environment, collection cadence, freshness tolerance, and
optional target/date. Omitting a metric retires its definition without deleting
its observations. Reusing an ID after changing its calculation, source, unit,
window, scope, or direction is rejected; use a new ID for the new series.

`get_goal_metrics` returns definitions, recent history and deterministic progress
snapshots suitable for review and notifications. Active streams retain up to 120
recent observations each in the response, independently of unrelated Pulse rows;
the underlying database retains the full history.

`record_goal_observations` is available to producing runs and collectors. It uses
`pulse_goal_observations` and requires configured IDs, matching scope/unit, source
run identity, observation time and evidence. Missing values are represented by a
status, never zero. Repeated identical submissions are idempotent; conflicting
values for the same run/metric/scope are rejected. Use the actual collection run
ID for delayed refreshes, retaining source/post identity in evidence.

The builder wires collection using managed workflow tools and verifies a value
against its source. A definition is not an executable query or proof of a working
collector. Deterministic collection should be scripted; delayed outcomes need a
suitable later collection run. Merely saving definitions never launches external
requests or changes existing schedules. Backfill uses original observation times
and only evidence that matches the new definition; incompatible historical rows
are retained but excluded from the displayed series.

## Presentation

The primary card shows current value, window, configured target, change since the
previous measurement and a dated trend. Supporting cards show the same facts at
smaller scale. Expand details for definition, source, cadence and exact history.
States distinguish missing setup, unavailable collection, stale data, initial
baseline collection, tracking and a currently observed target met. Missing latest
collection never falls back silently to an earlier successful value. There is no
invented universal progress percentage. Target met is a metric state, not a claim
that every goal and boundary is satisfied.

Run/Pulse summaries receive platform-computed Goal progress and Supporting metrics
sections ahead of review details, including when agents provide rich email HTML.
The provider reads the trusted notification workspace; recipients and routing are
unchanged. If readings cannot be loaded, the summary says progress is unavailable.
Reviewers interpret the same definitions/history; they do not collect measurements
or maintain parallel Markdown/chart reports.

## Rollout

No live workflow is bulk-migrated. Begin with `/setup-goals` in social-media to map
existing follower history, then LinkedIn/Substack. Target choices and semantic
changes remain user decisions. Workflows with missing outcome instrumentation
can keep running while setup identifies and connects the required collector.

## Using measurements in workflow reports

The report host exposes the same scoped metric history and progress calculations
used by Pulse. Add an optional prebuilt section without designing a chart:

```html
<section id="goal-progress"></section>
<script>
window.report.ready(async () => {
  await window.report.renderGoalProgress('#goal-progress');
});
</script>
```

For a custom visualization, use `await window.report.getGoalMetrics()` inside
`ready`. The result is `{ metrics, observations, progress }`; progress entries
include the metric definition, current value, delta, state, freshness, target
status, and recent comparable history. Up to 120 observations per active metric
are returned. The existing read-only `window.report.query` can query the full
`workflow_goal_metrics` / `pulse_goal_observations` history when needed.

The widget shows the primary metric first, supporting metrics, trends, targets,
last observation time, and expandable definitions and evidence. Styling is bundled
and isolated from the report's CSS. It works in both the app and `preview_report`,
refreshes through the standard `ready` lifecycle, and never replaces missing or
failed measurements with invented values. Legacy workflows show a setup message.
Report field edits cannot modify these managed measurement tables.
