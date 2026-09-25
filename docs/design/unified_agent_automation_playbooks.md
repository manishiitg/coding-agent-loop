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

## Detailed implementation contract

### Product vocabulary and ownership

| Term | Meaning | Owner and source of truth |
| --- | --- | --- |
| Catalog Playbook | Immutable, versioned recipe offered to customers. | Published catalog package; no customer values or credentials. |
| Agent installation | One primary agent recipe installed in a Crew. | Crew owner; Crew project identity and installation state. |
| Capability pack | An additional skill or procedure usable by the same agent. | Crew owner; its own installation and setup state. It does not change the primary agent role. |
| Automation installation | A team recipe installed in one existing or new Workflow Automation. | Automation owner; Workflow workspace and installation state. |
| Agent slot | Named responsibility in an Automation, bound to one Crew ID. | Automation installation. The Crew retains its own identity and access. |
| Handoff | Versioned artifact passed between steps, with an expected schema and evidence. | Automation run. The receiving Crew gets only the authorized artifact/reference. |
| Workflow Builder Playbook | Current `agentworks-playbook/v1` guidance for adapting a workflow. | Existing Workflow Builder installation and receipt. It remains usable independently. |

An Automation Playbook does **not** become a second type of Crew, and an Agent Playbook does **not** create a recurring Workflow. An Automation's Workflow plan is the execution engine; Crew steps invoke the bound agents. It must use at least two distinct Crew IDs. The owner can also put ordinary deterministic Workflow steps between Crew steps for validation, routing, or publication approvals.

### Catalog package v2

Keep the current `agentworks-playbook/v1` format and validator intact. Introduce `agentworks-playbook/v2` for the two new `kind` values. A package contains `playbook.json` plus bundled procedure files, examples, and schemas referenced by relative path. The server validates and indexes packages before publishing; the frontend reads a catalog API rather than importing every template into one JavaScript bundle. This keeps search and preview workable beyond 100 templates.

Shared manifest fields:

| Field | Required contract |
| --- | --- |
| `content_schema`, `id`, `version`, `kind` | `agentworks-playbook/v2`, stable kebab-case ID, semantic version, `agent` or `automation`. The tuple `(id, version)` is immutable. |
| `title`, `summary`, `category`, `tags`, `icon` | Catalog presentation and search metadata. Category is a stable ID with a separate display label. |
| `outcome`, `first_result`, `example_requests` | Promise shown before install. Examples are illustrative, never prefilled customer facts. |
| `inputs` | Stable input IDs, type, required condition, validation, scope (`shared` or slot-specific), and whether the value may contain sensitive data. |
| `capabilities` | Provider-neutral abilities and acceptable alternatives; read/write scope and whether required for the minimum result. |
| `setup_checks` | Stable IDs, requirement level, verification rule, and recovery instruction. Agent packages have five to ten checks. |
| `files`, `changelog` | Relative package files with checksums and authored version notes. No executable path escapes or customer paths. |

Agent-specific fields: `role`, `boundaries`, `starter_instruction`, `bundled_skills`, `output_contracts`, and optional `callable_actions`. Each callable action declares input/output schema IDs, required permissions, timeout, and one validation fixture; declaring one does not register or expose it.

Automation-specific fields: `goal_definition`, `agent_slots`, `handoffs`, `plan_recipe`, `run_policy`, `measurement`, `approval_points`, and `setup_checks`. Each slot has a stable ID, accepted Agent Playbook IDs or a capability contract, a minimum setup status, and whether the installer can create a matching Crew. Each handoff names source and target slots, artifact type and schema version, producer and consumer steps, failure/retry policy, and whether human review is required.

The catalog validator must reject duplicate IDs/check IDs, missing referenced files, cyclic handoffs without an explicit bounded loop, a required slot with no accepted capability, fewer than two distinct required agent responsibilities, invalid JSON schemas, unsupported permissions, and a promised output that has no testable completion evidence. It must also ensure example data is clearly fictional.

