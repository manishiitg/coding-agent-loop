import { describe, expect, it } from 'vitest'
import { buildEmptyAskMessage, buildErrorAskMessage, buildRunAskMessage, buildStepAskMessage } from './logsAskAI'

describe('buildRunAskMessage', () => {
  it('names the failed steps and first error', () => {
    const message = buildRunAskMessage({
      runFolder: 'iteration-84-sched/default',
      folderLabel: 'Run 84 · sched · default',
      stepCount: 5,
      failedSteps: [{ title: 'Classify intent', error: 'rate limit exceeded' }, { title: 'Draft reply' }],
    })
    expect(message).toContain('Run 84 · sched · default (iteration-84-sched/default)')
    expect(message).toContain('2 of 5 steps failed: "Classify intent", "Draft reply"')
    expect(message).toContain('First error: rate limit exceeded.')
  })

  it('asks for a summary when nothing failed', () => {
    const message = buildRunAskMessage({ runFolder: 'iteration-1/default', folderLabel: 'Run 1 · default', stepCount: 3, failedSteps: [] })
    expect(message).toContain('Summarize run Run 1 · default')
    expect(message).toContain('3 steps')
  })

  it('asks why nothing was recorded when there are no steps', () => {
    const message = buildRunAskMessage({ runFolder: 'iteration-2/default', folderLabel: 'Run 2 · default', stepCount: 0, failedSteps: [] })
    expect(message).toContain('has no step logs')
  })
})

describe('buildStepAskMessage', () => {
  it('packs status, cost, and evidence into one ask', () => {
    const message = buildStepAskMessage({
      runFolder: 'iteration-1/default',
      folderLabel: 'Run 1 · default',
      stepTitle: 'Classify intent',
      status: 'failed',
      duration: '2m',
      tokens: '12.4k tokens',
      error: 'rate limit exceeded',
    })
    expect(message).toContain('step "Classify intent" failed, took 2m, used 12.4k tokens')
    expect(message).toContain('Error: rate limit exceeded.')
  })

  it('falls back to the output preview without an error', () => {
    const message = buildStepAskMessage({
      runFolder: 'iteration-1/default',
      folderLabel: 'Run 1 · default',
      stepTitle: 'Draft reply',
      status: 'completed',
      preview: 'Drafted three variants.',
    })
    expect(message).toContain('step "Draft reply" completed')
    expect(message).toContain('Latest output: Drafted three variants.')
  })
})

describe('buildErrorAskMessage', () => {
  it('quotes the error and asks for cause and fix', () => {
    const message = buildErrorAskMessage({
      runFolder: 'iteration-1/default',
      folderLabel: 'Run 1 · default',
      stepTitle: 'Classify intent',
      error: 'boom',
    })
    expect(message).toContain('step "Classify intent" hit this error: boom.')
    expect(message).toContain('how to fix it')
  })

  it('truncates long errors', () => {
    const message = buildErrorAskMessage({
      runFolder: 'iteration-1/default',
      folderLabel: 'Run 1 · default',
      stepTitle: 'Classify intent',
      error: `x${'y'.repeat(600)}`,
    })
    expect(message.length).toBeLessThan(700)
    expect(message).toContain('…')
  })
})

describe('buildEmptyAskMessage', () => {
  it('asks how to produce the first run', () => {
    expect(buildEmptyAskMessage()).toContain('Help me run this workflow')
  })
})
