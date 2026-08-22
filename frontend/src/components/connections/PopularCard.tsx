import { Check, Loader2 } from 'lucide-react'
import ConnectionIcon from './ConnectionIcon'
import type { CatalogEntry, Connection } from '../../services/connectionsApi'

interface PopularCardProps {
  entry: CatalogEntry
  connection?: Connection
  isConnecting?: boolean
  onConnect: () => void
}

/**
 * A compact featured tile for the "Popular" strip — identity plus the one
 * action worth taking. Everything else about the integration lives in the
 * table row below.
 */
export default function PopularCard({
  entry,
  connection,
  isConnecting = false,
  onConnect,
}: PopularCardProps) {
  const isConnected = connection?.health === 'connected'
  const blocked = entry.status === 'coming_soon' || entry.setup_required

  return (
    <div className="flex min-w-0 items-center gap-2.5 rounded-xl border border-gray-200 bg-white px-3 py-2.5 dark:border-slate-700 dark:bg-slate-800">
      <ConnectionIcon icon={entry.icon} brandColor={entry.brand_color} size="sm" />
      <span className="min-w-0 flex-1 truncate text-sm font-medium text-gray-900 dark:text-gray-100">
        {entry.name}
      </span>

      {isConnected ? (
        <span className="flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium text-green-600 dark:text-green-400">
          <Check className="h-3.5 w-3.5" aria-hidden="true" />
          Connected
        </span>
      ) : blocked ? (
        <span className="whitespace-nowrap px-2 py-1 text-xs text-gray-400 dark:text-gray-500">
          {entry.status === 'coming_soon' ? 'Coming soon' : 'Setup required'}
        </span>
      ) : (
        <button
          type="button"
          onClick={onConnect}
          disabled={isConnecting}
          className="flex shrink-0 items-center gap-1.5 rounded-md bg-gray-100 px-3 py-1.5 text-xs font-medium text-gray-800 transition-colors hover:bg-gray-200 disabled:cursor-not-allowed disabled:opacity-50 dark:bg-slate-700 dark:text-gray-100 dark:hover:bg-slate-600"
        >
          {isConnecting && <Loader2 className="h-3 w-3 animate-spin" aria-hidden="true" />}
          {connection?.health === 'needs_reconnect' ? 'Reconnect' : 'Connect'}
        </button>
      )}
    </div>
  )
}
