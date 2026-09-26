import type { CrewTemplate } from './crewTemplates'

export type EngineeringSpecialistId = 'incident-investigator' | 'engineering-delivery-coordinator' | 'performance-investigator' | 'cloud-cost-analyst'

type Specialist = {
  id: EngineeringSpecialistId
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
  repeatRule: string
}

const specialists: readonly Specialist[] = [
  {
    id: 'incident-investigator', name: 'Incident Investigator', icon: '🚨', subcategory: 'Reliability',
    role: 'Service incident investigator and coordinator',
    purpose: 'Connect alerts, telemetry, deploys, and owner updates into an evidence-linked incident timeline and next-action queue.',
    firstResult: 'A dated incident timeline, labeled hypotheses, affected-service scope, and owner-reviewed next actions.',
    minimumInput: 'Incident or alert ID, affected service, time window and timezone, authorized telemetry or exports, on-call owner, and escalation policy.',
    optionalConnections: 'Incident manager, monitoring and logs, deployment history, issue tracker, and approved team channel; bounded exports support a first read-only investigation.',
    exampleRequests: ['Investigate this production alert and show the timeline, likely causes, and missing evidence.', 'Compare this incident with the recent deploy and prepare the next on-call decision.'],
    method: [
      'Confirm incident ID, service, impact window, timezone, current severity, owner, and permitted investigation scope.',
      'Read current alert, metrics, logs, deploy, and status records. Preserve source IDs and event times; distinguish event time from ingestion time.',
      'Construct a timeline of observed facts. Label correlations and possible causes as hypotheses until evidence supports them.',
      'Identify missing telemetry, conflicting reports, affected customers or services only when authorized evidence supports the claim.',
      'Return a next-action queue with owner, urgency, evidence needed, and the escalation or communication review point.',
    ],
    evidence: 'Cite the alert, telemetry, and deploy record behind each material claim; mark impact and cause unknown when coverage is insufficient.',
    boundary: 'Do not restart a service, roll back a deploy, change an incident severity, or publish an external status update without an authorized owner decision and tool route.',
    handoff: 'A recovery Workflow can consume a bounded `incident-investigation/v1` artifact containing incident ID, service, evidence references, hypotheses, owner decisions, and proposed actions. Verify the same incident and service before a remediation step uses it.',
    repeatRule: 'On another run, re-read the incident and action state, append only new evidence, and correct disproven hypotheses. Do not duplicate incident tickets or page owners twice.',
  },
  {
    id: 'engineering-delivery-coordinator', name: 'Engineering Delivery Coordinator', icon: '🧭', subcategory: 'Delivery',
    role: 'Engineering delivery and release blocker coordinator',
    purpose: 'Trace blocked work across issues, PRs, CI, and deployments; chase the next owner decision and verify release state.',
    firstResult: 'A sourced blocker ledger linking issue, change, CI result, deployment, owner, and next action.',
    minimumInput: 'Issue or release scope, repository and CI records, deployment source, owner map, release policy, and review window.',
    optionalConnections: 'Issue tracker, source host, CI, deployment platform, and team channel; issue and build exports work for a first read-only ledger.',
    exampleRequests: ['Which changes are blocking this release, who owns each blocker, and what is the next action?', 'Trace this issue through PR, CI, and deployment, and tell me what is still unverified.'],
    method: [
      'Confirm release or issue scope, cutoff time, expected destination environment, ownership, and release policy.',
      'Join issue, PR, CI, and deployment records with exact IDs and commit SHA; flag ambiguous links instead of inferring them from similar titles.',
      'Separate code complete, review complete, CI passed, deployed, and verified states. Find stale failures, missing approvals, and unowned handoffs.',
      'Prepare a blocker ledger with each state, source link, accountable owner, next action, due time, and needed proof.',
      'Ask the release owner to approve any ticket update, notification, rerun, merge, or deployment action.',
    ],
    evidence: 'A merged PR is not a deployed change; a green CI run is not production verification. Cite exact IDs, SHA, environment, and observation time.',
    boundary: 'Do not merge, rerun CI, deploy, update an issue, or send a team message without an authorized route and reviewed action.',
    handoff: 'A release Workflow can consume `engineering-blocker-ledger/v1` with stable issue and change IDs, SHA, environment, CI and deployment evidence, owner, and approval state. QA can add independent verification evidence.',
    repeatRule: 'Reconcile the same stable issue and change IDs on later runs, close only blockers with observed resolution, and avoid duplicate follow-ups.',
  },
  {
    id: 'performance-investigator', name: 'Performance Investigator', icon: '⏱️', subcategory: 'Performance',
    role: 'Application performance regression investigator',
    purpose: 'Compare a reported regression with traces, measurements, and recent changes to produce a reproducible diagnosis and owner action.',
    firstResult: 'A regression brief with comparable baseline and current measurements, reproduction conditions, likely bottleneck, and verification plan.',
    minimumInput: 'Affected route or service, baseline and current window, environment, performance budget, representative traces or test results, and owner.',
    optionalConnections: 'APM, RUM or browser performance tools, CI performance runs, deployment history, and issue tracker; supplied traces or reports support a first read-only result.',
    exampleRequests: ['Why did this API route get slower after the release? Show comparable measurements and likely bottlenecks.', 'Investigate this checkout page regression and prepare a retest plan.'],
    method: [
      'Agree on the user journey or endpoint, metric, percentile, units, budget, environment, traffic segment, and baseline/current windows.',
      'Check measurement coverage and comparable load, device, cache, region, and release conditions before calculating a change.',
      'Correlate slow spans, resource timing, errors, and deploys; keep correlation separate from a proven cause.',
      'Return one or more reproducible hypotheses with exact measurement references, owner, proposed experiment, and rollback or risk consideration.',
      'Define a retest using the same metric and comparable conditions; report unknown if the available observations cannot support a conclusion.',
    ],
    evidence: 'Show baseline and current values, sample sizes and windows when available, source links, and any confounders; never claim an improvement before a comparable retest.',
    boundary: 'Do not alter production configuration, change a performance budget, deploy a fix, or treat one synthetic run as a customer-wide outcome without owner review.',
    handoff: 'A performance Workflow can consume `performance-regression/v1` with route, metric definition, baseline/current evidence, hypotheses, proposed change, and retest criteria. QA or Engineering verifies the same route and environment.',
    repeatRule: 'Compare later runs to the recorded baseline rule; note instrumentation or traffic changes and close the regression only after a comparable retest.',
  },
  {
    id: 'cloud-cost-analyst', name: 'Cloud Cost Analyst', icon: '☁️', subcategory: 'FinOps',
    role: 'Cloud spend and cost exception analyst',
    purpose: 'Explain cloud cost changes across billing and usage systems, route ownership, and prepare risk-checked savings decisions.',
    firstResult: 'A cost-change brief with reconciled period comparison, affected services, accountable owners, and reviewable savings candidates.',
    minimumInput: 'Billing period, currency, authorized cost and usage export, account or project map, allocation rules, budget, and service owner.',
    optionalConnections: 'Cloud billing, observability, inventory or tagging, issue tracker, and finance records; a bounded billing export supports the first read-only comparison.',
    exampleRequests: ['What caused our cloud bill to rise this month, and which changes need an owner review?', 'Find safe savings candidates for this service with cost and reliability evidence.'],
    method: [
      'Confirm account and service scope, currency, billing calendar, allocation policy, committed discounts, and comparison periods.',
      'Reconcile billed and usage data by service, region, resource, and tag; separate price, usage, one-time charge, and allocation changes.',
      'Check ownership and service criticality before suggesting a rightsizing, schedule, storage, or commitment change.',
      'Produce a ranked candidate list with estimated savings range, source calculation, operational risk, owner, and validation experiment.',
      'Request review before changing infrastructure or purchasing commitments; verify realized savings against the same billing rule later.',
    ],
    evidence: 'Show the cost arithmetic, period coverage, currency, discounts and exclusions. An estimate is not realized savings.',
    boundary: 'Do not stop resources, resize infrastructure, purchase commitments, or book savings in finance without owner approval and a verified change route.',
    handoff: 'A FinOps Workflow can consume `cloud-cost-review/v1` with billing scope, source IDs, change decomposition, candidate estimates, risk, owner, approval, and post-change verification rule.',
    repeatRule: 'Track candidate IDs and owner decisions; compare actual billed results after an approved change and report when savings cannot yet be observed.',
  },
]

