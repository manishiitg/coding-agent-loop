import { describe, expect, it } from 'vitest'
import { completedSparkQuillTurns } from './turnRefresh'

describe('completedSparkQuillTurns', () => {
  it('refreshes after a parent or child chat turn finishes', () => {
    const previous = {
      parent: { isStreaming: true, metadata: { agentProfileId: 'sparkquill' } },
      child: { isStreaming: true, metadata: { agentProfileId: 'sparkquill-child' } },
    }
    const current = {
      parent: { ...previous.parent, isStreaming: false },
      child: { ...previous.child, isStreaming: false },
    }
    expect(completedSparkQuillTurns(current, previous)).toEqual({ parent: true, child: true })
  })

  it('ignores unrelated tabs and state changes that are not turn completions', () => {
    const previous = {
      other: { isStreaming: true, metadata: { agentProfileId: 'work' } },
      parent: { isStreaming: false, metadata: { agentProfileId: 'sparkquill' } },
    }
    const current = {
      other: { ...previous.other, isStreaming: false },
      parent: { ...previous.parent, isStreaming: true },
    }
    expect(completedSparkQuillTurns(current, previous)).toEqual({ parent: false, child: false })
  })
})
