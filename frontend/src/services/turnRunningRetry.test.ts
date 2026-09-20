import { describe, expect, it, vi } from 'vitest'
import {
  isTurnRunningConflict,
  retryTurnRunningSubmission,
  turnRunningRetryDelayMs,
} from './turnRunningRetry'

function turnRunningError(config: Record<string, unknown>, shape: 'error' | 'status' = 'error') {
  return {
    isAxiosError: true,
    config,
    response: {
      status: 409,
      data: shape === 'error'
        ? { error: 'turn_running', status: 'turn_running', message: 'The previous turn is still running.' }
        : { status: 'turn_running' },
    },
  }
}

describe('isTurnRunningConflict', () => {
  it('matches the lane 409 in either marker field', () => {
    expect(isTurnRunningConflict(turnRunningError({}))).toBe(true)
    expect(isTurnRunningConflict(turnRunningError({}, 'status'))).toBe(true)
  })

  it('ignores other conflicts and failures', () => {
    expect(isTurnRunningConflict({
      response: { status: 409, data: { error: 'delivery_uncertain' } },
    })).toBe(false)
    expect(isTurnRunningConflict({
      response: { status: 409, data: { error: 'workflow_busy' } },
    })).toBe(false)
    expect(isTurnRunningConflict({ response: { status: 500, data: {} } })).toBe(false)
    expect(isTurnRunningConflict(new Error('boom'))).toBe(false)
    expect(isTurnRunningConflict(undefined)).toBe(false)
  })
})

describe('turnRunningRetryDelayMs', () => {
  it('backs off and clamps to the last bucket', () => {
    expect(turnRunningRetryDelayMs(0)).toBe(2000)
    expect(turnRunningRetryDelayMs(1)).toBe(5000)
    expect(turnRunningRetryDelayMs(5)).toBe(30000)
    expect(turnRunningRetryDelayMs(99)).toBe(30000)
  })
})

describe('retryTurnRunningSubmission', () => {
  it('re-posts with a fresh idempotency key and adopts its result', async () => {
    const config = { headers: { 'Idempotency-Key': 'sub-1' }, data: { query: 'hi' } }
    const instance = vi.fn(async () => ({ status: 200, data: { status: 'started' } }))
    const sleep = vi.fn(async () => {})

    const result = await retryTurnRunningSubmission(instance, turnRunningError(config), sleep)

    expect(result).toEqual({ status: 200, data: { status: 'started' } })
    expect(instance).toHaveBeenCalledTimes(1)
    expect(instance).toHaveBeenCalledWith(config)
    expect(config.headers['Idempotency-Key']).not.toBe('sub-1')
    expect(typeof config.headers['Idempotency-Key']).toBe('string')
    expect(sleep).toHaveBeenCalledWith(2000)
  })

  it('rotates AxiosHeaders instances via set', async () => {
    const set = vi.fn()
    const config = { headers: { set } }
    const instance = vi.fn(async () => ({}))
    const sleep = vi.fn(async () => {})

    await retryTurnRunningSubmission(instance, turnRunningError(config), sleep)

    expect(set).toHaveBeenCalledTimes(1)
    expect(set.mock.calls[0][0]).toBe('Idempotency-Key')
    expect(typeof set.mock.calls[0][1]).toBe('string')
  })

  it('gives up after bounded retries and keeps the original error', async () => {
    const config: Record<string, unknown> = { __turnRunningRetries: 10 }
    const instance = vi.fn(async () => ({}))
    const sleep = vi.fn(async () => {})
    const error = turnRunningError(config)

    await expect(retryTurnRunningSubmission(instance, error, sleep)).rejects.toBe(error)
    expect(instance).not.toHaveBeenCalled()
    expect(sleep).not.toHaveBeenCalled()
  })

  it('rejects non-turn_running errors without retrying', async () => {
    const instance = vi.fn(async () => ({}))
    const sleep = vi.fn(async () => {})
    const error = { response: { status: 500, data: {} } }

    await expect(retryTurnRunningSubmission(instance, error, sleep)).rejects.toBe(error)
    expect(instance).not.toHaveBeenCalled()
  })
})
