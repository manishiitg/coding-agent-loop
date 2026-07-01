import { describe, expect, it } from 'vitest'
import { normalizeAnsiForEmbeddedXterm } from './ansiSanitize'

describe('normalizeAnsiForEmbeddedXterm', () => {
  it('removes semicolon neutral 256-color backgrounds while preserving foregrounds', () => {
    expect(normalizeAnsiForEmbeddedXterm('\x1b[48;5;237;38;5;231mHello\x1b[0m'))
      .toBe('\x1b[38;5;231mHello\x1b[0m')
  })

  it('removes colon neutral 256-color backgrounds while preserving foregrounds', () => {
    expect(normalizeAnsiForEmbeddedXterm('\x1b[48:5:237;38:5:231mHello\x1b[0m'))
      .toBe('\x1b[38:5:231mHello\x1b[0m')
  })

  it('removes neutral truecolor backgrounds while preserving foregrounds', () => {
    expect(normalizeAnsiForEmbeddedXterm('\x1b[48:2::58:58:58;38:2::255:255:255mHello\x1b[0m'))
      .toBe('\x1b[38:2::255:255:255mHello\x1b[0m')
    expect(normalizeAnsiForEmbeddedXterm('\x1b[48;2;58;58;58;38;5;231mHello\x1b[0m'))
      .toBe('\x1b[38;5;231mHello\x1b[0m')
  })

  it('keeps non-neutral colored backgrounds', () => {
    expect(normalizeAnsiForEmbeddedXterm('\x1b[48;5;160;38;5;231mHello\x1b[0m'))
      .toBe('\x1b[48;5;160;38;5;231mHello\x1b[0m')
    expect(normalizeAnsiForEmbeddedXterm('\x1b[48:2::12:80:180;38:5:231mHello\x1b[0m'))
      .toBe('\x1b[48:2::12:80:180;38:5:231mHello\x1b[0m')
  })

  it('lifts the dark prompt marker when its neutral background is removed', () => {
    expect(normalizeAnsiForEmbeddedXterm('\x1b[48;5;237;38;5;239m❯ \x1b[0m'))
      .toBe('\x1b[38;5;244m❯ \x1b[0m')
  })

  it('keeps inverse video and foreground styles', () => {
    expect(normalizeAnsiForEmbeddedXterm('\x1b[1;7;38;5;231mHello\x1b[27m'))
      .toBe('\x1b[1;7;38;5;231mHello\x1b[27m')
  })
})
