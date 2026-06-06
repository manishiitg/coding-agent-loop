import { memo, useEffect } from 'react'
import {
  BaseEdge,
  EdgeLabelRenderer,
  getSmoothStepPath,
  Position,
  type EdgeProps
} from '@xyflow/react'

interface RoutingEdgeData extends Record<string, unknown> {
  routeIndex?: number
  routeCount?: number
  routeName?: string
  selected?: boolean
  color?: string
}

function getRouteLabelPosition(
  sourceX: number,
  sourceY: number,
  targetX: number,
  targetY: number
) {
  const deltaX = targetX - sourceX
  const deltaY = targetY - sourceY
  const labelOffsetY = Math.min(96, Math.max(44, Math.abs(deltaY) * 0.16))

  return {
    x: sourceX + deltaX * 0.18,
    y: sourceY + (deltaY >= 0 ? labelOffsetY : -labelOffsetY)
  }
}

export const RoutingEdge = memo(({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition = Position.Bottom,
  targetPosition = Position.Top,
  style = {},
  markerEnd,
  label,
  data
}: EdgeProps) => {
  const edgeData = (data ?? {}) as RoutingEdgeData
  const color = edgeData.color || '#0f766e'
  const selectedOpacity = edgeData.selected === false ? 0.45 : 1
  const labelText = typeof label === 'string'
    ? label
    : edgeData.routeName

  useEffect(() => {
    console.log('[WorkflowCanvas] RoutingEdge rendered', {
      id,
      label: labelText,
      source: { x: Math.round(sourceX), y: Math.round(sourceY) },
      target: { x: Math.round(targetX), y: Math.round(targetY) },
      color,
      routeIndex: edgeData.routeIndex,
      routeCount: edgeData.routeCount,
      selected: edgeData.selected
    })
  }, [color, edgeData.routeCount, edgeData.routeIndex, edgeData.selected, id, labelText, sourceX, sourceY, targetX, targetY])

  const [edgePath] = getSmoothStepPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
    borderRadius: 18,
    offset: 32
  })
  const labelPosition = getRouteLabelPosition(sourceX, sourceY, targetX, targetY)

  return (
    <>
      <BaseEdge
        id={id}
        path={edgePath}
        markerEnd={markerEnd}
        interactionWidth={18}
        style={{
          ...style,
          stroke: color,
          strokeWidth: style.strokeWidth ?? 2.5,
          opacity: selectedOpacity
        }}
      />

      {labelText && (
        <EdgeLabelRenderer>
          <div
            className="nodrag nopan pointer-events-none absolute z-10 flex max-w-[190px] items-center gap-1.5 rounded-md border px-2 py-1 text-[10px] font-semibold shadow-sm"
            style={{
              transform: `translate(-50%, -50%) translate(${labelPosition.x}px, ${labelPosition.y}px)`,
              color,
              borderColor: color,
              background: 'hsl(var(--popover))',
              opacity: selectedOpacity
            }}
          >
            {typeof edgeData.routeIndex === 'number' && (
              <span
                className="flex h-4 w-4 shrink-0 items-center justify-center rounded-full text-[9px] text-white"
                style={{ background: color }}
              >
                {edgeData.routeIndex + 1}
              </span>
            )}
            <span className="truncate">{labelText}</span>
          </div>
        </EdgeLabelRenderer>
      )}
    </>
  )
})

RoutingEdge.displayName = 'RoutingEdge'
