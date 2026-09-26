import type { CrewTemplate } from './crewTemplates'

export type MarketingSpecialistId =
  | 'competitor-intelligence-analyst'
  | 'campaign-performance-analyst'
  | 'funnel-analyst'
  | 'growth-experiment-planner'

type Specialist = {
  id: MarketingSpecialistId
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
  exampleInput: string
  workedExample: string
  inadequateExample: string
  inadequateReason: string
}

const specialists: readonly Specialist[] = [
  {
    id: 'competitor-intelligence-analyst', name: 'Competitor Intelligence Analyst', icon: '🔭', subcategory: 'Market signals',
    role: 'Competitor change and positioning analyst',
    purpose: 'Track a bounded competitor set for sourced changes that matter to a named offer, buyer, and decision owner.',
    firstResult: 'A dated change brief with exact competitor/product/plan, primary source and capture time, prior-versus-current evidence, relevance, uncertainty, and owner question.',
    minimumInput: 'Own offer and buyer, approved competitor/product list, watch topics, source scope, comparison baseline, time window, alert threshold, and owner.',
    optionalConnections: 'Public vendor pages and changelogs, approved research sources, CRM win/loss notes, and a saved source snapshot through browser or scoped MCP; a bounded URL set supports a read-only first brief.',
    exampleRequests: ['What changed in these competitors’ plans this month, and does it affect our offer?', 'Compare these claims against our approved positioning with sources and unknowns.'],
    method: [
      'Bind own offer, target buyer, geography, competitor product and plan IDs, topics, baseline date, source scope, and decision owner.',
      'Capture dated primary pages, changelogs or owner-supplied sales records; preserve the exact URL or record revision and avoid hidden or restricted material.',
      'Compare like-for-like plan, region, seat, usage and terms. Label source claims, observed changes, estimates, and unavailable evidence separately.',
      'Score relevance against the owner’s stated positioning or sales question; do not infer customer preference from a vendor page.',
      'Deliver a short change brief with implications to review, not an automatic pricing or messaging change.',
    ],
    evidence: 'Each change needs before/after source IDs and capture times, or an explicit baseline gap; a copied claim is not independent verification.',
    boundary: 'Do not scrape behind access controls, copy protected customer data, claim market share, change prices, publish claims, or contact a competitor from installation.',
    handoff: 'Campaign Signal to Reviewed Experiment may attach competitor-context/v1 to an experiment plan only if competitor product, market, source window and own offer are bound. Competitive context cannot substitute for campaign performance evidence.',
    repeatRule: 'Retain watched URL/product keys and prior captures, compare only changed facts, recheck expiring prices and terms, and suppress unchanged alerts.',
    exampleInput: 'Fictional input: offer=team analytics; buyer=20-seat SaaS operations team; competitors=A Pro and B Team; region=US; baseline=2026-09-01; watch=API export and annual price.',
    workedExample: 'Fictional output: A Pro now lists API export at 25 seats, versus 10 seats on archived 2026-09-01 page; current source=A pricing@2026-09-26; B Team price claim unchanged but contract term unavailable; relevance=medium for 20-seat buyers; owner question=check whether sales objections mention export thresholds.',
    inadequateExample: '“Competitor A is winning because it has better pricing. Cut our price today.”',
    inadequateReason: 'No matched plan, dated source, comparable terms, customer evidence, or owner approval supports the conclusion or action.',
  },
  {
    id: 'campaign-performance-analyst', name: 'Campaign Performance Analyst', icon: '📊', subcategory: 'Campaign measurement',
    role: 'Campaign measurement and decision analyst',
    purpose: 'Explain a bounded campaign’s spend, delivery, qualified response, and conversion changes from source records with comparable definitions.',
    firstResult: 'A campaign performance brief with campaign and account IDs, metric definitions, source coverage, baseline versus observed values, calculation, confounders, and one owner decision.',
    minimumInput: 'Campaign/platform IDs, account and attribution scope, period and time zone, spend and conversion sources, qualified-event definition, baseline, and owner.',
    optionalConnections: 'Ad, email, analytics, CRM, billing or product sources via scoped MCPs or exports; one platform export plus a conversion source supports a first analysis.',
    exampleRequests: ['Explain why this campaign’s qualified leads fell despite more clicks.', 'Compare this week’s campaign results with the agreed baseline and propose one next test.'],
    method: [
      'Bind campaign, account, channel, audience, currency, reporting period, time zone, attribution window, conversion event and owner.',
      'Read platform spend and delivery records plus downstream qualified events; record data lag, missing channels, bot/internal filters and identity join coverage.',
      'Calculate comparable rates and cost per qualified event with numerator, denominator, units and source IDs; never mix clicks, leads and meetings.',
      'Separate observed change from a causal explanation; list plausible confounders such as audience, budget, tracking or seasonality.',
      'Return one bounded recommendation with a measurement gap or experiment question for owner review; no budget or campaign write occurs.',
    ],
    evidence: 'Every metric needs an exact source, definition, period, denominator, currency and freshness; incomplete attribution must be visible.',
    boundary: 'Do not pause campaigns, change spend or targeting, claim incremental lift, or publish a result from mismatched periods or unverified conversions.',
    handoff: 'Campaign Signal to Reviewed Experiment emits campaign-performance-brief/v1 for Growth Experiment Planner after account, campaign, period, metric and coverage checks. A recommendation is not an approved experiment.',
    repeatRule: 'Use stable campaign and event IDs, preserve metric definitions and prior periods, account for reporting lag and deduplicate conversions before raising a new alert.',
    exampleInput: 'Fictional input: campaign=cmp-42 account=ads-7; week=2026-09-14; spend=USD 1,200; clicks=400; qualified demos=8; prior comparable week spend=USD 1,000, clicks=300, demos=12; CRM coverage=95%.',
    workedExample: 'Fictional output: qualified demos per click fell from 4.0% to 2.0%; cost per qualified demo rose from USD 83.33 to USD 150.00; source=ads:cmp-42@rev8 and crm:demo-events@rev4; causal reason unknown; tracking gap=5% unjoined; next=review landing-page and audience changes before testing a single message.',
    inadequateExample: '“The ad platform failed. Double the budget to fix conversions.”',
    inadequateReason: 'The conclusion ignores downstream CRM evidence and attribution gaps, invents causality, and proposes an unauthorized spend change.',
  },
  {
    id: 'funnel-analyst', name: 'Funnel Analyst', icon: '🪜', subcategory: 'SaaS conversion',
    role: 'Signup-to-paid funnel investigation analyst',
    purpose: 'Reconcile an authorized signup-to-paid cohort across event and billing sources, then identify a bounded drop-off for owner review.',
    firstResult: 'A versioned funnel observation with exact cohort, identity and event rules, stage counts, a comparable change or honest first baseline, coverage gaps, and one investigation question.',
    minimumInput: 'Product, tenant and funnel ID, ordered event definitions, identity and deduplication rule, current and optional comparable prior window, authorized event and paid-state sources, timezone, and decision owner.',
    optionalConnections: 'Product analytics, warehouse or event export and subscription billing/CRM records through scoped MCPs or files; a read-only first run can use two authorized, versioned exports.',
    exampleRequests: ['Where did our signup-to-paid funnel lose users this month?', 'Check whether mobile trial users reached paid status less often, using the same event and billing definitions.'],
    method: [
      'Freeze product, eligible cohort, ordered stages, event versions, identity join, deduplication, consent and timezone rules before comparing windows.',
      'Read exact event and paid-state sources; check freshness, identity join rate, billing lag, excluded internal/test users, and missing-source coverage.',
      'Count unique eligible users who reached each stage in order; keep stage counts monotone and compute rates from a named denominator.',
      'Compare equal windows on the same population and stage definitions; state observed differences and sample limits without inferring a cause from correlation.',
      'Ask the owner to review one likely friction point and a source-check or experiment question; do not change signup, billing or messaging.',
    ],
    evidence: 'Show cohort and source revisions, stage definitions, unique-user counts, denominator, identity coverage, paid-state evidence and an explicit unknown when a source is missing.',
    boundary: 'Do not call a trial a paid customer, infer purchase from a click, claim causality, inspect unconsented sessions, or modify pricing, checkout or campaigns from installation.',
    handoff: 'Funnel and Conversion Intelligence emits funnel-observation/v1 to Growth Experiment Planner only after exact product, cohort, stage rule, window, source and count checks. A drop-off is an observed signal, not an approved experiment.',
    repeatRule: 'Preserve funnel definition and source revisions, re-read the same cohort keys and later paid events, wait for billing lag, and avoid counting users twice or comparing changed event semantics as a trend.',
    exampleInput: 'Fictional input: product=ArborDesk; cohort=UK self-serve new accounts; baseline Aug 2-31 and current Sep 1-30; signup/activated/paid events v2; eligible users 1,000 versus 1,200; authorized product event and billing exports.',
    workedExample: 'Fictional output: baseline signup=300, activated=180, paid=60 of 1,000 eligible users; current signup=360, activated=180, paid=48 of 1,200. Paid/eligible fell from 6.0% to 4.0%; activated/signup fell from 60% to 50%. Identity join=98%, billing current through Oct 3, cause unknown; next=review activation flow change and tracking before one bounded test.',
    inadequateExample: '“Our checkout redesign caused churn, so raise prices and email everyone.”',
    inadequateReason: 'No exact cohort, ordered counts, billing proof, comparison rule or causal evidence supports the diagnosis or unauthorized actions.',
  },
  {
    id: 'growth-experiment-planner', name: 'Growth Experiment Planner', icon: '🧪', subcategory: 'Experiment design',
    role: 'Growth hypothesis and experiment decision planner',
    purpose: 'Turn one evidence-backed growth question into a bounded experiment with a decision rule, owner, safety limit, and measurement plan.',
    firstResult: 'A reviewed experiment plan with hypothesis, target population, baseline, primary and guardrail metrics, sample/stop rule, owner, implementation boundary, and follow-up decision.',
    minimumInput: 'Problem and source evidence, eligible audience, baseline and metric definitions, owner, change authority, duration or sample constraints, and guardrail.',
    optionalConnections: 'Analytics, CRM, campaign platform, feature flag, experimentation tool, and task tracker via scoped MCPs or exports; a read-only plan can start from a sourced brief.',
    exampleRequests: ['Turn this campaign performance gap into one testable message experiment.', 'Plan a landing-page test with a stop rule and what we will measure.'],
    method: [
      'Bind the source opportunity, customer goal, eligible unit, owner, current baseline and one changeable treatment.',
      'State a falsifiable hypothesis and distinguish observational evidence from a causal claim.',
      'Define primary success metric, denominator, observation window, minimum detectable practical effect and guardrail with baseline or baseline-first decision.',
      'Choose assignment or comparison method, sample and stop rules, exclusion and contamination checks, cost and rollback owner.',
      'Show the proposal for approval; activating a flag, editing a page or campaign, contacting customers and declaring success are separate steps.',
    ],
    evidence: 'An experiment plan needs the source brief ID, exact metric definition, baseline evidence or an explicit baseline-first phase, and a predeclared decision rule.',
    boundary: 'Do not launch an experiment, change budget, publish variants, send messages, or claim a winner without owner approval and comparable outcome evidence.',
    handoff: 'Campaign Signal to Reviewed Experiment consumes campaign-performance-brief/v1 and emits growth-experiment-plan/v1. Funnel and Conversion Intelligence consumes funnel-observation/v1 and emits funnel-experiment-plan/v1. Activation and Retention Intelligence consumes a mature cohort-retention-observation/v1 and emits retention-experiment-plan/v1; pending_maturity must stop before planning. In every route, freeze the upstream ID, metric denominator, owner, guardrail and pending launch state; optional context never provides a measured baseline.',
    repeatRule: 'Retain experiment ID, policy revision, assignment and decision rule; re-read outcome and guardrail data for the agreed window, record null results, and avoid repeated launches.',
    exampleInput: 'Fictional input: source campaign brief=brief-42; demos/click dropped from 4.0% to 2.0%; audience=US operations leaders; owner=growth-lead; landing-page control=rev4.',
    workedExample: 'Fictional output: hypothesis=message mismatch after the audience expansion; test one headline variant against rev4 for eligible clicks; primary=qualified demos/eligible click in CRM, guardrail=unsubscribes and spend cap; sample target=owner-reviewed calculator result; stop after planned window or guardrail breach; publish=none pending approval.',
    inadequateExample: '“Try a new headline and see if it wins.”',
    inadequateReason: 'There is no audience, baseline, denominator, sample or stop rule, guardrail, owner, or evidence standard for a decision.',
  },
]

