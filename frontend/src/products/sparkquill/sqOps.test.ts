import { describe, expect, it } from 'vitest'
import {
  buildSqAnswerText,
  buildSqTimerText,
  sanitizeSqId,
  sanitizeSqTimerConfigs,
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
})
