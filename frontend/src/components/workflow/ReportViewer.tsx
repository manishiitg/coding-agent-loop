// Dynamic report viewer — parses reports/report_plan.json, fetches each widget's
// JSON source, and renders widgets.
// See docs/workflow/persistent_stores_design.md.

import { useEffect, useMemo, useState } from 'react'
import { TextWidget } from './reportWidgets/TextWidget'
import { MarkdownWidget } from './reportWidgets/MarkdownWidget'
import { StatWidget } from './reportWidgets/StatWidget'
import { AlertWidget } from './reportWidgets/AlertWidget'
import { CardsWidget } from './reportWidgets/CardsWidget'
import { TableWidget } from './reportWidgets/TableWidget'
import { PivotWidget } from './reportWidgets/PivotWidget'
import { ChartWidget } from './reportWidgets/ChartWidget'
import { CostsWidget } from './reportWidgets/CostsWidget'
import { EvalsWidget } from './reportWidgets/EvalsWidget'
import { RunsWidget } from './reportWidgets/RunsWidget'
import {
  StandaloneWidgetNotice,
  WidgetError,
  WidgetHeader,
  WidgetShell,
  WidgetVisibilityButton,
  normalizeSingularWidgetPath,
  resolveSingularWidgetSource,
  type SingularWidgetSourceResolution,
} from './reportWidgets/shared'
import { useCompactWidgetLayout } from './reportWidgets/tableHelpers'
import {
  parseTimestamp,
  type RunCostSummary,
  summariseRunCosts,
} from './reportWidgets/costSummaries'
import { BarChart3, ChevronDown, Monitor, RefreshCw, Smartphone } from 'lucide-react'
import { agentApi } from '../../services/api'
import {
  applyWidgetFilter,
  evaluateShowIf,
  parseReportPlan,
  resolveJSONPath,
} from '../../lib/reportPlanParser'
import ModalPortal from '../ui/ModalPortal'
import type {
  EvaluationReportsResponse,
  ModelTokenUsage,
  ParsedReportPlan,
  PhaseTokenUsageFile,
  PlannerFile,
  ReportEntry,
  ReportWidget,
  ReportWidgetKind,
  RunFoldersResponse,
  TokenUsageFile,
  WorkflowCostsResponse,
} from '../../services/api-types'

export const REPORT_PREVIEW_PREFERENCE_KEY = 'workflow_report_preview_preference'
export const REPORT_PREVIEW_PREFERENCE_CHANGED_EVENT = 'workflow-report-preview-preference-changed'

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
    <div
      className="relative h-full w-full flex flex-col overflow-hidden bg-gradient-to-b from-background via-background to-muted/20 text-foreground"
      data-report-theme={plan.theme || undefined}
    >
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
                {visibleSections.map(({ section, sectionIndex, entries }) => {
                  const gridColumns = section.layout?.columns
                  const gridGap = section.layout?.gap ?? 12
                  const containerClassName = gridColumns
                    ? 'grid'
                    : 'flex flex-col gap-3'
                  const containerStyle = gridColumns
                    ? {
                        gridTemplateColumns: `repeat(${gridColumns}, minmax(0, 1fr))`,
                        gap: `${gridGap}px`,
                      }
                    : undefined
                  return (
                    <section key={sectionIndex} className="flex flex-col gap-2.5 p-0 sm:gap-3 sm:rounded-2xl sm:border sm:border-border/50 sm:bg-card/55 sm:p-3.5 sm:shadow-sm">
                      <SectionHeader
                        heading={section.heading}
                      />
                      <div className={containerClassName} style={containerStyle}>
                        {entries.map(({ entry, entryIndex }) => {
                          const span = entry.kind === 'single'
                            ? entry.widget.layout?.span
                            : undefined
                          // Row entries always span the full grid; widgets within reflow via the row's own flex.
                          const cellSpan = gridColumns
                            ? entry.kind === 'row'
                              ? gridColumns
                              : Math.min(span ?? gridColumns, gridColumns)
                            : undefined
                          const cellMinWidth = entry.kind === 'single'
                            ? entry.widget.layout?.minWidth
                            : undefined
                          const cellStyle = gridColumns
                            ? {
                                gridColumn: `span ${cellSpan} / span ${cellSpan}`,
                                ...(cellMinWidth ? { minWidth: `${cellMinWidth}px` } : {}),
                              }
                            : undefined
                          const renderer = (
                            <EntryRenderer
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
                          )
                          return gridColumns ? (
                            <div key={entryIndex} style={cellStyle}>{renderer}</div>
                          ) : (
                            <div key={entryIndex}>{renderer}</div>
                          )
                        })}
                      </div>
                    </section>
                  )
                })}
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
