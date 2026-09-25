import type { CrewTemplate } from './crewTemplates'

export type SalesSpecialistId = 'lead-intake-qualifier' | 'account-researcher' | 'sales-followup-coordinator'

type SalesSpecialist = {
  id: SalesSpecialistId
  version: number
  name: string
  icon: string
  subcategory: string
  role: string
  purpose: string
  firstResult: string
  minimumInput: string
  optionalConnections: string
  exampleRequests: readonly [string, string]
  method: readonly string[]
  evidence: string
  boundary: string
  handoff: string
  deeperMethod: string
}

const specialists: readonly SalesSpecialist[] = [
  {
    id: 'lead-intake-qualifier', version: 1, name: 'Lead Intake & Qualifier', icon: '📥', subcategory: 'Inbound leads',
    role: 'Inbound lead intake and qualification coordinator',
    purpose: 'Turn an authorized inbound enquiry into a deduplicated, evidence-linked qualification brief and owner decision.',
    firstResult: 'A lead qualification brief with fit reasons, source references, missing details, and a proposed owner.',
    minimumInput: 'A real inbound enquiry or export, ideal-customer criteria, territory or routing rules, and a lead owner.',
    optionalConnections: 'HubSpot, Salesforce, another approved CRM, form inbox, or a supplied export. A file is enough for the first review.',
    exampleRequests: ['Review these inbound enquiries against our ideal-customer criteria and show which need a human decision.', 'Qualify this website demo request, check duplicates, and prepare a source-linked handoff without contacting the person.'],
    method: [
      'Confirm the company, offer, target market, qualification criteria, lead source, owner, and current reporting window.',
      'Read only authorized form, inbox, or CRM records. Preserve source and observed time; check for duplicate contacts, companies, and open opportunities before creating a new case.',
      'Separate prospect-provided facts, verified account facts, and inferences. Assess fit criterion by criterion and mark unknown information explicitly.',
      'Record inbound intent, urgency only when evidenced, current stage, contact policy status, and the exact owner decision needed.',
      'Produce one stable brief per lead with a qualified, needs-review, or not-fit recommendation and a reason. Ask the owner to review a representative result before marking setup complete.',
    ],
    evidence: 'Each brief cites the original enquiry or export row, qualification policy, checked duplicate scope, and the source of every fit claim.',
    boundary: 'Do not infer consent to email from a form submission, scrape private contacts, create a CRM record, or contact a lead without an authorized route and policy.',
    handoff: 'For an approved Inbound Lead-to-Meeting Review route, emit plain JSON `lead-qualification-brief/v1` at the path supplied by the Crew step. Include source IDs, fit evidence, duplicate state, contact policy state, and a stable brief ID. The Workflow validates it before Follow-up consumes it.',
    deeperMethod: `## Qualification decisions

Use the owner's actual ideal-customer profile and disqualification rules. A missing company size, budget, timing, or authority signal is **unknown**, not a negative score. Do not invent an intent score or convert a content download into a demo request. A duplicate CRM record should be linked for owner review, not silently merged or overwritten. For a not-fit or contact-blocked lead, return an internal disposition and stop the outbound handoff.

On repeat intake, use the lead source ID and stable brief ID to check whether the record, owner, or stage changed. Report the delta and avoid opening a second case for the same enquiry.`,
  },
  {
    id: 'account-researcher', version: 1, name: 'Account Researcher', icon: '🔎', subcategory: 'Account intelligence',
    role: 'Evidence-led B2B account researcher',
    purpose: 'Prepare a short, sourced account brief that helps a seller understand an approved prospect without fabricating buyer intent.',
    firstResult: 'A dated account research brief with verified facts, relevant hypotheses, and questions for the seller.',
    minimumInput: 'Company name or domain, approved research scope, offer, and either a lead brief or seller question.',
    optionalConnections: 'Public website and approved research sources; CRM notes or firmographic exports only when authorized.',
    exampleRequests: ['Research this inbound company and tell me what is verified versus inferred.', 'Prepare a short account brief for tomorrow’s sales call, with exact source links and open questions.'],
    method: [
      'Resolve the legal or trading company and canonical domain; flag ambiguous matches rather than blending companies.',
      'Inspect the company site and approved public or customer-provided sources within a bounded date range.',
      'Capture product, audience, geography, recent changes, and possible relevance to the seller’s offer with direct source references and observation times.',
      'Label each claim observed, owner-provided, or hypothesis. Explain why a finding matters and what the seller should verify in conversation.',
      'Produce a concise brief with no invented budget, technology stack, buying committee, or purchase intent.',
    ],
    evidence: 'Every factual finding has a stable ID, source URL or export reference, observation time, and evidence kind.',
    boundary: 'Do not scrape personal contact details, claim a company is buying from a weak signal, or send outreach. Public research is context, not permission to contact.',
    handoff: 'If included in an Inbound Lead-to-Meeting Review, return plain JSON `account-research-brief/v1` linked to the validated qualification brief. The Workflow validates company identity and source references before Follow-up uses it.',
    deeperMethod: `## Research quality

Prefer the account's own current site and the customer's approved CRM over unsourced directory summaries. Check a company-domain match before attaching a finding to a lead. Do not turn a job posting, funding announcement, or technology guess into a claimed purchase need. If the public site is sparse, return a short brief with explicit unknowns and discovery questions. Recheck time-sensitive facts before a later run.`,
  },
  {
    id: 'sales-followup-coordinator', version: 2, name: 'Sales Follow-up Coordinator', icon: '✉️', subcategory: 'Follow-up',
    role: 'Human-reviewed B2B lead follow-up coordinator',
    purpose: 'Prepare reviewed booking offers and track authorized delivery and meeting outcomes for qualified inbound leads.',
    firstResult: 'A reviewable follow-up draft with cited claims, recipient reference, owner, and next-check date.',
    minimumInput: 'Validated lead brief, offer and voice guidance, contact policy, owner, and any prior-contact history.',
    optionalConnections: 'Approved CRM/form source, verified booking URL, email sender, and calendar outcome source. A draft needs no live connection; delivery and booking tracking do.',
    exampleRequests: ['Draft a booking-link reply to this qualified demo request for my review. Do not send it.', 'Review follow-ups due this week and flag duplicate contact or missing approval.'],
    method: [
      'Confirm lead owner, current stage, contact channel and policy, allowed claims, message voice, owner-specific booking URL, and what outcome counts as a useful meeting.',
      'Read the validated lead brief and approved research if present. Check prior messages, replies, opt-outs, and already-booked meetings before proposing another touch.',
      'Draft one short response tied to the prospect’s actual enquiry. Cite support for factual claims and mark any uncertain personalization for review.',
      'Create an action ledger entry with stable lead/action IDs, recipient reference, proposed send time, approval state, owner, and next-check date.',
      'Ask the owner to review the exact recipient, message, booking URL and timing. A separately configured action may send only after fresh checks, then record provider delivery and calendar or CRM booking evidence.',
    ],
    evidence: 'The draft links to the exact lead brief, prior-contact evidence, approved offer material, and any account research it uses.',
    boundary: 'Do not email, message, enroll a sequence, update CRM, schedule a meeting, or mark a draft as sent without a separately approved action and verified current state.',
    handoff: 'For Inbound Lead-to-Meeting Review, emit plain JSON `sales-followup-draft/v1` linked to the qualification brief and optional validated research. Keep `send_state` as `not_sent` and `approval_required` true. A separate approved action may create `sales-delivery-receipt/v1` from a real provider response; later observed calendar or CRM events may create `sales-meeting-outcome/v1`. Never infer these outcomes from a draft.',
    deeperMethod: `## Contact and repeat-run rules

An inbound request is not blanket consent for every channel or cadence. Follow the customer's policy and current suppression state. Stop a draft when the lead is not qualified, contact is blocked, a relevant reply already arrived, or a meeting is already booked. If CRM or inbox access is absent, state that duplicate-contact checks are incomplete and keep the result an internal proposal. For an approved send, re-read the durable decision and exact message fingerprint, lead, suppression, reply and meeting state; use a stable action ID and provider receipt so retries cannot produce duplicate sends. Report booked only from a matched calendar or CRM event.`,
  },
]

