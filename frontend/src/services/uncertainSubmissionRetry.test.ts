import { describe, expect, it, vi } from 'vitest'
import {
  isUncertainChatSubmission,
  retryUncertainChatSubmission,
  uncertainSubmissionRetryDelayMs,
} from './uncertainSubmissionRetry'

function uncertainError(config: Record<string, unknown>, shape: 'error' | 'delivery_status' = 'error') {
  return {
    isAxiosError: true,
    config,
    response: {
      status: 409,
      data: shape === 'error'
        ? { error: 'delivery_uncertain', submission_id: 'sub-1', message: 'Submission sub-1 is durable but delivery is uncertain.' }
        : { delivery_status: 'delivery_uncertain', submission_id: 'sub-1' },
    },
  }
}

describe('isUncertainChatSubmission', () => {
  it('matches the journal 409 in either marker field', () => {
    expect(isUncertainChatSubmission(uncertainError({}))).toBe(true)
    expect(isUncertainChatSubmission(uncertainError({}, 'delivery_status'))).toBe(true)
  })

  it('ignores other conflicts and failures', () => {
    expect(isUncertainChatSubmission({
      response: { status: 409, data: { error: 'delivery_not_sent' } },
    })).toBe(false)
    expect(isUncertainChatSubmission({ response: { status: 500, data: {} } })).toBe(false)
    expect(isUncertainChatSubmission(new Error('boom'))).toBe(false)
    expect(isUncertainChatSubmission(undefined)).toBe(false)
  })
})

describe('uncertainSubmissionRetryDelayMs', () => {
  it('backs off and clamps to the last bucket', () => {
    expect(uncertainSubmissionRetryDelayMs(0)).toBe(2000)
    expect(uncertainSubmissionRetryDelayMs(1)).toBe(5000)
    expect(uncertainSubmissionRetryDelayMs(2)).toBe(10000)
    expect(uncertainSubmissionRetryDelayMs(99)).toBe(10000)
  })
})

describe('retryUncertainChatSubmission', () => {
  it('re-posts the identical config and adopts its result', async () => {
    const config = { headers: { 'Idempotency-Key': 'sub-1' }, data: { query: 'hi' } }
    const instance = vi.fn(async () => ({ status: 200, data: { status: 'started' } }))
    const sleep = vi.fn(async () => {})

    const result = await retryUncertainChatSubmission(instance, uncertainError(config), sleep)

    expect(result).toEqual({ status: 200, data: { status: 'started' } })
    expect(instance).toHaveBeenCalledTimes(1)
    expect(instance).toHaveBeenCalledWith(config)
    expect(config).toMatchObject({ headers: { 'Idempotency-Key': 'sub-1' } })
    expect(sleep).toHaveBeenCalledWith(2000)
  })

  it('gives up after bounded retries and keeps the original error', async () => {
    const config: Record<string, unknown> = { __uncertainSubmissionRetries: 3 }
    const instance = vi.fn(async () => ({}))
    const sleep = vi.fn(async () => {})
    const error = uncertainError(config)

    await expect(retryUncertainChatSubmission(instance, error, sleep)).rejects.toBe(error)
    expect(instance).not.toHaveBeenCalled()
    expect(sleep).not.toHaveBeenCalled()
  })

  it('rejects non-uncertain errors without retrying', async () => {
    const instance = vi.fn(async () => ({}))
    const sleep = vi.fn(async () => {})
    const error = { response: { status: 500, data: {} } }

    await expect(retryUncertainChatSubmission(instance, error, sleep)).rejects.toBe(error)
    expect(instance).not.toHaveBeenCalled()
  })
})
