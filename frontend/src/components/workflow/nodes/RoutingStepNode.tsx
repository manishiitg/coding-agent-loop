import { memo, type ReactElement } from 'react'
import { Handle, Position } from '@xyflow/react'
import { CheckCircle, XCircle, Loader2, Plus, RefreshCw, Route } from 'lucide-react'
import type { RoutingStepNodeData } from '../hooks/usePlanToFlow'
import type { ChangeType } from '../hooks/usePlanData'

interface RoutingStepNodeProps {
  data: RoutingStepNodeData
  selected?: boolean
}

const statusBorderColors: Record<string, string> = {
  pending: 'border-teal-400 dark:border-teal-500',
  executing: 'border-teal-500 dark:border-teal-400',
  evaluating: 'border-purple-500 dark:border-purple-400',
  routed: 'border-green-500 dark:border-green-400',
  completed: 'border-green-500 dark:border-green-400'
}

const changeHighlightStyles: Record<ChangeType, string> = {
  added: 'ring-2 ring-emerald-500/60 shadow-emerald-500/20',
  updated: 'ring-2 ring-blue-500/60 shadow-blue-500/20',
  deleted: 'ring-2 ring-red-500/60 shadow-red-500/20 opacity-50'
}

const changeBadgeStyles: Record<ChangeType, { bg: string; icon: ReactElement }> = {
  added: { bg: 'bg-emerald-500', icon: <Plus className="w-3 h-3" /> },
  updated: { bg: 'bg-blue-500', icon: <RefreshCw className="w-3 h-3" /> },
  deleted: { bg: 'bg-red-500', icon: <XCircle className="w-3 h-3" /> }
}

const statusIcons: Record<string, ReactElement | null> = {
  pending: null,
  executing: <Loader2 className="w-4 h-4 text-teal-500 animate-spin" />,
  evaluating: <Loader2 className="w-4 h-4 text-purple-500 animate-spin" />,
  routed: <CheckCircle className="w-4 h-4 text-green-500" />,
  completed: <CheckCircle className="w-4 h-4 text-green-500" />
}

