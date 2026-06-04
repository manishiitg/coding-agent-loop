// Dynamic report viewer — parses reports/report_plan.json. Data widgets bind via
// `db` + `sql` and render from the read-only SQLite query endpoint; file/markdown
// widgets fetch their `source` file directly.
// See docs/workflow/persistent_stores_design.md.

import { memo, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { TextWidget } from './reportWidgets/TextWidget'
import { MarkdownWidget } from './reportWidgets/MarkdownWidget'
import { StatWidget } from './reportWidgets/StatWidget'
import { AlertWidget } from './reportWidgets/AlertWidget'
import { CardsWidget } from './reportWidgets/CardsWidget'
import { TableWidget } from './reportWidgets/TableWidget'
import { PivotWidget } from './reportWidgets/PivotWidget'
import { ChartWidget } from './reportWidgets/ChartWidget'
import { FileListWidget, FileWidget } from './reportWidgets/FileWidget'
import { FilePreviewModal } from './reportWidgets/FilePreviewModal'
import { ReportEmbedProvider, type ReportDataApi } from './reportWidgets/reportEmbedContext'
import {
  StandaloneWidgetNotice,
  WidgetError,
  WidgetShell,
  WidgetVisibilityButton,
  isDocumentWidget,
  isHtmlDocumentWidget,
  resolveSingularWidgetSource,
  type SingularWidgetSourceResolution,
} from './reportWidgets/shared'
import { useCompactWidgetLayout, useContainerSizeTier } from './reportWidgets/tableHelpers'
import { BarChart3, Check, ChevronDown, Download, Laptop, Loader2, RefreshCw, Smartphone, TabletSmartphone } from 'lucide-react'
import { agentApi, workspaceApi } from '../../services/api'
import { useReportFilePreviewStore } from '../../stores/useReportFilePreviewStore'
import { useChatStore } from '../../stores/useChatStore'
import {
  applyWidgetFilter,
  evaluateShowIf,
  parseReportPlan,
  resolveJSONPath,
} from '../../lib/reportPlanParser'
import ModalPortal from '../ui/ModalPortal'
import type {
  ParsedReportPlan,
  ReportEntry,
  ReportSection,
  ReportWidget,
  ReportWidgetKind,
} from '../../services/api-types'

export const REPORT_PREVIEW_PREFERENCE_KEY = 'workflow_report_preview_preference'
export const REPORT_PREVIEW_PREFERENCE_CHANGED_EVENT = 'workflow-report-preview-preference-changed'

// The device-width preview preference is PER WORKFLOW — scope the storage key by
// the workflow's workspacePath so a choice in one workflow doesn't leak to others.
// A workflow with no saved choice defaults to mobile.
export function reportPreviewPreferenceKey(scopeId?: string | null): string {
  return scopeId ? `${REPORT_PREVIEW_PREFERENCE_KEY}:${scopeId}` : REPORT_PREVIEW_PREFERENCE_KEY
}
const REPORT_SVG_EXPORT_SCALE = 2
const REPORT_PNG_EXPORT_SCALE = 1
const REPORT_PNG_EXPORT_MAX_SIDE = 16000
const REPORT_PNG_EXPORT_MAX_PIXELS = 64_000_000
type ReportExportFormat = 'svg' | 'png'

function utf8ToBase64(value: string): string {
  const bytes = new TextEncoder().encode(value)
  let binary = ''
  const chunkSize = 8192
  for (let index = 0; index < bytes.length; index += chunkSize) {
    const chunk = bytes.slice(index, index + chunkSize)
    binary += String.fromCharCode(...chunk)
  }
  return btoa(binary)
}

function dataUrlPayload(dataUrl: string): string {
  const commaIndex = dataUrl.indexOf(',')
  return commaIndex >= 0 ? dataUrl.slice(commaIndex + 1) : dataUrl
}

function inlineComputedStyles(source: Element, target: Element): void {
  if (target instanceof HTMLElement || target instanceof SVGElement) {
    const computed = window.getComputedStyle(source)
    for (const property of Array.from(computed)) {
      target.style.setProperty(property, computed.getPropertyValue(property), computed.getPropertyPriority(property))
    }
  }

  const sourceChildren = Array.from(source.children)
  const targetChildren = Array.from(target.children)
  sourceChildren.forEach((sourceChild, index) => {
    const targetChild = targetChildren[index]
    if (targetChild) inlineComputedStyles(sourceChild, targetChild)
  })
}

function triggerSvgDownload(dataUrl: string, filename: string): void {
  const link = document.createElement('a')
  link.href = dataUrl
  link.download = filename
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
}

async function saveReportImage(dataUrl: string, filename: string, format: ReportExportFormat): Promise<{ canceled?: boolean; filePath?: string } | null> {
  const electronAPI = (window as unknown as {
    electronAPI?: {
      saveFlowImage?: (payload: { filename: string; dataUrl: string; format: ReportExportFormat }) => Promise<{ canceled?: boolean; filePath?: string }>
    }
  }).electronAPI

  if (electronAPI?.saveFlowImage) {
    const payload = dataUrlPayload(dataUrl)
    if (format === 'png' && !payload.startsWith('iVBOR')) {
      throw new Error('PNG export payload was invalid. Reload the Electron window and try again.')
    }
    return electronAPI.saveFlowImage({ filename, dataUrl: payload, format })
  }

  triggerSvgDownload(dataUrl, filename)
  return null
}

function reportExportFilename(workspacePath: string, format: ReportExportFormat): string {
  const workflowName = workspacePath.split('/').filter(Boolean).pop() || 'workflow'
  const safeName = workflowName.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '') || 'workflow'
  const timestamp = new Date().toISOString().replace(/[:.]/g, '-')
  return `${safeName}-report-${timestamp}.${format}`
}

function renderReportElementToSvg(reportElement: HTMLElement): string {
  const width = Math.max(1, Math.ceil(reportElement.scrollWidth || reportElement.getBoundingClientRect().width))
  const height = Math.max(1, Math.ceil(reportElement.scrollHeight || reportElement.getBoundingClientRect().height))
  const exportWidth = width * REPORT_SVG_EXPORT_SCALE
  const exportHeight = height * REPORT_SVG_EXPORT_SCALE
  const clone = reportElement.cloneNode(true) as HTMLElement
  inlineComputedStyles(reportElement, clone)
  clone.setAttribute('xmlns', 'http://www.w3.org/1999/xhtml')
  clone.style.width = `${width}px`
  clone.style.minHeight = `${height}px`
  clone.style.margin = '0'

  const html = new XMLSerializer().serializeToString(clone)
  const svg = [
    `<svg xmlns="http://www.w3.org/2000/svg" width="${exportWidth}" height="${exportHeight}" viewBox="0 0 ${width} ${height}">`,
    `<foreignObject width="100%" height="100%">${html}</foreignObject>`,
    '</svg>',
  ].join('')
  return `data:image/svg+xml;base64,${utf8ToBase64(svg)}`
}

