// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { Terminal } from '@xterm/xterm'
import { installDisplayOnlyXtermGuards, xtermCopyText, xtermVisibleText } from './displayOnlyXterm'

const { copyToClipboard } = vi.hoisted(() => ({ copyToClipboard: vi.fn(async () => true) }))
vi.mock('./textUtils', () => ({ copyToClipboard }))

const terminals: Terminal[] = []

// Scroll tests use an unopened terminal: once opened, xterm scrolls through its
// DOM viewport, which needs real cell metrics that happy-dom cannot measure.
function makeTerminal(guarded = true, open = true) {
  const term = new Terminal({ cols: 40, rows: 5, scrollback: 1000, disableStdin: true, allowProposedApi: false })
  if (open) {
    const mount = document.createElement('div')
    document.body.appendChild(mount)
    term.open(mount)
  }
  const guards = guarded ? installDisplayOnlyXtermGuards(term) : { dispose: () => {} }
  terminals.push(term)
  return { term, guards }
}

function write(term: Terminal, data: string): Promise<void> {
  return new Promise(resolve => term.write(data, resolve))
}

// Claude Code's fullscreen renderer, as recorded in a real tmux pipe log.
const CLAUDE_FULLSCREEN_ENTER = '\x1b[?1049h\x1b[2J\x1b[H\x1b[<u\x1b[>5u\x1b[?1000h\x1b[?1002h\x1b[?1003h\x1b[?1006h'

afterEach(() => {
  for (const term of terminals.splice(0)) term.dispose()
  document.body.innerHTML = ''
  copyToClipboard.mockClear()
})

describe('installDisplayOnlyXtermGuards', () => {
  it('reproduces the bug without guards: alt buffer + mouse tracking', async () => {
    const { term } = makeTerminal(false)
    await write(term, CLAUDE_FULLSCREEN_ENTER)
    expect(term.buffer.active.type).toBe('alternate')
    expect(term.modes.mouseTrackingMode).not.toBe('none')
  })

  it('keeps the normal buffer and mouse tracking off after TUI mode sequences', async () => {
    const { term } = makeTerminal()
    await write(term, CLAUDE_FULLSCREEN_ENTER)
    await write(term, '\x1b[?1047h\x1b[?47h\x1b[?9h\x1b[?1015h\x1b[?1005h')
    expect(term.buffer.active.type).toBe('normal')
    expect(term.modes.mouseTrackingMode).toBe('none')
  })

  it('catches sequences split across writes (WebSocket frames)', async () => {
    const { term } = makeTerminal()
    await write(term, '\x1b[?10')
    await write(term, '49h\x1b[?100')
    await write(term, '3h')
    expect(term.buffer.active.type).toBe('normal')
    expect(term.modes.mouseTrackingMode).toBe('none')
  })

  it('still applies harmless modes in a combined sequence', async () => {
    const { term } = makeTerminal()
    await write(term, '\x1b[?1049;2004h')
    await write(term, '')
    expect(term.buffer.active.type).toBe('normal')
    expect(term.modes.bracketedPasteMode).toBe(true)
  })

  it('grows scrollback under a TUI and the viewport scrolls', async () => {
    const { term } = makeTerminal(true, false)
    await write(term, CLAUDE_FULLSCREEN_ENTER)
    for (let i = 0; i < 30; i++) await write(term, `line ${i}\r\n`)
    const buffer = term.buffer.active
    expect(buffer.baseY).toBeGreaterThan(20)
    term.scrollLines(-10)
    expect(term.buffer.active.viewportY).toBe(term.buffer.active.baseY - 10)
  })

  it('keeps the user scroll position when new output arrives', async () => {
    const { term } = makeTerminal(true, false)
    for (let i = 0; i < 30; i++) await write(term, `line ${i}\r\n`)
    term.scrollLines(-8)
    const viewportY = term.buffer.active.viewportY
    for (let i = 30; i < 40; i++) await write(term, `line ${i}\r\n`)
    expect(term.buffer.active.viewportY).toBe(viewportY)
    expect(term.buffer.active.baseY).toBeGreaterThan(viewportY + 8)
  })

  it('copies the selection on Cmd+C and Ctrl+C', async () => {
    const { term } = makeTerminal()
    await write(term, 'hello world\r\nsecond row')
    term.select(0, 0, 5)
    expect(term.getSelection()).toBe('hello')
    const textarea = term.textarea!
    for (const init of [{ metaKey: true }, { ctrlKey: true }]) {
      const event = new KeyboardEvent('keydown', { key: 'c', bubbles: true, cancelable: true, ...init })
      textarea.dispatchEvent(event)
      expect(event.defaultPrevented).toBe(true)
    }
    expect(copyToClipboard).toHaveBeenCalledTimes(2)
    expect(copyToClipboard).toHaveBeenCalledWith('hello')
  })

  it('leaves Cmd+C alone with no selection', async () => {
    const { term } = makeTerminal()
    await write(term, 'hello')
    term.textarea!.dispatchEvent(new KeyboardEvent('keydown', { key: 'c', metaKey: true, bubbles: true, cancelable: true }))
    expect(copyToClipboard).not.toHaveBeenCalled()
  })
})

describe('xtermCopyText', () => {
  it('copies the selection, or the visible screen without one', async () => {
    const { term } = makeTerminal()
    await write(term, 'alpha\r\nbeta\r\n')
    expect(xtermVisibleText(term)).toBe('alpha\nbeta')
    expect(xtermCopyText(term)).toBe('alpha\nbeta')
    term.select(0, 1, 4)
    expect(xtermCopyText(term)).toBe('beta')
  })
})
