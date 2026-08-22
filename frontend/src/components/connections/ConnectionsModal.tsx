import { useEffect, useMemo, useRef, useState } from 'react'
import {
  X,
  Search,
  Plus,
  ChevronLeft,
  ChevronDown,
  Loader2,
  Code2,
  Plug,
} from 'lucide-react'
import ModalPortal from '../ui/ModalPortal'
import ConnectionRow, { CONNECTION_GRID } from './ConnectionRow'
import PopularCard from './PopularCard'
import ConnectFlowModal from './ConnectFlowModal'
import ErrorNotice from './ErrorNotice'
import MCPServersSection from '../sidebar/MCPServersSection'
import { useConnectionsStore } from '../../stores/useConnectionsStore'
import { buildRows, filterRows, pickPopular, type ConnectionFilter } from './connectionsTableUtils'
import type { CatalogEntry } from '../../services/connectionsApi'

type View = 'list' | 'custom'

const FILTERS: { id: ConnectionFilter; label: string }[] = [
  { id: 'all', label: 'All' },
  { id: 'connected', label: 'Connected' },
  { id: 'not_connected', label: 'Not connected' },
]

/**
 * Featured in the Popular strip, in this order, when present in the catalog.
 * All three register their own OAuth client, so each is genuinely one click —
 * featuring an entry that needs server credentials would put a blocked card in
 * the most prominent place on the screen.
 */
const POPULAR_IDS = ['notion', 'linear', 'atlassian']

interface ConnectionsModalProps {
  onClose: () => void
}

/**
 * The Connections hub. Replaces "MCP Servers" as the default consumer surface:
 * a catalog-driven table with one-click connect, with raw MCP configuration
 * kept a click away under Add -> Custom MCP server.
 */
