import type { PulseFindingLifecycle } from '../../services/api-types'

const FIX_EVENTS = new Set(['closed', 'fix_applied'])
const CLOSED_STATUSES = new Set(['resolved', 'rejected', 'external_action_required'])
const WEEK_MS = 7 * 24 * 60 * 60 * 1000

export interface PulseFixStats {
  fixedThisWeek: number
  /** Median hours from first seen to fixed, over this week's fixes; null when none. */
  typicalHoursToFix: number | null
  open: number
  /** Age in days of the oldest open issue; null when none are open. */
  oldestOpenDays: number | null
}

/** How fast Pulse closes issues: this week's fixes and what is still open. */
export function pulseFixStats(findings: PulseFindingLifecycle[], now = Date.now()): PulseFixStats {
  const hours: number[] = []
  let open = 0
  let oldestOpen: number | null = null
  for (const finding of findings) {
    const firstSeen = Date.parse(finding.first_seen_at || '')
    if (!CLOSED_STATUSES.has(finding.status)) {
      open++
      if (Number.isFinite(firstSeen)) oldestOpen = oldestOpen === null ? firstSeen : Math.min(oldestOpen, firstSeen)
      continue
    }
    if (finding.status !== 'resolved') continue
    const fixedAt = Math.max(...finding.events
      .filter(event => FIX_EVENTS.has(event.event_type))
      .map(event => Date.parse(event.recorded_at))
      .filter(Number.isFinite), -Infinity)
    if (!Number.isFinite(fixedAt) || now - fixedAt > WEEK_MS) continue
    hours.push(Number.isFinite(firstSeen) ? Math.max(0, (fixedAt - firstSeen) / 3_600_000) : 0)
  }
  hours.sort((a, b) => a - b)
  const middle = Math.floor(hours.length / 2)
  const typical = hours.length === 0 ? null : hours.length % 2 ? hours[middle] : (hours[middle - 1] + hours[middle]) / 2
  return {
    fixedThisWeek: hours.length,
    typicalHoursToFix: typical,
    open,
    oldestOpenDays: oldestOpen === null ? null : Math.floor((now - oldestOpen) / (24 * 3_600_000)),
  }
}

function formatHours(hours: number): string {
  if (hours < 1) return 'under an hour'
  if (hours < 48) return `${Math.round(hours)}h`
  return `${Math.round(hours / 24)} days`
}

/** One plain line for the Pulse tab. */
export function pulseFixSummary(stats: PulseFixStats): string {
  const parts: string[] = []
  parts.push(stats.fixedThisWeek === 0 ? 'No issues fixed this week' : `Fixed ${stats.fixedThisWeek} issue${stats.fixedThisWeek === 1 ? '' : 's'} this week`
    + (stats.typicalHoursToFix === null ? '' : `, typically in ${formatHours(stats.typicalHoursToFix)}`))
  if (stats.open > 0) {
    parts.push(`${stats.open} open` + (stats.oldestOpenDays && stats.oldestOpenDays > 0 ? `, oldest ${stats.oldestOpenDays} day${stats.oldestOpenDays === 1 ? '' : 's'}` : ''))
  } else {
    parts.push('nothing open')
  }
  return parts.join(' · ')
}
