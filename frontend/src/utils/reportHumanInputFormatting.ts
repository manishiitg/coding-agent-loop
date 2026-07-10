export type ReportHumanInputContextSection = {
  label: string
  body: string
  items: string[]
}

const CONTEXT_MARKER_PATTERN = /(?:^|\s)(Proposal|Exact intended edits(?: if approved)?|Rationale|Expected impact|Risk|Evidence):\s*/gi

function displayLabel(value: string): string {
  const normalized = value.trim().toLowerCase()
  if (normalized.startsWith('exact intended edits')) return 'Intended edits'
  if (normalized === 'expected impact') return 'Expected impact'
  return normalized.charAt(0).toUpperCase() + normalized.slice(1)
}

function splitNumberedItems(value: string, forceList: boolean): { body: string; items: string[] } {
  const matches = Array.from(value.matchAll(/(?:^|\s)\((\d+)\)\s*/g))
  if (matches.length === 0 || (!forceList && matches.length < 2)) return { body: value.trim(), items: [] }

  const items = matches.map((match, index) => {
    const start = (match.index || 0) + match[0].length
    const end = index + 1 < matches.length ? matches[index + 1].index : value.length
    return value.slice(start, end).trim()
  }).filter(Boolean)

  return {
    body: value.slice(0, matches[0].index || 0).trim(),
    items,
  }
}

export function parseReportHumanInputContext(value: string): ReportHumanInputContextSection[] {
  const text = value.trim()
  if (!text) return []

  const matches = Array.from(text.matchAll(CONTEXT_MARKER_PATTERN))
  if (matches.length === 0) return [{ label: '', body: text, items: [] }]

  const sections: ReportHumanInputContextSection[] = []
  const prefix = text.slice(0, matches[0].index || 0).trim()
  if (prefix) sections.push({ label: '', body: prefix, items: [] })

  matches.forEach((match, index) => {
    const label = displayLabel(match[1])
    const start = (match.index || 0) + match[0].length
    const end = index + 1 < matches.length ? matches[index + 1].index : text.length
    const content = text.slice(start, end).trim()
    const structured = splitNumberedItems(content, label === 'Intended edits')
    if (structured.body || structured.items.length > 0) {
      sections.push({ label, ...structured })
    }
  })

  return sections.length > 0 ? sections : [{ label: '', body: text, items: [] }]
}
