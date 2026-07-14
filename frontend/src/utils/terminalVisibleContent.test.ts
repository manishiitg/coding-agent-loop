import { describe, expect, it } from 'vitest'
import { terminalPayloadHasVisibleContent } from './terminalVisibleContent'

describe('terminalPayloadHasVisibleContent', () => {
  it('rejects terminal control-only seeds', () => {
    expect(terminalPayloadHasVisibleContent('\x1bc\x1b[2J\x1b[H\x1b[?25h   \r\n')).toBe(false)
    expect(terminalPayloadHasVisibleContent('\x1b]0;Claude Code\x07\x1b[1;1H')).toBe(false)
  })

  it('accepts readable text surrounded by terminal controls', () => {
    expect(terminalPayloadHasVisibleContent('\x1b[32mClaude Code\x1b[0m\r\n')).toBe(true)
    expect(terminalPayloadHasVisibleContent('\x1b]0;title\x07\x1b[2JReady')).toBe(true)
  })
})
