# API performance workflow

## Define scenarios and load safely

Each scenario has a stable ID, method/protocol operation, sanitized target template, actor/auth role, payload class and size band, data ownership, setup/cleanup, functional response checks, idempotency behavior, and metric requirements. Keep exact credentials and sensitive bodies in approved runtime inputs, not scenario documents.

Define a versioned load profile with worker location, virtual users/concurrency or arrival rate, stages, warmup, measured duration/iterations, connection reuse, think time when relevant, per-request and overall timeouts, maximum requests, abort thresholds, and environment owner approval. Separate smoke, baseline, and capacity profiles. Start with a low-volume trial.

## Adapt the plan

A typical workflow uses:

1. **Preflight — scripted.** Verify exact origin/environment/build, DNS/TLS reachability through the supported runner, credentials, fixture ownership, limits, and policy. Initialize expected scenario/sample rows.
2. **Queue trial review — scripted when needed.** For high-load, production, or state-changing profiles, save the exact target, profile, request envelope, limits, and evidence through `create_human_input_request` with approve/reject/defer options, then end preparation without sending the gated traffic.
3. **Load approval and execute — separate scripted action route.** Read the durable decision by stable ID, verify the reviewer, expiry, exact profile/target identity, and current environment, then run the checked-in workload. Enforce abort/timeout limits, preserve functional assertions and all errors, finalize cleanup, and save machine-readable output. Missing, rejected, deferred, expired, or stale approval remains held.
4. **Aggregate and compare — scripted.** Account for planned scenarios/samples, calculate configured distributions and throughput/error metrics, compare only compatible baselines, and apply the frozen budget policy.
5. **Investigate — message sequence when warranted.** Correlate sanitized request timings with authorized service metrics/logs/traces and state confidence and alternative explanations.
6. **Finalize — scripted.** Persist lifecycle and decision, including pending review, answered, executed, incomplete, incomparable, blocked, warning, or failed states.

Do not create one workflow step per endpoint or virtual user. Split profiles when target, authorization, side effects, load limits, or retry/failure domains differ.

## Measurement and diagnostics

Apply the shared [performance measurement contract](../../references/performance-measurement.md). Preserve raw request samples or bounded histograms according to scale, plus scenario-level counts, latency distribution, data transferred, connection/TLS timing when supported, functional failures, transport errors, timeouts, throttling, and server status groups.

Observability is supporting evidence only when timestamps, environment, service, and build align. Client latency alone cannot prove CPU, database, or downstream saturation. Record absent telemetry and clock uncertainty.

Store sanitized runner outputs, traces, and request summaries under `db/assets/performance/<run-id>/api/`. Redact authorization, cookies, tokens, query secrets, and sensitive payloads. Response bodies are disabled by default.

## Acceptance cases

Exercise low-volume success, functional failure under load, timeout/error threshold abort, target/build mismatch, unauthorized production target, rate limiting, incomplete samples, incomparable baseline, missing budget, duplicate invocation, cleanup failure, missing observability, human defer/reject/approve, and storage/report failure. Confirm limits stop traffic promptly and empty results never pass.
