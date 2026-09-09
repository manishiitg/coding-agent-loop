export type PulseCommandDefinition = {
  id: string
  label: string
  description: string
}

export const PULSE_MODULE_COMMANDS: PulseCommandDefinition[] = [
  { id: 'technical_review', label: 'Health', description: 'Investigates correctness failures, invalid outputs and regressions' },
  { id: 'architecture_review', label: 'Architecture', description: 'Improves prompts, orchestration, scripts, learning, knowledge, data, reports and efficiency' },
  { id: 'strategic_review', label: 'Strategy', description: 'Audits hidden strategic mechanisms and conditionally explores materially different approaches' },
  { id: 'plan_drift_review', label: 'Plan drift review', description: 'Checks steps flagged by a plan edit for DB, report, learnings, KB, and validation_schema drift' },
]

export const PULSE_FIXED_COMMANDS: PulseCommandDefinition[] = [
  { id: 'dashboard', label: 'Dashboard + questions', description: 'Updates the Pulse narrative and records decisions that need your input' },
  { id: 'backup', label: 'Backup', description: 'Saves current workflow artifacts when changed' },
  { id: 'publish', label: 'Publish', description: 'Refreshes a verified public report when stale' },
  { id: 'notify', label: 'Notify', description: 'Sends the final run summary' },
]
