export type PulseSectionId = 'goal' | 'signals' | 'reflection' | 'improvements'

export type PulseCommandDefinition = {
  id: string
  label: string
  description: string
}

export type PulseSectionDefinition = {
  id: PulseSectionId
  label: string
  concept: string
  moduleIds: string[]
  commandIds: string[]
  historyIds: string[]
}

export const PULSE_MODULE_COMMANDS: PulseCommandDefinition[] = [
  { id: 'bug_review', label: 'Bug review', description: 'Read-only reliability checks; Pulse Fixer applies safe fixes' },
  { id: 'artifact_review', label: 'Artifact review', description: 'Plan-change artifact drift' },
  { id: 'learning_health', label: 'Learning health', description: 'Learning freshness and quality' },
  { id: 'knowledgebase_health', label: 'Knowledge base', description: 'KB freshness and contradictions' },
  { id: 'db_health', label: 'Database health', description: 'DB/schema/data quality checks' },
  { id: 'eval_health', label: 'Eval health', description: 'Rubric and eval wiring quality' },
  { id: 'report_health', label: 'Report health', description: 'Dashboard/report accuracy' },
  { id: 'cost_llm_time', label: 'Cost + time', description: 'Cost, model usage, and runtime telemetry' },
  { id: 'llm_ops_review', label: 'LLM + operations', description: 'Model routing and workflow setup recommendations' },
  { id: 'goal_advisor', label: 'Goal Advisor', description: 'Strategic review when goal evidence is weak' },
]

export const PULSE_FIXED_COMMANDS: PulseCommandDefinition[] = [
  { id: 'dashboard', label: 'Dashboard + questions', description: 'Updates the Pulse narrative and records decisions that need your input' },
  { id: 'backup', label: 'Backup', description: 'Saves current workflow artifacts when changed' },
  { id: 'publish', label: 'Publish', description: 'Refreshes a verified public report when stale' },
  { id: 'notify', label: 'Notify', description: 'Sends the final run summary' },
]

export const PULSE_HISTORY_ITEMS: PulseCommandDefinition[] = [
  { id: 'pulse_fixer', label: 'Pulse fixes', description: 'Verified fixes and their outcomes across Pulse runs' },
]

export const PULSE_FOOTER_COMMAND_IDS = ['backup', 'publish', 'notify'] as const

export const PULSE_SECTIONS: PulseSectionDefinition[] = [
  {
    id: 'goal',
    label: 'Goal',
    concept: 'Ikigai',
    moduleIds: [],
    commandIds: [],
    historyIds: [],
  },
  {
    id: 'signals',
    label: 'Signals',
    concept: 'Kizuki',
    moduleIds: [
      'bug_review',
      'artifact_review',
      'learning_health',
      'knowledgebase_health',
      'db_health',
      'eval_health',
      'report_health',
      'cost_llm_time',
      'llm_ops_review',
    ],
    commandIds: [],
    historyIds: [],
  },
  {
    id: 'reflection',
    label: 'Reflection',
    concept: 'Hansei',
    moduleIds: [],
    commandIds: ['dashboard'],
    historyIds: [],
  },
  {
    id: 'improvements',
    label: 'Improvements',
    concept: 'Kaizen',
    moduleIds: ['goal_advisor'],
    commandIds: [],
    historyIds: ['pulse_fixer'],
  },
]
