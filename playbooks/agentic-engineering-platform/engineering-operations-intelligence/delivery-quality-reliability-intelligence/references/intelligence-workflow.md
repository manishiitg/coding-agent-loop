# Delivery, quality, and reliability intelligence workflow

## Freeze the analysis contract

Bind each analysis to a data-foundation revision, data-quality snapshot, metric-policy revision, team/repository/service scope, analysis window, comparison window, timezone/business calendar, and tested exclusions. Do not calculate a metric when its minimum freshness or coverage is unmet.

Start with a compact metric set that supports decisions:

- **Delivery flow:** work-item and PR cycle time, review wait, work in progress, aging work, and queue transitions.
- **Quality:** CI success/duration, failed-job categories, flaky-test impact, QA/security/performance finding trends, escaped or reopened defects when traceable.
- **Reliability:** deployment frequency, rollback or change-failure observations, incident frequency/duration, recovery time, and changes related through supported source links.

Use the customer's definitions. Avoid industry labels when the underlying events do not match their expected contract.

## Adapt the plan

A scripted snapshot step validates input quality, selects the declared population, calculates metrics from versioned definitions, stores denominators and distributions, compares compatible windows, and identifies threshold/anomaly candidates. Deterministic calculations must be reproducible from stored source IDs and timestamps.

One message sequence investigates candidates. It reopens supporting records, checks mapping and denominator changes, looks for seasonality or scope changes, considers competing explanations, and produces findings with confidence, evidence, impact, and a bounded recommended action. Correlation remains correlation unless stronger evidence supports causation.

A scripted finalizer accounts for every expected metric, persists accepted findings/recommendations, and generates the report data snapshot. External publication or work-item creation uses configured approval and a separate deterministic action/receipt boundary.

## Report and follow-up

The report shows data-quality status, scope/window, metric definitions, current value and distribution, comparable history, threshold/target, material changes, evidence-backed findings, confidence, recommendations, owners, and prior follow-up outcomes. Users can drill from a metric to normalized records and then to authorized source references.

Store recommendation status such as proposed, accepted, rejected, deferred, in-progress, completed, or ineffective. A later snapshot evaluates whether the intended outcome changed without claiming the recommendation caused it.

## Acceptance cases

Exercise complete data, stale source, partial history, changed team scope, denominator change, incompatible comparison, null versus zero, duplicate events, long-tail distribution hidden by an average, correlated release/incident without proven causation, no material change, supported bottleneck, rejected recommendation, and successful follow-up. Confirm reproducibility and no individual ranking.
