[← Pulse platform index](../pulse_platform_issue_register.md)

# PLAT-309 — Authenticated workflow webhooks and visible schedule/trigger routes

| Coordination | Value |
|---|---|
| Assigned agent | Codex |
| Ticket state | Implemented and deployed to RTS; verification scope below |
| Last synchronized | 2026-09-12 |
| Implementation commits | 76b2423bc, 3e7aa7280, afdf2ed1d, de575af6c, e0b5d402f |

## Delivered behavior and verification

External services can invoke saved workflow routes through authenticated POST webhooks. Generic bearer and GitHub signature authentication are supported; trigger secrets are encrypted, with plaintext returned only at creation/rotation. Route execution retains prerequisites and uses existing workflow concurrency and execution history.

Accepted deliveries return 202 with run identity and execute asynchronously. Persistent delivery IDs deduplicate retries. Busy/unavailable execution returns 503 for sender retry: this release does not implement an automatic durable replay queue. Other provider-specific signature/challenge formats require adapters.

Builder chat creates/configures triggers. Webhooks sits beside Schedules in the toolbar; the manual Add API trigger form was removed. URLs remain copyable, and run history/activity distinguish webhook versus scheduled/manual origin. Plan displays schedule/webhook cards and saved route highlighting; Triggers focuses the cards reliably in a narrow split view.

Validation: focused trigger/layout/toolbar contract tests and production build passed. RTS gateway deployment, Webhooks placement, absent creation form, and all six rts-latency schedule cards in view were verified. External provider end-to-end acceptance is not claimed. See [API triggers](../../workflow/api-triggers.md) for response/authentication details.

## Deployment receipt

Included in RTS app release `bb7ac6d17ec43750e74a5c92d73ef067f66c69bf`
(`bb7ac6d-20260912135437`), with provider `570ede69fb85beef251ddca9792a2e5ad0dfe95d`.
All three services were active and the public health endpoint was healthy after deployment.
Deployment health is distinct from the feature-specific acceptance scope above.
