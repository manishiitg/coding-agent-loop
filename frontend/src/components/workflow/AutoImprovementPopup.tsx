import React, { useEffect, useState, useCallback, useRef } from 'react'
import {
  X,
  Loader2,
  RefreshCw,
  Target,
  TrendingUp,
  FileText,
} from 'lucide-react'
import { agentApi } from '../../services/api'
import ModalPortal from '../ui/ModalPortal'
import { MarkdownRenderer } from '../ui/MarkdownRenderer'
import { HtmlRenderer } from '../ui/HtmlRenderer'

// =====================================================================
// AutoImprovementPopup — the workflow's single agent-curated log
// (builder/improve.html) plus its north-star soul. Metric/eval trajectory
// charts and the legacy review-log tab were removed in favour of one HTML log
// (see docs/design/single-log-view-mockup.html); review findings now live in
// the log itself, and the deterministic metric/eval files remain as substrate.
// =====================================================================

interface AutoImprovementPopupProps {
  isOpen: boolean
  onClose: () => void
  workspacePath: string | null
  // Retained for call-site compatibility; no longer consumed here.
  selectedRunFolder?: string | null
  // Called when the user actually views the log, so the toolbar can clear that
  // doc's "unseen" badge dot.
  onViewDoc?: (which: 'improve' | 'review') => void
}

type Tab = 'log' | 'soul'
type BuilderDocKind = 'soul' | 'improve'
type BuilderDoc = { exists: boolean; content: string; path: string }

const emptyDocLoadingState = (): Record<BuilderDocKind, boolean> => ({
  soul: false,
  improve: false,
})

const emptyDocErrorState = (): Record<BuilderDocKind, string | null> => ({
  soul: null,
  improve: null,
})

interface BuilderDocArchiveFile {
  path: string
  label: string
}

interface BuilderDocPanelProps {
  which: BuilderDocKind
  doc: BuilderDoc | null
  loading: boolean
  error: string | null
  onRefresh: () => void
  archiveFiles?: BuilderDocArchiveFile[]
  selectedPath?: string
  onSelectPath?: (path: string) => void
}

