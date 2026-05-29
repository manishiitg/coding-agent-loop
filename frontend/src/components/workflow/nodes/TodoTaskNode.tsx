import { memo, useMemo, type ReactElement } from 'react'
import { Handle, Position } from '@xyflow/react'
import { CheckCircle, XCircle, Loader2, Plus, RefreshCw, ListTodo, Bot, Route, ListOrdered } from 'lucide-react'
import type { TodoTaskNodeData } from '../hooks/usePlanToFlow'
import type { ChangeType } from '../hooks/usePlanData'

interface TodoTaskNodeProps {
  data: TodoTaskNodeData
  selected?: boolean
}

const statusBorderColors: Record<string, string> = {
  pending: 'border-gray-300 dark:border-gray-600',
  running: 'border-purple-500 dark:border-purple-600',
  executing: 'border-purple-500 dark:border-purple-600',
  evaluating: 'border-purple-500 dark:border-purple-600',
  orchestrating: 'border-purple-500 dark:border-purple-600',
  completed: 'border-green-500 dark:border-green-400',
  failed: 'border-red-500 dark:border-red-400'
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
  running: <Loader2 className="w-4 h-4 text-purple-500 dark:text-purple-400 animate-spin" />,
  executing: <Loader2 className="w-4 h-4 text-purple-500 dark:text-purple-400 animate-spin" />,
  evaluating: <Loader2 className="w-4 h-4 text-purple-500 dark:text-purple-400 animate-spin" />,
  orchestrating: <Loader2 className="w-4 h-4 text-purple-500 dark:text-purple-400 animate-spin" />,
  completed: <CheckCircle className="w-4 h-4 text-green-500" />,
  failed: <XCircle className="w-4 h-4 text-red-500" />
}

