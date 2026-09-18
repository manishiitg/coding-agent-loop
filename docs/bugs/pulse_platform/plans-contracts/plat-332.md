[← Pulse platform issue index](../../pulse_platform_issue_register.md)

# PLAT-332 — manual workflow execution skipped contract migration preflight

| Coordination | Value |
|---|---|
| State | Deployed and verified in production |
| Date | 2026-09-18 |
| Owner | workflow execution / contract migrations |

## Problem

Contract migrations ran as schedule preflight turns. A workflow used only from
Run Workflow or Run Step could therefore remain on an old contract forever,
and manual execution could start without applying required platform repairs.
Webhook execution already failed closed on an old contract, but its error did
not give the owner an interactive migration path.

## Contract

Every manual `run_full_workflow` and `execute_step` call re-reads
`workflow.json` immediately before execution. When its contract differs from
the platform contract, the call starts no execution and tells the agent to ask
the owner whether to migrate. After approval, the agent uses Workshop
`get_contract_upgrades`, completes and verifies each migration in order, then
retries the original run.

The guard lives on the shared execution-tool registrar, so toolbar actions,
chat requests, restored sessions, and agent-profile callers receive the same
behavior. Scheduled sessions retain their existing blocking migration
preflight, and direct webhooks retain their own fail-closed preflight.

Release `bb24ab5-20260918164118` (`bb24ab544`) was deployed on 2026-09-18.
Post-deployment verification confirmed the exact source revision, all four
services active, a healthy planner endpoint, and the migration guard present in
the production binary.
