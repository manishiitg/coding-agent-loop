# Installable Crew template profiles

Status: documentation companion to the [Crew template catalog](crew_template_catalog.md), reviewed 2026-09-26. These 42 profiles describe the job to prove during chat setup. The [frontend catalog](../../frontend/src/products/work/crewTemplates.ts) and its specialist modules remain the source of truth for installed skill text, versions, and checklist IDs. An example or suggested connection here does not grant access, enable recurrence, or establish that a customer has completed setup.

Each profile answers: **when to use it, what a first result must contain, what setup must verify, and what changes on a later run.** The fourteen multi-Crew journeys have linked JSON fixtures; other individual agent outputs still need complete worked examples before they are promoted as fully demonstrated public templates. See the [content quality review](../reviews/playbook_template_content_quality_2026-09-25.md).

## Finance

### Finance Analyst (`finance-analyst`)

- **Use case:** explain revenue, expense, cash, or SaaS metric changes from authorized records for a named period.
- **First result:** a sourced brief that distinguishes billed, collected, recognized, and cash amounts, shows calculations and definitions, and lists unreconciled items. The Finance Operations Review has an [illustrative impact readout](../../playbooks/agentic-engineering-platform/finance/finance-operations-review/examples/finance-impact-readout.json).
- **Setup proof:** read one actual statement/export or connected record, agree on entity, period, currency, accounting basis, and metric definitions, reproduce one calculation, and have the owner review the brief. A spreadsheet skill or accounting MCP is optional when the file route works.
- **Later run:** preserve definitions and source IDs, explain changes against the prior period, and carry unresolved exceptions forward rather than generating a new unsupported forecast.

### Billing Operations Coordinator (`billing-operations-coordinator`)

- **Use case:** prioritize overdue invoices, failed payments, refund requests, and disputes for a subscription business.
- **First result:** a dated exception queue with invoice/payment IDs, amount and currency, deadline or status, prior-contact evidence, proposed owner action, and drafts awaiting review. See the [illustrative queue](../../playbooks/agentic-engineering-platform/finance/finance-operations-review/examples/billing-exception-queue.json).
- **Setup proof:** read one authorized billing record or export, verify the customer's invoice, refund, and contact policies, identify the owner, and review one queue item against source status. Stripe or Paddle is a provider choice, never a presumed connection.
- **Later run:** re-read current payment/refund/dispute and prior-contact state, retain stable case IDs, and avoid a second follow-up or refund proposal for a resolved case. A real refund or message requires a separate authorized action.

### Revenue & Close Analyst (`revenue-close-analyst`)

- **Use case:** prepare a reviewable subscription period close.
- **First result:** a close checklist and memo that reconciles invoices, credits, collections, and ledger or revenue-schedule amounts, with source-linked differences and their owner.
- **Setup proof:** agree on entity, close period, currency, accounting policy, and authoritative billing and ledger sources; reproduce one matching and one unmatched transaction. An export is sufficient for a first read-only review.
- **Later run:** retain the prior exception IDs, report cleared versus new differences, and avoid changing an accounting entry without separate approval and write access.

### Spend & Payables Coordinator (`spend-payables-coordinator`)

- **Use case:** review bills and company spend before payment or approval.
- **First result:** a source-linked queue with due dates, duplicate candidates, missing evidence, policy exceptions, approvers, and next decisions.
- **Setup proof:** read a representative bill or expense export, establish entity, currency, period, approval thresholds, and paid-status source, then have an owner review a flagged item.
- **Later run:** deduplicate by vendor, invoice ID and amount under the customer's policy; recheck due and paid states; preserve deferred exceptions. The Crew does not pay a bill by being installed.

### Tax Export Preparer (`tax-export`)

- **Use case:** prepare transaction records for the owner and tax professional.
- **First result:** a reconciled export with source references, period, jurisdiction, currencies, classification questions, and an exceptions list.
- **Setup proof:** agree on recipient format and required fields; verify one invoice/payment/refund mapping and totals against an authorized export; have the owner or professional review unresolved treatment.
- **Later run:** carry prior source IDs and recipient decisions forward, avoid duplicate rows, and identify changed or newly missing evidence. This pack does not file or classify tax obligations automatically.

