# Compute cost analysis

## Cost drivers

Separate instance time by family, size, operating system, tenancy, region, purchase model, and accelerator. Add attached disks, provisioned IOPS/throughput, snapshots, public IPs, data transfer, licenses, autoscaling overhead, and commitment allocation.

## Evidence to collect

Collect instance and group configuration, desired/min/max capacity, uptime, scaling history, scheduled jobs, CPU, available memory when exposed, disk, network, accelerator use, load/queue depth, service health, failover role, and representative peaks. Record burst-credit behavior and whether missing memory or accelerator telemetry limits the conclusion.

## Candidate checks

- Flag persistently idle capacity, stopped instances that still retain billable resources, and unattached dependent resources.
- Model downsizing or a newer compatible family from workload demand plus required headroom.
- Review autoscaling bounds and nonproduction schedules.
- Consider interruptible capacity only for workloads whose retry, checkpoint, and availability design permits interruption.
- Consider commitments only after demand is stable and already-rightsized coverage is understood.

## Guardrails and verification

Exclude standby/failover capacity, infrequent peaks, license-bound shapes, local-disk dependencies, quotas, and topology requirements unless the owner explicitly models them. Calculate savings from current price and commitment inputs. After rollout, verify equivalent demand, scaling, saturation, latency, errors, availability, and the complete billing window.
