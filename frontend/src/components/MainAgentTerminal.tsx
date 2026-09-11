import { useCallback, useEffect, useRef, useState } from 'react'
import { agentApi } from '../services/api'
import type { TerminalSnapshot } from '../services/api-types'
import { useTheme } from '../hooks/useTheme'
import { LiveAttachXtermPane, RAW_XTERM_THEMES, StaticXtermPane } from './TerminalCenter'

type MainAgentTerminalProps = {
  sessionId: string
  /** Called when the session has no live pane to show (structured-transport
   * providers such as Cursor never have one); the host returns the tab to
   * the conversation instead of leaving a placeholder on screen. */
  onUnavailable?: () => void
}

// Keep the diagnostics-only terminal readable without allowing it to dictate
// the normal chat/workspace split. At the 13px JetBrains Mono metrics used by
// TerminalCenter this leaves room for roughly 80 columns, including xterm's
// padding. Narrow chat panes scroll the terminal horizontally instead of
// repeatedly shrinking and reflowing the underlying tmux TUI.
export const MAIN_AGENT_TERMINAL_MIN_WIDTH_PX = 680

// Product raw view for the one main coding-agent terminal. This intentionally
// reuses the mature xterm renderer/live tmux attach rather than maintaining a
// second preformatted-text terminal. Child terminal rails stay diagnostics-only.
export function MainAgentTerminal({ sessionId, onUnavailable }: MainAgentTerminalProps) {
  const onUnavailableRef = useRef(onUnavailable)
  useEffect(() => { onUnavailableRef.current = onUnavailable }, [onUnavailable])
  const { theme } = useTheme()
  const [snapshot, setSnapshot] = useState<TerminalSnapshot | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const contentRef = useRef<HTMLDivElement | null>(null)
  const requestInFlight = useRef(false)

  const refresh = useCallback(async () => {
    if (requestInFlight.current) return
    requestInFlight.current = true
    try {
      // The live socket owns scrollback. Screen is merely an initial/reconnect
      // fallback, avoiding the former full-history polling storm.
      const next = await agentApi.getMainTerminal(sessionId, { content: 'screen', lines: 200 })
      setSnapshot(next)
      setError(null)
    } catch (cause: any) {
      if (cause?.response?.status === 404) {
        setSnapshot(null)
        setError(null)
        onUnavailableRef.current?.()
      } else {
        setError(cause?.message || 'Could not load the live view.')
      }
    } finally {
      setLoading(false)
      requestInFlight.current = false
    }
  }, [sessionId])

  useEffect(() => {
    setSnapshot(null)
    setError(null)
    setLoading(true)
    void refresh()
    const timer = window.setInterval(() => { void refresh() }, 3000)
    return () => window.clearInterval(timer)
  }, [refresh])

  const isLive = Boolean(snapshot?.active && snapshot.tmux_session)

  return (
    <section
      className="flex min-h-0 min-w-0 flex-1 flex-col overflow-x-auto bg-[#0b0e14] text-[#e7e9e5]"
      data-testid="main-agent-terminal-scroll-container"
    >
      <div
        className="min-h-0 flex-1"
        data-testid="main-agent-terminal-grid"
        style={{ minWidth: MAIN_AGENT_TERMINAL_MIN_WIDTH_PX }}
      >
        {error ? (
          <div className="p-4 text-sm text-red-300">{error}</div>
        ) : !snapshot ? (
          <div className="flex h-full items-center justify-center text-sm text-neutral-500">
            {/* Users are not told about terminals or tmux: while the live session has
                not come up yet this simply reads as the agent starting. */}
            {loading ? 'Starting…' : 'Starting…'}
          </div>
        ) : isLive ? (
          <LiveAttachXtermPane
            key={`${snapshot.terminal_id}:${snapshot.tmux_session}`}
            terminalId={snapshot.terminal_id}
            tmuxSession={snapshot.tmux_session}
            sessionId={sessionId}
            className="h-full w-full"
            contentRef={contentRef}
            xtermTheme={RAW_XTERM_THEMES[theme]}
            authoritativeContent={snapshot.content}
            authoritativeVersion={`${snapshot.chunk_index}:${snapshot.updated_at}`}
            reconnectOnClose
            streamUrl={(cols, rows) => agentApi.getMainTerminalStreamUrl(sessionId, cols, rows, snapshot.tmux_session)}
            loadSnapshot={() => agentApi.getMainTerminal(sessionId, { content: 'screen', lines: 200 })}
          />
        ) : (
          <StaticXtermPane
            key={`${snapshot.terminal_id}:${snapshot.chunk_index}`}
            content={snapshot.content}
            className="h-full w-full"
            contentRef={contentRef}
            xtermTheme={RAW_XTERM_THEMES[theme]}
          />
        )}
      </div>
    </section>
  )
}
