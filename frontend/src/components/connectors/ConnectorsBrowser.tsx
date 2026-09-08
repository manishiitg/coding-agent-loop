import { useEffect, useMemo, useState } from 'react'
import { useShallow } from 'zustand/react/shallow'
import {
  Loader2,
  AlertCircle,
  Check,
  Code2,
  Download,
  ScrollText,
  Search,
  Wrench,
} from 'lucide-react'
import { OAuthStatusBadge } from '../OAuthStatusBadge'
import { isSelectedServer } from '../../utils/mcpServerAlias'
import ConnectionIcon from './ConnectionIcon'
import { brandSlugFor } from './brandSlug'
import { CATEGORY_ORDER, categoryFor, descriptionFor } from './catalog'
import { useMCPStore } from '../../stores'
import { READ_ONLY_TITLE, useCanWriteWorkflow } from '../../hooks/useCanWriteWorkflow'
import { useToolSelectionStore } from '../../stores/useToolSelectionStore'
import MCPConfigPopup from '../MCPConfigPopup'

/**
 * The card's status marker answers "is this mine?", which is what `connection`
 * reports. `status` answers a different question — whether the server is
 * currently reachable — so a connected-but-down server is surfaced as an
 * amber dot against the connected state rather than silently reading as not
 * connected. Rendered as a corner dot rather than a text line: it's the same
 * information in a fraction of the card's vertical space, and the label was
 * repeating what the connect/disconnect control already shows.
 */
const statusIndicator = (connection: string | undefined, status: string | undefined) => {
  if (connection === 'connected') {
    if (status === 'error') return { dot: 'bg-amber-500', title: 'Connected — unreachable' }
    if (status === 'loading') return { dot: 'bg-gray-400 animate-pulse', title: 'Connected — checking...' }
    if (status === 'not_loaded') return { dot: 'bg-gray-400', title: 'Connected — tools load when used' }
    return { dot: 'bg-green-500', title: 'Connected' }
  }
  return { dot: 'bg-gray-300 dark:bg-gray-600', title: 'Not connected' }
}

type Filter = 'all' | 'connected' | 'available'

const FILTERS: { value: Filter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'connected', label: 'Connected' },
  { value: 'available', label: 'Available' },
]

interface ConnectorsBrowserProps {
  // The embedded workflow-panel context: defaults the filter to "available"
  // (the framing is "here's what you can add", not connector management)
  // and drops the per-card connection-logs disclosure, which is a debugging
  // affordance that belongs in the full modal, not a tool-selection panel.
  compact?: boolean
  // When provided (the workflow-panel embedding), a connected card also gets
  // a checkbox to add/remove it from this workflow's own tool selection --
  // the point of surfacing "connect a new MCP" here in the first place.
  // Not shown for a not-yet-connected server: there's nothing to select
  // until it's connected via the OAuthStatusBadge action first.
  selectedServers?: string[]
  onToggleServer?: (serverName: string) => void
  workspacePath?: string | null
}

