# Billing Operations Coordinator setup

Template: `billing-operations-coordinator` version 1. Progress is saved in `templates/billing-operations-coordinator/TEMPLATE_SETUP.json`.

## First result

Provide a dated invoice/payment export or authorize and test a billing source, then share invoice terms, refund/contact policies, and an owner for exceptions. Ask: “Review overdue invoices, failed payments, refund requests, and disputes for this period. Give me a source-linked queue and drafts for review.” The first result may cover only the case types supported by the supplied data; state the gaps.

Choose the customer's actual billing source. Stripe and Paddle are bundled MCP discovery options; availability of a particular account or action must be tested in this Crew. Chargebee, QuickBooks, Xero, and customer messaging may require another authorized connector or an export. Do not copy accounts or secrets from another Crew.

## Optional work after the first queue

| Capability | Setup decision |
| --- | --- |
| Queue review schedule | Choose timezone, cadence, source freshness, and reviewer; test a run before enabling. |
| Billing event trigger | Validate provider signature, account, event ID, object ID, and duplicate delivery; fetch current object state before opening a case. |
| Customer message or refund action | Review the exact customer/payment, content or amount, policy, destination, permission, and approval. Record provider response. |
| Finance handoff | Agree on queue fields, source links, and exception ownership before a separate Automation sends a bounded summary to Finance Analyst or Revenue & Close Analyst. |

Selecting this template enables none of these actions.