## Marketing → Website Growth

### Website Growth Starter (`website-growth-starter`)

- **Use case:** a newly launched website needs a first prioritized plan for relevant visitors.
- **First result:** a dated, sourced site audit and 30-day action brief tied to the offer, buyer, and useful visitor action. See the [illustrative priority brief](../../playbooks/agentic-engineering-platform/website-growth/website-growth-loop/examples/growth-priority-brief.json).
- **Setup proof:** inspect bounded public URLs or the owner's export, record offer/audience/action and the actual evidence behind each priority, then review the order with the owner. Search Console and analytics are optional for a new site's first plan.
- **Later run:** read prior actions and decisions, inspect changed pages first, and report no new evidence if nothing changed. A traffic lift requires comparable measured data.

### SEO Analyst (`seo-analyst`)

- **Use case:** identify observable crawl, metadata, internal-link, and page-experience issues.
- **First result:** page-level issue list with inspected URL, observation time, impact and effort rationale, confidence, and retest.
- **Setup proof:** agree on canonical domain and crawl scope; reproduce one issue from a fetched or rendered page. Search Console access is required only for claims about its private indexing data.
- **Later run:** retest prior issue IDs and changed URLs; mark fixed, persistent, or unverified rather than reporting every issue as new.

### Search Opportunity Mapper (`search-opportunity-mapper`)

- **Use case:** map real buyer questions to existing pages and justified content gaps.
- **First result:** a ranked question-to-page map with source, buyer intent, answer status, evidence, and next action. See the [illustrative map](../../playbooks/agentic-engineering-platform/website-growth/website-growth-loop/examples/search-opportunity-list.json) and its [rejected counterpart](../../playbooks/agentic-engineering-platform/website-growth/website-growth-loop/examples/invalid-search-opportunity-list.json).
- **Setup proof:** save an exact question and its origin, inspect the relevant page set, and have the owner review one `answered`, `weak_answer`, or `gap` decision. Search volume remains unknown without an authorized source.
- **Later run:** keep stable question IDs, inspect changed pages and new question evidence, and preserve the owner's rejected or deferred choices.

### Content Brief Writer (`content-brief-writer`)

- **Use case:** turn an approved buyer question or search opportunity into a page brief.
- **First result:** intended reader and action, angle, outline, claims with sources or verification flags, relevant internal links, and an owner decision.
- **Setup proof:** verify the approved opportunity, inspect existing pages for duplication, and review at least one factual claim against product material. A CMS connection is optional for a brief.
- **Later run:** update the same brief when new source facts or owner direction arrive; do not issue a second brief as if the first were never reviewed.

### Content Page Builder (`content-page-builder`)

- **Use case:** draft an approved page for human review.
- **First result:** a page draft tied to its brief, with source notes, internal links, a clear next action, and unresolved claims.
- **Setup proof:** read the approved brief and brand/source material, inspect the existing page context, and review the rendered preview if an implementation is supplied. Drafting is distinct from publication.
- **Later run:** revise the same draft against reviewer comments and changed facts; require an actual ship record before any measurement route treats it as published.

### Search Console Optimizer (`search-console-optimizer`)

- **Use case:** find existing pages with actionable query evidence.
- **First result:** page/query recommendation with property, filters, date range, source rows, interpretation, and proposed title, copy, or link change.
- **Setup proof:** test the exact Search Console property or export and reproduce one page/query metric using compatible dimensions and dates; note missing or anonymized query coverage.
- **Later run:** compare compatible windows and filters, retain the prior recommendation decision, and separate observed change from unsupported causal claims.

### Traffic & Engagement Analyst (`traffic-engagement-analyst`)

- **Use case:** explain changes in useful visits and conversion actions.
- **First result:** sourced readout with metric definitions, numerator and denominator where relevant, segments, data gaps, and one prioritized next action.
- **Setup proof:** verify analytics property/export, event definition, timezone and period; reproduce one reported metric. A new site may need a baseline-first report instead of a trend.
- **Later run:** compare like-for-like periods, flag instrumentation changes, and link any actual shipped change without claiming causality from timing alone.

### AI Visibility Analyst (`ai-visibility-analyst`)

