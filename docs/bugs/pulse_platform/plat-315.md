[← Pulse platform index](../pulse_platform_issue_register.md)

# PLAT-315 — Pulse slash command resolves the open chat workflow

| Coordination | Value |
|---|---|
| Assigned agent | Codex |
| Ticket state | Implemented and deployed to RTS; verification scope below |
| Last synchronized | 2026-09-12 |
| Implementation commits | bb7ac6d17 |

## Delivered behavior and verification

Running /pulse could display “Open a workflow before running Pulse” with a workflow already open because the command read the file browser's activeFolder.

ChatInput now passes the resolved command workflow path into CommandContext, using the same workflow resolution as command permission checks. /pulse sends that path to the scheduler rather than directly reading the file browser store.

Validation: all 25 command-registry tests passed, including an empty browser selection with an open workflow, a browser folder pointing elsewhere, and no resolved workflow producing no scheduler request. Type checking and production build passed. Existing unrelated ChatInput lint errors/warnings remain. RTS release and public health were verified; a live Pulse was not launched solely for deployment verification.

## Deployment receipt

Included in RTS app release `bb7ac6d17ec43750e74a5c92d73ef067f66c69bf`
(`bb7ac6d-20260912135437`), with provider `570ede69fb85beef251ddca9792a2e5ad0dfe95d`.
All three services were active and the public health endpoint was healthy after deployment.
Deployment health is distinct from the feature-specific acceptance scope above.
