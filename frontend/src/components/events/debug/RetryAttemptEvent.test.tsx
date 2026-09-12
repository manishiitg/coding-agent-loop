import React from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { RetryAttemptEventDisplay } from './RetryAttemptEvent'

describe('RetryAttemptEventDisplay', () => {
  it('renders a same-model retry as a scheduled retry, not a failed fallback', () => {
    const markup = renderToStaticMarkup(
      <RetryAttemptEventDisplay
        event={{
          attempt_index: 1,
          total_attempts: 5,
          model_id: 'muse-spark-1.3-contributor',
          provider: 'muse-cli',
          phase: 'retry',
          success: false,
          duration: '10s',
          error: 'connection_error - retrying original model'
        }}
      />
    )

    expect(markup).toContain('Retrying Model')
    expect(markup).toContain('Attempt 2 of 5')
    expect(markup).toContain('Waiting 10s')
    expect(markup).toContain('Reason:')
    expect(markup).not.toContain('Failed Fallback Attempt')
    expect(markup).not.toContain('retrying original model')
  })

  it('shows a retry with unknown attempt limits', () => {
    const markup = renderToStaticMarkup(<RetryAttemptEventDisplay event={{ model_id: 'selected-model', error: 'connection_error' }} />)
    expect(markup).toContain('selected-model')
    expect(markup).toContain('Attempt 1')
    expect(markup).toContain('Reason:')
  })
})
