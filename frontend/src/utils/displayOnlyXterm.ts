import type { IDisposable, Terminal } from '@xterm/xterm'
import { copyToClipboard } from './textUtils'

// DEC private modes a display-only (disableStdin) xterm must never honor.
//
// Full-screen TUIs (Claude Code's fullscreen renderer, ratatui apps, ...) emit
// these for a REAL terminal the user types into. In our read-only panes they
// only break the viewer:
//   - 47/1047/1049 switch xterm to the alternate buffer, which has NO
//     scrollback, so the wheel has nothing to scroll.
//   - 9/1000-1003 (+ encodings 1005/1006/1015/1016) turn on mouse tracking:
//     xterm then routes drags and the wheel to the app (and drops them, since
//     stdin is disabled) instead of selecting text / scrolling the viewport.
// Swallowing them keeps the TUI drawing into the normal buffer, where its
// absolute-cursor redraws still land on the viewport and the seeded history
// stays in native scrollback.
export const DISPLAY_ONLY_SUPPRESSED_PRIVATE_MODES: ReadonlySet<number> = new Set([
  47, 1047, 1049,
  9, 1000, 1001, 1002, 1003,
  1005, 1006, 1015, 1016,
])

function privateModeCodes(params: (number | number[])[]): number[] {
  return params.map(param => (Array.isArray(param) ? param[0] : param))
}

// Registers parser hooks that keep a display-only xterm on the normal buffer
// with mouse tracking off, plus Cmd/Ctrl+C copy of the current selection.
// Parser hooks (rather than regex-filtering written data) also catch sequences
// split across WebSocket frames.
export function installDisplayOnlyXtermGuards(term: Terminal): IDisposable {
  const disposables: IDisposable[] = []
  for (const final of ['h', 'l'] as const) {
    disposables.push(term.parser.registerCsiHandler({ prefix: '?', final }, params => {
      const codes = privateModeCodes(params as (number | number[])[])
      if (!codes.some(code => DISPLAY_ONLY_SUPPRESSED_PRIVATE_MODES.has(code))) return false
      // A combined sequence (e.g. ?1049;25h) still needs its harmless modes
      // applied; re-emit just those. They take effect after the current chunk,
      // which only matters for cosmetic modes like cursor visibility.
      const kept = codes.filter(code => !DISPLAY_ONLY_SUPPRESSED_PRIVATE_MODES.has(code))
      if (kept.length > 0) term.write(`\x1b[?${kept.join(';')}${final}`)
      return true
    }))
  }

  // With disableStdin, xterm turns Ctrl+C into a dropped ^C (and cancels the
  // event), so the browser never copies. Copy the selection ourselves for both
  // Ctrl+C and Cmd+C; with no selection the key is left alone.
  term.attachCustomKeyEventHandler(event => {
    if (event.type !== 'keydown') return true
    if (!isCopyShortcut(event)) return true
    const selection = term.hasSelection() ? term.getSelection() : ''
    if (!selection) return true
    void copyToClipboard(selection)
    event.preventDefault()
    return false
  })
  disposables.push({ dispose: () => term.attachCustomKeyEventHandler(() => true) })

  return {
    dispose: () => {
      for (const disposable of disposables.splice(0)) {
        try {
          disposable.dispose()
        } catch {
          // Disposal can race the terminal's own dispose on unmount.
        }
      }
    },
  }
}

function isCopyShortcut(event: KeyboardEvent): boolean {
  return (event.metaKey || event.ctrlKey) && !event.altKey && !event.shiftKey && event.key.toLowerCase() === 'c'
}

// Text of the rows currently in view, trailing blank rows/cells trimmed.
export function xtermVisibleText(term: Terminal): string {
  const buffer = term.buffer.active
  const lines: string[] = []
  for (let row = buffer.viewportY; row < buffer.viewportY + term.rows; row++) {
    lines.push(buffer.getLine(row)?.translateToString(true) ?? '')
  }
  while (lines.length > 0 && lines[lines.length - 1].trim() === '') lines.pop()
  return lines.join('\n')
}

// What the pane's Copy button copies: the selection, or the visible screen.
export function xtermCopyText(term: Terminal): string {
  const selection = term.hasSelection() ? term.getSelection() : ''
  return selection || xtermVisibleText(term)
}