### Installed records and files

The installer persists a pinned receipt and customer state separately from the catalog source. Proposed workspace layout:

```text
<crew-project>/
  product.json                         # one primary Agent Playbook pointer; capability pack pointers
  playbooks/installations/<id>/
    source.json                        # ID, version, digest, package file digests
    setup.json                         # checks, answers, evidence, and status
    files/...                          # pinned skill/procedure snapshot

<workflow-workspace>/
  workflow.json                        # Automation Playbook pointer and Crew attachments
  playbooks/installations/<id>/
    source.json
    setup.json                         # goal, slot bindings, capability resolution, activation state
    plan-proposal.json                 # intended steps, handoffs, metrics, policy
  runs/<run-id>/...                     # normal Workflow run evidence and handoff artifacts
```

The paths are a proposed v2 layout, not paths the current runtime already reads. Server APIs must own installation mutations so a partially failed file copy cannot advertise a ready Playbook. Keep the existing Workflow Builder `installed_playbooks` records in their current form; do not reinterpret them as v2 receipts.

The `source.json` receipt contains `installation_id`, `kind`, `source_id`, `source_version`, `content_schema`, `source_digest`, `installed_at`, `installed_by`, and `instance_id`. The Automation state additionally contains `workflow_id`, slot-to-`crew_profile_id`/`crew_project_id` bindings, actual goal and metric definitions, run policy, and validation results. The Crew state contains the primary agent pointer and capability pack IDs. Secret **names or connection references** may appear only in access selections; secret values never appear in receipts, chat setup files, artifacts, or copied package content.

Store customer answers and adaptations in state, not by rewriting the catalog package. A source update produces an explicit diff of authored changes and customer overrides. Installation and upgrade endpoints require an idempotency key and return the existing installation for a repeated request; a changed request under the same key is rejected. On a failed install, preserve a resumable `draft` with a concrete error, or roll back newly created files and links. Never leave a half-created active trigger.

The proposed server operations are:

| Operation | Inputs | Result and server guard |
| --- | --- | --- |
| `listPlaybooks` / `getPlaybook` | Kind, category, search, cursor or ID/version. | Published metadata/preview only; no customer secrets. |
| `installAgentPlaybook` | Source ID/version, target new/existing Crew, idempotency key. | Draft receipt and Crew ID; owner access, primary-role conflict, file digest, and skill availability checked. |
| `installAutomationPlaybook` | Source ID/version, target new/existing Workflow, idempotency key. | Draft receipt and Workflow ID; owner edit rights and package validity checked. |
| `setAutomationGoal` / `bindAgentSlot` | Installation ID, customer decisions or slot/Crew IDs, expected state revision. | Updated draft and exact missing checks; binding verifies Crew access and capability contract. |
| `resolveCapability` / `recordCheckEvidence` | Installation/check ID, selected resource or evidence ref, expected state revision. | New check state after a server-side probe or authorized owner decision; arbitrary chat text cannot set `verified`. |
| `proposePlan` / `testInstallation` | Installation ID and expected state revision. | Reviewable plan diff or bounded test run ID; side effects stay within approved test scope. |
| `activateInstallation` / `pauseInstallation` | Installation ID and expected state revision. | New state only after readiness, policy, access, and trigger checks; operations are audit logged. |
| `proposeUpdate` / `replaceSlot` / `removeInstallation` | Installation ID, requested change, expected state revision. | Impact preview followed by a reviewed mutation; running bindings and customer data are preserved or explicitly retired. |

Every mutation is owner-scoped, checks the current revision to avoid overwriting a concurrent chat/UI change, and returns the installation status plus its blocking check IDs. The frontend should use these operations instead of directly editing pinned receipts. The Builder can call the same authorized service operations through tools, keeping chat and UI in sync.

### Setup checks and readiness

