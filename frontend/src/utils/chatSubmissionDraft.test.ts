import { describe, expect, it } from 'vitest'
import { shouldClearAcceptedChatDraft } from './chatSubmissionDraft'

describe('shouldClearAcceptedChatDraft', () => {
  it('clears the exact draft after the source tab accepts it', () => {
    expect(shouldClearAcceptedChatDraft({
      accepted: true,
      submittedTabId: 'tab-a',
      currentTabId: 'tab-a',
      submittedMessage: 'find investors',
      currentMessage: 'find investors',
    })).toBe(true)
  })

  it('keeps a draft when the backend rejects the message', () => {
    expect(shouldClearAcceptedChatDraft({
      accepted: false,
      submittedTabId: 'tab-a',
      currentTabId: 'tab-a',
      submittedMessage: 'find investors',
      currentMessage: 'find investors',
    })).toBe(false)
  })

  it('does not clear another tab after a switch during submission', () => {
    expect(shouldClearAcceptedChatDraft({
      accepted: true,
      submittedTabId: 'tab-a',
      currentTabId: 'tab-b',
      submittedMessage: 'find investors',
      currentMessage: 'publish update',
    })).toBe(false)
  })

  it('does not clear newer text typed while the send is in flight', () => {
    expect(shouldClearAcceptedChatDraft({
      accepted: true,
      submittedTabId: 'tab-a',
      currentTabId: 'tab-a',
      submittedMessage: 'find investors',
      currentMessage: 'find investors in Europe',
    })).toBe(false)
  })
})
