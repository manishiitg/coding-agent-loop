export type PlaybookCatalogItem = {
  id: string
  title: string
  description: string
  version: string
  changelog?: PlaybookChangelogEntry[]
  category: string
  order: number
  inputCount: number
  toolCount: number
  teamScope?: 'small_team'
  setupPrompt?: string
  setupInputs?: PlaybookSetupInput[]
  requiredCapabilities?: string[]
  recommendedTools?: PlaybookRecommendedTool[]
  pulseFocus?: PlaybookPulseFocus[]
  outputs?: string[]
  agentSlots?: PlaybookAgentSlot[]
  handoffs?: PlaybookHandoff[]
  setupChecks?: string[]
}

export type PlaybookAgentSlot = { id: string; agent_playbook_id: string; accepts?: string[]; required: boolean; output: string }
export type PlaybookHandoff = { id: string; from: string; to: string; artifact_type: string; required: boolean }

export type PlaybookChangelogEntry = {
  version: string
  summary: string
}

export type PlaybookSetupInput = {
  id: string
  label: string
  required: boolean
  default?: unknown
}

export type PlaybookRecommendedTool = {
  id: string
  name: string
  type: string
  purpose: string
  capability: string
  optional: boolean
}

export type PlaybookPulseFocus = {
  module: 'strategic_review'
  label: string
  focus_areas: string[]
  review_when: string[]
}

