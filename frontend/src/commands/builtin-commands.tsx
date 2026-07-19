import React from 'react'
import { FileText, Server, Cpu, Bot, Layers, Minimize2, RefreshCw, GitBranch, CheckCircle, Search, BookOpen, Activity, BellRing, Cloud, Globe, Target } from 'lucide-react'
import type { CommandContext, CommandDefinition } from './types'

function submitGuidedWorkflowCommand(
  ctx: CommandContext,
  kind: string,
  options: { runFolder?: string | null; background?: boolean } = {}
) {
  const focus = ctx.beforeSlash.trim()
  const args = [
    `kind=${JSON.stringify(kind)}`,
    `focus=${JSON.stringify(focus)}`,
  ]
  if (options.runFolder !== undefined) {
    args.push(`run_folder=${JSON.stringify(options.runFolder || '')}`)
  }
  const guidanceCall = `get_workflow_command_guidance(${args.join(', ')})`

  // Read-only reviews run as a background task so the chat stays responsive. The
  // parent presents the complete result after the completion notification.
  if (options.background) {
    const outputContract = ctx.workshopMode === 'run'
      ? 'Return findings in chat only; do not write or edit any workspace file.'
      : 'Write recommendations to builder/improve.html as required by the returned guidance.'
    const instruction =
      `Call ${guidanceCall} and follow the returned instructions verbatim. ${outputContract} ` +
      `Treat focus as the request context before the slash command. The tool returns the canonical guided-flow text; do not paraphrase or skip its steps.`
    ctx.onSubmit(
      `Run the /${kind} review as a BACKGROUND task so this chat stays responsive. ` +
      `If the run_in_background tool is available: call run_in_background(name=${JSON.stringify(kind + ' review')}, instruction=${JSON.stringify(instruction)}) and do NOT perform the review yourself this turn — you'll get a completion notification, then present a short executive summary followed by every finding and recommendation in severity order. Do not truncate the result to a Top 3. ` +
      `If run_in_background is not available, perform the review inline this turn instead.`
    )
    return
  }

  ctx.onSubmit(
    `Call ${guidanceCall} and follow the returned instructions verbatim. ` +
    `Treat focus as the conversation/request context that appeared before the slash command, including the user's recent constraints and intent. ` +
    `The tool returns the canonical guided-flow text for this command — do not paraphrase or skip its steps.`
  )
}

