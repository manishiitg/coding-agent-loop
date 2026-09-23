// @vitest-environment happy-dom
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { describe, expect, it, vi } from 'vitest'
import { SparkQuillConversation, submitToParentChat } from './PlatformChat'

describe('parent page answers', () => {
  it('submits a page choice to the open parent conversation', async () => {
    const container = document.createElement('div')
    const root = createRoot(container)
    const submit = vi.fn()
    await act(async () => {
      root.render(<SparkQuillConversation events={[]} isStreaming={false} isRestoring={false} streamingText="" onSubmitQuery={submit} />)
    })

    expect(submitToParentChat('q1: 2/3')).toBe(true)
    expect(submit).toHaveBeenCalledWith('q1: 2/3')

    await act(async () => root.unmount())
    expect(submitToParentChat('q1: 2/3')).toBe(false)
  })
})
