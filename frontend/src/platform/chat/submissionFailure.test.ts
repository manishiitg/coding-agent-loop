import { describe, expect, it } from 'vitest'
import { normalizeProductChatFailure } from './productChatFailure'
import { submissionFailure } from './submissionFailure'

describe('submissionFailure', () => {
  it('preserves a diagnostic 409 response for the Technical details card', () => {
    const failure = submissionFailure({
      message: 'Request failed with status code 409',
      response: {
        status: 409,
        data: {
          error: 'live_input_unavailable',
          message: 'Live input unavailable: context deadline exceeded',
          provider: 'pi-cli',
          technical_details: 'Request failed with status code 409\ntmux: mlp-pi-cli-test\nTerminal snapshot:\nπ • ✅ api-bridge_execute_shell_command',
        },
      },
    })

    expect(failure).toEqual({
      message: 'Live input unavailable: context deadline exceeded',
      code: 'live_input_unavailable',
      provider: 'pi-cli',
      technicalDetails: 'Request failed with status code 409\ntmux: mlp-pi-cli-test\nTerminal snapshot:\nπ • ✅ api-bridge_execute_shell_command',
    })
    const productFailure = normalizeProductChatFailure(failure.message, {
      code: failure.code,
      provider: failure.provider,
      technicalDetails: failure.technicalDetails,
    })
    expect(productFailure.title).toBe('The response could not be completed')
    expect(productFailure.technicalDetails).toContain('tmux: mlp-pi-cli-test')
  })

  it('keeps plain transport errors useful', () => {
    const failure = submissionFailure(new Error('Network Error'))
    expect(failure.message).toBe('Network Error')
    expect(failure.technicalDetails).toBe('Network Error')
  })
})
