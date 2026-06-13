import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { AlertTriangle, ArrowDownToLine, ArrowRightToLine, Braces, Bug, Check, ChevronDown, ChevronsLeft, ChevronsRight, ChevronUp, Copy, CornerDownLeft, CornerUpLeft, GitBranch, History, Info, Minus, Palette, Plus, Power, RefreshCw, Square, Terminal, Trash2, X } from 'lucide-react'
import { AnsiUp } from 'ansi_up'
import { Terminal as XTerm, type ITheme } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { agentApi } from '../services/api'
import type { PollingEvent, TerminalSnapshot } from '../services/api-types'
import { useGlobalPresetStore } from '../stores/useGlobalPresetStore'
import { useChatStore } from '../stores/useChatStore'
import { useWorkflowStore } from '../stores/useWorkflowStore'
import { TERMINAL_REFRESH_REQUEST_EVENT } from '../utils/terminalRefresh'
import { MarkdownRenderer } from './ui/MarkdownRenderer'
import { Tooltip, TooltipContent, TooltipTrigger } from './ui/tooltip'

// Module-level ansi_up singleton for synthetic/structured terminal rows.
// Real tmux panes render raw ANSI through xterm.js.
//
// use_classes = true makes ansi_up emit CSS classes (ansi-red-fg,
// ansi-bright-blue-fg, ansi-bold, etc.) instead of inline colors. The
// classes are styled in index.css with a single palette that reads well
// across every TERMINAL_THEMES dark variant. Switching to per-theme color
// palettes (one ANSI scheme per dropdown selection) would mean scoped CSS
// variables on the pane wrapper — deferred until anyone asks for it.
const ansiUp = new AnsiUp()
ansiUp.use_classes = true

