import { ChevronDown, Loader2, MessageSquare, Phone, PlayCircle, Trash2, Wrench } from 'lucide-react'
import { READ_ONLY_TITLE } from '../../../hooks/useCanWriteWorkflow'
import { routeId, type WorkflowRoute } from './types'
import type { WorkflowBots } from './useWorkflowBots'

// One route card in "this workflow answers on".

type RouteChipBots = Pick<WorkflowBots, 'readOnly' | 'expandedChip' | 'setExpandedChip' | 'routeSaving' | 'removeRoute' | 'updateRoute'>

export function RouteChip({ bots, route }: { bots: RouteChipBots; route: WorkflowRoute }) {
  const { readOnly, expandedChip, setExpandedChip, routeSaving, removeRoute, updateRoute } = bots

  const id = routeId(route)
  const expanded = expandedChip === id
  const saving = routeSaving === id
  const channelLabel = route.kind === 'slack' ? route.key : `@${route.key}`
  const mode = route.kind === 'slack' && route.workshop_mode === 'workshop' ? 'workshop' : 'run'
  const Icon = route.kind === 'slack' ? MessageSquare : Phone
  const platformLabel = route.kind === 'slack' ? 'Slack' : 'WhatsApp'
  const setMode = (next: 'run' | 'workshop') => {
    if (route.kind !== 'slack' || next === mode) return
    void updateRoute(route, { workshop_mode: next })
  }

  return (
    <div className="min-w-0 rounded-md border border-border bg-background p-2 shadow-sm">
      <div className="flex min-w-0 items-start gap-2">
        <span className="mt-0.5 rounded-md border border-border bg-muted/40 p-1.5 text-muted-foreground">
          <Icon className="h-3.5 w-3.5" />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-center gap-1.5">
            <span className="text-xs font-medium text-muted-foreground">{platformLabel}</span>
            {saving && <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" />}
          </div>
          <div className="truncate font-mono text-sm font-semibold text-foreground" title={channelLabel}>{channelLabel}</div>
        </div>
        {!saving && (
          <button
            type="button"
            onClick={() => void removeRoute(route)}
            disabled={readOnly}
            className="rounded-md p-1 text-muted-foreground transition-colors hover:bg-red-500/10 hover:text-red-600 disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:bg-transparent disabled:hover:text-muted-foreground"
            aria-label={`Stop answering on ${platformLabel} ${channelLabel}`}
            title={readOnly ? READ_ONLY_TITLE : 'Remove from this workflow'}
          >
            <Trash2 className="h-3.5 w-3.5" />
          </button>
        )}
      </div>
      <div className="mt-2 grid grid-cols-2 rounded-md border border-border bg-muted/30 p-0.5">
        <button
          type="button"
          onClick={() => setMode('run')}
          disabled={readOnly || saving || route.kind === 'whatsapp'}
          className={`inline-flex h-7 min-w-0 items-center justify-center gap-1 rounded px-2 text-xs font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-70 ${mode === 'run' ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'}`}
          title={route.kind === 'whatsapp' ? 'WhatsApp routes use Run mode.' : 'Run mode'}
        >
          <PlayCircle className="h-3.5 w-3.5 shrink-0" />
          <span className="truncate">Run</span>
        </button>
        <button
          type="button"
          onClick={() => setMode('workshop')}
          disabled={readOnly || saving || route.kind === 'whatsapp'}
          className={`inline-flex h-7 min-w-0 items-center justify-center gap-1 rounded px-2 text-xs font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-70 ${mode === 'workshop' ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'}`}
          title={route.kind === 'whatsapp' ? 'Build mode is available for Slack routes.' : 'Build mode'}
        >
          <Wrench className="h-3.5 w-3.5 shrink-0" />
          <span className="truncate">Build</span>
        </button>
      </div>
      <button
        type="button"
        onClick={() => setExpandedChip(expanded ? null : id)}
        className="mt-2 inline-flex items-center gap-1 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground"
        aria-expanded={expanded}
      >
        Options
        <ChevronDown className={`h-3 w-3 transition-transform ${expanded ? 'rotate-180' : ''}`} />
      </button>
      {expanded && (
        <div className="mt-1.5 flex flex-wrap items-center gap-3 rounded-md border border-border bg-muted/20 px-3 py-2 text-xs">
          <span className="text-muted-foreground">Access follows the routed user's workflow permission.</span>
          <label className="flex items-center gap-1.5 text-muted-foreground" title="Send detailed automation step/runtime messages to this channel">
            <input
              type="checkbox"
              checked={!!route.send_full_details}
              disabled={readOnly || saving}
              onChange={e => void updateRoute(route, { send_full_details: e.target.checked })}
              className="h-3.5 w-3.5"
            />
            Send full details
          </label>
        </div>
      )}
    </div>
  )
}
