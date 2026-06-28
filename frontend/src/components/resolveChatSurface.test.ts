import { describe, it, expect } from 'vitest'
import { resolveChatSurface, type ChatSurfaceInputs } from './resolveChatSurface'

// Baseline = a fresh New Chat: nothing loading, no content, no read-only run.
const base: ChatSurfaceInputs = {
  isRestoring: false,
  resumeSettling: false,
  hasContent: false,
  isStreaming: false,
  hasRestoredLiveContent: false,
  isReadOnlyRunView: false,
}

describe('resolveChatSurface', () => {
  it('New Chat (no content, no restore) → landing', () => {
    expect(resolveChatSurface(base)).toBe('landing')
  })

  it('active content → active', () => {
    expect(resolveChatSurface({ ...base, hasContent: true })).toBe('active')
  })

  it('streaming → active', () => {
    expect(resolveChatSurface({ ...base, isStreaming: true })).toBe('active')
  })

  it('isRestoring + empty → restoring', () => {
    expect(resolveChatSurface({ ...base, isRestoring: true })).toBe('restoring')
  })

  it('resume-empty (resumeSettling) → restoring', () => {
    expect(resolveChatSurface({ ...base, resumeSettling: true })).toBe('restoring')
  })

  it('resume-with-execution/terminal content → active', () => {
    expect(resolveChatSurface({ ...base, hasRestoredLiveContent: true })).toBe('active')
  })

  // --- Native/terminal resume (the terminal-only resume flicker fix) ---
  // A native/terminal resume reattaches a live terminal but leaves NO
  // execution-tree nodes; hasRestoredLiveContent carries the terminal-presence
  // signal so the surface stays on the terminal instead of flipping to landing.
  it('resumed tab with a terminal (no execution tree) → active', () => {
    expect(resolveChatSurface({ ...base, hasRestoredLiveContent: true })).toBe('active')
  })

  it('resumed tab with neither terminal nor execution content after settle → landing', () => {
    expect(
      resolveChatSurface({ ...base, resumeSettling: false, hasRestoredLiveContent: false }),
    ).toBe('landing')
  })

  it('resumed tab still settling → restoring (not landing)', () => {
    expect(resolveChatSurface({ ...base, resumeSettling: true })).toBe('restoring')
  })

  it('resumed tab still settling but terminal already present → active (not landing)', () => {
    expect(
      resolveChatSurface({ ...base, resumeSettling: true, hasRestoredLiveContent: true }),
    ).toBe('active')
  })

  it('content wins over a still-set restoring flag → active', () => {
    expect(
      resolveChatSurface({ ...base, isRestoring: true, hasContent: true }),
    ).toBe('active')
  })

  it('restored terminal content wins over restoring flag → active', () => {
    expect(
      resolveChatSurface({ ...base, isRestoring: true, hasRestoredLiveContent: true }),
    ).toBe('active')
  })

  // --- Read-only run view (scheduled/bot run) — the "schedule-bounce" fix ---
  it('read-only run with no events yet → restoring (never landing)', () => {
    expect(resolveChatSurface({ ...base, isReadOnlyRunView: true })).toBe('restoring')
  })

  it('read-only run with events → active', () => {
    expect(
      resolveChatSurface({ ...base, isReadOnlyRunView: true, hasContent: true }),
    ).toBe('active')
  })

  it('read-only run while streaming → active', () => {
    expect(
      resolveChatSurface({ ...base, isReadOnlyRunView: true, isStreaming: true }),
    ).toBe('active')
  })

  it('a normal fresh chat still → landing (read-only off)', () => {
    expect(resolveChatSurface({ ...base, isReadOnlyRunView: false })).toBe('landing')
  })
})
