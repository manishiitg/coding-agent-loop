# AgentWorks playbooks

Versioned, authorable skill packages for AgentWorks' workflow builder.

Playbooks provide concise outcome guidance, decision criteria, and proven patterns for small teams, including Website Growth. They do not override the user's requested process or require one fixed workflow graph. The builder starts with one understandable workflow, preserves explicit user choices, and adds a separate workflow only for incompatible access or lifecycle boundaries.

```text
AgentWorks
└── Agentic Engineering Platform
    ├── Browser QA
    │   ├── Basic Browser Setup
    │   ├── Authentication and Session Validation
    │   ├── Role and Permission Validation
    │   ├── Critical Journey Validation
    │   ├── Flaky-Test Detection and Stabilization
    │   ├── Browser Test Self-Healing
    │   ├── Scheduled Regression and Synthetic Monitoring
    │   └── Release and PR Quality Gate
    ├── Security Engineering
    │   └── Application Security
    │       └── Application Security Assessment and Remediation
    ├── Performance Engineering
    │   ├── Browser Performance Validation
    │   └── API Performance Validation
    ├── Engineering Operations Intelligence
    │   └── Engineering Operations Intelligence
    ├── Engineering
    │   └── Incident to Verified Recovery
    ├── QA
    │   └── Release Candidate to Reviewed Gate
    ├── Security
    │   └── Finding to Verified Remediation
    ├── FinOps
    │   └── Cost Anomaly to Verified Savings
    ├── Reliability Operations
    │   ├── CI and Deployment Failure Triage
    │   ├── Incident Investigation and Coordination
    │   ├── Governed Remediation and Recovery
    │   └── Post-Incident Review and Actions
    ├── Growth Analytics
    │   ├── Growth Data Foundation
    │   ├── Funnel and Conversion Intelligence
    │   ├── Activation and Retention Intelligence
    │   ├── Growth Experimentation and Follow-Through
    │   ├── SEO Intelligence
    │   └── AI Visibility Intelligence
    ├── Website Growth
    │   └── Website Growth Loop
    ├── Marketing
    │   └── Campaign Signal to Reviewed Experiment
    ├── Finance
    │   ├── Finance Operations Review
    │   ├── Invoice Intake to Reviewed Payable
    │   └── Refund Request to Reconciled Outcome
    ├── Sales
    │   ├── Inbound Lead-to-Meeting Review
    │   ├── Discovery to Reviewed Proposal
    │   └── Pipeline Health to Owned Action
    ├── Customer Success
    │   └── New Customer to First Value
    ├── Customer Support
    │   └── Support Case to Reviewed Resolution
    ├── Product
    │   └── Feedback Theme to Product Decision
    ├── Operations
    │   └── Meeting Decision to Owned Follow-through
    ├── GTM
    │   └── Launch to Qualified Pipeline
    └── Shopify
        ├── Order Exception to Resolution
        ├── Storefront Opportunity to Verified Change
        ├── Inventory Availability to Owner Action
        ├── Payment Exception to Order Decision
        ├── Product Launch Readiness to Go/No-Go
        ├── Checkout Signal to Reviewed Recovery
        └── Inventory Risk to Reviewed Replenishment
```

## Browse categories and package locations

The [Crew and use-case catalog](../docs/design/crew_template_catalog.md) treats **Engineering, QA, Security, GTM, Shopify, Customer Support, and Product** as first-class browse categories. Package directories retain their canonical IDs; a browse category can link to an existing package without copying it or installing a Crew.

