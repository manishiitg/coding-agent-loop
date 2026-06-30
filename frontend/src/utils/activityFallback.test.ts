import { describe, expect, it } from 'vitest'
import { isLocalActivityFallbackTab } from './activityFallback'
import type { ChatTab } from '../stores/useChatStore'

const tab = (overrides: Partial<ChatTab>): ChatTab => ({
  tabId: 'tab-1',
  name: 'hetznerssh',
  sessionId: 'wfrun_1',
  isStreaming: false,
  isCompleted: false,
  hasRunningBgAgents: false,
  isSyntheticTurn: false,
  canSteer: false,
  hideToolCalls: true,
  viewMode: 'terminal',
  config: {} as ChatTab['config'],
  createdAt: Date.now(),
  lastViewedEventCount: 0,
  lastViewedEventCounts: { micro: 0 },
  metadata: { mode: 'workflow' },
  ...overrides,
})

describe('local activity fallback tabs', () => {
  it('hides stale completed tabs even if hasRunningBgAgents is stuck true', () => {
    expect(isLocalActivityFallbackTab(tab({
      isCompleted: true,
      hasRunningBgAgents: true,
      isStreaming: false,
    }))).toBe(false)
  })

  it('hides non-streaming local tabs with only stale background-agent state', () => {
    expect(isLocalActivityFallbackTab(tab({
      hasRunningBgAgents: true,
      isStreaming: false,
      isSyntheticTurn: false,
      canSteer: false,
    }))).toBe(false)
  })

  it('shows genuinely live local fallback tabs', () => {
    expect(isLocalActivityFallbackTab(tab({
      hasRunningBgAgents: true,
      isStreaming: true,
    }))).toBe(true)
    expect(isLocalActivityFallbackTab(tab({
      hasRunningBgAgents: true,
      isSyntheticTurn: true,
    }))).toBe(true)
    expect(isLocalActivityFallbackTab(tab({
      hasRunningBgAgents: true,
      canSteer: true,
    }))).toBe(true)
  })
})
