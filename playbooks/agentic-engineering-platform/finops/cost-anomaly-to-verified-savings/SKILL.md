---
name: cost-anomaly-to-verified-savings
description: Build an AgentWorks FinOps workflow that detects cost anomalies, proposes evidence-backed rightsizing, prepares reviewable IaC fixes, obtains approval, and verifies realized savings. Use for governed cloud-cost optimization.
---

# Cost Anomaly to Verified Savings

## Outcome

Create an auditable FinOps workflow that detects and explains cost anomalies, proposes safe rightsizing, prepares exact infrastructure-as-code changes, obtains configured approval, and verifies realized savings and service health after rollout.

## When to use

Use for authorized cloud accounts and services with trustworthy billing, inventory, utilization, ownership, and IaC sources. Use investigation-only mode when changes or sufficient telemetry are unavailable.

## Required inputs

Resolve accounts/projects/subscriptions, environments, billing basis/currency, attribution and owner mapping, budgets/baselines, utilization and service-health signals, IaC repositories/state, allowed resource/change types, risk/SLO limits, approval/deployment/rollback policy, and verification window.

## Plan and AgentWorks tools

Use scripted steps for cost ingestion, normalization, anomaly scoring, candidate calculations, IaC validation, approved application, and savings measurement. Use message sequences for evidence-based cause and risk analysis. Present the concrete candidate and diff before a human approval branch; unattended default is defer.

## Knowledge and persistence

Store cost/usage observations, baselines, anomalies, candidates, decisions, changes, health checks, and savings measurements in durable tables. Store plans, diffs, validation output, and receipts in durable assets. Keep customer policy in KB context and verified service/IaC facts in scoped notes without secrets.

## Validation and reporting

Require source freshness, attribution, comparable baselines, utilization coverage, exact IaC and deployment identity, approval receipts, post-change health, and a completed verification window. The dashboard shows current, projected, implemented, and verified cost/savings; filters by provider, account, service, environment, and owner; and drills into anomaly evidence, candidates, IaC, approval, rollout, and health. Incomplete evidence cannot become realized savings.

## Guardrails

Do not change infrastructure from anomaly output alone, apply unapproved production changes, bypass IaC/state ownership, violate capacity or reliability limits, expose billing secrets, double-count savings, or claim credits, demand changes, or workload drops as rightsizing savings.

## Read details when needed

- [Workflow design and outcomes](../../references/workflow-design-and-outcomes.md): goals, metrics, and current-versus-separate workflow decisions.
- [FinOps workflow](references/finops-workflow.md): data contract, anomaly analysis, rightsizing, IaC, approval, and verification.
- [Service cost analysis](references/service-cost-analysis.md): choose and load detailed cost checks for the affected service family.
- [Example candidate](examples/optimization-candidate.json): fictional optimization record.
- [Catalog metadata](playbook.json): presentation and optional recommendations.

## Completion contract

Return installed playbook and policy/baseline revisions, affected provider/service/resource IDs, service-cost model and source revisions, anomaly evidence, candidate and risk, IaC diff/validation, approval/deployment/rollback receipts, health result, projected and verified savings, report locations, capability resolution, and limitations.
