# Managed database cost analysis

## Cost drivers

Separate database compute, storage capacity, provisioned IOPS/throughput, requests, backups/snapshots, replicas, multi-zone or multi-region availability, data transfer, licensing, support, and commitment effects.

## Evidence to collect

Collect engine/version, topology, instance class, storage configuration and growth, CPU, available memory/cache pressure when exposed, connections, query latency, throughput, IOPS, queue depth, replica lag, failovers, maintenance events, backup/restore policy, peak cycles, growth forecast, and SLOs.

## Candidate checks

- Model compute resizing only when memory, connections, I/O, peak demand, and failover headroom are known.
- Review storage tier, provisioned IOPS/throughput, autoscaling limits, backup retention, and confirmed obsolete snapshots.
- Identify idle development instances and confirmed obsolete replicas.
- Evaluate serverless/autoscaling modes when workload shape and latency constraints fit.
- Consider commitments after topology and steady demand are validated.

## Guardrails and verification

Do not remove replicas or standby capacity based on low query traffic; record recovery, read-scaling, and compliance purpose. Account for migration downtime, engine constraints, licenses, storage growth, and unavailable memory metrics. Verify query latency, connections, I/O, errors, replication, failover readiness, backup success, restore requirements, SLOs, and comparable billed cost.