export const RoutingStepNode = memo(({ data, selected }: RoutingStepNodeProps) => {
  const { title, routing_question, routes, status, stepIndex, changeType, isOrphan } = data
  const selectedRouteId = data.step && 'selected_route_id' in data.step
    ? data.step.selected_route_id
    : undefined

  const borderColor = statusBorderColors[status] || statusBorderColors.pending
  const statusIcon = statusIcons[status] || null

  return (
    <div className={`relative ${changeType ? changeHighlightStyles[changeType] : ''}`}>
      {/* Change badge */}
      {changeType && (
        <div className={`absolute -top-2 -right-2 z-10 ${changeBadgeStyles[changeType].bg} text-white rounded-full w-5 h-5 flex items-center justify-center shadow-md`}>
          {changeBadgeStyles[changeType].icon}
        </div>
      )}

      {/* Input handle */}
      <Handle
        type="target"
        position={Position.Top}
        className="!w-3 !h-3 !bg-teal-400 dark:!bg-teal-500 !border-2 !border-white dark:!border-gray-900"
      />

      {/* Main node */}
      <div
        className={`
          w-[300px] rounded-xl border-2 ${borderColor}
          bg-white dark:bg-gray-900
          shadow-lg overflow-visible transition-all duration-200
          ${isOrphan ? 'border-dashed border-amber-400 dark:border-amber-500' : ''}
          ${selected ? 'ring-2 ring-teal-500/60' : ''}
          ${status === 'executing' || status === 'evaluating' ? 'shadow-lg shadow-teal-500/30' : ''}
        `}
      >
        {/* Header */}
        <div className="px-3 py-2 bg-gray-50 dark:bg-gray-800/50 border-b border-gray-200 dark:border-gray-700 flex items-center gap-2">
          <div className="flex items-center gap-1.5 min-w-0 flex-1">
            <div className="flex items-center justify-center w-6 h-6 rounded-md bg-teal-100 dark:bg-teal-900/40 flex-shrink-0">
              <Route className="w-3.5 h-3.5 text-teal-600 dark:text-teal-400" />
            </div>
            <div className="text-xs font-semibold text-gray-900 dark:text-white truncate">
              {title || `Routing ${stepIndex + 1}`}
            </div>
            {statusIcon}
          </div>

          <div className="flex-shrink-0 text-[10px] font-medium text-gray-500 dark:text-gray-400">
            Step {stepIndex + 1}
          </div>
        </div>

        {/* Routing question */}
        {routing_question && (
          <div className="px-3 pt-2 pb-1.5">
            <div className="p-1.5 rounded-lg bg-teal-50 dark:bg-teal-900/20 border border-teal-200 dark:border-teal-800/50">
              <div className="text-[10px] text-teal-700 dark:text-teal-300 line-clamp-2 italic">
                {routing_question}
              </div>
            </div>
          </div>
        )}

        {/* Routes */}
        {routes && routes.length > 0 && (
          <div className="px-3 py-2 space-y-1.5">
            <div className="text-[10px] font-semibold uppercase tracking-wide text-teal-700 dark:text-teal-300">
              {routes.length} route{routes.length === 1 ? '' : 's'}
            </div>
            {routes.map((route, index) => {
              const isSelectedRoute = selectedRouteId === route.route_id
              return (
                <div
                  key={route.route_id}
                  className={`flex items-start gap-2 rounded-md border px-2 py-1.5 ${
                    isSelectedRoute
                      ? 'border-teal-300 bg-teal-50 dark:border-teal-500/70 dark:bg-teal-500/15'
                      : 'border-slate-200 bg-slate-50 dark:border-slate-700/80 dark:bg-slate-800/70'
                  }`}
                  title={route.condition || route.route_name || route.route_id}
                >
                  <span className={`mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-full text-[9px] font-bold ${
                    isSelectedRoute
                      ? 'bg-teal-700 text-white dark:bg-teal-400 dark:text-teal-950'
                      : 'bg-teal-600 text-white dark:bg-teal-500/80 dark:text-teal-50'
                  }`}>
                    {isSelectedRoute ? <CheckCircle className="h-3 w-3" /> : index + 1}
                  </span>
                  <div className="min-w-0">
                    <div className={`truncate text-[11px] font-semibold ${
                      isSelectedRoute
                        ? 'text-teal-900 dark:text-teal-100'
                        : 'text-slate-800 dark:text-slate-200'
                    }`}>
                      {route.route_name || route.route_id}
                    </div>
                    {route.condition && (
                      <div className="line-clamp-1 text-[10px] text-slate-500 dark:text-slate-400">
                        {route.condition}
                      </div>
                    )}
                  </div>
                </div>
              )
            })}
          </div>
        )}

      </div>

      {/* Output handles - one per route */}
      {routes && routes.map((route, idx) => {
        const handleOffset = routes.length > 1
          ? (idx / (routes.length - 1)) * 80 + 10 // Spread from 10% to 90%
          : 50
        return (
          <Handle
            key={`route-${route.route_id}`}
            type="source"
            position={Position.Bottom}
            id={`route-${route.route_id}`}
            className="!w-3 !h-3 !bg-teal-400 dark:!bg-teal-500 !border-2 !border-white dark:!border-gray-900"
            style={{ left: `${handleOffset}%` }}
          />
        )
      })}

      {/* Fallback single output handle if no routes */}
      {(!routes || routes.length === 0) && (
        <Handle
          type="source"
          position={Position.Bottom}
          className="!w-3 !h-3 !bg-teal-400 dark:!bg-teal-500 !border-2 !border-white dark:!border-gray-900"
        />
      )}
    </div>
  )
})

RoutingStepNode.displayName = 'RoutingStepNode'