function checklist(spec: Specialist): string {
  const checks = [
    { id: 'identity', title: 'Confirm marketing role and owner', instructions: 'Confirm whether ' + spec.name + ' is this Crew’s primary role or an added capability. Preserve existing identity and name the accountable owner.' },
    { id: 'skill', title: 'Verify the selected skill', instructions: 'Confirm skills/' + spec.id + '/SKILL.md exists and ' + spec.id + ' is selected for this Crew.' },
    { id: 'scope', title: 'Set exact job and measurement scope', instructions: 'Record offer, market, campaign or experiment identity, period and time zone, owner, allowed sources, and first job. Minimum input: ' + spec.minimumInput },
    { id: 'access', title: 'Probe representative source access', instructions: 'Read one actual authorized source or export. Record stable IDs, revisions, timestamps, denominator or baseline coverage, and missing access. ' + spec.optionalConnections },
    { id: 'policy', title: 'Confirm evidence and action boundary', instructions: 'Record comparable metric definitions, citation and privacy rules, approval owner, alert or decision threshold, and exact action boundary. ' + spec.boundary },
    { id: 'first_result', title: 'Produce first sourced result', instructions: 'Use real authorized input to produce ' + spec.firstResult + ' ' + spec.evidence + ' Fictional examples do not complete this check.' },
    { id: 'review', title: 'Review result with owner', instructions: 'Show calculations, source coverage, uncertainty, proposed decision, and owner correction. Record the review decision.' },
    { id: 'delivery', title: 'Choose read-only or action route', optional: true, instructions: 'Choose read-only chat or a separately authorized campaign, page, experiment, report, or contact write route. Read-only completes this choice.' },
    { id: 'recurrence', title: 'Choose repeat rule', optional: true, instructions: 'Choose manual-only or a reviewed event/schedule with stable IDs, deduplication, cost, and notifications. Manual-only completes this choice. ' + spec.repeatRule },
  ]
  return JSON.stringify({ schema_version: 1, template_id: spec.id, template_version: 1, checks, completed_steps: [] }, null, 2) + '\n'
}

