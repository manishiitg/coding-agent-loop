// Cell formatters used by report TableWidget. Maps a named preset
// (declared in report_plan.json widget `formats`) to a stringification function.
// Keep these as named presets — not user-supplied expressions — so the schema
// stays bounded and predictable.

import type { ReportFormatterName } from '../services/api-types'

export interface FormatResult {
  text: string
  // Whether this cell holds a number-like value (used for right-alignment).
  isNumeric: boolean
}

const inrFmt = new Intl.NumberFormat('en-IN', { maximumFractionDigits: 2, minimumFractionDigits: 2 })
const usdFmt = new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', maximumFractionDigits: 2 })
const numFmt = new Intl.NumberFormat('en-US', { maximumFractionDigits: 0 })
const num1dpFmt = new Intl.NumberFormat('en-US', { minimumFractionDigits: 1, maximumFractionDigits: 1 })
const num2dpFmt = new Intl.NumberFormat('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
const shortDateFmt = new Intl.DateTimeFormat(undefined, { year: 'numeric', month: 'short', day: 'numeric' })
const longDateFmt = new Intl.DateTimeFormat(undefined, { year: 'numeric', month: 'long', day: 'numeric' })
const datetimeFmt = new Intl.DateTimeFormat(undefined, {
  year: 'numeric', month: 'short', day: 'numeric',
  hour: '2-digit', minute: '2-digit',
})

function asNumber(value: unknown): number | null {
  if (typeof value === 'number' && Number.isFinite(value)) return value
  if (typeof value === 'string') {
    const trimmed = value.trim()
    if (trimmed === '') return null
    const n = Number(trimmed)
    return Number.isFinite(n) ? n : null
  }
  return null
}

function asDate(value: unknown): Date | null {
  if (value instanceof Date && !Number.isNaN(value.getTime())) return value
  if (typeof value === 'string' || typeof value === 'number') {
    const d = new Date(value as string | number)
    return Number.isNaN(d.getTime()) ? null : d
  }
  return null
}

const EM_DASH: FormatResult = { text: '—', isNumeric: false }

export function formatNamed(value: unknown, preset: ReportFormatterName): FormatResult {
  if (value == null) return EM_DASH
  switch (preset) {
    case 'currency-inr': {
      const n = asNumber(value)
      return n == null ? EM_DASH : { text: `₹${inrFmt.format(n)}`, isNumeric: true }
    }
    case 'currency-usd': {
      const n = asNumber(value)
      return n == null ? EM_DASH : { text: usdFmt.format(n), isNumeric: true }
    }
    case 'percent': {
      const n = asNumber(value)
      // Treat values <= 1 as fractions (0.42 → 42%); values > 1 as already-percent (42 → 42%).
      return n == null ? EM_DASH : { text: `${(Math.abs(n) <= 1 ? n * 100 : n).toFixed(0)}%`, isNumeric: true }
    }
    case 'percent-1dp': {
      const n = asNumber(value)
      return n == null ? EM_DASH : { text: `${(Math.abs(n) <= 1 ? n * 100 : n).toFixed(1)}%`, isNumeric: true }
    }
    case 'short-date': {
      const d = asDate(value)
      return d == null ? EM_DASH : { text: shortDateFmt.format(d), isNumeric: false }
    }
    case 'long-date': {
      const d = asDate(value)
      return d == null ? EM_DASH : { text: longDateFmt.format(d), isNumeric: false }
    }
    case 'datetime': {
      const d = asDate(value)
      return d == null ? EM_DASH : { text: datetimeFmt.format(d), isNumeric: false }
    }
    case 'number': {
      const n = asNumber(value)
      return n == null ? EM_DASH : { text: numFmt.format(n), isNumeric: true }
    }
    case 'number-1dp': {
      const n = asNumber(value)
      return n == null ? EM_DASH : { text: num1dpFmt.format(n), isNumeric: true }
    }
    case 'number-2dp': {
      const n = asNumber(value)
      return n == null ? EM_DASH : { text: num2dpFmt.format(n), isNumeric: true }
    }
    case 'bytes': {
      const n = asNumber(value)
      if (n == null) return EM_DASH
      const units = ['B', 'KB', 'MB', 'GB', 'TB']
      let v = n
      let i = 0
      while (Math.abs(v) >= 1024 && i < units.length - 1) {
        v /= 1024
        i++
      }
      return { text: `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`, isNumeric: true }
    }
    case 'boolean-icon': {
      if (value === true) return { text: '✓', isNumeric: false }
      if (value === false) return { text: '✗', isNumeric: false }
      return EM_DASH
    }
    default:
      return EM_DASH
  }
}

// Auto-format when no explicit preset is given. Keeps display reasonable for raw
// JSON values without forcing the builder to declare formats for every column.
//   - numbers >= 1000 get thousand separators
//   - ISO date strings (yyyy-mm-dd...) get short-date
//   - booleans get ✓ / ✗
//   - everything else falls through to String(value)
const ISO_DATE_RE = /^\d{4}-\d{2}-\d{2}([Tt]|$)/
export function formatAuto(value: unknown): FormatResult {
  if (value == null) return EM_DASH
  if (typeof value === 'boolean') {
    return { text: value ? '✓' : '✗', isNumeric: false }
  }
  if (typeof value === 'number' && Number.isFinite(value)) {
    if (Number.isInteger(value)) {
      return { text: numFmt.format(value), isNumeric: true }
    }
    return { text: num2dpFmt.format(value), isNumeric: true }
  }
  if (typeof value === 'string') {
    if (ISO_DATE_RE.test(value)) {
      const d = asDate(value)
      if (d) return { text: shortDateFmt.format(d), isNumeric: false }
    }
    return { text: value, isNumeric: false }
  }
  // Object / array — show JSON for the user to inspect, no special formatting.
  try {
    return { text: JSON.stringify(value), isNumeric: false }
  } catch {
    return { text: String(value), isNumeric: false }
  }
}

// Used by sortable column headers to compare cells. Numeric/date values sort
// natively; everything else falls back to localeCompare.
export function compareValues(a: unknown, b: unknown): number {
  if (a == null && b == null) return 0
  if (a == null) return 1
  if (b == null) return -1
  const an = asNumber(a)
  const bn = asNumber(b)
  if (an != null && bn != null) return an - bn
  const ad = asDate(a)
  const bd = asDate(b)
  if (ad && bd) return ad.getTime() - bd.getTime()
  return String(a).localeCompare(String(b), undefined, { numeric: true })
}

// Convert a row array to CSV. Strings get quoted only when they contain
// commas, quotes, or newlines (RFC 4180 minimal).
export function rowsToCSV(rows: Record<string, unknown>[], columns: string[]): string {
  const escape = (v: unknown): string => {
    if (v == null) return ''
    const s = typeof v === 'object' ? JSON.stringify(v) : String(v)
    if (/[",\n\r]/.test(s)) return `"${s.replace(/"/g, '""')}"`
    return s
  }
  const header = columns.map(escape).join(',')
  const body = rows
    .map(row => columns.map(c => escape(row[c])).join(','))
    .join('\n')
  return `${header}\n${body}\n`
}
