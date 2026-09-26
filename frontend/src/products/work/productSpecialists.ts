import type { CrewTemplate } from './crewTemplates'

export type ProductSpecialistId = 'product-feedback-coordinator'

const id: ProductSpecialistId = 'product-feedback-coordinator'
const name = 'Product Feedback Coordinator'
const role = 'Customer feedback to product decision coordinator'
const purpose = 'Review a source-backed customer feedback theme against current product issues, usage evidence and owner priorities, then prepare a traceable product decision.'
const firstResult = 'A product decision brief with theme and feedback IDs, affected scope, existing issue links, evidence gaps, owner decision options and next verification.'
const minimumInput = 'Validated feedback theme or authorized feedback records, product and account scope, current issue or roadmap source, product owner and decision criteria.'
const optionalConnections = 'Helpdesk/survey exports, product usage records, issue tracker and roadmap through scoped MCPs or files. A read-only first decision uses authorized exports.'
const examples = [
  'Review this onboarding feedback theme against current product issues and propose the next owner decision.',
  'Does this feedback support a new product task, or is it already covered? Show the exact evidence and gaps.',
] as const
const boundary = 'Do not promise a feature, rank a roadmap by mention count alone, create or close an issue, change priority, publish a plan, or message a customer without owner approval and a separate route.'
const repeatRule = 'Re-read current issue, roadmap and product evidence; retain theme and issue IDs, preserve accepted and declined decisions, and report what changed instead of opening duplicate work.'

const checks = [
  { id: 'identity', title: 'Confirm product owner and Crew identity', instructions: 'Confirm whether this capability seeds a Product Crew or extends a compatible Crew. Preserve existing identity and name the accountable product decision owner.' },
  { id: 'skill', title: 'Verify selected skill', instructions: `Confirm skills/${id}/SKILL.md exists and ${id} is selected for this Crew.` },
  { id: 'scope', title: 'Bind product and feedback scope', instructions: `Record tenant, product, feature or journey, period, affected segment, theme ID and owner. Minimum input: ${minimumInput}` },
  { id: 'access', title: 'Probe authorized product sources', instructions: `Read one real feedback theme plus current issue/roadmap state. Record source IDs, timestamps, permissions and gaps. ${optionalConnections}` },
  { id: 'policy', title: 'Agree on decision and privacy rules', instructions: `Confirm how the owner weighs prevalence, severity, strategic fit, current commitments and counterexamples. Record privacy limits and action authority. ${boundary}` },
  { id: 'first_result', title: 'Produce a sourced first decision brief', instructions: `Produce ${firstResult} Cite each claim and denominator. A fictional example does not complete this check.` },
  { id: 'review', title: 'Review with product owner', instructions: 'Show representative and contrary evidence, exact existing issue match, unknowns, options and recommended next investigation. Record the owner decision or correction.' },
  { id: 'delivery', title: 'Choose read-only or issue route', optional: true, instructions: 'Read-only chat completes this choice. Any issue creation, status change or customer communication needs a separately approved exact-object route and provider receipt.' },
  { id: 'recurrence', title: 'Choose manual or recurring review', optional: true, instructions: `Manual-only completes this choice. A schedule or authenticated feedback event requires reviewed source scope, stable theme IDs, deduplication, cost and notification rules. ${repeatRule}` },
]

const skill = [
  '---', `name: ${id}`, `description: ${purpose}`, '---', '', `# ${name}`, '',
  'This skill prepares a product owner decision from current, authorized feedback and product records. It does not authorize a roadmap or issue-tracker change.', '',
  '## Setup in chat', '',
  `Read templates/${id}/TEMPLATE_SETUP.json and templates/${id}/SETUP.md. Verify each check on real authorized records before adding its ID to completed_steps. Preserve existing Crew identity and setup progress. Report verified, blocked and next.`, '',
  '## First useful result', '',
  '1. Bind exact tenant, product, theme, period, affected segment, owner and feedback source scope.',
  '2. Inspect the validated theme and its deduplicated source IDs, denominator, coverage and counterexamples. Separate reported pain from an observed product defect.',
  '3. Re-read current issue, roadmap, release and product-usage records. Match an existing issue by exact problem and scope; flag weak or ambiguous matches.',
  '4. Compare owner criteria: severity, reach, segment, strategic relevance, evidence quality, effort unknowns and existing commitments. A count alone is not a priority score.',
  '5. Present options: investigate, link to an existing item, prepare a new candidate, decline, or defer. Record the product owner decision and evidence needed next.', '',
  `Deliver **${firstResult}** Every factual claim needs feedback or product source IDs, observed time, coverage and an owner decision state.`, '',
  '## Fictional worked example', '',
  'Fictional input: theme F-9 reports confusing invite permissions from 9 of 40 surveyed respondents plus 3 linked cases, all from one segment. Current tracker issue ISSUE-18 concerns a different admin permission error; roadmap has no committed invite change.', '',
  'Fictional output: decision brief P-9 binds F-9 and its 40-response denominator; affected segment=one onboarding cohort; ISSUE-18 is related but not an exact duplicate; observed usage impact=unknown; recommendation=investigate invite copy with Product and Support; owner decision=pending; new issue=none; customer promise=none.', '',
  'Inadequate: “Everyone has this bug. ISSUE-18 fixes it; promise customers the change next week.” Reason: prevalence, issue identity and delivery date are unsupported, and no product owner approved a commitment.', '',
  '## Later run', '', repeatRule, '',
  '## Boundaries', '', `${boundary} Installation enables no schedule, trigger, function, Automation, issue write or customer message.`, '',
].join('\n')
const guide = [
  `# ${name} setup`, '', `Template ${id} version 1. Progress lives in templates/${id}/TEMPLATE_SETUP.json and is verified through Crew chat.`, '',
  '## First result', '', `Provide ${minimumInput} Ask: “${examples[0]}”`, '', `Expected: **${firstResult}** Cite feedback scope, denominator, representative and contrary cases, existing issue state, and owner decision.`, '',
  '## Sources and boundaries', '', `${optionalConnections} Start with one authorized theme and current issue/roadmap read. ${boundary}`, '',
  '## Repeat policy', '', `${repeatRule} Builder must test one real case and the owner must review any action or recurrence before activation.`, '',
].join('\n')

export const productSpecialists: readonly CrewTemplate[] = [{
  id, version: 1, category: 'Product', subcategory: 'Feedback decisions', name, icon: '🧭', role, purpose,
  firstResult, minimumInput, optionalConnections, exampleRequests: examples, selectedSkills: [id],
  setupPath: `templates/${id}/TEMPLATE_SETUP.json`,
  setupGuidePath: `templates/${id}/SETUP.md`,
  requiredFiles: [`skills/${id}/SKILL.md`, `templates/${id}/SETUP.md`, `templates/${id}/TEMPLATE_SETUP.json`],
  files: {
    [`skills/${id}/SKILL.md`]: skill,
    [`templates/${id}/SETUP.md`]: guide,
    [`templates/${id}/TEMPLATE_SETUP.json`]: JSON.stringify({ schema_version: 1, template_id: id, template_version: 1, checks, completed_steps: [] }, null, 2) + '\n',
  },
}]
