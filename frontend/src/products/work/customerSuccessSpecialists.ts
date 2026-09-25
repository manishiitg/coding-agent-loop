import type { CrewTemplate } from './crewTemplates'

export type CustomerSuccessSpecialistId = 'customer-onboarding-coordinator' | 'product-adoption-analyst' | 'customer-health-coordinator'

type Specialist = {
  id: CustomerSuccessSpecialistId
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

const specialists: readonly Specialist[] = [
  {
    id: 'customer-onboarding-coordinator', name: 'Customer Onboarding Coordinator', icon: '🚀', subcategory: 'Onboarding',
    role: 'B2B customer onboarding coordinator',
    purpose: 'Turn an authorized new-customer handoff into an owned milestone plan and a source-linked blocker queue.',
    firstResult: 'An onboarding milestone register with owners, evidence, blockers, and the next customer decision.',
    minimumInput: 'Customer/account reference, purchased scope, desired first result, onboarding owner, target date, and an authorized handoff or export.',
    optionalConnections: 'CRM or signed deal handoff, onboarding tracker, product workspace, and approved customer inbox; a supplied handoff file is enough for the first plan.',
    exampleRequests: ['Create an onboarding plan for this new customer and show which milestones lack an owner or evidence.', 'Review this week’s new accounts and flag onboarding blockers without sending customer messages.'],
    method: [
      'Confirm the account, purchased product or plan, promised outcomes, first-value definition, onboarding owner, dates, and permitted handoff scope.',
      'Read the authorized signed handoff and current onboarding tracker. Separate contractual facts from sales notes and assumptions; reconcile conflicting dates or scope.',
      'Define a short sequence of observable milestones with a stable ID, accountable owner, due date, completion evidence, and blocker state for each.',
      'Mark a milestone complete only from an authorized product, customer, or owner record. Treat an invitation or setup email as an invitation, not adoption.',
      'Return the current register, missing inputs, at-risk dates, and one next decision for the owner to review.',
    ],
    evidence: 'Every completion and blocker cites a source and observation time; unobserved milestones remain pending or unknown.',
    boundary: 'Do not promise implementation dates, provision access, change account settings, or contact the customer without an authorized route and exact owner decision.',
    handoff: 'For New Customer to First Value, emit plain JSON `onboarding-milestone-register/v1` at the path supplied by the Crew step. Include account and handoff IDs, first-value goal, milestone state, source references, and owner. The Workflow validates it before Adoption consumes it.',
    deeperMethod: `## Milestone quality

Use the customer's actual promised scope; do not import a generic SaaS checklist as fact. Stable milestone IDs let the next run report changes rather than duplicate tasks. If the sales handoff is incomplete, show the exact missing decision. Customer outreach is a separate approved action with contact policy and prior-message checks.`,
  },
  {
    id: 'product-adoption-analyst', name: 'Product Adoption Analyst', icon: '📈', subcategory: 'Adoption',
    role: 'Evidence-led product adoption analyst',
    purpose: 'Determine whether a new customer reached the agreed first-value event using defined product evidence and explicit coverage limits.',
    firstResult: 'A first-value readout with an observed status, event definition, source records, and adoption gaps.',
    minimumInput: 'Customer/account reference, agreed first-value definition, observation window, relevant usage or setup export, and account owner.',
    optionalConnections: 'Product analytics or database, onboarding tracker, billing plan, and approved customer notes; a bounded event export works first.',
    exampleRequests: ['Did this customer reach the agreed first-value event? Show the exact evidence and gaps.', 'Compare onboarding milestones with observed product use for these accounts.'],
    method: [
      'Confirm account identity, product entitlement, event names, success threshold, timezone, and observation window with the owner.',
      'Read only the authorized usage or setup source. Map actor and account IDs carefully; exclude test, internal, duplicate, and wrong-tenant events.',
      'Compare each required event with the onboarding register and report observed, not-observed-with-coverage, or unknown when access or instrumentation is incomplete.',
      'Keep seat invites, sign-ins, feature use, and the agreed outcome separate. Do not turn activity into value without the owner-defined rule.',
      'Produce a dated readout with source IDs, denominators where relevant, gaps, and the next verification or intervention question.',
    ],
    evidence: 'Every observed event includes a source ID and time; a missing event is not a failure when the source has incomplete coverage.',
    boundary: 'Do not invent adoption scores, infer churn from sparse use, expose another tenant’s data, or message the customer from an analysis request.',
    handoff: 'For New Customer to First Value, emit plain JSON `first-value-readout/v1` linked to the validated onboarding register. Include observed event references and source coverage. The Workflow validates account binding and state before reporting or passing it to Health.',
    deeperMethod: `## First-value measurement

The event definition is customer-specific. Record the exact predicate and any exclusions. When instrumentation is missing, return **unknown** and a testable instrumentation task. On repeat runs compare the same account and rule version, and show when an event arrived late or was corrected.`,
  },
  {
    id: 'customer-health-coordinator', name: 'Customer Health Coordinator', icon: '💚', subcategory: 'Account health',
    role: 'Customer success account health coordinator',
    purpose: 'Combine first-value progress, support history, and renewal timing into a sourced account review and owner action.',
    firstResult: 'A current customer health brief with evidence, unknowns, risks, and an owner-reviewed next action.',
    minimumInput: 'Customer/account reference, first-value readout, account owner, health rules, support scope, and renewal date if relevant.',
    optionalConnections: 'CRM or customer success platform, support inbox, product usage, and billing or renewal source; bounded exports are sufficient for a first review.',
    exampleRequests: ['Review this account’s onboarding and support signals and tell me what needs a human follow-up.', 'Prepare a current account health brief without guessing a churn score.'],
    method: [
      'Confirm account identity, owner, lifecycle stage, review window, and the customer’s actual health and escalation definitions.',
      'Read validated first-value progress and authorized support, usage, relationship, and renewal records; preserve their source and freshness.',
      'Separate active blockers, recent resolved issues, usage gaps, and renewal timing. Label unsupported risk claims as hypotheses.',
      'Choose the next useful owner action, escalation or watch condition; include a due date and the evidence needed to close it.',
      'Return a concise brief and request owner review before customer outreach, CRM updates, or renewal commitments.',
    ],
    evidence: 'Each risk or positive signal cites a record and observation time; missing support or renewal access is an explicit limitation.',
    boundary: 'Do not present a model guess as a verified health score, promise a renewal outcome, change terms, or contact the customer without an approved route.',
    handoff: 'If included in New Customer to First Value, emit plain JSON `customer-health-brief/v1` linked to the validated first-value readout and onboarding register. Record each signal and its source, and keep all customer-facing actions pending review.',
    deeperMethod: `## Health decisions

Use the account owner's rules for severity and renewal timing. An unopened ticket, low login count, or unobserved event is not automatically churn risk. Separate data quality from customer risk. On repeat reviews, report meaningful changes and whether previous owner actions were completed.`,
  },
]

function checklist(spec: Specialist): string {
  const checks = [
    { id: 'identity', title: 'Confirm the Customer Success role', instructions: `Confirm whether ${spec.name} is this Crew's primary role or a supporting capability. Preserve an existing Crew identity and record the named owner.` },
    { id: 'skill', title: 'Verify the selected skill', instructions: `Confirm skills/${spec.id}/SKILL.md exists and ${spec.id} is selected for this Crew.` },
    { id: 'scope', title: 'Set account and outcome scope', instructions: `Confirm the customer/account, purchased scope, intended first result, owner, timeframe, and authorized source scope. Minimum input: ${spec.minimumInput}` },
    { id: 'access', title: 'Test source access', instructions: `Read one representative authorized source or export. ${spec.optionalConnections} Record exact account, date coverage, and missing access; a named provider is not a connected account.` },
    { id: 'definitions', title: 'Agree on evidence and action rules', instructions: 'Confirm milestone or metric definitions, identity mapping, source coverage, owner, customer contact policy, and what counts as observed completion. Mark unresolved definitions as blockers.' },
    { id: 'first_result', title: 'Produce the first result', instructions: `Produce ${spec.firstResult} ${spec.evidence} Use actual authorized data; a fictional example alone does not complete this check.` },
    { id: 'review', title: 'Review the first result', instructions: 'Show a representative result to the owner and record corrections, decision, and next action before completing setup.' },
    { id: 'delivery', title: 'Decide on delivery', optional: true, instructions: 'Choose chat-only or a separately authorized customer/CRM delivery route. Chat-only completes this decision. A draft, reminder, or health brief is not permission to message a customer or mutate an account.' },
    { id: 'recurrence', title: 'Decide on recurrence', optional: true, instructions: 'Choose manual-only or a separately reviewed schedule, authenticated trigger, callable function, or Automation. Manual-only completes this decision; test any configured route before activation.' },
  ]
  return `${JSON.stringify({ schema_version: 1, template_id: spec.id, template_version: 1, checks, completed_steps: [] }, null, 2)}\n`
}

function skill(spec: Specialist): string {
  return `---
name: ${spec.id}
description: ${spec.purpose}
---

# ${spec.name}

This skill provides the ${spec.name} capability. It can seed one Crew or be added to an existing Crew without changing that Crew's primary identity.

## Setup through chat

Read \`templates/${spec.id}/TEMPLATE_SETUP.json\` and \`templates/${spec.id}/SETUP.md\`. Verify each check before adding its ID to \`completed_steps\`. Preserve the checklist and previous progress. A chat-only or manual-only choice completes the corresponding optional check. Report verified, blocked, and next.

## First useful result

${spec.method.map((step, index) => `${index + 1}. ${step}`).join('\n')}

Deliver **${spec.firstResult}** ${spec.evidence}

${spec.deeperMethod}

## Automation handoff

${spec.handoff} The Crew step must supply schema fields and an output path; ask Builder to repair a route that omits them. A validator checks shape and references; a human checks source truth.

## Boundaries

${spec.boundary} Never copy another Crew's credentials. Installing this skill enables no schedule, trigger, function, Automation, customer message, CRM write, or account action.
`
}

function guide(spec: Specialist): string {
  return `# ${spec.name} setup

Template \`${spec.id}\` version 1. Progress lives in \`templates/${spec.id}/TEMPLATE_SETUP.json\` and is verified in Crew chat.

## First result

Provide ${spec.minimumInput} Ask: “${spec.exampleRequests[0]}”

Expected output: **${spec.firstResult}** ${spec.evidence}

## Source and connection choice

${spec.optionalConnections} A file or export supports a first read-only result. Select a live account only after the owner authorizes scope and a representative read succeeds.

## Optional recurring work

A schedule can repeat this Crew's own review. A separately proposed New Customer to First Value Automation coordinates distinct Crews; Builder must test handoffs manually before recurrence. ${spec.boundary}
`
}

export const customerSuccessSpecialists: readonly CrewTemplate[] = specialists.map(spec => {
  const base = `templates/${spec.id}`
  const skillPath = `skills/${spec.id}/SKILL.md`
  const setupGuidePath = `${base}/SETUP.md`
  const setupPath = `${base}/TEMPLATE_SETUP.json`
  return {
    id: spec.id, version: 1, category: 'Customer Success', subcategory: spec.subcategory, name: spec.name, icon: spec.icon,
    role: spec.role, purpose: spec.purpose, firstResult: spec.firstResult,
    minimumInput: spec.minimumInput, optionalConnections: spec.optionalConnections,
    exampleRequests: spec.exampleRequests, selectedSkills: [spec.id],
    setupPath, setupGuidePath, requiredFiles: [skillPath, setupGuidePath, setupPath],
    files: { [skillPath]: skill(spec), [setupGuidePath]: guide(spec), [setupPath]: checklist(spec) },
  }
})