// HTML report widgets render inside a sandboxed `srcDoc` iframe, whose content is
// a SEPARATE document — serializing the outer report DOM (renderReportElementToSvg)
// produces an empty box where the iframe sits, so the PNG export comes out blank.
// The iframe is `allow-same-origin`, so we can reach its contentDocument and
// rasterize the INNER document directly.
//
// We serialize the WHOLE document (<html> incl. <head>), NOT a style-inlined <body>
// clone: the report's styling lives in <head><style> blocks, the injected theme
// tokens are a <style> node, and the light/dark state is the `.dark` class +
// CSS variables on <html>. Carrying the full document verbatim preserves all of
// that (stylesheets, custom properties, pseudo-elements, fonts); element-by-element
// computed-style inlining silently dropped it. Returns null if the frame isn't
// reachable, so the caller can fall back to the normal outer-DOM capture.
function renderIframeDocumentToSvg(iframe: HTMLIFrameElement): string | null {
  const doc = iframe.contentDocument
  const docEl = doc?.documentElement
  const body = doc?.body
  if (!doc || !docEl || !body) return null
  const width = Math.max(1, Math.ceil(docEl.scrollWidth || body.scrollWidth || iframe.clientWidth))
  const height = Math.max(1, Math.ceil(docEl.scrollHeight || body.scrollHeight || iframe.clientHeight))
  const exportWidth = width * REPORT_SVG_EXPORT_SCALE
  const exportHeight = height * REPORT_SVG_EXPORT_SCALE
  const clone = docEl.cloneNode(true) as HTMLElement
  clone.setAttribute('xmlns', 'http://www.w3.org/1999/xhtml')
  // A cloned <canvas> is blank — its drawn bitmap doesn't clone. Charting libs
  // (Chart.js etc.) draw to canvas, so snapshot each live canvas into an <img> at
  // the matching position in the clone. SVG/DOM charts clone fine and are untouched.
  const liveCanvases = doc.querySelectorAll('canvas')
  const cloneCanvases = clone.querySelectorAll('canvas')
  liveCanvases.forEach((live, i) => {
    const target = cloneCanvases[i]
    if (!target) return
    try {
      const png = (live as HTMLCanvasElement).toDataURL('image/png')
      const img = doc.createElement('img')
      img.setAttribute('src', png)
      img.setAttribute('width', String(live.clientWidth || (live as HTMLCanvasElement).width))
      img.setAttribute('height', String(live.clientHeight || (live as HTMLCanvasElement).height))
      img.setAttribute('style', target.getAttribute('style') || '')
      target.replaceWith(img)
    } catch {
      /* tainted canvas (cross-origin draw) — leave the blank clone */
    }
  })
  // Pin the document box to the measured content size so foreignObject lays it out
  // identically to the live frame (the iframe auto-sizes to content).
  clone.style.width = `${width}px`
  clone.style.height = `${height}px`
  clone.style.margin = '0'
  // Backstop background: <html>/<body> are often transparent over the iframe's
  // default white, which rasterizes to black on some platforms. Use the resolved
  // page background, falling back to the theme surface so the export is never black.
  const view = doc.defaultView || window
  const transparent = (c: string) => !c || c === 'transparent' || c === 'rgba(0, 0, 0, 0)'
  const rootBg = view.getComputedStyle(docEl).backgroundColor
  const bodyBg = view.getComputedStyle(body).backgroundColor
  const pageBg = !transparent(bodyBg) ? bodyBg : !transparent(rootBg) ? rootBg : ''
  const html = new XMLSerializer().serializeToString(clone)
  const svg = [
    `<svg xmlns="http://www.w3.org/2000/svg" width="${exportWidth}" height="${exportHeight}" viewBox="0 0 ${width} ${height}">`,
    pageBg ? `<rect width="100%" height="100%" fill="${pageBg}"/>` : '',
    `<foreignObject width="100%" height="100%">${html}</foreignObject>`,
    '</svg>',
  ].join('')
  return `data:image/svg+xml;base64,${utf8ToBase64(svg)}`
}

function svgDataUrlToPngDataUrl(svgDataUrl: string, scale = REPORT_PNG_EXPORT_SCALE): Promise<string> {
  return new Promise((resolve, reject) => {
    const image = new Image()
    image.onload = () => {
      const sourceWidth = Math.max(1, image.naturalWidth || image.width)
      const sourceHeight = Math.max(1, image.naturalHeight || image.height)
      const maxScale = Math.min(
        scale,
        REPORT_PNG_EXPORT_MAX_SIDE / sourceWidth,
        REPORT_PNG_EXPORT_MAX_SIDE / sourceHeight,
        Math.sqrt(REPORT_PNG_EXPORT_MAX_PIXELS / (sourceWidth * sourceHeight))
      )
      const safeScale = Math.max(0.1, maxScale)
      const canvas = document.createElement('canvas')
      canvas.width = Math.ceil(sourceWidth * safeScale)
      canvas.height = Math.ceil(sourceHeight * safeScale)
      const context = canvas.getContext('2d')
      if (!context) {
        reject(new Error('Could not create PNG export canvas'))
        return
      }
      context.scale(safeScale, safeScale)
      context.drawImage(image, 0, 0, sourceWidth, sourceHeight)
      const dataUrl = canvas.toDataURL('image/png')
      if (!dataUrl.startsWith('data:image/png;base64,')) {
        reject(new Error('Failed to create a valid PNG export'))
        return
      }
      resolve(dataUrl)
    }
    image.onerror = () => reject(new Error('Failed to render SVG export as PNG'))
    image.src = svgDataUrl
  })
}

// Convert "#rgb" / "#rrggbb" / "rgb(r,g,b)" / "hsl(h,s%,l%)" to a Tailwind-style
// "H S% L%" triplet. Tailwind's CSS variables expect that triplet so they can
// be wrapped in `hsl(var(--name))` — passing a full `hsl(...)` or hex string
// breaks every consumer. Returns null for unrecognized inputs so the caller
// can fall back to the named theme.
function hexToHslTriplet(input: string): string | null {
  const value = input.trim()
  if (!value) return null

  // Pass through if the author already wrote "H S% L%" (e.g. "200 70% 45%").
  if (/^\s*\d+(\.\d+)?\s+\d+(\.\d+)?%\s+\d+(\.\d+)?%\s*$/.test(value)) {
    return value.replace(/\s+/g, ' ').trim()
  }

  // Hex (#rgb or #rrggbb).
  let hex = value
  if (hex.startsWith('#')) hex = hex.slice(1)
  if (/^[0-9a-fA-F]{3}$/.test(hex)) {
    hex = hex.split('').map(c => c + c).join('')
  }
  if (!/^[0-9a-fA-F]{6}$/.test(hex)) return null

  const r = parseInt(hex.slice(0, 2), 16) / 255
  const g = parseInt(hex.slice(2, 4), 16) / 255
  const b = parseInt(hex.slice(4, 6), 16) / 255

  const max = Math.max(r, g, b)
  const min = Math.min(r, g, b)
  const l = (max + min) / 2
  let h = 0
  let s = 0
  if (max !== min) {
    const d = max - min
    s = l > 0.5 ? d / (2 - max - min) : d / (max + min)
    switch (max) {
      case r: h = (g - b) / d + (g < b ? 6 : 0); break
      case g: h = (b - r) / d + 2; break
      case b: h = (r - g) / d + 4; break
    }
    h /= 6
  }
  return `${Math.round(h * 360)} ${Math.round(s * 100)}% ${Math.round(l * 100)}%`
}

