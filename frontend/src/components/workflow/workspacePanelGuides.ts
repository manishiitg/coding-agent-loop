export type WorkspacePanelGuide = {
  title: string
  purpose: string
  howTo: string
}

const GUIDES: Record<string, Omit<WorkspacePanelGuide, 'title'>> = {
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
    purpose: 'Watch the browser an agent is using for web work.',
    howTo: 'Inspect the live page, take control when needed, and use Browser settings to choose how the agent connects.',
  },
  'Browser automation': {
    purpose: 'Configure and inspect browser access for this automation.',
    howTo: 'Choose the browser mode and connection settings, then watch the live browser when a run uses it.',
  },
  'Cost Analysis': {
    purpose: 'See the model and tool usage behind this work and what it costs.',
    howTo: 'Compare totals and individual runs to find expensive work. Refresh to load the latest usage.',
  },
  Dashboard: {
    purpose: 'See the results and visual report produced for this project or automation.',
    howTo: 'Review the latest output here. Ask AI to explain or change the dashboard, and refresh after new work finishes.',
  },
  Database: {
    purpose: 'Inspect structured records stored by this project or automation.',
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
    purpose: 'Browse the files that belong to this project or automation.',
    howTo: 'Open folders and files to inspect outputs and source material. Ask AI to find or explain a file.',
  },
  Identity: {
    purpose: 'Define this agent’s name, purpose, model, and allowed resources.',
    howTo: 'Use the tabs to review its role and settings. Changes here shape how it behaves in future chats and runs.',
  },
  Integrations: {
    purpose: 'Choose the apps, skills, and other connections this agent may use.',
    howTo: 'Open a tab to inspect each kind of connection, then enable only what this project needs.',
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
    purpose: 'See the steps and routes that make up this automation.',
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
    purpose: 'Browse the files that belong to this Crew project.',
    howTo: 'Open a folder or file to inspect project material. Ask Crew to explain what a file contains or help change it.',
  },
}

export function getWorkspacePanelGuide(topic: string): WorkspacePanelGuide {
  const key = topic.startsWith('Schedules for ') ? 'Schedules' : topic
  const copy = GUIDES[key]
  return copy
    ? { title: topic, ...copy }
    : {
        title: topic || 'This panel',
        purpose: `Review ${topic || 'this view'} in the workspace pane.`,
        howTo: 'Use the controls in this panel to inspect its content. Ask AI for help with what you see, and refresh for the latest state.',
      }
}