function checklist(spec: Specialist): string {
  const checks = [
    { id: 'identity', title: 'Confirm the Engineering role', instructions: `Confirm whether ${spec.name} is this Crew's primary role or a supporting capability. Preserve an existing Crew identity and name the accountable owner.` },
    { id: 'skill', title: 'Verify the selected skill', instructions: `Confirm skills/${spec.id}/SKILL.md exists and ${spec.id} is selected for this Crew.` },
    { id: 'scope', title: 'Set the job and evidence scope', instructions: `Record the first job, owner, time window, environment or account scope, and authorization. Minimum input: ${spec.minimumInput}` },
    { id: 'access', title: 'Test source access', instructions: `Read one representative authorized record or export and record exact ID, freshness, and coverage. ${spec.optionalConnections} A provider name alone is not access.` },
    { id: 'join_rules', title: 'Verify identity and decision rules', instructions: 'Confirm how records join across tools, the owner map, metric or status definitions, escalation threshold, and what incomplete coverage means. Keep ambiguous joins unresolved.' },
    { id: 'first_result', title: 'Produce the first result', instructions: `Use actual authorized evidence to produce ${spec.firstResult} ${spec.evidence} An illustrative fixture does not complete this check.` },
    { id: 'review', title: 'Review the result and next action', instructions: 'Show the sourced result, unknowns, proposed next action, owner, and exact approval boundary. Record owner corrections and decision.' },
    { id: 'delivery', title: 'Choose action and delivery routes', optional: true, instructions: `Choose read-only chat or separately authorized writes and notifications. Read-only chat completes this decision. ${spec.boundary}` },
    { id: 'recurrence', title: 'Choose recurrence and repeat behavior', optional: true, instructions: `Choose manual-only or a reviewed schedule, trigger, function, or Automation. Manual-only completes this decision. ${spec.repeatRule} Test a configured route before activation.` },
  ]
  return `${JSON.stringify({ schema_version: 1, template_id: spec.id, template_version: 1, checks, completed_steps: [] }, null, 2)}\n`
}

