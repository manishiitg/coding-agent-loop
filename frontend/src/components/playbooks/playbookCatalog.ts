export type PlaybookCatalogItem = {
  id: string
  title: string
  description: string
  version: string
  category: string
  order: number
  inputCount: number
  toolCount: number
  setupPrompt?: string
  setupInputs?: PlaybookSetupInput[]
  requiredCapabilities?: string[]
  recommendedTools?: PlaybookRecommendedTool[]
  outputs?: string[]
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

// Read-only catalog projection of the first-party playbook manifests. The API
// slice will replace this projection when installation records are introduced.
export const PLAYBOOK_CATALOG: readonly PlaybookCatalogItem[] = [
  { id: 'basic-browser-setup', title: 'Basic Browser Setup', description: 'Configure browser access and Playwright, save verified locators, capture video plus console/network evidence, and establish reporting.', version: '0.6.0', category: 'Browser QA', order: 1, inputCount: 7, toolCount: 4 },
  { id: 'authentication-session-validation', title: 'Authentication and Session Validation', description: 'Validate login, logout, MFA, recovery, expiry, refresh, and invalid-session behavior with durable evidence.', version: '0.1.0', category: 'Browser QA', order: 2, inputCount: 6, toolCount: 3 },
  { id: 'role-permission-validation', title: 'Role and Permission Validation', description: 'Validate page, action, and data permissions across roles, ownership states, and tenant boundaries.', version: '0.1.0', category: 'Browser QA', order: 3, inputCount: 5, toolCount: 3 },
  { id: 'critical-journey-validation', title: 'Critical Journey Validation', description: 'Reuse browser setup and locator helpers to run critical journeys, investigate failures, and retain video plus console/network evidence.', version: '0.6.0', category: 'Browser QA', order: 4, inputCount: 5, toolCount: 6 },
  { id: 'flaky-test-detection-stabilization', title: 'Flaky-Test Detection and Stabilization', description: 'Detect inconsistent browser-test outcomes through bounded repeated runs and verify reviewed stabilization without hiding failures.', version: '0.1.0', category: 'Browser QA', order: 5, inputCount: 6, toolCount: 3 },
  { id: 'browser-test-self-healing', title: 'Browser Test Self-Healing', description: 'Diagnose browser-test failures, verify test-only repairs, obtain review, and update canonical tests and knowledge.', version: '0.3.0', category: 'Browser QA', order: 6, inputCount: 7, toolCount: 5 },
  { id: 'scheduled-regression-synthetic-monitoring', title: 'Scheduled Regression and Synthetic Monitoring', description: 'Run approved browser journeys on a schedule, retain comparable history, and notify only on actionable changes.', version: '0.1.0', category: 'Browser QA', order: 7, inputCount: 6, toolCount: 3 },
  { id: 'release-pr-quality-gate', title: 'Release and PR Quality Gate', description: 'Bind an exact change or build to required Browser QA suites and publish an auditable decision.', version: '0.1.0', category: 'Browser QA', order: 8, inputCount: 6, toolCount: 3 },
  { id: 'application-security-assessment-remediation', title: 'Application Security Assessment and Remediation', description: 'Run authorized web, API, code, dependency, secret, and configuration assessment through verified closure.', version: '0.2.0', category: 'Security Engineering', order: 1, inputCount: 6, toolCount: 14 },
  { id: 'browser-performance-validation', title: 'Browser Performance Validation', description: 'Measure approved browser pages and journeys against customer-defined budgets with comparable samples.', version: '0.2.0', category: 'Performance Engineering', order: 1, inputCount: 6, toolCount: 3 },
  { id: 'api-performance-validation', title: 'API Performance Validation', description: 'Measure approved API scenarios under bounded load against latency, throughput, error, and capacity policies.', version: '0.1.0', category: 'Performance Engineering', order: 2, inputCount: 7, toolCount: 3 },
  { id: 'ci-deployment-failure-triage', title: 'CI and Deployment Failure Triage', description: 'Ingest CI and deployment failures, classify likely cause with evidence, and route the result.', version: '0.1.0', category: 'Reliability Operations', order: 1, inputCount: 5, toolCount: 4 },
  { id: 'incident-investigation-coordination', title: 'Incident Investigation and Coordination', description: 'Correlate incident signals, perform evidence-backed RCA, establish impact, and coordinate status.', version: '0.2.0', category: 'Reliability Operations', order: 2, inputCount: 5, toolCount: 4 },
  { id: 'governed-remediation-recovery', title: 'Governed Remediation and Recovery', description: 'Prepare, validate, approve, and execute operational remediation with recovery verification.', version: '0.1.0', category: 'Reliability Operations', order: 3, inputCount: 5, toolCount: 4 },
  { id: 'post-incident-review-actions', title: 'Post-Incident Review and Actions', description: 'Produce a sourced post-incident review, create governed follow-up work, and verify improvements.', version: '0.1.0', category: 'Reliability Operations', order: 4, inputCount: 5, toolCount: 4 },
  { id: 'engineering-data-foundation', title: 'Engineering Data Foundation', description: 'Connect and normalize delivery, quality, release, and incident data with freshness and provenance.', version: '0.1.0', category: 'Engineering Operations Intelligence', order: 1, inputCount: 6, toolCount: 4 },
  { id: 'delivery-quality-reliability-intelligence', title: 'Delivery, Quality, and Reliability Intelligence', description: 'Calculate governed engineering metrics and identify evidence-backed bottlenecks and recurring risks.', version: '0.1.0', category: 'Engineering Operations Intelligence', order: 2, inputCount: 6, toolCount: 3 },
  { id: 'engineering-operations-review', title: 'Engineering Operations Review', description: 'Produce recurring evidence-backed engineering reviews with material changes and tracked actions.', version: '0.1.0', category: 'Engineering Operations Intelligence', order: 3, inputCount: 6, toolCount: 4 },
  { id: 'cost-anomaly-to-verified-savings', title: 'Cost Anomaly to Verified Savings', description: 'Detect cloud-cost anomalies, propose rightsizing, prepare approved IaC changes, and verify savings.', version: '0.2.0', category: 'FinOps', order: 1, inputCount: 7, toolCount: 5 },
] as const

export const PLAYBOOK_CATEGORIES = [...new Set(PLAYBOOK_CATALOG.map(playbook => playbook.category))]
