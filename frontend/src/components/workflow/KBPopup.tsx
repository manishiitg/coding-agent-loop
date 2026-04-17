// Knowledgebase popup — read-only viewer for the workspace KB:
// graph.json + index.json (atomic facts) and notes/{topic}.md + notes/_index.json
// (per-topic narrative). Reads via the existing workspace file API (same path
// as ReportViewer). All mutations happen via the workshop builder's
// reorganize_knowledgebase tool — this popup never writes.

import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
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
  Network,
  GitBranch,
} from 'lucide-react'
import { agentApi } from '../../services/api'
import { useAppStore } from '../../stores/useAppStore'
import { MarkdownRenderer } from '../ui/MarkdownRenderer'

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
  const workspaceMinimized = useAppStore(state => state.workspaceMinimized)
  const setWorkspaceMinimized = useAppStore(state => state.setWorkspaceMinimized)
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
  const wasOpenRef = useRef(false)
  const restoreWorkspaceOnCloseRef = useRef(false)

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
    if (isOpen && !wasOpenRef.current) {
      const shouldRestoreWorkspace = !workspaceMinimized
      restoreWorkspaceOnCloseRef.current = shouldRestoreWorkspace
      if (shouldRestoreWorkspace) {
        setWorkspaceMinimized(true)
      }
    } else if (!isOpen && wasOpenRef.current) {
      if (restoreWorkspaceOnCloseRef.current) {
        setWorkspaceMinimized(false)
        restoreWorkspaceOnCloseRef.current = false
      }
    }

    wasOpenRef.current = isOpen
  }, [isOpen, workspaceMinimized, setWorkspaceMinimized])

  useEffect(() => {
    return () => {
      if (restoreWorkspaceOnCloseRef.current) {
        setWorkspaceMinimized(false)
        restoreWorkspaceOnCloseRef.current = false
      }
    }
  }, [setWorkspaceMinimized])

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

  const entityTypeCounts = useMemo(() => {
    const counts = new Map<string, number>()
    for (const entity of entities) {
      const key = entity.type || 'untyped'
      counts.set(key, (counts.get(key) ?? 0) + 1)
    }
    return Array.from(counts.entries()).sort((a, b) => {
      if (b[1] !== a[1]) return b[1] - a[1]
      return a[0].localeCompare(b[0])
    })
  }, [entities])

  const relationshipsByEntity = useMemo(() => {
    const map = new Map<string, KBRelationship[]>()
    for (const relationship of relationships) {
      map.set(relationship.from, [...(map.get(relationship.from) ?? []), relationship])
      if (relationship.to !== relationship.from) {
        map.set(relationship.to, [...(map.get(relationship.to) ?? []), relationship])
      }
    }
    return map
  }, [relationships])

  const filteredEntityIds = useMemo(
    () => new Set(filteredEntities.map(entity => entity.id)),
    [filteredEntities],
  )

  const visibleRelationshipCount = useMemo(
    () =>
      relationships.filter(
        relationship =>
          filteredEntityIds.has(relationship.from) || filteredEntityIds.has(relationship.to),
      ).length,
    [relationships, filteredEntityIds],
  )

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
            <div className="space-y-4">
              <div className="grid gap-3 md:grid-cols-3">
                <div className="rounded-xl border border-border/70 bg-muted/20 p-4">
                  <div className="flex items-center gap-2 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
                    <Database className="w-3.5 h-3.5" />
                    Entities
                  </div>
                  <div className="mt-2 text-2xl font-semibold">{filteredEntities.length}</div>
                  <div className="mt-1 text-xs text-muted-foreground">
                    Showing {filteredEntities.length} of {entities.length}
                  </div>
                </div>
                <div className="rounded-xl border border-border/70 bg-muted/20 p-4">
                  <div className="flex items-center gap-2 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
                    <Network className="w-3.5 h-3.5" />
                    Relationships
                  </div>
                  <div className="mt-2 text-2xl font-semibold">{visibleRelationshipCount}</div>
                  <div className="mt-1 text-xs text-muted-foreground">
                    {typeFilter === 'all'
                      ? 'All visible graph links'
                      : `Filtered by type: ${typeFilter}`}
                  </div>
                </div>
                <div className="rounded-xl border border-border/70 bg-muted/20 p-4">
                  <div className="flex items-center gap-2 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
                    <GitBranch className="w-3.5 h-3.5" />
                    Entity Types
                  </div>
                  <div className="mt-2 text-2xl font-semibold">{entityTypes.length}</div>
                  <div className="mt-1 text-xs text-muted-foreground">
                    Distinct categories in graph.json
                  </div>
                </div>
              </div>

              {entityTypeCounts.length > 0 && (
                <div className="rounded-xl border border-border/70 bg-background p-4">
                  <div className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
                    Type Distribution
                  </div>
                  <div className="mt-3 flex flex-wrap gap-2">
                    {entityTypeCounts.map(([type, count]) => (
                      <button
                        key={type}
                        onClick={() => setTypeFilter(prev => (prev === type ? 'all' : type))}
                        className={`rounded-full border px-3 py-1 text-xs transition-colors ${
                          typeFilter === type
                            ? 'border-primary/50 bg-primary/10 text-primary'
                            : 'border-border bg-muted/40 text-foreground hover:bg-muted'
                        }`}
                      >
                        <span className="font-mono">{type}</span>
                        <span className="ml-2 text-muted-foreground">{count}</span>
                      </button>
                    ))}
                  </div>
                </div>
              )}

              {filteredEntities.length === 0 ? (
                <div className="rounded-xl border border-dashed border-border bg-muted/10 px-4 py-8 text-center text-sm text-muted-foreground">
                  No entities match the current search or type filter.
                </div>
              ) : (
                <div className="space-y-3">
                  {filteredEntities.map(e => {
                    const isOpenRow = expanded.has(e.id)
                    const relatedRels = relationshipsByEntity.get(e.id) ?? []
                    return (
                      <div
                        key={e.id}
                        className="overflow-hidden rounded-xl border border-border/70 bg-gradient-to-br from-background to-muted/20 shadow-sm"
                      >
                        <button
                          onClick={() => toggleExpanded(e.id)}
                          className="w-full px-4 py-3 text-left transition-colors hover:bg-muted/30"
                        >
                          <div className="flex items-start gap-3">
                            <div className="mt-0.5 flex h-9 w-9 flex-shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
                              <Database className="w-4 h-4" />
                            </div>
                            <div className="min-w-0 flex-1">
                              <div className="flex flex-wrap items-center gap-2">
                                <span className="rounded-full bg-muted px-2 py-0.5 text-[11px] font-mono text-muted-foreground">
                                  {e.type || 'untyped'}
                                </span>
                                <span className="text-sm font-semibold text-foreground">
                                  {e.label || e.id}
                                </span>
                                <span className="rounded-full border border-border bg-background px-2 py-0.5 text-[11px] text-muted-foreground">
                                  {relatedRels.length} link{relatedRels.length === 1 ? '' : 's'}
                                </span>
                              </div>
                              <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
                                <span className="font-mono break-all">{e.id}</span>
                                {e.source?.step && <span>step {e.source.step}</span>}
                                {e.updated_at && (
                                  <span>updated {new Date(e.updated_at).toLocaleString()}</span>
                                )}
                              </div>
                            </div>
                            {isOpenRow ? (
                              <ChevronDown className="mt-1 h-4 w-4 flex-shrink-0 text-muted-foreground" />
                            ) : (
                              <ChevronRight className="mt-1 h-4 w-4 flex-shrink-0 text-muted-foreground" />
                            )}
                          </div>
                        </button>
                        {isOpenRow && (
                          <div className="border-t border-border/70 bg-muted/10 p-4">
                            <div className="grid gap-4 lg:grid-cols-[minmax(0,1.1fr)_minmax(260px,0.9fr)]">
                              <div className="space-y-4">
                                <section className="rounded-lg border border-border/70 bg-background p-3">
                                  <div className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
                                    Properties
                                  </div>
                                  {e.properties && Object.keys(e.properties).length > 0 ? (
                                    <pre className="mt-2 overflow-x-auto whitespace-pre-wrap break-all rounded-md bg-muted/30 p-3 text-[11px] text-foreground">
                                      {JSON.stringify(e.properties, null, 2)}
                                    </pre>
                                  ) : (
                                    <div className="mt-2 text-xs text-muted-foreground">
                                      No structured properties on this entity.
                                    </div>
                                  )}
                                </section>
                              </div>
                              <div className="space-y-4">
                                <section className="rounded-lg border border-border/70 bg-background p-3">
                                  <div className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
                                    Metadata
                                  </div>
                                  <div className="mt-2 space-y-2 text-xs">
                                    {(e.source?.step || e.source?.run) && (
                                      <div className="flex flex-wrap gap-2">
                                        <span className="rounded bg-muted px-2 py-1 font-medium text-foreground">
                                          Source
                                        </span>
                                        <span className="font-mono text-muted-foreground">
                                          {e.source?.step ?? '?'} / {e.source?.run ?? '?'}
                                        </span>
                                      </div>
                                    )}
                                    {e.created_at && (
                                      <div className="text-muted-foreground">
                                        Created {new Date(e.created_at).toLocaleString()}
                                      </div>
                                    )}
                                    {e.updated_at && (
                                      <div className="text-muted-foreground">
                                        Updated {new Date(e.updated_at).toLocaleString()}
                                      </div>
                                    )}
                                    {!e.source?.step &&
                                      !e.source?.run &&
                                      !e.created_at &&
                                      !e.updated_at && (
                                        <div className="text-muted-foreground">
                                          No source or timestamp metadata.
                                        </div>
                                      )}
                                  </div>
                                </section>

                                <section className="rounded-lg border border-border/70 bg-background p-3">
                                  <div className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
                                    Relationships
                                  </div>
                                  {relatedRels.length > 0 ? (
                                    <div className="mt-2 space-y-2">
                                      {relatedRels.map(r => {
                                        const isOutgoing = r.from === e.id
                                        const counterpart = isOutgoing ? r.to : r.from
                                        return (
                                          <div
                                            key={r.id}
                                            className="rounded-md border border-border/60 bg-muted/20 px-3 py-2 text-xs"
                                          >
                                            <div className="flex flex-wrap items-center gap-2">
                                              <span className="font-mono text-muted-foreground">
                                                {isOutgoing ? 'outgoing' : 'incoming'}
                                              </span>
                                              <span className="rounded bg-background px-2 py-0.5 font-mono text-foreground">
                                                {r.type ?? '?'}
                                              </span>
                                            </div>
                                            <div className="mt-1 break-all font-mono text-muted-foreground">
                                              {isOutgoing ? `${e.id} -> ${counterpart}` : `${counterpart} -> ${e.id}`}
                                            </div>
                                          </div>
                                        )
                                      })}
                                    </div>
                                  ) : (
                                    <div className="mt-2 text-xs text-muted-foreground">
                                      No relationships connected to this entity.
                                    </div>
                                  )}
                                </section>
                              </div>
                            </div>
                          </div>
                        )}
                      </div>
                    )
                  })}
                </div>
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
