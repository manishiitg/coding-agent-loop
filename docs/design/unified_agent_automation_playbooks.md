# Unified Playbooks for Crews and Automations

Status: proposed product contract, 2026-09-25. This document describes the next implementation. Current Crew templates and Workflow Builder playbooks do not yet share an installer or automatically bind a Crew to an Automation.

## The model

**Playbook** is the reusable, versioned definition in one catalog. It has two new install targets:

| Playbook kind | Installed instance | What the installation owns |
| --- | --- | --- |
| **Agent** | One Crew member | One durable agent identity, its skill/procedure, expected outputs, tools it may need, callable actions, and a chat-led setup checklist. |
| **Automation** | One Automation | A goal, a team of agent slots, the order and contracts of their handoffs, shared inputs, measurement, run policy, and an Automation setup checklist. |

A Crew is one agent. Its **primary Agent Playbook** establishes its role. The Crew may add supporting skills or capability packs, such as Finance Analyst plus Tax Export, without silently changing that role or creating a second agent inside the same Crew. An Automation coordinates at least two distinct Crew members; an agent slot binds to an existing Crew or to a newly installed Agent Playbook. One Crew may fill additional slots only when it has the verified capabilities and compatible access for them.

Playbooks are definitions; Crews and Automations are customer-owned installed instances. The installed instance pins the source Playbook ID, version, and digest, and keeps customer decisions and progress separately. A catalog update never changes a running agent or Automation on its own.

The existing [Workflow Builder playbooks](../../playbooks/spec/PLAYBOOK-SPEC-v1.md) are a third, current target. They guide a builder to adapt a workflow; their manifest does not declare an Automation's agent roster or handoff contracts. Keep them working while adapting applicable packages into the new Automation Playbook format. Do not relabel an existing Workflow Playbook as an installable multi-agent Automation until its roster, contracts, and tests exist.

## Shared catalog record

Every new Playbook needs an ID, version, kind (`agent` or `automation`), category, summary, example outcome, required inputs, optional integrations, setup checks, capabilities, update notes, and an installable package digest. The catalog can show both kinds under Website Growth, Finance, and other categories. The card action must name the target: **Create Crew member** or **Set up Automation**.

An **Agent Playbook** additionally declares:

- The single agent role and boundaries; local skill and starter files; first result possible with minimum inputs.
- Required and optional capabilities expressed independently of a specific MCP provider.
- Optional typed callable actions (name, input/output schemas, permissions, test example) that an Automation may use.
- Five to ten checks for identity, data and tool access, first result, and optional recurrence or delivery decisions.

An **Automation Playbook** additionally declares:

- Outcome and metric definitions, with baseline and target collected from the owner; no illustrative number becomes a customer target.
- Required and optional agent slots, each with an Agent Playbook reference or capability contract, and whether an existing Crew can fill it.
- A versioned handoff plan: source slot, target slot, typed input/output, durable artifact reference, retry and review policy. An Automation passes bounded artifacts or references, not every agent's entire workspace.
- Shared business context; required data sources; schedule, event, or manual start options; budget and concurrency limits; approvals; dashboard and completion evidence.
- Its own setup checks and one representative test run. Installed and configured do not mean active.

For example, a proposed Automation Playbook record could contain the following references. The real package would also include the inputs, output schemas, permissions, checks, and run policy described above:

```json
{
  "id": "website-growth-loop",
  "kind": "automation",
  "version": "1.0.0",
  "agent_slots": [
    { "id": "strategist", "agent_playbook_id": "website-growth-starter", "required": true },
    { "id": "search", "agent_playbook_id": "seo-analyst", "required": true }
  ],
  "handoffs": [
    { "from": "strategist", "to": "search", "artifact_type": "growth-priority-brief/v1" }
  ]
}
```

The installed Automation records actual Crew IDs for those slots and setup state. The Playbook package never contains customer credentials or assigns Crew IDs in advance.

## Install and setup flow

### A. Create an individual Crew agent

