[← Pulse platform index](../pulse_platform_issue_register.md)

# PLAT-305 — Separate QA, Architecture and Strategy and close the improvement loop

> **2026-09-10 update:** [PLAT-306](plat-306.md) supersedes mandatory Markdown
> checkpoint/report maintenance with optional SQLite review notes on the existing
> result tool. Typed lifecycle records remain authoritative; historical design
> discussion below is retained. No reporting-only turn is required.

| Coordination | Value |
|---|---|
| Assigned agent | Codex |
| Ticket state | `implemented first release; deployment and live workflow acceptance pending` |
| Last synchronized | `2026-09-10` |

## User problem

Technical Review mixes bug repair with architectural improvement. Recurring
platform defects occupy review attention while opportunities in learning, KB,
reports, databases, scripts and orchestration receive little independent focus.
Strategy needs internal/external investigation and tracked outcomes rather than
only reading the existing plan/dashboard. Approval, application and impact must
be distinguishable.

## Implemented

- Canonical Architecture module, focus history, finding/decision ownership,
  reports and research guidance; historical identities remain intact.
- Independent due decisions and sequential module stages with their own receipts
  and recovery. Protected Architecture/Strategy dates persist across Gate skips;
  explicit dated deferrals remain possible. See PLAT-303.
- Typed research scope on background dispatch: inherited authorized MCP/browser
  setup, research and Pulse custom tools, scoped checkpoint writes, no workflow
  edit/production execution custom tools. External connection grants are preserved;
  there is no new universal external read-only proxy.
- Architecture improvement records reuse the existing impact ledger. Decision
  answers and application receipts atomically advance linked Architecture and
  Strategy proposals. Architecture application/adoption require appropriate
  application/outcome evidence; applied records cannot reset to proposed.
- Health / Architecture / Strategy UI with separate Drift Check and compact
  improvement-to-outcome history. Full reports remain readable.

## Acceptance

Automated tests cover all four modules due, independent completion after QA
failure, protected boundary persistence, research tool filtering, Architecture
coverage and finding ownership, and proposal/approval/application/assessment/
adoption transitions. Frontend tests cover areas, filters, history, and honest
outcome presentation. Full affected Go packages, focused frontend tests and
production frontend build pass; see the implementation document for exact scope.

## Remaining / not claimed complete

Deploy and observe an actual autonomous workflow improvement. Finish immutable
partial-run evidence under PLAT-047/089 and builder contract/learning prevention
under PLAT-257/298. An automated lifecycle fixture is not evidence of business
impact. No legacy workflow schema or learning file is rewritten by this change.

[Implementation and operational contract](../../pulse-workflow-improvement-system.md)
