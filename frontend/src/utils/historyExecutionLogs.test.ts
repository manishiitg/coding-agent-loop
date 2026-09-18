import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { PollingEvent } from '../services/api-types'
const mocks = vi.hoisted(() => ({ getTabEvents: vi.fn(), setSelectedRunFolder: vi.fn(), openWorkspaceView: vi.fn() }))
vi.mock('../stores/useChatStore', () => ({ useChatStore: { getState: () => ({ getTabEvents: mocks.getTabEvents }) } }))
vi.mock('../stores/useWorkflowStore', () => ({ useWorkflowStore: { getState: () => mocks } }))
import { historyExecutionRunFolder, openHistoryExecutionLogs } from './historyExecutionLogs'
const event = (folder: string): PollingEvent => ({ type: 'batch_group_start', data: { data: { run_folder: folder } } } as PollingEvent)

describe('history execution logs navigation', () => {
  beforeEach(() => { vi.clearAllMocks(); mocks.getTabEvents.mockReturnValue([]) })
  it.each(['schedule-cron--one', 'schedule-webhook--one', 'bot-slack--one', 'bot-whatsapp--one'])('opens %s alongside its matching logs', session_id => {
    openHistoryExecutionLogs({ session_id, run_folder: 'iteration-7/group-2' })
    expect(mocks.setSelectedRunFolder).toHaveBeenCalledWith('iteration-7/group-2')
    expect(mocks.openWorkspaceView).toHaveBeenCalledWith('execution-logs', `history:${session_id}`)
  })
  it('uses the latest structured execution when a bot thread runs multiple requests', () => {
    expect(historyExecutionRunFolder({ session_id: 'bot-slack--one' }, [event('iteration-3'), event('iteration-4')])).toBe('iteration-4')
  })
  it('prefers the scheduled run record over event history', () => {
    expect(historyExecutionRunFolder({ session_id: 'schedule-one', run_folder: 'iteration-3' }, [event('iteration-4')])).toBe('iteration-3')
  })
  it('clears the prior run instead of selecting unrelated logs for a chat without execution metadata', () => {
    openHistoryExecutionLogs({ session_id: 'bot-slack--one', query: 'iteration-99' })
    expect(mocks.setSelectedRunFolder).toHaveBeenCalledWith(null)
    expect(mocks.openWorkspaceView).toHaveBeenCalled()
  })
  it('leaves the workspace view alone for ordinary builder chat history', () => {
    openHistoryExecutionLogs({ session_id: 'builder-one' })
    expect(mocks.openWorkspaceView).not.toHaveBeenCalled()
  })
})
