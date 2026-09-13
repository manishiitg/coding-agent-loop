# Performance measurement contract

Use this contract for browser and API performance workflows. Performance results are comparisons under a declared measurement profile, not timeless facts about a system.

## Identity and provenance

Every run records:

- target application/service, environment, region when relevant, and exact build/deployment identity;
- source playbook, test/runner, scenario, policy, and measurement-profile revisions;
- runtime/runner version and worker location when known;
- authentication role, fixture/data shape, and payload class without secret values;
- start/end times, warmup policy, planned/completed/invalid samples, concurrency, and cache/connection state;
- browser/device/network/CPU profile for browser tests or protocol/connection/load profile for API tests.

Never compare results whose profiles are materially incompatible. Mark them as separate series or `not_comparable` with the reason.

## Sampling and aggregation

Predeclare warmups, measured sample count, duration or iteration limit, concurrency/rate shape, timeout, and aggregation method. Preserve raw samples and invalid-sample reasons. Do not silently rerun only slow samples, discard outliers, or average cold and warm measurements together.

Use the customer's chosen statistics and thresholds. When no policy exists, report observations and trends as `unrated`; propose a draft budget for review without applying it retroactively to the same run.

## Budgets and decisions

Version every budget. A rule identifies metric, scenario/profile, comparison operator, threshold or baseline, allowed variance, minimum valid samples, and action such as warn, fail, or review. Freeze the applied policy with each result.

Functional correctness is a prerequisite. A fast error page or failed API response cannot satisfy a performance budget. Missing samples, build mismatch, excessive errors, or invalid provenance cannot produce a passing decision.

## Persistence

Document tables in `db/README.md` for profiles, policies, scenarios, runs, samples, metric aggregates, comparisons, findings, and artifacts. Use stable keys and idempotent upserts without overwriting historical runs.

Store queryable measurements in `db/db.sqlite`. Store raw runner outputs, sanitized request/network summaries, traces, profiles, and other retained files under `db/assets/performance/<run-id>/`. Do not persist credentials, authorization headers, cookies, or unrestricted request/response bodies.

## Report

Show target/build/profile identity, sample completeness, functional status, current and historical metric distributions, budget/baseline revision, decision, variance, evidence, and limitations. Keep browser and API series distinct while allowing an end-to-end browser journey to link to contributing API observations when their timestamps, build, and environment align.

Validate empty, collecting, incomplete, incomparable, unrated, passed, warning, failed, cancelled, and evidence-missing states. A chart without sample and profile context is not sufficient evidence.
