# Lifecycle intelligence workflow

## Define lifecycle before analysis

Version the activation definition (events, thresholds, time window), cohort keys (signup period, plan, channel, segment), retention windows and censoring rules, feature/flag inventory with launch dates, and revenue linkage scope. Reuse customer definitions when their populations and windows are compatible; never silently redefine activation to improve a trend.

Record cohort comparability rules: cohorts must share definitions, taxonomy revision, and observation windows before their curves are compared. Flag immature cohorts explicitly instead of ranking them against mature ones.

## Adapt the plan

Use scripted steps for activation scoring, cohort retention curves, feature-adoption and revenue-join calculations, feedback-theme aggregation, and completeness checks (freshness, denominators, censoring, missing events). A difference is reportable only when it clears the customer's minimum detectable effect and data-quality gate.

Use a message sequence to investigate a supported difference: contrast retaining and churning cohorts on behavior, feature use, plan, and feedback themes; test alternative explanations (mix shift, seasonality, tracking change, outage, pricing change); and converge on an attributed difference with confidence. Correlations propose candidates for experimentation; they do not prove causes.

For recurring monitoring, prove an on-demand analysis first, then configure scheduled lifecycle runs or threshold alerts with explicit scope, cadence, timezone, and notification conditions.

## Validation and report

Validate lifecycle reproducibility from durable snapshots, denominator and censoring disclosure on every rate, cohort compatibility, revenue-join reconciliation against billing samples, feedback sampling and consent basis, and evidence for every finding. Verify that taxonomy or tracking changes surface as data-quality events rather than silent retention shifts.

Build a live lifecycle dashboard showing activation trends, retention curves by cohort, feature adoption with retention/revenue impact, correlated feedback themes, open findings with confidence, limitations, and history. Incomplete or untrusted states stay visibly unrated.

## Handoff

Growth Experimentation and Follow-Through consumes frozen findings with their evidence and confidence. Return lifecycle/policy versions, usable cohorts and windows, attributed differences, feedback references, and recommended hypotheses. Do not require downstream agents to recompute cohorts from raw events or chat history.
