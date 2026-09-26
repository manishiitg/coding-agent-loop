import { beforeEach, describe, expect, it, vi } from 'vitest'

const dryRunSlackConnection = vi.fn()
vi.mock('../../../services/api', () => ({ agentApi: { dryRunSlackConnection: (...args: unknown[]) => dryRunSlackConnection(...args) } }))

import { withSlackDryRun } from './slackDryRun'

const tokensOK = { success: true, message: 'Connected', checks: [{ name: 'Bot token', status: 'passed' as const, message: '' }] }

describe('withSlackDryRun', () => {
  beforeEach(() => {
    dryRunSlackConnection.mockReset()
  })

  it('adds a passing routing check when a mention reaches the crew', async () => {
    dryRunSlackConnection.mockResolvedValue({ admitted: true, mode: 'run', destination: 'crew sde (_users/u1/Chats/Work/projects/sde)' })
    const result = await withSlackDryRun('slack_sde', tokensOK, 'crew')
    expect(result.success).toBe(true)
    expect(result.checks?.at(-1)).toMatchObject({ name: 'A mention reaches this crew', status: 'passed', message: 'crew sde (_users/u1/Chats/Work/projects/sde) · answers in Run mode in channels' })
  })

  it('fails the test with the reply the user would see when routing refuses', async () => {
    dryRunSlackConnection.mockResolvedValue({ admitted: false, reason: 'handleQuery returned status 403: Slack bot route was revoked', replies: ["This bot isn't set up to start this conversation."] })
    const result = await withSlackDryRun('slack_sde', tokensOK, 'crew')
    expect(result.success).toBe(false)
    expect(result.message).toContain('would not reach this crew')
    const check = result.checks?.at(-1)
    expect(check?.status).toBe('failed')
    expect(check?.message).toContain("This bot isn't set up")
    expect(check?.message).toContain('route was revoked')
  })

  it('reports a dry run that could not run', async () => {
    dryRunSlackConnection.mockRejectedValue(new Error('network down'))
    const result = await withSlackDryRun('slack_wf', tokensOK, 'workflow')
    expect(result.success).toBe(false)
    expect(result.checks?.at(-1)).toMatchObject({ status: 'failed', message: 'network down' })
  })
})
