import type { ReactNode } from 'react'
import type { ModeCategory } from '../stores/useModeStore'
import type { ExecutionOptions } from '../services/api-types'

// Visible workshop modes:
//   - 'builder'   — design the workflow plan, step config, and live report
//   - 'optimizer' — run / eval / harden existing steps until reliable
//   - 'run'       — execute and inspect; no plan / config changes
//
// Historical: 'eval' and 'output' modes folded into 'builder' in an earlier
// migration; 'ask' (formerly 'debugger') folded into 'run'. 'reporting' is
// still accepted for backend compatibility but the UI maps report authoring to
// Builder.
export type WorkshopMode = 'builder' | 'optimizer' | 'run' | 'reporting'

export interface CommandContext {
  beforeSlash: string
  activeTabId: string
  tabSessionId: string | null
  tabConfig: any
  isSummarizing: boolean
  isStreaming: boolean
  onSubmit: (msg: string) => void
  setInputText: (text: string) => void
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
  workshopMode?: WorkshopMode
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
