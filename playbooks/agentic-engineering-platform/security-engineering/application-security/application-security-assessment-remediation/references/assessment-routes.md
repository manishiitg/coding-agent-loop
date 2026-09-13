# Application security assessment routes

Load only the routes selected by the authorization and application attack surface.

## Common plan shape

1. **Scope gate — scripted:** validate authorization, target/source identity, route/check inventory, fixtures, evidence paths, and stop conditions.
2. **Attack-surface inventory — scripted plus message sequence:** combine declared architecture with observed routes/endpoints/components, identities/trust boundaries, data flows, dependencies, and deployment configuration. Mark unknown coverage.
3. **Selected assessment routes — scripted:** run repeatable allowlisted tools/checks with fixed versions/configuration, bounded resources, and isolated outputs.
4. **Normalize — scripted:** convert observations into one result schema, preserve tool-native IDs/raw restricted artifacts, and initialize completeness.
5. **Validate findings — message sequence plus deterministic checks:** deduplicate, reproduce safely, test control expectations and exploitability, record contradictions and confidence, and apply customer severity policy.
6. **Disposition branch:** dismiss/false-positive, remediate, accept risk, escalate, or hold for missing evidence.
7. **Fix and retest — scripted plus decisions:** prepare the smallest change, validate, obtain required review, deploy through the canonical path, and rerun the exact check plus affected regression routes.
8. **Finalize — scripted:** require terminal status for every expected check/finding and update durable dashboard records.

## Route selection

- [Browser security testing](browser-security-testing.md): browser controls, sessions, client-side exposure, cross-origin behavior, and browser-mediated authorization.
- [API security testing](api-security-testing.md): endpoint/object/function authorization, input handling, rate/abuse controls, and business logic.
- [Code and supply-chain testing](code-supply-chain-configuration.md): source analysis, dependencies, secrets, IaC, and deployed configuration.

Choose routes from actual components and data flows. Do not run every scanner against every repository or generic payload lists against every endpoint. Record `not_applicable` with a reason separately from `not_tested`, `blocked`, and `passed`.

## Common observation contract

Each tool/check result records run and check IDs, route, tool/config/rule version, exact target/source/build/deployment, actor/role and starting state when relevant, start/end, status, expected control, observed result, side effects/cleanup, evidence pointers and access class, error/truncation, and source freshness.

A scanner result is an observation. It becomes a finding only after validation establishes the affected asset/version, reproducible or otherwise credible evidence, security impact in the customer's model, confidence, severity rationale, and duplicate relationship.

## Coverage and completion

Create expected check rows before execution. Finalization compares expected with terminal results and distinguishes passed, finding, false-positive, blocked, skipped-by-policy, tool-error, incomplete, and not-applicable. Missing results cannot produce a clean assessment or passing CI decision.
