# Engineering data foundation workflow

## Inspect and map before ingestion

Inventory existing workspace DB/report contracts and connected sources. For each source, record account/tenant, authorization scope, entities, stable IDs, pagination, update timestamps, rate limits, history limits, retention, and webhook or polling options. Prefer read-only permissions and preserve the customer's existing warehouse as authoritative when applicable.

Create a versioned source map covering repositories, projects, teams, services, environments, workflow states, build/deployment identity, and cross-source relationships. Ambiguous mappings remain candidates for review.

## Adapt the plan

Use one or a few scripted ingestion steps per credential/rate-limit/failure domain. A step should:

1. initialize a sync run and read its durable cursor;
2. fetch deterministically with known pagination and bounded retries;
3. preserve source identity, timestamps, and provenance;
4. normalize through versioned mappings;
5. upsert without erasing source history;
6. advance the cursor only after successful persistence;
7. reconcile counts/samples and finalize success, partial, blocked, or failed.

Use a message sequence only for mapping ambiguity or data-quality investigation that needs judgment. Its output proposes a mapping with evidence; it does not silently mutate the authoritative map. Use a human branch when the customer must choose between conflicting identities.

For recurring refresh, prove an on-demand sync first, then configure the current AgentWorks schedule tools with explicit groups, route selection, timezone, overlap behavior, and notification conditions.

## Validation and report

Validate initial and incremental sync, pagination boundary, duplicate delivery, source update, deleted/archived source record, cursor restart, rate limit, permission loss, partial source outage, mapping conflict, schema drift, and two sources referring to the same commit/build. Verify idempotent reruns and that one source failure does not falsely mark the entire model fresh.

Build a live data-quality view showing source status, last successful and attempted sync, lag, records by entity, coverage window, cursor, errors, unmapped/conflicting identities, relationship coverage, reconciliation samples, and downstream readiness. No dashboard metric should appear trusted when its declared source quality gate fails.

## Handoff

Delivery/Quality/Reliability Intelligence consumes the schema and a frozen data-quality snapshot. Return table contracts, source/mapping versions, usable coverage intervals, known exclusions, and query examples. Do not require downstream agents to reconstruct meaning from connector payloads or chat history.