// Read-only catalog projection of the first-party playbook manifests. The API
// slice will replace this projection when installation records are introduced.
export const PLAYBOOK_CATALOG: readonly PlaybookCatalogItem[] = [
  { id: 'order-exception-to-resolution', title: 'Order Exception to Resolution', description: 'Connect an order or fulfillment problem to a return or refund request, then prepare an approved action and verify the resulting store and customer state.', version: '0.1.1', category: 'Shopify', order: 1, inputCount: 5, toolCount: 5, teamScope: 'small_team', agentSlots: [
    { id: 'store_operations', agent_playbook_id: 'store-operations-coordinator', required: true, output: 'store-order-exception/v1' },
    { id: 'returns_refunds', agent_playbook_id: 'returns-refunds-coordinator', required: true, output: 'return-resolution-review/v1' },
  ], handoffs: [
    { id: 'order-to-returns', from: 'store_operations', to: 'returns_refunds', artifact_type: 'store-order-exception/v1', required: true },
  ], setupChecks: ['goal_owner', 'store_order_scope', 'policy_money_rules', 'team_bindings', 'source_access', 'handoff_contract', 'plan_review', 'test_run', 'action_ledger', 'activation_choice'] },
  { id: 'storefront-opportunity-to-verified-change', title: 'Storefront Opportunity to Verified Change', description: 'Find a product-page or variant issue, prepare a merchant-reviewed catalog correction, then verify the shipped change and comparable shopper evidence.', version: '0.1.0', category: 'Shopify', order: 2, inputCount: 5, toolCount: 5, teamScope: 'small_team', agentSlots: [
    { id: 'growth', agent_playbook_id: 'shopify-growth-analyst', required: true, output: 'shopify-growth-opportunity/v1' },
    { id: 'catalog', agent_playbook_id: 'catalog-merchandising-analyst', required: true, output: 'catalog-change-review/v1' },
  ], handoffs: [
    { id: 'growth-to-catalog', from: 'growth', to: 'catalog', artifact_type: 'shopify-growth-opportunity/v1', required: true },
  ], setupChecks: ['goal_owner', 'store_market_scope', 'metric_rule', 'team_bindings', 'source_access', 'handoff_contract', 'plan_review', 'test_run', 'change_ledger', 'activation_choice'] },
  { id: 'incident-to-verified-recovery', title: 'Incident to Verified Recovery', description: 'Coordinate an incident investigation, an owned fix or rollback decision, and evidence that the affected service recovered.', version: '0.1.0', category: 'Engineering', order: 1, inputCount: 5, toolCount: 5, teamScope: 'small_team', agentSlots: [
    { id: 'investigation', agent_playbook_id: 'incident-investigator', required: true, output: 'incident-investigation/v1' },
    { id: 'delivery', agent_playbook_id: 'engineering-delivery-coordinator', required: true, output: 'engineering-blocker-ledger/v1' },
  ], handoffs: [
    { id: 'investigation-to-delivery', from: 'investigation', to: 'delivery', artifact_type: 'incident-investigation/v1', required: true },
  ], setupChecks: ['goal_owner', 'incident_scope', 'source_access', 'recovery_rule', 'team_bindings', 'handoff_contract', 'plan_review', 'test_run', 'action_ledger', 'activation_choice'] },
  { id: "basic-browser-setup", title: "Basic Browser Setup", description: "Configure browser access and Playwright, save verified locators, capture video plus console/network evidence, and establish reporting.", version: "0.9.0", changelog: [{"version":"0.9.0","summary":"Scopes setup to small engineering teams and defaults to one workflow unless access or lifecycle boundaries require a split."},{"version":"0.8.0","summary":"Defaults human approvals to durable asynchronous review: preparation ends pending review and a later action validates the recorded decision and current state."},{"version":"0.7.0","summary":"Adds an explicit inspect-first discovery step, focused customer questions, recorded user direction, and a clear boundary that installation is not approval."}], category: "Browser QA", order: 1, inputCount: 7, toolCount: 4, teamScope: "small_team" },
  { id: "authentication-session-validation", title: "Authentication and Session Validation", description: "Validate login, logout, MFA, recovery, expiry, refresh, and invalid-session behavior with durable evidence.", version: "0.4.0", changelog: [{"version":"0.4.0","summary":"Scopes setup to small engineering teams and defaults to one workflow unless access or lifecycle boundaries require a split."},{"version":"0.3.0","summary":"Defaults human approvals to durable asynchronous review: preparation ends pending review and a later action validates the recorded decision and current state."},{"version":"0.2.0","summary":"Adds an explicit inspect-first discovery step, focused customer questions, recorded user direction, and a clear boundary that installation is not approval."}], category: "Browser QA", order: 2, inputCount: 6, toolCount: 3, teamScope: "small_team" },
  { id: "role-permission-validation", title: "Role and Permission Validation", description: "Validate page, action, and data permissions across roles, ownership states, and tenant boundaries.", version: "0.4.0", changelog: [{"version":"0.4.0","summary":"Scopes setup to small engineering teams and defaults to one workflow unless access or lifecycle boundaries require a split."},{"version":"0.3.0","summary":"Defaults human approvals to durable asynchronous review: preparation ends pending review and a later action validates the recorded decision and current state."},{"version":"0.2.0","summary":"Adds an explicit inspect-first discovery step, focused customer questions, recorded user direction, and a clear boundary that installation is not approval."}], category: "Browser QA", order: 3, inputCount: 5, toolCount: 3, teamScope: "small_team" },
  { id: "critical-journey-validation", title: "Critical Journey Validation", description: "Reuse browser setup and locator helpers to run critical journeys, investigate failures, and retain video plus console/network evidence.", version: "0.9.0", changelog: [{"version":"0.9.0","summary":"Scopes setup to small engineering teams and defaults to one workflow unless access or lifecycle boundaries require a split."},{"version":"0.8.0","summary":"Defaults human approvals to durable asynchronous review: preparation ends pending review and a later action validates the recorded decision and current state."},{"version":"0.7.0","summary":"Adds an explicit inspect-first discovery step, focused customer questions, recorded user direction, and a clear boundary that installation is not approval."}], category: "Browser QA", order: 4, inputCount: 5, toolCount: 6, teamScope: "small_team" },
  { id: "flaky-test-detection-stabilization", title: "Flaky-Test Detection and Stabilization", description: "Detect inconsistent browser-test outcomes through bounded repeated runs and verify reviewed stabilization without hiding failures.", version: "0.4.0", changelog: [{"version":"0.4.0","summary":"Scopes setup to small engineering teams and defaults to one workflow unless access or lifecycle boundaries require a split."},{"version":"0.3.0","summary":"Defaults human approvals to durable asynchronous review: preparation ends pending review and a later action validates the recorded decision and current state."},{"version":"0.2.0","summary":"Adds an explicit inspect-first discovery step, focused customer questions, recorded user direction, and a clear boundary that installation is not approval."}], category: "Browser QA", order: 5, inputCount: 6, toolCount: 3, teamScope: "small_team" },
  { id: "browser-test-self-healing", title: "Browser Test Self-Healing", description: "Diagnose browser-test failures with preserved video and console/network evidence, verify test-only repairs, obtain review, and update canonical tests and knowledge.", version: "0.6.0", changelog: [{"version":"0.6.0","summary":"Scopes setup to small engineering teams and defaults to one workflow unless access or lifecycle boundaries require a split."},{"version":"0.5.1","summary":"Makes the Report dashboard the primary asynchronous review surface, with Ask in chat for discussion and separate durable approval and application."},{"version":"0.5.0","summary":"Separates repair preparation from application: verified candidates become durable pending reviews, and a later action route validates approval and current source state before applying."},{"version":"0.4.0","summary":"Adds an explicit inspect-first discovery step, focused customer questions, recorded user direction, and a clear boundary that installation is not approval."}], category: "Browser QA", order: 6, inputCount: 7, toolCount: 5, teamScope: "small_team" },
  { id: "scheduled-regression-synthetic-monitoring", title: "Scheduled Regression and Synthetic Monitoring", description: "Run approved browser journeys on a schedule, retain comparable history, and notify only on actionable changes.", version: "0.4.0", changelog: [{"version":"0.4.0","summary":"Scopes setup to small engineering teams and defaults to one workflow unless access or lifecycle boundaries require a split."},{"version":"0.3.0","summary":"Defaults human approvals to durable asynchronous review: preparation ends pending review and a later action validates the recorded decision and current state."},{"version":"0.2.0","summary":"Adds an explicit inspect-first discovery step, focused customer questions, recorded user direction, and a clear boundary that installation is not approval."}], category: "Browser QA", order: 7, inputCount: 6, toolCount: 3, teamScope: "small_team" },
  { id: "release-pr-quality-gate", title: "Release and PR Quality Gate", description: "Bind an exact change or build to required Browser QA suites and publish an auditable pass, fail, or needs-review decision.", version: "0.4.0", changelog: [{"version":"0.4.0","summary":"Scopes setup to small engineering teams and defaults to one workflow unless access or lifecycle boundaries require a split."},{"version":"0.3.0","summary":"Defaults human approvals to durable asynchronous review: preparation ends pending review and a later action validates the recorded decision and current state."},{"version":"0.2.0","summary":"Adds an explicit inspect-first discovery step, focused customer questions, recorded user direction, and a clear boundary that installation is not approval."}], category: "Browser QA", order: 8, inputCount: 6, toolCount: 3, teamScope: "small_team" },
  { id: "engineering-operations-intelligence", title: "Engineering Operations Intelligence", description: "Connect engineering data, calculate governed delivery, quality, and reliability intelligence, and run evidence-backed reviews in one small-team workflow.", version: "0.1.0", changelog: [{"version":"0.1.0","summary":"Combines engineering data foundation, delivery/quality/reliability intelligence, and recurring operations review into one small-team workflow playbook."}], category: "Engineering Operations Intelligence", order: 1, inputCount: 6, toolCount: 6, teamScope: "small_team" },
  { id: "cost-anomaly-to-verified-savings", title: "Cost Anomaly to Verified Savings", description: "Detect cloud-cost anomalies, propose evidence-backed rightsizing, prepare approved IaC changes, and verify realized savings and service health.", version: "0.5.0", changelog: [{"version":"0.5.0","summary":"Scopes setup to small engineering teams and defaults to one workflow unless access or lifecycle boundaries require a split."},{"version":"0.4.0","summary":"Defaults human approvals to durable asynchronous review: preparation ends pending review and a later action validates the recorded decision and current state."},{"version":"0.3.0","summary":"Adds an explicit inspect-first discovery step, focused customer questions, recorded user direction, and a clear boundary that installation is not approval."}], category: "FinOps", order: 1, inputCount: 7, toolCount: 5, teamScope: "small_team" },
  { id: "browser-performance-validation", title: "Browser Performance Validation", description: "Measure approved browser pages and journeys against customer-defined budgets with comparable samples and durable diagnostics.", version: "0.5.0", changelog: [{"version":"0.5.0","summary":"Scopes setup to small engineering teams and defaults to one workflow unless access or lifecycle boundaries require a split."},{"version":"0.4.0","summary":"Defaults human approvals to durable asynchronous review: preparation ends pending review and a later action validates the recorded decision and current state."},{"version":"0.3.0","summary":"Adds an explicit inspect-first discovery step, focused customer questions, recorded user direction, and a clear boundary that installation is not approval."},{"version":"0.2.1","summary":"Adds workflow goal, metric, and workflow-boundary guidance plus Technical, Architecture, and Strategic Pulse focus recommendations."}], category: "Performance Engineering", order: 1, inputCount: 6, toolCount: 3, teamScope: "small_team" },
  { id: "api-performance-validation", title: "API Performance Validation", description: "Measure approved API scenarios under bounded load against customer-defined latency, throughput, error, and capacity policies.", version: "0.4.0", changelog: [{"version":"0.4.0","summary":"Scopes setup to small engineering teams and defaults to one workflow unless access or lifecycle boundaries require a split."},{"version":"0.3.0","summary":"Defaults human approvals to durable asynchronous review: preparation ends pending review and a later action validates the recorded decision and current state."},{"version":"0.2.0","summary":"Adds an explicit inspect-first discovery step, focused customer questions, recorded user direction, and a clear boundary that installation is not approval."}], category: "Performance Engineering", order: 2, inputCount: 7, toolCount: 3, teamScope: "small_team" },
  { id: "ci-deployment-failure-triage", title: "CI and Deployment Failure Triage", description: "Ingest CI and deployment failures, classify likely cause with evidence, perform safe policy actions, and route the result.", version: "0.4.0", changelog: [{"version":"0.4.0","summary":"Scopes setup to small engineering teams and defaults to one workflow unless access or lifecycle boundaries require a split."},{"version":"0.3.0","summary":"Defaults human approvals to durable asynchronous review: preparation ends pending review and a later action validates the recorded decision and current state."},{"version":"0.2.0","summary":"Adds an explicit inspect-first discovery step, focused customer questions, recorded user direction, and a clear boundary that installation is not approval."}], category: "Reliability Operations", order: 1, inputCount: 5, toolCount: 4, teamScope: "small_team" },
  { id: "incident-investigation-coordination", title: "Incident Investigation and Coordination", description: "Receive reliability errors, correlate incident signals, perform basic evidence-backed RCA, establish impact and severity, and coordinate current status.", version: "0.5.0", changelog: [{"version":"0.5.0","summary":"Scopes setup to small engineering teams and defaults to one workflow unless access or lifecycle boundaries require a split."},{"version":"0.4.0","summary":"Defaults human approvals to durable asynchronous review: preparation ends pending review and a later action validates the recorded decision and current state."},{"version":"0.3.0","summary":"Adds an explicit inspect-first discovery step, focused customer questions, recorded user direction, and a clear boundary that installation is not approval."}], category: "Reliability Operations", order: 2, inputCount: 5, toolCount: 4, teamScope: "small_team" },
  { id: "governed-remediation-recovery", title: "Governed Remediation and Recovery", description: "Prepare exact operational remediation, validate and approve it, execute through authorized paths, and verify sustained recovery or rollback.", version: "0.4.0", changelog: [{"version":"0.4.0","summary":"Scopes setup to small engineering teams and defaults to one workflow unless access or lifecycle boundaries require a split."},{"version":"0.3.0","summary":"Defaults human approvals to durable asynchronous review: preparation ends pending review and a later action validates the recorded decision and current state."},{"version":"0.2.0","summary":"Adds an explicit inspect-first discovery step, focused customer questions, recorded user direction, and a clear boundary that installation is not approval."}], category: "Reliability Operations", order: 3, inputCount: 5, toolCount: 4, teamScope: "small_team" },
  { id: "post-incident-review-actions", title: "Post-Incident Review and Actions", description: "Produce a sourced post-incident review, create governed follow-up work, and verify improvements through completion.", version: "0.4.0", changelog: [{"version":"0.4.0","summary":"Scopes setup to small engineering teams and defaults to one workflow unless access or lifecycle boundaries require a split."},{"version":"0.3.0","summary":"Defaults human approvals to durable asynchronous review: preparation ends pending review and a later action validates the recorded decision and current state."},{"version":"0.2.0","summary":"Adds an explicit inspect-first discovery step, focused customer questions, recorded user direction, and a clear boundary that installation is not approval."}], category: "Reliability Operations", order: 4, inputCount: 5, toolCount: 4, teamScope: "small_team" },
  { id: "application-security-assessment-remediation", title: "Application Security Assessment and Remediation", description: "Run authorized web, API, code, dependency, secret, and configuration assessment through reviewed remediation and verified closure.", version: "0.5.0", changelog: [{"version":"0.5.0","summary":"Scopes setup to small engineering teams and defaults to one workflow unless access or lifecycle boundaries require a split."},{"version":"0.4.0","summary":"Defaults human approvals to durable asynchronous review: preparation ends pending review and a later action validates the recorded decision and current state."},{"version":"0.3.0","summary":"Adds an explicit inspect-first discovery step, focused customer questions, recorded user direction, and a clear boundary that installation is not approval."}], category: "Security Engineering", order: 1, inputCount: 6, toolCount: 14, teamScope: "small_team" },
  { id: 'growth-data-foundation', title: 'Growth Data Foundation', description: 'Connect and normalize traffic, product, billing, and feedback data with durable customer identity and provenance.', version: '0.2.0', category: 'Growth Analytics', order: 1, inputCount: 6, toolCount: 4, teamScope: 'small_team' },
  { id: 'funnel-conversion-intelligence', title: 'Funnel and Conversion Intelligence', description: 'Analyze signup-to-purchase funnels, detect conversion changes, and attribute them with session evidence.', version: '0.1.0', category: 'Growth Analytics', order: 2, inputCount: 7, toolCount: 3, teamScope: 'small_team' },
  { id: 'activation-retention-intelligence', title: 'Activation and Retention Intelligence', description: 'Find success-predicting behaviors, explain cohort divergence, and measure feature impact on retention and revenue.', version: '0.1.0', category: 'Growth Analytics', order: 3, inputCount: 7, toolCount: 3, teamScope: 'small_team' },
  { id: 'growth-experimentation-follow-through', title: 'Growth Experimentation and Follow-Through', description: 'Prioritize evidence-backed experiments, create tracked actions, and verify KPI improvement after shipping.', version: '0.2.0', category: 'Growth Analytics', order: 4, inputCount: 6, toolCount: 4, teamScope: 'small_team' },
  { id: 'seo-intelligence', title: 'SEO Intelligence', description: 'Find winnable keywords, diagnose technical SEO issues, and close content gaps with page-level briefs.', version: '0.1.0', category: 'Growth Analytics', order: 5, inputCount: 7, toolCount: 4, teamScope: 'small_team' },
  { id: 'ai-visibility-intelligence', title: 'AI Visibility Intelligence', description: 'Track AI-assistant brand citations against competitors and close gaps with content and authority changes.', version: '0.1.0', category: 'Growth Analytics', order: 6, inputCount: 6, toolCount: 3, teamScope: 'small_team' },
  { id: 'inbound-lead-to-meeting-review', title: 'Inbound Lead-to-Meeting Review', description: 'Qualify inbound enquiries, offer a reviewed booking path, and track provider-confirmed delivery and meetings.', version: '0.2.0', category: 'Sales', order: 1, inputCount: 7, toolCount: 6, teamScope: 'small_team', agentSlots: [
    { id: 'qualification', agent_playbook_id: 'lead-intake-qualifier', required: true, output: 'lead-qualification-brief/v1' },
    { id: 'research', agent_playbook_id: 'account-researcher', required: false, output: 'account-research-brief/v1' },
    { id: 'followup', agent_playbook_id: 'sales-followup-coordinator', required: true, output: 'sales-followup-draft/v1' },
  ], handoffs: [
    { id: 'qualification-to-followup', from: 'qualification', to: 'followup', artifact_type: 'lead-qualification-brief/v1', required: true },
    { id: 'qualification-to-research', from: 'qualification', to: 'research', artifact_type: 'lead-qualification-brief/v1', required: false },
    { id: 'research-to-followup', from: 'research', to: 'followup', artifact_type: 'account-research-brief/v1', required: false },
  ], setupChecks: ['goal_owner', 'source_scope', 'policy_metric', 'team_bindings', 'access', 'handoff_contract', 'plan_review', 'test_run', 'action_ledger', 'activation_choice'] },
  { id: 'new-customer-to-first-value', title: 'New Customer to First Value', description: 'Coordinate onboarding milestones and observed product adoption so a new B2B customer reaches an agreed first result.', version: '0.1.0', category: 'Customer Success', order: 1, inputCount: 6, toolCount: 5, teamScope: 'small_team', agentSlots: [
    { id: 'onboarding', agent_playbook_id: 'customer-onboarding-coordinator', required: true, output: 'onboarding-milestone-register/v1' },
    { id: 'adoption', agent_playbook_id: 'product-adoption-analyst', required: true, output: 'first-value-readout/v1' },
    { id: 'health', agent_playbook_id: 'customer-health-coordinator', required: false, output: 'customer-health-brief/v1' },
  ], handoffs: [
    { id: 'onboarding-to-adoption', from: 'onboarding', to: 'adoption', artifact_type: 'onboarding-milestone-register/v1', required: true },
    { id: 'adoption-to-health', from: 'adoption', to: 'health', artifact_type: 'first-value-readout/v1', required: false },
  ], setupChecks: ['goal_owner', 'account_scope', 'first_value_rule', 'team_bindings', 'source_access', 'handoff_contract', 'plan_review', 'test_run', 'action_ledger', 'activation_choice'] },
  { id: 'finance-operations-review', title: 'Finance Operations Review', description: 'Coordinate billing exceptions and a sourced finance impact review, with optional accounting close and payables specialists.', version: '0.1.0', category: 'Finance', order: 1, inputCount: 6, toolCount: 4, teamScope: 'small_team', agentSlots: [
    { id: 'billing', agent_playbook_id: 'billing-operations-coordinator', required: true, output: 'billing-exception-queue/v1' },
    { id: 'finance', agent_playbook_id: 'finance-analyst', required: true, output: 'finance-impact-readout/v1' },
    { id: 'close', agent_playbook_id: 'revenue-close-analyst', required: false, output: 'revenue-close-exceptions/v1' },
    { id: 'payables', agent_playbook_id: 'spend-payables-coordinator', required: false, output: 'payables-exception-queue/v1' },
  ], handoffs: [
    { id: 'billing-to-finance', from: 'billing', to: 'finance', artifact_type: 'billing-exception-queue/v1', required: true },
  ], setupChecks: ['goal_owner', 'source_scope', 'policy_metric', 'team_bindings', 'access', 'handoff_contract', 'plan_review', 'test_run', 'action_ledger', 'activation_choice'] },
  { id: 'website-growth-loop', title: 'Website Growth Loop', description: 'Coordinate a growth strategist and buyer-question specialist to find relevant website traffic opportunities, then track approved changes and measurement.', version: '0.3.0', category: 'Website Growth', order: 1, inputCount: 6, toolCount: 4, teamScope: 'small_team', agentSlots: [
    { id: 'strategist', agent_playbook_id: 'website-growth-starter', required: true, output: 'growth-priority-brief/v1' },
    { id: 'search', agent_playbook_id: 'search-opportunity-mapper', required: true, output: 'search-opportunity-list/v1' },
    { id: 'technical_seo', agent_playbook_id: 'seo-analyst', required: false, output: 'seo-issue-list/v1' },
    { id: 'content', agent_playbook_id: 'content-brief-writer', required: false, output: 'content-brief/v1' },
    { id: 'page', agent_playbook_id: 'content-page-builder', required: false, output: 'reviewable-page-draft/v1' },
    { id: 'measurement', agent_playbook_id: 'traffic-engagement-analyst', required: false, output: 'traffic-readout/v1' },
  ], handoffs: [
    { id: 'strategy-to-search', from: 'strategist', to: 'search', artifact_type: 'growth-priority-brief/v1', required: true },
    { id: 'search-to-content', from: 'search', to: 'content', artifact_type: 'search-opportunity-list/v1', required: false },
    { id: 'content-to-page', from: 'content', to: 'page', artifact_type: 'content-brief/v1', required: false },
  ], setupChecks: ['goal_owner', 'metric_policy', 'team_bindings', 'site_scope', 'capabilities', 'handoff_contract', 'plan_review', 'test_run', 'action_ledger', 'activation_choice'] },
] as const

export const PLAYBOOK_CATEGORIES = [...new Set(PLAYBOOK_CATALOG.map(playbook => playbook.category))]

export function isNewerPlaybookVersion(candidate: string, installed: string): boolean {
  const parse = (value: string) => value.split('.').map(part => Number.parseInt(part, 10))
  const next = parse(candidate)
  const current = parse(installed)
  if (next.length !== 3 || current.length !== 3 || [...next, ...current].some(Number.isNaN)) return candidate !== installed
  for (let index = 0; index < 3; index += 1) {
    if (next[index] !== current[index]) return next[index] > current[index]
  }
  return false
}
