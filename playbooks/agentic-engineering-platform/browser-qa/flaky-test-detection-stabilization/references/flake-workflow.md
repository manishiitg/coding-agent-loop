# Flake detection and stabilization workflow

## Design a bounded experiment

Define the hypothesis before repeating tests. Freeze the application build, canonical test/source revision, browser/device profile, fixtures, account role, worker/concurrency count, retry setting, execution order, and evidence policy. Assign stable attempt IDs and predeclare the number of attempts.

Begin with retries disabled so the experiment observes original outcomes. A default may be proposed, but the customer chooses cost and confidence requirements. Add targeted order, concurrency, timing, or network experiments only when the first evidence justifies them. Never create an unbounded rerun loop.

## Adapt the plan

A scripted step initializes the expected attempts, runs the exact case in isolated contexts, finalizes evidence and cleanup, and persists every outcome. A deterministic analysis can calculate pass/fail distribution and signature groups. One message sequence compares DOM/assertion, timing, video, console, network, trace, runner, and environment evidence to classify:

- likely deterministic test defect;
- likely timing/order/concurrency-sensitive test defect;
- likely intermittent application defect;
- environment/infrastructure instability;
- mixed causes; or
- insufficient evidence.

If a test-only stabilization is eligible, create a candidate outside canonical source, verify it against the original and related cases, present its diff and evidence, and use a human branch for approve/reject/defer. Apply only the exact approved patch and run a new bounded post-change experiment. Self-healing may consume the confirmed eligible finding rather than repeating diagnosis.

## Persist and report

Store experiment policy, expected attempt set, attempt results, signatures, classification/confidence, candidates, decisions, post-change results, and artifact metadata. Preserve passing and failing evidence. Use idempotent keys scoped to test/build/profile/experiment/attempt; do not overwrite a failure with a later pass.

The report shows outcome distribution, sequence/timing, signature clusters, environment changes, evidence comparison, candidate and review state, and before/after stability. `Stable` means the configured post-change experiment completed without unexpected variation; it is not a permanent guarantee.

## Acceptance cases

Exercise consistently passing, consistently failing, alternating, order-dependent, concurrency-dependent, environment-failure, incomplete-attempt, evidence-gap, candidate-fails-related-case, human-defer, and successful reviewed stabilization paths. Confirm bounded execution, preserved failures, no automatic quarantine, and no canonical write before approval.
