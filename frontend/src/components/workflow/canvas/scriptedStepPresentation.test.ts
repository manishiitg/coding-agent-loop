import { describe, expect, it } from 'vitest'

import { planStepTypeLabel, scriptedStepFilePath } from './scriptedStepPresentation'

describe('scripted step presentation', () => {
  it('uses the user-facing scripted step label for regular plan steps', () => {
    expect(planStepTypeLabel('regular')).toBe('scripted step')
    expect(planStepTypeLabel('routing')).toBe('routing')
  })

  it('labels crew plan steps as Crew', () => {
    expect(planStepTypeLabel('crew')).toBe('Crew')
  })

  it('resolves the canonical script path from the workflow code layout', () => {
    expect(scriptedStepFilePath('provider-lookup', 0)).toBe('learnings/provider-lookup/main.py')
    expect(scriptedStepFilePath('provider-lookup', 1)).toBe('code/provider-lookup/main.py')
  })
})