Each check has `id`, `title`, `required`, `method` (`owner_answer`, `resource_probe`, `artifact_review`, `test_call`, or `run_evidence`), `status`, `evidence`, `checked_at`, and `failure_reason`. Allowed check statuses are `pending`, `in_progress`, `verified`, `skipped`, `blocked`, and `stale`. Only a required check with current `verified` evidence counts toward readiness. An optional check may be `skipped` after the owner records the choice; skipping cannot be used to waive an output or permission the selected run policy requires.

The Crew setup button sends a message into that Crew's chat with its installation ID and incomplete check IDs. The Crew can ask for inputs and perform available probes. A check is marked `verified` only when its method produces the stated evidence, such as a site URL inspected, skill selected, connected account probe, schema-validated artifact, or owner-reviewed first result. The UI reads the saved state; chat text alone never completes a check. This extends the current `TEMPLATE_SETUP.json` behavior, which records `completed_steps` but has no per-check evidence or staleness.

The Automation setup button opens the Automation Builder chat with its installation ID, current goal, slot bindings, and blocked checks. The Builder may propose a plan and create or bind Crews through authorized controls. It must save the proposed Workflow steps and capability resolutions before reporting ready. The installer never treats an agent's generic `Setup complete` label as proof that it can fill a particular slot; it verifies the slot's required outputs, access, and test call.

An illustrative saved state connects the catalog recipe to real customer instances without embedding credentials:

```json
{
  "schema_version": 2,
  "installation_id": "example-growth-installation",
  "source_id": "website-growth-loop",
  "source_version": "1.0.0",
  "workflow_id": "example-growth-workflow",
  "status": "draft",
  "goal": {
    "site_url": "https://example.test",
    "metric_id": "qualified_organic_visits",
    "baseline_mode": "baseline_first"
  },
  "slots": {
    "strategist": { "crew_profile_id": "work", "crew_project_id": "example-strategist" },
    "search": { "crew_profile_id": "work", "crew_project_id": "example-search" }
  },
  "checks": {
    "team_bindings": { "status": "verified", "evidence_ref": "checks/team-bindings.json" },
    "handoff_contract": { "status": "pending" },
    "test_run": { "status": "pending" }
  },
  "run_policy": { "start": "manual", "enabled": false }
}
```

The example omits other required checks for readability. It is a proposed record shape, not a currently supported runtime schema. The source digest and installer audit data live in the separate pinned receipt.

Status calculation:

| Level | States | Rule |
| --- | --- | --- |
| Crew Agent Playbook | `pending`, `blocked`, `ready`, `outdated` | Ready when every required check is verified with current evidence. Outdated means a newer catalog package exists; it does not revoke readiness by itself. |
| Automation Playbook | `draft`, `blocked`, `ready`, `active`, `paused`, `outdated` | Ready requires goal/metric decisions, at least two distinct ready Crews, all required slot and handoff checks, valid Workflow plan, and a passing bounded test run. Active additionally requires enabled run policy. |
| Run | `queued`, `running`, `awaiting_review`, `succeeded`, `failed`, `canceled` | Run state is independent of setup state. A failed run does not erase the installed Playbook or its receipt. |

Recheck evidence when a Crew is deleted, access revoked, selected skill/server/secret removed, output schema or callable action changed, or an attached resource loses authorization. Block new affected runs and show the specific failed check. For an already running job, stop at the next safe boundary and preserve partial evidence. An unchanged catalog update is not a runtime failure.

### Installer experience

The single **Playbooks** catalog is reachable from both Crew and Automations. The browsing surface supports category navigation, search across title/outcome/tags, kind filter (**Agents** / **Automations**), data/tool requirements, and installed/available status. A result card shows the first useful output and a clear action: **Create Crew member** or **Set up Automation**. Preview shows the team and permissions before an Automation is installed. Catalog results are paginated or virtualized and filtered by the server once the library grows.

**Agent path**

