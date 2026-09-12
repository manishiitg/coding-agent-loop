// @vitest-environment happy-dom
import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, describe, expect, it, vi } from 'vitest'

const { getMainTerminal } = vi.hoisted(() => ({
  getMainTerminal: vi.fn(),
}))

vi.mock('../services/api', () => ({
  agentApi: {
    getMainTerminal,
    getMainTerminalStreamUrl: vi.fn(() => 'ws://localhost/terminal'),
  },
}))
vi.mock('../hooks/useTheme', () => ({ useTheme: () => ({ theme: 'dark' }) }))
vi.mock('./TerminalCenter', () => ({
  LiveAttachXtermPane: () => <div data-testid="live-terminal" />,
  StaticXtermPane: () => <div data-testid="static-terminal" />,
  RAW_XTERM_THEMES: { dark: {} },
}))

import { MainAgentTerminal, MAIN_AGENT_TERMINAL_MIN_WIDTH_PX } from './MainAgentTerminal'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })

afterEach(() => {
  vi.clearAllMocks()
  document.body.innerHTML = ''
})

describe('MainAgentTerminal sizing', () => {
  it('keeps the debug terminal near 80 columns and scrolls when the chat pane is narrower', async () => {
    getMainTerminal.mockResolvedValue({
      terminal_id: 'terminal-1',
      session_id: 'session-1',
      tmux_session: 'tmux-1',
      content: '',
      rows: [],
      chunk_index: 1,
      active: true,
      state: 'running',
      status: {},
      created_at: '2026-09-11T00:00:00Z',
      updated_at: '2026-09-11T00:00:00Z',
    })

    const host = document.createElement('div')
    document.body.append(host)
    const root = createRoot(host)
    try {
      await act(async () => root.render(<MainAgentTerminal sessionId="session-1" />))
      await act(async () => Promise.resolve())

      const scroller = host.querySelector('[data-testid="main-agent-terminal-scroll-container"]')
      const terminalGrid = host.querySelector<HTMLElement>('[data-testid="main-agent-terminal-grid"]')

      expect(scroller?.classList.contains('overflow-x-auto')).toBe(true)
      expect(terminalGrid?.style.minWidth).toBe(`${MAIN_AGENT_TERMINAL_MIN_WIDTH_PX}px`)
      expect(MAIN_AGENT_TERMINAL_MIN_WIDTH_PX).toBe(680)
      expect(host.querySelector('[data-testid="live-terminal"]')).not.toBeNull()
    } finally {
      await act(async () => root.unmount())
      host.remove()
    }
  })
})