export const TodoTaskNode = memo(({ data, selected }: TodoTaskNodeProps) => {
  const {
    id,
    title,
    predefined_routes,
    enable_generic_agent,
    status,
    stepIndex,
    changeType,
    step,
    parentOrchestratorTitle,
    routeName,
    routeCondition,
    isOrphan
  } = data

  const isNestedTodoSubAgent = useMemo(() => id.includes('-sub-agent-') && !!parentOrchestratorTitle, [id, parentOrchestratorTitle])
  const cardWidth = isNestedTodoSubAgent ? 264 : 300

  // Scripted message sequence (foreach / scripted turns / prevalidation gates)
  // fed into the orchestrator's own conversation after its first turn.
  const scriptedCount = step && 'messages' in step && Array.isArray(step.messages) ? step.messages.length : 0

  // Calculate node height based on content
  const nodeHeight = useMemo(() => {
    let height = isNestedTodoSubAgent ? 72 : 80
    if (isNestedTodoSubAgent && (routeName || parentOrchestratorTitle)) height += 24
    if (step?.description) height += isNestedTodoSubAgent ? 24 : 30
    if (predefined_routes && predefined_routes.length > 0) height += isNestedTodoSubAgent ? 28 : 36 + Math.min(predefined_routes.length, 4) * 32
    if (scriptedCount > 0) height += isNestedTodoSubAgent ? 16 : 20
    if (enable_generic_agent) height += isNestedTodoSubAgent ? 16 : 20
    return Math.max(height, isNestedTodoSubAgent ? 108 : 120)
  }, [isNestedTodoSubAgent, routeName, parentOrchestratorTitle, step?.description, predefined_routes, scriptedCount, enable_generic_agent])

  return (
    <div
      className={`relative ${changeType ? changeHighlightStyles[changeType] : ''} ${isOrphan ? 'border-dashed border-2 border-amber-400 dark:border-amber-500 rounded-xl' : ''}`}
      style={{ width: `${cardWidth}px` }}
    >
      {/* Header metadata - above the card */}
      <div className={`absolute ${isNestedTodoSubAgent ? '-top-10' : '-top-12'} left-0 right-0 flex items-center justify-center gap-2 z-20`}>
        {isNestedTodoSubAgent && (
          <div
            className="flex items-center gap-1 px-2 py-1 rounded-md bg-card text-foreground border border-border text-[10px] font-semibold shadow-sm"
            title="Nested todo sub-agents run through the parent todo task"
          >
            <Bot className="w-3 h-3" />
            <span>Child</span>
          </div>
        )}
      </div>

      {/* Todo Task Badge - Top */}
      <div className={`absolute ${isNestedTodoSubAgent ? '-top-2' : '-top-2.5'} left-1/2 -translate-x-1/2 z-10 flex items-center gap-1.5 ${isNestedTodoSubAgent ? 'px-2 py-0.5 text-[10px]' : 'px-2.5 py-1 text-[11px]'} rounded-full ${isNestedTodoSubAgent ? 'bg-violet-600 dark:bg-violet-700' : 'bg-purple-600 dark:bg-purple-700'} text-white font-semibold shadow-lg`}>
        <ListTodo className="w-3.5 h-3.5" />
        <span>{isNestedTodoSubAgent ? 'Nested Orchestrator' : 'Orchestrator'}</span>
      </div>

      {/* Change badge */}
      {changeType && (
        <div className={`absolute top-0 right-0 z-10 flex items-center gap-1 px-1.5 py-0.5 rounded-bl-lg rounded-tr-xl ${changeBadgeStyles[changeType].bg} text-white text-[10px] font-medium shadow-lg`}>
          {changeBadgeStyles[changeType].icon}
          <span className="capitalize">{changeType}</span>
        </div>
      )}

      {/* Rectangle Shape Card */}
      <div
        className={`
          relative rounded-xl border-2 ${isNestedTodoSubAgent ? 'bg-card shadow-md' : 'bg-white dark:bg-gray-900 shadow-lg'} overflow-visible
          ${statusBorderColors[status]}
          ${selected ? 'ring-2 ring-purple-500/60' : ''}
          ${status === 'running' || status === 'executing' || status === 'evaluating' || status === 'orchestrating' ? 'animate-pulse' : ''}
        `}
        style={{
          minHeight: `${nodeHeight}px`,
          width: `${cardWidth}px`
        }}
      >
        {/* Input handle */}
        <Handle
          type="target"
          position={Position.Top}
          id={isNestedTodoSubAgent ? 'top' : undefined}
          className={`!w-3 !h-3 !border-2 !border-white dark:!border-gray-900 ${isNestedTodoSubAgent ? '!bg-violet-500 dark:!bg-violet-600' : '!bg-purple-500 dark:!bg-purple-600'}`}
          style={{ top: '-6px', left: '50%' }}
        />

        {/* Content */}
        <div className={`flex flex-col ${isNestedTodoSubAgent ? 'px-3.5 py-3.5' : 'px-4 py-4'}`}>
          {(isNestedTodoSubAgent && (routeName || parentOrchestratorTitle)) && (
            <div className="mb-2 flex flex-wrap items-center justify-center gap-1.5">
              {routeName && (
                <span
                  className="px-2 py-0.5 rounded-full text-[9px] font-semibold border border-violet-300/70 bg-violet-50 text-violet-800 dark:border-violet-400/25 dark:bg-violet-400/10 dark:text-violet-200/85"
                  title={routeCondition || routeName}
                >
                  {routeName}
                </span>
              )}
              {parentOrchestratorTitle && (
                <span
                  className="px-2 py-0.5 rounded-full text-[9px] font-medium bg-muted text-muted-foreground border border-border max-w-[180px] truncate"
                  title={`Nested under ${parentOrchestratorTitle}`}
                >
                  {parentOrchestratorTitle}
                </span>
              )}
            </div>
          )}

          <div className={`flex items-center gap-1.5 ${isNestedTodoSubAgent ? 'mb-1.5' : 'mb-2'} justify-center`}>
            {statusIcons[status]}
          </div>
          <h3 className={`${isNestedTodoSubAgent ? 'text-[13px]' : 'text-sm'} font-semibold text-gray-900 dark:text-white leading-tight text-center mb-1.5`}>
            {title || `Orchestrator ${stepIndex + 1}`}
          </h3>

          {/* Main todo task step description */}
          {step?.description && (
            <div className={`mt-1.5 ${isNestedTodoSubAgent ? 'p-1.5 bg-muted border-border' : 'p-2 bg-gray-50 dark:bg-gray-800/50 border-gray-200 dark:border-gray-700/60'} rounded-lg border`}>
              <p className={`text-[10px] font-semibold ${isNestedTodoSubAgent ? 'text-foreground' : 'text-gray-700 dark:text-gray-300'}`}>
                Task: {step.title || 'Untitled Step'}
              </p>
            </div>
          )}

          {/* Predefined Routes */}
          {predefined_routes && predefined_routes.length > 0 && (
            <div className="mt-2 space-y-1.5">
              <div className={`flex items-center gap-1.5 text-[10px] font-semibold ${isNestedTodoSubAgent ? 'text-violet-600 dark:text-violet-400' : 'text-purple-600 dark:text-purple-400'}`}>
                <Route className="w-3 h-3" />
                <span>{predefined_routes.length} route{predefined_routes.length > 1 ? 's' : ''}</span>
              </div>
              {!isNestedTodoSubAgent && predefined_routes.slice(0, 4).map((route, index) => (
                <div
                  key={route.route_id}
                  className="flex items-start gap-2 rounded-md border border-slate-200 bg-slate-50 px-2 py-1.5 dark:border-slate-700/80 dark:bg-slate-800/70"
                  title={route.condition || route.route_name || route.route_id}
                >
                  <span className="mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-full bg-purple-600 text-[9px] font-bold text-white dark:bg-purple-500/80 dark:text-purple-50">
                    {index + 1}
                  </span>
                  <div className="min-w-0">
                    <div className="truncate text-[11px] font-semibold text-slate-800 dark:text-slate-200">
                      {route.route_name || route.route_id}
                    </div>
                    {route.condition && (
                      <div className="line-clamp-1 text-[10px] text-slate-500 dark:text-slate-400">
                        {route.condition}
                      </div>
                    )}
                  </div>
                </div>
              ))}
              {!isNestedTodoSubAgent && predefined_routes.length > 4 && (
                <div className="text-[10px] text-gray-500 dark:text-gray-400">
                  +{predefined_routes.length - 4} more route{predefined_routes.length - 4 === 1 ? '' : 's'}
                </div>
              )}
            </div>
          )}

          {/* Scripted message sequence indicator */}
          {scriptedCount > 0 && (
            <div className={`mt-2 flex items-center gap-1.5 text-[10px] font-semibold ${isNestedTodoSubAgent ? 'text-violet-600 dark:text-violet-400' : 'text-purple-600 dark:text-purple-400'}`}>
              <ListOrdered className="w-3 h-3" />
              <span>{scriptedCount} scripted step{scriptedCount === 1 ? '' : 's'}</span>
            </div>
          )}

          {/* Generic Agent Indicator */}
          {enable_generic_agent && (
            <div className="mt-1.5 flex items-center gap-1.5 text-[10px] text-gray-500 dark:text-gray-400">
              <Bot className="w-3 h-3" />
              <span>Fallback agent</span>
            </div>
          )}

        </div>

        {/* Output handles - one for each predefined route + "end" route option */}
        {predefined_routes && predefined_routes.length > 0 ? (
          <>
            {predefined_routes.map((route, index) => {
              const totalRoutes = predefined_routes.length + 1 // +1 for "end" route
              const positionPercent = 20 + (index * (60 / (totalRoutes - 1)))
              return (
                <Handle
                  key={route.route_id}
                  type="source"
                  position={Position.Bottom}
                  id={route.route_id}
                  className="!w-3 !h-3 !bg-purple-500 dark:!bg-purple-600 !border-2 !border-white dark:!border-gray-900 !shadow-md"
                  style={{ left: `${positionPercent}%`, bottom: '-6px' }}
                />
              )
            })}
            {/* "end" route handle */}
            <Handle
              key="end"
              type="source"
              position={Position.Bottom}
              id="end"
              className="!w-3 !h-3 !bg-red-500 dark:!bg-red-600 !border-2 !border-white dark:!border-gray-900 !shadow-md"
              style={{ left: `${20 + (predefined_routes.length * (60 / (predefined_routes.length)))}%`, bottom: '-6px' }}
              title="End workflow route"
            />
          </>
        ) : (
          <>
            <Handle
              type="source"
              position={Position.Bottom}
              className="!w-3 !h-3 !bg-purple-500 dark:!bg-purple-600 !border-2 !border-white dark:!border-gray-900 !shadow-md"
              style={{ left: '40%', bottom: '-6px' }}
            />
            {/* "end" route handle */}
            <Handle
              type="source"
              position={Position.Bottom}
              id="end"
              className="!w-3 !h-3 !bg-red-500 dark:!bg-red-600 !border-2 !border-white dark:!border-gray-900 !shadow-md"
              style={{ left: '60%', bottom: '-6px' }}
              title="End workflow route"
            />
          </>
        )}
      </div>

    </div>
  )
})

TodoTaskNode.displayName = 'TodoTaskNode'
export default TodoTaskNode