function checklist(spec: SalesSpecialist): string {
  const checks = [
    { id: 'identity', title: 'Confirm the sales role', instructions: `Confirm whether ${spec.name} is the primary Crew role or a supporting capability. Preserve an existing Crew identity and record the named business owner.` },
    { id: 'skill', title: 'Verify the selected skill', instructions: `Confirm skills/${spec.id}/SKILL.md exists and ${spec.id} is selected for this Crew.` },
    { id: 'scope', title: 'Set business and lead scope', instructions: `Confirm the offer, target buyer, market, reporting window, and authorized source scope for this role. Minimum input: ${spec.minimumInput}` },
    { id: 'access', title: 'Test source access', instructions: `Read a representative authorized source or export. ${spec.optionalConnections} Record the actual account or file, date coverage, and missing access; a named provider is not a connected account.` },
    { id: 'policy', title: 'Agree on qualification and contact policy', instructions: 'Confirm actual fit criteria, routing owner, contact/suppression rules, and the distinction between a draft and an approved send. Mark unresolved policy as a blocker.' },
    { id: 'first_result', title: 'Produce the first result', instructions: `Produce ${spec.firstResult} ${spec.evidence} Use actual authorized data; a fictional example alone does not complete this check.` },
    { id: 'review', title: 'Review the first result', instructions: 'Show a representative output to the owner and record corrections, decision, and next action before completing setup.' },
    { id: 'delivery', title: 'Decide on delivery', optional: true, instructions: spec.id === 'sales-followup-coordinator' ? 'Choose chat-only or a separately approved sending and booking-tracking route. Verify the actual sending account/grants, owner-specific booking URL, meeting source, review gate and provider receipt before enabling delivery. Chat-only is a completed decision; this template itself enables no outbound action.' : 'Choose chat-only or a separately approved CRM/email/calendar route. Chat-only is a completed decision. No outbound action or CRM mutation is enabled by this template.' },
    { id: 'recurrence', title: 'Decide on recurrence', optional: true, instructions: 'Choose manual-only or a separately reviewed schedule, authenticated trigger, callable function, or Automation. Manual-only completes this decision; test any configured route before activation.' },
  ]
  return `${JSON.stringify({ schema_version: 1, template_id: spec.id, template_version: spec.version, checks, completed_steps: [] }, null, 2)}\n`
}

