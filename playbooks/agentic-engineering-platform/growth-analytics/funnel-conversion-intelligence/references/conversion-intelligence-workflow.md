# Conversion intelligence workflow

## Define funnels before analysis

Version each funnel with its steps, event/taxonomy revision, population, filters, attribution rule, comparison window, target or threshold, owner, and exclusions. Reuse customer funnel definitions when their steps, population, and windows are compatible; never silently redefine a step to make a trend look better.

Record segment dimensions (plan, channel/campaign, page/screen, device, geography, account size) with their compatibility rules. A segment breakdown is only valid when every segment shares the funnel definition and window.

## Adapt the plan

Use scripted steps for funnel snapshots, conversion and drop-off calculations, segment breakdowns, statistical change detection against the comparison window, and completeness checks (freshness, denominators, missing events). A change is reportable only when it clears the customer's minimum detectable effect and data-quality gate.

Use a message sequence to investigate a supported change: rank contributing segments by impact, inspect the responsible pages/devices/sources, pull consented session replays for the affected cohort, test alternative explanations (tracking change, seasonality, mix shift, outage), and converge on an attributed cause with confidence. Sessions are qualitative evidence for an attributed change, not a detection method on their own.

For recurring monitoring, prove an on-demand investigation first, then configure scheduled funnel runs or threshold alerts with explicit scope, cadence, timezone, and notification conditions. Never page a human for a change that fails the quality gate.

## Validation and report

Validate funnel reproducibility from durable snapshots, denominator presence on every rate, segment/window compatibility, comparison-window alignment, session linkage for qualitative claims, and evidence for every finding. Verify that taxonomy or tracking changes surface as data-quality events rather than silent conversion shifts.

Build a live conversion dashboard showing funnel steps with drop-offs, trends with comparison bands, segment breakdowns, open investigations with attributed causes and confidence, linked sessions, limitations, and history. Incomplete or untrusted states stay visibly unrated.

## Handoff

Growth Experimentation and Follow-Through consumes frozen findings with their evidence and confidence. Return funnel/policy versions, usable windows, attributed causes, session references, and recommended hypotheses. Do not require downstream agents to recompute funnels from raw events or chat history.
