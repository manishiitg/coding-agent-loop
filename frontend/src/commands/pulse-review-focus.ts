export interface PulseReviewFocus {
  id: string
  label: string
  description: string
  legacyCommand: string
  argumentNames: string[]
  instructions: string
}

const technicalFocus = (key: string) => `Manual Pulse review focus: ${key}. Prioritize this focus and preserve the normal lightweight safety scan.`
const storeFocus = (lens: string) => `Manual Pulse review focus: store_integrity. Prioritize the ${lens === 'knowledge' ? 'knowledgebase' : lens} lens and load the canonical improve-${lens} checklist inside Technical Review.`

// One source for the picker, typed arguments, and retained command shortcuts.
// Keep the review rubrics here so a simpler menu does not weaken an investigation.
export const pulseReviewFocuses: PulseReviewFocus[] = [
  { id: 'execution', label: 'Execution health', description: 'Meaningful failures, retries, and reliability problems.',
    legacyCommand: 'pulse-review-execution-health', argumentNames: ['execution', 'execution-health'], instructions: technicalFocus('execution_health') },
  { id: 'prompts', label: 'Prompt quality', description: 'Step instructions, clarity, duplication, and validation alignment.',
    legacyCommand: 'plan-prompt-bloat', argumentNames: ['prompts', 'prompt', 'prompt-quality'],
    instructions: 'Manual Pulse review focus: plan_orchestration_integrity. Run the complete prompt-contract review: call read_skill(skills=[{"name":"builder-reference","path":"references/step-description.md"}]), call get_plan_prompt_health, and assess the authored step descriptions and validation schemas against that guide. Report semantic prompt-quality failures separately from mechanical size or exact-duplication signals; a short prompt can still be poor and a long prompt can be justified.' },
  { id: 'validation', label: 'Validation contracts', description: 'Checks that protect real outcomes without unnecessary gates.',
    legacyCommand: 'pulse-review-validation-contract', argumentNames: ['validation', 'validation-contract'], instructions: technicalFocus('validation_contract_health') },
  { id: 'reports', label: 'Report quality', description: 'Accuracy and trustworthiness of the reports users receive.',
    legacyCommand: 'pulse-review-report-quality', argumentNames: ['reports', 'report', 'report-quality'], instructions: technicalFocus('report_quality_truth') },
  { id: 'evaluation', label: 'Evaluation quality', description: 'Whether evaluations measure the intended outcomes correctly.',
    legacyCommand: 'pulse-review-evaluation-quality', argumentNames: ['evaluation', 'evaluations', 'evaluation-quality'], instructions: technicalFocus('evaluation_quality_truth') },
  { id: 'costs', label: 'Model costs', description: 'Model choices and avoidable cost for the required quality.',
    legacyCommand: 'pulse-review-model-cost', argumentNames: ['costs', 'cost', 'model-cost'], instructions: technicalFocus('model_cost_fitness') },
  { id: 'database', label: 'Database integrity', description: 'Durable data, schemas, and the contracts used by consumers.',
    legacyCommand: 'pulse-review-database', argumentNames: ['database', 'db'], instructions: storeFocus('database') },
  { id: 'knowledge', label: 'Knowledgebase', description: 'Ownership, organization, and consolidation of useful knowledge.',
    legacyCommand: 'pulse-review-knowledge', argumentNames: ['knowledge', 'knowledgebase', 'kb'], instructions: storeFocus('knowledge') },
  { id: 'learnings', label: 'Learnings', description: 'Reusable learning quality, ownership, and duplication.',
    legacyCommand: 'pulse-review-learnings', argumentNames: ['learnings', 'learning'], instructions: storeFocus('learnings') },
]

export function resolvePulseReviewFocus(selectedFocus: string | undefined, context: string): PulseReviewFocus | undefined {
  // An explicit automatic selection keeps the free-text request open-ended.
  if (selectedFocus !== undefined) return pulseReviewFocuses.find(focus => focus.id === selectedFocus)
  const firstWord = context.trim().split(/\s+/)[0].toLowerCase()
  return pulseReviewFocuses.find(focus => focus.argumentNames.includes(firstWord))
}