| Browse category | Current Workflow Playbook coverage | Crew and gap status |
| --- | --- | --- |
| Marketing | Campaign Signal to Reviewed Experiment; Growth Analytics Workflows and Website Growth Loop are adjacent | Three Marketing Crews are locally installable with pending setup. Campaign Performance and Growth Experiment are required; Competitor context is optional. |
| Finance | Finance Operations Review; Invoice Intake to Reviewed Payable; Refund Request to Reconciled Outcome | Four core Finance roles, Tax Export, and four billing capability packs are locally installable. The packs can share one Billing Operations Coordinator Crew; each has independent chat setup. Invoice intake reuses Operations Document Intake Assistant. Refund review hands an exact decision to Revenue & Close; provider and ledger actions remain separately approved. |
| Sales | Inbound Lead-to-Meeting Review; Discovery to Reviewed Proposal; Pipeline Health to Owned Action | Seven Sales Crew templates are locally installable. The proposal route requires reviewed discovery and current pricing. The pipeline route pairs comparable-snapshot analysis with a fresh seller next-step decision. |
| Engineering | Incident to Verified Recovery; Engineering Operations Intelligence, Reliability Operations, Performance Engineering, FinOps | Four Engineering Crew roles and one multi-Crew Automation Playbook are locally installable with pending setup. |
| QA | Release Candidate to Reviewed Gate; Browser QA suite, including Critical Journey Validation, flaky-test work, and Release and PR Quality Gate | Three QA Crew roles and one multi-Crew Automation Playbook are locally installable with pending setup; existing single-workflow packages remain under `browser-qa/`. |
| Security | Finding to Verified Remediation; Application Security Assessment and Remediation; Role and Permission Validation can be discovered here too | Three Security Crew roles and one multi-Crew Automation Playbook are locally installable with pending setup; the role-permission package remains under `browser-qa/`. |
| GTM | Website Growth Loop and Inbound Lead-to-Meeting Review cover separate parts of the journey; Growth Analytics is adjacent | Two GTM Crews and the Launch to Qualified Pipeline Automation are locally installable with pending setup. Website Growth and Sales Crews are reused, not duplicated. |
| Customer Support | Support Case to Reviewed Resolution; Feedback Theme to Product Decision crosses into Product | Four Support Crews are locally installable with pending setup. Triage and Reply form the required support-case route; Escalation is optional. Feedback & Review Analyst also supplies a validated theme to Product. |
| Product | Feedback Theme to Product Decision | Product Feedback Coordinator is locally installable with pending setup. The route reuses Support's Feedback & Review Analyst, verifies current issue state and prepares an owner decision before any issue write or customer promise. |
| Operations | Meeting Decision to Owned Follow-through; Invoice Intake to Reviewed Payable crosses into Finance | Six Operations Crews are locally installable with pending setup. Meeting Actions and Project Status form the required route; Chief of Staff review is optional. Document Intake also supports the invoice route; order and vendor work can start as standalone Crew jobs. |
| Shopify | Order Exception to Resolution; Storefront Opportunity to Verified Change; Inventory Availability to Owner Action; Payment Exception to Order Decision; Product Launch Readiness to Go/No-Go; Checkout Signal to Reviewed Recovery; Inventory Risk to Reviewed Replenishment | Seven Shopify Crews and seven multi-Crew Automation Playbooks are locally installable with pending setup. Generic Website Growth Crews can support public-storefront work without store access. |

### Engineering Automation

| Playbook | Outcome |
| --- | --- |
| [Incident to Verified Recovery](agentic-engineering-platform/engineering/incident-to-verified-recovery/SKILL.md) | Propose an Incident Investigator → Engineering Delivery Coordinator route, validate the incident/change handoff, review any production action, and check service recovery from telemetry before closure. |

### QA Automation

| Playbook | Outcome |
| --- | --- |
| [Release Candidate to Reviewed Gate](agentic-engineering-platform/qa/release-candidate-to-reviewed-gate/SKILL.md) | Propose Browser Journey QA Analyst → Release Quality Assistant with optional Flaky Test Investigator; validate exact candidate and required-suite evidence, preserve failures, and publish a gate only through a separately approved provider route. |

### Security Automation

| Playbook | Outcome |
| --- | --- |
| [Finding to Verified Remediation](agentic-engineering-platform/security/finding-to-verified-remediation/SKILL.md) | Propose Security Findings Analyst → Security Remediation Coordinator for a written-scope finding; validate exact identity, separate merged from deployed, and require independent matching retest and owner decision before closure. |

### GTM Automation

