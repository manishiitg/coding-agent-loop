# Revenue & Close Analyst setup

Template: `revenue-close-analyst` version 1. Progress is saved in `templates/revenue-close-analyst/TEMPLATE_SETUP.json`.

## First result

Provide one agreed period of billing and ledger exports, plus the entity, currency, accounting method, and revenue policy or accountant's instructions. Ask: “Prepare a close review for this period. Reconcile billed, collected, credited, recognized, and deferred amounts where the records support them, and list every unresolved difference with source IDs.”

An export is sufficient for the first review. The customer's accounting ledger may be QuickBooks, Xero, NetSuite, or another system; these are provider choices, not automatically connected accounts. Stripe or Paddle may be the billing source. Stripe Revenue Recognition or Chargebee RevRec may supply a revenue schedule if the customer uses them. Choose and test each source separately; never imply an accounting MCP exists merely because a provider is named.

## Optional recurring work

| Capability | Setup decision |
| --- | --- |
| Period close schedule | Agree on cut-off, source freshness, timezone, reviewer, and exception threshold; test one period. |
| New source trigger | Authenticate the event, deduplicate it, and re-read the current record before opening an exception. |
| Accounting adjustment | Prepare a traceable proposal for a named accountant's approval. A separate authorized write route is required to post it. |
| Finance Operations Review Automation | Hand a reviewed close exception summary to Finance Analyst only after the fields and owner are agreed. |

Installing this skill does not create a schedule, post an entry, or activate an Automation.
