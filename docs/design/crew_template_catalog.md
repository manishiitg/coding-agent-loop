# Crew template catalog

Status: catalog in progress, 2026-09-26. **29 Crew templates are installable locally:** five Finance, ten Website Growth, three Sales, three Customer Success, four Engineering, and four Shopify. The remaining Crew names in this document are proposals. The seven multi-Crew Automation Playbooks below are installable chat proposals, not active automations. This document is a product map, not evidence that a customer has connected tools, completed setup, or achieved an outcome. The local release has not been deployed to Dominion.

## Find a job and its status

Use **category** for the team or business function, **use case** for the customer's concrete job, **Crew template** for a reusable agent capability, and **Automation Playbook** for a proposed multi-Crew journey. One Crew can hold several compatible capabilities. A Playbook can reuse existing Crews; installing it does not create them or start a run. See [how setup and activation work](#product-model).

**Product target:** A Crew does the recurring work a person would do *using* the customer's existing SaaS: read signals, investigate exceptions, prepare decisions, coordinate owners, and verify outcomes across tools. The template specifies the human job and its first reviewable result. Existing systems remain the sources of record and, when authorized, the places where approved actions happen. A strong template names its input systems, decisions, handoffs, approval boundary, and proof of completion. Prioritize cross-tool jobs with an accountable owner over standalone features such as generic PR review, vulnerability scanning, or test generation.

