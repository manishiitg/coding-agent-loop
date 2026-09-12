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

## Follow-up — Builder webhook registrar and testing skill

The initial release shipped management HTTP endpoints but no corresponding
Builder registrar, so agents incorrectly directed users to a removed creation
form. `manage_workflow_webhook` now exposes list/create/update/delete and test in
interactive writable Builder mode, reusing the management handlers and receiver.
Run has no management tool. Every call checks current workflow visibility and
mutations/tests enforce ownership/write access.

The workshop-only webhook-triggers skill covers discovery, real route binding,
bearer versus GitHub authentication, secret handling, test payloads, delivery
idempotency and run-result follow-through. Test uses the stored credential
internally; real events may execute the route. GitHub ping verifies setup without
route execution. Internal tests do not prove public gateway reachability.

Regression tests cover generated secrets, list redaction, a signed setup ping,
reader rejection and Run exclusion. Feature implemented; final release pending.

## 2026-09-12 deployment confirmation

The latest follow-up above is included in RTS release `3a37a1c-20260912143119`
(app `3a37a1c75`). Source revisions and all three services were verified after
activation. The configured-environment Linux sandbox regression passed.
Earlier pending-deployment notes are superseded; feature-specific live acceptance
limits remain as documented. No user accounts/sharing or notification recipients
were changed during verification.

## Follow-up — Trigger connectors and route destinations

Revision `2e3be2042` fixes presentation-card reconciliation clearing React Flow
handle measurements, which caused trigger edges to disappear. Fixed card sizes
now retain measured dimensions. Solid labeled arrows connect triggers to Start;
dashed `Selects: <route>` arrows connect saved choices to their route entry steps.
Full-workflow triggers are labeled explicitly. The Triggers viewport includes
connection destinations, and route highlighting retains prerequisites from Start.

Verification: eight focused layout/node tests and the frontend build passed.
A browser fixture verified both route-specific schedules and full-workflow
webhooks render real SVG paths that remain after graph updates. RTS release `2e3be20-20260912145652` deployed successfully with all three
services active and public health passing; user will test the production interaction.
