[← Pulse platform index](../../pulse_platform_issue_register.md)

# PLAT-342 — Ctrl+K quick switcher incurred avoidable first-open latency

| Coordination | Value |
|---|---|
| Assigned agent | Codex |
| Ticket state | `implemented; regression test green; deployment pending` |
| Last synchronized | `2026-09-21` |
| Priority | `P1 performance / navigation` |

## Report

Opening the quick switcher with Ctrl+K was visibly slow, especially on its first
use. The switcher is core navigation and should appear from local state without
waiting for code or network work.

## Cause

The application split the small switcher into a lazy chunk, so first use could
pay an extra production asset round trip. Its open effect also forced the
active-session endpoint and depended on `workflowPresets`, causing the reset
effect to rerun when presets changed while the overlay was open.

## Fix

- Bundle the quick switcher with the application shell so Ctrl+K can render it
  immediately.
- Paint from the already-subscribed session cache and use the store's normal TTL
  refresh instead of forcing a request on every open.
- Reset query/selection only when the overlay opens or its explicit initial
  query changes.

## Verification

- A focused performance contract test prevents reintroducing lazy loading,
  force-refresh-on-open, or the unstable preset dependency.
- Deployment and production timing verification remain pending.
