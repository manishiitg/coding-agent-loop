---
name: spend-payables-coordinator
description: Review vendor bills, subscriptions, company card charges, and expenses against approval policy with a source-linked payables queue.
---

# Spend & Payables Coordinator

Use this skill to organize what the business owes and what it has spent. It is a separate Crew when vendor, card, or payment permissions differ from finance analysis. It may also be a supporting capability in a Crew with the same approved owner and access.

## Setup in chat

Read `templates/spend-payables-coordinator/TEMPLATE_SETUP.json` and `SETUP.md`. Verify each check with the owner before adding its ID to `completed_steps`. Preserve existing progress and an existing primary Crew identity. Chat-only or no-recurrence is a valid completed decision for optional checks. Report verified work, exceptions, and the next pending check.

## Produce the first payables queue

1. Confirm entity, period, currency, vendor owner, bill approval and payment policy, budget owners, and duplicate criteria. Obtain authorized bill, card, expense, or vendor exports; a live connection is optional. Identify which system is authoritative for bill status and whether payment is already scheduled.
2. Extract bill ID, vendor identity, invoice number, invoice date, due date, amount, currency, tax fields present, PO/contract reference, receipt, category, approval status, and payment status. Preserve the original document and source link. Treat a receipt, card transaction, and vendor invoice as different records until matched.
3. Check exact and near duplicates using vendor, invoice number, amount, currency, dates, and document evidence. Check credits, partial payments, recurring subscriptions, missing receipts or POs, unusual amounts, policy limits, and expenses coded to the wrong period. Flag uncertainty rather than accusing a vendor or employee.
4. Produce a queue with one row per bill or exception: source IDs, due date, amount/currency, approval owner, current state, policy reason, proposed next step, and evidence gap. Separate **ready for owner review**, **needs more evidence**, and **already paid/scheduled**. Do not represent an uncoded bill as an approved payment.
5. Summarize spend by vendor/category and compare to a supplied budget only when periods and definitions match. Ask the owner to review sample matches, classifications, and priorities before completing setup.

## Boundaries

The default skill reads and prepares decisions. Do not create a vendor, change bank details, approve an expense or bill, initiate a payment, or post ledger entries from template installation. Payment execution requires verified payee details, a separate authorized write route, exact amount/currency approval, duplicate check immediately before execution, and a recorded provider receipt. If bank details changed, escalate to the owner's independent verification process. A schedule or Automation needs separate setup and testing.
