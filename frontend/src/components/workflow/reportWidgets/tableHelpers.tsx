// Helpers shared across Table / Cards / Pivot / Chart widgets:
//   - column inference (compact / numeric)
//   - cell value rendering
//   - widget palette + semantic-color resolution
//   - the compact-layout hook
//
// Pulled out of ReportViewer.tsx so the heavy widget renderers can each live
// in their own file without duplicating these utilities.

import { useEffect, useRef, useState } from 'react'
import { formatAuto, formatNamed, type FormatResult } from '../../../lib/reportFormatters'
import type { ReportFormatterName, ReportWidget } from '../../../services/api-types'

// Default categorical palette. Widgets override via `colors:` / `colorsDark:`.
// Keep theme-driven so report charts follow the active app palette.
export const CHART_COLORS = [
  'hsl(var(--chart-1))',
  'hsl(var(--chart-2))',
  'hsl(var(--chart-3))',
  'hsl(var(--chart-4))',
  'hsl(var(--chart-5))',
  'hsl(var(--primary))',
  'hsl(var(--warning))',
  'hsl(var(--success))',
]

// Default rows-per-page for tables; overridable per-widget via `page_size:`.
export const DEFAULT_TABLE_PAGE_SIZE = 25

export type SortDirection = 'asc' | 'desc'

// Tracks whether the parent container is below the compact-layout threshold so
// table/cards widgets can switch from a multi-column grid to a stacked card
// layout. The hook returns a ref the consumer attaches to its outer wrapper.
export function useCompactWidgetLayout(maxWidth = 520) {
  const ref = useRef<HTMLDivElement | null>(null)
  const [isCompact, setIsCompact] = useState(false)

  useEffect(() => {
    const node = ref.current
    if (!node) return

    const update = (width: number) => {
      setIsCompact(width <= maxWidth)
    }

    const measure = () => update(node.getBoundingClientRect().width)
    measure()

    if (typeof ResizeObserver !== 'undefined') {
      const observer = new ResizeObserver(entries => {
        const entry = entries[0]
        if (!entry) return
        update(entry.contentRect.width)
      })
      observer.observe(node)
      return () => observer.disconnect()
    }

    window.addEventListener('resize', measure)
    return () => window.removeEventListener('resize', measure)
  }, [maxWidth])

  return [ref, isCompact] as const
}

// Three-tier sibling of useCompactWidgetLayout. Used by the section grid
// container so a user-declared `columns: 12` collapses to ~half on tablets
// (640–960px) and 1 column on phones (<640px), matching the project's
// Tailwind sm/md breakpoints. Container-width based, not viewport-based, so
// it works inside split-pane / preview-mode layouts where the report tab
// is narrower than the viewport.
export type ContainerSizeTier = 'phone' | 'tablet' | 'desktop'

export function useContainerSizeTier(phoneMax = 640, tabletMax = 960) {
  const ref = useRef<HTMLDivElement | null>(null)
  const [tier, setTier] = useState<ContainerSizeTier>('desktop')

  useEffect(() => {
    const node = ref.current
    if (!node) return

    const update = (width: number) => {
      if (width <= phoneMax) setTier('phone')
      else if (width <= tabletMax) setTier('tablet')
      else setTier('desktop')
    }

    const measure = () => update(node.getBoundingClientRect().width)
    measure()

    if (typeof ResizeObserver !== 'undefined') {
      const observer = new ResizeObserver(entries => {
        const entry = entries[0]
        if (!entry) return
        update(entry.contentRect.width)
      })
      observer.observe(node)
      return () => observer.disconnect()
    }

    window.addEventListener('resize', measure)
    return () => window.removeEventListener('resize', measure)
  }, [phoneMax, tabletMax])

  return [ref, tier] as const
}

const COMPACT_PRIMARY_COLUMN_CANDIDATES = [
  'title',
  'name',
  'label',
  'headline',
  'job_title',
  'role',
  'position',
]

const COMPACT_SECONDARY_COLUMN_CANDIDATES = [
  'subtitle',
  'company',
  'company_name',
  'budget_display',
  'status',
  'location',
  'type',
  'created_at',
  'updated_at',
]

const COMPACT_DEPRIORITIZED_COLUMNS = new Set([
  'id',
  'url',
  'job_url',
  'link',
  'description',
  'job_text',
  'text',
  'content',
  'body',
  'summary',
])

export function isPrimitiveTableValue(value: unknown): value is string | number | boolean {
  return typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean'
}

export function isURLString(value: string): boolean {
  return /^https?:\/\//i.test(value)
}

export function stringifyTableValue(value: unknown): string {
  if (value == null) return '—'
  if (Array.isArray(value)) {
    if (value.length === 0) return '—'
    if (value.every(isPrimitiveTableValue)) return value.map(item => String(item)).join(', ')
    try {
      return JSON.stringify(value)
    } catch {
      return String(value)
    }
  }
  if (typeof value === 'object') {
    const entries = Object.entries(value as Record<string, unknown>)
    if (entries.length === 0) return '—'
    if (entries.every(([, item]) => item == null || isPrimitiveTableValue(item))) {
      return entries
        .map(([key, item]) => `${key}: ${item == null ? '—' : String(item)}`)
        .join(', ')
    }
    try {
      return JSON.stringify(value)
    } catch {
      return String(value)
    }
  }
  return String(value)
}

