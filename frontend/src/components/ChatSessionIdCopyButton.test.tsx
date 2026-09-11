// @vitest-environment happy-dom
import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, describe, expect, it, vi } from 'vitest'

vi.mock('../utils/textUtils', () => ({ copyToClipboard: vi.fn() }))

import { copyToClipboard } from '../utils/textUtils'
import { ChatSessionIdCopyButton } from './ChatSessionIdCopyButton'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })

afterEach(() => vi.resetAllMocks())

describe('ChatSessionIdCopyButton', () => {
  it('copies the complete durable chat ID and confirms success', async () => {
    vi.mocked(copyToClipboard).mockResolvedValue(true)
    const host = document.createElement('div')
    document.body.append(host)
    const root = createRoot(host)

    try {
      await act(async () => root.render(<ChatSessionIdCopyButton sessionId="chat-full-unique-id-123456789" />))
      const button = host.querySelector('button')!
      expect(button.textContent).toBe('Copy ID')

      await act(async () => button.click())

      expect(copyToClipboard).toHaveBeenCalledWith('chat-full-unique-id-123456789')
      expect(button.textContent).toBe('Copied')
      expect(button.getAttribute('aria-label')).toContain('chat-full-unique-id-123456789')
    } finally {
      await act(async () => root.unmount())
      host.remove()
    }
  })

  it('shows a visible failure state when clipboard access fails', async () => {
    vi.mocked(copyToClipboard).mockResolvedValue(false)
    const host = document.createElement('div')
    const root = createRoot(host)

    try {
      await act(async () => root.render(<ChatSessionIdCopyButton sessionId="chat-1" />))
      const button = host.querySelector('button')!
      await act(async () => button.click())
      expect(button.textContent).toBe('Copy failed')
    } finally {
      await act(async () => root.unmount())
    }
  })
})