1. Preview outcome, minimum input, bundled skill, optional tools, and the checklist.
2. Choose a Crew name and create it. The installation records the primary Agent Playbook, copies its pinned files, and selects only the bundled local skill. No external MCP, channel, or secret is selected automatically.
3. Show `Setup pending` above chat. Clicking it sends the checklist setup request. The Crew asks for the few missing inputs, verifies its files and access, produces the first result, and saves check evidence.
4. Show `Ready` only after required checks pass. The user can later add a capability pack from Identity; it has separate checks and cannot overwrite the primary identity.

**Automation path**

1. Preview outcome, team diagram/list, first result, required information and integrations, handoff sequence, approvals, cadence options, and a cost/budget estimate range when available.
2. Create a draft Automation with the pinned Playbook receipt. Collect the business goal, audience/scope, metric, baseline or baseline-first choice, named owner, spending ceiling, and excluded actions. Do not auto-enable a trigger or schedule.
3. For each slot, show matching existing Crews and **Create specialist**. Matching uses actual owner access, Playbook/capability IDs, output contract, and setup state. If the owner chooses a matching Crew that lacks one optional tool, show the reduced behavior honestly.
4. Reuse shared business context once. Resolve each Crew's MCP, skills, secrets, folders, and channels under its own permissions. Explicitly review any additional read/write scope before attaching it. Existing Crew accounts stay with that Crew; no credential is copied to the Automation or other Crews.
5. Generate a proposed Workflow plan with Crew steps and deterministic validation/approval steps. Save it as a draft. Show exactly which Crew will receive which input and what artifact it must return.
6. Run setup checks in Builder chat. Where checks belong to a Crew, link to that Crew's chat and bring completion status back to the Automation. Test each required handoff with bounded data and inspect schemas and source references.
7. Configure a manual, scheduled, or authenticated event start. Choose timezone, cadence/filter, concurrency, retries, timeout, notification, approval points, and budget. Save triggers paused. Run one authorized test and display the result alongside the plan.
8. Present a final review card: team, bindings, scopes, cadence, estimated cost, first run, open decisions. Enable only when the owner activates the reviewed Automation. The owner can pause, resume, replace a slot, or update the Playbook later.

The Automation setup screen has five compact sections: **Goal**, **Team**, **Access**, **Test**, and **Run policy**. Each section shows ready/needs attention and opens the relevant chat or control. It should never be a 100-field form. The chat handles discovery; the screen makes progress, evidence, and decisions inspectable. The team view displays bound Crew names and a one-line handoff between them. If a Crew is reused by another Automation, each Automation owns its own run policy and slot binding.

### Capability and security resolution

Resolve provider-neutral capabilities at install time. For example, `read_site_pages` can be satisfied by public web access or an approved export; `read_search_performance` can be satisfied by Search Console or a compatible uploaded report. The Playbook says what is needed, while the customer chooses an available provider and account. The installer records the chosen method and a probe result, not credentials. Optional capabilities can be omitted with a visible reduction in the output promise.

An Automation may read a Crew artifact through an authorized read-only attachment, or invoke it through an internal trigger/Crew step. Both are supported building blocks today. The Workflow Builder currently offers `manage_crew_attachment`, `manage_crew_trigger`, and `create_crew`; Crew steps already run through the Workflow engine with preflight access checks. The v2 installer should compose these server controls instead of adding a second path around their authorization. `create_crew` currently requires an explicitly approved Builder proposal; the new installer must capture the same concrete approval in its UI or chat before creating Crew projects.

Callable Crew functions are a future typed interface. The first release can pass a bounded JSON artifact through existing Crew steps, validate it in a deterministic Workflow step, and retain the artifact in the Automation run. A v2 manifest may declare a future function contract, but the UI must label it **proposed** until the runtime can register and enforce it. Never claim a function was installed because a skill file describes it.