export default function ConnectorsBrowser({ compact = false, selectedServers, onToggleServer, workspacePath }: ConnectorsBrowserProps) {
  const {
    toolList,
    isLoadingTools,
    toolsError,
    getServerGroups,
    refreshTools,
    serverLogs,
    fetchServerLogs,
  } = useMCPStore(useShallow(state => ({
    toolList: state.toolList,
    isLoadingTools: state.isLoadingTools,
    toolsError: state.toolsError,
    getServerGroups: state.getServerGroups,
    refreshTools: state.refreshTools,
    serverLogs: state.serverLogs,
    fetchServerLogs: state.fetchServerLogs,
  })))

  // Refetch on mount rather than trusting whatever the store last held (app
  // boot, or the last time some other instance of this panel called
  // refreshTools). A chat-driven install_mcp_server call has no way to push
  // into this store directly, so a server installed since this panel was
  // last open would otherwise render as if it didn't exist.
  useEffect(() => {
    void refreshTools()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const [expandedLogs, setExpandedLogs] = useState<Set<string>>(new Set())
  const [loadingLogs, setLoadingLogs] = useState<Set<string>>(new Set())
  const [expandedTools, setExpandedTools] = useState<Set<string>>(new Set())
  // Connect/disconnect and the JSON-config import write shared MCP config
  // immediately; add-to-workflow changes this workflow's selection. All
  // disable for read-only users. Browsing, tool lists, and logs stay open.
  const readOnly = !useCanWriteWorkflow(workspacePath)
  const loadServerTools = useToolSelectionStore(state => state.loadServerTools)
  const getServerTools = useToolSelectionStore(state => state.getServerTools)
  const toggleToolsDisclosure = (serverName: string) => {
    setExpandedTools(current => {
      const next = new Set(current)
      if (next.has(serverName)) {
        next.delete(serverName)
      } else {
        next.add(serverName)
        void loadServerTools(serverName)
      }
      return next
    })
  }
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState<Filter>(compact ? 'available' : 'all')
  // Local to this instance -- deliberately independent of the store's global
  // showConfigEditor/showMCPDetails flags, which were owned by the (since
  // removed) top-menu MCP modal. Reusing a global flag here would let two
  // embedded browsers fight over one popup.
  const [showJsonConfig, setShowJsonConfig] = useState(false)

  const toggleLogs = async (serverName: string) => {
    const newExpanded = new Set(expandedLogs)
    if (newExpanded.has(serverName)) {
      newExpanded.delete(serverName)
      setExpandedLogs(newExpanded)
    } else {
      newExpanded.add(serverName)
      setExpandedLogs(newExpanded)
      setLoadingLogs((prev) => new Set([...prev, serverName]))
      await fetchServerLogs(serverName)
      setLoadingLogs((prev) => {
        const next = new Set(prev)
        next.delete(serverName)
        return next
      })
    }
  }

  const refreshLogs = async (serverName: string) => {
    setLoadingLogs((prev) => new Set([...prev, serverName]))
    await fetchServerLogs(serverName)
    setLoadingLogs((prev) => {
      const next = new Set(prev)
      next.delete(serverName)
      return next
    })
  }

  useEffect(() => {
    if (expandedLogs.size === 0) return

    const interval = window.setInterval(() => {
      expandedLogs.forEach((serverName) => {
        fetchServerLogs(serverName)
      })
    }, 3000)

    return () => window.clearInterval(interval)
  }, [expandedLogs, fetchServerLogs])

  const groups = getServerGroups()

  // Connected first, then alphabetical, so the services in use stay at the top
  // of the grid instead of scattering through it as tokens come and go.
  const visible = useMemo(() => {
    const q = query.trim().toLowerCase()
    return Object.entries(groups)
      .filter(([name, tools]) => {
        const connected = tools[0]?.connection === 'connected'
        if (filter === 'connected' && !connected) return false
        if (filter === 'available' && connected) return false
        if (!q) return true
        return (
          name.toLowerCase().includes(q) ||
          descriptionFor(name).toLowerCase().includes(q)
        )
      })
      .sort(([aName, aTools], [bName, bTools]) => {
        const aOk = aTools[0]?.connection === 'connected' ? 0 : 1
        const bOk = bTools[0]?.connection === 'connected' ? 0 : 1
        return aOk - bOk || aName.localeCompare(bName)
      })
  }, [groups, query, filter])

  // Group into directory sections, keeping the connected-first order inside
  // each one. Only sections holding a visible connector are rendered, so a
  // search matching two services does not leave four empty headings behind.
  const sections = useMemo(() => {
    const byCategory = new Map<string, typeof visible>()
    visible.forEach((entry) => {
      const category = categoryFor(entry[0])
      const bucket = byCategory.get(category)
      if (bucket) {
        bucket.push(entry)
      } else {
        byCategory.set(category, [entry])
      }
    })

    const rank = (category: string) => {
      const i = CATEGORY_ORDER.indexOf(category)
      return i === -1 ? CATEGORY_ORDER.length : i
    }

    return [...byCategory.entries()].sort(
      ([a], [b]) => rank(a) - rank(b) || a.localeCompare(b)
    )
  }, [visible])

  const total = Object.keys(groups).length
  const cardPadding = compact ? 'p-3' : 'p-4'
  const gridGap = compact ? 'gap-2' : 'gap-3'

  return (
    <div className="flex h-full min-h-0 flex-col">
      {/* Search + filter */}
      <div className="flex shrink-0 items-center gap-2">
        <div className="relative flex-1">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-400" />
          <input
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search connectors"
            aria-label="Search connectors"
            className="w-full rounded-lg border border-gray-300 bg-white py-2.5 pl-10 pr-3 text-sm text-gray-900 placeholder-gray-400 transition-colors focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500 dark:border-gray-700 dark:bg-gray-800 dark:text-gray-100"
          />
        </div>
        <div className="flex shrink-0 items-center rounded-lg border border-gray-300 p-0.5 dark:border-gray-700">
          {FILTERS.map((f) => (
            <button
              key={f.value}
              onClick={() => setFilter(f.value)}
              className={`rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${
                filter === f.value
                  ? 'bg-gray-900 text-white dark:bg-gray-100 dark:text-gray-900'
                  : 'text-gray-500 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-100'
              }`}
            >
              {f.label}
            </button>
          ))}
        </div>
        {compact && (
          <button
            type="button"
            onClick={() => setShowJsonConfig(true)}
            disabled={readOnly}
            className="flex shrink-0 items-center gap-1.5 rounded-lg border border-gray-300 px-3 py-2 text-xs font-medium text-gray-500 transition-colors hover:border-gray-400 hover:text-gray-900 disabled:cursor-not-allowed disabled:opacity-50 dark:border-gray-700 dark:text-gray-400 dark:hover:border-gray-600 dark:hover:text-gray-100"
            title={readOnly ? READ_ONLY_TITLE : 'Add a custom MCP server by pasting its JSON config'}
          >
            <Code2 className="h-3.5 w-3.5" />
            <span>Add via JSON</span>
          </button>
        )}
      </div>

      {/* Grid */}
      <div className="min-h-0 flex-1 overflow-y-auto pt-5">
        <div className="mb-3 flex items-baseline justify-between">
          <h3 className="text-sm font-semibold text-gray-900 dark:text-gray-100">
            {filter === 'connected'
              ? 'Connected'
              : filter === 'available'
                ? 'Available connectors'
                : 'All connectors'}
          </h3>
          <span className="text-xs text-gray-500 dark:text-gray-400">
            {visible.length === total
              ? `${total} total`
              : `${visible.length} of ${total}`}
          </span>
        </div>

        {isLoadingTools && toolList.length === 0 && (
          <div className="flex items-center gap-2 py-8 text-sm text-gray-500 dark:text-gray-400">
            <Loader2 className="h-4 w-4 animate-spin" />
            <span>Loading connectors...</span>
          </div>
        )}

        {toolsError && (
          <div className="flex items-center gap-2 py-3 text-sm text-red-500 dark:text-red-400">
            <AlertCircle className="h-4 w-4" />
            <span>Error: {toolsError}</span>
          </div>
        )}

        {!isLoadingTools && visible.length === 0 && !toolsError && (
          <p className="py-8 text-center text-sm text-gray-500 dark:text-gray-400">
            {total === 0
              // An empty catalog is a deployment fact, not a filter result:
              // this server's MCP config lists no connectors at all, so
              // neither "all connected" nor "pick one" would be true.
              ? 'No connectors are configured on this server. The deployment’s MCP server config lists none.'
              : query
                ? `No connectors match "${query}".`
                : filter === 'connected'
                  ? 'No connectors yet. Pick one from Available and press Connect.'
                  : filter === 'available'
                    ? 'Every connector is already connected.'
                    : 'No connectors configured.'}
          </p>
        )}

        {sections.map(([category, entries]) => (
          <section key={category} className={compact ? 'mb-4 last:mb-0' : 'mb-6 last:mb-0'}>
            <h4 className="mb-3 border-b border-gray-200 pb-2 text-sm font-semibold text-gray-900 dark:border-gray-800 dark:text-gray-100">
              {category}
            </h4>
            <div className={`grid grid-cols-1 ${gridGap} md:grid-cols-2`}>
              {entries.map(([serverName, tools]) => {
            // eslint-disable-next-line @typescript-eslint/no-explicit-any
            const requiresOAuth = (tools[0] as any).requires_oauth as boolean | undefined
            const status = tools[0]?.status
            const connection = tools[0]?.connection
            const isOpen = expandedLogs.has(serverName)
            const isToolsOpen = expandedTools.has(serverName)

            return (
              <div
                key={serverName}
                className="flex flex-col rounded-xl border border-gray-200 bg-white transition-colors hover:border-gray-300 dark:border-gray-800 dark:bg-gray-900/60 dark:hover:border-gray-700"
              >
                <div className={`flex items-start gap-3 ${cardPadding}`}>
                  <ConnectionIcon
                    icon={brandSlugFor(serverName)}
                    name={serverName}
                    size="lg"
                  />

                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-1.5">
                      <span className="truncate text-sm font-semibold text-gray-900 dark:text-gray-100">
                        {serverName}
                      </span>
                    </div>
                    <p className="mt-0.5 line-clamp-2 text-xs leading-relaxed text-gray-500 dark:text-gray-400">
                      {descriptionFor(serverName)}
                    </p>
                  </div>

                  <span
                    className={`mt-1.5 h-2 w-2 shrink-0 rounded-full ${statusIndicator(connection, status).dot}`}
                    title={statusIndicator(connection, status).title}
                    aria-label={statusIndicator(connection, status).title}
                  />
                </div>

                {/* Action bar -- one row of equal-height controls, primary
                    action stretched, secondary ones as matching icon squares. */}
                <div className="mt-auto flex items-center gap-1.5 border-t border-gray-200 px-3 py-2 dark:border-gray-800">
                  {onToggleServer && connection === 'connected' && (
                    <button
                      onClick={() => onToggleServer(serverName)}
                      disabled={readOnly}
                      className={`flex h-8 flex-1 items-center justify-center gap-1.5 rounded-lg border text-xs font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-50 ${
                        isSelectedServer(selectedServers || [], serverName)
                          ? 'border-blue-500/40 bg-blue-500/10 text-blue-600 hover:border-red-500/40 hover:bg-red-500/10 hover:text-red-500 dark:text-blue-300'
                          : 'border-gray-300 text-gray-600 hover:bg-gray-100 hover:text-gray-900 dark:border-gray-600 dark:text-gray-300 dark:hover:bg-gray-800 dark:hover:text-gray-100'
                      }`}
                      title={readOnly ? READ_ONLY_TITLE : `${isSelectedServer(selectedServers || [], serverName) ? 'Remove' : 'Add'} ${serverName} for this workflow`}
                    >
                      {isSelectedServer(selectedServers || [], serverName) ? (
                        <>
                          <Check className="h-3.5 w-3.5" />
                          <span>Added</span>
                        </>
                      ) : (
                        <>
                          <Download className="h-3.5 w-3.5" />
                          <span>Add to workflow</span>
                        </>
                      )}
                    </button>
                  )}
                  {connection === 'connected' && (
                    <button
                      onClick={() => toggleToolsDisclosure(serverName)}
                      className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border transition-colors ${
                        isToolsOpen
                          ? 'border-gray-400 bg-gray-100 text-gray-700 dark:border-gray-500 dark:bg-gray-800 dark:text-gray-200'
                          : 'border-gray-300 text-gray-500 hover:bg-gray-100 hover:text-gray-900 dark:border-gray-600 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-gray-100'
                      }`}
                      title="Tools"
                      aria-label={`${isToolsOpen ? 'Hide' : 'Show'} tools for ${serverName}`}
                    >
                      <Wrench className="h-3.5 w-3.5" />
                    </button>
                  )}
                  <button
                    onClick={() => toggleLogs(serverName)}
                    className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border transition-colors ${
                      isOpen
                        ? 'border-gray-400 bg-gray-100 text-gray-700 dark:border-gray-500 dark:bg-gray-800 dark:text-gray-200'
                        : 'border-gray-300 text-gray-500 hover:bg-gray-100 hover:text-gray-900 dark:border-gray-600 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-gray-100'
                    }`}
                    title="Connection logs"
                    aria-label={`${isOpen ? 'Hide' : 'Show'} logs for ${serverName}`}
                  >
                    <ScrollText className="h-3.5 w-3.5" />
                  </button>
                  <OAuthStatusBadge
                    serverName={serverName}
                    requiresOAuth={requiresOAuth}
                    connection={connection}
                    variant="icon"
                    readOnly={readOnly}
                    onAuthChange={() => {
                      // Refresh on disconnect too — the card's status text
                      // reads from the store, so skipping this left it
                      // claiming "Connected" after the token was revoked.
                      refreshTools()
                    }}
                  />
                </div>

                {/* Connection Logs Panel */}
                {isOpen && (
                  <div className="mx-3 mb-3 rounded-md bg-gray-900 px-3 py-2 dark:bg-black">
                    <div className="mb-2 flex items-center justify-between">
                      <h5 className="text-[10px] font-semibold uppercase tracking-wide text-gray-400">
                        Connection Logs
                      </h5>
                      <button
                        onClick={() => refreshLogs(serverName)}
                        disabled={loadingLogs.has(serverName)}
                        className="text-xs text-gray-400 transition-colors hover:text-gray-200 disabled:opacity-50"
                      >
                        {loadingLogs.has(serverName) ? '...' : 'Refresh'}
                      </button>
                    </div>
                    <div className="max-h-40 space-y-0.5 overflow-y-auto font-mono text-xs">
                      {loadingLogs.has(serverName) && !serverLogs[serverName]?.length ? (
                        <div className="flex items-center gap-2 text-gray-400">
                          <div className="h-3 w-3 animate-spin rounded-full border border-gray-500 border-t-blue-400"></div>
                          Loading logs...
                        </div>
                      ) : serverLogs[serverName]?.length ? (
                        serverLogs[serverName].map((log, i) => (
                          <div key={i} className="flex gap-2 py-0.5">
                            <span className="shrink-0 whitespace-nowrap text-gray-500">
                              {new Date(log.timestamp).toLocaleTimeString()}
                            </span>
                            <span
                              className={
                                log.level === 'error'
                                  ? 'text-red-400'
                                  : log.level === 'warn'
                                    ? 'text-yellow-400'
                                    : log.level === 'debug'
                                      ? 'text-gray-500'
                                      : 'text-green-400'
                              }
                            >
                              {log.message}
                            </span>
                          </div>
                        ))
                      ) : (
                        <div className="text-gray-500">No logs available yet.</div>
                      )}
                    </div>
                  </div>
                )}

                {/* Tools panel -- names as compact chips rather than a list of
                    full descriptions. The name is what a user scans for; a
                    paragraph per tool blows the card's height out and wraps
                    badly in a narrow grid column. Descriptions stay one hover
                    away. */}
                {isToolsOpen && (() => {
                  const serverTools = getServerTools(serverName)
                  return (
                    <div className="border-t border-gray-200 px-3 py-2.5 dark:border-gray-800">
                      {serverTools === undefined ? (
                        <div className="flex items-center gap-2 text-xs text-gray-400">
                          <Loader2 className="h-3 w-3 animate-spin" />
                          Loading tools...
                        </div>
                      ) : serverTools.length > 0 ? (
                        <>
                          <div className="mb-1.5 text-[10px] font-semibold uppercase tracking-wide text-gray-400">
                            {serverTools.length} tool{serverTools.length === 1 ? '' : 's'}
                          </div>
                          <div className="flex flex-wrap gap-1">
                            {serverTools.map(tool => (
                              <span
                                key={tool.name}
                                title={tool.description || tool.name}
                                className="cursor-default rounded border border-gray-200 bg-gray-50 px-1.5 py-0.5 font-mono text-[11px] text-gray-600 transition-colors hover:border-gray-300 hover:text-gray-900 dark:border-gray-700 dark:bg-gray-800/60 dark:text-gray-400 dark:hover:border-gray-600 dark:hover:text-gray-100"
                              >
                                {tool.name}
                              </span>
                            ))}
                          </div>
                        </>
                      ) : (
                        <div className="text-xs text-gray-500">No tools reported by this server.</div>
                      )}
                    </div>
                  )
                })()}
              </div>
              )
              })}
            </div>
          </section>
        ))}
      </div>

      {showJsonConfig && (
        <MCPConfigPopup
          initialView="json"
          onConfigChange={() => refreshTools()}
          onClose={() => setShowJsonConfig(false)}
        />
      )}
    </div>
  )
}
