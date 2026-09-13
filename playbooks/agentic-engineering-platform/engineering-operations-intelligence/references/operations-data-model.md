# Engineering operations data model

## Canonical relationship chain

The foundation should support this evidence chain without assuming every source provides every link:

```text
organization/team → repository/service → work item → change/PR → commit
                                                        ↓
                                         CI run/job → build → deployment
                                                        ↓
                           QA/security/performance result → incident
```

Preserve source-native records and normalized identities. A normalized link records its source, method (`source_native`, `explicit_mapping`, or `inferred_candidate`), confidence, creation time, and review state. Inferred candidates do not become authoritative links until the configured policy accepts them.

## Core records

Prefer small relational tables with stable primary keys:

- sources, sync runs/errors, cursors, and source-record receipts;
- organizations, teams, people-directory references, repositories, and services;
- projects, work items, states, labels, and work-item transitions;
- pull/change requests, reviews, commits, branches, and merge events;
- CI runs, jobs, attempts, test summaries, artifacts, and cancellations;
- builds, deployments, environments, promotions, rollbacks, and approvals;
- QA runs/cases/findings, security assessments/findings, and performance runs/metrics;
- incidents, impact intervals, responders/owners, mitigations, recoveries, and related changes;
- relationship links plus mapping/reconciliation findings;
- metric definitions, snapshots, intelligence findings, recommendations, actions, and reviews.

Reuse equivalent existing tables. Document DDL, keys, writers, upserts, indexes, retention, and restricted columns in `db/README.md` before ingestion.

## Time and identity

Store source timestamps, source-updated time, ingested time, timezone/offset when supplied, and normalized UTC values. Preserve raw status/state labels and map them through a versioned customer state model. Do not infer missing timestamps from ingestion time.

Keep stable source IDs and canonical URLs/references where access permits. Link repository, commit, build, deployment, and incident identities explicitly. Email/name similarity is insufficient for silently merging people; use approved directory mappings or leave records unmapped.

## Metric governance

Every metric definition records owner, version, purpose, population, unit, numerator/denominator, inclusion/exclusion rules, time boundaries, aggregation, null/missing behavior, freshness requirement, and interpretation limits. Freeze the applied definition with each snapshot.

Useful team/system signals may include work-item or PR cycle time, review wait, work in progress, aging work, CI duration/success, flaky-test impact, escaped/reopened defects, deployment frequency, rollback/change-failure observations, incident duration, and recovery time. Calculate only metrics supported by available source events.

Avoid individual rankings and proxy productivity measures such as commit count, lines changed, hours online, messages, or ticket count. Use person identity only when needed for ownership, workload routing, or follow-up under the customer's policy.

## Data quality

Track coverage, freshness, duplicate/conflict counts, unmapped identities, orphan relationships, cursor age, permission failures, schema changes, and reconciliation samples per source. Downstream analyses declare minimum quality requirements. Missing records remain unknown, not zero.

The data-quality report is part of the product contract: users should see why a metric or finding is unavailable, partial, or stale.
