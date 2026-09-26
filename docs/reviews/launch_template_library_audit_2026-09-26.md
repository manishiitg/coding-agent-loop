# Launch template library audit

Date: 2026-09-26. Scope: authored Crew and Automation Playbook library in this checkout. This is a progress audit, not a claim of customer or production readiness.

## Current evidence

| Requirement from the launch catalog | Current proof | State |
| --- | --- | --- |
| Browse coverage for Finance, Marketing/Website Growth, Sales, Customer Success, Customer Support, Product, Operations, Engineering, QA, Security, GTM, and Shopify | `docs/design/crew_template_catalog.md`; generated `playbooks/crew-agents/*/catalog.json` | All named categories have locally installable Crew definitions. |
| Reusable single-Crew setup | 67 generated Crew records with selected skill and pending chat checklist | Available locally; no customer setup is implied. |
| Multi-Crew proposal and typed handoffs | 30 locally installable multi-Crew Playbooks inside 47 total packages; 35 contract suites pass `python3 playbooks/scripts/validate_playbooks.py` | Authored and mechanically checked. Builder must still insert blocking validation in an actual Workflow. |
| Website Growth from proposal to measured result | Website Growth Loop v0.5.0, approved page, ship, distribution and traffic examples | Fictional complete route; source truth and a real site run remain unproved. |
| FinOps cost to billed savings | Cost Anomaly to Verified Savings v0.6.0; Cloud Cost Analyst → Engineering Delivery Coordinator → Finance Analyst, with pending change and verified billing fixtures, a rejected false savings claim, ten setup checks and an installer copy test | Fictional typed route; a real billing source, change approval, deployment and later bill remain unproved. |
| Signup-to-paid funnel | Funnel and Conversion Intelligence v0.2.0; new Funnel Analyst → Growth Experiment Planner, reconciled or baseline-first stage counts, pending plan and rejected false winner, ten setup checks and installer test | Fictional typed route; real event/billing join, owner decision and later experiment outcome remain unproved. |
| SaaS activation and retention | Activation and Retention Intelligence v0.2.0; new Lifecycle Analyst → Growth Experiment Planner, comparable, baseline-first and pending-maturity cohorts, pending plan and rejected false winner, ten setup checks and installer test | Fictional typed route; real event/billing join, owner decision and later experiment outcome remain unproved. |
| Approved experiment to outcome | Growth Experimentation and Follow-Through v0.3.0; new Experiment Run Coordinator → Growth Outcome Analyst, pending approval and window, exact provider launch, inconclusive and measured readouts, rejected false launch/winner, ten setup checks and installer test | Fictional typed route; real plan approval, provider and outcome sources, owner decision and later action remain unproved. |
| Sampled AI-answer visibility | AI Visibility Intelligence v0.2.0; AI Visibility Analyst → Search Opportunity Mapper, two-answer snapshot, pending page opportunity, rejected inflated citation/publication claim, ten setup checks and installer copy test | Fictional typed route; real answer access, page facts, owner review and repeat sampling remain unproved. |
| Focused SEO review | SEO Intelligence v0.2.0, SEO Analyst → Search Opportunity Mapper, dated issue-to-question artifacts and rejected publication claim; mocked installer test copies its ten-check setup and validator | Fictional typed route; real Builder run, page/source and owner review remain unproved. |
| SaaS finance collections, payables and refunds | Finance Operations Review, Subscription Receivable to Verified Outcome, Invoice Intake to Reviewed Payable, Refund Request to Reconciled Outcome | Exact-object fictional contracts; actual provider, bank and ledger integration remains customer-specific. |
| Shopify operator journeys | Seven Shopify Crews and seven Automation Playbooks, with source and rejected examples | Fictional contracts; no live merchant run is proved. |
| Production availability | Current branch is not the planned final Dominion deployment | Pending by user direction: deploy at the end. |

## Content depth still to close

