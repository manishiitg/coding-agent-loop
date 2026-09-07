import { describe, it, expect } from 'vitest'
import { supportedAction } from './client'
import { UI_CONTROL_CONTRACT } from './contract.generated'

const base = { request_id: 'test', expires_at: '2030-01-01T00:00:00Z' }
describe('closed semantic UI control contract', () => {
  it('accounts for all views but never advertises placeholder deep actions', () => {
    expect(UI_CONTROL_CONTRACT.views).toHaveLength(22)
    for (const { id } of UI_CONTROL_CONTRACT.views) {
      expect(supportedAction({ ...base, view: id, action: 'open' })).toBe(true)
      expect(supportedAction({ ...base, view: id, action: 'send' })).toBe(false)
      expect(supportedAction({ ...base, view: id, action: 'open', target: 'guessed' })).toBe(id === 'flow' || id === 'report' || id === 'files')
      expect(supportedAction({ ...base, view: id, action: 'refresh' })).toBe(false)
    }
  })
  it('allows a bounded report tab target', () => {
    expect(supportedAction({ ...base, view: 'report', action: 'open', target: 'Suites' })).toBe(true)
  })
  it('advertises a workspace file target', () => {
    expect(supportedAction({ ...base, view: 'files', action: 'open', target: 'code/shared/helpers.py' })).toBe(true)
  })
  it('only expands the two known notification instruction disclosures', () => {
    expect(supportedAction({ ...base, view: 'notify', action: 'expand', target: 'run_summary' })).toBe(true)
    expect(supportedAction({ ...base, view: 'notify', action: 'expand', target: 'pulse_review' })).toBe(true)
    expect(supportedAction({ ...base, view: 'notify', action: 'expand', target: 'send-test' })).toBe(false)
    expect(supportedAction({ ...base, view: 'unknown', action: 'open' })).toBe(false)
  })
})