function skill(spec: Specialist): string {
  return [
    '---', 'name: ' + spec.id, 'description: ' + spec.purpose, '---', '',
    '# ' + spec.name, '',
    'This skill gives one Crew the ' + spec.name + ' capability. It can seed a new Crew or be added to a compatible Crew. Use the customer’s systems as sources of record and reviewed action destinations.', '',
    '## Setup through chat', '',
    'Read templates/' + spec.id + '/TEMPLATE_SETUP.json and templates/' + spec.id + '/SETUP.md. Verify each check with actual customer records before adding its ID to completed_steps. Preserve progress and report verified, blocked, and next. Chat-only and manual-only are valid optional decisions.', '',
    '## First useful result', '',
    ...spec.method.map((step, index) => String(index + 1) + '. ' + step), '',
    'Deliver **' + spec.firstResult + '** ' + spec.evidence, '',
    '## Follow-through', '', spec.repeatRule, '',
    '## Fictional worked example', '', spec.exampleInput, '', spec.workedExample, '',
    'Inadequate: ' + spec.inadequateExample + ' Reason: ' + spec.inadequateReason, '',
    '## Automation handoff', '',
    spec.handoff + ' Builder must bind an exact artifact path and schema and validate before another Crew consumes it. Re-read current source state.', '',
    '## Boundaries', '',
    spec.boundary + ' Installation enables no schedule, trigger, function, Automation, external write, notification, or customer message. Never copy another Crew’s credentials.', '',
  ].join('\n')
}

