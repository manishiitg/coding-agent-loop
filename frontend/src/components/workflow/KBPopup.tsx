// Knowledgebase popup — viewer + simple management for knowledgebase/graph.json
// and knowledgebase/index.json. Reads via the existing workspace file API (same path
// as ReportViewer); writes via agentApi.updatePlannerFile.

import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  X,
  Database,
  Loader2,
  AlertCircle,
  Trash2,
  Download,
  RefreshCw,
  Search,
  ChevronDown,
  ChevronRight,
} from 'lucide-react'
import { agentApi } from '../../services/api'
import ConfirmationDialog from '../ui/ConfirmationDialog'

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

const EMPTY_GRAPH = `{
  "version": "1",
  "entities": [],
  "relationships": []
}
`

const EMPTY_INDEX = `{
  "entity_count": 0,
  "relationship_count": 0,
  "entity_types": [],
  "relationship_types": []
}
`

async function readJSON<T>(filepath: string): Promise<T | null> {
  try {
    const resp = await agentApi.getPlannerFileContent(filepath)
    if (!resp?.success || !resp.data?.content) return null
    return JSON.parse(resp.data.content) as T
  } catch {
    return null
  }
}

export default function KBPopup({ isOpen, onClose, workspacePath }: KBPopupProps) {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [graph, setGraph] = useState<KBGraph | null>(null)
  const [index, setIndex] = useState<KBIndex | null>(null)
  const [typeFilter, setTypeFilter] = useState<string>('all')
  const [search, setSearch] = useState('')
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [confirmClear, setConfirmClear] = useState(false)

  const graphPath = workspacePath ? `${workspacePath}/knowledgebase/graph.json` : null
  const indexPath = workspacePath ? `${workspacePath}/knowledgebase/index.json` : null

  const load = useCallback(async () => {
    if (!graphPath || !indexPath) return
    setLoading(true)
    setError(null)
    try {
      const [g, i] = await Promise.all([readJSON<KBGraph>(graphPath), readJSON<KBIndex>(indexPath)])
      setGraph(g)
      setIndex(i)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }, [graphPath, indexPath])

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

  const handleClear = async () => {
    if (!graphPath || !indexPath) return
    setConfirmClear(false)
    setLoading(true)
    setError(null)
    try {
      await agentApi.updatePlannerFile(graphPath, EMPTY_GRAPH)
      await agentApi.updatePlannerFile(indexPath, EMPTY_INDEX)
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
      setLoading(false)
    }
  }

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

  const hasGraphFile = graph !== null
  const hasAnyContent = (entities.length + relationships.length) > 0

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
      <div className="bg-background border border-border rounded-lg shadow-xl w-full max-w-4xl max-h-[90vh] flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between p-4 border-b border-border flex-shrink-0">
          <div className="flex items-center gap-2">
            <Database className="w-5 h-5 text-primary" />
            <h2 className="text-lg font-semibold">Knowledgebase</h2>
            <span className="text-xs text-muted-foreground ml-2">
              graph.json · entities + relationships
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
              onClick={() => setConfirmClear(true)}
              disabled={!hasAnyContent}
              className="p-1.5 rounded-md hover:bg-destructive/10 text-destructive transition-colors disabled:opacity-40"
              title="Clear knowledgebase"
            >
              <Trash2 className="w-4 h-4" />
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
          {index?.last_updated && (
            <div className="text-xs text-muted-foreground ml-auto">
              Last updated: {new Date(index.last_updated).toLocaleString()}
              {index.last_updated_by?.step ? ` · ${index.last_updated_by.step}` : ''}
              {index.last_updated_by?.run ? ` / ${index.last_updated_by.run}` : ''}
            </div>
          )}
        </div>

        {/* Filter controls */}
        {hasAnyContent && (
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
                Knowledgebase is empty. Facts appear here after steps run with a
                non-empty <code className="px-1 rounded bg-muted">knowledgebase_contribution</code>.
              </div>
            </div>
          )}

          {!loading && !error && hasAnyContent && (
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
        </div>
      </div>

      <ConfirmationDialog
        isOpen={confirmClear}
        onClose={() => setConfirmClear(false)}
        onConfirm={handleClear}
        title="Clear knowledgebase?"
        message={`This deletes all ${entities.length} entities and ${relationships.length} relationships. graph.json and index.json will be reset to empty. This cannot be undone.`}
        confirmText="Clear"
        type="danger"
      />
    </div>
  )
}
