import React, { useEffect, useState, useCallback, useMemo } from 'react'
import {
  Clock, DollarSign, Loader2, Calendar, FileText, BarChart3, ChevronDown, ChevronRight,
  Plus, Minus, Database, RefreshCw, AlertCircle, Target, Activity, LayoutDashboard
} from 'lucide-react'
import {
  Bar,
  BarChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { agentApi, type MetricSnapshotRow, type WorkflowMetricRunSummary } from '../services/api'
import { schedulerApi } from '../api/scheduler'
import type { EvaluationReportEntry, ModelTokenUsage, PhaseTokenUsageFile, PlannerFile, TokenUsageFile, ToolCostUsage, WorkflowPhaseDailyCostsEntry, WorkflowReviewDataResponse, WorkflowRunCostsEntry, WorkflowRunDailyCostsEntry } from '../services/api-types'
import ExecutionLogsPopup from './workflow/ExecutionLogsPopup'
import { ReportView } from './workflow/ReportViewer'
import { WorkflowCanvas } from './workflow/canvas'
import { useAppStore } from '../stores/useAppStore'
import { useGlobalPresetStore } from '../stores/useGlobalPresetStore'
import { formatStepOutputContent, hasStepOutputContent, isFinalScoringPlaceholderText, parseEvaluationPlanDetails } from '../utils/evaluationReport'
import { MarkdownRenderer } from './ui/MarkdownRenderer'
import { OrgGoalsPanel, OrgPulsePanel } from './org/OrgHtmlPanels'
import { OrgDashboard } from './org/OrgDashboard'

interface WorkflowSummary {
  id: string
  label: string
  latestStatus: string
  totalRuns: number
  lastActive: string | null
  totalCost: number | null
  metricsSummary: WorkflowMetricRunSummary | null
  workspacePath: string
  latestRunFolder: string | null
  scheduleCount: number
  nextScheduleAt: string | null
}

const workflowsSignature = (workflows: WorkflowSummary[]): string => {
  return JSON.stringify(
    workflows
      .map(wf => ({
        path: wf.workspacePath,
        label: wf.label,
        status: wf.latestStatus,
        runs: wf.totalRuns,
        lastActive: wf.lastActive || '',
        latestRunFolder: wf.latestRunFolder || '',
        scheduleCount: wf.scheduleCount,
        nextScheduleAt: wf.nextScheduleAt || '',
      }))
      .sort((a, b) => a.path.localeCompare(b.path))
  )
}

type ReviewTab = 'report' | 'flow' | 'evaluation' | 'cost' | 'knowledgebase' | 'logs' | 'soul' | 'skills' | 'config'

interface ImproveDocState {
  loading: boolean
  path: string
  exists: boolean
  content: string
  error: string | null
}

interface KBNotesTopic {
  id: string
  file: string
  covers?: string[]
  last_updated?: string
  last_updated_by?: { step?: string; run?: string }
  size_bytes?: number
  section_count?: number
}

interface KBNotesIndex {
  topics?: KBNotesTopic[]
  last_updated?: string
  last_updated_by?: { step?: string; run?: string }
}

interface KnowledgebaseState {
  loading: boolean
  error: string | null
  index: KBNotesIndex | null
  bodies: Record<string, string | null>
  expanded: Set<string>
}

interface SkillReferenceFile {
  relPath: string
  rawPath: string
}

interface WorkflowSkillsState {
  loading: boolean
  error: string | null
  path: string
  exists: boolean
  content: string
  files: SkillReferenceFile[]
  metadataCount: number
  fileBodies: Record<string, string | null>
  expandedFiles: Set<string>
}

interface WorkflowReviewState {
  loading: boolean
  reviewData: WorkflowReviewDataResponse | null
  evaluation: EvaluationReportEntry | null
  evaluationError: string | null
  metrics: MetricDefinition[]
  metricsHistory: MetricSnapshotRow[]
  metricsError: string | null
  tokenUsage: TokenUsageFile | null
  evaluationTokenUsage: TokenUsageFile | null
  costRuns: WorkflowRunCostsEntry[]
  phaseDailyCosts: WorkflowPhaseDailyCostsEntry[]
  runDailyCosts: WorkflowRunDailyCostsEntry[]
  costError: string | null
}

type WorkflowsSummaryResponse = Awaited<ReturnType<typeof agentApi.getWorkflowsSummary>>
type WorkflowApiSummary = WorkflowsSummaryResponse['workflows'][number]

interface MetricDefinition {
  id: string
  label?: string
  unit: string
  direction: 'higher_better' | 'lower_better'
  mode: 'target' | 'slo'
  role?: 'primary' | 'secondary' | string
  category?: string
  target?: number
  floor?: number
  ceiling?: number
}

// Mini status indicator
const StatusDot: React.FC<{ status: string }> = ({ status }) => {
  if (status === 'completed') return <div className="h-2.5 w-2.5 rounded-full bg-emerald-500" />
  if (status === 'running') return <div className="h-2.5 w-2.5 rounded-full bg-sky-500 animate-pulse" />
  if (status === 'failed') return <div className="h-2.5 w-2.5 rounded-full bg-rose-500" />
  return <div className="w-2.5 h-2.5 rounded-full bg-gray-300 dark:bg-slate-500" />
}

const EMPTY_REVIEW_STATE: WorkflowReviewState = {
  loading: false,
  reviewData: null,
  evaluation: null,
  evaluationError: null,
  metrics: [],
  metricsHistory: [],
  metricsError: null,
  tokenUsage: null,
  evaluationTokenUsage: null,
  costRuns: [],
  phaseDailyCosts: [],
  runDailyCosts: [],
  costError: null,
}

const EMPTY_SOUL_DOC_STATE: ImproveDocState = {
  loading: false,
  path: 'soul/soul.md',
  exists: false,
  content: '',
  error: null,
}

const EMPTY_WORKFLOW_CONFIG_STATE: ImproveDocState = {
  loading: false,
  path: 'workflow.json',
  exists: false,
  content: '',
  error: null,
}

const EMPTY_KNOWLEDGEBASE_STATE: KnowledgebaseState = {
  loading: false,
  error: null,
  index: null,
  bodies: {},
  expanded: new Set<string>(),
}

const EMPTY_WORKFLOW_SKILLS_STATE: WorkflowSkillsState = {
  loading: false,
  error: null,
  path: 'learnings/_global/SKILL.md',
  exists: false,
  content: '',
  files: [],
  metadataCount: 0,
  fileBodies: {},
  expandedFiles: new Set<string>(),
}

type LegacyKBTopicMeta = {
  covers?: unknown
  last_updated?: unknown
  last_updated_by?: unknown
  size_bytes?: unknown
  section_count?: unknown
}

const runFolderMatches = (candidate: string | null | undefined, requested: string | null | undefined): boolean => {
  if (!candidate || !requested) return false
  return candidate === requested || candidate.startsWith(`${requested}/`) || requested.startsWith(`${candidate}/`)
}

// Merge N TokenUsageFile entries into one by summing per-model numeric fields.
// Mirrors what the workflows CostsPopup does client-side.
const mergeTokenUsageFiles = (files: Array<TokenUsageFile | null | undefined>): TokenUsageFile | null => {
  const nonNull = files.filter((f): f is TokenUsageFile => !!f)
  if (nonNull.length === 0) return null

  const merged: TokenUsageFile = {
    created_at: nonNull[0].created_at,
    updated_at: nonNull[0].updated_at,
    by_model: {},
  }

  for (const file of nonNull) {
    for (const [model, stats] of Object.entries(file.by_model || {})) {
      const existing = merged.by_model[model]
      if (!existing) {
        merged.by_model[model] = { ...stats }
        continue
      }
      existing.input_tokens = (existing.input_tokens || 0) + (stats.input_tokens || 0)
      existing.output_tokens = (existing.output_tokens || 0) + (stats.output_tokens || 0)
      existing.cache_tokens = (existing.cache_tokens || 0) + (stats.cache_tokens || 0)
      existing.reasoning_tokens = (existing.reasoning_tokens || 0) + (stats.reasoning_tokens || 0)
      existing.llm_call_count = (existing.llm_call_count || 0) + (stats.llm_call_count || 0)
      existing.total_cost_usd = (existing.total_cost_usd || 0) + (stats.total_cost_usd || 0)
      existing.input_cost_usd = (existing.input_cost_usd || 0) + (stats.input_cost_usd || 0)
      existing.output_cost_usd = (existing.output_cost_usd || 0) + (stats.output_cost_usd || 0)
    }
  }

  return merged
}

const getTokenUsageTotal = (usage: TokenUsageFile | null | undefined): number | null => {
  if (!usage) return null
  let total = 0
  let found = false
  Object.values(usage.by_model || {}).forEach(model => {
    const cost = model.total_cost_usd || 0
    total += cost
    if (cost > 0) found = true
  })
  return found || total > 0 ? total : null
}

const formatUsd = (value: number | null): string => {
  if (value === null) return '-'
  if (value < 0.01) return '<$0.01'
  return `$${value.toFixed(2)}`
}

const formatUsdDetailed = (value: number | null | undefined): string => {
  if (!value || value <= 0) return '$0'
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 4,
    maximumFractionDigits: 4,
  }).format(value)
}

const formatCostCount = (value: number | null | undefined): string => {
  if (!value || value <= 0) return '-'
  return value.toLocaleString()
}

