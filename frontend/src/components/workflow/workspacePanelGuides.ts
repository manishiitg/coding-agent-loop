export type WorkspacePanelSurface = 'agentworks' | 'crew'
export type WorkspacePanelGroup = 'Main toolbar' | 'Ops' | 'Setup'

export type WorkspacePanelGuide = {
  title: string
  surface: WorkspacePanelSurface
  group: WorkspacePanelGroup
  purpose: string
  howTo: string
}

type GuideCopy = Pick<WorkspacePanelGuide, 'purpose' | 'howTo'>

const AGENTWORKS_GUIDES: Record<string, GuideCopy> = {
  Access: {
    purpose: 'Control who can open, run, edit, and share this automation.',
    howTo: 'Review each access tab, then invite people or adjust their roles. A read-only role can inspect and run work without changing its setup.',
  },
  Automation: {
    purpose: 'Manage the ways work starts and review earlier conversations.',
    howTo: 'Use the tabs for chats, schedules, webhooks, functions, and bots. A schedule starts work at a time; a webhook or function starts it from another system.',
  },
  'Automation Learnings': {
    purpose: 'See observations the automation has kept from earlier work.',
    howTo: 'Review what it learned and ask AI to correct anything stale. These notes can inform future runs.',
  },
  'Automation Schedules': {
    purpose: 'See timed jobs across your automations and whether they are running or paused.',
    howTo: 'Open a schedule to inspect its next run and history. Use the controls here to pause, resume, or refresh scheduling status.',
  },
  'Attached folders': {
    purpose: 'Give this automation access to files outside its own workspace.',
    howTo: 'Review the attached folders and their access level. Attach a folder when the automation needs to read or update its contents.',
  },
  Backup: {
    purpose: 'Keep recoverable copies of this automation and its data.',
    howTo: 'Check backup status and destinations here. You can export a ZIP or ask AI to help configure a remote backup.',
  },
  Browser: {
    purpose: 'Watch the browser this AgentWorks automation uses for web work.',
    howTo: 'Inspect the live page, take control when needed, and use Browser settings to choose how the agent connects.',
  },
  'Browser automation': {
    purpose: 'Configure and inspect browser access for this automation.',
    howTo: 'Choose the browser mode and connection settings, then watch the live browser when a run uses it.',
  },
  'Cost Analysis': {
    purpose: 'See the model and tool usage behind this automation and what it costs.',
    howTo: 'Compare totals and individual runs to find expensive work. Refresh to load the latest usage.',
  },
  Dashboard: {
    purpose: 'See the visual results this AgentWorks automation produced.',
    howTo: 'Review the latest output here. Ask AI to explain or change the dashboard, and refresh after new work finishes.',
  },
  Database: {
    purpose: 'Inspect structured records stored by this automation.',
    howTo: 'Choose a table to browse its rows. Ask AI when you need help understanding or changing stored data.',
  },
  'Execution Logs': {
    purpose: 'See the detailed events and output from an automation run.',
    howTo: 'Select a run and follow its log to diagnose failures or confirm what happened. Refresh for recent events.',
  },
  File: {
    purpose: 'Read the selected workspace file and inspect its contents.',
    howTo: 'Use Back to files to return to the tree. If editing is available, save changes before leaving this view.',
  },
  Files: {
    purpose: 'Browse the files and outputs that belong to this automation.',
    howTo: 'Open folders and files to inspect outputs and source material. Ask AI to find or explain a file.',
  },
  Identity: {
    purpose: 'Define this AgentWorks workflow’s name, purpose, model, and allowed resources.',
    howTo: 'Use the tabs to review its general settings, secrets, file access, models, and upgrades. Changes here shape future chats and runs.',
  },
  Integrations: {
    purpose: 'Choose the apps, skills, and bots this AgentWorks workflow may use.',
    howTo: 'Open a tab to inspect each kind of connection, then enable only what this workflow needs.',
  },
  Knowledge: {
    purpose: 'Bring together what this automation has learned, its notes, and its stored records.',
    howTo: 'Switch tabs to inspect each source of knowledge. Ask AI to explain or update information that is missing or stale.',
  },
  Knowledgebase: {
    purpose: 'Browse narrative notes and topics saved for this automation.',
    howTo: 'Pick a source or topic to read its notes. Refresh after the automation adds new knowledge.',
  },
  Memory: {
    purpose: 'Keep durable project context that a Crew member carries across chats and automatic work.',
    howTo: 'Read what the Crew remembers and which skills it can use. Owners can ask Crew to update memory; shared viewers can inspect it.',
  },
  Notify: {
    purpose: 'Control where status updates and important run notifications go.',
    howTo: 'Check the connected channels and delivery state. Ask AI to change which events send notifications.',
  },
  Plan: {
    purpose: 'AgentWorks turns a goal into a reusable automation. This plan shows its steps and routes.',
    howTo: 'Inspect the flow to understand what runs next. Ask AI to build or revise the plan, and refresh to see recent changes.',
  },
  'Project agent configuration': {
    purpose: 'Choose the coding agent and model this Crew member uses.',
    howTo: 'Review the available options, then change the selection when a project needs different capabilities or cost.',
  },
  Publish: {
    purpose: 'Share this automation’s dashboard and Pulse log at a public URL.',
    howTo: 'Check the publishing destination and status. Ask AI to help set up a host or update what gets published.',
  },
  Pulse: {
    purpose: 'See the automation’s current health, progress, and items that need attention.',
    howTo: 'Review recorded statuses and open an issue when something needs a decision. Refresh to check the latest state.',
  },
  Schedules: {
    purpose: 'Run work automatically at chosen times.',
    howTo: 'Review each schedule’s next run, status, and history. Pause or edit a schedule when its timing should change.',
  },
  Webhooks: {
    purpose: 'Start work when another service sends an event.',
    howTo: 'Create a webhook, share its endpoint with the sending service, and inspect delivery history when an event does not start work.',
  },
  'Workflow playbooks': {
    purpose: 'Reuse proven setups when building or improving this automation.',
    howTo: 'Browse the available playbooks and ask the Builder to apply one that fits your goal.',
  },
  Workspace: {
    purpose: 'Browse the files that belong to this automation.',
    howTo: 'Open a folder or file to inspect project material. Ask AI to explain what a file contains or help change it.',
  },
}

