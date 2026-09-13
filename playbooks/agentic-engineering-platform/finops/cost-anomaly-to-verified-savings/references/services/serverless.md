# Serverless cost analysis

## Cost drivers

Separate invocations, execution duration, allocated memory/CPU, provisioned or reserved concurrency, accelerator use, event delivery, queue/stream operations, data transfer, temporary storage, logs, and downstream-service cost.

## Evidence to collect

Collect invocation volume, duration percentiles, peak concurrency, allocated and observed memory when available, cold starts, throttles, timeouts, retries, errors, queue age/backlog, event batch size, payload size, schedules, version/configuration, and end-to-end latency/SLOs.

## Candidate checks

- Test memory/CPU settings against the measured cost-duration curve; lower allocation can increase duration and total cost.
- Review provisioned concurrency, schedules, unused versions/functions, retry amplification, event batching, and excessive telemetry volume.
- Identify workloads whose sustained shape may fit another execution model, including migration cost and operations burden.

## Guardrails and verification

Preserve burst capacity, timeout margin, idempotency, dead-letter behavior, ordering, latency, and downstream quotas. Test candidates with representative payloads and concurrency before rollout. Verify results, duration, cold starts, throttles, retries, backlog, latency, errors, downstream usage, log volume, and comparable billed cost.
