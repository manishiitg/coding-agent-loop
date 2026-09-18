import { describe, expect, it } from 'vitest'
import { withLiveInputReceipt } from './liveInputReceipt'

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