// Build the inline style object that injects custom theme colors as CSS
// variables. Each entry maps a themeColors field to the matching Tailwind
// variable name. The variables cascade to every descendant, so charts, cards,
// stats, and primary buttons all pick them up automatically. Returns undefined
// when there's nothing to inject so React doesn't churn on an empty object.
function buildThemeStyle(themeColors: ParsedReportPlan['themeColors']): React.CSSProperties | undefined {
  if (!themeColors) return undefined
  const entries: Array<[string, string]> = []
  const set = (name: string, value: string | undefined) => {
    if (!value) return
    const triplet = hexToHslTriplet(value)
    if (triplet) entries.push([name, triplet])
  }
  set('--primary', themeColors.primary)
  set('--accent', themeColors.accent)
  set('--card', themeColors.card)
  set('--muted', themeColors.muted)
  set('--border', themeColors.border)
  set('--ring', themeColors.primary) // Focus ring tracks primary by convention.
  if (themeColors.chart) {
    themeColors.chart.slice(0, 5).forEach((color, idx) => {
      set(`--chart-${idx + 1}`, color)
    })
  }
  if (entries.length === 0) return undefined
  return Object.fromEntries(entries) as React.CSSProperties
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

interface ReportViewerProps {
  workspacePath: string
  isOpen: boolean
  onClose: () => void
}

interface ReportViewProps {
  workspacePath: string
  selectedRunFolder?: string | null
  reviewData?: unknown
  /** Optional close/back handler; when omitted, no close button is rendered (used for canvas-mode). */
  onClose?: () => void
  // When the report pane is chat-focused it shrinks, so the parent caps the report
  // at a smaller tier than the user's saved device width: laptop→tablet, and the
  // narrowest case (saved mobile) → mobile. undefined = no chat-focus override.
  focusTier?: 'mobile' | 'tablet'
}

// Source content cached per workspace-relative path. `undefined` = not yet fetched;
// `null` = fetched and missing/malformed; otherwise the parsed JSON value.
type SourceCache = Record<string, unknown>

// SQL query result for a data widget. `undefined` = loading; `null` = error.
type WidgetSQLState =
  | { status: 'idle' }
  | { status: 'loading' }
  | { status: 'success'; rows: Record<string, unknown>[] }
  | { status: 'error'; message: string }

type WidgetSourceInput =
  | { status: 'ok'; value: unknown; label: string }
  | { status: 'loading'; label: string }
  | { status: 'missing'; label: string }

interface ReportDataSnapshot {
  planSource: string | null
  sources: SourceCache
}

const reportDataCache = new Map<string, ReportDataSnapshot>()
const reportDataPromises = new Map<string, Promise<ReportDataSnapshot>>()

// Per (db, sql) result cache so re-renders don't re-hit the endpoint.
const widgetSQLCache = new Map<string, Record<string, unknown>[]>()

function isDataWidget(widget: ReportWidget): boolean {
  return !!(widget.db && widget.sql)
}

// useWidgetSQLData runs a data widget's `sql` against its `db` via the read-only
// query endpoint and returns the rows. Pass `null` for non-data widgets (the
// hook then stays idle — hooks must be called unconditionally).
function useWidgetSQLData(widget: ReportWidget | null, workspacePath: string): WidgetSQLState {
  const db = widget?.db ?? ''
  const sql = widget?.sql ?? ''
  const [state, setState] = useState<WidgetSQLState>({ status: 'idle' })

  useEffect(() => {
    if (!db || !sql) {
      setState({ status: 'idle' })
      return
    }
    // db is workspace-relative to the workflow root (e.g. "db/db.sqlite").
    const dbPath = `${workspacePath}/${db}`
    const cacheKey = `${dbPath}::${sql}`
    const cached = widgetSQLCache.get(cacheKey)
    if (cached) {
      setState({ status: 'success', rows: cached })
      return
    }

    let cancelled = false
    setState({ status: 'loading' })
    agentApi
      .queryWorkflowDB(dbPath, sql)
      .then(res => {
        if (cancelled) return
        if (!res.success || !res.data) {
          setState({ status: 'error', message: res.error || 'Query failed.' })
          return
        }
        widgetSQLCache.set(cacheKey, res.data.rows)
        setState({ status: 'success', rows: res.data.rows })
      })
      .catch(error => {
        if (cancelled) return
        const message = error instanceof Error ? error.message : String(error)
        setState({ status: 'error', message: message || 'Query failed.' })
      })

    return () => {
      cancelled = true
    }
  }, [db, sql, workspacePath])

  return state
}

// File-path sources (for file/file-list/markdown widgets that point `source` at
// a file). Data widgets contribute none — they bind via db+sql.
function widgetSourcePaths(widget: ReportWidget): string[] {
  if (isDataWidget(widget)) return []
  return widget.source ? [widget.source] : []
}

function isFileArtifactWidget(widget: ReportWidget): boolean {
  return widget.kind === 'file' || widget.kind === 'file-list'
}

function collectReportSourcePaths(planSource: string | null): string[] {
  if (!planSource) return []
  const plan = parseReportPlan(planSource)
  const set = new Set<string>()
  for (const section of plan.sections) {
    for (const entry of section.entries) {
      if (entry.kind === 'single') {
        for (const source of widgetSourcePaths(entry.widget)) set.add(source)
      } else {
        for (const widget of entry.row.widgets) {
          for (const source of widgetSourcePaths(widget)) set.add(source)
        }
      }
    }
  }
  return Array.from(set).sort()
}

async function loadReportDataSnapshot(workspacePath: string, force = false): Promise<ReportDataSnapshot> {
  if (!force) {
    const cached = reportDataCache.get(workspacePath)
    if (cached) return cached
    const inFlight = reportDataPromises.get(workspacePath)
    if (inFlight) return inFlight
  }

  const promise = (async (): Promise<ReportDataSnapshot> => {
    const planSource = await readWorkspaceText(`${workspacePath}/reports/report_plan.json`)
    const paths = collectReportSourcePaths(planSource)
    const sourceEntries = await Promise.all(
      paths.map(async (path): Promise<readonly [string, unknown]> => {
        const content = await readWorkspaceText(`${workspacePath}/${path}`)
        if (content === null || content.trim() === '') return [path, null] as const
        // Raw-text sources (markdown / plain text) are not JSON — keep the file
        // content verbatim so a `markdown` widget can point `source` directly at
        // a .md file in the workspace and render it inline (no `path` needed).
        // resolveJSONPath returns a bare string unchanged when no path is set,
        // and relative links inside the file resolve against basePath=source.
        if (/\.(md|markdown|txt)$/i.test(path)) return [path, content] as const
        try {
          return [path, JSON.parse(content)] as const
        } catch {
          return [path, null] as const
        }
      })
    )
    const snapshot: ReportDataSnapshot = {
      planSource,
      sources: Object.fromEntries(sourceEntries),
    }
    reportDataCache.set(workspacePath, snapshot)
    return snapshot
  })()

  reportDataPromises.set(workspacePath, promise)
  try {
    return await promise
  } finally {
    reportDataPromises.delete(workspacePath)
  }
}

function widgetSourceLabel(widget: ReportWidget): string {
  return widget.source ?? ''
}

// Resolve a file/markdown widget's `source` file from the prefetched cache.
function resolveWidgetSourceInput(widget: ReportWidget, sources: SourceCache): WidgetSourceInput {
  const label = widgetSourceLabel(widget)
  if (!widget.source) return { status: 'missing', label }
  const raw = sources[widget.source]
  if (raw === undefined) return { status: 'loading', label }
  if (raw === null) return { status: 'missing', label }
  return { status: 'ok', value: raw, label }
}

function widgetRawForVisibility(widget: ReportWidget, sources: SourceCache): unknown {
  const input = resolveWidgetSourceInput(widget, sources)
  if (input.status === 'ok') return input.value
  if (input.status === 'loading') return undefined
  return null
}

function widgetInstanceKey(
  widget: ReportWidget,
  ids: { sectionIndex: number; entryIndex: number; widgetIndex: number },
) {
  return [
    ids.sectionIndex,
    ids.entryIndex,
    ids.widgetIndex,
    widget.kind,
    widget.source ?? '',
    widget.db ?? '',
    widget.sql ?? '',
    widget.path ?? '',
    widget.title ?? '',
  ].join('::')
}

function widgetShouldRender(widget: ReportWidget, raw: unknown) {
  if (widget.hidden) return false
  if (isFileArtifactWidget(widget)) return Boolean(widget.source)
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
function ReportViewComponent({ workspacePath, selectedRunFolder, reviewData, onClose, focusTier }: ReportViewProps) {
  // Three explicit preview widths plus 'auto'. The internal name 'desktop' is
  // surfaced as "Laptop" in the UI to match the user's mental model — laptop
  // viewports are what fill the full max-width shell. 'auto' falls back to the
  // mobilePreview prop, used when nothing has been selected yet.
  const [previewPreference, setPreviewPreference] = useState<'auto' | 'desktop' | 'tablet' | 'mobile'>(() => {
    try {
      const saved = localStorage.getItem(reportPreviewPreferenceKey(workspacePath))
      return saved === 'desktop' || saved === 'tablet' || saved === 'mobile' ? saved : 'mobile'
    } catch {
      return 'mobile'
    }
  })
  // Re-read the per-workflow preference when the workflow (workspacePath) changes,
  // since this component can be reused across workflows without remounting.
  useEffect(() => {
    try {
      const saved = localStorage.getItem(reportPreviewPreferenceKey(workspacePath))
      setPreviewPreference(saved === 'desktop' || saved === 'tablet' || saved === 'mobile' ? saved : 'mobile')
    } catch {
      setPreviewPreference('mobile')
    }
  }, [workspacePath])
  const [loading, setLoading] = useState(false)
  const initialSnapshot = reportDataCache.get(workspacePath)
  const [planSource, setPlanSource] = useState<string | null>(initialSnapshot?.planSource ?? null)
  const [sources, setSources] = useState<SourceCache>(() => initialSnapshot?.sources ?? {})
  const [error, setError] = useState<string | null>(null)
  const [refreshNonce, setRefreshNonce] = useState(0)
  const [initialLoadDeferred, setInitialLoadDeferred] = useState(false)
  const [hiddenWidgetKeys, setHiddenWidgetKeys] = useState<Set<string>>(() => new Set())
  const [isExportingReport, setIsExportingReport] = useState(false)
  const reportExportRef = useRef<HTMLDivElement>(null)
  const refreshWorkspaceRef = useRef<string | null>(null)

  const plan: ParsedReportPlan = useMemo(() => {
    if (!planSource) return { sections: [] }
    return parseReportPlan(planSource)
  }, [planSource])

  useEffect(() => {
    if (!workspacePath) return
    const isExplicitRefreshForWorkspace = refreshNonce > 0 && refreshWorkspaceRef.current === workspacePath
    const cached = !isExplicitRefreshForWorkspace ? reportDataCache.get(workspacePath) : undefined
    if (cached) {
      setInitialLoadDeferred(false)
      setPlanSource(cached.planSource)
      setSources(cached.sources)
      setLoading(false)
      setError(null)
      return
    }

    // Previously the workflow split view deferred initial report load to
    // avoid parsing/rendering large JSON on the main thread during
    // workflow switches — the user had to click "Load report" manually.
    // That created a UX where opening the Report tab still showed an
    // empty pane. Now we always auto-load on mount; if perf regressions
    // surface from heavy report data, revisit with a worker-side parse.
    let cancelled = false
    setInitialLoadDeferred(false)
    setLoading(true)
    setError(null)

    // Debounce the heavy fetch+parse so flicking through workflows (Ctrl+K /
    // header pills) only loads the report you actually LAND on — not every one
    // you pass through. The skeleton shows immediately; cached workflows above
    // skip this entirely and stay instant. An explicit refresh skips the wait.
    const loadDelayMs = isExplicitRefreshForWorkspace ? 0 : 250
    const loadTimer = setTimeout(() => {
      if (cancelled) return
      loadReportDataSnapshot(workspacePath, isExplicitRefreshForWorkspace)
        .then(snapshot => {
          if (cancelled) return
          setPlanSource(snapshot.planSource)
          setSources(snapshot.sources)
        })
        .catch(error => {
          if (cancelled) return
          const message = error instanceof Error ? error.message : String(error)
          setError(message || 'Failed to load report.')
          setPlanSource(null)
          setSources({})
        })
        .finally(() => {
          if (!cancelled) setLoading(false)
        })
    }, loadDelayMs)

    return () => {
      cancelled = true
      clearTimeout(loadTimer)
    }
  }, [workspacePath, refreshNonce, focusTier])

  useEffect(() => {
    setHiddenWidgetKeys(new Set())
  }, [workspacePath, planSource, refreshNonce])

  const handleRefresh = () => {
    setInitialLoadDeferred(false)
    setError(null)
    setSources({})
    refreshWorkspaceRef.current = workspacePath
    setRefreshNonce(prev => prev + 1)
  }

  const handleExportReport = async (format: ReportExportFormat) => {
    const target = reportExportRef.current
    if (!target) return
    setIsExportingReport(true)
    try {
      const filename = reportExportFilename(workspacePath, format)
      // For an HTML-document report the content is a same-origin srcDoc iframe that
      // the outer-DOM capture can't see — rasterize the iframe's own document so the
      // export isn't blank. Fall back to the normal capture if it isn't reachable.
      let svgDataUrl: string | null = null
      if (htmlOnlyReport) {
        const iframe = target.querySelector('iframe')
        if (iframe) svgDataUrl = renderIframeDocumentToSvg(iframe)
      }
      if (!svgDataUrl) svgDataUrl = renderReportElementToSvg(target)
      const dataUrl = format === 'png' ? await svgDataUrlToPngDataUrl(svgDataUrl) : svgDataUrl
      const result = await saveReportImage(dataUrl, filename, format)
      if (result?.canceled) return
      const location = result?.filePath ? ` to ${result.filePath}` : ''
      useChatStore.getState().addToast(`Exported report as ${format.toUpperCase()}${location}`, 'success')
    } catch (error) {
      console.error('[ReportView] Failed to export report:', error)
      useChatStore.getState().addToast(error instanceof Error ? error.message : 'Failed to export report', 'error')
    } finally {
      setIsExportingReport(false)
    }
  }

  // The shared on-pane toolbar (PreviewPaneControls) triggers report export by
  // dispatching this window event (string matches WORKFLOW_REPORT_EXPORT_EVENT
  // in WorkflowCanvas). A ref keeps the latest handler without re-subscribing.
  const exportReportRef = useRef(handleExportReport)
  exportReportRef.current = handleExportReport
  const refreshReportRef = useRef(handleRefresh)
  refreshReportRef.current = handleRefresh
  useEffect(() => {
    const onExport = () => { void exportReportRef.current('png') }
    const onRefresh = () => { void refreshReportRef.current() }
    const onPref = (e: Event) => {
      const detail = (e as CustomEvent).detail
      // Only react to changes for THIS workflow (scoped per workspacePath).
      if ((detail?.scopeId ?? null) !== (workspacePath ?? null)) return
      const p = detail?.preference
      if (p === 'mobile' || p === 'tablet' || p === 'desktop' || p === 'auto') setPreviewPreference(p)
    }
    window.addEventListener('workflow-report-export-requested', onExport)
    window.addEventListener('workflow-report-refresh-requested', onRefresh)
    window.addEventListener(REPORT_PREVIEW_PREFERENCE_CHANGED_EVENT, onPref)
    return () => {
      window.removeEventListener('workflow-report-export-requested', onExport)
      window.removeEventListener('workflow-report-refresh-requested', onRefresh)
      window.removeEventListener(REPORT_PREVIEW_PREFERENCE_CHANGED_EVENT, onPref)
    }
  }, [workspacePath])

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
          return widgetShouldRender(widget, widgetRawForVisibility(widget, sources))
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
          if (widgetShouldRender(w, widgetRawForVisibility(w, sources))) return true
        }
      }
    }
    return false
  }, [planExists, plan, sources, hiddenWidgetKeys])
  // Report renders at the selected device width. When the pane is chat-focused the
  // parent passes a capped focusTier (laptop→tablet, mobile→mobile) so the report
  // fits the narrower pane; otherwise it follows the user's saved preference.
  // Device selection lives in the shared on-pane bar (PreviewPaneControls).
  const previewMode: 'desktop' | 'tablet' | 'mobile' = focusTier
    ? focusTier
    : (previewPreference === 'mobile' || previewPreference === 'tablet' || previewPreference === 'desktop'
        ? previewPreference
        : 'desktop')
  // Per-mode shell width. Mobile mimics a phone (~480px), tablet mimics an
  // iPad-class device (~880px), laptop fills available space. Content width
  // mirrors the shell so widget reflow tests against the right container size.
  const previewShellClassName =
    previewMode === 'mobile'
      ? 'mx-auto w-full max-w-[480px] p-1.5 transition-all duration-200'
      : previewMode === 'tablet'
        ? 'mx-auto w-full max-w-[880px] p-1.5 transition-all duration-200'
        : 'mx-auto w-full max-w-full transition-all duration-200'
  // A report made only of self-contained documents (md/html) renders edge-to-edge:
  // each document owns its own width / padding / background, so we add no
  // content-width cap, no scroll padding, and no card chrome around it (avoids
  // double margins/frames). This covers a single document AND a tabbed/multi-entry
  // section of documents (e.g. per-PAN report tabs) — every visible entry must be a
  // single document widget for the report to count as document-only.
  const documentWidgets = useMemo(() => {
    const all: ReportWidget[] = []
    for (const { entries } of visibleSections) {
      for (const { entry } of entries) {
        if (entry.kind !== 'single' || !isDocumentWidget(entry.widget)) return null
        all.push(entry.widget)
      }
    }
    return all.length > 0 ? all : null
  }, [visibleSections])
  const documentOnlyReport = documentWidgets != null
  // HTML documents render in iframes that own their full width/scroll; when EVERY
  // document is HTML the report goes full-width with no reserved scrollbar gutter.
  // (Mixed md+html keeps the readable content width so markdown stays legible.)
  const htmlOnlyReport = documentWidgets != null && documentWidgets.every(isHtmlDocumentWidget)

  const previewContentClassName =
    previewMode === 'mobile' || previewMode === 'tablet'
      ? 'w-full max-w-full'
      : htmlOnlyReport
        ? 'w-full max-w-full'
        : 'mx-auto w-full max-w-5xl'


  // Inline custom palette → CSS variables on the report root. Hex values get
  // converted to "H S% L%" triplets because Tailwind variables are HSL-shaped
  // (`hsl(var(--primary))`). Anything we don't override falls through to the
  // named theme block and ultimately the workspace defaults.
  const themeStyle = useMemo(() => buildThemeStyle(plan.themeColors), [plan.themeColors])

  // Renderer for ```report-widget blocks embedded in markdown documents — gives
  // them the live, db-bound WidgetCard with the report's loaded sources.
  const renderEmbeddedWidget = useCallback(
    (spec: unknown) => (
      // Wrap embeds in a themed app surface. Critical for HTML embeds, which
      // mount in a separate iframe document outside the report's themed
      // container: this carries the report theme + CSS variables, sets the app
      // base surface/typography (bg-background / text-foreground / font-sans),
      // and isolates the widget from the host HTML's inherited fonts and colors
      // (otherwise stats/tables inherit the document's styling and look wrong).
      <div
        className="report-embed-root rounded-lg bg-background p-2 text-foreground font-sans"
        data-report-theme={plan.theme || undefined}
        style={themeStyle}
      >
        <EmbeddedReportWidget spec={spec} workspacePath={workspacePath} baseSources={sources} />
      </div>
    ),
    [workspacePath, sources, plan.theme, themeStyle],
  )

  // Live data API exposed to HTML report documents as `window.report`. HTML
  // renders its own visuals; we just deliver data. `query` runs read-only SQL
  // against db/db.sqlite (the primary data source); `get`/`getText` are scoped to
  // db/ knowledgebase/ docs/ (same as file widgets) for markdown/text/assets so a
  // report can't read arbitrary workspace files.
  const dataApi = useMemo<ReportDataApi>(() => {
    const query = async (sql: string): Promise<Record<string, unknown>[]> => {
      const res = await agentApi.queryWorkflowDB(`${workspacePath}/db/db.sqlite`, sql)
      if (!res.success || !res.data) throw new Error(res.error || 'Query failed.')
      return res.data.rows
    }
    const allowed = (p: string): string => {
      const n = p.replace(/\\/g, '/').replace(/^\/+/, '')
      if (!n || n.split('/').includes('..')) return ''
      return n.startsWith('db/') || n.startsWith('knowledgebase/') || n.startsWith('docs/') ? n : ''
    }
    const getText = async (path: string): Promise<string | null> => {
      const n = allowed(path)
      if (!n) return null
      return readWorkspaceText(`${workspacePath}/${n}`)
    }
    const get = async (path: string): Promise<unknown> => {
      const text = await getText(path)
      if (text == null || text.trim() === '') return null
      try {
        return JSON.parse(text)
      } catch {
        return text
      }
    }
    const fileUrl = async (path: string): Promise<string | null> => {
      const n = allowed(path)
      if (!n) return null
      try {
        const response = await workspaceApi.get(`/api/documents/${encodeURIComponent(`${workspacePath}/${n}`)}`, {
          params: { download: 'true' },
          responseType: 'blob',
          headers: { Accept: 'application/octet-stream' },
          transformResponse: [(d) => d],
        })
        const blob = response.data instanceof Blob ? response.data : new Blob([response.data])
        return URL.createObjectURL(blob)
      } catch {
        return null
      }
    }
    const openFile = (path: string): void => {
      const n = allowed(path)
      if (!n) return
      useReportFilePreviewStore.getState().show({ path: `${workspacePath}/${n}` })
    }
    return { workspacePath, query, get, getText, fileUrl, openFile }
  }, [workspacePath])

  const reportRuntime = useMemo(
    () => ({ renderEmbeddedWidget, data: dataApi }),
    [renderEmbeddedWidget, dataApi],
  )

  return (
    <ReportEmbedProvider value={reportRuntime}>
    <div
      className="relative h-full w-full flex flex-col overflow-hidden bg-gradient-to-b from-background via-background to-muted/20 text-foreground"
      data-report-theme={plan.theme || undefined}
      style={themeStyle}
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

      <div className={`min-h-0 flex-1 overflow-y-auto overscroll-y-contain ${htmlOnlyReport ? '' : '[scrollbar-gutter:stable]'} ${documentOnlyReport ? '' : 'px-2 py-2 sm:px-3 sm:py-3'}`}>
        <div ref={reportExportRef} className={previewShellClassName}>
          <div className={`flex flex-col gap-3 ${previewContentClassName}`}>
            {loading && <ReportSkeleton />}
            {error && <div className="text-destructive">Failed to load report: {error}</div>}

            {!loading && !error && initialLoadDeferred && (
              <div className="flex flex-col items-center justify-center gap-3 rounded-2xl border border-dashed border-border/70 bg-card/70 px-4 py-8 text-center shadow-sm sm:px-6 sm:py-10">
                <div className="flex h-12 w-12 items-center justify-center rounded-2xl border border-primary/20 bg-primary/10 text-primary sm:h-14 sm:w-14">
                  <BarChart3 className="h-6 w-6" />
                </div>
                <div className="space-y-1">
                  <div className="text-base font-semibold text-foreground">Report not loaded</div>
                  <div className="text-xs uppercase tracking-[0.22em] text-muted-foreground">Refresh When Needed</div>
                </div>
                <button
                  type="button"
                  onClick={handleRefresh}
                  className="inline-flex items-center gap-2 rounded-md border border-border bg-background px-3 py-1.5 text-sm font-medium text-foreground transition-colors hover:bg-muted"
                >
                  <RefreshCw className="h-3.5 w-3.5" />
                  Load report
                </button>
              </div>
            )}

            {!loading && !error && !initialLoadDeferred && !hasAnyContent && (
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
              <div className="flex flex-col gap-4 animate-in fade-in duration-200 sm:gap-5">
                {visibleSections.map(({ section, sectionIndex, entries }) => (
                  <SectionContainer
                    key={sectionIndex}
                    section={section}
                    sectionIndex={sectionIndex}
                    workspacePath={workspacePath}
                    entries={entries}
                    sources={sources}
                    hiddenWidgetKeys={hiddenWidgetKeys}
                    handleToggleWidgetHidden={handleToggleWidgetHidden}
                  />
                ))}
              </div>
            )}
          </div>
        </div>
      </div>

      {/* Report export (SVG/PNG) is triggered from the shared on-pane toolbar's
          download button (PreviewPaneControls) via WORKFLOW_REPORT_EXPORT_EVENT —
          see the listener effect below. The old top-right export cluster was
          removed because it overlapped that bar. */}

      {/* Device-width selection + refresh moved to the shared on-pane bar
          (PreviewPaneControls); preference changes arrive via the
          REPORT_PREVIEW_PREFERENCE_CHANGED_EVENT listener, refresh via
          WORKFLOW_REPORT_REFRESH_EVENT. */}

      {/* In-report file preview modal — opened from file-list rows and
          table/cards file links via useReportFilePreviewStore. Self-contained
          (fixed overlay) so it works inside the workflow layout without the chat
          workspace's file-content viewer. */}
      <FilePreviewModal />
    </div>
    </ReportEmbedProvider>
  )
}

