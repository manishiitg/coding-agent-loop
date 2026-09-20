import { describe, expect, it } from 'vitest'
import { applyLiveInputConfirmation, readLiveInputConfirmation, resolveLiveInputConfirmations, splitLiveInputConfirmations, stampLiveInputIdentity, withLiveInputReceipt } from './liveInputReceipt'

describe('withLiveInputReceipt', () => {
  it('links the optimistic row to the server message identity', () => {
    const event = withLiveInputReceipt({
      id: 'user-message-local',
      type: 'user_message',
      data: { data: { content: 'hello' } },
    }, 'sent_to_cli', 'claude-code', 'steer-server-id')

    expect(event.data).toMatchObject({ data: { metadata: {
      source: 'coding_agent_live_input',
      delivery_status: 'sent_to_cli',
      provider: 'claude-code',
      message_id: 'steer-server-id',
    } } })
  })
})

describe('delivery confirmation', () => {
  it('marks accepted receipts as fast without downgrading a verdict', () => {
    const fast = withLiveInputReceipt({
      id: 'user-message-local',
      type: 'user_message',
      data: { data: { content: 'hello' } },
    }, 'sent_to_cli', 'codex-cli', 'steer-server-id')
    expect(fast.data).toMatchObject({ data: { metadata: { confirmation: 'fast' } } })

    const sending = withLiveInputReceipt({
      id: 'user-message-local',
      type: 'user_message',
      data: { data: { content: 'hello' } },
    }, 'sending')
    expect((sending.data as { data: { metadata: Record<string, unknown> } }).data.metadata.confirmation).toBeUndefined()

    const alreadyConfirmed = withLiveInputReceipt({
      id: 'user-message-local',
      type: 'user_message',
      data: { data: { content: 'hello', metadata: { confirmation: 'confirmed' } } },
    }, 'sent_to_cli', 'codex-cli', 'steer-server-id')
    expect(alreadyConfirmed.data).toMatchObject({ data: { metadata: { confirmation: 'confirmed' } } })
  })

  it('upgrades only the row carrying the confirmed message id', () => {
    const rows = [
      { id: 'user-message-1', type: 'user_message', data: { data: { content: 'first', metadata: { source: 'coding_agent_live_input', message_id: 'steer-1', confirmation: 'fast' } } } },
      { id: 'user-message-2', type: 'user_message', data: { data: { content: 'second', metadata: { source: 'coding_agent_live_input', message_id: 'steer-2', confirmation: 'fast' } } } },
    ]
    const upgraded = applyLiveInputConfirmation(rows, { messageId: 'steer-2', outcome: 'confirmed', proofSource: '/tmp/rollout.jsonl', latencyMs: 1200, provider: 'codex-cli' })
    expect(upgraded[0].data).toMatchObject({ data: { metadata: { confirmation: 'fast' } } })
    expect(upgraded[1].data).toMatchObject({ data: { metadata: {
      confirmation: 'confirmed', proof_source: '/tmp/rollout.jsonl', latency_ms: 1200, provider: 'codex-cli', message_id: 'steer-2',
    } } })
    // Re-applying is safe: a replayed receipt must not duplicate or clear state.
    const replayed = applyLiveInputConfirmation(upgraded, { messageId: 'steer-2', outcome: 'confirmed' })
    expect(replayed[1].data).toMatchObject({ data: { metadata: { confirmation: 'confirmed' } } })
  })

  it('never un-confirms a row with a stale duplicate', () => {
    const rows = [
      { id: 'user-message-1', type: 'user_message', data: { data: { content: 'first', metadata: { message_id: 'steer-1', confirmation: 'confirmed' } } } },
    ]
    const kept = applyLiveInputConfirmation(rows, { messageId: 'steer-1', outcome: 'failed' })
    expect(kept[0].data).toMatchObject({ data: { metadata: { confirmation: 'confirmed' } } })
  })

  it('stamps echo identity without flipping the bubble source', () => {
    const stamped = stampLiveInputIdentity({
      id: 'user-message-9', type: 'user_message', data: { data: { content: 'query steered live' } },
    }, 'steer-server-9', 'sent_to_cli', 'codex-cli')
    expect(stamped.data).toMatchObject({ data: { metadata: {
      message_id: 'steer-server-9', delivery_status: 'sent_to_cli', provider: 'codex-cli', confirmation: 'fast',
    } } })
    expect((stamped.data as { data: { metadata: Record<string, unknown> } }).data.metadata.source).toBeUndefined()

    const row = {
      id: 'user-message-9', type: 'user_message', data: { data: { content: 'query steered live', metadata: { message_id: 'steer-server-9' } } },
    }
    const alreadyStamped = stampLiveInputIdentity(row, 'steer-server-other', 'sent_to_cli')
    expect(alreadyStamped).toBe(row)
    expect(alreadyStamped.data).toMatchObject({ data: { metadata: { message_id: 'steer-server-9' } } })
  })

  it('parses the live_input_confirmed wire event and rejects impostors', () => {
    const parsed = readLiveInputConfirmation({
      id: 'steer-1:confirmed', type: 'live_input_confirmed',
      data: { data: { message_id: 'steer-1', outcome: 'confirmed', proof_source: '/tmp/r.jsonl', latency_ms: 300, provider: 'codex-cli' } },
    })
    expect(parsed).toEqual({ messageId: 'steer-1', outcome: 'confirmed', proofSource: '/tmp/r.jsonl', latencyMs: 300, provider: 'codex-cli' })

    const viaMetadata = readLiveInputConfirmation({
      id: 'steer-2:confirmed', type: 'live_input_confirmed',
      data: { data: { metadata: { message_id: 'steer-2', confirmation: 'accepted_but_unflushed' } } },
    })
    expect(viaMetadata).toMatchObject({ messageId: 'steer-2', outcome: 'accepted_but_unflushed' })

    expect(readLiveInputConfirmation({ id: 'x', type: 'user_message', data: {} })).toBeNull()
    expect(readLiveInputConfirmation({ id: 'x', type: 'live_input_confirmed', data: { data: { outcome: 'confirmed' } } })).toBeNull()
    expect(readLiveInputConfirmation({ id: 'x', type: 'live_input_confirmed', data: { data: { message_id: 's', outcome: 'maybe' } } })).toBeNull()
  })

  it('splits receipts from timeline rows so restore paths consume them', () => {
    const row = { id: 'user-message-1', type: 'user_message', data: { data: { content: 'steer', metadata: { source: 'coding_agent_live_input', message_id: 'steer-1', confirmation: 'fast' } } } }
    const receipt = { id: 'steer-1:confirmed', type: 'live_input_confirmed', data: { data: { message_id: 'steer-1', outcome: 'confirmed', proof_source: '/tmp/r.jsonl', latency_ms: 300, provider: 'codex-cli' } } }
    const { timelineEvents, confirmations } = splitLiveInputConfirmations([row, receipt])
    expect(timelineEvents).toEqual([row])
    expect(confirmations).toEqual([{ messageId: 'steer-1', outcome: 'confirmed', proofSource: '/tmp/r.jsonl', latencyMs: 300, provider: 'codex-cli' }])

    const resolved = resolveLiveInputConfirmations([row, receipt])
    expect(resolved).toHaveLength(1)
    expect(resolved[0].data).toMatchObject({ data: { metadata: { confirmation: 'confirmed', proof_source: '/tmp/r.jsonl' } } })
  })
})