export const builtinCommands: CommandDefinition[] = [
  {
    command: 'design-plan',
    description: 'Comprehensively review the plan, dependent artifacts, and better design options',
    icon: <GitBranch className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: ['workshop', 'run'],
    source: 'builtin',
    execute: (ctx) => {
      // design-plan already delegates its expensive audit to the dedicated
      // review_plan background tool. Keep the coordinating turn in the main
      // conversation so its completion notification can resume synthesis and
      // persist the final open findings.
      submitGuidedWorkflowCommand(ctx, 'design-plan')
    }
  },
  {
    command: 'review-speed',
    description: 'Review automation latency and how to make it faster',
    icon: <Minimize2 className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'workshop',
    source: 'builtin',
    execute: (ctx) => {
      const runFolder = ctx.getWorkflowStore().selectedRunFolder
      submitGuidedWorkflowCommand(ctx, 'review-speed', { runFolder, background: true })
    }
  },
  {
    command: 'review-cost',
    description: 'Review automation cost and how to reduce it safely',
    icon: <Cpu className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'workshop',
    source: 'builtin',
    execute: (ctx) => {
      const runFolder = ctx.getWorkflowStore().selectedRunFolder
      submitGuidedWorkflowCommand(ctx, 'review-cost', { runFolder, background: true })
    }
  },
  {
    command: 'review-artifact-drift',
    description: 'Check whether artifacts drifted from recent plan changes',
    icon: <RefreshCw className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: ['workshop'],
    source: 'builtin',
    execute: (ctx) => {
      submitGuidedWorkflowCommand(ctx, 'review-artifact-drift', { background: true })
    }
  },
  {
    command: 'improve-knowledge',
    description: 'Improve knowledge notes with targeted cleanup or cross-step consolidation',
    icon: <Layers className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: ['workshop'],
    source: 'builtin',
    execute: (ctx) => {
      submitGuidedWorkflowCommand(ctx, 'improve-knowledge')
    }
  },
  {
    command: 'improve-learnings',
    description: 'Improve global learnings with targeted cleanup or current-plan consolidation',
    icon: <BookOpen className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: ['workshop'],
    source: 'builtin',
    execute: (ctx) => {
      submitGuidedWorkflowCommand(ctx, 'improve-learnings')
    }
  },
  {
    command: 'improve-database',
    description: 'Improve durable data contracts, schemas, and report compatibility',
    icon: <Server className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: ['workshop'],
    source: 'builtin',
    execute: (ctx) => {
      submitGuidedWorkflowCommand(ctx, 'improve-database')
    }
  },
  {
    command: 'design-reporting-ui',
    description: 'Design the reporting UI from scratch: pick HTML (live data) or Markdown documents and build them',
    icon: <CheckCircle className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: ['workshop'],
    source: 'builtin',
    execute: (ctx) => {
      submitGuidedWorkflowCommand(ctx, 'design-reporting-ui')
    }
  },
  {
    command: 'improve-report',
    description: 'Improve the report dashboard for goal tracking, plan context, issues, and live data clarity',
    icon: <CheckCircle className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: ['workshop'],
    source: 'builtin',
    execute: (ctx) => {
      submitGuidedWorkflowCommand(ctx, 'improve-report')
    }
  },
  {
    command: 'improve-evaluation',
    description: 'Validate evaluation/evaluation_plan.json and improve goal/criteria coverage',
    icon: <CheckCircle className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'workshop',
    source: 'builtin',
    execute: (ctx) => {
      const runFolder = ctx.getWorkflowStore().selectedRunFolder
      submitGuidedWorkflowCommand(ctx, 'improve-evaluation', { runFolder })
    }
  },
  {
    command: 'pulse',
    description: 'Run one complete Pulse now against the latest retained run',
    icon: <Activity className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'workshop',
    source: 'builtin',
    execute: (ctx) => {
      const runFolder = ctx.getWorkflowStore().selectedRunFolder
      submitGuidedWorkflowCommand(ctx, 'pulse', { runFolder })
    }
  },
  {
    command: 'pulse-setup',
    description: 'Enable Pulse and configure the recurring workflow run schedule',
    icon: <RefreshCw className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'workshop',
    source: 'builtin',
    execute: (ctx) => {
      submitGuidedWorkflowCommand(ctx, 'pulse-setup')
    }
  },
  {
    command: 'bug-review',
    description: 'Run the Pulse QA and logic-bug review without applying fixes',
    icon: <Search className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'workshop',
    source: 'builtin',
    execute: (ctx) => {
      const runFolder = ctx.getWorkflowStore().selectedRunFolder
      submitGuidedWorkflowCommand(ctx, 'bug-review', { runFolder, background: true })
    }
  },
  {
    command: 'llm-ops-review',
    description: 'Review model tiers, cost, latency, fallbacks, backup, publish, and notify setup',
    icon: <Cpu className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'workshop',
    source: 'builtin',
    execute: (ctx) => {
      const runFolder = ctx.getWorkflowStore().selectedRunFolder
      submitGuidedWorkflowCommand(ctx, 'llm-ops-review', { runFolder, background: true })
    }
  },
  {
    command: 'pulse-fixer',
    description: 'Apply and verify safe fixes from existing Pulse review findings',
    icon: <CheckCircle className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'workshop',
    source: 'builtin',
    execute: (ctx) => {
      submitGuidedWorkflowCommand(ctx, 'pulse-fixer')
    }
  },
  {
    command: 'goal-advisor',
    description: 'Run a one-off strategic Goal Advisor review without changing Pulse setup',
    icon: <Bot className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'workshop',
    source: 'builtin',
    execute: (ctx) => {
      submitGuidedWorkflowCommand(ctx, 'goal-advisor', { background: true })
    }
  },
  {
    command: 'review-code',
    description: 'Review saved scripts (main.py) against step descriptions to detect drift',
    icon: <FileText className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'workshop',
    source: 'builtin',
    execute: (ctx) => {
      submitGuidedWorkflowCommand(ctx, 'review-code', { background: true })
    }
  },
  {
    command: 'backup',
    description: 'Set up, run, or restore this automation’s backup',
    icon: <Cloud className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'workshop',
    source: 'builtin',
    execute: (ctx) => {
      const instruction = `Help me set up or run backup for this workflow. Call get_reference_doc(kind="backup-strategy"), then read workflow.json.backup and backup/status.json.
- If backup is NOT configured yet: recommend a private GitHub repository or another off-device destination first. Ask for the account/org, private visibility, and repository/bucket name before creating or connecting it. A local Git checkpoint is acceptable temporarily, but label it local-only and not durable; do not report it as a healthy backup.
- If backup IS configured: run a backup now and report the result (destinations, commit/ref).
- If I asked to restore: restore the tracked files from the latest backup (or a commit I name) instead.
Always write backup/status.json; never write operational status into workflow.json.`
      ctx.onSubmit(ctx.beforeSlash ? `${ctx.beforeSlash}\n\n${instruction}` : instruction)
    }
  },
  {
    command: 'publish',
    description: 'Set up or publish this automation’s Pulse log & report to a public URL',
    icon: <Globe className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'workshop',
    source: 'builtin',
    execute: (ctx) => {
      const instruction = `Help me set up or run publish for this workflow. Call get_reference_doc(kind="publish-strategy") and follow it exactly, then read workflow.json.publish and publish/status.json.
- If publish is NOT configured: set it up — ask me which static host (Netlify / Vercel / Cloudflare Pages / Cloudflare R2 / S3 / any). As soon as I pick one, AUTO-CHECK its CLI (command -v) and INSTALL it for me if missing (announce it, e.g. "Installing the Vercel CLI…", then run npm i -g <cli>); do NOT ask me for an access token/API key — the path is install CLI → I run <cli> login once → you deploy. Default visibility is PRIVATE via a simple password gate (StatiCrypt with $SECRET_PUBLISH_PASSWORD and the Runloop dark gate styling from the reference doc); ask me to set a PUBLISH_PASSWORD secret, or confirm if I want it fully public instead. Then write workflow.json.publish and publish/status.json with state "configured_not_verified". Do not publish yet.
- If publish IS configured: publish now. Publish BOTH artifacts — bake the report dashboard to static HTML AND publish the Pulse log (builder/improve.html); deploy dashboard.html + pulse.html + the nav index.html wrapper per the reference doc. If publish.targets only lists one, update it to include both first. Force every page to **DARK only** (matching the app) — set BOTH class="dark" and data-theme="dark" on the html element per the reference doc; no toggle, do NOT use prefers-color-scheme. Stage the files in a /tmp dir; if visibility is private, encrypt them with StatiCrypt ($SECRET_PUBLISH_PASSWORD) and apply the Runloop dark password-gate styling before deploying; run the deploy CLI from /tmp. Then give me the URL and confirm visibility + what's public.
CRITICAL — after deploying, come BACK to the workflow folder and persist state there (never in the /tmp staging dir): set workflow.json.publish.enabled=true with the destination + top-level url, AND write publish/status.json with state "published", the url, and last_source_hash (= the current_source_hash the backend reports; leave empty if unknown). A deploy that doesn't write these shows a grey "not configured" dot even though the site is live.
Always write publish/status.json.`
      ctx.onSubmit(ctx.beforeSlash ? `${ctx.beforeSlash}\n\n${instruction}` : instruction)
    }
  },
  {
    command: 'notify',
    description: 'Set up, review, or test agentic notifications',
    icon: <BellRing className="w-4 h-4" />,
    modes: ['workflow', 'multi-agent'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'workshop',
    source: 'builtin',
    execute: (ctx) => {
      if (ctx.modeCategory === 'multi-agent') {
        const instruction = `Help me set up or review notifications for Chief of Staff.
- First review the saved Chief of Staff notification configuration. Explain the effective destinations and whether the Slack webhook secret reference is healthy. Never reveal or write a webhook URL to config files, prompts, logs, or ordinary files.
- Notifications are agentic: Chief of Staff decides when a non-blocking FYI, alert, progress update, or completion notice is useful and chooses the content. Delivery is deterministic: call notify_user and let the backend apply the configured Chief of Staff Slack webhook plus enabled account-level notification channels. Slack is rich Block Kit by default; for structured summaries set slack_title, factual slack_color, slack_fields, slack_sections, and slack_footer on that same call. Never access or post to a webhook URL directly. The same setting applies to interactive Chief chats and scheduled Chief/Org Pulse runs.
- Ask what events should notify and what a useful message should contain. Treat those as agent guidance, not routing. If I explicitly want the preference remembered, confirm it and use the existing Chief of Staff memory mechanism; never put preferences or credentials in the capabilities JSON.
- To configure Slack, call list_secrets first. If I provide a new Slack Incoming Webhook URL, store it with set_user_secret(name="SLACK_NOTIFICATION_WEBHOOK_URL", value=<url>), then call update_chief_of_staff_notifications(slack_webhook_secret_name="SLACK_NOTIFICATION_WEBHOOK_URL"). The configuration tool validates the encrypted secret. To disable the dedicated webhook, call update_chief_of_staff_notifications(slack_webhook_secret_name="").
- Gmail is an inherited account-level notification channel.
- Do not add a routing step or notification schedule merely to choose a channel.
- If I ask to test delivery, call notify_user once with a clearly labeled test message and report its delivered/skipped/failed channels honestly. Do not test unless requested.
- human_feedback is separate: use it only for short-lived input that must block this run, such as OTP, CAPTCHA, or immediate approval.`
        ctx.onSubmit(ctx.beforeSlash ? `${ctx.beforeSlash}\n\n${instruction}` : instruction)
        return
      }
      const instruction = `Help me set up or review notifications for this workflow.
- First read the current workflow configuration and soul/soul.md. Explain the current effective destinations and whether the Slack webhook secret reference is healthy. Never reveal or write a webhook URL to workflow.json, prompts, logs, or ordinary files.
- Notifications are agentic: the agent decides when a non-blocking FYI, alert, progress update, or completion notice is useful and chooses the content. Delivery is deterministic: the agent calls notify_user and the backend automatically applies the workflow Slack webhook plus enabled account-level notification channels. Slack is rich Block Kit by default; for structured summaries set slack_title, factual slack_color, slack_fields, slack_sections, and slack_footer on that same call. Never access a SECRET_* webhook variable, post with curl, or disable automatic Slack delivery to avoid a duplicate. Do not add a routing step merely to choose a notification channel.
- Ask what events should notify and what a useful message should contain. Put only explicit, durable user-approved notification preferences in soul/soul.md; do not store temporary choices or credentials there.
- To configure a workflow Slack Incoming Webhook, use list_secrets first. If I provide a new URL, store it with set_workflow_secret(name="SLACK_NOTIFICATION_WEBHOOK_URL", value=<url>), then call update_workflow_config(slack_webhook_secret_name="SLACK_NOTIFICATION_WEBHOOK_URL"). The configuration tool validates the encrypted secret, makes it backend-only, and removes it from agent-visible secret injection. To disable workflow webhook delivery, call update_workflow_config(slack_webhook_secret_name="").
- Gmail is an inherited account-level notification channel. The agent may set email_to/email_cc only when an explicit workflow preference names those recipients; otherwise it uses the configured account default.
- If I asked to test delivery, call notify_user once with a clearly labeled test message and report its returned delivered/skipped/failed channels honestly. Do not send a test unless I requested one.
- human_feedback is separate: use it only for short-lived input that must block this run, such as OTP, CAPTCHA, or immediate approval.`
      ctx.onSubmit(ctx.beforeSlash ? `${ctx.beforeSlash}\n\n${instruction}` : instruction)
    }
  },
  {
    command: 'org-setup',
    description: 'Set org goals, align automations, and configure Daily Org Pulse',
    icon: <Target className="w-4 h-4" />,
    modes: ['multi-agent'],
    source: 'builtin',
    execute: (ctx) => {
      const appStore = ctx.getAppStore()
      appStore.setWorkspaceMinimized(false)
      appStore.setMultiAgentRightPanelView?.('org-goals')

      const focus = ctx.beforeSlash.trim()
      const instruction = `Set up org goals and Daily Org Pulse.

Call get_reference_doc(kind="org-goals") and follow it. Before writing or changing goals HTML, also call get_reference_doc(kind="backup-strategy") and back up org-level artifacts using pulse/backup.json and pulse/backup/status.json, then call get_reference_doc(kind="org-html") and use its Goals skeleton.
Read pulse/goals.html if it exists.
Review existing workflows and employees, then classify workflows as aligned, supporting/maintenance, or unaligned.

If goals are missing or vague, ask me only the missing questions needed to make them measurable:
- outcome
- horizon
- KPI targets with quantity: metric name, baseline/current value, target value, unit, direction, and target date
- source of truth for each target: workflow report, db table, external system, or manual update
- accountable owner/person
- contributing workflows or employees
- review cadence

Do not create vague goals. Each goal should look like a company operating target. Prefer numeric targets; if a goal is qualitative, convert it into dated milestone/checklist acceptance criteria. Do not invent quantities I did not give you — ask for them or mark the goal as needing a target.

If I gave enough detail${focus ? ' in the request above' : ''}, create or update pulse/goals.html now as a self-contained HTML scorecard. Keep prior goal history unless I explicitly ask to remove it.

After goals are saved, ask whether I want to turn on Daily Org Pulse. If I confirm, enable the built-in Org Pulse schedule (builtin-org-pulse). Do not silently enable it without confirmation.

Also ask whether I want to set up org-level backup and publish:
- backup records config + status in pulse/backup.json and pulse/backup/status.json
- publish shares pulse/goals.html + pulse/org-pulse.html and records config + status in pulse/publish.json and pulse/publish/status.json`

      ctx.onSubmit(focus ? `${focus}\n\n${instruction}` : instruction)
    }
  },
  {
    command: 'pulse-setup',
    description: 'Set up or tune Daily Org Pulse',
    icon: <Activity className="w-4 h-4" />,
    modes: ['multi-agent'],
    source: 'builtin',
    execute: (ctx) => {
      const appStore = ctx.getAppStore()
      appStore.setWorkspaceMinimized(false)
      appStore.setMultiAgentRightPanelView?.('org-pulse')

      const focus = ctx.beforeSlash.trim()
      const instruction = `Set up Daily Org Pulse.

Call get_reference_doc(kind="org-pulse") and follow it for what Daily Org Pulse should do. Before writing or changing pulse/org-pulse.html, also call get_reference_doc(kind="backup-strategy") and confirm org backup status in pulse/backup.json and pulse/backup/status.json, then call get_reference_doc(kind="org-html") and use its Org Pulse skeleton.
Read pulse/goals.html if it exists, and read pulse/org-pulse.html if it exists.
Read pulse/backup.json, pulse/backup/status.json, pulse/publish.json, and pulse/publish/status.json if they exist.
First call list_multiagent_schedules and find the existing Org Pulse schedule — the built-in builtin-org-pulse AND any other schedule that is really an Org Pulse (its name/description/query mentions "Org Pulse"). Report whether it is enabled, its cron, timezone, and last/next run state.
To enable or configure Daily Org Pulse, ALWAYS call update_multiagent_schedule on builtin-org-pulse (this owns/materializes the built-in). NEVER call create_multiagent_schedule for Org Pulse — a freshly-minted UUID schedule becomes a duplicate that the Org Pulse pill and the scheduler can disagree about. If a duplicate non-builtin Org Pulse schedule already exists, consolidate: enable builtin-org-pulse and disable (or delete) the duplicate so exactly one Org Pulse schedule remains.
Before editing any multi-agent schedule file directly, call get_reference_doc(kind="schedule-management").

If pulse/goals.html is missing or has no measurable goals, explain that Daily Org Pulse can only measure org progress after goals exist. Ask whether I want to run /org-setup first or enable a workflow-health-only pulse temporarily. Do not create goals from this command unless I explicitly ask.

If goals exist, help me choose or confirm:
- enabled or disabled
- cadence and timezone
- whether it should notify only on decision-worthy changes
- whether the current pulse/org-pulse.html needs to be bootstrapped with the org-html skeleton
- whether org backup should be enabled before Daily Org Pulse writes goals/pulse/tasks
- whether org publish should share pulse/goals.html + pulse/org-pulse.html after verified runs

Only enable or change the built-in Org Pulse schedule after I confirm the cadence/timezone. Do not manually run Org Pulse from this command unless I explicitly ask for a one-time run.`

      ctx.onSubmit(focus ? `${focus}\n\n${instruction}` : instruction)
    }
  },
  {
    command: 'org-backup',
    description: 'Set up or run backup for org goals, pulse, and tasks',
    icon: <Cloud className="w-4 h-4" />,
    modes: ['multi-agent'],
    source: 'builtin',
    execute: (ctx) => {
      const appStore = ctx.getAppStore()
      appStore.setWorkspaceMinimized(false)
      appStore.setMultiAgentRightPanelView?.('org-pulse')
      const focus = ctx.beforeSlash.trim()
      const instruction = `Help me set up or run org-level backup.

Call get_reference_doc(kind="backup-strategy") and follow its org-level workflow-style contract. Read pulse/backup.json and pulse/backup/status.json if they exist.

Scope:
- pulse/goals.html
- pulse/org-pulse.html
- pulse/task.html
- employee/org config files
- multi-agent schedules/config

If org backup is NOT configured yet: recommend a private GitHub repository or another off-device destination first. Ask for the account/org, private visibility, and repository/bucket name before creating or connecting it. A local Git checkpoint is acceptable temporarily, but label it local-only and not durable; do not report it as a healthy off-device backup.

If org backup IS configured: run a backup now, skip only if pulse/backup/status.json proves the current source hash is unchanged, and report the result.

Always write pulse/backup/status.json. Never write org backup state into any workflow.json or content HTML file, and never back up secrets.`

      ctx.onSubmit(focus ? `${focus}\n\n${instruction}` : instruction)
    }
  },
  {
    command: 'org-publish',
    description: 'Set up or publish org goals and Org Pulse pages',
    icon: <Globe className="w-4 h-4" />,
    modes: ['multi-agent'],
    source: 'builtin',
    execute: (ctx) => {
      const appStore = ctx.getAppStore()
      appStore.setWorkspaceMinimized(false)
      appStore.setMultiAgentRightPanelView?.('org-pulse')
      const focus = ctx.beforeSlash.trim()
      const instruction = `Help me set up or run org-level publish.

Call get_reference_doc(kind="publish-strategy") and follow its org-level workflow-style contract. Read pulse/publish.json and pulse/publish/status.json if they exist.

Publish scope:
- pulse/goals.html as goals.html
- pulse/org-pulse.html as pulse.html
- an index.html wrapper with Goals | Pulse navigation

If org publish is NOT configured: ask me which static host to use, default to private visibility with a PUBLISH_PASSWORD secret, write pulse/publish.json, and write pulse/publish/status.json with state "configured_not_verified". Do not do the first/verifying publish until I confirm the destination and visibility.

If org publish IS configured and verified: publish now only if the org HTML changed since the last publish. Stage files outside the workspace, force dark mode, deploy, then come back and update pulse/publish/status.json with state "published", the url, and last_source_hash.

Always write pulse/publish/status.json. Never publish secrets or raw task transcripts. Never write org publish state into any workflow.json or content HTML file.`

      ctx.onSubmit(focus ? `${focus}\n\n${instruction}` : instruction)
    }
  },
  {
    command: 'workflow-builder',
    description: 'Turn this conversation into a reusable automation (Workflow/<name>/)',
    icon: <Layers className="w-4 h-4" />,
    modes: ['multi-agent'],
    source: 'builtin',
    execute: (ctx) => {
      const instruction = `Turn our current conversation into a new reusable workflow by calling the \`create_workflow\` tool with a valid workflow.json and plan.json.

## Step 1 — Pick a folder_name AND a display label
Workflows have two separate names:
- **folder_name** (the on-disk path under \`Workflow/\`) — must be **shell-safe kebab-case**: lowercase letters/digits with hyphens between words, no spaces, no underscores, no uppercase, no special characters (e.g. "customer-onboarding", "sales-report", "api-health-check"). 2-5 words, ≤64 chars.
- **label** (the human-readable display name that goes in \`workflow_json.label\`) — can be any string: spaces, capitalization, punctuation, whatever reads naturally (e.g. "Customer Onboarding", "AWS Cost Analysis Q3", "Müller's Pipeline").

If I gave you a label in my preamble, keep it verbatim as the \`label\` and slugify it for the \`folder_name\`. If I gave you a kebab-case name, use it for \`folder_name\` and also as the starting point for \`label\` (titlecased). Otherwise infer both from what we've been working on. If you cannot produce a clean folder_name, ask me one clarifying question instead of proceeding.

## Step 2 — Pick the capabilities from context
Analyze this conversation and select ONLY the MCP servers, skills, and LLM tier settings that are actually relevant to the workflow being extracted. **Do not blindly copy every currently-enabled server and skill — pick the ones the steps actually need.** If a server was enabled in chat but never used for this specific work, leave it out.

For secrets, default to no global secrets: set \`workflow_json.capabilities.selected_global_secret_names\` to \`[]\` unless this specific workflow clearly needs named global secrets. Do not use \`null\` as a default because it means all global secrets.

## Step 3 — Extract the steps
Re-read the conversation and extract the concrete, repeatable steps the workflow should run. Each step must have:
- A stable kebab-case \`id\` (e.g. "fetch-data", "analyze-results"), unique within the plan
- A human \`title\`
- A detailed \`description\` of what the step does, in enough detail that a worker with no memory of this conversation could execute it
- A \`success_criteria\` line describing how to tell the step succeeded
- Optionally \`context_dependencies\` (file names produced by earlier steps) and \`context_output\` (file name this step produces)
- Use \`"type": "regular"\` only for deterministic scripted work such as fixed API/CLI calls, parsing, normalization, and mechanical validation. Use \`"message_sequence"\` for every conversational, judgment-heavy, browser-driven, or adaptive step, even when it needs only one message. Non-scripted regular steps are unsupported. Use \`"routing"\` / \`"human_input"\` / \`"todo_task"\` only when the conversation genuinely calls for branching, human input, or sub-workflow orchestration.

## Step 4 — Call create_workflow
Build the two JSON objects yourself in this turn and call the privileged tool:

\`create_workflow(folder_name: "<kebab-name>", workflow_json: {..., label: "<human-readable>", ...}, plan_json: {...})\`

**IMPORTANT**: Use the \`create_workflow\` tool — do NOT try to \`mkdir\` or write files with shell commands. The \`Workflow/\` folder is read-only to normal shell writes; \`create_workflow\` is the only path that can create a new workflow folder. The tool validates folder_name (shell-safe kebab-case), enforces required JSON fields, refuses to overwrite existing workflows, and writes both files in one call.

The workflow.json schema (required: schema_version, id, label) and the plan.json schema (required: steps array with type/id/title) are already documented in your system prompt — follow that shape exactly.

## Step 5 — Report back to me
After the tool returns, tell me:
- The folder path returned by the tool
- The display label
- A one-line summary of what the workflow does
- The step IDs + titles (numbered list)
- Tell me I can pick it from the workflow picker to activate it.`

      const message = ctx.beforeSlash
        ? `${ctx.beforeSlash}\n\n${instruction}`
        : instruction

      ctx.onSubmit(message)
    }
  }
]
