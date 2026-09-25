# Crew template catalog

Status: catalog in progress, 2026-09-25. Finance Analyst v1, Tax Export Preparer v1, and all ten Website Growth v1 specialists are available in the Crew creation dialog. They can be combined as capabilities in one Crew. The other entries are planned. Each installed template contributes a local skill and its own setup checklist. Optional integrations and recurring capabilities still require separate setup.

## Product model

A **Crew template** currently provides a reusable capability pack: a local skill, starter instructions, example requests, expected outputs, and a setup checklist. A new Crew may start from one template to seed its identity. An existing Crew can add more packs without changing its role or purpose. Each pack keeps its own setup progress. The target model is a [unified Playbook catalog](unified_agent_automation_playbooks.md): an **Agent Playbook** proposes one primary agent for a Crew, while an **Automation Playbook** proposes a multi-agent team and Workflow plan. Selecting a Playbook opens a setup draft in chat; the Builder inspects what exists, proposes concrete changes, and applies them through authorized tools after review. Supporting packs remain capabilities of one Crew, not additional agent identities. The Automation owns the recurring goal and handoffs; selecting either Playbook does not activate a schedule or run.

Each template must work as a useful interactive Crew after the user provides its minimum inputs. Connected accounts, schedules, outbound messages, payments, production changes, and other consequential actions require explicit setup and the product's normal permissions and approvals. Never prefill a customer's target metric with an illustrative website number.

The catalog tracks candidate jobs in seven categories. Several related jobs should become capabilities of one Crew rather than separate Crew identities. The current website groups Money, Customers, Growth, Operations, and Engineering; the catalog separates Sales, Marketing & Growth, and Website Growth for the specific new-site traffic journey. Existing Workflow playbooks may inform a template or its suggested Automation, but they are not Crew templates.

The creation picker is designed for a larger installed catalog: keep Blank Crew separate from scrolling results; search across template names, categories, purposes, and first outputs; show category counts and the result count; reveal results in batches; and preserve the chosen template while filters change. On phones, browsing and Crew details are separate views. Only implemented templates appear in the picker—planned catalog entries are not offered for installation.

## Finance

Use **two default Crew identities** for a small business. Finance Analyst owns read-oriented analysis and planning. Receivables Coordinator owns customer-facing payment follow-up proposals. Different data or action permissions remain separate even when the same owner uses both. Tax Export Preparer is already available as an installable supporting pack for Finance Analyst; create a separate tax Crew only when a different owner, access scope, or review boundary requires it.

| Default Crew | Job and first useful output | Minimum user input | Optional recurring work |
| --- | --- | --- | --- |
| **Finance Analyst** — available v1 | Explain revenue, expense, cash, and reconciliation changes in a sourced finance brief. Add optional forecasting, expense review, and tax export capabilities within this Crew. | Authorized records, period, currency, metric definitions; further inputs only for selected capabilities. | A weekly brief can be this Crew's schedule. A broader finance review Automation is useful only when it coordinates distinct owners or Crews. |
| **Receivables Coordinator** — planned | Review overdue invoices and failed payments, produce an exceptions and follow-up queue, and draft customer-safe messages for approval. | Invoice/payment records, terms, retry and contact policies, routing owner. | Receivables Review, if a recurring goal and review policy are wanted. |

| Capability pack | Installed in | First result and boundary |
| --- | --- | --- |
| **Revenue Reconciliation** — planned | Finance Analyst | Matched orders, invoices, payments, and refunds with a source-linked mismatch list. Read-only until the owner separately authorizes a correction. |
| **Expense Review** — planned | Finance Analyst | Categorized expenses and an exception queue under the owner's chart of accounts and approval rules. No automatic approval or payment. |
| **Cash Flow Planning** — planned | Finance Analyst | A forecast with source balances, receivable/payable assumptions, scenarios, and uncertainty. It is a forecast, not a verified balance. |
| **Tax Export Preparer** — available v1 | Finance Analyst by default; separate Crew when access requires | Reconciled transaction export and exception list for review by the owner and tax professional. No automatic tax classification, filing, or delivery. |
| **Collections** — planned | Receivables Coordinator | Ranked overdue-invoice queue and proposed follow-ups. Messages remain drafts until reviewed. |
| **Failed Payment Recovery** — planned | Receivables Coordinator | Failed-payment investigation and policy-compliant next steps. No charge retry or customer message without a separately authorized route. |

Setup checks belong to each selected capability pack, so a Finance Analyst can be ready for a sourced brief while tax export remains pending. Builder should first inspect the existing Finance Analyst Crew and offer a supporting pack; it should propose another Crew only when the permission, owner, cadence, or independent-review boundary makes sharing inappropriate. Adding a pack never imports another Crew's connections or activates a schedule, function, trigger, payment action, or delivery channel.

## Customer Support

