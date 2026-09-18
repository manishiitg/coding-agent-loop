# WORKFLOW HELP GUIDE

Use this guide when a user asks what a workflow concept or workspace view means,
or when an **Ask AI** action opens a conversation from a view. It is an
explanation guide, not authority to edit configuration or run work.

## How to help

1. Answer the user's question first in plain language. Define unfamiliar terms
   once and avoid internal implementation names unless they clarify the answer.
2. Use the named view and current workflow as context. Read only the artifacts
   or state needed to distinguish what is configured from what is merely
   possible. Never invent a status, result, metric, or recommendation.
3. Explain in this order: what this area is for, what the user is seeing, how it
   relates to the workflow, and the most relevant next action.
4. Ask a focused question only when the user's desired outcome is unclear.
   Otherwise answer directly and offer up to three useful next actions.
5. Do not edit, configure, approve, or run anything unless the user asks. If
   they do, load the specific reference for that action before changing it.

## Core mental model

- **Workflow**: a reusable system that coordinates instructions, tools, data,
  and execution toward an outcome.
- **Plan**: the workflow blueprint. It shows what should happen and in what
  order; it is not the history of what already ran.
- **Group**: one unit of context, such as an account, region, product, or test
  target. Selected plan steps run for each selected group.
- **Step**: one bounded unit of work. It may use an agent conversation, a saved
  deterministic script, a message sequence, an orchestrator, a route or branch,
  or a human decision.
- **Route**: a major choice between sub-workflows. A **branch** is a smaller
  choice inside a flow. Both must make their selection explicit.
- **Run**: one execution of the plan. Its status, outputs, evidence, logs, and
  costs describe what actually happened.
- **Dashboard**: the workflow's user-facing results and metrics, built from
  durable evidence. Explain data source and freshness when known.
- **Pulse**: recurring reviews that surface findings, recommendations, and
  decisions. A Pulse review is separate from executing the main plan.
- **Evaluation**: checks whether a run's outputs meet defined quality criteria.
- **Knowledgebase**: user-provided context and workflow findings. **Learnings**
  capture reusable operating know-how. **Database** stores structured durable
  records. **Files** show the underlying workspace artifacts.

## Ask AI triage contract

When a user clicks **Ask AI** from a tab, assume they are stuck in that exact
area. Do not answer with only a generic definition of the tab. The useful answer
is: what state AgentWorks can see, what likely blocked the user, and the next
safe action inside this platform.

For every Ask AI handoff:

1. Treat the provided tab message as the starting symptom, not as permission to
   make changes.
2. Inspect the current workflow/platform state for that tab before giving a
   solution when tools are available. If a tool is missing, say what could not be
   verified.
3. Separate three layers: account/platform connection, workflow selection or
   allowlist, and run-time use by a workflow step or schedule.
4. If the user mentions an error, stale UI, missing option, failed auth, or
   blocked run, diagnose that concrete failure first.
5. Never ask for secrets, OAuth codes, state parameters, cookies, or tokens in
   chat. Use the setup UI, OAuth flow, or secret tools.
6. After explaining, give one clear action you can take now. If mutation is
   needed, ask only for the missing decision, not for information AgentWorks can
   inspect itself.

## Setup tab failure guide

- **MCP server**: users usually click Ask AI here because chat could not connect
  an app, could not produce an authorization URL, listed the server but exposed
  no tools, or a workflow run says OAuth/authentication failed. First inspect the
  account-level server entry, OAuth/discovery status, workflow `selected_servers`
  and `selected_tools`, and server logs. Explain whether the problem is catalog
  install, OAuth/DCR, missing account consent, workflow allowlisting, or a tool
  name mismatch. If connecting requires user consent, guide them to the platform
  auth flow; do not request credentials in chat. When adding a server to a
  workflow, include tool allowlisting such as `Server:*` unless a narrower set
  is requested. If the needed MCP is not listed, and the user asks to add it,
  search the web, MCP catalogs, and official provider documentation for a
  suitable MCP server, then install or register it through the supported
  platform tools. After install, verify discovery/auth status and add it to the
  workflow only after the required tools are visible. If that MCP needs
  operating guidance, also search for or create a matching skill and attach it to
  the workspace so future workflow steps know how to use the server correctly.
- **Secrets**: users are blocked because a workflow needs a credential, a secret
  is named differently than the step expects, or a run cannot read it. Inspect
  attached secret names and expected environment/config names. Explain the exact
  missing name or mismatch and use secret setup tools/UI for values; never expose
  or ask for the value in chat.
- **Skills**: users are blocked because Builder does not know a domain-specific
  procedure, an installed playbook expects a skill, or a step keeps repeating
  generic attempts. Inspect selected skills and read the relevant skill before
  advising. If a skill is missing, propose the smallest skill to attach and state
  what behavior it would teach.
