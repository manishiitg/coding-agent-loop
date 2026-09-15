import { ChevronDown, Loader2, MessageSquare, Phone, X } from 'lucide-react'
import { READ_ONLY_TITLE } from '../../../hooks/useCanWriteWorkflow'
import { routeId, type WorkflowRoute } from './types'
import type { WorkflowBots } from './useWorkflowBots'

// One "this workflow answers on" chip with its expandable route options.

type RouteChipBots = Pick<WorkflowBots, 'readOnly' | 'expandedChip' | 'setExpandedChip' | 'routeSaving' | 'removeRoute' | 'updateRoute'>

export function RouteChip({ bots, route }: { bots: RouteChipBots; route: WorkflowRoute }) {
  const { readOnly, expandedChip, setExpandedChip, routeSaving, removeRoute, updateRoute } = bots

  const id = routeId(route)
  const expanded = expandedChip === id
  const saving = routeSaving === id
  const label = route.kind === 'slack' ? `Slack ${route.key}` : `WhatsApp @${route.key}`
  const modeLabel = route.kind === 'slack' && route.workshop_mode === 'workshop' ? 'Build' : 'Run'
  const Icon = route.kind === 'slack' ? MessageSquare : Phone

  return (
    <div className="min-w-0">
      <span className="inline-flex max-w-full items-center gap-1.5 rounded-full border border-primary/30 bg-primary/10 py-1 pl-2 pr-1 text-xs font-medium text-primary">
        <button
          type="button"
          onClick={() => setExpandedChip(expanded ? null : id)}
          className="inline-flex min-w-0 items-center gap-1.5"
          aria-expanded={expanded}
          title="Edit route options"
        >
          <Icon className="h-3 w-3 shrink-0" />
          <span className="truncate font-mono">{label}</span>
          {route.kind === 'slack' && <span className="shrink-0 rounded-full bg-background/70 px-1.5 py-0.5 text-[10px]">{modeLabel}</span>}
          <ChevronDown className={`h-3 w-3 shrink-0 transition-transform ${expanded ? 'rotate-180' : ''}`} />
        </button>
        {saving ? (
          <Loader2 className="h-3 w-3 animate-spin" />
        ) : (
          <button
            type="button"
            onClick={() => void removeRoute(route)}
            disabled={readOnly}
            className="rounded-full p-0.5 text-primary/70 transition-colors hover:bg-red-500/15 hover:text-red-500 disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:bg-transparent disabled:hover:text-primary/70"
            aria-label={`Stop answering on ${label}`}
            title={readOnly ? READ_ONLY_TITLE : 'Remove from this workflow'}
          >
            <X className="h-3 w-3" />
          </button>
        )}
      </span>
      {expanded && (
        <div className="mt-1.5 space-y-2 rounded-md border border-border bg-muted/20 px-3 py-2 text-xs">
          <div className="flex flex-wrap items-center gap-3">
            <label className="flex items-center gap-1.5 text-muted-foreground">
                Mode
                <select
                value={route.kind === 'slack' && route.workshop_mode === 'workshop' ? 'workshop' : 'run'}
                onChange={e => void updateRoute(route, { workshop_mode: e.target.value })}
                disabled={readOnly || saving || route.kind === 'whatsapp'}
                className="px-1.5 py-1 text-xs bg-secondary border border-border rounded focus:outline-none focus:ring-1 focus:ring-primary disabled:opacity-60"
                title="Run can execute and answer questions. Build can edit this workflow from Slack."
              >
                <option value="run">Run</option>
                {route.kind === 'slack' && <option value="workshop">Build</option>}
              </select>
            </label>
          </div>
        </div>
      )}
    </div>
  )
}
