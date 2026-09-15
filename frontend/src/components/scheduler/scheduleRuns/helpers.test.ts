import { describe, expect, it } from 'vitest'
import { defaultSchedulePanelView } from './helpers'

describe('defaultSchedulePanelView', () => {
  it('opens global views on the per-workflow grouping', () => {
    expect(defaultSchedulePanelView(false)).toBe('by-workflow')
  })

  it('opens a workflow-scoped view on the schedule list', () => {
    expect(defaultSchedulePanelView(true)).toBe('schedules')
  })
})
