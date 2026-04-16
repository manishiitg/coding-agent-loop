// Knowledgebase popup — read-only viewer for the workspace KB:
// graph.json + index.json (atomic facts) and notes/{topic}.md + notes/_index.json
// (per-topic narrative). Reads via the existing workspace file API (same path
// as ReportViewer). All mutations happen via the workshop builder's
// reorganize_knowledgebase tool — this popup never writes.

import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  X,
  Database,
  Loader2,
  AlertCircle,
  Download,
  RefreshCw,
  Search,
  ChevronDown,
  ChevronRight,
  FileText,
} from 'lucide-react'
import { agentApi } from '../../services/api'

interface KBPopupProps {
  isOpen: boolean
  onClose: () => void
  workspacePath: string | null
}

interface KBEntity {
  id: string
  type?: string
  label?: string
  properties?: Record<string, unknown>
  created_at?: string
  updated_at?: string
  source?: { step?: string; run?: string }
}

interface KBRelationship {
  id: string
  from: string
  to: string
  type?: string
  properties?: Record<string, unknown>
  source?: { step?: string; run?: string }
}

interface KBGraph {
  version?: string
  updated_at?: string
  entities?: KBEntity[]
  relationships?: KBRelationship[]
}

interface KBIndex {
  entity_count?: number
  relationship_count?: number
  entity_types?: string[]
  relationship_types?: string[]
  last_updated?: string
  last_updated_by?: { step?: string; run?: string }
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

type Tab = 'graph' | 'notes'

export default function KBPopup({ isOpen, onClose, workspacePath }: KBPopupProps) {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [graph, setGraph] = useState<KBGraph | null>(null)
  const [index, setIndex] = useState<KBIndex | null>(null)
  const [notesIndex, setNotesIndex] = useState<KBNotesIndex | null>(null)
  const [typeFilter, setTypeFilter] = useState<string>('all')
  const [search, setSearch] = useState('')
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [tab, setTab] = useState<Tab>('graph')
  // Per-topic markdown body cache. undefined = not loaded; null = loaded and missing/empty.
  const [notesBodies, setNotesBodies] = useState<Record<string, string | null>>({})
  const [expandedNotes, setExpandedNotes] = useState<Set<string>>(new Set())

  const graphPath = workspacePath ? `${workspacePath}/knowledgebase/graph.json` : null
  const indexPath = workspacePath ? `${workspacePath}/knowledgebase/index.json` : null
  const notesIndexPath = workspacePath
    ? `${workspacePath}/knowledgebase/notes/_index.json`
    : null

  const load = useCallback(async () => {
    if (!graphPath || !indexPath || !notesIndexPath) return
    setLoading(true)
    setError(null)
    try {
      const [g, i, ni] = await Promise.all([
        readJSON<KBGraph>(graphPath),
        readJSON<KBIndex>(indexPath),
        readJSON<KBNotesIndex>(notesIndexPath),
      ])
      setGraph(g)
      setIndex(i)
      setNotesIndex(ni)
      // Reset per-topic markdown cache on reload — sizes/content may have changed.
      setNotesBodies({})
      setExpandedNotes(new Set())
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }, [graphPath, indexPath, notesIndexPath])

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

  const entities = graph?.entities ?? []
  const relationships = graph?.relationships ?? []

  // Entity types derived from live graph rather than index.json — avoids stale index
  // data when the KB update agent's last write hasn't flushed to index yet.
  const entityTypes = useMemo(() => {
    const s = new Set<string>()
    for (const e of entities) {
      if (e.type) s.add(e.type)
    }
    return Array.from(s).sort()
  }, [entities])

  const filteredEntities = useMemo(() => {
    const needle = search.trim().toLowerCase()
    return entities.filter(e => {
      if (typeFilter !== 'all' && e.type !== typeFilter) return false
      if (!needle) return true
      if (e.id.toLowerCase().includes(needle)) return true
      if (e.label?.toLowerCase().includes(needle)) return true
      if (e.type?.toLowerCase().includes(needle)) return true
      return false
    })
  }, [entities, typeFilter, search])

  const toggleExpanded = (id: string) => {
    setExpanded(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

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

  const handleExport = () => {
    if (!graph) return
    const blob = new Blob([JSON.stringify(graph, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `graph-${new Date().toISOString().replace(/[:.]/g, '-')}.json`
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
  }

  if (!isOpen) return null

  const topics = notesIndex?.topics ?? []
  const hasGraphFile = graph !== null
  const hasAnyContent = (entities.length + relationships.length + topics.length) > 0
  const hasGraphContent = (entities.length + relationships.length) > 0
  const hasNotesContent = topics.length > 0

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
      <div className="bg-background border border-border rounded-lg shadow-xl w-full max-w-4xl max-h-[90vh] flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between p-4 border-b border-border flex-shrink-0">
          <div className="flex items-center gap-2">
            <Database className="w-5 h-5 text-primary" />
            <h2 className="text-lg font-semibold">Knowledgebase</h2>
            <span className="text-xs text-muted-foreground ml-2">
              graph.json + notes/ · entities, relationships, narrative
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
              onClick={handleExport}
              disabled={!hasAnyContent}
              className="p-1.5 rounded-md hover:bg-muted transition-colors disabled:opacity-40"
              title="Export graph.json"
            >
              <Download className="w-4 h-4" />
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
            <span className="text-muted-foreground">Entities: </span>
            <span className="font-medium">{entities.length}</span>
          </div>
          <div>
            <span className="text-muted-foreground">Relationships: </span>
            <span className="font-medium">{relationships.length}</span>
          </div>
          <div>
            <span className="text-muted-foreground">Notes topics: </span>
            <span className="font-medium">{topics.length}</span>
          </div>
          {index?.last_updated && (
            <div className="text-xs text-muted-foreground ml-auto">
              Last updated: {new Date(index.last_updated).toLocaleString()}
              {index.last_updated_by?.step ? ` · ${index.last_updated_by.step}` : ''}
              {index.last_updated_by?.run ? ` / ${index.last_updated_by.run}` : ''}
            </div>
          )}
        </div>

        {/* Tabs */}
        <div className="flex items-center gap-1 px-4 pt-2 border-b border-border flex-shrink-0">
          <button
            onClick={() => setTab('graph')}
            className={`px-3 py-1.5 text-xs font-medium rounded-t-md transition-colors ${
              tab === 'graph'
                ? 'bg-muted text-foreground border border-b-0 border-border'
                : 'text-muted-foreground hover:text-foreground'
            }`}
          >
            Graph ({entities.length + relationships.length})
          </button>
          <button
            onClick={() => setTab('notes')}
            className={`px-3 py-1.5 text-xs font-medium rounded-t-md transition-colors ${
              tab === 'notes'
                ? 'bg-muted text-foreground border border-b-0 border-border'
                : 'text-muted-foreground hover:text-foreground'
            }`}
          >
            Notes ({topics.length})
          </button>
        </div>

        {/* Filter controls — only relevant in graph tab */}
        {tab === 'graph' && hasGraphContent && (
          <div className="flex items-center gap-2 px-4 py-2 border-b border-border flex-shrink-0">
            <div className="relative flex-1">
              <Search className="absolute left-2.5 top-2.5 w-4 h-4 text-muted-foreground" />
              <input
                type="text"
                placeholder="Search entities by id, label, or type…"
                value={search}
                onChange={e => setSearch(e.target.value)}
                className="w-full pl-9 pr-3 py-2 text-sm bg-muted/50 border border-input rounded-md focus:outline-none focus:ring-1 focus:ring-primary"
              />
            </div>
            <select
              value={typeFilter}
              onChange={e => setTypeFilter(e.target.value)}
              className="px-2 py-2 text-sm bg-muted/50 border border-input rounded-md focus:outline-none focus:ring-1 focus:ring-primary"
            >
              <option value="all">All types ({entityTypes.length})</option>
              {entityTypes.map(t => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
            </select>
          </div>
        )}

        {/* Body */}
        <div className="flex-1 overflow-y-auto p-4">
          {loading && !hasGraphFile && (
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

          {!loading && !error && !hasAnyContent && (
            <div className="text-center py-12 text-muted-foreground">
              <Database className="w-10 h-10 mx-auto opacity-30 mb-3" />
              <div className="text-sm">
                Knowledgebase is empty. Facts and narrative appear here after steps run with a
                non-empty <code className="px-1 rounded bg-muted">knowledgebase_contribution</code>.
              </div>
            </div>
          )}

          {/* GRAPH TAB */}
          {tab === 'graph' && !loading && !error && hasGraphContent && (
            <div className="space-y-1">
              {filteredEntities.length === 0 ? (
                <div className="text-sm text-muted-foreground py-6 text-center">
                  No entities match the current filter.
                </div>
              ) : (
                filteredEntities.map(e => {
                  const isOpenRow = expanded.has(e.id)
                  const relatedRels = relationships.filter(
                    r => r.from === e.id || r.to === e.id,
                  )
                  return (
                    <div key={e.id} className="border border-border rounded-md">
                      <button
                        onClick={() => toggleExpanded(e.id)}
                        className="w-full flex items-center gap-2 px-3 py-2 text-left hover:bg-muted/50 transition-colors"
                      >
                        {isOpenRow ? (
                          <ChevronDown className="w-4 h-4 flex-shrink-0" />
                        ) : (
                          <ChevronRight className="w-4 h-4 flex-shrink-0" />
                        )}
                        {e.type && (
                          <span className="text-xs px-1.5 py-0.5 rounded bg-muted font-mono">
                            {e.type}
                          </span>
                        )}
                        <span className="font-medium text-sm">{e.label || e.id}</span>
                        <span className="text-xs text-muted-foreground ml-auto font-mono">
                          {e.id}
                        </span>
                        {relatedRels.length > 0 && (
                          <span className="text-xs text-muted-foreground ml-2">
                            {relatedRels.length} rel{relatedRels.length === 1 ? '' : 's'}
                          </span>
                        )}
                      </button>
                      {isOpenRow && (
                        <div className="border-t border-border px-3 py-2 text-xs space-y-2 bg-muted/20">
                          {e.properties && Object.keys(e.properties).length > 0 && (
                            <div>
                              <div className="font-medium mb-1">Properties</div>
                              <pre className="whitespace-pre-wrap break-all">
                                {JSON.stringify(e.properties, null, 2)}
                              </pre>
                            </div>
                          )}
                          {(e.source?.step || e.source?.run) && (
                            <div>
                              <span className="font-medium">Source: </span>
                              <span className="font-mono">
                                {e.source?.step ?? '?'} / {e.source?.run ?? '?'}
                              </span>
                            </div>
                          )}
                          {(e.created_at || e.updated_at) && (
                            <div className="text-muted-foreground">
                              {e.created_at && <>created {new Date(e.created_at).toLocaleString()}</>}
                              {e.updated_at && <> · updated {new Date(e.updated_at).toLocaleString()}</>}
                            </div>
                          )}
                          {relatedRels.length > 0 && (
                            <div>
                              <div className="font-medium mb-1">
                                Relationships ({relatedRels.length})
                              </div>
                              <ul className="space-y-1">
                                {relatedRels.map(r => (
                                  <li key={r.id} className="font-mono">
                                    {r.from === e.id ? '→ ' : '← '}
                                    <span className="px-1 rounded bg-muted">{r.type ?? '?'}</span>
                                    {' '}
                                    <span>{r.from === e.id ? r.to : r.from}</span>
                                  </li>
                                ))}
                              </ul>
                            </div>
                          )}
                        </div>
                      )}
                    </div>
                  )
                })
              )}
            </div>
          )}

          {/* GRAPH TAB — empty state inside the tab */}
          {tab === 'graph' && !loading && !error && !hasGraphContent && hasAnyContent && (
            <div className="text-sm text-muted-foreground py-8 text-center">
              No entities or relationships yet. Notes are present — switch to the Notes tab.
            </div>
          )}

          {/* NOTES TAB */}
          {tab === 'notes' && !loading && !error && hasNotesContent && (
            <div className="space-y-1">
              {topics.map(t => {
                const isOpenRow = expandedNotes.has(t.id)
                const body = notesBodies[t.id]
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
                        {t.covers && t.covers.length > 0 && (
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

          {/* NOTES TAB — empty state inside the tab */}
          {tab === 'notes' && !loading && !error && !hasNotesContent && hasAnyContent && (
            <div className="text-sm text-muted-foreground py-8 text-center">
              No narrative notes yet. Notes appear here when a step's{' '}
              <code className="px-1 rounded bg-muted">knowledgebase_contribution</code> asks
              the KB update agent to write narrative analysis.
            </div>
          )}
        </div>
      </div>

    </div>
  )
}
