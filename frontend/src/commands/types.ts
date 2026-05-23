import type { ReactNode } from 'react'
import type { ModeCategory } from '../stores/useModeStore'
import type { ExecutionOptions } from '../services/api-types'

// Visible workshop modes in the main workshop UI:
//   - 'builder'   — design the workflow plan, step config, and live report
//   - 'optimizer' — run / eval / harden existing steps until reliable
//
// Historical: 'eval' and 'output' modes folded into 'builder' in an earlier
// migration; 'ask' (formerly 'debugger') folded into 'run'. 'reporting' is
// still accepted for backend compatibility but the UI maps report authoring to
// Builder. 'run' remains in the type for backend/bot routes such as Slack and
// WhatsApp, but it is not shown in the main workshop mode toggle.
// WorkshopMode values (post-merge):
//   - 'workshop' — the unified design+run+harden+replan+report mode.
//   - 'run' — read-mostly runtime (Slack/WhatsApp/deployed use).
//
// Legacy modes ("builder", "optimizer", "reporting") were merged into
// "workshop" in the prompt-restructure migration. Persisted sessions with
// the old names are normalized on the backend; the frontend never receives
// them as canonical values (loadtime migration in migrateWorkshopMode).
export type WorkshopMode = 'workshop' | 'run'

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
  openResumeDialog?: () => void
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
