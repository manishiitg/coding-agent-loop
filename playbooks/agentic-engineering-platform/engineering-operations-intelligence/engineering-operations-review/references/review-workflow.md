# Engineering operations review workflow

## Define the review contract

Every review policy specifies cadence/period, audience, engineering scope, comparison window, required sections, materiality/significance rules, data-quality minimums, sensitive-data treatment, action ownership, prior-review follow-up, approval, delivery destinations, and notification behavior.

Useful sections are current health, material changes, delivery flow, quality, reliability, active risks, aging or blocked work, prior actions, recommended actions, and data limitations. Adapt sections to the audience. A daily operational review is shorter than a monthly leadership review but uses the same governed sources.

## Adapt the plan

1. **Freeze review snapshot — scripted.** Resolve the period once, bind source/metric-policy revisions, copy expected section IDs and prior action IDs, validate data quality, and persist immutable input references.
2. **Analyze and draft — message sequence.** Account for each section, inspect material findings and their evidence, compare alternative explanations, state confidence/limitations, assess prior actions, and propose concrete actions with owners and expected outcomes.
3. **Finalize draft — scripted.** Check section/action completeness, evidence links, sensitive-data rules, and stable review identity. Populate durable report data; do not generate a new HTML file per run.
4. **Review decision — human branch when configured.** Present the actual draft and destinations. Approve, reject, or defer; unattended default is defer when approval is required.
5. **Deliver — scripted.** Send only the approved revision through configured tools, persist receipts, and distinguish queued, attempted, delivered, and failed.

Run on demand until the full flow succeeds. Then use current AgentWorks schedule tools for a cadence, explicit groups, timezone, overlap behavior, safe branch defaults, and meaningful-change notifications.

## Dashboard and actions

The report shows selected review period/revision, audience/scope, data-quality state, section summaries, metric/finding drill-downs, prior action outcomes, proposed/accepted actions, owner/status, approval, delivery receipts, and historical reviews. Preserve the exact snapshot behind an older review.

An action is a tracked recommendation, not proof work was completed. External issue creation or messaging needs configured authorization and a real receipt. Stay quiet when a scheduled review has no material change unless the customer requests routine delivery.

## Acceptance cases

Exercise first review, no material change, material delivery regression, conflicting indicators, stale data, missing required section, prior action completed/rejected/unknown, sensitive finding excluded, human edit/approve/defer/reject, delivery failure, duplicate schedule trigger, and historical view after metric definitions change. Confirm no invented cause, individual ranking, alert spam, or publication before review.
