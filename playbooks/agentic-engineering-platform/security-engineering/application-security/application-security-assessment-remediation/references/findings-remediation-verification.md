# Findings, remediation, and verification

## Normalize and validate

Normalize each observation into route/category, tool/rule/source, exact asset and source/build/deployment identity, control expectation, observed behavior, evidence, first/last seen, and access class. Create a stable fingerprint from control/category and affected asset/location; preserve tool-native IDs and duplicate links.

Validation records a safe reproduction or other credible confirmation, preconditions, reachability/exposure, affected actors/data, impact within the authorized model, supporting and contradicting evidence, confidence, reviewer, and important unknowns. Scanner confidence is an input, not the final confidence. Use `observation`, `needs_validation`, `confirmed`, `false_positive`, `duplicate`, `accepted_risk`, `remediation_planned`, `fix_in_progress`, `deployed_pending_retest`, `verified`, `reopened`, and `blocked` as distinct states.

Apply the customer's severity model and revision. Record impact and likelihood factors, environmental adjustments, rationale, SLA/due date, and owner. Do not silently replace customer severity with a generic score.

## Prepare remediation

Link the finding to the canonical repository/module/resource and current revision. Define the intended security invariant, smallest proposed code/dependency/configuration/IaC change, alternatives, compatibility and availability risk, validation tests, deployment path, monitoring, rollback, and owner.

Use scripted steps for deterministic edits and checks. Use a message sequence to reason about design fixes and likely bypasses. Present the exact diff and validation before required review. External issue/PR creation, protected changes, deployment, and risk acceptance follow the customer's decision policy and retain receipts.

Security tests assert the intended invariant and a permitted control case. Do not merely change scanner configuration, suppress the rule, reduce evidence, or loosen the test unless a reviewed false-positive or accepted-risk decision explicitly supports it.

## Risk acceptance

Record the authorized approver, scope, rationale, compensating controls, residual severity, start/expiry/review date, linked work, and evidence. Expired acceptance reopens the finding. Acceptance changes disposition; it does not turn the control into passed or delete history.

## Deploy and verify

Bind the fix commit/artifact/configuration to the actual deployment. Rerun the exact original check and applicable bypass/adjacent regression cases against that deployed identity. Verify security behavior, permitted behavior, service health, and monitoring. A fixed source scan, merged PR, successful deployment, or one missing symptom alone cannot produce `verified`.

If retest fails, deployment identity mismatches, evidence is unavailable, or the finding recurs, keep it open/reopen it with the new evidence. Preserve all prior states and decisions.

## Acceptance cases

Test duplicate observations, unsupported scanner claim, confirmed finding, severity override with rationale, issue/PR delivery failure, rejected/deferred fix, stale diff, failed validation, accepted risk and expiry, merge without deploy, wrong deployment, exact retest pass/fail, permitted-case regression, rollback, and recurrence.