- **Use case:** observe whether chosen answer engines cite the brand or competitors for buyer questions.
- **First result:** question-level observations with engine/surface, run time, cited URLs, absence or error state, and source-backed content gaps.
- **Setup proof:** freeze the question set and sampling method, run and retain an inspectable observation, and distinguish unavailable engine output from a genuine absence. This is an experimental measure.
- **Later run:** repeat under the same method or explicitly mark method changes; do not present one stochastic answer as a stable ranking.

### Landing Page Optimizer (`landing-page-optimizer`)

- **Use case:** diagnose one page's path to a useful visitor action.
- **First result:** page-level diagnosis, observed friction, bounded revision or test hypothesis, success signal, and guardrails.
- **Setup proof:** inspect the page and target action, verify the visitor intent and available behavior data, and review one proposed change with the owner. A low-traffic page may warrant a qualitative revision rather than an A/B test.
- **Later run:** compare the reviewed change against a verified ship record and appropriate observation window; report inconclusive results honestly.

### Content Distribution Coordinator (`content-distribution-coordinator`)

- **Use case:** distribute an already published asset through relevant approved channels.
- **First result:** channel-by-channel audience fit, asset URL, drafts, approval points, tracking plan, and owner action.
- **Setup proof:** verify the live asset and an allowed channel, review one complete channel-specific draft and the contact policy. A social, email, or CRM connection is optional for planning and required for its actual route.
- **Later run:** retain campaign/action IDs and delivery receipts, avoid duplicate posts or outreach, and compare observed results without inventing attribution.

## Sales

### Lead Intake & Qualifier (`lead-intake-qualifier`)

- **Use case:** decide what to do with an authorized inbound enquiry.
- **First result:** deduplicated, source-linked fit brief with unknowns, policy state and owner decision. See the [illustrative qualification brief](../../playbooks/agentic-engineering-platform/sales/inbound-lead-to-meeting-review/examples/lead-qualification-brief.json).
- **Setup proof:** read one form/CRM/export record, agree on fit and routing rules, check duplicate scope and contact policy, and have the owner review a real brief. A form submission is not permission for every channel.
- **Later run:** keep the source and brief IDs, re-read lead stage and duplicate state, and stop downstream contact for not-fit, blocked, or unresolved duplicate cases.

### Account Researcher (`account-researcher`)

- **Use case:** give a seller verified context for an approved company or lead.
- **First result:** dated company facts, source URLs, clearly labeled hypotheses, and discovery questions. See the [illustrative research brief](../../playbooks/agentic-engineering-platform/sales/inbound-lead-to-meeting-review/examples/account-research-brief.json).
- **Setup proof:** resolve the actual company and domain, inspect approved public or customer-provided sources, and review one claimed fact against its source. Public research does not authorize outreach.
- **Later run:** recheck time-sensitive facts, preserve the company binding and prior questions, and describe what changed since the last brief.

### Sales Follow-up Coordinator (`sales-followup-coordinator`)

- **Use case:** prepare a reviewed reply or booking offer for a qualified inbound lead.
- **First result:** an [unsent draft](../../playbooks/agentic-engineering-platform/sales/inbound-lead-to-meeting-review/examples/sales-followup-draft.json) with cited claims, exact recipient reference, booking path, approval owner, and next check.
- **Setup proof:** verify a valid qualification brief, prior contact, suppression and replies, policy, owner, approved offer, and booking URL. A connected sender and observed calendar/CRM source are needed only if delivery and outcome tracking are selected.
- **Later run:** re-read current state before another touch, preserve a stable action and exact draft fingerprint, and count send/booking only from linked provider and event receipts.

## Customer Success

### Customer Onboarding Coordinator (`customer-onboarding-coordinator`)

- **Use case:** turn an authorized signed-customer handoff into an owned path to first value.
- **First result:** milestone register with purchased scope, owner, target date, evidence, blockers, and next customer decision. See the [illustrative register](../../playbooks/agentic-engineering-platform/customer-success/new-customer-to-first-value/examples/onboarding-milestone-register.json).
- **Setup proof:** match CRM/handoff account identity to the onboarding tracker, agree on the customer's actual first-value definition, and review one milestone and evidence source with the owner.
- **Later run:** update stable milestone IDs and slippage reasons; do not claim completion from a plan or contact the customer without an approved route.

