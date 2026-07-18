import { describe, expect, it } from 'vitest'

import { liveTerminalControlKey } from './liveTerminalKeys'

describe('liveTerminalControlKey', () => {
  it.each([
    ['ArrowUp', 'Up'],
    ['ArrowDown', 'Down'],
    ['Enter', 'Enter'],
    ['Escape', 'Escape'],
  ])('maps empty-input %s to %s', (key, expected) => {
    expect(liveTerminalControlKey({ key }, '')).toBe(expected)
  })

  it('does not take navigation or Enter away from typed chat input', () => {
    expect(liveTerminalControlKey({ key: 'ArrowDown' }, 'draft')).toBeNull()
    expect(liveTerminalControlKey({ key: 'Enter' }, 'draft')).toBeNull()
  })

  it('routes Escape even when the composer contains a draft', () => {
    expect(liveTerminalControlKey({ key: 'Escape' }, 'draft')).toBe('Escape')
  })

  it('does not intercept modified keys', () => {
    expect(liveTerminalControlKey({ key: 'Enter', shiftKey: true }, '')).toBeNull()
    expect(liveTerminalControlKey({ key: 'ArrowDown', metaKey: true }, '')).toBeNull()
  })

  it('leaves non-contract navigation keys with the composer', () => {
    expect(liveTerminalControlKey({ key: 'ArrowLeft' }, '')).toBeNull()
    expect(liveTerminalControlKey({ key: 'Tab' }, '')).toBeNull()
  })
})
