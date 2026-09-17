import { beforeEach, describe, expect, it, vi } from 'vitest'

const state = vi.hoisted(() => ({ tab: undefined as any, generation: 0 }))
vi.mock('../stores/useChatStore', () => ({ useChatStore: { getState: () => ({
  getTab: () => state.tab,
  setTabConfig: (_id: string, patch: object) => Object.assign(state.tab.config, patch),
}) } }))
vi.mock('./chatIdentity', () => ({
  captureChatIdentity: () => state.generation,
  isChatIdentityCurrent: (generation: number) => generation === state.generation,
}))
import { captureChatDraft, updateOwnedChatDraft } from './chatDraftOwnership'

describe('draft acknowledgement ownership', () => {
  beforeEach(() => {
    state.generation = 1
    state.tab = { tabId: 'A', sessionId: 'conversation-A', config: { inputText: 'sent', composerRevision: 3 } }
  })
  it('consumes the exact accepted draft', () => {
    expect(updateOwnedChatDraft(captureChatDraft('A'), { inputText: '' })).toBe(true)
    expect(state.tab.config.inputText).toBe('')
  })
  it('preserves edits by a newly mounted composer even if the text is identical', () => {
    const old = captureChatDraft('A')
    state.tab.config.composerRevision++
    expect(updateOwnedChatDraft(old, { inputText: '' })).toBe(false)
    expect(state.tab.config.inputText).toBe('sent')
  })
  it('does not restore rejected input over newer text', () => {
    state.tab.config.inputText = ''
    const cleared = captureChatDraft('A')
    state.tab.config = { inputText: 'new draft', composerRevision: 4 }
    expect(updateOwnedChatDraft(cleared, { inputText: 'failed old draft' })).toBe(false)
    expect(state.tab.config.inputText).toBe('new draft')
  })
  it.each(['account', 'session', 'close'])('rejects callbacks after %s changes', change => {
    const old = captureChatDraft('A')
    if (change === 'account') state.generation++
    if (change === 'session') state.tab.sessionId = 'replacement'
    if (change === 'close') state.tab = undefined
    expect(updateOwnedChatDraft(old, { inputText: '' })).toBe(false)
  })
})
