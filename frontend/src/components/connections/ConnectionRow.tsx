import { Loader2, Plug, PlugZap } from 'lucide-react'
import ConnectionIcon from './ConnectionIcon'
import type { CatalogEntry, Connection, ConnectionTransport } from '../../services/connectionsApi'

const TRANSPORT_LABELS: Record<ConnectionTransport, string> = {
  web: 'Web',
  local: 'Local',
}

/** Shared column template, so the header and every row stay aligned. */
export const CONNECTION_GRID = 'grid-cols-[minmax(0,1fr)_7rem_9rem]'

interface ConnectionRowProps {
  /** Catalog metadata. Absent for custom MCP servers the user added by hand. */
  entry?: CatalogEntry
  /** Present once the integration has been provisioned. */
  connection?: Connection
  isConnecting?: boolean
  isDisconnecting?: boolean
  onConnect: () => void
  onDisconnect: () => void
}

/**
 * One row of the connections table: identity, transport, and a single button
 * that both reports the state and changes it. The row is the whole surface for
 * a connector — there is no separate page behind it.
 */
export default function ConnectionRow({
  entry,
  connection,
  isConnecting = false,
  isDisconnecting = false,
  onConnect,
  onDisconnect,
}: ConnectionRowProps) {
  const name = entry?.name ?? connection?.name ?? 'Unknown'
  const icon = entry?.icon ?? connection?.icon
  const brandColor = entry?.brand_color ?? connection?.brand_color
  const transport = entry?.transport ?? connection?.transport ?? 'web'

  const health = connection?.health ?? 'not_connected'
  const isConnected = health === 'connected'
  const comingSoon = entry?.status === 'coming_soon'
  const setupRequired = entry?.setup_required ?? health === 'setup_required'

  // Blocked states get muted explanatory text rather than a dead button, so the
  // user learns why instead of clicking something that cannot work.
  const blockedLabel = comingSoon
    ? 'Coming soon'
    : setupRequired
      ? 'Setup required'
      : null

  return (
    <div className={`group grid ${CONNECTION_GRID} items-center gap-4 rounded-lg px-3 py-2.5 transition-colors hover:bg-gray-50 dark:hover:bg-slate-700/40`}>
      {/* Connector — identity only; the row's actions live in the last column */}
      <div className="flex min-w-0 items-center gap-3">
        <ConnectionIcon icon={icon} brandColor={brandColor} size="sm" />
        <span className="truncate text-sm font-medium text-gray-900 dark:text-gray-100">
          {name}
        </span>
        {connection?.custom && (
          <span className="shrink-0 rounded bg-gray-100 px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide text-gray-500 dark:bg-slate-700 dark:text-gray-400">
            Custom
          </span>
        )}
      </div>

      {/* Type */}
      <span className="text-sm text-gray-500 dark:text-gray-400">
        {TRANSPORT_LABELS[transport]}
      </span>

      {/* Status — the button IS the state: what it says is what this row is
          not, and pressing it is how you change that. */}
      <div className="flex items-center justify-start">
        {blockedLabel ? (
          <span className="text-sm text-gray-400 dark:text-gray-500">{blockedLabel}</span>
        ) : isConnected ? (
          <button
            type="button"
            onClick={onDisconnect}
            disabled={isDisconnecting}
            className="flex items-center gap-1.5 rounded-md border border-gray-300 px-3 py-1.5 text-xs font-medium text-gray-800 transition-colors hover:bg-gray-100 disabled:cursor-not-allowed disabled:opacity-50 dark:border-slate-600 dark:text-gray-200 dark:hover:bg-slate-700"
          >
            {isDisconnecting ? (
              <Loader2 className="h-3 w-3 animate-spin" aria-hidden="true" />
            ) : (
              <PlugZap className="h-3 w-3" aria-hidden="true" />
            )}
            Disconnect
          </button>
        ) : (
          <button
            type="button"
            onClick={onConnect}
            disabled={isConnecting}
            className="flex items-center gap-1.5 rounded-md border border-gray-300 px-3 py-1.5 text-xs font-medium text-gray-800 transition-colors hover:bg-gray-100 disabled:cursor-not-allowed disabled:opacity-50 dark:border-slate-600 dark:text-gray-200 dark:hover:bg-slate-700"
          >
            {isConnecting ? (
              <Loader2 className="h-3 w-3 animate-spin" aria-hidden="true" />
            ) : (
              <Plug className="h-3 w-3" aria-hidden="true" />
            )}
            {health === 'needs_reconnect' ? 'Reconnect' : 'Connect'}
          </button>
        )}
      </div>
    </div>
  )
}