| Playbook | Outcome |
| --- | --- |
| [Launch to Qualified Pipeline](agentic-engineering-platform/gtm/launch-to-qualified-pipeline/SKILL.md) | Propose a GTM Strategy Analyst → Launch Coordinator → Lead Intake & Qualifier → Sales Follow-up Coordinator route, validate launch and source joins, reuse the Sales qualification contract, and verify actual delivery and meeting outcomes separately. |

### Customer Support Automation

| Playbook | Outcome |
| --- | --- |
| [Support Case to Reviewed Resolution](agentic-engineering-platform/customer-support/support-case-to-reviewed-resolution/SKILL.md) | Propose Support Triage Assistant → Support Reply Drafter with optional Escalation Coordinator; validate exact case and thread handoffs, approve contact separately, and distinguish provider delivery from observed case resolution. |

### Product Automation

| Playbook | Outcome |
| --- | --- |
| [Feedback Theme to Product Decision](agentic-engineering-platform/product/feedback-theme-to-product-decision/SKILL.md) | Propose Feedback & Review Analyst → Product Feedback Coordinator; validate the bounded theme and source coverage, inspect current issues, and prepare a product owner decision. Issue changes and customer promises require separate approval and receipts. |

### Operations Automation

| Playbook | Outcome |
| --- | --- |
| [Meeting Decision to Owned Follow-through](agentic-engineering-platform/operations/meeting-decision-to-owned-follow-through/SKILL.md) | Propose Meeting Actions Coordinator → Project Status Reporter with optional Chief of Staff review; validate meeting revision and owner acceptance, re-read tracker status, and keep task writes separate. |

### Shopify Automation

| Playbook | Outcome |
| --- | --- |
| [Order Exception to Resolution](agentic-engineering-platform/shopify/order-exception-to-resolution/SKILL.md) | Propose a Store Operations Coordinator → Returns & Refunds Coordinator route for an order problem tied to a return or refund request; verify store/order identity, policy and money rules, then require owner approval and provider receipts for actions. |
| [Storefront Opportunity to Verified Change](agentic-engineering-platform/shopify/storefront-opportunity-to-verified-change/SKILL.md) | Propose a Shopify Growth Analyst → Catalog & Merchandising Analyst route for an observed shopper problem, validate the exact product/variant handoff, review a merchant edit, and verify the shipped change before measuring a comparable result. |
| [Inventory Availability to Owner Action](agentic-engineering-platform/shopify/inventory-availability-to-owner-action/SKILL.md) | Propose a Catalog & Merchandising Analyst → Store Operations Coordinator route for a variant/location stock or display mismatch; review the action and retest inventory and storefront state. |
| [Payment Exception to Order Decision](agentic-engineering-platform/shopify/payment-exception-to-order-decision/SKILL.md) | Propose a Payment Operations Investigator → Store Operations Coordinator route that distinguishes authorization from capture and gates fulfillment release on source evidence and owner approval. |
| [Product Launch Readiness to Go/No-Go](agentic-engineering-platform/shopify/product-launch-readiness-to-go-no-go/SKILL.md) | Propose a Catalog & Merchandising Analyst → Shopify Growth Analyst preflight for one product, market, and Publication; block on unresolved facts and verify any approved launch. |
| [Checkout Signal to Reviewed Recovery](agentic-engineering-platform/shopify/checkout-signal-to-reviewed-recovery/SKILL.md) | Propose a Shopify Growth Analyst → Checkout Recovery Coordinator route for one checkout and channel; block unsafe contact, keep the draft unsent, and verify later send and order states separately. |
| [Inventory Risk to Reviewed Replenishment](agentic-engineering-platform/shopify/inventory-risk-to-reviewed-replenishment/SKILL.md) | Propose a Catalog & Merchandising Analyst → Replenishment Planner route for one item, location and supplier; recompute order quantity and cost, then distinguish PO status from transfer receipt. |

### Browser QA

