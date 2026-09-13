# Kubernetes cost analysis

## Cost drivers

Reconcile control-plane fees, node pools, accelerators, disks, snapshots, load balancers, IPs, and network transfer. Allocate node cost into workload-requested, shared platform, system/daemon, and unallocated capacity without pretending shared cost is directly owned.

## Evidence to collect

Collect workload requests, limits, observed CPU/memory, replicas, QoS, HPA/VPA behavior, cluster-autoscaler bounds, pending pods, throttling, OOM/evictions, restarts, queue/load signals, node utilization, daemon overhead, affinity, taints, disruption budgets, topology rules, and namespace/team ownership across representative peaks.

## Candidate checks

- Right-size workload requests and limits from demand, performance, and restart evidence.
- Model node-pool family/size changes and consolidation with real bin-packing constraints.
- Review minimum nodes, autoscaler bounds, idle namespaces, orphaned volumes/load balancers, and nonproduction schedules.
- Use interruptible nodes only for workloads placed in an interruption-tolerant pool.

## Guardrails and verification

Preserve availability zones, disruption budgets, system headroom, rollout capacity, workload cycles, and recovery needs. Fragmentation can make apparently free CPU or memory unusable; simulate scheduling before proposing node removal. Verify scheduling, pending pods, evictions, throttling, latency, errors, autoscaling, SLOs, allocation, and billed node/supporting-resource cost.
