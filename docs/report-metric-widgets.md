# Reusable report data and widgets

Workflow reports can read existing platform data or render optional prebuilt sections.
The widgets include responsive styles; authors only choose where to place them.

| Data function | Optional widget | Source |
| --- | --- | --- |
| `getGoalMetrics()` | `renderGoalProgress('#goals')` | Managed goal definitions and observations |
| `getEvaluations()` | `renderEvaluations('#evals')` | Stored evaluations, joined to the current evaluation plan |
| `getCosts({ days: 30 })` | `renderCosts('#costs', { days: 30 })` | Same canonical ledger summary as the Costs view |

```html
<section id="goals"></section>
<section id="evals"></section>
<section id="costs"></section>
<script>
window.report.ready(async () => {
  await Promise.all([
    window.report.renderGoalProgress('#goals'),
    window.report.renderEvaluations('#evals'),
    window.report.renderCosts('#costs', { days: 30 })
  ]);
});
</script>
```

Use empty `div`/`section` containers. Each renderer replaces its own contents on
refresh, returns its data, and rejects on load failure. The app and `preview_report`
share the runtime. No additional collector or duplicate reporting tables are needed.

## Evaluation data

`getEvaluations()` returns `results`, grouped `criteria`, `run_count`, `result_limit`
(200), and `possibly_truncated`. A criterion contains `id`, `title`, `historical`,
`latest`, and `history`. Each result preserves run, date, raw score/max score,
captured/skipped flags, reasoning and evidence. The widget shows the latest score
per criterion with expandable history and groups historical criteria separately.
There is no invented aggregate score or pass/fail threshold. Missing and skipped
scores remain explicit. Run counts describe the returned recent history only.

## Cost data

`getCosts(options)` returns `summary`, `history`, `window_total_usd`, and `state`.
The `summary.total` and `by_scope` cover **all time**; `by_model`, `by_date`, and
`window_total_usd` cover the selected UTC window. The widget labels these scopes
separately and provides a daily trend and expandable date/activity/model tables.
An unavailable ledger returns a null summary and undefined period total.

Options: `days` is an integer from 1 to 90 (default 30); `before` is an exclusive
YYYY-MM-DD date. For older daily history, pass `history.next_before` when
`history.has_more` is true. All-time totals repeat on each page; do not sum them.
Amounts are recorded USD costs, not an assertion that all usage has been priced.
The preview endpoints are bound to the preview token's workflow and force the
bounded cost-summary reader. Metric helpers expose data reads only.