const BuilderDocPanel: React.FC<BuilderDocPanelProps> = ({ which, doc, loading, error, onRefresh, archiveFiles = [], selectedPath, onSelectPath }) => {
  const copy = {
    soul: {
      title: 'Soul',
      blurb: 'The workflow north star. The optimizer treats this as the source of truth for objective and success criteria; metrics, the log, and plans are judged against it.',
      emptyHint: 'soul/soul.md is missing. Define ## Objective and ## Success Criteria before relying on metrics or improvement.',
    },
    improve: {
      title: 'Workflow log',
      blurb: 'The workflow\'s single durable journal: applied/proposed changes, review findings, per-run notes, monitor observations, and captured user rules — newest first. /improve-* and /review-* skills and the monitor append here.',
      emptyHint: 'No entries yet. Run /define-success to bootstrap it, then use /improve-* and /review-* commands for ongoing work.',
    },
  }[which]
  const showFileMenu = which === 'improve' && !!onSelectPath
  const activePath = selectedPath || doc?.path || ''
  const currentPath = 'builder/improve.html'
  const fileOptions = [{ path: currentPath, label: 'Current log' }, ...archiveFiles]

  return (
    <div className="space-y-3">
      <div className="flex items-baseline justify-between gap-2 flex-wrap">
        <div>
          <h3 className="text-sm font-semibold">{copy.title}</h3>
          {doc?.path && <code className="text-[10px] text-muted-foreground">{doc.path}</code>}
        </div>
        <button
          onClick={onRefresh}
          disabled={loading}
          className="inline-flex items-center gap-1 px-2 py-1 text-xs rounded border hover:bg-accent disabled:opacity-50"
        >
          {loading ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <RefreshCw className="w-3.5 h-3.5" />}
          Refresh
        </button>
      </div>
      <p className="text-xs text-muted-foreground">{copy.blurb}</p>
      {error && (
        <div className="text-xs text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded p-2">
          {error}
        </div>
      )}
      {loading && !doc && (
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="w-4 h-4 animate-spin" /> Loading…
        </div>
      )}
      {showFileMenu && (
        <div className="flex items-center justify-between gap-3 rounded-md border bg-card px-3 py-2">
          <div className="min-w-0">
            <div className="text-[10px] uppercase tracking-wide text-muted-foreground font-medium">Log files</div>
            <div className="truncate text-xs text-muted-foreground">{activePath || currentPath}</div>
          </div>
          <select
            value={activePath || currentPath}
            onChange={(event) => onSelectPath?.(event.target.value)}
            className="min-w-[180px] max-w-[280px] rounded-md border bg-background px-2 py-1 text-xs text-foreground focus:outline-none focus:ring-2 focus:ring-primary/40"
            title={activePath || currentPath}
          >
            {fileOptions.map((file) => (
              <option key={file.path} value={file.path}>
                {file.label}
              </option>
            ))}
            {archiveFiles.length === 0 && (
              <option value="__no_archives" disabled>
                No archive files
              </option>
            )}
          </select>
        </div>
      )}
      <div className="min-w-0">
        {doc && !doc.exists && (
          <div className="border border-dashed rounded-md p-4 text-sm text-muted-foreground">
            {copy.emptyHint}
          </div>
        )}
        {doc && doc.exists && (
          <div className="border rounded-md p-3 bg-card">
            {doc.content.trimStart().startsWith('<') ? (
              <div className="h-[70vh]">
                <HtmlRenderer content={doc.content} />
              </div>
            ) : (
              <MarkdownRenderer
                content={doc.content}
                disablePathLinking
                className="!text-[12px] leading-relaxed [&_p]:!text-[12px] [&_li]:!text-[12px] [&_h1]:!text-base [&_h2]:!text-sm [&_h3]:!text-xs [&_h1]:mt-3 [&_h2]:mt-3 [&_h3]:mt-2 [&_p]:my-1.5 [&_ul]:my-1.5 [&_ol]:my-1.5 [&_code]:!text-[11px] [&_pre]:!text-[11px]"
              />
            )}
          </div>
        )}
      </div>
    </div>
  )
}

