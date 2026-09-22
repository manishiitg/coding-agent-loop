import { describe, expect, it } from 'vitest'
import {
  shouldKeepChatSessionSubscribed,
  shouldKeepWorkflowSessionSubscribed,
} from './workflowSessionSubscription'

describe('shouldKeepWorkflowSessionSubscribed', () => {
  it('keeps listening after the foreground turn settles while the backend session is active', () => {
    expect(shouldKeepWorkflowSessionSubscribed({
      isStreaming: false,
      hasRunningBackgroundAgents: false,
      isBackendActive: true,
    })).toBe(true)
  })

  it('keeps listening for foreground and background activity', () => {
    expect(shouldKeepWorkflowSessionSubscribed({
      isStreaming: true,
      hasRunningBackgroundAgents: false,
      isBackendActive: false,
    })).toBe(true)
    expect(shouldKeepWorkflowSessionSubscribed({
      isStreaming: false,
      hasRunningBackgroundAgents: true,
      isBackendActive: false,
    })).toBe(true)
  })

  it('allows a genuinely idle workflow session to disconnect', () => {
    expect(shouldKeepWorkflowSessionSubscribed({
      isStreaming: false,
      hasRunningBackgroundAgents: false,
      isBackendActive: false,
    })).toBe(false)
  })
})

describe('shouldKeepChatSessionSubscribed', () => {
  it('keeps the visible chat connected even after its turn completes', () => {
    expect(shouldKeepChatSessionSubscribed({
      isVisible: true,
      isStreaming: false,
      hasRunningBackgroundAgents: false,
      isBackendActive: false,
    })).toBe(true)
  })

  it('keeps a hidden chat connected only while it has real activity', () => {
    expect(shouldKeepChatSessionSubscribed({
      isVisible: false,
      isStreaming: true,
      hasRunningBackgroundAgents: false,
      isBackendActive: false,
    })).toBe(true)
    expect(shouldKeepChatSessionSubscribed({
      isVisible: false,
      isStreaming: false,
      hasRunningBackgroundAgents: true,
      isBackendActive: false,
    })).toBe(true)
    expect(shouldKeepChatSessionSubscribed({
      isVisible: false,
      isStreaming: false,
      hasRunningBackgroundAgents: false,
      isBackendActive: true,
    })).toBe(true)
  })

  it('disconnects a hidden completed chat', () => {
    expect(shouldKeepChatSessionSubscribed({
      isVisible: false,
      isStreaming: false,
      hasRunningBackgroundAgents: false,
      isBackendActive: false,
    })).toBe(false)
  })
})
