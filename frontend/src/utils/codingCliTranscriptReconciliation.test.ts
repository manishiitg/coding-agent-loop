import { describe, expect, it } from 'vitest'
import type { PollingEvent } from '../services/api-types'
import { codingCliCompletionNeedsTranscriptReconciliation } from './codingCliTranscriptReconciliation'

function completion(provider: string, source = 'mcpagent_session'): PollingEvent {
  return {
    id: `${provider}-${source}`,
    type: 'unified_completion',
    data: { data: { metadata: { provider, source } } },
  } as PollingEvent
}

describe('codingCliCompletionNeedsTranscriptReconciliation', () => {
  it.each(['claude-code', 'codex-cli', 'cursor-cli', 'pi-cli', 'muse-cli'])(
    'reconciles retained %s completions',
    (provider) => expect(codingCliCompletionNeedsTranscriptReconciliation(completion(provider))).toBe(true),
  )

  it('accepts the server sidecar completion source', () => {
    expect(codingCliCompletionNeedsTranscriptReconciliation(completion('claude-code', 'coding_agent_sidecar'))).toBe(true)
  })

  it('accepts the canonical retained-terminal completion without a legacy source marker', () => {
    const event = {
      id: 'retained-terminal-completion',
      type: 'unified_completion',
      data: { data: { metadata: { provider: 'claude-code', coding_agent_terminal_format: true } } },
    } as PollingEvent

    expect(codingCliCompletionNeedsTranscriptReconciliation(event)).toBe(true)
  })

  it('does not refresh ordinary API-model or unrelated events', () => {
    expect(codingCliCompletionNeedsTranscriptReconciliation(completion('anthropic'))).toBe(false)
    expect(codingCliCompletionNeedsTranscriptReconciliation({ type: 'streaming_end' } as PollingEvent)).toBe(false)
  })
})
