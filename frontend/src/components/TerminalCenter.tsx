import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { AlertTriangle, ArrowDownToLine, Braces, Bug, Check, Copy, CornerDownLeft, CornerUpLeft, GitBranch, History, Info, Palette, Power, RefreshCw, Square, Terminal, X } from 'lucide-react'
import { agentApi } from '../services/api'
import type { PollingEvent, TerminalSnapshot } from '../services/api-types'
import { useGlobalPresetStore } from '../stores/useGlobalPresetStore'
import { useChatStore } from '../stores/useChatStore'
import { TERMINAL_REFRESH_REQUEST_EVENT } from '../utils/terminalRefresh'
import { MarkdownRenderer } from './ui/MarkdownRenderer'

interface TerminalCenterProps {
  currentSessionId?: string
  compact?: boolean
  hasConversationActivity?: boolean
}

const TERMINAL_REFRESH_HISTORY_LINES = 10000
const TERMINAL_DETAIL_CACHE_LIMIT = 40
const MAX_PRIOR_ARCHIVED_TURNS_TO_INLINE = 3
const PROMPT_COMPLETION_FALLBACK_SECONDS = 60
const INACTIVE_FALLBACK_SECONDS = 120
const EMPTY_TERMINAL_RESPONSE_GRACE_POLLS = 10
const TERMINAL_POLL_INTERVAL_MS = 1500
const TERMINAL_FAST_POLL_INTERVAL_MS = 300
const TERMINAL_FAST_POLL_DURATION_MS = 7000

type TerminalColorScheme = 'neon' | 'mono' | 'homebrew' | 'catppuccin' | 'nord' | 'gruvbox' | 'solarized' | 'tokyo'

const DEFAULT_TERMINAL_COLOR_SCHEME: TerminalColorScheme = 'homebrew'
const TERMINAL_COLOR_SCHEME_STORAGE_KEY = 'terminal-color-scheme'

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
  { value: 'homebrew', label: 'Homebrew' },
  { value: 'mono', label: 'Mono' },
  { value: 'catppuccin', label: 'Catppuccin' },
  { value: 'nord', label: 'Nord' },
  { value: 'gruvbox', label: 'Gruvbox' },
  { value: 'solarized', label: 'Solarized' },
  { value: 'tokyo', label: 'Tokyo Night' },
  { value: 'neon', label: 'Neon' },
]

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
}

interface RoutingDecision {
  id: string
  stepId?: string
  stepTitle?: string
  selectedRouteId: string
  selectedRouteName?: string
  nextStepId?: string
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
  return `Routing: ${label}${decision.nextStepId ? ` -> ${decision.nextStepId}` : ''}`
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
    return terminal.step_id || terminal.step_name || (isOpaqueID(rawLabel) ? '' : humanizeIdentifier(rawLabel))
  }
  return terminal.step_name || terminal.agent_name || visibleStepID(terminal) || (isOpaqueID(rawLabel) ? '' : humanizeIdentifier(rawLabel))
}