export const ReportView = memo(ReportViewComponent)

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
    <div className="flex flex-col gap-2 border-b border-border pb-3 sm:flex-row sm:flex-wrap sm:items-end">
      <div className="flex min-w-0 items-center gap-2.5">
        <div className="min-w-0">
          <div className="text-[10px] font-semibold uppercase tracking-[0.22em] text-muted-foreground">
            Report Section
          </div>
          <h3 className="report-heading m-0 truncate text-xl font-semibold text-foreground">
            {heading}
          </h3>
        </div>
      </div>
    </div>
  )
}

type SectionTabGroup = { key: string; label: string; entries: Array<{ entry: ReportEntry; entryIndex: number }> }

// MobileTabPicker collapses a section's tab strip into a dropdown on phones, so
// every tab is reachable in one tap instead of scrolling a horizontal strip
// that hides tabs off-screen (important when tabs are per-entity, e.g. per-PAN).
// The strip is still used on tablet/desktop.
function MobileTabPicker({
  tabGroups,
  activeKey,
  onSelect,
}: {
  tabGroups: SectionTabGroup[]
  activeKey: string
  onSelect: (key: string) => void
}) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const onDoc = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onDoc)
    window.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDoc)
      window.removeEventListener('keydown', onKey)
    }
  }, [open])

  const active = tabGroups.find(t => t.key === activeKey) ?? tabGroups[0]

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        onClick={() => setOpen(o => !o)}
        aria-haspopup="listbox"
        aria-expanded={open}
        className="flex w-full items-center justify-between gap-2 rounded-md border border-border bg-background px-3 py-2 text-sm font-medium text-foreground transition-colors hover:bg-muted/40"
      >
        <span className="min-w-0 truncate">{active?.label}</span>
        <ChevronDown className={`h-4 w-4 shrink-0 text-muted-foreground transition-transform ${open ? 'rotate-180' : ''}`} />
      </button>
      {open && (
        <div role="listbox" className="absolute left-0 right-0 z-20 mt-1 max-h-72 overflow-auto rounded-md border border-border bg-background py-1 shadow-lg">
          {tabGroups.map(tab => {
            const isActive = active?.key === tab.key
            return (
              <button
                key={tab.key}
                type="button"
                role="option"
                aria-selected={isActive}
                onClick={() => {
                  onSelect(tab.key)
                  setOpen(false)
                }}
                className={`flex w-full items-center justify-between gap-2 px-3 py-2 text-left text-sm ${
                  isActive ? 'bg-muted/60 font-medium text-foreground' : 'text-muted-foreground hover:bg-muted/40 hover:text-foreground'
                }`}
              >
                <span className="flex min-w-0 items-center gap-2">
                  <Check className={`h-3.5 w-3.5 shrink-0 ${isActive ? 'opacity-100' : 'opacity-0'}`} />
                  <span className="truncate">{tab.label}</span>
                </span>
              </button>
            )
          })}
        </div>
      )}
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
  workspacePath,
  entries,
  sources,
  hiddenWidgetKeys,
  handleToggleWidgetHidden,
}: {
  section: ReportSection
  sectionIndex: number
  workspacePath: string
  entries: Array<{ entry: ReportEntry; entryIndex: number }>
  sources: SourceCache
  hiddenWidgetKeys: Set<string>
  handleToggleWidgetHidden: (widgetKey: string) => void
}) {
  const tabsEnabled = section.layout?.mode === 'tabs'
  const tabGroups = useMemo(() => {
    if (!tabsEnabled) return []
    const groups: Array<{ key: string; label: string; entries: Array<{ entry: ReportEntry; entryIndex: number }> }> = []
    const byKey = new Map<string, { key: string; label: string; entries: Array<{ entry: ReportEntry; entryIndex: number }> }>()
    for (const item of entries) {
      const label = item.entry.tab?.trim() || 'Overview'
      const key = label.toLowerCase()
      let group = byKey.get(key)
      if (!group) {
        group = { key, label, entries: [] }
        byKey.set(key, group)
        groups.push(group)
      }
      group.entries.push(item)
    }
    return groups
  }, [entries, tabsEnabled])
  const [activeTabKey, setActiveTabKey] = useState<string | null>(null)
  useEffect(() => {
    if (!tabsEnabled || tabGroups.length === 0) return
    if (!activeTabKey || !tabGroups.some(tab => tab.key === activeTabKey)) {
      setActiveTabKey(tabGroups[0].key)
    }
  }, [activeTabKey, tabGroups, tabsEnabled])

  // Container size tier — phone / tablet / desktop, matching the project's
  // sm/md Tailwind breakpoints. Container-width based, so it works in
  // split-pane / mobile-preview modes where the report tab is narrower than
  // the actual viewport.
  const [gridRef, sizeTier] = useContainerSizeTier()
  const requestedColumns = section.layout?.columns
  const gridGap = section.layout?.gap ?? 8
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
    : 'flex flex-col gap-2'
  const containerStyle = effectiveColumns
    ? {
        gridTemplateColumns: `repeat(${effectiveColumns}, minmax(0, 1fr))`,
        gap: `${gridGap}px`,
      }
    : undefined
  const activeTab = tabsEnabled && tabGroups.length > 0
    ? tabGroups.find(tab => tab.key === activeTabKey) ?? tabGroups[0]
    : null
  const renderedEntries = activeTab ? activeTab.entries : entries
  // A section whose entries are ALL self-contained documents (md/html) doesn't
  // need the section heading + card chrome — each document carries its own title
  // and should fill the whole section. This holds whether the documents are
  // stacked or arranged as tabs (e.g. per-PAN report tabs): the tab bar still
  // renders, but the card border/padding is dropped so an HTML iframe sits flush
  // against the report panel instead of inside a framed, padded box.
  const documentOnly =
    entries.length > 0 &&
    entries.every(({ entry }) => entry.kind === 'single' && isDocumentWidget(entry.widget))
  return (
    <section className={documentOnly ? 'flex flex-col' : 'flex flex-col gap-2 p-0 sm:gap-2.5 sm:rounded-2xl sm:border sm:border-border/50 sm:bg-card/55 sm:p-3 sm:shadow-sm'}>
      {!documentOnly && <SectionHeader heading={section.heading} />}
      {tabsEnabled && tabGroups.length > 0 && (
        sizeTier === 'phone' ? (
          <MobileTabPicker
            tabGroups={tabGroups}
            activeKey={activeTab?.key ?? tabGroups[0]?.key}
            onSelect={setActiveTabKey}
          />
        ) : (
          <div className="flex gap-1 overflow-x-auto border-b border-border/60 pb-1 [scrollbar-width:thin]">
            {tabGroups.map(tab => (
              <button
                key={tab.key}
                type="button"
                onClick={() => setActiveTabKey(tab.key)}
                className={`shrink-0 rounded-t-md border px-3 py-1.5 text-sm transition-colors ${
                  (activeTab?.key ?? tabGroups[0]?.key) === tab.key
                    ? 'border-border border-b-background bg-background font-medium text-foreground shadow-sm'
                    : 'border-transparent text-muted-foreground hover:bg-muted/50 hover:text-foreground'
                }`}
              >
                <span>{tab.label}</span>
              </button>
            ))}
          </div>
        )
      )}
      <div ref={gridRef} className={containerClassName} style={containerStyle}>
        {renderedEntries.map(({ entry, entryIndex }) => {
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
              workspacePath={workspacePath}
              sources={sources}
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

// EmbeddedReportWidget renders a widget spec parsed from a ```report-widget
// block inside a markdown document. Embedded specs reference source files the
// plan scanner never saw, so it loads any that aren't already in the report
// snapshot, then defers to the normal WidgetCard so embedded widgets behave
// exactly like plan widgets (live, db-bound).
function EmbeddedReportWidget({
  spec,
  workspacePath,
  baseSources,
}: {
  spec: unknown
  workspacePath: string
  baseSources: SourceCache
}) {
  const widget = (spec && typeof spec === 'object' ? spec : {}) as ReportWidget
  const validKind = typeof widget.kind === 'string' && widget.kind in WIDGET_SOURCE_MODE
  const needed = useMemo(() => (validKind ? widgetSourcePaths(widget) : []), [validKind, widget])
  const missing = useMemo(() => needed.filter(p => !(p in baseSources)), [needed, baseSources])
  const missingKey = missing.join('|')
  const [extra, setExtra] = useState<SourceCache | null>(missing.length === 0 ? {} : null)

  useEffect(() => {
    if (missing.length === 0) { setExtra({}); return }
    let cancelled = false
    setExtra(null)
    void Promise.all(
      missing.map(async (path): Promise<readonly [string, unknown]> => {
        const content = await readWorkspaceText(`${workspacePath}/${path}`)
        if (content === null || content.trim() === '') return [path, null] as const
        if (/\.(md|markdown|txt)$/i.test(path)) return [path, content] as const
        try { return [path, JSON.parse(content)] as const } catch { return [path, null] as const }
      })
    ).then(entries => { if (!cancelled) setExtra(Object.fromEntries(entries)) })
    return () => { cancelled = true }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [workspacePath, missingKey])

  if (!validKind) {
    return (
      <WidgetError
        widget={widget}
        message={`Unknown embedded widget kind "${String(widget.kind ?? '')}".`}
        hint="Use a valid kind: table, stat, chart, cards, text, markdown, file, file-list, alert, pivot."
      />
    )
  }
  if (extra === null) {
    return <div className="py-2 text-xs italic text-muted-foreground">Loading embedded widget…</div>
  }
  return <WidgetCard widget={widget} workspacePath={workspacePath} sources={{ ...baseSources, ...extra }} />
}

function EntryRenderer({
  entry,
  entryIndex,
  sectionIndex,
  workspacePath,
  sources,
  hiddenWidgetKeys,
  onToggleWidgetHidden,
}: {
  entry: ReportEntry
  entryIndex: number
  sectionIndex: number
  workspacePath: string
  sources: SourceCache
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
        hidden={hiddenWidgetKeys.has(widgetKey)}
        onToggleHidden={() => onToggleWidgetHidden(widgetKey)}
        workspacePath={workspacePath}
      />
    )
  }
  const visibleWidgets = entry.row.widgets.flatMap((widget, widgetIndex) => {
    const widgetKey = widgetInstanceKey(widget, { sectionIndex, entryIndex, widgetIndex })
    if (hiddenWidgetKeys.has(widgetKey)) return [{ widget, widgetKey, hidden: true }]
    if (!widgetShouldRender(widget, widgetRawForVisibility(widget, sources))) return []
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
            hidden={Boolean(hidden)}
            onToggleHidden={() => onToggleWidgetHidden(widgetKey)}
            workspacePath={workspacePath}
          />
        </div>
      ))}
    </div>
  )
}

// Source mode classifies how WidgetCard should resolve data for each widget kind.
// The modes determine the dispatch path:
//   - singular   — narrows the source to a single row (for stat / alert)
//   - pivot      — feeds the raw source straight to the renderer
//   - collection — resolves `path` + `filter` then hands an array/value to
//                  the renderer
type WidgetSourceMode = 'singular' | 'pivot' | 'collection'

const WIDGET_SOURCE_MODE: Record<ReportWidgetKind, WidgetSourceMode> = {
  stat: 'singular',
  alert: 'singular',
  pivot: 'pivot',
  text: 'collection',
  markdown: 'collection',
  table: 'collection',
  cards: 'collection',
  chart: 'collection',
  file: 'collection',
  'file-list': 'collection',
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
type FileWidgetRenderer = React.FC<{ widget: ReportWidget; workspacePath: string }>

const COLLECTION_WIDGET_RENDERERS: Partial<Record<ReportWidgetKind, CollectionWidgetRenderer>> = {}
const SINGULAR_WIDGET_RENDERERS: Partial<Record<ReportWidgetKind, SingularWidgetRenderer>> = {}
const PIVOT_WIDGET_RENDERERS: Partial<Record<ReportWidgetKind, PivotWidgetRenderer>> = {}
const FILE_WIDGET_RENDERERS: Partial<Record<ReportWidgetKind, FileWidgetRenderer>> = {}

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
  hidden = false,
  onToggleHidden,
  workspacePath,
}: {
  widget: ReportWidget
  sources: SourceCache
  hidden?: boolean
  onToggleHidden?: () => void
  workspacePath: string
}) {
  const dataWidget = isDataWidget(widget)
  const sourceInput = useMemo(() => resolveWidgetSourceInput(widget, sources), [widget, sources])
  const raw = sourceInput.status === 'ok' ? sourceInput.value : sourceInput.status === 'loading' ? undefined : null
  const sqlState = useWidgetSQLData(dataWidget ? widget : null, workspacePath)

  if (hidden && onToggleHidden) {
    return <HiddenWidgetCard widget={widget} onShow={onToggleHidden} />
  }

  const mode = WIDGET_SOURCE_MODE[widget.kind]

  const wrapNotice = (content: React.ReactNode) =>
    isStandaloneWidget(widget.kind) ? (
      <StandaloneWidgetNotice onToggleHidden={onToggleHidden}>{content}</StandaloneWidgetNotice>
    ) : (
      <WidgetShell widget={widget} onToggleHidden={onToggleHidden}>{content}</WidgetShell>
    )

  if (isFileArtifactWidget(widget)) {
    const Renderer = FILE_WIDGET_RENDERERS[widget.kind]
    if (!Renderer) return null
    if (!widget.source) {
      return wrapNotice(
        <WidgetError
          widget={widget}
          message="File widget has no source."
          hint="Set source to a path under db/, knowledgebase/, or docs/."
        />,
      )
    }
    return (
      <WidgetShell widget={widget} onToggleHidden={onToggleHidden}>
        <Renderer widget={widget} workspacePath={workspacePath} />
      </WidgetShell>
    )
  }

  // A widget binds to either a data source (db + sql) or a file (source).
  if (!widget.source && !dataWidget) {
    return (
      <div
        aria-hidden="true"
        className="w-full"
        style={{ minHeight: `${widget.height ?? 72}px` }}
      />
    )
  }

  let effectiveRaw: unknown
  if (dataWidget) {
    if (sqlState.status === 'idle' || sqlState.status === 'loading') {
      return wrapNotice(
        <div className="py-1.5 text-xs italic text-muted-foreground">Running query…</div>,
      )
    }
    if (sqlState.status === 'error') {
      return wrapNotice(
        <WidgetError widget={widget} message="SQL query failed." hint={sqlState.message} />,
      )
    }
    effectiveRaw = sqlState.rows
  } else {
    if (raw === undefined) {
      return wrapNotice(
        <div className="py-1.5 text-xs italic text-muted-foreground">Loading {sourceInput.label}…</div>,
      )
    }
    if (raw === null) {
      return wrapNotice(
        <div className="py-1.5 text-xs italic text-muted-foreground">
          Source not available: <code className="px-1 rounded bg-muted">{sourceInput.label}</code>
        </div>,
      )
    }
    effectiveRaw = raw
  }

  const singularSource = mode === 'singular' ? resolveSingularWidgetSource(effectiveRaw, widget) : null
  const conditionalSource = singularSource?.status === 'ok' ? singularSource.value : effectiveRaw
  if (!evaluateShowIf(conditionalSource, widget.showIf)) return null

  if (mode === 'singular') {
    const Renderer = SINGULAR_WIDGET_RENDERERS[widget.kind]
    if (!Renderer) return null
    return (
      <Renderer
        source={singularSource?.status === 'ok' ? singularSource.value : undefined}
        resolution={singularSource ?? { status: 'ok', value: effectiveRaw }}
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
        <Renderer source={effectiveRaw} widget={widget} />
      </WidgetShell>
    )
  }

  // Collection mode: resolve path → filter → render.
  const resolvedRaw = resolveJSONPath(effectiveRaw, widget.path)
  let content: React.ReactNode = null
  if (resolvedRaw === undefined) {
    content = (
      <WidgetError
        widget={widget}
        message={`Path "${widget.path || '(root)'}" doesn't resolve in ${sourceInput.label}.`}
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
          message={`No rows in ${sourceInput.label}${widget.filter ? ` matching filter "${widget.filter}"` : ''}.`}
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
FILE_WIDGET_RENDERERS.file = FileWidget
FILE_WIDGET_RENDERERS['file-list'] = FileListWidget

SINGULAR_WIDGET_RENDERERS.stat = StatWidget
SINGULAR_WIDGET_RENDERERS.alert = AlertWidget

PIVOT_WIDGET_RENDERERS.pivot = PivotWidget
