// Pure cost / token-usage aggregation. Used by CostsWidget and the
// shared models by EvalsWidget. No React; consumers can call these
// from useMemo or compute them eagerly.
//
// `summariseRunCosts` and `summarisePhaseCosts` are the two entry
// points; everything else is helper plumbing.

import { formatAuto, formatNamed } from '../../../lib/reportFormatters'
import type {
  ModelTokenUsage,
  PhaseTokenUsageFile,
  ReportCostsMetric,
  TokenUsageFile,
} from '../../../services/api-types'

export type CostStageBucket =
  | 'execution'
  | 'learning'
  | 'evaluation'
  | 'knowledgebase'
  | 'routing'
  | 'workshop'
  | 'other'

export type CostSummary = {
  totalCost: number
  totalInputTokens: number
  totalOutputTokens: number
  totalTokens: number
  totalLLMCalls: number
  stageCosts: Record<CostStageBucket, number>
  stepCosts: Array<{
    stepID: string
    stepTitle: string
    totalCost: number
    inputTokens: number
    outputTokens: number
    totalTokens: number
    llmCalls: number
    stage: CostStageBucket
  }>
  modelCosts: Array<{
    modelID: string
    provider: string
    totalCost: number
    inputTokens: number
    outputTokens: number
    totalTokens: number
    llmCalls: number
  }>
}

export type RunCostSummary = CostSummary & {
  runFolder: string
  updatedAt: string | null
}

export type PhaseCostSummary = {
  totalCost: number
  totalInputTokens: number
  totalOutputTokens: number
  totalTokens: number
  totalLLMCalls: number
  phaseCosts: Array<{
    phaseID: string
    phaseTitle: string
    totalCost: number
    inputTokens: number
    outputTokens: number
    totalTokens: number
    llmCalls: number
  }>
  modelCosts: Array<{
    modelID: string
    provider: string
    totalCost: number
    inputTokens: number
    outputTokens: number
    totalTokens: number
    llmCalls: number
  }>
}

export function emptyStageCosts(): Record<CostStageBucket, number> {
  return {
    execution: 0,
    learning: 0,
    evaluation: 0,
    knowledgebase: 0,
    routing: 0,
    workshop: 0,
    other: 0,
  }
}

export function classifyCostPhase(phase: string): CostStageBucket {
  if (phase === 'execution_only') return 'execution'
  if (phase === 'success_learning' || phase === 'failure_learning' || phase.includes('learning')) return 'learning'
  if (phase === 'evaluation_scoring' || phase.startsWith('evaluation')) return 'evaluation'
  if (phase.startsWith('kb_')) return 'knowledgebase'
  if (phase === 'conditional_evaluation' || phase === 'todo_task' || phase === 'routing' || phase.includes('routing')) return 'routing'
  if (phase === 'harden_workflow' || phase === 'review_step_code' || phase === 'replan_workflow_from_results' || phase === 'optimize_step') return 'workshop'
  return 'other'
}

export function formatPhaseTitle(phaseID: string) {
  const knownTitles: Record<string, string> = {
    'workflow-builder': 'Workflow Builder',
    'report-execution': 'Report Execution',
    planning: 'Planning',
    'plan-improvement': 'Plan Improvement',
    'evaluation-builder': 'Evaluation Builder',
    'output-builder': 'Output Builder',
  }
  if (knownTitles[phaseID]) return knownTitles[phaseID]
  return phaseID
    .split(/[-_]/)
    .filter(Boolean)
    .map(part => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ')
}

export function metricValue(metric: ReportCostsMetric | undefined, item: {
  totalCost?: number
  totalTokens?: number
  inputTokens?: number
  outputTokens?: number
  llmCalls?: number
}): number {
  switch (metric ?? 'cost') {
    case 'input_tokens': return item.inputTokens ?? 0
    case 'output_tokens': return item.outputTokens ?? 0
    case 'llm_calls': return item.llmCalls ?? 0
    case 'total_tokens': return item.totalTokens ?? 0
    case 'cost':
    default: return item.totalCost ?? 0
  }
}

export function metricLabel(metric: ReportCostsMetric | undefined): string {
  switch (metric ?? 'cost') {
    case 'input_tokens': return 'Input Tokens'
    case 'output_tokens': return 'Output Tokens'
    case 'llm_calls': return 'LLM Calls'
    case 'total_tokens': return 'Total Tokens'
    case 'cost':
    default: return 'Cost'
  }
}

export function formatMetricValue(metric: ReportCostsMetric | undefined, value: number): string {
  if ((metric ?? 'cost') === 'cost') return formatNamed(value, 'currency-usd').text
  return formatAuto(value).text
}

export function parseTimestamp(value?: string | null): number | null {
  if (!value) return null
  const time = new Date(value).getTime()
  return Number.isFinite(time) ? time : null
}

export function formatRuntimeDuration(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) return '0m'
  const totalMinutes = Math.round(ms / 60000)
  if (totalMinutes < 60) return `${totalMinutes}m`
  const hours = Math.floor(totalMinutes / 60)
  const minutes = totalMinutes % 60
  if (hours < 24) return minutes > 0 ? `${hours}h ${minutes}m` : `${hours}h`
  const days = Math.floor(hours / 24)
  const remHours = hours % 24
  return remHours > 0 ? `${days}d ${remHours}h` : `${days}d`
}