export function formatTableValue(value: unknown, preset?: ReportFormatterName): FormatResult & {
  href?: string
  rawText: string
  prefersBlock: boolean
} {
  if (preset) {
    const formatted = formatNamed(value, preset)
    return {
      ...formatted,
      rawText: formatted.text,
      prefersBlock: formatted.text.length > 80 || formatted.text.includes('\n'),
    }
  }

  const rawText = stringifyTableValue(value)
  if (typeof value === 'string' && isURLString(value)) {
    return {
      text: value,
      href: value,
      isNumeric: false,
      rawText,
      prefersBlock: true,
    }
  }

  if (Array.isArray(value) || (value != null && typeof value === 'object')) {
    return {
      text: rawText,
      isNumeric: false,
      rawText,
      prefersBlock: rawText.length > 60 || Array.isArray(value),
    }
  }

  const formatted = formatAuto(value)
  return {
    ...formatted,
    rawText,
    prefersBlock: rawText.length > 80 || rawText.includes('\n'),
  }
}

export function renderTableValueContent(formatted: {
  text: string
  href?: string
}) {
  if (formatted.href) {
    return (
      <a
        href={formatted.href}
        target="_blank"
        rel="noreferrer"
        className="text-primary underline underline-offset-2 break-all hover:text-primary/80"
      >
        {formatted.text}
      </a>
    )
  }
  return formatted.text
}

export function collectVisibleColumns(rows: Array<Record<string, unknown>>, hidden: Set<string>): string[] {
  const cols: string[] = []
  const seen = new Set<string>()
  for (const row of rows) {
    if (!row || typeof row !== 'object') continue
    for (const key of Object.keys(row)) {
      if (!seen.has(key) && !hidden.has(key)) {
        seen.add(key)
        cols.push(key)
      }
    }
  }
  return cols
}

export function detectNumericColumns(rows: Array<Record<string, unknown>>, columns: string[]): Set<string> {
  const out = new Set<string>()
  for (const col of columns) {
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
}

export function inferPrimaryColumn(columns: string[], numericColumns: Set<string>, preferred?: string): string | null {
  if (preferred && columns.includes(preferred)) return preferred
  const nonNumericColumns = columns.filter(col => !numericColumns.has(col))
  const candidate = COMPACT_PRIMARY_COLUMN_CANDIDATES.find(name => nonNumericColumns.includes(name))
  if (candidate) return candidate
  return nonNumericColumns.find(col => !COMPACT_DEPRIORITIZED_COLUMNS.has(col)) ?? nonNumericColumns[0] ?? columns[0] ?? null
}

export function inferSecondaryColumn(
  columns: string[],
  numericColumns: Set<string>,
  primaryColumn: string | null,
  preferred?: string,
): string | null {
  const remainingColumns = columns.filter(col => col !== primaryColumn && !numericColumns.has(col))
  if (preferred && remainingColumns.includes(preferred)) return preferred
  const candidate = COMPACT_SECONDARY_COLUMN_CANDIDATES.find(name => remainingColumns.includes(name))
  if (candidate) return candidate
  return remainingColumns.find(col => !COMPACT_DEPRIORITIZED_COLUMNS.has(col)) ?? remainingColumns[0] ?? null
}

// Resolves the effective color palette for a widget given the current theme.
// Precedence: colorsDark (when dark) > colors > CHART_COLORS.
export function resolvePalette(widget: ReportWidget, theme: 'light' | 'dark'): string[] {
  if (theme === 'dark' && widget.colorsDark && widget.colorsDark.length > 0) return widget.colorsDark
  if (widget.colors && widget.colors.length > 0) return widget.colors
  return CHART_COLORS
}

// Resolves a per-row color from widget semantic coloring. Returns undefined when
// no match exists (caller falls back to cycled palette / default).
export function resolveSemanticColor(
  widget: ReportWidget,
  row: Record<string, unknown> | null | undefined,
  palette: string[],
  index: number,
): string | undefined {
  if (!widget.colorBy || !row) return undefined
  const rawValue = row[widget.colorBy]
  if (rawValue === undefined || rawValue === null) return undefined
  const key = String(rawValue)
  if (widget.colorMap && widget.colorMap[key]) return widget.colorMap[key]
  // No map entry — cycle palette deterministically by the distinct-value index.
  if (palette.length > 0) return palette[index % palette.length]
  return undefined
}

// Shifts a hex/named color toward a subtle tint — used for table row backgrounds
// so semantic coloring stays legible against the app theme. Hex shortcuts only;
// named colors pass through at low opacity via rgba-ish CSS.
export function toRowTint(color: string): string {
  // #rgb / #rrggbb → 14% alpha; named colors → rely on color-mix-ish fallback.
  if (color.startsWith('#')) {
    if (color.length === 4) {
      const r = color[1], g = color[2], b = color[3]
      return `#${r}${r}${g}${g}${b}${b}24` // ~14% alpha
    }
    if (color.length === 7) return `${color}24`
    if (color.length === 9) return color // already has alpha
  }
  return color
}
