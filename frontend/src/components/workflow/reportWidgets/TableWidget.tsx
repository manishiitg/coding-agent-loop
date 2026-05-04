// Table widget — paginated, searchable, sortable record grid. Auto-detects
// numeric columns for right alignment, supports per-column formatters,
// per-row semantic coloring (colorBy + colorMap), and CSV export. Switches
// to a stacked card layout when the parent container is narrow.

import { useMemo, useState } from 'react'
import { ArrowDown, ArrowUp, ArrowUpDown, ChevronLeft, ChevronRight, Download, Search } from 'lucide-react'
import type { ReportFormatterName, ReportWidget } from '../../../services/api-types'
import { compareValues, rowsToCSV } from '../../../lib/reportFormatters'
import { useTheme } from '../../../hooks/useTheme'
import { WidgetHeader } from './shared'
import {
  DEFAULT_TABLE_PAGE_SIZE,
  type SortDirection,
  formatTableValue,
  inferPrimaryColumn,
  inferSecondaryColumn,
  renderTableValueContent,
  resolvePalette,
  resolveSemanticColor,
  toRowTint,
  useCompactWidgetLayout,
} from './tableHelpers'

export function TableWidget({ value, widget }: { value: unknown; widget: ReportWidget }) {
  const { theme } = useTheme()
  const [widgetRef, isCompact] = useCompactWidgetLayout()
  const formats = widget.formats ?? {}
  const enableSearch = widget.enableSearch !== false // default true
  const pageSize = widget.pageSize ?? DEFAULT_TABLE_PAGE_SIZE
  const hidden = useMemo(() => new Set(widget.hideColumns ?? []), [widget.hideColumns])
  const palette = useMemo(() => resolvePalette(widget, theme), [widget, theme])
  const rows = useMemo(() => {
    if (!Array.isArray(value)) return []
    return value.filter((row): row is Record<string, unknown> => Boolean(row) && typeof row === 'object')
  }, [value])
  // Distinct-value → palette-index map so unmapped colorBy values get stable colors.
  const distinctIndex = useMemo(() => {
    if (!widget.colorBy) return {}
    const out: Record<string, number> = {}
    let next = 0
    for (const row of rows) {
      const raw: unknown = row?.[widget.colorBy]
      if (raw === undefined || raw === null) continue
      const key = String(raw)
      if (!(key in out)) out[key] = next++
    }
    return out
  }, [rows, widget.colorBy])

  const [sortField, setSortField] = useState<string | null>(widget.defaultSort?.field ?? null)
  const [sortDir, setSortDir] = useState<SortDirection>(widget.defaultSort?.direction ?? 'asc')
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(0)

  const columns = useMemo(() => {
    if (rows.length === 0) return []
    const cols: string[] = []
    const seen = new Set<string>()
    for (const row of rows) {
      if (!row || typeof row !== 'object') continue
      for (const k of Object.keys(row)) {
        if (!seen.has(k) && !hidden.has(k)) {
          seen.add(k)
          cols.push(k)
        }
      }
    }
    return cols
  }, [rows, hidden])

  // Auto-detect numeric columns (used for right-alignment when no formatter declared).
  const numericColumns = useMemo(() => {
    const out = new Set<string>()
    for (const col of columns) {
      // Treat a column as numeric when at least one non-null value parses as a finite number
      // AND no value parses as a non-empty non-numeric string.
      let sawNumeric = false
      let sawNonNumeric = false
      for (const row of rows) {
        const v = row?.[col]
        if (v == null || v === '') continue
        if (typeof v === 'number' && Number.isFinite(v)) {
          sawNumeric = true
        } else if (typeof v === 'string' && v.trim() !== '' && Number.isFinite(Number(v))) {
          sawNumeric = true
        } else {
          sawNonNumeric = true
          break
        }
      }
      if (sawNumeric && !sawNonNumeric) out.add(col)
    }
    return out
  }, [rows, columns])

  const compactPrimaryColumn = useMemo(
    () => inferPrimaryColumn(columns, numericColumns),
    [columns, numericColumns],
  )

  const compactSecondaryColumn = useMemo(
    () => inferSecondaryColumn(columns, numericColumns, compactPrimaryColumn),
    [columns, compactPrimaryColumn, numericColumns],
  )

  const compactDetailColumns = useMemo(() => {
    return columns.filter(col => col !== compactPrimaryColumn && col !== compactSecondaryColumn)
  }, [columns, compactPrimaryColumn, compactSecondaryColumn])

  const filtered = useMemo(() => {
    const needle = search.trim().toLowerCase()
    if (!needle) return rows
    return rows.filter(row => {
      if (!row) return false
      for (const col of columns) {
        const v = row[col]
        if (v == null) continue
        const s = typeof v === 'object' ? JSON.stringify(v) : String(v)
        if (s.toLowerCase().includes(needle)) return true
      }
      return false
    })
  }, [rows, columns, search])

  const sorted = useMemo(() => {
    if (!sortField) return filtered
    const copy = [...filtered]
    copy.sort((a, b) => {
      const cmp = compareValues(a?.[sortField], b?.[sortField])
      return sortDir === 'asc' ? cmp : -cmp
    })
    return copy
  }, [filtered, sortField, sortDir])

  const totalPages = Math.max(1, Math.ceil(sorted.length / pageSize))
  const safePage = Math.min(page, totalPages - 1)
  const pageRows = useMemo(
    () => sorted.slice(safePage * pageSize, (safePage + 1) * pageSize),
    [sorted, safePage, pageSize],
  )

  if (rows.length === 0 || columns.length === 0) return null
  const rowCountText =
    sorted.length === rows.length
      ? `${sorted.length} row${sorted.length === 1 ? '' : 's'}`
      : `${sorted.length} of ${rows.length}`

  const onSortClick = (col: string) => {
    if (sortField === col) {
      setSortDir(prev => (prev === 'asc' ? 'desc' : 'asc'))
    } else {
      setSortField(col)
      setSortDir('asc')
    }
    setPage(0)
  }

  const handleExport = () => {
    const csv = rowsToCSV(sorted as Record<string, unknown>[], columns)
    const blob = new Blob([csv], { type: 'text/csv;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `report-table-${new Date().toISOString().replace(/[:.]/g, '-')}.csv`
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
  }

  return (
    <div ref={widgetRef} className="flex flex-col gap-2.5">
      <WidgetHeader widget={widget} />
      <div className="flex flex-col gap-1.5 text-xs sm:flex-row sm:flex-wrap sm:items-center">
        {enableSearch && (
          <div className="relative w-full sm:max-w-xs sm:flex-1">
            <Search className="absolute left-2 top-1.5 w-3.5 h-3.5 text-muted-foreground" />
            <input
              type="text"
              placeholder="Search…"
              value={search}
              onChange={e => {
                setSearch(e.target.value)
                setPage(0)
              }}
              className="w-full rounded-md border border-input bg-muted/30 py-1.5 pl-7 pr-2 text-xs focus:outline-none focus:ring-1 focus:ring-primary"
            />
          </div>
        )}
        <div className="inline-flex items-center self-start rounded-full border border-border bg-background/80 px-2 py-1 text-muted-foreground sm:self-auto">{rowCountText}</div>
        <button
          onClick={handleExport}
          className="inline-flex w-full items-center justify-center gap-1 rounded-md border border-border bg-background/80 px-2 py-1.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground sm:ml-auto sm:w-auto sm:justify-start"
          title="Export CSV"
        >
          <Download className="w-3.5 h-3.5" />
          <span>CSV</span>
        </button>
      </div>

      {isCompact ? (
        <div className="flex flex-col gap-2">
          {pageRows.map((row, i) => {
            const rowColor = resolveSemanticColor(widget, row, palette, distinctIndex[String(row?.[widget.colorBy ?? ''] ?? '')] ?? i)
            const rowStyle = rowColor ? { backgroundColor: toRowTint(rowColor) } : undefined
            const primaryValue = compactPrimaryColumn ? row?.[compactPrimaryColumn] : undefined
            const primaryText = compactPrimaryColumn
              ? formatTableValue(primaryValue, formats[compactPrimaryColumn] as ReportFormatterName | undefined)
              : null
            const secondaryValue = compactSecondaryColumn ? row?.[compactSecondaryColumn] : undefined
            const secondaryText = compactSecondaryColumn
              ? formatTableValue(secondaryValue, formats[compactSecondaryColumn] as ReportFormatterName | undefined)
              : null
            return (
              <div
                key={safePage * pageSize + i}
                className="rounded-xl border border-border/55 bg-card/90 px-3 py-3 shadow-sm"
                style={rowStyle}
              >
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="text-[10px] font-semibold uppercase tracking-[0.16em] text-muted-foreground">
                      Row {safePage * pageSize + i + 1}
                    </div>
                    <div className="break-words text-sm font-semibold text-foreground">
                      {primaryText ? renderTableValueContent(primaryText) : 'Untitled row'}
                    </div>
                    {compactSecondaryColumn && secondaryText && secondaryText.text !== primaryText?.text && (
                      <div className="break-words text-xs text-muted-foreground">
                        {compactSecondaryColumn}: {renderTableValueContent(secondaryText)}
                      </div>
                    )}
                  </div>
                </div>
                <div className="mt-2">
                  {compactDetailColumns.length === 0 && (
                    <div className="rounded-lg bg-background/45 px-2.5 py-2 text-sm text-muted-foreground">
                      Compact summary view
                    </div>
                  )}
                  {compactDetailColumns.length > 0 && (
                    <div className="divide-y divide-border/35 overflow-hidden rounded-lg bg-background/40">
                      {compactDetailColumns.map(c => {
                        const preset = formats[c] as ReportFormatterName | undefined
                        const formatted = formatTableValue(row?.[c], preset)
                        const useBlockLayout = formatted.prefersBlock || formatted.rawText.length > 72
                        return (
                          <div key={c} className={`px-2.5 py-2 ${useBlockLayout ? 'space-y-1.5' : 'flex items-start justify-between gap-3'}`}>
                            <div className="min-w-0 text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">
                              {c}
                            </div>
                            <div className={`min-w-0 break-words text-sm text-foreground ${useBlockLayout ? 'text-left whitespace-pre-wrap' : 'text-right'} ${formatted.isNumeric ? 'tabular-nums' : ''}`}>
                              {renderTableValueContent(formatted)}
                            </div>
                          </div>
                        )
                      })}
                    </div>
                  )}
                </div>
              </div>
            )
          })}
        </div>
      ) : (
        <div className="overflow-x-auto rounded-xl border border-border/60 bg-background/75 shadow-[inset_0_1px_0_rgba(255,255,255,0.03)] [scrollbar-gutter:stable]">
          <table className="w-full border-collapse text-sm">
            <thead className="sticky top-0 bg-muted/60 backdrop-blur-sm z-10 shadow-[0_1px_0_0_var(--border)]">
              <tr>
                {columns.map(c => {
                  const isSorted = sortField === c
                  const align = numericColumns.has(c) || c in formats ? 'text-right' : 'text-left'
                  return (
                    <th
                      key={c}
                      className={`px-2.5 py-2 border-b-2 border-border font-semibold text-xs uppercase tracking-wide text-muted-foreground select-none cursor-pointer hover:bg-muted hover:text-foreground transition-colors ${align} ${isSorted ? 'text-primary' : ''}`}
                      onClick={() => onSortClick(c)}
                    >
                      <span className="inline-flex items-center gap-1">
                        <span>{c}</span>
                        {isSorted ? (
                          sortDir === 'asc' ? (
                            <ArrowUp className="w-3 h-3" />
                          ) : (
                            <ArrowDown className="w-3 h-3" />
                          )
                        ) : (
                          <ArrowUpDown className="w-3 h-3 opacity-30" />
                        )}
                      </span>
                    </th>
                  )
                })}
              </tr>
            </thead>
            <tbody>
              {pageRows.map((row, i) => {
                const rowColor = resolveSemanticColor(widget, row, palette, distinctIndex[String(row?.[widget.colorBy ?? ''] ?? '')] ?? i)
                const rowStyle = rowColor ? { backgroundColor: toRowTint(rowColor) } : undefined
                return (
                <tr
                  key={safePage * pageSize + i}
                  className={`group/row transition-colors ${rowColor ? 'hover:bg-muted/40' : 'even:bg-muted/20 hover:bg-muted/40'} hover:shadow-[inset_2px_0_0_0_hsl(var(--primary))]`}
                  style={rowStyle}
                >
                  {columns.map(c => {
                    const preset = formats[c] as ReportFormatterName | undefined
                    const formatted = formatTableValue(row?.[c], preset)
                    const align = formatted.isNumeric ? 'text-right tabular-nums' : 'text-left'
                    return (
                      <td
                        key={c}
                        className={`px-2.5 py-1.5 border-b border-border/40 align-top text-foreground break-words whitespace-pre-wrap ${align}`}
                      >
                        {renderTableValueContent(formatted)}
                      </td>
                    )
                  })}
                </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}

      {totalPages > 1 && (
        <div className="flex flex-wrap items-center justify-end gap-1.5 text-xs text-muted-foreground">
          <button
            onClick={() => setPage(p => Math.max(0, p - 1))}
            disabled={safePage === 0}
            className="inline-flex items-center gap-0.5 pl-1 pr-2 py-1 rounded-md border border-border bg-background disabled:opacity-30 hover:bg-muted hover:text-foreground transition-colors"
            aria-label="Previous page"
          >
            <ChevronLeft className="w-3.5 h-3.5" />
            Prev
          </button>
          <span className="inline-flex items-center px-2 py-1 rounded-md bg-primary/10 text-primary font-medium tabular-nums">
            {safePage + 1}
            <span className="opacity-60 mx-1">/</span>
            {totalPages}
          </span>
          <button
            onClick={() => setPage(p => Math.min(totalPages - 1, p + 1))}
            disabled={safePage >= totalPages - 1}
            className="inline-flex items-center gap-0.5 pl-2 pr-1 py-1 rounded-md border border-border bg-background disabled:opacity-30 hover:bg-muted hover:text-foreground transition-colors"
            aria-label="Next page"
          >
            Next
            <ChevronRight className="w-3.5 h-3.5" />
          </button>
        </div>
      )}
    </div>
  )
}
