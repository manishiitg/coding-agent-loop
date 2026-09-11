import { describe, expect, it, vi } from 'vitest'
import { ReportChatRequestController } from './reportChatRequest'

const queued = { tabId: 'chat-one', reused: true, queuedBehindRunningTurn: true }

describe('report to chat requests', () => {
  it('dispatches directly through the existing workflow chat lane', async () => {
    const dispatch = vi.fn().mockResolvedValue(queued)
    const controller = new ReportChatRequestController('Workflow/one', dispatch)
    await expect(controller.request(' Run visual QA ')).resolves.toEqual({ status: 'queued', ...queued })
    expect(dispatch).toHaveBeenCalledExactlyOnceWith({
      workspacePath: 'Workflow/one',
      message: 'From the report for Workflow/one:\n\nRun visual QA',
    })
  })

  it('deduplicates double clicks and successful requests with the same ID', async () => {
    let finish!: (value: typeof queued) => void
    const dispatch = vi.fn().mockImplementation(() => new Promise(resolve => { finish = resolve }))
    const controller = new ReportChatRequestController('Workflow/one', dispatch)
    const result = controller.request('Apply item 42', { requestId: '42:v1' })
    expect(controller.request('Apply item 42', { requestId: '42:v1' })).toBe(result)
    await expect(controller.request('Apply item 99')).rejects.toThrow('Wait for the current report message')
    expect(dispatch).toHaveBeenCalledTimes(1)
    finish(queued)
    await result
    await expect(controller.request('Apply item 42', { requestId: '42:v1' })).resolves.toEqual(await result)
    expect(dispatch).toHaveBeenCalledTimes(1)
    await expect(controller.request('Apply something else', { requestId: '42:v1' })).rejects.toThrow('different message')
  })

  it('rejects failed enqueues and permits retrying the same action ID', async () => {
    const dispatch = vi.fn().mockRejectedValueOnce(new Error('Automation unavailable')).mockResolvedValue(queued)
    const controller = new ReportChatRequestController('Workflow/one', dispatch)
    await expect(controller.request('Apply item 42', { requestId: '42:v1' })).rejects.toThrow('Automation unavailable')
    await expect(controller.request('Apply item 42', { requestId: '42:v1' })).resolves.toMatchObject({ status: 'queued' })
    expect(dispatch).toHaveBeenCalledTimes(2)
  })

  it('rejects stale callbacks after the report closes', async () => {
    const dispatch = vi.fn()
    const controller = new ReportChatRequestController('Workflow/one', dispatch)
    controller.dispose()
    await expect(controller.request('Apply item 42')).rejects.toThrow('no longer open')
    expect(dispatch).not.toHaveBeenCalled()
  })

  it('settles an accepted enqueue even when navigation closes the report', async () => {
    const controller = new ReportChatRequestController('Workflow/one', vi.fn().mockResolvedValue(queued))
    const result = controller.request('Apply item 42')
    controller.dispose()
    await expect(result).resolves.toMatchObject({ status: 'queued' })
  })

  it('validates message and options without dispatching', async () => {
    const dispatch = vi.fn()
    const controller = new ReportChatRequestController('Workflow/one', dispatch)
    for (const value of ['', '  ', 'x'.repeat(12001), 42, null]) {
      await expect(controller.request(value as string)).rejects.toThrow('message between')
    }
    await expect(controller.request('Apply', { requestId: '' })).rejects.toThrow('requestId')
    await expect(controller.request('Apply', [] as never)).rejects.toThrow('options must be an object')
    expect(dispatch).not.toHaveBeenCalled()
  })

  it('reports enqueue failures after navigation instead of claiming cancellation', async () => {
    const controller = new ReportChatRequestController('Workflow/one', vi.fn().mockRejectedValue(new Error('Queue unavailable')))
    const result = controller.request('Apply item 42')
    controller.dispose()
    await expect(result).rejects.toThrow('Queue unavailable')
  })
})