// stripAnsi removes ANSI CSI sequences from a string. Used to feed clean text
// into the line classifier regexes while we preserve the raw colored line for
// rendering. Mirrors the Go-side strip on the backend so what we display
// matches what the matcher saw.
function stripAnsi(s: string): string {
  // eslint-disable-next-line no-control-regex
  return s.replace(/\x1B\[[0-9;?]*[A-Za-z]/g, '')
}

// hasAnsiCodes returns true when the string contains at least one CSI escape.
// Used to decide whether to take the colored-render path or fall back to the
// existing plain text path.
function hasAnsiCodes(s: string): boolean {
  // eslint-disable-next-line no-control-regex
  return s.includes('\x1B[')
}

// rawAfterVisibleChars returns the substring of `raw` that starts after the
// first `visibleCharCount` non-ANSI characters. Lets us slice the raw line
// at a position discovered against the stripped version (e.g. "$ " prefix
// length is always 2 in the stripped form but variable in the raw form).
function rawAfterVisibleChars(raw: string, visibleCharCount: number): string {
  let i = 0
  let visible = 0
  while (i < raw.length && visible < visibleCharCount) {
    if (raw[i] === '\x1B' && raw[i + 1] === '[') {
      let j = i + 2
      while (j < raw.length && !/[A-Za-z]/.test(raw[j])) j++
      i = j + 1
      continue
    }
    visible++
    i++
  }
  return raw.slice(i)
}

interface TerminalCenterProps {
  currentSessionId?: string
  compact?: boolean
  hasConversationActivity?: boolean
}

const TERMINAL_REFRESH_HISTORY_LINES = 10000
const TERMINAL_ACTIVE_DISPLAY_HISTORY_LINES = 10000
const TERMINAL_ACTIVE_RAIL_MAIN_HISTORY_LINES = 600
const TERMINAL_ACTIVE_RAIL_STEP_HISTORY_LINES = 1200
const TERMINAL_DETAIL_CACHE_LIMIT = 40
const MAX_PRIOR_ARCHIVED_TURNS_TO_INLINE = 3
const PROMPT_COMPLETION_FALLBACK_SECONDS = 60
const INACTIVE_FALLBACK_SECONDS = 120
const EMPTY_TERMINAL_RESPONSE_GRACE_POLLS = 10
const TERMINAL_POLL_INTERVAL_MS = 3000
const TERMINAL_ACTIVE_RAIL_PROBE_LIMIT = 8
const TERMINAL_FAST_POLL_INTERVAL_MS = 300
const TERMINAL_FAST_POLL_DURATION_MS = 7000

type TerminalColorScheme = 'neon' | 'mono' | 'homebrew' | 'catppuccin' | 'nord' | 'gruvbox' | 'solarized' | 'tokyo'
type TerminalDebugKey = 'enter' | 'esc' | 'ctrl-c' | 'ctrl-o' | 'tab' | 'up' | 'down'
type TerminalRailFilter = 'all' | 'running' | 'non-running'
type TerminalDetailOptions = { content?: 'stored' | 'screen' | 'history' | 'tmux' | 'deep'; lines?: number; debug?: boolean; debugSource?: string }

const DEFAULT_TERMINAL_COLOR_SCHEME: TerminalColorScheme = 'homebrew'
const TERMINAL_COLOR_SCHEME_STORAGE_KEY = 'terminal-color-scheme'
const TERMINAL_SCROLL_DEBUG_STORAGE_KEY = 'runloop_terminal_debug'

const TERMINAL_THEMES = {
  neon: {
    selection: 'selection:bg-cyan-500/25',
    contentText: 'text-[12px] leading-5',
    assistantLabelText: 'text-[11px] leading-4',
    assistantBodyText: 'text-[12.5px] leading-6',
    toolText: 'text-[11px] leading-4',
    doneText: 'text-[10.5px]',
    footerText: 'text-[11px]',
    headerText: 'text-xs',
    railText: 'text-xs',
    railMetaText: 'text-[10px]',
    chipText: 'text-[10px]',
    microText: 'text-[9px]',
    markdown:
      '!font-mono !text-[12.5px] !leading-6 !text-neutral-100 [&_*]:!font-mono [&_p]:!my-1 [&_p]:!leading-6 [&_p]:!text-neutral-100 [&_ul]:!my-1 [&_ol]:!my-1 [&_li]:!my-0.5 [&_li]:!leading-6 [&_strong]:!text-amber-200 [&_code]:!rounded [&_code]:!bg-neutral-900/80 [&_code]:!px-1 [&_code]:!text-amber-200 [&_pre]:!my-1.5 [&_pre]:!rounded [&_pre]:!border [&_pre]:!border-neutral-800 [&_pre]:!bg-neutral-950/70 [&_pre]:!p-2 [&_pre]:!text-[11.5px] [&_a]:!text-cyan-300 [&_blockquote]:!my-1 [&_blockquote]:!border-neutral-700 [&_h1]:!text-[13px] [&_h2]:!text-[13px] [&_h3]:!text-[12.5px] [&_h1]:!text-cyan-300 [&_h2]:!text-cyan-300 [&_h3]:!text-cyan-200',
    prompt: 'text-cyan-300/90',
    userAuto: 'text-cyan-300/80',
    user: 'text-emerald-300/85',
    assistant: 'text-cyan-300/85',
    toolPending: 'text-yellow-300',
    done: 'text-emerald-500/70',
    streaming: 'text-cyan-300/80',
    preValidationPassedText: 'text-emerald-300',
    preValidationPassedChip: 'border-emerald-700/60 bg-emerald-950/30 text-emerald-300',
    dotRunning: 'bg-emerald-400',
    dotCompleted: 'bg-sky-400',
    dotClosing: 'bg-amber-400',
    stateRunning: 'text-emerald-300',
    stateCompleted: 'text-sky-300',
    stateClosing: 'text-amber-300',
    routeRail: 'bg-cyan-950/15 text-cyan-100',
    routeIcon: 'bg-cyan-400/15 text-cyan-300',
    routeClose: 'text-cyan-300/50 hover:bg-cyan-900/45 hover:text-cyan-100',
    routeMeta: 'text-cyan-300/75',
    railSelected: 'border-l-emerald-300 bg-[#222826] text-neutral-50 shadow-[inset_0_1px_0_rgba(255,255,255,0.04)]',
    railSpinner: 'text-cyan-300/90',
    selectedRouteChip: 'border-cyan-700/60 bg-cyan-950/25 text-cyan-200',
    warningText: 'text-amber-300',
    warningChip: 'border-amber-800/70 bg-amber-950/30 text-amber-200',
    copiedIcon: 'text-emerald-300',
    debugActive: 'border-cyan-700/80 text-cyan-300',
    inputFocus: 'focus:border-cyan-500/80',
    emptyPulse: 'bg-blue-400',
  },
  mono: {
    selection: 'selection:bg-white/15',
    contentText: 'text-[12px] leading-[1.55]',
    assistantLabelText: 'text-[10.5px] leading-4',
    assistantBodyText: 'text-[12px] leading-5',
    toolText: 'text-[10.5px] leading-4',
    doneText: 'text-[10px]',
    footerText: 'text-[10.5px]',
    headerText: 'text-[11px]',
    railText: 'text-[11px]',
    railMetaText: 'text-[9.5px]',
    chipText: 'text-[9.5px]',
    microText: 'text-[9px]',
    markdown:
      '!font-mono !text-[12.5px] !leading-6 !text-neutral-100 [&_*]:!font-mono [&_p]:!my-1 [&_p]:!leading-6 [&_p]:!text-neutral-100 [&_ul]:!my-1 [&_ol]:!my-1 [&_li]:!my-0.5 [&_li]:!leading-6 [&_strong]:!text-neutral-50 [&_code]:!rounded [&_code]:!bg-neutral-900/80 [&_code]:!px-1 [&_code]:!text-neutral-100 [&_pre]:!my-1.5 [&_pre]:!rounded [&_pre]:!border [&_pre]:!border-neutral-800 [&_pre]:!bg-neutral-950/70 [&_pre]:!p-2 [&_pre]:!text-[11.5px] [&_a]:!text-neutral-100 [&_blockquote]:!my-1 [&_blockquote]:!border-neutral-700 [&_h1]:!text-[13px] [&_h2]:!text-[13px] [&_h3]:!text-[12.5px] [&_h1]:!text-neutral-50 [&_h2]:!text-neutral-50 [&_h3]:!text-neutral-100',
    prompt: 'text-neutral-100',
    userAuto: 'text-neutral-400',
    user: 'text-neutral-200',
    assistant: 'text-neutral-100',
    toolPending: 'text-neutral-300',
    done: 'text-neutral-500',
    streaming: 'text-neutral-300',
    preValidationPassedText: 'text-neutral-300',
    preValidationPassedChip: 'border-neutral-700/80 bg-neutral-900/70 text-neutral-300',
    dotRunning: 'bg-neutral-100',
    dotCompleted: 'bg-neutral-500',
    dotClosing: 'bg-neutral-400',
    stateRunning: 'text-neutral-100',
    stateCompleted: 'text-neutral-400',
    stateClosing: 'text-neutral-400',
    routeRail: 'bg-neutral-900/45 text-neutral-200',
    routeIcon: 'bg-neutral-800 text-neutral-200',
    routeClose: 'text-neutral-500 hover:bg-neutral-800 hover:text-neutral-100',
    routeMeta: 'text-neutral-500',
    railSelected: 'border-l-neutral-100 bg-[#242424] text-neutral-50 shadow-[inset_0_1px_0_rgba(255,255,255,0.05)]',
    railSpinner: 'text-neutral-100',
    selectedRouteChip: 'border-neutral-700/80 bg-neutral-900/80 text-neutral-200',
    warningText: 'text-neutral-400',
    warningChip: 'border-neutral-700/80 bg-neutral-900/70 text-neutral-300',
    copiedIcon: 'text-neutral-100',
    debugActive: 'border-neutral-500 text-neutral-100',
    inputFocus: 'focus:border-neutral-400',
    emptyPulse: 'bg-neutral-400',
  },
  homebrew: {
    selection: 'selection:bg-lime-300/20',
    contentText: 'text-[12.5px] leading-[1.65]',
    assistantLabelText: 'text-[11px] leading-4',
    assistantBodyText: 'text-[12.5px] leading-6',
    toolText: 'text-[11px] leading-4',
    doneText: 'text-[10.5px]',
    footerText: 'text-[11px]',
    headerText: 'text-[11px]',
    railText: 'text-[11.5px]',
    railMetaText: 'text-[10px]',
    chipText: 'text-[10px]',
    microText: 'text-[9px]',
    markdown:
      '!font-mono !text-[12.5px] !leading-6 !text-neutral-100 [&_*]:!font-mono [&_p]:!my-1 [&_p]:!leading-6 [&_p]:!text-neutral-100 [&_ul]:!my-1 [&_ol]:!my-1 [&_li]:!my-0.5 [&_li]:!leading-6 [&_strong]:!text-neutral-50 [&_code]:!rounded [&_code]:!bg-neutral-900/85 [&_code]:!px-1 [&_code]:!text-neutral-100 [&_pre]:!my-1.5 [&_pre]:!rounded [&_pre]:!border [&_pre]:!border-neutral-800 [&_pre]:!bg-black/45 [&_pre]:!p-2 [&_pre]:!text-[11.5px] [&_a]:!text-lime-200 [&_blockquote]:!my-1 [&_blockquote]:!border-neutral-700 [&_h1]:!text-[13px] [&_h2]:!text-[13px] [&_h3]:!text-[12.5px] [&_h1]:!text-neutral-50 [&_h2]:!text-neutral-50 [&_h3]:!text-neutral-100',
    prompt: 'text-lime-200/90',
    userAuto: 'text-neutral-400',
    user: 'text-neutral-100',
    assistant: 'text-neutral-100',
    toolPending: 'text-lime-200/80',
    done: 'text-neutral-500',
    streaming: 'text-lime-200/85',
    preValidationPassedText: 'text-lime-200/80',
    preValidationPassedChip: 'border-lime-900/50 bg-lime-950/20 text-lime-200/80',
    dotRunning: 'bg-lime-300',
    dotCompleted: 'bg-neutral-500',
    dotClosing: 'bg-neutral-400',
    stateRunning: 'text-lime-200',
    stateCompleted: 'text-neutral-400',
    stateClosing: 'text-neutral-400',
    routeRail: 'bg-lime-950/10 text-neutral-200',
    routeIcon: 'bg-lime-950/45 text-lime-200/80',
    routeClose: 'text-neutral-500 hover:bg-neutral-800 hover:text-neutral-100',
    routeMeta: 'text-neutral-500',
    railSelected: 'border-l-lime-300 bg-[#20231d] text-neutral-50 shadow-[inset_0_1px_0_rgba(255,255,255,0.05)]',
    railSpinner: 'text-lime-200/90',
    selectedRouteChip: 'border-lime-900/55 bg-lime-950/20 text-lime-100/85',
    warningText: 'text-neutral-400',
    warningChip: 'border-neutral-700/80 bg-neutral-900/70 text-neutral-300',
    copiedIcon: 'text-lime-200',
    debugActive: 'border-lime-700/70 text-lime-200',
    inputFocus: 'focus:border-lime-700/80',
    emptyPulse: 'bg-lime-300',
  },
  catppuccin: {
    selection: 'selection:bg-pink-300/20',
    contentText: 'text-[12.5px] leading-[1.62]',
    assistantLabelText: 'text-[11px] leading-4',
    assistantBodyText: 'text-[12.5px] leading-6',
    toolText: 'text-[11px] leading-4',
    doneText: 'text-[10.5px]',
    footerText: 'text-[11px]',
    headerText: 'text-[11px]',
    railText: 'text-[11.5px]',
    railMetaText: 'text-[10px]',
    chipText: 'text-[10px]',
    microText: 'text-[9px]',
    markdown:
      '!font-mono !text-[12.5px] !leading-6 !text-[#cdd6f4] [&_*]:!font-mono [&_p]:!my-1 [&_p]:!leading-6 [&_p]:!text-[#cdd6f4] [&_ul]:!my-1 [&_ol]:!my-1 [&_li]:!my-0.5 [&_li]:!leading-6 [&_strong]:!text-[#f5e0dc] [&_code]:!rounded [&_code]:!bg-[#11111b]/85 [&_code]:!px-1 [&_code]:!text-[#f5c2e7] [&_pre]:!my-1.5 [&_pre]:!rounded [&_pre]:!border [&_pre]:!border-[#45475a] [&_pre]:!bg-[#11111b]/75 [&_pre]:!p-2 [&_pre]:!text-[11.5px] [&_a]:!text-[#89b4fa] [&_blockquote]:!my-1 [&_blockquote]:!border-[#585b70] [&_h1]:!text-[13px] [&_h2]:!text-[13px] [&_h3]:!text-[12.5px] [&_h1]:!text-[#f5c2e7] [&_h2]:!text-[#f5c2e7] [&_h3]:!text-[#cba6f7]',
    prompt: 'text-[#89b4fa]',
    userAuto: 'text-[#a6adc8]',
    user: 'text-[#cdd6f4]',
    assistant: 'text-[#f5c2e7]',
    toolPending: 'text-[#f9e2af]',
    done: 'text-[#a6adc8]',
    streaming: 'text-[#89b4fa]',
    preValidationPassedText: 'text-[#a6e3a1]',
    preValidationPassedChip: 'border-[#a6e3a1]/40 bg-[#1e1e2e] text-[#a6e3a1]',
    dotRunning: 'bg-[#89b4fa]',
    dotCompleted: 'bg-[#a6adc8]',
    dotClosing: 'bg-[#f9e2af]',
    stateRunning: 'text-[#89b4fa]',
    stateCompleted: 'text-[#a6adc8]',
    stateClosing: 'text-[#f9e2af]',
    routeRail: 'bg-[#1e1e2e]/55 text-[#cdd6f4]',
    routeIcon: 'bg-[#313244] text-[#cba6f7]',
    routeClose: 'text-[#a6adc8] hover:bg-[#313244] hover:text-[#f5e0dc]',
    routeMeta: 'text-[#a6adc8]',
    railSelected: 'border-l-[#89b4fa] bg-[#1e1e2e] text-[#cdd6f4] shadow-[inset_0_1px_0_rgba(255,255,255,0.05)]',
    railSpinner: 'text-[#89b4fa]',
    selectedRouteChip: 'border-[#cba6f7]/50 bg-[#1e1e2e] text-[#cba6f7]',
    warningText: 'text-[#f9e2af]',
    warningChip: 'border-[#f9e2af]/45 bg-[#1e1e2e] text-[#f9e2af]',
    copiedIcon: 'text-[#a6e3a1]',
    debugActive: 'border-[#89b4fa]/70 text-[#89b4fa]',
    inputFocus: 'focus:border-[#89b4fa]',
    emptyPulse: 'bg-[#89b4fa]',
  },
  nord: {
    selection: 'selection:bg-sky-300/20',
    contentText: 'text-[12px] leading-[1.6]',
    assistantLabelText: 'text-[10.5px] leading-4',
    assistantBodyText: 'text-[12px] leading-5',
    toolText: 'text-[10.5px] leading-4',
    doneText: 'text-[10px]',
    footerText: 'text-[10.5px]',
    headerText: 'text-[11px]',
    railText: 'text-[11px]',
    railMetaText: 'text-[9.5px]',
    chipText: 'text-[9.5px]',
    microText: 'text-[9px]',
    markdown:
      '!font-mono !text-[12px] !leading-5 !text-[#d8dee9] [&_*]:!font-mono [&_p]:!my-1 [&_p]:!leading-5 [&_p]:!text-[#d8dee9] [&_ul]:!my-1 [&_ol]:!my-1 [&_li]:!my-0.5 [&_li]:!leading-5 [&_strong]:!text-[#eceff4] [&_code]:!rounded [&_code]:!bg-[#2e3440]/85 [&_code]:!px-1 [&_code]:!text-[#88c0d0] [&_pre]:!my-1.5 [&_pre]:!rounded [&_pre]:!border [&_pre]:!border-[#4c566a] [&_pre]:!bg-[#2e3440]/70 [&_pre]:!p-2 [&_pre]:!text-[11.5px] [&_a]:!text-[#88c0d0] [&_blockquote]:!my-1 [&_blockquote]:!border-[#4c566a] [&_h1]:!text-[13px] [&_h2]:!text-[13px] [&_h3]:!text-[12.5px] [&_h1]:!text-[#88c0d0] [&_h2]:!text-[#88c0d0] [&_h3]:!text-[#81a1c1]',
    prompt: 'text-[#88c0d0]',
    userAuto: 'text-[#8fbcbb]',
    user: 'text-[#eceff4]',
    assistant: 'text-[#d8dee9]',
    toolPending: 'text-[#ebcb8b]',
    done: 'text-[#8fbcbb]/75',
    streaming: 'text-[#88c0d0]',
    preValidationPassedText: 'text-[#a3be8c]',
    preValidationPassedChip: 'border-[#a3be8c]/45 bg-[#2e3440]/70 text-[#a3be8c]',
    dotRunning: 'bg-[#88c0d0]',
    dotCompleted: 'bg-[#81a1c1]',
    dotClosing: 'bg-[#ebcb8b]',
    stateRunning: 'text-[#88c0d0]',
    stateCompleted: 'text-[#81a1c1]',
    stateClosing: 'text-[#ebcb8b]',
    routeRail: 'bg-[#2e3440]/45 text-[#d8dee9]',
    routeIcon: 'bg-[#3b4252] text-[#88c0d0]',
    routeClose: 'text-[#4c566a] hover:bg-[#3b4252] hover:text-[#eceff4]',
    routeMeta: 'text-[#81a1c1]',
    railSelected: 'border-l-[#88c0d0] bg-[#242933] text-[#eceff4] shadow-[inset_0_1px_0_rgba(255,255,255,0.05)]',
    railSpinner: 'text-[#88c0d0]',
    selectedRouteChip: 'border-[#88c0d0]/50 bg-[#2e3440]/70 text-[#88c0d0]',
    warningText: 'text-[#ebcb8b]',
    warningChip: 'border-[#ebcb8b]/45 bg-[#2e3440]/70 text-[#ebcb8b]',
    copiedIcon: 'text-[#a3be8c]',
    debugActive: 'border-[#88c0d0]/70 text-[#88c0d0]',
    inputFocus: 'focus:border-[#88c0d0]',
    emptyPulse: 'bg-[#88c0d0]',
  },
  gruvbox: {
    selection: 'selection:bg-yellow-300/20',
    contentText: 'text-[12.5px] leading-[1.62]',
    assistantLabelText: 'text-[11px] leading-4',
    assistantBodyText: 'text-[12.5px] leading-6',
    toolText: 'text-[11px] leading-4',
    doneText: 'text-[10.5px]',
    footerText: 'text-[11px]',
    headerText: 'text-[11px]',
    railText: 'text-[11.5px]',
    railMetaText: 'text-[10px]',
    chipText: 'text-[10px]',
    microText: 'text-[9px]',
    markdown:
      '!font-mono !text-[12.5px] !leading-6 !text-[#ebdbb2] [&_*]:!font-mono [&_p]:!my-1 [&_p]:!leading-6 [&_p]:!text-[#ebdbb2] [&_ul]:!my-1 [&_ol]:!my-1 [&_li]:!my-0.5 [&_li]:!leading-6 [&_strong]:!text-[#fbf1c7] [&_code]:!rounded [&_code]:!bg-[#1d2021]/85 [&_code]:!px-1 [&_code]:!text-[#fabd2f] [&_pre]:!my-1.5 [&_pre]:!rounded [&_pre]:!border [&_pre]:!border-[#504945] [&_pre]:!bg-[#1d2021]/75 [&_pre]:!p-2 [&_pre]:!text-[11.5px] [&_a]:!text-[#83a598] [&_blockquote]:!my-1 [&_blockquote]:!border-[#665c54] [&_h1]:!text-[13px] [&_h2]:!text-[13px] [&_h3]:!text-[12.5px] [&_h1]:!text-[#fabd2f] [&_h2]:!text-[#fabd2f] [&_h3]:!text-[#d3869b]',
    prompt: 'text-[#b8bb26]',
    userAuto: 'text-[#a89984]',
    user: 'text-[#ebdbb2]',
    assistant: 'text-[#fbf1c7]',
    toolPending: 'text-[#fabd2f]',
    done: 'text-[#a89984]',
    streaming: 'text-[#b8bb26]',
    preValidationPassedText: 'text-[#b8bb26]',
    preValidationPassedChip: 'border-[#b8bb26]/45 bg-[#282828]/70 text-[#b8bb26]',
    dotRunning: 'bg-[#b8bb26]',
    dotCompleted: 'bg-[#a89984]',
    dotClosing: 'bg-[#fabd2f]',
    stateRunning: 'text-[#b8bb26]',
    stateCompleted: 'text-[#a89984]',
    stateClosing: 'text-[#fabd2f]',
    routeRail: 'bg-[#282828]/55 text-[#ebdbb2]',
    routeIcon: 'bg-[#3c3836] text-[#fabd2f]',
    routeClose: 'text-[#928374] hover:bg-[#3c3836] hover:text-[#fbf1c7]',
    routeMeta: 'text-[#a89984]',
    railSelected: 'border-l-[#fabd2f] bg-[#2d2926] text-[#fbf1c7] shadow-[inset_0_1px_0_rgba(255,255,255,0.05)]',
    railSpinner: 'text-[#b8bb26]',
    selectedRouteChip: 'border-[#fabd2f]/50 bg-[#282828]/70 text-[#fabd2f]',
    warningText: 'text-[#fabd2f]',
    warningChip: 'border-[#fabd2f]/45 bg-[#282828]/70 text-[#fabd2f]',
    copiedIcon: 'text-[#b8bb26]',
    debugActive: 'border-[#fabd2f]/70 text-[#fabd2f]',
    inputFocus: 'focus:border-[#fabd2f]',
    emptyPulse: 'bg-[#b8bb26]',
  },
  solarized: {
    selection: 'selection:bg-cyan-300/20',
    contentText: 'text-[12px] leading-[1.6]',
    assistantLabelText: 'text-[10.5px] leading-4',
    assistantBodyText: 'text-[12px] leading-5',
    toolText: 'text-[10.5px] leading-4',
    doneText: 'text-[10px]',
    footerText: 'text-[10.5px]',
    headerText: 'text-[11px]',
    railText: 'text-[11px]',
    railMetaText: 'text-[9.5px]',
    chipText: 'text-[9.5px]',
    microText: 'text-[9px]',
    markdown:
      '!font-mono !text-[12px] !leading-5 !text-[#93a1a1] [&_*]:!font-mono [&_p]:!my-1 [&_p]:!leading-5 [&_p]:!text-[#93a1a1] [&_ul]:!my-1 [&_ol]:!my-1 [&_li]:!my-0.5 [&_li]:!leading-5 [&_strong]:!text-[#eee8d5] [&_code]:!rounded [&_code]:!bg-[#002b36]/85 [&_code]:!px-1 [&_code]:!text-[#2aa198] [&_pre]:!my-1.5 [&_pre]:!rounded [&_pre]:!border [&_pre]:!border-[#586e75] [&_pre]:!bg-[#002b36]/75 [&_pre]:!p-2 [&_pre]:!text-[11.5px] [&_a]:!text-[#268bd2] [&_blockquote]:!my-1 [&_blockquote]:!border-[#586e75] [&_h1]:!text-[13px] [&_h2]:!text-[13px] [&_h3]:!text-[12.5px] [&_h1]:!text-[#b58900] [&_h2]:!text-[#b58900] [&_h3]:!text-[#2aa198]',
    prompt: 'text-[#2aa198]',
    userAuto: 'text-[#839496]',
    user: 'text-[#eee8d5]',
    assistant: 'text-[#93a1a1]',
    toolPending: 'text-[#b58900]',
    done: 'text-[#839496]',
    streaming: 'text-[#2aa198]',
    preValidationPassedText: 'text-[#859900]',
    preValidationPassedChip: 'border-[#859900]/45 bg-[#073642]/70 text-[#859900]',
    dotRunning: 'bg-[#2aa198]',
    dotCompleted: 'bg-[#268bd2]',
    dotClosing: 'bg-[#b58900]',
    stateRunning: 'text-[#2aa198]',
    stateCompleted: 'text-[#268bd2]',
    stateClosing: 'text-[#b58900]',
    routeRail: 'bg-[#073642]/55 text-[#93a1a1]',
    routeIcon: 'bg-[#002b36] text-[#2aa198]',
    routeClose: 'text-[#586e75] hover:bg-[#073642] hover:text-[#eee8d5]',
    routeMeta: 'text-[#839496]',
    railSelected: 'border-l-[#2aa198] bg-[#073642] text-[#eee8d5] shadow-[inset_0_1px_0_rgba(255,255,255,0.05)]',
    railSpinner: 'text-[#2aa198]',
    selectedRouteChip: 'border-[#2aa198]/50 bg-[#073642]/70 text-[#2aa198]',
    warningText: 'text-[#b58900]',
    warningChip: 'border-[#b58900]/45 bg-[#073642]/70 text-[#b58900]',
    copiedIcon: 'text-[#859900]',
    debugActive: 'border-[#2aa198]/70 text-[#2aa198]',
    inputFocus: 'focus:border-[#2aa198]',
    emptyPulse: 'bg-[#2aa198]',
  },
  tokyo: {
    selection: 'selection:bg-indigo-300/20',
    contentText: 'text-[12px] leading-[1.58]',
    assistantLabelText: 'text-[10.5px] leading-4',
    assistantBodyText: 'text-[12px] leading-5',
    toolText: 'text-[10.5px] leading-4',
    doneText: 'text-[10px]',
    footerText: 'text-[10.5px]',
    headerText: 'text-[11px]',
    railText: 'text-[11px]',
    railMetaText: 'text-[9.5px]',
    chipText: 'text-[9.5px]',
    microText: 'text-[9px]',
    markdown:
      '!font-mono !text-[12px] !leading-5 !text-[#c0caf5] [&_*]:!font-mono [&_p]:!my-1 [&_p]:!leading-5 [&_p]:!text-[#c0caf5] [&_ul]:!my-1 [&_ol]:!my-1 [&_li]:!my-0.5 [&_li]:!leading-5 [&_strong]:!text-[#d5d6db] [&_code]:!rounded [&_code]:!bg-[#1a1b26]/85 [&_code]:!px-1 [&_code]:!text-[#bb9af7] [&_pre]:!my-1.5 [&_pre]:!rounded [&_pre]:!border [&_pre]:!border-[#414868] [&_pre]:!bg-[#1a1b26]/75 [&_pre]:!p-2 [&_pre]:!text-[11.5px] [&_a]:!text-[#7aa2f7] [&_blockquote]:!my-1 [&_blockquote]:!border-[#414868] [&_h1]:!text-[13px] [&_h2]:!text-[13px] [&_h3]:!text-[12.5px] [&_h1]:!text-[#7dcfff] [&_h2]:!text-[#7dcfff] [&_h3]:!text-[#bb9af7]',
    prompt: 'text-[#7dcfff]',
    userAuto: 'text-[#9aa5ce]',
    user: 'text-[#c0caf5]',
    assistant: 'text-[#c0caf5]',
    toolPending: 'text-[#e0af68]',
    done: 'text-[#9aa5ce]',
    streaming: 'text-[#7dcfff]',
    preValidationPassedText: 'text-[#9ece6a]',
    preValidationPassedChip: 'border-[#9ece6a]/45 bg-[#1a1b26]/70 text-[#9ece6a]',
    dotRunning: 'bg-[#7dcfff]',
    dotCompleted: 'bg-[#7aa2f7]',
    dotClosing: 'bg-[#e0af68]',
    stateRunning: 'text-[#7dcfff]',
    stateCompleted: 'text-[#7aa2f7]',
    stateClosing: 'text-[#e0af68]',
    routeRail: 'bg-[#1a1b26]/55 text-[#c0caf5]',
    routeIcon: 'bg-[#24283b] text-[#7dcfff]',
    routeClose: 'text-[#565f89] hover:bg-[#24283b] hover:text-[#c0caf5]',
    routeMeta: 'text-[#9aa5ce]',
    railSelected: 'border-l-[#7dcfff] bg-[#1f2335] text-[#c0caf5] shadow-[inset_0_1px_0_rgba(255,255,255,0.05)]',
    railSpinner: 'text-[#7dcfff]',
    selectedRouteChip: 'border-[#7dcfff]/50 bg-[#1a1b26]/70 text-[#7dcfff]',
    warningText: 'text-[#e0af68]',
    warningChip: 'border-[#e0af68]/45 bg-[#1a1b26]/70 text-[#e0af68]',
    copiedIcon: 'text-[#9ece6a]',
    debugActive: 'border-[#7dcfff]/70 text-[#7dcfff]',
    inputFocus: 'focus:border-[#7dcfff]',
    emptyPulse: 'bg-[#7dcfff]',
  },
} as const

type TerminalTheme = (typeof TERMINAL_THEMES)[TerminalColorScheme]

const TERMINAL_COLOR_SCHEME_OPTIONS: Array<{ value: TerminalColorScheme; label: string }> = [
  { value: 'homebrew', label: 'Standard' },
  { value: 'mono', label: 'Compact' },
  { value: 'gruvbox', label: 'Classic' },
]

const XTERM_PROFILE_OPTIONS: Record<TerminalColorScheme, {
  fontFamily: string
  fontSize: number
  lineHeight: number
  cursorStyle: 'block' | 'underline' | 'bar'
  panePaddingClass: string
}> = {
  neon: {
    fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace',
    fontSize: 12.5,
    lineHeight: 1.45,
    cursorStyle: 'block',
    panePaddingClass: 'p-2',
  },
  mono: {
    fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace',
    fontSize: 11,
    lineHeight: 1.22,
    cursorStyle: 'bar',
    panePaddingClass: 'p-1.5',
  },
  homebrew: {
    fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace',
    fontSize: 12.5,
    lineHeight: 1.45,
    cursorStyle: 'block',
    panePaddingClass: 'p-2',
  },
  catppuccin: {
    fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace',
    fontSize: 12.5,
    lineHeight: 1.45,
    cursorStyle: 'block',
    panePaddingClass: 'p-2',
  },
  nord: {
    fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace',
    fontSize: 12,
    lineHeight: 1.4,
    cursorStyle: 'block',
    panePaddingClass: 'p-2',
  },
  gruvbox: {
    fontFamily: 'Menlo, Monaco, Consolas, "Liberation Mono", ui-monospace, monospace',
    fontSize: 13,
    lineHeight: 1.5,
    cursorStyle: 'underline',
    panePaddingClass: 'p-2.5',
  },
  solarized: {
    fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace',
    fontSize: 12,
    lineHeight: 1.4,
    cursorStyle: 'block',
    panePaddingClass: 'p-2',
  },
  tokyo: {
    fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace',
    fontSize: 12,
    lineHeight: 1.4,
    cursorStyle: 'block',
    panePaddingClass: 'p-2',
  },
}

const XTERM_BASE_THEMES: Record<TerminalColorScheme, ITheme> = {
  neon: {
    background: '#020403',
    foreground: '#8cff9a',
    cursor: '#39ff14',
    selectionBackground: '#14532d',
    black: '#020403',
    red: '#ff5f5f',
    green: '#39ff14',
    yellow: '#d6ff5f',
    blue: '#00d7ff',
    magenta: '#ff5fff',
    cyan: '#5fffff',
    white: '#d7ffd7',
    brightBlack: '#166534',
    brightRed: '#ff8787',
    brightGreen: '#8cff9a',
    brightYellow: '#efff8a',
    brightBlue: '#5fd7ff',
    brightMagenta: '#ff87ff',
    brightCyan: '#87ffff',
    brightWhite: '#f0fff4',
  },
  mono: {
    background: '#101010',
    foreground: '#e5e5e5',
    cursor: '#f5f5f5',
    selectionBackground: '#404040',
    black: '#171717',
    red: '#d4d4d4',
    green: '#d4d4d4',
    yellow: '#e5e5e5',
    blue: '#d4d4d4',
    magenta: '#d4d4d4',
    cyan: '#d4d4d4',
    white: '#e5e5e5',
    brightBlack: '#737373',
    brightRed: '#f5f5f5',
    brightGreen: '#f5f5f5',
    brightYellow: '#fafafa',
    brightBlue: '#f5f5f5',
    brightMagenta: '#f5f5f5',
    brightCyan: '#f5f5f5',
    brightWhite: '#ffffff',
  },
  homebrew: {
    background: '#0b0d0c',
    foreground: '#e5e7eb',
    cursor: '#a3e635',
    selectionBackground: '#334155',
    black: '#1f2937',
    red: '#ef4444',
    green: '#22c55e',
    yellow: '#eab308',
    blue: '#3b82f6',
    magenta: '#d946ef',
    cyan: '#06b6d4',
    white: '#e5e7eb',
    brightBlack: '#9ca3af',
    brightRed: '#f87171',
    brightGreen: '#4ade80',
    brightYellow: '#facc15',
    brightBlue: '#60a5fa',
    brightMagenta: '#e879f9',
    brightCyan: '#22d3ee',
    brightWhite: '#f9fafb',
  },
  catppuccin: {
    background: '#11111b',
    foreground: '#cdd6f4',
    cursor: '#f5c2e7',
    selectionBackground: '#45475a',
    black: '#45475a',
    red: '#f38ba8',
    green: '#a6e3a1',
    yellow: '#f9e2af',
    blue: '#89b4fa',
    magenta: '#cba6f7',
    cyan: '#94e2d5',
    white: '#bac2de',
    brightBlack: '#585b70',
    brightRed: '#f38ba8',
    brightGreen: '#a6e3a1',
    brightYellow: '#f9e2af',
    brightBlue: '#89b4fa',
    brightMagenta: '#f5c2e7',
    brightCyan: '#94e2d5',
    brightWhite: '#f5e0dc',
  },
  nord: {
    background: '#2e3440',
    foreground: '#d8dee9',
    cursor: '#88c0d0',
    selectionBackground: '#4c566a',
    black: '#3b4252',
    red: '#bf616a',
    green: '#a3be8c',
    yellow: '#ebcb8b',
    blue: '#81a1c1',
    magenta: '#b48ead',
    cyan: '#88c0d0',
    white: '#e5e9f0',
    brightBlack: '#4c566a',
    brightRed: '#bf616a',
    brightGreen: '#a3be8c',
    brightYellow: '#ebcb8b',
    brightBlue: '#81a1c1',
    brightMagenta: '#b48ead',
    brightCyan: '#8fbcbb',
    brightWhite: '#eceff4',
  },
  gruvbox: {
    background: '#1d2021',
    foreground: '#ebdbb2',
    cursor: '#fabd2f',
    selectionBackground: '#504945',
    black: '#282828',
    red: '#cc241d',
    green: '#98971a',
    yellow: '#d79921',
    blue: '#458588',
    magenta: '#b16286',
    cyan: '#689d6a',
    white: '#a89984',
    brightBlack: '#928374',
    brightRed: '#fb4934',
    brightGreen: '#b8bb26',
    brightYellow: '#fabd2f',
    brightBlue: '#83a598',
    brightMagenta: '#d3869b',
    brightCyan: '#8ec07c',
    brightWhite: '#fbf1c7',
  },
  solarized: {
    background: '#002b36',
    foreground: '#93a1a1',
    cursor: '#2aa198',
    selectionBackground: '#073642',
    black: '#073642',
    red: '#dc322f',
    green: '#859900',
    yellow: '#b58900',
    blue: '#268bd2',
    magenta: '#d33682',
    cyan: '#2aa198',
    white: '#eee8d5',
    brightBlack: '#586e75',
    brightRed: '#cb4b16',
    brightGreen: '#859900',
    brightYellow: '#b58900',
    brightBlue: '#268bd2',
    brightMagenta: '#6c71c4',
    brightCyan: '#2aa198',
    brightWhite: '#fdf6e3',
  },
  tokyo: {
    background: '#1a1b26',
    foreground: '#c0caf5',
    cursor: '#7dcfff',
    selectionBackground: '#33467c',
    black: '#15161e',
    red: '#f7768e',
    green: '#9ece6a',
    yellow: '#e0af68',
    blue: '#7aa2f7',
    magenta: '#bb9af7',
    cyan: '#7dcfff',
    white: '#a9b1d6',
    brightBlack: '#414868',
    brightRed: '#f7768e',
    brightGreen: '#9ece6a',
    brightYellow: '#e0af68',
    brightBlue: '#7aa2f7',
    brightMagenta: '#bb9af7',
    brightCyan: '#7dcfff',
    brightWhite: '#c0caf5',
  },
}

// Claude Code (and other CLIs) paint a "queued message" — one submitted while the
// agent is mid-turn — with a background of xterm 256-color index 237. xterm.js's
// built-in value for 237 (#3a3a3a) renders noticeably brighter against our dark
// terminal backgrounds than the same index does in a native terminal, so queued
// messages look like a harsh grey bar here. ITheme can only style the 16 base
// ANSI colors, so we override index 237 through `extendedAnsi` (which covers the
// full 16-255 range). To keep every other color pixel-identical to xterm.js's
// defaults we regenerate the standard 256-color palette and only tweak 237.
const QUEUED_MESSAGE_BG_237 = '#1e1e1e'

function buildExtendedAnsiPalette(): string[] {
  const toHex = (v: number) => v.toString(16).padStart(2, '0')
  // extendedAnsi covers indices 16-255 (240 entries; extendedAnsi[i] === color i+16).
  const palette: string[] = []
  for (let i = 16; i <= 255; i++) {
    if (i < 232) {
      // 6x6x6 color cube.
      const n = i - 16
      const cube = [Math.floor(n / 36), Math.floor((n % 36) / 6), n % 6].map(c =>
        c === 0 ? 0 : 55 + 40 * c,
      )
      palette.push(`#${toHex(cube[0])}${toHex(cube[1])}${toHex(cube[2])}`)
    } else {
      // 24-step grayscale ramp.
      const v = 8 + 10 * (i - 232)
      palette.push(`#${toHex(v)}${toHex(v)}${toHex(v)}`)
    }
  }
  palette[237 - 16] = QUEUED_MESSAGE_BG_237
  return palette
}

const EXTENDED_ANSI_PALETTE = buildExtendedAnsiPalette()

const XTERM_THEMES: Record<TerminalColorScheme, ITheme> = Object.fromEntries(
  Object.entries(XTERM_BASE_THEMES).map(([scheme, theme]) => [
    scheme,
    { ...theme, extendedAnsi: EXTENDED_ANSI_PALETTE },
  ]),
) as Record<TerminalColorScheme, ITheme>

function isTerminalColorScheme(value: string | null): value is TerminalColorScheme {
  return TERMINAL_COLOR_SCHEME_OPTIONS.some(option => option.value === value)
}

function readStoredTerminalColorScheme(): TerminalColorScheme {
  if (typeof window === 'undefined') return DEFAULT_TERMINAL_COLOR_SCHEME
  try {
    const stored = window.localStorage.getItem(TERMINAL_COLOR_SCHEME_STORAGE_KEY)
    return isTerminalColorScheme(stored) ? stored : DEFAULT_TERMINAL_COLOR_SCHEME
  } catch {
    return DEFAULT_TERMINAL_COLOR_SCHEME
  }
}

function writeStoredTerminalColorScheme(scheme: TerminalColorScheme) {
  try {
    window.localStorage.setItem(TERMINAL_COLOR_SCHEME_STORAGE_KEY, scheme)
  } catch {
    // Best-effort visual preference only.
  }
}

interface RoutingRouteSummary {
  route_id?: string
  route_name?: string
  condition?: string
  next_step_id?: string
  next_step_type?: string
}

interface RoutingDecision {
  id: string
  stepId?: string
  stepTitle?: string
  selectedRouteId: string
  selectedRouteName?: string
  nextStepId?: string
  nextStepType?: string
  routeCount: number
  reasoning?: string
  timestamp?: string
}

type TerminalRailItem =
  | { kind: 'terminal'; terminal: TerminalSnapshot; depth: number }
  | { kind: 'route'; decision: RoutingDecision; depth: number }

function isOpaqueID(value?: string): boolean {
  if (!value) return false
  return /^[a-z]+:[0-9a-f-]{16,}$/i.test(value) || /^[0-9a-f-]{24,}$/i.test(value)
}

function humanizeIdentifier(value?: string): string {
  if (!value) return ''
  const cleaned = value
    .replace(/^exec[-_:]/i, '')
    .replace(/^step[-_:]\d+[-_:]/i, '')
    .replace(/^main[-_:]/i, '')
    .replace(/[-_]+/g, ' ')
    .trim()
  if (!cleaned) return ''
  return cleaned.charAt(0).toUpperCase() + cleaned.slice(1)
}

function asRecord(value: unknown): Record<string, unknown> | undefined {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return undefined
  return value as Record<string, unknown>
}

function stringField(value: unknown): string {
  return typeof value === 'string' ? value.trim() : ''
}

function routingPayload(event: PollingEvent): Record<string, unknown> | undefined {
  const data = asRecord(event.data)
  const nested = asRecord(data?.data)
  return nested || data
}

function routingRoutes(value: unknown): RoutingRouteSummary[] {
  if (!Array.isArray(value)) return []
  const routes: RoutingRouteSummary[] = []
  for (const item of value) {
    const route = asRecord(item)
    if (!route) continue
    routes.push({
      route_id: stringField(route.route_id) || undefined,
      route_name: stringField(route.route_name) || undefined,
      condition: stringField(route.condition) || undefined,
      next_step_id: stringField(route.next_step_id) || undefined,
      next_step_type: stringField(route.next_step_type) || undefined,
    })
  }
  return routes
}

function routingDecisionFromEvent(event: PollingEvent): RoutingDecision | null {
  if (event.type !== 'routing_evaluated') return null
  const payload = routingPayload(event)
  if (!payload) return null
  const response = asRecord(payload.routing_response)
  const selectedRouteId = stringField(response?.selected_route_id)
  if (!selectedRouteId) return null
  const routes = routingRoutes(payload.routes)
  const selectedRoute = routes.find(route => route.route_id === selectedRouteId)
  const stepId = stringField(payload.step_id)
  return {
    id: event.id || `${stepId || 'routing'}:${selectedRouteId}:${event.timestamp || ''}`,
    stepId: stepId || undefined,
    stepTitle: stringField(payload.step_title) || undefined,
    selectedRouteId,
    selectedRouteName: selectedRoute?.route_name,
    nextStepId: selectedRoute?.next_step_id,
    nextStepType: selectedRoute?.next_step_type,
    routeCount: routes.length,
    reasoning: stringField(response?.reasoning) || undefined,
    timestamp: event.timestamp,
  }
}

function routeDecisionLabel(decision: RoutingDecision): string {
  return decision.selectedRouteName || humanizeIdentifier(decision.selectedRouteId) || decision.selectedRouteId
}

function routeDecisionTitle(decision: RoutingDecision): string {
  const label = routeDecisionLabel(decision)
  const nextStepType = decision.nextStepType ? ` (${stepTypeLabel(decision.nextStepType)})` : ''
  return `Routing: ${label}${decision.nextStepId ? ` -> ${decision.nextStepId}${nextStepType}` : ''}`
}

function routeDecisionDedupeKey(decision: RoutingDecision): string {
  return [
    decision.stepId || '',
    decision.selectedRouteId || '',
    decision.nextStepId || '',
  ].join('|')
}

function routingDecisionTime(decision: RoutingDecision): number {
  return decision.timestamp ? new Date(decision.timestamp).getTime() || 0 : 0
}

function workflowNameFromPath(path?: string): string {
  if (!path) return ''
  const parts = path.split('/').filter(Boolean)
  const workflowIndex = parts.findIndex(part => part === 'Workflow')
  if (workflowIndex >= 0 && parts[workflowIndex + 1]) {
    return humanizeIdentifier(parts[workflowIndex + 1])
  }
  return humanizeIdentifier(parts[parts.length - 1])
}

function formatExecutionKind(kind?: string): string {
  switch (kind) {
    case 'main_agent':
      return 'Main agent'
    case 'workflow_step':
    case 'execution_only':
    case 'step':
      return 'Workflow step'
    case 'background_agent':
      return 'Background agent'
    case 'todo_task':
    case 'sub_agent':
    case 'delegation':
      return 'Sub-agent'
    default:
      return humanizeIdentifier(kind)
  }
}

function formatTerminalKindLabel(terminal: TerminalSnapshot): string {
  const kind = terminal.execution_kind || terminal.scope
  if ((kind === 'workflow_step' || kind === 'execution_only' || kind === 'step') && terminal.step_type) {
    return `${humanizeIdentifier(terminal.step_type)} step`
  }
  return formatExecutionKind(terminal.execution_kind)
}

function terminalWorkflowLabel(terminal: TerminalSnapshot): string {
  return terminal.workflow_label || terminal.workflow_name || workflowNameFromPath(terminal.workflow_path)
}

function terminalTaskLabel(terminal: TerminalSnapshot): string {
  const rawLabel = terminal.label || terminal.execution_id || terminal.owner_id || ''
  const kind = terminal.execution_kind || terminal.scope
  if (kind === 'workflow_step' || kind === 'step' || kind === 'execution_only') {
    // Prefer the human step title / agent name over the raw step_id. The ID
    // (e.g. "_global" for the global-learnings skill) is a folder/lookup key,
    // not a display name; it remains the last-resort fallback.
    return terminal.step_name || terminal.agent_name || terminal.step_id || (isOpaqueID(rawLabel) ? '' : humanizeIdentifier(rawLabel))
  }
  return terminal.step_name || terminal.agent_name || visibleStepID(terminal) || (isOpaqueID(rawLabel) ? '' : humanizeIdentifier(rawLabel))
}

function formatTerminalTitle(terminal: TerminalSnapshot): string {
  // Prefer a human title (step title, or the agent's own name for step-less
  // maintenance agents like learning/organize) over the raw step_id. The ID —
  // e.g. "_global" for the global-learnings skill — is a folder/lookup key, not
  // a display name, so it's the last-resort fallback. Everything else — parent,
  // chip, workflow name, kind — lives in the meta row so the title stays minimal.
  const kind = (terminal.execution_kind || terminal.scope || '').toLowerCase()
  if (isMainAgentTerminal(terminal)) {
    return terminal.agent_name || terminal.step_name || 'Main agent'
  }
  if (kind === 'background_agent' || kind === 'background' || kind === 'delegation' || kind === 'todo_task' || kind === 'sub_agent') {
    return terminal.agent_name || terminal.step_name || terminal.display_title || visibleStepID(terminal) || formatTerminalKindLabel(terminal) || 'Terminal'
  }
  return terminal.step_name || terminal.agent_name || visibleStepID(terminal) || formatTerminalKindLabel(terminal) || 'Terminal'
}

function visibleStepID(terminal: TerminalSnapshot): string {
  const value = terminal.step_id || ''
  if (!value) return ''
  if (isMainAgentTerminal(terminal) && value.startsWith('main_agent:')) return ''
  return value
}

// formatTransportChip returns "transport·provider" (e.g. "api·anthropic",
// "structured·claudecode", "tmux·codex") for the title prefix. Falls
// back to inference: tmux_session implies tmux; absence implies the
// caller-supplied step_transport or "api".
function formatTransportChip(terminal: TerminalSnapshot): string {
  let transport = terminal.step_transport || ''
  if (!transport) {
    transport = terminal.tmux_session ? 'tmux' : 'api'
  }
  // Normalize backend strings to the short chip form.
  if (transport === 'structured_cli' || transport === 'structured') transport = 'structured'
  if (transport === 'non_tmux') transport = 'api'
  const provider = terminal.status?.provider_label || ''
  const transportLabel = humanizeIdentifier(transport)
  return provider ? `${provider} · ${transportLabel}` : transportLabel
}

function formatRailTransportChip(terminal: TerminalSnapshot): string {
  return formatTransportChip(terminal)
    .replace(/^Claude Code\b/, 'Claude')
    .replace(/^Codex CLI\b/, 'Codex')
    .replace(/^Gemini CLI\b/, 'Gemini')
}

// extractDoneStats parses the synthetic terminal's "[done · 1240ms · 412 in
// · 28 out · $0.000089]" trailer out of the pane content, so we can
// surface duration / tokens / cost in the meta row without needing
// dedicated backend fields. Returns empty when the trailer is absent —
// tmux providers don't emit a Done line, only real pane scrapes.
function extractDoneStats(content: string): { duration?: string; tokensIn?: string; tokensOut?: string; cost?: string } {
  if (!content) return {}
  const match = content.match(/\[done · ([^\]]+)\][^\[]*$/)
  if (!match) return {}
  const parts = match[1].split('·').map(p => p.trim())
  const out: { duration?: string; tokensIn?: string; tokensOut?: string; cost?: string } = {}
  for (const p of parts) {
    // Backend now emits duration in human-readable form already
    // ("234ms", "30.7s", "2m 14s", "1h 5m") — accept any non-token,
    // non-cost segment as the duration.
    if (p.endsWith(' in')) {
      out.tokensIn = p.replace(' in', '')
    } else if (p.endsWith(' out')) {
      out.tokensOut = p.replace(' out', '')
    } else if (p.startsWith('$')) {
      out.cost = p
    } else if (!out.duration && /\d/.test(p)) {
      out.duration = p
    }
  }
  return out
}

// humanizeMs mirrors the Go-side humanizeDuration: "234ms", "30.7s",
// "2m 14s", "1h 5m". Used when surfacing duration_ms from terminal.status.
function humanizeMs(ms: number): string {
  if (!ms || ms < 0) return ''
  if (ms < 1000) return `${ms}ms`
  const sec = ms / 1000
  if (sec < 60) return `${sec.toFixed(1)}s`
  const secs = Math.floor(ms / 1000)
  const mins = Math.floor(secs / 60)
  const rem = secs % 60
  if (mins < 60) return `${mins}m ${rem}s`
  const hours = Math.floor(mins / 60)
  const remMin = mins % 60
  return `${hours}h ${remMin}m`
}

// formatCost matches the Go-side formatUSD scale: cheap calls keep
// six decimals so a $0.000089 haiku call doesn't render as "$0.0000".
function formatCost(cost: number): string {
  if (cost >= 1) return cost.toFixed(2)
  if (cost >= 0.01) return cost.toFixed(4)
  if (cost > 0) return cost.toFixed(6)
  return '0'
}

function formatTerminalModelLabel(terminal: TerminalSnapshot): string {
  const lines = (terminal.content || '')
    .replace(/\x1B(?:[@-Z\\-_]|\[[0-?]*[ -/]*[@-~])/g, '')
    .split(/\r?\n/)
    .slice(0, 30)
    .map(line => line.replace(/\s+/g, ' ').trim())
    .filter(Boolean)

  for (const line of lines) {
    const explicit = line.match(/\bmodel:\s*([^·|]+?)(?:\s+\/model\b|\s*[·|]|$)/i)
    if (explicit?.[1]) return explicit[1].trim()

    const assignment = line.match(/\bmodel=([^\s·|]+)/i)
    if (assignment?.[1]) return assignment[1].trim()

    const claude = line.match(/\bClaude Code\s+v[\w.-]+.*?\s([A-Za-z]+(?:\s+\d+(?:\.\d+)?){1,2}(?:\s+with\s+[^·|]+)?)(?:\s+·\s+Claude Max|\s*[·|]|$)/i)
    if (claude?.[1]) return claude[1].trim()
  }

  return ''
}

function stepTypeLabel(stepType?: string): string {
  const type = (stepType || '').trim()
  return type ? `${humanizeIdentifier(type)} step` : ''
}

function terminalStepTypeLabel(terminal: TerminalSnapshot): string {
  return stepTypeLabel(terminal.step_type)
}

function terminalRailStepTypeLabel(terminal: TerminalSnapshot): string {
  const type = (terminal.step_type || '').trim().toLowerCase()
  if (!type || type === 'regular') return ''
  return terminalStepTypeLabel(terminal)
}

function displayMetaWithoutStepType(terminal: TerminalSnapshot): string {
  const meta = terminal.display_meta || ''
  const stepType = (terminal.step_type || '').trim()
  if (!meta || !stepType) return meta
  const stepTypeLabel = humanizeIdentifier(stepType)
  return meta
    .split('·')
    .map(part => part.trim())
    .filter(part => part && part !== stepTypeLabel)
    .join(' · ')
}

function formatTerminalMeta(terminal: TerminalSnapshot): string {
  const chip = formatTransportChip(terminal)
  const modelLabel = formatTerminalModelLabel(terminal)
  const stepTypeLabel = terminalStepTypeLabel(terminal)
  const parts: string[] = [
    isArchivedTurnTerminal(terminal) ? 'Previous turn' : '',
    chip,
    stepTypeLabel,
    modelLabel,
  ]
  if (terminal.step_attempt && terminal.step_attempt > 1) {
    parts.push(`attempt ${terminal.step_attempt}`)
  }
  if (terminal.step_execution_mode) {
    parts.push(formatStepExecutionModeChip(terminal.step_execution_mode))
  }
  const displayMeta = displayMetaWithoutStepType(terminal)
  if (displayMeta) parts.push(displayMeta)
  return [...new Set(parts.filter(Boolean))].join(' · ')
}

function formatStepExecutionModeChip(mode?: string): string {
  const normalized = (mode || '').toLowerCase().trim()
  if (normalized === 'learn_code') return 'Api'
  return humanizeIdentifier(mode)
}

function formatSelectedTerminalMeta(terminal: TerminalSnapshot): string {
  return [formatTerminalMeta(terminal), formatUpdatedAge(terminal)].filter(Boolean).join(' · ')
}

function terminalPreValidationSummary(terminal: TerminalSnapshot): string {
  return terminal.status?.pre_validation_summary || ''
}

function terminalPreValidationClass(terminal: TerminalSnapshot, theme: TerminalTheme): string {
  switch ((terminal.status?.pre_validation_status || '').toLowerCase()) {
    case 'passed':
      return theme.preValidationPassedText
    case 'failed':
      return 'text-red-300'
    default:
      return 'text-neutral-400'
  }
}

function terminalPreValidationChip(terminal: TerminalSnapshot, theme: TerminalTheme): { label: string; className: string; title: string } | null {
  const summary = terminalPreValidationSummary(terminal)
  if (!summary) return null

  const passed = terminal.status?.pre_validation_passed_checks || 0
  const failed = terminal.status?.pre_validation_failed_checks || 0
  const total = terminal.status?.pre_validation_total_checks || 0
  const countLabel = total > 0 ? `${passed}/${total}` : ''
  switch ((terminal.status?.pre_validation_status || '').toLowerCase()) {
    case 'passed':
      return {
        label: countLabel ? `✓ ${countLabel}` : '✓',
        className: theme.preValidationPassedChip,
        title: summary,
      }
    case 'failed':
      return {
        label: failed > 0 ? `✕ ${failed}` : '✕',
        className: 'border-red-700/60 bg-red-950/30 text-red-300',
        title: summary,
      }
    default:
      return {
        label: countLabel ? `• ${countLabel}` : '•',
        className: 'border-neutral-700 bg-neutral-900 text-neutral-400',
        title: summary,
      }
  }
}

function terminalClosesAt(terminal: TerminalSnapshot): Date | null {
  if (!terminal.closes_at) return null
  const date = new Date(terminal.closes_at)
  if (Number.isNaN(date.getTime())) return null
  return date
}

function terminalSecondsUntilClose(terminal: TerminalSnapshot): number {
  const closesAt = terminalClosesAt(terminal)
  if (!closesAt) return 0
  return Math.max(0, Math.ceil((closesAt.getTime() - Date.now()) / 1000))
}

function formatCloseCountdown(seconds: number): string {
  if (seconds <= 0) return 'closing'
  if (seconds >= 60) return `${Math.ceil(seconds / 60)}m`
  return `${seconds}s`
}

function terminalState(terminal: TerminalSnapshot): string {
  if (isArchivedTurnTerminal(terminal)) return 'completed'
  if (!terminal.active && terminalSecondsUntilClose(terminal) > 0) return 'closing'
  if (terminal.state === 'closing' && terminalSecondsUntilClose(terminal) <= 0) return 'completed'
  if (terminal.active && terminal.state === 'idle') return 'running'
  if (terminal.state) return terminal.state
  return terminal.active ? 'running' : 'completed'
}

function terminalStateLabel(terminal: TerminalSnapshot): string {
  switch (terminalState(terminal)) {
    case 'running':
      return 'active'
    case 'completed':
      return 'completed'
    case 'failed':
      return 'failed'
    case 'stale':
      return 'stale'
    case 'closing':
      // The tmux process is already gone (killed 30s after task end);
      // what this countdown measures is when the read-only snapshot
      // expires from the rail. "closes" reads like a live process is
      // shutting down — say "kept" so the user knows the work is done.
      return `kept ${formatCloseCountdown(terminalSecondsUntilClose(terminal))}`
    default:
      return terminal.active ? 'active' : 'idle'
  }
}

function terminalStateDescription(terminal: TerminalSnapshot): string {
  switch (terminalState(terminal)) {
    case 'running':
      return 'Active: the coding agent is still running and this terminal is updating.'
    case 'completed':
      return 'Completed: the coding agent finished; this is the retained terminal snapshot.'
    case 'failed':
      return 'Failed: the coding agent or workflow step ended with an error.'
    case 'stale':
      return 'Stale: no terminal updates were received for a long time; this pane may have lost its lifecycle event.'
    case 'closing':
      return `Snapshot: the agent finished and this read-only view will be removed in ${formatCloseCountdown(terminalSecondsUntilClose(terminal))}.`
    default:
      return terminal.active ? 'Active terminal' : 'Inactive terminal snapshot'
  }
}

function terminalDotClass(terminal: TerminalSnapshot, theme: TerminalTheme): string {
  switch (terminalState(terminal)) {
    case 'running':
      return theme.dotRunning
    case 'completed':
      return theme.dotCompleted
    case 'failed':
      return 'bg-red-400'
    case 'stale':
      return 'bg-zinc-400'
    case 'closing':
      return theme.dotClosing
    default:
      return 'bg-neutral-500'
  }
}

function terminalStateTextClass(terminal: TerminalSnapshot, theme: TerminalTheme): string {
  switch (terminalState(terminal)) {
    case 'running':
      return theme.stateRunning
    case 'completed':
      return theme.stateCompleted
    case 'failed':
      return 'text-red-300'
    case 'stale':
      return 'text-zinc-300'
    case 'closing':
      return theme.stateClosing
    default:
      return 'text-neutral-500'
  }
}

function canDismissTerminal(terminal: TerminalSnapshot): boolean {
  if (isMainAgentTerminal(terminal)) return false
  const state = terminalState(terminal)
  return state === 'completed' || state === 'closing' || state === 'failed' || state === 'stale'
}

function canForceCompleteTerminal(terminal: TerminalSnapshot): boolean {
  const state = terminalState(terminal)
  return state === 'running' || state === 'stale'
}

function canSendTerminalDebugInput(terminal: TerminalSnapshot): boolean {
  // Allow key input whenever a tmux pane exists — not only while the terminal
  // reports "running". A pane can be alive and waiting at a prompt even when the
  // terminal state is idle/completed (e.g. a turn mis-detected as "completed"
  // while actually stalled on an MCP-tool approval prompt), and sending
  // Tab/Enter/Esc is exactly what unblocks it. If the pane is truly gone the
  // backend send-keys fails gracefully and surfaces an error.
  return Boolean(terminal.tmux_session)
}

function hasTerminalDebugActions(terminal: TerminalSnapshot): boolean {
  return Boolean(terminal.tmux_session)
}

function shellQuote(value: string): string {
  return `'${value.replace(/'/g, `'\\''`)}'`
}

function shortDebugID(value?: string): string {
  if (!value) return ''
  if (value.length <= 18) return value
  return `${value.slice(0, 10)}...${value.slice(-6)}`
}

function terminalDebugText(terminal: TerminalSnapshot): string {
  return [
    `terminal_id=${terminal.terminal_id}`,
    terminal.tmux_session ? `tmux_session=${terminal.tmux_session}` : '',
    `session_id=${terminal.session_id}`,
    terminal.owner_id ? `owner_id=${terminal.owner_id}` : '',
    terminal.execution_id ? `execution_id=${terminal.execution_id}` : '',
    terminal.execution_kind ? `execution_kind=${terminal.execution_kind}` : '',
    terminal.step_type ? `step_type=${terminal.step_type}` : '',
    terminal.state ? `state=${terminal.state}` : '',
    terminal.closes_at ? `closes_at=${terminal.closes_at}` : '',
    terminal.retention_seconds ? `retention_seconds=${terminal.retention_seconds}` : '',
    `title=${formatTerminalTitle(terminal)}`,
  ].filter(Boolean).join('\n')
}

function isScrolledNearBottom(el: HTMLElement): boolean {
  return el.scrollHeight - el.scrollTop - el.clientHeight < 24
}

function trimTerminalDisplayContent(content: string): string {
  // Tmux screen captures often include the pane's empty rows after the last
  // prompt. Keep the raw snapshot in state, but do not render those trailing
  // blank rows or first-open auto-scroll lands on empty space.
  return content.replace(/(?:[ \t\r]*\n)+[ \t\r]*$/g, '')
}

function terminalUpdatedTime(terminal: TerminalSnapshot): number {
  const value = new Date(terminal.updated_at || terminal.created_at).getTime()
  return Number.isNaN(value) ? 0 : value
}

function terminalCreatedTime(terminal: TerminalSnapshot): number {
  const value = new Date(terminal.created_at || terminal.updated_at).getTime()
  return Number.isNaN(value) ? 0 : value
}

function formatUpdatedAge(terminal: TerminalSnapshot): string {
  const updatedAt = terminalUpdatedTime(terminal)
  if (!updatedAt) return ''
  const seconds = Math.max(0, Math.floor((Date.now() - updatedAt) / 1000))
  if (seconds < 5) return 'updated now'
  if (seconds < 60) return `updated ${seconds}s ago`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `updated ${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  return `updated ${hours}h ago`
}

function formatStartedTimestamp(terminal: TerminalSnapshot): { label: string; title: string } | null {
  const startedAt = terminalCreatedTime(terminal)
  if (!startedAt) return null
  const date = new Date(startedAt)
  const now = new Date()
  const sameDay = date.toDateString() === now.toDateString()
  const time = date.toLocaleTimeString([], {
    hour: '2-digit',
    minute: '2-digit',
  })
  const label = sameDay
    ? `start ${time}`
    : `start ${date.toLocaleDateString([], { month: 'short', day: 'numeric' })} ${time}`
  return {
    label,
    title: `Started ${date.toLocaleString()}`,
  }
}

function formatRailAge(terminal: TerminalSnapshot): { label: string; title: string } | null {
  const startedAt = terminalCreatedTime(terminal)
  if (!startedAt) return null
  const date = new Date(startedAt)
  const seconds = Math.max(0, Math.floor((Date.now() - startedAt) / 1000))
  const title = `Started ${date.toLocaleString()}`
  if (seconds < 10) return { label: 'now', title }
  if (seconds < 60) return { label: `${seconds}s ago`, title }
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return { label: `${minutes}m ago`, title }
  const hours = Math.floor(minutes / 60)
  return { label: `${hours}h ago`, title }
}

function formatFallbackSeconds(seconds: number): string {
  if (seconds <= 0) return 'now'
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  const rem = seconds % 60
  return rem ? `${minutes}m ${rem}s` : `${minutes}m`
}

function hasPromptCompletionFallbackText(content?: string): boolean {
  if (!content) return false
  return /status:\s*complete(d)?/i.test(content)
}

function terminalUsesIdleFallback(terminal: TerminalSnapshot): boolean {
  return Boolean(terminal.tmux_session) || (terminal.step_transport || '').toLowerCase() === 'tmux'
}

function terminalFallbackInfo(terminal: TerminalSnapshot): { label: string; title: string } | null {
  if (!terminalUsesIdleFallback(terminal)) return null
  if (!terminal.active || terminalState(terminal) !== 'running') return null
  const updatedAt = terminalUpdatedTime(terminal)
  if (!updatedAt) return null
  const ageSeconds = Math.max(0, Math.floor((Date.now() - updatedAt) / 1000))
  const hasPromptFallback = hasPromptCompletionFallbackText(terminal.content)
  const threshold = hasPromptFallback ? PROMPT_COMPLETION_FALLBACK_SECONDS : INACTIVE_FALLBACK_SECONDS
  const remaining = Math.max(0, threshold - ageSeconds)
  if (remaining > 30) return null
  const rule = hasPromptFallback ? 'completion fallback' : 'idle fallback'
  const label = remaining > 0
    ? `${rule} in ${formatFallbackSeconds(remaining)}`
    : `${rule} due`
  const title = hasPromptFallback
    ? `Backend will mark this terminal completed if the STATUS: COMPLETED fallback remains unchanged for ${formatFallbackSeconds(PROMPT_COMPLETION_FALLBACK_SECONDS)}.`
    : `Backend will mark this terminal completed if the screen has no changes for ${formatFallbackSeconds(INACTIVE_FALLBACK_SECONDS)}.`
  return { label, title }
}

// isMainAgentTerminal returns true for the persistent chat-session
// terminal that the user keeps coming back to. We pin it to the top of
// every list so it's the first thing the eye lands on when switching
// to Debug view.
function isMainAgentTerminal(terminal: TerminalSnapshot): boolean {
  const kind = (terminal.execution_kind || '').toLowerCase()
  const ownerID = (terminal.owner_id || '').toLowerCase()
  const terminalID = (terminal.terminal_id || '').toLowerCase()
  return kind === 'main_agent' || kind === 'main' || kind === 'chat' ||
    ownerID.startsWith('main:') ||
    terminalID.includes(':main:')
}

function isArchivedTurnTerminal(terminal: TerminalSnapshot): boolean {
  return terminal.terminal_id.includes(':turn-')
}

function isRailVisibleTerminal(terminal: TerminalSnapshot): boolean {
  return !(isArchivedTurnTerminal(terminal) && isMainAgentTerminal(terminal))
}

function terminalMatchesRailFilter(terminal: TerminalSnapshot, filter: TerminalRailFilter): boolean {
  if (isMainAgentTerminal(terminal)) return true
  if (filter === 'all') return true
  const isRunning = terminalState(terminal) === 'running'
  return filter === 'running' ? isRunning : !isRunning
}

// turnIndexFromTerminalID parses ":turn-N" out of an archived-turn terminal_id.
// Returns 0 for terminals that don't carry a turn marker so the caller can
// safely sort mixed lists.
function turnIndexFromTerminalID(terminalID: string): number {
  const m = terminalID.match(/:turn-(\d+)/)
  return m ? parseInt(m[1], 10) : 0
}

// findPriorArchivedTurns returns the `:turn-N` archived terminals for the
// same session as `current`, sorted in chronological order. Used both to
// drive the lazy content fetch and to stitch the aggregated scrollback.
function findPriorArchivedTurns(current: TerminalSnapshot, allTerminals: TerminalSnapshot[]): TerminalSnapshot[] {
  const sessionID = (current.session_id || '').trim()
  const ownerID = (current.owner_id || '').trim()
  if (!sessionID || isArchivedTurnTerminal(current) || !isSyntheticTerminal(current)) {
    return []
  }
  const matchingTurns = allTerminals
    .filter(t =>
      t.terminal_id !== current.terminal_id &&
      (t.session_id || '').trim() === sessionID &&
      (t.owner_id || '').trim() === ownerID &&
      isArchivedTurnTerminal(t) &&
      isSyntheticTerminal(t),
    )
    .sort((a, b) => turnIndexFromTerminalID(a.terminal_id) - turnIndexFromTerminalID(b.terminal_id))
  return matchingTurns.slice(-MAX_PRIOR_ARCHIVED_TURNS_TO_INLINE)
}

// aggregatePriorTurnContent stitches archived :turn- snapshot bodies (read
// from `contentByID` — the rail poll fetches metadata only, so per-archived
// content has to be loaded on demand and cached) in front of the current
// live terminal's content. Result reads like a tmux pane scrollback.
function aggregatePriorTurnContent(
  current: TerminalSnapshot,
  priorTurns: TerminalSnapshot[],
  contentByID: Record<string, string>,
): string {
  const currentContent = current.content || ''
  if (priorTurns.length === 0) return currentContent
  const parts: string[] = []
  for (const t of priorTurns) {
    const cached = contentByID[t.terminal_id]
    const c = (cached ?? t.content ?? '').trim()
    if (c) parts.push(c)
  }
  if (currentContent.trim()) parts.push(currentContent)
  return parts.join('\n\n')
}

function sortTerminalsNewestFirst(terminals: TerminalSnapshot[]): TerminalSnapshot[] {
  return [...terminals].sort((a, b) => {
    const mainDelta = (isMainAgentTerminal(b) && !isArchivedTurnTerminal(b) ? 1 : 0) - (isMainAgentTerminal(a) && !isArchivedTurnTerminal(a) ? 1 : 0)
    if (mainDelta !== 0) return mainDelta
    const archivedDelta = (isArchivedTurnTerminal(a) ? 1 : 0) - (isArchivedTurnTerminal(b) ? 1 : 0)
    if (archivedDelta !== 0) return archivedDelta
    return terminalUpdatedTime(b) - terminalUpdatedTime(a)
  })
}

function sortTerminalsForRail(terminals: TerminalSnapshot[]): TerminalSnapshot[] {
  return [...terminals].sort((a, b) => {
    // Rail order must not depend on state or updated_at. A pane moving
    // from running -> completed, or receiving a fresh tmux scrape,
    // should only change its dot/content, not jump around the list.
    const currentMainDelta = (isMainAgentTerminal(b) && !isArchivedTurnTerminal(b) ? 1 : 0) - (isMainAgentTerminal(a) && !isArchivedTurnTerminal(a) ? 1 : 0)
    if (currentMainDelta !== 0) return currentMainDelta
    const archivedDelta = (isArchivedTurnTerminal(a) ? 1 : 0) - (isArchivedTurnTerminal(b) ? 1 : 0)
    if (archivedDelta !== 0) return archivedDelta
    const mainDelta = (isMainAgentTerminal(b) ? 1 : 0) - (isMainAgentTerminal(a) ? 1 : 0)
    if (mainDelta !== 0) return mainDelta
    const createdDelta = terminalCreatedTime(a) - terminalCreatedTime(b)
    if (createdDelta !== 0) return createdDelta
    return terminalPaneKey(a).localeCompare(terminalPaneKey(b))
  })
}

function terminalPaneKey(terminal: TerminalSnapshot): string {
  return terminal.terminal_id
}

function terminalRailPadding(depth: number): number {
  return 8 + Math.min(Math.max(depth, 0), 10) * 6
}

function TerminalRailBranchMarker({ depth }: { depth: number }) {
  if (depth <= 0) return null
  return (
    <span className="relative h-4 w-2.5 shrink-0" aria-hidden>
      <span className="absolute left-1 top-0 h-2.5 border-l border-neutral-700/70" />
      <span className="absolute left-1 top-2.5 w-1.5 border-t border-neutral-700/70" />
    </span>
  )
}

function StepTypeIcon({ stepType, labelPrefix }: { stepType?: string; labelPrefix?: string }) {
  const type = (stepType || '').toLowerCase()
  if (!type) return null

  const label = `${labelPrefix || ''}${stepTypeLabel(type)}`
  const iconClass = 'h-2.5 w-2.5'
  let icon = <Terminal className={iconClass} />
  if (type === 'routing') {
    icon = <GitBranch className={iconClass} />
  } else if (type === 'conditional') {
    icon = <Braces className={iconClass} />
  } else if (type === 'todo_task') {
    icon = <Check className={iconClass} />
  } else if (type === 'message_sequence') {
    icon = <CornerUpLeft className={iconClass} />
  } else if (type === 'human_input') {
    icon = <CornerDownLeft className={iconClass} />
  }

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span
          className="inline-flex h-4 w-4 shrink-0 items-center justify-center rounded border border-neutral-700/70 bg-neutral-900/80 text-neutral-400"
          aria-label={label}
        >
          {icon}
        </span>
      </TooltipTrigger>
      <TooltipContent side="right" className="text-xs">
        {label}
      </TooltipContent>
    </Tooltip>
  )
}

function TerminalStepTypeIcon({ terminal }: { terminal: TerminalSnapshot }) {
  return <StepTypeIcon stepType={terminal.step_type} />
}

function TerminalArchivedTurnIcon() {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span
          className="inline-flex h-4 w-4 shrink-0 items-center justify-center rounded border border-amber-700/60 bg-amber-950/20 text-amber-300/80"
          aria-label="Archived previous turn"
        >
          <History className="h-2.5 w-2.5" />
        </span>
      </TooltipTrigger>
      <TooltipContent side="right" className="text-xs">
        Archived previous turn
      </TooltipContent>
    </Tooltip>
  )
}

function terminalDetailCacheKey(terminal: TerminalSnapshot): string {
  return `${terminal.terminal_id}:${terminal.chunk_index}:${terminal.updated_at || terminal.created_at || ''}`
}

function latestCachedTerminalDetail(
  terminal: TerminalSnapshot,
  cache: Record<string, TerminalSnapshot>,
): TerminalSnapshot | undefined {
  let latest: TerminalSnapshot | undefined
  let latestTime = -1
  for (const detail of Object.values(cache)) {
    if (detail.terminal_id !== terminal.terminal_id) continue
    const detailTime = terminalUpdatedTime(detail)
    if (!latest || detailTime >= latestTime) {
      latest = detail
      latestTime = detailTime
    }
  }
  return latest
}

function terminalWithCachedBody(base: TerminalSnapshot, detail: TerminalSnapshot, includeDetailState = false): TerminalSnapshot {
  const snapshot = includeDetailState ? { ...base, ...detail } : base
  return {
    ...snapshot,
    content: detail.content || base.content || '',
    content_source: detail.content_source || base.content_source,
    rows: Array.isArray(detail.rows) && detail.rows.length > 0 ? detail.rows : base.rows,
  }
}

function dedupeTerminalsByID(terminals: TerminalSnapshot[]): TerminalSnapshot[] {
  const byID = new Map<string, TerminalSnapshot>()
  for (const terminal of terminals) {
    const existing = byID.get(terminal.terminal_id)
    const terminalIsRunning = terminalState(terminal) === 'running'
    const existingIsRunning = existing ? terminalState(existing) === 'running' : false
    if (
      !existing ||
      (terminalIsRunning && !existingIsRunning) ||
      (
        terminalIsRunning === existingIsRunning &&
        terminalUpdatedTime(terminal) >= terminalUpdatedTime(existing)
      )
    ) {
      byID.set(terminal.terminal_id, terminal)
    }
  }
  return Array.from(byID.values())
}

function terminalStartTime(terminal: TerminalSnapshot): number {
  return new Date(terminal.created_at || terminal.updated_at || '').getTime() || 0
}

function resolveRailParentKey(
  terminal: TerminalSnapshot,
  terminalsByStepID: Map<string, TerminalSnapshot[]>,
): string {
  const parentStepID = terminal.parent_step_id || ''
  if (!parentStepID || (terminal.step_id && terminal.step_id === parentStepID)) return ''

  const candidates = (terminalsByStepID.get(parentStepID) || [])
    .filter(candidate => candidate.terminal_id !== terminal.terminal_id)
  if (candidates.length === 0) return ''

  const terminalTime = terminalStartTime(terminal) || terminalUpdatedTime(terminal)
  const priorCandidates = candidates
    .filter(candidate => {
      const candidateTime = terminalStartTime(candidate) || terminalUpdatedTime(candidate)
      return candidateTime <= terminalTime
    })
    .sort((a, b) => (terminalStartTime(b) || terminalUpdatedTime(b)) - (terminalStartTime(a) || terminalUpdatedTime(a)))

  return (priorCandidates[0] || candidates[candidates.length - 1]).terminal_id
}

// ---------------------------------------------------------------------------
// Structured terminal view — parses synthetic/archived terminal buffers into
// typed rows so we can colorize roles and fold long tool I/O behind a one-line
// summary. Real tmux pane captures skip this parser and render raw ANSI through
// xterm.js.
// ---------------------------------------------------------------------------

// rawText carries the ANSI-colored version of `text`. The parser populates
// it only when the source line contained ANSI codes; the renderer falls
// back to plain `text` otherwise. tool rows skip rawText because their
// internal args/result text comes from regex group captures that are
// already stripped — coloring them would require a separate raw extraction
// per group and isn't worth the complexity for this round.
type TerminalRow =
  | { kind: 'banner'; text: string; rawText?: string }
  | { kind: 'context'; text: string; rawText?: string }
  | { kind: 'user'; text: string; rawText?: string }
  | { kind: 'asst'; text: string; rawText?: string }
  | { kind: 'tool'; name: string; args: string; result?: string; resultPrefix?: '✓' | '✗'; result_prefix?: '✓' | '✗' | string }
  | { kind: 'attachment'; text: string; rawText?: string }
  | { kind: 'done'; text: string; rawText?: string }
  | { kind: 'error'; text: string; rawText?: string }
  | { kind: 'plain'; text: string; rawText?: string }

const TERMINAL_USER_PREVIEW_CHARS = 180
const TERMINAL_TOOL_ARGS_PREVIEW_CHARS = 240

function compactTerminalPreview(value: string, maxChars: number = TERMINAL_USER_PREVIEW_CHARS): string {
  const compact = value.replace(/\s+/g, ' ').trim()
  const chars = Array.from(compact)
  if (chars.length <= maxChars) return compact
  return `${chars.slice(0, maxChars).join('')}...`
}

function tryFormatJson(value: string): string | null {
  const trimmed = value.trim()
  if (!trimmed) return null
  if (!trimmed.startsWith('{') && !trimmed.startsWith('[')) return null
  try {
    return JSON.stringify(JSON.parse(trimmed), null, 2)
  } catch {
    return null
  }
}

function formatToolDetail(value: string): { text: string; isJson: boolean } {
  const trimmed = value.trim()
  if (!trimmed) return { text: '', isJson: false }
  const direct = tryFormatJson(trimmed)
  if (direct) return { text: direct, isJson: true }

  const firstObject = trimmed.search(/[{\[]/)
  if (firstObject > 0) {
    const prefix = trimmed.slice(0, firstObject).trimEnd()
    const formatted = tryFormatJson(trimmed.slice(firstObject))
    if (formatted) {
      return { text: prefix ? `${prefix}\n${formatted}` : formatted, isJson: true }
    }
  }

  return { text: value, isJson: false }
}

function shouldCollapseUserMessage(value: string): boolean {
  return value.includes('\n') || Array.from(value.trim()).length > TERMINAL_USER_PREVIEW_CHARS
}

function terminalUserMessageMeta(value: string): { label: string; body: string; isAuto: boolean } {
  const trimmed = value.trim()
  const isAuto = trimmed.startsWith('[AUTO-NOTIFICATION]')
  if (isAuto) {
    return {
      label: 'Auto',
      body: trimmed.replace(/^\[AUTO-NOTIFICATION\]\s*/, ''),
      isAuto,
    }
  }
  return { label: 'User', body: value, isAuto }
}

function isAutoNotificationText(value: string): boolean {
  return value.trim().startsWith('[AUTO-NOTIFICATION]')
}

// classifyTerminalLine inspects a single pane line and returns a typed row.
// `stripped` is the line with ANSI removed (for regex / prefix matching);
// `raw` is the original line preserving ANSI SGR codes. Rows whose text body
// can carry color are annotated with rawText so the renderer can pass it
// through ansi_up. When the raw line has no ANSI codes the parser leaves
// rawText undefined and the renderer falls back to plain text.
function classifyTerminalLine(stripped: string, raw: string): TerminalRow {
  const rawHasColor = hasAnsiCodes(raw)
  const withColor = (kind: 'banner' | 'context' | 'asst' | 'plain' | 'attachment' | 'done' | 'error', text: string, prefixLen: number) => {
    const rawText = rawHasColor ? rawAfterVisibleChars(raw, prefixLen) : undefined
    return { kind, text, rawText } as TerminalRow
  }
  if (stripped.startsWith('$ ')) return withColor('banner', stripped.slice(2), 2)
  if (stripped.startsWith('↳ ')) return withColor('context', stripped.slice(2), 2)
  if (stripped.startsWith('> user: ')) {
    // user rows have their own preview/expand UX; rawText carried so the
    // expanded body can render in color.
    const rawText = rawHasColor ? rawAfterVisibleChars(raw, 8) : undefined
    return { kind: 'user', text: stripped.slice(8), rawText } as TerminalRow
  }
  if (stripped.startsWith('< asst: ')) return withColor('asst', stripped.slice(8), 8)
  if (stripped.startsWith('  ')) return withColor('asst', stripped.slice(2), 2)
  if (stripped.startsWith('[image ')) return withColor('attachment', stripped, 0)
  if (stripped.startsWith('[document ')) return withColor('attachment', stripped, 0)
  if (stripped.startsWith('[done')) return withColor('done', stripped, 0)
  if (stripped.startsWith('[error]')) return withColor('error', stripped.slice(7).trim(), 7)
  // Tool start: "→ tool: name(args)" or "→ name args"
  if (stripped.startsWith('→ ')) {
    const rest = stripped.slice(2)
    const toolMatch = rest.match(/^tool:\s*([^(]+)\((.*)\)$/)
    if (toolMatch) {
      return { kind: 'tool', name: toolMatch[1].trim(), args: toolMatch[2] }
    }
    const spaceIdx = rest.indexOf(' ')
    if (spaceIdx > 0) {
      return { kind: 'tool', name: rest.slice(0, spaceIdx), args: rest.slice(spaceIdx + 1) }
    }
    return { kind: 'tool', name: rest, args: '' }
  }
  return withColor('plain', stripped, 0)
}

function isTerminalRowBoundary(line: string): boolean {
  return line.startsWith('$ ') ||
    line.startsWith('↳ ') ||
    line.startsWith('> user: ') ||
    line.startsWith('< asst: ') ||
    line.startsWith('[image ') ||
    line.startsWith('[document ') ||
    line.startsWith('[done') ||
    line.startsWith('[error]') ||
    line.startsWith('→ ') ||
    /^([✓✗])\s+result\s+([^:]+):\s*(.*)$/.test(line) ||
    /^([✓✗])\s+(\S+)\s+\(([^)]+)\)\s*(.*)$/.test(line)
}

// Pair tool starts with their matching result lines. A line beginning
// "✓ result <name>:" or "✗ result <name>:" or the short "✓ <name> (<dur>) ..."
// form gets merged into the most recent tool row with the same name.
function parseTerminalContent(content: string): TerminalRow[] {
  if (!content) return []
  const lines = content.split('\n')
  const rows: TerminalRow[] = []
  let activeToolResultIndex: number | null = null
  let activeTextRowIndex: number | null = null
  for (const rawLine of lines) {
    // Each line is matched / classified against the stripped form so the
    // existing regexes keep working with colored input. rawLine is carried
    // into classifyTerminalLine to populate row.rawText for colored render.
    const line = stripAnsi(rawLine)
    // Tool result variants
    const fullResult = line.match(/^([✓✗])\s+result\s+([^:]+):\s*(.*)$/)
    if (fullResult) {
      const [, prefix, name, body] = fullResult
      activeTextRowIndex = null
      // Find the most recent tool row with this name that has no result yet
      for (let i = rows.length - 1; i >= 0; i--) {
        const row = rows[i]
        if (row.kind === 'tool' && row.name === name.trim() && !row.result) {
          row.result = body
          row.resultPrefix = prefix as '✓' | '✗'
          activeToolResultIndex = i
          break
        }
      }
      continue
    }
    const shortResult = line.match(/^([✓✗])\s+(\S+)\s+\(([^)]+)\)\s*(.*)$/)
    if (shortResult) {
      const [, prefix, name, dur, body] = shortResult
      activeTextRowIndex = null
      for (let i = rows.length - 1; i >= 0; i--) {
        const row = rows[i]
        if (row.kind === 'tool' && row.name === name && !row.result) {
          row.result = body ? `${dur} · ${body}` : dur
          row.resultPrefix = prefix as '✓' | '✗'
          activeToolResultIndex = i
          break
        }
      }
      continue
    }
    if (activeToolResultIndex !== null && !isTerminalRowBoundary(line)) {
      const activeTool = rows[activeToolResultIndex]
      if (activeTool?.kind === 'tool') {
        activeTool.result = activeTool.result ? `${activeTool.result}\n${line}` : line
        continue
      }
      activeToolResultIndex = null
    }
    if (activeTextRowIndex !== null && !isTerminalRowBoundary(line)) {
      const activeTextRow = rows[activeTextRowIndex]
      const shouldAttachToTextRow = activeTextRow?.kind === 'asst' ||
        (activeTextRow?.kind === 'user' && isAutoNotificationText(activeTextRow.text))
      if (shouldAttachToTextRow) {
        const continuation = line.startsWith('  ') ? line.slice(2) : line
        const rawContinuation = line.startsWith('  ') ? rawAfterVisibleChars(rawLine, 2) : rawLine
        activeTextRow.text = activeTextRow.text ? `${activeTextRow.text}\n${continuation}` : continuation
        if (hasAnsiCodes(rawLine) || activeTextRow.rawText !== undefined) {
          const prevRaw = activeTextRow.rawText ?? activeTextRow.text
          activeTextRow.rawText = activeTextRow.rawText
            ? `${prevRaw}\n${rawContinuation}`
            : `${prevRaw}\n${rawContinuation}`
        }
        continue
      }
      activeTextRowIndex = null
    }
    const classified = classifyTerminalLine(line, rawLine)
    activeToolResultIndex = null
    // Coalesce consecutive assistant continuation lines into one row. Join
    // with a newline (not a space) so the markdown structure that the model
    // emitted — lists, paragraph breaks, fenced code, headings — survives
    // and ReactMarkdown can render it. An empty continuation line becomes a
    // blank line, which markdown reads as a paragraph break.
    if (classified.kind === 'asst' && rows.length > 0 && rows[rows.length - 1].kind === 'asst') {
      const prev = rows[rows.length - 1] as { kind: 'asst'; text: string; rawText?: string }
      prev.text = prev.text ? `${prev.text}\n${classified.text}` : classified.text
      if (classified.rawText !== undefined || prev.rawText !== undefined) {
        const prevRaw = prev.rawText ?? prev.text
        const nextRaw = classified.rawText ?? classified.text
        prev.rawText = prev.rawText ? `${prevRaw}\n${nextRaw}` : `${prevRaw}\n${nextRaw}`
      }
      activeTextRowIndex = rows.length - 1
      continue
    }
    if (classified.kind === 'asst' && line.startsWith('  ')) {
      rows.push({ kind: 'plain', text: line, rawText: hasAnsiCodes(rawLine) ? rawLine : undefined })
      activeTextRowIndex = null
      continue
    }
    activeTextRowIndex = (
      classified.kind === 'asst' ||
      (classified.kind === 'user' && isAutoNotificationText(classified.text))
    ) ? rows.length : null
    rows.push(classified)
  }
  return rows
}

interface StructuredTerminalViewProps {
  content: string
  rows?: TerminalSnapshot['rows']
  scrollRef: React.RefObject<HTMLDivElement | null>
  onScroll: (e: React.UIEvent<HTMLDivElement>) => void
  onWheel: (e: React.WheelEvent<HTMLDivElement>) => void
  theme: TerminalTheme
  // Optional snapshot is used to render a Claude-Code-style bottom status
  // footer (model · turns · tokens · cost · elapsed) and to drive the
  // streaming spinner when the pane is still active.
  terminal?: TerminalSnapshot | null
}

const SPINNER_FRAMES = ['◐', '◓', '◑', '◒']

function useSpinnerFrame(active: boolean): string {
  const [frame, setFrame] = useState(0)
  useEffect(() => {
    if (!active) return
    const id = window.setInterval(() => {
      setFrame(f => (f + 1) % SPINNER_FRAMES.length)
    }, 110)
    return () => window.clearInterval(id)
  }, [active])
  return SPINNER_FRAMES[frame]
}

const TerminalAssistantMarkdown: React.FC<{ text: string; theme: TerminalTheme }> = ({ text, theme }) => (
  <MarkdownRenderer
    content={text}
    maxHeight="none"
    className={theme.markdown}
  />
)

// ColoredText renders ANSI-laden text via ansi_up. Memoized per input
// string so re-renders don't repeatedly invoke the parser.
//
// When `rawText` is undefined or contains no ANSI escapes, the parent
// component should render plain text directly — using this component for
// non-colored text wastes a parse pass.
const ColoredText: React.FC<{ rawText: string; className?: string }> = ({ rawText, className }) => {
  const html = useMemo(() => ansiUp.ansi_to_html(rawText), [rawText])
  return <span className={className} dangerouslySetInnerHTML={{ __html: html }} />
}

const XtermTerminalPane: React.FC<{
  content: string
  contentSource?: string
  className?: string
  contentRef: React.RefObject<HTMLDivElement | null>
  xtermTheme: ITheme
  xtermProfile: (typeof XTERM_PROFILE_OPTIONS)[TerminalColorScheme]
  onViewportStickChange?: (isNearBottom: boolean) => void
  debugLabel?: string
}> = ({ content, contentSource, className, contentRef, xtermTheme, xtermProfile, onViewportStickChange, debugLabel }) => {
  const mountRef = useRef<HTMLDivElement | null>(null)
  const terminalRef = useRef<XTerm | null>(null)
  const lastContentRef = useRef<string>('')
  const onViewportStickChangeRef = useRef(onViewportStickChange)

  useEffect(() => {
    onViewportStickChangeRef.current = onViewportStickChange
  }, [onViewportStickChange])

  const logXtermDebug = useCallback((phase: string, extra: Record<string, unknown> = {}) => {
    if (!terminalScrollDebugEnabled()) return
    const term = terminalRef.current
    const buffer = term?.buffer.active
    console.info('[TERMINAL_DEBUG] xterm', {
      phase,
      label: debugLabel,
      rows: term?.rows,
      cols: term?.cols,
      baseY: buffer?.baseY,
      viewportY: buffer?.viewportY,
      cursorY: buffer?.cursorY,
      scrollable: typeof buffer?.baseY === 'number' ? buffer.baseY > 0 : undefined,
      content_lines: terminalTextLineCount(content),
      content_bytes: content.length,
      content_source: contentSource,
      ...extra,
    })
  }, [content, contentSource, debugLabel])

  useEffect(() => {
    const mount = mountRef.current
    if (!mount) return

    const term = new XTerm({
      allowProposedApi: false,
      convertEol: true,
      cursorBlink: false,
      cursorStyle: xtermProfile.cursorStyle,
      disableStdin: true,
      fontFamily: xtermProfile.fontFamily,
      fontSize: xtermProfile.fontSize,
      lineHeight: xtermProfile.lineHeight,
      scrollback: 20000,
      theme: xtermTheme,
    })
    const fit = new FitAddon()
    term.loadAddon(fit)
    term.open(mount)
    terminalRef.current = term
    const scrollDisposable = term.onScroll(viewportY => {
      const distanceFromBottom = Math.max(0, term.buffer.active.baseY - viewportY)
      onViewportStickChangeRef.current?.(distanceFromBottom <= 1)
      logXtermDebug('scroll', { viewportY, distanceFromBottom })
    })

    const fitTerminal = () => {
      try {
        fit.fit()
        logXtermDebug('fit')
      } catch {
        // Fit can fail during unmount or while the pane is display:none.
      }
    }
    fitTerminal()
    const resizeObserver = new ResizeObserver(fitTerminal)
    resizeObserver.observe(mount)

    return () => {
      scrollDisposable.dispose()
      resizeObserver.disconnect()
      terminalRef.current = null
      term.dispose()
    }
  }, [])

  useEffect(() => {
    const term = terminalRef.current
    if (!term) return
    term.options.theme = xtermTheme
  }, [xtermTheme])

  useEffect(() => {
    const term = terminalRef.current
    if (!term) return
    term.options.fontFamily = xtermProfile.fontFamily
    term.options.fontSize = xtermProfile.fontSize
    term.options.lineHeight = xtermProfile.lineHeight
    term.options.cursorStyle = xtermProfile.cursorStyle
  }, [xtermProfile])

  const handleWheel = useCallback((event: React.WheelEvent<HTMLDivElement>) => {
    const term = terminalRef.current
    if (!term) return

    const lineHeightPx = xtermProfile.fontSize * xtermProfile.lineHeight
    const rawLines = event.deltaMode === 1
      ? event.deltaY
      : event.deltaMode === 2
        ? event.deltaY * term.rows
        : event.deltaY / Math.max(1, lineHeightPx)
    const direction = rawLines < 0 ? -1 : 1
    const lines = Math.max(1, Math.min(12, Math.ceil(Math.abs(rawLines)))) * direction

    event.preventDefault()
    event.stopPropagation()
    term.scrollLines(lines)
    const distanceFromBottom = Math.max(0, term.buffer.active.baseY - term.buffer.active.viewportY)
    onViewportStickChangeRef.current?.(distanceFromBottom <= 1)
    logXtermDebug('wheel', {
      deltaY: event.deltaY,
      deltaMode: event.deltaMode,
      computed_lines: lines,
      distanceFromBottom,
    })
  }, [logXtermDebug, xtermProfile.fontSize, xtermProfile.lineHeight])

  useEffect(() => {
    const term = terminalRef.current
    if (!term) return
    const previousContent = lastContentRef.current
    if (content === previousContent) return
    const buffer = term.buffer.active
    const distanceFromBottom = Math.max(0, buffer.baseY - buffer.viewportY)
    const shouldStickToBottom = distanceFromBottom <= 1
    const previousBaseY = buffer.baseY
    const previousViewportY = buffer.viewportY
    const appendOnly = previousContent !== '' && content.startsWith(previousContent)
    const writeContent = appendOnly ? content.slice(previousContent.length) : content
    lastContentRef.current = content
    const restoreScroll = () => {
      if (shouldStickToBottom) {
        term.scrollToBottom()
        return
      }
      const nextBaseY = term.buffer.active.baseY
      // When the user is reading older output, keep the absolute viewport line
      // stable across live refreshes. Preserving distance-from-bottom would drag
      // the viewport downward every time new accumulated screen lines append.
      term.scrollToLine(Math.max(0, Math.min(previousViewportY, nextBaseY)))
    }
    const afterWrite = () => {
      restoreScroll()
      logXtermDebug('write', {
        previousBaseY,
        previousViewportY,
        previousDistanceFromBottom: distanceFromBottom,
        restoredViewportY: term.buffer.active.viewportY,
        restoredDistanceFromBottom: Math.max(0, term.buffer.active.baseY - term.buffer.active.viewportY),
        appendOnly,
        shouldStickToBottom,
      })
    }
    if (!appendOnly) {
      term.reset()
    }
    if (!writeContent) {
      afterWrite()
      return
    }
    term.write(normalizeXtermWriteContent(writeContent, contentSource), afterWrite)
  }, [content, contentSource, logXtermDebug])

  return (
    <div
      ref={contentRef}
      className={className}
      style={{ backgroundColor: xtermTheme.background }}
      onWheel={handleWheel}
    >
      <div ref={mountRef} className="h-full w-full [&_.xterm]:h-full" />
    </div>
  )
}

function normalizeXtermWriteContent(content: string, contentSource?: string): string {
  if (contentSource === 'tmux_pipe') {
    return content
  }
  return content.replace(/\r?\n/g, '\r\n')
}

function normalizeTerminalRows(rows: TerminalSnapshot['rows'] | undefined): TerminalRow[] {
  if (!Array.isArray(rows)) return []
  const normalized: TerminalRow[] = []
  // Server-pre-parsed synthetic rows can carry ANSI. We strip ANSI for the
  // `text` field (so existing display logic and string comparisons stay clean)
  // but stash the colored original in `rawText` so the structured renderer can
  // colorize via ansi_up. Real tmux panes do not use this path.
  for (const row of rows) {
    switch (row.kind) {
      case 'banner':
      case 'context':
      case 'user':
      case 'asst':
      case 'attachment':
      case 'done':
      case 'error':
      case 'plain': {
        const raw = row.text || ''
        const stripped = hasAnsiCodes(raw) ? stripAnsi(raw) : raw
        const rawText = hasAnsiCodes(raw) ? raw : undefined
        normalized.push({ kind: row.kind, text: stripped, rawText } as TerminalRow)
        break
      }
      case 'tool':
        normalized.push({
          kind: 'tool',
          // tool name / args / result come from server-side regex captures —
          // they're already stripped on the backend and don't currently
          // carry color. Leave them as plain text.
          name: row.name || 'tool',
          args: row.args || '',
          result: row.result,
          resultPrefix: row.result_prefix === '✗' ? '✗' : row.result_prefix === '✓' ? '✓' : undefined,
          result_prefix: row.result_prefix,
        })
        break
      default: {
        const raw = row.text || ''
        const stripped = hasAnsiCodes(raw) ? stripAnsi(raw) : raw
        const rawText = hasAnsiCodes(raw) ? raw : undefined
        normalized.push({ kind: 'plain', text: stripped, rawText })
      }
    }
  }
  return normalized
}

function normalizeTerminalWorkflowPath(path?: string | null): string {
  return (path || '')
    .replace(/^\/data\/docs\//, '')
    .replace(/\/+$/, '')
}

// workflowMatchKey reduces a path to its workflow identity — the segment after
// "Workflow/" (lowercased), or the last path segment as a fallback. Matching on
// this is robust to the many forms the same workflow's path takes (relative vs
// absolute, "/data/docs/..." vs "...workspace-docs/...", trailing slashes, case),
// which the previous full-path endsWith comparison was not — that fragility hid
// a resumed session's terminals when the active preset path and the terminal's
// workflow_path described the same workflow in different forms.
function workflowMatchKey(path?: string | null): string {
  const parts = normalizeTerminalWorkflowPath(path).replace(/\\/g, '/').split('/').filter(Boolean)
  const wfIdx = parts.findIndex(p => p.toLowerCase() === 'workflow')
  const name = wfIdx >= 0 && parts[wfIdx + 1] ? parts[wfIdx + 1] : parts[parts.length - 1]
  return (name || '').toLowerCase()
}

function terminalMatchesWorkflow(terminal: TerminalSnapshot, workflowPath?: string | null): boolean {
  const target = workflowMatchKey(workflowPath)
  if (!target) return true
  const terminalKey = workflowMatchKey(terminal.workflow_path)
  if (!terminalKey) return false
  return terminalKey === target
}

function formatTokens(n?: number): string {
  if (n === undefined || n === null) return '–'
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`
  return String(n)
}

function formatStatusFooterCost(usd?: number): string {
  if (usd === undefined || usd === null || usd === 0) return ''
  return `$${formatCost(usd)}`
}

const StructuredTerminalView: React.FC<StructuredTerminalViewProps> = ({ content, rows: structuredRows, scrollRef, onScroll, onWheel, terminal, theme }) => {
  const rows = useMemo(() => {
    const normalizedRows = normalizeTerminalRows(structuredRows)
    return normalizedRows.length > 0 ? normalizedRows : parseTerminalContent(content)
  }, [content, structuredRows])
  // Long user prompts and tool rows collapse behind one-line summaries.
  // We key by row index; content updates are append-only so indexes stay
  // stable for existing rows.
  const [expanded, setExpanded] = useState<Set<number>>(new Set())
  const toggle = useCallback((idx: number) => {
    setExpanded(prev => {
      const next = new Set(prev)
      if (next.has(idx)) next.delete(idx)
      else next.add(idx)
      return next
    })
  }, [])
  const isStreaming = !!terminal?.active && (terminal.state === 'running' || terminal.state === 'idle' || terminal.state === undefined)
  const spinner = useSpinnerFrame(isStreaming)
  return (
    <div
      ref={scrollRef}
      onScroll={onScroll}
      onWheel={onWheel}
      className={`min-w-0 flex-1 overflow-y-auto overflow-x-hidden overscroll-contain px-3 py-2.5 font-mono bg-[#0b0d0c] ${theme.contentText} ${theme.selection}`}
    >
      {rows.map((row, idx) => {
        // Subtle divider between turns: a new banner (other than the first
        // row) marks a fresh turn boundary in the aggregated scrollback.
        const showDivider = row.kind === 'banner' && idx > 0
        switch (row.kind) {
          case 'banner':
            return (
              <div key={idx}>
                {showDivider && <div className="my-2 border-t border-dashed border-neutral-700/60" />}
                <div className={theme.prompt}>
                  <span className="text-neutral-500">$ </span>
                  {row.rawText !== undefined
                    ? <ColoredText rawText={row.rawText} />
                    : row.text}
                </div>
              </div>
            )
          case 'context':
            return (
              <div key={idx} className="text-neutral-500">
                ↳ {row.rawText !== undefined
                  ? <ColoredText rawText={row.rawText} />
                  : row.text}
              </div>
            )
          case 'user':
            {
              const message = terminalUserMessageMeta(row.text)
              const longUserMessage = shouldCollapseUserMessage(message.body)
              const isOpen = expanded.has(idx) || !longUserMessage
              const preview = compactTerminalPreview(message.body)
              const labelClass = message.isAuto ? theme.userAuto : theme.user
              return (
                <div key={idx} className="my-0.5">
                  {longUserMessage ? (
                    <>
                      <button
                        type="button"
                        onClick={() => toggle(idx)}
                        className="group flex w-full min-w-0 items-start gap-1.5 rounded px-0.5 py-0.5 text-left hover:bg-white/[0.04]"
                      >
                        <span className="shrink-0 text-neutral-500">&gt;</span>
                        <span className={`shrink-0 font-semibold ${labelClass}`}>{message.label}</span>
                        <span className="min-w-0 flex-1 truncate text-neutral-300">
                          {isOpen ? 'message' : preview}
                        </span>
                        <span className="shrink-0 font-mono text-[10px] text-neutral-600 group-hover:text-neutral-400">
                          {isOpen ? '[-]' : '[+]'}
                        </span>
                      </button>
                      {isOpen && (
                        <pre className={`ml-5 border-l border-neutral-800/80 pl-2 whitespace-pre-wrap break-words [overflow-wrap:anywhere] font-mono text-neutral-200 ${theme.contentText}`}>
                          {message.body}
                        </pre>
                      )}
                    </>
                  ) : (
                    <div className="flex min-w-0 items-start gap-1.5 whitespace-pre-wrap break-words px-0.5 py-0.5 text-neutral-200">
                      <span className="shrink-0 text-neutral-500">&gt;</span>
                      <span className={`shrink-0 font-semibold ${labelClass}`}>{message.label}</span>
                      <span className="min-w-0 flex-1">{message.body}</span>
                    </div>
                  )}
                </div>
              )
            }
          case 'asst':
            return (
              <div key={idx} className="my-1.5">
                <div className={`mb-0.5 flex items-center gap-1.5 px-0.5 ${theme.assistantLabelText}`}>
                  <span className="text-neutral-500">&lt;</span>
                  <span className={`font-semibold ${theme.assistant}`}>AI</span>
                </div>
                <div className={`border-l border-neutral-700/70 pl-3 ${theme.assistantBodyText}`}>
                  <TerminalAssistantMarkdown text={row.text} theme={theme} />
                </div>
              </div>
            )
          case 'tool': {
            const hasResult = row.result !== undefined
            const isError = row.resultPrefix === '✗'
            const longResult = (row.result?.length ?? 0) > 80
            const isOpen = expanded.has(idx)
            const argsDetail = row.args ? formatToolDetail(row.args) : null
            const resultDetail = hasResult ? formatToolDetail(row.result || '') : null
            const hasJsonDetail = !!(argsDetail?.isJson || resultDetail?.isJson)
            const statusColor = !hasResult
              ? theme.toolPending
              : isError
                ? 'text-red-400'
                : 'text-neutral-500'
            return (
              <div key={idx} className={`my-px ${theme.toolText}`}>
                <button
                  type="button"
                  onClick={() => toggle(idx)}
                  className="w-full rounded px-0.5 -mx-0.5 text-left text-neutral-500 hover:bg-white/[0.035] hover:text-neutral-300"
                >
                  <span className={statusColor}>
                    {hasResult ? (isError ? '✗' : '·') : '→'}
                  </span>
                  <span className="ml-1 text-neutral-400">{row.name}</span>
                  {hasJsonDetail && (
                    <Braces className="ml-1 inline h-3 w-3 align-[-2px] text-neutral-500" aria-hidden />
                  )}
                  {row.args && (
                    <span className="ml-1 text-neutral-600">
                      ({compactTerminalPreview(row.args, TERMINAL_TOOL_ARGS_PREVIEW_CHARS)})
                    </span>
                  )}
                  <span className="text-neutral-600 ml-1">{isOpen ? '▾' : '▸'}</span>
                </button>
                {isOpen && (argsDetail || resultDetail) && (
                  <div className="ml-3 mt-1 space-y-1 border-l border-neutral-800/80 pl-2">
                    {argsDetail && (
                      <div>
                        <div className="mb-0.5 text-[10px] uppercase tracking-wide text-neutral-600">
                          args{argsDetail.isJson ? ' · json' : ''}
                          {argsDetail.isJson && <Braces className="ml-1 inline h-3 w-3 align-[-2px]" aria-hidden />}
                        </div>
                        <pre className="max-h-72 overflow-auto rounded border border-neutral-800/80 bg-black/25 p-2 whitespace-pre-wrap break-words [overflow-wrap:anywhere] text-neutral-300">
                          {argsDetail.text}
                        </pre>
                      </div>
                    )}
                    {resultDetail && (
                      <div>
                        <div className={`mb-0.5 text-[10px] uppercase tracking-wide ${isError ? 'text-red-400/70' : 'text-neutral-600'}`}>
                          {isError ? 'error' : 'result'}{resultDetail.isJson ? ' · json' : ''}
                          {resultDetail.isJson && <Braces className="ml-1 inline h-3 w-3 align-[-2px]" aria-hidden />}
                        </div>
                        <pre className={`max-h-72 overflow-auto rounded border p-2 whitespace-pre-wrap break-words [overflow-wrap:anywhere] ${isError ? 'border-red-900/45 bg-red-950/15 text-red-200' : 'border-neutral-800/80 bg-black/25 text-neutral-400'}`}>
                          {resultDetail.text}
                        </pre>
                      </div>
                    )}
                  </div>
                )}
                {!isOpen && hasResult && !longResult && row.result && (
                  <div className={`ml-1 flex whitespace-pre-wrap break-words ${isError ? 'text-red-300' : 'text-neutral-500'}`}>
                    <span className="text-neutral-600 select-none mr-1">└─</span>
                    <span className="flex-1">{row.result}</span>
                  </div>
                )}
              </div>
            )
          }
          case 'attachment':
            return (
              <div key={idx} className="text-neutral-500">
                {row.rawText !== undefined ? <ColoredText rawText={row.rawText} /> : row.text}
              </div>
            )
          case 'done':
            return (
              <div key={idx} className={`${theme.done} mt-1 ${theme.doneText} font-mono`}>
                {row.rawText !== undefined ? <ColoredText rawText={row.rawText} /> : row.text}
              </div>
            )
          case 'error':
            return (
              <div key={idx} className="text-red-400">
                [error] {row.rawText !== undefined ? <ColoredText rawText={row.rawText} /> : row.text}
              </div>
            )
          case 'plain':
            return (
              <div key={idx} className="text-neutral-300 whitespace-pre-wrap break-words">
                {row.rawText !== undefined ? <ColoredText rawText={row.rawText} /> : row.text}
              </div>
            )
        }
      })}
      {isStreaming && (
        <div className={`mt-1 font-mono ${theme.streaming}`} aria-label="Running">
          {spinner}
        </div>
      )}
    </div>
  )
}

function isSyntheticTerminal(terminal: TerminalSnapshot): boolean {
  const transport = (terminal.step_transport || '').toLowerCase()
  if (transport === 'tmux') return false
  if (transport === 'api' || transport === 'structured' || transport === 'structured_cli' || transport === 'non_tmux') return true
  if (terminal.tmux_session) return false
  // Retained tmux snapshots can lose tmux_session after the pane is removed.
  // If the stored content still carries terminal escape sequences, keep the
  // xterm renderer so colors, alternate-screen redraws, and terminal spacing
  // remain close to the live pane.
  if (hasAnsiCodes(terminal.content || '')) return false
  return true
}

function terminalTmuxDetailOptions(terminal: TerminalSnapshot, displayDetail = false): TerminalDetailOptions | undefined {
  if (!terminal.tmux_session) return undefined
  const state = terminalState(terminal)
  if (terminal.active && state !== 'stale' && state !== 'failed') {
    const lines = displayDetail
      ? TERMINAL_ACTIVE_DISPLAY_HISTORY_LINES
      : isMainAgentTerminal(terminal)
        ? TERMINAL_ACTIVE_RAIL_MAIN_HISTORY_LINES
        : TERMINAL_ACTIVE_RAIL_STEP_HISTORY_LINES
    return {
      content: 'screen',
      lines,
    }
  }
  return { content: 'history' }
}

function terminalScrollDebugEnabled(): boolean {
  if (typeof window === 'undefined') return false
  try {
    const stored = window.localStorage.getItem(TERMINAL_SCROLL_DEBUG_STORAGE_KEY)
    if (stored && ['1', 'true', 'yes', 'on'].includes(stored.toLowerCase())) return true
    const query = new URLSearchParams(window.location.search)
    return query.get('terminal_debug') === '1'
  } catch {
    return false
  }
}

function withTerminalScrollDebug(options: TerminalDetailOptions | undefined, debugSource: string): TerminalDetailOptions | undefined {
  if (!terminalScrollDebugEnabled()) return options
  return {
    ...(options || {}),
    debug: true,
    debugSource,
  }
}

function terminalRequestOptions(terminal: TerminalSnapshot, displayDetail: boolean, debugSource: string): TerminalDetailOptions | undefined {
  return withTerminalScrollDebug(terminalTmuxDetailOptions(terminal, displayDetail), debugSource)
}

function terminalTextLineCount(content?: string): number {
  if (!content) return 0
  return content.split(/\n/).length
}

function logTerminalScrollDebug(
  debugSource: string,
  requestTerminal: TerminalSnapshot | undefined,
  options: TerminalDetailOptions | undefined,
  detail?: TerminalSnapshot,
): void {
  if (!terminalScrollDebugEnabled()) return
  const terminal = detail || requestTerminal
  const payload = {
    source: debugSource,
    terminal_id: terminal?.terminal_id,
    tmux_session: terminal?.tmux_session,
    active: terminal?.active,
    state: terminal ? terminalState(terminal) : undefined,
    chunk_index: terminal?.chunk_index,
    requested_content: options?.content || 'stored',
    requested_lines: options?.lines,
    content_lines: terminalTextLineCount(detail?.content),
    content_bytes: detail?.content?.length || 0,
    row_count: Array.isArray(detail?.rows) ? detail.rows.length : undefined,
  }
  console.info('[TERMINAL_DEBUG] frontend detail', payload)
}

// Error event types that should surface as a banner above the terminal
// pane. Tree view renders these as their own cards; in Terminal mode
// they would otherwise be invisible because the rail only shows pane
// content + synthetic terminal chunks.
const TERMINAL_ERROR_EVENT_TYPES = new Set<string>([
  'orchestrator_agent_error',
  'background_agent_failed',
  'conversation_error',
  'workflow_error',
  'agent_error',
  'tool_call_error',
  'llm_generation_error',
  'context_cancelled',
  'batch_execution_canceled',
])

interface TerminalErrorBannerEntry {
  id: string
  type: string
  message: string
  timestamp?: string
  terminalID?: string
}

const TERMINAL_ERROR_MESSAGE_LIMIT = 220

function compactTerminalErrorMessage(message: string): string {
  const singleLine = message.replace(/\s+/g, ' ').trim()
  if (singleLine.length <= TERMINAL_ERROR_MESSAGE_LIMIT) return singleLine
  return `${singleLine.slice(0, TERMINAL_ERROR_MESSAGE_LIMIT)}...`
}

function extractErrorMessage(event: unknown): string {
  const e = event as { type?: string; data?: unknown }
  const data = e?.data as { data?: Record<string, unknown>; message?: string; error?: string } | undefined
  const payload = (data?.data && typeof data.data === 'object') ? data.data : (data as Record<string, unknown> | undefined)
  if (!payload) return ''
  for (const key of ['error', 'message', 'detail', 'reason']) {
    const v = payload[key]
    if (typeof v === 'string' && v.trim()) return v
  }
  return ''
}

function eventErrorParts(event: PollingEvent): {
  eventRecord: Record<string, unknown>
  data?: Record<string, unknown>
  inner?: Record<string, unknown>
  metadata?: Record<string, unknown>
} {
  const eventRecord = event as unknown as Record<string, unknown>
  const data = asRecord(event.data)
  const inner = asRecord(data?.data)
  const metadata = asRecord(inner?.metadata) || asRecord(data?.metadata) || asRecord(eventRecord.metadata)
  return { eventRecord, data, inner, metadata }
}

function collectStringFields(...values: unknown[]): string[] {
  const out: string[] = []
  for (const value of values) {
    const text = stringField(value)
    if (text) out.push(text)
  }
  return out
}

function normalizeTerminalOwnerCandidate(value: string): string {
  let trimmed = value.trim()
  for (const prefix of ['delegation:', 'workflow:', 'background:', 'agent:', 'batch:']) {
    if (trimmed.startsWith(prefix)) {
      trimmed = trimmed.slice(prefix.length).trim()
      break
    }
  }
  return trimmed
}

function resolveErrorTerminal(event: PollingEvent, terminals: TerminalSnapshot[]): TerminalSnapshot | undefined {
  const { eventRecord, data, inner, metadata } = eventErrorParts(event)
  const exactTerminalIDs = collectStringFields(
    eventRecord.terminal_id,
    data?.terminal_id,
    inner?.terminal_id,
    metadata?.terminal_id,
  )
  for (const terminalID of exactTerminalIDs) {
    const matched = terminals.find(terminal => terminal.terminal_id === terminalID)
    if (matched) return matched
  }

  const tmuxSessions = collectStringFields(
    eventRecord.tmux_session,
    data?.tmux_session,
    inner?.tmux_session,
    metadata?.tmux_session,
    metadata?.tmux_session_name,
    metadata?.claude_code_interactive_session,
    metadata?.codex_interactive_session,
    metadata?.gemini_interactive_session, // Known limitation: Gemini CLI (Ink-based) redraws in place, so its tmux pane has no scrollback — use Gemini's in-app scroll keys.
    metadata?.cursor_interactive_session,
  )
  for (const tmuxSession of tmuxSessions) {
    const matched = terminals.find(terminal => terminal.tmux_session === tmuxSession)
    if (matched) return matched
  }

  const ownerCandidates = collectStringFields(
    eventRecord.execution_id,
    eventRecord.parent_execution_id,
    eventRecord.correlation_id,
    data?.execution_id,
    data?.parent_execution_id,
    data?.correlation_id,
    data?.delegation_id,
    data?.background_agent_id,
    data?.agent_id,
    inner?.execution_id,
    inner?.parent_execution_id,
    inner?.correlation_id,
    inner?.delegation_id,
    inner?.background_agent_id,
    inner?.agent_id,
    metadata?.execution_id,
    metadata?.owner_execution_id,
    metadata?.execution_owner_id,
    metadata?.parent_execution_id,
    metadata?.background_agent_id,
    metadata?.delegation_id,
    metadata?.agent_id,
    metadata?.workshop_step_id,
    metadata?.current_step_id,
    metadata?.orchestrator_step_id,
    metadata?.workflow_step_id,
    metadata?.step_id,
  ).map(normalizeTerminalOwnerCandidate)

  for (const candidate of ownerCandidates) {
    const matched = terminals.find(terminal =>
      terminal.terminal_id === candidate ||
      terminal.owner_id === candidate ||
      terminal.execution_id === candidate ||
      terminal.step_id === candidate ||
      `${terminal.session_id}:${candidate}` === terminal.terminal_id ||
      (candidate.startsWith('main:') && isMainAgentTerminal(terminal)),
    )
    if (matched) return matched
  }
  return undefined
}

const DISMISSED_TERMINAL_ERRORS_KEY_PREFIX = 'terminal-dismissed-errors:'

function dismissedTerminalErrorsKey(sessionId?: string): string | null {
  return sessionId ? `${DISMISSED_TERMINAL_ERRORS_KEY_PREFIX}${sessionId}` : null
}

function readDismissedTerminalErrorIDs(sessionId?: string): Set<string> {
  const key = dismissedTerminalErrorsKey(sessionId)
  if (!key) return new Set()
  try {
    const parsed = JSON.parse(window.localStorage.getItem(key) || '[]')
    return new Set(Array.isArray(parsed) ? parsed.filter((item): item is string => typeof item === 'string') : [])
  } catch {
    return new Set()
  }
}

function writeDismissedTerminalErrorIDs(sessionId: string | undefined, ids: Set<string>) {
  const key = dismissedTerminalErrorsKey(sessionId)
  if (!key) return
  try {
    window.localStorage.setItem(key, JSON.stringify(Array.from(ids).slice(-100)))
  } catch {
    // Best-effort UI preference only.
  }
}

export const TerminalCenter: React.FC<TerminalCenterProps> = ({ currentSessionId, compact, hasConversationActivity = false }) => {
  // terminalCenterOpen was the legacy toggle gate (separate sidekick
  // panel); kept here for any callers that still pass the flag but no
  // longer affects rendering — Debug-mode mount is the only gate.
  // Scope terminals to the current chat session. The workflowEventBridge
  // adds every workflow-step event under the chat tab's sessionID, so
  // filtering by currentSessionId surfaces this chat's workflow steps
  // without leaking terminals from other chat tabs / unrelated workflows.
  const [viewAll, setViewAll] = useState(false)
  const [terminals, setTerminals] = useState<TerminalSnapshot[]>([])
  // archivedTurnContents caches `:turn-N` snapshot bodies so we can stitch
  // prior turns into the live synthetic terminal's scrollback without
  // refetching on every render. Archived turns are immutable once written,
  // so the cache is correct without invalidation.
  const [archivedTurnContents, setArchivedTurnContents] = useState<Record<string, string>>({})
  const [terminalDetailCache, setTerminalDetailCache] = useState<Record<string, TerminalSnapshot>>({})
  const [selectedID, setSelectedID] = useState<string | null>(null)
  const [userSelectedID, setUserSelectedID] = useState<string | null>(null)
  const [copiedTerminalID, setCopiedTerminalID] = useState<string | null>(null)
  const [dismissedTerminalIDs, setDismissedTerminalIDs] = useState<Set<string>>(() => new Set())
  const [dismissedRouteIDs, setDismissedRouteIDs] = useState<Set<string>>(() => new Set())
  const [dismissedErrorIDs, setDismissedErrorIDs] = useState<Set<string>>(() => readDismissedTerminalErrorIDs(currentSessionId))
  const [expandedErrorIDs, setExpandedErrorIDs] = useState<Set<string>>(() => new Set())
  const [terminalColorScheme, setTerminalColorScheme] = useState<TerminalColorScheme>(() => readStoredTerminalColorScheme())
  // Slim each agent rail card to one line whenever the report/plan pane is up
  // (chat sits in the side-by-side split). When the workspace pane is hidden and
  // chat is full-width, the cards render with their full meta row. NOTE: this is
  // deliberately NOT keyed off focusedPane — clicking into the chat input must
  // not resize the agent tree; only opening/closing the report/plan view does.
  const slimAgentRail = useWorkflowStore(s => s.showChatArea && s.showWorkspacePane)
  // Manual width override for the agent rail: null = follow the auto narrow
  // behavior (slim in the report/plan rail), true/false = user-pinned narrow/wide.
  // A toggle in the rail controls lets the user resize it themselves.
  const [railManualNarrow, setRailManualNarrow] = useState<boolean | null>(null)
  const railNarrow = railManualNarrow !== null ? railManualNarrow : slimAgentRail
  const [terminalRailFilter, setTerminalRailFilter] = useState<TerminalRailFilter>('running')
  // Sub-agent terminals are shown by default (the +/- toggle hides them on
  // demand). In the narrow report/plan rail the cards are *slimmed* (see
  // slimAgentRail) rather than hidden.
  const [terminalRailMinimized, setTerminalRailMinimized] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [terminalActionBusy, setTerminalActionBusy] = useState<string | null>(null)
  const [debugPanelOpenForID, setDebugPanelOpenForID] = useState<string | null>(null)
  const [debugText, setDebugText] = useState<string>('')
  const debugMenuRef = useRef<HTMLDivElement | null>(null)

  const activeWorkflowPath = useGlobalPresetStore(state => {
    const activeWorkflowId = state.activePresetIds.workflow
    if (!activeWorkflowId) return null
    return state.workflowPresets.find(preset => preset.id === activeWorkflowId)?.selectedFolder?.filepath ?? null
  })
  const isWorkflowTerminalContext = useChatStore(state => {
    if (!currentSessionId) return false
    return Object.values(state.chatTabs).some(tab =>
      tab.sessionId === currentSessionId &&
      tab.metadata?.mode === 'workflow'
    )
  })
  const terminalWorkflowPathFilter = isWorkflowTerminalContext ? activeWorkflowPath : null

  const sessionEvents = useChatStore(state => (
    currentSessionId ? state.tabEvents[currentSessionId] : undefined
  ))
  useEffect(() => {
    setDismissedErrorIDs(readDismissedTerminalErrorIDs(currentSessionId))
  }, [currentSessionId])

  const dismissTerminalError = useCallback((errorID: string) => {
    setDismissedErrorIDs(prev => {
      const next = new Set(prev)
      next.add(errorID)
      writeDismissedTerminalErrorIDs(currentSessionId, next)
      return next
    })
  }, [currentSessionId])

  const updateTerminalColorScheme = useCallback((scheme: TerminalColorScheme) => {
    setTerminalColorScheme(scheme)
    writeStoredTerminalColorScheme(scheme)
  }, [])

  const toggleTerminalError = useCallback((errorID: string) => {
    setExpandedErrorIDs(prev => {
      const next = new Set(prev)
      if (next.has(errorID)) {
        next.delete(errorID)
      } else {
        next.add(errorID)
      }
      return next
    })
  }, [])

  const routingDecisions = useMemo(() => {
    const byKey = new Map<string, RoutingDecision>()
    for (const event of sessionEvents || []) {
      const decision = routingDecisionFromEvent(event)
      if (decision && dismissedRouteIDs.has(decision.id)) continue
      if (!decision) continue
      const key = routeDecisionDedupeKey(decision)
      const existing = byKey.get(key)
      if (!existing || routingDecisionTime(decision) >= routingDecisionTime(existing)) {
        byKey.set(key, decision)
      }
    }
    return Array.from(byKey.values()).sort((a, b) => routingDecisionTime(a) - routingDecisionTime(b))
  }, [sessionEvents, dismissedRouteIDs])
  const routingDecisionByNextStepID = useMemo(() => {
    const byStep = new Map<string, RoutingDecision>()
    for (const decision of routingDecisions) {
      if (!decision.nextStepId || decision.nextStepId === 'end') continue
      byStep.set(decision.nextStepId, decision)
    }
    return byStep
  }, [routingDecisions])
  // Surface error events (llm_generation_error, context_cancelled, etc.)
  // on the terminal that caused them when the event carries enough
  // identity. Only unscoped errors stay in the global banner.
  //
  // CAUTION: the zustand selector returns a value compared by reference.
  // Build the list with useMemo over a narrowly-selected events array
  // so a re-derived list with the same content doesn't trigger an
  // infinite render loop (a previous version returned a fresh [] every
  // call, which Zustand saw as "changed" → re-render → repeat).
  const terminalErrorGroups = useMemo<{
    global: TerminalErrorBannerEntry[]
    byTerminalID: Map<string, TerminalErrorBannerEntry[]>
  }>(() => {
    const byTerminalID = new Map<string, TerminalErrorBannerEntry[]>()
    const global: TerminalErrorBannerEntry[] = []
    if (!sessionEvents || sessionEvents.length === 0) return { global, byTerminalID }
    const seen = new Set<string>()
    for (let i = sessionEvents.length - 1; i >= 0; i--) {
      const evt = sessionEvents[i] as unknown as { id?: string; type?: string; timestamp?: string }
      if (!evt?.type || !TERMINAL_ERROR_EVENT_TYPES.has(evt.type)) continue
      const id = evt.id || `${evt.type}-${i}`
      if (dismissedErrorIDs.has(id)) continue
      const message = extractErrorMessage(evt) || evt.type.replace(/_/g, ' ')
      const terminal = resolveErrorTerminal(sessionEvents[i], terminals)
      const terminalID = terminal ? terminalPaneKey(terminal) : undefined
      const dedupeKey = `${terminalID || 'global'}:${evt.type}:${compactTerminalErrorMessage(message)}`
      if (seen.has(dedupeKey)) continue
      seen.add(dedupeKey)
      const entry = { id, type: evt.type, message, timestamp: evt.timestamp, terminalID }
      if (terminalID) {
        const items = byTerminalID.get(terminalID) || []
        if (items.length < 2) {
          items.push(entry)
          byTerminalID.set(terminalID, items)
        }
      } else if (global.length < 3) {
        global.push(entry)
      }
      if (global.length >= 3 && byTerminalID.size >= terminals.length) {
        break
      }
    }
    return { global, byTerminalID }
  }, [sessionEvents, dismissedErrorIDs, terminals])
  const sessionErrorBanner = terminalErrorGroups.global
  const terminalErrorsByID = terminalErrorGroups.byTerminalID
  const terminalOutputRef = useRef<HTMLElement | null>(null)
  const terminalAutoScrollRef = useRef(true)
  const terminalManualScrollLockRef = useRef(false)
  const selectedTerminalIDRef = useRef<string | null>(null)
  const fetchInFlightRef = useRef(false)
  const fetchInFlightScopeRef = useRef<string | null>(null)
  const fetchRequestSeqRef = useRef(0)
  const detailRequestSeqRef = useRef(0)
  const terminalsRef = useRef<TerminalSnapshot[]>([])
  const terminalDetailCacheRef = useRef<Record<string, TerminalSnapshot>>({})
  const probeInFlightRef = useRef(false)
  const emptyResponseCountRef = useRef(0)
  const lastFetchScopeRef = useRef<string | null>(null)
  const fastPollUntilRef = useRef(0)
  const fastPollIntervalRef = useRef<number | null>(null)
  const terminalTheme = TERMINAL_THEMES[terminalColorScheme]

  useEffect(() => {
    setTerminals([])
    setSelectedID(null)
    setUserSelectedID(null)
    selectedTerminalIDRef.current = null
    terminalAutoScrollRef.current = true
    terminalManualScrollLockRef.current = false
  }, [currentSessionId])

  useEffect(() => {
    terminalsRef.current = terminals
  }, [terminals])

  useEffect(() => {
    terminalDetailCacheRef.current = terminalDetailCache
  }, [terminalDetailCache])

  // Insert a freshly-fetched terminal detail into the LRU cache. Keyed by
  // terminalDetailCacheKey (id:chunk_index:updated_at), so an identical key is a
  // no-op — the body is already current.
  const cacheTerminalDetail = useCallback((detail: TerminalSnapshot) => {
    setTerminalDetailCache(current => {
      const key = terminalDetailCacheKey(detail)
      if (current[key]) return current
      const next: Record<string, TerminalSnapshot> = { ...current, [key]: detail }
      const entries = Object.entries(next)
      if (entries.length <= TERMINAL_DETAIL_CACHE_LIMIT) return next
      return Object.fromEntries(entries.slice(entries.length - TERMINAL_DETAIL_CACHE_LIMIT)) as Record<string, TerminalSnapshot>
    })
  }, [])

  const copyTerminalDebug = useCallback(async (terminal: TerminalSnapshot) => {
    await navigator.clipboard.writeText(terminalDebugText(terminal))
    setCopiedTerminalID(terminal.terminal_id)
    window.setTimeout(() => setCopiedTerminalID(current => current === terminal.terminal_id ? null : current), 1500)
  }, [])

  const applyTerminalSnapshotUpdate = useCallback((updated: TerminalSnapshot) => {
    setTerminals(current => current.map(item => (
      item.terminal_id === updated.terminal_id ? { ...item, ...updated } : item
    )))
    setTerminalDetailCache(current => {
      const key = terminalDetailCacheKey(updated)
      const next = Object.fromEntries(
        Object.entries(current).filter(([, detail]) => detail.terminal_id !== updated.terminal_id),
      ) as Record<string, TerminalSnapshot>
      next[key] = updated
      return next
    })
  }, [])

  const forceCompleteTerminal = useCallback(async (terminal: TerminalSnapshot) => {
    if (!canForceCompleteTerminal(terminal)) return
    const optimistic: TerminalSnapshot = {
      ...terminal,
      active: false,
      state: 'completed',
      closes_at: undefined,
      retention_seconds: 0,
      updated_at: new Date().toISOString(),
    }
    applyTerminalSnapshotUpdate(optimistic)
    setTerminalActionBusy('complete')
    try {
      const updated = await agentApi.completeTerminal(terminal.terminal_id)
      applyTerminalSnapshotUpdate(updated)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to mark terminal complete')
    } finally {
      setTerminalActionBusy(current => current === 'complete' ? null : current)
    }
  }, [applyTerminalSnapshotUpdate])

  const forceFailTerminal = useCallback(async (terminal: TerminalSnapshot) => {
    const optimistic: TerminalSnapshot = {
      ...terminal,
      active: false,
      state: 'failed',
      closes_at: undefined,
      retention_seconds: 0,
      updated_at: new Date().toISOString(),
    }
    applyTerminalSnapshotUpdate(optimistic)
    setTerminalActionBusy('fail')
    try {
      const updated = await agentApi.failTerminal(terminal.terminal_id)
      applyTerminalSnapshotUpdate(updated)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to mark terminal failed')
    } finally {
      setTerminalActionBusy(current => current === 'fail' ? null : current)
    }
  }, [applyTerminalSnapshotUpdate])

  const refreshTerminalSnapshot = useCallback(async (terminal: TerminalSnapshot) => {
    if (!canSendTerminalDebugInput(terminal)) return
    setTerminalActionBusy('refresh')
    try {
      const updated = await agentApi.refreshTerminal(terminal.terminal_id, { lines: TERMINAL_REFRESH_HISTORY_LINES })
      applyTerminalSnapshotUpdate(updated)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to refresh terminal')
    } finally {
      setTerminalActionBusy(current => current === 'refresh' ? null : current)
    }
  }, [applyTerminalSnapshotUpdate])

  const killTerminalSession = useCallback(async (terminal: TerminalSnapshot) => {
    if (!canSendTerminalDebugInput(terminal)) return
    const confirmed = window.confirm(`Kill tmux session ${terminal.tmux_session}? This stops the underlying coding agent process.`)
    if (!confirmed) return
    setTerminalActionBusy('kill')
    try {
      const updated = await agentApi.killTerminal(terminal.terminal_id)
      applyTerminalSnapshotUpdate(updated)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to kill terminal tmux session')
    } finally {
      setTerminalActionBusy(current => current === 'kill' ? null : current)
    }
  }, [applyTerminalSnapshotUpdate])

  const copyTerminalPaneText = useCallback(async (terminal: TerminalSnapshot) => {
    await navigator.clipboard.writeText(terminal.content || '')
    setCopiedTerminalID(terminal.terminal_id)
    window.setTimeout(() => setCopiedTerminalID(current => current === terminal.terminal_id ? null : current), 1500)
  }, [])

  const copyTmuxAttachCommand = useCallback(async (terminal: TerminalSnapshot) => {
    if (!terminal.tmux_session) return
    await navigator.clipboard.writeText(`tmux attach -t ${shellQuote(terminal.tmux_session)}`)
    setCopiedTerminalID(terminal.terminal_id)
    window.setTimeout(() => setCopiedTerminalID(current => current === terminal.terminal_id ? null : current), 1500)
  }, [])

  const sendTerminalDebugKey = useCallback(async (terminal: TerminalSnapshot, key: TerminalDebugKey) => {
    if (!canSendTerminalDebugInput(terminal)) return
    setTerminalActionBusy(key)
    try {
      await agentApi.sendTerminalKey(terminal.terminal_id, key)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : `Failed to send ${key}`)
    } finally {
      setTerminalActionBusy(current => current === key ? null : current)
    }
  }, [])

  const sendTerminalDebugText = useCallback(async (terminal: TerminalSnapshot, text: string, submit: boolean) => {
    if (!canSendTerminalDebugInput(terminal) || !text.trim()) return
    setTerminalActionBusy('send-text')
    try {
      await agentApi.sendTerminalInput(terminal.terminal_id, text, submit)
      setDebugText('')
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to send text')
    } finally {
      setTerminalActionBusy(current => current === 'send-text' ? null : current)
    }
  }, [])

  const toggleDebugPanel = useCallback((terminal: TerminalSnapshot) => {
    const key = terminalPaneKey(terminal)
    setSelectedID(key)
    setUserSelectedID(key)
    terminalAutoScrollRef.current = true
    terminalManualScrollLockRef.current = false
    setDebugPanelOpenForID(current => current === key ? null : key)
  }, [])

  // Dismiss the debug action menu on outside click / Escape.
  useEffect(() => {
    if (!debugPanelOpenForID) return
    const handleMouseDown = (event: MouseEvent) => {
      if (debugMenuRef.current?.contains(event.target as Node)) return
      setDebugPanelOpenForID(null)
    }
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setDebugPanelOpenForID(null)
    }
    document.addEventListener('mousedown', handleMouseDown)
    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('mousedown', handleMouseDown)
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [debugPanelOpenForID])

  const selectTerminalFromRail = useCallback((terminal: TerminalSnapshot) => {
    const key = terminalPaneKey(terminal)
    setSelectedID(key)
    setUserSelectedID(key)
    terminalAutoScrollRef.current = true
    terminalManualScrollLockRef.current = false
  }, [])

  const fetchTerminals = useCallback(async () => {
    const fetchScope = `${viewAll ? 'all' : (currentSessionId || '')}:${terminalWorkflowPathFilter || ''}`
    if (fetchInFlightRef.current && fetchInFlightScopeRef.current === fetchScope) return
    fetchInFlightRef.current = true
    fetchInFlightScopeRef.current = fetchScope
    const requestSeq = fetchRequestSeqRef.current + 1
    fetchRequestSeqRef.current = requestSeq
    if (lastFetchScopeRef.current !== fetchScope) {
      lastFetchScopeRef.current = fetchScope
      emptyResponseCountRef.current = 0
    }

    try {
      const response = await agentApi.listTerminals(viewAll ? undefined : currentSessionId, 'none')
      const visibleTerminals = (response.terminals || []).filter(terminal =>
        !dismissedTerminalIDs.has(terminal.terminal_id) &&
        terminalMatchesWorkflow(terminal, terminalWorkflowPathFilter)
      )

      const nextTerminals = dedupeTerminalsByID(visibleTerminals)
      if (fetchRequestSeqRef.current !== requestSeq) return
      setTerminals(current => {
        const currentMatchesScope = (
          viewAll ||
          !currentSessionId ||
          current.every(terminal =>
            terminal.session_id === currentSessionId &&
            terminalMatchesWorkflow(terminal, terminalWorkflowPathFilter)
          )
        )
        if (!viewAll && currentSessionId && nextTerminals.length === 0 && current.length > 0 && currentMatchesScope) {
          emptyResponseCountRef.current += 1
          if (emptyResponseCountRef.current <= EMPTY_TERMINAL_RESPONSE_GRACE_POLLS) {
            return current
          }
        }
        emptyResponseCountRef.current = nextTerminals.length === 0 ? emptyResponseCountRef.current : 0
        return nextTerminals
      })
      setError(null)
    } catch (err) {
      if (fetchRequestSeqRef.current !== requestSeq) return
      setError(err instanceof Error ? err.message : 'Failed to load terminals')
    } finally {
      if (fetchRequestSeqRef.current === requestSeq) {
        fetchInFlightRef.current = false
        fetchInFlightScopeRef.current = null
      }
    }
  }, [currentSessionId, dismissedTerminalIDs, terminalWorkflowPathFilter, viewAll])

  const dismissTerminal = useCallback(async (terminal: TerminalSnapshot) => {
    if (!canDismissTerminal(terminal)) return
    setDismissedTerminalIDs(current => {
      const next = new Set(current)
      next.add(terminal.terminal_id)
      return next
    })
    setTerminals(current => current.filter(item => item.terminal_id !== terminal.terminal_id))
    setTerminalDetailCache(current => (
      Object.fromEntries(
        Object.entries(current).filter(([, detail]) => detail.terminal_id !== terminal.terminal_id),
      ) as Record<string, TerminalSnapshot>
    ))
    if (selectedID === terminalPaneKey(terminal)) {
      setSelectedID(null)
    }
    if (userSelectedID === terminalPaneKey(terminal)) {
      setUserSelectedID(null)
    }
    if (debugPanelOpenForID === terminalPaneKey(terminal)) {
      setDebugPanelOpenForID(null)
    }
    try {
      await agentApi.dismissTerminal(terminal.terminal_id)
    } catch (err) {
      // Keep the terminal hidden locally even if the running backend has not picked up
      // the DELETE route yet. A backend restart will make dismissal persistent there too.
      console.warn('Failed to dismiss terminal on backend', err)
    }
  }, [debugPanelOpenForID, selectedID, userSelectedID])

  const clearableNonRunningTerminals = useMemo(
    () => dedupeTerminalsByID(terminals)
      .filter(isRailVisibleTerminal)
      .filter(terminal => !isMainAgentTerminal(terminal) && canDismissTerminal(terminal)),
    [terminals],
  )

  const clearNonRunningTerminals = useCallback(() => {
    clearableNonRunningTerminals.forEach(terminal => {
      void dismissTerminal(terminal)
    })
  }, [clearableNonRunningTerminals, dismissTerminal])

  // buildTree turns a flat list of terminals into a parent → children
  // tree. parent_step_id is a logical workflow edge, but terminal_id is
  // the actual node identity; this keeps repeated runs of the same
  // workflow from collapsing terminals that share a step_id.
  const buildTree = (
    list: TerminalSnapshot[],
    routeByNextStepID: Map<string, RoutingDecision>,
    routeDecisions: RoutingDecision[],
  ): TerminalRailItem[] => {
    const byParent = new Map<string, TerminalSnapshot[]>()
    const terminalsByStepID = new Map<string, TerminalSnapshot[]>()
    for (const t of list) {
      if (!t.step_id) continue
      const bucket = terminalsByStepID.get(t.step_id) || []
      bucket.push(t)
      terminalsByStepID.set(t.step_id, bucket)
    }
    for (const t of list) {
      const parent = resolveRailParentKey(t, terminalsByStepID)
      const bucket = byParent.get(parent) || []
      bucket.push(t)
      byParent.set(parent, bucket)
    }
    const out: TerminalRailItem[] = []
    const visited = new Set<string>()
    const renderedRoutes = new Set<string>()
    const walk = (parent: string, depth: number) => {
      // Defense in depth against cycles + degenerate-deep trees.
      if (depth > 32) return
      for (const t of byParent.get(parent) || []) {
        const nodeKey = terminalPaneKey(t)
        if (visited.has(nodeKey)) continue
        visited.add(nodeKey)
        const routeDecision = t.step_id ? routeByNextStepID.get(t.step_id) : undefined
        const terminalDepth = routeDecision ? depth + 1 : depth
        if (routeDecision && !renderedRoutes.has(routeDecision.id)) {
          out.push({
            kind: 'route',
            decision: routeDecision.nextStepType || !t.step_type
              ? routeDecision
              : { ...routeDecision, nextStepType: t.step_type },
            depth,
          })
          renderedRoutes.add(routeDecision.id)
        }
        out.push({ kind: 'terminal', terminal: t, depth: terminalDepth })
        walk(nodeKey, terminalDepth + 1)
      }
    }
    walk('', 0)
    for (const decision of routeDecisions) {
      if (decision.nextStepId && terminalsByStepID.has(decision.nextStepId)) continue
      if (!renderedRoutes.has(decision.id)) {
        out.push({ kind: 'route', decision, depth: 0 })
      }
    }
    return out
  }

  const groupedTerminals = useMemo(() => {
    const uniqueTerminals = dedupeTerminalsByID(terminals)
    const railTerminals = uniqueTerminals.filter(isRailVisibleTerminal)
    const filteredRailTerminals = terminalRailMinimized
      ? railTerminals.filter(isMainAgentTerminal)
      : railTerminals.filter(terminal => terminalMatchesRailFilter(terminal, terminalRailFilter))
    const visibleRouteByNextStepID = terminalRailFilter === 'all' && !terminalRailMinimized ? routingDecisionByNextStepID : new Map<string, RoutingDecision>()
    const visibleRouteDecisions = terminalRailFilter === 'all' && !terminalRailMinimized ? routingDecisions : []
    // Build a single tree from the rail-visible terminals after applying
    // the user's running/non-running filter.
    // Splitting them was breaking the parent→child relationship when
    // a child step finished while its parent was still running — the
    // child got displaced into the "Finished" group, losing its
    // visual nesting under the parent. One tree keeps lineage intact;
    // the colored dot on each rail row already conveys per-row state.
    const allTerminals = sortTerminalsForRail(filteredRailTerminals)
    const activeTerminals = filteredRailTerminals.filter(terminal => terminalState(terminal) === 'running')
    const finishedTerminals = filteredRailTerminals.filter(terminal => terminalState(terminal) !== 'running')
    const currentTerminals = sortTerminalsNewestFirst(filteredRailTerminals.filter(terminal => !isArchivedTurnTerminal(terminal)))
    const allRunningCount = railTerminals.filter(terminal => !isMainAgentTerminal(terminal) && terminalState(terminal) === 'running').length
    const allNonRunningCount = railTerminals.filter(terminal => !isMainAgentTerminal(terminal) && terminalState(terminal) !== 'running').length
    const clearableNonRunningCount = railTerminals.filter(terminal => !isMainAgentTerminal(terminal) && canDismissTerminal(terminal)).length
    // Stable number map: assign #1, #2, #3… based on all rail terminals
    // sorted by creation time. Never changes as terminals are filtered or dismissed.
    const stableNumberMap = new Map<string, number>()
    sortTerminalsForRail(railTerminals).forEach((t, i) => {
      stableNumberMap.set(terminalPaneKey(t), i + 1)
    })
    return {
      activeTerminals,
      finishedTerminals,
      currentTerminals,
      orderedTerminals: allTerminals,
      tree: buildTree(allTerminals, visibleRouteByNextStepID, visibleRouteDecisions),
      allRunningCount,
      allNonRunningCount,
      clearableNonRunningCount,
      stableNumberMap,
    }
  }, [terminals, terminalRailFilter, terminalRailMinimized, routingDecisionByNextStepID, routingDecisions])
  const currentMainTerminal = useMemo(
    () => groupedTerminals.currentTerminals.find(terminal => isMainAgentTerminal(terminal)) || null,
    [groupedTerminals.currentTerminals],
  )

  useEffect(() => {
    // Component is now only mounted when Debug view is active (it's
    // not a sidekick panel anymore), so polling should always run
    // whenever this component is on screen. The previous
    // terminalCenterOpen flag gated a standalone toggle that no
    // longer exists.
    void fetchTerminals()
    const interval = window.setInterval(fetchTerminals, TERMINAL_POLL_INTERVAL_MS)
    return () => window.clearInterval(interval)
  }, [fetchTerminals])

  useEffect(() => {
    const stopFastPolling = () => {
      if (fastPollIntervalRef.current !== null) {
        window.clearInterval(fastPollIntervalRef.current)
        fastPollIntervalRef.current = null
      }
    }

    const startFastPolling = () => {
      fastPollUntilRef.current = Date.now() + TERMINAL_FAST_POLL_DURATION_MS
      void fetchTerminals()
      if (fastPollIntervalRef.current !== null) return

      fastPollIntervalRef.current = window.setInterval(() => {
        if (Date.now() > fastPollUntilRef.current) {
          stopFastPolling()
          return
        }
        void fetchTerminals()
      }, TERMINAL_FAST_POLL_INTERVAL_MS)
    }

    window.addEventListener(TERMINAL_REFRESH_REQUEST_EVENT, startFastPolling)
    return () => {
      window.removeEventListener(TERMINAL_REFRESH_REQUEST_EVENT, startFastPolling)
      stopFastPolling()
    }
  }, [fetchTerminals])

  useEffect(() => {
    if (groupedTerminals.orderedTerminals.length === 0) {
      setSelectedID(null)
      return
    }
    const selected = groupedTerminals.orderedTerminals.find(terminal => terminalPaneKey(terminal) === selectedID)
    const userSelected = groupedTerminals.orderedTerminals.find(terminal => terminalPaneKey(terminal) === userSelectedID)
    const latestActive = groupedTerminals.activeTerminals[0]
    const preferredTerminal = currentMainTerminal || latestActive || groupedTerminals.currentTerminals[0] || groupedTerminals.orderedTerminals[0]

    if (userSelected) {
      const userSelectedKey = terminalPaneKey(userSelected)
      if (selectedID !== userSelectedKey) {
        setSelectedID(userSelectedKey)
      }
      return
    }

    if (userSelectedID && !userSelected) {
      setUserSelectedID(null)
    }

    if (
      selected &&
      preferredTerminal &&
      isArchivedTurnTerminal(selected) &&
      !isArchivedTurnTerminal(preferredTerminal) &&
      terminalPaneKey(selected) !== terminalPaneKey(preferredTerminal)
    ) {
      setSelectedID(terminalPaneKey(preferredTerminal))
      return
    }

    if (!selectedID || !selected) {
      setSelectedID(terminalPaneKey(preferredTerminal))
    }
  }, [currentMainTerminal, groupedTerminals, selectedID, userSelectedID])

  const selectedTerminal = useMemo(
    () => {
      if (!selectedID) return null
      return groupedTerminals.orderedTerminals.find(terminal => terminalPaneKey(terminal) === selectedID) || null
    },
    [groupedTerminals, selectedID],
  )
  const selectedTerminalKey = selectedTerminal ? terminalPaneKey(selectedTerminal) : null
  const selectedTerminalDetailCacheKey = selectedTerminal ? terminalDetailCacheKey(selectedTerminal) : null
  const selectedTerminalView = useMemo(() => {
    if (!selectedTerminal) return null
    const cachedDetail = selectedTerminalDetailCacheKey ? terminalDetailCache[selectedTerminalDetailCacheKey] : undefined
    if (cachedDetail && terminalPaneKey(cachedDetail) === selectedTerminalKey) {
      return terminalWithCachedBody(selectedTerminal, cachedDetail, true)
    }
    const staleDetail = latestCachedTerminalDetail(selectedTerminal, terminalDetailCache)
    if (staleDetail && terminalPaneKey(staleDetail) === selectedTerminalKey) {
      return terminalWithCachedBody(selectedTerminal, staleDetail)
    }
    return selectedTerminal
  }, [selectedTerminal, selectedTerminalDetailCacheKey, selectedTerminalKey, terminalDetailCache])
  const priorArchivedTurns = useMemo(
    () => (selectedTerminalView ? findPriorArchivedTurns(selectedTerminalView, terminals) : []),
    [selectedTerminalView, terminals],
  )

  // Lazily fetch full content for each prior :turn- snapshot so the
  // aggregated scrollback isn't blank. The rail's listTerminals poll uses
  // content='none' for payload size, so archived turn bodies aren't already
  // in state. Archived snapshots are immutable, so a one-shot fetch per id
  // is enough — keep a cache to skip refetches when terminals state churns.
  useEffect(() => {
    if (priorArchivedTurns.length === 0) return
    let cancelled = false
    const missing = priorArchivedTurns.filter(t => archivedTurnContents[t.terminal_id] === undefined)
    if (missing.length === 0) return
    void Promise.all(missing.map(async t => {
      try {
        const detailOptions = withTerminalScrollDebug(undefined, 'archived-turn')
        const detail = await agentApi.getTerminal(t.terminal_id, detailOptions)
        logTerminalScrollDebug('archived-turn', t, detailOptions, detail)
        return { id: t.terminal_id, content: detail.content || '' }
      } catch (err) {
        console.warn('Failed to load archived turn content', t.terminal_id, err)
        return { id: t.terminal_id, content: '' }
      }
    })).then(results => {
      if (cancelled) return
      setArchivedTurnContents(prev => {
        const next = { ...prev }
        for (const r of results) next[r.id] = r.content
        return next
      })
    })
    return () => { cancelled = true }
  }, [priorArchivedTurns, archivedTurnContents])

  const selectedTerminalDisplayContent = useMemo(
    () => {
      if (!selectedTerminalView) return ''
      const aggregated = aggregatePriorTurnContent(selectedTerminalView, priorArchivedTurns, archivedTurnContents)
      return trimTerminalDisplayContent(aggregated)
    },
    [selectedTerminalView, priorArchivedTurns, archivedTurnContents],
  )
  const selectedTerminalIsSynthetic = selectedTerminalView ? isSyntheticTerminal(selectedTerminalView) : false
  const selectedRouteDecision = selectedTerminalView?.step_id
    ? routingDecisionByNextStepID.get(selectedTerminalView.step_id)
    : undefined
  const selectedTerminalErrors = selectedTerminalView
    ? terminalErrorsByID.get(terminalPaneKey(selectedTerminalView)) || []
    : []
  const railSpinner = useSpinnerFrame(groupedTerminals.activeTerminals.length > 0)
  const isSelectedTerminalStreaming = !!selectedTerminalView?.active && (selectedTerminalView.state === 'running' || selectedTerminalView.state === 'idle' || selectedTerminalView.state === undefined)
  const selectedTerminalSpinner = useSpinnerFrame(isSelectedTerminalStreaming)

  const activeRailTmuxProbeTargets = useMemo(
    () => groupedTerminals.orderedTerminals
      .filter(terminal =>
        terminalState(terminal) === 'running' &&
        !!terminal.tmux_session &&
        terminal.terminal_id !== selectedTerminalView?.terminal_id &&
        !isMainAgentTerminal(terminal)
      )
      .slice(0, TERMINAL_ACTIVE_RAIL_PROBE_LIMIT),
    [groupedTerminals.orderedTerminals, selectedTerminalView?.terminal_id],
  )

  useEffect(() => {
    if (!selectedTerminal) {
      return
    }
    // Read the cache through a ref so this effect doesn't re-run on every
    // cache write (the rail poll churns the cache ~27x/sec); it should only
    // re-fire when the selected terminal's identity or chunk_index changes.
    const detailCacheKey = terminalDetailCacheKey(selectedTerminal)
    if (terminalDetailCacheRef.current[detailCacheKey]) return
    const requestSeq = detailRequestSeqRef.current + 1
    detailRequestSeqRef.current = requestSeq
    let cancelled = false
    probeInFlightRef.current = true
    const detailOptions = terminalRequestOptions(selectedTerminal, true, 'selected-detail')
    agentApi.getTerminal(selectedTerminal.terminal_id, detailOptions)
      .then(detail => {
        logTerminalScrollDebug('selected-detail', selectedTerminal, detailOptions, detail)
        if (!cancelled && detailRequestSeqRef.current === requestSeq && terminalPaneKey(detail) === selectedTerminalKey) {
          cacheTerminalDetail(detail)
        }
      })
      .catch(err => {
        if (!cancelled) {
          console.warn('Failed to load terminal detail', err)
        }
      })
      .finally(() => {
        probeInFlightRef.current = false
      })
    return () => {
      cancelled = true
    }
  }, [selectedTerminal?.terminal_id, selectedTerminal?.chunk_index, selectedTerminal?.updated_at, selectedTerminalKey, cacheTerminalDetail])

  // The rail/list poll is metadata-only. Without a screen probe, a workflow-step
  // tmux pane that finished without a fresh streaming_end can keep showing the
  // spinner until the user clicks it. Probe visible active step panes in the
  // background so their state can settle to completed in the tree.
  useEffect(() => {
    if (activeRailTmuxProbeTargets.length === 0) return
    let cancelled = false
    let inFlight = false

    const probeActiveRailTerminals = () => {
      if (cancelled || inFlight) return
      inFlight = true
      void Promise.all(activeRailTmuxProbeTargets.map(async terminal => {
        const detailOptions = terminalRequestOptions(terminal, false, 'rail-probe')
        if (!detailOptions) return null
        try {
          const detail = await agentApi.getTerminal(terminal.terminal_id, detailOptions)
          logTerminalScrollDebug('rail-probe', terminal, detailOptions, detail)
          return detail
        } catch {
          return null
        }
      })).then(details => {
        if (cancelled) return
        let applied = false
        for (const detail of details) {
          if (!detail) continue
          const current = terminalsRef.current.find(terminal => terminal.terminal_id === detail.terminal_id)
          const changed = !current ||
            current.active !== detail.active ||
            current.state !== detail.state ||
            current.chunk_index !== detail.chunk_index ||
            current.updated_at !== detail.updated_at
          if (changed) {
            cacheTerminalDetail(detail)
            applyTerminalSnapshotUpdate(detail)
            applied = true
          }
        }
        if (applied) void fetchTerminals()
      }).finally(() => {
        inFlight = false
      })
    }

    const interval = window.setInterval(probeActiveRailTerminals, TERMINAL_POLL_INTERVAL_MS)
    return () => {
      cancelled = true
      window.clearInterval(interval)
    }
  }, [activeRailTmuxProbeTargets, applyTerminalSnapshotUpdate, cacheTerminalDetail, fetchTerminals])

  // Tmux panes can change without a new streaming event: prompt text is typed
  // directly into tmux, and some CLIs redraw their screen in-place. Probe the
  // selected live pane every 3s so the detail view follows the actual tmux
  // screen instead of only the last stored stream chunk.
  useEffect(() => {
    const terminalId = selectedTerminalView?.terminal_id
    const tmuxSession = selectedTerminalView?.tmux_session
    const state = selectedTerminalView ? terminalState(selectedTerminalView) : ''
    // Stale/failed terminals have no live tmux pane to recapture; probing them
    // just loops over a dead session (the backend now marks such GETs stale).
    const shouldProbeTmux = !!tmuxSession && state !== 'stale' && state !== 'failed'
    if (!terminalId || !shouldProbeTmux) return

    let stopped = false
    let interval = 0
    const stop = () => {
      if (stopped) return
      stopped = true
      window.clearInterval(interval)
    }

    const probe = () => {
      // Skip if a capture (probe or detail fetch) is still in flight — two
      // concurrent capture-pane calls on the same tmux session race.
      if (probeInFlightRef.current) return
      probeInFlightRef.current = true
      const detailOptions = terminalRequestOptions(selectedTerminalView, true, 'selected-probe')
      void agentApi.getTerminal(terminalId, detailOptions).then(detail => {
        logTerminalScrollDebug('selected-probe', selectedTerminalView, detailOptions, detail)
        cacheTerminalDetail(detail)
        // The detail carries the freshest active/state — react to it directly
        // instead of waiting a full list-poll cycle for selectedTerminalView to
        // catch up. Dead sessions should stop; active sessions keep probing
        // because their pane can update without server-side stream events.
        const detailState = terminalState(detail)
        if (detailState === 'stale' || detailState === 'failed') {
          stop()
        }
        void fetchTerminals()
      }).catch(() => { /* stale or closed session — ignore */ })
        .finally(() => { probeInFlightRef.current = false })
    }

    interval = window.setInterval(probe, 3000)
    probe()
    return () => stop()
  }, [selectedTerminalView?.terminal_id, selectedTerminalView?.tmux_session, selectedTerminalView?.active, cacheTerminalDetail, fetchTerminals])

  useEffect(() => {
    const refreshTmuxDetails = () => {
      const candidates = [selectedTerminalView, currentMainTerminal]
      const seen = new Set<string>()
      for (const terminal of candidates) {
        if (!terminal?.terminal_id || !terminal.tmux_session || seen.has(terminal.terminal_id)) continue
        seen.add(terminal.terminal_id)
        const detailOptions = terminalRequestOptions(terminal, true, 'manual-refresh')
        void agentApi.getTerminal(terminal.terminal_id, detailOptions)
          .then(detail => {
            logTerminalScrollDebug('manual-refresh', terminal, detailOptions, detail)
            cacheTerminalDetail(detail)
            void fetchTerminals()
          })
          .catch(() => { /* best-effort refresh burst */ })
      }
    }

    window.addEventListener(TERMINAL_REFRESH_REQUEST_EVENT, refreshTmuxDetails)
    return () => window.removeEventListener(TERMINAL_REFRESH_REQUEST_EVENT, refreshTmuxDetails)
  }, [selectedTerminalView, currentMainTerminal, cacheTerminalDetail, fetchTerminals])

  const handleTerminalScroll = useCallback(() => {
    const el = terminalOutputRef.current
    if (!el) return
    const isNearBottom = isScrolledNearBottom(el)
    terminalAutoScrollRef.current = isNearBottom
    terminalManualScrollLockRef.current = !isNearBottom
  }, [])

  const handleTerminalWheel = useCallback((event: React.WheelEvent<HTMLElement>) => {
    if (event.deltaY < 0) {
      terminalAutoScrollRef.current = false
      terminalManualScrollLockRef.current = true
    }
  }, [])

  const handleXtermViewportStickChange = useCallback((isNearBottom: boolean) => {
    terminalAutoScrollRef.current = isNearBottom
    terminalManualScrollLockRef.current = !isNearBottom
  }, [])

  const scrollSelectedTerminalToBottom = useCallback(() => {
    terminalAutoScrollRef.current = true
    terminalManualScrollLockRef.current = false
    const scroll = () => {
      const el = terminalOutputRef.current
      if (!el) return
      el.scrollTop = Math.max(0, el.scrollHeight - el.clientHeight)
    }
    scroll()
    window.requestAnimationFrame(scroll)
  }, [])

  useEffect(() => {
    const el = terminalOutputRef.current
    if (!el || !selectedTerminalDisplayContent) return

    const terminalChanged = selectedTerminalIDRef.current !== selectedTerminalKey
    if (terminalChanged) {
      const isFirstSelection = selectedTerminalIDRef.current === null
      selectedTerminalIDRef.current = selectedTerminalKey
      if (isFirstSelection || !terminalManualScrollLockRef.current) {
        terminalAutoScrollRef.current = true
        terminalManualScrollLockRef.current = false
      }
    }

    if (!terminalAutoScrollRef.current || terminalManualScrollLockRef.current) return

    const frame = window.requestAnimationFrame(() => {
      const maxScrollTop = Math.max(0, el.scrollHeight - el.clientHeight)
      el.scrollTop = maxScrollTop
    })
    return () => window.cancelAnimationFrame(frame)
  }, [selectedTerminalKey, selectedTerminalDisplayContent])

  // Startup size hint: report the terminal viewport size to the backend as soon
  // as the TerminalCenter mounts — even before any session exists — so the very
  // first coding-agent tmux session launches at the correct width instead of the
  // 120×36 default. Best-effort (fires once, silently ignored if it fails).
  const sizeHintSentRef = useRef(false)
  useEffect(() => {
    if (sizeHintSentRef.current) return
    const el = terminalOutputRef.current as HTMLElement | null
    if (!el) return
    const ruler = document.createElement('span')
    ruler.textContent = '0'.repeat(100)
    ruler.setAttribute('aria-hidden', 'true')
    ruler.style.cssText = 'position:absolute;visibility:hidden;white-space:pre;top:0;left:0;pointer-events:none;'
    el.appendChild(ruler)
    const charWidth = ruler.getBoundingClientRect().width / 100
    const lineHeight = ruler.getBoundingClientRect().height || 16
    ruler.remove()
    if (!charWidth || !lineHeight) return
    const style = window.getComputedStyle(el)
    const padX = (parseFloat(style.paddingLeft) || 0) + (parseFloat(style.paddingRight) || 0)
    const padY = (parseFloat(style.paddingTop) || 0) + (parseFloat(style.paddingBottom) || 0)
    const cols = Math.max(40, Math.floor((el.clientWidth - padX) / charWidth) - 1)
    const rows = Math.max(10, Math.floor((el.clientHeight - padY) / lineHeight))
    sizeHintSentRef.current = true
    void agentApi.reportTerminalSizeHint(cols, rows).catch(() => { /* best-effort */ })
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  // Dynamic tmux resize: measure the terminal viewport in monospace chars and
  // POST the dimensions to /api/terminals/{id}/resize. The backend runs
  // `tmux resize-window` on the live pane AND records the size as the
  // process-wide preferred size, so the next CLI-agent tmux session launches
  // at the operator's actual viewport instead of the 120×36 default. Only
  // fires for terminals backed by a tmux session.
  const lastResizeSentRef = useRef<{ terminalId: string; cols: number; rows: number } | null>(null)
  useEffect(() => {
    const el = terminalOutputRef.current as HTMLElement | null
    const tmuxBacked = selectedTerminalView && Boolean(selectedTerminalView.tmux_session)
    if (!el || !selectedTerminalView || !tmuxBacked || isSyntheticTerminal(selectedTerminalView)) return
    const terminalId = selectedTerminalView.terminal_id

    const ruler = document.createElement('span')
    ruler.textContent = '0'.repeat(100)
    ruler.setAttribute('aria-hidden', 'true')
    ruler.style.cssText = 'position:absolute;visibility:hidden;white-space:pre;top:0;left:0;pointer-events:none;'
    el.appendChild(ruler)

    let timer: number | undefined
    const measureAndSend = () => {
      const rulerRect = ruler.getBoundingClientRect()
      const charWidth = rulerRect.width / 100
      const lineHeight = rulerRect.height || parseFloat(window.getComputedStyle(ruler).lineHeight) || 16
      if (!charWidth || !lineHeight) return
      const style = window.getComputedStyle(el)
      const padX = (parseFloat(style.paddingLeft) || 0) + (parseFloat(style.paddingRight) || 0)
      const padY = (parseFloat(style.paddingTop) || 0) + (parseFloat(style.paddingBottom) || 0)
      const innerWidth = el.clientWidth - padX
      const innerHeight = el.clientHeight - padY
      // -1 col guard so cursor's right-edge box-drawing doesn't wrap mid-char.
      const cols = Math.max(40, Math.floor(innerWidth / charWidth) - 1)
      const rows = Math.max(10, Math.floor(innerHeight / lineHeight))
      const last = lastResizeSentRef.current
      if (last && last.terminalId === terminalId && last.cols === cols && last.rows === rows) return
      lastResizeSentRef.current = { terminalId, cols, rows }
      void agentApi.resizeTerminal(terminalId, cols, rows).catch(() => {
        // Resize is best-effort: tmux pane may have been killed, terminal may
        // have transitioned. Don't surface as a user error.
        lastResizeSentRef.current = null
      })
    }

    const schedule = () => {
      if (timer !== undefined) window.clearTimeout(timer)
      timer = window.setTimeout(measureAndSend, 200)
    }

    schedule()
    const observer = new ResizeObserver(schedule)
    observer.observe(el)
    window.addEventListener('resize', schedule)

    return () => {
      observer.disconnect()
      window.removeEventListener('resize', schedule)
      if (timer !== undefined) window.clearTimeout(timer)
      ruler.remove()
    }
  }, [selectedTerminalView])


  // Rail item — one row in the left rail. Compact vertical layout:
  // dot + step title (top line), transport chip + closing countdown
  // (bottom line). Click → select; hover → highlight.
  // depth controls left padding so child terminals nest under their
  // parent without drawing noisy repeated text guides.
  const renderRouteRailItem = (decision: RoutingDecision, depth: number = 0) => (
    <div
      key={`route-${decision.id}`}
      className={`group block w-full py-1.5 pl-2.5 pr-2.5 text-left ${terminalTheme.railText} ${terminalTheme.routeRail}`}
      title={routeDecisionTitle(decision)}
      style={{ paddingLeft: terminalRailPadding(depth) }}
    >
      <div className="flex items-center gap-1.5">
        <TerminalRailBranchMarker depth={depth} />
        <span className={`flex h-4 w-4 shrink-0 items-center justify-center rounded ${terminalTheme.routeIcon}`}>
          <GitBranch className="h-3 w-3" />
        </span>
        <span className="min-w-0 flex-1 truncate font-semibold">
          Routing: {routeDecisionLabel(decision)}
        </span>
        <button
          type="button"
          onClick={event => {
            event.stopPropagation()
            setDismissedRouteIDs(current => {
              const next = new Set(current)
              next.add(decision.id)
              return next
            })
          }}
          className={`shrink-0 rounded p-0.5 opacity-0 group-hover:opacity-100 focus:opacity-100 ${terminalTheme.routeClose}`}
          title="Remove routing marker from UI"
          aria-label="Remove routing marker from UI"
        >
          <X className="h-3 w-3" />
        </button>
      </div>
      <div className={`mt-0.5 flex items-center gap-1.5 ${terminalTheme.railMetaText} ${terminalTheme.routeMeta}`}>
        {decision.nextStepType && (
          <StepTypeIcon stepType={decision.nextStepType} labelPrefix="Next step type: " />
        )}
        <span className="min-w-0 truncate">
          {decision.nextStepId ? `next ${decision.nextStepId}` : decision.stepTitle || decision.stepId || 'route selected'}
        </span>
        {decision.routeCount > 1 && (
          <span className="shrink-0">· {decision.routeCount} routes</span>
        )}
      </div>
    </div>
  )

  const renderRailControls = () => {
    // Active filter uses the theme's prompt color (consistent with the
    // colored `$ ` prompts in the pane). Inactive uses a dim neutral that
    // reads as "muted" across every dark theme; hover lifts to the rail's
    // standard text color so the cue is theme-coherent on rollover.
    const filterButtonClass = (filter: TerminalRailFilter) => (
      `px-1 py-0.5 font-mono leading-4 transition-colors ${
        terminalRailFilter === filter
          ? terminalTheme.prompt
          : `text-neutral-500 hover:text-neutral-200`
      }`
    )
    const filterLabel = (filter: TerminalRailFilter, label: string) => (
      terminalRailFilter === filter ? `[${label}]` : label
    )

    return (
      <div key="terminal-rail-controls" className="border-y border-neutral-800/80 bg-[#0b0d0c] px-2 py-1 font-mono">
        <div className={`flex min-w-0 items-center gap-0.5 text-[10px] leading-4 ${terminalTheme.microText}`}>
          <button
            type="button"
            onClick={() => setRailManualNarrow(!railNarrow)}
            className="inline-flex h-4 w-4 shrink-0 items-center justify-center text-neutral-500 transition-colors hover:text-emerald-300"
            title={railNarrow ? 'Widen agent tree' : 'Narrow agent tree'}
            aria-label={railNarrow ? 'Widen agent tree' : 'Narrow agent tree'}
          >
            {railNarrow ? <ChevronsRight className="h-2.5 w-2.5" /> : <ChevronsLeft className="h-2.5 w-2.5" />}
          </button>
          <button
            type="button"
            onClick={() => setTerminalRailFilter('all')}
            disabled={terminalRailMinimized}
            className={filterButtonClass('all')}
            title="Show all agents"
          >
            {filterLabel('all', 'all')}
          </button>
          <button
            type="button"
            onClick={() => setTerminalRailFilter('running')}
            disabled={terminalRailMinimized}
            className={filterButtonClass('running')}
            title="Show running agents"
          >
            {filterLabel('running', `run:${groupedTerminals.allRunningCount}`)}
          </button>
          <button
            type="button"
            onClick={() => setTerminalRailFilter('non-running')}
            disabled={terminalRailMinimized}
            className={filterButtonClass('non-running')}
            title="Show non-running agents"
          >
            {filterLabel('non-running', `done:${groupedTerminals.allNonRunningCount}`)}
          </button>
          <button
            type="button"
            onClick={clearNonRunningTerminals}
            disabled={clearableNonRunningTerminals.length === 0}
            className="ml-auto inline-flex h-4 w-4 shrink-0 items-center justify-center text-neutral-500 transition-colors hover:text-red-300 disabled:cursor-not-allowed disabled:opacity-35 disabled:hover:text-neutral-500"
            title="Clear all non-running agents"
            aria-label="Clear all non-running agents"
          >
            <Trash2 className="h-2.5 w-2.5" />
          </button>
        </div>
      </div>
    )
  }

  const renderRailItem = (terminal: TerminalSnapshot, depth: number = 0, index?: number) => (
    (() => {
      const preValidationChip = terminalPreValidationChip(terminal, terminalTheme)
      const state = terminalState(terminal)
      const isRunning = state === 'running'
      const startedTimestamp = formatStartedTimestamp(terminal)
      const railAge = formatRailAge(terminal)
      const stepTypeLabel = terminalRailStepTypeLabel(terminal)
      const railTransport = formatRailTransportChip(terminal)
      const terminalErrors = terminalErrorsByID.get(terminalPaneKey(terminal)) || []
      const latestTerminalError = terminalErrors[0]
      return (
        <div
          key={terminalPaneKey(terminal)}
          role="button"
          tabIndex={0}
          onClick={() => {
            selectTerminalFromRail(terminal)
          }}
          onKeyDown={event => {
            if (event.key === 'Enter' || event.key === ' ') {
              event.preventDefault()
              selectTerminalFromRail(terminal)
            }
          }}
          className={`group block w-full cursor-pointer border-l-2 py-1.5 pl-2.5 pr-2.5 text-left transition-colors ${terminalTheme.railText} ${
            terminalPaneKey(terminal) === selectedTerminalKey
              ? terminalTheme.railSelected
              : 'border-l-transparent text-neutral-400 hover:bg-[#1b1f1d] hover:text-neutral-200'
          }`}
          style={{ paddingLeft: terminalRailPadding(depth) }}
        >
          <div className="flex items-center gap-1.5">
            <TerminalRailBranchMarker depth={depth} />
            {isRunning ? (
              <span
                className={`w-2 shrink-0 text-center font-mono text-[10px] leading-none ${terminalTheme.railSpinner}`}
                title={terminalStateDescription(terminal)}
                aria-label={terminalStateDescription(terminal)}
              >
                {railSpinner}
              </span>
            ) : (
              <span
                className={`h-2 w-2 shrink-0 rounded-full ${terminalDotClass(terminal, terminalTheme)}`}
                title={terminalStateDescription(terminal)}
                aria-label={terminalStateDescription(terminal)}
              />
            )}
            <TerminalStepTypeIcon terminal={terminal} />
            {isArchivedTurnTerminal(terminal) && (
              <TerminalArchivedTurnIcon />
            )}
            <span className="min-w-0 flex-1 truncate font-medium">{formatTerminalTitle(terminal)}</span>
            {latestTerminalError && (
              <AlertTriangle
                className="h-3.5 w-3.5 shrink-0 text-red-300"
                aria-label="Terminal error"
              />
            )}
            {canDismissTerminal(terminal) && (
              <button
                type="button"
                onClick={event => {
                  event.stopPropagation()
                  void dismissTerminal(terminal)
                }}
                className="shrink-0 rounded p-0.5 text-neutral-500 opacity-0 hover:bg-neutral-700/80 hover:text-neutral-100 group-hover:opacity-100"
                title="Remove terminal from UI"
                aria-label="Remove terminal from UI"
              >
                <X className="h-3 w-3" />
              </button>
            )}
          </div>
          {/* The provider/start-time meta row is the bulk of the card height;
              hide it when the chat is the narrow report/plan rail to slim each
              card to one line (info stays on hover via title attrs / when the
              chat is given more room). */}
          {!railNarrow && (
          <div className={`mt-0.5 flex items-center gap-1.5 opacity-70 ${terminalTheme.railMetaText}`}>
            {index !== undefined && (
              <span className="shrink-0 font-mono text-[9px] text-neutral-600">#{index}</span>
            )}
            {railTransport && (
              <span className="min-w-0 truncate text-neutral-500" title={formatTransportChip(terminal)}>
                {railTransport}
              </span>
            )}
            {railAge && (
              <span className="shrink-0 text-neutral-500" title={railAge.title}>
                {railAge.label}
              </span>
            )}
            {preValidationChip && (
              <span
                className={`shrink-0 rounded border px-1 py-0.5 font-semibold leading-none ${terminalTheme.microText} ${preValidationChip.className}`}
                title={preValidationChip.title}
              >
                {preValidationChip.label}
              </span>
            )}
            {state === 'closing' && (
              <span className={`shrink-0 ${terminalTheme.warningText}`}>· {terminalStateLabel(terminal)}</span>
            )}
            {terminalFallbackInfo(terminal) && (
              <span className={`shrink-0 ${terminalTheme.warningText}`} title={terminalFallbackInfo(terminal)?.title}>
                · {terminalFallbackInfo(terminal)?.label}
              </span>
            )}
          </div>
          )}
          {latestTerminalError && (
            <div
              className="mt-1 flex min-w-0 items-center gap-1.5 text-[10px] leading-4 text-red-300"
              title={latestTerminalError.message}
            >
              <AlertTriangle className="h-3 w-3 shrink-0" aria-hidden />
              <span className="min-w-0 truncate">{compactTerminalErrorMessage(latestTerminalError.message)}</span>
            </div>
          )}
        </div>
      )
    })()
  )

  return (
    <div className={`flex min-h-0 min-w-0 flex-col bg-[#191a18] text-neutral-100 ${compact ? '' : 'flex-1 overflow-hidden'}`}>
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        {(!hasConversationActivity && groupedTerminals.orderedTerminals.length === 0) ? (
          // Blank only when there's neither live conversation activity nor any
          // terminal to show. A resumed/completed session has no "activity" but
          // does have a terminal snapshot — keep showing it instead of a blank pane.
          <div className="flex flex-1" />
        ) : (
          <>
        {error && (
          <div className="rounded border border-red-800/50 bg-red-950/20 px-3 py-2 text-xs text-red-200">
            {error}
          </div>
        )}

        {sessionErrorBanner.length > 0 && (
          <div className="flex flex-col gap-1 border-b border-red-900/35 bg-red-950/15 px-3 py-2">
            {sessionErrorBanner.map(entry => {
              const isOpen = expandedErrorIDs.has(entry.id)
              return (
              <div key={entry.id} className="text-xs text-red-300">
                <div className="flex min-w-0 items-center gap-2">
                  <span className="shrink-0 rounded bg-red-900/35 px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide text-red-200">
                    {entry.type.replace(/_/g, ' ')}
                  </span>
                  <span className="min-w-0 flex-1 truncate leading-5" title={entry.message}>
                    {compactTerminalErrorMessage(entry.message)}
                  </span>
                  <button
                    type="button"
                    onClick={() => toggleTerminalError(entry.id)}
                    className="shrink-0 rounded border border-red-800/60 px-2 py-0.5 text-[10px] font-medium text-red-200 hover:bg-red-900/35"
                    aria-expanded={isOpen}
                  >
                    {isOpen ? 'Close' : 'Open'}
                  </button>
                  <button
                    type="button"
                    onClick={() => dismissTerminalError(entry.id)}
                    className="shrink-0 rounded p-0.5 text-red-400/60 hover:bg-red-900/35 hover:text-red-200"
                    title="Dismiss"
                    aria-label="Dismiss error"
                  >
                    <X className="h-3 w-3" />
                  </button>
                </div>
                {isOpen && (
                  <div className="mt-1 max-h-32 overflow-y-auto rounded border border-red-900/45 bg-red-950/25 p-2 font-mono text-[11px] leading-4 text-red-200">
                    {entry.message}
                  </div>
                )}
              </div>
              )
            })}
          </div>
        )}

        {!error && terminals.length === 0 && routingDecisions.length === 0 && (
          <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 py-12 text-center">
            <Terminal className="h-10 w-10 text-neutral-700" strokeWidth={1.25} />
            <div className="text-sm font-medium text-neutral-300">No terminals yet</div>
            <div className="max-w-md text-xs leading-relaxed text-neutral-500">
              Run a workflow step, send a message to the main agent, or kick off
              a coding-agent task to see its activity stream here. Each call
              becomes its own pane — the rail on the left lists them all, the
              right pane shows live output, tool calls, and cost.
            </div>
            <div className="mt-1 flex items-center gap-1.5 text-[11px] text-neutral-600">
              <span className={`inline-block h-1.5 w-1.5 animate-pulse rounded-full ${terminalTheme.emptyPulse}`} />
              <span>Watching for activity…</span>
            </div>
          </div>
        )}

        {groupedTerminals.orderedTerminals.length > 0 && (
          <div className="flex min-h-0 min-w-0 flex-1 gap-0 overflow-hidden border border-neutral-700/70 bg-[#101211]">
            {/* Left rail — vertical list of all terminals. Scrolls
                independently of the right pane so the user can navigate
                a long list without losing the selected terminal's
                content. Hidden below sm breakpoint to save space. */}
            <div className={`hidden shrink-0 flex-col overflow-y-auto overflow-x-hidden border-r border-neutral-700/70 bg-[#141615] sm:flex ${railNarrow ? 'w-14' : 'w-48'}`}>
              {(() => {
                let controlsRendered = false
                const rows = groupedTerminals.tree.flatMap(item => {
                  if (item.kind === 'route') return [renderRouteRailItem(item.decision, item.depth)]

                  const rendered = renderRailItem(item.terminal, item.depth, groupedTerminals.stableNumberMap.get(terminalPaneKey(item.terminal)))
                  if (!controlsRendered && isMainAgentTerminal(item.terminal) && !isArchivedTurnTerminal(item.terminal)) {
                    controlsRendered = true
                    return [rendered, renderRailControls()]
                  }
                  return [rendered]
                })

                if (!controlsRendered) rows.unshift(renderRailControls())
                return rows
              })()}
            </div>

            {/* Right pane — the selected terminal's content. Header
                bar at top (chip + meta + actions), content below. */}
            <div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
              {selectedTerminalView ? (
                <>
                  <div className={`flex items-center justify-between gap-3 border-b border-neutral-700/70 bg-[#171a18] px-3 py-2 text-neutral-400 ${terminalTheme.headerText}`}>
                    <div className="flex min-w-0 flex-1 items-center gap-2">
                      {selectedRouteDecision && (
                        <span
                          className={`inline-flex max-w-[45%] shrink-0 items-center gap-1 rounded border px-1.5 py-0.5 font-medium ${terminalTheme.chipText} ${terminalTheme.selectedRouteChip}`}
                          title={routeDecisionTitle(selectedRouteDecision)}
                        >
                          <GitBranch className="h-3 w-3 shrink-0" />
                          <span className="truncate">{routeDecisionLabel(selectedRouteDecision)}</span>
                        </span>
                      )}
                      {isArchivedTurnTerminal(selectedTerminalView) && (
                        <TerminalArchivedTurnIcon />
                      )}
                      <span className="min-w-0 flex-1 truncate opacity-80">
                        {formatSelectedTerminalMeta(selectedTerminalView)}
                      </span>
                    </div>
                    <div className="flex shrink-0 items-center gap-2">
                      {terminalState(selectedTerminalView) === 'closing' && (
                        <span
                          className={terminalTheme.warningText}
                          title={terminalStateDescription(selectedTerminalView)}
                        >
                          {terminalStateLabel(selectedTerminalView)}
                        </span>
                      )}
                      {terminalFallbackInfo(selectedTerminalView) && (
                        <span
                          className={`rounded border px-1.5 py-0.5 font-medium ${terminalTheme.chipText} ${terminalTheme.warningChip}`}
                          title={terminalFallbackInfo(selectedTerminalView)?.title}
                        >
                          {terminalFallbackInfo(selectedTerminalView)?.label}
                        </span>
                      )}
                      <button
                        type="button"
                        onClick={scrollSelectedTerminalToBottom}
                        className="inline-flex items-center justify-center rounded p-1 text-neutral-500 hover:bg-neutral-800/80 hover:text-neutral-100"
                        title="Scroll terminal to bottom"
                        aria-label="Scroll terminal to bottom"
                      >
                        <ArrowDownToLine className="h-3.5 w-3.5" />
                      </button>
                      <button
                        type="button"
                        onClick={() => void copyTerminalDebug(selectedTerminalView)}
                        className="inline-flex items-center justify-center rounded p-1 text-neutral-500 hover:bg-neutral-800/80 hover:text-neutral-100"
                        title="Copy terminal debug IDs"
                        aria-label="Copy terminal debug IDs"
                      >
                        {copiedTerminalID === selectedTerminalView.terminal_id ? (
                          <Check className={`h-3.5 w-3.5 ${terminalTheme.copiedIcon}`} />
                        ) : (
                          <Info className="h-3.5 w-3.5" />
                        )}
                      </button>
                      {hasTerminalDebugActions(selectedTerminalView) && (
                        <div ref={debugMenuRef} className="relative inline-flex">
                          <button
                            type="button"
                            onMouseDown={event => event.preventDefault()}
                            onClick={() => toggleDebugPanel(selectedTerminalView)}
                            className={`inline-flex items-center justify-center rounded border p-1 hover:bg-neutral-800/80 hover:text-neutral-100 ${
                              debugPanelOpenForID === terminalPaneKey(selectedTerminalView)
                                ? terminalTheme.debugActive
                                : 'border-neutral-700/90 text-neutral-300'
                            }`}
                            title="Debug terminal actions"
                            aria-label="Debug terminal actions"
                            aria-haspopup="menu"
                            aria-expanded={debugPanelOpenForID === terminalPaneKey(selectedTerminalView)}
                          >
                            <Bug className="h-3.5 w-3.5" />
                          </button>
                          {debugPanelOpenForID === terminalPaneKey(selectedTerminalView) && (
                            <div
                              role="menu"
                              className="absolute right-0 top-full z-[60] mt-1 min-w-[200px] rounded-md border border-neutral-700/90 bg-[#151716] p-1 text-xs text-neutral-200 shadow-lg"
                            >
                              <button
                                type="button"
                                role="menuitem"
                                onMouseDown={event => event.preventDefault()}
                                onClick={() => { setDebugPanelOpenForID(null); void copyTerminalPaneText(selectedTerminalView) }}
                                disabled={!selectedTerminalView.content}
                                className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left hover:bg-neutral-800/80 disabled:cursor-not-allowed disabled:opacity-40"
                              >
                                <Copy className="h-3.5 w-3.5 shrink-0" />
                                <span>Copy pane text</span>
                              </button>
                              {selectedTerminalView.tmux_session && (
                                <button
                                  type="button"
                                  role="menuitem"
                                  onMouseDown={event => event.preventDefault()}
                                  onClick={() => { setDebugPanelOpenForID(null); void copyTmuxAttachCommand(selectedTerminalView) }}
                                  className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left hover:bg-neutral-800/80"
                                >
                                  <Terminal className="h-3.5 w-3.5 shrink-0" />
                                  <span>Copy tmux attach</span>
                                </button>
                              )}
                              {canSendTerminalDebugInput(selectedTerminalView) && (
                                <button
                                  type="button"
                                  role="menuitem"
                                  onMouseDown={event => event.preventDefault()}
                                  onClick={() => { setDebugPanelOpenForID(null); void refreshTerminalSnapshot(selectedTerminalView) }}
                                  disabled={terminalActionBusy === 'refresh'}
                                  className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left hover:bg-neutral-800/80 disabled:cursor-wait disabled:opacity-50"
                                >
                                  <RefreshCw className="h-3.5 w-3.5 shrink-0" />
                                  <span>Capture deeper history</span>
                                </button>
                              )}
                              {canForceCompleteTerminal(selectedTerminalView) && (
                                <button
                                  type="button"
                                  role="menuitem"
                                  onMouseDown={event => event.preventDefault()}
                                  onClick={() => { setDebugPanelOpenForID(null); void forceCompleteTerminal(selectedTerminalView) }}
                                  disabled={terminalActionBusy === 'complete'}
                                  className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left hover:bg-neutral-800/80 disabled:cursor-wait disabled:opacity-50"
                                >
                                  <Check className="h-3.5 w-3.5 shrink-0" />
                                  <span>Mark complete</span>
                                </button>
                              )}
                              <button
                                type="button"
                                role="menuitem"
                                onMouseDown={event => event.preventDefault()}
                                onClick={() => { setDebugPanelOpenForID(null); void forceFailTerminal(selectedTerminalView) }}
                                disabled={terminalActionBusy === 'fail'}
                                className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left hover:bg-neutral-800/80 disabled:cursor-wait disabled:opacity-50"
                              >
                                <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
                                <span>Mark failed</span>
                              </button>
                              {canSendTerminalDebugInput(selectedTerminalView) && (
                                <>
                                  <div className="my-1 h-px bg-neutral-800/80" role="none" />
                                  <button
                                    type="button"
                                    role="menuitem"
                                    onMouseDown={event => event.preventDefault()}
                                    onClick={() => { setDebugPanelOpenForID(null); void sendTerminalDebugKey(selectedTerminalView, 'enter') }}
                                    disabled={terminalActionBusy === 'enter'}
                                    className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left hover:bg-neutral-800/80 disabled:cursor-wait disabled:opacity-50"
                                  >
                                    <CornerDownLeft className="h-3.5 w-3.5 shrink-0" />
                                    <span>Send Enter</span>
                                  </button>
                                  <button
                                    type="button"
                                    role="menuitem"
                                    onMouseDown={event => event.preventDefault()}
                                    onClick={() => { setDebugPanelOpenForID(null); void sendTerminalDebugKey(selectedTerminalView, 'esc') }}
                                    disabled={terminalActionBusy === 'esc'}
                                    className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left hover:bg-neutral-800/80 disabled:cursor-wait disabled:opacity-50"
                                  >
                                    <CornerUpLeft className="h-3.5 w-3.5 shrink-0" />
                                    <span>Send Esc</span>
                                  </button>
                                  <button
                                    type="button"
                                    role="menuitem"
                                    onMouseDown={event => event.preventDefault()}
                                    onClick={() => { setDebugPanelOpenForID(null); void sendTerminalDebugKey(selectedTerminalView, 'ctrl-c') }}
                                    disabled={terminalActionBusy === 'ctrl-c'}
                                    className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left hover:bg-neutral-800/80 disabled:cursor-wait disabled:opacity-50"
                                  >
                                    <Square className="h-3.5 w-3.5 shrink-0" />
                                    <span>Send Ctrl+C (interrupt)</span>
                                  </button>
                                  <button
                                    type="button"
                                    role="menuitem"
                                    onMouseDown={event => event.preventDefault()}
                                    onClick={() => { setDebugPanelOpenForID(null); void sendTerminalDebugKey(selectedTerminalView, 'ctrl-o') }}
                                    disabled={terminalActionBusy === 'ctrl-o'}
                                    className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left hover:bg-neutral-800/80 disabled:cursor-wait disabled:opacity-50"
                                  >
                                    <Braces className="h-3.5 w-3.5 shrink-0" />
                                    <span>Send Ctrl+O (expand)</span>
                                  </button>
                                  <button
                                    type="button"
                                    role="menuitem"
                                    onMouseDown={event => event.preventDefault()}
                                    onClick={() => { setDebugPanelOpenForID(null); void sendTerminalDebugKey(selectedTerminalView, 'tab') }}
                                    disabled={terminalActionBusy === 'tab'}
                                    className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left hover:bg-neutral-800/80 disabled:cursor-wait disabled:opacity-50"
                                  >
                                    <ArrowRightToLine className="h-3.5 w-3.5 shrink-0" />
                                    <span>Send Tab (allowlist / select)</span>
                                  </button>
                                  <button
                                    type="button"
                                    role="menuitem"
                                    onMouseDown={event => event.preventDefault()}
                                    onClick={() => { setDebugPanelOpenForID(null); void sendTerminalDebugKey(selectedTerminalView, 'up') }}
                                    disabled={terminalActionBusy === 'up'}
                                    className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left hover:bg-neutral-800/80 disabled:cursor-wait disabled:opacity-50"
                                  >
                                    <ChevronUp className="h-3.5 w-3.5 shrink-0" />
                                    <span>Send Up arrow</span>
                                  </button>
                                  <button
                                    type="button"
                                    role="menuitem"
                                    onMouseDown={event => event.preventDefault()}
                                    onClick={() => { setDebugPanelOpenForID(null); void sendTerminalDebugKey(selectedTerminalView, 'down') }}
                                    disabled={terminalActionBusy === 'down'}
                                    className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left hover:bg-neutral-800/80 disabled:cursor-wait disabled:opacity-50"
                                  >
                                    <ChevronDown className="h-3.5 w-3.5 shrink-0" />
                                    <span>Send Down arrow</span>
                                  </button>
                                  <div className="my-1 h-px bg-neutral-800/80" role="none" />
                                  <div className="px-2 py-1.5">
                                    <div className="flex items-center gap-1.5">
                                      <input
                                        type="text"
                                        value={debugText}
                                        onChange={e => setDebugText(e.target.value)}
                                        onKeyDown={e => {
                                          if (e.key === 'Enter' && !e.shiftKey) {
                                            e.preventDefault()
                                            void sendTerminalDebugText(selectedTerminalView, debugText, true)
                                          }
                                        }}
                                        onMouseDown={e => e.stopPropagation()}
                                        placeholder="Send text…"
                                        className="h-6 flex-1 rounded border border-neutral-700/80 bg-neutral-900 px-2 text-xs text-neutral-200 placeholder-neutral-600 focus:border-neutral-500 focus:outline-none"
                                      />
                                      <button
                                        type="button"
                                        onMouseDown={e => e.preventDefault()}
                                        onClick={() => { void sendTerminalDebugText(selectedTerminalView, debugText, false) }}
                                        disabled={terminalActionBusy === 'send-text' || !debugText.trim()}
                                        title="Send without Enter"
                                        className="flex h-6 items-center rounded border border-neutral-700/80 bg-neutral-900 px-1.5 text-neutral-400 hover:bg-neutral-800 hover:text-neutral-100 disabled:cursor-wait disabled:opacity-40"
                                      >
                                        <Terminal className="h-3 w-3" />
                                      </button>
                                      <button
                                        type="button"
                                        onMouseDown={e => e.preventDefault()}
                                        onClick={() => { void sendTerminalDebugText(selectedTerminalView, debugText, true) }}
                                        disabled={terminalActionBusy === 'send-text' || !debugText.trim()}
                                        title="Send + Enter"
                                        className="flex h-6 items-center rounded border border-neutral-700/80 bg-neutral-900 px-1.5 text-neutral-400 hover:bg-neutral-800 hover:text-neutral-100 disabled:cursor-wait disabled:opacity-40"
                                      >
                                        <CornerDownLeft className="h-3 w-3" />
                                      </button>
                                    </div>
                                  </div>
                                  <div className="my-1 h-px bg-neutral-800/80" role="none" />
                                  <button
                                    type="button"
                                    role="menuitem"
                                    onMouseDown={event => event.preventDefault()}
                                    onClick={() => { setDebugPanelOpenForID(null); void killTerminalSession(selectedTerminalView) }}
                                    disabled={terminalActionBusy === 'kill'}
                                    className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-red-300 hover:bg-red-950/35 hover:text-red-100 disabled:cursor-wait disabled:opacity-50"
                                  >
                                    <Power className="h-3.5 w-3.5 shrink-0" />
                                    <span>Kill tmux session</span>
                                  </button>
                                </>
                              )}
                            </div>
                          )}
                        </div>
                      )}
                      <label
                        className={`relative inline-flex h-6 w-7 items-center justify-center rounded border border-neutral-700/90 bg-[#101211] text-neutral-500 hover:bg-neutral-800/80 hover:text-neutral-100 focus-within:text-neutral-100 ${terminalTheme.inputFocus}`}
                        title={`Terminal profile: ${TERMINAL_COLOR_SCHEME_OPTIONS.find(option => option.value === terminalColorScheme)?.label || terminalColorScheme}`}
                        aria-label="Terminal profile"
                      >
                        <Palette className="pointer-events-none h-3.5 w-3.5" />
                        <select
                          value={terminalColorScheme}
                          onChange={event => {
                            const next = event.target.value
                            if (isTerminalColorScheme(next)) updateTerminalColorScheme(next)
                          }}
                          className="absolute inset-0 h-full w-full cursor-pointer opacity-0"
                        >
                          {TERMINAL_COLOR_SCHEME_OPTIONS.map(option => (
                            <option key={option.value} value={option.value}>
                              {option.label}
                            </option>
                          ))}
                        </select>
                      </label>
                      {canDismissTerminal(selectedTerminalView) && (
                        <button
                          type="button"
                          onClick={() => void dismissTerminal(selectedTerminalView)}
                          className="inline-flex items-center justify-center rounded border border-neutral-700/90 p-1 text-neutral-300 hover:bg-neutral-800/80 hover:text-neutral-100"
                          title="Remove terminal from UI"
                          aria-label="Remove terminal from UI"
                        >
                          <X className="h-3.5 w-3.5" />
                        </button>
                      )}
                    </div>
                  </div>
                  {terminalPreValidationSummary(selectedTerminalView) && (
                    <div className={`border-b border-neutral-700/70 bg-[#151716] px-3 py-1.5 ${terminalTheme.headerText} ${terminalPreValidationClass(selectedTerminalView, terminalTheme)}`}>
                      {terminalPreValidationSummary(selectedTerminalView)}
                    </div>
                  )}
                  {selectedTerminalErrors.length > 0 && (
                    <div className="flex flex-col gap-1 border-b border-red-900/45 bg-red-950/15 px-3 py-2">
                      {selectedTerminalErrors.map(entry => {
                        const isOpen = expandedErrorIDs.has(entry.id)
                        return (
                          <div key={entry.id} className="text-xs text-red-300">
                            <div className="flex min-w-0 items-center gap-2">
                              <AlertTriangle className="h-3.5 w-3.5 shrink-0 text-red-300" aria-hidden />
                              <span className="shrink-0 rounded bg-red-900/35 px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide text-red-200">
                                {entry.type.replace(/_/g, ' ')}
                              </span>
                              <span className="min-w-0 flex-1 truncate leading-5" title={entry.message}>
                                {compactTerminalErrorMessage(entry.message)}
                              </span>
                              <button
                                type="button"
                                onClick={() => toggleTerminalError(entry.id)}
                                className="shrink-0 rounded border border-red-800/60 px-2 py-0.5 text-[10px] font-medium text-red-200 hover:bg-red-900/35"
                                aria-expanded={isOpen}
                              >
                                {isOpen ? 'Close' : 'Open'}
                              </button>
                              <button
                                type="button"
                                onClick={() => dismissTerminalError(entry.id)}
                                className="shrink-0 rounded p-0.5 text-red-400/60 hover:bg-red-900/35 hover:text-red-200"
                                title="Dismiss"
                                aria-label="Dismiss terminal error"
                              >
                                <X className="h-3 w-3" />
                              </button>
                            </div>
                            {isOpen && (
                              <div className="mt-1 max-h-40 overflow-y-auto rounded border border-red-900/45 bg-red-950/25 p-2 font-mono text-[11px] leading-4 text-red-200">
                                {entry.message}
                              </div>
                            )}
                          </div>
                        )
                      })}
                    </div>
                  )}
                  {selectedTerminalIsSynthetic ? (
                    <StructuredTerminalView
                      content={selectedTerminalDisplayContent}
                      rows={selectedTerminalIsSynthetic && priorArchivedTurns.length === 0 ? selectedTerminalView.rows : undefined}
                      scrollRef={terminalOutputRef as React.RefObject<HTMLDivElement | null>}
                      onScroll={handleTerminalScroll}
                      onWheel={handleTerminalWheel as (e: React.WheelEvent<HTMLDivElement>) => void}
                      terminal={selectedTerminalView}
                      theme={terminalTheme}
                    />
                  ) : (
                    <XtermTerminalPane
                      contentRef={terminalOutputRef as React.RefObject<HTMLDivElement | null>}
                      content={selectedTerminalDisplayContent}
                      contentSource={selectedTerminalView?.content_source}
                      xtermTheme={XTERM_THEMES[terminalColorScheme]}
                      xtermProfile={XTERM_PROFILE_OPTIONS[terminalColorScheme]}
                      onViewportStickChange={handleXtermViewportStickChange}
                      debugLabel={selectedTerminalView ? terminalPaneKey(selectedTerminalView) : undefined}
                      className={`min-w-0 flex-1 overflow-hidden overscroll-contain font-mono text-neutral-100 ${XTERM_PROFILE_OPTIONS[terminalColorScheme].panePaddingClass} ${terminalTheme.selection}`}
                    />
                  )}
                  {selectedTerminalView && (() => {
                    const st = selectedTerminalView.status || {}
                    const tokensIn = formatTokens(st.input_tokens)
                    const tokensOut = formatTokens(st.output_tokens)
                    const cost = formatStatusFooterCost(st.cost_usd)
                    // Surface cache read/write tokens when the provider reports
                    // them, so this real-time telemetry isn't silently dropped.
                    const cacheParts: string[] = []
                    if (st.cache_read_input_tokens) cacheParts.push(`${formatTokens(st.cache_read_input_tokens)} cache`)
                    if (st.cache_creation_input_tokens) cacheParts.push(`${formatTokens(st.cache_creation_input_tokens)} cache-w`)
                    const cacheSeg = cacheParts.join(' · ')
                    const dur = typeof st.duration_ms === 'number' && st.duration_ms > 0
                      ? `${(st.duration_ms / 1000).toFixed(st.duration_ms < 10_000 ? 1 : 0)}s`
                      : ''
                    const toolCount = typeof st.tool_count === 'number' ? st.tool_count : 0
                    const tools = toolCount > 0 ? `${toolCount} ${toolCount === 1 ? 'tool' : 'tools'}` : ''
                    // Provider-agnostic statusline extras (e.g. plan rate-limit usage).
                    // Each CLI adapter normalizes its own schema into these display-ready
                    // segments upstream (status_meta.status_extras); render them verbatim
                    // with no per-provider knowledge here.
                    const rawExtras = (st.status_meta as Record<string, unknown> | undefined)?.status_extras
                    const extraSegs = Array.isArray(rawExtras)
                      ? rawExtras.filter((x): x is string => typeof x === 'string')
                      : []
                    const segments = [
                      st.provider_label || selectedTerminalView.label || selectedTerminalView.execution_kind || 'pane',
                      tools,
                      tokensIn !== '–' || tokensOut !== '–' ? `${tokensIn} in · ${tokensOut} out` : '',
                      cacheSeg,
                      cost,
                      dur,
                      ...extraSegs,
                    ].filter(Boolean)
                    return (
                      <div className={`flex items-center gap-2 border-t border-neutral-700/70 bg-[#101211] px-3 py-1 font-mono text-neutral-500 ${terminalTheme.footerText}`}>
                        <span className={isSelectedTerminalStreaming ? terminalTheme.streaming : 'text-neutral-600'}>
                          {isSelectedTerminalStreaming ? selectedTerminalSpinner : '·'}
                        </span>
                        <span className="truncate">{segments.join('  ·  ')}</span>
                      </div>
                    )
                  })()}
                </>
              ) : (
                <div className="flex flex-1 items-center justify-center text-xs text-neutral-500">
                  Select a terminal from the rail to view its content.
                </div>
              )}
            </div>
          </div>
        )}
          </>
        )}
      </div>
    </div>
  )
}