Cross-user Crew use requires the platform's actual sharing and caller authorization. A readable/shared Crew is not automatically runnable by another user's Automation, and an owner cannot lend connection scopes merely by binding the Crew. The server checks both Automation edit/run access and Crew invoke/read access on bind and again at run start. Keep every Crew invocation attributable to the Automation run, Crew ID, caller, and approved scope.

### Workflow plan and handoff execution

The Automation Playbook compiles into a **proposal** for the existing Workflow plan, not an opaque second runner. A proposed Crew step stores its slot ID, bound Crew project ID, internal trigger ID, instruction, input artifact refs, expected output schema, timeout, and retry policy. A deterministic step validates its output before the next Crew receives it. The installer records the mapping from Playbook slot/handoff IDs to actual plan step IDs so updates and run views can explain what happened.

A handoff envelope should include `automation_installation_id`, `workflow_run_id`, `handoff_id`, `producer_step_id`, `artifact_type`, `schema_version`, `artifact_ref`, `created_at`, and a content digest. Sensitive artifacts remain in the authorized Workflow/Crew storage and are passed by checked reference where possible. The consumer sees only fields allowed by its contract. Each Crew run records its own run ID, input digest, output ref, cost, status, and error so the Automation dashboard can drill down without parsing chat prose.

For retries, use the Workflow run and handoff ID as the idempotency scope. If a Crew step succeeded and its artifact validates, a retry adopts the same result; it does not repeat an external side effect. If the step failed or timed out, retry only within the configured bound and preserve prior attempts. If a human rejects an artifact, return to the producing step with explicit feedback; do not silently promote a rejected draft. Existing Crew-step retry and run-ID logic is a useful base, but artifact validation and approval links are new work.

Do not create one Crew per deterministic step. A specialist Crew should own a durable skill, context, or continuing responsibility. Ordinary extraction, schema checking, routing, or notification can remain Workflow steps. This keeps the team understandable and costs bounded.

### Example: Website Growth Loop v1

The first installable Automation can use two agents while the fuller catalog is built:

| Slot | Minimum Agent Playbook | Required output | Required access |
| --- | --- | --- | --- |
| `strategist` | Website Growth Starter (available Crew template, migrated to Agent Playbook) | `growth-priority-brief/v1`: site scope, target audience, prioritized opportunities, evidence links, uncertainty. | Public site or owner-provided export; owner goal. |
| `search` | SEO Analyst or Search Opportunity Mapper (new Agent Playbook to build) | `search-opportunity-list/v1`: page/question opportunities, source evidence, priority and open questions. | Public site; Search Console only if the selected method needs it. |

The v1 Workflow starts manually. It asks the strategist for a bounded brief, validates it, passes the brief to the search specialist, validates the opportunity list, and presents a combined reviewable action plan. This proves binding, handoff, evidence, and test-run visibility without depending on CMS publishing or external outreach. A later version can add the Content Brief Writer and Traffic & Engagement Analyst slots, approval-gated page work, and a weekly measurement cadence.

Example Automation setup checks (stable IDs in the package):

| Check ID | Evidence required |
| --- | --- |
| `goal_owner` | Named owner, canonical site URL, offer, audience, and target visitor action. |
| `metric_policy` | Agreed useful-traffic or conversion metric; actual baseline or `baseline_first` decision and measurement start date. |
| `team_bindings` | Two distinct authorized Crew IDs, each meeting its slot contract. |
| `site_scope` | Public pages or approved export inspected, with crawl boundaries saved. |
| `capabilities` | Required skills selected and any selected MCP/account probe passed for the Crew that uses it. |
| `handoff_contract` | Strategist output validates as `growth-priority-brief/v1`; search specialist accepts that artifact. |
| `plan_review` | Draft Workflow plan, cost/budget limit, and approval point inspected by owner. |
| `test_run` | One bounded run with both Crew run IDs, validated artifacts, and a combined result. |
| `activation` | Manual-only choice or a reviewed paused schedule/event configuration; recurrence enabled separately. |

