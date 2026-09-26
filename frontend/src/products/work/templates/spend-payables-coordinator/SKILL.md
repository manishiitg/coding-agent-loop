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

## Fictional worked example

Input: document `invoice-document-88` version `v1`, hash `aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa`, claims vendor `vendor-42`, invoice `INV-88`, USD 108.00. Intake `intake-88`, AP snapshot `ap:vendor-42@rev-7`, and policy `policy:ap-v3` refer to the same fictional entity. The AP search finds no exact duplicate at 10:10 UTC; approval is pending. No current payment source or provider receipt is supplied.

First result: review `review-88` links the document hash and version to the intake and AP snapshot. It records duplicate_state **none within the checked AP scope**, approval_state **pending**, disposition **ready_for_owner_review**, action_state **none**, USD 108.00, policy reference and `ap-owner`. Ask the owner to confirm invoice fields, required evidence and current paid/scheduled status before an approval proposal. Keep document intake, AP review, approval and payment as separate states. On a later run, recheck the AP revision and duplicate search while retaining the document and review IDs.

## Inadequate output to reject

“INV-88 is approved and paid because it appears once in the AP export.” Reject: one export match does not prove approval, current payment state or a provider receipt. Do not create a bill or initiate a payment from this review.

This fictional example does not complete setup. Reproduce a bill against authorized AP and payment-status sources and save the owner's decision.

## Boundaries

The default skill reads and prepares decisions. Do not create a vendor, change bank details, approve an expense or bill, initiate a payment, or post ledger entries from template installation. Payment execution requires verified payee details, a separate authorized write route, exact amount/currency approval, duplicate check immediately before execution, and a recorded provider receipt. If bank details changed, escalate to the owner's independent verification process. A schedule or Automation needs separate setup and testing.