| Playbook | Outcome |
| --- | --- |
| [Basic Browser Setup](agentic-engineering-platform/browser-qa/basic-browser-setup/SKILL.md) | Configure browser access and Playwright, save verified locators, capture video plus console/network evidence, save an application profile, and establish reporting. |
| [Authentication and Session Validation](agentic-engineering-platform/browser-qa/authentication-session-validation/SKILL.md) | Validate approved login, logout, MFA, recovery, expiry, refresh, and invalid-session behavior. |
| [Role and Permission Validation](agentic-engineering-platform/browser-qa/role-permission-validation/SKILL.md) | Validate allowed and denied page, action, and data access across roles, ownership states, and tenants. |
| [Critical Journey Validation](agentic-engineering-platform/browser-qa/critical-journey-validation/SKILL.md) | Reuse that profile to run agreed journeys, investigate failures, and retain attempt-scoped results, video, and console/network evidence. |
| [Flaky-Test Detection and Stabilization](agentic-engineering-platform/browser-qa/flaky-test-detection-stabilization/SKILL.md) | Detect inconsistent outcomes, classify their cause, and verify reviewed stabilization without hiding failures. |
| [Browser Test Self-Healing](agentic-engineering-platform/browser-qa/browser-test-self-healing/SKILL.md) | Classify failures, verify test-only repairs with preserved diagnostics, obtain review, rerun canonical tests, and update knowledge/reporting. |
| [Scheduled Regression and Synthetic Monitoring](agentic-engineering-platform/browser-qa/scheduled-regression-synthetic-monitoring/SKILL.md) | Run proven routes on a schedule, retain comparable history, and notify on actionable changes. |
| [Release and PR Quality Gate](agentic-engineering-platform/browser-qa/release-pr-quality-gate/SKILL.md) | Bind exact changes/builds to required suites and publish an auditable pass, fail, or needs-review decision. |

### Security Engineering

| Playbook | Outcome |
| --- | --- |
| [Application Security Assessment and Remediation](agentic-engineering-platform/security-engineering/application-security/application-security-assessment-remediation/SKILL.md) | Run authorized browser, API, code, dependency, secret, and configuration assessment through reviewed remediation and deployed retesting. |

### Performance Engineering

| Playbook | Outcome |
| --- | --- |
| [Browser Performance Validation](agentic-engineering-platform/performance-engineering/browser-performance-validation/SKILL.md) | Measure approved pages and journeys against customer budgets using comparable samples and durable browser diagnostics. |
| [API Performance Validation](agentic-engineering-platform/performance-engineering/api-performance-validation/SKILL.md) | Measure approved API scenarios under bounded load against latency, throughput, error, and capacity policies. |

### Engineering Operations Intelligence

| Playbook | Outcome |
| --- | --- |
| [Engineering Operations Intelligence](agentic-engineering-platform/engineering-operations-intelligence/engineering-operations-intelligence/SKILL.md) | Connect authorized engineering data, calculate governed delivery/quality/reliability signals, and run evidence-backed reviews with tracked actions in one workflow. |

Engineering Operations Intelligence uses the shared [operations data model](agentic-engineering-platform/engineering-operations-intelligence/references/operations-data-model.md) for identity, lineage, metric definitions, and data-quality rules.

### FinOps

| Playbook | Outcome |
| --- | --- |
| [Cost Anomaly to Verified Savings](agentic-engineering-platform/finops/cost-anomaly-to-verified-savings/SKILL.md) | Detect and explain cloud-cost anomalies, prepare safe rightsizing IaC changes, obtain approval, and verify realized savings plus service health. |

### Reliability Operations

| Playbook | Outcome |
| --- | --- |
| [CI and Deployment Failure Triage](agentic-engineering-platform/reliability-operations/ci-deployment-failure-triage/SKILL.md) | Ingest CI/deployment failures, establish exact identity, classify them from evidence, and route safe rerun, escalation, or owner action. |
| [Incident Investigation and Coordination](agentic-engineering-platform/reliability-operations/incident-investigation-coordination/SKILL.md) | Correlate signals, establish impact and severity, maintain an evidence-backed timeline and hypotheses, and coordinate current status. |
| [Governed Remediation and Recovery](agentic-engineering-platform/reliability-operations/governed-remediation-recovery/SKILL.md) | Prepare, validate, approve, execute, and verify remediation or rollback through authorized control paths. |
| [Post-Incident Review and Actions](agentic-engineering-platform/reliability-operations/post-incident-review-actions/SKILL.md) | Produce a sourced review, create governed follow-up work, and verify improvements through completion. |

