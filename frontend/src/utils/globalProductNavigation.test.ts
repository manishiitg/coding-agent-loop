import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
const activateTab = vi.hoisted(() => vi.fn(() => true))
vi.mock('./activateTab', () => ({ activateTab }))
import type { ActiveSessionInfo } from '../services/api-types'
import { useChatStore, type ChatTab } from '../stores/useChatStore'
import { useProductSurfaceStore } from '../stores/useProductSurfaceStore'
import {
  isWorkProductSession,
  openGlobalActivitySession,
  openGlobalTab,
  workProjectIdForSession,
  workProjectIdForTab,
} from './globalProductNavigation'

const workTab = {
  tabId: 'crew-tab',
  name: 'Crew chat',
  sessionId: 'crew-session',
  metadata: {
    mode: 'multi-agent',
    agentProfileId: 'work',
    agentProfileProjectId: 'project-1',
    agentProfileConversationKey: 'project-1:crew-session',
  },
} as ChatTab

const session = (overrides: Partial<ActiveSessionInfo>): ActiveSessionInfo => ({
  session_id: 'session-1',
  observer_id: '',
  agent_mode: 'multi-agent',
  status: 'running',
  created_at: '',
  last_activity: '',
  ...overrides,
})

beforeEach(() => {
  useProductSurfaceStore.persist.setOptions({
    storage: { getItem: () => null, setItem: () => {}, removeItem: () => {} },
  })
})

afterEach(() => {
  useProductSurfaceStore.setState({
    productSurface: 'agentworks',
    selectedWorkProjectId: null,
    pendingWorkView: null,
  })
  useChatStore.setState({ chatTabs: {}, activeTabId: null })
  activateTab.mockClear()
})

describe('global AgentWorks and Crew navigation', () => {
  it('recognizes public and physical Crew sessions without admitting other products', () => {
    expect(isWorkProductSession(session({ session_id: 'work:project:project-1' }))).toBe(true)
    expect(isWorkProductSession(session({ workspace_path: '_users/user-1/Chats/Work/projects/demo' }))).toBe(true)
    expect(isWorkProductSession(session({ workspace_path: 'Chats/Video Studio/projects/demo' }))).toBe(false)
  })

  it('resolves stable Crew project identity from tabs and sessions', () => {
    expect(workProjectIdForTab(workTab)).toBe('project-1')
    expect(workProjectIdForSession(session({ session_id: 'work:project:project-2' }))).toBe('project-2')
    expect(workProjectIdForSession(session({ preset_query_id: 'project-3' }))).toBe('project-3')
  })

  it('switches surfaces and selects the Crew project before activating its tab', () => {
    useChatStore.setState({ chatTabs: { [workTab.tabId]: workTab } })

    expect(openGlobalTab(workTab.tabId)).toBe(true)
    expect(useProductSurfaceStore.getState().productSurface).toBe('work')
    expect(useProductSurfaceStore.getState().selectedWorkProjectId).toBe('project-1')
    expect(activateTab).toHaveBeenCalledWith(workTab.tabId)
  })

  it('opens a tabless Crew schedule in the project schedules view', async () => {
    await openGlobalActivitySession(session({
      session_id: 'work:project:project-4',
      triggered_by: 'schedule',
    }))

    expect(useProductSurfaceStore.getState().productSurface).toBe('work')
    expect(useProductSurfaceStore.getState().selectedWorkProjectId).toBe('project-4')
    expect(useProductSurfaceStore.getState().pendingWorkView).toBe('schedules')
  })
})
