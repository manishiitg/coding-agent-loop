// Dynamic report viewer — parses reports/report_plan.json, fetches each widget's
// JSON source, and renders widgets.
// See docs/workflow/persistent_stores_design.md.

import { useEffect, useId, useMemo, useRef, useState } from 'react'
import {
  Bar, BarChart,
  CartesianGrid,
  Cell,
  Legend,
  Line, LineChart,
  Area, AreaChart,
  Pie, PieChart,
  ResponsiveContainer,
  Tooltip,
  XAxis, YAxis,
} from 'recharts'
import { ArrowDown, ArrowUp, ArrowUpDown, BarChart3, ChevronDown, ChevronLeft, ChevronRight, ChevronUp, Download, Monitor, RefreshCw, Search, Smartphone } from 'lucide-react'
import { agentApi } from '../../services/api'
import {
  applyWidgetFilter,
  evaluateShowIf,
  parseReportPlan,
  resolveJSONPath,
} from '../../lib/reportPlanParser'
import { compareValues, formatAuto, formatNamed, rowsToCSV, type FormatResult } from '../../lib/reportFormatters'
import {
  evalScoreTone as evalScoreToneFromTokens,
  scoreTones,
  scoreTier,
  severityIcons,
  severityTones,
  trendArrow,
  trendDirection,
  trendTones,
  type ScoreTone,
} from '../../lib/reportTokens'
import { useTheme } from '../../hooks/useTheme'
import { MarkdownRenderer } from '../ui/MarkdownRenderer'
import ModalPortal from '../ui/ModalPortal'
import type {
  EvaluationReportsResponse,
  ModelTokenUsage,
  ParsedReportPlan,
  PhaseTokenUsageFile,
  PlannerFile,
  ReportAlertSeverity,
  ReportCostsMetric,
  ReportEvalsMetric,
  ReportEntry,
  ReportFormatterName,
  ReportWidget,
  ReportWidgetKind,
  RunFoldersResponse,
  TokenUsageFile,
  WorkflowCostsResponse,
} from '../../services/api-types'

// Default rows-per-page for tables; overridable per-widget via `page_size:`.
const DEFAULT_TABLE_PAGE_SIZE = 25
const DEFAULT_COST_WIDGET_RUN_LIMIT = 3
const DEFAULT_EVAL_WIDGET_RUN_LIMIT = 3
export const REPORT_PREVIEW_PREFERENCE_KEY = 'workflow_report_preview_preference'
export const REPORT_PREVIEW_PREFERENCE_CHANGED_EVENT = 'workflow-report-preview-preference-changed'
// Default categorical palette. Widgets override via `colors:` / `colors_dark:`.
// Keep the fallback theme-driven so report charts follow the app palette.
const CHART_COLORS = [
  'hsl(var(--chart-1))',
  'hsl(var(--chart-2))',
  'hsl(var(--chart-3))',
  'hsl(var(--chart-4))',
  'hsl(var(--chart-5))',
  'hsl(var(--primary))',
  'hsl(var(--warning))',
  'hsl(var(--success))',
]

type CostStageBucket = 'execution' | 'learning' | 'evaluation' | 'knowledgebase' | 'routing' | 'workshop' | 'other'

