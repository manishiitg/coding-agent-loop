# Post-incident review and action workflow

## Freeze and reconstruct

After recovery, freeze a versioned snapshot of the canonical incident, signals, timeline, hypotheses, decisions, status updates, remediation/rollback, health verification, and communication receipts. Retain links to restricted evidence. Calculate milestones only from sourced timestamps and the customer's definitions; label gaps and clock disagreement.

Describe customer/user impact, duration, affected services, SLO/error-budget effect, detection path, and recovery. Separate confirmed facts, reasonable reviewed inferences, and unknowns.

## Analyze

Examine detection, escalation, diagnosis, mitigation, recovery, coordination, tooling, safeguards, change process, and organizational/system conditions. For every contributing factor, link evidence and explain how it influenced impact or response. Avoid a single-root-cause requirement when several conditions interacted.

Capture what worked as well as what failed. Compare related incidents only through stable service, signature, control, or contributing-factor links. Similar language is insufficient to declare recurrence.

## Create and verify actions

Each accepted action defines the observed gap, intended outcome, type, service/scope, owner, due/review date, priority basis, exact external work-item identity, dependencies, verification method, and closure evidence. Defer/reject decisions retain rationale. Create external issues only after the configured review, then save delivery receipts and synchronize without duplicating items.

Completion requires the intended control or outcome to be verified: test/runbook evidence, deployed change and health, alert exercise, game day, documentation review, or another policy-approved check. Issue closure alone is not verification. Link later incidents to assess recurrence and effectiveness.

## Publish and learn

Produce audience-specific views from the approved snapshot, redact by policy, and record publication receipts. Promote verified reusable knowledge with scope and source revision. The dashboard tracks draft/reviewed/published state and action aging without ranking people.

## Acceptance cases

Exercise missing timeline data, disputed fact, multiple contributing factors, sensitive evidence, unresolved incident, reviewer edits, rejected publication, duplicate issue delivery, owner change, overdue action, issue closed without evidence, verified completion, recurring incident, and metric-definition change. Confirm no unsupported causal statement or unapproved external write.