### Product Adoption Analyst (`product-adoption-analyst`)

- **Use case:** determine whether the customer achieved the agreed first product result.
- **First result:** [first-value readout](../../playbooks/agentic-engineering-platform/customer-success/new-customer-to-first-value/examples/first-value-readout.json) with event definition, observed state, source records, coverage gaps, and owner action.
- **Setup proof:** verify account-to-event identity mapping, the observation window, a representative authorized product event or export, and the agreed rule for first value. Missing instrumentation is a blocker or unknown, not proof of no adoption.
- **Later run:** compare the same event rule and account identity, record newly observed evidence, and preserve earlier unknowns or changed instrumentation.

### Customer Health Coordinator (`customer-health-coordinator`)

- **Use case:** review an account's adoption, support and renewal signals together.
- **First result:** [health brief](../../playbooks/agentic-engineering-platform/customer-success/new-customer-to-first-value/examples/customer-health-brief.json) with evidence, unknowns, risks, and an owner-reviewed next action.
- **Setup proof:** agree on account owner, health rules, support scope and renewal source; verify one current signal and the linked first-value readout. A missing source must remain visible.
- **Later run:** update stable risk/action IDs, distinguish a resolved blocker from stale data, and avoid declaring churn risk from a single unsupported signal.

## Customer Support

### Support Triage Assistant

- **Use case:** classify a current support case, its urgency, duplicate state, and next owner decision.
- **First result:** a sourced `support-case-triage/v1` with tenant, case, account, thread revision, priority policy, symptom versus verified incident state, owner, and next action. See the [illustrative triage](../../playbooks/agentic-engineering-platform/customer-support/support-case-to-reviewed-resolution/examples/support-case-triage.json).
- **Setup proof:** read one authorized case and latest thread, verify policy and owner, check prior contact and duplicate records, then review a real triage decision.
- **Later run:** re-read the current thread and status, retain case identity, and supersede outdated priority or incident claims when evidence changes.

### Support Reply Drafter

- **Use case:** prepare a customer response for the exact case and recipient from current approved material.
- **First result:** a grounded, unsent `support-reply-draft/v1` with claim references, channel, recipient, open questions, and approval state. See the [illustrative reply](../../playbooks/agentic-engineering-platform/customer-support/support-case-to-reviewed-resolution/examples/support-reply-draft.json).
- **Setup proof:** inspect a real case, approved help source, reply policy, and prior contact; have the owner review one exact unsent message.
- **Later run:** re-read the thread before revising or sending. A delivered message needs exact approval and a provider receipt; a draft is never delivery proof.

### Escalation Coordinator

- **Use case:** hand a case to a receiving team when access, ownership, or expertise changes.
- **First result:** a bounded impact brief with case and account IDs, deadline, receiving owner, acceptance state, and evidence. See the [illustrative escalation](../../playbooks/agentic-engineering-platform/customer-support/support-case-to-reviewed-resolution/examples/support-escalation-brief.json).
- **Setup proof:** verify escalation policy, accepting team, one real case, and a receiving-owner decision; do not report a pending handoff as accepted.
- **Later run:** re-read impact and owner state, preserve handoff and deadline IDs, and notify the support owner when acceptance or status changes.

### Feedback & Review Analyst

- **Use case:** group a bounded feedback or review set into themes and owner decisions.
- **First result:** sourced theme brief with time window, denominator and source coverage, examples, urgency, and a proposed product or support action; any public response remains an unsent draft.
- **Setup proof:** verify one authorized feedback export, source scope, review policy, owner, and a theme against representative and counterexample records.
- **Later run:** compare the same source and window rules, retain prior theme IDs and owner decisions, and avoid counting duplicate reviews as new signals.

## Engineering

### Incident Investigator (`incident-investigator`)

