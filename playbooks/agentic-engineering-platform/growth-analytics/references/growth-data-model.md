# Growth data model

## Canonical relationship chain

The foundation should support this evidence chain without assuming every source provides every link:

```text
traffic source/campaign → visitor → user → account/workspace
                                              ↓
                                   product event → funnel step → feature use
                                              ↓
                              subscription/order → revenue → support/feedback case
```

Preserve source-native records and normalized identities. A normalized link records its source, method (`source_native`, `explicit_mapping`, or `inferred_candidate`), confidence, creation time, and review state. Inferred candidates do not become authoritative links until the configured policy accepts them.

## Core records

Prefer small relational tables with stable primary keys:

- sources, sync runs/errors, cursors, and source-record receipts;
- traffic sources, campaigns, landing pages, referrers, and channel mappings;
- visitors, users, accounts/workspaces, seats, roles, and identity links;
- product events, screens/pages, funnels, funnel steps, and feature flags;
- plans, subscriptions, orders, invoices, refunds, and revenue adjustments;
- support conversations, tickets, reviews, survey responses, and feedback themes;
- sessions/replays, experiments, experiment assignments, and experiment results;
- metric definitions, snapshots, intelligence findings, recommendations, actions, and reviews.

Reuse equivalent existing tables. Document DDL, keys, writers, upserts, indexes, retention, and restricted columns in `db/README.md` before ingestion.

## Time and identity

Store source timestamps, source-updated time, ingested time, timezone/offset when supplied, and normalized UTC values. Preserve raw event names and map them through a versioned customer event taxonomy. Do not infer missing timestamps from ingestion time.

Keep stable source IDs and canonical URLs/references where access permits. Link visitor, user, account, subscription, and feedback identities explicitly. Device fingerprint or email similarity alone is insufficient for silently merging customers; use authenticated IDs, approved mappings, or leave records unmapped.

## Metric governance

Every growth metric records its definition version, population, filters, event taxonomy revision, attribution rule, comparison window, owner, and known limitations. Attribution (first-touch, last-touch, or modeled) is a customer choice recorded per metric, never silently switched. Denominators and null/missing handling are explicit; missing events are not zero behavior.

## Data quality

Track per-source coverage windows, lag, event validation failures, identity match rates, unmapped records, duplicate deliveries, and revenue reconciliation deltas against billing samples. Downstream funnels, cohorts, and revenue metrics stay untrusted until their declared quality gates pass.