function formatTerminalTitle(terminal: TerminalSnapshot): string {
  // Title is just the step_id (the most useful identifier). Everything
  // else — parent, chip, workflow name, kind — moves to the meta row
  // so the title stays minimal and scannable in dense lists.
  const kind = (terminal.execution_kind || terminal.scope || '').toLowerCase()
  if (isMainAgentTerminal(terminal)) {
    return terminal.agent_name || terminal.step_name || 'Main agent'
  }
  if (kind === 'background_agent' || kind === 'background' || kind === 'delegation' || kind === 'todo_task' || kind === 'sub_agent') {
    return terminal.agent_name || terminal.step_name || terminal.display_title || visibleStepID(terminal) || formatTerminalKindLabel(terminal) || 'Terminal'
  }
  return visibleStepID(terminal) || terminal.step_name || formatTerminalKindLabel(terminal) || terminal.display_title || 'Terminal'
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

function terminalStepTypeLabel(terminal: TerminalSnapshot): string {
  const type = (terminal.step_type || '').trim()
  return type ? `${humanizeIdentifier(type)} step` : ''
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
  const state = terminalState(terminal)
  return state === 'completed' || state === 'closing' || state === 'failed' || state === 'stale'
}

function canForceCompleteTerminal(terminal: TerminalSnapshot): boolean {
  const state = terminalState(terminal)
  return state === 'running' || state === 'stale'
}

function canSendTerminalDebugInput(terminal: TerminalSnapshot): boolean {
  return Boolean(terminal.tmux_session) && terminalState(terminal) === 'running'
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

function TerminalStepTypeIcon({ terminal }: { terminal: TerminalSnapshot }) {
  const type = (terminal.step_type || '').toLowerCase()
  if (!type) return null

  const label = terminalStepTypeLabel(terminal)
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
    <span
      className="inline-flex h-4 w-4 shrink-0 items-center justify-center rounded border border-neutral-700/70 bg-neutral-900/80 text-neutral-400"
      title={label}
      aria-label={label}
    >
      {icon}
    </span>
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

function terminalWithCachedBody(base: TerminalSnapshot, detail: TerminalSnapshot): TerminalSnapshot {
  return {
    ...base,
    content: detail.content || base.content || '',
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
// Structured terminal view — parses the synthetic terminal's plain-text
// buffer into typed rows so we can colorize roles and fold long tool I/O
// behind a one-line summary. Tmux pane scrapes skip this and keep the
// raw <pre> rendering: they're literal screen captures and adding any
// frontend interpretation breaks the "this is exactly what the CLI saw"
// contract.
// ---------------------------------------------------------------------------

type TerminalRow =
  | { kind: 'banner'; text: string }
  | { kind: 'context'; text: string }
  | { kind: 'user'; text: string }
  | { kind: 'asst'; text: string }
  | { kind: 'tool'; name: string; args: string; result?: string; resultPrefix?: '✓' | '✗'; result_prefix?: '✓' | '✗' | string }
  | { kind: 'attachment'; text: string }
  | { kind: 'done'; text: string }
  | { kind: 'error'; text: string }
  | { kind: 'plain'; text: string }

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

function classifyTerminalLine(line: string): TerminalRow {
  if (line.startsWith('$ ')) return { kind: 'banner', text: line.slice(2) }
  if (line.startsWith('↳ ')) return { kind: 'context', text: line.slice(2) }
  if (line.startsWith('> user: ')) return { kind: 'user', text: line.slice(8) }
  if (line.startsWith('< asst: ')) return { kind: 'asst', text: line.slice(8) }
  if (line.startsWith('  ')) return { kind: 'asst', text: line.slice(2) }
  if (line.startsWith('[image ')) return { kind: 'attachment', text: line }
  if (line.startsWith('[document ')) return { kind: 'attachment', text: line }
  if (line.startsWith('[done')) return { kind: 'done', text: line }
  if (line.startsWith('[error]')) return { kind: 'error', text: line.slice(7).trim() }
  // Tool start: "→ tool: name(args)" or "→ name args"
  if (line.startsWith('→ ')) {
    const rest = line.slice(2)
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
  return { kind: 'plain', text: line }
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
  for (const line of lines) {
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
        activeTextRow.text = activeTextRow.text ? `${activeTextRow.text}\n${continuation}` : continuation
        continue
      }
      activeTextRowIndex = null
    }
    const classified = classifyTerminalLine(line)
    activeToolResultIndex = null
    // Coalesce consecutive assistant continuation lines into one row. Join
    // with a newline (not a space) so the markdown structure that the model
    // emitted — lists, paragraph breaks, fenced code, headings — survives
    // and ReactMarkdown can render it. An empty continuation line becomes a
    // blank line, which markdown reads as a paragraph break.
    if (classified.kind === 'asst' && rows.length > 0 && rows[rows.length - 1].kind === 'asst') {
      const prev = rows[rows.length - 1] as { kind: 'asst'; text: string }
      prev.text = prev.text ? `${prev.text}\n${classified.text}` : classified.text
      activeTextRowIndex = rows.length - 1
      continue
    }
    if (classified.kind === 'asst' && line.startsWith('  ')) {
      rows.push({ kind: 'plain', text: line })
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

function normalizeTerminalRows(rows: TerminalSnapshot['rows'] | undefined): TerminalRow[] {
  if (!Array.isArray(rows)) return []
  const normalized: TerminalRow[] = []
  for (const row of rows) {
    switch (row.kind) {
      case 'banner':
      case 'context':
      case 'user':
      case 'asst':
      case 'attachment':
      case 'done':
      case 'error':
      case 'plain':
        normalized.push({ kind: row.kind, text: row.text || '' } as TerminalRow)
        break
      case 'tool':
        normalized.push({
          kind: 'tool',
          name: row.name || 'tool',
          args: row.args || '',
          result: row.result,
          resultPrefix: row.result_prefix === '✗' ? '✗' : row.result_prefix === '✓' ? '✓' : undefined,
          result_prefix: row.result_prefix,
        })
        break
      default:
        normalized.push({ kind: 'plain', text: row.text || '' })
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
    <div className="min-h-0 min-w-0 flex-1 flex flex-col overflow-hidden bg-[#0b0d0c]">
      <div
        ref={scrollRef}
        onScroll={onScroll}
        onWheel={onWheel}
        className={`min-w-0 flex-1 overflow-y-auto overflow-x-hidden overscroll-contain px-3 py-2.5 font-mono ${theme.contentText} ${theme.selection}`}
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
                    <span className="text-neutral-500">$ </span>{row.text}
                  </div>
                </div>
              )
            case 'context':
              return <div key={idx} className="text-neutral-500">↳ {row.text}</div>
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
              return <div key={idx} className="text-neutral-500">{row.text}</div>
            case 'done':
              return <div key={idx} className={`${theme.done} mt-1 ${theme.doneText} font-mono`}>{row.text}</div>
            case 'error':
              return <div key={idx} className="text-red-400">[error] {row.text}</div>
            case 'plain':
              return <div key={idx} className="text-neutral-300 whitespace-pre-wrap break-words">{row.text}</div>
          }
        })}
        {isStreaming && (
          <div className={`mt-1 font-mono ${theme.streaming}`} aria-label="Running">
            {spinner}
          </div>
        )}
      </div>
      {terminal && (() => {
        const st = terminal.status || {}
        const tokensIn = formatTokens(st.input_tokens)
        const tokensOut = formatTokens(st.output_tokens)
        const cost = formatStatusFooterCost(st.cost_usd)
        const dur = typeof st.duration_ms === 'number' && st.duration_ms > 0
          ? `${(st.duration_ms / 1000).toFixed(st.duration_ms < 10_000 ? 1 : 0)}s`
          : ''
        const toolCount = typeof st.tool_count === 'number' ? st.tool_count : 0
        const tools = toolCount > 0 ? `${toolCount} ${toolCount === 1 ? 'tool' : 'tools'}` : ''
        const segments = [
          st.provider_label || terminal.label || terminal.execution_kind || 'pane',
          tools,
          tokensIn !== '–' || tokensOut !== '–' ? `${tokensIn} in · ${tokensOut} out` : '',
          cost,
          dur,
        ].filter(Boolean)
        return (
          <div className={`flex items-center gap-2 border-t border-neutral-700/70 bg-[#101211] px-3 py-1 font-mono text-neutral-500 ${theme.footerText}`}>
            <span className={isStreaming ? theme.streaming : 'text-neutral-600'}>
              {isStreaming ? spinner : '·'}
            </span>
            <span className="truncate">{segments.join('  ·  ')}</span>
          </div>
        )
      })()}
    </div>
  )
}

function isSyntheticTerminal(terminal: TerminalSnapshot): boolean {
  const transport = (terminal.step_transport || '').toLowerCase()
  if (transport === 'tmux') return false
  if (transport === 'api' || transport === 'structured' || transport === 'structured_cli' || transport === 'non_tmux') return true
  // Fall back to tmux_session presence — pane scrapes always have one.
  return !terminal.tmux_session
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
    metadata?.gemini_interactive_session,
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
  const [error, setError] = useState<string | null>(null)
  const [terminalActionBusy, setTerminalActionBusy] = useState<string | null>(null)
  const [debugPanelOpenForID, setDebugPanelOpenForID] = useState<string | null>(null)
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

  const sendTerminalDebugKey = useCallback(async (terminal: TerminalSnapshot, key: 'enter' | 'esc' | 'ctrl-c') => {
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
          out.push({ kind: 'route', decision: routeDecision, depth })
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
    // Build a single tree from ALL terminals (active + finished).
    // Splitting them was breaking the parent→child relationship when
    // a child step finished while its parent was still running — the
    // child got displaced into the "Finished" group, losing its
    // visual nesting under the parent. One tree keeps lineage intact;
    // the colored dot on each rail row already conveys per-row state.
    const allTerminals = sortTerminalsForRail(railTerminals)
    const activeTerminals = railTerminals.filter(terminal => terminalState(terminal) === 'running')
    const finishedTerminals = railTerminals.filter(terminal => terminalState(terminal) !== 'running')
    const currentTerminals = sortTerminalsNewestFirst(railTerminals.filter(terminal => !isArchivedTurnTerminal(terminal)))
    return {
      activeTerminals,
      finishedTerminals,
      currentTerminals,
      orderedTerminals: allTerminals,
      tree: buildTree(allTerminals, routingDecisionByNextStepID, routingDecisions),
    }
  }, [terminals, routingDecisionByNextStepID, routingDecisions])
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
      return terminalWithCachedBody(selectedTerminal, cachedDetail)
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
        const detail = await agentApi.getTerminal(t.terminal_id)
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
  const selectedRouteDecision = selectedTerminalView?.step_id
    ? routingDecisionByNextStepID.get(selectedTerminalView.step_id)
    : undefined
  const selectedTerminalErrors = selectedTerminalView
    ? terminalErrorsByID.get(terminalPaneKey(selectedTerminalView)) || []
    : []
  const railSpinner = useSpinnerFrame(groupedTerminals.activeTerminals.length > 0)

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
    agentApi.getTerminal(selectedTerminal.terminal_id)
      .then(detail => {
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

  // For inactive tmux terminals (e.g. after Claude Code context compaction), there
  // are no streaming events to bump chunk_index, so the detail cache never expires
  // and the UI shows stale content. Probe the backend every 3s — the GET endpoint
  // captures a live tmux snapshot for inactive terminals, which increments chunk_index
  // if content changed. The updated chunk_index flows back through the list poll and
  // re-triggers the detail fetch above, creating a self-sustaining refresh loop until
  // the content stabilises.
  useEffect(() => {
    const terminalId = selectedTerminalView?.terminal_id
    const tmuxSession = selectedTerminalView?.tmux_session
    const state = selectedTerminalView ? terminalState(selectedTerminalView) : ''
    // Stale/failed terminals have no live tmux pane to recapture; probing them
    // just loops over a dead session (the backend now marks such GETs stale).
    const isInactiveTmux = !selectedTerminalView?.active && !!tmuxSession && state !== 'stale' && state !== 'failed'
    if (!terminalId || !isInactiveTmux) return

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
      void agentApi.getTerminal(terminalId, { content: 'tmux' }).then(detail => {
        cacheTerminalDetail(detail)
        // The detail carries the freshest active/state — react to it directly
        // instead of waiting a full list-poll cycle for selectedTerminalView to
        // catch up. Re-activated (compaction resumed) or dead (stale) ⇒ stop.
        const detailState = terminalState(detail)
        if (detail.active || detailState === 'stale' || detailState === 'failed') {
          stop()
        }
        void fetchTerminals()
      }).catch(() => { /* stale or closed session — ignore */ })
        .finally(() => { probeInFlightRef.current = false })
    }

    interval = window.setInterval(probe, 3000)
    return () => stop()
  }, [selectedTerminalView?.terminal_id, selectedTerminalView?.tmux_session, selectedTerminalView?.active, cacheTerminalDetail, fetchTerminals])

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

  // Dynamic tmux resize: measure the terminal viewport in monospace chars and
  // POST the dimensions to /api/terminals/{id}/resize. The backend runs
  // `tmux resize-window` on the live pane AND records the size as the
  // process-wide preferred size, so the next CLI-agent tmux session launches
  // at the operator's actual viewport instead of the 160×48 default. Only
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
        <span className="min-w-0 truncate">
          {decision.nextStepId ? `next ${decision.nextStepId}` : decision.stepTitle || decision.stepId || 'route selected'}
        </span>
        {decision.routeCount > 1 && (
          <span className="shrink-0">· {decision.routeCount} routes</span>
        )}
      </div>
    </div>
  )

  const renderRailItem = (terminal: TerminalSnapshot, depth: number = 0) => (
    (() => {
      const preValidationChip = terminalPreValidationChip(terminal, terminalTheme)
      const state = terminalState(terminal)
      const isRunning = state === 'running'
      const startedTimestamp = formatStartedTimestamp(terminal)
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
          <div className={`mt-0.5 flex items-center gap-1.5 opacity-70 ${terminalTheme.railMetaText}`}>
            {stepTypeLabel && (
              <span className="shrink-0 text-neutral-400">{stepTypeLabel}</span>
            )}
            {railTransport && (
              <span className="min-w-0 truncate text-neutral-500" title={formatTransportChip(terminal)}>
                {stepTypeLabel ? `· ${railTransport}` : railTransport}
              </span>
            )}
            {startedTimestamp && (
              <span className="shrink-0 text-neutral-500" title={startedTimestamp.title}>
                · {startedTimestamp.label}
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
            {isArchivedTurnTerminal(terminal) && (
              <span
                className="inline-flex h-4 w-4 shrink-0 items-center justify-center rounded border border-neutral-700/80 text-neutral-500"
                title="Previous turn"
                aria-label="Previous turn"
              >
                <History className="h-2.5 w-2.5" />
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
            <div className="hidden w-48 shrink-0 flex-col overflow-y-auto overflow-x-hidden border-r border-neutral-700/70 bg-[#141615] sm:flex">
              {groupedTerminals.tree.map(item => (
                item.kind === 'route'
                  ? renderRouteRailItem(item.decision, item.depth)
                  : renderRailItem(item.terminal, item.depth)
              ))}
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
                        title={`Terminal color theme: ${TERMINAL_COLOR_SCHEME_OPTIONS.find(option => option.value === terminalColorScheme)?.label || terminalColorScheme}`}
                        aria-label="Terminal color theme"
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
                  {isSyntheticTerminal(selectedTerminalView) ? (
                    <StructuredTerminalView
                      content={selectedTerminalDisplayContent}
                      rows={priorArchivedTurns.length === 0 ? selectedTerminalView.rows : undefined}
                      scrollRef={terminalOutputRef as React.RefObject<HTMLDivElement | null>}
                      onScroll={handleTerminalScroll}
                      onWheel={handleTerminalWheel as (e: React.WheelEvent<HTMLDivElement>) => void}
                      terminal={selectedTerminalView}
                      theme={terminalTheme}
                    />
                  ) : (
                    <pre
                      ref={terminalOutputRef as React.RefObject<HTMLPreElement | null>}
                      onScroll={handleTerminalScroll}
                      onWheel={handleTerminalWheel}
                      className={`min-w-0 flex-1 overflow-y-auto overflow-x-hidden overscroll-contain bg-[#0b0d0c] p-2.5 font-mono whitespace-pre-wrap break-words text-neutral-100 ${terminalTheme.contentText} ${terminalTheme.selection}`}
                    >
                      {selectedTerminalDisplayContent}
                    </pre>
                  )}
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