- **Use case:** investigate a production incident across the customer's alerts, telemetry, deploys, and incident records.
- **First result:** dated, sourced timeline with observed impact, labeled hypotheses, gaps, accountable owner, and next decisions. The existing [incident Workflow](../../playbooks/agentic-engineering-platform/reliability-operations/incident-investigation-coordination/SKILL.md) covers a related route.
- **Setup proof:** read one real alert and associated telemetry or deploy record; verify incident and service IDs, event versus ingestion time, source coverage, escalation policy, and an on-call review.
- **Later run:** append new evidence to the same incident, correct disproven hypotheses, and check action state before notifying or creating another ticket. Recovery actions require a reviewed route.

### Engineering Delivery Coordinator (`engineering-delivery-coordinator`)

- **Use case:** follow a blocked change across issue, PR, CI, deployment, and owner handoffs.
- **First result:** blocker ledger with exact issue and change IDs, commit SHA, CI and deployment states, environment, owner, due time, and the next evidence needed.
- **Setup proof:** join one actual issue to a PR, build, and deployment record using stable identifiers; verify release policy, owner, and whether a green build reached the intended environment.
- **Later run:** update the same blocker IDs, distinguish merged from deployed and verified, and avoid duplicate reminders. Merge, CI rerun, ticket write, and deployment need separate authorization.

### Performance Investigator (`performance-investigator`)

- **Use case:** investigate a page or API regression using the customer's performance tools and release history.
- **First result:** regression brief with metric definition, comparable baseline/current measurements, coverage caveats, likely bottleneck, owner, and retest plan.
- **Setup proof:** inspect a representative trace or measurement, confirm route, environment, percentile, units, traffic segment, baseline and current windows, and budget; review the diagnosis with the owner.
- **Later run:** repeat the same measurement rule, record changed traffic or instrumentation, and close only after a comparable retest. Existing [browser](../../playbooks/agentic-engineering-platform/performance-engineering/browser-performance-validation/SKILL.md) and [API](../../playbooks/agentic-engineering-platform/performance-engineering/api-performance-validation/SKILL.md) Workflows can inform the route.

### Cloud Cost Analyst (`cloud-cost-analyst`)

- **Use case:** explain a cloud bill change and prepare risk-checked savings decisions across billing, usage, ownership, and service data.
- **First result:** reconciled cost-change brief with currency and period rules, service owners, arithmetic, candidate savings ranges, risks, and verification plan.
- **Setup proof:** read an authorized bill or export, reconcile one change against usage or allocation evidence, confirm discounts and owner map, and review a candidate with the service owner.
- **Later run:** track the same candidate and approval IDs, check actual billed results after a change, and separate estimated from realized savings. The existing [FinOps Workflow](../../playbooks/agentic-engineering-platform/finops/cost-anomaly-to-verified-savings/SKILL.md) covers the longer route.

## QA

The [Release Candidate to Reviewed Gate](../../playbooks/agentic-engineering-platform/qa/release-candidate-to-reviewed-gate/SKILL.md) Automation Playbook composes the Journey and Gate roles with optional Flaky Test investigation. Its fictional [journey attempt](../../playbooks/agentic-engineering-platform/qa/release-candidate-to-reviewed-gate/examples/journey-result.json), [required-suite matrix](../../playbooks/agentic-engineering-platform/qa/release-candidate-to-reviewed-gate/examples/release-quality-brief.json), and [blocked flake review](../../playbooks/agentic-engineering-platform/qa/release-candidate-to-reviewed-gate/examples/flake-needs-review-gate.json) demonstrate the contract. A real setup still needs an authorized candidate and owner decision.

### Browser Journey QA Analyst

- **Use case:** run an approved user journey against an exact build and investigate a failure.
- **First result:** an attempt-level pass, fail, or blocked result with build and environment, expected and observed behavior, reproduction steps, and durable video, screenshot, and console/network evidence.
- **Setup proof:** verify a representative authorized test account, canonical journey revision, runner, evidence policy, and real attempt. Review a failure or pass against the expected outcome with the QA owner.
- **Later run:** preserve prior attempts, record changed build or test revisions, and verify the same journey on the named later artifact. A retry never erases a failure.

### Flaky Test Investigator

