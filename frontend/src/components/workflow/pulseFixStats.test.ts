import { describe, expect, it } from 'vitest'
import type { PulseFindingLifecycle } from '../../services/api-types'
import { pulseFixStats, pulseFixSummary } from './pulseFixStats'

const now = Date.parse('2026-09-24T12:00:00Z')
function finding(status: string, firstSeen: string, events: Array<[string, string]> = []): PulseFindingLifecycle {
  return {
    status, first_seen_at: firstSeen,
    events: events.map(([event_type, recorded_at]) => ({ event_type, recorded_at, summary: '' })),
  } as unknown as PulseFindingLifecycle
}

describe('pulseFixStats', () => {
  it('counts this week\'s fixes, their typical time, and what is still open', () => {
    const stats = pulseFixStats([
      finding('resolved', '2026-09-24T08:00:00Z', [['fix_applied', '2026-09-24T10:00:00Z']]),
      finding('resolved', '2026-09-22T12:00:00Z', [['closed', '2026-09-23T12:00:00Z']]),
      finding('resolved', '2026-09-01T00:00:00Z', [['closed', '2026-09-02T00:00:00Z']]),
      finding('rejected', '2026-09-23T00:00:00Z', [['rejected', '2026-09-23T01:00:00Z']]),
      finding('open', '2026-09-21T12:00:00Z'),
      finding('acknowledged', '2026-09-23T12:00:00Z'),
    ], now)
    expect(stats).toEqual({ fixedThisWeek: 2, typicalHoursToFix: 13, open: 2, oldestOpenDays: 3 })
    expect(pulseFixSummary(stats)).toBe('Fixed 2 issues this week, typically in 13h · 2 open, oldest 3 days')
  })

  it('says so plainly when nothing was fixed and nothing is open', () => {
    expect(pulseFixSummary(pulseFixStats([], now))).toBe('No issues fixed this week · nothing open')
  })
})
