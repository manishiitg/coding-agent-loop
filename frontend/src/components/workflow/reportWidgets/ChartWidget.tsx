// Chart widget — bar / line / area / pie via Recharts. Single-series colors
// cycle through the palette per point (or per colorMap value); multi-series
// uses parallel seriesColors. Stacked bars/areas share a stackId.

import { useId, useMemo } from 'react'
import {
  Area, AreaChart,
  Bar, BarChart,
  CartesianGrid,
  Cell,
  Legend,
  Line, LineChart,
  Pie, PieChart,
  ResponsiveContainer,
  Tooltip,
  XAxis, YAxis,
} from 'recharts'
import type { ReportWidget } from '../../../services/api-types'
import { useTheme } from '../../../hooks/useTheme'
import { WidgetHeader } from './shared'
import { ChartTooltipContent, normaliseChartPoints } from './chartHelpers'
import { resolvePalette, useCompactWidgetLayout } from './tableHelpers'

export function ChartWidget({ value, widget }: { value: unknown; widget: ReportWidget }) {
  const { theme } = useTheme()
  const [widgetRef, isCompact] = useCompactWidgetLayout()
  const points = useMemo(() => {
    let pts = normaliseChartPoints(value, widget)
    // Sort first (so topN takes the right slice), then truncate.
    if (widget.sort === 'asc') pts = [...pts].sort((a, b) => a._value - b._value)
    else if (widget.sort === 'desc') pts = [...pts].sort((a, b) => b._value - a._value)
    if (widget.topN && widget.topN > 0) pts = pts.slice(0, widget.topN)
    return pts
  }, [value, widget])

  // For semantic coloring, assign each distinct colorBy value a stable index into
  // the palette so unmapped values still get consistent (not random) colors.
  const distinctIndex = useMemo(() => {
    if (!widget.colorBy) return {}
    const out: Record<string, number> = {}
    let next = 0
    for (const p of points) {
      const raw: unknown = (p as unknown as Record<string, unknown>)[widget.colorBy]
      if (raw === undefined || raw === null) continue
      const key = String(raw)
      if (!(key in out)) out[key] = next++
    }
    return out
  }, [points, widget.colorBy])

  // Stable, unique gradient ids per widget instance. Recharts re-renders would
  // re-create random ids and break the reference, so useId() is required here.
  const gradPrefix = useId().replace(/:/g, '')

  if (points.length === 0) return null

  const chartType = widget.chartType ?? 'bar'
  const showValues = widget.showValues === true
  const heightPx = widget.height ?? 288 // h-72 default
  const minHeightPx = Math.min(isCompact ? 180 : 220, heightPx)
  const chartFrameStyle = { width: '100%', height: `clamp(${minHeightPx}px, ${isCompact ? 34 : 42}vh, ${heightPx}px)` }

  const palette = resolvePalette(widget, theme)
  const colorForPoint = (p: (typeof points)[number], fallbackIndex: number): string => {
    if (widget.colorBy) {
      const raw = (p as unknown as Record<string, unknown>)[widget.colorBy]
      const key = raw === undefined || raw === null ? '' : String(raw)
      if (widget.colorMap && widget.colorMap[key]) return widget.colorMap[key]
      if (key in distinctIndex) return palette[distinctIndex[key] % palette.length]
    }
    return palette[fallbackIndex % palette.length]
  }

  const header = (widget.title || widget.description) ? (
    <WidgetHeader widget={widget} className="mb-2" />
  ) : null

  // Pie chart — always colors per slice.
  if (chartType === 'pie') {
    return (
      <div className="flex flex-col">
        {header}
        <div style={chartFrameStyle}>
          <ResponsiveContainer>
            <PieChart>
              <Pie
                data={points}
                dataKey="_value"
                nameKey="_label"
                outerRadius={Math.min(120, heightPx * 0.4)}
                label={showValues ? ((entry: unknown) => {
                  const e = entry as { _label?: string; _value?: number }
                  return `${e._label ?? ''}: ${e._value ?? ''}`
                }) as never : undefined}
              >
                {points.map((p, i) => (
                  <Cell key={i} fill={colorForPoint(p, i)} stroke="hsl(var(--background))" strokeWidth={2} />
                ))}
              </Pie>
              <Tooltip content={<ChartTooltipContent />} />
              <Legend wrapperStyle={{ fontSize: 11 }} iconType="circle" iconSize={8} />
            </PieChart>
          </ResponsiveContainer>
        </div>
      </div>
    )
  }

  // Bar / line / area share the same XY layout.
  const ChartContainer =
    chartType === 'line' ? LineChart : chartType === 'area' ? AreaChart : BarChart
  const ValueLabel = showValues
    ? { dataKey: '_value', position: chartType === 'bar' ? 'top' as const : 'top' as const, fontSize: 10 }
    : undefined

  // Multi-series mode: one series per field in `widget.series`. Colors come
  // from `seriesColors` (parallel) with fallback to the palette. Stacked bars
  // and areas share a stackId; lines never stack.
  const series = widget.series && widget.series.length > 0 ? widget.series : null
  const seriesColorFor = (idx: number): string => {
    if (widget.seriesColors && widget.seriesColors[idx]) return widget.seriesColors[idx]
    return palette[idx % palette.length]
  }
  const stackId = widget.stacked ? 'stack-a' : undefined

  // Single-series fallback — palette[0] for line/area; bar supports per-Cell.
  const singleSeriesColor = palette[0]

  const axisTick = { fontSize: 11, fill: 'hsl(var(--muted-foreground))' }
  const gridStroke = 'hsl(var(--border))'
  const axisLine = { stroke: gridStroke, opacity: 0.65 }
  const hoverCursorFill = 'hsl(var(--muted))'

  return (
    <div ref={widgetRef} className="flex flex-col text-muted-foreground">
      {header}
      <div className="rounded-lg bg-background/55 px-0.5 py-1.5" style={chartFrameStyle}>
        <ResponsiveContainer>
          <ChartContainer data={points} margin={{ top: 8, right: isCompact ? 8 : 16, left: isCompact ? -12 : 0, bottom: 8 }}>
            <defs>
              {/* Per-series gradient for area fills; per-point gradient for single-bar color cycling. */}
              {series
                ? series.map((field, i) => {
                    const c = seriesColorFor(i)
                    return (
                      <linearGradient key={field} id={`${gradPrefix}-s-${i}`} x1="0" y1="0" x2="0" y2="1">
                        <stop offset="0%" stopColor={c} stopOpacity={0.5} />
                        <stop offset="100%" stopColor={c} stopOpacity={0.05} />
                      </linearGradient>
                    )
                  })
                : (
                    <linearGradient id={`${gradPrefix}-single`} x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stopColor={singleSeriesColor} stopOpacity={0.5} />
                      <stop offset="100%" stopColor={singleSeriesColor} stopOpacity={0.05} />
                    </linearGradient>
                  )}
            </defs>
            <CartesianGrid strokeDasharray="3 3" stroke={gridStroke} opacity={0.55} vertical={false} />
            <XAxis dataKey="_label" tick={axisTick} tickLine={false} axisLine={axisLine} interval={0} angle={points.length > (isCompact ? 5 : 8) ? -30 : 0} textAnchor={points.length > (isCompact ? 5 : 8) ? 'end' : 'middle'} height={points.length > (isCompact ? 5 : 8) ? 60 : 30} />
            <YAxis tick={axisTick} tickLine={false} axisLine={axisLine} />
            <Tooltip
              cursor={{ fill: hoverCursorFill, opacity: 0.65 }}
              content={<ChartTooltipContent />}
            />
            {series && !isCompact && <Legend wrapperStyle={{ fontSize: 11 }} iconType="circle" iconSize={8} />}
            {series
              ? series.map((field, i) => {
                  const color = seriesColorFor(i)
                  if (chartType === 'bar') {
                    return <Bar key={field} dataKey={field} fill={color} stackId={stackId} radius={widget.stacked ? undefined : [4, 4, 0, 0]} />
                  }
                  if (chartType === 'line') {
                    return <Line key={field} type="monotone" dataKey={field} stroke={color} strokeWidth={2} dot={{ r: 3, fill: color }} activeDot={{ r: 5 }} />
                  }
                  if (chartType === 'area') {
                    return <Area key={field} type="monotone" dataKey={field} stroke={color} strokeWidth={2} fill={`url(#${gradPrefix}-s-${i})`} stackId={stackId} activeDot={{ r: 5 }} />
                  }
                  return null
                })
              : <>
                  {chartType === 'bar' && (
                    // Single-series bars cycle palette colors per point when no colorBy is set,
                    // so reports don't render as monochrome blocks. `colorBy` still wins when present.
                    <Bar dataKey="_value" radius={[4, 4, 0, 0]} label={ValueLabel}>
                      {points.map((p, i) => (
                        <Cell key={i} fill={colorForPoint(p, i)} />
                      ))}
                    </Bar>
                  )}
                  {chartType === 'line' && <Line type="monotone" dataKey="_value" stroke={singleSeriesColor} strokeWidth={2.25} dot={{ r: 3, fill: singleSeriesColor }} activeDot={{ r: 5 }} label={ValueLabel} />}
                  {chartType === 'area' && <Area type="monotone" dataKey="_value" stroke={singleSeriesColor} strokeWidth={2.25} fill={`url(#${gradPrefix}-single)`} activeDot={{ r: 5 }} label={ValueLabel} />}
                </>}
          </ChartContainer>
        </ResponsiveContainer>
      </div>
    </div>
  )
}
