// Pivot widget — rows × columns × aggregate(values). Reads an array of records
// (source + path + optional filter), buckets by rowsField/columnsField, and
// applies the aggregator to valuesField for each cell.

import type { ReportWidget } from '../../../services/api-types'
import { formatAuto, formatNamed } from '../../../lib/reportFormatters'
import { applyWidgetFilter, resolveJSONPath } from '../../../lib/reportPlanParser'
import { useTheme } from '../../../hooks/useTheme'
import { WidgetError, WidgetHeader } from './shared'
import { lerpHex } from './chartHelpers'
import { useCompactWidgetLayout } from './tableHelpers'

export function PivotWidget({ source, widget }: { source: unknown; widget: ReportWidget }) {
  const { theme } = useTheme()
  const [widgetRef, isCompact] = useCompactWidgetLayout()
  const resolvedRaw = resolveJSONPath(source, widget.path)
  const resolved = applyWidgetFilter(resolvedRaw, widget.filter)

  if (!widget.rowsField || !widget.columnsField || !widget.valuesField) {
    return (
      <WidgetError
        widget={widget}
        message="pivot requires rows, columns, and values fields."
        hint="Set `rows: <field>`, `columns: <field>`, `values: <field>`; optionally `aggregate: sum|avg|count|min|max|first`."
      />
    )
  }
  if (!Array.isArray(resolved) || resolved.length === 0) {
    return (
      <WidgetError
        widget={widget}
        message={`pivot source resolves to ${resolvedRaw == null ? 'nothing' : 'an empty or non-array value'}.`}
        hint="Point `path:` at a non-empty array of objects."
        severity="info"
      />
    )
  }

  const rowsField = widget.rowsField
  const columnsField = widget.columnsField
  const valuesField = widget.valuesField
  const aggregate = widget.aggregate ?? 'sum'

  // Build the cell grid — Map<rowKey, Map<colKey, aggregated>> with column-order
  // and row-order tracked in insertion-order arrays so rendering is stable.
  const rowKeys: string[] = []
  const colKeys: string[] = []
  const seenRow = new Set<string>()
  const seenCol = new Set<string>()
  const buckets = new Map<string, Map<string, number[]>>() // rowKey -> colKey -> raw numeric values
  const stringBuckets = new Map<string, Map<string, unknown>>() // for 'first' aggregate
  for (const row of resolved as Record<string, unknown>[]) {
    if (!row || typeof row !== 'object') continue
    const rk = resolveJSONPath(row, rowsField)
    const ck = resolveJSONPath(row, columnsField)
    if (rk == null || ck == null) continue
    const rKey = String(rk)
    const cKey = String(ck)
    if (!seenRow.has(rKey)) { seenRow.add(rKey); rowKeys.push(rKey) }
    if (!seenCol.has(cKey)) { seenCol.add(cKey); colKeys.push(cKey) }
    if (!buckets.has(rKey)) buckets.set(rKey, new Map())
    const inner = buckets.get(rKey)!
    if (!inner.has(cKey)) inner.set(cKey, [])
    const v = resolveJSONPath(row, valuesField)
    if (aggregate === 'first') {
      if (!stringBuckets.has(rKey)) stringBuckets.set(rKey, new Map())
      const sInner = stringBuckets.get(rKey)!
      if (!sInner.has(cKey)) sInner.set(cKey, v)
    } else if (aggregate === 'count') {
      inner.get(cKey)!.push(1)
    } else {
      const num = typeof v === 'number' ? v : Number(v)
      if (Number.isFinite(num)) inner.get(cKey)!.push(num)
    }
  }

  // Reduce each cell's array via the aggregator.
  const cell = (rKey: string, cKey: string): { raw: unknown; num: number | undefined } => {
    if (aggregate === 'first') {
      const raw = stringBuckets.get(rKey)?.get(cKey)
      const num = typeof raw === 'number' ? raw : Number(raw)
      return { raw, num: Number.isFinite(num) ? num : undefined }
    }
    const vals = buckets.get(rKey)?.get(cKey) ?? []
    if (vals.length === 0) return { raw: undefined, num: undefined }
    let num: number
    switch (aggregate) {
      case 'avg': num = vals.reduce((a, b) => a + b, 0) / vals.length; break
      case 'min': num = Math.min(...vals); break
      case 'max': num = Math.max(...vals); break
      case 'count': num = vals.length; break
      case 'sum':
      default: num = vals.reduce((a, b) => a + b, 0); break
    }
    return { raw: num, num }
  }

  const allNums: number[] = []
  for (const rk of rowKeys) for (const ck of colKeys) {
    const { num } = cell(rk, ck)
    if (num !== undefined) allNums.push(num)
  }
  const min = allNums.length > 0 ? Math.min(...allNums) : 0
  const max = allNums.length > 0 ? Math.max(...allNums) : 0
  const span = max - min || 1

  const heatmap = widget.heatmap === true
  const [heatLow, heatHigh] = widget.heatmapColors ?? (
    theme === 'dark' ? ['#1e293b', '#60a5fa'] : ['#eff6ff', '#2563eb']
  )

  const tintFor = (n: number | undefined): string | undefined => {
    if (!heatmap || n === undefined) return undefined
    const t = (n - min) / span
    return lerpHex(heatLow, heatHigh, Math.max(0, Math.min(1, t)))
  }

  return (
    <div ref={widgetRef} className="flex flex-col gap-3">
      <WidgetHeader widget={widget} />
      {isCompact ? (
        <div className="flex flex-col gap-2">
          {rowKeys.map(rk => (
            <div key={rk} className="rounded-xl border border-border/55 bg-card/90 px-3 py-3 shadow-sm">
              <div className="mb-2 text-[11px] font-semibold uppercase tracking-[0.16em] text-muted-foreground">
                {rowsField}: <span className="text-foreground">{rk}</span>
              </div>
              <div className="divide-y divide-border/35 overflow-hidden rounded-lg bg-background/40">
                {colKeys.map(ck => {
                  const { raw, num } = cell(rk, ck)
                  const text = raw == null
                    ? ''
                    : widget.format
                      ? formatNamed(raw, widget.format).text
                      : formatAuto(raw).text
                  const bg = tintFor(num)
                  return (
                    <div
                      key={ck}
                      className="flex items-start justify-between gap-3 px-2.5 py-2"
                      style={bg ? { backgroundColor: bg } : undefined}
                    >
                      <div className="text-[11px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">
                        {ck}
                      </div>
                      <div className="text-right text-sm tabular-nums text-foreground">
                        {text || '—'}
                      </div>
                    </div>
                  )
                })}
              </div>
            </div>
          ))}
        </div>
      ) : (
        <div className="overflow-x-auto rounded-xl border border-border/60 bg-background/75 shadow-[inset_0_1px_0_rgba(255,255,255,0.03)] [scrollbar-gutter:stable]">
          <table className="border-collapse text-sm">
            <thead className="sticky top-0 bg-muted/60 backdrop-blur-sm z-10 shadow-[0_1px_0_0_var(--border)]">
              <tr>
                <th className="px-2.5 py-2 border-b-2 border-border font-semibold text-xs uppercase tracking-wide text-left text-muted-foreground">
                  {rowsField} ╲ {columnsField}
                </th>
                {colKeys.map(ck => (
                  <th key={ck} className="px-2.5 py-2 border-b-2 border-border font-semibold text-xs uppercase tracking-wide text-right text-muted-foreground">
                    {ck}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rowKeys.map(rk => (
                <tr key={rk} className="hover:bg-muted/30 transition-colors">
                  <th className="px-2.5 py-1.5 border-b border-border/40 font-medium text-left text-foreground bg-muted/20 sticky left-0">
                    {rk}
                  </th>
                  {colKeys.map(ck => {
                    const { raw, num } = cell(rk, ck)
                    const text = raw == null
                      ? ''
                      : widget.format
                        ? formatNamed(raw, widget.format).text
                        : formatAuto(raw).text
                    const bg = tintFor(num)
                    return (
                      <td
                        key={ck}
                        className="px-2.5 py-1.5 border-b border-border/40 text-right tabular-nums text-foreground"
                        style={bg ? { backgroundColor: bg } : undefined}
                      >
                        {text}
                      </td>
                    )
                  })}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <div className="text-xs text-muted-foreground">
        {rowKeys.length} row{rowKeys.length === 1 ? '' : 's'} × {colKeys.length} col{colKeys.length === 1 ? '' : 's'} · aggregate: {aggregate}
      </div>
    </div>
  )
}
