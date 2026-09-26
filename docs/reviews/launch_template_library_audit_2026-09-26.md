# Launch template library audit

Date: 2026-09-26. Scope: authored Crew and Automation Playbook library in this checkout. This is a progress audit, not a claim of customer or production readiness.

## Current evidence

| Requirement from the launch catalog | Current proof | State |
| --- | --- | --- |
| Browse coverage for Finance, Marketing/Website Growth, Sales, Customer Success, Customer Support, Product, Operations, Engineering, QA, Security, GTM, and Shopify | `docs/design/crew_template_catalog.md`; generated `playbooks/crew-agents/*/catalog.json` | All named categories have locally installable Crew definitions. |
| Reusable single-Crew setup | 63 generated Crew records with selected skill and pending chat checklist | Available locally; no customer setup is implied. |
| Multi-Crew proposal and typed handoffs | 26 locally installable multi-Crew Playbooks inside 47 total packages; 31 contract suites pass `python3 playbooks/scripts/validate_playbooks.py` | Authored and mechanically checked. Builder must still insert blocking validation in an actual Workflow. |
| Website Growth from proposal to measured result | Website Growth Loop v0.5.0, approved page, ship, distribution and traffic examples | Fictional complete route; source truth and a real site run remain unproved. |
| FinOps cost to billed savings | Cost Anomaly to Verified Savings v0.6.0; Cloud Cost Analyst → Engineering Delivery Coordinator → Finance Analyst, with pending change and verified billing fixtures, a rejected false savings claim, ten setup checks and an installer copy test | Fictional typed route; a real billing source, change approval, deployment and later bill remain unproved. |
| Focused SEO review | SEO Intelligence v0.2.0, SEO Analyst → Search Opportunity Mapper, dated issue-to-question artifacts and rejected publication claim; mocked installer test copies its ten-check setup and validator | Fictional typed route; real Builder run, page/source and owner review remain unproved. |
| SaaS finance collections, payables and refunds | Finance Operations Review, Subscription Receivable to Verified Outcome, Invoice Intake to Reviewed Payable, Refund Request to Reconciled Outcome | Exact-object fictional contracts; actual provider, bank and ledger integration remains customer-specific. |
| Shopify operator journeys | Seven Shopify Crews and seven Automation Playbooks, with source and rejected examples | Fictional contracts; no live merchant run is proved. |
| Production availability | Current branch is not the planned final Dominion deployment | Pending by user direction: deploy at the end. |

## Content depth still to close

The launch catalog's quality rule asks each Crew for an inspectable input/output example, a failed example, a hard source probe, owner review, repeat rule and a customer-like exercise. A passing package validator proves shape and links, not this full content standard. The four Engineering, three Customer Success, three inbound Sales, and three core Finance Crews now include worked and rejected outputs plus role-specific source probes in their installed skills. Shopify skills have illustrative output and rejection sections. The next highest-value gaps are:

1. **Remaining category-by-category content review:** The newly deepened skills meet the example/probe pattern, while the other installed roles and older single-workflow Playbooks still need an individual review against the same rule before the library is called complete.
2. **Customer case verification:** Authored examples now cover the core Finance, inbound Sales, Customer Success, and Engineering roles, but no actual account, source access or owner review has been exercised. The same applies to the other template categories until pilot setup evidence is recorded.

These are prioritized content gaps, not proof that the remaining skills are complete. Review each category's actual installed skill and setup guide before promotion. Exercise at least one authorized customer-like case per role during pilot setup. Keep external sends, payments, infrastructure changes and schedules behind the customer's reviewed action route.

## Legacy Playbooks still in the catalog

The other **21 of 47** installable Playbook packages have no `agent_slots`, `SETUP.json`, or executable handoff suite. They remain Workflow guides in the same catalog; they are not yet equivalent to the 26 chat-led multi-Crew proposals. The Playbook picker now labels the two kinds separately and states that Workflow guides have no predefined Crew team or tracked setup checklist; an installer test confirms this for Basic Browser Setup. This count excludes the example package under `playbooks/templates/`.

| Area | Packages needing a launch decision or deeper route |
| --- | --- |
| Browser QA (8) | Basic Browser Setup; Authentication and Session Validation; Role and Permission Validation; Critical Journey Validation; Flaky-Test Detection and Stabilization; Browser Test Self-Healing; Scheduled Regression and Synthetic Monitoring; Release and PR Quality Gate. |
| Growth Analytics (5) | Growth Data Foundation; Funnel and Conversion Intelligence; Activation and Retention Intelligence; Growth Experimentation and Follow-Through; AI Visibility Intelligence. |
| Reliability Operations (4) | CI and Deployment Failure Triage; Incident Investigation and Coordination; Governed Remediation and Recovery; Post-Incident Review and Actions. |
| Performance Engineering (2) | Browser Performance Validation; API Performance Validation. |
| Engineering Operations Intelligence (1), Security Engineering (1) | Engineering Operations Intelligence; Application Security Assessment and Remediation. |

Review each against the newer QA, Security, Engineering, Marketing and Website Growth routes. Promote it to a typed Crew journey where it has a distinct customer job and owner; otherwise present it clearly as a Workflow guide. Do not imply a pending Crew setup checklist or verified multi-Crew handoff for these packages.

## Next verification

After the remaining content and legacy package decisions, run generated catalog checks, Playbook validation, targeted Crew tests and a production frontend build. Then inspect a real Builder installation: Playbook proposal, Crew reuse/creation, actual file paths, blocking validator behavior, setup evidence, owner review and manual first run. Deployment and admin-only Dominion testing come after the library and installation checks.