The launch catalog's quality rule asks each Crew for an inspectable input/output example, a failed example, a hard source probe, owner review, repeat rule and a customer-like exercise. A passing package validator proves shape and links, not this full content standard. The four Engineering, four Customer Success, three inbound Sales, and three core Finance Crews now include worked and rejected outputs plus role-specific source probes in their installed skills. Shopify skills have illustrative output and rejection sections. The next highest-value gaps are:

1. **Remaining category-by-category content review:** The newly deepened skills meet the example/probe pattern, while the other installed roles and older single-workflow Playbooks still need an individual review against the same rule before the library is called complete.
2. **Customer case verification:** Authored examples now cover the core Finance, inbound Sales, Customer Success, and Engineering roles, but no actual account, source access or owner review has been exercised. The same applies to the other template categories until pilot setup evidence is recorded.

These are prioritized content gaps, not proof that the remaining skills are complete. Review each category's actual installed skill and setup guide before promotion. Exercise at least one authorized customer-like case per role during pilot setup. Keep external sends, payments, infrastructure changes and schedules behind the customer's reviewed action route.

## Legacy Playbooks still in the catalog

The other **17 of 47** installable Playbook packages have no `agent_slots`, `SETUP.json`, or executable handoff suite. They remain Workflow guides in the same catalog; they are not yet equivalent to the 30 chat-led multi-Crew proposals. The Playbook picker now labels the two kinds separately and states that Workflow guides have no predefined Crew team or tracked setup checklist; an installer test confirms this for Basic Browser Setup. This count excludes the example package under `playbooks/templates/`.

| Area | Packages | Launch disposition and reason |
| --- | --- | --- |
| Browser QA (8) | Basic Browser Setup; Authentication and Session Validation; Role and Permission Validation; Critical Journey Validation; Flaky-Test Detection and Stabilization; Browser Test Self-Healing; Scheduled Regression and Synthetic Monitoring; Release and PR Quality Gate. | Keep as specialized Workflow guides for setup, test methods and recurrence. Browser Journey QA Analyst, Flaky Test Investigator and Release Quality Assistant are the installable Crews; Release Candidate to Reviewed Gate is the typed team route. A guide does not imply that its test runner, schedule or approval path is configured. |
| Growth Analytics foundation (1) | Growth Data Foundation. | Keep as a shared data/identity setup guide. It is useful beneath multiple Crew jobs but does not itself require a second Crew. |
| Reliability Operations methods (3) | CI and Deployment Failure Triage; Incident Investigation and Coordination; Governed Remediation and Recovery. | Keep as focused investigation/remediation Workflow guides. Incident Investigator and Engineering Delivery Coordinator can use their methods; Incident to Verified Recovery is the typed team route. Never imply a remediation action follows from installing a guide. |
| Reliability Operations candidate (1) | Post-Incident Review and Actions. | Distinct retrospective and follow-up job after recovery. Needs a review owner, exact incident-to-action artifact and proof of accepted/completed work if promoted to a team route. |
| Performance Engineering (2) | Browser Performance Validation; API Performance Validation. | Keep as measurement methods for Performance Investigator. A second Crew is useful only when an actual owner or access boundary is identified. |
| Engineering Operations Intelligence (1) | Engineering Operations Intelligence. | Keep as a Workflow guide for now. Its cross-team metrics and durable data model need a named analytical Crew owner and a concrete review/action handoff before becoming a team template. |
| Security Engineering (1) | Application Security Assessment and Remediation. | Keep as an authorized assessment method guide. Security Findings Analyst and Security Remediation Coordinator already form the typed Finding to Verified Remediation route; the broad assessment package does not itself configure those Crews. |

These dispositions describe the current installable product, not a claim that the guides are operationally configured. The next authored candidate is the distinct post-incident job. Each needs a customer-like source case, rejected claim, owner and exact handoff before it is promoted.

## Next verification

After the remaining content and legacy package decisions, run generated catalog checks, Playbook validation, targeted Crew tests and a production frontend build. Then inspect a real Builder installation: Playbook proposal, Crew reuse/creation, actual file paths, blocking validator behavior, setup evidence, owner review and manual first run. Deployment and admin-only Dominion testing come after the library and installation checks.
