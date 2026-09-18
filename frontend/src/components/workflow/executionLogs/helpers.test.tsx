import { describe, expect, it } from 'vitest'
import { getDefaultRunFolder, isWebhookRunFolder } from './helpers'

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

describe('execution log run selection', () => {
  it('keeps an explicitly opened run even when another grouped run is available', () => {
    expect(getDefaultRunFolder('iteration-85-slack-one', ['iteration-84/default'])).toBe('iteration-85-slack-one')
  })
})
