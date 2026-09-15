import { useEffect, useMemo, useState } from 'react'
import { useShallow } from 'zustand/react/shallow'
import {
  Loader2,
  AlertCircle,
  Code2,
  Search,
} from 'lucide-react'
import ConnectionIcon from './ConnectionIcon'
import { brandSlugFor } from './brandSlug'
import { CATEGORY_ORDER, categoryFor, descriptionFor } from './catalog'
import { useMCPStore } from '../../stores'
import { useAuthStore } from '../../stores/useAuthStore'
import { READ_ONLY_TITLE, useCanWriteWorkflow } from '../../hooks/useCanWriteWorkflow'
import MCPConfigPopup from '../MCPConfigPopup'
import { AskAIButton } from '../workflow/AskAIButton'

/**
 * The card's status marker answers whether this platform connection exists.
 * `status` answers a different question — whether the server is
 * currently reachable — so a connected-but-down server is surfaced as an
 * amber dot against the connected state rather than silently reading as not
 * connected. A dot beside the provider name keeps status visible without
 * adding another row to the card.
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
  // The embedded workflow panel defaults to available connectors.
  compact?: boolean
  workspacePath?: string | null
  /** Terminology for the workspace receiving the connection. */
  workspaceLabel?: string
  /** Name of the conversational agent that helps with setup. */
  assistantLabel?: string
  /** Optional product-owned chat delivery; workflows use their existing lane. */
  onAskAI?: (message: string) => void | Promise<void>
  /** Let an embedding scroll the complete page instead of only this list. */
  manageOwnScroll?: boolean
}

