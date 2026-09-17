import { describe, expect, it } from 'vitest'
import { isWebhookRunFolder } from './helpers'

describe('isWebhookRunFolder', () => {
  it('recognizes webhook iteration roots and group folders', () => {
    expect(isWebhookRunFolder('iteration-6-hook')).toBe(true)
    expect(isWebhookRunFolder('iteration-6-hook/default')).toBe(true)
  })

  it('does not expose payload UI for manual or scheduled runs', () => {
    expect(isWebhookRunFolder('iteration-6/default')).toBe(false)
    expect(isWebhookRunFolder('iteration-6-sched/default')).toBe(false)
    expect(isWebhookRunFolder(null)).toBe(false)
  })
})
