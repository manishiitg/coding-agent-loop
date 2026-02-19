import type { ReactNode } from 'react'
import type { ModeCategory } from '../stores/useModeStore'

export interface CommandContext {
  beforeSlash: string
  activeTabId: string
  tabSessionId: string | null
  tabConfig: any
  isSummarizing: boolean
  isStreaming: boolean
  onSubmit: (msg: string) => void
  openDialog: (name: any) => void
  setTabConfig: (tabId: string, config: any) => void
  addToast: (msg: string, type: 'success' | 'error' | 'info') => void
  handleSummarize: (ctx?: string) => void
  handleCompact: (ctx?: string) => void
  getAppStore: () => any
  getWorkspaceStore: () => any
}

export interface CommandDefinition {
  command: string
  description: string
  icon: ReactNode
  modes?: ModeCategory[]
  hidden?: boolean
  source: 'builtin' | 'user'
  execute: (ctx: CommandContext) => void
}
