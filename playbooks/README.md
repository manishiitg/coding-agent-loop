# AgentWorks playbooks

Versioned, authorable skill packages for AgentWorks' workflow builder.

Playbooks provide concise outcome guidance, decision criteria, and proven patterns. They do not override the user's requested process or require one fixed workflow graph. The builder inspects the existing workflow and current AgentWorks capabilities, preserves explicit user choices, and adapts only the relevant guidance.

```text
AgentWorks
└── Agentic Engineering Platform
    ├── Browser QA
    │   ├── Basic Browser Setup
    │   ├── Authentication and Session Validation
    │   ├── Role and Permission Validation
    │   ├── Critical Journey Validation
    │   ├── Flaky-Test Detection and Stabilization
    │   ├── Browser Test Self-Healing
    │   ├── Scheduled Regression and Synthetic Monitoring
    │   └── Release and PR Quality Gate
    ├── Security Engineering
    │   └── Application Security
    │       └── Application Security Assessment and Remediation
    ├── Performance Engineering
    │   ├── Browser Performance Validation
    │   └── API Performance Validation
    ├── Engineering Operations Intelligence
    │   ├── Engineering Data Foundation
    │   ├── Delivery, Quality, and Reliability Intelligence
    │   └── Engineering Operations Review
    ├── FinOps
    │   └── Cost Anomaly to Verified Savings
    └── Reliability Operations
        ├── CI and Deployment Failure Triage
        ├── Incident Investigation and Coordination
        ├── Governed Remediation and Recovery
        └── Post-Incident Review and Actions
```

### Browser QA

| Playbook | Outcome |
| --- | --- |
| [Basic Browser Setup](agentic-engineering-platform/browser-qa/basic-browser-setup/SKILL.md) | Configure browser access and Playwright, save verified locators, capture video plus console/network evidence, save an application profile, and establish reporting. |
| [Authentication and Session Validation](agentic-engineering-platform/browser-qa/authentication-session-validation/SKILL.md) | Validate approved login, logout, MFA, recovery, expiry, refresh, and invalid-session behavior. |
| [Role and Permission Validation](agentic-engineering-platform/browser-qa/role-permission-validation/SKILL.md) | Validate allowed and denied page, action, and data access across roles, ownership states, and tenants. |
| [Critical Journey Validation](agentic-engineering-platform/browser-qa/critical-journey-validation/SKILL.md) | Reuse that profile to run agreed journeys, investigate failures, and retain attempt-scoped results, video, and console/network evidence. |
| [Flaky-Test Detection and Stabilization](agentic-engineering-platform/browser-qa/flaky-test-detection-stabilization/SKILL.md) | Detect inconsistent outcomes, classify their cause, and verify reviewed stabilization without hiding failures. |
| [Browser Test Self-Healing](agentic-engineering-platform/browser-qa/browser-test-self-healing/SKILL.md) | Classify failures, verify test-only repairs with preserved diagnostics, obtain review, rerun canonical tests, and update knowledge/reporting. |
| [Scheduled Regression and Synthetic Monitoring](agentic-engineering-platform/browser-qa/scheduled-regression-synthetic-monitoring/SKILL.md) | Run proven routes on a schedule, retain comparable history, and notify on actionable changes. |
| [Release and PR Quality Gate](agentic-engineering-platform/browser-qa/release-pr-quality-gate/SKILL.md) | Bind exact changes/builds to required suites and publish an auditable pass, fail, or needs-review decision. |

### Security Engineering

| Playbook | Outcome |
| --- | --- |
| [Application Security Assessment and Remediation](agentic-engineering-platform/security-engineering/application-security/application-security-assessment-remediation/SKILL.md) | Run authorized browser, API, code, dependency, secret, and configuration assessment through reviewed remediation and deployed retesting. |

### Performance Engineering

