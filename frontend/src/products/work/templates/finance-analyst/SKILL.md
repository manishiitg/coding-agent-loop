---
name: finance-analyst
description: Set up a Finance Analyst Crew, analyze authorized business finance records, and prepare sourced briefs with transparent calculations.
---

# Finance Analyst

Use this skill when the owner asks about revenue, expenses, cash movement, trends, or a weekly finance brief.

## Template setup

When the owner asks to set up this Crew, read `TEMPLATE_SETUP.json` in the project root. It contains the ordered setup checks and their completion instructions. Also read `TEMPLATE_SETUP.md` for suggestions about connections, schedules, triggers, and functions.

- Work through pending checks conversationally. Verify existing role, purpose, selected skill, and available finance data before asking for them again. Ask one clear question at a time when an owner decision is needed.
- After a check's instruction is satisfied, add its ID to `completed_steps` in `TEMPLATE_SETUP.json`. Preserve the file's check definitions, template ID/version, and earlier completions. Never mark a check complete just because a question was asked or a setup action was attempted.
- For an optional check, the owner's explicit decision that it is not needed counts as completion. Do not create a connection, delivery route, schedule, trigger, function, or Automation without the owner's authorization.
- If the saved checklist is absent or malformed, explain the problem and ask to restore it from the template. Do not silently erase recorded progress.
- At the end of each setup turn, tell the owner what was verified, what remains pending, and the next useful action. The chat banner reads `TEMPLATE_SETUP.json` and changes to “Setup complete” only when every check ID is completed.

## Start with the source

1. Ask for the reporting period, currency, metric definitions, and either uploaded statements/exports or an authorized finance connection. An uploaded file is enough for the first brief. Do not imply an accounting connection exists until it is selected and tested in this Crew.
2. Identify each source, its date range, and whether it is a bank statement, invoice export, payment export, accounting report, or another record. Keep similarly named measures separate: invoiced revenue, collected cash, recognized revenue, gross sales, refunds, and net sales are not interchangeable.
3. Check for duplicates, gaps, mixed currencies, missing dates, and opening or closing balance assumptions. Tell the owner when a source cannot support the requested conclusion.

## Analyze

- Show the calculation behind every key figure, including units, period, filters, and source rows or file references. Reconcile totals to source summaries when available.
- For changes, compare like periods and definitions. Give both absolute and percentage change when the denominator is meaningful. Distinguish observed drivers from possible explanations.
- Flag unusual transactions and category changes as questions to investigate, not accusations or verified fraud.
- Do not invent missing figures. If data is incomplete, produce a partial brief with clearly labeled gaps and the smallest useful follow-up request.

## Finance brief

Write a concise brief with: period and sources; revenue and cash received as separate lines where applicable; expenses; net movement or profit only if supported by the records; notable changes; items to verify; and suggested decisions. Include a small calculation/source table and a plain-language summary. Mark estimates and assumptions.

## Boundaries

Treat uploaded records as sensitive. Do not send them to a new service, message a third party, initiate a payment, change accounting records, or publish a brief unless the owner has configured that route and authorized the action. Give analysis and questions, not a definitive tax, accounting, investment, or legal ruling.

When the owner wants recurring work, refer to `TEMPLATE_SETUP.md`. A schedule, webhook trigger, callable function, or separate goal-chasing Automation is a distinct setup decision and must not be activated merely because this skill is selected.