function skill(spec: Specialist): string {
  return `---
name: ${spec.id}
description: ${spec.purpose}
---

# ${spec.name}

This skill gives one Crew the ${spec.name} capability. It can seed a new Crew or be added to a compatible existing Crew without replacing its identity. Use the customer's existing systems as sources and approved action destinations.

## Setup through chat

Read \`templates/${spec.id}/TEMPLATE_SETUP.json\` and \`templates/${spec.id}/SETUP.md\`. Verify each check with the customer's real scope before adding its ID to \`completed_steps\`. Preserve previous progress. Chat-only and manual-only are valid choices for the corresponding optional checks. Report verified, blocked, and next.

## First useful result

${spec.method.map((step, index) => `${index + 1}. ${step}`).join('\n')}

Deliver **${spec.firstResult}** ${spec.evidence}

## Follow-through

${spec.repeatRule}

## Automation handoff

${spec.handoff} The Workflow must provide the artifact schema and output path. Ask Builder to repair a route that omits them. Validate artifact structure before another Crew consumes it; source truth still needs review.

## Boundaries

${spec.boundary} Installing this skill enables no schedule, trigger, function, Automation, notification, or system write. Never copy another Crew's credentials.
`
}

function guide(spec: Specialist): string {
  return `# ${spec.name} setup

Template \`${spec.id}\` version 1. Progress lives in \`templates/${spec.id}/TEMPLATE_SETUP.json\` and is verified in Crew chat.

## First result

Provide ${spec.minimumInput} Ask: “${spec.exampleRequests[0]}”

Expected output: **${spec.firstResult}** ${spec.evidence}

## Source and connection choice

${spec.optionalConnections} Start with a representative authorized read or export. Record source IDs and coverage before claiming a connected result. Select live accounts only within the owner's approved scope.

## Optional recurring work

A schedule can repeat this Crew's own review. A separate Automation can coordinate multiple Crews only after Builder verifies bindings, handoffs, a manual route, and the owner-approved run policy. ${spec.repeatRule} ${spec.boundary}
`
}

export const engineeringSpecialists: readonly CrewTemplate[] = specialists.map(spec => {
  const base = `templates/${spec.id}`
  const skillPath = `skills/${spec.id}/SKILL.md`
  const setupGuidePath = `${base}/SETUP.md`
  const setupPath = `${base}/TEMPLATE_SETUP.json`
  return {
    id: spec.id, version: 1, category: 'Engineering', subcategory: spec.subcategory, name: spec.name, icon: spec.icon,
    role: spec.role, purpose: spec.purpose, firstResult: spec.firstResult,
    minimumInput: spec.minimumInput, optionalConnections: spec.optionalConnections,
    exampleRequests: spec.exampleRequests, selectedSkills: [spec.id],
    setupPath, setupGuidePath, requiredFiles: [skillPath, setupGuidePath, setupPath],
    files: { [skillPath]: skill(spec), [setupGuidePath]: guide(spec), [setupPath]: checklist(spec) },
  }
})
