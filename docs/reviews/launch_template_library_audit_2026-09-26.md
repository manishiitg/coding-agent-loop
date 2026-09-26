# Launch template library audit

Date: 2026-09-26. Scope: authored Crew and Automation Playbook library in this checkout. This is a progress audit, not a claim of customer or production readiness.

## Current evidence

| Requirement from the launch catalog | Current proof | State |
| --- | --- | --- |
| Browse coverage for Finance, Marketing/Website Growth, Sales, Customer Success, Customer Support, Product, Operations, Engineering, QA, Security, GTM, and Shopify | `docs/design/crew_template_catalog.md`; generated `playbooks/crew-agents/*/catalog.json` | All named categories have locally installable Crew definitions. |
| Reusable single-Crew setup | 63 generated Crew records with selected skill and pending chat checklist | Available locally; no customer setup is implied. |
| Multi-Crew proposal and typed handoffs | 24 locally installable multi-Crew Playbooks inside 47 total packages; 29 contract suites pass `python3 playbooks/scripts/validate_playbooks.py` | Authored and mechanically checked. Builder must still insert blocking validation in an actual Workflow. |
| Website Growth from proposal to measured result | Website Growth Loop v0.5.0, approved page, ship, distribution and traffic examples | Fictional complete route; source truth and a real site run remain unproved. |
| SaaS finance collections, payables and refunds | Finance Operations Review, Subscription Receivable to Verified Outcome, Invoice Intake to Reviewed Payable, Refund Request to Reconciled Outcome | Exact-object fictional contracts; actual provider, bank and ledger integration remains customer-specific. |
| Shopify operator journeys | Seven Shopify Crews and seven Automation Playbooks, with source and rejected examples | Fictional contracts; no live merchant run is proved. |
| Production availability | Current branch is not the planned final Dominion deployment | Pending by user direction: deploy at the end. |

## Content depth still to close

The launch catalog's quality rule asks each Crew for an inspectable input/output example, a failed example, a hard source probe, owner review, repeat rule and a customer-like exercise. A passing package validator proves shape and links, not this full content standard. The four Engineering Crews now include worked and rejected outputs plus role-specific source probes in their installed skills. Shopify skills have illustrative output and rejection sections. The next highest-value authored gaps are:

1. **Customer Success:** Customer Onboarding Coordinator, Product Adoption Analyst and Customer Health Coordinator have methods and an Automation contract, but their installed skills still need full standalone worked and rejected outputs with source coverage and owner decisions.
2. **Sales intake:** Lead Intake & Qualifier, Account Researcher and Sales Follow-up Coordinator have a validated Automation route, but their installed skills still need equivalent standalone examples and failure cases.
3. **Finance core:** Billing Operations Coordinator, Revenue & Close Analyst and Spend & Payables Coordinator need richer standalone cases even though the billing packs and several Finance Playbooks already contain fictional artifacts.

These are prioritized content gaps, not proof that the remaining skills are complete. Review each category's actual installed skill and setup guide before promotion. Exercise at least one authorized customer-like case per role during pilot setup. Keep external sends, payments, infrastructure changes and schedules behind the customer's reviewed action route.

## Next verification

After those examples are authored, run generated catalog checks, Playbook validation, targeted Crew tests and a production frontend build. Then inspect a real Builder installation: Playbook proposal, Crew reuse/creation, actual file paths, blocking validator behavior, setup evidence, owner review and manual first run. Deployment and admin-only Dominion testing come after the library and installation checks.