Reliability Operations shares a [reliability event and evidence contract](agentic-engineering-platform/reliability-operations/references/reliability-event-contract.md) plus [trigger, webhook, and Slack guidance](agentic-engineering-platform/reliability-operations/references/triggers-webhooks-and-slack.md). Authenticated webhooks start fixed saved routes; the Slack bot provides threaded investigation, status, and correlated human decisions backed by durable workflow records.

The default [error webhook to recovery](agentic-engineering-platform/reliability-operations/references/error-webhook-to-recovery.md) path connects an external reliability system to AgentWorks, validates and groups errors, performs basic RCA, selects an approved resolution or escalation path, verifies service recovery, and updates the dashboard, Slack thread, and authorized source system.

All Browser QA playbooks share an [AgentWorks plan and tool guide](agentic-engineering-platform/browser-qa/references/agentworks-plan-and-tools.md) and an [evidence capture contract](agentic-engineering-platform/browser-qa/references/evidence-capture.md). They define step/tool choices plus durable, redacted video, console/network, screenshot, and trace evidence.

### Growth Analytics

| Playbook | Outcome |
| --- | --- |
| [Growth Data Foundation](agentic-engineering-platform/growth-analytics/growth-data-foundation/SKILL.md) | Connect and normalize traffic, product, billing, and feedback data with durable customer identity, event quality, freshness, and provenance. |
| [Funnel and Conversion Intelligence](agentic-engineering-platform/growth-analytics/funnel-conversion-intelligence/SKILL.md) | Analyze signup-to-purchase funnels, detect conversion changes, and attribute them to segments, pages, devices, or sources with session evidence. |
| [Activation and Retention Intelligence](agentic-engineering-platform/growth-analytics/activation-retention-intelligence/SKILL.md) | Find success-predicting behaviors, explain cohort retention divergence, and measure feature adoption impact on retention and revenue. |
| [Growth Experimentation and Follow-Through](agentic-engineering-platform/growth-analytics/growth-experimentation-follow-through/SKILL.md) | Prioritize evidence-backed experiments, create tracked actions, and verify shipped changes against pre-registered KPI targets. |
| [SEO Intelligence](agentic-engineering-platform/growth-analytics/seo-intelligence/SKILL.md) | Find winnable keywords, diagnose technical SEO issues, close content gaps, and track rankings with page-level briefs. |
| [AI Visibility Intelligence](agentic-engineering-platform/growth-analytics/ai-visibility-intelligence/SKILL.md) | Track AI-assistant brand citations against competitors and close gaps with content and authority changes. |

Growth Analytics shares the [growth data model](agentic-engineering-platform/growth-analytics/references/growth-data-model.md) for identity, lineage, metric definitions, and data-quality rules.

### Website Growth

| Playbook | Outcome |
| --- | --- |
| [Website Growth Loop](agentic-engineering-platform/website-growth/website-growth-loop/SKILL.md) | Propose a multi-Crew path from site audit and buyer questions through optional approved content, page draft, verified publication, channel plan and traffic readout. Each selected handoff has an exact-ID validator; measurement follows a verified ship and comparable source window. |

The Website Growth Loop is installed as guidance and a saved ten-check setup file in a Workflow. Builder chat inspects existing Crews, proposes a concrete team, records check evidence, and uses the `create_crew` template option to set up missing specialists after review. The eleven Website Growth agent templates live in the Crew catalog; selecting this Workflow Playbook alone creates no Crew or recurring run.

### Finance