- **Use case:** investigate intermittent outcomes for the same canonical test and build.
- **First result:** a controlled attempt comparison with passing and failing evidence, classification with confidence limits, and a reviewable experiment or fix.
- **Setup proof:** bind test, source revision, build, environment, fixtures, concurrency and retry policy; inspect one real history or bounded repeat and review the diagnosis.
- **Later run:** retain earlier attempts, recheck the classification when product or environment evidence changes, and verify any approved stabilization without weakening assertions.

### Release Quality Assistant

- **Use case:** determine whether a release candidate has complete QA evidence under an agreed gate policy.
- **First result:** a required suite matrix for the exact SHA, build, and environment; a pass, fail, or needs-review proposal; and missing evidence or owner decisions.
- **Setup proof:** verify one real release identity, required suite policy, result source, owner, and status destination. Reproduce a gate decision from the source records before enabling any publication route.
- **Later run:** evaluate each new build independently, preserve previous failures and waivers, and count a published gate only from the destination receipt.

## Security

The [Finding to Verified Remediation](../../playbooks/agentic-engineering-platform/security/finding-to-verified-remediation/SKILL.md) Automation Playbook composes Findings Analyst and Remediation Coordinator. Its fictional [finding](../../playbooks/agentic-engineering-platform/security/finding-to-verified-remediation/examples/security-finding.json), [open ledger](../../playbooks/agentic-engineering-platform/security/finding-to-verified-remediation/examples/security-remediation-ledger.json), and [verified closure](../../playbooks/agentic-engineering-platform/security/finding-to-verified-remediation/examples/verified-security-remediation-ledger.json) show the contract. They do not establish a real customer's scope authorization, deployed state, or retest.

### Security Findings Analyst

- **Use case:** triage an authorized application, dependency, code, or configuration finding against a named asset and build.
- **First result:** a sourced finding queue with applicability, confidence, customer severity rule, owner, remediation options, and a verification criterion. Scanner output alone remains unconfirmed.
- **Setup proof:** record written scope, allowed methods, stop conditions, source and evidence restrictions; inspect one real finding and affected build with the asset owner.
- **Later run:** reconcile stable finding and asset IDs, check the deployed version and prior decision, and avoid duplicate tickets or unsupported closure.

### Access Review Analyst

- **Use case:** compare approved role, ownership, and tenant policy with observed application behavior.
- **First result:** an actor-by-resource-by-action matrix with expected and observed UI and server results, exact build, evidence, and exceptions.
- **Setup proof:** verify the policy revision, authorized test actors and isolated fixtures, target build, direct-route rules, and one safe matrix cell with the decision owner.
- **Later run:** repeat the same denied cells after a reviewed fix, preserve older attempts, and distinguish a changed policy from a changed application.

### Security Remediation Coordinator

- **Use case:** follow a validated finding through approved change, deployment, independent retest, and closure.
- **First result:** an action ledger joining finding, issue, change SHA, deployment, approval, retest, owner, and disposition. A merged fix without deployed retest remains open.
- **Setup proof:** verify the finding source, approved fix or risk-acceptance policy, owner, deployment source, sensitive evidence handling, and one real remediation state.
- **Later run:** re-read exact finding, change, deployment, and retest state; preserve failed checks and risk expiry; close only with owner decision and evidence from the affected environment.

## GTM

### GTM Strategy Analyst

- **Use case:** choose an evidence-backed first audience, message, channel hypothesis, and pipeline measurement rule for a B2B offer.
- **First result:** a reviewed launch brief with offer version, approved claims, buyer problem and sources, channels and budget, owner, qualified lead definition, and open decisions.
- **Setup proof:** verify offer claims, representative customer or market evidence, audience and market, measurement source, baseline or baseline-first decision, and owner approval of one real brief.
- **Later run:** compare new evidence with the recorded hypothesis and metric rule; retain prior decisions and avoid claiming lift from a single campaign result.

### Launch Coordinator

- **Use case:** carry an approved brief through channel assets, owner decisions, provider observations, and inbound lead signals.
- **First result:** an action ledger with asset and campaign IDs, approval versus publication or send state, provider receipt, event and lead IDs, deduplication, attribution confidence, and next owner action.
- **Setup proof:** inspect real asset revisions, approved channels, budget, contact policy, lead and CRM sources, and one provider or public observation. Review exact launch and lead joins with owners.
- **Later run:** reconcile stable asset, campaign, event, and lead IDs; preserve approvals and receipts, recheck contact state, and avoid duplicate distribution or follow-up.