For a newly launched site, `baseline_first` is a valid setup decision. The first run reports what can be observed today and what cannot yet be measured. No package may imply that traffic increased before the measurement window exists. Publishing a page and sending outreach remain separate approval-gated actions.

The same model applies to Finance: a Finance Analyst Crew may install a Tax Export capability pack and perform both jobs interactively. A **Weekly Finance Review** Automation could bind that Crew as the analysis slot and a distinct Cash Flow Planner Crew as the forecast slot, passing a reconciled finance brief between them. If the customer only wants the Finance Analyst to send its own weekly brief, that is a Crew schedule, not a multi-agent Automation Playbook. The catalog should label these three choices clearly.

### Updates, replacement, and removal

Installed instances pin versions and digests. A newer catalog version appears as **Update available** with authored release notes and a diff of role, checks, capabilities, slots, handoffs, approvals, and run policy. Update creates a draft proposal, preserves customer inputs/bindings and evidence where the contract is unchanged, and marks affected checks `stale`. It cannot silently alter an active Workflow plan, trigger, schedule, Crew role, or connection selection. Major changes require explicit remapping and another test run.

Replacing a Crew in one slot pauses the affected Automation, checks the replacement's identity/access/output contract, tests the incoming and outgoing handoffs, and resumes only after review. Other Automations using the old Crew are unaffected. Removing an Agent Playbook from a Crew that still fills a required Automation slot is blocked until the owner replaces the slot or pauses/removes that Automation. Removing an Automation Playbook stops its installed run policy and unbinds its slot/attachment links only after showing the impact; it does not delete Crew projects or their independent schedules and data.

### Migration and delivery slices

| Slice | Build | Done when |
| --- | --- | --- |
| 1. Catalog and receipt | v2 schema/validator, indexed catalog API, pinned receipts, install idempotency, kind-aware browsing. | An Agent Playbook can be installed without overwriting another Crew's state; 100+ fixture entries can be searched and previewed. |
| 2. Agent setup | Primary role vs capability pack model, check evidence, Crew chat setup, Identity display, migration of current integer-version templates. | Finance Analyst and Website Growth Starter retain existing Crews and checklist progress; new installs yield a verifiable first result. |
| 3. Team draft | Automation Playbook draft, slot matching/binding, goal/metric capture, plan proposal, Crew access checks. | A user can bind two distinct Crews to Website Growth Loop and see every missing dependency before a run. |
| 4. Handoff proof | Internal trigger/Crew-step wiring, bounded artifact contracts, deterministic validation, run evidence and cost links. | A manual two-Crew test passes and can be inspected step by step; revoked access or invalid output blocks it. |
| 5. Activation and updates | Paused schedule/event setup, review card, activation, drift detection, update/rebind flow. | The owner can activate, pause, recover, and upgrade without a silent role or permission change. |

Current implementation touchpoints: `frontend/src/products/work/crewTemplates.ts` and `workSessions.ts` define/copy the three Crew templates; `WorkTemplateSetup.tsx` displays their chat-led checklist; `frontend/src/platform/chat/productProjects.ts` stores Crew template IDs and versions; `agent_go/cmd/server/crew_builder_tools.go` exposes Crew creation, internal triggers, and attachments to the Workflow Builder; `agent_go/cmd/server/crew_step_runner.go` invokes Crew steps with preflight checks; `playbooks/spec/PLAYBOOK-SPEC-v1.md` defines the separate Workflow Builder guidance format. These are integration points, not proof that v2 is implemented.

Acceptance tests should cover catalog validation, duplicate install retries, partial-install recovery, primary-role preservation, owner and shared-Crew access, account-scope isolation, optional-capability skips, stale evidence, two distinct Crew bindings, schema-invalid handoff, timeout/retry idempotency, an approval pause, version update without active-plan mutation, and the full Website Growth manual run. A 100+ item catalog UI check should include keyboard search, filters, responsive layout, and a preserved selection while filters change.