| Playbook | Outcome |
| --- | --- |
| [Finance Operations Review](agentic-engineering-platform/finance/finance-operations-review/SKILL.md) | Propose a Billing Operations Coordinator → Finance Analyst review of subscription billing exceptions and source-linked financial impact. Close and payables specialists are optional; their handoffs are not part of the first packaged route. |
| [Invoice Intake to Reviewed Payable](agentic-engineering-platform/finance/invoice-intake-to-reviewed-payable/SKILL.md) | Propose a Document Intake Assistant → Spend & Payables Coordinator route. Validate invoice fields and page spans, re-read current AP records for duplicates and payment state, and stop at an owner-reviewed payable decision. Bill writes and payment need separate authorization and receipts. |
| [Refund Request to Reconciled Outcome](agentic-engineering-platform/finance/refund-request-to-reconciled-outcome/SKILL.md) | Propose Billing Operations Coordinator with Refund Review → Revenue & Close Analyst. Validate the exact payment and remaining amount, record a reviewed decision, and distinguish unprocessed, provider-processed, and ledger-reconciled states. Refund execution stays a separate approved route. |

### Marketing

| Playbook | Outcome |
| --- | --- |
| [Campaign Signal to Reviewed Experiment](agentic-engineering-platform/marketing/campaign-signal-to-reviewed-experiment/SKILL.md) | Propose Campaign Performance Analyst → Growth Experiment Planner, with optional sourced competitor context. Validate matched campaign and CRM metrics, baseline arithmetic, and a bounded plan; a launched variant requires a separate approved route. |

### Sales

| Playbook | Outcome |
| --- | --- |
| [Inbound Lead-to-Meeting Review](agentic-engineering-platform/sales/inbound-lead-to-meeting-review/SKILL.md) | Propose a Lead Intake & Qualifier → Sales Follow-up Coordinator route with optional account research, a reviewed booking offer, and delivery or booking status only when provider evidence exists. |
| [Discovery to Reviewed Proposal](agentic-engineering-platform/sales/discovery-to-reviewed-proposal/SKILL.md) | Propose Sales Call Briefing Assistant → Proposal Drafter with optional account research. Validate exact meeting and opportunity identity, require approved post-call discovery and current pricing, and stop at an unsent owner-reviewed draft. |
| [Pipeline Health to Owned Action](agentic-engineering-platform/sales/pipeline-health-to-owned-action/SKILL.md) | Propose Pipeline Analyst → Deal Follow-through Coordinator for one stale opportunity. Validate comparable snapshots and current CRM/contact state, then require seller review and separate approval for any CRM or contact action. |

### Customer Success

| Playbook | Outcome |
| --- | --- |
| [New Customer to First Value](agentic-engineering-platform/customer-success/new-customer-to-first-value/SKILL.md) | Propose Customer Onboarding Coordinator → Product Adoption Analyst handoffs for an agreed first-value result, with optional account health review. |

The multi-Crew Automation Playbooks are installed as **chat-led Workflow proposals** with ten setup checks each. Installation copies guidance and pending checks. Builder must inspect existing Crews, customer sources and policies, agree on the concrete plan, wire and validate handoffs, and run a real manual case before any optional recurrence or external action is activated. See the [Crew category, use-case, and agent catalog](../docs/design/crew_template_catalog.md) for available versus planned Crew templates.

## Authoring contract

[Playbook Specification v1](spec/PLAYBOOK-SPEC-v1.md) defines the package, fixed entrypoint sections, manifest, builder-consumption behavior, installation record, and versioning rules. Start a new package from the [playbook template](templates/playbook/SKILL.md), then replace its fictional metadata, reference, and example.

Every playbook entrypoint uses the same concise sections: Outcome, When to use, Discovery and user direction, Required inputs, Plan and AgentWorks tools, Knowledge and persistence, Validation and reporting, Guardrails, Read details when needed, and Completion contract. Detailed references remain topic-specific so entrypoint skills stay small.

## Package and integration boundary

Each folder is a self-contained skill package: `SKILL.md`, supporting references, an example, and `playbook.json`. Frontmatter uses the current AgentWorks skill format. The JSON powers the Playbooks catalog, including hierarchy, setup prompt, capability requirements, and optional tool recommendations.

