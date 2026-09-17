import { describe, expect, it } from 'vitest'
import { acquireBuilderSubmission } from './chatSubmissionTarget'

describe('Builder submission destination', () => {
  it('binds already queued sends to the created conversation, independent of selection', () => {
    const first = acquireBuilderSubmission('account:builder:composer-1')
    const second = acquireBuilderSubmission('account:builder:composer-1')
    Object.assign(first.target, { tabId: 'created-chat', sessionId: 'created-session' })
    // Selecting an unrelated chat has no input to this binding.
    expect(second.target).toEqual({ tabId: 'created-chat', sessionId: 'created-session' })
    first.release(); second.release()
  })
  it('returning to Builder starts an independent composer sequence', () => {
    const old = acquireBuilderSubmission('account:builder:old-composer')
    old.target.tabId = 'old-chat'
    const next = acquireBuilderSubmission('account:builder:new-composer')
    expect(next.target.tabId).toBeUndefined()
    old.release(); next.release()
  })
  it('does not reuse a completed batch or another account', () => {
    const old = acquireBuilderSubmission('account-A:builder:composer')
    old.target.tabId = 'old-chat'
    old.release()
    const next = acquireBuilderSubmission('account-A:builder:composer')
    const other = acquireBuilderSubmission('account-B:builder:composer')
    expect(next.target.tabId).toBeUndefined()
    expect(other.target).not.toBe(next.target)
    next.release(); other.release()
  })
})