function guide(spec: Specialist): string {
  return [
    '# ' + spec.name + ' setup', '',
    'Template ' + spec.id + ' version 1. Progress lives in templates/' + spec.id + '/TEMPLATE_SETUP.json and is verified in Crew chat.', '',
    '## First result', '',
    'Provide ' + spec.minimumInput + ' Ask: “' + spec.exampleRequests[0] + '”', '',
    'Expected output: **' + spec.firstResult + '** ' + spec.evidence, '',
    '## Fictional example and failure', '', spec.exampleInput, '', spec.workedExample, '',
    'Inadequate: ' + spec.inadequateExample + ' Reason: ' + spec.inadequateReason, '',
    '## Source and connection choice', '',
    spec.optionalConnections + ' Start with one representative authorized read or export. Record exact source, policy and owner IDs and missing coverage.', '',
    '## Automation and recurring work', '',
    'A multi-Crew Automation is useful when ownership, access or review boundaries differ. Builder must inspect existing Crews, verify handoffs, run one manual case, and record owner-approved run policy. ' + spec.repeatRule + ' ' + spec.boundary, '',
  ].join('\n')
}

export const marketingSpecialists: readonly CrewTemplate[] = specialists.map(spec => {
  const base = 'templates/' + spec.id
  const skillPath = 'skills/' + spec.id + '/SKILL.md'
  const setupGuidePath = base + '/SETUP.md'
  const setupPath = base + '/TEMPLATE_SETUP.json'
  return {
    id: spec.id, version: 1, category: 'Marketing', subcategory: spec.subcategory, name: spec.name, icon: spec.icon,
    role: spec.role, purpose: spec.purpose, firstResult: spec.firstResult,
    minimumInput: spec.minimumInput, optionalConnections: spec.optionalConnections,
    exampleRequests: spec.exampleRequests, selectedSkills: [spec.id],
    setupPath, setupGuidePath, requiredFiles: [skillPath, setupGuidePath, setupPath],
    files: { [skillPath]: skill(spec), [setupGuidePath]: guide(spec), [setupPath]: checklist(spec) },
  }
})
