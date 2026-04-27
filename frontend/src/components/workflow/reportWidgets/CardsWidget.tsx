// Cards widget — renders rows from a table-shaped source as standalone tiles.
// Title / subtitle / description / link / image fields are inferred from the
// usual column heuristics (cardTitleField, cardSubtitleField, etc.) with
// hardcoded fallbacks. Searchable, sortable, paginated.

import { useEffect, useMemo, useState } from 'react'
import { ArrowDown, ArrowUp, ChevronLeft, ChevronRight, Search } from 'lucide-react'
import type { ReportFormatterName, ReportWidget } from '../../../services/api-types'
import { compareValues } from '../../../lib/reportFormatters'
import { useTheme } from '../../../hooks/useTheme'
import { WidgetHeader } from './shared'
import {
  DEFAULT_TABLE_PAGE_SIZE,
  type SortDirection,
  collectVisibleColumns,
  detectNumericColumns,
  formatTableValue,
  inferPrimaryColumn,
  inferSecondaryColumn,
  isURLString,
  renderTableValueContent,
  resolvePalette,
  resolveSemanticColor,
  stringifyTableValue,
  toRowTint,
  useCompactWidgetLayout,
} from './tableHelpers'

export function CardsWidget({ value, widget }: { value: unknown; widget: ReportWidget }) {
  const { theme } = useTheme()
  const [widgetRef, isCompact] = useCompactWidgetLayout()
  const formats = widget.formats ?? {}
  const enableSearch = widget.enableSearch !== false
  const pageSize = widget.pageSize ?? DEFAULT_TABLE_PAGE_SIZE
  const hidden = useMemo(() => new Set(widget.hideColumns ?? []), [widget.hideColumns])
  const palette = useMemo(() => resolvePalette(widget, theme), [widget, theme])
  const [sortField, setSortField] = useState<string | null>(widget.defaultSort?.field ?? null)
  const [sortDir, setSortDir] = useState<SortDirection>(widget.defaultSort?.direction ?? 'asc')
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(0)

  const rows = useMemo(() => {
    if (Array.isArray(value)) return value.filter((row): row is Record<string, unknown> => Boolean(row) && typeof row === 'object')
    if (value && typeof value === 'object') return [value as Record<string, unknown>]
    return []
  }, [value])

  const columns = useMemo(() => collectVisibleColumns(rows, hidden), [rows, hidden])
  const numericColumns = useMemo(() => detectNumericColumns(rows, columns), [rows, columns])
  const titleField = useMemo(
    () => inferPrimaryColumn(columns, numericColumns, widget.cardTitleField),
    [columns, numericColumns, widget.cardTitleField],
  )
  const subtitleField = useMemo(
    () => inferSecondaryColumn(columns, numericColumns, titleField, widget.cardSubtitleField),
    [columns, numericColumns, titleField, widget.cardSubtitleField],
  )
  const descriptionField = useMemo(() => {
    if (widget.cardDescriptionField && columns.includes(widget.cardDescriptionField)) return widget.cardDescriptionField
    const candidates = ['job_text', 'description', 'text', 'content', 'body', 'summary', 'notes']
    return candidates.find(candidate => columns.includes(candidate)) ?? null
  }, [columns, widget.cardDescriptionField])
  const linkField = useMemo(() => {
    if (widget.cardLinkField && columns.includes(widget.cardLinkField)) return widget.cardLinkField
    const candidates = ['job_url', 'url', 'link', 'href']
    return candidates.find(candidate => columns.includes(candidate)) ?? null
  }, [columns, widget.cardLinkField])
  const imageField = useMemo(() => {
    if (widget.cardImageField && columns.includes(widget.cardImageField)) return widget.cardImageField
    const candidates = ['image_url', 'thumbnail_url', 'avatar_url', 'logo_url']
    return candidates.find(candidate => columns.includes(candidate)) ?? null
  }, [columns, widget.cardImageField])

  const detailColumns = useMemo(() => {
    const excluded = new Set([titleField, subtitleField, descriptionField, linkField, imageField].filter(Boolean))
    const baseColumns = widget.fields && widget.fields.length > 0
      ? widget.fields.filter(field => columns.includes(field) && !excluded.has(field))
      : columns.filter(field => !excluded.has(field))
    return baseColumns
  }, [columns, descriptionField, imageField, linkField, subtitleField, titleField, widget.fields])

  const distinctIndex = useMemo(() => {
    if (!widget.colorBy) return {}
    const out: Record<string, number> = {}
    let next = 0
    for (const row of rows) {
      const rawValue: unknown = row?.[widget.colorBy]
      if (rawValue === undefined || rawValue === null) continue
      const key = String(rawValue)
      if (!(key in out)) out[key] = next++
    }
    return out
  }, [rows, widget.colorBy])

  const filtered = useMemo(() => {
    const needle = search.trim().toLowerCase()
    if (!needle) return rows
    return rows.filter(row => columns.some(col => stringifyTableValue(row[col]).toLowerCase().includes(needle)))
  }, [rows, columns, search])

  const sorted = useMemo(() => {
    if (!sortField) return filtered
    return [...filtered].sort((a, b) => {
      const result = compareValues(a?.[sortField], b?.[sortField])
      return sortDir === 'asc' ? result : -result
    })
  }, [filtered, sortDir, sortField])

  const totalRows = sorted.length
  const totalPages = Math.max(1, Math.ceil(totalRows / pageSize))
  const safePage = Math.min(page, totalPages - 1)
  const pageRows = sorted.slice(safePage * pageSize, safePage * pageSize + pageSize)
  const rowCountText = `${totalRows} record${totalRows === 1 ? '' : 's'}`

  useEffect(() => {
    if (page > totalPages - 1) setPage(Math.max(0, totalPages - 1))
  }, [page, totalPages])

  const onSortClick = (field: string) => {
    if (sortField === field) {
      setSortDir(dir => (dir === 'asc' ? 'desc' : 'asc'))
      return
    }
    setSortField(field)
    setSortDir('asc')
  }

  if (rows.length === 0) return null

  return (
    <div ref={widgetRef} className="flex flex-col gap-3">
      <WidgetHeader widget={widget} />
      <div className={`flex flex-wrap items-center gap-2 ${isCompact ? 'justify-start' : 'justify-between'}`}>
        {enableSearch && (
          <div className={`relative ${isCompact ? 'w-full' : 'min-w-[220px] flex-1 max-w-sm'}`}>
            <Search className="pointer-events-none absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
            <input
              type="search"
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
        <div className="inline-flex items-center rounded-full border border-border bg-background/80 px-2 py-1 text-xs text-muted-foreground">
          {rowCountText}
        </div>
      </div>

      <div className={`grid gap-3 ${isCompact ? 'grid-cols-1' : 'grid-cols-1 xl:grid-cols-2 2xl:grid-cols-3'}`}>
        {pageRows.map((row, index) => {
          const rowColor = resolveSemanticColor(widget, row, palette, distinctIndex[String(row?.[widget.colorBy ?? ''] ?? '')] ?? index)
          const rowStyle = rowColor ? { backgroundColor: toRowTint(rowColor) } : undefined
          const titleValue = titleField ? formatTableValue(row?.[titleField], formats[titleField] as ReportFormatterName | undefined) : null
          const subtitleValue = subtitleField ? formatTableValue(row?.[subtitleField], formats[subtitleField] as ReportFormatterName | undefined) : null
          const descriptionValue = descriptionField ? formatTableValue(row?.[descriptionField], formats[descriptionField] as ReportFormatterName | undefined) : null
          const linkValue = linkField ? formatTableValue(row?.[linkField], formats[linkField] as ReportFormatterName | undefined) : null
          const imageURL = imageField && typeof row?.[imageField] === 'string' && isURLString(row[imageField] as string)
            ? row[imageField] as string
            : null
          return (
            <div
              key={safePage * pageSize + index}
              className="overflow-hidden rounded-2xl border border-border/60 bg-card/90 shadow-sm"
              style={rowStyle}
            >
              {imageURL && (
                <img src={imageURL} alt={titleValue?.text || 'Card image'} className="h-40 w-full object-cover" />
              )}
              <div className="flex h-full flex-col gap-3 px-4 py-4">
                <div className="min-w-0">
                  <div className="break-words text-base font-semibold text-foreground">
                    {titleValue ? renderTableValueContent(titleValue) : 'Untitled record'}
                  </div>
                  {subtitleValue && subtitleValue.text !== titleValue?.text && (
                    <div className="mt-1 break-words text-sm text-muted-foreground">
                      {renderTableValueContent(subtitleValue)}
                    </div>
                  )}
                  {descriptionValue && (
                    <div className="mt-2 whitespace-pre-wrap break-words text-sm leading-6 text-foreground/90">
                      {renderTableValueContent(descriptionValue)}
                    </div>
                  )}
                </div>

                {detailColumns.length > 0 && (
                  <div className="space-y-2">
                    {detailColumns.map(field => {
                      const formatted = formatTableValue(row?.[field], formats[field] as ReportFormatterName | undefined)
                      const useBlockLayout = formatted.prefersBlock || formatted.rawText.length > 72
                      return (
                        <div key={field} className={`rounded-lg bg-background/45 px-3 py-2 ${useBlockLayout ? 'space-y-1.5' : 'flex items-start justify-between gap-3'}`}>
                          <div className="min-w-0 text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">
                            {field}
                          </div>
                          <div className={`min-w-0 break-words text-sm text-foreground ${useBlockLayout ? 'text-left whitespace-pre-wrap' : 'text-right'} ${formatted.isNumeric ? 'tabular-nums' : ''}`}>
                            {renderTableValueContent(formatted)}
                          </div>
                        </div>
                      )
                    })}
                  </div>
                )}

                {linkValue?.href && (
                  <div className="mt-auto">
                    <a
                      href={linkValue.href}
                      target="_blank"
                      rel="noreferrer"
                      className="inline-flex items-center rounded-full border border-border bg-background/80 px-3 py-1.5 text-xs font-medium text-foreground transition-colors hover:bg-muted"
                    >
                      Open
                    </a>
                  </div>
                )}
              </div>
            </div>
          )
        })}
      </div>

      {columns.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {columns.map(col => {
            const isSorted = sortField === col
            return (
              <button
                key={col}
                type="button"
                onClick={() => onSortClick(col)}
                className={`inline-flex items-center gap-1 rounded-full border px-2.5 py-1 text-xs transition-colors ${isSorted ? 'border-primary/40 bg-primary/10 text-primary' : 'border-border bg-background/70 text-muted-foreground hover:bg-muted hover:text-foreground'}`}
              >
                <span>{col}</span>
                {isSorted ? (sortDir === 'asc' ? <ArrowUp className="h-3 w-3" /> : <ArrowDown className="h-3 w-3" />) : null}
              </button>
            )
          })}
        </div>
      )}

      {totalPages > 1 && (
        <div className="flex flex-wrap items-center justify-end gap-1.5 text-xs text-muted-foreground">
          <button
            onClick={() => setPage(p => Math.max(0, p - 1))}
            disabled={safePage === 0}
            className="inline-flex items-center gap-0.5 rounded-md border border-border bg-background px-2 py-1 disabled:opacity-30 hover:bg-muted hover:text-foreground transition-colors"
            aria-label="Previous page"
          >
            <ChevronLeft className="h-3.5 w-3.5" />
            Prev
          </button>
          <span className="inline-flex items-center rounded-md bg-primary/10 px-2 py-1 font-medium text-primary tabular-nums">
            {safePage + 1}
            <span className="mx-1 opacity-60">/</span>
            {totalPages}
          </span>
          <button
            onClick={() => setPage(p => Math.min(totalPages - 1, p + 1))}
            disabled={safePage >= totalPages - 1}
            className="inline-flex items-center gap-0.5 rounded-md border border-border bg-background px-2 py-1 disabled:opacity-30 hover:bg-muted hover:text-foreground transition-colors"
            aria-label="Next page"
          >
            Next
            <ChevronRight className="h-3.5 w-3.5" />
          </button>
        </div>
      )}
    </div>
  )
}