const formatCostDetailLabel = (raw: string): string => {
  const parts = raw.split(':').filter(Boolean)
  const value = parts[parts.length - 1] || raw
  return value
    .split(/[-_]/)
    .filter(Boolean)
    .map(part => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ')
}

const getRunFolderDisplayName = (runFolder: string): string => {
  const parts = runFolder.split('/').filter(Boolean)
  if (parts.length >= 2) return `${parts[0]} · ${parts.slice(1).join('/')}`
  return parts[parts.length - 1] || runFolder
}

const topicIdFromFile = (file: string): string => file.replace(/\.md$/i, '')

const normalizeUpdatedBy = (value: unknown): KBNotesTopic['last_updated_by'] => {
  if (!value) return undefined
  if (typeof value === 'string') return { step: value }
  if (typeof value !== 'object') return undefined
  const record = value as Record<string, unknown>
  return {
    step: typeof record.step === 'string' ? record.step : undefined,
    run: typeof record.run === 'string' ? record.run : undefined,
  }
}

const normalizeKBIndex = (raw: unknown): KBNotesIndex | null => {
  if (!raw || typeof raw !== 'object') return null
  const record = raw as Record<string, unknown>
  if (Array.isArray(record.topics)) return raw as KBNotesIndex

  const topics: KBNotesTopic[] = Object.entries(record)
    .filter(([file, meta]) => file.toLowerCase().endsWith('.md') && meta && typeof meta === 'object' && !Array.isArray(meta))
    .map(([file, rawMeta]) => {
      const meta = rawMeta as LegacyKBTopicMeta
      return {
        id: topicIdFromFile(file),
        file,
        covers: Array.isArray(meta.covers) ? meta.covers.filter((item): item is string => typeof item === 'string') : undefined,
        last_updated: typeof meta.last_updated === 'string' ? meta.last_updated : undefined,
        last_updated_by: normalizeUpdatedBy(meta.last_updated_by),
        size_bytes: typeof meta.size_bytes === 'number' ? meta.size_bytes : undefined,
        section_count: typeof meta.section_count === 'number' ? meta.section_count : undefined,
      }
    })
    .sort((a, b) => a.id.localeCompare(b.id))

  return { topics }
}

const readWorkspaceText = async (filepath: string): Promise<string | null> => {
  try {
    const response = await agentApi.getPlannerFileContent(filepath)
    if (!response?.success || typeof response.data?.content !== 'string') return null
    return response.data.content
  } catch {
    return null
  }
}

const readWorkspaceJSON = async <T,>(filepath: string): Promise<T | null> => {
  const content = await readWorkspaceText(filepath)
  if (!content) return null
  try {
    return JSON.parse(content) as T
  } catch {
    return null
  }
}

const withTimeout = async <T,>(promise: Promise<T>, timeoutMs: number, label: string): Promise<T> => {
  let timeoutId: ReturnType<typeof setTimeout> | null = null
  try {
    return await Promise.race([
      promise,
      new Promise<T>((_, reject) => {
        timeoutId = setTimeout(() => reject(new Error(`${label} timed out`)), timeoutMs)
      }),
    ])
  } finally {
    if (timeoutId) clearTimeout(timeoutId)
  }
}

const stripMarkdownFrontmatter = (content: string): string => {
  if (!content.startsWith('---')) return content
  const endIdx = content.indexOf('\n---', 3)
  return endIdx === -1 ? content : content.slice(endIdx + 4).trim()
}

const plannerFilesFromResponse = (response: unknown): PlannerFile[] => {
  if (Array.isArray(response)) return response as PlannerFile[]
  if (response && typeof response === 'object' && 'data' in response) {
    const data = (response as { data?: unknown }).data
    if (Array.isArray(data)) return data as PlannerFile[]
  }
  return []
}

const flattenPlannerLeafFiles = (files: PlannerFile[]): PlannerFile[] => {
  const result: PlannerFile[] = []
  const walk = (nodes: PlannerFile[]) => {
    for (const node of nodes) {
      const isFolder = node.type === 'folder' || (Array.isArray(node.children) && node.children.length > 0)
      if (isFolder) {
        if (Array.isArray(node.children)) walk(node.children)
      } else {
        result.push(node)
      }
    }
  }
  walk(files)
  return result
}

const relFromKnowledgebaseNotesPath = (filepath: string): string => {
  const normalized = filepath.replace(/\\/g, '/')
  const marker = '/knowledgebase/notes/'
  const idx = normalized.indexOf(marker)
  return idx === -1 ? normalized.replace(/^\/+/, '') : normalized.slice(idx + marker.length)
}

const buildKnowledgebaseIndexFromFiles = async (workspacePath: string): Promise<KBNotesIndex> => {
  const filesResponse = await withTimeout(
    agentApi.getPlannerFiles(`${workspacePath}/knowledgebase/notes`, 500, 2),
    6000,
    'Knowledgebase file listing'
  )
  const topicsByFile = new Map<string, KBNotesTopic>()
  flattenPlannerLeafFiles(plannerFilesFromResponse(filesResponse)).forEach(file => {
    const relPath = relFromKnowledgebaseNotesPath(file.filepath || file.originalFilepath || '')
    if (!relPath || relPath === '_index.json' || !relPath.toLowerCase().endsWith('.md')) return
    topicsByFile.set(relPath, {
      id: topicIdFromFile(relPath),
      file: relPath,
    })
  })
  return { topics: Array.from(topicsByFile.values()).sort((a, b) => a.id.localeCompare(b.id)) }
}

const relFromGlobalSkillPath = (filepath: string): string => {
  const normalized = filepath.replace(/\\/g, '/')
  const marker = '/learnings/_global/'
  const idx = normalized.indexOf(marker)
  return idx === -1 ? normalized.replace(/^\/+/, '') : normalized.slice(idx + marker.length)
}

const normalizeSkillRelativePath = (filepath: string): string => {
  try {
    filepath = decodeURIComponent(filepath)
  } catch {
    // keep original path
  }
  return filepath
    .split(/[?#]/, 1)[0]
    .replace(/^\/+/, '')
    .split('/')
    .filter(segment => segment && segment !== '.')
    .join('/')
}

const resolveGlobalSkillFilePath = (workspacePath: string, rawPath: string): string => {
  if (rawPath.startsWith(workspacePath)) return rawPath
  const clean = rawPath.replace(/^\/+/, '')
  if (clean.startsWith(workspacePath)) return clean
  if (clean.includes('/learnings/_global/')) return clean
  if (clean.startsWith('learnings/_global/')) return `${workspacePath}/${clean}`
  return `${workspacePath}/learnings/_global/${clean}`
}

interface DailyCostDetail {
  key: string
  source: string
  type: 'Model' | 'Tool'
  label: string
  sublabel: string
  provider: string
  calls: number
  inputTokens: number
  cachedTokens: number
  outputTokens: number
  cost: number
}

const modelUsageToDailyDetail = (
  key: string,
  source: string,
  label: string,
  modelID: string,
  usage: ModelTokenUsage
): DailyCostDetail => ({
  key,
  source,
  type: 'Model',
  label,
  sublabel: modelID,
  provider: usage.provider || 'unknown',
  calls: usage.llm_call_count || 0,
  inputTokens: usage.input_tokens || 0,
  cachedTokens: (usage.cache_read_tokens || usage.cache_tokens || 0) + (usage.cache_write_tokens || 0),
  outputTokens: usage.output_tokens || 0,
  cost: usage.total_cost_usd || 0,
})

const toolUsageToDailyDetail = (
  key: string,
  source: string,
  label: string,
  toolKey: string,
  usage: ToolCostUsage
): DailyCostDetail => ({
  key,
  source,
  type: 'Tool',
  label,
  sublabel: usage.tool_name || toolKey,
  provider: usage.provider || '-',
  calls: usage.count || usage.quantity || 0,
  inputTokens: 0,
  cachedTokens: 0,
  outputTokens: 0,
  cost: usage.total_cost_usd || 0,
})

const buildRunDailyDetails = (
  tokenUsage: TokenUsageFile | null | undefined,
  source: string,
  keyPrefix: string
): DailyCostDetail[] => {
  if (!tokenUsage) return []
  const details: DailyCostDetail[] = []
  const stepModelMap = tokenUsage.by_step_and_model || {}
  const stepToolMap = tokenUsage.by_step_and_tool || {}

  Object.entries(stepModelMap).forEach(([stepKey, modelMap]) => {
    Object.entries(modelMap).forEach(([modelID, usage]) => {
      details.push(modelUsageToDailyDetail(
        `${keyPrefix}:step-model:${stepKey}:${modelID}`,
        source,
        formatCostDetailLabel(stepKey),
        modelID,
        usage
      ))
    })
  })

  Object.entries(stepToolMap).forEach(([stepKey, toolMap]) => {
    Object.entries(toolMap).forEach(([toolKey, usage]) => {
      details.push(toolUsageToDailyDetail(
        `${keyPrefix}:step-tool:${stepKey}:${toolKey}`,
        source,
        formatCostDetailLabel(stepKey),
        toolKey,
        usage
      ))
    })
  })

  if (Object.keys(stepModelMap).length === 0) {
    Object.entries(tokenUsage.by_model || {}).forEach(([modelID, usage]) => {
      details.push(modelUsageToDailyDetail(
        `${keyPrefix}:model:${modelID}`,
        source,
        'Run total',
        modelID,
        usage
      ))
    })
  }

  if (Object.keys(stepToolMap).length === 0) {
    Object.entries(tokenUsage.by_tool || {}).forEach(([toolKey, usage]) => {
      details.push(toolUsageToDailyDetail(
        `${keyPrefix}:tool:${toolKey}`,
        source,
        'Run total',
        toolKey,
        usage
      ))
    })
  }

  return details
}

const buildPhaseDailyDetails = (
  tokenUsage: PhaseTokenUsageFile | null | undefined,
  keyPrefix: string
): DailyCostDetail[] => {
  if (!tokenUsage) return []
  const details: DailyCostDetail[] = []
  const phaseModelMap = tokenUsage.by_phase_and_model || {}

  Object.entries(phaseModelMap).forEach(([phaseID, modelMap]) => {
    Object.entries(modelMap).forEach(([modelID, usage]) => {
      details.push(modelUsageToDailyDetail(
        `${keyPrefix}:phase-model:${phaseID}:${modelID}`,
        'Builder',
        formatCostDetailLabel(phaseID),
        modelID,
        usage
      ))
    })
  })

  if (Object.keys(phaseModelMap).length === 0) {
    Object.entries(tokenUsage.by_model || {}).forEach(([modelID, usage]) => {
      details.push(modelUsageToDailyDetail(
        `${keyPrefix}:phase-total:${modelID}`,
        'Builder',
        'Builder total',
        modelID,
        usage
      ))
    })
  }

  return details
}

const ReviewTabButton: React.FC<{
  active: boolean
  label: string
  onClick: () => void
}> = ({ active, label, onClick }) => (
  <button
    onClick={onClick}
    className={`px-3 py-1.5 rounded-md text-xs font-medium transition-colors ${
      active
        ? 'border border-border bg-background text-foreground shadow-sm'
        : 'text-muted-foreground hover:text-foreground'
    }`}
  >
    {label}
  </button>
)

const metricHealthText = (summary: WorkflowMetricRunSummary | null): string | null => {
  if (!summary || summary.total <= 0) return null
  if (summary.failed > 0) return `${summary.failed} failing`
  if (summary.with_value < summary.total) return `${summary.with_value}/${summary.total} values`
  if (summary.passed > 0) return `${summary.passed}/${summary.total} passing`
  return `${summary.total} metrics`
}

const metricHealthClass = (summary: WorkflowMetricRunSummary | null): string => {
  if (!summary || summary.total <= 0) return 'bg-muted text-muted-foreground ring-1 ring-inset ring-border'
  if (summary.failed > 0) return 'bg-destructive/15 text-destructive'
  if (summary.with_value < summary.total) return 'bg-warning/15 text-warning'
  if (summary.passed > 0) return 'bg-success/15 text-success'
  return 'bg-muted text-muted-foreground ring-1 ring-inset ring-border'
}

const metricRowStatusClass = (row: MetricSnapshotRow): string => {
  if (!row.has_value) return 'text-warning'
  if (row.passed === false) return 'text-destructive'
  if (row.passed === true) return 'text-success'
  return 'text-muted-foreground'
}

const metricThresholdLabel = (row: MetricSnapshotRow): string => {
  if (!row.threshold_kind || typeof row.threshold_value !== 'number') return 'no threshold'
  return `${row.threshold_kind} ${row.threshold_value}`
}

const metricRoleLabel = (metric: MetricDefinition | undefined): string | null => {
  const role = metric?.role?.trim()
  if (!role) return null
  if (role.toLowerCase() === 'primary') return 'Primary'
  if (role.toLowerCase() === 'secondary') return 'Secondary'
  return role
    .replace(/[-_]+/g, ' ')
    .replace(/\b\w/g, match => match.toUpperCase())
}

const metricRoleClass = (label: string | null): string => {
  const role = label?.toLowerCase()
  if (role === 'primary') return 'border-primary/30 bg-primary/10 text-primary'
  if (role === 'secondary') return 'border-sky-500/30 bg-sky-500/10 text-sky-700 dark:text-sky-300'
  return 'border-border bg-muted text-muted-foreground'
}

const metricRoleRank = (metric: MetricDefinition | undefined): number => {
  const role = metric?.role?.toLowerCase()
  if (role === 'primary') return 0
  if (role === 'secondary') return 1
  return 2
}

const formatMetricDisplayValue = (value: number, unit?: string): string => {
  const formatted = Number.isInteger(value) ? value.toLocaleString() : value.toLocaleString(undefined, { maximumFractionDigits: 3 })
  return unit ? `${formatted} ${unit}` : formatted
}

export const EmployeeDashboard: React.FC = () => {
  const showWorkflowsOverview = useAppStore(state => state.showWorkflowsOverview)
  const workflowPresets = useGlobalPresetStore(state => state.workflowPresets)
  const workflowPresetsLoaded = useGlobalPresetStore(state => state.workflowPresetsLoaded)
  const presetsLoading = useGlobalPresetStore(state => state.loading)
  const refreshPresets = useGlobalPresetStore(state => state.refreshPresets)
  const [workflows, setWorkflows] = useState<WorkflowSummary[]>([])
  const [loading, setLoading] = useState(true)
  const [selectedWorkflowId, setSelectedWorkflowId] = useState<string | null>(null)
  const [orgView, setOrgView] = useState<'workflow' | 'goals' | 'pulse' | 'dashboard'>('dashboard')
  const [reviewTab, setReviewTab] = useState<ReviewTab>('report')
  const [reviewState, setReviewState] = useState<WorkflowReviewState>(EMPTY_REVIEW_STATE)
  const [soulDocState, setSoulDocState] = useState<ImproveDocState>(EMPTY_SOUL_DOC_STATE)
  const [workflowConfigState, setWorkflowConfigState] = useState<ImproveDocState>(EMPTY_WORKFLOW_CONFIG_STATE)
  const [knowledgebaseState, setKnowledgebaseState] = useState<KnowledgebaseState>(EMPTY_KNOWLEDGEBASE_STATE)
  const [workflowSkillsState, setWorkflowSkillsState] = useState<WorkflowSkillsState>(EMPTY_WORKFLOW_SKILLS_STATE)
  const [expandedEvalSteps, setExpandedEvalSteps] = useState<Set<string>>(new Set())
  const [expandedDailyCostDates, setExpandedDailyCostDates] = useState<Set<string>>(new Set())

  const loadData = useCallback(async (opts?: { silent?: boolean }) => {
    if (!opts?.silent) setLoading(true)
    try {
      const schedulesResp = await schedulerApi.listJobs({ entity_type: 'workflow' }).catch(() => ({ jobs: [], total: 0, limit: 0, offset: 0 }))

      const discoveredWorkflows = workflowPresets
      const scheduleByWorkspace = new Map<string, { count: number; nextRunAt: string | null }>()
      for (const job of schedulesResp.jobs || []) {
        const workspacePath = job.workspace_path
        if (!workspacePath) continue
        const prev = scheduleByWorkspace.get(workspacePath) || { count: 0, nextRunAt: null }
        let nextRunAt = prev.nextRunAt
        if (job.enabled && job.next_run_at) {
          if (!nextRunAt || job.next_run_at < nextRunAt) nextRunAt = job.next_run_at
        }
        scheduleByWorkspace.set(workspacePath, { count: prev.count + 1, nextRunAt })
      }

      // Build workflow summaries using the batch summary endpoint (single API call)
      const summaries: Map<string, WorkflowSummary> = new Map()
      const allWorkspacePaths = discoveredWorkflows.map(wf => wf.selectedFolder?.filepath).filter((path): path is string => Boolean(path))

      // Fetch only lightweight workflow summaries for the dashboard list. Detailed
      // evaluation data is loaded for the selected workflow via review-data below.
      const summaryResp = await (
        allWorkspacePaths.length > 0
          ? agentApi.getWorkflowsSummary(allWorkspacePaths).catch(() => null)
          : Promise.resolve(null)
      )

      // Index summary results by workspace path
      const summaryByPath = new Map<string, WorkflowApiSummary>()
      if (summaryResp?.success && summaryResp.workflows) {
        for (const ws of summaryResp.workflows) {
          summaryByPath.set(ws.workspace_path, ws)
        }
      }

      for (const workflow of discoveredWorkflows) {
        const wp = workflow.selectedFolder?.filepath
        if (!wp) continue
        const ws = summaryByPath.get(wp)
        const sched = scheduleByWorkspace.get(wp)

        let latestStatus = ws?.latest_run?.status || 'unknown'
        const lastActive = ws?.latest_run?.created_at || null
        const latestRunFolder = ws?.latest_run?.folder || null

        if (ws?.is_running) {
          latestStatus = 'running'
        }

        summaries.set(wp, {
          id: workflow.id || wp,
          label: workflow.label || wp.split('/').pop() || wp,
          latestStatus,
          totalRuns: ws?.total_runs || 0,
          lastActive,
          totalCost: null,
          metricsSummary: ws?.latest_run?.metrics_summary || null,
          workspacePath: wp,
          latestRunFolder,
          scheduleCount: sched?.count || 0,
          nextScheduleAt: sched?.nextRunAt || null,
        })
      }

      const nextWorkflows = Array.from(summaries.values()).sort((a, b) => a.label.localeCompare(b.label))
      setWorkflows(prev => workflowsSignature(prev) === workflowsSignature(nextWorkflows) ? prev : nextWorkflows)
    } catch (err) {
      console.error('Failed to load automations dashboard:', err)
    }
    if (!opts?.silent) setLoading(false)
  }, [workflowPresets])

  const refreshWorkflows = useCallback(async () => {
    try {
      await loadData({ silent: true })
    } catch (error) {
      console.error('[AutomationsDashboard] Failed to refresh automations:', error)
    }
  }, [loadData])

  useEffect(() => {
    if (!workflowPresetsLoaded && workflowPresets.length === 0) {
      if (!presetsLoading) {
        refreshPresets().catch(error => {
          console.error('[EmployeeDashboard] Failed to refresh workflow presets:', error)
        })
      }
      return
    }
    if (presetsLoading && workflowPresets.length === 0) return
    loadData()
  }, [showWorkflowsOverview, workflowPresetsLoaded, presetsLoading, workflowPresets.length, refreshPresets, loadData])

  useEffect(() => {
    if (!showWorkflowsOverview) return

    const refreshIfVisible = () => {
      if (document.visibilityState === 'visible') {
        refreshWorkflows()
      }
    }

    window.addEventListener('focus', refreshIfVisible)
    document.addEventListener('visibilitychange', refreshIfVisible)

    return () => {
      window.removeEventListener('focus', refreshIfVisible)
      document.removeEventListener('visibilitychange', refreshIfVisible)
    }
  }, [showWorkflowsOverview, refreshWorkflows])

  const selectedWorkflow = useMemo<WorkflowSummary | null>(() => {
    if (workflows.length === 0 || !selectedWorkflowId) return null
    return workflows.find(wf => wf.workspacePath === selectedWorkflowId) || null
  }, [workflows, selectedWorkflowId])

  useEffect(() => {
    if (workflows.length === 0 || !selectedWorkflowId) {
      if (selectedWorkflowId !== null) setSelectedWorkflowId(null)
      return
    }

    if (!workflows.some(wf => wf.workspacePath === selectedWorkflowId)) {
      setSelectedWorkflowId(null)
    }
  }, [selectedWorkflowId, workflows])

  const loadWorkflowReview = useCallback(async (workflow: WorkflowSummary | null) => {
    if (!workflow || !workflow.workspacePath || !workflow.latestRunFolder) {
      setReviewState(EMPTY_REVIEW_STATE)
      return
    }

    const workspacePath = workflow.workspacePath
    const runFolder = workflow.latestRunFolder

    setReviewState({
      ...EMPTY_REVIEW_STATE,
      loading: true,
    })

    try {
      const [reviewData, metricsResp, metricsHistoryResp] = await Promise.all([
        agentApi.getWorkflowReviewData(workspacePath, runFolder),
        agentApi.getAutoImprovementMetrics(workspacePath).catch(err => ({ success: false, file: { metrics: [] }, error: err instanceof Error ? err.message : 'Failed to load metrics' })),
        agentApi.getMetricsHistory(workspacePath).catch(err => ({ success: false, rows: [], error: err instanceof Error ? err.message : 'Failed to load metric history' })),
      ])
      const evaluationResponse = reviewData.evaluations
      const costsResponse = reviewData.costs
      const metrics = metricsResp.success && metricsResp.file?.metrics
        ? metricsResp.file.metrics as MetricDefinition[]
        : []
      const metricsHistory = metricsHistoryResp.success && Array.isArray(metricsHistoryResp.rows)
        ? metricsHistoryResp.rows as MetricSnapshotRow[]
        : []
      const metricsError = metricsResp.success && metricsHistoryResp.success
        ? null
        : (metricsResp.error || metricsHistoryResp.error || 'Failed to load metrics')

      let evaluation: EvaluationReportEntry | null = null
      let evaluationError: string | null = null
      if (evaluationResponse?.success) {
        const evaluations = Array.isArray(evaluationResponse.reports) ? evaluationResponse.reports : []
        evaluation = evaluations.find(item => runFolderMatches(item.run_folder, runFolder)) || evaluations[0] || null
      } else if (evaluationResponse?.error) {
        evaluationError = evaluationResponse.error
      } else {
        evaluationError = 'Failed to load evaluation'
      }

      let tokenUsage: TokenUsageFile | null = null
      let evaluationTokenUsage: TokenUsageFile | null = null
      let costRuns: WorkflowRunCostsEntry[] = []
      let phaseDailyCosts: WorkflowPhaseDailyCostsEntry[] = []
      let runDailyCosts: WorkflowRunDailyCostsEntry[] = []
      let costError: string | null = null
      if (costsResponse?.success) {
        costRuns = costsResponse.runs || []
        phaseDailyCosts = costsResponse.phase_daily_costs || []
        runDailyCosts = costsResponse.run_daily_costs || []
        tokenUsage = mergeTokenUsageFiles(costRuns.map(r => r.token_usage))
        evaluationTokenUsage = mergeTokenUsageFiles(costRuns.map(r => r.evaluation_token_usage))
      } else {
        costError = 'Failed to load cost data'
      }

      setReviewState({
        loading: false,
        reviewData,
        evaluation,
        evaluationError,
        metrics,
        metricsHistory,
        metricsError,
        tokenUsage,
        evaluationTokenUsage,
        costRuns,
        phaseDailyCosts,
        runDailyCosts,
        costError,
      })
    } catch (err) {
      setReviewState({
        loading: false,
        reviewData: null,
        evaluation: null,
        evaluationError: err instanceof Error ? err.message : 'Failed to load evaluation',
        metrics: [],
        metricsHistory: [],
        metricsError: err instanceof Error ? err.message : 'Failed to load metrics',
        tokenUsage: null,
        evaluationTokenUsage: null,
        costRuns: [],
        phaseDailyCosts: [],
        runDailyCosts: [],
        costError: err instanceof Error ? err.message : 'Failed to load cost data',
      })
    }
  }, [])

  useEffect(() => {
    loadWorkflowReview(selectedWorkflow)
  }, [loadWorkflowReview, selectedWorkflow?.workspacePath, selectedWorkflow?.latestRunFolder])

  useEffect(() => {
    setExpandedDailyCostDates(new Set())
    setSoulDocState(EMPTY_SOUL_DOC_STATE)
    setWorkflowConfigState(EMPTY_WORKFLOW_CONFIG_STATE)
    setKnowledgebaseState(EMPTY_KNOWLEDGEBASE_STATE)
    setWorkflowSkillsState(EMPTY_WORKFLOW_SKILLS_STATE)
  }, [selectedWorkflowId])

  const loadSoulDoc = useCallback(async (workspacePath: string | null | undefined) => {
    if (!workspacePath) {
      setSoulDocState(EMPTY_SOUL_DOC_STATE)
      return
    }
    setSoulDocState(prev => ({ ...prev, loading: true, error: null }))
    try {
      const response = await agentApi.getBuilderDoc(workspacePath, 'soul')
      setSoulDocState({
        loading: false,
        path: response.path || EMPTY_SOUL_DOC_STATE.path,
        exists: !!response.exists,
        content: response.content || '',
        error: response.success ? null : (response.error || 'Failed to load soul.md'),
      })
    } catch (err) {
      setSoulDocState({
        ...EMPTY_SOUL_DOC_STATE,
        loading: false,
        error: err instanceof Error ? err.message : 'Failed to load soul.md',
      })
    }
  }, [])

  useEffect(() => {
    if (reviewTab === 'soul') {
      loadSoulDoc(selectedWorkflow?.workspacePath)
    }
  }, [loadSoulDoc, reviewTab, selectedWorkflow?.workspacePath])

  const loadWorkflowConfig = useCallback(async (workspacePath: string | null | undefined) => {
    if (!workspacePath) {
      setWorkflowConfigState(EMPTY_WORKFLOW_CONFIG_STATE)
      return
    }
    const path = `${workspacePath}/workflow.json`
    setWorkflowConfigState(prev => ({ ...prev, loading: true, error: null, path }))
    try {
      const content = await readWorkspaceText(path)
      if (!content) {
        setWorkflowConfigState({
          ...EMPTY_WORKFLOW_CONFIG_STATE,
          loading: false,
          path,
          error: null,
        })
        return
      }
      let formatted = content
      try {
        formatted = JSON.stringify(JSON.parse(content), null, 2)
      } catch {
        // Keep original content if the file is not valid JSON so the user can inspect it.
      }
      setWorkflowConfigState({
        loading: false,
        path,
        exists: true,
        content: formatted,
        error: null,
      })
    } catch (err) {
      setWorkflowConfigState({
        ...EMPTY_WORKFLOW_CONFIG_STATE,
        loading: false,
        path,
        error: err instanceof Error ? err.message : 'Failed to load automation config',
      })
    }
  }, [])

  useEffect(() => {
    if (reviewTab === 'config') {
      loadWorkflowConfig(selectedWorkflow?.workspacePath)
    }
  }, [loadWorkflowConfig, reviewTab, selectedWorkflow?.workspacePath])

  const loadKnowledgebase = useCallback(async (workspacePath: string | null | undefined) => {
    if (!workspacePath) {
      setKnowledgebaseState(EMPTY_KNOWLEDGEBASE_STATE)
      return
    }
    setKnowledgebaseState(prev => ({ ...prev, loading: true, error: null }))
    try {
      let index: KBNotesIndex | null = null
      try {
        const rawIndex = await withTimeout(
          readWorkspaceJSON<unknown>(`${workspacePath}/knowledgebase/notes/_index.json`),
          6000,
          'Knowledgebase index'
        )
        index = normalizeKBIndex(rawIndex)
      } catch {
        index = await buildKnowledgebaseIndexFromFiles(workspacePath)
      }
      setKnowledgebaseState({
        loading: false,
        error: null,
        index,
        bodies: {},
        expanded: new Set<string>(),
      })
    } catch (err) {
      setKnowledgebaseState({
        ...EMPTY_KNOWLEDGEBASE_STATE,
        loading: false,
        error: err instanceof Error ? err.message : 'Failed to load knowledgebase',
      })
    }
  }, [])

  useEffect(() => {
    if (reviewTab === 'knowledgebase') {
      loadKnowledgebase(selectedWorkflow?.workspacePath)
    }
  }, [loadKnowledgebase, reviewTab, selectedWorkflow?.workspacePath])

  const loadWorkflowSkills = useCallback(async (workspacePath: string | null | undefined) => {
    if (!workspacePath) {
      setWorkflowSkillsState(EMPTY_WORKFLOW_SKILLS_STATE)
      return
    }

    const skillPath = `${workspacePath}/learnings/_global/SKILL.md`
    setWorkflowSkillsState(prev => ({ ...prev, loading: true, error: null, path: skillPath }))

    try {
      const [skillContent, filesResponse, learningsResponse] = await Promise.all([
        readWorkspaceText(skillPath),
        agentApi.getPlannerFiles(`${workspacePath}/learnings/_global`, 500, 3).catch(() => []),
        agentApi.getAllStepLearnings(workspacePath).catch(() => ({ success: false, learnings: {} })),
      ])
      const rawFiles: PlannerFile[] = Array.isArray(filesResponse)
        ? filesResponse as PlannerFile[]
        : (filesResponse?.data && Array.isArray(filesResponse.data) ? filesResponse.data as PlannerFile[] : [])

      const filesByRelPath = new Map<string, SkillReferenceFile>()
      flattenPlannerLeafFiles(rawFiles).forEach(file => {
        const rawPath = file.filepath || file.originalFilepath || ''
        const relPath = normalizeSkillRelativePath(relFromGlobalSkillPath(rawPath))
        if (!relPath || relPath === 'SKILL.md' || relPath.endsWith('.learning_metadata.json')) return
        if (!filesByRelPath.has(relPath)) filesByRelPath.set(relPath, { relPath, rawPath })
      })
      const files = Array.from(filesByRelPath.values()).sort((a, b) => a.relPath.localeCompare(b.relPath))

      const metadataCount = Object.entries(learningsResponse.learnings || {})
        .filter(([stepId, metadata]) => stepId !== '_global' && metadata && typeof metadata === 'object')
        .length

      setWorkflowSkillsState({
        loading: false,
        error: null,
        path: skillPath,
        exists: Boolean(skillContent),
        content: skillContent ? stripMarkdownFrontmatter(skillContent) : '',
        files,
        metadataCount,
        fileBodies: {},
        expandedFiles: new Set<string>(),
      })
    } catch (err) {
      setWorkflowSkillsState({
        ...EMPTY_WORKFLOW_SKILLS_STATE,
        loading: false,
        path: skillPath,
        error: err instanceof Error ? err.message : 'Failed to load skills',
      })
    }
  }, [])

  useEffect(() => {
    if (reviewTab === 'skills') {
      loadWorkflowSkills(selectedWorkflow?.workspacePath)
    }
  }, [loadWorkflowSkills, reviewTab, selectedWorkflow?.workspacePath])

  const handleSelectWorkflow = useCallback((workflowPath: string, nextTab?: ReviewTab) => {
    setSelectedWorkflowId(workflowPath)
    setOrgView('workflow')
    if (nextTab) setReviewTab(nextTab)
  }, [])

  const executionCost = getTokenUsageTotal(reviewState.tokenUsage)
  const evaluationCost = getTokenUsageTotal(reviewState.evaluationTokenUsage)
  const totalKnownCost = (executionCost || 0) + (evaluationCost || 0)

  // Latest-run figures for the header chips. "Latest" = the run_folder the
  // workspace reports as most recent, with a fallback to the run whose cost
  // file has the newest updated_at if that folder isn't present in costs.
  const latestRunFolder = selectedWorkflow?.latestRunFolder || null
  const latestRunCost = useMemo(() => {
    if (reviewState.costRuns.length === 0) return null
    const sumCost = (usage: TokenUsageFile | null | undefined): number => {
      if (!usage) return 0
      let total = 0
      for (const m of Object.values(usage.by_model || {})) total += m.total_cost_usd || 0
      for (const t of Object.values(usage.by_tool || {})) total += t.total_cost_usd || 0
      return total
    }
    const pickTs = (run: WorkflowRunCostsEntry): number => {
      const stamps = [
        run.token_usage?.updated_at,
        run.evaluation_token_usage?.updated_at,
        run.token_usage?.created_at,
        run.evaluation_token_usage?.created_at,
      ].map(s => (s ? Date.parse(s) : 0))
      return Math.max(0, ...stamps)
    }
    const matched = latestRunFolder
      ? reviewState.costRuns.find(r => runFolderMatches(r.run_folder, latestRunFolder))
      : null
    const run = matched || [...reviewState.costRuns].sort((a, b) => pickTs(b) - pickTs(a))[0]
    if (!run) return null
    return sumCost(run.token_usage) + sumCost(run.evaluation_token_usage)
  }, [reviewState.costRuns, latestRunFolder])

  const metricById = useMemo(() => {
    return new Map(reviewState.metrics.map(metric => [metric.id, metric]))
  }, [reviewState.metrics])

  const latestMetricRows = useMemo(() => {
    const rows = reviewState.metricsHistory
    if (rows.length === 0) return []

    const matchingRunRows = latestRunFolder
      ? rows.filter(row => runFolderMatches(row.run_folder, latestRunFolder))
      : []
    const candidates = matchingRunRows.length > 0 ? matchingRunRows : rows
    const latestCompletedAt = candidates.reduce((latest, row) => row.completed_at > latest ? row.completed_at : latest, '')
    return candidates
      .filter(row => row.completed_at === latestCompletedAt)
      .sort((a, b) => {
        const rankDelta = metricRoleRank(metricById.get(a.metric_id)) - metricRoleRank(metricById.get(b.metric_id))
        if (rankDelta !== 0) return rankDelta
        const aLabel = metricById.get(a.metric_id)?.label || a.metric_id
        const bLabel = metricById.get(b.metric_id)?.label || b.metric_id
        return aLabel.localeCompare(bLabel)
      })
  }, [metricById, reviewState.metricsHistory, latestRunFolder])

  const latestMetricsSummary = useMemo<WorkflowMetricRunSummary | null>(() => {
    if (selectedWorkflow?.metricsSummary) return selectedWorkflow.metricsSummary
    if (latestMetricRows.length === 0) return null
    let withValue = 0
    let passed = 0
    let failed = 0
    let unknown = 0
    for (const row of latestMetricRows) {
      if (row.has_value) withValue++
      if (row.passed === true) passed++
      else if (row.passed === false) failed++
      else unknown++
    }
    return {
      total: latestMetricRows.length,
      with_value: withValue,
      passed,
      failed,
      unknown,
      rows: latestMetricRows,
    }
  }, [selectedWorkflow?.metricsSummary, latestMetricRows])

  // Cost trend: daily totals per day, split into execution / evaluation / phase
  // (phase = workflow-builder/planning/etc, not tied to a specific run).
  const costTrend = useMemo(() => {
    type Row = {
      date: string
      dateLabel: string
      ts: number
      total: number
      execution: number
      evaluation: number
      phase: number
      details: DailyCostDetail[]
      runKeys: Set<string>
    }
    const rowByDate = new Map<string, Row>()

    const sumCost = (usage: TokenUsageFile | PhaseTokenUsageFile | null | undefined): number => {
      if (!usage) return 0
      let total = 0
      for (const m of Object.values(usage.by_model || {})) {
        total += m.total_cost_usd || 0
      }
      if ('by_tool' in usage) {
        for (const t of Object.values(usage.by_tool || {})) {
          total += t.total_cost_usd || 0
        }
      }
      return total
    }

    const pickTimestamp = (usage: TokenUsageFile | PhaseTokenUsageFile | null | undefined): number | null => {
      const ts = usage?.updated_at || usage?.created_at
      if (!ts) return null
      const parsed = new Date(ts)
      return Number.isNaN(parsed.getTime()) ? null : parsed.getTime()
    }

    const parseDateKey = (key: string): number | null => {
      const parsed = new Date(`${key}T00:00:00Z`)
      return Number.isNaN(parsed.getTime()) ? null : parsed.getTime()
    }

    const bump = (
      ts: number | null,
      field: 'execution' | 'evaluation' | 'phase',
      amount: number,
      details: DailyCostDetail[] = [],
      runKey?: string
    ) => {
      if (ts === null || amount <= 0) return
      const d = new Date(ts)
      const date = d.toISOString().slice(0, 10)
      const dateLabel = d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
      const row = rowByDate.get(date) || {
        date,
        dateLabel,
        ts: d.getTime(),
        total: 0,
        execution: 0,
        evaluation: 0,
        phase: 0,
        details: [],
        runKeys: new Set<string>(),
      }
      row[field] += amount
      row.total += amount
      row.details.push(...details)
      if (runKey) row.runKeys.add(runKey)
      if (d.getTime() > row.ts) row.ts = d.getTime()
      rowByDate.set(date, row)
    }

    const runDailyEntriesByKey = new Map<string, WorkflowRunDailyCostsEntry>()
    for (const entry of reviewState.runDailyCosts) {
      runDailyEntriesByKey.set(`${entry.date}:${entry.scope}:${entry.run_folder}`, entry)
    }
    const runDailyEntries = Array.from(runDailyEntriesByKey.values())

    if (runDailyEntries.length > 0) {
      for (const entry of runDailyEntries) {
        const field = entry.scope === 'evaluation' ? 'evaluation' : 'execution'
        const ts = parseDateKey(entry.date) ?? pickTimestamp(entry.token_usage)
        const source = `${field === 'evaluation' ? 'Evaluation' : 'Execution'} · ${getRunFolderDisplayName(entry.run_folder)}`
        bump(
          ts,
          field,
          sumCost(entry.token_usage),
          buildRunDailyDetails(entry.token_usage, source, `${entry.date}:${entry.scope}:${entry.run_folder}`),
          `${entry.scope}:${entry.run_folder}`
        )
      }
    } else {
      for (const run of reviewState.costRuns) {
        const source = `Execution · ${getRunFolderDisplayName(run.run_folder)}`
        bump(
          pickTimestamp(run.token_usage),
          'execution',
          sumCost(run.token_usage),
          buildRunDailyDetails(run.token_usage, source, `run:${run.run_folder}:execution`),
          `execution:${run.run_folder}`
        )
        bump(
          pickTimestamp(run.evaluation_token_usage),
          'evaluation',
          sumCost(run.evaluation_token_usage),
          buildRunDailyDetails(run.evaluation_token_usage, `Evaluation · ${getRunFolderDisplayName(run.run_folder)}`, `run:${run.run_folder}:evaluation`),
          `evaluation:${run.run_folder}`
        )
      }
    }

    for (const entry of reviewState.phaseDailyCosts) {
      // The phase daily file's own date key is authoritative (e.g. "2026-04-21");
      // its token_usage timestamp may drift if the file was rewritten later.
      const ts = parseDateKey(entry.date) ?? pickTimestamp(entry.token_usage)
      bump(ts, 'phase', sumCost(entry.token_usage), buildPhaseDailyDetails(entry.token_usage, `phase:${entry.date}`))
    }

    const rows = Array.from(rowByDate.values())
      .map(row => ({
        ...row,
        details: row.details.sort((a, b) => b.cost - a.cost || a.source.localeCompare(b.source) || a.label.localeCompare(b.label)),
        runCount: row.runKeys.size,
      }))
      .sort((a, b) => a.date.localeCompare(b.date))
    return { rows }
  }, [reviewState.costRuns, reviewState.phaseDailyCosts, reviewState.runDailyCosts])

  const currentEvalEntry = useMemo(() => {
    return reviewState.evaluation
  }, [reviewState.evaluation])

  const currentEvalStepScores = useMemo(() => {
    return Array.isArray(currentEvalEntry?.report?.step_scores) ? currentEvalEntry.report.step_scores : []
  }, [currentEvalEntry])

  const evalStepDetailsById = useMemo(() => {
    return parseEvaluationPlanDetails(reviewState.reviewData?.evaluations?.evaluation_plan)
  }, [reviewState.reviewData?.evaluations?.evaluation_plan])

  const toggleEvalStep = useCallback((stepKey: string) => {
    setExpandedEvalSteps(prev => {
      const next = new Set(prev)
      if (next.has(stepKey)) next.delete(stepKey)
      else next.add(stepKey)
      return next
    })
  }, [])

  const toggleDailyCostDate = useCallback((date: string) => {
    setExpandedDailyCostDates(prev => {
      const next = new Set(prev)
      if (next.has(date)) next.delete(date)
      else next.add(date)
      return next
    })
  }, [])

  const toggleKnowledgebaseTopic = useCallback(async (topic: KBNotesTopic, workspacePath: string) => {
    const wasExpanded = knowledgebaseState.expanded.has(topic.id)
    setKnowledgebaseState(prev => {
      const nextExpanded = new Set(prev.expanded)
      if (nextExpanded.has(topic.id)) nextExpanded.delete(topic.id)
      else nextExpanded.add(topic.id)
      return { ...prev, expanded: nextExpanded }
    })

    if (!wasExpanded && knowledgebaseState.bodies[topic.id] === undefined) {
      const body = await readWorkspaceText(`${workspacePath}/knowledgebase/notes/${topic.file}`)
      setKnowledgebaseState(prev => ({ ...prev, bodies: { ...prev.bodies, [topic.id]: body } }))
    }
  }, [knowledgebaseState.bodies, knowledgebaseState.expanded])

  const toggleSkillReferenceFile = useCallback(async (file: SkillReferenceFile, workspacePath: string) => {
    const wasExpanded = workflowSkillsState.expandedFiles.has(file.relPath)
    setWorkflowSkillsState(prev => {
      const nextExpanded = new Set(prev.expandedFiles)
      if (nextExpanded.has(file.relPath)) nextExpanded.delete(file.relPath)
      else nextExpanded.add(file.relPath)
      return { ...prev, expandedFiles: nextExpanded }
    })

    if (!wasExpanded && workflowSkillsState.fileBodies[file.relPath] === undefined) {
      const body = await readWorkspaceText(resolveGlobalSkillFilePath(workspacePath, file.rawPath || file.relPath))
      setWorkflowSkillsState(prev => ({
        ...prev,
        fileBodies: { ...prev.fileBodies, [file.relPath]: body },
      }))
    }
  }, [workflowSkillsState.expandedFiles, workflowSkillsState.fileBodies])

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loader2 className="w-6 h-6 animate-spin text-indigo-500" />
      </div>
    )
  }

  return (
    <div className="space-y-3">
      <div className="grid gap-5 lg:grid-cols-[minmax(260px,320px)_minmax(0,1fr)] xl:grid-cols-[minmax(260px,320px)_minmax(768px,1fr)]">
          <div className="space-y-4">
            <div className="space-y-2">
              <h3 className="text-sm font-semibold text-foreground">Organization</h3>
              <div className="overflow-hidden rounded-2xl border border-border bg-card shadow-sm divide-y divide-border">
                <button
                  type="button"
                  onClick={() => setOrgView('dashboard')}
                  className={`flex w-full items-center gap-2 px-5 py-3 text-left transition-colors ${
                    orgView === 'dashboard'
                      ? 'border-l-2 border-l-primary bg-primary/10'
                      : 'border-l-2 border-l-transparent hover:bg-muted/40'
                  }`}
                >
                  <LayoutDashboard className="h-4 w-4 shrink-0 text-muted-foreground" />
                  <span className="truncate text-sm font-medium text-foreground">Org Dashboard</span>
                </button>
                <button
                  type="button"
                  onClick={() => setOrgView('goals')}
                  className={`flex w-full items-center gap-2 px-5 py-3 text-left transition-colors ${
                    orgView === 'goals'
                      ? 'border-l-2 border-l-primary bg-primary/10'
                      : 'border-l-2 border-l-transparent hover:bg-muted/40'
                  }`}
                >
                  <Target className="h-4 w-4 shrink-0 text-muted-foreground" />
                  <span className="truncate text-sm font-medium text-foreground">Org Goals</span>
                </button>
                <button
                  type="button"
                  onClick={() => setOrgView('pulse')}
                  className={`flex w-full items-center gap-2 px-5 py-3 text-left transition-colors ${
                    orgView === 'pulse'
                      ? 'border-l-2 border-l-primary bg-primary/10'
                      : 'border-l-2 border-l-transparent hover:bg-muted/40'
                  }`}
                >
                  <Activity className="h-4 w-4 shrink-0 text-muted-foreground" />
                  <span className="truncate text-sm font-medium text-foreground">Org Pulse</span>
                </button>
              </div>
            </div>

            {workflows.length === 0 ? (
              <div className="rounded-2xl border border-dashed border-border bg-card px-6 py-16 text-center">
                <FileText className="mx-auto mb-4 h-14 w-14 text-muted-foreground/60" />
                <p className="text-base font-medium text-foreground">No automations found.</p>
                <p className="mt-1 text-sm text-muted-foreground">Create a workflow to see it listed here.</p>
              </div>
            ) : (
              <div className="overflow-hidden rounded-2xl border border-border bg-card shadow-sm divide-y divide-border">
                {workflows.map(wf => {
                  const isSelected = selectedWorkflow?.workspacePath === wf.workspacePath
                  return (
                    <div
                      key={wf.workspacePath}
                      className={`px-5 py-3 cursor-pointer transition-colors ${
                        isSelected
                          ? 'border-l-2 border-l-primary bg-primary/10'
                          : 'border-l-2 border-l-transparent hover:bg-muted/40'
                      }`}
                      onClick={() => handleSelectWorkflow(wf.workspacePath)}
                    >
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2 min-w-0">
                          <StatusDot status={wf.latestStatus} />
                          <span className="truncate text-sm font-medium text-foreground">{wf.label}</span>
                          {isSelected && (
                            <span className="rounded-full bg-primary/15 px-1.5 py-0.5 text-[10px] font-medium text-primary">
                              Selected
                            </span>
                          )}
                        </div>
                        <div className="mt-1 flex flex-wrap items-center gap-2 text-[11px] text-muted-foreground">
                          {wf.latestRunFolder ? (
                            <span className="inline-flex items-center gap-1">
                              <FileText className="w-3 h-3" />
                              {wf.latestRunFolder}
                            </span>
                          ) : (
                            <span>No run yet</span>
                          )}
                          {wf.totalRuns > 0 && <span>{wf.totalRuns} runs</span>}
                          {wf.nextScheduleAt ? (
                            <span className="inline-flex items-center gap-1 text-warning">
                              <Calendar className="w-3 h-3" />
                              {formatScheduleTime(wf.nextScheduleAt)}
                            </span>
                          ) : wf.scheduleCount > 0 ? (
                            <span>{wf.scheduleCount} schedules</span>
                          ) : null}
                          {wf.lastActive && <span>{formatTimestamp(wf.lastActive)}</span>}
                        </div>
                      </div>
                    </div>
                  )
                })}
              </div>
            )}
          </div>

          <div className="lg:sticky lg:top-6 self-start">
            {orgView === 'dashboard' ? (
              <div className="h-[calc(100vh-160px)] min-h-[480px] overflow-auto rounded-2xl border border-border bg-card shadow-sm">
                <OrgDashboard workflows={workflows} />
              </div>
            ) : orgView === 'goals' ? (
              <div className="h-[calc(100vh-160px)] min-h-[480px] overflow-hidden rounded-2xl border border-border bg-card shadow-sm">
                <OrgGoalsPanel fixedDevice="desktop" />
              </div>
            ) : orgView === 'pulse' ? (
              <div className="h-[calc(100vh-160px)] min-h-[480px] overflow-hidden rounded-2xl border border-border bg-card shadow-sm">
                <OrgPulsePanel fixedDevice="desktop" />
              </div>
            ) : (
            <div className="overflow-hidden rounded-2xl border border-border bg-card shadow-sm">
              <div className="border-b border-border bg-muted/30 px-5 py-4">
                {selectedWorkflow ? (
                  <div className="flex flex-wrap items-center gap-2 text-[11px]">
                    <h4 className="min-w-0 max-w-[240px] truncate text-base font-semibold text-foreground">
                      {selectedWorkflow.label}
                    </h4>
                    <span className="text-muted-foreground">·</span>
                      {selectedWorkflow.latestRunFolder ? (
                        <span className="inline-flex items-center gap-1 rounded-full border border-border bg-background px-2 py-1 text-muted-foreground">
                          <FileText className="w-3 h-3" />
                          {selectedWorkflow.latestRunFolder}
                        </span>
                      ) : (
                        <span className="inline-flex items-center gap-1 rounded-full border border-border bg-background px-2 py-1 text-muted-foreground">
                          No runs yet
                        </span>
                      )}
                      {latestMetricsSummary && (
                        <span
                          className={`inline-flex items-center gap-1 rounded-full px-2 py-1 ${metricHealthClass(latestMetricsSummary)}`}
                          title={`${latestMetricsSummary.passed} passing, ${latestMetricsSummary.failed} failing, ${latestMetricsSummary.unknown} unknown`}
                        >
                          <BarChart3 className="w-3 h-3" />
                          Metrics {metricHealthText(latestMetricsSummary)}
                        </span>
                      )}
                      {latestRunCost !== null && latestRunCost > 0 && (
                        <span
                          className="inline-flex items-center gap-1 rounded-full bg-warning/15 px-2 py-1 text-warning"
                          title="Cost of the latest run (execution + evaluation)"
                        >
                          <DollarSign className="w-3 h-3" />
                          {formatUsd(latestRunCost)}
                        </span>
                      )}
                      {selectedWorkflow.nextScheduleAt && (
                        <span className="inline-flex items-center gap-1 rounded-full bg-primary/15 px-2 py-1 text-primary">
                          <Calendar className="w-3 h-3" />
                          {formatScheduleTime(selectedWorkflow.nextScheduleAt)}
                        </span>
                      )}
                      {selectedWorkflow.lastActive && (
                        <span className="inline-flex items-center gap-1 rounded-full border border-border bg-background px-2 py-1 text-muted-foreground">
                          <Clock className="w-3 h-3" />
                          {formatTimestamp(selectedWorkflow.lastActive)}
                        </span>
                      )}
                  </div>
                ) : (
                  <div>
                    <h4 className="text-base font-semibold text-foreground">Latest report</h4>
                    <p className="mt-1 text-xs text-muted-foreground">
                      Select an automation to review its report, metrics, and cost.
                    </p>
                  </div>
                )}
              </div>

              <div className="border-b border-border px-5 py-3">
                <div className="inline-flex items-center gap-1 rounded-xl bg-muted/60 p-1">
                  <ReviewTabButton active={reviewTab === 'report'} label="Report" onClick={() => setReviewTab('report')} />
                  <ReviewTabButton active={reviewTab === 'flow'} label="Flow" onClick={() => setReviewTab('flow')} />
                  <ReviewTabButton active={reviewTab === 'evaluation'} label="Metrics" onClick={() => setReviewTab('evaluation')} />
                  <ReviewTabButton active={reviewTab === 'cost'} label="Cost" onClick={() => setReviewTab('cost')} />
                  <ReviewTabButton active={reviewTab === 'soul'} label="Soul" onClick={() => setReviewTab('soul')} />
                  <ReviewTabButton active={reviewTab === 'skills'} label="Skills" onClick={() => setReviewTab('skills')} />
                  <ReviewTabButton active={reviewTab === 'knowledgebase'} label="Knowledgebase" onClick={() => setReviewTab('knowledgebase')} />
                  <ReviewTabButton active={reviewTab === 'logs'} label="Logs" onClick={() => setReviewTab('logs')} />
                  <ReviewTabButton active={reviewTab === 'config'} label="Config" onClick={() => setReviewTab('config')} />
                </div>
              </div>

              <div className="max-h-[calc(100vh-240px)] overflow-y-auto p-5">
                {!selectedWorkflow ? (
                  <div className="rounded-2xl border border-dashed border-border p-8 text-center text-sm text-muted-foreground">
                    Select an automation from the left to review its report, metrics, and cost.
                  </div>
                ) : reviewTab !== 'soul' && reviewTab !== 'skills' && reviewTab !== 'config' && reviewTab !== 'knowledgebase' && reviewTab !== 'logs' && reviewTab !== 'flow' && !selectedWorkflow.latestRunFolder ? (
                  <div className="rounded-2xl border border-dashed border-border p-8 text-center text-sm text-muted-foreground">
                    This automation has not produced a run yet, so there is no report, metrics, or cost data to review.
                  </div>
                ) : reviewTab !== 'soul' && reviewTab !== 'skills' && reviewTab !== 'config' && reviewTab !== 'knowledgebase' && reviewTab !== 'logs' && reviewTab !== 'flow' && reviewState.loading ? (
                  <div className="flex items-center justify-center py-16 text-sm text-muted-foreground">
                    <Loader2 className="mr-2 h-5 w-5 animate-spin text-cyan-500" />
                    Loading latest automation review data...
                  </div>
                ) : reviewTab === 'report' ? (
                  <div className="h-[calc(100vh-320px)] min-h-[400px]">
                    <ReportView workspacePath={selectedWorkflow.workspacePath} selectedRunFolder={selectedWorkflow.latestRunFolder} reviewData={reviewState.reviewData} />
                  </div>
                ) : reviewTab === 'flow' ? (
                  <div className="h-[calc(100vh-320px)] min-h-[520px] overflow-hidden rounded-xl border border-border bg-card">
                    <WorkflowCanvas
                      workspacePath={selectedWorkflow.workspacePath}
                      presetQueryId={selectedWorkflow.id}
                      viewMode="flow"
                      hideToolbar
                      readOnly
                      className="h-full"
                    />
                  </div>
                ) : reviewTab === 'evaluation' ? (
                  <div className="space-y-4">
                    <div className="grid gap-3 sm:grid-cols-3">
                      <div className="rounded-xl border border-border bg-muted/30 px-4 py-3">
                        <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Passing</div>
                        <div className="mt-2 text-2xl font-semibold text-foreground">{latestMetricsSummary?.passed ?? 0}</div>
                      </div>
                      <div className="rounded-xl border border-border bg-muted/30 px-4 py-3">
                        <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Failing</div>
                        <div className="mt-2 text-2xl font-semibold text-foreground">{latestMetricsSummary?.failed ?? 0}</div>
                      </div>
                      <div className="rounded-xl border border-border bg-muted/30 px-4 py-3">
                        <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Unknown</div>
                        <div className="mt-2 text-2xl font-semibold text-foreground">{latestMetricsSummary?.unknown ?? 0}</div>
                      </div>
                    </div>

                    {reviewState.metricsError && latestMetricRows.length === 0 && (
                      <div className="rounded-2xl border border-dashed border-border p-8 text-center text-sm text-muted-foreground">
                        {reviewState.metricsError}
                      </div>
                    )}

                    {!reviewState.metricsError && latestMetricRows.length === 0 && (
                      <div className="rounded-2xl border border-dashed border-border p-8 text-center text-sm text-muted-foreground">
                        No metric snapshot exists for the latest run yet.
                      </div>
                    )}

                    {/* Metric trends chart removed — per-metric latest values live in the
                        summary tiles and the metric table below; trajectory now lives in the
                        agent-curated workflow log. */}

                    {latestMetricRows.length > 0 && (
                      <div className="overflow-hidden rounded-xl border border-border bg-card">
                        <div className="grid grid-cols-[1fr_auto_auto] items-center gap-x-4 border-b border-border bg-muted/20 px-4 py-2 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
                          <div>Metric</div>
                          <div className="text-right">Value</div>
                          <div className="text-right">Target</div>
                        </div>
                        <div className="divide-y divide-border">
                          {latestMetricRows.map(row => {
                            const metric = metricById.get(row.metric_id)
                            const roleLabel = metricRoleLabel(metric)
                            return (
                              <div key={`${row.completed_at}-${row.metric_id}`} className="grid grid-cols-[1fr_auto_auto] items-center gap-x-4 px-4 py-2.5 text-sm">
                                <div className="min-w-0">
                                  <div className="flex min-w-0 flex-wrap items-center gap-2">
                                    <div className="truncate font-medium text-foreground">{metric?.label || row.metric_id}</div>
                                    {roleLabel && (
                                      <span className={`rounded-full border px-1.5 py-0.5 text-[10px] font-semibold uppercase leading-none ${metricRoleClass(roleLabel)}`}>
                                        {roleLabel}
                                      </span>
                                    )}
                                  </div>
                                  <div className="mt-0.5 flex items-center gap-2 text-[11px] text-muted-foreground">
                                    <span className="font-mono">{row.metric_id}</span>
                                    {row.resolve_error && <span className="truncate text-warning">{row.resolve_error}</span>}
                                  </div>
                                </div>
                                <div className={`text-right font-medium ${metricRowStatusClass(row)}`}>
                                  {row.has_value ? `${row.value}${metric?.unit ? ` ${metric.unit}` : ''}` : 'missing'}
                                </div>
                                <div className="text-right text-xs text-muted-foreground">
                                  {metricThresholdLabel(row)}
                                </div>
                              </div>
                            )
                          })}
                        </div>
                      </div>
                    )}

                    {currentEvalEntry && (
                      <div className="space-y-2">
                        <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                          Eval evidence ({currentEvalStepScores.length})
                        </div>
                        {currentEvalStepScores.length === 0 && (
                          <div className="rounded-xl border border-warning/40 bg-warning/10 px-4 py-3 text-sm text-warning">
                            This evaluation report has no step_scores. It may be from an older or incomplete eval run.
                          </div>
                        )}
                        {currentEvalStepScores.map((step, idx) => {
                          const stepKey = `${currentEvalEntry.run_folder}-${step.step_id}-${idx}`
                          const isExpanded = expandedEvalSteps.has(stepKey)
                          const outputText = formatStepOutputContent(step.output_content)
                          const showReasoning = Boolean(step.reasoning && !isFinalScoringPlaceholderText(step.reasoning))
                          const showEvidence = Boolean(step.evidence && !isFinalScoringPlaceholderText(step.evidence))
                          const stepDetails = evalStepDetailsById.get(step.step_id)
                          return (
                            <div key={stepKey} className="overflow-hidden rounded-xl border border-border bg-card">
                              <button
                                type="button"
                                onClick={() => toggleEvalStep(stepKey)}
                                className="flex w-full items-center gap-3 px-4 py-2.5 text-left transition-colors hover:bg-accent/50"
                              >
                                {isExpanded ? (
                                  <ChevronDown className="h-4 w-4 flex-shrink-0 text-muted-foreground" />
                                ) : (
                                  <ChevronRight className="h-4 w-4 flex-shrink-0 text-muted-foreground" />
                                )}
                                <div className="min-w-0 flex-1">
                                  <div className="flex items-center gap-2">
                                    <span className="rounded bg-muted px-1 py-0.5 font-mono text-[10px] text-muted-foreground">#{idx + 1}</span>
                                    <span className="truncate text-sm font-medium text-foreground">{stepDetails?.title || step.step_id}</span>
                                    {stepDetails?.title && (
                                      <span className="truncate font-mono text-[10px] text-muted-foreground">{step.step_id}</span>
                                    )}
                                  </div>
                                </div>
                              </button>
                              {isExpanded && (stepDetails?.description || hasStepOutputContent(step) || showReasoning || showEvidence) && (
                                <div className="space-y-3 border-t border-border bg-muted/20 px-4 py-3">
                                  {stepDetails?.description && (
                                    <div>
                                      <div className="mb-1 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">Description</div>
                                      <p className="whitespace-pre-wrap text-xs text-foreground">{stepDetails.description}</p>
                                    </div>
                                  )}
                                  {outputText && (
                                    <div>
                                      <div className="mb-1 flex items-center justify-between gap-2">
                                        <div className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">Output</div>
                                        {step.output_content?.file_path && (
                                          <span className="truncate font-mono text-[10px] text-muted-foreground">{step.output_content.file_path}</span>
                                        )}
                                      </div>
                                      <pre className="max-h-72 overflow-auto whitespace-pre-wrap rounded border border-border bg-background p-2 text-[11px]">
                                        {outputText}
                                      </pre>
                                    </div>
                                  )}
                                  {showReasoning && (
                                    <div>
                                      <div className="mb-1 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">Reasoning</div>
                                      <p className="whitespace-pre-wrap text-xs text-foreground">{step.reasoning}</p>
                                    </div>
                                  )}
                                  {showEvidence && (
                                    <div>
                                      <div className="mb-1 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">Evidence</div>
                                      <pre className="max-h-40 overflow-auto whitespace-pre-wrap rounded border border-border bg-background p-2 text-[11px]">
                                        {step.evidence}
                                      </pre>
                                    </div>
                                  )}
                                </div>
                              )}
                            </div>
                          )
                        })}
                      </div>
                    )}
                  </div>
                ) : reviewTab === 'soul' ? (
                  <div className="space-y-3">
                    {(() => {
                      const docState = soulDocState
                      return (
                        <>
                    <div className="flex flex-wrap items-center justify-between gap-3">
                      <div>
                        <h4 className="text-base font-semibold text-foreground">Soul</h4>
                        <p className="mt-1 font-mono text-[11px] text-muted-foreground">{docState.path}</p>
                      </div>
                      <button
                        type="button"
                        onClick={() => loadSoulDoc(selectedWorkflow.workspacePath)}
                        disabled={docState.loading}
                        className="inline-flex items-center gap-1.5 rounded-lg border border-border bg-background px-3 py-1.5 text-xs font-medium text-foreground transition-colors hover:bg-muted disabled:opacity-60"
                      >
                        {docState.loading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <FileText className="h-3.5 w-3.5" />}
                        <span>Refresh</span>
                      </button>
                    </div>

                    {docState.error && (
                      <div className="rounded-xl border border-destructive/30 bg-destructive/10 px-4 py-3 text-sm text-destructive">
                        {docState.error}
                      </div>
                    )}

                    {docState.loading && !docState.content ? (
                      <div className="flex items-center justify-center rounded-2xl border border-dashed border-border py-16 text-sm text-muted-foreground">
                        <Loader2 className="mr-2 h-5 w-5 animate-spin text-cyan-500" />
                        Loading soul.md...
                      </div>
                    ) : !docState.exists ? (
                      <div className="rounded-2xl border border-dashed border-border p-8 text-center text-sm text-muted-foreground">
                        No soul/soul.md exists for this automation yet.
                      </div>
                    ) : (
                      <div className="rounded-xl border border-border bg-card p-4">
                        <MarkdownRenderer
                          content={docState.content}
                          disablePathLinking
                          className="!text-[12px] leading-relaxed [&_p]:!text-[12px] [&_li]:!text-[12px] [&_h1]:!text-base [&_h2]:!text-sm [&_h3]:!text-xs [&_h1]:mt-3 [&_h2]:mt-3 [&_h3]:mt-2 [&_p]:my-1.5 [&_ul]:my-1.5 [&_ol]:my-1.5 [&_code]:!text-[11px] [&_pre]:!text-[11px]"
                        />
                      </div>
                    )}
                        </>
                      )
                    })()}
                  </div>
                ) : reviewTab === 'skills' ? (
                  <div className="space-y-3">
                    <div className="flex flex-wrap items-center justify-between gap-3">
                      <div>
                        <h4 className="text-base font-semibold text-foreground">Skills</h4>
                        <p className="mt-1 font-mono text-[11px] text-muted-foreground">{workflowSkillsState.path}</p>
                      </div>
                      <button
                        type="button"
                        onClick={() => loadWorkflowSkills(selectedWorkflow.workspacePath)}
                        disabled={workflowSkillsState.loading}
                        className="inline-flex items-center gap-1.5 rounded-lg border border-border bg-background px-3 py-1.5 text-xs font-medium text-foreground transition-colors hover:bg-muted disabled:opacity-60"
                      >
                        {workflowSkillsState.loading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
                        <span>Refresh</span>
                      </button>
                    </div>

                    <div className="flex flex-wrap items-center gap-4 rounded-xl border border-border bg-muted/30 px-4 py-3 text-sm">
                      <div>
                        <span className="text-muted-foreground">Global skill: </span>
                        <span className="font-medium text-foreground">{workflowSkillsState.exists ? 'Available' : 'Missing'}</span>
                      </div>
                        <div>
                        <span className="text-muted-foreground">Additional files: </span>
                        <span className="font-medium text-foreground">{workflowSkillsState.files.length}</span>
                      </div>
                      <div>
                        <span className="text-muted-foreground">Steps with learning metadata: </span>
                        <span className="font-medium text-foreground">{workflowSkillsState.metadataCount}</span>
                      </div>
                    </div>

                    {workflowSkillsState.error && (
                      <div className="flex items-start gap-2 rounded-xl border border-destructive/30 bg-destructive/10 px-4 py-3 text-sm text-destructive">
                        <AlertCircle className="mt-0.5 h-4 w-4 flex-shrink-0" />
                        <div>{workflowSkillsState.error}</div>
                      </div>
                    )}

                    {workflowSkillsState.loading && !workflowSkillsState.content ? (
                      <div className="flex items-center justify-center rounded-2xl border border-dashed border-border py-16 text-sm text-muted-foreground">
                        <Loader2 className="mr-2 h-5 w-5 animate-spin text-cyan-500" />
                        Loading learnings...
                      </div>
                    ) : !workflowSkillsState.exists ? (
                      <div className="rounded-2xl border border-dashed border-border p-8 text-center text-sm text-muted-foreground">
                        No global automation skill exists yet. Learnings appear at learnings/_global/SKILL.md after learning-enabled steps contribute reusable know-how.
                      </div>
                    ) : (
                      <div className="space-y-4">
                        <div className="rounded-xl border border-border bg-card p-4">
                          <MarkdownRenderer
                            content={workflowSkillsState.content}
                            basePath={workflowSkillsState.path}
                            className="!text-[12px] leading-relaxed [&_p]:!text-[12px] [&_li]:!text-[12px] [&_h1]:!text-base [&_h2]:!text-sm [&_h3]:!text-xs [&_h1]:mt-3 [&_h2]:mt-3 [&_h3]:mt-2 [&_p]:my-1.5 [&_ul]:my-1.5 [&_ol]:my-1.5 [&_code]:!text-[11px] [&_pre]:!text-[11px]"
                          />
                        </div>

                        {workflowSkillsState.files.length > 0 && (
                          <div className="space-y-2">
                            <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Additional files</div>
                            {workflowSkillsState.files.map(file => {
                              const isOpenFile = workflowSkillsState.expandedFiles.has(file.relPath)
                              const body = workflowSkillsState.fileBodies[file.relPath]
                              const isMarkdownFile = file.relPath.toLowerCase().endsWith('.md')
                              return (
                                <div key={file.relPath} className="overflow-hidden rounded-xl border border-border bg-card">
                                  <button
                                    type="button"
                                    onClick={() => toggleSkillReferenceFile(file, selectedWorkflow.workspacePath)}
                                    className="flex w-full items-center gap-2 px-4 py-2.5 text-left transition-colors hover:bg-muted/50"
                                  >
                                    {isOpenFile ? (
                                      <ChevronDown className="h-4 w-4 flex-shrink-0 text-muted-foreground" />
                                    ) : (
                                      <ChevronRight className="h-4 w-4 flex-shrink-0 text-muted-foreground" />
                                    )}
                                    <FileText className="h-3.5 w-3.5 flex-shrink-0 text-muted-foreground" />
                                    <span className="min-w-0 flex-1 truncate font-mono text-xs text-foreground">{file.relPath}</span>
                                  </button>
                                  {isOpenFile && (
                                    <div className="border-t border-border bg-muted/20 p-3">
                                      {body === undefined ? (
                                        <div className="flex items-center gap-2 text-xs text-muted-foreground">
                                          <Loader2 className="h-3 w-3 animate-spin" />
                                          Loading {file.relPath}...
                                        </div>
                                      ) : body === null ? (
                                        <div className="text-xs italic text-muted-foreground">File missing or empty.</div>
                                      ) : isMarkdownFile ? (
                                        <MarkdownRenderer
                                          content={body}
                                          disablePathLinking
                                          className="max-h-96 overflow-y-auto rounded border border-border/60 bg-background p-3 !text-sm [&_p]:!text-sm [&_li]:!text-sm [&_code]:!text-[12px]"
                                        />
                                      ) : (
                                        <pre className="max-h-96 overflow-y-auto whitespace-pre-wrap break-words rounded border border-border/60 bg-background p-3 font-mono text-xs text-foreground">
                                          {body}
                                        </pre>
                                      )}
                                    </div>
                                  )}
                                </div>
                              )
                            })}
                          </div>
                        )}
                      </div>
                    )}
                  </div>
                ) : reviewTab === 'config' ? (
                  <div className="space-y-3">
                    <div className="flex flex-wrap items-center justify-between gap-3">
                      <div>
                        <h4 className="text-base font-semibold text-foreground">Automation config</h4>
                        <p className="mt-1 font-mono text-[11px] text-muted-foreground">{workflowConfigState.path}</p>
                      </div>
                      <button
                        type="button"
                        onClick={() => loadWorkflowConfig(selectedWorkflow.workspacePath)}
                        disabled={workflowConfigState.loading}
                        className="inline-flex items-center gap-1.5 rounded-lg border border-border bg-background px-3 py-1.5 text-xs font-medium text-foreground transition-colors hover:bg-muted disabled:opacity-60"
                      >
                        {workflowConfigState.loading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
                        <span>Refresh</span>
                      </button>
                    </div>

                    {workflowConfigState.error && (
                      <div className="rounded-xl border border-destructive/30 bg-destructive/10 px-4 py-3 text-sm text-destructive">
                        {workflowConfigState.error}
                      </div>
                    )}

                    {workflowConfigState.loading && !workflowConfigState.content ? (
                      <div className="flex items-center justify-center rounded-2xl border border-dashed border-border py-16 text-sm text-muted-foreground">
                        <Loader2 className="mr-2 h-5 w-5 animate-spin text-cyan-500" />
                        Loading workflow.json...
                      </div>
                    ) : !workflowConfigState.exists ? (
                      <div className="rounded-2xl border border-dashed border-border p-8 text-center text-sm text-muted-foreground">
                        No workflow.json exists for this automation.
                      </div>
                    ) : (
                      <pre className="max-h-[calc(100vh-360px)] overflow-auto rounded-xl border border-border bg-card p-4 font-mono text-xs leading-relaxed text-foreground">
                        {workflowConfigState.content}
                      </pre>
                    )}
                  </div>
                ) : reviewTab === 'knowledgebase' ? (
                  <div className="space-y-3">
                    <div className="flex flex-wrap items-center justify-between gap-3">
                      <div>
                        <h4 className="text-base font-semibold text-foreground">Knowledgebase</h4>
                        <p className="mt-1 text-xs text-muted-foreground">
                          notes/ narrative topics
                        </p>
                      </div>
                      <button
                        type="button"
                        onClick={() => loadKnowledgebase(selectedWorkflow.workspacePath)}
                        disabled={knowledgebaseState.loading}
                        className="inline-flex items-center gap-1.5 rounded-lg border border-border bg-background px-3 py-1.5 text-xs font-medium text-foreground transition-colors hover:bg-muted disabled:opacity-60"
                      >
                        <RefreshCw className={`h-3.5 w-3.5 ${knowledgebaseState.loading ? 'animate-spin' : ''}`} />
                        <span>Refresh</span>
                      </button>
                    </div>

                    <div className="flex flex-wrap items-center gap-4 rounded-xl border border-border bg-muted/30 px-4 py-3 text-sm">
                      <div>
                        <span className="text-muted-foreground">Notes topics: </span>
                        <span className="font-medium text-foreground">{knowledgebaseState.index?.topics?.length ?? 0}</span>
                      </div>
                      {knowledgebaseState.index?.last_updated && (
                        <div className="text-xs text-muted-foreground">
                          Last updated: {new Date(knowledgebaseState.index.last_updated).toLocaleString()}
                          {knowledgebaseState.index.last_updated_by?.step ? ` · ${knowledgebaseState.index.last_updated_by.step}` : ''}
                          {knowledgebaseState.index.last_updated_by?.run ? ` / ${knowledgebaseState.index.last_updated_by.run}` : ''}
                        </div>
                      )}
                    </div>

                    {knowledgebaseState.loading && !knowledgebaseState.index && (
                      <div className="flex items-center justify-center rounded-2xl border border-dashed border-border py-16 text-sm text-muted-foreground">
                        <Loader2 className="mr-2 h-5 w-5 animate-spin text-cyan-500" />
                        Loading knowledgebase...
                      </div>
                    )}

                    {knowledgebaseState.error && (
                      <div className="flex items-start gap-2 rounded-xl border border-destructive/30 bg-destructive/10 px-4 py-3 text-sm text-destructive">
                        <AlertCircle className="mt-0.5 h-4 w-4 flex-shrink-0" />
                        <div>{knowledgebaseState.error}</div>
                      </div>
                    )}

                    {!knowledgebaseState.loading && !knowledgebaseState.error && (knowledgebaseState.index?.topics?.length ?? 0) === 0 && (
                      <div className="rounded-2xl border border-dashed border-border p-8 text-center text-sm text-muted-foreground">
                        <Database className="mx-auto mb-3 h-10 w-10 opacity-30" />
                        Knowledgebase is empty. Narrative topics appear after steps run with a knowledgebase contribution.
                      </div>
                    )}

                    {!knowledgebaseState.error && (knowledgebaseState.index?.topics?.length ?? 0) > 0 && (
                      <div className="space-y-2">
                        {(knowledgebaseState.index?.topics || []).map(topic => {
                          const isOpenTopic = knowledgebaseState.expanded.has(topic.id)
                          const body = knowledgebaseState.bodies[topic.id]
                          const isMarkdownFile = topic.file.toLowerCase().endsWith('.md')
                          return (
                            <div key={topic.id} className="overflow-hidden rounded-xl border border-border bg-card">
                              <button
                                type="button"
                                onClick={() => toggleKnowledgebaseTopic(topic, selectedWorkflow.workspacePath)}
                                className="flex w-full items-center gap-2 px-4 py-2.5 text-left transition-colors hover:bg-muted/50"
                              >
                                {isOpenTopic ? (
                                  <ChevronDown className="h-4 w-4 flex-shrink-0 text-muted-foreground" />
                                ) : (
                                  <ChevronRight className="h-4 w-4 flex-shrink-0 text-muted-foreground" />
                                )}
                                <FileText className="h-3.5 w-3.5 flex-shrink-0 text-muted-foreground" />
                                <span className="min-w-0 flex-1 truncate text-sm font-medium text-foreground">{topic.id}</span>
                                <span className="hidden font-mono text-xs text-muted-foreground sm:inline">{topic.file}</span>
                                {typeof topic.section_count === 'number' && (
                                  <span className="text-xs text-muted-foreground">{topic.section_count} section{topic.section_count === 1 ? '' : 's'}</span>
                                )}
                                {typeof topic.size_bytes === 'number' && (
                                  <span className="text-xs text-muted-foreground">{(topic.size_bytes / 1024).toFixed(1)}KB</span>
                                )}
                              </button>
                              {isOpenTopic && (
                                <div className="space-y-3 border-t border-border bg-muted/20 px-4 py-3 text-xs">
                                  {Array.isArray(topic.covers) && topic.covers.length > 0 && (
                                    <div>
                                      <span className="font-medium text-foreground">Covers: </span>
                                      <span className="font-mono text-muted-foreground">{topic.covers.join(', ')}</span>
                                    </div>
                                  )}
                                  {(topic.last_updated_by?.step || topic.last_updated_by?.run) && (
                                    <div>
                                      <span className="font-medium text-foreground">Last updated by: </span>
                                      <span className="font-mono text-muted-foreground">
                                        {topic.last_updated_by?.step ?? '?'} / {topic.last_updated_by?.run ?? '?'}
                                      </span>
                                    </div>
                                  )}
                                  {topic.last_updated && (
                                    <div className="text-muted-foreground">
                                      updated {new Date(topic.last_updated).toLocaleString()}
                                    </div>
                                  )}
                                  <div>
                                    <div className="mb-1 font-medium text-foreground">Content</div>
                                    {body === undefined ? (
                                      <div className="flex items-center gap-2 text-muted-foreground">
                                        <Loader2 className="h-3 w-3 animate-spin" />
                                        Loading {topic.file}...
                                      </div>
                                    ) : body === null ? (
                                      <div className="italic text-muted-foreground">Topic file missing or empty.</div>
                                    ) : isMarkdownFile ? (
                                      <div className="max-h-96 overflow-y-auto rounded border border-border/60 bg-background p-3 text-sm text-foreground">
                                        <MarkdownRenderer
                                          content={body}
                                          className="max-w-none !text-sm [&_p]:!text-sm [&_li]:!text-sm [&_code]:!text-[12px]"
                                        />
                                      </div>
                                    ) : (
                                      <pre className="max-h-96 overflow-y-auto whitespace-pre-wrap break-words rounded border border-border/60 bg-background p-2 text-foreground">
                                        {body}
                                      </pre>
                                    )}
                                  </div>
                                </div>
                              )}
                            </div>
                          )
                        })}
                      </div>
                    )}
                  </div>
                ) : reviewTab === 'logs' ? (
                  selectedWorkflow.latestRunFolder ? (
                    <ExecutionLogsPopup
                      isOpen
                      embedded
                      onClose={() => {}}
                      workspacePath={selectedWorkflow.workspacePath}
                      runFolder={selectedWorkflow.latestRunFolder}
                      runFolders={[selectedWorkflow.latestRunFolder]}
                    />
                  ) : (
                    <div className="rounded-2xl border border-dashed border-border p-8 text-center text-sm text-muted-foreground">
                      This automation has not produced a run yet, so there are no execution logs to review.
                    </div>
                  )
                ) : (
                  <div className="space-y-4">
                    {(() => {
                      const phaseCostTotal = costTrend.rows.reduce((sum, r) => sum + r.phase, 0)
                      const grandTotal = (totalKnownCost || 0) + phaseCostTotal
                      return (
                        <div className="rounded-xl border border-border bg-muted/30 px-4 py-3">
                          <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Total cost</div>
                          <div className="mt-2 text-2xl font-semibold text-foreground">{formatUsd(grandTotal > 0 ? grandTotal : null)}</div>
                          <div className="mt-1 text-xs text-muted-foreground">
                            {formatUsd(executionCost)} execution · {formatUsd(evaluationCost)} evaluation · {formatUsd(phaseCostTotal > 0 ? phaseCostTotal : null)} builder
                          </div>
                        </div>
                      )
                    })()}

                    {reviewState.costError && !reviewState.tokenUsage && !reviewState.evaluationTokenUsage && (
                      <div className="rounded-2xl border border-dashed border-border p-6 text-center text-sm text-muted-foreground">
                        {reviewState.costError}
                      </div>
                    )}

                    {!reviewState.costError && !reviewState.tokenUsage && !reviewState.evaluationTokenUsage && costTrend.rows.length === 0 && (
                      <div className="rounded-2xl border border-dashed border-border p-6 text-center text-sm text-muted-foreground">
                        No cost data has been recorded for the latest run yet.
                      </div>
                    )}

                    {costTrend.rows.length >= 1 && (
                      <div className="rounded-xl border border-border bg-card px-4 py-3">
                        <div className="mb-2 flex items-center justify-between">
                          <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Cost over time</div>
                          <div className="text-[11px] text-muted-foreground">
                            {costTrend.rows.length} day{costTrend.rows.length !== 1 ? 's' : ''}
                          </div>
                        </div>
                        <div className="h-52 w-full">
                          <ResponsiveContainer width="100%" height="100%">
                            <BarChart data={costTrend.rows} margin={{ top: 8, right: 12, left: -8, bottom: 0 }}>
                              <CartesianGrid strokeDasharray="3 3" stroke="currentColor" className="text-border" opacity={0.3} />
                              <XAxis dataKey="dateLabel" fontSize={11} tick={{ fill: 'currentColor' }} className="text-muted-foreground" />
                              <YAxis fontSize={11} tick={{ fill: 'currentColor' }} className="text-muted-foreground" tickFormatter={v => `$${v}`} />
                              <Tooltip
                                formatter={(value: unknown, name: unknown) => [
                                  typeof value === 'number' ? `$${value.toFixed(2)}` : String(value),
                                  String(name),
                                ]}
                                contentStyle={{ fontSize: 12, borderRadius: 6 }}
                              />
                              <Bar dataKey="total" name="Total" fill="#6366f1" />
                            </BarChart>
                          </ResponsiveContainer>
                        </div>
                      </div>
                    )}

                    {costTrend.rows.length >= 1 && (
                      <div className="overflow-hidden rounded-xl border border-border">
                        <div className="border-b border-border bg-muted/30 px-4 py-3">
                          <div className="text-sm font-medium text-foreground">Cost by day</div>
                        </div>
                        <div className="grid grid-cols-[auto_1fr_auto_auto_auto_auto] items-center gap-x-4 border-b border-border bg-muted/20 px-4 py-2 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
                          <div className="w-7"></div>
                          <div>Date</div>
                          <div className="text-right">Execution</div>
                          <div className="text-right">Evaluation</div>
                          <div className="text-right">Builder</div>
                          <div className="text-right">Total</div>
                        </div>
                        <div className="divide-y divide-border">
                          {[...costTrend.rows].reverse().map(row => {
                            const isExpanded = expandedDailyCostDates.has(row.date)
                            return (
                              <React.Fragment key={row.date}>
                                <div className="grid grid-cols-[auto_1fr_auto_auto_auto_auto] items-center gap-x-4 px-4 py-2.5 text-sm">
                                  <button
                                    type="button"
                                    onClick={() => toggleDailyCostDate(row.date)}
                                    className="inline-flex h-7 w-7 items-center justify-center rounded-md border border-border text-muted-foreground hover:bg-muted hover:text-foreground"
                                    title={isExpanded ? 'Hide daily cost details' : 'Show daily cost details'}
                                    aria-label={isExpanded ? 'Hide daily cost details' : 'Show daily cost details'}
                                    aria-expanded={isExpanded}
                                  >
                                    {isExpanded ? <Minus className="h-3.5 w-3.5" /> : <Plus className="h-3.5 w-3.5" />}
                                  </button>
                                  <div>
                                    <div className="text-foreground">{row.dateLabel}</div>
                                    {row.runCount > 0 && (
                                      <div className="text-[11px] text-muted-foreground">
                                        {row.runCount} run{row.runCount !== 1 ? 's' : ''}
                                      </div>
                                    )}
                                  </div>
                                  <div className="text-right text-muted-foreground">{row.execution > 0 ? formatUsd(row.execution) : '-'}</div>
                                  <div className="text-right text-muted-foreground">{row.evaluation > 0 ? formatUsd(row.evaluation) : '-'}</div>
                                  <div className="text-right text-muted-foreground">{row.phase > 0 ? formatUsd(row.phase) : '-'}</div>
                                  <div className="text-right font-medium text-foreground">{formatUsd(row.total)}</div>
                                </div>
                                {isExpanded && (
                                  <div className="bg-muted/20 px-4 py-3">
                                    {row.details.length === 0 ? (
                                      <div className="rounded-md border border-dashed border-border bg-background p-3 text-xs text-muted-foreground">
                                        No model or tool-level cost details were recorded for this date.
                                      </div>
                                    ) : (
                                      <div className="overflow-x-auto rounded-md border border-border bg-background">
                                        <table className="w-full text-xs">
                                          <thead>
                                            <tr className="border-b border-border bg-muted/40 text-muted-foreground">
                                              <th className="px-3 py-2 text-left font-medium">Source</th>
                                              <th className="px-3 py-2 text-left font-medium">Item</th>
                                              <th className="px-3 py-2 text-left font-medium">Type</th>
                                              <th className="px-3 py-2 text-left font-medium">Provider</th>
                                              <th className="px-3 py-2 text-right font-medium">Calls</th>
                                              <th className="px-3 py-2 text-right font-medium">Input</th>
                                              <th className="px-3 py-2 text-right font-medium">Cached</th>
                                              <th className="px-3 py-2 text-right font-medium">Output</th>
                                              <th className="px-3 py-2 text-right font-medium">Cost</th>
                                            </tr>
                                          </thead>
                                          <tbody className="divide-y divide-border">
                                            {row.details.map(detail => (
                                              <tr key={detail.key} className="hover:bg-muted/30">
                                                <td className="px-3 py-2 text-muted-foreground">{detail.source}</td>
                                                <td className="px-3 py-2">
                                                  <div className="font-medium text-foreground">{detail.label}</div>
                                                  <div className="font-mono text-[10px] text-muted-foreground">{detail.sublabel}</div>
                                                </td>
                                                <td className="px-3 py-2 text-muted-foreground">{detail.type}</td>
                                                <td className="px-3 py-2 font-mono text-muted-foreground">{detail.provider}</td>
                                                <td className="px-3 py-2 text-right font-mono text-muted-foreground">{formatCostCount(detail.calls)}</td>
                                                <td className="px-3 py-2 text-right font-mono text-muted-foreground">{formatCostCount(detail.inputTokens)}</td>
                                                <td className="px-3 py-2 text-right font-mono text-muted-foreground">{formatCostCount(detail.cachedTokens)}</td>
                                                <td className="px-3 py-2 text-right font-mono text-muted-foreground">{formatCostCount(detail.outputTokens)}</td>
                                                <td className="px-3 py-2 text-right font-semibold text-foreground">{formatUsdDetailed(detail.cost)}</td>
                                              </tr>
                                            ))}
                                          </tbody>
                                        </table>
                                      </div>
                                    )}
                                  </div>
                                )}
                              </React.Fragment>
                            )
                          })}
                        </div>
                      </div>
                    )}
                  </div>
                )}
              </div>
            </div>
            )}
          </div>
      </div>
    </div>
  )
}

function formatTimestamp(ts: string): string {
  try {
    const d = new Date(ts)
    const now = new Date()
    const diffMs = now.getTime() - d.getTime()
    const diffMin = Math.floor(diffMs / 60000)
    const diffHr = Math.floor(diffMs / 3600000)
    const diffDay = Math.floor(diffMs / 86400000)
    if (diffMin < 1) return 'just now'
    if (diffMin < 60) return `${diffMin}m ago`
    if (diffHr < 24) return `${diffHr}h ago`
    if (diffDay < 7) return `${diffDay}d ago`
    return d.toLocaleDateString()
  } catch {
    return ts
  }
}

function formatScheduleTime(ts: string): string {
  try {
    const d = new Date(ts)
    return d.toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
  } catch {
    return ts
  }
}