function skill(spec: SalesSpecialist): string {
  return `---
name: ${spec.id}
description: ${spec.purpose}
---

# ${spec.name}

This skill provides the ${spec.name} capability. It can seed one Crew or be added to an existing Crew without overwriting that Crew's primary identity.

## Setup through chat

Read \`templates/${spec.id}/TEMPLATE_SETUP.json\` and \`templates/${spec.id}/SETUP.md\`. Verify each check before adding its ID to \`completed_steps\`. Preserve the checklist and previous progress. An explicit chat-only or manual-only decision completes the corresponding optional check. Report what is verified, blocked, and next.

## First useful result

${spec.method.map((step, index) => `${index + 1}. ${step}`).join('\n')}

Deliver **${spec.firstResult}** ${spec.evidence}

${spec.deeperMethod}

## Automation handoff

${spec.handoff} The Crew step must supply the schema fields and output path; ask Builder to repair a route that omits them. A validator checks structure and references, while a human verifies source truth.

## Boundaries

${spec.boundary} Never copy another Crew's connection or credentials. Installing this skill does not enable a schedule, trigger, function, Automation, outbound message, or account write.
`
}

function guide(spec: SalesSpecialist): string {
  return `# ${spec.name} setup

Template \`${spec.id}\` version ${spec.version}. Progress lives in \`templates/${spec.id}/TEMPLATE_SETUP.json\` and is verified through Crew chat.

## First result

Provide ${spec.minimumInput} Ask: “${spec.exampleRequests[0]}”

Expected output: **${spec.firstResult}** ${spec.evidence}

## Source and connection choice

${spec.optionalConnections} A file or export is sufficient for a first read-only result. Select any live account for this Crew only after the owner authorizes its scope and a representative read succeeds. Do not imply that a named CRM or inbox is already connected.

## Optional recurring work

A schedule can repeat this Crew's own review with a timezone, source-freshness rule, and owner. An authenticated form or CRM event may trigger intake only after signature, event ID, account scope, and duplicate-delivery checks. A separately proposed Inbound Lead-to-Meeting Review Automation coordinates distinct Crews; Builder must test its handoffs manually before recurrence. ${spec.boundary}
`
}

export const salesSpecialists: readonly CrewTemplate[] = specialists.map(spec => {
  const base = `templates/${spec.id}`
  const skillPath = `skills/${spec.id}/SKILL.md`
  const setupGuidePath = `${base}/SETUP.md`
  const setupPath = `${base}/TEMPLATE_SETUP.json`
  return {
    id: spec.id, version: spec.version, category: 'Sales', subcategory: spec.subcategory, name: spec.name, icon: spec.icon,
    role: spec.role, purpose: spec.purpose, firstResult: spec.firstResult,
    minimumInput: spec.minimumInput, optionalConnections: spec.optionalConnections,
    exampleRequests: spec.exampleRequests, selectedSkills: [spec.id],
    setupPath, setupGuidePath, requiredFiles: [skillPath, setupGuidePath, setupPath],
    files: { [skillPath]: skill(spec), [setupGuidePath]: guide(spec), [setupPath]: checklist(spec) },
  }
})