| Playbook | Outcome |
| --- | --- |
| [Browser Performance Validation](agentic-engineering-platform/performance-engineering/browser-performance-validation/SKILL.md) | Measure approved pages and journeys against customer budgets using comparable samples and durable browser diagnostics. |
| [API Performance Validation](agentic-engineering-platform/performance-engineering/api-performance-validation/SKILL.md) | Measure approved API scenarios under bounded load against latency, throughput, error, and capacity policies. |

### Engineering Operations Intelligence

| Playbook | Outcome |
| --- | --- |
| [Engineering Data Foundation](agentic-engineering-platform/engineering-operations-intelligence/engineering-data-foundation/SKILL.md) | Connect and normalize issues, code changes, CI, builds, deployments, QA, security, performance, and incidents with provenance. |
| [Delivery, Quality, and Reliability Intelligence](agentic-engineering-platform/engineering-operations-intelligence/delivery-quality-reliability-intelligence/SKILL.md) | Calculate governed team/system metrics and identify evidence-backed bottlenecks, regressions, and recurring risks. |
| [Engineering Operations Review](agentic-engineering-platform/engineering-operations-intelligence/engineering-operations-review/SKILL.md) | Produce recurring evidence-backed reviews with tracked actions, governed approval, and delivery receipts. |

Engineering Operations Intelligence shares the [operations data model](agentic-engineering-platform/engineering-operations-intelligence/references/operations-data-model.md) for identity, lineage, metric definitions, and data-quality rules.

### FinOps

| Playbook | Outcome |
| --- | --- |
| [Cost Anomaly to Verified Savings](agentic-engineering-platform/finops/cost-anomaly-to-verified-savings/SKILL.md) | Detect and explain cloud-cost anomalies, prepare safe rightsizing IaC changes, obtain approval, and verify realized savings plus service health. |

### Reliability Operations

| Playbook | Outcome |
| --- | --- |
| [CI and Deployment Failure Triage](agentic-engineering-platform/reliability-operations/ci-deployment-failure-triage/SKILL.md) | Ingest CI/deployment failures, establish exact identity, classify them from evidence, and route safe rerun, escalation, or owner action. |
| [Incident Investigation and Coordination](agentic-engineering-platform/reliability-operations/incident-investigation-coordination/SKILL.md) | Correlate signals, establish impact and severity, maintain an evidence-backed timeline and hypotheses, and coordinate current status. |
| [Governed Remediation and Recovery](agentic-engineering-platform/reliability-operations/governed-remediation-recovery/SKILL.md) | Prepare, validate, approve, execute, and verify remediation or rollback through authorized control paths. |
| [Post-Incident Review and Actions](agentic-engineering-platform/reliability-operations/post-incident-review-actions/SKILL.md) | Produce a sourced review, create governed follow-up work, and verify improvements through completion. |

Reliability Operations shares a [reliability event and evidence contract](agentic-engineering-platform/reliability-operations/references/reliability-event-contract.md) plus [trigger, webhook, and Slack guidance](agentic-engineering-platform/reliability-operations/references/triggers-webhooks-and-slack.md). Authenticated webhooks start fixed saved routes; the Slack bot provides threaded investigation, status, and correlated human decisions backed by durable workflow records.

The default [error webhook to recovery](agentic-engineering-platform/reliability-operations/references/error-webhook-to-recovery.md) path connects an external reliability system to AgentWorks, validates and groups errors, performs basic RCA, selects an approved resolution or escalation path, verifies service recovery, and updates the dashboard, Slack thread, and authorized source system.

All Browser QA playbooks share an [AgentWorks plan and tool guide](agentic-engineering-platform/browser-qa/references/agentworks-plan-and-tools.md) and an [evidence capture contract](agentic-engineering-platform/browser-qa/references/evidence-capture.md). They define step/tool choices plus durable, redacted video, console/network, screenshot, and trace evidence.

## Authoring contract