type CostSummary = {
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

type RunCostSummary = CostSummary & {
  runFolder: string
  updatedAt: string | null
}

type PhaseCostSummary = {
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

function emptyStageCosts(): Record<CostStageBucket, number> {
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

function classifyCostPhase(phase: string): CostStageBucket {
  if (phase === 'execution_only') return 'execution'
  if (phase === 'success_learning' || phase === 'failure_learning' || phase.includes('learning')) return 'learning'
  if (phase === 'evaluation_scoring' || phase.startsWith('evaluation')) return 'evaluation'
  if (phase.startsWith('kb_')) return 'knowledgebase'
  if (phase === 'conditional_evaluation' || phase === 'todo_task' || phase === 'routing' || phase.includes('routing')) return 'routing'
  if (phase === 'harden_workflow' || phase === 'review_step_code' || phase === 'replan_workflow_from_results' || phase === 'optimize_step') return 'workshop'
  return 'other'
}

function formatPhaseTitle(phaseID: string) {
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

function metricValue(metric: ReportCostsMetric | undefined, item: {
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

function metricLabel(metric: ReportCostsMetric | undefined): string {
  switch (metric ?? 'cost') {
    case 'input_tokens': return 'Input Tokens'
    case 'output_tokens': return 'Output Tokens'
    case 'llm_calls': return 'LLM Calls'
    case 'total_tokens': return 'Total Tokens'
    case 'cost':
    default: return 'Cost'
  }
}

function formatMetricValue(metric: ReportCostsMetric | undefined, value: number): string {
  if ((metric ?? 'cost') === 'cost') return formatNamed(value, 'currency-usd').text
  return formatAuto(value).text
}

function evalScoreTone(scorePercentage: number): ScoreTone {
  return evalScoreToneFromTokens(scorePercentage)
}

function evalScoreBarClass(scorePercentage: number): string {
  return scoreTones[scoreTier(scorePercentage)].barClassName
}

function parseTimestamp(value?: string | null): number | null {
  if (!value) return null
  const time = new Date(value).getTime()
  return Number.isFinite(time) ? time : null
}

function formatRuntimeDuration(ms: number): string {
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

function timestampForTokenUsage(tokenUsage?: TokenUsageFile | null, evaluationTokenUsage?: TokenUsageFile | null): string | null {
  return tokenUsage?.updated_at || evaluationTokenUsage?.updated_at || tokenUsage?.created_at || evaluationTokenUsage?.created_at || null
}

function summariseModelUsage(map: Record<string, ModelTokenUsage>, target: Map<string, {
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

function summariseRunCosts(
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

function summarisePhaseCosts(tokenUsage: PhaseTokenUsageFile | null | undefined): PhaseCostSummary | null {
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

function aggregateRunCostSummaries(runs: RunCostSummary[]): CostSummary | null {
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

function useCompactWidgetLayout(maxWidth = 520) {
  const ref = useRef<HTMLDivElement | null>(null)
  const [isCompact, setIsCompact] = useState(false)

  useEffect(() => {
    const node = ref.current
    if (!node) return

    const update = (width: number) => {
      setIsCompact(width <= maxWidth)
    }

    const measure = () => update(node.getBoundingClientRect().width)
    measure()

    if (typeof ResizeObserver !== 'undefined') {
      const observer = new ResizeObserver(entries => {
        const entry = entries[0]
        if (!entry) return
        update(entry.contentRect.width)
      })
      observer.observe(node)
      return () => observer.disconnect()
    }

    window.addEventListener('resize', measure)
    return () => window.removeEventListener('resize', measure)
  }, [maxWidth])

  return [ref, isCompact] as const
}

const COMPACT_PRIMARY_COLUMN_CANDIDATES = [
  'title',
  'name',
  'label',
  'headline',
  'job_title',
  'role',
  'position',
]

const COMPACT_SECONDARY_COLUMN_CANDIDATES = [
  'subtitle',
  'company',
  'company_name',
  'budget_display',
  'status',
  'location',
  'type',
  'created_at',
  'updated_at',
]

const COMPACT_DEPRIORITIZED_COLUMNS = new Set([
  'id',
  'url',
  'job_url',
  'link',
  'description',
  'job_text',
  'text',
  'content',
  'body',
  'summary',
])

function isPrimitiveTableValue(value: unknown): value is string | number | boolean {
  return typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean'
}

function isURLString(value: string): boolean {
  return /^https?:\/\//i.test(value)
}

function stringifyTableValue(value: unknown): string {
  if (value == null) return '—'
  if (Array.isArray(value)) {
    if (value.length === 0) return '—'
    if (value.every(isPrimitiveTableValue)) return value.map(item => String(item)).join(', ')
    try {
      return JSON.stringify(value)
    } catch {
      return String(value)
    }
  }
  if (typeof value === 'object') {
    const entries = Object.entries(value as Record<string, unknown>)
    if (entries.length === 0) return '—'
    if (entries.every(([, item]) => item == null || isPrimitiveTableValue(item))) {
      return entries
        .map(([key, item]) => `${key}: ${item == null ? '—' : String(item)}`)
        .join(', ')
    }
    try {
      return JSON.stringify(value)
    } catch {
      return String(value)
    }
  }
  return String(value)
}

function formatTableValue(value: unknown, preset?: ReportFormatterName): FormatResult & {
  href?: string
  rawText: string
  prefersBlock: boolean
} {
  if (preset) {
    const formatted = formatNamed(value, preset)
    return {
      ...formatted,
      rawText: formatted.text,
      prefersBlock: formatted.text.length > 80 || formatted.text.includes('\n'),
    }
  }

  const rawText = stringifyTableValue(value)
  if (typeof value === 'string' && isURLString(value)) {
    return {
      text: value,
      href: value,
      isNumeric: false,
      rawText,
      prefersBlock: true,
    }
  }

  if (Array.isArray(value) || (value != null && typeof value === 'object')) {
    return {
      text: rawText,
      isNumeric: false,
      rawText,
      prefersBlock: rawText.length > 60 || Array.isArray(value),
    }
  }

  const formatted = formatAuto(value)
  return {
    ...formatted,
    rawText,
    prefersBlock: rawText.length > 80 || rawText.includes('\n'),
  }
}

function renderTableValueContent(formatted: {
  text: string
  href?: string
}) {
  if (formatted.href) {
    return (
      <a
        href={formatted.href}
        target="_blank"
        rel="noreferrer"
        className="text-primary underline underline-offset-2 break-all hover:text-primary/80"
      >
        {formatted.text}
      </a>
    )
  }
  return formatted.text
}

function collectVisibleColumns(rows: Array<Record<string, unknown>>, hidden: Set<string>): string[] {
  const cols: string[] = []
  const seen = new Set<string>()
  for (const row of rows) {
    if (!row || typeof row !== 'object') continue
    for (const key of Object.keys(row)) {
      if (!seen.has(key) && !hidden.has(key)) {
        seen.add(key)
        cols.push(key)
      }
    }
  }
  return cols
}

function detectNumericColumns(rows: Array<Record<string, unknown>>, columns: string[]): Set<string> {
  const out = new Set<string>()
  for (const col of columns) {
    let sawNumeric = false
    let sawNonNumeric = false
    for (const row of rows) {
      const v = row?.[col]
      if (v == null || v === '') continue
      if (typeof v === 'number' && Number.isFinite(v)) {
        sawNumeric = true
      } else if (typeof v === 'string' && v.trim() !== '' && Number.isFinite(Number(v))) {
        sawNumeric = true
      } else {
        sawNonNumeric = true
        break
      }
    }
    if (sawNumeric && !sawNonNumeric) out.add(col)
  }
  return out
}

function inferPrimaryColumn(columns: string[], numericColumns: Set<string>, preferred?: string): string | null {
  if (preferred && columns.includes(preferred)) return preferred
  const nonNumericColumns = columns.filter(col => !numericColumns.has(col))
  const candidate = COMPACT_PRIMARY_COLUMN_CANDIDATES.find(name => nonNumericColumns.includes(name))
  if (candidate) return candidate
  return nonNumericColumns.find(col => !COMPACT_DEPRIORITIZED_COLUMNS.has(col)) ?? nonNumericColumns[0] ?? columns[0] ?? null
}

function inferSecondaryColumn(
  columns: string[],
  numericColumns: Set<string>,
  primaryColumn: string | null,
  preferred?: string,
): string | null {
  const remainingColumns = columns.filter(col => col !== primaryColumn && !numericColumns.has(col))
  if (preferred && remainingColumns.includes(preferred)) return preferred
  const candidate = COMPACT_SECONDARY_COLUMN_CANDIDATES.find(name => remainingColumns.includes(name))
  if (candidate) return candidate
  return remainingColumns.find(col => !COMPACT_DEPRIORITIZED_COLUMNS.has(col)) ?? remainingColumns[0] ?? null
}

// Resolves the effective color palette for a widget given the current theme.
// Precedence: colorsDark (when dark) > colors > CHART_COLORS.
function resolvePalette(widget: ReportWidget, theme: 'light' | 'dark'): string[] {
  if (theme === 'dark' && widget.colorsDark && widget.colorsDark.length > 0) return widget.colorsDark
  if (widget.colors && widget.colors.length > 0) return widget.colors
  return CHART_COLORS
}

// Resolves a per-row color from widget semantic coloring. Returns undefined when
// no match exists (caller falls back to cycled palette / default).
function resolveSemanticColor(
  widget: ReportWidget,
  row: Record<string, unknown> | null | undefined,
  palette: string[],
  index: number,
): string | undefined {
  if (!widget.colorBy || !row) return undefined
  const rawValue = row[widget.colorBy]
  if (rawValue === undefined || rawValue === null) return undefined
  const key = String(rawValue)
  if (widget.colorMap && widget.colorMap[key]) return widget.colorMap[key]
  // No map entry — cycle palette deterministically by the distinct-value index.
  if (palette.length > 0) return palette[index % palette.length]
  return undefined
}

// Shifts a hex/named color toward a subtle tint — used for table row backgrounds
// so semantic coloring stays legible against the app theme. Hex shortcuts only;
// named colors pass through at low opacity via rgba-ish CSS.
function toRowTint(color: string): string {
  // #rgb / #rrggbb → 14% alpha; named colors → rely on color-mix-ish fallback.
  if (color.startsWith('#')) {
    if (color.length === 4) {
      const r = color[1], g = color[2], b = color[3]
      return `#${r}${r}${g}${g}${b}${b}24` // ~14% alpha
    }
    if (color.length === 7) return `${color}24`
    if (color.length === 9) return color // already has alpha
  }
  return color
}

async function readWorkspaceText(filepath: string): Promise<string | null> {
  try {
    const resp = await agentApi.getPlannerFileContent(filepath)
    if (resp && resp.success && resp.data && typeof resp.data.content === 'string') {
      return resp.data.content
    }
    return null
  } catch {
    // 404 / network — missing source files are expected when a widget points at a db/
    // file that hasn't been written yet. Callers distinguish missing from fetched by
    // the `null` vs `undefined` cache entry below.
    return null
  }
}

type DailyGroupTokenUsageFile = {
  date?: string
  group_folder?: string
  updated_at?: string
  run_folders?: Record<string, TokenUsageFile>
}

type DailyPhaseTokenUsageFile = {
  date: string
  updated_at?: string
  token_usage?: PhaseTokenUsageFile
}

function collectPlannerFilePaths(items: PlannerFile[], into: Set<string>): void {
  for (const item of items) {
    if (item.type === 'file' && item.filepath) {
      into.add(item.filepath)
    }
    if (Array.isArray(item.children) && item.children.length > 0) {
      collectPlannerFilePaths(item.children, into)
    }
  }
}

async function listWorkspaceFilePaths(folderPath: string, maxDepth = 10): Promise<string[]> {
  try {
    const resp = await agentApi.getPlannerFiles(folderPath, -1, maxDepth)
    const items = Array.isArray(resp?.data) ? resp.data : []
    const filePaths = new Set<string>()
    collectPlannerFilePaths(items, filePaths)
    for (const item of items) {
      if (item.type === 'file' && item.filepath) filePaths.add(item.filepath)
    }
    return Array.from(filePaths).sort()
  } catch {
    return []
  }
}

async function readWorkspaceJSON<T>(filepath: string): Promise<T | null> {
  const content = await readWorkspaceText(filepath)
  if (!content) return null
  try {
    return JSON.parse(content) as T
  } catch {
    return null
  }
}

function mergeModelUsage(a?: ModelTokenUsage, b?: ModelTokenUsage): ModelTokenUsage | undefined {
  if (!a) return b ? { ...b } : undefined
  if (!b) return { ...a }
  return {
    provider: b.provider || a.provider,
    input_tokens: (a.input_tokens || 0) + (b.input_tokens || 0),
    output_tokens: (a.output_tokens || 0) + (b.output_tokens || 0),
    input_tokens_m: b.input_tokens_m || a.input_tokens_m || '0.000M',
    output_tokens_m: b.output_tokens_m || a.output_tokens_m || '0.000M',
    cache_tokens: (a.cache_tokens || 0) + (b.cache_tokens || 0),
    cache_tokens_m: b.cache_tokens_m || a.cache_tokens_m || '0.000M',
    cache_read_tokens: (a.cache_read_tokens || 0) + (b.cache_read_tokens || 0),
    cache_read_tokens_m: b.cache_read_tokens_m || a.cache_read_tokens_m || '0.000M',
    cache_write_tokens: (a.cache_write_tokens || 0) + (b.cache_write_tokens || 0),
    cache_write_tokens_m: b.cache_write_tokens_m || a.cache_write_tokens_m || '0.000M',
    reasoning_tokens: (a.reasoning_tokens || 0) + (b.reasoning_tokens || 0),
    reasoning_tokens_m: b.reasoning_tokens_m || a.reasoning_tokens_m || '0.000M',
    llm_call_count: (a.llm_call_count || 0) + (b.llm_call_count || 0),
    input_cost_usd: (a.input_cost_usd || 0) + (b.input_cost_usd || 0),
    output_cost_usd: (a.output_cost_usd || 0) + (b.output_cost_usd || 0),
    reasoning_cost_usd: (a.reasoning_cost_usd || 0) + (b.reasoning_cost_usd || 0),
    cache_cost_usd: (a.cache_cost_usd || 0) + (b.cache_cost_usd || 0),
    cache_read_cost_usd: (a.cache_read_cost_usd || 0) + (b.cache_read_cost_usd || 0),
    cache_write_cost_usd: (a.cache_write_cost_usd || 0) + (b.cache_write_cost_usd || 0),
    total_cost_usd: (a.total_cost_usd || 0) + (b.total_cost_usd || 0),
    context_window_usage: Math.max(a.context_window_usage || 0, b.context_window_usage || 0),
    model_context_window: Math.max(a.model_context_window || 0, b.model_context_window || 0),
    context_usage_percent: Math.max(a.context_usage_percent || 0, b.context_usage_percent || 0),
  }
}

function pickEarlierTimestamp(a?: string, b?: string): string {
  if (!a) return b || ''
  if (!b) return a
  return new Date(a).getTime() <= new Date(b).getTime() ? a : b
}

function pickLaterTimestamp(a?: string, b?: string): string {
  if (!a) return b || ''
  if (!b) return a
  return new Date(a).getTime() >= new Date(b).getTime() ? a : b
}

function mergeTokenUsageFiles(a?: TokenUsageFile, b?: TokenUsageFile): TokenUsageFile | undefined {
  if (!a) return b ? { ...b } : undefined
  if (!b) return { ...a }
  const byModel: Record<string, ModelTokenUsage> = { ...(a.by_model || {}) }
  for (const [modelID, usage] of Object.entries(b.by_model || {})) {
    const merged = mergeModelUsage(byModel[modelID], usage)
    if (merged) byModel[modelID] = merged
  }

  const byStepAndModel: Record<string, Record<string, ModelTokenUsage>> = { ...(a.by_step_and_model || {}) }
  for (const [stepID, modelUsage] of Object.entries(b.by_step_and_model || {})) {
    const mergedStep = { ...(byStepAndModel[stepID] || {}) }
    for (const [modelID, usage] of Object.entries(modelUsage || {})) {
      const merged = mergeModelUsage(mergedStep[modelID], usage)
      if (merged) mergedStep[modelID] = merged
    }
    byStepAndModel[stepID] = mergedStep
  }

  return {
    created_at: pickEarlierTimestamp(a.created_at, b.created_at),
    updated_at: pickLaterTimestamp(a.updated_at, b.updated_at),
    by_model: byModel,
    by_step_and_model: Object.keys(byStepAndModel).length > 0 ? byStepAndModel : undefined,
  }
}

async function loadCostsDataFallback(workspacePath: string): Promise<WorkflowCostsResponse | null> {
  const [phaseTokenUsage, phaseDailyPaths, executionPaths, evaluationPaths] = await Promise.all([
    readWorkspaceJSON<PhaseTokenUsageFile>(`${workspacePath}/costs/phase/token_usage.json`),
    listWorkspaceFilePaths(`${workspacePath}/costs/phase/daily`, 4),
    listWorkspaceFilePaths(`${workspacePath}/costs/execution`, 6),
    listWorkspaceFilePaths(`${workspacePath}/costs/evaluation`, 6),
  ])

  const executionMap = new Map<string, TokenUsageFile>()
  const evaluationMap = new Map<string, TokenUsageFile>()

  const loadScopedRuns = async (
    filePaths: string[],
    target: Map<string, TokenUsageFile>,
  ) => {
    for (const filePath of filePaths.filter(path => path.endsWith('.json'))) {
      const daily = await readWorkspaceJSON<DailyGroupTokenUsageFile>(filePath)
      if (!daily?.run_folders) continue
      for (const [runFolder, tokenUsage] of Object.entries(daily.run_folders)) {
        const merged = mergeTokenUsageFiles(target.get(runFolder), tokenUsage)
        if (merged) target.set(runFolder, merged)
      }
    }
  }

  await Promise.all([
    loadScopedRuns(executionPaths, executionMap),
    loadScopedRuns(evaluationPaths, evaluationMap),
  ])

  const phaseDailyCosts: WorkflowCostsResponse['phase_daily_costs'] = []
  for (const filePath of phaseDailyPaths.filter(path => path.endsWith('.json'))) {
    const daily = await readWorkspaceJSON<DailyPhaseTokenUsageFile>(filePath)
    if (!daily?.date) continue
    phaseDailyCosts.push({
      date: daily.date,
      token_usage: daily.token_usage,
    })
  }

  const runFolderSet = new Set<string>([
    ...Array.from(executionMap.keys()),
    ...Array.from(evaluationMap.keys()),
  ])

  const runs = Array.from(runFolderSet)
    .map(runFolder => ({
      run_folder: runFolder,
      token_usage: executionMap.get(runFolder),
      evaluation_token_usage: evaluationMap.get(runFolder),
    }))
    .sort((a, b) => {
      const aTime = parseTimestamp(a.token_usage?.updated_at || a.evaluation_token_usage?.updated_at || a.token_usage?.created_at || a.evaluation_token_usage?.created_at || null) || 0
      const bTime = parseTimestamp(b.token_usage?.updated_at || b.evaluation_token_usage?.updated_at || b.token_usage?.created_at || b.evaluation_token_usage?.created_at || null) || 0
      return bTime - aTime || a.run_folder.localeCompare(b.run_folder)
    })

  if (!phaseTokenUsage && phaseDailyCosts.length === 0 && runs.length === 0) return null

  return {
    success: true,
    phase_token_usage: phaseTokenUsage || undefined,
    phase_daily_costs: phaseDailyCosts,
    runs,
  }
}

interface ReportViewerProps {
  workspacePath: string
  isOpen: boolean
  onClose: () => void
}

interface ReportViewProps {
  workspacePath: string
  /** Optional close/back handler; when omitted, no close button is rendered (used for canvas-mode). */
  onClose?: () => void
  mobilePreview?: boolean
}

// Source content cached per workspace-relative path. `undefined` = not yet fetched;
// `null` = fetched and missing/malformed; otherwise the parsed JSON value.
type SourceCache = Record<string, unknown>

function widgetInstanceKey(
  widget: ReportWidget,
  ids: { sectionIndex: number; entryIndex: number; widgetIndex: number },
) {
  return [
    ids.sectionIndex,
    ids.entryIndex,
    ids.widgetIndex,
    widget.kind,
    widget.source,
    widget.path ?? '',
    widget.title ?? '',
  ].join('::')
}

function widgetShouldRender(widget: ReportWidget, raw: unknown) {
  if (widget.hidden) return false
  if (widget.kind === 'costs' || widget.kind === 'evals' || widget.kind === 'runs') return true
  if (raw === undefined || raw === null) return true
  if (!evaluateShowIf(raw, widget.showIf)) return false
  if (widget.kind === 'stat' || widget.kind === 'alert' || widget.kind === 'pivot') return true

  const resolvedRaw = resolveJSONPath(raw, widget.path)
  if (resolvedRaw === undefined) return true

  const resolved = applyWidgetFilter(resolvedRaw, widget.filter)
  if (resolved == null) return false
  if (Array.isArray(resolved) && resolved.length === 0) return true
  return true
}

// Modal wrapper — overlay + panel + close-on-backdrop. Used by scheduler runs panel.
export function ReportViewer({ workspacePath, isOpen, onClose }: ReportViewerProps) {
  if (!isOpen) return null
  return (
    <ModalPortal>
    <div
      className="fixed inset-0 z-[9999] flex items-center justify-center bg-black/60 px-2 py-3 backdrop-blur-sm sm:px-4 sm:py-6"
      onClick={onClose}
    >
      <div
        className="flex max-h-[94vh] w-full max-w-6xl flex-col overflow-hidden rounded-xl border border-border/70 bg-background shadow-[0_24px_80px_rgba(0,0,0,0.45)] sm:max-h-[90vh] sm:rounded-2xl"
        onClick={e => e.stopPropagation()}
      >
        <ReportView workspacePath={workspacePath} onClose={onClose} />
      </div>
    </div>
    </ModalPortal>
  )
}

// Inline content — renders the report plan directly without modal chrome. Used by the
// workflow canvas when canvasViewMode === 'report'.
export function ReportView({ workspacePath, onClose, mobilePreview = false }: ReportViewProps) {
  const [previewPreference, setPreviewPreference] = useState<'auto' | 'desktop' | 'mobile'>(() => {
    try {
      const saved = localStorage.getItem(REPORT_PREVIEW_PREFERENCE_KEY)
      return saved === 'desktop' || saved === 'mobile' ? saved : 'auto'
    } catch {
      return 'auto'
    }
  })
  const [loading, setLoading] = useState(false)
  const [planSource, setPlanSource] = useState<string | null>(null)
  const [sources, setSources] = useState<SourceCache>({})
  const [costsData, setCostsData] = useState<WorkflowCostsResponse | null>(null)
  const [costsLoading, setCostsLoading] = useState(false)
  const [costsError, setCostsError] = useState<string | null>(null)
  const [evalsData, setEvalsData] = useState<EvaluationReportsResponse | null>(null)
  const [evalsLoading, setEvalsLoading] = useState(false)
  const [evalsError, setEvalsError] = useState<string | null>(null)
  const [runsData, setRunsData] = useState<RunFoldersResponse | null>(null)
  const [runsLoading, setRunsLoading] = useState(false)
  const [runsError, setRunsError] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [refreshNonce, setRefreshNonce] = useState(0)
  const [hiddenWidgetKeys, setHiddenWidgetKeys] = useState<Set<string>>(() => new Set())

  const plan: ParsedReportPlan = useMemo(() => {
    if (!planSource) return { sections: [] }
    return parseReportPlan(planSource)
  }, [planSource])

  // Stable key: same set of paths → same string → effect below doesn't re-run.
  // Using the array identity directly would recompute every render because useMemo
  // returns a fresh Array.from each time the plan parses.
  const referencedSourcesKey = useMemo(() => {
    const set = new Set<string>()
    for (const section of plan.sections) {
      for (const entry of section.entries) {
        if (entry.kind === 'single') {
          if (entry.widget.kind !== 'costs' && entry.widget.kind !== 'evals' && entry.widget.kind !== 'runs' && entry.widget.source) set.add(entry.widget.source)
        } else {
          for (const w of entry.row.widgets) {
            if (w.kind !== 'costs' && w.kind !== 'evals' && w.kind !== 'runs' && w.source) set.add(w.source)
          }
        }
      }
    }
    return Array.from(set).sort().join('|')
  }, [plan])

  const hasCostsWidget = useMemo(() => {
    for (const section of plan.sections) {
      for (const entry of section.entries) {
        if (entry.kind === 'single') {
          if (entry.widget.kind === 'costs') return true
        } else {
          if (entry.row.widgets.some(widget => widget.kind === 'costs')) return true
        }
      }
    }
    return false
  }, [plan])

  const hasEvalsWidget = useMemo(() => {
    for (const section of plan.sections) {
      for (const entry of section.entries) {
        if (entry.kind === 'single') {
          if (entry.widget.kind === 'evals') return true
        } else {
          if (entry.row.widgets.some(widget => widget.kind === 'evals')) return true
        }
      }
    }
    return false
  }, [plan])

  const hasRunsWidget = useMemo(() => {
    for (const section of plan.sections) {
      for (const entry of section.entries) {
        if (entry.kind === 'single') {
          if (entry.widget.kind === 'runs') return true
        } else {
          if (entry.row.widgets.some(widget => widget.kind === 'runs')) return true
        }
      }
    }
    return false
  }, [plan])

  useEffect(() => {
    if (!workspacePath) return
    let cancelled = false
    setLoading(true)
    setError(null)

    readWorkspaceText(`${workspacePath}/reports/report_plan.json`)
      .then(jsonContent => {
        if (cancelled) return
        setPlanSource(jsonContent)
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })

    return () => {
      cancelled = true
    }
  }, [workspacePath, refreshNonce])

  // Fetch sources referenced by the plan. `sources` is intentionally NOT a dep — we
  // read it fresh via setSources' functional form and return `prev` unchanged when
  // every referenced path is already cached, which is the no-op guard that prevents
  // the effect from looping on its own setState.
  useEffect(() => {
    if (!workspacePath || referencedSourcesKey === '') return
    const paths = referencedSourcesKey.split('|')
    let cancelled = false

    Promise.all(
      paths.map(async (path): Promise<readonly [string, unknown]> => {
        const content = await readWorkspaceText(`${workspacePath}/${path}`)
        if (content === null || content.trim() === '') return [path, null] as const
        try {
          return [path, JSON.parse(content)] as const
        } catch {
          return [path, null] as const
        }
      })
    ).then(results => {
      if (cancelled) return
      setSources(prev => {
        let changed = false
        const next = { ...prev }
        for (const [path, data] of results) {
          if (!(path in prev)) {
            next[path] = data
            changed = true
          }
        }
        return changed ? next : prev
      })
    })

    return () => {
      cancelled = true
    }
  }, [workspacePath, referencedSourcesKey, refreshNonce])

  useEffect(() => {
    if (!workspacePath || (!hasCostsWidget && !hasEvalsWidget)) {
      if (!hasCostsWidget) {
        setCostsData(null)
        setCostsError(null)
        setCostsLoading(false)
      }
      if (!hasEvalsWidget) {
        setEvalsData(null)
        setEvalsError(null)
        setEvalsLoading(false)
      }
      return
    }

    let cancelled = false
    if (hasCostsWidget) {
      setCostsLoading(true)
      setCostsError(null)
    }
    if (hasEvalsWidget) {
      setEvalsLoading(true)
      setEvalsError(null)
    }

    agentApi.getWorkflowReviewData(workspacePath)
      .then(async response => {
        if (cancelled) return

        if (hasCostsWidget) {
          const costsResponse = response.costs
          if (costsResponse?.success) {
            if ((costsResponse.runs?.length ?? 0) > 0) {
              setCostsData(costsResponse)
            } else {
              const fallback = await loadCostsDataFallback(workspacePath)
              if (cancelled) return
              setCostsData(fallback ?? costsResponse)
            }
          } else {
            const fallback = await loadCostsDataFallback(workspacePath)
            if (cancelled) return
            if (fallback) {
              setCostsData(fallback)
              setCostsError(null)
            } else {
              setCostsData(null)
              setCostsError('Failed to load workflow costs.')
            }
          }
        }

        if (hasEvalsWidget) {
          const evalsResponse = response.evaluations
          if (evalsResponse?.success) {
            setEvalsData(evalsResponse)
          } else {
            setEvalsData(null)
            setEvalsError(evalsResponse?.error || 'Failed to load evaluation reports.')
          }
        }
      })
      .catch(async () => {
        if (cancelled) return
        if (hasCostsWidget) {
          const fallback = await loadCostsDataFallback(workspacePath)
          if (cancelled) return
          if (fallback) {
            setCostsData(fallback)
            setCostsError(null)
          } else {
            setCostsData(null)
            setCostsError('Failed to load workflow costs.')
          }
        }
        if (hasEvalsWidget) {
          setEvalsData(null)
          setEvalsError('Failed to load evaluation reports.')
        }
      })
      .finally(() => {
        if (cancelled) return
        if (hasCostsWidget) setCostsLoading(false)
        if (hasEvalsWidget) setEvalsLoading(false)
      })

    return () => {
      cancelled = true
    }
  }, [workspacePath, hasCostsWidget, hasEvalsWidget, refreshNonce])

  useEffect(() => {
    if (!workspacePath || !hasRunsWidget) {
      setRunsData(null)
      setRunsError(null)
      setRunsLoading(false)
      return
    }
    let cancelled = false
    setRunsLoading(true)
    setRunsError(null)
    agentApi.getRunFolders(workspacePath)
      .then(response => {
        if (cancelled) return
        setRunsData(response)
      })
      .catch(() => {
        if (cancelled) return
        setRunsData(null)
        setRunsError('Failed to load workflow runs.')
      })
      .finally(() => {
        if (!cancelled) setRunsLoading(false)
      })

    return () => {
      cancelled = true
    }
  }, [workspacePath, hasRunsWidget, refreshNonce])

  useEffect(() => {
    setHiddenWidgetKeys(new Set())
  }, [workspacePath, planSource, refreshNonce])

  const handleRefresh = () => {
    setError(null)
    setSources({})
    setCostsData(null)
    setCostsError(null)
    setEvalsData(null)
    setEvalsError(null)
    setRunsData(null)
    setRunsError(null)
    setRefreshNonce(prev => prev + 1)
  }

  const handleToggleWidgetHidden = (widgetKey: string) => {
    setHiddenWidgetKeys(prev => {
      const next = new Set(prev)
      if (next.has(widgetKey)) next.delete(widgetKey)
      else next.add(widgetKey)
      return next
    })
  }

  const planExists = planSource !== null
  const visibleSections = useMemo(() => {
    return plan.sections.flatMap((section, sectionIndex) => {
      const entries = section.entries.flatMap((entry, entryIndex) => {
        const widgets = entry.kind === 'single' ? [entry.widget] : entry.row.widgets
        const hasVisibleWidget = widgets.some((widget, widgetIndex) => {
          if (hiddenWidgetKeys.has(widgetInstanceKey(widget, { sectionIndex, entryIndex, widgetIndex }))) return true
          return widgetShouldRender(widget, sources[widget.source])
        })
        return hasVisibleWidget ? [{ entry, entryIndex }] : []
      })

      if (entries.length === 0) return []
      return [{ section, sectionIndex, entries }]
    })
  }, [plan, hiddenWidgetKeys, sources])
  const hasAnyContent = useMemo(() => {
    if (!planExists) return false
    for (let sectionIndex = 0; sectionIndex < plan.sections.length; sectionIndex += 1) {
      const section = plan.sections[sectionIndex]
      for (let entryIndex = 0; entryIndex < section.entries.length; entryIndex += 1) {
        const entry = section.entries[entryIndex]
        const widgets = entry.kind === 'single' ? [entry.widget] : entry.row.widgets
        for (let widgetIndex = 0; widgetIndex < widgets.length; widgetIndex += 1) {
          const w = widgets[widgetIndex]
          if (hiddenWidgetKeys.has(widgetInstanceKey(w, { sectionIndex, entryIndex, widgetIndex }))) return true
          if (w.kind === 'costs' || w.kind === 'evals' || w.kind === 'runs') return true
          if (widgetShouldRender(w, sources[w.source])) return true
        }
      }
    }
    return false
  }, [planExists, plan, sources, hiddenWidgetKeys])
  const canUseSplitPreview = mobilePreview
  const isMobilePreview = canUseSplitPreview && (
    previewPreference === 'mobile'
      ? true
      : previewPreference === 'desktop'
        ? false
        : mobilePreview
  )
  const isRefreshing = loading || costsLoading || evalsLoading || runsLoading
  const previewShellClassName = isMobilePreview
    ? 'mx-auto w-full max-w-[480px] p-1.5 transition-all duration-200'
    : 'mx-auto w-full max-w-full transition-all duration-200'
  const previewContentClassName = isMobilePreview
    ? 'w-full max-w-full'
    : 'mx-auto w-full max-w-5xl'

  const handleTogglePreviewMode = () => {
    setPreviewPreference(prev => {
      const currentIsMobile =
        prev === 'mobile'
          ? true
          : prev === 'desktop'
            ? false
            : mobilePreview
      const next = currentIsMobile ? 'desktop' : 'mobile'
      try {
        localStorage.setItem(REPORT_PREVIEW_PREFERENCE_KEY, next)
        window.dispatchEvent(new CustomEvent(REPORT_PREVIEW_PREFERENCE_CHANGED_EVENT, { detail: { preference: next } }))
      } catch {
        // ignore
      }
      return next
    })
  }

  return (
    <div className="relative h-full w-full flex flex-col overflow-hidden bg-gradient-to-b from-background via-background to-muted/20 text-foreground">
      {onClose && (
        <div className="flex flex-shrink-0 items-center justify-end border-b border-border/50 bg-background/80 px-3 py-2.5 backdrop-blur-sm sm:px-5">
          <button
            onClick={onClose}
            className="inline-flex h-9 w-9 items-center justify-center rounded-lg border border-border/70 bg-background/80 text-xl leading-none text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
            title="Close"
            aria-label="Close report"
          >
            ×
          </button>
        </div>
      )}

      <div className="min-h-0 flex-1 overflow-y-auto overscroll-y-contain px-2.5 py-3 [scrollbar-gutter:stable] sm:px-4 sm:py-4">
        <div className={previewShellClassName}>
          <div className={`flex flex-col gap-3 ${previewContentClassName}`}>
            {loading && <ReportSkeleton />}
            {error && <div className="text-destructive">Failed to load report: {error}</div>}

            {!loading && !error && !hasAnyContent && (
              <div className="flex flex-col items-center justify-center gap-3 rounded-2xl border border-dashed border-border/70 bg-card/70 px-4 py-8 text-center shadow-sm sm:px-6 sm:py-10">
                <div className="flex h-12 w-12 items-center justify-center rounded-2xl border border-primary/20 bg-primary/10 text-primary sm:h-14 sm:w-14">
                  <BarChart3 className="h-6 w-6" />
                </div>
                <div className="space-y-1">
                  <div className="text-base font-semibold text-foreground">No report yet</div>
                  <div className="text-xs uppercase tracking-[0.22em] text-muted-foreground">Waiting For Plan Or Data</div>
                </div>
                <div className="max-w-md text-center text-sm text-muted-foreground leading-6">
                  The builder writes <code className="px-1 rounded bg-muted">reports/report_plan.json</code> to configure
                  this view; widgets render once <code className="px-1 rounded bg-muted">db/</code> has data.
                </div>
              </div>
            )}

            {!loading && !error && hasAnyContent && (
              <div className="flex flex-col gap-5 animate-in fade-in duration-200">
                {visibleSections.map(({ section, sectionIndex, entries }) => (
                  <section key={sectionIndex} className="flex flex-col gap-2.5 p-0 sm:gap-3 sm:rounded-2xl sm:border sm:border-border/50 sm:bg-card/55 sm:p-3.5 sm:shadow-sm">
                    <SectionHeader
                      heading={section.heading}
                    />
                    <div className="flex flex-col gap-3">
                      {entries.map(({ entry, entryIndex }) => (
                        <EntryRenderer
                          key={entryIndex}
                          entry={entry}
                          entryIndex={entryIndex}
                          sectionIndex={sectionIndex}
                          sources={sources}
                          costsData={costsData}
                          costsLoading={costsLoading}
                          costsError={costsError}
                          evalsData={evalsData}
                          evalsLoading={evalsLoading}
                          evalsError={evalsError}
                          runsData={runsData}
                          runsLoading={runsLoading}
                          runsError={runsError}
                          hiddenWidgetKeys={hiddenWidgetKeys}
                          onToggleWidgetHidden={handleToggleWidgetHidden}
                        />
                      ))}
                    </div>
                  </section>
                ))}
              </div>
            )}
          </div>
        </div>
      </div>

      <div className="absolute bottom-4 right-4 z-20 flex flex-col gap-2 sm:bottom-5 sm:right-5">
        {canUseSplitPreview && (
          <button
            onClick={handleTogglePreviewMode}
            className="inline-flex h-9 w-9 items-center justify-center rounded-full border border-border/70 bg-background/95 text-muted-foreground shadow-lg backdrop-blur-sm transition-all hover:-translate-y-0.5 hover:bg-muted hover:text-foreground"
            title={isMobilePreview ? 'Switch to full-width preview' : 'Switch to mobile preview'}
            aria-label={isMobilePreview ? 'Switch to full-width preview' : 'Switch to mobile preview'}
          >
            {isMobilePreview ? <Monitor className="h-3.5 w-3.5" /> : <Smartphone className="h-3.5 w-3.5" />}
          </button>
        )}
        <button
          onClick={handleRefresh}
          disabled={isRefreshing}
          className="inline-flex h-9 w-9 items-center justify-center rounded-full border border-border/70 bg-background/95 text-muted-foreground shadow-lg backdrop-blur-sm transition-all hover:-translate-y-0.5 hover:bg-muted hover:text-foreground disabled:translate-y-0 disabled:cursor-not-allowed disabled:opacity-50"
          title={isRefreshing ? 'Refreshing…' : 'Refresh report'}
          aria-label="Refresh report"
        >
          <RefreshCw className={`h-3.5 w-3.5 ${isRefreshing ? 'animate-spin' : ''}`} />
        </button>
      </div>
    </div>
  )
}

// Loading skeleton — shimmer placeholders so the layout doesn't jump when widgets
// resolve. Uses a moving gradient overlay (keyframes defined inline) for a subtle
// shimmer, with card-shaped blocks matching typical section + widget heights.
function ReportSkeleton() {
  const shimmer =
    'relative overflow-hidden bg-muted/40 before:absolute before:inset-0 before:-translate-x-full before:animate-[shimmer_1.6s_infinite] before:bg-gradient-to-r before:from-transparent before:via-muted-foreground/10 before:to-transparent'
  return (
    <>
      <style>{`@keyframes shimmer { 100% { transform: translateX(100%); } }`}</style>
      <div className="flex flex-col gap-4">
        {[0, 1, 2].map(i => (
          <div key={i} className="flex flex-col gap-2.5 rounded-xl border border-border/50 bg-card/55 p-2.5 sm:rounded-2xl sm:p-3.5">
            <div className={`h-3 w-24 rounded-full ${shimmer}`} />
            <div className={`h-4 w-48 rounded ${shimmer}`} />
            <div className={`h-32 w-full rounded-xl border border-border/50 ${shimmer}`} />
          </div>
        ))}
      </div>
    </>
  )
}

function SectionHeader({
  heading,
}: {
  heading: string
}) {
  return (
    <div className="flex flex-col gap-2 border-b border-border/50 pb-2.5 sm:flex-row sm:flex-wrap sm:items-end">
      <div className="flex min-w-0 items-center gap-2.5">
        <div className="min-w-0">
          <div className="text-[10px] font-semibold uppercase tracking-[0.22em] text-muted-foreground">
            Report Section
          </div>
          <h3 className="m-0 truncate text-lg font-semibold tracking-tight text-foreground">
            {heading}
          </h3>
        </div>
      </div>
    </div>
  )
}

function WidgetHeader({
  widget,
  mode = 'default',
  className = '',
}: {
  widget: ReportWidget
  mode?: 'default' | 'metric'
  className?: string
}) {
  if (!widget.title && !widget.description) return null
  if (mode === 'metric') {
    return (
      <div className={`flex flex-col gap-1 ${className}`}>
        {widget.title && <div className="text-[11px] font-semibold uppercase tracking-[0.18em] text-muted-foreground">{widget.title}</div>}
        {widget.description && <div className="text-xs leading-5 text-muted-foreground">{widget.description}</div>}
      </div>
    )
  }
  return (
    <div className={`flex flex-col gap-1 ${className}`}>
      {widget.title && <div className="text-sm font-semibold text-foreground">{widget.title}</div>}
      {widget.description && <div className="text-xs leading-5 text-muted-foreground">{widget.description}</div>}
    </div>
  )
}

function EntryRenderer({
  entry,
  entryIndex,
  sectionIndex,
  sources,
  costsData,
  costsLoading,
  costsError,
  evalsData,
  evalsLoading,
  evalsError,
  runsData,
  runsLoading,
  runsError,
  hiddenWidgetKeys,
  onToggleWidgetHidden,
}: {
  entry: ReportEntry
  entryIndex: number
  sectionIndex: number
  sources: SourceCache
  costsData: WorkflowCostsResponse | null
  costsLoading: boolean
  costsError: string | null
  evalsData: EvaluationReportsResponse | null
  evalsLoading: boolean
  evalsError: string | null
  runsData: RunFoldersResponse | null
  runsLoading: boolean
  runsError: string | null
  hiddenWidgetKeys: Set<string>
  onToggleWidgetHidden: (widgetKey: string) => void
}) {
  const [rowRef, isCompact] = useCompactWidgetLayout()
  if (entry.kind === 'single') {
    const widgetKey = widgetInstanceKey(entry.widget, { sectionIndex, entryIndex, widgetIndex: 0 })
    return (
      <WidgetCard
        widget={entry.widget}
        sources={sources}
        costsData={costsData}
        costsLoading={costsLoading}
        costsError={costsError}
        evalsData={evalsData}
        evalsLoading={evalsLoading}
        evalsError={evalsError}
        runsData={runsData}
        runsLoading={runsLoading}
        runsError={runsError}
        hidden={hiddenWidgetKeys.has(widgetKey)}
        onToggleHidden={() => onToggleWidgetHidden(widgetKey)}
      />
    )
  }
  const visibleWidgets = entry.row.widgets.flatMap((widget, widgetIndex) => {
    const widgetKey = widgetInstanceKey(widget, { sectionIndex, entryIndex, widgetIndex })
    if (hiddenWidgetKeys.has(widgetKey)) return [{ widget, widgetKey, hidden: true }]
    if (!widgetShouldRender(widget, sources[widget.source])) return []
    return [{ widget, widgetKey, hidden: false }]
  })
  if (visibleWidgets.length === 0) return null
  return (
    <div ref={rowRef} className={`flex gap-2.5 ${isCompact ? 'flex-col' : 'flex-col xl:flex-row xl:flex-wrap'}`}>
      {visibleWidgets.map(({ widget, widgetKey, hidden }) => (
        <div key={widgetKey} className={`w-full ${isCompact ? '' : 'xl:min-w-[260px] xl:flex-1'}`}>
          <WidgetCard
            widget={widget}
            sources={sources}
            costsData={costsData}
            costsLoading={costsLoading}
            costsError={costsError}
            evalsData={evalsData}
            evalsLoading={evalsLoading}
            evalsError={evalsError}
            runsData={runsData}
            runsLoading={runsLoading}
            runsError={runsError}
            hidden={Boolean(hidden)}
            onToggleHidden={() => onToggleWidgetHidden(widgetKey)}
          />
        </div>
      ))}
    </div>
  )
}

function WidgetVisibilityButton({
  hidden = false,
  onToggle,
  className = '',
}: {
  hidden?: boolean
  onToggle: () => void
  className?: string
}) {
  const Icon = hidden ? ChevronDown : ChevronUp
  return (
    <button
      type="button"
      onClick={onToggle}
      className={`absolute right-2 top-2 z-20 inline-flex h-7 w-7 items-center justify-center rounded-full border border-border/70 bg-background/90 text-muted-foreground shadow-sm backdrop-blur-sm transition-colors hover:bg-muted hover:text-foreground ${className}`}
      title={hidden ? 'Show widget' : 'Hide widget'}
      aria-label={hidden ? 'Show widget' : 'Hide widget'}
    >
      <Icon className="h-3.5 w-3.5" />
    </button>
  )
}

function StandaloneWidgetNotice({
  children,
  hidden = false,
  onToggleHidden,
}: {
  children: React.ReactNode
  hidden?: boolean
  onToggleHidden?: () => void
}) {
  return (
    <div className="relative rounded-xl bg-card/70 px-4 py-3 sm:border sm:border-border/60 sm:bg-card/80 sm:shadow-sm">
      {onToggleHidden && <WidgetVisibilityButton hidden={hidden} onToggle={onToggleHidden} />}
      {children}
    </div>
  )
}

type SingularWidgetSourceResolution =
  | { status: 'ok'; value: unknown }
  | { status: 'no-match'; value: undefined }
  | { status: 'multi-match'; value: unknown[] }

function normalizeSingularWidgetPath(path: string): string {
  const trimmed = path.trim()
  if (!trimmed || trimmed === '$' || trimmed === '$[*]' || trimmed === '.' || trimmed === '*') return ''
  if (trimmed.startsWith('$[*].')) return trimmed.slice(5)
  if (trimmed.startsWith('$.')) return trimmed.slice(2)
  return trimmed
}

function resolveSingularWidgetSource(source: unknown, widget: ReportWidget): SingularWidgetSourceResolution {
  const filtered = applyWidgetFilter(source, widget.filter)
  if (!Array.isArray(filtered)) return { status: 'ok', value: filtered }
  if (filtered.length === 0) return { status: 'no-match', value: undefined }
  if (filtered.length === 1) return { status: 'ok', value: filtered[0] }
  return { status: 'multi-match', value: filtered }
}

// Source mode classifies how WidgetCard should resolve data for each widget
// kind. The four modes determine the dispatch path:
//   - api-*       — pulls from a workflow-level API endpoint, ignores `source`
//   - singular    — narrows the source to a single row (for stat / alert)
//   - pivot       — feeds the raw source straight to the renderer
//   - collection  — resolves `path` + `filter` then hands an array/value to
//                   the renderer
type WidgetSourceMode = 'api-costs' | 'api-evals' | 'api-runs' | 'singular' | 'pivot' | 'collection'

const WIDGET_SOURCE_MODE: Record<ReportWidgetKind, WidgetSourceMode> = {
  costs: 'api-costs',
  evals: 'api-evals',
  runs: 'api-runs',
  stat: 'singular',
  alert: 'singular',
  pivot: 'pivot',
  text: 'collection',
  markdown: 'collection',
  table: 'collection',
  cards: 'collection',
  chart: 'collection',
}

// Standalone widgets render their own outer container; everything else gets
// wrapped in WidgetShell. This is also the set whose loading / missing-source
// notices use StandaloneWidgetNotice instead of the standard shell.
const STANDALONE_WIDGET_KINDS: ReadonlySet<ReportWidgetKind> = new Set(['stat', 'alert'])

const isStandaloneWidget = (kind: ReportWidgetKind) => STANDALONE_WIDGET_KINDS.has(kind)

// Renderer registries. Each map covers one source mode. Adding a new widget
// kind: extend ReportWidgetKind, add an entry to WIDGET_SOURCE_MODE, and
// register the renderer here — WidgetCard itself doesn't change.
type CollectionWidgetRenderer = React.FC<{ value: unknown; widget: ReportWidget }>
type SingularWidgetRenderer = React.FC<{
  source: unknown
  resolution: SingularWidgetSourceResolution
  widget: ReportWidget
  onToggleHidden?: () => void
}>
type PivotWidgetRenderer = React.FC<{ source: unknown; widget: ReportWidget }>

const COLLECTION_WIDGET_RENDERERS: Partial<Record<ReportWidgetKind, CollectionWidgetRenderer>> = {}
const SINGULAR_WIDGET_RENDERERS: Partial<Record<ReportWidgetKind, SingularWidgetRenderer>> = {}
const PIVOT_WIDGET_RENDERERS: Partial<Record<ReportWidgetKind, PivotWidgetRenderer>> = {}

function HiddenWidgetCard({
  widget,
  onShow,
}: {
  widget: ReportWidget
  onShow: () => void
}) {
  return (
    <div className="relative flex min-h-[52px] items-center rounded-xl border border-dashed border-border/70 bg-muted/15 px-3 py-2.5 shadow-sm">
      <div className="min-w-0 pr-10">
        <div className="truncate text-sm font-medium text-foreground">
          {widget.title || `${widget.kind[0].toUpperCase()}${widget.kind.slice(1)} widget`}
        </div>
        <div className="text-xs text-muted-foreground">
          Hidden widget
        </div>
      </div>
      <WidgetVisibilityButton hidden onToggle={onShow} />
    </div>
  )
}

function WidgetCard({
  widget,
  sources,
  costsData,
  costsLoading,
  costsError,
  evalsData,
  evalsLoading,
  evalsError,
  runsData,
  runsLoading,
  runsError,
  hidden = false,
  onToggleHidden,
}: {
  widget: ReportWidget
  sources: SourceCache
  costsData: WorkflowCostsResponse | null
  costsLoading: boolean
  costsError: string | null
  evalsData: EvaluationReportsResponse | null
  evalsLoading: boolean
  evalsError: string | null
  runsData: RunFoldersResponse | null
  runsLoading: boolean
  runsError: string | null
  hidden?: boolean
  onToggleHidden?: () => void
}) {
  if (hidden && onToggleHidden) {
    return <HiddenWidgetCard widget={widget} onShow={onToggleHidden} />
  }

  const mode = WIDGET_SOURCE_MODE[widget.kind]

  // API-driven widgets bypass the file-source pipeline.
  if (mode === 'api-costs') {
    return (
      <WidgetShell widget={widget} onToggleHidden={onToggleHidden}>
        <CostsWidget widget={widget} costsData={costsData} loading={costsLoading} error={costsError} />
      </WidgetShell>
    )
  }
  if (mode === 'api-evals') {
    return (
      <WidgetShell widget={widget} onToggleHidden={onToggleHidden}>
        <EvalsWidget widget={widget} evalsData={evalsData} loading={evalsLoading} error={evalsError} />
      </WidgetShell>
    )
  }
  if (mode === 'api-runs') {
    return (
      <WidgetShell widget={widget} onToggleHidden={onToggleHidden}>
        <RunsWidget widget={widget} runsData={runsData} loading={runsLoading} error={runsError} />
      </WidgetShell>
    )
  }

  const wrapNotice = (content: React.ReactNode) =>
    isStandaloneWidget(widget.kind) ? (
      <StandaloneWidgetNotice onToggleHidden={onToggleHidden}>{content}</StandaloneWidgetNotice>
    ) : (
      <WidgetShell widget={widget} onToggleHidden={onToggleHidden}>{content}</WidgetShell>
    )

  const raw = sources[widget.source]
  if (raw === undefined) {
    return wrapNotice(
      <div className="py-1.5 text-xs italic text-muted-foreground">Loading {widget.source}…</div>,
    )
  }
  if (raw === null) {
    return wrapNotice(
      <div className="py-1.5 text-xs italic text-muted-foreground">
        Source not available: <code className="px-1 rounded bg-muted">{widget.source}</code>
      </div>,
    )
  }

  const singularSource = mode === 'singular' ? resolveSingularWidgetSource(raw, widget) : null
  const conditionalSource = singularSource?.status === 'ok' ? singularSource.value : raw
  if (!evaluateShowIf(conditionalSource, widget.showIf)) return null

  if (mode === 'singular') {
    const Renderer = SINGULAR_WIDGET_RENDERERS[widget.kind]
    if (!Renderer) return null
    return (
      <Renderer
        source={singularSource?.status === 'ok' ? singularSource.value : undefined}
        resolution={singularSource ?? { status: 'ok', value: raw }}
        widget={widget}
        onToggleHidden={onToggleHidden}
      />
    )
  }

  if (mode === 'pivot') {
    const Renderer = PIVOT_WIDGET_RENDERERS[widget.kind]
    if (!Renderer) return null
    return (
      <WidgetShell widget={widget} onToggleHidden={onToggleHidden}>
        <Renderer source={raw} widget={widget} />
      </WidgetShell>
    )
  }

  // Collection mode: resolve path → filter → render.
  const resolvedRaw = resolveJSONPath(raw, widget.path)
  let content: React.ReactNode = null
  if (resolvedRaw === undefined) {
    content = (
      <WidgetError
        widget={widget}
        message={`Path "${widget.path || '(root)'}" doesn't resolve in ${widget.source}.`}
        hint="Check the source JSON for a matching key. Run validate_report_plan in builder chat for specifics."
      />
    )
  } else {
    const resolved = applyWidgetFilter(resolvedRaw, widget.filter)
    if (resolved == null) return null
    if (Array.isArray(resolved) && resolved.length === 0) {
      content = (
        <WidgetError
          widget={widget}
          message={`No rows in ${widget.source}${widget.filter ? ` matching filter "${widget.filter}"` : ''}.`}
          hint="The source is valid but empty for this widget; this usually clears after the workflow runs."
          severity="info"
        />
      )
    } else {
      const Renderer = COLLECTION_WIDGET_RENDERERS[widget.kind]
      if (Renderer) content = <Renderer value={resolved} widget={widget} />
    }
  }

  if (content == null) return null
  return (
    <WidgetShell widget={widget} onToggleHidden={onToggleHidden}>
      {content}
    </WidgetShell>
  )
}

function makeSyntheticTableWidget(options: {
  formats?: Record<string, ReportFormatterName>
  defaultSort?: { field: string; direction: 'asc' | 'desc' }
  colorBy?: string
  colorMap?: Record<string, string>
  hideColumns?: string[]
} = {}): ReportWidget {
  return {
    kind: 'table',
    source: '',
    path: '',
    enableSearch: true,
    pageSize: 10,
    ...options,
  }
}

function makeSyntheticChartWidget(options: {
  chartType?: 'bar' | 'line' | 'area'
  xAxis?: string
  yAxis?: string
  series?: string[]
  stacked?: boolean
  format?: ReportFormatterName
} = {}): ReportWidget {
  return {
    kind: 'chart',
    source: '',
    path: '',
    chartType: options.chartType ?? 'bar',
    xAxis: options.xAxis,
    yAxis: options.yAxis,
    series: options.series,
    stacked: options.stacked,
    format: options.format,
    showValues: false,
    height: 260,
  }
}

function CostsWidget({
  widget,
  costsData,
  loading,
  error,
}: {
  widget: ReportWidget
  costsData: WorkflowCostsResponse | null
  loading: boolean
  error: string | null
}) {
  const [summaryRef, isCompact] = useCompactWidgetLayout()
  const scope = widget.costsScope ?? 'all'
  const view = widget.costsView ?? 'summary'
  const metric = widget.costsMetric ?? 'cost'
  const runScope = scope === 'phase' ? 'all' : scope

  const phaseSummary = useMemo(() => summarisePhaseCosts(costsData?.phase_token_usage ?? null), [costsData])

  const runSummaries = useMemo(() => {
    if (!costsData?.runs || scope === 'phase') return []
    const filtered = costsData.runs
      .filter(entry => {
        if (!widget.group) return true
        return entry.run_folder === widget.group || entry.run_folder.endsWith(`/${widget.group}`)
      })
      .map(entry => summariseRunCosts(entry.run_folder, entry.token_usage, entry.evaluation_token_usage, runScope))
      .filter((entry): entry is RunCostSummary => entry !== null)
      .sort((a, b) => {
        const aTime = a.updatedAt ? new Date(a.updatedAt).getTime() : 0
        const bTime = b.updatedAt ? new Date(b.updatedAt).getTime() : 0
        return bTime - aTime || b.totalCost - a.totalCost || a.runFolder.localeCompare(b.runFolder)
      })
    if (!widget.runFolder) return filtered.slice(0, DEFAULT_COST_WIDGET_RUN_LIMIT)
    if (widget.runFolder === 'latest') return filtered.length > 0 ? [filtered[0]] : []
    return filtered.filter(entry => entry.runFolder === widget.runFolder)
  }, [costsData, runScope, scope, widget.group, widget.runFolder])

  const aggregateRunSummary = useMemo(() => aggregateRunCostSummaries(runSummaries), [runSummaries])
  const hasAnyRunCosts = (costsData?.runs?.length ?? 0) > 0
  const hasScopedRunCosts = runSummaries.length > 0
  const costsEmptyState = useMemo(() => {
    if (!hasAnyRunCosts) {
      return {
        message: 'No run cost data is available yet.',
        hint: 'Execution and evaluation costs appear after runs have persisted token usage into the workflow costs ledger.',
      }
    }
    if (widget.runFolder || widget.group) {
      return {
        message: 'No run cost data matched the selected filter.',
        hint: 'Adjust the run/group filter, or remove it so the widget can see the available run ledgers.',
      }
    }
    return {
      message: 'No run cost data is available for the selected scope.',
      hint: 'The workflow has cost data, but none of the persisted run ledgers matched this widget scope.',
    }
  }, [hasAnyRunCosts, widget.group, widget.runFolder])

  const summaryCards = useMemo(() => {
    if (scope === 'phase') {
      if (!phaseSummary) return []
      return [
        { label: 'Total Cost', value: formatMetricValue('cost', phaseSummary.totalCost) },
        { label: 'Total Tokens', value: formatMetricValue('total_tokens', phaseSummary.totalTokens) },
        { label: 'LLM Calls', value: formatMetricValue('llm_calls', phaseSummary.totalLLMCalls) },
        { label: 'Phases', value: String(phaseSummary.phaseCosts.length) },
      ]
    }
    if (!aggregateRunSummary) return []
    return [
      { label: 'Total Cost', value: formatMetricValue('cost', aggregateRunSummary.totalCost) },
      { label: 'Total Tokens', value: formatMetricValue('total_tokens', aggregateRunSummary.totalTokens) },
      { label: 'LLM Calls', value: formatMetricValue('llm_calls', aggregateRunSummary.totalLLMCalls) },
      { label: 'Runs', value: String(runSummaries.length) },
    ]
  }, [aggregateRunSummary, phaseSummary, runSummaries.length, scope])

  const recentRunCards = useMemo(() => {
    return runSummaries.slice(0, DEFAULT_COST_WIDGET_RUN_LIMIT).map(run => ({
      runFolder: run.runFolder,
      updatedAt: run.updatedAt,
      value: formatMetricValue(metric, metricValue(metric, run)),
    }))
  }, [metric, runSummaries])

  if (loading) {
    return (
      <div className="flex flex-col gap-3">
        <WidgetHeader widget={widget} />
        <div className="rounded-xl border border-border/60 bg-background/70 px-3 py-3 text-sm text-muted-foreground">
          Loading workflow costs…
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex flex-col gap-3">
        <WidgetHeader widget={widget} />
        <WidgetError
          widget={widget}
          message={error}
          hint="Costs widgets read aggregated data from the workflow costs API."
          showWidgetMeta={false}
        />
      </div>
    )
  }

  if (scope === 'phase') {
    if (!phaseSummary) {
      return (
        <div className="flex flex-col gap-3">
          <WidgetHeader widget={widget} />
          <WidgetError
            widget={widget}
            message="No phase cost data is available yet."
            hint="Phase costs come from builder-style workflow sessions and appear after token usage has been persisted."
            severity="info"
            showWidgetMeta={false}
          />
        </div>
      )
    }

    const phaseChartRows = phaseSummary.phaseCosts.map(phase => ({
      label: phase.phaseTitle,
      value: metricValue(metric, phase),
    }))
    const modelRows = phaseSummary.modelCosts.map(model => ({
      model: model.modelID,
      provider: model.provider,
      total_cost: model.totalCost,
      total_tokens: model.totalTokens,
      input_tokens: model.inputTokens,
      output_tokens: model.outputTokens,
      llm_calls: model.llmCalls,
    }))

    return (
      <div ref={summaryRef} className="flex flex-col gap-3">
        <WidgetHeader widget={widget} />
        {view === 'summary' && (
          <>
            <div className={`grid gap-2.5 ${isCompact ? 'grid-cols-1' : 'grid-cols-2 xl:grid-cols-4'}`}>
              {summaryCards.map(card => (
                <div key={card.label} className={isCompact ? 'rounded-xl bg-card/70 px-3 py-2.5' : 'rounded-2xl border border-border/60 bg-gradient-to-br from-card via-card to-muted/20 px-3 py-2.5 shadow-sm'}>
                  <div className="text-[10px] font-semibold uppercase tracking-[0.18em] text-muted-foreground">{card.label}</div>
                  <div className="mt-1 text-xl font-semibold tracking-tight text-foreground">{card.value}</div>
                </div>
              ))}
            </div>
            <div className={isCompact ? 'rounded-xl bg-background/35 px-2.5 py-2.5' : 'rounded-xl border border-border/60 bg-background/55 px-2.5 py-2.5'}>
              <div className="mb-2 text-[11px] font-semibold uppercase tracking-[0.16em] text-muted-foreground">Phase Breakdown</div>
              <div className="flex flex-wrap gap-1.5">
                {phaseSummary.phaseCosts.slice(0, 6).map(phase => (
                  <div key={phase.phaseID} className={isCompact ? 'rounded-full bg-background/60 px-2.5 py-1 text-xs text-foreground' : 'rounded-full border border-border/60 bg-background/80 px-2.5 py-1 text-xs text-foreground'}>
                    {phase.phaseTitle}: <span className="font-medium">{formatMetricValue(metric, metricValue(metric, phase))}</span>
                  </div>
                ))}
              </div>
            </div>
          </>
        )}
        {view === 'stage-breakdown' && (
          <ChartWidget
            value={phaseChartRows}
            widget={makeSyntheticChartWidget({
              chartType: 'bar',
              xAxis: 'label',
              yAxis: 'value',
            })}
          />
        )}
        {view === 'model-table' && (
          <TableWidget
            value={modelRows}
            widget={makeSyntheticTableWidget({
              formats: {
                total_cost: 'currency-usd',
                total_tokens: 'number',
                input_tokens: 'number',
                output_tokens: 'number',
                llm_calls: 'number',
              },
              defaultSort: { field: metric === 'cost' ? 'total_cost' : metric, direction: 'desc' },
            })}
          />
        )}
        {view === 'run-table' || view === 'step-table' ? (
          <WidgetError
            widget={widget}
            message={`"${view}" is not available for phase scope.`}
            hint="Use `summary`, `stage-breakdown`, or `model-table` with `scope: phase`."
            severity="info"
            showWidgetMeta={false}
          />
        ) : null}
        {view !== 'summary' && view !== 'stage-breakdown' && view !== 'model-table' && view !== 'run-table' && view !== 'step-table' && (
          <WidgetError widget={widget} message={`Unsupported costs view "${view}".`} severity="info" showWidgetMeta={false} />
        )}
      </div>
    )
  }

  if (!aggregateRunSummary || !hasScopedRunCosts) {
    return (
      <div className="flex flex-col gap-3">
        <WidgetHeader widget={widget} />
        <WidgetError
          widget={widget}
          message={costsEmptyState.message}
          hint={costsEmptyState.hint}
          severity="info"
          showWidgetMeta={false}
        />
      </div>
    )
  }

  const stageMetricMap = aggregateRunSummary.stepCosts.reduce((acc, step) => {
    const current = acc.get(step.stage) ?? {
      label: step.stage.replace(/_/g, ' '),
      totalCost: 0,
      totalTokens: 0,
      inputTokens: 0,
      outputTokens: 0,
      llmCalls: 0,
    }
    current.totalCost += step.totalCost
    current.totalTokens += step.totalTokens
    current.inputTokens += step.inputTokens
    current.outputTokens += step.outputTokens
    current.llmCalls += step.llmCalls
    acc.set(step.stage, current)
    return acc
  }, new Map<string, {
    label: string
    totalCost: number
    totalTokens: number
    inputTokens: number
    outputTokens: number
    llmCalls: number
  }>())

  const stageMetricRows = Array.from(stageMetricMap.values())
    .map(stage => ({
      label: stage.label,
      value: metricValue(metric, stage),
    }))
    .filter(stage => stage.value > 0)

  const runRows = runSummaries.map(run => ({
    run_folder: run.runFolder,
    updated_at: run.updatedAt ?? '',
    total_cost: run.totalCost,
    total_tokens: run.totalTokens,
    input_tokens: run.totalInputTokens,
    output_tokens: run.totalOutputTokens,
    llm_calls: run.totalLLMCalls,
  }))

  const stepRows = aggregateRunSummary.stepCosts.map(step => ({
    step: step.stepTitle,
    stage: step.stage,
    total_cost: step.totalCost,
    total_tokens: step.totalTokens,
    input_tokens: step.inputTokens,
    output_tokens: step.outputTokens,
    llm_calls: step.llmCalls,
  }))

  const modelRows = aggregateRunSummary.modelCosts.map(model => ({
    model: model.modelID,
    provider: model.provider,
    total_cost: model.totalCost,
    total_tokens: model.totalTokens,
    input_tokens: model.inputTokens,
    output_tokens: model.outputTokens,
    llm_calls: model.llmCalls,
  }))

  return (
    <div ref={summaryRef} className="flex flex-col gap-3">
      <WidgetHeader widget={widget} />
      {view === 'summary' && (
        <>
          <div className={`grid gap-2.5 ${isCompact ? 'grid-cols-1' : 'grid-cols-1 xl:grid-cols-3'}`}>
            {recentRunCards.map(card => (
              <div key={card.runFolder} className={isCompact ? 'rounded-xl bg-card/70 px-3 py-2.5' : 'rounded-2xl border border-border/60 bg-gradient-to-br from-card via-card to-muted/20 px-3 py-2.5 shadow-sm'}>
                <div className="text-[10px] font-semibold uppercase tracking-[0.18em] text-muted-foreground">{metricLabel(metric)}</div>
                <div className="mt-1 truncate text-sm font-medium text-foreground">{card.runFolder}</div>
                <div className="mt-1 text-xl font-semibold tracking-tight text-foreground">{card.value}</div>
                <div className="mt-1 text-xs text-muted-foreground">
                  {formatNamed(card.updatedAt, 'datetime').text}
                </div>
              </div>
            ))}
          </div>
        </>
      )}
      {view === 'stage-breakdown' && (
        <ChartWidget
          value={stageMetricRows}
          widget={makeSyntheticChartWidget({
            chartType: 'bar',
            xAxis: 'label',
            yAxis: 'value',
          })}
        />
      )}
      {view === 'run-table' && (
        <TableWidget
          value={runRows}
          widget={makeSyntheticTableWidget({
            formats: {
              total_cost: 'currency-usd',
              total_tokens: 'number',
              input_tokens: 'number',
              output_tokens: 'number',
              llm_calls: 'number',
              updated_at: 'datetime',
            },
            defaultSort: { field: metric === 'cost' ? 'total_cost' : metric, direction: 'desc' },
          })}
        />
      )}
      {view === 'step-table' && (
        <TableWidget
          value={stepRows}
          widget={makeSyntheticTableWidget({
            formats: {
              total_cost: 'currency-usd',
              total_tokens: 'number',
              input_tokens: 'number',
              output_tokens: 'number',
              llm_calls: 'number',
            },
            defaultSort: { field: metric === 'cost' ? 'total_cost' : metric, direction: 'desc' },
          })}
        />
      )}
      {view === 'model-table' && (
        <TableWidget
          value={modelRows}
          widget={makeSyntheticTableWidget({
            formats: {
              total_cost: 'currency-usd',
              total_tokens: 'number',
              input_tokens: 'number',
              output_tokens: 'number',
              llm_calls: 'number',
            },
            defaultSort: { field: metric === 'cost' ? 'total_cost' : metric, direction: 'desc' },
          })}
        />
      )}
      {view !== 'summary' && view !== 'stage-breakdown' && view !== 'run-table' && view !== 'step-table' && view !== 'model-table' && (
        <WidgetError widget={widget} message={`Unsupported costs view "${view}".`} severity="info" showWidgetMeta={false} />
      )}
    </div>
  )
}

function EvalsWidget({
  widget,
  evalsData,
  loading,
  error,
}: {
  widget: ReportWidget
  evalsData: EvaluationReportsResponse | null
  loading: boolean
  error: string | null
}) {
  const [summaryRef, isCompact] = useCompactWidgetLayout()
  const view = widget.evalsView ?? 'summary'
  const metric: ReportEvalsMetric = widget.evalsMetric ?? 'score_percentage'

  const selectedReports = useMemo(() => {
    const reports = (evalsData?.reports ?? [])
      .filter(entry => {
        if (!widget.group) return true
        return entry.run_folder === widget.group || entry.run_folder.endsWith(`/${widget.group}`)
      })
      .sort((a, b) => {
        const aTime = new Date(a.report.generated_at).getTime()
        const bTime = new Date(b.report.generated_at).getTime()
        return bTime - aTime || b.report.score_percentage - a.report.score_percentage || a.run_folder.localeCompare(b.run_folder)
      })

    if (!widget.runFolder) return reports.slice(0, DEFAULT_EVAL_WIDGET_RUN_LIMIT)
    if (widget.runFolder === 'latest') return reports.length > 0 ? [reports[0]] : []
    return reports.filter(entry => entry.run_folder === widget.runFolder)
  }, [evalsData, widget.group, widget.runFolder])

  const summary = useMemo(() => {
    if (selectedReports.length === 0) return null
    const totals = selectedReports.reduce((acc, entry) => {
      acc.totalScore += entry.report.total_score
      acc.totalMaxScore += entry.report.max_possible_score
      acc.totalSteps += entry.report.step_scores.length
      return acc
    }, {
      totalScore: 0,
      totalMaxScore: 0,
      totalSteps: 0,
    })
    const percentages = selectedReports.map(entry => entry.report.score_percentage)
    return {
      totalRuns: selectedReports.length,
      averagePercentage: percentages.reduce((acc, value) => acc + value, 0) / percentages.length,
      highestPercentage: Math.max(...percentages),
      lowestPercentage: Math.min(...percentages),
      totalScore: totals.totalScore,
      totalMaxScore: totals.totalMaxScore,
      totalSteps: totals.totalSteps,
    }
  }, [selectedReports])

  const runChartRows = useMemo(() => {
    return selectedReports.map(entry => ({
      label: entry.run_folder.split('/').slice(-2).join(' / ') || entry.run_folder,
      value: metric === 'total_score' ? entry.report.total_score : entry.report.score_percentage,
    }))
  }, [metric, selectedReports])

  const runRows = useMemo(() => {
    return selectedReports.map(entry => ({
      run_folder: entry.run_folder,
      generated_at: entry.report.generated_at,
      total_score: entry.report.total_score,
      max_possible_score: entry.report.max_possible_score,
      score_percentage: entry.report.score_percentage,
      step_count: entry.report.step_scores.length,
      score_band: evalScoreTone(entry.report.score_percentage).label,
    }))
  }, [selectedReports])

  const stepRows = useMemo(() => {
    return selectedReports.flatMap(entry =>
      entry.report.step_scores.map(step => ({
        run_folder: entry.run_folder,
        generated_at: entry.report.generated_at,
        step_id: step.step_id,
        score: step.score,
        max_score: step.max_score,
        score_percentage: step.max_score > 0 ? (step.score / step.max_score) * 100 : 0,
        reasoning: step.reasoning,
        evidence: step.evidence,
      }))
    )
      .sort((a, b) => a.score_percentage - b.score_percentage || a.run_folder.localeCompare(b.run_folder) || a.step_id.localeCompare(b.step_id))
  }, [selectedReports])

  const summaryCards = useMemo(() => {
    if (!summary) return []
    return [
      { label: 'Average Score', value: `${summary.averagePercentage.toFixed(1)}%` },
      { label: 'Highest Score', value: `${summary.highestPercentage.toFixed(1)}%` },
      { label: 'Lowest Score', value: `${summary.lowestPercentage.toFixed(1)}%` },
      { label: 'Runs', value: String(summary.totalRuns) },
    ]
  }, [summary])

  if (loading) {
    return (
      <div className="flex flex-col gap-3">
        <WidgetHeader widget={widget} />
        <div className="rounded-xl border border-border/60 bg-background/70 px-3 py-3 text-sm text-muted-foreground">
          Loading evaluation reports…
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex flex-col gap-3">
        <WidgetHeader widget={widget} />
        <WidgetError
          widget={widget}
          message={error}
          hint="Evals widgets read aggregated data from the evaluation reports API."
          showWidgetMeta={false}
        />
      </div>
    )
  }

  if (!summary || selectedReports.length === 0) {
    return (
      <div className="flex flex-col gap-3">
        <WidgetHeader widget={widget} />
        <WidgetError
          widget={widget}
          message="No evaluation reports are available for the selected scope."
          hint="Run evaluation first, or remove the run/group filter so the widget can see existing reports."
          severity="info"
          showWidgetMeta={false}
        />
      </div>
    )
  }

  return (
    <div ref={summaryRef} className="flex flex-col gap-3">
      <WidgetHeader widget={widget} />
      {view === 'summary' && (
        <>
          <div className={`grid gap-2.5 ${isCompact ? 'grid-cols-1' : 'grid-cols-2 xl:grid-cols-4'}`}>
            {summaryCards.map(card => (
              <div key={card.label} className={isCompact ? 'rounded-xl bg-card/70 px-3 py-2.5' : 'rounded-2xl border border-border/60 bg-gradient-to-br from-card via-card to-muted/20 px-3 py-2.5 shadow-sm'}>
                <div className="text-[10px] font-semibold uppercase tracking-[0.18em] text-muted-foreground">{card.label}</div>
                <div className="mt-1 text-xl font-semibold tracking-tight text-foreground">{card.value}</div>
              </div>
            ))}
          </div>
        </>
      )}
      {view === 'run-chart' && (
        <ChartWidget
          value={runChartRows}
          widget={makeSyntheticChartWidget({
            chartType: 'bar',
            xAxis: 'label',
            yAxis: 'value',
          })}
        />
      )}
      {view === 'run-table' && (
        <EvalRunCards
          rows={runRows}
          pageSize={widget.pageSize ?? DEFAULT_TABLE_PAGE_SIZE}
          compact={isCompact}
        />
      )}
      {view === 'step-table' && (
        <TableWidget
          value={stepRows}
          widget={makeSyntheticTableWidget({
            formats: {
              generated_at: 'datetime',
              score: 'number',
              max_score: 'number',
              score_percentage: 'number-1dp',
            },
            defaultSort: { field: 'generated_at', direction: 'desc' },
          })}
        />
      )}
      {view !== 'summary' && view !== 'run-chart' && view !== 'run-table' && view !== 'step-table' && (
        <WidgetError widget={widget} message={`Unsupported evals view "${view}".`} severity="info" showWidgetMeta={false} />
      )}
    </div>
  )
}

function EvalRunCards({
  rows,
  pageSize,
  compact,
}: {
  rows: Array<{
    run_folder: string
    generated_at: string
    total_score: number
    max_possible_score: number
    score_percentage: number
    step_count: number
    score_band: string
  }>
  pageSize: number
  compact: boolean
}) {
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(0)

  const filteredRows = useMemo(() => {
    const needle = search.trim().toLowerCase()
    if (!needle) return rows
    return rows.filter(row =>
      row.run_folder.toLowerCase().includes(needle) ||
      row.score_band.toLowerCase().includes(needle) ||
      formatNamed(row.generated_at, 'datetime').text.toLowerCase().includes(needle),
    )
  }, [rows, search])

  const totalPages = Math.max(1, Math.ceil(filteredRows.length / pageSize))
  const safePage = Math.min(page, totalPages - 1)
  const pageRows = useMemo(
    () => filteredRows.slice(safePage * pageSize, (safePage + 1) * pageSize),
    [filteredRows, safePage, pageSize],
  )

  const rowCountText =
    filteredRows.length === rows.length
      ? `${filteredRows.length} run${filteredRows.length === 1 ? '' : 's'}`
      : `${filteredRows.length} of ${rows.length}`

  const handleExport = () => {
    const csv = rowsToCSV(filteredRows as Record<string, unknown>[], [
      'run_folder',
      'generated_at',
      'total_score',
      'max_possible_score',
      'score_percentage',
      'step_count',
    ])
    const blob = new Blob([csv], { type: 'text/csv;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `eval-scores-${new Date().toISOString().replace(/[:.]/g, '-')}.csv`
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
  }

  return (
    <div className="flex flex-col gap-2.5">
      <div className="flex flex-col gap-1.5 text-xs">
        <div className="relative w-full">
          <Search className="absolute left-2 top-1.5 h-3.5 w-3.5 text-muted-foreground" />
          <input
            type="text"
            placeholder="Search runs…"
            value={search}
            onChange={e => {
              setSearch(e.target.value)
              setPage(0)
            }}
            className="w-full rounded-md border border-input bg-muted/30 py-1.5 pl-7 pr-2 text-xs focus:outline-none focus:ring-1 focus:ring-primary"
          />
        </div>
        <div className="flex items-center gap-2">
          <div className="inline-flex items-center rounded-full border border-border bg-background/80 px-2 py-1 text-muted-foreground">
            {rowCountText}
          </div>
          <button
            onClick={handleExport}
            className="ml-auto inline-flex items-center gap-1 rounded-md border border-border bg-background/80 px-2 py-1.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
            title="Export CSV"
          >
            <Download className="h-3.5 w-3.5" />
            <span>CSV</span>
          </button>
        </div>
      </div>

      <div className={`grid gap-2.5 ${compact ? 'grid-cols-1' : 'grid-cols-1 xl:grid-cols-2'}`}>
        {pageRows.map((row, index) => {
          const tone = evalScoreTone(row.score_percentage)
          const generatedAt = formatNamed(row.generated_at, 'datetime').text
          const clampedPercentage = Math.max(0, Math.min(100, row.score_percentage))
          return (
            <div
              key={`${row.run_folder}:${row.generated_at}:${index}`}
              className={`overflow-hidden rounded-xl border border-border/50 bg-gradient-to-br ${tone.accentClassName} px-3 py-3 shadow-sm`}
            >
              <div className="flex items-start gap-3">
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm font-semibold text-foreground">
                    {row.run_folder}
                  </div>
                  <div className="mt-0.5 text-xs text-muted-foreground">
                    {generatedAt}
                  </div>
                </div>
                <div className={`inline-flex shrink-0 items-center rounded-full border px-2 py-1 text-[11px] font-semibold ${tone.pillClassName}`}>
                  {row.score_percentage.toFixed(1)}%
                </div>
              </div>

              <div className="mt-3 rounded-xl border border-border/45 bg-background/45 px-3 py-2.5">
                <div className="flex items-center justify-between gap-3">
                  <div className="text-[10px] font-semibold uppercase tracking-[0.16em] text-muted-foreground">
                    Score Progress
                  </div>
                  <div className="text-sm font-semibold tabular-nums text-foreground">
                    {row.total_score}
                    <span className="text-muted-foreground"> / {row.max_possible_score}</span>
                  </div>
                </div>
                <div className="mt-2 h-2 overflow-hidden rounded-full bg-background/80">
                  <div
                    className={`h-full rounded-full transition-[width] duration-300 ${evalScoreBarClass(row.score_percentage)}`}
                    style={{ width: `${clampedPercentage}%` }}
                  />
                </div>
                <div className="mt-1 flex items-center justify-between text-[11px] text-muted-foreground">
                  <span>{row.score_band}</span>
                  <span>{clampedPercentage.toFixed(1)}%</span>
                </div>
              </div>

              <div className={`mt-3 grid gap-2 ${compact ? 'grid-cols-2' : 'grid-cols-3'}`}>
                <div className="rounded-lg bg-background/60 px-2.5 py-2">
                  <div className="text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">
                    Steps
                  </div>
                  <div className="mt-1 text-sm font-semibold tabular-nums text-foreground">
                    {row.step_count}
                  </div>
                </div>
                <div className="rounded-lg bg-background/60 px-2.5 py-2">
                  <div className="text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">
                    Status
                  </div>
                  <div className="mt-1 text-sm font-semibold text-foreground">
                    {row.score_band}
                  </div>
                </div>
                <div className={`rounded-lg bg-background/60 px-2.5 py-2 ${compact ? 'col-span-2' : ''}`}>
                  <div className="text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">
                    Generated
                  </div>
                  <div className="mt-1 truncate text-sm font-medium text-foreground">
                    {generatedAt}
                  </div>
                </div>
              </div>
            </div>
          )
        })}
      </div>

      {totalPages > 1 && (
        <div className="flex flex-wrap items-center justify-end gap-1.5 text-xs text-muted-foreground">
          <button
            onClick={() => setPage(current => Math.max(0, current - 1))}
            disabled={safePage === 0}
            className="inline-flex items-center gap-0.5 rounded-md border border-border bg-background px-2 py-1 disabled:opacity-30 hover:bg-muted hover:text-foreground transition-colors"
            aria-label="Previous page"
          >
            <ChevronLeft className="h-3.5 w-3.5" />
            Prev
          </button>
          <span className="inline-flex items-center rounded-md bg-primary/10 px-2 py-1 font-medium tabular-nums text-primary">
            {safePage + 1}
            <span className="mx-1 opacity-60">/</span>
            {totalPages}
          </span>
          <button
            onClick={() => setPage(current => Math.min(totalPages - 1, current + 1))}
            disabled={safePage >= totalPages - 1}
            className="inline-flex items-center gap-0.5 rounded-md border border-border bg-background px-2 py-1 disabled:opacity-30 hover:bg-muted hover:text-foreground transition-colors"
            aria-label="Next page"
          >
            Next
            <ChevronRight className="h-3.5 w-3.5" />
          </button>
        </div>
      )}
    </div>
  )
}

function RunsWidget({
  widget,
  runsData,
  loading,
  error,
}: {
  widget: ReportWidget
  runsData: RunFoldersResponse | null
  loading: boolean
  error: string | null
}) {
  const [summaryRef, isCompact] = useCompactWidgetLayout()
  const view = widget.runsView ?? 'summary'
  const now = Date.now()

  const selectedRuns = useMemo(() => {
    const runs = (runsData?.folders ?? [])
      .filter(entry => {
        if (!widget.group) return true
        return entry.name === widget.group || entry.name.endsWith(`/${widget.group}`)
      })
      .sort((a, b) => {
        const aTime = parseTimestamp(a.metadata?.created_at) ?? 0
        const bTime = parseTimestamp(b.metadata?.created_at) ?? 0
        return bTime - aTime || a.name.localeCompare(b.name)
      })

    if (!widget.runFolder) return runs
    if (widget.runFolder === 'latest') return runs.length > 0 ? [runs[0]] : []
    return runs.filter(entry => entry.name === widget.runFolder)
  }, [runsData, widget.group, widget.runFolder])

  const normalisedRuns = useMemo(() => {
    return selectedRuns.map(run => {
      const startedAt = run.metadata?.started_at ?? run.metadata?.created_at ?? ''
      const completedAt = run.metadata?.completed_at ?? ''
      const startedMs = parseTimestamp(startedAt)
      const completedMs = parseTimestamp(completedAt)
      const explicitDurationMs = run.metadata?.duration_ms
      const durationMs = explicitDurationMs != null
        ? Math.max(0, explicitDurationMs)
        : startedMs == null
          ? 0
          : Math.max(0, (completedMs ?? now) - startedMs)
      return {
        run,
        run_folder: run.name,
        status: run.metadata?.status ?? 'unknown',
        triggered_by: run.metadata?.triggered_by ?? '',
        created_at: startedAt,
        completed_at: completedAt,
        duration_ms: durationMs,
        duration_minutes: durationMs / 60000,
        duration_label: formatRuntimeDuration(durationMs),
      }
    })
  }, [now, selectedRuns])

  const summary = useMemo(() => {
    if (normalisedRuns.length === 0) return null
    const completed = normalisedRuns.filter(run => run.status === 'completed').length
    const running = normalisedRuns.filter(run => run.status === 'running').length
    const failed = normalisedRuns.filter(run => run.status === 'failed').length
    const totalDurationMs = normalisedRuns.reduce((sum, run) => sum + run.duration_ms, 0)
    return {
      totalRuns: normalisedRuns.length,
      completed,
      running,
      failed,
      totalDurationMs,
      averageDurationMs: totalDurationMs / normalisedRuns.length,
      latestRun: normalisedRuns[0] ?? null,
    }
  }, [normalisedRuns])

  const summaryCards = useMemo(() => {
    if (!summary) return []
    const latestRunAt =
      summary.latestRun?.created_at ||
      summary.latestRun?.completed_at ||
      null
    return [
      { label: 'Last Run', value: formatNamed(latestRunAt, 'datetime').text },
    ]
  }, [summary])

  const durationChartRows = useMemo(() => {
    return normalisedRuns.map(run => ({
      label: run.run_folder.split('/').slice(-2).join(' / ') || run.run_folder,
      value: Number(run.duration_minutes.toFixed(2)),
    }))
  }, [normalisedRuns])

  const statusChartRows = useMemo(() => {
    if (!summary) return []
    return [
      { label: 'Completed', value: summary.completed },
      { label: 'Running', value: summary.running },
      { label: 'Failed', value: summary.failed },
    ].filter(row => row.value > 0)
  }, [summary])

  const runRows = useMemo(() => {
    return normalisedRuns.map(run => ({
      run_folder: run.run_folder,
      status: run.status,
      triggered_by: run.triggered_by,
      created_at: run.created_at,
      completed_at: run.completed_at,
      duration: run.duration_label,
      duration_minutes: Number(run.duration_minutes.toFixed(2)),
    }))
  }, [normalisedRuns])

  if (loading) {
    return (
      <div className="flex flex-col gap-3">
        <WidgetHeader widget={widget} />
        <div className="rounded-xl border border-border/60 bg-background/70 px-3 py-3 text-sm text-muted-foreground">
          Loading workflow runs…
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex flex-col gap-3">
        <WidgetHeader widget={widget} />
        <WidgetError
          widget={widget}
          message={error}
          hint="Runs widgets read workspace run-folder metadata from the workflow API."
          showWidgetMeta={false}
        />
      </div>
    )
  }

  if (!summary || normalisedRuns.length === 0) {
    return (
      <div className="flex flex-col gap-3">
        <WidgetHeader widget={widget} />
        <WidgetError
          widget={widget}
          message="No workflow runs are available for the selected scope."
          hint="Run the workflow first, or remove the run/group filter so the widget can see existing runs."
          severity="info"
          showWidgetMeta={false}
        />
      </div>
    )
  }

  return (
    <div ref={summaryRef} className="flex flex-col gap-3">
      <WidgetHeader widget={widget} />
      {view === 'summary' && (
        <>
          <div className={`grid gap-2.5 ${isCompact ? 'grid-cols-1' : 'grid-cols-1'}`}>
            {summaryCards.map(card => (
              <div key={card.label} className={isCompact ? 'rounded-xl bg-card/70 px-3 py-2.5' : 'rounded-2xl border border-border/60 bg-gradient-to-br from-card via-card to-muted/20 px-3 py-2.5 shadow-sm'}>
                <div className="text-[10px] font-semibold uppercase tracking-[0.18em] text-muted-foreground">{card.label}</div>
                <div className="mt-1 text-xl font-semibold tracking-tight text-foreground">{card.value}</div>
              </div>
            ))}
          </div>
        </>
      )}
      {view === 'duration-chart' && (
        <ChartWidget
          value={durationChartRows}
          widget={makeSyntheticChartWidget({
            chartType: 'bar',
            xAxis: 'label',
            yAxis: 'value',
          })}
        />
      )}
      {view === 'status-chart' && (
        <ChartWidget
          value={statusChartRows}
          widget={makeSyntheticChartWidget({
            chartType: 'bar',
            xAxis: 'label',
            yAxis: 'value',
          })}
        />
      )}
      {view === 'table' && (
        <TableWidget
          value={runRows}
          widget={makeSyntheticTableWidget({
            formats: {
              created_at: 'datetime',
              completed_at: 'datetime',
              duration_minutes: 'number-2dp',
            },
            defaultSort: { field: 'created_at', direction: 'desc' },
            colorBy: 'status',
            colorMap: {
              completed: '#10b981',
              running: '#3b82f6',
              failed: '#ef4444',
              unknown: '#6b7280',
            },
            hideColumns: ['duration_minutes'],
          })}
        />
      )}
      {view !== 'summary' && view !== 'duration-chart' && view !== 'status-chart' && view !== 'table' && (
        <WidgetError widget={widget} message={`Unsupported runs view "${view}".`} severity="info" showWidgetMeta={false} />
      )}
    </div>
  )
}

// Adds a soft card around table/chart/pivot widgets. Hover lifts the shadow
// slightly so the dashboard feels interactive even when the content is static.
function WidgetShell({
  widget,
  children,
  onToggleHidden,
}: {
  widget: ReportWidget
  children: React.ReactNode
  onToggleHidden?: () => void
}) {
  if (widget.kind === 'stat' || widget.kind === 'alert') return <>{children}</>
  const shellClassName =
    widget.kind === 'text'
      ? 'group relative px-0 py-0 transition-all duration-200 sm:rounded-xl sm:border sm:border-border/60 sm:bg-background/85 sm:px-3 sm:py-3 sm:shadow-sm sm:hover:border-border sm:hover:shadow-md'
      : 'group relative px-0 py-0 transition-all duration-200 sm:overflow-hidden sm:rounded-xl sm:border sm:border-border/60 sm:bg-gradient-to-b sm:from-card sm:to-muted/15 sm:px-3 sm:py-3 sm:shadow-sm sm:hover:border-border sm:hover:shadow-md'
  return (
    <div className={shellClassName}>
      {onToggleHidden && <WidgetVisibilityButton onToggle={onToggleHidden} />}
      <span className="absolute inset-x-0 top-0 hidden h-[2px] bg-gradient-to-r from-primary/0 via-primary/60 to-primary/0 opacity-60 transition-opacity group-hover:opacity-100 sm:block" aria-hidden />
      {children}
    </div>
  )
}

// Inline per-widget diagnostic — surfaces silent-failure cases (unresolved path,
// empty filter result) so the builder doesn't see a mystery blank space.
function WidgetError({
  widget,
  message,
  hint,
  severity = 'error',
  showWidgetMeta = true,
}: {
  widget: ReportWidget
  message: string
  hint?: string
  severity?: 'error' | 'info'
  showWidgetMeta?: boolean
}) {
  const tone = severity === 'error'
    ? 'border-destructive/30 bg-destructive/5 text-destructive'
    : 'border-border/70 bg-muted/30 text-muted-foreground'
  return (
    <div className={`rounded-xl border px-2.5 py-2 text-xs ${tone}`}>
      {showWidgetMeta && (
        <div className="flex items-center gap-2">
          {widget.title && <span className="font-semibold">{widget.title}</span>}
          <span className="opacity-70">({widget.kind})</span>
        </div>
      )}
      <div className="mt-0.5">{message}</div>
      {hint && <div className="mt-0.5 opacity-75">{hint}</div>}
    </div>
  )
}

function TextWidget({ value, widget }: { value: unknown; widget: ReportWidget }) {
  const text =
    typeof value === 'string'
      ? value
      : typeof value === 'number' || typeof value === 'boolean'
        ? String(value)
        : JSON.stringify(value, null, 2)
  return (
    <div className="flex flex-col gap-2.5">
      <WidgetHeader widget={widget} />
      <div className="whitespace-pre-wrap rounded-lg bg-muted/25 px-2.5 py-2.5 text-sm leading-7 text-foreground sm:px-3 sm:text-[15px]">
        {text}
      </div>
    </div>
  )
}

function MarkdownWidget({ value, widget }: { value: unknown; widget: ReportWidget }) {
  const markdown =
    typeof value === 'string'
      ? value
      : typeof value === 'number' || typeof value === 'boolean'
        ? String(value)
        : `\`\`\`json\n${JSON.stringify(value, null, 2)}\n\`\`\``
  return (
    <div className="flex flex-col gap-2.5">
      <WidgetHeader widget={widget} />
      <div className="rounded-lg bg-muted/20 px-3 py-3 text-sm text-foreground">
        <MarkdownRenderer content={markdown} className="max-w-none" maxHeight="none" disablePathLinking />
      </div>
    </div>
  )
}

function CardsWidget({ value, widget }: { value: unknown; widget: ReportWidget }) {
  const { theme } = useTheme()
  const [widgetRef, isCompact] = useCompactWidgetLayout()
  const formats = widget.formats ?? {}
  const enableSearch = widget.enableSearch !== false
  const pageSize = widget.pageSize ?? DEFAULT_TABLE_PAGE_SIZE
  const hidden = useMemo(() => new Set(widget.hideColumns ?? []), [widget.hideColumns])
  const palette = useMemo(() => resolvePalette(widget, theme), [widget, theme])
  const [sortField, setSortField] = useState<string | null>(widget.defaultSort?.field ?? null)
  const [sortDir, setSortDir] = useState<SortDirection>(widget.defaultSort?.direction ?? 'asc')
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(0)

  const rows = useMemo(() => {
    if (Array.isArray(value)) return value.filter((row): row is Record<string, unknown> => Boolean(row) && typeof row === 'object')
    if (value && typeof value === 'object') return [value as Record<string, unknown>]
    return []
  }, [value])

  const columns = useMemo(() => collectVisibleColumns(rows, hidden), [rows, hidden])
  const numericColumns = useMemo(() => detectNumericColumns(rows, columns), [rows, columns])
  const titleField = useMemo(
    () => inferPrimaryColumn(columns, numericColumns, widget.cardTitleField),
    [columns, numericColumns, widget.cardTitleField],
  )
  const subtitleField = useMemo(
    () => inferSecondaryColumn(columns, numericColumns, titleField, widget.cardSubtitleField),
    [columns, numericColumns, titleField, widget.cardSubtitleField],
  )
  const descriptionField = useMemo(() => {
    if (widget.cardDescriptionField && columns.includes(widget.cardDescriptionField)) return widget.cardDescriptionField
    const candidates = ['job_text', 'description', 'text', 'content', 'body', 'summary', 'notes']
    return candidates.find(candidate => columns.includes(candidate)) ?? null
  }, [columns, widget.cardDescriptionField])
  const linkField = useMemo(() => {
    if (widget.cardLinkField && columns.includes(widget.cardLinkField)) return widget.cardLinkField
    const candidates = ['job_url', 'url', 'link', 'href']
    return candidates.find(candidate => columns.includes(candidate)) ?? null
  }, [columns, widget.cardLinkField])
  const imageField = useMemo(() => {
    if (widget.cardImageField && columns.includes(widget.cardImageField)) return widget.cardImageField
    const candidates = ['image_url', 'thumbnail_url', 'avatar_url', 'logo_url']
    return candidates.find(candidate => columns.includes(candidate)) ?? null
  }, [columns, widget.cardImageField])

  const detailColumns = useMemo(() => {
    const excluded = new Set([titleField, subtitleField, descriptionField, linkField, imageField].filter(Boolean))
    const baseColumns = widget.fields && widget.fields.length > 0
      ? widget.fields.filter(field => columns.includes(field) && !excluded.has(field))
      : columns.filter(field => !excluded.has(field))
    return baseColumns
  }, [columns, descriptionField, imageField, linkField, subtitleField, titleField, widget.fields])

  const distinctIndex = useMemo(() => {
    if (!widget.colorBy) return {}
    const out: Record<string, number> = {}
    let next = 0
    for (const row of rows) {
      const rawValue: unknown = row?.[widget.colorBy]
      if (rawValue === undefined || rawValue === null) continue
      const key = String(rawValue)
      if (!(key in out)) out[key] = next++
    }
    return out
  }, [rows, widget.colorBy])

  const filtered = useMemo(() => {
    const needle = search.trim().toLowerCase()
    if (!needle) return rows
    return rows.filter(row => columns.some(col => stringifyTableValue(row[col]).toLowerCase().includes(needle)))
  }, [rows, columns, search])

  const sorted = useMemo(() => {
    if (!sortField) return filtered
    return [...filtered].sort((a, b) => {
      const result = compareValues(a?.[sortField], b?.[sortField])
      return sortDir === 'asc' ? result : -result
    })
  }, [filtered, sortDir, sortField])

  const totalRows = sorted.length
  const totalPages = Math.max(1, Math.ceil(totalRows / pageSize))
  const safePage = Math.min(page, totalPages - 1)
  const pageRows = sorted.slice(safePage * pageSize, safePage * pageSize + pageSize)
  const rowCountText = `${totalRows} record${totalRows === 1 ? '' : 's'}`

  useEffect(() => {
    if (page > totalPages - 1) setPage(Math.max(0, totalPages - 1))
  }, [page, totalPages])

  const onSortClick = (field: string) => {
    if (sortField === field) {
      setSortDir(dir => (dir === 'asc' ? 'desc' : 'asc'))
      return
    }
    setSortField(field)
    setSortDir('asc')
  }

  if (rows.length === 0) return null

  return (
    <div ref={widgetRef} className="flex flex-col gap-3">
      <WidgetHeader widget={widget} />
      <div className={`flex flex-wrap items-center gap-2 ${isCompact ? 'justify-start' : 'justify-between'}`}>
        {enableSearch && (
          <div className={`relative ${isCompact ? 'w-full' : 'min-w-[220px] flex-1 max-w-sm'}`}>
            <Search className="pointer-events-none absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
            <input
              type="search"
              placeholder="Search…"
              value={search}
              onChange={e => {
                setSearch(e.target.value)
                setPage(0)
              }}
              className="w-full rounded-md border border-input bg-muted/30 py-1.5 pl-7 pr-2 text-xs focus:outline-none focus:ring-1 focus:ring-primary"
            />
          </div>
        )}
        <div className="inline-flex items-center rounded-full border border-border bg-background/80 px-2 py-1 text-xs text-muted-foreground">
          {rowCountText}
        </div>
      </div>

      <div className={`grid gap-3 ${isCompact ? 'grid-cols-1' : 'grid-cols-1 xl:grid-cols-2 2xl:grid-cols-3'}`}>
        {pageRows.map((row, index) => {
          const rowColor = resolveSemanticColor(widget, row, palette, distinctIndex[String(row?.[widget.colorBy ?? ''] ?? '')] ?? index)
          const rowStyle = rowColor ? { backgroundColor: toRowTint(rowColor) } : undefined
          const titleValue = titleField ? formatTableValue(row?.[titleField], formats[titleField] as ReportFormatterName | undefined) : null
          const subtitleValue = subtitleField ? formatTableValue(row?.[subtitleField], formats[subtitleField] as ReportFormatterName | undefined) : null
          const descriptionValue = descriptionField ? formatTableValue(row?.[descriptionField], formats[descriptionField] as ReportFormatterName | undefined) : null
          const linkValue = linkField ? formatTableValue(row?.[linkField], formats[linkField] as ReportFormatterName | undefined) : null
          const imageURL = imageField && typeof row?.[imageField] === 'string' && isURLString(row[imageField] as string)
            ? row[imageField] as string
            : null
          return (
            <div
              key={safePage * pageSize + index}
              className="overflow-hidden rounded-2xl border border-border/60 bg-card/90 shadow-sm"
              style={rowStyle}
            >
              {imageURL && (
                <img src={imageURL} alt={titleValue?.text || 'Card image'} className="h-40 w-full object-cover" />
              )}
              <div className="flex h-full flex-col gap-3 px-4 py-4">
                <div className="min-w-0">
                  <div className="break-words text-base font-semibold text-foreground">
                    {titleValue ? renderTableValueContent(titleValue) : 'Untitled record'}
                  </div>
                  {subtitleValue && subtitleValue.text !== titleValue?.text && (
                    <div className="mt-1 break-words text-sm text-muted-foreground">
                      {renderTableValueContent(subtitleValue)}
                    </div>
                  )}
                  {descriptionValue && (
                    <div className="mt-2 whitespace-pre-wrap break-words text-sm leading-6 text-foreground/90">
                      {renderTableValueContent(descriptionValue)}
                    </div>
                  )}
                </div>

                {detailColumns.length > 0 && (
                  <div className="space-y-2">
                    {detailColumns.map(field => {
                      const formatted = formatTableValue(row?.[field], formats[field] as ReportFormatterName | undefined)
                      const useBlockLayout = formatted.prefersBlock || formatted.rawText.length > 72
                      return (
                        <div key={field} className={`rounded-lg bg-background/45 px-3 py-2 ${useBlockLayout ? 'space-y-1.5' : 'flex items-start justify-between gap-3'}`}>
                          <div className="min-w-0 text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">
                            {field}
                          </div>
                          <div className={`min-w-0 break-words text-sm text-foreground ${useBlockLayout ? 'text-left whitespace-pre-wrap' : 'text-right'} ${formatted.isNumeric ? 'tabular-nums' : ''}`}>
                            {renderTableValueContent(formatted)}
                          </div>
                        </div>
                      )
                    })}
                  </div>
                )}

                {linkValue?.href && (
                  <div className="mt-auto">
                    <a
                      href={linkValue.href}
                      target="_blank"
                      rel="noreferrer"
                      className="inline-flex items-center rounded-full border border-border bg-background/80 px-3 py-1.5 text-xs font-medium text-foreground transition-colors hover:bg-muted"
                    >
                      Open
                    </a>
                  </div>
                )}
              </div>
            </div>
          )
        })}
      </div>

      {columns.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {columns.map(col => {
            const isSorted = sortField === col
            return (
              <button
                key={col}
                type="button"
                onClick={() => onSortClick(col)}
                className={`inline-flex items-center gap-1 rounded-full border px-2.5 py-1 text-xs transition-colors ${isSorted ? 'border-primary/40 bg-primary/10 text-primary' : 'border-border bg-background/70 text-muted-foreground hover:bg-muted hover:text-foreground'}`}
              >
                <span>{col}</span>
                {isSorted ? (sortDir === 'asc' ? <ArrowUp className="h-3 w-3" /> : <ArrowDown className="h-3 w-3" />) : null}
              </button>
            )
          })}
        </div>
      )}

      {totalPages > 1 && (
        <div className="flex flex-wrap items-center justify-end gap-1.5 text-xs text-muted-foreground">
          <button
            onClick={() => setPage(p => Math.max(0, p - 1))}
            disabled={safePage === 0}
            className="inline-flex items-center gap-0.5 rounded-md border border-border bg-background px-2 py-1 disabled:opacity-30 hover:bg-muted hover:text-foreground transition-colors"
            aria-label="Previous page"
          >
            <ChevronLeft className="h-3.5 w-3.5" />
            Prev
          </button>
          <span className="inline-flex items-center rounded-md bg-primary/10 px-2 py-1 font-medium text-primary tabular-nums">
            {safePage + 1}
            <span className="mx-1 opacity-60">/</span>
            {totalPages}
          </span>
          <button
            onClick={() => setPage(p => Math.min(totalPages - 1, p + 1))}
            disabled={safePage >= totalPages - 1}
            className="inline-flex items-center gap-0.5 rounded-md border border-border bg-background px-2 py-1 disabled:opacity-30 hover:bg-muted hover:text-foreground transition-colors"
            aria-label="Next page"
          >
            Next
            <ChevronRight className="h-3.5 w-3.5" />
          </button>
        </div>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Stat widget — KPI tile: big number with optional label, delta and sparkline.
// `path` resolves to a scalar. `delta_path` and `trend_path` resolve against
// the same source; delta is a signed number, trend is an array of numbers.
// ---------------------------------------------------------------------------
function StatWidget({
  source,
  resolution,
  widget,
  onToggleHidden,
}: {
  source: unknown
  resolution: SingularWidgetSourceResolution
  widget: ReportWidget
  onToggleHidden?: () => void
}) {
  if (resolution.status === 'no-match') {
    return (
      <StandaloneWidgetNotice onToggleHidden={onToggleHidden}>
        <WidgetError
          widget={widget}
          message={`No rows in ${widget.source}${widget.filter ? ` matching filter "${widget.filter}"` : ''}.`}
          hint="Stat widgets backed by array sources need one matching row."
          severity="info"
        />
      </StandaloneWidgetNotice>
    )
  }
  if (resolution.status === 'multi-match') {
    return (
      <StandaloneWidgetNotice onToggleHidden={onToggleHidden}>
        <WidgetError
          widget={widget}
          message={`Stat widget matched ${resolution.value.length} rows in ${widget.source}${widget.filter ? ` for filter "${widget.filter}"` : ''}.`}
          hint="Point the widget at a single row, or narrow the filter so exactly one record remains."
        />
      </StandaloneWidgetNotice>
    )
  }

  const scalarPath = normalizeSingularWidgetPath(widget.path)
  const rawValue = resolveJSONPath(source, scalarPath)
  if (rawValue === undefined) {
    return (
      <StandaloneWidgetNotice onToggleHidden={onToggleHidden}>
        <WidgetError
          widget={widget}
          message={`Path "${widget.path || '(root)'}" doesn't resolve in ${widget.source}.`}
          hint="Point `path:` at a scalar (number or string) the stat should display."
        />
      </StandaloneWidgetNotice>
    )
  }
  const formatted = widget.format ? formatNamed(rawValue, widget.format) : formatAuto(rawValue)
  const delta = widget.deltaPath != null ? resolveJSONPath(source, normalizeSingularWidgetPath(widget.deltaPath)) : undefined
  const deltaNum = typeof delta === 'number' ? delta : Number(delta)
  const deltaFormatted =
    Number.isFinite(deltaNum) && widget.deltaPath
      ? widget.deltaFormat
        ? formatNamed(deltaNum, widget.deltaFormat).text
        : formatAuto(deltaNum).text
      : undefined
  const trend = widget.trendPath != null ? resolveJSONPath(source, normalizeSingularWidgetPath(widget.trendPath)) : undefined
  const trendNumbers = Array.isArray(trend)
    ? (trend as unknown[]).map(v => Number(v)).filter(n => Number.isFinite(n))
    : []

  const direction = trendDirection(Number.isFinite(deltaNum) ? deltaNum : null)
  const deltaTone = trendTones[direction]
  const deltaArrow = trendArrow[direction]

  return (
    <div className="relative flex h-full flex-col gap-2 overflow-hidden rounded-xl bg-card/75 px-3 py-3 transition-shadow sm:border sm:border-border/60 sm:bg-gradient-to-br sm:from-card sm:via-card sm:to-muted/25 sm:shadow-sm sm:hover:shadow-md">
      {onToggleHidden && <WidgetVisibilityButton onToggle={onToggleHidden} />}
      <span className="absolute inset-x-0 top-0 hidden h-[2px] bg-gradient-to-r from-primary/0 via-primary/60 to-primary/0 sm:block" aria-hidden />
      <WidgetHeader widget={widget} mode="metric" />
      <div className="flex items-baseline gap-2">
        <div className="text-2xl font-semibold tabular-nums tracking-tight text-foreground sm:text-3xl">
          {widget.prefix ?? ''}{formatted.text}{widget.suffix ?? ''}
        </div>
        {deltaFormatted !== undefined && (
          <div className={`inline-flex items-center gap-1 text-[10px] font-semibold px-1.5 py-0.5 rounded-full ${deltaTone}`}>
            <span aria-hidden>{deltaArrow}</span>
            <span className="tabular-nums">{deltaFormatted}</span>
          </div>
        )}
      </div>
      {widget.label && <div className="text-xs text-muted-foreground">{widget.label}</div>}
      {trendNumbers.length >= 2 && <Sparkline points={trendNumbers} />}
    </div>
  )
}

// Inline sparkline SVG — minimal: a single path scaled to fit, 1px stroke.
// No axes, no points, no tooltip. Fits inside stat widgets + table cells.
function Sparkline({ points, width = 120, height = 28 }: { points: number[]; width?: number; height?: number }) {
  const stroke = 'hsl(var(--chart-1))'
  const gradId = useId().replace(/:/g, '') + '-spark'
  const min = Math.min(...points)
  const max = Math.max(...points)
  const span = max - min || 1
  const stepX = points.length > 1 ? width / (points.length - 1) : 0
  const coords = points.map((v, i) => {
    const x = i * stepX
    const y = height - ((v - min) / span) * height
    return [x, y] as const
  })
  const lineD = coords.map(([x, y], i) => `${i === 0 ? 'M' : 'L'}${x.toFixed(2)},${y.toFixed(2)}`).join(' ')
  const fillD = coords.length > 0
    ? `${lineD} L${(coords[coords.length - 1][0]).toFixed(2)},${height} L0,${height} Z`
    : ''
  return (
    <svg width={width} height={height} viewBox={`0 0 ${width} ${height}`} className="mt-1">
      <defs>
        <linearGradient id={gradId} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={stroke} stopOpacity={0.35} />
          <stop offset="100%" stopColor={stroke} stopOpacity={0} />
        </linearGradient>
      </defs>
      {fillD && <path d={fillD} fill={`url(#${gradId})`} stroke="none" />}
      <path d={lineD} fill="none" stroke={stroke} strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

// ---------------------------------------------------------------------------
// Alert widget — colored callout banner. Typically used with show_if so the
// widget only renders when the underlying condition is true ("unassigned > 0",
// "last_sync_days > 7", etc.). `path` is optional — if set, its resolved
// value is available via `{value}` interpolation in message/title.
// ---------------------------------------------------------------------------
function AlertWidget({
  source,
  resolution,
  widget,
  onToggleHidden,
}: {
  source: unknown
  resolution: SingularWidgetSourceResolution
  widget: ReportWidget
  onToggleHidden?: () => void
}) {
  if (resolution.status === 'no-match') return null
  if (resolution.status === 'multi-match') {
    return (
      <StandaloneWidgetNotice onToggleHidden={onToggleHidden}>
        <WidgetError
          widget={widget}
          message={`Alert widget matched ${resolution.value.length} rows in ${widget.source}${widget.filter ? ` for filter "${widget.filter}"` : ''}.`}
          hint="Alert widgets backed by array sources need a single row so `{value}` and `show_if` resolve consistently."
        />
      </StandaloneWidgetNotice>
    )
  }

  const severity: ReportAlertSeverity = widget.severity ?? 'info'
  const tone = severityTones[severity]
  const icon = severityIcons[severity]
  const resolvedValue = widget.path ? resolveJSONPath(source, normalizeSingularWidgetPath(widget.path)) : undefined
  const valueText =
    resolvedValue === undefined || resolvedValue === null
      ? ''
      : widget.format
        ? formatNamed(resolvedValue, widget.format).text
        : formatAuto(resolvedValue).text
  const interpolate = (s: string | undefined): string | undefined =>
    s == null ? s : s.replace(/\{value\}/g, valueText)
  const title = interpolate(widget.title)
  const message = interpolate(widget.message)
  return (
    <div className={`relative flex items-start gap-3 rounded-xl px-3 py-2.5 sm:border sm:shadow-sm ${tone}`}>
      {onToggleHidden && <WidgetVisibilityButton onToggle={onToggleHidden} />}
      <div className="mt-0.5 flex h-7 w-7 items-center justify-center rounded-full border border-current/15 bg-background/60 text-sm leading-5" aria-hidden>{icon}</div>
      <div className="flex flex-col gap-0.5">
        {title && <div className="text-sm font-semibold">{title}</div>}
        {message && <div className="text-sm leading-6">{message}</div>}
        {!title && !message && (
          <div className="text-sm leading-6">
            {valueText || <span className="italic text-muted-foreground">(no message)</span>}
          </div>
        )}
      </div>
    </div>
  )
}

type SortDirection = 'asc' | 'desc'

function TableWidget({ value, widget }: { value: unknown; widget: ReportWidget }) {
  const { theme } = useTheme()
  const [widgetRef, isCompact] = useCompactWidgetLayout()
  const formats = widget.formats ?? {}
  const enableSearch = widget.enableSearch !== false // default true
  const pageSize = widget.pageSize ?? DEFAULT_TABLE_PAGE_SIZE
  const hidden = useMemo(() => new Set(widget.hideColumns ?? []), [widget.hideColumns])
  const palette = useMemo(() => resolvePalette(widget, theme), [widget, theme])
  // Distinct-value → palette-index map so unmapped colorBy values get stable colors.
  const distinctIndex = useMemo(() => {
    if (!widget.colorBy || !Array.isArray(value)) return {}
    const out: Record<string, number> = {}
    let next = 0
    for (const row of value as Record<string, unknown>[]) {
      const raw: unknown = row?.[widget.colorBy]
      if (raw === undefined || raw === null) continue
      const key = String(raw)
      if (!(key in out)) out[key] = next++
    }
    return out
  }, [value, widget.colorBy])

  const [sortField, setSortField] = useState<string | null>(widget.defaultSort?.field ?? null)
  const [sortDir, setSortDir] = useState<SortDirection>(widget.defaultSort?.direction ?? 'asc')
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(0)

  const columns = useMemo(() => {
    if (!Array.isArray(value) || value.length === 0) return []
    const rows = value as Record<string, unknown>[]
    const cols: string[] = []
    const seen = new Set<string>()
    for (const row of rows) {
      if (!row || typeof row !== 'object') continue
      for (const k of Object.keys(row)) {
        if (!seen.has(k) && !hidden.has(k)) {
          seen.add(k)
          cols.push(k)
        }
      }
    }
    return cols
  }, [value, hidden])

  // Auto-detect numeric columns (used for right-alignment when no formatter declared).
  const numericColumns = useMemo(() => {
    const out = new Set<string>()
    if (!Array.isArray(value)) return out
    const rows = value as Record<string, unknown>[]
    for (const col of columns) {
      // Treat a column as numeric when at least one non-null value parses as a finite number
      // AND no value parses as a non-empty non-numeric string.
      let sawNumeric = false
      let sawNonNumeric = false
      for (const row of rows) {
        const v = row?.[col]
        if (v == null || v === '') continue
        if (typeof v === 'number' && Number.isFinite(v)) {
          sawNumeric = true
        } else if (typeof v === 'string' && v.trim() !== '' && Number.isFinite(Number(v))) {
          sawNumeric = true
        } else {
          sawNonNumeric = true
          break
        }
      }
      if (sawNumeric && !sawNonNumeric) out.add(col)
    }
    return out
  }, [value, columns])

  const compactPrimaryColumn = useMemo(() => {
    const nonNumericColumns = columns.filter(col => !numericColumns.has(col))
    const preferred = COMPACT_PRIMARY_COLUMN_CANDIDATES.find(candidate => nonNumericColumns.includes(candidate))
    if (preferred) return preferred
    return nonNumericColumns.find(col => !COMPACT_DEPRIORITIZED_COLUMNS.has(col)) ?? nonNumericColumns[0] ?? columns[0] ?? null
  }, [columns, numericColumns])

  const compactSecondaryColumn = useMemo(() => {
    const remainingColumns = columns.filter(col => col !== compactPrimaryColumn && !numericColumns.has(col))
    const preferred = COMPACT_SECONDARY_COLUMN_CANDIDATES.find(candidate => remainingColumns.includes(candidate))
    if (preferred) return preferred
    return remainingColumns.find(col => !COMPACT_DEPRIORITIZED_COLUMNS.has(col)) ?? remainingColumns[0] ?? null
  }, [columns, compactPrimaryColumn, numericColumns])

  const compactDetailColumns = useMemo(() => {
    return columns.filter(col => col !== compactPrimaryColumn && col !== compactSecondaryColumn)
  }, [columns, compactPrimaryColumn, compactSecondaryColumn])

  const filtered = useMemo(() => {
    if (!Array.isArray(value)) return []
    const rows = value as Record<string, unknown>[]
    const needle = search.trim().toLowerCase()
    if (!needle) return rows
    return rows.filter(row => {
      if (!row) return false
      for (const col of columns) {
        const v = row[col]
        if (v == null) continue
        const s = typeof v === 'object' ? JSON.stringify(v) : String(v)
        if (s.toLowerCase().includes(needle)) return true
      }
      return false
    })
  }, [value, columns, search])

  const sorted = useMemo(() => {
    if (!sortField) return filtered
    const copy = [...filtered]
    copy.sort((a, b) => {
      const cmp = compareValues(a?.[sortField], b?.[sortField])
      return sortDir === 'asc' ? cmp : -cmp
    })
    return copy
  }, [filtered, sortField, sortDir])

  const totalPages = Math.max(1, Math.ceil(sorted.length / pageSize))
  const safePage = Math.min(page, totalPages - 1)
  const pageRows = useMemo(
    () => sorted.slice(safePage * pageSize, (safePage + 1) * pageSize),
    [sorted, safePage, pageSize],
  )

  if (!Array.isArray(value) || value.length === 0 || columns.length === 0) return null
  const rowCountText =
    sorted.length === (value as unknown[]).length
      ? `${sorted.length} row${sorted.length === 1 ? '' : 's'}`
      : `${sorted.length} of ${(value as unknown[]).length}`

  const onSortClick = (col: string) => {
    if (sortField === col) {
      setSortDir(prev => (prev === 'asc' ? 'desc' : 'asc'))
    } else {
      setSortField(col)
      setSortDir('asc')
    }
    setPage(0)
  }

  const handleExport = () => {
    const csv = rowsToCSV(sorted as Record<string, unknown>[], columns)
    const blob = new Blob([csv], { type: 'text/csv;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `report-table-${new Date().toISOString().replace(/[:.]/g, '-')}.csv`
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
  }

  return (
    <div ref={widgetRef} className="flex flex-col gap-2.5">
      <WidgetHeader widget={widget} />
      <div className="flex flex-col gap-1.5 text-xs sm:flex-row sm:flex-wrap sm:items-center">
        {enableSearch && (
          <div className="relative w-full sm:max-w-xs sm:flex-1">
            <Search className="absolute left-2 top-1.5 w-3.5 h-3.5 text-muted-foreground" />
            <input
              type="text"
              placeholder="Search…"
              value={search}
              onChange={e => {
                setSearch(e.target.value)
                setPage(0)
              }}
              className="w-full rounded-md border border-input bg-muted/30 py-1.5 pl-7 pr-2 text-xs focus:outline-none focus:ring-1 focus:ring-primary"
            />
          </div>
        )}
        <div className="inline-flex items-center self-start rounded-full border border-border bg-background/80 px-2 py-1 text-muted-foreground sm:self-auto">{rowCountText}</div>
        <button
          onClick={handleExport}
          className="inline-flex w-full items-center justify-center gap-1 rounded-md border border-border bg-background/80 px-2 py-1.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground sm:ml-auto sm:w-auto sm:justify-start"
          title="Export CSV"
        >
          <Download className="w-3.5 h-3.5" />
          <span>CSV</span>
        </button>
      </div>

      {isCompact ? (
        <div className="flex flex-col gap-2">
          {pageRows.map((row, i) => {
            const rowColor = resolveSemanticColor(widget, row, palette, distinctIndex[String(row?.[widget.colorBy ?? ''] ?? '')] ?? i)
            const rowStyle = rowColor ? { backgroundColor: toRowTint(rowColor) } : undefined
            const primaryValue = compactPrimaryColumn ? row?.[compactPrimaryColumn] : undefined
            const primaryText = compactPrimaryColumn
              ? formatTableValue(primaryValue, formats[compactPrimaryColumn] as ReportFormatterName | undefined)
              : null
            const secondaryValue = compactSecondaryColumn ? row?.[compactSecondaryColumn] : undefined
            const secondaryText = compactSecondaryColumn
              ? formatTableValue(secondaryValue, formats[compactSecondaryColumn] as ReportFormatterName | undefined)
              : null
            return (
              <div
                key={safePage * pageSize + i}
                className="rounded-xl border border-border/55 bg-card/90 px-3 py-3 shadow-sm"
                style={rowStyle}
              >
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="text-[10px] font-semibold uppercase tracking-[0.16em] text-muted-foreground">
                      Row {safePage * pageSize + i + 1}
                    </div>
                    <div className="break-words text-sm font-semibold text-foreground">
                      {primaryText ? renderTableValueContent(primaryText) : 'Untitled row'}
                    </div>
                    {compactSecondaryColumn && secondaryText && secondaryText.text !== primaryText?.text && (
                      <div className="break-words text-xs text-muted-foreground">
                        {compactSecondaryColumn}: {renderTableValueContent(secondaryText)}
                      </div>
                    )}
                  </div>
                </div>
                <div className="mt-2">
                  {compactDetailColumns.length === 0 && (
                    <div className="rounded-lg bg-background/45 px-2.5 py-2 text-sm text-muted-foreground">
                      Compact summary view
                    </div>
                  )}
                  {compactDetailColumns.length > 0 && (
                    <div className="divide-y divide-border/35 overflow-hidden rounded-lg bg-background/40">
                      {compactDetailColumns.map(c => {
                        const preset = formats[c] as ReportFormatterName | undefined
                        const formatted = formatTableValue(row?.[c], preset)
                        const useBlockLayout = formatted.prefersBlock || formatted.rawText.length > 72
                        return (
                          <div key={c} className={`px-2.5 py-2 ${useBlockLayout ? 'space-y-1.5' : 'flex items-start justify-between gap-3'}`}>
                            <div className="min-w-0 text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">
                              {c}
                            </div>
                            <div className={`min-w-0 break-words text-sm text-foreground ${useBlockLayout ? 'text-left whitespace-pre-wrap' : 'text-right'} ${formatted.isNumeric ? 'tabular-nums' : ''}`}>
                              {renderTableValueContent(formatted)}
                            </div>
                          </div>
                        )
                      })}
                    </div>
                  )}
                </div>
              </div>
            )
          })}
        </div>
      ) : (
        <div className="overflow-x-auto rounded-xl border border-border/60 bg-background/75 shadow-[inset_0_1px_0_rgba(255,255,255,0.03)] [scrollbar-gutter:stable]">
          <table className="w-full border-collapse text-sm">
            <thead className="sticky top-0 bg-muted/60 backdrop-blur-sm z-10 shadow-[0_1px_0_0_var(--border)]">
              <tr>
                {columns.map(c => {
                  const isSorted = sortField === c
                  const align = numericColumns.has(c) || c in formats ? 'text-right' : 'text-left'
                  return (
                    <th
                      key={c}
                      className={`px-2.5 py-2 border-b-2 border-border font-semibold text-xs uppercase tracking-wide text-muted-foreground select-none cursor-pointer hover:bg-muted hover:text-foreground transition-colors ${align} ${isSorted ? 'text-primary' : ''}`}
                      onClick={() => onSortClick(c)}
                    >
                      <span className="inline-flex items-center gap-1">
                        <span>{c}</span>
                        {isSorted ? (
                          sortDir === 'asc' ? (
                            <ArrowUp className="w-3 h-3" />
                          ) : (
                            <ArrowDown className="w-3 h-3" />
                          )
                        ) : (
                          <ArrowUpDown className="w-3 h-3 opacity-30" />
                        )}
                      </span>
                    </th>
                  )
                })}
              </tr>
            </thead>
            <tbody>
              {pageRows.map((row, i) => {
                const rowColor = resolveSemanticColor(widget, row, palette, distinctIndex[String(row?.[widget.colorBy ?? ''] ?? '')] ?? i)
                const rowStyle = rowColor ? { backgroundColor: toRowTint(rowColor) } : undefined
                return (
                <tr
                  key={safePage * pageSize + i}
                  className={`group/row transition-colors ${rowColor ? 'hover:bg-muted/40' : 'even:bg-muted/20 hover:bg-muted/40'} hover:shadow-[inset_2px_0_0_0_hsl(var(--primary))]`}
                  style={rowStyle}
                >
                  {columns.map(c => {
                    const preset = formats[c] as ReportFormatterName | undefined
                    const formatted = formatTableValue(row?.[c], preset)
                    const align = formatted.isNumeric ? 'text-right tabular-nums' : 'text-left'
                    return (
                      <td
                        key={c}
                        className={`px-2.5 py-1.5 border-b border-border/40 align-top text-foreground break-words whitespace-pre-wrap ${align}`}
                      >
                        {renderTableValueContent(formatted)}
                      </td>
                    )
                  })}
                </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}

      {totalPages > 1 && (
        <div className="flex flex-wrap items-center justify-end gap-1.5 text-xs text-muted-foreground">
          <button
            onClick={() => setPage(p => Math.max(0, p - 1))}
            disabled={safePage === 0}
            className="inline-flex items-center gap-0.5 pl-1 pr-2 py-1 rounded-md border border-border bg-background disabled:opacity-30 hover:bg-muted hover:text-foreground transition-colors"
            aria-label="Previous page"
          >
            <ChevronLeft className="w-3.5 h-3.5" />
            Prev
          </button>
          <span className="inline-flex items-center px-2 py-1 rounded-md bg-primary/10 text-primary font-medium tabular-nums">
            {safePage + 1}
            <span className="opacity-60 mx-1">/</span>
            {totalPages}
          </span>
          <button
            onClick={() => setPage(p => Math.min(totalPages - 1, p + 1))}
            disabled={safePage >= totalPages - 1}
            className="inline-flex items-center gap-0.5 pl-2 pr-1 py-1 rounded-md border border-border bg-background disabled:opacity-30 hover:bg-muted hover:text-foreground transition-colors"
            aria-label="Next page"
          >
            Next
            <ChevronRight className="w-3.5 h-3.5" />
          </button>
        </div>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Pivot widget — rows × columns × aggregate(values). Reads an array of records
// (source + path + optional filter), buckets by rowsField/columnsField, and
// applies the aggregator to valuesField for each cell.
// ---------------------------------------------------------------------------
function PivotWidget({ source, widget }: { source: unknown; widget: ReportWidget }) {
  const { theme } = useTheme()
  const [widgetRef, isCompact] = useCompactWidgetLayout()
  const resolvedRaw = resolveJSONPath(source, widget.path)
  const resolved = applyWidgetFilter(resolvedRaw, widget.filter)

  if (!widget.rowsField || !widget.columnsField || !widget.valuesField) {
    return (
      <WidgetError
        widget={widget}
        message="pivot requires rows, columns, and values fields."
        hint="Set `rows: <field>`, `columns: <field>`, `values: <field>`; optionally `aggregate: sum|avg|count|min|max|first`."
      />
    )
  }
  if (!Array.isArray(resolved) || resolved.length === 0) {
    return (
      <WidgetError
        widget={widget}
        message={`pivot source resolves to ${resolvedRaw == null ? 'nothing' : 'an empty or non-array value'}.`}
        hint="Point `path:` at a non-empty array of objects."
        severity="info"
      />
    )
  }

  const rowsField = widget.rowsField
  const columnsField = widget.columnsField
  const valuesField = widget.valuesField
  const aggregate = widget.aggregate ?? 'sum'

  // Build the cell grid — Map<rowKey, Map<colKey, aggregated>> with column-order
  // and row-order tracked in insertion-order arrays so rendering is stable.
  const rowKeys: string[] = []
  const colKeys: string[] = []
  const seenRow = new Set<string>()
  const seenCol = new Set<string>()
  const buckets = new Map<string, Map<string, number[]>>() // rowKey -> colKey -> raw numeric values
  const stringBuckets = new Map<string, Map<string, unknown>>() // for 'first' aggregate
  for (const row of resolved as Record<string, unknown>[]) {
    if (!row || typeof row !== 'object') continue
    const rk = resolveJSONPath(row, rowsField)
    const ck = resolveJSONPath(row, columnsField)
    if (rk == null || ck == null) continue
    const rKey = String(rk)
    const cKey = String(ck)
    if (!seenRow.has(rKey)) { seenRow.add(rKey); rowKeys.push(rKey) }
    if (!seenCol.has(cKey)) { seenCol.add(cKey); colKeys.push(cKey) }
    if (!buckets.has(rKey)) buckets.set(rKey, new Map())
    const inner = buckets.get(rKey)!
    if (!inner.has(cKey)) inner.set(cKey, [])
    const v = resolveJSONPath(row, valuesField)
    if (aggregate === 'first') {
      if (!stringBuckets.has(rKey)) stringBuckets.set(rKey, new Map())
      const sInner = stringBuckets.get(rKey)!
      if (!sInner.has(cKey)) sInner.set(cKey, v)
    } else if (aggregate === 'count') {
      inner.get(cKey)!.push(1)
    } else {
      const num = typeof v === 'number' ? v : Number(v)
      if (Number.isFinite(num)) inner.get(cKey)!.push(num)
    }
  }

  // Reduce each cell's array via the aggregator.
  const cell = (rKey: string, cKey: string): { raw: unknown; num: number | undefined } => {
    if (aggregate === 'first') {
      const raw = stringBuckets.get(rKey)?.get(cKey)
      const num = typeof raw === 'number' ? raw : Number(raw)
      return { raw, num: Number.isFinite(num) ? num : undefined }
    }
    const vals = buckets.get(rKey)?.get(cKey) ?? []
    if (vals.length === 0) return { raw: undefined, num: undefined }
    let num: number
    switch (aggregate) {
      case 'avg': num = vals.reduce((a, b) => a + b, 0) / vals.length; break
      case 'min': num = Math.min(...vals); break
      case 'max': num = Math.max(...vals); break
      case 'count': num = vals.length; break
      case 'sum':
      default: num = vals.reduce((a, b) => a + b, 0); break
    }
    return { raw: num, num }
  }

  const allNums: number[] = []
  for (const rk of rowKeys) for (const ck of colKeys) {
    const { num } = cell(rk, ck)
    if (num !== undefined) allNums.push(num)
  }
  const min = allNums.length > 0 ? Math.min(...allNums) : 0
  const max = allNums.length > 0 ? Math.max(...allNums) : 0
  const span = max - min || 1

  const heatmap = widget.heatmap === true
  const [heatLow, heatHigh] = widget.heatmapColors ?? (
    theme === 'dark' ? ['#1e293b', '#60a5fa'] : ['#eff6ff', '#2563eb']
  )

  const tintFor = (n: number | undefined): string | undefined => {
    if (!heatmap || n === undefined) return undefined
    const t = (n - min) / span
    return lerpHex(heatLow, heatHigh, Math.max(0, Math.min(1, t)))
  }

  return (
    <div ref={widgetRef} className="flex flex-col gap-3">
      <WidgetHeader widget={widget} />
      {isCompact ? (
        <div className="flex flex-col gap-2">
          {rowKeys.map(rk => (
            <div key={rk} className="rounded-xl border border-border/55 bg-card/90 px-3 py-3 shadow-sm">
              <div className="mb-2 text-[11px] font-semibold uppercase tracking-[0.16em] text-muted-foreground">
                {rowsField}: <span className="text-foreground">{rk}</span>
              </div>
              <div className="divide-y divide-border/35 overflow-hidden rounded-lg bg-background/40">
                {colKeys.map(ck => {
                  const { raw, num } = cell(rk, ck)
                  const text = raw == null
                    ? ''
                    : widget.format
                      ? formatNamed(raw, widget.format).text
                      : formatAuto(raw).text
                  const bg = tintFor(num)
                  return (
                    <div
                      key={ck}
                      className="flex items-start justify-between gap-3 px-2.5 py-2"
                      style={bg ? { backgroundColor: bg } : undefined}
                    >
                      <div className="text-[11px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">
                        {ck}
                      </div>
                      <div className="text-right text-sm tabular-nums text-foreground">
                        {text || '—'}
                      </div>
                    </div>
                  )
                })}
              </div>
            </div>
          ))}
        </div>
      ) : (
        <div className="overflow-x-auto rounded-xl border border-border/60 bg-background/75 shadow-[inset_0_1px_0_rgba(255,255,255,0.03)] [scrollbar-gutter:stable]">
          <table className="border-collapse text-sm">
            <thead className="sticky top-0 bg-muted/60 backdrop-blur-sm z-10 shadow-[0_1px_0_0_var(--border)]">
              <tr>
                <th className="px-2.5 py-2 border-b-2 border-border font-semibold text-xs uppercase tracking-wide text-left text-muted-foreground">
                  {rowsField} ╲ {columnsField}
                </th>
                {colKeys.map(ck => (
                  <th key={ck} className="px-2.5 py-2 border-b-2 border-border font-semibold text-xs uppercase tracking-wide text-right text-muted-foreground">
                    {ck}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rowKeys.map(rk => (
                <tr key={rk} className="hover:bg-muted/30 transition-colors">
                  <th className="px-2.5 py-1.5 border-b border-border/40 font-medium text-left text-foreground bg-muted/20 sticky left-0">
                    {rk}
                  </th>
                  {colKeys.map(ck => {
                    const { raw, num } = cell(rk, ck)
                    const text = raw == null
                      ? ''
                      : widget.format
                        ? formatNamed(raw, widget.format).text
                        : formatAuto(raw).text
                    const bg = tintFor(num)
                    return (
                      <td
                        key={ck}
                        className="px-2.5 py-1.5 border-b border-border/40 text-right tabular-nums text-foreground"
                        style={bg ? { backgroundColor: bg } : undefined}
                      >
                        {text}
                      </td>
                    )
                  })}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <div className="text-xs text-muted-foreground">
        {rowKeys.length} row{rowKeys.length === 1 ? '' : 's'} × {colKeys.length} col{colKeys.length === 1 ? '' : 's'} · aggregate: {aggregate}
      </div>
    </div>
  )
}

// Linear-interpolates between two hex colors (#rrggbb). Used by the pivot
// heatmap tint calculation. Gracefully falls back to the low color for bad
// inputs so a malformed `heatmap_colors` never crashes rendering.
function lerpHex(lo: string, hi: string, t: number): string {
  const parse = (c: string): [number, number, number] | null => {
    if (!c.startsWith('#')) return null
    const hex = c.length === 4
      ? `#${c[1]}${c[1]}${c[2]}${c[2]}${c[3]}${c[3]}`
      : c.length === 7 ? c : null
    if (!hex) return null
    return [
      parseInt(hex.slice(1, 3), 16),
      parseInt(hex.slice(3, 5), 16),
      parseInt(hex.slice(5, 7), 16),
    ]
  }
  const a = parse(lo)
  const b = parse(hi)
  if (!a || !b) return lo
  const mix = (x: number, y: number) => Math.round(x + (y - x) * t)
  const r = mix(a[0], b[0]).toString(16).padStart(2, '0')
  const g = mix(a[1], b[1]).toString(16).padStart(2, '0')
  const bl = mix(a[2], b[2]).toString(16).padStart(2, '0')
  return `#${r}${g}${bl}`
}

// Normalises chart data to {label, value, ...rest} pairs. When `xAxis`/`yAxis` are
// declared in the widget, those field names take precedence; otherwise we accept
// either the canonical {label,value} shape or fall back to the first two object keys.
//
// Multi-series mode: when `widget.series` is non-empty, every field in `series`
// becomes a numeric key on each point. `_value` is kept as the FIRST series'
// value so single-series code paths (sort, topN) continue to work intuitively.
function normaliseChartPoints(value: unknown, widget: ReportWidget): Array<Record<string, unknown> & { _label: string; _value: number }> {
  if (!Array.isArray(value) || value.length === 0) return []
  const xField = widget.xAxis
  const yField = widget.yAxis
  const series = widget.series && widget.series.length > 0 ? widget.series : null
  const out: Array<Record<string, unknown> & { _label: string; _value: number }> = []
  for (const item of value as Record<string, unknown>[]) {
    if (!item || typeof item !== 'object') continue
    let label: string | undefined
    let num: number | undefined
    const extra: Record<string, number> = {}
    if (series) {
      // Multi-series: require an explicit xField (or fall back to first key).
      const xKey = xField ?? Object.keys(item)[0]
      label = item[xKey] != null ? String(item[xKey]) : undefined
      for (const s of series) {
        const v = Number(item[s])
        if (Number.isFinite(v)) extra[s] = v
      }
      // `_value` mirrors the first series value so sort/topN still mean something.
      num = series.length > 0 ? extra[series[0]] : undefined
    } else if (xField && yField) {
      label = item[xField] != null ? String(item[xField]) : undefined
      num = Number(item[yField])
    } else if ('label' in item && 'value' in item) {
      label = String(item.label ?? '')
      num = Number(item.value)
    } else {
      const keys = Object.keys(item)
      if (keys.length < 2) continue
      label = String(item[keys[0]] ?? '')
      num = Number(item[keys[1]])
    }
    if (label === undefined) continue
    if (!series && !Number.isFinite(num)) continue
    out.push({ ...item, ...extra, _label: label, _value: Number.isFinite(num) ? (num as number) : 0 })
  }
  return out
}

// Themed tooltip — replaces recharts' default gray box. Matches app theme,
// shows a small color swatch per series, tabular numbers.
type TooltipEntry = { name?: string; value?: unknown; color?: string; fill?: string; payload?: Record<string, unknown> }
function ChartTooltipContent({ active, payload, label }: { active?: boolean; payload?: TooltipEntry[]; label?: unknown }) {
  if (!active || !payload || payload.length === 0) return null
  const fmt = (v: unknown) => {
    if (typeof v === 'number') return Number.isInteger(v) ? v.toLocaleString() : v.toLocaleString(undefined, { maximumFractionDigits: 2 })
    return String(v ?? '')
  }
  return (
    <div className="rounded-md border border-border bg-popover/95 backdrop-blur-sm px-2.5 py-1.5 text-xs shadow-lg">
      {label != null && String(label) !== '' && (
        <div className="font-semibold text-foreground mb-1">{String(label)}</div>
      )}
      <div className="flex flex-col gap-0.5">
        {payload.map((p, i) => (
          <div key={i} className="flex items-center gap-1.5">
            <span className="w-2 h-2 rounded-sm flex-shrink-0" style={{ backgroundColor: p.color || p.fill }} />
            {p.name && <span className="text-muted-foreground">{p.name}:</span>}
            <span className="font-medium tabular-nums text-foreground">{fmt(p.value)}</span>
          </div>
        ))}
      </div>
    </div>
  )
}

function ChartWidget({ value, widget }: { value: unknown; widget: ReportWidget }) {
  const { theme } = useTheme()
  const [widgetRef, isCompact] = useCompactWidgetLayout()
  const points = useMemo(() => {
    let pts = normaliseChartPoints(value, widget)
    // Sort first (so topN takes the right slice), then truncate.
    if (widget.sort === 'asc') pts = [...pts].sort((a, b) => a._value - b._value)
    else if (widget.sort === 'desc') pts = [...pts].sort((a, b) => b._value - a._value)
    if (widget.topN && widget.topN > 0) pts = pts.slice(0, widget.topN)
    return pts
  }, [value, widget])

  if (points.length === 0) return null

  const chartType = widget.chartType ?? 'bar'
  const showValues = widget.showValues === true
  const heightPx = widget.height ?? 288 // h-72 default
  const minHeightPx = Math.min(isCompact ? 180 : 220, heightPx)
  const chartFrameStyle = { width: '100%', height: `clamp(${minHeightPx}px, ${isCompact ? 34 : 42}vh, ${heightPx}px)` }

  const palette = resolvePalette(widget, theme)
  // For semantic coloring, assign each distinct colorBy value a stable index into
  // the palette so unmapped values still get consistent (not random) colors.
  const distinctIndex = useMemo(() => {
    if (!widget.colorBy) return {}
    const out: Record<string, number> = {}
    let next = 0
    for (const p of points) {
      const raw: unknown = (p as unknown as Record<string, unknown>)[widget.colorBy]
      if (raw === undefined || raw === null) continue
      const key = String(raw)
      if (!(key in out)) out[key] = next++
    }
    return out
  }, [points, widget.colorBy])
  const colorForPoint = (p: (typeof points)[number], fallbackIndex: number): string => {
    if (widget.colorBy) {
      const raw = (p as unknown as Record<string, unknown>)[widget.colorBy]
      const key = raw === undefined || raw === null ? '' : String(raw)
      if (widget.colorMap && widget.colorMap[key]) return widget.colorMap[key]
      if (key in distinctIndex) return palette[distinctIndex[key] % palette.length]
    }
    return palette[fallbackIndex % palette.length]
  }

  const header = (widget.title || widget.description) ? (
    <WidgetHeader widget={widget} className="mb-2" />
  ) : null

  // Pie chart — always colors per slice.
  if (chartType === 'pie') {
    return (
      <div className="flex flex-col">
        {header}
        <div style={chartFrameStyle}>
          <ResponsiveContainer>
            <PieChart>
              <Pie
                data={points}
                dataKey="_value"
                nameKey="_label"
                outerRadius={Math.min(120, heightPx * 0.4)}
                label={showValues ? ((entry: unknown) => {
                  const e = entry as { _label?: string; _value?: number }
                  return `${e._label ?? ''}: ${e._value ?? ''}`
                }) as never : undefined}
              >
                {points.map((p, i) => (
                  <Cell key={i} fill={colorForPoint(p, i)} stroke="hsl(var(--background))" strokeWidth={2} />
                ))}
              </Pie>
              <Tooltip content={<ChartTooltipContent />} />
              <Legend wrapperStyle={{ fontSize: 11 }} iconType="circle" iconSize={8} />
            </PieChart>
          </ResponsiveContainer>
        </div>
      </div>
    )
  }

  // Bar / line / area share the same XY layout.
  const ChartContainer =
    chartType === 'line' ? LineChart : chartType === 'area' ? AreaChart : BarChart
  const ValueLabel = showValues
    ? { dataKey: '_value', position: chartType === 'bar' ? 'top' as const : 'top' as const, fontSize: 10 }
    : undefined

  // Multi-series mode: one series per field in `widget.series`. Colors come
  // from `seriesColors` (parallel) with fallback to the palette. Stacked bars
  // and areas share a stackId; lines never stack.
  const series = widget.series && widget.series.length > 0 ? widget.series : null
  const seriesColorFor = (idx: number): string => {
    if (widget.seriesColors && widget.seriesColors[idx]) return widget.seriesColors[idx]
    return palette[idx % palette.length]
  }
  const stackId = widget.stacked ? 'stack-a' : undefined

  // Single-series fallback — palette[0] for line/area; bar supports per-Cell.
  const singleSeriesColor = palette[0]

  // Stable, unique gradient ids per widget instance. Recharts re-renders would
  // re-create random ids and break the reference, so useId() is required here.
  const gradPrefix = useId().replace(/:/g, '')
  const axisTick = { fontSize: 11, fill: 'hsl(var(--muted-foreground))' }
  const gridStroke = 'hsl(var(--border))'
  const axisLine = { stroke: gridStroke, opacity: 0.65 }
  const hoverCursorFill = 'hsl(var(--muted))'

  return (
    <div ref={widgetRef} className="flex flex-col text-muted-foreground">
      {header}
      <div className="rounded-lg bg-background/55 px-0.5 py-1.5" style={chartFrameStyle}>
        <ResponsiveContainer>
          <ChartContainer data={points} margin={{ top: 8, right: isCompact ? 8 : 16, left: isCompact ? -12 : 0, bottom: 8 }}>
            <defs>
              {/* Per-series gradient for area fills; per-point gradient for single-bar color cycling. */}
              {series
                ? series.map((field, i) => {
                    const c = seriesColorFor(i)
                    return (
                      <linearGradient key={field} id={`${gradPrefix}-s-${i}`} x1="0" y1="0" x2="0" y2="1">
                        <stop offset="0%" stopColor={c} stopOpacity={0.5} />
                        <stop offset="100%" stopColor={c} stopOpacity={0.05} />
                      </linearGradient>
                    )
                  })
                : (
                    <linearGradient id={`${gradPrefix}-single`} x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stopColor={singleSeriesColor} stopOpacity={0.5} />
                      <stop offset="100%" stopColor={singleSeriesColor} stopOpacity={0.05} />
                    </linearGradient>
                  )}
            </defs>
            <CartesianGrid strokeDasharray="3 3" stroke={gridStroke} opacity={0.55} vertical={false} />
            <XAxis dataKey="_label" tick={axisTick} tickLine={false} axisLine={axisLine} interval={0} angle={points.length > (isCompact ? 5 : 8) ? -30 : 0} textAnchor={points.length > (isCompact ? 5 : 8) ? 'end' : 'middle'} height={points.length > (isCompact ? 5 : 8) ? 60 : 30} />
            <YAxis tick={axisTick} tickLine={false} axisLine={axisLine} />
            <Tooltip
              cursor={{ fill: hoverCursorFill, opacity: 0.65 }}
              content={<ChartTooltipContent />}
            />
            {series && !isCompact && <Legend wrapperStyle={{ fontSize: 11 }} iconType="circle" iconSize={8} />}
            {series
              ? series.map((field, i) => {
                  const color = seriesColorFor(i)
                  if (chartType === 'bar') {
                    return <Bar key={field} dataKey={field} fill={color} stackId={stackId} radius={widget.stacked ? undefined : [4, 4, 0, 0]} />
                  }
                  if (chartType === 'line') {
                    return <Line key={field} type="monotone" dataKey={field} stroke={color} strokeWidth={2} dot={{ r: 3, fill: color }} activeDot={{ r: 5 }} />
                  }
                  if (chartType === 'area') {
                    return <Area key={field} type="monotone" dataKey={field} stroke={color} strokeWidth={2} fill={`url(#${gradPrefix}-s-${i})`} stackId={stackId} activeDot={{ r: 5 }} />
                  }
                  return null
                })
              : <>
                  {chartType === 'bar' && (
                    // Single-series bars cycle palette colors per point when no colorBy is set,
                    // so reports don't render as monochrome blocks. `colorBy` still wins when present.
                    <Bar dataKey="_value" radius={[4, 4, 0, 0]} label={ValueLabel}>
                      {points.map((p, i) => (
                        <Cell key={i} fill={colorForPoint(p, i)} />
                      ))}
                    </Bar>
                  )}
                  {chartType === 'line' && <Line type="monotone" dataKey="_value" stroke={singleSeriesColor} strokeWidth={2.25} dot={{ r: 3, fill: singleSeriesColor }} activeDot={{ r: 5 }} label={ValueLabel} />}
                  {chartType === 'area' && <Area type="monotone" dataKey="_value" stroke={singleSeriesColor} strokeWidth={2.25} fill={`url(#${gradPrefix}-single)`} activeDot={{ r: 5 }} label={ValueLabel} />}
                </>}
          </ChartContainer>
        </ResponsiveContainer>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Widget registry. Each kind registers its renderer into the map matching its
// source mode (see WIDGET_SOURCE_MODE). WidgetCard reads from these maps so
// adding a new widget kind doesn't require editing the dispatcher.
// ---------------------------------------------------------------------------
COLLECTION_WIDGET_RENDERERS.text = TextWidget
COLLECTION_WIDGET_RENDERERS.markdown = MarkdownWidget
COLLECTION_WIDGET_RENDERERS.table = TableWidget
COLLECTION_WIDGET_RENDERERS.cards = CardsWidget
COLLECTION_WIDGET_RENDERERS.chart = ChartWidget

SINGULAR_WIDGET_RENDERERS.stat = StatWidget
SINGULAR_WIDGET_RENDERERS.alert = AlertWidget

PIVOT_WIDGET_RENDERERS.pivot = PivotWidget