Installing a playbook copies its complete package to `<workflow>/skills/agentworks-playbook-<playbook-id>/` and writes an `installed_playbooks` receipt to the workflow manifest with its version, source hash, status, and skill name. The prefix prevents a playbook from overwriting a customer skill with the same ID. The interactive Workflow Builder attaches these workflow-local playbook skills after resolving the workflow path. Supporting references and examples remain available through progressive skill disclosure.

The Installed view compares the receipt version with the current catalog. A newer catalog version is labeled `Update available` with installed/latest versions, its short authored changelog, and upgrade instructions. `search_playbooks` returns the same comparison so Builder can explain the upgrade without guessing from version numbers. Updating refreshes only the installed guidance and marks setup `draft`; Builder reviews it against the existing workflow and no operational configuration changes automatically.

Installation does not create schedules, connect accounts, install recommended public software, or execute tests. Builder first inspects the existing workflow, summarizes reusable design and gaps, and asks focused questions for material unresolved customer choices. It records answers as customer direction before proposing changes. Runtime steps receive only workflow-selected or per-step `enabled_skills`; installed Builder playbooks do not cascade into execution.

Human review is asynchronous by default. A preparation route saves the exact proposal and evidence, creates a nonblocking `create_human_input_request`, shows it in the Report dashboard and Pulse decision panel, and ends without applying the change. The user may ask about it in chat, then approve, reject, or defer later. Discussion does not decide; deciding does not apply. A separate action reads that durable answer, revalidates the proposal and current target state, uses the executor authorized for that target, and records the outcome. Blocking in-run human branches are reserved for explicitly attended bounded runs.

## Foundation and reuse

The catalog currently targets small engineering teams. Each playbook defaults to extending one workflow with routes that share its goal, access, durable data, dashboard, and lifecycle. Recommend another workflow only when access, ownership, deployment, retention, scale, or failure isolation creates a boundary the team must operate independently.

Basic Browser Setup produces a versioned, non-secret `browser-foundation/v1` profile pointing to canonical suite/config/locator sources. The other Browser QA playbooks reuse these sources and add outcome-specific coverage or operations. Each accepts an equivalent verified configuration where declared and preserves an explicitly chosen compatible runner; a prerequisite need not have been installed by name.

Prefer extending the same application workflow so its profile, DB, evidence, and report are already accessible. For a separate workflow, the builder explicitly transfers an authorized profile snapshot and selects credentials independently. No cross-workflow access is implied by a path or profile ID.

Record the source playbook ID/version and customer overrides in the workflow's durable setup data. Updates to these source packages do not silently rewrite installed workflows.

## Keep skills small

`SKILL.md` holds the purpose, essential constraints, and links to optional detail. Load supporting references only for the current operation. A reference workflow is a starting pattern, not a mandatory graph. The builder may combine or split steps when the user's process, existing plan, retry boundaries, permissions, or supported capabilities justify it.

Save application-specific verified locators and test setup in the knowledgebase with code/evidence references, and wire producer/consumer KB access. Executable locators stay in shared test helpers; chronological run results stay in the DB. Do not grow shared playbook skills with customer discoveries or repeat platform manuals in their entrypoints.

## Authoring checks

- Run `python3 playbooks/scripts/validate_playbooks.py` from the repository root.
- This runs all 21 package-local contract suites; every multi-Crew Automation Playbook now has one.
- Validate every skill's frontmatter and supporting links.
- Parse `playbook.json` and confirm entrypoint/example paths exist.
- Use each reference's behavioral cases when testing the builder on an authorized fixture application.
- Check current `builder-reference` guidance before adapting to a deployed version. The packages follow the checked-in managed browser, per-step skill, and live HTML report contracts.

Recommended CLIs, MCPs, and public skills remain optional. Their availability, compatibility, source revision, license, and trust are checked at setup time rather than claimed by metadata. Install hints are UI guidance only; playbook installation never executes them automatically. No public registry installation is prescribed for AgentWorks' private Playwright packages.