export function timestampForTokenUsage(tokenUsage?: TokenUsageFile | null, evaluationTokenUsage?: TokenUsageFile | null): string | null {
  return tokenUsage?.updated_at || evaluationTokenUsage?.updated_at || tokenUsage?.created_at || evaluationTokenUsage?.created_at || null
}

export function summariseModelUsage(map: Record<string, ModelTokenUsage>, target: Map<string, {
  modelID: string
  provider: string
  totalCost: number
  inputTokens: number
  outputTokens: number
  totalTokens: number
  llmCalls: number
}>): void {
  for (const [modelID, usage] of Object.entries(map)) {
    const existing = target.get(modelID) ?? {
      modelID,
      provider: usage.provider || 'unknown',
      totalCost: 0,
      inputTokens: 0,
      outputTokens: 0,
      totalTokens: 0,
      llmCalls: 0,
    }
    existing.totalCost += usage.total_cost_usd || 0
    existing.inputTokens += usage.input_tokens || 0
    existing.outputTokens += usage.output_tokens || 0
    existing.totalTokens += (usage.input_tokens || 0) + (usage.output_tokens || 0)
    existing.llmCalls += usage.llm_call_count || 0
    target.set(modelID, existing)
  }
}

export function summariseRunCosts(
  runFolder: string,
  tokenUsage: TokenUsageFile | null | undefined,
  evaluationTokenUsage: TokenUsageFile | null | undefined,
  scope: 'execution' | 'evaluation' | 'all',
): RunCostSummary | null {
  const totals = {
    totalCost: 0,
    totalInputTokens: 0,
    totalOutputTokens: 0,
    totalTokens: 0,
    totalLLMCalls: 0,
  }
  const stageCosts = emptyStageCosts()
  const stepMap = new Map<string, {
    stepID: string
    stepTitle: string
    totalCost: number
    inputTokens: number
    outputTokens: number
    totalTokens: number
    llmCalls: number
    stage: CostStageBucket
  }>()
  const modelMap = new Map<string, {
    modelID: string
    provider: string
    totalCost: number
    inputTokens: number
    outputTokens: number
    totalTokens: number
    llmCalls: number
  }>()

  const processUsage = (
    usageFile: TokenUsageFile | null | undefined,
    mode: 'execution' | 'evaluation',
  ) => {
    if (!usageFile?.by_model) return
    summariseModelUsage(usageFile.by_model, modelMap)
    for (const usage of Object.values(usageFile.by_model)) {
      totals.totalCost += usage.total_cost_usd || 0
      totals.totalInputTokens += usage.input_tokens || 0
      totals.totalOutputTokens += usage.output_tokens || 0
      totals.totalTokens += (usage.input_tokens || 0) + (usage.output_tokens || 0)
      totals.totalLLMCalls += usage.llm_call_count || 0
    }

    for (const [key, modelUsage] of Object.entries(usageFile.by_step_and_model || {})) {
      const [phase, rawStepID] = key.split(':')
      const stage = mode === 'evaluation' ? 'evaluation' : classifyCostPhase(phase || '')
      const stepID = rawStepID || phase || 'unknown'
      const displayStepID = mode === 'evaluation' ? `eval:${stepID}` : stepID
      const stepTitle = mode === 'evaluation' ? `[Eval] ${stepID}` : stepID
      const entry = stepMap.get(displayStepID) ?? {
        stepID: displayStepID,
        stepTitle,
        totalCost: 0,
        inputTokens: 0,
        outputTokens: 0,
        totalTokens: 0,
        llmCalls: 0,
        stage,
      }
      for (const usage of Object.values(modelUsage)) {
        const inputTokens = usage.input_tokens || 0
        const outputTokens = usage.output_tokens || 0
        entry.totalCost += usage.total_cost_usd || 0
        entry.inputTokens += inputTokens
        entry.outputTokens += outputTokens
        entry.totalTokens += inputTokens + outputTokens
        entry.llmCalls += usage.llm_call_count || 0
        stageCosts[stage] += usage.total_cost_usd || 0
      }
      stepMap.set(displayStepID, entry)
    }
  }

  if (scope === 'execution' || scope === 'all') processUsage(tokenUsage, 'execution')
  if (scope === 'evaluation' || scope === 'all') processUsage(evaluationTokenUsage, 'evaluation')

  if (totals.totalCost === 0 && totals.totalTokens === 0 && modelMap.size === 0 && stepMap.size === 0) return null

  return {
    runFolder,
    updatedAt: timestampForTokenUsage(tokenUsage, evaluationTokenUsage),
    ...totals,
    stageCosts,
    stepCosts: Array.from(stepMap.values()).sort((a, b) => b.totalCost - a.totalCost || a.stepTitle.localeCompare(b.stepTitle)),
    modelCosts: Array.from(modelMap.values()).sort((a, b) => b.totalCost - a.totalCost || a.modelID.localeCompare(b.modelID)),
  }
}