| Crew template | Job and first useful output | Minimum user input | Suggested Automation |
| --- | --- | --- | --- |
| **Support Triage Assistant** | Classify incoming cases, identify urgency, and propose an owner and next action. | Case examples, priority rules, support channels. | Inbox Triage |
| **Support Reply Drafter** | Draft grounded replies from approved help content and show citations or source links. | Help docs, tone guide, escalation rules. | Support First Response |
| **Customer Onboarding Assistant** | Guide a new customer through setup and track unresolved onboarding questions. | Onboarding guide, product access boundaries, handoff owner. | Onboarding Check-in |
| **Customer Success Analyst** | Review account health and summarize risk, adoption, and follow-up opportunities. | Account data, health definitions, account ownership. | Account Health Review |
| **Escalation Coordinator** | Keep a customer escalation brief current and coordinate human handoffs. | Escalation policy, case history, responsible team. | Escalation Watch |
| **Feedback & Review Analyst** | Cluster feedback and reviews into themes, draft responses, and flag urgent issues. | Review or feedback export, response policy, product context. | Review Responder |

## Sales

| Crew template | Job and first useful output | Minimum user input | Suggested Automation |
| --- | --- | --- | --- |
| **Lead Researcher** | Prepare a concise, sourced brief on a prospective account and likely needs. | Lead or company name, ideal customer profile, research scope. | Lead Research Queue |
| **Lead Qualifier** | Compare inbound leads with qualification rules and explain the recommended route. | Lead records, qualification criteria, routing owners. | Lead Qualification |
| **Outreach Personalizer** | Draft relevant first-touch and follow-up messages for human review. | Approved offer, voice guide, lead context, contact policy. | Lead Follow-up |
| **Sales Call Briefing Assistant** | Assemble account context, likely questions, and a meeting brief. | Meeting details, CRM notes or files, product material. | Pre-meeting Brief |
| **Proposal Drafter** | Turn discovery notes into a scoped proposal draft with open questions and evidence. | Discovery notes, pricing rules, approved proposal format. | Proposal Preparation |
| **Pipeline Analyst** | Explain pipeline movement, stale deals, and forecast risks with record links. | Pipeline export or authorized CRM, stage definitions, reporting period. | Pipeline Health Review |

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

| Crew template | Job and first useful output | Minimum user input | Suggested Automation |
| --- | --- | --- | --- |
| **Competitor Intelligence Analyst** | Track meaningful changes in competitor positioning, pricing, and launches with sources. | Competitor list, watch topics, alert threshold. | Competitor Watch |
| **Campaign Performance Analyst** | Explain campaign results, anomalies, and the next test to run. | Campaign data, spend, conversion definitions, reporting window. | Campaign Review |
| **Growth Experiment Planner** | Turn a growth hypothesis into a bounded experiment plan and decision rule. | Baseline, target audience, metric, constraints. | Growth Experiment Review |

## Operations

| Crew template | Job and first useful output | Minimum user input | Suggested Automation |
| --- | --- | --- | --- |
| **Chief of Staff** | Synthesize priorities, decisions, blockers, and follow-ups into an operator brief. | Team goals, current notes, owners, reporting cadence. | Weekly Business Review |
| **Meeting Actions Coordinator** | Extract decisions and action items with owners, due dates, and source references. | Notes or transcript, owner list, task conventions. | Meeting Notes to Actions |
| **Project Status Reporter** | Summarize progress, risks, and requests for decisions across project records. | Project updates, milestone definitions, stakeholders. | Project Status Digest |
| **Order Operations Coordinator** | Investigate stuck orders and prepare safe next actions and customer updates. | Order data, fulfillment policy, exception thresholds. | Order Watchdog |
| **Vendor Researcher** | Compare vendors against requirements and produce an evidence-linked shortlist. | Requirements, budget, security constraints, decision owner. | Vendor Review |
| **Document Intake Assistant** | Extract and check structured facts from incoming documents, flagging uncertain fields. | Sample documents, required fields, validation rules. | Document Intake Queue |

## Engineering

| Crew template | Job and first useful output | Minimum user input | Suggested Automation |
| --- | --- | --- | --- |
| **Release Quality Assistant** | Inspect a release candidate and summarize tests, failures, evidence, and a proposed gate decision. | Repository or build, test policy, release scope. | Release & PR Quality Gate |
| **Incident Investigator** | Correlate alerts, logs, and deploys into a sourced timeline and initial hypotheses. | Incident scope, authorized telemetry, escalation policy. | Incident Investigation |
| **Security Findings Analyst** | Triage authorized findings and draft reviewed remediation steps with verification criteria. | Findings, asset scope, severity policy, code access. | Application Security |
| **Cloud Cost Analyst** | Explain cost changes and propose evidence-backed savings with service-risk checks. | Billing data, ownership map, budget, change policy. | Cloud Cost Anomaly to Savings |
| **Performance Investigator** | Analyze latency or page performance regressions and produce a reproducible diagnosis. | Targets, traces or test runs, performance budgets. | Performance Validation |
| **PR Review Assistant** | Review a change against repository standards and surface specific, verifiable risks. | Repository access, review rules, change scope. | PR Review Queue |

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
