// Helpers used by ChartWidget (and PivotWidget for heatmap tinting):
//   - hex color interpolation for heatmaps
//   - chart-data normalization (label/value extraction, multi-series)
//   - themed Recharts tooltip
//
// Recharts tooltip lives here rather than in ChartWidget.tsx so it can be
// reused if other widgets render charts in the future.

import type { ReportWidget } from '../../../services/api-types'

// Linear-interpolates between two hex colors (#rrggbb). Used by the pivot
// heatmap tint calculation. Gracefully falls back to the low color for bad
// inputs so a malformed `heatmap_colors` never crashes rendering.
export function lerpHex(lo: string, hi: string, t: number): string {
  const parse = (c: string): [number, number, number] | null => {
    if (!c.startsWith('#')) return null
    const hex = c.length === 4
      ? `#${c[1]}${c[1]}${c[2]}${c[2]}${c[3]}${c[3]}`
      : c.length === 7 ? c : null
    if (!hex) return null
    return [
      parseInt(hex.slice(1, 3), 16),
      parseInt(hex.slice(3, 5), 16),
      parseInt(hex.slice(5, 7), 16),
    ]
  }
  const a = parse(lo)
  const b = parse(hi)
  if (!a || !b) return lo
  const mix = (x: number, y: number) => Math.round(x + (y - x) * t)
  const r = mix(a[0], b[0]).toString(16).padStart(2, '0')
  const g = mix(a[1], b[1]).toString(16).padStart(2, '0')
  const bl = mix(a[2], b[2]).toString(16).padStart(2, '0')
  return `#${r}${g}${bl}`
}

// Normalises chart data to {label, value, ...rest} pairs. When `xAxis`/`yAxis` are
// declared in the widget, those field names take precedence; otherwise we accept
// either the canonical {label,value} shape or fall back to the first two object keys.
//
// Multi-series mode: when `widget.series` is non-empty, every field in `series`
// becomes a numeric key on each point. `_value` is kept as the FIRST series'
// value so single-series code paths (sort, topN) continue to work intuitively.
export function normaliseChartPoints(value: unknown, widget: ReportWidget): Array<Record<string, unknown> & { _label: string; _value: number }> {
  if (!Array.isArray(value) || value.length === 0) return []
  const xField = widget.xAxis
  const yField = widget.yAxis
  const series = widget.series && widget.series.length > 0 ? widget.series : null
  const out: Array<Record<string, unknown> & { _label: string; _value: number }> = []
  for (const item of value as Record<string, unknown>[]) {
    if (!item || typeof item !== 'object') continue
    let label: string | undefined
    let num: number | undefined
    const extra: Record<string, number> = {}
    if (series) {
      // Multi-series: require an explicit xField (or fall back to first key).
      const xKey = xField ?? Object.keys(item)[0]
      label = item[xKey] != null ? String(item[xKey]) : undefined
      for (const s of series) {
        const v = Number(item[s])
        if (Number.isFinite(v)) extra[s] = v
      }
      // `_value` mirrors the first series value so sort/topN still mean something.
      num = series.length > 0 ? extra[series[0]] : undefined
    } else if (xField && yField) {
      label = item[xField] != null ? String(item[xField]) : undefined
      num = Number(item[yField])
    } else if ('label' in item && 'value' in item) {
      label = String(item.label ?? '')
      num = Number(item.value)
    } else {
      const keys = Object.keys(item)
      if (keys.length < 2) continue
      label = String(item[keys[0]] ?? '')
      num = Number(item[keys[1]])
    }
    if (label === undefined) continue
    if (!series && !Number.isFinite(num)) continue
    out.push({ ...item, ...extra, _label: label, _value: Number.isFinite(num) ? (num as number) : 0 })
  }
  return out
}

// Themed tooltip — replaces recharts' default gray box. Matches app theme,
// shows a small color swatch per series, tabular numbers.
type TooltipEntry = { name?: string; value?: unknown; color?: string; fill?: string; payload?: Record<string, unknown> }
export function ChartTooltipContent({ active, payload, label }: { active?: boolean; payload?: TooltipEntry[]; label?: unknown }) {
  if (!active || !payload || payload.length === 0) return null
  const fmt = (v: unknown) => {
    if (typeof v === 'number') return Number.isInteger(v) ? v.toLocaleString() : v.toLocaleString(undefined, { maximumFractionDigits: 2 })
    return String(v ?? '')
  }
  return (
    <div className="rounded-md border border-border bg-popover/95 backdrop-blur-sm px-2.5 py-1.5 text-xs shadow-lg">
      {label != null && String(label) !== '' && (
        <div className="font-semibold text-foreground mb-1">{String(label)}</div>
      )}
      <div className="flex flex-col gap-0.5">
        {payload.map((p, i) => (
          <div key={i} className="flex items-center gap-1.5">
            <span className="w-2 h-2 rounded-sm flex-shrink-0" style={{ backgroundColor: p.color || p.fill }} />
            {p.name && <span className="text-muted-foreground">{p.name}:</span>}
            <span className="font-medium tabular-nums text-foreground">{fmt(p.value)}</span>
          </div>
        ))}
      </div>
    </div>
  )
}
