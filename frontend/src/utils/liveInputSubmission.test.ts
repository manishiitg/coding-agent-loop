import { describe, expect, it, vi } from 'vitest'

import { createLiveInputSubmissionCoordinator } from './liveInputSubmission'

describe('createLiveInputSubmissionCoordinator', () => {
  it('executes a rapid duplicate live message exactly once', async () => {
    const submitLiveInput = createLiveInputSubmissionCoordinator()
    let resolve!: (value: boolean) => void
    const submit = vi.fn(() => new Promise<boolean>(done => { resolve = done }))

    const first = submitLiveInput('session-a', 'continue with this', submit)
    const duplicate = submitLiveInput('session-a', 'continue with this', submit)

    expect(duplicate).toBe(first)
    expect(submit).toHaveBeenCalledTimes(1)
    resolve(true)
    await expect(Promise.all([first, duplicate])).resolves.toEqual([true, true])
  })

  it('does not suppress different messages or a later intentional repeat', async () => {
    const submitLiveInput = createLiveInputSubmissionCoordinator()
    const submit = vi.fn(async (value: string) => value)

    await Promise.all([
      submitLiveInput('session-a', 'first', () => submit('first')),
      submitLiveInput('session-a', 'second', () => submit('second')),
    ])
    await submitLiveInput('session-a', 'first', () => submit('first-again'))

    expect(submit).toHaveBeenCalledTimes(3)
  })
})
