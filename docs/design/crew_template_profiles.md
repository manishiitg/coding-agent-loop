# Installable Crew template profiles

Status: documentation companion to the [Crew template catalog](crew_template_catalog.md), reviewed 2026-09-26. These 21 profiles describe the job to prove during chat setup. The [frontend catalog](../../frontend/src/products/work/crewTemplates.ts) and its specialist modules remain the source of truth for installed skill text, versions, and checklist IDs. An example or suggested connection here does not grant access, enable recurrence, or establish that a customer has completed setup.

Each profile answers: **when to use it, what a first result must contain, what setup must verify, and what changes on a later run.** The first four multi-Crew journeys have linked JSON fixtures; other individual agent outputs still need complete worked examples before they are promoted as fully demonstrated public templates. See the [content quality review](../reviews/playbook_template_content_quality_2026-09-25.md).

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

## Content completion rule

These profiles make the **jobs and setup evidence** explicit. They are not substitutes for actual output evaluation. Before an agent appears as a fully demonstrated public template, add its own complete good and bad fictional outputs, source probes, a reviewed real first run, and a repeat-run case. The existing four Playbook fixtures cover selected handoffs and should be linked from the relevant agent page when that work is done. Track planned roles in the [main catalog](crew_template_catalog.md); do not add them to this installable list until the Crew picker and Builder can install them.
