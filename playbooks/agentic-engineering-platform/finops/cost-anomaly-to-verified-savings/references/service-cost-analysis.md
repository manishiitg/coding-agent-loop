# Service cost analysis

Use this index after an anomaly is attributed to a service family. Load only the relevant guide plus the provider-adapter contract. Preserve the customer's billing basis, service architecture, reliability policy, and canonical IaC ownership.

## Common analysis sequence

For every service/resource:

1. Resolve exact provider/account/project, service family, region, environment, owner, resource ID, IaC address, configuration revision, and observation window.
2. Query cost and usage at the smallest reliable resource/SKU dimension. Preserve quantity, unit, rate, currency, credits/commitments, allocation, source time, and freshness.
3. Identify fixed, usage-based, request, storage, data-transfer, licensing, and support components. Reconcile the components to the source total before optimization.
4. Collect representative utilization, demand, capacity, health/SLO, scaling, scheduled workload, failover, and seasonality evidence.
5. Classify cost change as demand, configuration, pricing, commitment/credit, allocation/tagging, lifecycle/orphan, data-quality, or unknown.
6. Apply the relevant service guide's candidate rules and exclusions. Keep headroom, availability, recovery, quota, and growth requirements explicit.
7. Calculate projected gross/net savings from current provider prices and customer billing treatment; do not hardcode prices in the playbook.
8. Prepare the exact IaC change, validation, rollout, health checks, rollback, and verification window.
9. After rollout, compare equivalent demand, price basis, commitment treatment, and health before recording verified savings.

## Service guides

- [Compute](services/compute.md): virtual machines, instance groups, dedicated capacity, and accelerators.
- [Kubernetes](services/kubernetes.md): clusters, nodes, workloads, requests/limits, autoscaling, and cost allocation.
- [Managed databases](services/databases.md): compute, storage, I/O, backups, replicas, availability, and commitments.
- [Storage](services/storage.md): object/block/file storage, requests, lifecycle tiers, snapshots, and orphaned capacity.
- [Network](services/network.md): internet/cross-zone/cross-region transfer, NAT, load balancers, IPs, and topology.
- [Serverless](services/serverless.md): invocations, duration, memory/CPU allocation, concurrency, and event backlogs.
- [Observability and managed services](services/observability-and-managed-services.md): logs/metrics/traces plus cache, queue, search, and SaaS/license capacity.
- [Provider adapters](provider-adapters.md): map the common contract to the customer's cloud billing, inventory, monitoring, pricing, and IaC sources.

## Candidate evidence contract

Every candidate adds service family, provider-adapter revision, billing units/SKUs, reconciled cost components, utilization and health signals, observation/peak window, configuration dependency, service-specific exclusions, price source/time, savings calculation, capacity model, and verification queries. Missing required evidence leaves the candidate `insufficient_data`.