## Shopify

### Store Operations Coordinator (`store-operations-coordinator`)

- **Use case:** work an order exception across Shopify, fulfillment, carrier, inventory, and support records.
- **First result:** an order exception queue with exact store, order, line-item and fulfillment IDs, current states, customer deadline, owner, and next evidence. See the [illustrative order artifact](../../playbooks/agentic-engineering-platform/shopify/order-exception-to-resolution/examples/store-order-exception.json).
- **Setup proof:** read one actual authorized order and matching fulfillment or support record; verify store identity, policy, owner, partial fulfillment, and whether a label, shipment, or delivery was truly observed.
- **Later run:** re-read the same order and prior contact/refund state, preserve case IDs, and avoid duplicate follow-up, cancellation, or refund work.

### Returns & Refunds Coordinator (`returns-refunds-coordinator`)

- **Use case:** review a merchant's return or refund request using order, payment, delivery, prior-refund, and policy records.
- **First result:** [policy and money review](../../playbooks/agentic-engineering-platform/shopify/order-exception-to-resolution/examples/return-resolution-review.json) with eligibility, amount-to-verify, unknowns, owner decision, and an unsent customer draft.
- **Setup proof:** verify one actual store/order/customer match, captured and already-refunded amounts in one currency, current dispute and return state, policy version, prior messages, and approval owner. A draft or approval is not an issued refund.
- **Later run:** re-read transactions and contact state immediately before another action; retain one stable case/action ID; count refunds and messages only from provider receipts.

### Catalog & Merchandising Analyst (`catalog-merchandising-analyst`)

- **Use case:** inspect product and variant quality, availability presentation, collection placement, and search/navigation gaps.
- **First result:** exact product/variant issue queue with observed storefront effect, source, proposed edit, owner, and retest.
- **Setup proof:** inspect a bounded catalog export or authorized store plus actual public storefront, confirm market, collection, catalog rules and inventory authority, and review one issue with the merchant.
- **Later run:** retest the same product and variant IDs after approved edits, preserve deliberate merchant choices, and avoid claiming sales impact without measured evidence.

### Shopify Growth Analyst (`shopify-growth-analyst`)

- **Use case:** connect store discovery, product page, cart, checkout, and order evidence to bounded growth actions.
- **First result:** a sourced growth brief with audience and market, funnel or page observations, denominators and date range where measured, ranked actions, and a verification plan.
- **Setup proof:** inspect the storefront and one representative authorized analytics report if making performance claims; agree on market, currency, timezone, conversion action, attribution limits and owner.
- **Later run:** check what actually shipped, compare the same segment and metric rule, and report inconclusive results when traffic or instrumentation is inadequate.

### Payment Operations Investigator (`payment-operations-investigator`)

- **Use case:** investigate a Shopify order's authorization, capture, pending/failure, void, or refund transaction and its effect on the fulfillment decision.
- **First result:** a payment exception with exact order and transaction IDs, kind, status, parent, presentment currency and amount, owner, and next evidence. See the [authorization-only example](../../playbooks/agentic-engineering-platform/shopify/payment-exception-to-order-decision/examples/payment-exception.json).
- **Setup proof:** read a real authorized OrderTransaction and matching order, including kind/status, parent, amount/currency, capture policy, and owner. An authorization is not successful capture; a Refund object alone does not establish that the money arrived.
- **Later run:** re-read the transaction and order before any retry or release decision, preserve a stable case/action ID, and stop a stale proposal when a later capture, void, dispute, or refund changes the state.

## Content completion rule

These profiles make the **jobs and setup evidence** explicit. The installed skills and guides include fictional good/rejected outputs and source probes. They are not substitutes for actual output evaluation. Before an agent appears as a fully demonstrated public template, run its probe with authorized customer data, review its first result, and exercise a repeat case. The existing thirteen Playbook contract suites cover selected handoffs and should be linked from the relevant agent page when that work is done. Track planned roles in the [main catalog](crew_template_catalog.md); do not add them to this installable list until the Crew picker and Builder can install them.