const AutoImprovementPopup: React.FC<AutoImprovementPopupProps> = ({ isOpen, onClose, workspacePath, onViewDoc }) => {
  const [tab, setTab] = useState<Tab>('log')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [improveDoc, setImproveDoc] = useState<BuilderDoc | null>(null)
  const [soulDoc, setSoulDoc] = useState<BuilderDoc | null>(null)
  const [improveArchiveFiles, setImproveArchiveFiles] = useState<BuilderDocArchiveFile[]>([])
  const [selectedImprovePath, setSelectedImprovePath] = useState('builder/improve.html')
  const [docLoading, setDocLoading] = useState<Record<BuilderDocKind, boolean>>(emptyDocLoadingState)
  const [docError, setDocError] = useState<Record<BuilderDocKind, string | null>>(emptyDocErrorState)
  const docRequestSeq = useRef<Record<BuilderDocKind, number>>({ soul: 0, improve: 0 })
  const [frameworkHealth, setFrameworkHealth] = useState<{
    soul_exists: boolean
    objective_ok: boolean
    success_criteria_ok: boolean
  } | null>(null)

  const refresh = useCallback(async () => {
    if (!workspacePath) return
    setLoading(true)
    setError(null)
    try {
      const h = await agentApi.getFrameworkHealth(workspacePath).catch((err) => ({ success: false, error: String(err), soul_exists: false, objective_ok: false, success_criteria_ok: false }))
      if (h.success) {
        setFrameworkHealth({
          soul_exists: !!h.soul_exists,
          objective_ok: !!h.objective_ok,
          success_criteria_ok: !!h.success_criteria_ok,
        })
      } else {
        setFrameworkHealth(null)
      }
      if (h.error) setError(h.error)
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [workspacePath])

  useEffect(() => {
    if (isOpen && workspacePath) {
      refresh()
    }
  }, [isOpen, workspacePath, refresh])

  const fetchDoc = useCallback(async (which: BuilderDocKind, filePath?: string) => {
    if (!workspacePath) return
    const requestSeq = docRequestSeq.current[which] + 1
    docRequestSeq.current[which] = requestSeq
    setDocLoading((prev) => ({ ...prev, [which]: true }))
    setDocError((prev) => ({ ...prev, [which]: null }))
    try {
      const res = await agentApi.getBuilderDoc(workspacePath, which, filePath)
      if (docRequestSeq.current[which] !== requestSeq) return
      const payload = { exists: !!res.exists, content: res.content || '', path: res.path || '' }
      if (which === 'soul') setSoulDoc(payload)
      else setImproveDoc(payload)
      if (!res.success && res.error) setDocError((prev) => ({ ...prev, [which]: res.error || null }))
    } catch (err) {
      if (docRequestSeq.current[which] === requestSeq) {
        setDocError((prev) => ({ ...prev, [which]: err instanceof Error ? err.message : String(err) }))
      }
    } finally {
      if (docRequestSeq.current[which] === requestSeq) {
        setDocLoading((prev) => ({ ...prev, [which]: false }))
      }
    }
  }, [workspacePath])

  const fetchImproveArchives = useCallback(async () => {
    if (!workspacePath) return
    try {
      const res = await agentApi.getBuilderDocArchives(workspacePath, 'improve')
      setImproveArchiveFiles(res.success ? res.files : [])
      if (!res.success && res.error) setDocError((prev) => ({ ...prev, improve: res.error || null }))
    } catch (err) {
      setDocError((prev) => ({ ...prev, improve: err instanceof Error ? err.message : String(err) }))
    }
  }, [workspacePath])

  // Bust cached docs whenever the workspace switches or the popup re-opens.
  useEffect(() => {
    docRequestSeq.current = {
      soul: docRequestSeq.current.soul + 1,
      improve: docRequestSeq.current.improve + 1,
    }
    setSoulDoc(null)
    setImproveDoc(null)
    setImproveArchiveFiles([])
    setSelectedImprovePath('builder/improve.html')
    setDocLoading(emptyDocLoadingState())
    setDocError(emptyDocErrorState())
  }, [workspacePath, isOpen])

  useEffect(() => {
    if (!isOpen || !workspacePath) return
    fetchDoc('improve')
    fetchImproveArchives()
  }, [isOpen, workspacePath, fetchDoc, fetchImproveArchives])

  useEffect(() => {
    if (!isOpen || !workspacePath) return
    if (tab === 'soul' && soulDoc === null) fetchDoc('soul')
    if (tab === 'log') {
      fetchImproveArchives()
      if (improveDoc === null || improveDoc.path !== selectedImprovePath) {
        fetchDoc('improve', selectedImprovePath === 'builder/improve.html' ? undefined : selectedImprovePath)
      }
    }
  }, [isOpen, workspacePath, tab, soulDoc, improveDoc, selectedImprovePath, fetchDoc, fetchImproveArchives])

  // When the user actually views the log, clear its toolbar dot.
  useEffect(() => {
    if (!isOpen) return
    if (tab === 'log' && improveDoc?.exists) onViewDoc?.('improve')
  }, [isOpen, tab, improveDoc?.exists, onViewDoc])

  if (!isOpen) return null

  return (
    <ModalPortal>
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
        <div className="bg-background border rounded-lg shadow-xl w-full max-w-6xl max-h-[90vh] flex flex-col">
          <div className="flex items-center justify-between p-4 border-b">
            <div className="flex items-center gap-2">
              <TrendingUp className="w-5 h-5 text-purple-600" />
              <h2 className="text-lg font-semibold">Workflow log</h2>
            </div>
            <div className="flex items-center gap-2">
              <button
                onClick={refresh}
                disabled={loading}
                className="p-1.5 rounded-md hover:bg-accent disabled:opacity-50"
                title="Refresh"
              >
                {loading ? <Loader2 className="w-4 h-4 animate-spin" /> : <RefreshCw className="w-4 h-4" />}
              </button>
              <button onClick={onClose} className="p-1.5 rounded-md hover:bg-accent">
                <X className="w-4 h-4" />
              </button>
            </div>
          </div>

          <div className="flex border-b text-sm">
            {(
              [
                { id: 'log', icon: FileText, label: 'Log' },
                { id: 'soul', icon: Target, label: 'Soul' },
              ] as const
            ).map((t) => {
              const Icon = t.icon
              const active = tab === t.id
              return (
                <button
                  key={t.id}
                  onClick={() => setTab(t.id)}
                  className={`flex items-center gap-2 px-4 py-2 border-b-2 transition-colors ${
                    active ? 'border-purple-600 text-purple-600' : 'border-transparent text-muted-foreground hover:text-foreground'
                  }`}
                >
                  <Icon className="w-4 h-4" />
                  {t.label}
                </button>
              )
            })}
          </div>

          {error && (
            <div className="px-4 py-2 text-xs text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-900/20 border-b">
              {error}
            </div>
          )}

          {frameworkHealth && (() => {
            const issues: { kind: 'critical' | 'warning'; msg: string }[] = []
            if (!frameworkHealth.soul_exists) {
              issues.push({ kind: 'critical', msg: 'soul/soul.md is missing — define ## Objective and ## Success Criteria before adding metrics.' })
            } else {
              if (!frameworkHealth.objective_ok) issues.push({ kind: 'critical', msg: 'soul.md ## Objective is empty or still a TODO placeholder.' })
              if (!frameworkHealth.success_criteria_ok) issues.push({ kind: 'critical', msg: 'soul.md ## Success Criteria is empty — without it, metrics have no north star to measure against.' })
            }
            if (issues.length === 0) return null
            const hasCritical = issues.some((i) => i.kind === 'critical')
            return (
              <div className={`px-4 py-2 text-xs border-b ${hasCritical ? 'bg-red-50 dark:bg-red-900/20 text-red-800 dark:text-red-200 border-red-200 dark:border-red-800' : 'bg-amber-50 dark:bg-amber-900/20 text-amber-800 dark:text-amber-200 border-amber-200 dark:border-amber-800'}`}>
                <div className="font-medium mb-1">Framework health</div>
                <ul className="list-disc list-inside space-y-0.5">
                  {issues.map((i, n) => <li key={n}>{i.msg}</li>)}
                </ul>
              </div>
            )
          })()}

          <div className="flex-1 overflow-y-auto p-4">
            <BuilderDocPanel
              which={tab === 'soul' ? 'soul' : 'improve'}
              doc={tab === 'soul' ? soulDoc : improveDoc}
              loading={tab === 'soul' ? docLoading.soul : docLoading.improve}
              error={tab === 'soul' ? docError.soul : docError.improve}
              onRefresh={() => {
                if (tab === 'log') {
                  fetchImproveArchives()
                  fetchDoc('improve', selectedImprovePath !== 'builder/improve.html' ? selectedImprovePath : undefined)
                } else {
                  fetchDoc('soul')
                }
              }}
              archiveFiles={tab === 'log' ? improveArchiveFiles : undefined}
              selectedPath={tab === 'log' ? selectedImprovePath : undefined}
              onSelectPath={tab === 'log' ? (path) => {
                setSelectedImprovePath(path)
                fetchDoc('improve', path === 'builder/improve.html' ? undefined : path)
              } : undefined}
            />
          </div>
        </div>
      </div>
    </ModalPortal>
  )
}

export default AutoImprovementPopup