export function summarisePhaseCosts(tokenUsage: PhaseTokenUsageFile | null | undefined): PhaseCostSummary | null {
  if (!tokenUsage?.by_model) return null
  const totals = {
    totalCost: 0,
    totalInputTokens: 0,
    totalOutputTokens: 0,
    totalTokens: 0,
    totalLLMCalls: 0,
  }
  const modelMap = new Map<string, {
    modelID: string
    provider: string
    totalCost: number
    inputTokens: number
    outputTokens: number
    totalTokens: number
    llmCalls: number
  }>()
  summariseModelUsage(tokenUsage.by_model, modelMap)
  for (const usage of Object.values(tokenUsage.by_model)) {
    totals.totalCost += usage.total_cost_usd || 0
    totals.totalInputTokens += usage.input_tokens || 0
    totals.totalOutputTokens += usage.output_tokens || 0
    totals.totalTokens += (usage.input_tokens || 0) + (usage.output_tokens || 0)
    totals.totalLLMCalls += usage.llm_call_count || 0
  }

  const phaseCosts = Object.entries(tokenUsage.by_phase_and_model || {})
    .map(([phaseID, modelUsage]) => {
      let totalCost = 0
      let inputTokens = 0
      let outputTokens = 0
      let llmCalls = 0
      for (const usage of Object.values(modelUsage)) {
        totalCost += usage.total_cost_usd || 0
        inputTokens += usage.input_tokens || 0
        outputTokens += usage.output_tokens || 0
        llmCalls += usage.llm_call_count || 0
      }
      return {
        phaseID,
        phaseTitle: formatPhaseTitle(phaseID),
        totalCost,
        inputTokens,
        outputTokens,
        totalTokens: inputTokens + outputTokens,
        llmCalls,
      }
    })
    .sort((a, b) => b.totalCost - a.totalCost || a.phaseTitle.localeCompare(b.phaseTitle))

  return {
    ...totals,
    phaseCosts,
    modelCosts: Array.from(modelMap.values()).sort((a, b) => b.totalCost - a.totalCost || a.modelID.localeCompare(b.modelID)),
  }
}

export function aggregateRunCostSummaries(runs: RunCostSummary[]): CostSummary | null {
  if (runs.length === 0) return null
  const stageCosts = emptyStageCosts()
  const stepMap = new Map<string, CostSummary['stepCosts'][number]>()
  const modelMap = new Map<string, CostSummary['modelCosts'][number]>()
  const totals = {
    totalCost: 0,
    totalInputTokens: 0,
    totalOutputTokens: 0,
    totalTokens: 0,
    totalLLMCalls: 0,
  }

  for (const run of runs) {
    totals.totalCost += run.totalCost
    totals.totalInputTokens += run.totalInputTokens
    totals.totalOutputTokens += run.totalOutputTokens
    totals.totalTokens += run.totalTokens
    totals.totalLLMCalls += run.totalLLMCalls
    for (const stage of Object.keys(stageCosts) as CostStageBucket[]) stageCosts[stage] += run.stageCosts[stage]
    for (const step of run.stepCosts) {
      const current = stepMap.get(step.stepID) ?? { ...step }
      if (current !== step) {
        current.totalCost += step.totalCost
        current.inputTokens += step.inputTokens
        current.outputTokens += step.outputTokens
        current.totalTokens += step.totalTokens
        current.llmCalls += step.llmCalls
      }
      stepMap.set(step.stepID, current)
    }
    for (const model of run.modelCosts) {
      const current = modelMap.get(model.modelID) ?? { ...model }
      if (current !== model) {
        current.totalCost += model.totalCost
        current.inputTokens += model.inputTokens
        current.outputTokens += model.outputTokens
        current.totalTokens += model.totalTokens
        current.llmCalls += model.llmCalls
      }
      modelMap.set(model.modelID, current)
    }
  }

  return {
    ...totals,
    stageCosts,
    stepCosts: Array.from(stepMap.values()).sort((a, b) => b.totalCost - a.totalCost || a.stepTitle.localeCompare(b.stepTitle)),
    modelCosts: Array.from(modelMap.values()).sort((a, b) => b.totalCost - a.totalCost || a.modelID.localeCompare(b.modelID)),
  }
}