- **Playbooks**: users are blocked choosing how to configure a workflow pattern.
  Inspect the installed playbook record, setup inputs, required capabilities,
  and current workflow plan. Explain what the playbook can reuse, what is still
  missing, and which user decisions are required before edits.
- **Browser access**: users are blocked by login pages, CAPTCHA, disconnected
  CDP/browser state, or a workflow/test that cannot see the intended page.
  Inspect browser status, the configured browser mode/port, recent browser/test
  logs, and whether the task needs an authenticated human-controlled session.
  Distinguish a live agent browser from watch-only Playwright output.
- **LLM configuration**: users are blocked by model failures, poor output, high
  cost, provider auth, or the wrong model running a step. Inspect workflow LLM
  tiers, per-step overrides, selected provider credentials, and cost/execution
  evidence before recommending a model change.
- **Bots**: users are blocked because Slack/WhatsApp receives nothing, replies in
  the wrong workflow, lacks access, or sends too little detail. Inspect bot
  enablement, channel routes, route grant, workflow access, selected workflow,
  and recent bot connector logs. Explain whether the issue is shared bot setup,
  channel route mapping, sender access, or run/session state.
- **Folders and Access**: users are blocked because the workflow cannot see a
  file/folder, cannot write output, or another user cannot open the workflow.
  Inspect attached folders, grants, ownership/read-only state, and the target
  path. Explain whether the fix is attaching a folder, granting a user, or
  changing where the workflow reads/writes.

## Operational tab failure guide

- **Schedules**: users are blocked because a schedule did not run, ran the wrong
  instruction, paused, or has no next run. Inspect schedule config, pause state,
  last/next run, trigger payload, and recent run folder before editing cadence.
- **Webhooks**: users are blocked because an external service cannot trigger the
  workflow, auth fails, JSON shape is wrong, or runs are missing. Inspect webhook
  URL/config, auth mode, saved message, delivery history if available, and
  whether it targets project chat or workflow execution.
- **Execution logs**: users are blocked by a failed step or unclear output.
  Inspect the selected run folder, earliest fatal log entry, step output,
  pre-validation, and final summary. Do not infer success from a later cleanup
  line or generic completion text.
- **Cost**: users are blocked by unexpected spend. Inspect the cost ledger by
  run/group/step and separate LLM cost from tool cost before suggesting a model,
  caching, routing, or step-scope change.
- **Evaluation**: users are blocked because scores are low, missing, stale, or
  do not match expectations. Inspect `evaluation/evaluation_plan.json`, the
  selected run outputs, evaluator logs, and whether evaluation has actually run.
- **Knowledgebase**: users are blocked because Builder forgot context or is using
  stale/incorrect knowledge. Inspect context notes, source files, freshness, and
  conflicts before updating or relying on knowledge.
- **Learnings**: users are blocked because the workflow repeats a mistake.
  Inspect global and step learnings, then propose a concrete learning update tied
  to the observed failure.
- **Database**: users are blocked because rows are missing, stale, malformed, or
  the dashboard reads the wrong table. Inspect schema/README contracts and the
  relevant rows before proposing scripts or migrations.
- **Notifications**: users are blocked because updates are not delivered, go to
  the wrong channel/recipient, or omit details. Inspect notification config,
  delivery status, destination secret names, and channel-specific rules.
- **Backup**: users are blocked because backup is not configured, local-only,
  unverified, or failing against a remote destination. Inspect config,
  destination status, `backup/status.json`, last error, and whether large
  artifacts need object storage instead of Git.
- **Publish**: users are blocked because the public dashboard is missing, stale,
  private unexpectedly, or deployed to the wrong host. Inspect publish config,
  status URL, target artifacts, visibility/password mode, and last deploy error.

## View-specific emphasis

- In **Plan**, explain the goal, groups, sequence, selected step, dependencies,
  and the difference between editing the blueprint and running it.
- In **Dashboard**, explain the visible result, metric meaning, evidence source,
  freshness, and missing data before suggesting improvements.
- In **Pulse**, explain the selected review area, current findings, review
  history, and pending decisions. Discussion does not approve a decision.
- In setup or operational views, explain what is currently configured, what the
  view controls, and how it affects plan execution before suggesting changes.

For the exact purpose of each workspace view or to open one for the user, load
`references/workspace-views.md`. For design or mutation, load the relevant
specialist reference rather than expanding this guide.

## Slack bot setup and channel routes

For requests to connect Slack, add a channel route, change a bot grant, block senders, or configure Slack triggers, load `builder-reference/references/slack-bot-routing.md`. Use `get_slack_bot_settings` to inspect the current workflow before proposing or changing configuration. The route tools are scoped to this workflow; shared credentials and enablement require an operator and may use the settings UI or `configure_slack_bot`. Explain setup directly when asked and use the route tools when the user requests a change. Never ask for bot/app tokens in chat.
