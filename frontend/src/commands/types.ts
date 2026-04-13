import type { ReactNode } from 'react'
import type { ModeCategory } from '../stores/useModeStore'
import type { ExecutionOptions } from '../services/api-types'

export type WorkshopMode = 'builder' | 'optimizer' | 'debugger' | 'runner' | 'eval' | 'output'

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
  submitWithExecutionOptions?: (msg: string, executionOptions?: ExecutionOptions) => void
  getAppStore: () => any
  getWorkspaceStore: () => any
  getWorkflowStore: () => any
  workflowMode?: 'plan' | 'eval' | 'output'
  workshopMode?: 'builder' | 'optimizer' | 'debugger' | 'runner' | 'eval' | 'output'
  workflowPhaseId?: string
}

export interface CommandDefinition {
  command: string
  description: string
  icon: ReactNode
  modes?: ModeCategory[]
  requiredWorkflowMode?: 'plan' | 'eval' | 'output'
  requiredWorkshopMode?: WorkshopMode | WorkshopMode[]
  validate?: (ctx: CommandContext) => string | null
  hidden?: boolean
  source: 'builtin' | 'user'
  execute: (ctx: CommandContext) => void
}