| Browse category | Boundary and customer use cases | Installable Crew templates | Planned roles or packs | Relevant installable Automation Playbook |
| --- | --- | ---: | ---: | --- |
| [Finance](#finance) | Subscription billing exceptions; finance performance; close reconciliation; vendor spend; tax export. Owns money records and financial review, not sales contact. | 5 | 3 later roles and 4 deeper capability packs | [Finance Operations Review](../../playbooks/agentic-engineering-platform/finance/finance-operations-review/SKILL.md) |
| [Marketing → Website Growth](#website-growth) | A new or existing site needs relevant traffic: audit, buyer questions, SEO, content, distribution, conversion, and measurement. | 10 | 0 in this subcategory | [Website Growth Loop](../../playbooks/agentic-engineering-platform/website-growth/website-growth-loop/SKILL.md) |
| [Marketing → Other Growth](#marketing--growth) | Competitive positioning, campaign analysis, and experiment design beyond the website journey. | 0 | 3 | None in this Crew catalog |
| [Sales](#sales) | Turn inbound interest into a qualified, reviewed meeting; later prepare calls, proposals, and pipeline reviews. Owns prospect qualification and contact. | 3 | 3 | [Inbound Lead-to-Meeting Review](../../playbooks/agentic-engineering-platform/sales/inbound-lead-to-meeting-review/SKILL.md) |
| [Customer Success](#customer-success) | Move a signed customer through onboarding to observed first value and account health. Owns post-sale adoption. | 3 | 0 | [New Customer to First Value](../../playbooks/agentic-engineering-platform/customer-success/new-customer-to-first-value/SKILL.md) |
| [Customer Support](#customer-support) | Triage and answer incoming issues, coordinate escalation, and analyze feedback. Owns the support case, not the account's full adoption journey. | 0 | 4 | None in this Crew catalog |
| [Operations](#operations) | Meeting actions, project status, order exceptions, vendor research, and document intake. | 0 | 6 | None in this Crew catalog |
| [Engineering](#engineering) | Incident investigation, performance, cloud cost, and delivery coordination across engineering tools. QA and Security have their own browse entries below. | 4 | QA and Security cross-listings | [Incident to Verified Recovery](../../playbooks/agentic-engineering-platform/engineering/incident-to-verified-recovery/SKILL.md) |
| [QA](#qa) | Browser journeys, regression, role and permission checks, flaky tests, and release gates. Owns evidence that a change works. | 0 | 3 primary roles | [Browser QA Workflow Playbooks](../../playbooks/README.md#browser-qa) exist; no Crew-composition proposal here |
| [Security](#security) | Authorized application findings, exposure triage, remediation review, and retest. Owns evidence that a risk is understood and addressed. | 0 | 3 primary roles | [Application Security Assessment and Remediation](../../playbooks/agentic-engineering-platform/security-engineering/application-security/application-security-assessment-remediation/SKILL.md) exists as a Workflow Playbook |
| [GTM](#gtm) | Coordinate positioning, launch, website demand, lead capture, qualification, and a measurable pipeline outcome across Marketing and Sales. | Existing Marketing and Sales Crews are cross-listed; 0 GTM-specific packs | 2 primary role candidates | [Website Growth Loop](../../playbooks/agentic-engineering-platform/website-growth/website-growth-loop/SKILL.md) and [Inbound Lead-to-Meeting Review](../../playbooks/agentic-engineering-platform/sales/inbound-lead-to-meeting-review/SKILL.md) cover parts; no end-to-end GTM proposal yet |
| [Shopify](#shopify) | Store launch, catalog quality, storefront conversion, order exceptions, returns, and store performance. | 4 Shopify-specific Crews | Generic Website Growth Crews can support public-storefront work | [Order Exception to Resolution](../../playbooks/agentic-engineering-platform/shopify/order-exception-to-resolution/SKILL.md), [Storefront Opportunity to Verified Change](../../playbooks/agentic-engineering-platform/shopify/storefront-opportunity-to-verified-change/SKILL.md) |

The browse catalog now names **eleven first-class categories**: Finance, Marketing, Sales, Customer Success, Customer Support, Operations, Engineering, QA, Security, GTM, and Shopify. **Website Growth** is a Marketing subcategory, although today's Crew picker still labels it “Website Growth” directly. Engineering and Shopify now appear in the local Crew picker. QA and Security are planned as independent browse categories; GTM spans Marketing and Sales; Shopify is a store-specific lens across Growth, Sales, Operations, and Support. A template has one canonical ID, skill, and setup record even when it appears in several browse categories. QA, Security, and GTM remain **documentation taxonomy**, not new choices shipped in the Crew picker. The public site's broader Money, Customers, Growth, Operations, and Engineering labels are separate navigation groups.

**Status terms:** **Available locally** means the Crew template can be selected and installs a skill plus a pending setup checklist, or the Automation Playbook can be installed as a Builder proposal. **Planned** means the row is a candidate, not selectable. **Operationally ready** applies only to a particular customer's configured Crew or Automation after access, a representative result, handoffs, and run policy are verified. None of the category counts asserts operational readiness.

In the category tables below, an unlinked suggested Automation name is an idea, not an installable Playbook. A linked existing Workflow Playbook can be installed separately, but it does not make its proposed Crew identity or a new cross-category handoff available.

## Six installable Automation Playbooks

| Playbook and use case | Required Crew capabilities | Optional capabilities | First reviewable result | Manual first-run proof and outcome |
| --- | --- | --- | --- | --- |
| [Website Growth Loop](../../playbooks/agentic-engineering-platform/website-growth/website-growth-loop/playbook.json): find useful traffic opportunities for a new site | Website Growth Starter → Search Opportunity Mapper | Technical SEO, content brief, page draft, measurement | Source-linked priority brief and buyer-question opportunity map | Validate the strategist-to-search artifact on the customer's site; record owner decision and baseline or baseline-first policy. No traffic lift is claimed before comparable data exists. |
| [Inbound Lead-to-Meeting Review](../../playbooks/agentic-engineering-platform/sales/inbound-lead-to-meeting-review/playbook.json): turn an inbound enquiry into a reviewed booking path | Lead Intake & Qualifier → Sales Follow-up Coordinator | Account Researcher | Sourced qualification brief and an unsent, reviewable booking offer | Validate lead identity, fit, duplicate/contact gates and the draft. Count delivery only from a provider receipt, and a meeting only from a calendar or CRM event. |
| [Finance Operations Review](../../playbooks/agentic-engineering-platform/finance/finance-operations-review/playbook.json): resolve billing exceptions with finance context | Billing Operations Coordinator → Finance Analyst | Revenue & Close Analyst, Spend & Payables Coordinator | Validated exception queue and source-linked finance impact readout | Run with authorized billing and ledger records; review the queue and impact together. No refund, payment, or accounting write follows from installation. |
| [New Customer to First Value](../../playbooks/agentic-engineering-platform/customer-success/new-customer-to-first-value/playbook.json): track a new customer to an agreed result | Customer Onboarding Coordinator → Product Adoption Analyst | Customer Health Coordinator | Owned milestone register and evidence-linked first-value readout | Verify customer identity, event definition, source coverage, and the onboarding-to-adoption handoff on a real authorized account. A milestone plan alone is not first-value proof. |
| [Incident to Verified Recovery](../../playbooks/agentic-engineering-platform/engineering/incident-to-verified-recovery/playbook.json): move a production incident through investigation, owned delivery work, and observed recovery | Incident Investigator → Engineering Delivery Coordinator | None in v1 | Sourced incident timeline and validated blocker ledger | Validate same incident, service, owner, and approval state. Run manually on authorized records; an approved change and deployment still require a separate telemetry-based recovery decision. |
| [Order Exception to Resolution](../../playbooks/agentic-engineering-platform/shopify/order-exception-to-resolution/playbook.json): resolve a Shopify order problem tied to a return or refund request | Store Operations Coordinator → Returns & Refunds Coordinator | None in v1 | Sourced order exception and validated policy/money review with an unsent customer draft | Validate same store, order, case, currency, and policy. Run manually on an authorized case; count a refund or sent response only from a provider receipt. |
| [Storefront Opportunity to Verified Change](../../playbooks/agentic-engineering-platform/shopify/storefront-opportunity-to-verified-change/playbook.json): turn a specific buyer friction into a reviewed catalog correction | Shopify Growth Analyst → Catalog & Merchandising Analyst | None in v1 | Validated growth opportunity, exact variant change review, and post-publish retest rule | Validate store, market, product, variant, and opportunity; publish only after merchant approval, then measure only against a comparable baseline. |

Each Playbook's `playbook.json` declares its slots, artifact names, and capability suggestions; its `SETUP.json` records ten customer-specific checks. The [Website Growth guide](../../playbooks/agentic-engineering-platform/website-growth/website-growth-loop/references/team-and-handoffs.md), [Sales guide](sales_inbound_lead_to_meeting.md), and [Shopify operator guide](shopify_operator_category.md) provide detailed journeys. Existing Workflow Playbooks outside these seven are indexed in the [Playbooks README](../../playbooks/README.md); their presence does not make a planned Crew template installable.

## Route a customer request to an available Crew

This is the **current use-case map**, not a list of extra agents. Start with one relevant Crew for a chat request. Offer the named Automation Playbook when the customer wants an owned, measured journey across capabilities or a separate access/review boundary. A source export may support the first read-only result; the connected version needs a verified account and scope.

| Customer request or use case | Primary available Crew | First useful result | Multi-Crew route when needed |
| --- | --- | --- | --- |
| “Explain what changed in our SaaS revenue, spend, or cash.” | [Finance Analyst](crew_template_profiles.md#finance-analyst) | Sourced finance brief with calculations and unknowns | Finance Operations Review when billing exceptions need a separate owner |
| “Which invoices, failed payments, refunds, or disputes need attention?” | [Billing Operations Coordinator](crew_template_profiles.md#billing-operations-coordinator) | Dated exception queue and drafts for review | Finance Operations Review |
| “Reconcile subscription billing to our close.” | [Revenue & Close Analyst](crew_template_profiles.md#revenue--close-analyst) | Close checklist and mismatch memo | Optional specialist in Finance Operations Review; no close handoff is packaged yet |
| “Review bills and company expenses before approval.” | [Spend & Payables Coordinator](crew_template_profiles.md#spend--payables-coordinator) | Source-linked payables queue | Optional specialist in Finance Operations Review; no payables handoff is packaged yet |
| “Prepare transaction records for our tax professional.” | [Tax Export Preparer](crew_template_profiles.md#tax-export-preparer) | Reconciled export and exception list | Usually a capability on Finance Analyst; no tax filing Automation is packaged |
| “We launched a site; what should we fix first to attract relevant visitors?” | [Website Growth Starter](crew_template_profiles.md#website-growth-starter) | Source-linked audit and 30-day priority brief | Website Growth Loop |
| “Which technical SEO issues are observable on our site?” | [SEO Analyst](crew_template_profiles.md#seo-analyst) | Page-level issue list and retests | Optional technical SEO slot in Website Growth Loop |
| “Which buyer questions lack a useful page?” | [Search Opportunity Mapper](crew_template_profiles.md#search-opportunity-mapper) | Buyer-question-to-page map | Required search slot in Website Growth Loop |
| “Prepare a brief for this approved page opportunity.” | [Content Brief Writer](crew_template_profiles.md#content-brief-writer) | Reviewable page brief | Optional content slot in Website Growth Loop |
| “Draft the approved page for review.” | [Content Page Builder](crew_template_profiles.md#content-page-builder) | Sourced page draft and next action | Optional page slot; publication is a separate reviewed step |
| “Which existing pages can improve from Search Console evidence?” | [Search Console Optimizer](crew_template_profiles.md#search-console-optimizer) | Sourced page/query recommendations | A standalone Crew task unless Builder designs a compatible handoff |
| “What changed in traffic and useful visitor actions?” | [Traffic & Engagement Analyst](crew_template_profiles.md#traffic--engagement-analyst) | Sourced readout with a next action | Optional measurement slot in Website Growth Loop |
| “Where do answer engines cite us or competitors?” | [AI Visibility Analyst](crew_template_profiles.md#ai-visibility-analyst) | Question-level observations and citation gaps | A standalone Crew task or a separately designed AI Visibility workflow |
| “Why is this landing page not converting?” | [Landing Page Optimizer](crew_template_profiles.md#landing-page-optimizer) | Page diagnosis and testable improvement | A standalone Crew task or reviewed experiment workflow |
| “How should we distribute this published asset?” | [Content Distribution Coordinator](crew_template_profiles.md#content-distribution-coordinator) | Channel plan and reviewed drafts | A standalone Crew task; sending needs its own approved route |
| “Which inbound enquiries fit our customer criteria?” | [Lead Intake & Qualifier](crew_template_profiles.md#lead-intake--qualifier) | Deduplicated, source-linked qualification brief | Inbound Lead-to-Meeting Review |
| “What verified company context will help this seller?” | [Account Researcher](crew_template_profiles.md#account-researcher) | Dated facts, labeled hypotheses, seller questions | Optional research slot in Inbound Lead-to-Meeting Review |
| “Prepare and track a reply offering a meeting.” | [Sales Follow-up Coordinator](crew_template_profiles.md#sales-follow-up-coordinator) | Unsent, cited booking offer with owner decision | Inbound Lead-to-Meeting Review; send and booking require real receipts |
| “Turn this signed customer handoff into an onboarding plan.” | [Customer Onboarding Coordinator](crew_template_profiles.md#customer-onboarding-coordinator) | Owned milestone register with blockers | New Customer to First Value |
| “Did this customer achieve the agreed first result?” | [Product Adoption Analyst](crew_template_profiles.md#product-adoption-analyst) | Observed first-value status and source coverage | New Customer to First Value |
| “Which customer accounts need an owner decision?” | [Customer Health Coordinator](crew_template_profiles.md#customer-health-coordinator) | Sourced account health brief | Optional health slot in New Customer to First Value |
| “Investigate this incident across alerts and deploys.” | Incident Investigator | Sourced timeline, hypotheses, and owner decisions | Incident to Verified Recovery when a separate delivery owner must track a fix through release and service verification |
| “What is blocking this change from shipping?” | Engineering Delivery Coordinator | Joined issue, PR, CI, and deployment blocker ledger | Existing CI and Deployment Failure Triage Workflow can guide a Builder proposal |
| “Why did this route become slower?” | Performance Investigator | Comparable regression brief and retest plan | Existing Performance Engineering Workflows can guide a Builder proposal |
| “What changed in cloud spend?” | Cloud Cost Analyst | Reconciled cost-change brief and risk-checked candidates | Existing Cost Anomaly to Verified Savings Workflow can guide a Builder proposal |
| “Which orders are stuck between payment and fulfillment?” | [Store Operations Coordinator](crew_template_profiles.md#store-operations-coordinator-store-operations-coordinator) | Sourced order exception queue and owner action | Order Exception to Resolution when a return or refund request needs a separate policy owner |
| “Can we approve this return or refund request?” | [Returns & Refunds Coordinator](crew_template_profiles.md#returns--refunds-coordinator-returns-refunds-coordinator) | Policy and payment review with an unsent customer reply | Order Exception to Resolution when fulfillment facts must be verified first |
| “Which products and variants need merchant review?” | [Catalog & Merchandising Analyst](crew_template_profiles.md#catalog--merchandising-analyst-catalog-merchandising-analyst) | Product and variant issue queue with proposed edits and retests | Storefront Opportunity to Verified Change when a growth finding is involved |
| “Where is this store losing shoppers?” | [Shopify Growth Analyst](crew_template_profiles.md#shopify-growth-analyst-shopify-growth-analyst) | Store growth brief with evidence, limits, and measurement plan | Storefront Opportunity to Verified Change |

### How a template becomes a trustworthy detail page

The [first-party Crew metadata](../../frontend/src/products/work/crewTemplates.ts), [Website Growth specialists](../../frontend/src/products/work/websiteGrowthSpecialists.ts), [Sales specialists](../../frontend/src/products/work/salesSpecialists.ts), [Customer Success specialists](../../frontend/src/products/work/customerSuccessSpecialists.ts), [Engineering specialists](../../frontend/src/products/work/engineeringSpecialists.ts), and [Shopify specialists](../../frontend/src/products/work/shopifySpecialists.ts) are the current installable definitions. Each entry already provides a role, purpose, first result, minimum inputs, optional connections, example requests, a local skill, and a chat checklist. The Finance and Website Growth Starter skills are stored in [template files](../../frontend/src/products/work/templates/); the specialist skills and setup guides are generated from the typed definitions. These are implementation sources, not customer setup evidence.

Two journeys already have concrete **handoff examples** to use as the pattern for deeper agent detail:

| Reference journey | Fictional good output | Blocking example and review point | Second-run behavior |
| --- | --- | --- | --- |
| [Website Growth Loop](../../playbooks/agentic-engineering-platform/website-growth/website-growth-loop/references/team-and-handoffs.md) | [Priority brief](../../playbooks/agentic-engineering-platform/website-growth/website-growth-loop/examples/growth-priority-brief.json) → [buyer-question map](../../playbooks/agentic-engineering-platform/website-growth/website-growth-loop/examples/search-opportunity-list.json) | [Invalid map](../../playbooks/agentic-engineering-platform/website-growth/website-growth-loop/examples/invalid-search-opportunity-list.json) must stop the consumer. The owner chooses, defers, or rejects sourced actions before content or site work. | Read prior action IDs and owner decisions; inspect changed pages and new evidence first; report no new evidence when nothing changed. See [action and measurement](../../playbooks/agentic-engineering-platform/website-growth/website-growth-loop/references/action-and-measurement.md). |
| [Inbound Lead-to-Meeting Review](sales_inbound_lead_to_meeting.md) | [Qualification brief](../../playbooks/agentic-engineering-platform/sales/inbound-lead-to-meeting-review/examples/lead-qualification-brief.json) → [unsent follow-up](../../playbooks/agentic-engineering-platform/sales/inbound-lead-to-meeting-review/examples/sales-followup-draft.json) | [Invalid qualification](../../playbooks/agentic-engineering-platform/sales/inbound-lead-to-meeting-review/examples/invalid-lead-qualification-brief.json) must stop follow-up. The owner reviews exact recipient, offer, contact policy, and booking link; a [delivery receipt](../../playbooks/agentic-engineering-platform/sales/inbound-lead-to-meeting-review/examples/sales-delivery-receipt.json) is separate from a [booked outcome](../../playbooks/agentic-engineering-platform/sales/inbound-lead-to-meeting-review/examples/sales-meeting-outcome.json). | Re-read lead, suppression, prior contact, replies, and booking state before another touch; retain one stable action ID. See [team and handoffs](../../playbooks/agentic-engineering-platform/sales/inbound-lead-to-meeting-review/references/team-and-handoffs.md). |

These fixtures demonstrate fields and handoff shape, not a real customer result. Before promoting **each individual Crew** as a fully demonstrated public example, its own detail must also include one complete fictional input/output pair, an inadequate output with a reason it fails, the exact source or connection probe for its hard task, the owner review point, a second-run rule, and at least one exercised customer-like case. The [content quality review](../reviews/playbook_template_content_quality_2026-09-25.md) tracks gaps in the Website Growth specialists; the [setup quality review](../reviews/crew_playbook_setup_quality_review_2026-09-25.md) tracks remaining runtime readiness gaps. The seven packaged Playbooks have contract examples and validators for their selected handoffs; those fictional fixtures do not prove a customer's integration or business outcome.

## Product model

A **Crew template** currently provides a reusable capability pack: a local skill, starter instructions, example requests, expected outputs, and a setup checklist. A new Crew may start from one template to seed its identity. An existing Crew can add more packs without changing its role or purpose. Each pack keeps its own setup progress. The target model is a [unified Playbook catalog](unified_agent_automation_playbooks.md): an **Agent Playbook** proposes one primary agent for a Crew, while an **Automation Playbook** proposes a multi-agent team and Workflow plan. Selecting a Playbook opens a setup draft in chat; the Builder inspects what exists, proposes concrete changes, and applies them through authorized tools after review. Supporting packs remain capabilities of one Crew, not additional agent identities. The Automation owns the recurring goal and handoffs; selecting either Playbook does not activate a schedule or run.

Each template must work as a useful interactive Crew after the user provides its minimum inputs. Connected accounts, schedules, outbound messages, payments, production changes, and other consequential actions require explicit setup and the product's normal permissions and approvals. Never prefill a customer's target metric with an illustrative website number.

The catalog tracks eleven first-class browse categories and the Website Growth subcategory described above. Several related jobs should become capabilities of one Crew rather than separate Crew identities. Existing Workflow playbooks may inform a template or its suggested Automation, but they are not Crew templates.

The creation picker is designed for a larger installed catalog: keep Blank Crew separate from scrolling results; search across template names, categories, purposes, and first outputs; show category counts and the result count; reveal results in batches; and preserve the chosen template while filters change. On phones, browsing and Crew details are separate views. Only implemented templates appear in the picker—planned catalog entries are not offered for installation.

## Finance

The [B2B SaaS finance role and tool map](../research/b2b_saas_finance_roles_and_tools.md) identifies eight job families: subscription billing and receivables; payments and recovery; revenue accounting and close; payables, procurement and spend; planning and SaaS performance; cash and treasury; tax; and payroll. These are catalog filters and possible roles, not eight Crews every customer must install. Tool suggestions depend on the customer's actual billing, accounting, and regional stack.

Start a small business with **two default Crew identities**: Finance Analyst owns read-oriented analysis, processor account checks, and planning; Billing Operations Coordinator owns customer-facing invoice follow-up, failed-payment investigation, and refund-request preparation. Add Revenue & Close Analyst or Spend & Payables Coordinator when a separate accounting or payment owner needs a distinct setup and access scope. Payroll Review Coordinator, Tax Compliance Coordinator, and Cash & Treasury Analyst are later specialist candidates. Tax Export Preparer is already available as an installable supporting pack for Finance Analyst; create a separate tax Crew only when a different owner, access scope, or review boundary requires it.

**Later role candidates, not installable:** Payroll Review Coordinator for payroll exceptions and approvals; Tax Compliance Coordinator for jurisdiction-specific obligations and professional review; Cash & Treasury Analyst for liquidity and cash-position review. These are distinct owners only when a Finance Analyst capability would be insufficient for access or accountability.

The **Finance Operations Review** Automation Playbook is an installable chat proposal. Builder can reuse or create distinct Billing Operations Coordinator and Finance Analyst Crews, then plan a manual billing exception queue → validated handoff → finance impact readout. Its ten setup checks track actual source access, owner decisions, Crew binding, validators, and a real first run. The packaged fixtures demonstrate the contract; they do not count as a customer's first run. Optional close and payables roles can be proposed, but their automated handoffs are outside this first contract. No recurrence, customer message, refund, or accounting write is activated by installation.

| Crew template | Job and first useful output | Minimum user input | Optional recurring work |
| --- | --- | --- | --- |
| **Finance Analyst** — available v1 | Explain revenue, expense, cash, SaaS metrics, and reconciliation changes in a sourced finance brief. Add optional processor checks, forecasting, expense review, and tax export capabilities within this Crew. | Authorized records, period, currency, metric definitions; further inputs only for selected capabilities. | A weekly brief or processor account check can be this Crew's schedule. A broader review Automation is useful when distinct owners or Crews must coordinate. |
| **Billing Operations Coordinator** — available v1 | Review overdue invoices, failed payments, refund requests, and disputes; produce an exception queue and customer-safe drafts for approval. | Invoice/payment/refund records, billing and refund policies, contact rules, routing owner. | Billing Review, if a recurring goal and review policy are wanted. |
| **Revenue & Close Analyst** — available v1 | Reconcile subscription invoices, credits, cash, and ledger records into a close memo with source-linked exceptions. | Fiscal period, entity, accounting policy, billing and ledger records or exports. | Period Close Review when another Crew must review exceptions or forecast impact. |
| **Spend & Payables Coordinator** — available v1 | Review vendor bills and expenses for due dates, duplicates, missing evidence, and approval gaps. | Bill or expense records, period, currency, approval policy. | Payables Review when recurring intake or another Crew handoff is needed. |

| Capability pack | Installed in | First result and boundary |
| --- | --- | --- |
| **Revenue Reconciliation** — Finance Analyst procedure v1 | Finance Analyst | Match orders, invoices, payments, and refunds with a source-linked mismatch list. Read-only until the owner separately authorizes a correction. |
| **Processor Account Checks** — Finance Analyst procedure v1 | Finance Analyst | Read authorized Stripe or Paddle balances, transactions, payouts, fees, refunds, and disputes for a dated exception report; reconcile payout entries to deposits when bank evidence exists. An export works without a live connector. |
| **Expense Review** — Finance Analyst procedure v1 | Finance Analyst; Spend & Payables Coordinator when payment access differs | Categorize expenses under the owner's chart of accounts and approval rules. No automatic approval or payment. |
| **Cash Flow Planning** — Finance Analyst procedure v1 | Finance Analyst | Forecast from dated source balances, receivable/payable timing, and scenarios with stated uncertainty; do not present it as a verified balance. |
| **Tax Export Preparer** — available v1 | Finance Analyst by default; separate Crew when access requires | Reconciled transaction export and exception list for review by the owner and tax professional. No automatic tax classification, filing, or delivery. |
| **Invoice Chasing** — planned | Billing Operations Coordinator | Ranked overdue-invoice queue with invoice ID, open amount, due date, payment status, prior contact, and a proposed follow-up. Avoid duplicate or premature reminders; messages remain drafts until reviewed. |
| **Failed Payment Recovery** — planned | Billing Operations Coordinator | Failed-payment investigation and policy-compliant next steps. No charge retry or customer message without a separately authorized route. |
| **Refund Review** — planned | Billing Operations Coordinator | Queue customer refund requests with payment ID, original and previously refunded amounts, remaining refundable amount, currency, reason, policy, approver, and current Stripe status. Propose approval or escalation; never issue a refund merely because the pack is installed. |
| **Dispute Review** — planned | Billing Operations Coordinator | Record dispute ID, deadlines, evidence needed, and responsible reviewer. Do not submit evidence or contact a customer without the approved process. |

The Billing Operations Coordinator v1 skill can inspect these case types from authorized records and produce one review queue. The planned rows above are separately installable, deeper capability packs; no provider-specific write action is bundled with v1.

**Stripe check setup:** choose the exact Stripe account, live/test mode, currencies, timezone, reporting window, and read scope. Verify one accessible balance transaction and payout, reconcile gross, fees, refunds, and net using their source IDs, and record expected payout timing or a mismatch. Stripe's pending balance is not a bank deposit. Do not call a missing payout a loss before its expected arrival. Save the last inspected cursor or period and deduplicate webhook events by event ID. An authorized `invoice.payment_failed` or `payout.failed` webhook can open an exception, but first validate its signature, account, object ID, and idempotency before a Crew acts on it. See [Stripe balance transactions](https://docs.stripe.com/api/balance_transactions) and [Stripe events](https://docs.stripe.com/api/events/types).

**Refund boundary:** the default pack reads and prepares a reviewed refund decision. An actual refund is a separate action with narrow Stripe write access, owner approval of the exact payment and amount, an idempotency key, and a saved Stripe result/receipt. Check prior partial refunds and the remaining refundable amount first; Stripe supports partial refunds and rejects amounts beyond the remaining charge. See [Stripe refunds](https://docs.stripe.com/api/refunds/create).

Tax Export keeps its own setup checklist, so a Finance Analyst can be ready for a sourced brief while tax export remains pending. The analyst's processor, SaaS metric, reconciliation, and cash procedures require source and definition checks for each actual request; the basic Crew checklist does not certify them all. Builder should inspect the existing Finance Analyst Crew before proposing a new one and create another only when the permission, owner, cadence, or independent-review boundary makes sharing inappropriate. Adding a pack never imports another Crew's connections or activates a schedule, function, trigger, refund, payment action, or delivery channel.

## Customer Success

For a B2B SaaS company, the first useful post-sale journey is **new customer to first value**. Start with Customer Onboarding Coordinator and Product Adoption Analyst. Add Customer Health Coordinator when a distinct account owner needs a broader review of adoption, support and renewal context. These are reusable capabilities; one Crew may carry several when access and ownership are compatible.

The **New Customer to First Value** Automation Playbook is an installable chat proposal. Builder inspects the signed customer handoff, purchased scope, owner, onboarding tracker, product event source, and the customer's actual first-value definition. It proposes an owned milestone register → validated adoption readout → optional health review. Its ten setup checks require real source access, identity mapping, evidence rules, Crew bindings, validators, an owner-reviewed plan, and a real manual run. Selection does not contact customers, update accounts, or enable recurrence. The [Customer Success guide](customer_success_first_value.md) defines the handoffs and evidence boundaries.

| Crew template | Job and first useful output | Minimum user input | Suggested Automation |
| --- | --- | --- | --- |
| **Customer Onboarding Coordinator** — available v1 | Turn an authorized handoff into an owned milestone register with evidence and blockers. | Customer account, purchased scope, first-value goal, owner, target date, authorized handoff. | New Customer to First Value |
| **Product Adoption Analyst** — available v1 | Verify the agreed first-value event from product records and expose instrumentation gaps. | Account, event definition, observation window, authorized usage or setup export. | New Customer to First Value |
| **Customer Health Coordinator** — available v1, optional specialist | Review adoption, support and renewal signals in a sourced account brief. | Account, health rules, owner, first-value readout and authorized context. | Optional health slot in New Customer to First Value |

## Customer Support

All four rows in this section are planned use cases. A support case is the unit of work; the Customer Success category owns account-level onboarding and adoption. Escalations may pass a bounded case summary to the account owner, but sharing a support inbox does not merge the roles.

| Crew template | Job and first useful output | Minimum user input | Suggested Automation |
| --- | --- | --- | --- |
| **Support Triage Assistant** — planned | Classify incoming cases, identify urgency, and propose an owner and next action. | Case examples, priority rules, support channels. | Inbox Triage |
| **Support Reply Drafter** — planned | Draft grounded replies from approved help content and show citations or source links. | Help docs, tone guide, escalation rules. | Support First Response |
| **Escalation Coordinator** — planned | Keep a customer escalation brief current and coordinate human handoffs. | Escalation policy, case history, responsible team. | Escalation Watch |
| **Feedback & Review Analyst** — planned | Cluster feedback and reviews into themes, draft responses, and flag urgent issues. | Review or feedback export, response policy, product context. | Review Responder |

## Sales

For a B2B SaaS company receiving enquiries from a new website, start with **Lead Intake & Qualifier** and **Sales Follow-up Coordinator**. Use **Account Researcher** when sourced company context will improve the seller's response or call preparation. These are reusable Crew templates, not a requirement to create three separate Crews for every company. If one owner can safely handle the entire job in one Crew, use chat or a Crew schedule. A distinct owner or access boundary justifies a multi-Crew route.

The **Inbound Lead-to-Meeting Review** Automation Playbook is an installable chat proposal. Builder inspects existing Crews, lead sources, booking route and connections, then proposes qualification → validated handoff → owner-reviewed booking offer. Optional account research has its own validated handoffs. Its ten checks require a real inbound source, fit and contact policy, current prior-contact state, Crew binding, validator steps, a reviewed plan, and a real manual first run. A separate approved action may send through a verified provider and save a delivery receipt; a calendar or CRM event is required to record a booking. The example artifacts are fictional contract samples. Instant on-page booking requires a separate website integration.

| Crew template | Job and first useful output | Minimum user input | Suggested Automation |
| --- | --- | --- | --- |
| **Lead Intake & Qualifier** — available v1 | Deduplicate and assess an inbound request against owner-defined fit rules; return a sourced lead brief and next owner decision. | Inbound enquiry/export, ideal-customer criteria, routing owner and contact policy. | Inbound Lead-to-Meeting Review |
| **Account Researcher** — available v1, optional specialist | Prepare verified company context, labeled hypotheses and seller questions without assuming purchase intent. | Company/domain, approved research scope and offer. | Optional research slot in Inbound Lead-to-Meeting Review |
| **Sales Follow-up Coordinator** — available v2 | Prepare a cited booking offer, then track an approved provider send and observed booking when the customer connects those routes. | Validated lead brief, approved offer, voice/contact policy, owner, booking URL and outcome source. | Inbound Lead-to-Meeting Review |
| **Sales Call Briefing Assistant** — planned | Assemble account context, likely questions, and a meeting brief. | Meeting details, CRM notes or files, product material. | Pre-meeting Brief |
| **Proposal Drafter** — planned | Turn discovery notes into a scoped proposal draft with open questions and evidence. | Discovery notes, pricing rules, approved proposal format. | Proposal Preparation |
| **Pipeline Analyst** — planned | Explain pipeline movement, stale deals, and forecast risks with record links. | Pipeline export or authorized CRM, stage definitions, reporting period. | Pipeline Health Review |

Use the customer's CRM or form export for a first read-only result. HubSpot and Salesforce are relevant provider examples, not connected accounts by default; an email or calendar connection is needed for approved delivery and outcome tracking. The [Sales implementation guide](sales_inbound_lead_to_meeting.md) records the handoff, validation, and setup boundaries.

## Website Growth

For a new company with a recently launched site, start with a result that can be produced from the public website and owner context. Add Search Console and analytics when available; a new site may not have enough history to support trend claims. One Crew can install several packs, while the Website Growth Loop Workflow Playbook guides Builder through a separate multi-Crew Automation proposal. The ten specialist Crew templates below are available; the Automation Playbook guides setup in chat and does not start recurrence when selected.

| Crew template | Job and first useful output | Minimum user input | Suggested Automation |
| --- | --- | --- | --- |
| **Website Growth Starter** | Audit the public site and produce a source-linked 30-day plan for attracting relevant visitors. **Available v1.** | Site URL or page export, offer, target audience, primary visitor action. | Website Growth Loop |
| **SEO Analyst** | Prioritize crawl, indexability, internal-link, and on-page issues with page-level evidence. **Available v1.** | Site, approved crawl scope, Search Console if available. | SEO Intelligence |
| **Search Opportunity Mapper** | Map buyer questions and search intent to existing pages and a ranked content gap list. **Available v1.** | Offer, audience, market, site pages, optional search data. | Search Opportunity Review |
| **Content Brief Writer** | Create a sourced page brief with audience, angle, claims to verify, and success signal. **Available v1.** | Topic, buyer need, brand guide, source material. | Content Brief Queue |
| **Content Page Builder** | Draft a useful, reviewable page with sources, internal links, and a clear next action. **Available v1.** | Approved brief, existing site content, publishing format. | Content Page Queue |
| **Search Console Optimizer** | Find pages with measurable query opportunities and propose natural title, copy, or link improvements. **Available v1.** | Authorized Search Console property or export with page/query data. | Search Query Review |
| **Traffic & Engagement Analyst** | Explain landing-page, source, and conversion changes with a prioritized action list. **Available v1.** | Analytics export or connection, event definitions, comparison window. | Website Traffic Review |
| **AI Visibility Analyst** | Test buyer questions across chosen answer engines and summarize citation gaps. **Available v1.** | Brand, competitors, buyer questions, measurement method. | AI Visibility Intelligence |
| **Landing Page Optimizer** | Audit one conversion path and prepare a bounded page test or revision for review. **Available v1.** | Page URL, visitor intent, target action, available conversion data. | Landing Page Experiment Review |
| **Content Distribution Coordinator** | Find relevant channels and prepare a reviewable distribution plan and outreach drafts. **Available v1.** | Published asset, audience, approved channels, contact policy. | Content Distribution Review |

Suggested sequence: **audit and baseline → choose audience/search opportunities → improve or create pages → distribute → measure and repeat**. The first Website Growth Brief must not claim a ranking or traffic increase that has not been observed. Installing a template never grants Search Console, analytics, CMS, repository, email, or outreach access.

## Marketing & Growth

This is Marketing outside the Website Growth subcategory. All three Crew rows below are planned. The existing Growth Analytics Workflow Playbooks in the [Playbooks README](../../playbooks/README.md) can guide separate workflow setup; they are not these Crew templates.

| Crew template | Job and first useful output | Minimum user input | Suggested Automation |
| --- | --- | --- | --- |
| **Competitor Intelligence Analyst** — planned | Track meaningful changes in competitor positioning, pricing, and launches with sources. | Competitor list, watch topics, alert threshold. | Competitor Watch |
| **Campaign Performance Analyst** — planned | Explain campaign results, anomalies, and the next test to run. | Campaign data, spend, conversion definitions, reporting window. | Campaign Review |
| **Growth Experiment Planner** — planned | Turn a growth hypothesis into a bounded experiment plan and decision rule. | Baseline, target audience, metric, constraints. | Growth Experiment Review |

## Operations

All six Crew rows below are planned. Keep these as separate Crew identities only when the owner, source access, or recurring decision differs; meeting actions and project status may share one operations Crew for a small team.

| Crew template | Job and first useful output | Minimum user input | Suggested Automation |
| --- | --- | --- | --- |
| **Chief of Staff** — planned | Synthesize priorities, decisions, blockers, and follow-ups into an operator brief. | Team goals, current notes, owners, reporting cadence. | Weekly Business Review |
| **Meeting Actions Coordinator** — planned | Extract decisions and action items with owners, due dates, and source references. | Notes or transcript, owner list, task conventions. | Meeting Notes to Actions |
| **Project Status Reporter** — planned | Summarize progress, risks, and requests for decisions across project records. | Project updates, milestone definitions, stakeholders. | Project Status Digest |
| **Order Operations Coordinator** — planned | Investigate stuck orders and prepare safe next actions and customer updates. | Order data, fulfillment policy, exception thresholds. | Order Watchdog |
| **Vendor Researcher** — planned | Compare vendors against requirements and produce an evidence-linked shortlist. | Requirements, budget, security constraints, decision owner. | Vendor Review |
| **Document Intake Assistant** — planned | Extract and check structured facts from incoming documents, flagging uncertain fields. | Sample documents, required fields, validation rules. | Document Intake Queue |

## Engineering

The four primary Crew rows below are locally installable with skills and pending nine-check chat setup. The separate Incident to Verified Recovery Automation is an installable Builder proposal that can reuse or create Incident Investigator and Engineering Delivery Coordinator Crews. Existing engineering Workflow Playbooks cover related jobs in the Workflow Builder, but they do not install these Crew identities. QA owns test and release-gate evidence; Security owns application risk and remediation evidence. Engineering owns the investigation and follow-through that connects monitoring, issue tracking, repositories, CI, deployments, and cloud billing. Generic PR review is not a priority Crew template.

| Crew template | Job and first useful output | Minimum user input | Suggested Automation |
| --- | --- | --- | --- |
| **Incident Investigator** — available v1 | Correlate alerts, logs, and deploys into a sourced timeline, labeled hypotheses, and an owner action queue. | Incident scope, authorized telemetry, escalation policy. | Incident to Verified Recovery Automation; Incident Investigation and Coordination Workflow also exists. |
| **Engineering Delivery Coordinator** — available v1 | Trace a blocked change from issue to PR, CI, deployment, and owner; return a sourced blocker ledger with the next action and evidence of release status. | Issue tracker, repository and CI read access, deployment source, ownership and release policy. | Incident to Verified Recovery Automation; CI and Deployment Failure Triage Workflow also exists. |
| **Performance Investigator** — available v1 | Analyze latency or page performance regressions and produce a reproducible diagnosis. | Targets, traces or test runs, performance budgets. | Browser and API Performance Validation Workflows exist. |
| **Cloud Cost Analyst** — available v1 | Explain cost changes and propose evidence-backed savings with service-risk checks. | Billing data, ownership map, budget, change policy. | Cost Anomaly to Verified Savings Workflow exists. |

## QA

QA is a first-class browse category, even though its existing Workflow packages live under `browser-qa/` inside the Agentic Engineering Platform. It covers functional journeys, regression, permissions as experienced by users, and release gates. A security finding discovered during QA should hand off to Security without claiming it was remediated.

The Crew uses the customer's test runner, CI, issue tracker, browser, and release records to decide what failed, who should act, and whether a retest supports release. A testing SaaS may produce the test result; the Crew owns investigation and the reviewed decision.

| Crew template | Job and first useful output | Minimum user input | Existing Workflow Playbook or proposed Automation |
| --- | --- | --- | --- |
| **Browser Journey QA Analyst** — planned | Run an approved user journey and return exact pass/fail evidence with screenshots, console/network errors, and reproduction steps. | Approved journey, environment, test account, expected result. | [Critical Journey Validation](../../playbooks/agentic-engineering-platform/browser-qa/critical-journey-validation/SKILL.md) exists as a Workflow Playbook. |
| **Flaky Test Investigator** — planned | Distinguish intermittent product failures from test instability and propose a verified fix or owner action. | Test history, exact build, traces, retry policy. | [Flaky-Test Detection and Stabilization](../../playbooks/agentic-engineering-platform/browser-qa/flaky-test-detection-stabilization/SKILL.md) exists as a Workflow Playbook. |
| **Release Quality Assistant** — planned | Inspect a release candidate and summarize tests, failures, evidence, and a proposed gate decision. | Repository or build, test policy, release scope. | [Release and PR Quality Gate](../../playbooks/agentic-engineering-platform/browser-qa/release-pr-quality-gate/SKILL.md) exists as a Workflow Playbook. |

## Security

Security is a first-class browse category for authorized risk work. It begins with a bounded assessment, triage, reviewed remediation, and retest. Role and Permission Validation is also discoverable from Security, but remains one Workflow Playbook with its canonical `browser-qa` package ID.

Security tools can supply scanner findings, dependency alerts, and access evidence. The Crew validates relevance to the customer's asset, routes the finding to an owner, follows the approved fix, and closes it only with retest evidence.

| Crew template | Job and first useful output | Minimum user input | Existing Workflow Playbook or proposed Automation |
| --- | --- | --- | --- |
| **Security Findings Analyst** — planned | Triage authorized findings and draft reviewed remediation steps with verification criteria. | Findings, asset scope, severity policy, code access. | [Application Security Assessment and Remediation](../../playbooks/agentic-engineering-platform/security-engineering/application-security/application-security-assessment-remediation/SKILL.md) exists as a Workflow Playbook. |
| **Access Review Analyst** — planned | Compare actual user, role, and tenant permissions to an approved policy; return evidence-backed exceptions. | Role matrix, test accounts, asset scope, decision owner. | [Role and Permission Validation](../../playbooks/agentic-engineering-platform/browser-qa/role-permission-validation/SKILL.md) exists as a Workflow Playbook. |
| **Security Remediation Coordinator** — planned | Track an approved finding through owner assignment, change review, deployed retest, and closure evidence. | Finding ID, approved fix, code/deploy evidence, retest rule. | The existing Application Security Playbook covers this Workflow journey; this Crew identity is not packaged. |

## GTM

GTM is a cross-functional browse category for a company taking an offer to market and turning demand into qualified pipeline. Marketing owns audience, message, content, and channels; Sales owns lead fit, contact, and meeting outcomes. The category groups compatible existing Crews under one buyer journey rather than copying their templates. Neither Website Growth Loop nor Inbound Lead-to-Meeting Review alone covers the full launch-to-pipeline lifecycle.

The Crew and Automation use the customer's research, enrichment, CRM, analytics, campaign, and sequencing products where available. The agent job is to connect the launch plan to approved campaigns, handle lead exceptions, and verify a pipeline result against source records.

| Crew template or capability | Job and first useful output | Minimum user input | Status and Automation |
| --- | --- | --- | --- |
| **Website Growth Starter**, **Search Opportunity Mapper**, **Content Distribution Coordinator** | Find buyer questions, improve the site plan, and prepare reviewed distribution. | Offer, audience, site, relevant buyer evidence, approved channels. | Available as separate Website Growth Crews; [Website Growth Loop](../../playbooks/agentic-engineering-platform/website-growth/website-growth-loop/SKILL.md) covers the first two-Crew route. |
| **Lead Intake & Qualifier**, **Account Researcher**, **Sales Follow-up Coordinator** | Review inbound interest, prepare context and an approved booking offer. | Lead source, ideal-customer criteria, contact policy, owner, booking route. | Available as Sales Crews; [Inbound Lead-to-Meeting Review](../../playbooks/agentic-engineering-platform/sales/inbound-lead-to-meeting-review/SKILL.md) covers qualification to reviewed follow-up. |
| **GTM Strategy Analyst** — planned | Turn the offer, ideal customer, positioning, channels, and goals into an evidence-linked launch brief. | Offer, customer evidence, market, owner, success metric. | New Crew candidate; no GTM Automation packaged. |
| **Launch Coordinator** — planned | Keep approved launch assets, owners, dates, dependencies, and first pipeline signals in one action ledger. | Launch plan, asset inventory, owners, approved channels, measurement sources. | New Crew candidate; no GTM Automation packaged. |

The proposed **Launch to Qualified Pipeline** Automation would connect a reviewed message and site plan → approved distribution → captured lead → qualification → reviewed follow-up → observed meeting or pipeline outcome. It needs stable campaign and lead IDs, attribution limits, a verified source and consent policy, and a manual first route. It is a **planned Playbook**, not an installable package.

## Shopify

Shopify is a store-specific browse category for ecommerce businesses using that platform. A generic Website Growth Crew can inspect a public storefront, but Shopify catalog, order, inventory, return, and revenue claims require an authorized store connection or export. Store writes and customer messages need a separate reviewed route. The four Crews below are locally installable with skills and pending nine-check chat setup. Both Shopify Playbooks are Builder proposals with ten pending checks; they do not activate a refund, customer message, catalog edit, or recurrence. The [operator category guide](shopify_operator_category.md) maps jobs, source probes, boundaries, and repeat work.

Store teams may already use Shopify plus support, returns, analytics, email, and fulfillment apps. A Crew works through the store team's queue across those systems: identify a specific product or order, apply store policy, prepare the next action, and verify the resulting state.

| Crew template | Job and first useful output | Minimum user input | Suggested Automation |
| --- | --- | --- | --- |
| **Store Operations Coordinator** — available v1 | Investigate stuck orders, fulfillment and inventory exceptions with source-linked owner actions. | Authorized Shopify order/fulfillment export or account, service policy, owner. | Order Exception to Resolution for a related return/refund case. |
| **Returns & Refunds Coordinator** — available v1 | Review return/refund requests against order, payment, delivery, and policy; prepare an unsent customer reply. | Store/order ID, return request, payment and delivery records, refund policy, approval owner. | Order Exception to Resolution. |
| **Catalog & Merchandising Analyst** — available v1 | Identify missing product facts, variant conflicts, out-of-stock presentation, and search/navigation gaps with exact IDs. | Store URL or product export, catalog rules, priority collection, owner. | Storefront Opportunity to Verified Change. |
| **Shopify Growth Analyst** — available v1 | Analyze storefront discovery, product-page quality, traffic and checkout-path evidence; propose bounded improvements. | Storefront URL, buyer, product scope, analytics or export for performance claims. | Storefront Opportunity to Verified Change. |

The existing **Order Operations Coordinator** proposal under Operations could later become a shared capability instead of a duplicate Shopify Crew. Keep one canonical ID and put a Shopify-specific pack on it only when store access, schemas, and policy genuinely differ. Website Growth Starter, Landing Page Optimizer, and Traffic & Engagement Analyst may also be discovered here for their generic public-storefront or analytics jobs; they do not inherit Shopify credentials or order access.

## Template definition required for implementation

Each catalog entry should eventually include:

1. Stable ID, category, name, one-sentence purpose, icon, and search terms.
2. Crew identity and starter instructions, including its scope and when to ask a human.
3. Two or three example requests and one inspectable example output, clearly marked as illustrative.
4. Required and optional MCP capabilities, skills, channels, input data, and a setup checklist that checks actual connection state.
5. Suggested Crew schedules, authenticated triggers, and typed functions, plus any separate goal-chasing Automations and metrics.
6. Version and update notes so existing customer Crews are not silently rewritten.

Every template must include a project-owned checklist of five to ten checks. The first checks verify that this Crew can perform the pack's job and that its skill is selected; later checks cover customer data, definitions, a tested first result, and decisions about optional delivery or recurring work. The Crew reads and updates that checklist through chat. The frontend only displays whether each pack's setup is pending or complete, based on its saved checklist. Optional connections are not required if the owner chooses a valid file-based or chat-only path.

Finance Analyst v1 declares nine setup checks in its copied `TEMPLATE_SETUP.json`. The chat header shows **Setup pending** or **Setup complete** and starts a setup conversation when clicked. The Crew reads the checklist and marks a check complete in the same file only after verifying it; optional decisions can be completed when the owner explicitly chooses not to configure them. The header never marks a chat request as completion on its own. Existing Finance Analyst Crews with the older progress-only file are migrated without losing completed check IDs.

No entry should be labeled **ready to use** on the public website until a person can create it from the catalog, complete its stated setup, and produce the example output using supported product capabilities.

## MCP, skill, and channel setup

The template must declare **capabilities**, not assume one provider. For example, Lead Qualifier needs a way to read lead records; HubSpot, another CRM MCP, or a customer-provided file may satisfy that requirement. A provider-specific template may recommend a provider, but its preview must say so.

| Template field | Meaning | Creation behavior |
| --- | --- | --- |
| Bundled project skill | The template's reusable procedure and checks, stored as a project-local `SKILL.md`. | Copy into the new Crew and select it for that Crew. Finance Analyst v1 does this. It contains no customer facts or credentials. |
| Additional skill requirements | Capabilities such as spreadsheet analysis or browser research that need an existing skill. | Resolve installed skills and show any missing ones. Review the source before installing an external skill. |
| Required MCP capabilities | The minimum tools needed for the promised connected behavior, including read or write access. | Show compatible connected servers, let the owner choose an account/server, and select it for this Crew after permission review. |
| Optional MCP capabilities | Tools that enhance the template but are not needed for its minimum output. | Offer them during setup; keep the Crew usable without them. |
| Channels | Slack and WhatsApp routes, or Gmail access, when the job needs messages. | Set up through their existing Crew controls. Gmail and bot routes are separate from MCP selection. |
| Secrets and folders | Named credentials or administrator-authorized file access needed for a particular setup. | Ask the owner to attach them explicitly. Never put values or existing customer paths in a template. |

The Crew chat shows each pack as **Setup pending** or **Setup complete**. Clicking **Set up in chat** asks the Crew to inspect that pack's checklist, verify what is already available, and finish the remaining checks conversationally. A pack is ready for a particular job only after its required capabilities are tested with the user's access. Optional capabilities do not block the core job when the owner explicitly chooses to skip them.

For example, **Finance Analyst** bundles its finance-analysis procedure. An uploaded statement is enough to produce its first brief. Spreadsheet read access can be selected if already connected; an accounting MCP is optional. Posting the brief to Slack requires a separately configured route and approval policy. The template never copies a previous user's connection, token, selected secret, or Slack channel.

Current Crew has separate MCP, Skills, Slack, WhatsApp, and Gmail controls in its Integrations view. Template creation should use the same project selections and connection rules rather than introduce a second credential store. The server must validate the chosen resources against the owner and the new project before recording them.

## Schedules, triggers, and functions

Templates should include reusable **definitions and setup suggestions** for these Crew capabilities. They should not start recurring work or expose a callable function merely because a user selected a template.

| Capability | Template contains | Setup and activation |
| --- | --- | --- |
| Crew schedule | Suggested name, one complete message for the Crew, cadence, timezone question, required connections, and expected output. | Owner chooses the actual cadence and timezone. Create paused, test once with the owner's data, then enable explicitly. A Crew schedule sends a message to the Crew conversation; it is not a goal-chasing Workflow run. |
| Authenticated trigger | Event purpose, one saved instruction, expected payload fields, authentication options, and example test payload. | Owner chooses the source and authentication. Create only during setup, show its one-time secret privately, test a delivery, then enable. No endpoint or credential is copied from a template author. |
| Crew function | Stable function name, description, typed input and result schemas, execution instructions, required skills/MCP capabilities, and an example call/result. | Show the contract for review. Register it only after the owner accepts it and required capabilities are ready, because other Crews and workflows can call it. Test schema validation and an authorized call. |
| Goal-chasing Automation | Suggested outcome, metric, and relationship to the Crew. | Create as a separate Automation through its own setup and approvals. Do not turn a Crew schedule or function into a Workflow by implication. |

For **Finance Analyst**, a template might suggest a Monday finance-brief schedule, a `new_statement` webhook trigger, and an `analyze_finances(period)` function returning a structured brief. These are suggestions until the owner chooses a data source, reviews access, supplies a real timezone or event source, and tests the output. The separate Weekly Business Report Automation can be offered when the customer wants a measured recurring goal.

The setup checklist should report these independently: **suggested**, **configured but paused**, **tested**, and **active**. Template updates must never silently change an active schedule, trigger, function contract, or goal-chasing Automation.
