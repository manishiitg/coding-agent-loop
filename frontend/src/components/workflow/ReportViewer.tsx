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
import { useCompactWidgetLayout, useContainerSizeTier } from './reportWidgets/tableHelpers'
import {
  parseTimestamp,
  type RunCostSummary,
  summariseRunCosts,
} from './reportWidgets/costSummaries'
import { BarChart3, ChevronDown, Monitor, RefreshCw, Smartphone, Tablet } from 'lucide-react'
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
  ReportSection,
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
  // Three explicit preview widths plus 'auto'. The internal name 'desktop' is
  // surfaced as "Laptop" in the UI to match the user's mental model — laptop
  // viewports are what fill the full max-width shell. 'auto' falls back to the
  // mobilePreview prop, used when nothing has been selected yet.
  const [previewPreference, setPreviewPreference] = useState<'auto' | 'desktop' | 'tablet' | 'mobile'>(() => {
    try {
      const saved = localStorage.getItem(REPORT_PREVIEW_PREFERENCE_KEY)
      return saved === 'desktop' || saved === 'tablet' || saved === 'mobile' ? saved : 'auto'
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
  // Resolve 'auto' down to the concrete tier the layout would otherwise apply.
  // 'auto' picks 'mobile' when the layout is in mobile-preview mode, otherwise
  // 'desktop' (the laptop-class shell).
  const previewMode: 'desktop' | 'tablet' | 'mobile' = canUseSplitPreview
    ? previewPreference === 'auto'
      ? mobilePreview ? 'mobile' : 'desktop'
      : previewPreference
    : 'desktop'
  const isRefreshing = loading || costsLoading || evalsLoading || runsLoading
  // Per-mode shell width. Mobile mimics a phone (~480px), tablet mimics an
  // iPad-class device (~880px), laptop fills available space. Content width
  // mirrors the shell so widget reflow tests against the right container size.
  const previewShellClassName =
    previewMode === 'mobile'
      ? 'mx-auto w-full max-w-[480px] p-1.5 transition-all duration-200'
      : previewMode === 'tablet'
        ? 'mx-auto w-full max-w-[880px] p-1.5 transition-all duration-200'
        : 'mx-auto w-full max-w-full transition-all duration-200'
  const previewContentClassName =
    previewMode === 'mobile' || previewMode === 'tablet'
      ? 'w-full max-w-full'
      : 'mx-auto w-full max-w-5xl'

  // Three-state segmented control selects a preview mode directly. Falls back
  // to 'desktop' as the safe default when toggling out of any state.
  const setPreviewMode = (mode: 'desktop' | 'tablet' | 'mobile') => {
    setPreviewPreference(() => {
      try {
        localStorage.setItem(REPORT_PREVIEW_PREFERENCE_KEY, mode)
        window.dispatchEvent(new CustomEvent(REPORT_PREVIEW_PREFERENCE_CHANGED_EVENT, { detail: { preference: mode } }))
      } catch {
        // ignore
      }
      return mode
    })
  }

  // Collapsed-by-default preview controls — show only the active mode's icon
  // until the user hovers/focuses the cluster. JS state instead of CSS
  // group-hover so the expansion is predictable on touch devices and
  // tolerates Tailwind purge edge cases.
  const [previewControlsExpanded, setPreviewControlsExpanded] = useState(false)

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
                {visibleSections.map(({ section, sectionIndex, entries }) => (
                  <SectionContainer
                    key={sectionIndex}
                    section={section}
                    sectionIndex={sectionIndex}
                    entries={entries}
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
                    handleToggleWidgetHidden={handleToggleWidgetHidden}
                  />
                ))}
              </div>
            )}
          </div>
        </div>
      </div>

      <div className="absolute bottom-4 right-4 z-20 flex flex-col items-end gap-2 sm:bottom-5 sm:right-5">
        {canUseSplitPreview && (
          // Three-state preview-width control. Collapsed by default to just the
          // active-mode icon; expands on mouse-enter or focus to reveal the
          // other two. The cluster's outer padding gives the hover affordance
          // a comfortable target — pointer doesn't need to land on the icon.
          <div
            role="group"
            aria-label="Report preview width"
            onMouseEnter={() => setPreviewControlsExpanded(true)}
            onMouseLeave={() => setPreviewControlsExpanded(false)}
            onFocus={() => setPreviewControlsExpanded(true)}
            onBlur={(e) => {
              if (!e.currentTarget.contains(e.relatedTarget as Node)) {
                setPreviewControlsExpanded(false)
              }
            }}
            className="inline-flex items-center rounded-full border border-border/70 bg-background/95 p-0.5 shadow-lg backdrop-blur-sm focus-within:ring-1 focus-within:ring-ring"
          >
            {([
              { mode: 'mobile', Icon: Smartphone, label: 'Mobile preview (≈480px)' },
              { mode: 'tablet', Icon: Tablet, label: 'Tablet preview (≈880px)' },
              { mode: 'desktop', Icon: Monitor, label: 'Laptop preview (full width)' },
            ] as const).map(({ mode, Icon, label }) => {
              const active = previewMode === mode
              const visible = active || previewControlsExpanded
              return (
                <button
                  key={mode}
                  onClick={() => setPreviewMode(mode)}
                  className={`inline-flex h-8 items-center justify-center overflow-hidden rounded-full transition-[width,opacity] duration-150 ease-out ${
                    active
                      ? 'bg-muted text-foreground'
                      : 'text-muted-foreground hover:bg-muted/60 hover:text-foreground'
                  }`}
                  style={{
                    width: visible ? 32 : 0,
                    opacity: visible ? 1 : 0,
                    pointerEvents: visible ? 'auto' : 'none',
                  }}
                  title={label}
                  aria-label={label}
                  aria-pressed={active}
                >
                  <Icon className="h-3.5 w-3.5 flex-shrink-0" />
                </button>
              )
            })}
          </div>
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

// Renders one section's heading + entries. Lives in its own component so the
// section container can call useContainerSizeTier and collapse the grid into
// a tier-appropriate column count: 1 on phones (<640px), ~half on tablets
// (640–960px), full on desktop. A user-declared `columns: 12` therefore
// renders as 12 on desktop, 6 on tablet, 1 on mobile — and per-widget spans
// scale proportionally so a widget with span: 4 keeps roughly its share of
// the row at every tier.
function SectionContainer({
  section,
  sectionIndex,
  entries,
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
  handleToggleWidgetHidden,
}: {
  section: ReportSection
  sectionIndex: number
  entries: Array<{ entry: ReportEntry; entryIndex: number }>
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
  handleToggleWidgetHidden: (widgetKey: string) => void
}) {
  // Container size tier — phone / tablet / desktop, matching the project's
  // sm/md Tailwind breakpoints. Container-width based, so it works in
  // split-pane / mobile-preview modes where the report tab is narrower than
  // the actual viewport.
  const [gridRef, sizeTier] = useContainerSizeTier()
  const requestedColumns = section.layout?.columns
  const gridGap = section.layout?.gap ?? 12
  // Scale the user-requested column count to the active tier:
  //   phone   → 1 (always stack)
  //   tablet  → roughly half, rounded down, capped at 6 to keep cells legible
  //   desktop → as requested
  // Widget spans then scale by the same ratio so a widget keeps roughly its
  // declared share of a row at every tier (e.g. span: 4 of 12 cols stays at
  // span: 2 of 6 cols on tablet).
  const effectiveColumns = requestedColumns
    ? sizeTier === 'phone'
      ? 1
      : sizeTier === 'tablet'
        ? Math.min(6, Math.max(1, Math.floor(requestedColumns / 2)))
        : requestedColumns
    : undefined
  const tierSpan = (declared: number | undefined): number | undefined => {
    if (declared == null || !requestedColumns || !effectiveColumns) return undefined
    if (sizeTier === 'desktop') return Math.min(declared, effectiveColumns)
    // Scale by the same ratio columns were scaled, with a minimum of 1.
    const ratio = effectiveColumns / requestedColumns
    return Math.min(effectiveColumns, Math.max(1, Math.round(declared * ratio)))
  }
  const containerClassName = effectiveColumns
    ? 'grid'
    : 'flex flex-col gap-3'
  const containerStyle = effectiveColumns
    ? {
        gridTemplateColumns: `repeat(${effectiveColumns}, minmax(0, 1fr))`,
        gap: `${gridGap}px`,
      }
    : undefined
  return (
    <section className="flex flex-col gap-2.5 p-0 sm:gap-3 sm:rounded-2xl sm:border sm:border-border/50 sm:bg-card/55 sm:p-3.5 sm:shadow-sm">
      <SectionHeader heading={section.heading} />
      <div ref={gridRef} className={containerClassName} style={containerStyle}>
        {entries.map(({ entry, entryIndex }) => {
          const span = entry.kind === 'single'
            ? entry.widget.layout?.span
            : undefined
          // Row entries always span the full grid; widgets within reflow via the row's own flex.
          const cellSpan = effectiveColumns
            ? entry.kind === 'row'
              ? effectiveColumns
              : tierSpan(span) ?? effectiveColumns
            : undefined
          const cellMinWidth = entry.kind === 'single'
            ? entry.widget.layout?.minWidth
            : undefined
          const cellStyle = effectiveColumns
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
          return effectiveColumns ? (
            <div key={entryIndex} style={cellStyle}>{renderer}</div>
          ) : (
            <div key={entryIndex}>{renderer}</div>
          )
        })}
      </div>
    </section>
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
    <div ref={rowRef} className={`flex gap-2.5 ${isCompact ? 'flex-col' : 'flex-col md:flex-row md:flex-wrap'}`}>
      {visibleWidgets.map(({ widget, widgetKey, hidden }) => (
        <div key={widgetKey} className={`w-full ${isCompact ? '' : 'md:min-w-[260px] md:flex-1'}`}>
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