export default function ConnectionsModal({ onClose }: ConnectionsModalProps) {
  const {
    catalog,
    connections,
    summary,
    isLoadingCatalog,
    isLoadingConnections,
    connectingId,
    disconnectingId,
    loadError,
    actionError,
    refresh,
    loadConnections,
    connect,
    disconnect,
    clearActionError,
  } = useConnectionsStore()

  const [view, setView] = useState<View>('list')
  const [filter, setFilter] = useState<ConnectionFilter>('all')
  const [query, setQuery] = useState('')
  const [searchOpen, setSearchOpen] = useState(false)
  const [addOpen, setAddOpen] = useState(false)
  const [flowEntry, setFlowEntry] = useState<CatalogEntry | null>(null)
  const searchRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    refresh()
  }, [refresh])

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      // The connect flow owns Escape while it is open.
      if (e.key === 'Escape' && !flowEntry) onClose()
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [onClose, flowEntry])

  useEffect(() => {
    if (searchOpen) searchRef.current?.focus()
  }, [searchOpen])

  const connectionById = useMemo(
    () => new Map(connections.map(c => [c.id, c])),
    [connections]
  )
  const catalogById = useMemo(() => new Map(catalog.map(e => [e.id, e])), [catalog])

  const rows = useMemo(() => buildRows(catalog, connections), [catalog, connections])
  const visibleRows = useMemo(() => filterRows(rows, filter, query), [rows, filter, query])
  const popular = useMemo(() => pickPopular(catalog, POPULAR_IDS), [catalog])

  // Connecting always runs through the guided flow when the integration is in
  // the catalog; custom servers have nothing to review, so they connect directly.
  const startConnect = (id: string) => {
    const entry = catalogById.get(id)
    if (entry) setFlowEntry(entry)
    else connect(id)
  }

  const isLoading = isLoadingCatalog || isLoadingConnections

  return (
    <ModalPortal>
      <div
        className="fixed inset-0 z-[55] flex items-center justify-center bg-black/50 p-4"
        onClick={onClose}
      >
        <div
          role="dialog"
          aria-modal="true"
          aria-label="Connections"
          onClick={e => e.stopPropagation()}
          className="flex h-[85vh] max-h-[860px] w-full max-w-4xl flex-col overflow-hidden rounded-xl border border-gray-200 bg-white shadow-2xl dark:border-slate-700 dark:bg-slate-900"
        >
          {/* Header */}
          <div className="flex shrink-0 items-center gap-3 border-b border-gray-200 px-5 py-3.5 dark:border-slate-700">
            {view !== 'list' ? (
              <button
                type="button"
                onClick={() => {
                  setView('list')
                  loadConnections()
                }}
                className="-ml-1 flex items-center gap-1.5 rounded-md px-2 py-1 text-lg font-semibold text-gray-900 hover:bg-gray-100 dark:text-gray-100 dark:hover:bg-slate-800"
              >
                <ChevronLeft className="h-5 w-5" aria-hidden="true" />
                {view === 'custom' ? 'Custom MCP' : 'Connections'}
              </button>
            ) : (
              <h2 className="text-lg font-semibold text-gray-900 dark:text-gray-100">
                Connections
              </h2>
            )}

            {view === 'list' && summary.total > 0 && (
              <span className="text-xs text-gray-500 dark:text-gray-400">
                {summary.connected} connected
                {summary.needs_attention > 0 && ` · ${summary.needs_attention} needs attention`}
              </span>
            )}

            <div className="ml-auto flex items-center gap-2">
              {view === 'list' && (
                <>
                  {searchOpen ? (
                    <input
                      ref={searchRef}
                      type="search"
                      value={query}
                      onChange={e => setQuery(e.target.value)}
                      onBlur={() => !query && setSearchOpen(false)}
                      placeholder="Search connections"
                      aria-label="Search connections"
                      className="w-52 rounded-md border border-gray-300 bg-white px-3 py-1.5 text-sm text-gray-900 focus:border-blue-500 focus:ring-2 focus:ring-blue-500 dark:border-slate-600 dark:bg-slate-800 dark:text-gray-100"
                    />
                  ) : (
                    <button
                      type="button"
                      onClick={() => setSearchOpen(true)}
                      className="rounded-md p-2 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-slate-800"
                      aria-label="Search connections"
                    >
                      <Search className="h-4 w-4" />
                    </button>
                  )}

                  <div className="relative">
                    <button
                      type="button"
                      onClick={() => setAddOpen(v => !v)}
                      aria-expanded={addOpen}
                      className="flex items-center gap-1.5 rounded-md border border-gray-300 px-3 py-1.5 text-sm font-medium text-gray-800 transition-colors hover:bg-gray-100 dark:border-slate-600 dark:text-gray-200 dark:hover:bg-slate-800"
                    >
                      <Plus className="h-3.5 w-3.5" aria-hidden="true" />
                      Add
                      <ChevronDown className="h-3.5 w-3.5" aria-hidden="true" />
                    </button>

                    {addOpen && (
                      <>
                        <div
                          className="fixed inset-0 z-10"
                          onClick={() => setAddOpen(false)}
                          aria-hidden="true"
                        />
                        <div className="absolute right-0 z-20 mt-1 w-60 overflow-hidden rounded-md border border-gray-200 bg-white py-1 shadow-lg dark:border-slate-700 dark:bg-slate-800">
                          <button
                            type="button"
                            onClick={() => {
                              setAddOpen(false)
                              setFilter('not_connected')
                              setView('list')
                            }}
                            className="flex w-full items-start gap-2.5 px-3 py-2 text-left transition-colors hover:bg-gray-100 dark:hover:bg-slate-700"
                          >
                            <Plug
                              className="mt-0.5 h-4 w-4 shrink-0 text-gray-500 dark:text-gray-400"
                              aria-hidden="true"
                            />
                            <span>
                              <span className="block text-sm text-gray-900 dark:text-gray-100">
                                Browse integrations
                              </span>
                              <span className="block text-xs text-gray-500 dark:text-gray-400">
                                Connect a service in one click
                              </span>
                            </span>
                          </button>
                          <button
                            type="button"
                            onClick={() => {
                              setAddOpen(false)
                              setView('custom')
                            }}
                            className="flex w-full items-start gap-2.5 px-3 py-2 text-left transition-colors hover:bg-gray-100 dark:hover:bg-slate-700"
                          >
                            <Code2
                              className="mt-0.5 h-4 w-4 shrink-0 text-gray-500 dark:text-gray-400"
                              aria-hidden="true"
                            />
                            <span>
                              <span className="block text-sm text-gray-900 dark:text-gray-100">
                                Custom MCP server
                              </span>
                              <span className="block text-xs text-gray-500 dark:text-gray-400">
                                Advanced: HTTP or local stdio
                              </span>
                            </span>
                          </button>
                        </div>
                      </>
                    )}
                  </div>
                </>
              )}

              <button
                type="button"
                onClick={onClose}
                className="rounded-md p-2 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-slate-800"
                aria-label="Close connections"
              >
                <X className="h-4 w-4" />
              </button>
            </div>
          </div>

          {/* Body */}
          <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
            {view === 'custom' ? (
              <div className="space-y-3">
                <p className="rounded-md bg-gray-50 p-3 text-xs text-gray-600 dark:bg-slate-800 dark:text-gray-400">
                  Connect a Streamable HTTP or local stdio MCP server directly, and inspect
                  discovered tools, server logs, and raw configuration.
                </p>
                <MCPServersSection />
              </div>
            ) : (
              <>
                {loadError && (
                  <div className="mb-4">
                    <ErrorNotice error={loadError} onAction={() => refresh()} compact />
                  </div>
                )}
                {actionError && (
                  <div className="mb-4">
                    <ErrorNotice
                      error={actionError}
                      onAction={action => {
                        clearActionError()
                        if (action === 'open_advanced') setView('custom')
                        else loadConnections()
                      }}
                      compact
                    />
                  </div>
                )}
                {/* Popular */}
                {popular.length > 0 && (
                  <section className="mb-6">
                    <h3 className="mb-2.5 text-sm text-gray-500 dark:text-gray-400">Popular</h3>
                    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
                      {popular.map(entry => (
                        <PopularCard
                          key={entry.id}
                          entry={entry}
                          connection={connectionById.get(entry.id)}
                          isConnecting={connectingId === entry.id}
                          onConnect={() => startConnect(entry.id)}
                        />
                      ))}
                    </div>
                  </section>
                )}

                {/* Filters */}
                <div className="mb-3 flex items-center gap-1">
                  {FILTERS.map(({ id, label }) => (
                    <button
                      key={id}
                      type="button"
                      onClick={() => setFilter(id)}
                      aria-pressed={filter === id}
                      className={`rounded-md px-3 py-1.5 text-sm font-medium transition-colors ${
                        filter === id
                          ? 'bg-gray-100 text-gray-900 dark:bg-slate-700 dark:text-gray-100'
                          : 'text-gray-500 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-200'
                      }`}
                    >
                      {label}
                    </button>
                  ))}
                </div>

                {/* Table */}
                <div className={`grid ${CONNECTION_GRID} items-center gap-4 border-b border-gray-200 px-3 pb-2 dark:border-slate-700`}>
                  <span className="text-xs font-medium text-gray-500 dark:text-gray-400">
                    Connector
                  </span>
                  <span className="text-xs font-medium text-gray-500 dark:text-gray-400">
                    Type
                  </span>
                  <span className="text-xs font-medium text-gray-500 dark:text-gray-400">
                    Status
                  </span>
                </div>

                <div className="divide-y divide-gray-100 dark:divide-slate-800">
                  {isLoading && rows.length === 0 && (
                    <div className="flex items-center gap-2 py-8 text-sm text-gray-500 dark:text-gray-400">
                      <Loader2 className="h-4 w-4 animate-spin" aria-hidden="true" />
                      Loading connections...
                    </div>
                  )}

                  {!isLoading && visibleRows.length === 0 && (
                    <p className="py-10 text-center text-sm text-gray-500 dark:text-gray-400">
                      {query
                        ? `No connections match "${query}"`
                        : filter === 'connected'
                          ? 'Nothing connected yet.'
                          : 'Everything is connected.'}
                    </p>
                  )}

                  {visibleRows.map(row => (
                    <ConnectionRow
                      key={row.id}
                      entry={row.entry}
                      connection={row.connection}
                      isConnecting={connectingId === row.id}
                      isDisconnecting={disconnectingId === row.id}
                      onConnect={() => startConnect(row.id)}
                      onDisconnect={() => disconnect(row.id)}
                    />
                  ))}
                </div>
              </>
            )}
          </div>
        </div>
      </div>

      {flowEntry && (
        <ConnectFlowModal
          entry={flowEntry}
          onClose={() => {
            setFlowEntry(null)
            loadConnections()
          }}
        />
      )}
    </ModalPortal>
  )
}