export default function ConnectorsBrowser({
  compact = false,
  workspacePath,
  workspaceLabel = 'workflow',
  assistantLabel = 'builder',
  onAskAI,
  manageOwnScroll = true,
}: ConnectorsBrowserProps) {
  const {
    toolList,
    isLoadingTools,
    toolsError,
    getServerGroups,
    refreshTools,
  } = useMCPStore(useShallow(state => ({
    toolList: state.toolList,
    isLoadingTools: state.isLoadingTools,
    toolsError: state.toolsError,
    getServerGroups: state.getServerGroups,
    refreshTools: state.refreshTools,
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

  const canWriteWorkflow = useCanWriteWorkflow(workspacePath)
  const canManagePlatformMCP = useAuthStore(state =>
    state.user?.is_admin === true || (state.isMultiUserModeChecked && !state.isMultiUserMode),
  )
  const managementDisabledTitle = !canManagePlatformMCP
    ? 'Only a platform administrator can change shared MCP connections'
    : READ_ONLY_TITLE
  // Everyone can reuse connected MCPs; only platform admins may change the
  // deployment-wide connection registry.
  const readOnly = !canWriteWorkflow || !canManagePlatformMCP
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState<Filter>(compact ? 'available' : 'all')
  // Local to this instance -- deliberately independent of the store's global
  // showConfigEditor/showMCPDetails flags, which were owned by the (since
  // removed) top-menu MCP modal. Reusing a global flag here would let two
  // embedded browsers fight over one popup.
  const [showJsonConfig, setShowJsonConfig] = useState(false)

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
    <div className={manageOwnScroll ? 'flex h-full min-h-0 flex-col' : 'flex flex-col'}>
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
            title={readOnly ? managementDisabledTitle : 'Add a custom shared MCP server by pasting its JSON config'}
          >
            <Code2 className="h-3.5 w-3.5" />
            <span>Add via JSON</span>
          </button>
        )}
      </div>
      <div className="mt-3 flex shrink-0 flex-wrap items-center gap-4 rounded-xl border border-primary/20 bg-primary/5 p-4">
        <div className="min-w-0 flex-1 basis-56">
          <p className="text-base font-semibold text-foreground">Need another connection?</p>
          <p className="mt-1 text-sm leading-6 text-muted-foreground">
            MCP connections are shared across AgentWorks, including Work, workflows, chats, and schedules. {canManagePlatformMCP
              ? `Connecting an account makes it available to every user; ${assistantLabel} can help an administrator add one.`
              : 'You can use connected services here; only a platform administrator can add, reconnect, or disconnect them.'}
          </p>
        </div>
        <AskAIButton
          workspacePath={readOnly ? null : workspacePath ?? null}
          onAsk={onAskAI}
          label={canManagePlatformMCP ? 'Add platform MCP' : 'Why admin access?'}
          message={canManagePlatformMCP
            ? query.trim()
              ? `Help me add an MCP server for ${JSON.stringify(query.trim())} to this ${workspaceLabel}. Search the catalog and official provider documentation on the web, and help me connect it. Before authorization, remind me that the account will be shared by every AgentWorks user and product.`
              : `Help me add a shared platform MCP server to this ${workspaceLabel}. Ask which app or service I want, then search the catalog and official provider documentation and help me connect it. Before authorization, remind me that the account will be shared by every AgentWorks user and product.`
            : `Explain how shared platform MCP connections work in AgentWorks, why only an administrator can add or replace one, and how I can reuse an already-connected service in this ${workspaceLabel}. Do not attempt to change MCP configuration.`}
          className="inline-flex shrink-0 items-center justify-center gap-2 rounded-lg bg-primary px-4 py-2.5 text-sm font-medium text-primary-foreground shadow-sm transition-colors hover:bg-primary/90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
        />
      </div>

      {/* Grid */}
      <div className={manageOwnScroll ? 'min-h-0 flex-1 overflow-y-auto pt-5' : 'pt-5'}>
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
                ? `No connectors match "${query}". Ask in chat — it can search the web and connect one directly, even if it isn't in this list.`
                : filter === 'connected'
                  ? 'No connectors yet. Pick one from Available and choose Ask AI.'
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
            const status = tools[0]?.status
            const connection = tools[0]?.connection

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
                      <span
                        className={`h-2 w-2 shrink-0 rounded-full ${statusIndicator(connection, status).dot}`}
                        title={statusIndicator(connection, status).title}
                        aria-label={statusIndicator(connection, status).title}
                      />
                      {connection === 'connected' && (
                        <span className="rounded-full bg-blue-500/10 px-1.5 py-0.5 text-[10px] font-medium text-blue-600 dark:text-blue-400">
                          Shared platform connection
                        </span>
                      )}
                    </div>
                    <p className="mt-0.5 line-clamp-2 text-xs leading-relaxed text-gray-500 dark:text-gray-400">
                      {descriptionFor(serverName)}
                    </p>
                  </div>

                  <div className="flex shrink-0 flex-col items-center gap-1 self-center">
                    <AskAIButton
                      workspacePath={readOnly ? null : workspacePath ?? null}
                      onAsk={onAskAI}
                      iconOnly
                      label={`Ask AI about ${serverName}`}
                      message={connection === 'connected'
                        ? `Help me with the existing ${JSON.stringify(serverName)} MCP connection in this ${workspaceLabel}. Check its current connection status and ${workspaceLabel} selection, then ask what I want to do with it.`
                        : canManagePlatformMCP
                          ? `Help me connect ${JSON.stringify(serverName)} to this ${workspaceLabel}. It is already listed in the MCP catalog, so check its existing configuration and connection status first and reuse it. Before authorization, remind me that the account will be shared by every AgentWorks user and product. Guide me through the required authorization or secure credential setup, verify its tools, then add it to this ${workspaceLabel}. Do not ask me to paste secrets into chat.`
                          : `Explain that ${JSON.stringify(serverName)} is not yet connected to this AgentWorks platform, that only an administrator can connect the shared external account, and how I can select it for this ${workspaceLabel} after an administrator connects it. Do not attempt to change MCP configuration.`}
                      className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-primary/10 hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
                    />
                  </div>
                </div>


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
