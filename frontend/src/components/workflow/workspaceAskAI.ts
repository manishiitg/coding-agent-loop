import { getWorkspaceView, type WorkspaceViewId } from './workspaceViews'

// Every right-side workspace view has one useful first message for Builder.
// Keeping this exhaustive means a newly registered view cannot silently ship
// without the shared Ask AI action.
export const WORKSPACE_ASK_AI_MESSAGE = {
  report: "Help me understand this workflow's dashboard and reporting. Use the current workflow evidence, explain the important results, and ask what I want to investigate or improve.",
  flow: "Review this workflow's current plan with me. Explain how it works, identify gaps or risks, and ask what I want to change before editing it.",
  costs: "Help me understand or reduce this workflow's cost. Use the available cost evidence, explain what's driving it, and ask what I want to change.",
  'execution-logs': "Help me investigate this workflow's execution logs. Identify failures or unusual behavior, explain the evidence, and ask which run or issue I want to examine.",
  learnings: "Help me understand or change what this workflow has learned. Explain the relevant learnings and ask which behavior or step I want to examine.",
  knowledgebase: "Help me use this workflow's knowledgebase. Explain what is available and ask what I want to find, add, or improve.",
  database: "Help me understand or change this workflow's database. Use the current schema and data, then ask what I want to inspect or update.",
  evaluation: "Help me understand this workflow's evaluation results. Explain failures, quality gaps, and trends, then ask what I want to improve.",
  schedules: "Help me set up or change a schedule for this workflow. Ask what should run, when it should run, and any route requirements.",
  webhooks: "Help me set up or change a webhook for this workflow. Ask which external event should trigger it, what should run, and which authentication and routing it needs.",
  files: "Help me work with this workflow's files. Ask what I want to find, understand, create, or change before modifying anything.",
  pulse: "Help me understand this workflow's Pulse reviews, findings, and pending decisions. Explain what matters and ask what I want to review or act on.",
  backup: "Help me set up or change backup for this workflow. Explain the current state and ask what should be backed up and where.",
  publish: "Help me publish this workflow. Explain its current publishing state and ask which target and release process I want.",
  notify: "Help me change this workflow's notifications. Explain the current setup and ask which events, channels, and recipients I want.",
  access: "Help me understand or change access to this workflow. Explain the current permissions and ask who should have which level of access.",
  playbooks: "Help me choose and use an AgentWorks playbook for this workflow. Ask what outcome I need, compare relevant playbooks, and explain the setup before changing anything.",
  skills: "Help me find, create, or configure a skill for this workflow. Ask what capability or guidance it should provide.",
  secrets: "Help me configure a secret for this workflow. Ask which credential is needed and where it should come from without asking me to reveal it in chat.",
  mcp: "Help me add or configure an MCP server for this workflow. Ask which app or service I want to connect, then help me find and set up the right integration.",
  browser: "Help me configure browser access for this workflow. Ask which sites, accounts, and browser tasks it needs to handle.",
  llm: "Help me choose or configure the LLMs for this workflow. Explain the current setup and ask about quality, speed, and cost requirements.",
  email: "Help me connect or configure Gmail, default recipients, and email access settings.",
  bots: "Load builder-reference/references/slack-bot-routing.md and help me connect Slack or WhatsApp to this workflow. Inspect current settings, explain the connection and route setup, and ask which channel I need; use run mode without asking for a grant. Never request tokens in chat.",
  folders: "Help me attach a folder to this workflow. Ask which folder it needs, why, and whether access should be read-only or writable.",
} satisfies Record<WorkspaceViewId, string>

/**
 * Ask AI is a product entry point, not just a panel-specific canned prompt.
 * Every request loads the same compact guide so Builder explains unfamiliar
 * workflow concepts consistently while keeping the current view as context.
 */
export function getWorkspaceAskAIMessage(view: WorkspaceViewId): string {
  const label = getWorkspaceView(view).label
  return `Read the workflow help guide with read_skill(skills=[{"name":"builder-reference","path":"references/workflow-guide.md"}]), then use the ${label} view as context. ${WORKSPACE_ASK_AI_MESSAGE[view]}`
}
