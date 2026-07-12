import { describe, expect, it } from 'vitest'

import { PromiseLane } from './promiseLane'

describe('PromiseLane', () => {
  it('runs rapid submissions for one tab exactly once and in order', async () => {
    const lane = new PromiseLane()
    const executed: number[] = []
    let releaseFirst!: () => void
    const firstBlocked = new Promise<void>(resolve => { releaseFirst = resolve })

    const submissions = Array.from({ length: 20 }, (_, index) => lane.enqueue('tab-a', async () => {
      if (index === 0) await firstBlocked
      executed.push(index)
    }))

    await Promise.resolve()
    expect(executed).toEqual([])
    releaseFirst()
    await Promise.all(submissions)
    expect(executed).toEqual(Array.from({ length: 20 }, (_, index) => index))
  })

  it('continues the lane after a failed submission', async () => {
    const lane = new PromiseLane()
    const executed: string[] = []
    const failed = lane.enqueue('tab-a', async () => {
      executed.push('failed')
      throw new Error('expected')
    })
    const next = lane.enqueue('tab-a', async () => {
      executed.push('next')
    })

    await expect(failed).rejects.toThrow('expected')
    await next
    expect(executed).toEqual(['failed', 'next'])
  })

  it('does not serialize different tabs', async () => {
    const lane = new PromiseLane()
    let releaseA!: () => void
    const blockedA = new Promise<void>(resolve => { releaseA = resolve })
    let tabBCompleted = false

    const tabA = lane.enqueue('tab-a', () => blockedA)
    await lane.enqueue('tab-b', async () => { tabBCompleted = true })
    expect(tabBCompleted).toBe(true)
    releaseA()
    await tabA
  })
})