1. Pick an Agent Playbook, or Blank Crew. Preview its first output and minimum setup.
2. Create the Crew with one primary role, the pinned Playbook receipt, bundled skill, and an empty setup checklist.
3. Click **Setup pending** to start a chat. The Crew verifies the site/data, audience, tool access, and a first result. It marks checks only after verification.
4. Optionally add supporting capability packs, connections, schedules, triggers, and callable actions. Each has separate status. Nothing recurring starts merely because the Agent Playbook was installed.

### B. Install a multi-agent Automation

1. Pick an Automation Playbook. Before installation, show its outcome, required agent roster, example handoffs, required data, approvals, expected run cadence, and likely cost.
2. Create an Automation in **draft**. Pin the Playbook version/digest. Ask the owner for the actual goal, metric, baseline or baseline-first decision, scope, and budget.
3. For each required agent slot, offer **Use existing Crew** or **Create from Agent Playbook**. Inspect a selected Crew's identity, installed capabilities, setup status, and callable contract. Do not infer readiness from its name alone.
4. Resolve shared inputs once in the Automation (for example site URL, audience, and conversion goal). Give each Crew only the inputs and connection scopes its step needs. Existing account connections are selected and tested per Crew; credentials are never copied from a Playbook or another Crew.
5. Set up missing agent capabilities through each Crew's chat checklist. Register any proposed typed function only after reviewing and testing its contract. Mark the Automation team ready only when all required slots and handoffs pass.
6. Configure the schedule or authenticated event source, timezone, concurrency, retry, notification, approval, and spending limits. Start with these paused.
7. Run one bounded test with real authorized inputs. Inspect each agent's output, handoff artifact, final result, and dashboard metric. Fix failed checks through chat.
8. Activate after the owner reviews the concrete team, permissions, first run, and cadence. The Automation then owns recurrence, goal progress, run history, and failure recovery.

Setup state is visible at two levels: each Crew's Agent Playbook is **pending/ready**, and the Automation Playbook is **draft/blocked/ready/active/paused**. The Automation detail shows exactly which agent slot or handoff blocks readiness. If a Crew later loses access or a function contract changes, pause affected runs and show the failed check; do not silently substitute another agent.

## Website Growth example

The **Website Growth Starter** Agent Playbook is already represented by an installable Crew template. It can audit a public site and draft a 30-day plan before Search Console data exists. The **Website Growth Loop** is a proposed Automation Playbook, not an installed or active Automation today.

| Team slot | Agent Playbook | Handoff |
| --- | --- | --- |
| Strategist | Website Growth Starter | Receives the business goal and site context; emits a sourced priority brief. |
| Search analyst | SEO Analyst or Search Opportunity Mapper | Receives the site scope and brief; emits page/query opportunities with evidence. |
| Content builder | Content Brief Writer or Content Page Builder | Receives an approved opportunity; emits a reviewable brief or page draft. |
| Measurement analyst | Traffic & Engagement Analyst | Receives the shipped change and measurement window; emits a comparable readout and open questions. |

At minimum, a Website Growth Loop installation needs multiple ready Crew agents, one tested handoff between them, a named owner, and a measurable success signal. A new site may use **baseline first** until enough data exists. The loop can propose page changes, but publication and outreach follow separately configured approvals. SEO Intelligence and AI Visibility Intelligence from the current Workflow Playbook catalog can inform the corresponding agent/Automation designs; they are not automatically installed by this Crew.

## Compatibility and implementation sequence

1. Add the shared Playbook catalog schema and explicit install target. Preserve existing Workflow Playbooks and receipts.
2. Migrate the three current Crew templates to Agent Playbook receipts without recreating Crews. For an existing Crew with multiple templates, keep its current identity; present the first/role-matching template as a proposed primary agent and the rest as capabilities for owner review. Never rewrite identity silently.
3. Add Agent Playbook install and update status to Crew Identity; retain the current chat setup and per-pack checklist files.
4. Add Automation Playbook packages, a team-binding setup view, typed Crew function checks, and durable handoff/run records. The Automation installer must use existing authorization and integration controls.
5. Build and test Website Growth Loop with the smallest viable multi-agent roster before showing it as installable. A category listing or suggested Automation name is not evidence that the team can run.

The first release should prove one path end to end: install a Website Growth agent, create or reuse at least one other ready specialist Crew, bind both into a draft Automation, complete setup in chat, run once, inspect evidence, and only then enable recurrence.
