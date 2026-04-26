// Knowledgebase popup — read-only viewer for the workspace KB:
// notes/{topic}.md + notes/_index.json (per-topic narrative). Reads via the
// existing workspace file API (same path as ReportViewer). All mutations happen
// via the workshop builder's reorganize_knowledgebase tool — this popup never writes.

import { useCallback, useEffect, useState } from 'react'
import {
  X,
  Database,
  Loader2,
  AlertCircle,
  RefreshCw,
  ChevronDown,
  ChevronRight,
  FileText,
} from 'lucide-react'
import { agentApi } from '../../services/api'
import { MarkdownRenderer } from '../ui/MarkdownRenderer'

interface KBPopupProps {
  isOpen: boolean
  onClose: () => void
  workspacePath: string | null
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

async function readJSON<T>(filepath: string): Promise<T | null> {
  try {
    const resp = await agentApi.getPlannerFileContent(filepath)
    if (!resp?.success || !resp.data?.content) return null
    return JSON.parse(resp.data.content) as T
  } catch {
    return null
  }
}

async function readText(filepath: string): Promise<string | null> {
  try {
    const resp = await agentApi.getPlannerFileContent(filepath)
    if (!resp?.success || typeof resp.data?.content !== 'string') return null
    return resp.data.content
  } catch {
    return null
  }
}

export default function KBPopup({ isOpen, onClose, workspacePath }: KBPopupProps) {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [notesIndex, setNotesIndex] = useState<KBNotesIndex | null>(null)
  // Per-topic markdown body cache. undefined = not loaded; null = loaded and missing/empty.
  const [notesBodies, setNotesBodies] = useState<Record<string, string | null>>({})
  const [expandedNotes, setExpandedNotes] = useState<Set<string>>(new Set())

  const notesIndexPath = workspacePath
    ? `${workspacePath}/knowledgebase/notes/_index.json`
    : null

  const load = useCallback(async () => {
    if (!notesIndexPath) return
    setLoading(true)
    setError(null)
    try {
      const ni = await readJSON<KBNotesIndex>(notesIndexPath)
      setNotesIndex(ni)
      // Reset per-topic markdown cache on reload — sizes/content may have changed.
      setNotesBodies({})
      setExpandedNotes(new Set())
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }, [notesIndexPath])

  useEffect(() => {
    if (isOpen) load()
  }, [isOpen, load])

  useEffect(() => {
    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape' && isOpen) onClose()
    }
    if (isOpen) window.addEventListener('keydown', handleKey)
    return () => window.removeEventListener('keydown', handleKey)
  }, [isOpen, onClose])

  const toggleNoteExpanded = useCallback(
    async (topic: KBNotesTopic) => {
      const next = new Set(expandedNotes)
      if (next.has(topic.id)) {
        next.delete(topic.id)
        setExpandedNotes(next)
        return
      }
      next.add(topic.id)
      setExpandedNotes(next)
      // Selective load — only fetch the markdown when the row is expanded.
      // Matches the index-first read discipline the KB agent enforces.
      if (notesBodies[topic.id] === undefined && workspacePath) {
        const body = await readText(`${workspacePath}/knowledgebase/notes/${topic.file}`)
        setNotesBodies(prev => ({ ...prev, [topic.id]: body }))
      }
    },
    [expandedNotes, notesBodies, workspacePath],
  )

  if (!isOpen) return null

  const topics = notesIndex?.topics ?? []
  const hasNotesContent = topics.length > 0

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-2 sm:p-4">
      <div className="bg-background border border-border rounded-lg shadow-xl w-full max-w-4xl max-h-[calc(100dvh-1rem)] sm:max-h-[90vh] flex flex-col">
        {/* Header */}
        <div className="flex items-start justify-between gap-3 p-3 border-b border-border flex-shrink-0 sm:p-4">
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <Database className="w-5 h-5 text-primary" />
            <h2 className="text-lg font-semibold">Knowledgebase</h2>
            <span className="text-xs text-muted-foreground sm:ml-2">
              notes/ · narrative topics
            </span>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={load}
              disabled={loading}
              className="p-1.5 rounded-md hover:bg-muted transition-colors disabled:opacity-50"
              title="Refresh"
            >
              <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
            </button>
            <button
              onClick={onClose}
              className="p-1 rounded-md hover:bg-muted transition-colors ml-2"
              title="Close (Esc)"
            >
              <X className="w-5 h-5" />
            </button>
          </div>
        </div>

        {/* Summary strip */}
        <div className="flex items-center gap-4 px-4 py-3 border-b border-border flex-shrink-0 text-sm">
          <div>
            <span className="text-muted-foreground">Notes topics: </span>
            <span className="font-medium">{topics.length}</span>
          </div>
          {notesIndex?.last_updated && (
            <div className="text-xs text-muted-foreground ml-auto">
              Last updated: {new Date(notesIndex.last_updated).toLocaleString()}
              {notesIndex.last_updated_by?.step ? ` · ${notesIndex.last_updated_by.step}` : ''}
              {notesIndex.last_updated_by?.run ? ` / ${notesIndex.last_updated_by.run}` : ''}
            </div>
          )}
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto p-4">
          {loading && !notesIndex && (
            <div className="flex items-center gap-2 text-muted-foreground">
              <Loader2 className="w-4 h-4 animate-spin" />
              Loading knowledgebase…
            </div>
          )}

          {error && (
            <div className="flex items-start gap-2 p-3 rounded-md bg-destructive/10 text-destructive">
              <AlertCircle className="w-4 h-4 flex-shrink-0 mt-0.5" />
              <div className="text-sm">{error}</div>
            </div>
          )}

          {!loading && !error && !hasNotesContent && (
            <div className="text-center py-12 text-muted-foreground">
              <Database className="w-10 h-10 mx-auto opacity-30 mb-3" />
              <div className="text-sm">
                Knowledgebase is empty. Narrative topics appear here after steps run with a
                non-empty <code className="px-1 rounded bg-muted">knowledgebase_contribution</code>.
              </div>
            </div>
          )}

          {!loading && !error && hasNotesContent && (
            <div className="space-y-1">
              {topics.map(t => {
                const isOpenRow = expandedNotes.has(t.id)
                const body = notesBodies[t.id]
                const isMarkdownFile = t.file.toLowerCase().endsWith('.md')
                return (
                  <div key={t.id} className="border border-border rounded-md">
                    <button
                      onClick={() => toggleNoteExpanded(t)}
                      className="w-full flex items-center gap-2 px-3 py-2 text-left hover:bg-muted/50 transition-colors"
                    >
                      {isOpenRow ? (
                        <ChevronDown className="w-4 h-4 flex-shrink-0" />
                      ) : (
                        <ChevronRight className="w-4 h-4 flex-shrink-0" />
                      )}
                      <FileText className="w-3.5 h-3.5 flex-shrink-0 text-muted-foreground" />
                      <span className="font-medium text-sm">{t.id}</span>
                      <span className="text-xs text-muted-foreground ml-auto font-mono">
                        {t.file}
                      </span>
                      {typeof t.section_count === 'number' && (
                        <span className="text-xs text-muted-foreground ml-2">
                          {t.section_count} section{t.section_count === 1 ? '' : 's'}
                        </span>
                      )}
                      {typeof t.size_bytes === 'number' && (
                        <span className="text-xs text-muted-foreground ml-2">
                          {(t.size_bytes / 1024).toFixed(1)}KB
                        </span>
                      )}
                    </button>
                    {isOpenRow && (
                      <div className="border-t border-border px-3 py-2 text-xs space-y-2 bg-muted/20">
                        {Array.isArray(t.covers) && t.covers.length > 0 && (
                          <div>
                            <span className="font-medium">Covers: </span>
                            <span className="font-mono">{t.covers.join(', ')}</span>
                          </div>
                        )}
                        {(t.last_updated_by?.step || t.last_updated_by?.run) && (
                          <div>
                            <span className="font-medium">Last updated by: </span>
                            <span className="font-mono">
                              {t.last_updated_by?.step ?? '?'} / {t.last_updated_by?.run ?? '?'}
                            </span>
                          </div>
                        )}
                        {t.last_updated && (
                          <div className="text-muted-foreground">
                            updated {new Date(t.last_updated).toLocaleString()}
                          </div>
                        )}
                        <div>
                          <div className="font-medium mb-1">Content</div>
                          {body === undefined ? (
                            <div className="flex items-center gap-2 text-muted-foreground">
                              <Loader2 className="w-3 h-3 animate-spin" />
                              Loading {t.file}…
                            </div>
                          ) : body === null ? (
                            <div className="text-muted-foreground italic">
                              Topic file missing or empty.
                            </div>
                          ) : isMarkdownFile ? (
                            <div className="text-sm text-foreground bg-background border border-border/60 rounded p-3 max-h-96 overflow-y-auto">
                              <MarkdownRenderer
                                content={body}
                                className="max-w-none !text-sm [&_p]:!text-sm [&_li]:!text-sm [&_code]:!text-[12px]"
                              />
                            </div>
                          ) : (
                            <pre className="whitespace-pre-wrap break-words text-foreground bg-background border border-border/60 rounded p-2 max-h-96 overflow-y-auto">
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
      </div>

    </div>
  )
}