const CREW_GUIDES: Record<string, GuideCopy> = {
  Automation: {
    purpose: 'Let this Crew member continue work from chats, schedules, triggers, functions, and connected bots.',
    howTo: 'Use the tabs to review past chats and each way Crew can be started. Schedules run a saved instruction at a time; triggers start one from an external event.',
  },
  Browser: {
    purpose: 'Watch the browser this Crew member uses when it works on the web.',
    howTo: 'Follow the live page, take control when needed, and open Browser settings to choose how Crew connects.',
  },
  'Cost Analysis': {
    purpose: 'See how much this Crew project costs to run.',
    howTo: 'Review model and tool usage across this project. Compare totals with individual work to understand where the cost comes from.',
  },
  Dashboard: {
    purpose: 'Show the visual results page this Crew member can create for its project.',
    howTo: 'Review the latest page or ask Crew to build one. Ask AI to explain or change it, then refresh after new work finishes.',
  },
  Database: {
    purpose: 'Keep structured records this Crew project can use across conversations.',
    howTo: 'Choose a table to inspect its rows. Ask Crew to explain or update records when you need help.',
  },
  File: {
    purpose: 'Read a file in this Crew project.',
    howTo: 'Use Back to files to return to the tree. The owner can edit and save files; shared viewers can inspect them.',
  },
  Files: {
    purpose: 'Browse the files and outputs that belong to this Crew project.',
    howTo: 'Open folders and files to inspect the project. Ask Crew to find, explain, or change a file; shared viewers can inspect without editing.',
  },
  Identity: {
    purpose: 'Define who this Crew member is and what job it should do.',
    howTo: 'Review its name, purpose, model, secrets, and file access in the tabs. These settings shape future Crew conversations and automatic work.',
  },
  Integrations: {
    purpose: 'Choose the connected apps, skills, and bots this Crew member may use.',
    howTo: 'Open each tab to inspect available connections, then select the ones this project needs.',
  },
  Memory: {
    purpose: 'Crew is a project-based AI teammate in AgentWorks. Memory keeps context it carries across chats and automatic work.',
    howTo: 'Read what Crew remembers and which project skills it can use. Owners can ask Crew to update memory; shared viewers can inspect it.',
  },
  'Project agent configuration': {
    purpose: 'Choose the coding agent and model this Crew member uses.',
    howTo: 'Review the available options, then change the selection when this project needs different capabilities or cost.',
  },
  Schedules: {
    purpose: 'Send a saved instruction to this Crew member automatically at chosen times.',
    howTo: 'Review the next run, status, and history for each schedule. Pause or edit one when the timing or instruction should change.',
  },
  Webhooks: {
    purpose: 'Send a saved instruction to this Crew member when another service sends an event.',
    howTo: 'Create a trigger, share its authenticated endpoint with the sending service, and inspect deliveries when an event does not start work.',
  },
  Workspace: {
    purpose: 'Browse the files that belong to this Crew project.',
    howTo: 'Open a folder or file to inspect project material. Ask Crew to explain what a file contains or help change it.',
  },
}

const GROUPS: Record<WorkspacePanelSurface, Record<WorkspacePanelGroup, readonly string[]>> = {
  agentworks: {
    'Main toolbar': ['Automation', 'Automation Schedules', 'Browser', 'Dashboard', 'Plan', 'Pulse', 'Schedules', 'Webhooks'],
    Ops: ['Automation Learnings', 'Backup', 'Cost Analysis', 'Database', 'Execution Logs', 'File', 'Files', 'Knowledge', 'Knowledgebase', 'Notify', 'Publish', 'Workspace'],
    Setup: ['Access', 'Attached folders', 'Browser automation', 'Identity', 'Integrations', 'Project agent configuration', 'Workflow playbooks'],
  },
  crew: {
    'Main toolbar': ['Automation', 'Browser', 'Dashboard', 'Memory', 'Schedules', 'Webhooks'],
    Ops: ['Cost Analysis', 'Database', 'File', 'Files', 'Workspace'],
    Setup: ['Identity', 'Integrations', 'Project agent configuration'],
  },
}

function groupFor(surface: WorkspacePanelSurface, topic: string): WorkspacePanelGroup {
  for (const group of ['Main toolbar', 'Ops', 'Setup'] as const) {
    if (GROUPS[surface][group].includes(topic)) return group
  }
  return 'Main toolbar'
}

export function getWorkspacePanelGuide(topic: string, surface: WorkspacePanelSurface = 'agentworks'): WorkspacePanelGuide {
  const key = topic.startsWith('Schedules for ') ? 'Schedules' : topic
  const copy = (surface === 'crew' ? CREW_GUIDES : AGENTWORKS_GUIDES)[key]
  const group = groupFor(surface, key)
  return copy
    ? { title: topic, surface, group, ...copy }
    : {
        title: topic || 'This panel',
        surface,
        group,
        purpose: `Review ${topic || 'this view'} in the workspace pane.`,
        howTo: `Use the controls here to inspect its content. Ask ${surface === 'crew' ? 'Crew' : 'AI'} for help with what you see, and refresh for the latest state.`,
      }
}