[Playbook Specification v1](spec/PLAYBOOK-SPEC-v1.md) defines the package, fixed entrypoint sections, manifest, builder-consumption behavior, installation record, and versioning rules. Start a new package from the [playbook template](templates/playbook/SKILL.md), then replace its fictional metadata, reference, and example.

Every playbook entrypoint uses the same concise sections: Outcome, When to use, Required inputs, Plan and AgentWorks tools, Knowledge and persistence, Validation and reporting, Guardrails, Read details when needed, and Completion contract. Detailed references remain topic-specific so entrypoint skills stay small.

## Package and integration boundary

Each folder is a self-contained skill package: `SKILL.md`, supporting references, an example, and `playbook.json`. Frontmatter uses the current AgentWorks skill format. The JSON powers the Playbooks catalog, including hierarchy, setup prompt, capability requirements, and optional tool recommendations.

Installing a playbook copies its complete package to `<workflow>/skills/agentworks-playbook-<playbook-id>/` and writes an `installed_playbooks` receipt to the workflow manifest with its version, source hash, status, and skill name. The prefix prevents a playbook from overwriting a customer skill with the same ID. The interactive Workflow Builder attaches these workflow-local playbook skills after resolving the workflow path. Supporting references and examples remain available through progressive skill disclosure.

The Installed view compares the receipt version with the current catalog. A newer catalog version is labeled `Update available` with installed/latest versions, its short authored changelog, and upgrade instructions. `search_playbooks` returns the same comparison so Builder can explain the upgrade without guessing from version numbers. Updating refreshes only the installed guidance and marks setup `draft`; Builder reviews it against the existing workflow and no operational configuration changes automatically.

Installation does not create schedules, connect accounts, install recommended public software, or execute tests. The Builder follows the installed guidance through its existing chat tools and asks for missing access or decisions. Runtime steps receive only workflow-selected or per-step `enabled_skills`; installed Builder playbooks do not cascade into execution.

## Foundation and reuse

Basic Browser Setup produces a versioned, non-secret `browser-foundation/v1` profile pointing to canonical suite/config/locator sources. The other Browser QA playbooks reuse these sources and add outcome-specific coverage or operations. Each accepts an equivalent verified configuration where declared and preserves an explicitly chosen compatible runner; a prerequisite need not have been installed by name.

Prefer extending the same application workflow so its profile, DB, evidence, and report are already accessible. For a separate workflow, the builder explicitly transfers an authorized profile snapshot and selects credentials independently. No cross-workflow access is implied by a path or profile ID.

Record the source playbook ID/version and customer overrides in the workflow's durable setup data. Updates to these source packages do not silently rewrite installed workflows.

## Keep skills small

`SKILL.md` holds the purpose, essential constraints, and links to optional detail. Load supporting references only for the current operation. A reference workflow is a starting pattern, not a mandatory graph. The builder may combine or split steps when the user's process, existing plan, retry boundaries, permissions, or supported capabilities justify it.

Save application-specific verified locators and test setup in the knowledgebase with code/evidence references, and wire producer/consumer KB access. Executable locators stay in shared test helpers; chronological run results stay in the DB. Do not grow shared playbook skills with customer discoveries or repeat platform manuals in their entrypoints.

## Authoring checks

- Run `python3 playbooks/scripts/validate_playbooks.py` from the repository root.
- Validate every skill's frontmatter and supporting links.
- Parse `playbook.json` and confirm entrypoint/example paths exist.
- Use each reference's behavioral cases when testing the builder on an authorized fixture application.
- Check current `builder-reference` guidance before adapting to a deployed version. The packages follow the checked-in managed browser, per-step skill, and live HTML report contracts.

Recommended CLIs, MCPs, and public skills remain optional. Their availability, compatibility, source revision, license, and trust are checked at setup time rather than claimed by metadata. Install hints are UI guidance only; playbook installation never executes them automatically. No public registry installation is prescribed for AgentWorks' private Playwright packages.
