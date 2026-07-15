import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { hasSubmittedFeedback, markFeedbackSubmitted } from './notificationDedup'

const submittedFeedbackKey = 'mcp_submitted_feedback'

describe('submitted human-feedback marker', () => {
  const originalDescriptor = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')
  let values: Map<string, string>

  beforeEach(() => {
    values = new Map()
    Object.defineProperty(globalThis, 'localStorage', {
      configurable: true,
      value: {
        getItem: (key: string) => values.get(key) ?? null,
        setItem: (key: string, value: string) => values.set(key, value),
      },
    })
  })

  afterEach(() => {
    if (originalDescriptor) {
      Object.defineProperty(globalThis, 'localStorage', originalDescriptor)
    } else {
      Reflect.deleteProperty(globalThis, 'localStorage')
    }
  })

  it('persists only a completion marker', () => {
    markFeedbackSubmitted('otp-request')

    expect(hasSubmittedFeedback('otp-request')).toBe(true)
    const stored = values.get(submittedFeedbackKey) ?? ''
    expect(stored).toContain('true')
    expect(stored).not.toContain('123456')
  })

  it('sanitizes a raw answer written by an older build', () => {
    values.set(submittedFeedbackKey, JSON.stringify([
      ['legacy-request', { value: '123456', ts: Date.now() }],
    ]))

    expect(hasSubmittedFeedback('legacy-request')).toBe(true)
    const stored = values.get(submittedFeedbackKey) ?? ''
    expect(stored).not.toContain('123456')
    expect(JSON.parse(stored)[0][1].value).toBe(true)
  })
})
