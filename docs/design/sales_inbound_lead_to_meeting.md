# Inbound Sales v1: lead to reviewed follow-up

Status: implemented locally, not deployed. Scope: B2B inbound enquiries, especially demo or contact requests arriving after a new website launch.

## Product boundary

The **Sales** category starts with three reusable Crew capabilities: Lead Intake & Qualifier, Account Researcher, and Sales Follow-up Coordinator. Each installs a project-local skill plus a nine-check chat setup list. A Crew may carry more than one capability; a new Crew is proposed only when the owner, data access, or review boundary makes it useful. The creation picker and Builder both use the same versioned first-party catalog.

The **Inbound Lead-to-Meeting Review** Automation Playbook proposes two required Crew slots (Qualification and Follow-up) and one optional Research slot. Installing it copies guidance and a ten-check setup record into the Workflow. Builder chat inspects current Crews and customer direction, then proposes concrete steps. Selection does not create Crews or start a run. The first route remains manual until actual source access, policy, validator steps, and an owner-reviewed result are verified.

## First useful result

1. Qualification reads one authorized inbound enquiry or export, checks duplicate scope, applies owner-defined fit criteria, and creates `lead-qualification-brief/v1` with source IDs. Unknown fit signals stay unknown. A not-fit, possible duplicate, or contact-blocked lead receives an internal disposition and does not advance to an outbound draft.
2. Optional Research uses a resolved company domain and approved public or customer-provided sources to create `account-research-brief/v1`. It labels observed facts, owner-provided facts, and hypotheses.
3. Follow-up reads only the validated lead brief and optional validated research, checks current prior contact and policy, and creates `sales-followup-draft/v1` with `send_state: not_sent`, owner approval, claim references, and next-check time. It does not send, enroll, update CRM, or claim a booked meeting.

The bundled validator checks each artifact's shape, source references, lead/domain binding, duplicate and contact-state gate, and draft state. It blocks the consumer step on failure. A human verifies source truth and customer contact permission; valid JSON alone cannot establish either.

## Sources and providers

An export or file is sufficient for the first read-only result. During setup, ask for the authoritative form/CRM account, ideal-customer rules, lead owner, suppression policy, approved offer, and prior-contact scope. HubSpot and Salesforce are provider examples; this package does not assume either is connected. An approved email or calendar route is a separate setup decision with an exact recipient/content review, current suppression and reply check, and recorded provider outcome. A meeting counts only when an authorized calendar or CRM source confirms it.

The product choice follows the existing small-business sales workflow of capturing leads, recording activities and follow-ups, and tracking pipeline outcomes; see [HubSpot lead pipeline automation](https://knowledge.hubspot.com/object-settings/set-up-lead-pipeline-automation) and [Salesforce small-business lead management](https://www.salesforce.com/ap/small-business/lead-management/). These references explain market context, not a claim of AgentWorks integration.

## Acceptance evidence

- All three Crew templates install with an uncompleted nine-check list and no selected account, trigger, schedule, or outbound route.
- The Playbook installs with two required and one optional slot and an uncompleted ten-check setup record.
- The valid fictional qualification, research, and follow-up artifacts pass the local contract validator; a bad source reference, blocked contact, possible duplicate, another lead ID, or a claimed sent state fails.
- Builder can create a Sales Crew from the trusted template ID with its local skill selected and an idempotent receipt.
- Frontend and Go tests cover catalog visibility, setup, Playbook installation, and Builder creation. A real customer setup and manual run remain pending until that customer supplies authorized records and decisions.
