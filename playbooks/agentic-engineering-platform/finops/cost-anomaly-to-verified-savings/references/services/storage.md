# Storage cost analysis

## Cost drivers

For object, block, and file storage, separate capacity by class, request operations, retrieval, early-deletion charges, replication, snapshots/versions, provisioned IOPS/throughput, transfer, and attached service charges.

## Evidence to collect

Collect size and object/file counts, age distribution, access frequency, read/write/request pattern, growth, latency/throughput, attachment and mount state, snapshot lineage, replication, lifecycle rules, restore requirements, retention policy, legal holds, and owner. Inventory evidence alone does not prove data is disposable.

## Candidate checks

- Model lifecycle transitions from observed access and current retrieval/minimum-duration economics.
- Identify incomplete uploads, redundant versions, unattached volumes, and snapshots without a live retention purpose for owner review.
- Right-size provisioned IOPS/throughput from peaks and latency requirements.
- Review replication scope and cold/archive tiers against recovery objectives.

## Guardrails and verification

Preserve compliance retention, legal holds, backup independence, recovery points, shared mounts, minimum storage duration, and retrieval or egress costs. Destructive cleanup always requires confirmed ownership and the configured approval path. Test restore/access where policy requires it, then verify availability, latency, recovery behavior, lifecycle transitions, and net billed savings after retrieval and transition charges.
