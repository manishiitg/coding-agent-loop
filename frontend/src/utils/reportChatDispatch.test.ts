// @vitest-environment happy-dom
import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  chat: {} as Record<string, any>,
  presets: {} as Record<string, any>,
  workflow: {} as Record<string, any>,
  select: vi.fn(() => true), activate: vi.fn(),
}))
vi.mock('../stores/useChatStore', () => ({ useChatStore: { getState: () => mocks.chat } }))
vi.mock('../stores/useGlobalPresetStore', () => ({ useGlobalPresetStore: { getState: () => mocks.presets } }))
vi.mock('../stores/useWorkflowStore', () => ({ useWorkflowStore: { getState: () => mocks.workflow } }))
vi.mock('./workflowNavigation', () => ({ selectWorkflowPreset: mocks.select }))
vi.mock('./activateTab', () => ({ activateTab: mocks.activate }))
import { sendWorkflowMessageToChat } from './reportHumanInputChat'

function chat(tabId: string, extra = {}) {
  return { tabId, isStreaming: false, metadata: { mode: 'workflow', presetQueryId: 'one' }, ...extra }
}

beforeEach(() => {
  vi.clearAllMocks()
  mocks.select.mockReturnValue(true)
  mocks.presets = { workflowPresets: [{ id: 'one', selectedFolder: { filepath: 'Workflow/one' } }], refreshPresets: vi.fn() }
  mocks.workflow = { setShowChatArea: vi.fn(), setShowWorkspacePane: vi.fn(), setFocusedPane: vi.fn() }
  mocks.chat = {
    chatTabs: {}, activeTabId: null,
    createChatTab: vi.fn(async () => {
      mocks.chat.chatTabs.fresh = chat('fresh')
      return 'fresh'
    }),
    getTab: (id: string) => mocks.chat.chatTabs[id],
    getTabConfig: vi.fn(() => ({ queuedMessages: ['earlier request'], inputText: 'My unsent draft' })),
    setTabConfig: vi.fn(), setTabViewMode: vi.fn(), setAutoScroll: vi.fn(),
  }
})

describe('shared Ask in chat dispatch for reports', () => {
  it('appends to the running interactive chat queue without taking over a scheduled run', async () => {
    mocks.chat.chatTabs = {
      running: chat('running', { isStreaming: true }),
      scheduled: chat('scheduled', { isStreaming: true, metadata: { mode: 'workflow', presetQueryId: 'one', isScheduledRun: true } }),
      other: chat('other', { metadata: { mode: 'workflow', presetQueryId: 'other' } }),
    }
    const result = await sendWorkflowMessageToChat({ workspacePath: 'Workflow/one', message: 'Apply finding 42' })
    expect(result).toEqual({ tabId: 'running', reused: true, queuedBehindRunningTurn: true })
    expect(mocks.chat.createChatTab).not.toHaveBeenCalled()
    expect(mocks.chat.setTabConfig).toHaveBeenCalledWith('running', { queuedMessages: ['earlier request', 'Apply finding 42'] })
    expect(mocks.chat.setTabConfig.mock.calls[0][1]).not.toHaveProperty('inputText')
    expect(mocks.activate).toHaveBeenCalledWith('running')
  })

  it('reuses an idle interactive chat rather than opening another', async () => {
    mocks.chat.chatTabs.existing = chat('existing')
    const result = await sendWorkflowMessageToChat({ workspacePath: 'Workflow/one', message: 'Run visual QA' })
    expect(result).toEqual({ tabId: 'existing', reused: true, queuedBehindRunningTurn: false })
    expect(mocks.chat.createChatTab).not.toHaveBeenCalled()
    expect(mocks.chat.setTabConfig).toHaveBeenCalledWith('existing', { queuedMessages: ['earlier request', 'Run visual QA'] })
  })

  it('creates a chat when this automation has none', async () => {
    const result = await sendWorkflowMessageToChat({ workspacePath: 'Workflow/one', message: 'Run visual QA' })
    expect(result).toEqual({ tabId: 'fresh', reused: false, queuedBehindRunningTurn: false })
    expect(mocks.chat.createChatTab).toHaveBeenCalledExactlyOnceWith('Automation Builder', expect.objectContaining({ presetQueryId: 'one', phaseId: 'workflow-builder' }))
  })

  it('creates a chat when only a view-only schedule exists', async () => {
    mocks.chat.chatTabs.scheduled = chat('scheduled', { metadata: { mode: 'workflow', presetQueryId: 'one', isViewOnly: true } })
    await expect(sendWorkflowMessageToChat({ workspacePath: 'Workflow/one', message: 'Apply finding 42' })).resolves.toMatchObject({ tabId: 'fresh', reused: false })
  })

  it('does not submit when the automation cannot be resolved', async () => {
    await expect(sendWorkflowMessageToChat({ workspacePath: 'Workflow/missing', message: 'Apply finding 42' })).rejects.toThrow('Could not find')
    expect(mocks.chat.setTabConfig).not.toHaveBeenCalled()
    expect(mocks.chat.createChatTab).not.toHaveBeenCalled()
  })
})
