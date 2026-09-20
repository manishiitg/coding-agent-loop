import { describe, expect, it } from 'vitest'
import {
  buildSqAnswerText,
  buildSqTimerText,
  sanitizeSqId,
  sanitizeSqTimerConfigs,
  SQ_FALLBACK_VARS,
  withViewerLinkBridge,
} from './sqOps'

describe('sqOps', () => {
  it('prefixes widget answers with their question id', () => {
    expect(buildSqAnswerText('q3', '3/5')).toBe('q3: 3/5')
    expect(buildSqAnswerText('', 'hello')).toBe('hello')
    expect(buildSqAnswerText('q3', '   ')).toBeNull()
    expect(buildSqAnswerText('q3', 42 as unknown as string)).toBeNull()
  })

  it('sanitizes question ids and caps answer length', () => {
    expect(sanitizeSqId('../../etc')).toBe('etc')
    expect(sanitizeSqId('q'.repeat(100)).length).toBe(32)
    expect(buildSqAnswerText('q1', 'x'.repeat(5000))?.length).toBeLessThanOrEqual(2004)
  })

  it('marks timer expiries as system lines, never her words', () => {
    expect(buildSqTimerText('q3')).toBe('[Timer] Time expired on q3 with no answer.')
    expect(buildSqTimerText('')).toBe('[Timer] Time expired on this test.')
  })

  it('keeps only sane timer configs', () => {
    const configs = sanitizeSqTimerConfigs([
      { qid: 'q1', seconds: 120 },
      { qid: 'q2', seconds: 2 },
      { qid: 'q3', seconds: 1e9 },
      { qid: 'q1', seconds: 60 },
      'junk',
      null,
    ])
    expect(configs).toEqual([{ qid: 'q1', seconds: 120 }])
    expect(sanitizeSqTimerConfigs('nope')).toEqual([])
  })

  it('bridges viewer links: fallback theme first, click script last', () => {
    const out = withViewerLinkBridge('<html><head><style>:root{--good:#000}</style></head><body><a href="#s1">x</a></body></html>')
    // Fallback vars come before the page's own CSS so valid page vars win.
    expect(out.indexOf(SQ_FALLBACK_VARS)).toBeLessThan(out.indexOf('--good:#000'))
    expect(out).toContain("op: 'open'")
    expect(out).toContain('scrollIntoView')
    expect(out).toContain('preventDefault')
  })

  it('injects the fallback theme even with no head element', () => {
    const out = withViewerLinkBridge('<div id="s1">hi</div>')
    expect(out.startsWith(`<style>${SQ_FALLBACK_VARS}</style>`)).toBe(true)
    expect(out).toContain('scrollIntoView')
  })
})
