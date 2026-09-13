# AgentWorks Playbook Specification v1

## Purpose

An AgentWorks playbook is a small, versioned skill package that helps the workflow builder adapt proven operating guidance to a customer's process. It is guidance for building or improving a workflow, not an executable workflow definition.

The user's request, durable preferences, existing workflow, and live platform capabilities take precedence. Installation records how the builder adapted the source package; it does not silently replace the customer's plan.

## Package layout

```text
<playbook-id>/
├── SKILL.md
├── playbook.json
├── references/
│   └── ...
└── examples/
    └── ...
```

`SKILL.md` is the builder entrypoint. Keep it under 500 words and link to details instead of copying platform manuals into it. References may be organized for the topic; they do not repeat the entrypoint section schema. Examples must be fictional, non-secret, and clearly identified as illustrative.

## Fixed entrypoint sections

Every playbook `SKILL.md` uses these H2 headings exactly and in this order:

1. `Outcome`
2. `When to use`
3. `Required inputs`
4. `Plan and AgentWorks tools`
5. `Knowledge and persistence`
6. `Validation and reporting`
7. `Guardrails`
8. `Read details when needed`
9. `Completion contract`

These headings are the stable UI and builder-consumption contract. Keep each section concise:

- **Outcome:** the durable behavior or capability the installed workflow should provide.
- **When to use:** positive triggers, scope, and important exclusions.
- **Required inputs:** information, access, policy, and existing artifacts the builder must resolve. Mark unavailable required inputs as blockers rather than inventing them.
- **Plan and AgentWorks tools:** explain how the builder maps the playbook into a plan: which behavior belongs in scripted steps, message sequences, routes, branches, schedules, or approvals; what existing steps to reuse; and which AgentWorks capabilities to resolve. Link to shared or topic-specific detail.
- **Workflow design and outcomes:** inspect existing goals and metrics, recommend measurable additions when needed, and decide whether to extend the current workflow or propose a separate workflow using ownership, trigger, access, lifecycle, and operational boundaries.
- **Knowledge and persistence:** what belongs in context, knowledgebase notes, learnings, database tables, and durable assets.
- **Validation and reporting:** define machine-checkable readiness and the reporting dashboard: primary status and metrics, useful filters/dimensions, evidence drill-down, history/trends, incomplete states, and any restricted data. Keep the dashboard backed by durable workflow data rather than message text.
- **Guardrails:** constraints that must survive adaptation.
- **Read details when needed:** relative links with a one-line reason to load each reference or example.
- **Completion contract:** the locations, receipts, results, and unresolved items the builder returns.

Use `Not applicable — <reason>` only when the topic truly has no content for a required section. Do not delete or rename the section.

## Frontmatter

The frontmatter contains only:

```yaml
---
name: <playbook-id>
description: <what it does and when the builder should use it>
---
```

The name must match `playbook.json.id` and the package directory. Put triggering language in `description`; do not add customer-specific details.

## Manifest contract

`playbook.json` is catalog and setup metadata. Required fields are:

| Field | Contract |
| --- | --- |
| `metadata_version` | Integer manifest format version; `1` for this specification. |
| `content_schema` | `agentworks-playbook/v1`. |
| `id` | Lowercase kebab-case stable identifier matching the folder and skill name. |
| `version` | Semantic version of the source package. |
| `title`, `description` | Human-readable catalog presentation. |
| `hierarchy` | Ordered catalog path, starting with `AgentWorks`. |
| `order` | Sibling display order. |
| `entrypoint` | `SKILL.md`. |
| `audience` | `workflow_builder`. |
| `setup_prompt` | User-visible instruction used to start an installation or adaptation. |
| `setup_inputs` | Input descriptors with stable IDs, labels, and required/default conditions. |
| `required_capabilities` | Capability names the builder must resolve or explicitly mark unavailable. |
| `recommended_tools` | Optional recommendations, never implicit installation or authorization. |
| `outputs` | Durable completion artifacts and results. |

Topic-specific relationship fields such as `setup_playbook`, `setup_playbooks`, `accepts_equivalent_setup`, and `next_playbooks` are optional. Unknown extension fields should be ignored by compatible readers.

Each recommended tool contains `id`, `name`, `type`, `purpose`, `capability`, and `optional`. A recommendation improves discovery and UI presentation; the builder still checks current availability and preserves an explicitly selected compatible alternative.

`recommended_skills` is an optional UI/discovery list. Each item contains `id`, `name`, `publisher`, `source`, `install_hint`, `purpose`, and `optional: true`. The source and install hint are informational and may change independently of the playbook. Before import, resolve the current source, inspect the complete skill package and license, check compatibility, record the source revision/digest, and use the supported AgentWorks skill-import flow. Never execute an install hint or attach a skill automatically.

## Builder consumption

The builder consumes a playbook progressively:

1. Read `SKILL.md` and the manifest.
2. Inspect the existing plan, workflow configuration, selected capabilities, durable stores, and report.
3. Resolve required inputs and distinguish known values, reasonable reversible defaults, and blockers.
4. Map the requested outcome to existing goals and metrics; recommend missing definitions without inventing customer targets.
5. Recommend whether to extend the current workflow, add a route, or create a separate workflow, with concrete reasons and tradeoffs.
6. Load only the references relevant to the current operation.
7. Map the guidance onto the chosen workflow using supported AgentWorks plan/config tools. Reuse or revise compatible steps before adding new ones.
8. Show concrete human decisions only after the draft, evidence, or proposed change is ready for review.
9. Trial the affected step or route, validate durable output and the report, then record the installed snapshot.

The builder must not copy the reference graph mechanically, add a step per tool call, attach every recommended tool, overwrite user preferences, or edit system-managed plan files directly. Dashboard widgets and drill-downs must read the same durable records used for validation; reporting is part of installation, not a separate optional workflow.

## Installed playbook record

An installed playbook is a customer-specific snapshot and adaptation record. The product should persist at least:

```json
{
  "source_playbook_id": "basic-browser-setup",
  "source_version": "0.6.0",
  "content_schema": "agentworks-playbook/v1",
  "installed_at": "ISO-8601 timestamp",
  "source_digest": "content digest",
  "status": "draft|ready|blocked|outdated",
  "customer_overrides": {},
  "capability_resolution": {},
  "plan_step_ids": [],
  "artifacts": {},
  "validation": {}
}
```

Source updates never silently rewrite an installed workflow. The product compares versions/digests, shows relevant changes, and lets the builder merge them while preserving customer overrides.

## Versioning

- **Patch:** wording, examples, or corrections without changing expected behavior.
- **Minor:** new optional guidance, inputs, outputs, or compatible behavior.
- **Major:** renamed/removed contracts, changed guardrails, or incompatible installed behavior.

Record the source version and customer adaptations whenever a playbook is installed or upgraded.

## Validation

Run `python3 playbooks/scripts/validate_playbooks.py` from the repository root. The validator checks package identity, required manifest fields, fixed section order, explicit plan-step and dashboard guidance, the skill word limit, JSON parsing, local Markdown links, entrypoint existence, and recommended-tool shape.
