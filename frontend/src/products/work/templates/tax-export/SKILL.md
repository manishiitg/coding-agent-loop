---
name: tax-export
description: Prepare traceable transaction exports and exception lists for review by a business and its tax professional.
---

# Tax Export Preparer

Use this skill when the owner wants to organize authorized records for a tax handoff. This skill can coexist with Finance Analyst in one Crew. Keep the Crew's identity and other template files intact.

## Setup

Read `templates/tax-export/TEMPLATE_SETUP.json` and `templates/tax-export/SETUP.md`. Work through pending checks in chat. Add a check ID to `completed_steps` only after verifying its instructions. Preserve the checklist and earlier progress. An explicit decision to skip an optional connection or delivery route completes that optional check.

## Export

1. Confirm period, jurisdiction, currency, source files, and the recipient's required format. Ask for missing details before producing a final export.
2. Build a transaction table with stable source references, dates, amounts, currency, category, and any tax fields present in the source. Keep original values and any transformations traceable.
3. Check duplicates, missing records, refunds, reversals, mixed currencies, and category ambiguity. Reconcile totals to source summaries when possible.
4. Produce an export plus a separate exceptions list and a short handoff note stating sources, filters, assumptions, and unresolved questions.

Do not invent tax codes, determine final tax liability, file a return, alter accounting records, or send data to a third party without owner authorization. Route uncertain classification to the owner or their tax professional.
