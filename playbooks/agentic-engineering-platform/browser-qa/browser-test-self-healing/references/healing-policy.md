# Browser test healing policy

## Eligibility

A finding is eligible only when approved expected behavior remains known and evidence supports a defect in test automation. The candidate must target canonical test code or configuration within the customer's authorized scope.

| Classification | Healing action |
| --- | --- |
| Stale locator or scope | Update the shared locator helper after verifying semantic identity and uniqueness. |
| Synchronization defect | Replace brittle waits with observable conditions or Playwright assertions. |
| Navigation/setup drift | Repair test navigation, authentication setup, or documented browser configuration while preserving the journey. |
| Fixture lifecycle defect | Repair deterministic creation/reset/cleanup for case-owned test data. |
| Flaky automation | Propose a causal repair when reproduced; a passing retry alone is insufficient. |
| Product defect | Preserve the failed assertion and route to bug reporting. |
| Environment/infrastructure | Preserve blocked status and route to environment remediation. |
| Unknown or ambiguous expectation | Route to review; do not infer correctness from the current application. |

## Prohibited healing

Never change approved expected values, remove or weaken assertions, skip or quarantine a test, broaden timeouts without evidence, select the first ambiguous match, update baselines, or modify product code merely to make a test pass. Those are distinct review/remediation actions. A test-ID or accessibility attribute added to product code is a proposed product change and requires its own authorization and review.

Do not silently heal at locator-resolution time. A diagnostic fallback may identify a candidate, but the original run stays failed or unresolved until the canonical helper is changed, accepted, and rerun. Preserve original video, console/network logs, traces, screenshots, source hashes, and runner exit status as attempt-scoped evidence.

## Candidate contract

Each repair receives a stable ID and links to exact finding/run/case/attempt IDs, application/environment/build, profile/journey revisions, expected-behavior source, and original test/helper revision. Record:

- Classification and evidence.
- Files and symbols in scope.
- A unified patch against the expected base revision.
- Assertion-preservation statement and semantic locator mapping where relevant.
- Expected impact and related journeys to verify.
- Confidence with reasons and remaining uncertainty.
- Candidate status, verification attempts, approval decision, application receipt, and final canonical revision.

A candidate patch is data, not authorization. Store it under `db/assets/browser-qa/<run-id>/repairs/<repair-id>/candidate.patch` and reference it from durable records. Store retained original/candidate/canonical video and console/network artifacts under their owning attempts using the shared [evidence capture contract](../../references/evidence-capture.md). Do not expose secrets, authenticated page dumps, or unrestricted network bodies.

## Verification

Apply the patch to an isolated copy at the recorded base revision. Reject unexpected file changes and assert the patch touches only permitted test/config paths. Perform static/import checks, then run the original journey and the smallest evidence-based related set. Use fresh contexts, reconstruct fixtures, and retain recordings according to policy.

Verified means the intended behavior is still asserted, the original case passes, related required cases pass, cleanup succeeds, the expected source was tested, and required video/console/network or other evidence exists. A result is not verified when cases were skipped, the base changed, evidence required by policy is missing or failed redaction, or a different failure remains unresolved.

## Approval and application

The initial policy is human review for every candidate. Present the diff, original failure, verification results, affected tests, and recordings. The choices are approve, reject, or defer. Unattended execution defaults to defer.

On approval, recheck the canonical base revision and apply the exact verified patch idempotently. A mismatch returns to proposal/verification; do not fuzzy-apply. If configured and authorized, create a branch or PR using the customer's chosen integration. Otherwise save the reviewed patch for the customer to apply. External publication is not implied by installing the playbook.

After application, rerun the canonical source. Only then mark the repair `healed`. `verified_candidate`, `approved`, `applied`, and `healed` are distinct states. Rejection or deferral never changes the original QA finding.

## Knowledgebase update

After an accepted canonical change and successful rerun, update the application browser-QA note with the locator/setup purpose, new helper symbol/path, scope, source revision, verification build/time, evidence, and superseded entry. Keep executable selectors in test code. Proposed or rejected values remain repair history in the DB and must not replace verified knowledge.

## Stopping rules

Use bounded diagnostic and repair attempts. Stop and request review when the expectation is unclear, classification changes, the candidate touches prohibited paths, source revision drifts, the same failure survives the configured attempts, or new required failures appear. Never loop until green.
