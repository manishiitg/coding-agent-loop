import React, { useRef, useCallback, useMemo, useState, useEffect, useLayoutEffect } from 'react'

const DBG = '[skill-popup]'
import { Send, Square, Code2, Sparkles, Wand2, Loader2, Search, Globe, Layers, X, History, Bot, Server, Download, Paperclip, CalendarClock, MessageSquare, Trash2, Terminal } from 'lucide-react'
import { Button } from './ui/Button'
import { Textarea } from './ui/Textarea'
import FileContextDisplay from './FileContextDisplay'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from './ui/tooltip'
import { CircularProgress } from './ui/CircularProgress'
import { getEventData, isEventType } from '../generated/event-types'
import type { TokenUsageEvent } from '../generated/events'
import ServerSelectionDropdown from './ServerSelectionDropdown'
import SkillSelectionDropdown from './skills/SkillSelectionDropdown'
import SecretSelectionDropdown from './secrets/SecretSelectionDropdown'
import LLMSelectionDropdown from './LLMSelectionDropdown'
import FileSelectionDialog from './FileSelectionDialog'
import CommandSelectionDialog from './CommandSelectionDialog'
import { CommandEditorDialog } from './commands/CommandEditorDialog'
import { findCommand, findCommandAnyMode, loadAndRegisterUserCommands, type CommandContext, type CommandDefinition } from '../commands'
import { commandsApi } from '../api/commands'
import WorkflowSelectionDialog from './WorkflowSelectionDialog'
import { isChatCompatiblePhase } from '../utils/chatSubmitHelpers'
import { useWorkflowStore } from '../stores/useWorkflowStore'
import { useWorkflowManifestStore } from '../stores/useWorkflowManifestStore'
import { useAuthStore } from '../stores/useAuthStore'
import { hasWorkflowWriteAccess } from '../utils/workflowPermissions'

const WORKSHOP_MODE_SWITCH_CONFIRM_KEY = 'workflow_workshop_mode_switch_confirm_dismissed'
type VisibleWorkshopMode = 'builder' | 'optimizer' | 'run'

const removePasteMarkersFromText = (text: string, markers: string[]) => {
  return markers.reduce((next, marker) => {
    const escaped = marker.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    return next
      .replace(new RegExp(escaped, 'g'), '')
      .replace(/[ \t]{2,}/g, ' ')
      .replace(/[ \t]+\n/g, '\n')
      .replace(/\n[ \t]+/g, '\n')
      .trim()
  }, text)
}

function WorkshopModeToggle() {
  const canWriteWorkflow = useAuthStore(state => hasWorkflowWriteAccess(state.user, state.isMultiUserMode))
  const activePresetId = useGlobalPresetStore(state => state.activePresetIds.workflow)
  const workflowMode = useWorkflowStore(state => state.workflowMode)
  const setWorkflowMode = useWorkflowStore(state => state.setWorkflowMode)
  const workshopMode = useWorkflowStore(state => {
    return (activePresetId && state.workshopModeByPreset[activePresetId]) || state.workshopMode
  })
  const setWorkshopMode = useWorkflowStore(state => state.setWorkshopMode)
  const [pendingMode, setPendingMode] = useState<VisibleWorkshopMode | null>(null)
  const [dontAskAgain, setDontAskAgain] = useState(false)

  useEffect(() => {
    if (!canWriteWorkflow && workshopMode !== 'run') {
      setWorkflowMode('plan')
      setWorkshopMode('run')
    }
  }, [canWriteWorkflow, setWorkflowMode, setWorkshopMode, workshopMode])

  const persistWorkshopMode = (mode: string) => {
    if (!activePresetId) return
    const workspacePath = useWorkflowManifestStore.getState().getWorkflowById(activePresetId)?.workspace_path
    if (!workspacePath) return
    agentApi.updateWorkflowManifest({ workspace_path: workspacePath, workshop_mode: mode }).catch(() => {
      // Non-fatal: localStorage still tracks the mode; persist is best-effort
    })
  }

  const applyWorkshopMode = (mode: VisibleWorkshopMode) => {
    setWorkflowMode('plan')
    setWorkshopMode(mode)
    if (canWriteWorkflow) {
      persistWorkshopMode(mode)
    }
  }

  const requestWorkshopMode = (mode: VisibleWorkshopMode) => {
    if (workshopMode === mode) return
    let skipConfirm = false
    try {
      skipConfirm = localStorage.getItem(WORKSHOP_MODE_SWITCH_CONFIRM_KEY) === 'true'
    } catch {
      skipConfirm = false
    }
    if (skipConfirm) {
      applyWorkshopMode(mode)
      return
    }
    setDontAskAgain(false)
    setPendingMode(mode)
  }

  const confirmModeSwitch = () => {
    if (!pendingMode) return
    if (dontAskAgain) {
      try {
        localStorage.setItem(WORKSHOP_MODE_SWITCH_CONFIRM_KEY, 'true')
      } catch {
        // ignore storage failures; the switch itself should still work
      }
    }
    applyWorkshopMode(pendingMode)
    setPendingMode(null)
  }

  const pendingModeLabel = pendingMode === 'optimizer' ? 'Optimize' : pendingMode === 'run' ? 'Run' : 'Builder'

  const builderModes = [
    { id: 'builder' as const, label: 'Builder', title: 'Builder', description: 'Design the workflow plan, step config, and live report dashboard.' },
    { id: 'optimizer' as const, label: 'Optimize', title: 'Optimize', description: 'Harden existing steps — run, evaluate, fix, repeat until reliable.' },
    { id: 'run' as const, label: 'Run', title: 'Run', description: 'Use the workflow without builder or optimizer write tools.' },
  ].filter(mode => canWriteWorkflow || mode.id === 'run')

  return (
    <TooltipProvider delayDuration={120}>
      <div className="relative flex items-center gap-2">
        <div className="flex items-center rounded-md border border-border overflow-hidden text-xs font-medium">
          {builderModes.map(({ id, label, title, description }) => (
            <Tooltip key={id}>
              <TooltipTrigger asChild>
                <button
                  type="button"
                  onClick={() => {
                    requestWorkshopMode(id)
                  }}
                  className={`px-2.5 py-1 transition-colors ${
                    workshopMode === id
                      ? 'bg-primary text-primary-foreground'
                      : 'bg-background text-muted-foreground hover:text-foreground hover:bg-muted'
                  }`}
                  aria-label={title}
                >
                  {label}
                </button>
              </TooltipTrigger>
              <TooltipContent>
                <p>{description}</p>
              </TooltipContent>
            </Tooltip>
          ))}
        </div>
        {pendingMode && (
          <div className="absolute bottom-full left-0 z-50 mb-2 w-[20rem] rounded-md border border-gray-200 bg-white p-3 text-xs shadow-lg dark:border-gray-700 dark:bg-gray-900">
            <div className="font-semibold text-gray-900 dark:text-gray-100">
              Switch to {pendingModeLabel}?
            </div>
            <div className="mt-1 leading-5 text-gray-600 dark:text-gray-300">
              Full chat history is not passed between Builder and Optimize. The next mode receives a compact summary.
            </div>
            <label className="mt-2 flex items-center gap-2 text-gray-600 dark:text-gray-300">
              <input
                type="checkbox"
                checked={dontAskAgain}
                onChange={(event) => setDontAskAgain(event.target.checked)}
                className="h-3.5 w-3.5 rounded border-gray-300 text-blue-600 focus:ring-blue-500"
              />
              Do not ask again
            </label>
            <div className="mt-3 flex justify-end gap-2">
              <button
                type="button"
                onClick={() => setPendingMode(null)}
                className="rounded px-2 py-1 font-medium text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={confirmModeSwitch}
                className="rounded bg-blue-600 px-2 py-1 font-medium text-white hover:bg-blue-700"
              >
                Switch
              </button>
            </div>
          </div>
        )}
      </div>
    </TooltipProvider>
  )
}
import InlineSelectionPopup from './InlineSelectionPopup'
import type { InlineSelectionFilterTab, InlineSelectionItem } from './InlineSelectionPopup'
import SkillImportDialog from './skills/SkillImportDialog'
import { MCPConfigPopup } from './MCPConfigPopup'
import MCPDetailsModal from './MCPDetailsModal'
import LLMConfigurationModal from './LLMConfigurationModal'
import type { PlannerFile, PollingEvent, LLMProvider, ChatHistorySession } from '../services/api-types'
import type { LLMOption } from '../types/llm'
import { useAppStore, useMCPStore, useLLMStore, useChatStore } from '../stores'
import { useCapabilitiesStore } from '../stores/useCapabilitiesStore'
import { useWorkspaceStore } from '../stores/useWorkspaceStore'
import { useCommandDialogStore } from '../stores/useCommandDialogStore'
import { usePresetApplication, useGlobalPresetStore } from '../stores/useGlobalPresetStore'
import { useModeStore } from '../stores/useModeStore'
import { agentApi, getApiBaseUrl } from '../services/api'
import { skillsApi } from '../api/skills'
import type { Skill } from '../types/skills'

// MCP servers managed by dedicated toolbar buttons — excluded from the general server dropdown.
const DEDICATED_MCP_SERVERS = new Set(['playwright'])
const AUTO_NOTIFICATION_PREFIX = '[AUTO-NOTIFICATION]'

const formatResumeChatTime = (value?: string): string => {
  if (!value) return 'Unknown time'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return 'Unknown time'
  return date.toLocaleString([], {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

const resumeChatTitle = (session: ChatHistorySession): string => {
  const query = session.query?.replace(/\s+/g, ' ').trim()
  if (query) return query.length > 140 ? `${query.slice(0, 140)}...` : query
  return `${(session.agent_mode || 'chat').replace(/_/g, ' ')} ${session.session_id.slice(0, 8)}`
}

const resumeChatConversationPath = (session: ChatHistorySession): string => {
  if (session.conversation_path) return session.conversation_path
  const userId = session.user_id || 'default'
  return `_users/${userId}/chat_history/${session.session_id}/conversation.json`
}

const resumeChatRuntimeLabel = (session: ChatHistorySession): string | undefined => {
  const runtime = session.runtime
  const provider = runtime?.provider?.trim()
  if (!runtime || !provider) return undefined

  const model = runtime.model_id?.trim()
  if (model && model !== provider) return `${provider} · ${model}`
  return provider
}

const resumeChatWorkshopModeLabel = (session: ChatHistorySession): string | undefined => {
  const raw = (session.runtime?.workshop_mode || session.workshop_mode || '').trim().toLowerCase()
  if (!raw) return undefined
  if (raw === 'optimizer') return 'Optimizer'
  if (raw === 'builder') return 'Builder'
  if (raw === 'run') return 'Run'
  if (raw === 'reporting') return 'Reporting'
  return raw.replace(/_/g, ' ')
}

const resumeChatDetails = (session: ChatHistorySession): React.ReactNode | undefined => {
  const messages = (session.preview_messages || [])
    .filter(message => message.text?.trim())
    .slice(-6)

  if (messages.length === 0) return undefined

  return (
    <div className="space-y-2 rounded-md border border-border bg-background/80 p-2 text-xs text-foreground shadow-sm">
      {messages.map((message, index) => {
        const normalizedRole = message.role === 'ai' || message.role === 'assistant' ? 'Assistant' : 'User'
        const roleClass = normalizedRole === 'Assistant'
          ? 'text-emerald-600 dark:text-emerald-400'
          : 'text-sky-600 dark:text-sky-400'

        return (
          <div key={`${session.session_id}-preview-${index}`} className="space-y-0.5">
            <div className={`text-[10px] font-semibold uppercase tracking-wide ${roleClass}`}>
              {normalizedRole}
            </div>
            <div className="line-clamp-3 whitespace-pre-wrap break-words text-muted-foreground">
              {message.text}
            </div>
          </div>
        )
      })}
    </div>
  )
}

type ResumeSessionKind = 'chat' | 'schedule' | 'bot'
type ResumeFilter = ResumeSessionKind | 'all'

const getResumeSessionKind = (session: ChatHistorySession): ResumeSessionKind => {
  if (session.session_id.startsWith('schedule-cron--')) return 'schedule'
  if (session.session_id.startsWith('bot-')) return 'bot'
  return 'chat'
}

export interface ActiveAgentInfo {
  id: string
  name: string
  type: 'agent' | 'delegation'
  depth: number
  treePrefix: string
}

interface ChatInputProps {
  // Handlers (callbacks only)
  onSubmit: (query: string) => void
  onStopStreaming: () => void
  activeAgents?: ActiveAgentInfo[]
}

// Stable empty array reference to avoid infinite loops in selectors
const EMPTY_EVENTS: never[] = []

function isAutoNotificationMessage(msg: string): boolean {
  return msg.startsWith(AUTO_NOTIFICATION_PREFIX)
}

function compactActiveAgentName(name: string, maxLength = 96): string {
  const normalized = name.replace(/\s+/g, ' ').trim()
  if (normalized.length <= maxLength) return normalized
  return `${normalized.slice(0, maxLength - 3)}...`
}

function summarizeAutoNotification(msg: string): {
  headline: string
  detail: string
  status: 'completed' | 'failed' | 'other'
} {
  const lines = msg
    .split('\n')
    .map(line => line.trim())
    .filter(Boolean)

  const headline = (lines[0] || 'Auto-notification')
    .replace(AUTO_NOTIFICATION_PREFIX, '')
    .trim()
  const detail = lines.slice(1).join(' ')
  const status = headline.includes('FAILED')
    ? 'failed'
    : (headline.includes('COMPLETED') || headline.includes('COMPLETE'))
      ? 'completed'
      : 'other'

  return { headline, detail, status }
}

const SteerQueueButton: React.FC<{
  onClick: () => void
  isSteering?: boolean
  className?: string
}> = ({ onClick, isSteering, className = '' }) => (
  <Tooltip>
    <TooltipTrigger asChild>
      <button
        type="button"
        onClick={onClick}
        disabled={isSteering}
        className={`inline-flex items-center gap-1 rounded border border-slate-300 bg-transparent px-1.5 py-0 text-[10px] font-medium leading-4 text-slate-500 transition-colors hover:border-slate-400 hover:text-slate-700 dark:border-slate-600 dark:bg-transparent dark:text-slate-400 dark:hover:border-slate-500 dark:hover:text-slate-200 disabled:opacity-50 ${className}`}
        aria-label="Steer this queued message into the running conversation"
      >
        {isSteering ? (
          <>
            <svg className="h-3 w-3 animate-spin" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
              <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
              <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
            </svg>
            <span>Steering...</span>
          </>
        ) : (
          <span>Steer</span>
        )}
      </button>
    </TooltipTrigger>
    <TooltipContent side="top" className="max-w-64 text-xs">
      <p>Inject this queued message into the currently running agent. It shows up in chat when the model actually picks it up.</p>
    </TooltipContent>
  </Tooltip>
)

// Collapsible queued message item — shows preview for long messages with expand/collapse toggle
const QueuedMessageItem: React.FC<{
  index: number
  msg: string
  preview: string
  isLong: boolean
  onDelete: () => void
  onSteer?: () => void
  isSteering?: boolean
}> = ({ index, msg, preview, isLong, onDelete, onSteer, isSteering }) => {
  const [expanded, setExpanded] = useState(false)
  return (
    <div className="flex items-start gap-2 px-2 py-0.5 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded text-xs text-blue-700 dark:text-blue-300">
      <div className="w-1.5 h-1.5 bg-blue-500 rounded-full animate-pulse mt-1.5 flex-shrink-0"></div>
      <div className="flex-1 min-w-0">
        {expanded ? (
          <div className="max-h-48 overflow-y-auto break-words whitespace-pre-wrap pr-1">
            <span className="font-medium">#{index + 1}:</span> {msg}
          </div>
        ) : (
          <span className="break-words whitespace-pre-wrap">
            <span className="font-medium">#{index + 1}:</span> {preview}
          </span>
        )}
        {isLong && (
          <button
            type="button"
            onClick={() => setExpanded(!expanded)}
            className="ml-1 text-blue-500 dark:text-blue-400 hover:text-blue-700 dark:hover:text-blue-200 underline"
          >
            {expanded ? 'collapse' : 'expand'}
          </button>
        )}
      </div>
      {onSteer && (
        <SteerQueueButton
          onClick={onSteer}
          isSteering={isSteering}
          className="self-center flex-shrink-0"
        />
      )}
      <button
        type="button"
        onClick={onDelete}
        className="flex items-center justify-center w-5 h-5 self-center rounded hover:bg-blue-200 dark:hover:bg-blue-800 text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-200 transition-colors flex-shrink-0"
        title="Delete from queue"
      >
        <svg xmlns="http://www.w3.org/2000/svg" className="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
        </svg>
      </button>
    </div>
  )
}

const QueuedAutoNotificationGroup: React.FC<{
  items: Array<{ index: number; msg: string }>
  onDelete: (index: number) => void
  onSteer?: (index: number, msg: string) => void
  steeringIndex: number | null
}> = ({ items, onDelete, onSteer, steeringIndex }) => {
  const [expanded, setExpanded] = useState(false)

  const summaries = useMemo(() => items.map(item => ({
    ...item,
    ...summarizeAutoNotification(item.msg),
  })), [items])

  const completedCount = summaries.filter(item => item.status === 'completed').length
  const failedCount = summaries.filter(item => item.status === 'failed').length
  const otherCount = summaries.length - completedCount - failedCount
  const preview = summaries
    .slice(0, 2)
    .map(item => item.headline)
    .join(' • ')

  return (
    <div className="px-2 py-1.5 bg-slate-50 dark:bg-slate-900/20 border border-slate-200 dark:border-slate-800 rounded text-xs text-slate-700 dark:text-slate-300">
      <div className="flex items-start gap-2">
        <div className="w-1.5 h-1.5 bg-slate-500 rounded-full mt-1.5 flex-shrink-0"></div>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-1.5 flex-wrap">
            <span className="font-medium">{items.length} auto-update{items.length === 1 ? '' : 's'} queued</span>
            {completedCount > 0 && (
              <span className="px-1.5 py-0.5 rounded bg-green-100 text-green-700 dark:bg-green-900/40 dark:text-green-300">
                {completedCount} done
              </span>
            )}
            {failedCount > 0 && (
              <span className="px-1.5 py-0.5 rounded bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300">
                {failedCount} failed
              </span>
            )}
            {otherCount > 0 && (
              <span className="px-1.5 py-0.5 rounded bg-slate-200 text-slate-700 dark:bg-slate-800 dark:text-slate-300">
                {otherCount} other
              </span>
            )}
          </div>
          {!expanded && (
            <div className="mt-1 text-[11px] text-slate-600 dark:text-slate-400 break-words">
              {preview}
              {items.length > 2 ? ` +${items.length - 2} more` : ''}
            </div>
          )}
        </div>
        <button
          type="button"
          onClick={() => setExpanded(!expanded)}
          className="text-slate-500 dark:text-slate-400 hover:text-slate-700 dark:hover:text-slate-200 underline flex-shrink-0"
        >
          {expanded ? 'collapse' : 'expand'}
        </button>
      </div>

      {expanded && (
        <div className="mt-2 space-y-1.5">
          {summaries.map(item => {
            const isSteering = steeringIndex === item.index
            return (
              <div key={item.index} className="flex items-start gap-2 rounded border border-slate-200 dark:border-slate-700 bg-white/70 dark:bg-slate-950/30 px-2 py-1">
                <div className={`mt-1.5 w-1.5 h-1.5 rounded-full flex-shrink-0 ${
                  item.status === 'failed'
                    ? 'bg-red-500'
                    : item.status === 'completed'
                      ? 'bg-green-500'
                      : 'bg-slate-400'
                }`}></div>
                <div className="flex-1 min-w-0">
                  <div className="font-medium break-words">#{item.index + 1}: {item.headline}</div>
                  {item.detail && (
                    <div className="mt-0.5 text-[11px] text-slate-600 dark:text-slate-400 break-words">
                      {item.detail.length > 140 ? `${item.detail.slice(0, 140)}...` : item.detail}
                    </div>
                  )}
                </div>
                {onSteer && (
                  <SteerQueueButton
                    onClick={() => onSteer(item.index, item.msg)}
                    isSteering={isSteering}
                    className="self-center flex-shrink-0"
                  />
                )}
                <button
                  type="button"
                  onClick={() => onDelete(item.index)}
                  className="flex items-center justify-center w-5 h-5 self-center rounded hover:bg-slate-200 dark:hover:bg-slate-800 text-slate-600 dark:text-slate-400 hover:text-slate-800 dark:hover:text-slate-200 transition-colors flex-shrink-0"
                  title="Delete from queue"
                >
                  <svg xmlns="http://www.w3.org/2000/svg" className="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                  </svg>
                </button>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}

// Completely isolated input component that doesn't re-render when events change
const ChatInputComponent: React.FC<ChatInputProps> = ({
  onSubmit,
  onStopStreaming,
  activeAgents = []
}) => {
  // Store subscriptions
  const {
    agentMode,
    setWorkspaceMinimized,
    showWorkflowsOverview
  } = useAppStore()
  const selectedModeCategory = useModeStore(state => state.selectedModeCategory)
  const isMultiAgentMode = selectedModeCategory === 'multi-agent'
  // Detect workflow phase chat — hide extras like browser, skills, etc.
  const workflowPhaseId = useChatStore(state => {
    const tabId = state.activeTabId
    const tab = tabId ? state.chatTabs[tabId] : null
    if (tab?.metadata?.mode !== 'workflow' || !tab?.metadata?.phaseId) return undefined
    return isChatCompatiblePhase(tab.metadata.phaseId) ? tab.metadata.phaseId : undefined
  })
  const isWorkflowPhaseChat = !!workflowPhaseId
  const isWorkflowMode = selectedModeCategory === 'workflow'
  const workflowPhasePreset = useGlobalPresetStore(state => state.getActivePreset('workflow'))
  // Read phase LLM from workflow manifest (source of truth), not the global preset.
  // Subscribe to the manifest store so the provider/model badge updates without reopening the chat.
  const workflowPhaseWorkspacePath = isWorkflowPhaseChat ? workflowPhasePreset?.selectedFolder?.filepath : undefined
  const manifestPhaseLLM = useWorkflowManifestStore(state => {
    if (!workflowPhaseWorkspacePath) return null
    const wf = state.workflows.find(item => item.workspace_path === workflowPhaseWorkspacePath)
    return wf?.manifest?.capabilities?.llm_config?.phase_llm ?? null
  })
  // Hide extras (servers, skills, agent mode, etc.) in workflow mode but show in multi-agent
  const hideExtras = isWorkflowMode

  // Use selectors to subscribe only to specific values, reducing re-renders
  const activeTabId = useChatStore(state => state.activeTabId)
  const setTabConfig = useChatStore(state => state.setTabConfig)
  const addToast = useChatStore(state => state.addToast)
  // Get active tab and its config (ChatInput is only rendered in multi-agent mode)
  // Use selector to get only the tab we need, preventing re-renders when other tabs change
  const activeTab = useChatStore(state => 
    activeTabId ? state.chatTabs[activeTabId] : undefined
  )
  const isOrganizationAssistant = !!activeTab?.metadata?.isOrganizationAssistant
  const isOrganizationContext = isOrganizationAssistant || showWorkflowsOverview
  
  // Memoize tabConfig to prevent unnecessary re-renders
  const tabConfig = useMemo(() => activeTab?.config, [activeTab?.config])

  const defaultReasoningLevel = tabConfig?.defaultReasoningLevel ?? null
  const setDefaultReasoningLevel = useCallback((level: 'high' | 'medium' | 'low' | null) => {
    const tabId = useChatStore.getState().activeTabId
    if (tabId) {
      useChatStore.getState().setTabConfig(tabId, { defaultReasoningLevel: level })
    }
  }, [])

  // CRITICAL: Always use tab's status - never fall back to global to prevent mixing
  // If no active tab, this is an error condition (tabs should always exist)
  const isStreaming = activeTab?.isStreaming ?? false
  const canSteer = activeTab?.canSteer ?? false
  const tabSessionId = activeTab?.sessionId ?? null
  const isViewOnly = activeTab?.metadata?.isViewOnly ?? false
  const terminalOutputSessionKey = tabSessionId || activeTabId || '__default__'
  const terminalOutputVisible = useChatStore(state => state.terminalOutputOpen[terminalOutputSessionKey] ?? true)
  const toggleTerminalOutputOpen = useChatStore(state => state.toggleTerminalOutputOpen)
  
  // Note: activeTab may be undefined during initial render before tabs are created
  // This is expected and will resolve once the tab store initializes
  
  // Get all tab events from the store (stable selector)
  const allTabEvents = useChatStore(state => state.tabEvents)

  // Derive tab-specific events with useMemo (avoids selector closure issues)
  const tabEvents = useMemo(() => {
    if (!tabSessionId) return EMPTY_EVENTS
    return allTabEvents[tabSessionId] ?? EMPTY_EVENTS
  }, [tabSessionId, allTabEvents])

  // Helper: check if an event is from a sub-agent (delegation)
  const isSubAgentEvent = useCallback((event: PollingEvent): boolean => {
    const agentEvent = event.data as Record<string, unknown> | undefined
    const innerData = agentEvent?.data as Record<string, unknown> | undefined
    const comp = (event as unknown as Record<string, unknown>).component ?? innerData?.component ?? agentEvent?.component
    const corrId = (event as unknown as Record<string, unknown>).correlation_id ?? innerData?.correlation_id ?? agentEvent?.correlation_id
    return (typeof comp === 'string' && comp.startsWith('delegation-'))
      || (typeof corrId === 'string' && corrId.startsWith('delegation-'))
  }, [])

  const getFirstNumber = useCallback((...values: unknown[]): number | undefined => {
    for (const value of values) {
      if (typeof value === 'number' && Number.isFinite(value)) {
        return value
      }
    }
    return undefined
  }, [])

  const extractContextMetrics = useCallback((payload: Record<string, unknown> | undefined) => {
    const generationInfo = (payload?.generation_info && typeof payload.generation_info === 'object')
      ? payload.generation_info as Record<string, unknown>
      : undefined
    const metadata = (payload?.metadata && typeof payload.metadata === 'object')
      ? payload.metadata as Record<string, unknown>
      : undefined

    const contextWindowUsage = getFirstNumber(
      payload?.context_window_usage,
      generationInfo?.current_context_window_usage,
      metadata?.current_context_window_usage,
      metadata?.context_window_usage
    )
    const modelContextWindow = getFirstNumber(
      payload?.model_context_window,
      generationInfo?.model_context_window,
      metadata?.model_context_window
    )
    const directPercent = getFirstNumber(
      payload?.context_usage_percent,
      generationInfo?.context_usage_percent,
      metadata?.context_usage_percent
    )

    const computedPercent = (
      contextWindowUsage !== undefined &&
      modelContextWindow !== undefined &&
      modelContextWindow > 0
    )
      ? (contextWindowUsage / modelContextWindow) * 100
      : undefined

    const contextUsagePercent = directPercent && directPercent > 0
      ? directPercent
      : computedPercent ?? directPercent

    const hasConcreteContextMetrics = contextWindowUsage !== undefined || modelContextWindow !== undefined
    const normalizedContextUsagePercent = (
      contextUsagePercent === undefined ||
      contextUsagePercent === null ||
      (contextUsagePercent === 0 && !hasConcreteContextMetrics)
    )
      ? undefined
      : contextUsagePercent

    return {
      contextUsagePercent: normalizedContextUsagePercent,
      contextWindowUsage,
      modelContextWindow,
      modelId: typeof payload?.model_id === 'string'
        ? payload.model_id
        : typeof metadata?.model_id === 'string'
          ? metadata.model_id
          : undefined,
    }
  }, [getFirstNumber])

  // Find the latest token usage (optimized with backward iteration)
  // In multi-agent mode, skip sub-agent events — show the PARENT agent's context usage
  //
  // NOTE: Backend serializes AgentEvent.Data (Go interface) flat — event.data.data IS the
  // typed event (e.g. TokenUsageEvent) directly, NOT wrapped in EventDataUnion.
  // The schema-gen uses EventDataUnion for JSON Schema but the wire format is flat.
  // Use getEventData() or event.data?.data directly — NOT event.data?.data?.token_usage.
  const { contextUsagePercent, latestTokenUsage } = useMemo(() => {
    if (tabEvents.length === 0) return { contextUsagePercent: null, latestTokenUsage: null }

    // Iterate backwards (newest first) to find the latest quickly
    let latestTokenUsageEvent = null
    let latestTotalEvent = null

    for (let i = tabEvents.length - 1; i >= 0 && !latestTotalEvent; i--) {
      const event = tabEvents[i]
      if (isEventType(event, 'token_usage')) {
        // Skip sub-agent token_usage events — we want the parent agent's context
        if (isSubAgentEvent(event)) continue
        const data = getEventData(event)
        if (data.context === 'conversation_total') {
          latestTotalEvent = event
          break
        }
        if (!latestTokenUsageEvent) {
          latestTokenUsageEvent = event
        }
      }
    }

    const latestEvent = latestTotalEvent || latestTokenUsageEvent

    if (latestEvent && isEventType(latestEvent, 'token_usage')) {
      const tokenUsage = getEventData(latestEvent) as TokenUsageEvent
      const metrics = extractContextMetrics(tokenUsage as unknown as Record<string, unknown>)
      const contextPercent = metrics.contextUsagePercent

      if (contextPercent !== undefined && contextPercent !== null) {
        return {
          contextUsagePercent: contextPercent,
          latestTokenUsage: {
            ...tokenUsage,
            context_usage_percent: contextPercent,
            context_window_usage: metrics.contextWindowUsage ?? tokenUsage.context_window_usage,
            model_context_window: metrics.modelContextWindow ?? tokenUsage.model_context_window,
            model_id: metrics.modelId ?? tokenUsage.model_id,
          }
        }
      }
    }

    // Fallback: Check llm_generation_end and tool_call_end events for context usage (iterate backwards)
    for (let i = tabEvents.length - 1; i >= 0; i--) {
      const event = tabEvents[i]
      if (isSubAgentEvent(event)) continue

      if (isEventType(event, 'llm_generation_end')) {
        const data = getEventData(event)
        const metrics = extractContextMetrics(data as unknown as Record<string, unknown>)
        if (metrics.contextUsagePercent !== undefined && metrics.contextUsagePercent > 0) {
          return {
            contextUsagePercent: metrics.contextUsagePercent,
            latestTokenUsage: {
              context_usage_percent: metrics.contextUsagePercent,
              model_context_window: metrics.modelContextWindow,
              context_window_usage: metrics.contextWindowUsage,
              model_id: metrics.modelId,
            }
          }
        }
      }

      if (isEventType(event, 'tool_call_end')) {
        const data = getEventData(event)
        const metrics = extractContextMetrics(data as unknown as Record<string, unknown>)
        if (metrics.contextUsagePercent !== undefined && metrics.contextUsagePercent > 0) {
          return {
            contextUsagePercent: metrics.contextUsagePercent,
            latestTokenUsage: {
              context_usage_percent: metrics.contextUsagePercent,
              model_context_window: metrics.modelContextWindow,
              context_window_usage: metrics.contextWindowUsage,
              model_id: metrics.modelId,
            }
          }
        }
      }
    }

    return { contextUsagePercent: null, latestTokenUsage: null }
  }, [tabEvents, isSubAgentEvent, extractContextMetrics])

  // Always use tab-specific config (ChatInput is only in multi-agent mode)
  // Memoize to prevent unnecessary re-renders when other config values change
  const chatFileContext = useMemo(() => tabConfig?.fileContext || [], [tabConfig?.fileContext])
  const chatPastedAttachments = useMemo(() => tabConfig?.pastedAttachments || [], [tabConfig?.pastedAttachments])

  // Get input text from tab config (source of truth for persistence)
  const storedInputText = tabConfig?.inputText || ''

  // Local state for immediate UI updates (prevents Zustand updates on every keystroke)
  const [localInputText, setLocalInputText] = useState(storedInputText)
  const inputText = localInputText

  // Debounce ref for syncing to store
  const syncToStoreTimeoutRef = useRef<NodeJS.Timeout | null>(null)

  // Sync local state FROM store when store changes externally (preset sync, etc.)
  useLayoutEffect(() => {
    // Only sync if store value differs and we're not in the middle of typing
    if (storedInputText !== localInputText && !syncToStoreTimeoutRef.current) {
      setLocalInputText(storedInputText)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [storedInputText]) // Intentionally exclude localInputText to avoid loops

  // Cleanup timeout refs on unmount
  useEffect(() => {
    return () => {
      if (syncToStoreTimeoutRef.current) {
        clearTimeout(syncToStoreTimeoutRef.current)
      }
    }
  }, [])

  // Use ?? instead of || to preserve false values (user's selection)
  // Only default to false if the value is undefined/null (not explicitly set)
  const effectiveProviderForSteer = useMemo(() => {
    if (isWorkflowPhaseChat) {
      return manifestPhaseLLM?.provider
        || workflowPhasePreset?.llmConfig?.phase_llm?.provider
        || workflowPhasePreset?.llmConfig?.provider
        || tabConfig?.llmConfig?.provider
        || null
    }
    return tabConfig?.llmConfig?.provider ?? null
  }, [
    isWorkflowPhaseChat,
    manifestPhaseLLM?.provider,
    tabConfig?.llmConfig?.provider,
    workflowPhasePreset?.llmConfig?.phase_llm?.provider,
    workflowPhasePreset?.llmConfig?.provider,
  ])
  const isCLIProvider = useMemo(
    () => effectiveProviderForSteer === 'claude-code' || effectiveProviderForSteer === 'gemini-cli' || effectiveProviderForSteer === 'codex-cli' || effectiveProviderForSteer === 'cursor-cli' || effectiveProviderForSteer === 'opencode-cli',
    [effectiveProviderForSteer]
  )
  const supportsLiveCodingAgentInput = useMemo(
    () => effectiveProviderForSteer === 'claude-code' || effectiveProviderForSteer === 'gemini-cli' || effectiveProviderForSteer === 'codex-cli' || effectiveProviderForSteer === 'cursor-cli' || effectiveProviderForSteer === 'opencode-cli',
    [effectiveProviderForSteer]
  )
  const canShowSteer = useMemo(() => canSteer && !isCLIProvider, [canSteer, isCLIProvider])
  // CLI providers always require code execution mode
  const useCodeExecutionMode = useMemo(() => isCLIProvider ? true : (tabConfig?.useCodeExecutionMode ?? false), [isCLIProvider, tabConfig?.useCodeExecutionMode])
  const browserMode = useMemo(() => tabConfig?.browserMode ?? 'none', [tabConfig?.browserMode])
  const enableBrowserAccess = useMemo(() => browserMode === 'headless' || browserMode === 'cdp', [browserMode])
  const useCdp = useMemo(() => browserMode === 'cdp', [browserMode])
  const cdpPort = useMemo(() => tabConfig?.cdpPort ?? 9222, [tabConfig?.cdpPort])
  const isLocalMode = useCapabilitiesStore(state => state.capabilities?.local_mode ?? false)
  const workspaceActiveFolder = useWorkspaceStore(state => state.activeFolder)
  const [cdpConnected, setCdpConnected] = useState<boolean | null>(null)
  const [cdpChecking, setCdpChecking] = useState(false)
  const [showCdpPopup, setShowCdpPopup] = useState(false)
  const [showReasoningPopup, setShowReasoningPopup] = useState(false)
  const [showActiveAgentsPanel, setShowActiveAgentsPanel] = useState(false)
  const [isUploadingFiles, setIsUploadingFiles] = useState(false)
  const [isDraggingFiles, setIsDraggingFiles] = useState(false)
  // Auto-close panel when no agents are running
  useEffect(() => {
    if (activeAgents.length === 0) setShowActiveAgentsPanel(false)
  }, [activeAgents.length])
  // Playwright MCP availability: check if 'playwright' server exists in toolList
  const toolList = useMCPStore(state => state.toolList)
  const playwrightServerStatus = useMemo(() => {
    const entry = toolList.find(t => t.server === 'playwright')
    if (!entry) return 'not_found' as const
    if (entry.status === 'ok') return 'ok' as const
    if (entry.status === 'error') return 'error' as const
    return 'loading' as const
  }, [toolList])

  const isCdpDisconnected = browserMode === 'cdp' && cdpConnected === false
  const isPlaywrightMissing = browserMode === 'playwright' && playwrightServerStatus === 'not_found'

  // File context operations (always update tab config)
  const removeFileFromContext = useCallback((path: string) => {
    if (activeTabId && activeTab) {
      const newFileContext = chatFileContext.filter(f => f.path !== path)
      const configUpdate = activeTab.config?.restoredConversationPath === path
        ? { fileContext: newFileContext, restoredConversationPath: undefined, restoredConversationSummary: undefined }
        : { fileContext: newFileContext }
      setTabConfig(activeTabId, configUpdate)
    }
  }, [activeTabId, activeTab, chatFileContext, setTabConfig])
  
  const clearFileContext = useCallback(() => {
    if (activeTabId) {
      setTabConfig(activeTabId, { fileContext: [], restoredConversationPath: undefined, restoredConversationSummary: undefined })
    }
  }, [activeTabId, setTabConfig])
  
  const addPastedAttachment = useCallback((content: string): string | null => {
    if (!activeTabId) return null
    const existingAttachments = useChatStore.getState().getTabConfig(activeTabId)?.pastedAttachments || chatPastedAttachments
    const maxPasteNumber = existingAttachments.reduce((max, item, index) => {
      const match = item.marker?.match(/^\[paste(\d+)\]$/)
      const number = match ? Number(match[1]) : index + 1
      return Number.isFinite(number) ? Math.max(max, number) : max
    }, 0)
    const marker = `[paste${maxPasteNumber + 1}]`
    const lines = content.split('\n').length
    const item = {
      id: `paste_${Date.now()}_${Math.random().toString(36).slice(2, 8)}`,
      marker,
      content,
      chars: content.length,
      lines,
      createdAt: Date.now(),
    }
    setTabConfig(activeTabId, { pastedAttachments: [...existingAttachments, item] })
    return marker
  }, [activeTabId, chatPastedAttachments, setTabConfig])

  const removePastedAttachment = useCallback((id: string) => {
    if (!activeTabId) return
    const attachment = chatPastedAttachments.find(p => p.id === id)
    const marker = attachment?.marker
    const nextInputText = marker ? removePasteMarkersFromText(inputText, [marker]) : inputText
    setLocalInputText(nextInputText)
    prevInputTextRef.current = nextInputText
    if (syncToStoreTimeoutRef.current) {
      clearTimeout(syncToStoreTimeoutRef.current)
      syncToStoreTimeoutRef.current = null
    }
    setTabConfig(activeTabId, {
      inputText: nextInputText,
      pastedAttachments: chatPastedAttachments.filter(p => p.id !== id)
    })
  }, [activeTabId, chatPastedAttachments, inputText, setTabConfig])

  const clearPastedAttachments = useCallback(() => {
    if (!activeTabId) return
    const markers = chatPastedAttachments.map((p, index) => p.marker || `[paste${index + 1}]`)
    const nextInputText = removePasteMarkersFromText(inputText, markers)
    setLocalInputText(nextInputText)
    prevInputTextRef.current = nextInputText
    if (syncToStoreTimeoutRef.current) {
      clearTimeout(syncToStoreTimeoutRef.current)
      syncToStoreTimeoutRef.current = null
    }
    setTabConfig(activeTabId, { inputText: nextInputText, pastedAttachments: [] })
  }, [activeTabId, chatPastedAttachments, inputText, setTabConfig])

  const addFileToContext = useCallback((file: { name: string; path: string; type: 'file' | 'folder' }) => {
    if (activeTabId && activeTab) {
      const newFileContext = [...chatFileContext, file]
      setTabConfig(activeTabId, { fileContext: newFileContext })
    }
  }, [activeTabId, activeTab, chatFileContext, setTabConfig])
  
  const setUseCodeExecutionMode = useCallback((enabled: boolean) => {
    if (activeTabId) {
      setTabConfig(activeTabId, { useCodeExecutionMode: enabled })
    }
  }, [activeTabId, setTabConfig])


  const {
    toolList: mcpToolList,
    setChatSelectedServers
  } = useMCPStore()

  const availableServers = useMemo(
    () => [...new Set(mcpToolList.filter(t => t.status === 'ok').map(t => t.server).filter((s): s is string => typeof s === 'string' && !DEDICATED_MCP_SERVERS.has(s)))],
    [mcpToolList]
  )

  const setBrowserMode = useCallback((mode: 'none' | 'headless' | 'cdp' | 'playwright') => {
    if (!activeTabId) return

    if (mode === 'playwright') {
      // Playwright: no virtual tool, add 'playwright' to selectedServers, enable workspace
      const currentServers = tabConfig?.selectedServers || []
      const newServers = [...currentServers.filter(s => s !== 'playwright'), 'playwright']
      setTabConfig(activeTabId, {
        browserMode: 'playwright',
        enableBrowserAccess: false,
        useCdp: false,

        selectedServers: newServers
      })
      setChatSelectedServers(newServers)
      if (!showCdpPopup) setWorkspaceMinimized(false)
    } else if (mode === 'headless' || mode === 'cdp') {
      // Headless/CDP: use agent_browser virtual tool, remove Playwright from servers
      const currentServers = tabConfig?.selectedServers || []
      const newServers = currentServers.filter(s => s !== 'playwright')
      setTabConfig(activeTabId, {
        browserMode: mode,
        enableBrowserAccess: true,
        useCdp: mode === 'cdp',

        selectedServers: newServers
      })
      setChatSelectedServers(newServers)
      if (!showCdpPopup) setWorkspaceMinimized(false)
    } else {
      // None: disable everything, remove Playwright from servers
      const currentServers = tabConfig?.selectedServers || []
      const newServers = currentServers.filter(s => s !== 'playwright')
      setTabConfig(activeTabId, {
        browserMode: 'none',
        enableBrowserAccess: false,
        useCdp: false,
        selectedServers: newServers
      })
      setChatSelectedServers(newServers)
    }
  }, [activeTabId, tabConfig?.selectedServers, setTabConfig, setChatSelectedServers, setWorkspaceMinimized, showCdpPopup])

  const setCdpPort = useCallback((port: number) => {
    if (activeTabId) {
      setTabConfig(activeTabId, { cdpPort: port })
    }
  }, [activeTabId, setTabConfig])

  const checkCdpConnection = useCallback(async (port: number) => {
    setCdpChecking(true)
    setCdpConnected(null)
    try {
      const result = await agentApi.checkCdpPort(port)
      setCdpConnected(result.connected)
    } catch {
      setCdpConnected(false)
    } finally {
      setCdpChecking(false)
    }
  }, [])

  // Auto-check CDP connection when CDP mode is active or port changes
  useEffect(() => {
    if (browserMode !== 'cdp') {
      setCdpConnected(null)
      return
    }
    const timer = setTimeout(() => {
      checkCdpConnection(cdpPort)
    }, 500)
    return () => clearTimeout(timer)
  }, [browserMode, cdpPort, checkCdpConnection])

  // Get preset info for multi-agent mode
  const { getActivePreset, activePresetIds } = usePresetApplication()

  const activeWorkflowWorkspacePath = useMemo(() => {
    if (selectedModeCategory !== 'workflow') return undefined
    const workflowPreset = getActivePreset('workflow') as { selectedFolder?: PlannerFile } | null
    if (workflowPreset?.selectedFolder?.filepath) return workflowPreset.selectedFolder.filepath
    if (!activePresetIds.workflow) return undefined
    return useWorkflowManifestStore.getState().getWorkflowById(activePresetIds.workflow)?.workspace_path
  }, [activePresetIds.workflow, getActivePreset, selectedModeCategory])
  
  // Get queued messages from tab config
  const queuedMessages = useMemo(() => tabConfig?.queuedMessages || [], [tabConfig?.queuedMessages])

  useEffect(() => {
    if (!tabSessionId) return
    if (!isCLIProvider && !canSteer && queuedMessages.length === 0) return

  }, [
    activeTabId,
    canShowSteer,
    canSteer,
    effectiveProviderForSteer,
    isCLIProvider,
    queuedMessages.length,
    tabConfig?.llmConfig?.provider,
    tabSessionId,
  ])
  
  // State for summarization
  const [isSummarizing, setIsSummarizing] = useState(false)

  // State for steer message loading
  const [steeringIndex, setSteeringIndex] = useState<number | null>(null)

  const removeQueuedMessageAtIndex = useCallback((index: number) => {
    if (!activeTabId) return
    const updated = queuedMessages.filter((_: string, i: number) => i !== index)
    setTabConfig(activeTabId, { queuedMessages: updated })
  }, [activeTabId, queuedMessages, setTabConfig])

  const queueStreamingMessage = useCallback((msg: string) => {
    const trimmed = msg.trim()
    if (!activeTabId || !trimmed) return
    const currentQueued = useChatStore.getState().getTabConfig(activeTabId)?.queuedMessages || []
    setTabConfig(activeTabId, {
      inputText: '',
      queuedMessages: [...currentQueued, trimmed]
    })
  }, [activeTabId, setTabConfig])

  const handleSteerQueuedMessage = useCallback(async (index: number, msg: string) => {
    if (!canShowSteer || !tabSessionId) return

    setSteeringIndex(index)
    try {
      await agentApi.steerMessage(tabSessionId, msg)
      removeQueuedMessageAtIndex(index)
    } catch (err) {
      const status = (err as { response?: { status?: number } })?.response?.status
      if (status === 404) {
        if (activeTabId) {
          useChatStore.getState().setTabCanSteer(activeTabId, false)
        }
        addToast('No live agent is available to steer right now. Wait for the next active turn, or let the queued message send normally.', 'warning')
      } else {
        addToast('Failed to steer message: ' + (err instanceof Error ? err.message : 'Unknown error'), 'error')
      }
    } finally {
      setSteeringIndex(null)
    }
  }, [activeTabId, addToast, canShowSteer, removeQueuedMessageAtIndex, tabSessionId])

  const queuedDisplayItems = useMemo(() => {
    const items: Array<
      | { type: 'message'; index: number; msg: string }
      | { type: 'auto-group'; items: Array<{ index: number; msg: string }> }
    > = []
    let pendingAutoGroup: Array<{ index: number; msg: string }> = []

    queuedMessages.forEach((msg: string, index: number) => {
      if (isAutoNotificationMessage(msg)) {
        pendingAutoGroup.push({ index, msg })
        return
      }

      if (pendingAutoGroup.length > 0) {
        items.push({ type: 'auto-group', items: pendingAutoGroup })
        pendingAutoGroup = []
      }

      items.push({ type: 'message', index, msg })
    })

    if (pendingAutoGroup.length > 0) {
      items.push({ type: 'auto-group', items: pendingAutoGroup })
    }

    return items
  }, [queuedMessages])

  // Use tab-specific servers - memoize to prevent re-renders
  const manualSelectedServers = useMemo(() => tabConfig?.selectedServers || [], [tabConfig?.selectedServers])
  // Server operations (always update tab config AND sync to chat-specific MCP store)
  // This ensures new chat tabs inherit the user's manual server selection
  // Browser servers are mutually exclusive — only one can be active at a time
  const BROWSER_SERVERS = ['playwright'] as const

  const onManualServerToggle = useCallback((server: string) => {
    if (activeTabId) {
      // Remove "NO_SERVERS" if it exists (when selecting a real server)
      const serversWithoutNoServers = manualSelectedServers.filter(s => s !== "NO_SERVERS")

      const isToggling = serversWithoutNoServers.includes(server)
      let newServers: string[]
      if (isToggling) {
        // Toggling off — just remove it
        newServers = serversWithoutNoServers.filter(s => s !== server)
      } else {
        // Toggling on — if it's a browser server, remove the other browser servers
        const isBrowserServer = (BROWSER_SERVERS as readonly string[]).includes(server)
        const base = isBrowserServer
          ? serversWithoutNoServers.filter(s => !(BROWSER_SERVERS as readonly string[]).includes(s))
          : serversWithoutNoServers
        newServers = [...base, server]

        // If enabling a browser server via MCP dropdown, also sync browserMode
        if (server === 'playwright') {
          setTabConfig(activeTabId, {
            selectedServers: newServers,
            browserMode: 'playwright',
            enableBrowserAccess: false,
            useCdp: false,
    
          })
          setChatSelectedServers(newServers)
          setWorkspaceMinimized(false)
          return
        }
      }

      setTabConfig(activeTabId, { selectedServers: newServers })
      // Sync to chat-specific MCP store so new chat tabs inherit this selection
      setChatSelectedServers(newServers)
    }
  }, [activeTabId, manualSelectedServers, setTabConfig, setChatSelectedServers, setWorkspaceMinimized])
  
  const onSelectAllServers = useCallback(() => {
    if (activeTabId) {
      // Select all servers, but only keep one browser server (mutual exclusivity)
      // Keep whichever browser server is already selected; if none, exclude both
      const currentBrowser = manualSelectedServers.find(s => (BROWSER_SERVERS as readonly string[]).includes(s))
      const allServers = availableServers.filter(s => {
        if (!(BROWSER_SERVERS as readonly string[]).includes(s)) return true
        return s === currentBrowser // only keep the currently active browser server
      })
      setTabConfig(activeTabId, { selectedServers: allServers })
      // Sync to chat-specific MCP store so new chat tabs inherit this selection
      setChatSelectedServers(allServers)
    }
  }, [activeTabId, availableServers, manualSelectedServers, setTabConfig, setChatSelectedServers])

  const onClearAllServers = useCallback(() => {
    if (activeTabId) {
      setTabConfig(activeTabId, { selectedServers: ["NO_SERVERS"] })
      // Sync to chat-specific MCP store so new chat tabs inherit this selection
      setChatSelectedServers(["NO_SERVERS"])
    }
  }, [activeTabId, setTabConfig, setChatSelectedServers])


  // Use tab-specific skills - memoize to prevent re-renders
  const selectedSkills = useMemo(() => tabConfig?.selectedSkills || [], [tabConfig?.selectedSkills])

  // Skill operations (update tab config)
  const onSkillToggle = useCallback((skillFolderName: string) => {
    if (activeTabId) {
      const newSkills = selectedSkills.includes(skillFolderName)
        ? selectedSkills.filter(s => s !== skillFolderName)
        : [...selectedSkills, skillFolderName]
      setTabConfig(activeTabId, { selectedSkills: newSkills })
    }
  }, [activeTabId, selectedSkills, setTabConfig])

  const onSelectAllSkills = useCallback((allSkillNames: string[]) => {
    if (activeTabId) {
      setTabConfig(activeTabId, { selectedSkills: allSkillNames })
    }
  }, [activeTabId, setTabConfig])

  const onClearAllSkills = useCallback(() => {
    if (activeTabId) {
      setTabConfig(activeTabId, { selectedSkills: [] })
    }
  }, [activeTabId, setTabConfig])

  // Use tab-specific secrets - memoize to prevent re-renders
  const selectedSecrets = useMemo(() => tabConfig?.selectedSecrets || [], [tabConfig?.selectedSecrets])


  // Secret operations (update tab config)
  const onSecretToggle = useCallback((secretId: string) => {
    if (activeTabId) {
      const newSecrets = selectedSecrets.includes(secretId)
        ? selectedSecrets.filter(s => s !== secretId)
        : [...selectedSecrets, secretId]
      setTabConfig(activeTabId, { selectedSecrets: newSecrets })
    }
  }, [activeTabId, selectedSecrets, setTabConfig])

  const onSelectAllSecrets = useCallback((allSecretIds: string[]) => {
    if (activeTabId) {
      setTabConfig(activeTabId, { selectedSecrets: allSecretIds })
    }
  }, [activeTabId, setTabConfig])

  const onClearAllSecrets = useCallback(() => {
    if (activeTabId) {
      setTabConfig(activeTabId, { selectedSecrets: [] })
    }
  }, [activeTabId, setTabConfig])


  const {
    availableLLMs,
    getCurrentLLMOption,
    refreshAvailableLLMs: onRefreshAvailableLLMs,
    llmConfigLocked,
    workflowPrimaryConfig
  } = useLLMStore()

  const { scrollToFile } = useWorkspaceStore()
  const { showSkillImport, showMCPDetails, showMCPConfig, showModels, openDialog, closeDialog } = useCommandDialogStore()

  // LLM selection (always update tab config)
  const onPrimaryLLMSelect = useCallback((llm: LLMOption) => {
    if (activeTabId) {
      // Get current config to preserve fallback models and cross-provider fallback
      const currentConfig = tabConfig?.llmConfig || {
        provider: 'codex-cli',
        model_id: 'codex-cli',
        fallback_models: [],
        cross_provider_fallback: undefined
      }

      const newConfig = {
        ...currentConfig, // ✅ Preserve all existing configuration
        provider: llm.provider as LLMProvider,
        model_id: llm.model
      }

      // CLI providers always require code execution mode
      if (llm.provider === 'claude-code' || llm.provider === 'gemini-cli' || llm.provider === 'codex-cli' || llm.provider === 'cursor-cli' || llm.provider === 'opencode-cli') {
        setTabConfig(activeTabId, { llmConfig: newConfig, useCodeExecutionMode: true })
      } else {
        setTabConfig(activeTabId, { llmConfig: newConfig })
      }
    }
  }, [activeTabId, tabConfig?.llmConfig, setTabConfig])

  // Computed values - get LLM option from tab config
  const primaryLLM = useMemo(() => {
    if (isWorkflowPhaseChat) {
      // Show the phase_llm from workflow manifest (source of truth for backend)
      const phaseLLM = manifestPhaseLLM
      if (phaseLLM?.provider && phaseLLM?.model_id) {
        const found = availableLLMs.find(llm =>
          llm.provider === phaseLLM.provider && llm.model === phaseLLM.model_id
        )
        if (found) return found
        return {
          provider: phaseLLM.provider,
          model: phaseLLM.model_id,
          label: `${phaseLLM.provider} - ${phaseLLM.model_id}`,
          description: 'Phase LLM'
        }
      }
      // Fallback to preset
      const preset = workflowPhasePreset
      const presetPhaseLLM = preset?.llmConfig?.phase_llm
      if (presetPhaseLLM?.provider && presetPhaseLLM?.model_id) {
        const found = availableLLMs.find(llm =>
          llm.provider === presetPhaseLLM.provider && llm.model === presetPhaseLLM.model_id
        )
        if (found) return found
        return {
          provider: presetPhaseLLM.provider,
          model: presetPhaseLLM.model_id,
          label: `${presetPhaseLLM.provider} - ${presetPhaseLLM.model_id}`,
          description: 'Phase LLM'
        }
      }
      // Fallback to preset primary LLM
      const presetLLM = preset?.llmConfig
      if (presetLLM?.provider && presetLLM?.model_id) {
        const found = availableLLMs.find(llm =>
          llm.provider === presetLLM.provider && llm.model === presetLLM.model_id
        )
        if (found) return found
        return {
          provider: presetLLM.provider,
          model: presetLLM.model_id,
          label: `${presetLLM.provider} - ${presetLLM.model_id}`,
          description: 'Workflow preset LLM'
        }
      }
    }

    if (tabConfig?.llmConfig) {
      const config = tabConfig.llmConfig
      const foundLLM = availableLLMs.find(llm =>
        llm.provider === config.provider && llm.model === config.model_id
      )
      if (foundLLM) return foundLLM

      if (config.provider && config.model_id) {
        return {
          provider: config.provider,
          model: config.model_id,
          label: `${config.provider} - ${config.model_id}`,
          description: 'Selected model'
        }
      }
    }
    return getCurrentLLMOption()
  }, [
    tabConfig?.llmConfig,
    availableLLMs,
    getCurrentLLMOption,
    isWorkflowPhaseChat,
    workflowPrimaryConfig,
    manifestPhaseLLM?.provider,
    manifestPhaseLLM?.model_id,
    workflowPhasePreset?.llmConfig?.phase_llm?.provider,
    workflowPhasePreset?.llmConfig?.phase_llm?.model_id,
    workflowPhasePreset?.llmConfig?.provider,
    workflowPhasePreset?.llmConfig?.model_id,
  ])

  const activeLLMLabel = useMemo(() => {
    if (!primaryLLM?.provider) return 'LLM'
    const model = primaryLLM.model?.split('/').pop()
    return model ? `${primaryLLM.provider}/${model}` : primaryLLM.provider
  }, [primaryLLM?.model, primaryLLM?.provider])

  const handleTerminalOutputButtonClick = useCallback(() => {
    toggleTerminalOutputOpen(terminalOutputSessionKey)
  }, [terminalOutputSessionKey, toggleTerminalOutputOpen])
  
  // Preset folder selection
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const fileUploadInputRef = useRef<HTMLInputElement>(null)
  const dragCounterRef = useRef(0)
  
  // Track previous input value to distinguish user deletion from programmatic clearing
  const prevInputTextRef = useRef<string>('')
  
  // File selection dialog state
  const [showFileDialog, setShowFileDialog] = useState(false)
  const [fileDialogPosition, setFileDialogPosition] = useState({ top: 0, left: 0 })
  const [fileSearchQuery, setFileSearchQuery] = useState('')
  const [atPosition, setAtPosition] = useState(-1) // Position of @ in text
  // Extra files for @ dialog (Chats/ — loaded on demand so workflow-scoped trees still show them)
  const [extraAtFiles, setExtraAtFiles] = useState<PlannerFile[]>([])

  // Command selection dialog state
  const [showCommandDialog, setShowCommandDialog] = useState(false)
  const [commandDialogPosition, setCommandDialogPosition] = useState({ bottom: 0, left: 0 })
  const [commandSearchQuery, setCommandSearchQuery] = useState('')
  const [slashPosition, setSlashPosition] = useState(-1) // Position of / in text
  const [showResumeDialog, setShowResumeDialog] = useState(false)
  const [resumeDialogPosition, setResumeDialogPosition] = useState({ bottom: 0, left: 0 })
  const [resumeSessions, setResumeSessions] = useState<ChatHistorySession[]>([])
  const [resumeSessionsLoading, setResumeSessionsLoading] = useState(false)
  const [resumeCleanupLoading, setResumeCleanupLoading] = useState(false)
  const [resumeFilter, setResumeFilter] = useState<ResumeFilter>('chat')

  // Command editor dialog state
  const [showCommandEditor, setShowCommandEditor] = useState(false)
  const [editingUserCommand, setEditingUserCommand] = useState<{ folder_name: string; frontmatter: { name: string; description: string; icon?: string; modes?: string[] }; content: string } | null>(null)

  // Workflow selection dialog state (# trigger)
  const [showWorkflowDialog, setShowWorkflowDialog] = useState(false)
  const [workflowDialogPosition, setWorkflowDialogPosition] = useState({ bottom: 0, left: 0 })
  const [workflowSearchQuery, setWorkflowSearchQuery] = useState('')
  const [hashPosition, setHashPosition] = useState(-1) // Position of # in text

  // ! skill inline popup state
  const [showSkillPopup, setShowSkillPopup] = useState(false)
  const [skillPopupPosition, setSkillPopupPosition] = useState({ bottom: 0, left: 0 })
  const [skillPopupSearchQuery, setSkillPopupSearchQuery] = useState('')
  const [exclamationPosition, setExclamationPosition] = useState(-1)

  // $ server inline popup state
  const [showServerPopup, setShowServerPopup] = useState(false)
  const [serverPopupPosition, setServerPopupPosition] = useState({ bottom: 0, left: 0 })
  const [serverPopupSearchQuery, setServerPopupSearchQuery] = useState('')
  const [dollarPosition, setDollarPosition] = useState(-1)

  // Lazy-loaded data for inline popups
  const [allSkills, setAllSkills] = useState<Skill[]>([])
  const [skillsLoading, setSkillsLoading] = useState(false)

  const openResumeDialog = useCallback(() => {
    if (selectedModeCategory !== 'workflow' && selectedModeCategory !== 'multi-agent') {
      addToast('/resume is only available in chat or workflow', 'info')
      return
    }

    const rect = textareaRef.current?.getBoundingClientRect()
    setResumeDialogPosition({
      bottom: rect ? window.innerHeight - rect.top + 8 : 96,
      left: rect ? rect.left + window.scrollX : 24,
    })
    setShowCommandDialog(false)
    setSlashPosition(-1)
    setCommandSearchQuery('')
    setResumeFilter('chat')
    setShowResumeDialog(true)
  }, [addToast, selectedModeCategory])

  useEffect(() => {
    if (!showResumeDialog) return
    let cancelled = false
    setResumeSessionsLoading(true)
    agentApi.listChatHistorySessions(100, 0, activeWorkflowWorkspacePath)
      .then(response => {
        if (cancelled) return
        const sessions = [...(response.sessions || [])].sort((a, b) =>
          Date.parse(b.updated_at || b.created_at || '') - Date.parse(a.updated_at || a.created_at || '')
        )
        setResumeSessions(sessions)
      })
      .catch(() => {
        if (!cancelled) {
          setResumeSessions([])
          addToast('Failed to load previous chats', 'error')
        }
      })
      .finally(() => {
        if (!cancelled) setResumeSessionsLoading(false)
      })
    return () => { cancelled = true }
  }, [activeWorkflowWorkspacePath, addToast, showResumeDialog])

  const handleResumeCleanupOldChats = useCallback(async () => {
    const scopeLabel = activeWorkflowWorkspacePath || 'all chats'
    const confirmed = window.confirm(`Delete conversations older than 14 days from ${scopeLabel}? This cannot be undone.`)
    if (!confirmed) return

    setResumeCleanupLoading(true)
    try {
      const response = await agentApi.cleanupChatHistorySessions(14, activeWorkflowWorkspacePath)
      const deletedCount = response.result?.deleted_count ?? 0
      addToast(
        deletedCount === 0
          ? 'No conversations older than 14 days'
          : `Deleted ${deletedCount} old conversation${deletedCount === 1 ? '' : 's'}`,
        'success'
      )
      const refreshed = await agentApi.listChatHistorySessions(100, 0, activeWorkflowWorkspacePath)
      const sessions = [...(refreshed.sessions || [])].sort((a, b) =>
        Date.parse(b.updated_at || b.created_at || '') - Date.parse(a.updated_at || a.created_at || '')
      )
      setResumeSessions(sessions)
    } catch {
      addToast('Failed to delete old conversations', 'error')
    } finally {
      setResumeCleanupLoading(false)
    }
  }, [activeWorkflowWorkspacePath, addToast])

  // Auto-resize textarea based on content
  const adjustTextareaHeight = useCallback(() => {
    if (textareaRef.current) {
      const textarea = textareaRef.current
      // Reset height to auto to get correct scrollHeight
      textarea.style.height = 'auto'
      // Calculate new height (min 40px for 2 lines, max 100px)
      // scrollHeight includes padding, so we get the exact content height
      const newHeight = Math.min(Math.max(textarea.scrollHeight, 40), 100)
      textarea.style.height = `${newHeight}px`
    }
  }, [])

  // Get active preset for multi-agent mode (used for preset query sync and UI)
  const chatActivePreset = getActivePreset('multi-agent')
  
  // Sync tab config inputText with preset query when preset is selected
  useEffect(() => {
    const activePresetId = activePresetIds['multi-agent']

    if (activePresetId && activeTabId) {
      const preset = getActivePreset('multi-agent')

      if (preset && preset.query) {
        // Sync tab config with preset query
        setTabConfig(activeTabId, { inputText: preset.query })
      }
    } else if (!activePresetId && activeTabId) {
      // No preset active, clear input text
      setTabConfig(activeTabId, { inputText: '' })
    }
  }, [activePresetIds, getActivePreset, activeTabId, setTabConfig])

  // Sync ref with inputText when it changes externally (preset sync, programmatic clearing, etc.)
  useEffect(() => {
    prevInputTextRef.current = inputText || ''
  }, [inputText])

  // Handle auto-run from tab config
  useEffect(() => {
    // Check if autoRun is enabled and we have input text and a session
    if (tabConfig?.autoRun && inputText?.trim() && tabSessionId && !isStreaming) {
      // 1. First disable autoRun to prevent loops
      // 2. Clear input text as we're submitting it
      if (activeTabId) {
        setTabConfig(activeTabId, { autoRun: false, inputText: '' })
      }
      
      // 3. Submit the query
      onSubmit(inputText)
    }
  }, [tabConfig?.autoRun, inputText, tabSessionId, isStreaming, activeTabId, setTabConfig, onSubmit])


  // Set initial height and auto-resize textarea when inputText changes
  useEffect(() => {
    if (textareaRef.current) {
      // Set initial height to 2 lines (40px) if empty
      if (!inputText || inputText.trim() === '') {
        textareaRef.current.style.height = '40px'
      } else {
        adjustTextareaHeight()
      }
    }
  }, [inputText, adjustTextareaHeight])
  
  // Set initial height on mount
  useEffect(() => {
    if (textareaRef.current) {
      textareaRef.current.style.height = '40px'
    }
  }, [])


  // Fetch Chats/ on demand when @ dialog opens (these may not be in the
  // workspace tree when it's scoped to a workflow folder).
  // The API returns the CONTENTS of a folder, so we wrap them in synthetic folder entries.
  useEffect(() => {
    if (!showFileDialog) return
    let cancelled = false
    const fetchExtraFolders = async () => {
      try {
        const chats = await agentApi.getPlannerFiles('Chats', -1, 2).catch(() => null)
        if (cancelled) return
        const extra: PlannerFile[] = []
        if (chats?.success && chats.data?.length) {
          extra.push({ filepath: 'Chats', content: '', last_modified: '', type: 'folder', children: chats.data })
        }
        setExtraAtFiles(extra)
      } catch {
        // Silently ignore
      }
    }
    fetchExtraFolders()
    return () => { cancelled = true }
  }, [showFileDialog])

  // Lazy-load skills when ! popup opens (always re-fetch to pick up new skills)
  useEffect(() => {
    // console.log(DBG + ' showSkillPopup changed:', showSkillPopup)
    if (showSkillPopup) {
      setSkillsLoading(true)
      skillsApi.listSkills()
        .then(res => {
          const raw = res.skills || []
          const seen = new Set<string>()
          const unique = raw.filter((s: { file_path?: string; folder_name: string }) => {
            if (seen.has(s.folder_name)) return false
            seen.add(s.folder_name)
            return true
          })
          // console.log(DBG + ' skills loaded:', raw.length, '→ deduplicated:', unique.length)
          setAllSkills(unique)
        })
        .catch((err: unknown) => { console.error(DBG + ' skills load error:', err) })
        .finally(() => setSkillsLoading(false))
    }
  }, [showSkillPopup])

  // Consolidated query selection logic — pasted attachments are prepended as
  // fenced blocks so the LLM sees them as distinct sections, separate from the
  // user's typed message.
  const queryToSubmit = useMemo(() => {
    if (!chatPastedAttachments.length) return inputText
    const blocks = chatPastedAttachments.map((p, i) => {
      const marker = p.marker || `[paste${i + 1}]`
      const header = `${marker} Pasted text (${p.lines} line${p.lines === 1 ? '' : 's'}, ${p.chars} char${p.chars === 1 ? '' : 's'})`
      return `${header}\n\`\`\`\n${p.content}\n\`\`\``
    }).join('\n\n')
    const typed = inputText.trim()
    return typed ? `${blocks}\n\n${inputText}` : blocks
  }, [inputText, chatPastedAttachments])

  const canBootstrapMultiAgentTab = isMultiAgentMode && !showWorkflowsOverview && !isOrganizationAssistant
  // Workflow builder tabs are intentionally created before their backend session.
  // ChatArea assigns the session id on first submit, so the input must not block
  // empty builder tabs just because reports/session rehydration are still loading.
  const canBootstrapWorkflowPhaseTab = isWorkflowMode && isWorkflowPhaseChat && !!activeTab && !isViewOnly
  const hasSubmitTarget = Boolean(tabSessionId || canBootstrapMultiAgentTab || canBootstrapWorkflowPhaseTab)
  const canQueueWhileStreaming = useMemo(() => {
    return Boolean(queryToSubmit?.trim() && isStreaming && tabSessionId)
  }, [queryToSubmit, isStreaming, tabSessionId])

  const canSubmitImmediately = useMemo(() => {
    return Boolean(queryToSubmit?.trim() && !isStreaming && hasSubmitTarget)
  }, [queryToSubmit, isStreaming, hasSubmitTarget])

  const canSubmit = canSubmitImmediately || canQueueWhileStreaming

  // Ref for debounced file removal check
  const fileRemovalTimeoutRef = useRef<NodeJS.Timeout | null>(null)

  // Guard: prevent form submit from firing when Stop button click causes a button swap
  // (React re-renders Stop→Send mid-click, causing the browser to dispatch submit on the new button)
  const justStoppedStreamingRef = useRef(false)

  // Guard: prevent double submission from rapid Enter presses, key repeat, or double-clicks
  // queryToSubmit is a memoized value that doesn't update until re-render, so a second
  // submit within the same render cycle would re-send the same message
  const justSubmittedRef = useRef(false)

  const clearInputState = useCallback(() => {
    setLocalInputText('')
    if (syncToStoreTimeoutRef.current) {
      clearTimeout(syncToStoreTimeoutRef.current)
      syncToStoreTimeoutRef.current = null
    }
    if (activeTabId) {
      setTabConfig(activeTabId, { inputText: '', pastedAttachments: [] })
    }
  }, [activeTabId, setTabConfig])

  const sendLiveCodingAgentMessage = useCallback(async (msg: string): Promise<boolean> => {
    const trimmed = msg.trim()
    if (!trimmed || !isStreaming || !supportsLiveCodingAgentInput || !tabSessionId) return false

    clearInputState()
    try {
      await agentApi.steerMessage(tabSessionId, msg)
    } catch (err) {
      const status = (err as { response?: { status?: number } })?.response?.status
      queueStreamingMessage(trimmed)
      if (status === 404) {
        addToast('No live coding agent is available yet — message queued.', 'warning')
      } else {
        addToast('Failed to send to coding agent — message queued.', 'warning')
      }
    }
    return true
  }, [addToast, clearInputState, isStreaming, queueStreamingMessage, supportsLiveCodingAgentInput, tabSessionId])

  const ensureMultiAgentTabReady = useCallback(async (): Promise<boolean> => {
    if (!isMultiAgentMode || showWorkflowsOverview) return false

    const chatStore = useChatStore.getState()
    const currentActiveTab = chatStore.activeTabId ? chatStore.chatTabs[chatStore.activeTabId] : null
    if (
      currentActiveTab?.metadata?.mode === 'multi-agent' &&
      currentActiveTab.metadata?.isOrganizationAssistant !== true
    ) {
      return true
    }

    const modeTabs = Object.values(chatStore.chatTabs)
      .filter(tab => tab.metadata?.mode === 'multi-agent' && tab.metadata?.isOrganizationAssistant !== true)
      .sort((a, b) => (a.createdAt || 0) - (b.createdAt || 0))

    if (modeTabs.length > 0) {
      chatStore.switchTab(modeTabs[0].tabId)
      return true
    }

    try {
      await chatStore.createChatTab('Agent Chat 1', { mode: 'multi-agent' })
      return true
    } catch (error) {
      console.error('Failed to create fallback multi-agent tab:', error)
      addToast('Unable to initialize a chat tab right now.', 'error')
      return false
    }
  }, [isMultiAgentMode, showWorkflowsOverview, addToast])

  // If the user has already typed surrounding text, keep pasted content out of
  // the textarea and insert a stable marker the message can refer to.
  const handlePaste = useCallback((e: React.ClipboardEvent<HTMLTextAreaElement>) => {
    const pasted = e.clipboardData?.getData('text') ?? ''
    if (!pasted) return

    const textarea = e.currentTarget
    const start = textarea.selectionStart ?? inputText.length
    const end = textarea.selectionEnd ?? inputText.length
    const before = inputText.slice(0, start)
    const after = inputText.slice(end)
    const textWithoutSelection = before + after

    if (!textWithoutSelection.trim()) return

    e.preventDefault()
    const marker = addPastedAttachment(pasted)
    if (!marker) return

    const markerPrefix = before && !/\s$/.test(before) ? ' ' : ''
    const markerSuffix = after && !/^\s/.test(after) ? ' ' : ''
    const markerText = `${markerPrefix}${marker}${markerSuffix}`
    const newValue = `${before}${markerText}${after}`
    const cursorPosition = before.length + markerText.length

    setLocalInputText(newValue)
    prevInputTextRef.current = newValue
    if (syncToStoreTimeoutRef.current) {
      clearTimeout(syncToStoreTimeoutRef.current)
      syncToStoreTimeoutRef.current = null
    }
    if (activeTabId) {
      setTabConfig(activeTabId, { inputText: newValue })
    }

    setTimeout(() => {
      textarea.focus()
      textarea.setSelectionRange(cursorPosition, cursorPosition)
      adjustTextareaHeight()
    }, 0)
  }, [activeTabId, addPastedAttachment, adjustTextareaHeight, inputText, setTabConfig])

  // Memoized handlers to prevent re-creation
  const handleTextChange = useCallback((e: React.ChangeEvent<HTMLTextAreaElement>) => {
    const newValue = e.target.value
    const previousValue = prevInputTextRef.current
    const nextPastedAttachments = chatPastedAttachments.filter(p => !p.marker || newValue.includes(p.marker))
    const pastedAttachmentsChanged = nextPastedAttachments.length !== chatPastedAttachments.length

    // Update local state immediately for fast UI response
    setLocalInputText(newValue)

    if (syncToStoreTimeoutRef.current) {
      clearTimeout(syncToStoreTimeoutRef.current)
      syncToStoreTimeoutRef.current = null
    }

    if (pastedAttachmentsChanged && activeTabId) {
      setTabConfig(activeTabId, { inputText: newValue, pastedAttachments: nextPastedAttachments })
    } else {
      // Debounce sync to Zustand store (300ms delay)
      syncToStoreTimeoutRef.current = setTimeout(() => {
        if (activeTabId) {
          setTabConfig(activeTabId, { inputText: newValue })
        }
        syncToStoreTimeoutRef.current = null
      }, 300)
    }

    // Update ref for next comparison
    prevInputTextRef.current = newValue

    // Auto-resize textarea
    adjustTextareaHeight()

    // Skip most special character triggers for workflow phase chat — but allow @, /, and #.
    if (isWorkflowPhaseChat) {
      // Process @, /, and # triggers in workflow phase chat
      const cursorPos = e.target.selectionStart || 0
      const textBefore = newValue.substring(0, cursorPos)
      const atIdx = textBefore.lastIndexOf('@')
      const slashIdx = textBefore.lastIndexOf('/')
      const hashIdx = textBefore.lastIndexOf('#')
      const slashIsPartOfAtPath = atIdx >= 0 && slashIdx > atIdx

      // Determine closest trigger
      const atDist = atIdx >= 0 ? cursorPos - atIdx : Infinity
      const slashDist = slashIdx >= 0 ? cursorPos - slashIdx : Infinity
      const hashDist = hashIdx >= 0 ? cursorPos - hashIdx : Infinity
      const closestTrigger = Math.min(atDist, slashDist, hashDist)

      if (atIdx >= 0 && closestTrigger === atDist) {
        const textAfterAt = textBefore.substring(atIdx + 1)
        const hasValidAt = textAfterAt === '' || textAfterAt.match(/^[a-zA-Z0-9/._\-\\]*$/)
        if (hasValidAt) {
          setAtPosition(atIdx)
          setFileSearchQuery(textAfterAt)
          setShowFileDialog(true)
          setShowCommandDialog(false)
          setShowWorkflowDialog(false)

          const textarea = e.target
          const rect = textarea.getBoundingClientRect()
          const dialogHeight = 320
          const spaceAbove = rect.top
          setFileDialogPosition({
            top: spaceAbove > dialogHeight ? rect.top - dialogHeight - 8 : rect.bottom + 8,
            left: rect.left + window.scrollX
          })
        } else {
          setShowFileDialog(false)
          setAtPosition(-1)
          setFileSearchQuery('')
        }
      } else if (!slashIsPartOfAtPath && slashIdx >= 0 && closestTrigger === slashDist) {
        const textAfterSlash = textBefore.substring(slashIdx + 1)
        const hasValidSlash = textAfterSlash === '' || textAfterSlash.match(/^[a-zA-Z0-9_:-]*$/)
        if (hasValidSlash) {
          setSlashPosition(slashIdx)
          setCommandSearchQuery(textAfterSlash)
          setShowCommandDialog(true)
          setShowFileDialog(false)
          setShowWorkflowDialog(false)

          const textarea = e.target
          const rect = textarea.getBoundingClientRect()
          setCommandDialogPosition({
            bottom: window.innerHeight - rect.top + 8,
            left: rect.left + window.scrollX
          })
        } else {
          setShowCommandDialog(false)
          setSlashPosition(-1)
          setCommandSearchQuery('')
        }
      } else if (hashIdx >= 0 && closestTrigger === hashDist) {
        const textAfterHash = textBefore.substring(hashIdx + 1)
        const hasValidHash = textAfterHash === '' || textAfterHash.match(/^[a-zA-Z0-9_-]*$/)
        if (hasValidHash) {
          setHashPosition(hashIdx)
          setWorkflowSearchQuery(textAfterHash)
          setShowWorkflowDialog(true)
          setShowFileDialog(false)
          setShowCommandDialog(false)

          const textarea = e.target
          const rect = textarea.getBoundingClientRect()
          setWorkflowDialogPosition({
            bottom: window.innerHeight - rect.top + 8,
            left: rect.left + window.scrollX
          })
        } else {
          setShowWorkflowDialog(false)
          setHashPosition(-1)
          setWorkflowSearchQuery('')
        }
      } else {
        setShowFileDialog(false)
        setAtPosition(-1)
        setFileSearchQuery('')
        setShowCommandDialog(false)
        setSlashPosition(-1)
        setCommandSearchQuery('')
        setShowWorkflowDialog(false)
        setHashPosition(-1)
        setWorkflowSearchQuery('')
      }
      return
    }

    const cursorPosition = e.target.selectionStart || 0
    const textBeforeCursor = newValue.substring(0, cursorPosition)

    const lastSlashIndex = textBeforeCursor.lastIndexOf('/')
    const lastAtIndex = textBeforeCursor.lastIndexOf('@')
    const lastHashIndex = textBeforeCursor.lastIndexOf('#')
    const lastExclamationIndex = textBeforeCursor.lastIndexOf('!')
    const lastDollarIndex = textBeforeCursor.lastIndexOf('$')

    // If @ appears before the current /, the / is part of a path (e.g. "@ workflow /") — stay in file dialog
    const slashIsPartOfAtPath = lastAtIndex >= 0 && lastSlashIndex > lastAtIndex

    const slashDistance = lastSlashIndex >= 0 ? cursorPosition - lastSlashIndex : Infinity
    const atDistance = lastAtIndex >= 0 ? cursorPosition - lastAtIndex : Infinity
    const hashDistance = lastHashIndex >= 0 ? cursorPosition - lastHashIndex : Infinity
    const exclamationDistance = lastExclamationIndex >= 0 ? cursorPosition - lastExclamationIndex : Infinity
    const dollarDistance = lastDollarIndex >= 0 ? cursorPosition - lastDollarIndex : Infinity

    // Check if # is a markdown heading (at line start AND followed by a space) — don't trigger dialog for headings
    // e.g. "# Heading" is a heading, but "#workflow" is a workflow trigger
    const charAfterHash = lastHashIndex >= 0 ? newValue[lastHashIndex + 1] : undefined
    const hashIsAtLineStart = lastHashIndex >= 0 && (lastHashIndex === 0 || textBeforeCursor[lastHashIndex - 1] === '\n')
    const hashIsHeading = hashIsAtLineStart && charAfterHash === ' '

    // Find the closest trigger to cursor
    const closestTrigger = Math.min(slashDistance, atDistance, hashDistance, exclamationDistance, dollarDistance)

    // Check for / command (only when / is not part of an @ path)
    if (!slashIsPartOfAtPath && lastSlashIndex >= 0 && closestTrigger === slashDistance) {
      const textAfterSlash = textBeforeCursor.substring(lastSlashIndex + 1)
      const hasValidSlash = textAfterSlash === '' || textAfterSlash.match(/^[a-zA-Z0-9_:-]*$/)

      if (hasValidSlash) {
        setSlashPosition(lastSlashIndex)
        setCommandSearchQuery(textAfterSlash)
        setShowCommandDialog(true)
        setShowFileDialog(false)
        setShowWorkflowDialog(false)

        // Calculate dialog position — anchor from bottom so it grows upward
        const textarea = e.target
        const rect = textarea.getBoundingClientRect()

        setCommandDialogPosition({
          bottom: window.innerHeight - rect.top + 8,
          left: rect.left + window.scrollX
        })
      } else {
        setShowCommandDialog(false)
        setSlashPosition(-1)
        setCommandSearchQuery('')
      }
    }
    // Check for # workflow trigger (not a markdown heading, in chat/multi-agent mode)
    else if (!hashIsHeading && lastHashIndex >= 0 && closestTrigger === hashDistance) {
      const textAfterHash = textBeforeCursor.substring(lastHashIndex + 1)
      const hasValidHash = textAfterHash === '' || textAfterHash.match(/^[a-zA-Z0-9_-]*$/)

      if (hasValidHash) {
        setHashPosition(lastHashIndex)
        setWorkflowSearchQuery(textAfterHash)
        setShowWorkflowDialog(true)
        setShowCommandDialog(false)
        setShowFileDialog(false)

        // Calculate dialog position — anchor from bottom so it grows upward
        const textarea = e.target
        const rect = textarea.getBoundingClientRect()

        setWorkflowDialogPosition({
          bottom: window.innerHeight - rect.top + 8,
          left: rect.left + window.scrollX
        })
      } else {
        setShowWorkflowDialog(false)
        setHashPosition(-1)
        setWorkflowSearchQuery('')
      }
    }
    // Check for ! skill trigger
    else if (lastExclamationIndex >= 0 && closestTrigger === exclamationDistance) {
      const textAfterExcl = textBeforeCursor.substring(lastExclamationIndex + 1)
      const hasValidExcl = textAfterExcl === '' || textAfterExcl.match(/^[a-zA-Z0-9_-]*$/)
      // console.log(DBG + ' ! trigger — textAfterExcl:', JSON.stringify(textAfterExcl), 'hasValidExcl:', hasValidExcl)

      if (hasValidExcl) {
        setExclamationPosition(lastExclamationIndex)
        setSkillPopupSearchQuery(textAfterExcl)
        setShowSkillPopup(true)
        // console.log(DBG + ' ! trigger — setSkillPopupSearchQuery:', JSON.stringify(textAfterExcl))
        setShowCommandDialog(false)
        setShowFileDialog(false)
        setShowWorkflowDialog(false)
        setShowServerPopup(false)

        const textarea = e.target
        const rect = textarea.getBoundingClientRect()
        setSkillPopupPosition({
          bottom: window.innerHeight - rect.top + 8,
          left: rect.left + window.scrollX
        })
      } else {
        setShowSkillPopup(false)
        setExclamationPosition(-1)
        setSkillPopupSearchQuery('')
      }
    }
    // Check for $ server trigger
    else if (lastDollarIndex >= 0 && closestTrigger === dollarDistance) {
      const textAfterDollar = textBeforeCursor.substring(lastDollarIndex + 1)
      const hasValidDollar = textAfterDollar === '' || textAfterDollar.match(/^[a-zA-Z0-9_-]*$/)

      if (hasValidDollar) {
        setDollarPosition(lastDollarIndex)
        setServerPopupSearchQuery(textAfterDollar)
        setShowServerPopup(true)
        setShowCommandDialog(false)
        setShowFileDialog(false)
        setShowWorkflowDialog(false)
        setShowSkillPopup(false)

        const textarea = e.target
        const rect = textarea.getBoundingClientRect()
        setServerPopupPosition({
          bottom: window.innerHeight - rect.top + 8,
          left: rect.left + window.scrollX
        })
      } else {
        setShowServerPopup(false)
        setDollarPosition(-1)
        setServerPopupSearchQuery('')
      }
    }
    // Check for @ symbol and update file dialog state (only if no other dialog active and workspace access is enabled)
    else if (lastAtIndex >= 0 && !showCommandDialog && !showWorkflowDialog) {
      const textAfterAt = textBeforeCursor.substring(lastAtIndex + 1)
      const hasValidAt = textAfterAt === '' || textAfterAt.match(/^[a-zA-Z0-9/._\-\\]*$/)

      if (hasValidAt) {
        setAtPosition(lastAtIndex)
        setFileSearchQuery(textAfterAt)
        setShowFileDialog(true)

        // Calculate dialog position - smart positioning to avoid overlap
        const textarea = e.target
        const rect = textarea.getBoundingClientRect()
        const dialogHeight = 320 // Approximate dialog height
        const spaceAbove = rect.top
        const spaceBelow = window.innerHeight - rect.bottom

        // Position above if there's more space above, otherwise position below
        const shouldPositionAbove = spaceAbove > dialogHeight || spaceAbove > spaceBelow

        setFileDialogPosition({
          top: shouldPositionAbove
            ? rect.top + window.scrollY - dialogHeight - 10 // Above with gap
            : rect.bottom + window.scrollY + 10, // Below with gap
          left: rect.left + window.scrollX
        })
      } else {
        setShowFileDialog(false)
        setAtPosition(-1)
        setFileSearchQuery('')
      }
    } else {
      // Close all dialogs if none is active
      // console.log(DBG + ' no trigger matched — closing all popups. textBeforeCursor:', JSON.stringify(textBeforeCursor), 'closestTrigger:', closestTrigger)
      setShowFileDialog(false)
      setAtPosition(-1)
      setFileSearchQuery('')
      setShowCommandDialog(false)
      setSlashPosition(-1)
      setCommandSearchQuery('')
      setShowWorkflowDialog(false)
      setHashPosition(-1)
      setWorkflowSearchQuery('')
      setShowSkillPopup(false)
      setExclamationPosition(-1)
      setSkillPopupSearchQuery('')
      setShowServerPopup(false)
      setDollarPosition(-1)
      setServerPopupSearchQuery('')
    }

    // Debounce file reference removal check (500ms delay)
    // This prevents expensive iteration on every keystroke
    if (fileRemovalTimeoutRef.current) {
      clearTimeout(fileRemovalTimeoutRef.current)
    }
    fileRemovalTimeoutRef.current = setTimeout(() => {
      // Check if any @file references were removed and remove them from context
      // Only remove if:
      // 1. The file reference existed in the previous input
      // 2. The file reference is missing in the new input
      // 3. The new input is shorter than the previous (user deleted it, not cleared programmatically)
      if (previousValue.length > newValue.length) {
        const removedFiles: string[] = []
        chatFileContext.forEach((file: { path: string }) => {
          const fileReference = '@' + file.path
          const wasInPrevious = previousValue.includes(fileReference)
          const isInNew = newValue.includes(fileReference)

          if (wasInPrevious && !isInNew) {
            removedFiles.push(file.path)
          }
        })
        removedFiles.forEach(filePath => {
          removeFileFromContext(filePath)
        })

        // Check if any #workflow references were removed
        if (activeTabId) {
          const currentWorkflowContext = useChatStore.getState().getTabConfig(activeTabId)?.workflowContext || []
          const removedWorkflows = currentWorkflowContext.filter(w => {
            const wRef = '#' + w.label
            return previousValue.includes(wRef) && !newValue.includes(wRef)
          })
          if (removedWorkflows.length > 0) {
            const remaining = currentWorkflowContext.filter(w => !removedWorkflows.some(r => r.presetId === w.presetId))
            setTabConfig(activeTabId, { workflowContext: remaining })
          }
        }
      }
      fileRemovalTimeoutRef.current = null
    }, 500)
  }, [chatFileContext, chatPastedAttachments, removeFileFromContext, showCommandDialog, showWorkflowDialog, activeTabId, setTabConfig, adjustTextareaHeight, isWorkflowPhaseChat])

  // Handle manual summarization
  // If messageToSendAfter is provided, it will be sent as a user message after summarization completes
  const handleSummarize = useCallback(async (messageToSendAfter?: string) => {
    if (!tabSessionId || isSummarizing || isStreaming) {
      return
    }

    setIsSummarizing(true)
    try {
      const response = await agentApi.summarizeConversation(tabSessionId)
      addToast(`Summarized: ${response.original_count} → ${response.new_count} messages (−${response.reduced_by})`, 'success')
      
      // If there's a message to send after summarization, send it now
      if (messageToSendAfter && messageToSendAfter.trim() && tabSessionId) {
        // Small delay to ensure summarization is fully processed
        setTimeout(() => {
          onSubmit(messageToSendAfter.trim())
        }, 500)
      }
    } catch (error) {
      console.error('[SUMMARIZATION] Error:', error)
      const errorMessage = error instanceof Error ? error.message : 'Unknown error'
      addToast(`Failed to summarize: ${errorMessage}`, 'error')
    } finally {
      setIsSummarizing(false)
    }
  }, [tabSessionId, isSummarizing, isStreaming, onSubmit, addToast])

  // Handle manual context compaction (context editing)
  // If messageToSendAfter is provided, it will be sent as a user message after compaction completes
  const handleCompact = useCallback(async (messageToSendAfter?: string) => {
    if (!tabSessionId || isSummarizing || isStreaming) {
      return
    }

    setIsSummarizing(true) // Reuse the same loading state
    try {
      const response = await agentApi.compactContext(tabSessionId)
      addToast(`Compacted ${response.compacted_count} responses, saved ${response.total_tokens_saved?.toLocaleString() || 0} tokens`, 'success')
      
      // If there's a message to send after compaction, send it now
      if (messageToSendAfter && messageToSendAfter.trim() && tabSessionId) {
        // Small delay to ensure compaction is fully processed
        setTimeout(() => {
          onSubmit(messageToSendAfter.trim())
        }, 500)
      }
    } catch (error) {
      console.error('[CONTEXT_EDITING] Error:', error)
      const errorMessage = error instanceof Error ? error.message : 'Unknown error'
      addToast(`Failed to compact: ${errorMessage}`, 'error')
    } finally {
      setIsSummarizing(false)
    }
  }, [tabSessionId, isSummarizing, isStreaming, onSubmit, addToast])

  const getEffectiveWorkflowModes = useCallback(() => {
    const workflowState = useWorkflowStore.getState()
    const presetId = useGlobalPresetStore.getState().activePresetIds.workflow
    const effectiveWorkshopMode = (presetId && workflowState.workshopModeByPreset[presetId]) || workflowState.workshopMode

    return {
      workflowMode: workflowState.workflowMode,
      workshopMode: effectiveWorkshopMode,
    }
  }, [])

  const applyWorkflowCommandRequirements = useCallback((cmd: CommandDefinition) => {
    if (selectedModeCategory !== 'workflow') return
    if (!cmd.requiredWorkflowMode && !cmd.requiredWorkshopMode) return

    const workflowStore = useWorkflowStore.getState()
    const { workflowMode: currentWorkflowMode, workshopMode: currentWorkshopMode } = getEffectiveWorkflowModes()

    const requiredWorkshopModes = cmd.requiredWorkshopMode
      ? (Array.isArray(cmd.requiredWorkshopMode) ? cmd.requiredWorkshopMode : [cmd.requiredWorkshopMode])
      : []
    // If current workshop mode is already one of the allowed modes, no switch needed
    const workshopModeMatches = requiredWorkshopModes.length === 0 || requiredWorkshopModes.includes(currentWorkshopMode as any)
    // When we need to switch, pick the first allowed mode
    const targetWorkshopMode = workshopModeMatches ? undefined : requiredWorkshopModes[0]
    // After the 6→4 mode consolidation, all workshop modes live under workflowMode='plan'.
    // The legacy 'eval' / 'output' workflow-mode values are gone; eval-plan and report-widget
    // editing both happen in Builder mode.
    const targetWorkflowMode = cmd.requiredWorkflowMode
      ?? (targetWorkshopMode || (requiredWorkshopModes.length > 0 && !workshopModeMatches) ? 'plan' : undefined)

    let switched = false

    if (targetWorkshopMode && currentWorkshopMode !== targetWorkshopMode) {
      workflowStore.setWorkshopMode(targetWorkshopMode)
      switched = true
    } else if (targetWorkflowMode && currentWorkflowMode !== targetWorkflowMode) {
      workflowStore.setWorkflowMode(targetWorkflowMode)
      switched = true
    }

    if (switched) {
      const modeLabel = targetWorkshopMode
        ? targetWorkshopMode.charAt(0).toUpperCase() + targetWorkshopMode.slice(1)
        : targetWorkflowMode
          ? targetWorkflowMode.charAt(0).toUpperCase() + targetWorkflowMode.slice(1)
          : 'workflow'
      addToast(`Switched to ${modeLabel} mode for /${cmd.command}`, 'info')
    }
  }, [addToast, getEffectiveWorkflowModes, selectedModeCategory])

  const buildCommandContext = useCallback((beforeSlash: string): CommandContext | null => {
    if (!activeTabId) return null
    const effectiveModes = getEffectiveWorkflowModes()

    const setInputText = (text: string) => {
      setLocalInputText(text)
      setTabConfig(activeTabId, { inputText: text })
      setTimeout(() => {
        if (textareaRef.current) {
          textareaRef.current.focus()
          textareaRef.current.setSelectionRange(text.length, text.length)
        }
      }, 0)
    }

    // Queue-aware onSubmit. When the builder is currently streaming a previous
    // message, slash-command prompts must respect the same queue that regular
    // text submissions use (lines ~2198 and ~2254). Without this wrap, slash
    // commands raced the in-flight conversation and submitted immediately —
    // inconsistent with how Enter-on-text behaves.
    const queueAwareOnSubmit = (query: string) => {
      const trimmed = query?.trim()
      if (!trimmed) return
      if (isStreaming) {
        const currentQueued = tabConfig?.queuedMessages || []
        setTabConfig(activeTabId, {
          inputText: '',
          queuedMessages: [...currentQueued, trimmed]
        })
        addToast('Builder is busy — slash command queued', 'info')
        return
      }
      onSubmit(trimmed)
    }

    return {
      beforeSlash,
      activeTabId,
      tabSessionId,
      tabConfig,
      isSummarizing,
      isStreaming,
      onSubmit: queueAwareOnSubmit,
      setInputText,
      openDialog,
      openResumeDialog,
      setTabConfig,
      addToast,
      handleSummarize,
      handleCompact,
      getAppStore: () => useAppStore.getState(),
      getWorkspaceStore: () => useWorkspaceStore.getState(),
      getWorkflowStore: () => useWorkflowStore.getState(),
      workflowMode: effectiveModes.workflowMode,
      workshopMode: effectiveModes.workshopMode,
      workflowPhaseId
    }
  }, [activeTabId, tabSessionId, tabConfig, isSummarizing, isStreaming, onSubmit, openDialog, openResumeDialog, setTabConfig, addToast, handleSummarize, handleCompact, getEffectiveWorkflowModes, workflowPhaseId])

  const getCommandValidationError = useCallback((cmd: CommandDefinition, beforeSlash: string) => {
    if (!cmd.validate) return null

    const ctx = buildCommandContext(beforeSlash)
    if (!ctx) return 'Unable to run this command right now'

    return cmd.validate(ctx)
  }, [buildCommandContext])

  const executeSlashCommandFromQuery = useCallback((trimmedQuery: string) => {
    if (!trimmedQuery.startsWith('/')) return false

    const withoutSlash = trimmedQuery.slice(1).trim()
    if (!withoutSlash) return false

    const firstSpace = withoutSlash.indexOf(' ')
    const commandName = (firstSpace >= 0 ? withoutSlash.slice(0, firstSpace) : withoutSlash).trim()
    const commandArgs = (firstSpace >= 0 ? withoutSlash.slice(firstSpace + 1) : '').trim()
    if (!commandName) return false

    const cmd = findCommand(commandName, selectedModeCategory)
    if (!cmd) {
      const modeScopedCommand = findCommandAnyMode(commandName)
      if (modeScopedCommand && selectedModeCategory) {
        const availableInWorkflow = modeScopedCommand.modes?.includes('workflow') ?? false
        const targetLabel = availableInWorkflow ? 'workflow' : 'multi-agent'
        addToast(`/${commandName} is only available in ${targetLabel} chat`, 'info')
        return true
      }
      return false
    }

    const validationError = getCommandValidationError(cmd, commandArgs)
    if (validationError) {
      addToast(validationError, 'info')
      return true
    }

    applyWorkflowCommandRequirements(cmd)

    const ctx = buildCommandContext(commandArgs)
    if (!ctx) return false

    clearInputState()
    cmd.execute(ctx)
    return true
  }, [addToast, applyWorkflowCommandRequirements, buildCommandContext, clearInputState, getCommandValidationError, selectedModeCategory])

  const getSubmitBlockReason = useCallback((): string | null => {
    if (!queryToSubmit?.trim()) return null
    if (isViewOnly) return 'This conversation is view only.'
    if (isCdpDisconnected) return 'CDP browser mode is selected, but the browser is not connected yet.'
    if (isPlaywrightMissing) return 'Playwright browser support is not available right now.'
    if (isStreaming && !tabSessionId) return 'This chat is still initializing. Please wait a moment.'
    if (!isStreaming && !hasSubmitTarget) return 'This chat is still initializing. Please wait a moment.'
    return null
  }, [queryToSubmit, isViewOnly, isCdpDisconnected, isPlaywrightMissing, isStreaming, tabSessionId, hasSubmitTarget])

  const handleKeyDown = useCallback((e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    // If any selection dialog is open, let it handle keyboard events
    if (showCommandDialog || showFileDialog || showWorkflowDialog || showResumeDialog || showSkillPopup || showServerPopup) {
      // Prevent default for arrow keys, enter, escape so textarea doesn't move cursor
      if (['ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight', 'Enter', 'Escape'].includes(e.key)) {
        e.preventDefault()
        return
      }
    }

    // Handle Escape key to stop streaming (when no dialogs are open)
    if (e.key === 'Escape' && isStreaming) {
      e.preventDefault()
      onStopStreaming()
      return
    }

    // Handle normal Enter to submit
    if (e.key === 'Enter' && !e.ctrlKey && !e.metaKey) {
      e.preventDefault()

      // Check for slash commands
      const trimmedQuery = queryToSubmit?.trim() || ''
      if (executeSlashCommandFromQuery(trimmedQuery)) {
        return
      }

      if (canSubmitImmediately) {
        // Guard: prevent double submission from rapid key repeat or double press
        if (justSubmittedRef.current) return
        justSubmittedRef.current = true
        setTimeout(() => { justSubmittedRef.current = false }, 300)

        clearInputState()
        onSubmit(queryToSubmit)
      } else if (canSubmit && isStreaming) {
        if (supportsLiveCodingAgentInput && tabSessionId) {
          void sendLiveCodingAgentMessage(queryToSubmit)
        } else {
          clearInputState()
          queueStreamingMessage(queryToSubmit)
        }
      } else if (trimmedQuery) {
        const submitBlockReason = getSubmitBlockReason()
        if (submitBlockReason) {
          addToast(submitBlockReason, 'info')
        }
      }
    }
    // Handle CTRL+Enter (Windows/Linux) or CMD+Enter (Mac) to add new line
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
      e.preventDefault()
      const textarea = e.target as HTMLTextAreaElement
      const start = textarea.selectionStart
      const end = textarea.selectionEnd
      const newValue = inputText.substring(0, start) + '\n' + inputText.substring(end)
      // Update local state immediately for fast UI
      setLocalInputText(newValue)

      // Set cursor position after the newline
      setTimeout(() => {
        textarea.selectionStart = textarea.selectionEnd = start + 1
      }, 0)
    }
  }, [inputText, showFileDialog, showCommandDialog, showWorkflowDialog, showResumeDialog, showSkillPopup, showServerPopup, isStreaming, onStopStreaming, queryToSubmit, executeSlashCommandFromQuery, tabSessionId, isSummarizing, handleSummarize, clearInputState, handleCompact, canSubmitImmediately, onSubmit, canSubmit, supportsLiveCodingAgentInput, sendLiveCodingAgentMessage, queueStreamingMessage, getSubmitBlockReason, addToast])

  const handleSubmit = useCallback((e: React.FormEvent) => {
    e.preventDefault()

    // Guard: ignore form submit triggered by Stop→Send button swap during a click
    if (justStoppedStreamingRef.current) {
      return
    }

    // Check for slash commands
    const trimmedQuery = queryToSubmit?.trim() || ''
    if (executeSlashCommandFromQuery(trimmedQuery)) {
      return
    }

    if (canSubmitImmediately) {
      // Guard: prevent double submission from rapid clicks
      if (justSubmittedRef.current) return
      justSubmittedRef.current = true
      setTimeout(() => { justSubmittedRef.current = false }, 300)

      clearInputState()
      onSubmit(queryToSubmit)
    } else if (canSubmit && isStreaming) {
      if (supportsLiveCodingAgentInput && tabSessionId) {
        void sendLiveCodingAgentMessage(queryToSubmit)
      } else {
        clearInputState()
        queueStreamingMessage(queryToSubmit)
      }
    } else if (trimmedQuery) {
      const submitBlockReason = getSubmitBlockReason()
      if (submitBlockReason) {
        addToast(submitBlockReason, 'info')
      }
    }
  }, [queryToSubmit, executeSlashCommandFromQuery, tabSessionId, isSummarizing, isStreaming, handleSummarize, handleCompact, clearInputState, canSubmitImmediately, onSubmit, canSubmit, supportsLiveCodingAgentInput, sendLiveCodingAgentMessage, queueStreamingMessage, getSubmitBlockReason, addToast])

  // Command selection handler - executes commands directly
  const handleCommandSelect = useCallback((command: string) => {
    if (!activeTabId) return

    // Close dialog first
    setShowCommandDialog(false)
    setSlashPosition(-1)
    setCommandSearchQuery('')

    // Get text before the slash command (if any)
    const beforeSlash = slashPosition >= 0 ? inputText.substring(0, slashPosition).trim() : ''

    // Clear input
    clearInputState()

    // Look up and execute the command from the registry
    const cmd = findCommand(command, selectedModeCategory)
    const validationError = cmd ? getCommandValidationError(cmd, beforeSlash) : null
    if (cmd && validationError) {
      addToast(validationError, 'info')

      const currentStepId = useWorkflowStore.getState().currentStepId
      const commandText = command === 'optimize-step'
        ? `/${command} ${currentStepId || '<step-id>'}`
        : `/${command} `
      setLocalInputText(commandText)
      setTabConfig(activeTabId, { inputText: commandText })

      setTimeout(() => {
        if (textareaRef.current) {
          textareaRef.current.focus()
          if (command === 'optimize-step' && !currentStepId) {
            const placeholderStart = commandText.indexOf('<step-id>')
            const placeholderEnd = placeholderStart + '<step-id>'.length
            textareaRef.current.setSelectionRange(placeholderStart, placeholderEnd)
          } else {
            textareaRef.current.setSelectionRange(commandText.length, commandText.length)
          }
        }
      }, 0)
      return
    }

    if (cmd) {
      applyWorkflowCommandRequirements(cmd)
    }

    const ctx = buildCommandContext(beforeSlash)
    if (cmd && ctx) {
      cmd.execute(ctx)
    } else {
      // For unknown commands, insert into text (fallback)
      if (textareaRef.current) {
        const afterSearch = inputText.substring((slashPosition >= 0 ? slashPosition : 0) + 1 + commandSearchQuery.length)
        const newQuery = beforeSlash + '/' + command + ' ' + afterSearch
        setLocalInputText(newQuery)
        setTimeout(() => {
          if (textareaRef.current) {
            textareaRef.current.focus()
            const cursorPosition = beforeSlash.length + '/'.length + command.length + ' '.length
            textareaRef.current.setSelectionRange(cursorPosition, cursorPosition)
          }
        }, 0)
      }
    }

    // Focus back to textarea
    setTimeout(() => textareaRef.current?.focus(), 0)
  }, [inputText, slashPosition, commandSearchQuery, activeTabId, addToast, clearInputState, setTabConfig, applyWorkflowCommandRequirements, buildCommandContext, getCommandValidationError, selectedModeCategory])

  // Command management callbacks
  const handleManageCommands = useCallback(() => {
    setShowCommandDialog(false)
    setEditingUserCommand(null)
    setShowCommandEditor(true)
  }, [])

  const handleEditCommand = useCallback((cmd: CommandDefinition) => {
    setShowCommandDialog(false)
    // Fetch full command data from API to populate editor
    commandsApi.getCommand(cmd.command).then(uc => {
      setEditingUserCommand({
        folder_name: uc.folder_name,
        frontmatter: uc.frontmatter,
        content: uc.content
      })
      setShowCommandEditor(true)
    }).catch(() => {
      addToast('Failed to load command for editing', 'error')
    })
  }, [addToast])

  const handleDeleteCommand = useCallback(async (cmd: CommandDefinition) => {
    try {
      await commandsApi.deleteCommand(cmd.command)
      await loadAndRegisterUserCommands()
      addToast(`Command /${cmd.command} deleted`, 'success')
    } catch {
      addToast('Failed to delete command', 'error')
    }
  }, [addToast])

  const handleCommandEditorClose = useCallback(() => {
    setShowCommandEditor(false)
    setEditingUserCommand(null)
  }, [])

  const handleFileSelect = useCallback((file: PlannerFile) => {
    if (!textareaRef.current || atPosition === -1 || !activeTabId) return

    const beforeAt = inputText.substring(0, atPosition)
    const afterSearch = inputText.substring(atPosition + 1 + fileSearchQuery.length)
    const newQuery = beforeAt + '@' + file.filepath + ' ' + afterSearch

    // Update local state immediately for fast UI
    setLocalInputText(newQuery)
    setShowFileDialog(false)
    setAtPosition(-1)
    setFileSearchQuery('')

    // Add file/folder to context
    const fileContextItem = {
      name: file.filepath.split('/').pop() || file.filepath,
      path: file.filepath,
      type: file.type || 'file' as const
    }

    const isAlreadyInContext = chatFileContext.some((item: { path: string }) => item.path === file.filepath)
    if (!isAlreadyInContext) {
      addFileToContext(fileContextItem)
      scrollToFile(file.filepath)
    }

    // Focus back to textarea and position cursor after the space
    setTimeout(() => {
      if (textareaRef.current) {
        textareaRef.current.focus()
        const cursorPosition = beforeAt.length + '@'.length + file.filepath.length + ' '.length
        textareaRef.current.setSelectionRange(cursorPosition, cursorPosition)
      }
    }, 0)
  }, [inputText, atPosition, fileSearchQuery, chatFileContext, addFileToContext, scrollToFile, activeTabId, setTabConfig])

  const handleCommandDialogClose = useCallback(() => {
    setShowCommandDialog(false)
    setSlashPosition(-1)
    setCommandSearchQuery('')
    textareaRef.current?.focus()
  }, [])

  const handleFileDialogClose = useCallback(() => {
    setShowFileDialog(false)
    setAtPosition(-1)
    setFileSearchQuery('')
    textareaRef.current?.focus()
  }, [])

  const handleResumeDialogClose = useCallback(() => {
    setShowResumeDialog(false)
    textareaRef.current?.focus()
  }, [])

  const handleResumeChatSelect = useCallback((sessionId: string) => {
    if (!activeTabId) return
    const session = resumeSessions.find(item => item.session_id === sessionId)
    if (!session) return

    const path = resumeChatConversationPath(session)
    const existingContext = useChatStore.getState().getTabConfig(activeTabId)?.fileContext || chatFileContext
    const nextFileContext = existingContext.some((item: { path: string }) => item.path === path)
      ? existingContext
      : [
          ...existingContext,
          {
            name: resumeChatTitle(session),
            path,
            type: 'file' as const,
          },
        ]

    setTabConfig(activeTabId, {
      fileContext: nextFileContext,
      restoredConversationPath: path,
      restoredConversationSummary: undefined,
    })
    setShowResumeDialog(false)
    addToast('Previous chat added to context', 'success')
    setTimeout(() => textareaRef.current?.focus(), 0)
  }, [activeTabId, addToast, chatFileContext, resumeSessions, setTabConfig])

  const handleWorkflowSelect = useCallback((workflow: { presetId: string; label: string; workspacePath: string }) => {
    if (!textareaRef.current || hashPosition === -1 || !activeTabId) return

    const beforeHash = inputText.substring(0, hashPosition)
    const afterSearch = inputText.substring(hashPosition + 1 + workflowSearchQuery.length)
    const newQuery = beforeHash + '#' + workflow.label + ' ' + afterSearch

    // Update local state immediately
    setLocalInputText(newQuery)
    setShowWorkflowDialog(false)
    setHashPosition(-1)
    setWorkflowSearchQuery('')

    // Add workflow to context (avoid duplicates)
    const currentWorkflowContext = useChatStore.getState().getTabConfig(activeTabId)?.workflowContext || []
    const isAlreadyInContext = currentWorkflowContext.some(w => w.presetId === workflow.presetId)
    if (!isAlreadyInContext) {
      const updated = [...currentWorkflowContext, {
        presetId: workflow.presetId,
        label: workflow.label,
        workspacePath: workflow.workspacePath
      }]
      setTabConfig(activeTabId, { workflowContext: updated })
    }

    // Sync store
    if (syncToStoreTimeoutRef.current) {
      clearTimeout(syncToStoreTimeoutRef.current)
      syncToStoreTimeoutRef.current = null
    }
    setTabConfig(activeTabId, { inputText: newQuery })

    // Focus back to textarea and position cursor
    setTimeout(() => {
      if (textareaRef.current) {
        textareaRef.current.focus()
        const cursorPosition = beforeHash.length + '#'.length + workflow.label.length + ' '.length
        textareaRef.current.setSelectionRange(cursorPosition, cursorPosition)
      }
    }, 0)
  }, [inputText, hashPosition, workflowSearchQuery, activeTabId, setTabConfig])

  const handleWorkflowDialogClose = useCallback(() => {
    setShowWorkflowDialog(false)
    setHashPosition(-1)
    setWorkflowSearchQuery('')
    textareaRef.current?.focus()
  }, [])

  const uploadTargetFolder = useMemo(() => {
    if (selectedModeCategory === 'workflow') {
      return workspaceActiveFolder || 'Workflow'
    }
    return 'Chats'
  }, [selectedModeCategory, workspaceActiveFolder])

  const uploadFilesToChat = useCallback(async (files: File[]) => {
    if (files.length === 0 || isUploadingFiles) {
      console.info('[CHAT_UPLOAD] no files selected or upload already in progress', { fileCount: files.length, isUploadingFiles })
      return
    }

    setIsUploadingFiles(true)
    addToast(`Uploading ${files.length} file${files.length > 1 ? 's' : ''}...`, 'info')
    console.info('[CHAT_UPLOAD] starting upload', { count: files.length, target: uploadTargetFolder })
    const uploadedPaths: string[] = []
    const failures: string[] = []

    for (const file of files) {
      try {
        console.info('[CHAT_UPLOAD] uploading file', { name: file.name, size: file.size, type: file.type })
        const response = await agentApi.uploadPlannerFile(file, uploadTargetFolder, `Upload ${file.name} from chat input`)
        const uploadedPath =
          response?.data?.file_path ||
          response?.data?.filepath ||
          response?.file_path ||
          response?.filepath
        if (uploadedPath && typeof uploadedPath === 'string') {
          uploadedPaths.push(uploadedPath)
          console.info('[CHAT_UPLOAD] upload success', { name: file.name, path: uploadedPath })
        } else {
          failures.push(`${file.name}: Upload succeeded but filepath missing in response`)
          console.error('[CHAT_UPLOAD] missing filepath in upload response', response)
        }
      } catch (error) {
        const message = error instanceof Error ? error.message : 'Upload failed'
        failures.push(`${file.name}: ${message}`)
        console.error('[CHAT_UPLOAD] upload failed', { name: file.name, error })
      }
    }

    if (uploadedPaths.length > 0) {
      uploadedPaths.forEach((path) => {
        const exists = chatFileContext.some((item: { path: string }) => item.path === path)
        if (!exists) {
          addFileToContext({
            name: path.split('/').pop() || path,
            path,
            type: 'file'
          })
        }
      })

      const refs = uploadedPaths.map(path => `@${path}`).join(' ')
      const prefix = inputText.trim().length > 0 ? `${inputText} ` : ''
      const newText = `${prefix}${refs} `
      setLocalInputText(newText)
      if (activeTabId) {
        setTabConfig(activeTabId, { inputText: newText })
      }

      const ws = useWorkspaceStore.getState()
      ws.fetchFiles(ws.activeFolder ?? undefined).catch(() => {})

      addToast(
        `Uploaded ${uploadedPaths.length}/${files.length} file${files.length > 1 ? 's' : ''} to ${uploadTargetFolder}`,
        'success'
      )

      setTimeout(() => {
        textareaRef.current?.focus()
      }, 0)
    }

    if (failures.length > 0) {
      addToast(`Upload failed for ${failures.length} file(s): ${failures.slice(0, 2).join('; ')}`, 'error')
    }
    if (uploadedPaths.length === 0 && failures.length === 0) {
      addToast('No files were uploaded. Please try again.', 'error')
    }
    console.info('[CHAT_UPLOAD] upload completed', { uploadedCount: uploadedPaths.length, failureCount: failures.length })

    setIsUploadingFiles(false)
  }, [activeTabId, isUploadingFiles, uploadTargetFolder, chatFileContext, addFileToContext, inputText, setTabConfig, addToast])

  const handleUploadFilesSelected = useCallback(async (event: React.ChangeEvent<HTMLInputElement>) => {
    console.info('[CHAT_UPLOAD] file input change fired')
    const files = event.target.files ? Array.from(event.target.files) : []
    event.target.value = ''
    await uploadFilesToChat(files)
  }, [uploadFilesToChat])

  const handleTextareaDragEnter = useCallback((event: React.DragEvent<HTMLTextAreaElement>) => {
    if (!event.dataTransfer?.types?.includes('Files')) return
    event.preventDefault()
    event.stopPropagation()
    dragCounterRef.current += 1
    setIsDraggingFiles(true)
  }, [])

  const handleTextareaDragOver = useCallback((event: React.DragEvent<HTMLTextAreaElement>) => {
    if (!event.dataTransfer?.types?.includes('Files')) return
    event.preventDefault()
    event.stopPropagation()
    event.dataTransfer.dropEffect = 'copy'
  }, [])

  const handleTextareaDragLeave = useCallback((event: React.DragEvent<HTMLTextAreaElement>) => {
    if (!event.dataTransfer?.types?.includes('Files')) return
    event.preventDefault()
    event.stopPropagation()
    dragCounterRef.current = Math.max(0, dragCounterRef.current - 1)
    if (dragCounterRef.current === 0) {
      setIsDraggingFiles(false)
    }
  }, [])

  const handleTextareaDrop = useCallback(async (event: React.DragEvent<HTMLTextAreaElement>) => {
    if (!event.dataTransfer?.files) return
    event.preventDefault()
    event.stopPropagation()
    dragCounterRef.current = 0
    setIsDraggingFiles(false)
    const files = Array.from(event.dataTransfer.files)
    console.info('[CHAT_UPLOAD] files dropped', { count: files.length })
    await uploadFilesToChat(files)
  }, [uploadFilesToChat])

  // Inline skill popup: toggle skill (stays open for multi-select)
  const handleSkillPopupToggle = useCallback((skillFolderName: string) => {
    onSkillToggle(skillFolderName)
  }, [onSkillToggle])

  // Close skill popup: remove trigger text and close
  const handleSkillPopupClose = useCallback(() => {
    if (exclamationPosition >= 0) {
      const before = inputText.substring(0, exclamationPosition)
      const after = inputText.substring(exclamationPosition + 1 + skillPopupSearchQuery.length)
      const newText = (before + after).replace(/  +/g, ' ')
      setLocalInputText(newText)
      if (syncToStoreTimeoutRef.current) {
        clearTimeout(syncToStoreTimeoutRef.current)
        syncToStoreTimeoutRef.current = null
      }
      if (activeTabId) {
        setTabConfig(activeTabId, { inputText: newText })
      }
    }
    setShowSkillPopup(false)
    setExclamationPosition(-1)
    setSkillPopupSearchQuery('')
    setTimeout(() => textareaRef.current?.focus(), 0)
  }, [exclamationPosition, inputText, skillPopupSearchQuery, activeTabId, setTabConfig])

  // Inline server popup: toggle server (stays open for multi-select)
  const handleServerPopupToggle = useCallback((serverName: string) => {
    onManualServerToggle(serverName)
  }, [onManualServerToggle])

  // Close server popup: remove trigger text and close
  const handleServerPopupClose = useCallback(() => {
    if (dollarPosition >= 0) {
      const before = inputText.substring(0, dollarPosition)
      const after = inputText.substring(dollarPosition + 1 + serverPopupSearchQuery.length)
      const newText = (before + after).replace(/  +/g, ' ')
      setLocalInputText(newText)
      if (syncToStoreTimeoutRef.current) {
        clearTimeout(syncToStoreTimeoutRef.current)
        syncToStoreTimeoutRef.current = null
      }
      if (activeTabId) {
        setTabConfig(activeTabId, { inputText: newText })
      }
    }
    setShowServerPopup(false)
    setDollarPosition(-1)
    setServerPopupSearchQuery('')
    setTimeout(() => textareaRef.current?.focus(), 0)
  }, [dollarPosition, inputText, serverPopupSearchQuery, activeTabId, setTabConfig])

  // Memoized items arrays for inline popups
  const skillPopupItems: InlineSelectionItem[] = useMemo(() => {
    const seen = new Set<string>()
    return allSkills
      .filter(s => {
        if (seen.has(s.folder_name)) return false
        seen.add(s.folder_name)
        return true
      })
      .map(s => ({
        id: s.folder_name,
        name: s.frontmatter.name,
        description: s.frontmatter.description,
        isSelected: selectedSkills.includes(s.folder_name)
      }))
  }
  , [allSkills, selectedSkills])

  const serverPopupItems: InlineSelectionItem[] = useMemo(() =>
    [...new Set(availableServers)].map(name => ({
      id: name,
      name,
      isSelected: manualSelectedServers.includes(name)
    }))
  , [availableServers, manualSelectedServers])

  const resumeKindCounts = useMemo(() => {
    return resumeSessions.reduce<Record<ResumeSessionKind, number>>((counts, session) => {
      counts[getResumeSessionKind(session)] += 1
      return counts
    }, { chat: 0, schedule: 0, bot: 0 })
  }, [resumeSessions])

  const resumeFilterTabs: InlineSelectionFilterTab[] = useMemo(() => [
    { id: 'chat', label: 'Chats', count: resumeKindCounts.chat, icon: <MessageSquare className="h-3.5 w-3.5" /> },
    { id: 'schedule', label: 'Schedules', count: resumeKindCounts.schedule, icon: <CalendarClock className="h-3.5 w-3.5" /> },
    { id: 'bot', label: 'Bots', count: resumeKindCounts.bot, icon: <Bot className="h-3.5 w-3.5" /> },
    { id: 'all', label: 'All', count: resumeSessions.length, icon: <History className="h-3.5 w-3.5" /> },
  ], [resumeKindCounts, resumeSessions.length])

  const resumeFooterSummary = useMemo(() => {
    return `${resumeKindCounts.chat} chats · ${resumeKindCounts.schedule} schedules · ${resumeKindCounts.bot} bots`
  }, [resumeKindCounts])

  const resumeChatItems: InlineSelectionItem[] = useMemo(() => {
    const contextPaths = new Set(chatFileContext.map(item => item.path))
    const visibleSessions = resumeFilter === 'all'
      ? resumeSessions
      : resumeSessions.filter(session => getResumeSessionKind(session) === resumeFilter)

    return visibleSessions.map(session => {
      const path = resumeChatConversationPath(session)
      const kind = getResumeSessionKind(session)
      const botProvider = kind === 'bot' ? session.session_id.match(/^bot-([^-]+)--/)?.[1] : undefined
      const runtimeLabel = resumeChatRuntimeLabel(session)
      const workshopModeLabel = resumeChatWorkshopModeLabel(session)
      const mode = botProvider || (session.agent_mode || 'chat').replace(/_/g, ' ')
      const messageCount = session.message_count ?? 0
      const countLabel = messageCount > 0 ? `${messageCount} message${messageCount === 1 ? '' : 's'}` : 'conversation'
      const leadingIcon =
        kind === 'schedule'
          ? <CalendarClock className="h-4 w-4 text-amber-500" />
          : kind === 'bot'
            ? <Bot className="h-4 w-4 text-violet-500" />
            : <MessageSquare className="h-4 w-4 text-sky-500" />
      return {
        id: session.session_id,
        name: resumeChatTitle(session),
        description: [
          formatResumeChatTime(session.updated_at || session.created_at),
          mode,
          workshopModeLabel,
          runtimeLabel,
          countLabel,
        ].filter(Boolean).join(' · '),
        isSelected: contextPaths.has(path),
        leadingIcon,
        badge: runtimeLabel ? (session.runtime?.kind === 'coding_agent' ? 'coding' : 'llm') : kind === 'schedule' ? 'scheduled' : kind === 'bot' ? 'bot' : undefined,
        details: resumeChatDetails(session),
      }
    })
  }, [chatFileContext, resumeFilter, resumeSessions])

  // When user presses → on a folder in the file dialog, set search context to that folder (input after @ becomes folder path)
  const handleNavigateIntoFolder = useCallback((folderPath: string) => {
    if (atPosition === -1 || !activeTabId) return
    const beforeAt = inputText.substring(0, atPosition + 1)
    const newText = beforeAt + folderPath
    setLocalInputText(newText)
    if (syncToStoreTimeoutRef.current) {
      clearTimeout(syncToStoreTimeoutRef.current)
      syncToStoreTimeoutRef.current = null
    }
    setTabConfig(activeTabId, { inputText: newText })
    setFileSearchQuery(folderPath)
  }, [atPosition, inputText, activeTabId, setTabConfig])

  // Removed editing preset query functionality - not needed for multi-agent mode

  // Check if query is valid (view-only tabs cannot submit)
  const hasValidQuery = Boolean(inputText?.trim())
  const inputDisabled = isSummarizing || isViewOnly || (!tabSessionId && !canBootstrapMultiAgentTab && !canBootstrapWorkflowPhaseTab)
  const submitButtonDisabled = !hasValidQuery || !hasSubmitTarget || isViewOnly || isCdpDisconnected || isPlaywrightMissing
  
  // Memoized placeholder
  const placeholder = useMemo(() => {
    if (isViewOnly) return "View only — cannot continue this conversation"
    if (isWorkflowPhaseChat) {
      return 'Chat with the workflow builder... (@ files, / commands, # workflows)'
    }
    const baseHints = "@ files, / commands, # workflows, ! skills, $ servers"
    if (!tabSessionId && (canBootstrapMultiAgentTab || canBootstrapWorkflowPhaseTab)) return `Ask anything... chat will initialize on send (${baseHints})`
    if (isMultiAgentMode) return `Ask anything... (${baseHints})`
    return `Ask anything... (${baseHints})`
  }, [isViewOnly, isMultiAgentMode, isWorkflowPhaseChat, workflowPhaseId, tabSessionId, canBootstrapMultiAgentTab, canBootstrapWorkflowPhaseTab])

  // For view-only (restored) tabs, show a minimal indicator instead of the full input form
  if (isViewOnly) {
    const isScheduledRun = activeTab?.metadata?.isScheduledRun
    const isBotRun = activeTab?.metadata?.isBotRun
    const jobName = activeTab?.metadata?.scheduledJobName
    const botPlatform = activeTab?.metadata?.botPlatform
    return (
      <div data-tour="chat-input-area" data-testid="tour-chat-input-area" className="px-4 py-2 border-t border-gray-200 dark:border-gray-700">
        <div className="flex items-center justify-center gap-2 py-1 text-xs text-muted-foreground">
          <History className="w-3.5 h-3.5" />
          <span>
            {isScheduledRun
              ? `Scheduled run — view only${jobName ? ` (${jobName})` : ''}`
              : isBotRun
                ? `${botPlatform || 'Bot'} run — view only`
              : 'View only — restored conversation'}
          </span>
        </div>
      </div>
    )
  }

  return (
    <TooltipProvider>
      <div className="space-y-2">
      {/* Pasted-text Attachments */}
      {chatPastedAttachments.length > 0 && (
        <div className="px-4">
          <div className="border rounded px-1.5 py-0.5 mb-1 bg-gray-50 dark:bg-gray-800 border-gray-200 dark:border-gray-700">
            <div className="flex items-center gap-1.5 flex-wrap">
              <span className="text-xs font-medium text-gray-600 dark:text-gray-400">
                <Paperclip className="w-3 h-3 inline-block mr-0.5 -mt-0.5" />
                Pasted text:
              </span>
              {chatPastedAttachments.map((p, index) => {
                const marker = p.marker || `[paste${index + 1}]`
                const sizeLabel = p.chars >= 1024 ? `${(p.chars / 1024).toFixed(1)}KB` : `${p.chars}ch`
                return (
                  <div key={p.id} className="flex items-center gap-0.5">
                    <span
                      className="text-xs text-gray-700 dark:text-gray-300 font-mono"
                      title={`${marker} pasted text, ${p.lines} line${p.lines === 1 ? '' : 's'}, ${p.chars} character${p.chars === 1 ? '' : 's'}`}
                    >
                      {marker} · {p.lines}L · {sizeLabel}
                    </span>
                    <button
                      type="button"
                      onClick={() => removePastedAttachment(p.id)}
                      className="p-0.5 hover:bg-red-100 dark:hover:bg-red-900/20 rounded text-red-500 hover:text-red-700 dark:hover:text-red-400"
                      title="Remove pasted attachment"
                    >
                      <X className="w-2 h-2" />
                    </button>
                    {index < chatPastedAttachments.length - 1 && (
                      <span className="text-xs text-gray-400">&bull;</span>
                    )}
                  </div>
                )
              })}
              <button
                type="button"
                onClick={clearPastedAttachments}
                className="text-xs text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 hover:underline ml-0.5"
              >
                Clear
              </button>
            </div>
          </div>
        </div>
      )}

      {/* File Context Display */}
      {chatFileContext.length > 0 && (
        <div className="px-4 border-t border-gray-200 dark:border-gray-700">
          <FileContextDisplay
            files={chatFileContext}
            onRemoveFile={removeFileFromContext}
            onClearAll={clearFileContext}
            agentMode={agentMode}
            isRequiredFolderSelected={true}
          />
        </div>
      )}


      {/* Workflow Context Display — same style as FileContextDisplay */}
      {(tabConfig?.workflowContext?.length ?? 0) > 0 && (
        <div className="px-4">
          <div className="border rounded px-1.5 py-0.5 mb-1 bg-gray-50 dark:bg-gray-800 border-gray-200 dark:border-gray-700">
            <div className="flex items-center gap-1.5 flex-wrap">
              <span className="text-xs font-medium text-gray-600 dark:text-gray-400">
                <Layers className="w-3 h-3 inline-block mr-0.5 -mt-0.5" />
                Workflows:
              </span>
              {tabConfig!.workflowContext.map((w, index) => (
                <div key={w.presetId} className="flex items-center gap-0.5">
                  <span className="text-xs text-gray-700 dark:text-gray-300 font-mono">
                    {w.label}
                  </span>
                  <button
                    type="button"
                    onClick={() => {
                      if (activeTabId) {
                        const remaining = tabConfig!.workflowContext.filter(wc => wc.presetId !== w.presetId)
                        setTabConfig(activeTabId, { workflowContext: remaining })
                        const ref = '#' + w.label
                        if (inputText.includes(ref)) {
                          const newText = inputText.replace(ref, '').replace(/  +/g, ' ').trim()
                          setLocalInputText(newText)
                          setTabConfig(activeTabId, { inputText: newText })
                        }
                      }
                    }}
                    className="p-0.5 hover:bg-red-100 dark:hover:bg-red-900/20 rounded text-red-500 hover:text-red-700 dark:hover:text-red-400"
                    title="Remove workflow context"
                  >
                    <X className="w-2 h-2" />
                  </button>
                  {index < tabConfig!.workflowContext.length - 1 && (
                    <span className="text-xs text-gray-400">&bull;</span>
                  )}
                </div>
              ))}
              <button
                type="button"
                onClick={() => {
                  if (activeTabId) {
                    const labels = tabConfig!.workflowContext.map(w => '#' + w.label)
                    setTabConfig(activeTabId, { workflowContext: [] })
                    let newText = inputText
                    labels.forEach(ref => { newText = newText.replace(ref, '') })
                    newText = newText.replace(/  +/g, ' ').trim()
                    setLocalInputText(newText)
                    setTabConfig(activeTabId, { inputText: newText })
                  }
                }}
                className="text-xs text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 hover:underline ml-0.5"
              >
                Clear
              </button>
            </div>
          </div>
        </div>
      )}


      {/* Input Form */}
      <div data-tour="chat-input-area" data-testid="tour-chat-input-area" className="px-4 py-2 border-t border-gray-200 dark:border-gray-700">
        <form onSubmit={handleSubmit} className="space-y-2">
          <div className="space-y-1">
            {/* Queued messages indicator */}
            {queuedMessages.length > 0 && (
              <div className="space-y-1">
                {queuedDisplayItems.map((item, index) => {
                  if (item.type === 'auto-group') {
                    return (
                      <QueuedAutoNotificationGroup
                        key={`auto-group-${item.items[0]?.index ?? index}`}
                        items={item.items}
                        onDelete={removeQueuedMessageAtIndex}
                        onSteer={canShowSteer && tabSessionId ? handleSteerQueuedMessage : undefined}
                        steeringIndex={steeringIndex}
                      />
                    )
                  }

                  const isLong = item.msg.length > 150
                  const preview = isLong ? item.msg.substring(0, 150) + '...' : item.msg
                  return (
                    <QueuedMessageItem
                      key={item.index}
                      index={item.index}
                      msg={item.msg}
                      preview={preview}
                      isLong={isLong}
                      onDelete={() => removeQueuedMessageAtIndex(item.index)}
                      onSteer={canShowSteer && tabSessionId ? () => handleSteerQueuedMessage(item.index, item.msg) : undefined}
                      isSteering={steeringIndex === item.index}
                    />
                  )
                })}
              </div>
            )}
            {/* Show text input */}
            <Textarea
              data-tour="chat-input-box"
              ref={textareaRef}
              value={inputText}
              onChange={handleTextChange}
              onFocus={() => { void ensureMultiAgentTabReady() }}
              onKeyDown={handleKeyDown}
              onPaste={handlePaste}
              onDragEnter={handleTextareaDragEnter}
              onDragOver={handleTextareaDragOver}
              onDragLeave={handleTextareaDragLeave}
              onDrop={handleTextareaDrop}
              placeholder={placeholder}
              className={`!min-h-[40px] max-h-[100px] resize-none text-xs overflow-y-auto leading-[1.3] !py-1 !px-3 placeholder:text-xs ${
                isDraggingFiles ? 'ring-2 ring-blue-500 border-blue-500 bg-blue-50/30 dark:bg-blue-900/10' : ''
              }`}
              disabled={inputDisabled}
              data-testid="chat-input-textarea"
            />
            {isDraggingFiles && (
              <div className="text-[11px] text-blue-600 dark:text-blue-400 px-1">
                Drop files to upload and attach to this chat
              </div>
            )}
            <div className="flex justify-between items-center">
              <div className="flex items-center gap-2">
                {/* Workflow phase chat: show active LLM label */}
                {hideExtras && isWorkflowPhaseChat && (
                  <div className="flex max-w-[18rem] items-center gap-1 rounded-md border border-gray-300 bg-gray-100 px-2 py-1.5 text-xs text-gray-500 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-400">
                    <span className="truncate">{activeLLMLabel}</span>
                    {supportsLiveCodingAgentInput && (
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <button
                            type="button"
                            onClick={handleTerminalOutputButtonClick}
                            aria-pressed={terminalOutputVisible}
                            aria-label={terminalOutputVisible ? 'Terminal output visible; click to hide' : 'Terminal output hidden; click to show'}
                            title={terminalOutputVisible ? 'Terminal visible' : 'Terminal hidden'}
                            className={`ml-0.5 inline-flex h-5 w-5 shrink-0 items-center justify-center rounded border transition-colors ${
                              terminalOutputVisible
                                ? 'border-blue-500 bg-blue-600 text-white shadow-sm dark:border-blue-400 dark:bg-blue-500 dark:text-white'
                                : 'border-transparent text-gray-500 hover:bg-gray-200 hover:text-gray-800 dark:text-gray-400 dark:hover:bg-gray-700 dark:hover:text-gray-100'
                            }`}
                          >
                            <Terminal className="h-3.5 w-3.5" />
                          </button>
                        </TooltipTrigger>
                        <TooltipContent side="top">
                          <p>{terminalOutputVisible ? 'Terminal visible' : 'Terminal hidden'}</p>
                        </TooltipContent>
                      </Tooltip>
                    )}
                  </div>
                )}

                {/* Server and LLM Selection — hidden in workflow phase chat (servers come from preset) */}
                {(
                  <div data-tour="chat-input-tools" data-testid="tour-chat-input-tools" className="flex items-center gap-2">

                      <>
                        {!hideExtras && (
                        <ServerSelectionDropdown
                          availableServers={availableServers}
                          selectedServers={manualSelectedServers}
                          onServerToggle={onManualServerToggle}
                          onSelectAll={onSelectAllServers}
                          onClearAll={onClearAllServers}
                          disabled={isStreaming || isSummarizing}
                          agentMode={agentMode}
                        />
                        )}
                        {!hideExtras && (
                          <SkillSelectionDropdown
                            selectedSkills={selectedSkills}
                            onSkillToggle={onSkillToggle}
                            onSelectAll={onSelectAllSkills}
                            onClearAll={onClearAllSkills}
                            disabled={isStreaming || isSummarizing}
                            onImportClick={() => openDialog('skillImport')}
                          />
                        )}
                      </>

                    {!hideExtras && !isMultiAgentMode && (
                      <TooltipProvider>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <div className="flex">
                              <LLMSelectionDropdown
                                availableLLMs={availableLLMs}
                                selectedLLM={primaryLLM}
                                onLLMSelect={onPrimaryLLMSelect}
                                onRefresh={onRefreshAvailableLLMs}
                                disabled={isStreaming || isSummarizing}
                                openDirection="up"
                              />
                            </div>
                          </TooltipTrigger>
                          <TooltipContent side="top">
                            <p>{llmConfigLocked ? 'Select from admin-configured LLMs' : 'Select Primary LLM'}</p>
                          </TooltipContent>
                        </Tooltip>
                      </TooltipProvider>
                    )}
                    {/* Browser Access Toggle — hidden in workflow mode */}
                    {!hideExtras && <button
                      type="button"
                      data-tour="chat-browser-tools"
                      data-testid="tour-chat-browser-tools"
                      onClick={() => {
                        if (browserMode === 'none') {
                          // Enabling browser: show config popup and default to headless
                          setBrowserMode('headless')
                          setShowCdpPopup(true)
                          setWorkspaceMinimized(true)
                        } else {
                          // Clicking again while enabled: re-open popup to change settings
                          setShowCdpPopup(true)
                          setWorkspaceMinimized(true)
                        }
                      }}
                      disabled={isStreaming || isSummarizing}
                      className={`group flex items-center gap-1 p-1.5 rounded-md border transition-all duration-200 ${
                        browserMode === 'cdp'
                          ? cdpConnected === false
                            ? 'bg-red-900/40 border-red-600 text-red-400'
                            : cdpChecking || cdpConnected === null
                              ? 'bg-yellow-900/40 border-yellow-600 text-yellow-400'
                              : 'bg-green-900/40 border-green-600 text-green-400'
                          : browserMode === 'playwright'
                            ? playwrightServerStatus === 'not_found'
                              ? 'bg-red-900/40 border-red-600 text-red-400'
                              : 'bg-purple-900/40 border-purple-600 text-purple-400'
                            : browserMode === 'headless'
                              ? 'bg-blue-900/40 border-blue-600 text-blue-400'
                              : 'bg-gray-800 border-gray-600 text-gray-500'
                      } ${(isStreaming || isSummarizing) ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer hover:pr-2'}`}
                    >
                      <Globe className="w-4 h-4 flex-shrink-0" />
                      {browserMode !== 'none' ? (
                        <span className={`text-[10px] font-semibold px-1 rounded ${
                          browserMode === 'cdp'
                            ? cdpConnected === false
                              ? 'bg-red-800 text-red-200'
                              : cdpChecking || cdpConnected === null
                                ? 'bg-yellow-800 text-yellow-200'
                                : 'bg-green-800 text-green-200'
                            : browserMode === 'playwright'
                              ? playwrightServerStatus === 'not_found'
                                ? 'bg-red-800 text-red-200'
                                : 'bg-purple-800 text-purple-200'
                              : 'bg-blue-800 text-blue-200'
                        }`}>
                          {browserMode === 'cdp' ? 'CDP' : browserMode === 'playwright' ? 'Playwright' : 'Headless'}
                        </span>
                      ) : (
                        <span className="text-xs font-medium max-w-0 overflow-hidden whitespace-nowrap group-hover:max-w-[60px] transition-all duration-200">
                          Browser
                        </span>
                      )}
                    </button>}
                  </div>
                )}

                {/* Browser Access Configuration Popup */}
                {showCdpPopup && (
                  <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm" onClick={() => { setShowCdpPopup(false); setWorkspaceMinimized(false) }}>
                    <div className="bg-gray-900 rounded-xl shadow-2xl border border-gray-700 w-[900px] max-w-[95vw]" onClick={(e) => e.stopPropagation()}>
                      {/* Header */}
                      <div className="flex items-center justify-between px-5 py-4 border-b border-gray-700">
                        <div className="flex items-center gap-2">
                          <Globe className="w-5 h-5 text-green-400" />
                          <h3 className="text-base font-semibold text-white">Browser Access</h3>
                        </div>
                        <button onClick={() => { setShowCdpPopup(false); setWorkspaceMinimized(false) }} className="text-gray-400 hover:text-gray-200 transition-colors">
                          <X className="w-5 h-5" />
                        </button>
                      </div>

                      {/* Content: 2-column layout */}
                      <div className="px-5 py-4 flex gap-4 items-stretch">

                        {/* Left: mode options */}
                        <div className="flex-1 space-y-2">
                          {/* Headless */}
                          <label className={`flex items-start gap-3 p-3 rounded-lg border cursor-pointer transition-colors ${
                            browserMode === 'headless'
                              ? 'border-blue-500 bg-blue-950/40'
                              : 'border-gray-700 hover:bg-gray-800'
                          }`}>
                            <input
                              type="radio"
                              name="browserMode"
                              checked={browserMode === 'headless'}
                              onChange={() => setBrowserMode('headless')}
                              className="mt-0.5 w-4 h-4 text-blue-500 accent-blue-500"
                            />
                            <div>
                              <div className="text-sm font-medium text-gray-100">Headless Browser</div>
                              <div className="text-xs text-gray-400 mt-0.5">
                                Uses{' '}
                                <a
                                  href="https://github.com/vercel/agent-browser"
                                  target="_blank"
                                  rel="noopener noreferrer"
                                  className="text-blue-400 hover:underline"
                                  onClick={(e) => e.stopPropagation()}
                                >
                                  agent-browser
                                </a>
                                {' '}by Vercel running inside a Docker container. No visible window.
                              </div>
                            </div>
                          </label>

                          {/* CDP */}
                          <label className={`flex items-start gap-3 p-3 rounded-lg border cursor-pointer transition-colors ${
                            browserMode === 'cdp'
                              ? 'border-green-500 bg-green-950/40'
                              : 'border-gray-700 hover:bg-gray-800'
                          }`}>
                            <input
                              type="radio"
                              name="browserMode"
                              checked={browserMode === 'cdp'}
                              onChange={() => setBrowserMode('cdp')}
                              className="mt-0.5 w-4 h-4 text-green-500 accent-green-500"
                            />
                            <div>
                              <div className="text-sm font-medium text-gray-100">Local Chrome (CDP)</div>
                              <div className="text-xs text-gray-400 mt-0.5">Connects to your real Chrome browser and may bring Chrome to the foreground.</div>
                            </div>
                          </label>

                          {/* Playwright */}
                          <label className={`flex items-start gap-3 p-3 rounded-lg border transition-colors ${
                            playwrightServerStatus === 'not_found'
                              ? 'border-gray-700 opacity-50 cursor-not-allowed'
                              : browserMode === 'playwright'
                                ? 'border-purple-500 bg-purple-950/40 cursor-pointer'
                                : 'border-gray-700 hover:bg-gray-800 cursor-pointer'
                          }`}>
                            <input
                              type="radio"
                              name="browserMode"
                              checked={browserMode === 'playwright'}
                              onChange={() => setBrowserMode('playwright')}
                              disabled={playwrightServerStatus === 'not_found'}
                              className="mt-0.5 w-4 h-4 text-purple-500 accent-purple-500"
                            />
                            <div className="flex-1">
                              <div className="text-sm font-medium text-gray-100">Playwright MCP</div>
                              <div className="text-xs text-gray-400 mt-0.5">Opens a visible browser window via MCP server.</div>
                              {playwrightServerStatus === 'not_found' && (
                                <div className="text-xs text-red-400 mt-1 flex items-center gap-1">
                                  <span className="w-2 h-2 rounded-full bg-red-500 flex-shrink-0" />
                                  Server not found &mdash; add in MCP Settings
                                </div>
                              )}
                              {playwrightServerStatus === 'loading' && (
                                <div className="text-xs text-yellow-400 mt-1 flex items-center gap-1">
                                  <Loader2 className="w-3 h-3 animate-spin flex-shrink-0" />
                                  Discovering...
                                </div>
                              )}
                              {playwrightServerStatus === 'error' && (
                                <div className="text-xs text-amber-400 mt-1 flex items-center gap-1">
                                  <span className="w-2 h-2 rounded-full bg-amber-500 flex-shrink-0" />
                                  Server has errors &mdash; check MCP Settings
                                </div>
                              )}
                            </div>
                          </label>

                        </div>

                        {/* Right: context panel */}
                        <div className="w-80 flex-shrink-0 rounded-lg bg-gray-800/60 border border-gray-700 p-3 flex flex-col gap-3">
                          {browserMode === 'cdp' && (<>
                            <div className="rounded-md border border-amber-700/60 bg-amber-950/30 px-2.5 py-2 text-xs text-amber-200">
                              CDP drives visible Chrome. It can steal focus while you type. Use headless mode for background runs, or use a dedicated automation Chrome/profile/port for schedules.
                            </div>
                            <div className="space-y-2">
                              <div className="flex items-center gap-2">
                                <label className="text-xs text-gray-400 whitespace-nowrap">Port:</label>
                                <input
                                  type="number"
                                  value={cdpPort}
                                  onChange={(e) => setCdpPort(parseInt(e.target.value) || 9222)}
                                  className="w-20 px-2 py-1 text-sm border border-gray-600 rounded-md bg-gray-800 text-white focus:border-green-500 focus:outline-none"
                                  min={1}
                                  max={65535}
                                />
                              </div>
                              <button
                                type="button"
                                onClick={() => checkCdpConnection(cdpPort)}
                                disabled={cdpChecking}
                                className="w-full px-3 py-1.5 text-xs font-medium bg-gray-700 hover:bg-gray-600 rounded-md text-gray-200 disabled:opacity-50 transition-colors"
                              >
                                {cdpChecking ? 'Checking...' : 'Check Connection'}
                              </button>
                              <div className="flex items-start gap-1.5">
                                {cdpChecking ? (
                                  <>
                                    <div className="w-2.5 h-2.5 rounded-full bg-yellow-400 animate-pulse mt-0.5 flex-shrink-0" />
                                    <span className="text-xs text-yellow-400">Checking port {cdpPort}...</span>
                                  </>
                                ) : cdpConnected === true ? (
                                  <>
                                    <div className="w-2.5 h-2.5 rounded-full bg-green-500 mt-0.5 flex-shrink-0" />
                                    <span className="text-xs text-green-400">Connected on port {cdpPort}.</span>
                                  </>
                                ) : cdpConnected === false ? (
                                  <>
                                    <div className="w-2.5 h-2.5 rounded-full bg-red-500 mt-0.5 flex-shrink-0" />
                                    <span className="text-xs text-red-400">Not reachable on port {cdpPort}.</span>
                                  </>
                                ) : (
                                  <span className="text-xs text-gray-500">Click Check Connection to verify.</span>
                                )}
                              </div>
                            </div>
                            <div className="border-t border-gray-700 pt-3 space-y-1.5">
                              <p className="text-xs font-medium text-gray-300">Launch Chrome with CDP</p>
                              {navigator.platform?.includes('Mac') && (
                                <div className="space-y-1">
                                  <a
                                    href={`${getApiBaseUrl()}/api/downloads/chrome-cdp-macOS.zip`}
                                    download="Chrome-CDP-macOS.zip"
                                    target="_blank"
                                    rel="noopener noreferrer"
                                    onClick={(e) => e.stopPropagation()}
                                    className="inline-flex items-center gap-1.5 px-2 py-1 text-xs font-medium bg-green-700 hover:bg-green-600 text-white rounded-md transition-colors"
                                  >
                                    <Download className="w-3 h-3" />
                                    Download Chrome CDP.app (macOS)
                                  </a>
                                  <ol className="text-xs text-gray-500 list-decimal list-inside space-y-0.5">
                                    <li>Double-click the zip to unzip.</li>
                                    <li>Drag <strong className="text-gray-300">Chrome CDP.app</strong> to your <strong className="text-gray-300">Applications</strong> folder.</li>
                                    <li>Open it from Spotlight (⌘+Space) or Launchpad.</li>
                                  </ol>
                                  <p className="text-xs text-gray-500">If macOS says &quot;damaged&quot;, run in Terminal:</p>
                                  <code className="block bg-gray-950 px-2 py-1 rounded text-[10px] font-mono text-amber-400 border border-gray-700">
                                    xattr -c /Applications/Chrome\ CDP.app
                                  </code>
                                  <p className="text-xs text-gray-600">then open it again, or right-click → Open.</p>
                                </div>
                              )}
                              <p className="text-xs text-gray-500">Or run in Terminal (close all Chrome windows first):</p>
                              <code className="block bg-gray-950 px-2 py-1.5 rounded text-[10px] font-mono break-all text-green-400 border border-gray-700">
                                {navigator.platform?.includes('Mac')
                                  ? `/Applications/Google\\ Chrome.app/Contents/MacOS/Google\\ Chrome --remote-debugging-port=${cdpPort}`
                                  : `google-chrome --remote-debugging-port=${cdpPort}`}
                              </code>
                            </div>
                          </>)}

                          {browserMode === 'headless' && (
                            <div className="space-y-2">
                              <p className="text-xs font-medium text-gray-300">Headless Browser</p>
                              <p className="text-xs text-gray-400">
                                Powered by{' '}
                                <a
                                  href="https://github.com/vercel/agent-browser"
                                  target="_blank"
                                  rel="noopener noreferrer"
                                  className="text-blue-400 hover:underline"
                                >
                                  agent-browser
                                </a>
                                {' '}by Vercel, running inside a Docker container.
                              </p>
                              <p className="text-xs text-gray-500">No visible window — the agent navigates Chromium in the background.</p>
                            </div>
                          )}

                          {browserMode === 'playwright' && (
                            <div className="space-y-2">
                              <p className="text-xs font-medium text-gray-300">Playwright MCP</p>
                              <p className="text-xs text-gray-400">
                                Uses the official{' '}
                                <a
                                  href="https://github.com/microsoft/playwright-mcp"
                                  target="_blank"
                                  rel="noopener noreferrer"
                                  className="text-purple-400 hover:underline"
                                >
                                  microsoft/playwright-mcp
                                </a>
                                {' '}server.
                              </p>
                              <p className="text-xs text-gray-500">Each session opens a <strong className="text-gray-300">new Chrome instance</strong> — existing browser windows are not reused.</p>
                              <p className="text-xs text-gray-500">Requires the <code className="bg-gray-950 px-1 rounded text-[10px]">playwright</code> MCP server to be configured in MCP Settings.</p>
                            </div>
                          )}

                          {browserMode === 'none' && (
                            <p className="text-xs text-gray-500">Select a mode to see configuration options.</p>
                          )}
                        </div>
                      </div>

                      {/* Footer */}
                      <div className="flex justify-end gap-2 px-5 py-3 border-t border-gray-700">
                        <button
                          type="button"
                          onClick={() => {
                            setBrowserMode('none')
                            setShowCdpPopup(false)
                            setWorkspaceMinimized(false)
                          }}
                          className="px-4 py-2 text-sm text-gray-300 hover:bg-gray-800 rounded-md transition-colors"
                        >
                          Disable Browser
                        </button>
                        <button
                          type="button"
                          onClick={() => { setShowCdpPopup(false); setWorkspaceMinimized(false) }}
                          disabled={browserMode === 'cdp' && cdpConnected !== true}
                          className="px-4 py-2 text-sm font-medium bg-green-600 hover:bg-green-500 text-white rounded-md transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                        >
                          {browserMode === 'cdp' && cdpConnected !== true ? (cdpChecking ? 'Checking...' : 'Connect Chrome First') : 'Done'}
                        </button>
                      </div>
                    </div>
                  </div>
                )}

                {/* Reasoning Level Popup */}
                {showReasoningPopup && (
                  <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm" onClick={() => setShowReasoningPopup(false)}>
                    <div className="bg-gray-900 rounded-xl shadow-2xl border border-gray-700 w-[320px] max-w-[90vw]" onClick={(e) => e.stopPropagation()}>
                      <div className="flex items-center justify-between px-5 py-4 border-b border-gray-700">
                        <div className="flex items-center gap-2">
                          <Bot className="w-5 h-5 text-blue-400" />
                          <h3 className="text-base font-semibold text-white">Reasoning Level</h3>
                        </div>
                        <button onClick={() => setShowReasoningPopup(false)} className="text-gray-400 hover:text-gray-200 transition-colors">
                          <X className="w-5 h-5" />
                        </button>
                      </div>
                      <div className="px-5 py-4 space-y-2">
                        <p className="text-xs text-gray-400 mb-3">Sets the default reasoning effort for delegated sub-agent tasks.</p>
                        {([
                          { level: 'high',   label: 'High',   desc: 'Deep thinking — complex reasoning, research, planning',   activeClass: 'border-orange-500 bg-orange-950/40', dotClass: 'bg-orange-500' },
                          { level: 'medium', label: 'Medium', desc: 'Balanced — good for most tasks',                          activeClass: 'border-yellow-500 bg-yellow-950/40', dotClass: 'bg-yellow-400' },
                          { level: 'low',    label: 'Low',    desc: 'Fast — simple lookups, straightforward actions',          activeClass: 'border-green-500 bg-green-950/40',  dotClass: 'bg-green-500'  },
                        ] as const).map(({ level, label, desc, activeClass, dotClass }) => (
                          <label key={level} className={`flex items-start gap-3 p-3 rounded-lg border cursor-pointer transition-colors ${
                            defaultReasoningLevel === level ? activeClass : 'border-gray-700 hover:bg-gray-800'
                          }`}>
                            <input
                              type="radio"
                              name="reasoningLevel"
                              checked={defaultReasoningLevel === level}
                              onChange={() => setDefaultReasoningLevel(level)}
                              className="sr-only"
                            />
                            <div className={`w-3 h-3 rounded-full mt-0.5 flex-shrink-0 ${defaultReasoningLevel === level ? dotClass : 'bg-gray-600'}`} />
                            <div>
                              <div className="text-sm font-medium text-gray-100">{label}</div>
                              <div className="text-xs text-gray-400 mt-0.5">{desc}</div>
                            </div>
                          </label>
                        ))}
                      </div>
                      <div className="flex justify-between gap-2 px-5 py-3 border-t border-gray-700">
                        <button
                          type="button"
                          onClick={() => { setDefaultReasoningLevel(null); setShowReasoningPopup(false) }}
                          className="px-4 py-2 text-sm text-gray-300 hover:bg-gray-800 rounded-md transition-colors"
                        >
                          Clear (Auto)
                        </button>
                        <button
                          type="button"
                          onClick={() => setShowReasoningPopup(false)}
                          className="px-4 py-2 text-sm font-medium bg-blue-600 hover:bg-blue-500 text-white rounded-md transition-colors"
                        >
                          Done
                        </button>
                      </div>
                    </div>
                  </div>
                )}

                {/* Secrets dropdown - hidden for workflow mode */}
                {!hideExtras && (
                <SecretSelectionDropdown
                  selectedSecrets={selectedSecrets}
                  onSecretToggle={onSecretToggle}
                  onSelectAll={onSelectAllSecrets}
                  onClearAll={onClearAllSecrets}
                  disabled={isStreaming || isSummarizing}
                />
                )}

                {/* Status text - removed observer initialization message */}
              </div>
              {/* Show old buttons */}
              {(
                <div className="flex items-center gap-2">
                  {/* Active agents indicator — left of context circle */}
                  {activeAgents.length > 0 && (
                    <div className="relative">
                      <button
                        type="button"
                        data-tour="chat-active-agents"
                        data-testid="tour-chat-active-agents"
                        onClick={() => setShowActiveAgentsPanel(prev => !prev)}
                        className="flex items-center gap-1.5 px-2 py-1 rounded-md bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 hover:bg-blue-100 dark:hover:bg-blue-900/40 transition-colors"
                      >
                        <Loader2 className="w-3 h-3 animate-spin text-blue-500 dark:text-blue-400 flex-shrink-0" />
                        <span className="text-[11px] text-blue-600 dark:text-blue-400 font-medium">
                          {activeAgents.length}
                        </span>
                      </button>
                      {showActiveAgentsPanel && (
                        <>
                          <div className="fixed inset-0 z-40" onClick={() => setShowActiveAgentsPanel(false)} />
                          <div className="absolute bottom-full right-0 mb-2 z-50 w-80 bg-white dark:bg-gray-800 rounded-lg shadow-lg border border-gray-200 dark:border-gray-700 overflow-hidden">
                            <div className="px-3 py-2 border-b border-gray-200 dark:border-gray-700 flex items-center justify-between">
                              <span className="text-xs font-semibold text-gray-700 dark:text-gray-300">
                                {activeAgents.length === 1 ? '1 agent running' : `${activeAgents.length} agents running`}
                              </span>
                              <button type="button" onClick={() => setShowActiveAgentsPanel(false)} className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-200">
                                <X className="w-3.5 h-3.5" />
                              </button>
                            </div>
                            <div className="max-h-48 overflow-y-auto">
                              {activeAgents.map((a) => {
                                const displayName = compactActiveAgentName(a.name)
                                return (
                                  <div
                                    key={a.id}
                                    className="flex items-center gap-1.5 py-1.5 pr-3 border-b last:border-b-0 border-gray-100 dark:border-gray-700/50"
                                    style={{ paddingLeft: `${10 + a.depth * 18}px` }}
                                    title={a.name}
                                  >
                                    {a.treePrefix && (
                                      <span className="shrink-0 font-mono text-[10px] leading-none text-gray-400 dark:text-gray-500">
                                        {a.treePrefix}
                                      </span>
                                    )}
                                    <Loader2 className="w-3 h-3 animate-spin text-blue-500 dark:text-blue-400 flex-shrink-0" />
                                    <div className="min-w-0 flex-1 flex items-center gap-2">
                                      <span className="text-xs text-gray-700 dark:text-gray-300 truncate">{displayName}</span>
                                      <span className="shrink-0 rounded-full border border-gray-200 dark:border-gray-600 px-1.5 py-0.5 text-[10px] leading-none text-gray-500 dark:text-gray-400">
                                        {a.type === 'delegation' ? 'sub-agent' : a.depth === 0 ? 'step' : 'agent'}
                                      </span>
                                    </div>
                                  </div>
                                )
                              })}
                            </div>
                          </div>
                        </>
                      )}
                    </div>
                  )}

                  {/* Context completion indicator */}
                  {contextUsagePercent !== null && (
                    <CircularProgress
                      percentage={contextUsagePercent}
                      size={24}
                      strokeWidth={2.5}
                      tokenUsage={latestTokenUsage}
                    />
                  )}
                  {isSummarizing ? (
                    <div className="flex items-center gap-2 px-3 py-1.5 text-sm text-gray-600 dark:text-gray-400">
                      <Loader2 className="w-4 h-4 animate-spin" />
                      <span>Summarizing...</span>
                    </div>
                  ) : (
                    <div data-tour="chat-send-controls" data-testid="tour-chat-send-controls" className="flex items-center gap-1">
                      {/* Workshop mode toggle: Build / Optimize / Run */}
                      {workflowPhaseId === 'workflow-builder' && !isOrganizationContext && (
                        <WorkshopModeToggle />
                      )}
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <Button
                            type="button"
                            variant="outline"
                            size="sm"
                            disabled={isStreaming || isSummarizing || isUploadingFiles}
                            onClick={() => {
                              const inputEl = fileUploadInputRef.current
                              if (!inputEl) {
                                console.error('[CHAT_UPLOAD] upload input ref not available')
                                addToast('Upload input not ready. Please retry.', 'error')
                                return
                              }
                              console.info('[CHAT_UPLOAD] opening file picker')
                              inputEl.click()
                            }}
                            className="px-2.5"
                            data-testid="chat-upload-button"
                          >
                            {isUploadingFiles ? (
                              <Loader2 className="w-4 h-4 animate-spin" />
                            ) : (
                              <Paperclip className="w-4 h-4" />
                            )}
                          </Button>
                        </TooltipTrigger>
                        <TooltipContent>
                          <p>{isUploadingFiles ? 'Uploading files...' : `Upload file(s) to ${uploadTargetFolder}`}</p>
                        </TooltipContent>
                      </Tooltip>
                      {isStreaming ? (
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <Button
                              type="button"
                              variant="destructive"
                              onClick={() => {
                                justStoppedStreamingRef.current = true
                                setTimeout(() => { justStoppedStreamingRef.current = false }, 300)
                                onStopStreaming()
                              }}
                              size="sm"
                              className="px-3"
                              data-testid="chat-stop-button"
                            >
                              <Square className="w-4 h-4" />
                            </Button>
                          </TooltipTrigger>
                          <TooltipContent>
                            <p>Stop streaming</p>
                          </TooltipContent>
                        </Tooltip>
                      ) : (
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <Button
                              type="submit"
                              disabled={submitButtonDisabled}
                              size="sm"
                              className="px-3"
                              data-testid="chat-submit-button"
                            >
                              <Send className="w-4 h-4" />
                            </Button>
                          </TooltipTrigger>
                          <TooltipContent>
                            <p>
                              {isViewOnly
                                ? 'View only — cannot continue this conversation'
                                : !inputText?.trim()
                                  ? 'Type a message to send'
                                  : isCdpDisconnected
                                    ? 'Chrome CDP not reachable. Check connection.'
                                    : isPlaywrightMissing
                                      ? 'Playwright MCP server not found. Add it in MCP Settings.'
                                      : !tabSessionId && !canBootstrapWorkflowPhaseTab && !canBootstrapMultiAgentTab
                                        ? 'Session not ready yet'
                                        : 'Send message'
                              }
                            </p>
                          </TooltipContent>
                        </Tooltip>
                      )}
                    </div>
                  )}
                </div>
              )}
            </div>
          </div>
        </form>
        <input
          ref={fileUploadInputRef}
          type="file"
          multiple
          onChange={handleUploadFilesSelected}
          className="hidden"
          disabled={isStreaming || isSummarizing || isUploadingFiles}
        />
      </div>
      
      {/* Command Selection Dialog */}
      <CommandSelectionDialog
        isOpen={showCommandDialog}
        onClose={handleCommandDialogClose}
        onSelectCommand={handleCommandSelect}
        searchQuery={commandSearchQuery}
        position={commandDialogPosition}
        modeCategory={selectedModeCategory}
        workshopMode={selectedModeCategory === 'workflow' ? getEffectiveWorkflowModes().workshopMode : undefined}
        onManageCommands={handleManageCommands}
        onEditCommand={handleEditCommand}
        onDeleteCommand={handleDeleteCommand}
      />

      <InlineSelectionPopup
        isOpen={showResumeDialog}
        onClose={handleResumeDialogClose}
        onToggleItem={handleResumeChatSelect}
        items={resumeChatItems}
        searchQuery=""
        position={resumeDialogPosition}
        title="Attach Previous Context"
        icon={<History className="w-4 h-4 text-muted-foreground" />}
        emptyMessage="No previous context found"
        isLoading={resumeSessionsLoading}
        filterTabs={resumeFilterTabs}
        activeFilterId={resumeFilter}
        onFilterChange={id => setResumeFilter(id as ResumeFilter)}
        footerSummary={resumeFooterSummary}
        footerActions={
          <button
            type="button"
            onMouseDown={e => e.preventDefault()}
            onClick={handleResumeCleanupOldChats}
            disabled={resumeCleanupLoading || resumeSessionsLoading}
            className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-[11px] font-medium text-red-600 hover:bg-red-50 hover:text-red-700 disabled:cursor-not-allowed disabled:opacity-50 dark:text-red-400 dark:hover:bg-red-950/40 dark:hover:text-red-300"
            title="Delete conversations older than 14 days"
          >
            {resumeCleanupLoading ? <Loader2 className="h-3 w-3 animate-spin" /> : <Trash2 className="h-3 w-3" />}
            Delete &gt;14d
          </button>
        }
        searchPlaceholder="Search previous context..."
        widthClassName="w-[min(720px,calc(100vw-32px))] max-w-[720px]"
        enterHint="Enter to attach"
      />

      {/* Command Editor Dialog */}
      <CommandEditorDialog
        isOpen={showCommandEditor}
        onClose={handleCommandEditorClose}
        editingCommand={editingUserCommand}
      />

      {/* File Selection Dialog */}
      <FileSelectionDialog
        isOpen={showFileDialog}
        onClose={handleFileDialogClose}
        onSelectFile={handleFileSelect}
        onNavigateIntoFolder={handleNavigateIntoFolder}
        searchQuery={fileSearchQuery}
        position={fileDialogPosition}
        extraFiles={extraAtFiles}
      />

      {/* Workflow Selection Dialog */}
      <WorkflowSelectionDialog
        isOpen={showWorkflowDialog}
        onClose={handleWorkflowDialogClose}
        onSelectWorkflow={handleWorkflowSelect}
        searchQuery={workflowSearchQuery}
        position={workflowDialogPosition}
      />

      {/* Inline Skill Selection Popup */}
      <InlineSelectionPopup
        isOpen={showSkillPopup}
        onClose={handleSkillPopupClose}
        onToggleItem={handleSkillPopupToggle}
        items={skillPopupItems}
        searchQuery={skillPopupSearchQuery}
        position={skillPopupPosition}
        title="Skills"
        icon={<Wand2 className="w-4 h-4 text-muted-foreground" />}
        emptyMessage="No skills available"
        isLoading={skillsLoading}
      />

      {/* Inline Server Selection Popup */}
      <InlineSelectionPopup
        isOpen={showServerPopup}
        onClose={handleServerPopupClose}
        onToggleItem={handleServerPopupToggle}
        items={serverPopupItems}
        searchQuery={serverPopupSearchQuery}
        position={serverPopupPosition}
        title="MCP Servers"
        icon={<Server className="w-4 h-4 text-muted-foreground" />}
        emptyMessage="No MCP servers available"
      />

      {/* Slash command dialogs */}
      {showSkillImport && (
        <SkillImportDialog
          onClose={() => closeDialog('skillImport')}
          onSuccess={() => closeDialog('skillImport')}
        />
      )}

      {showMCPDetails && (
        <MCPDetailsModal
          onClose={() => closeDialog('mcpDetails')}
          onOpenConfigEditor={() => openDialog('mcpConfig')}
        />
      )}
      {showMCPConfig && (
        <MCPConfigPopup
          onClose={() => closeDialog('mcpConfig')}
        />
      )}
      <LLMConfigurationModal
        isOpen={showModels}
        onClose={() => closeDialog('models')}
      />
      </div>
    </TooltipProvider>
  )
}

ChatInputComponent.displayName = 'ChatInput'

export const ChatInput = React.memo(ChatInputComponent)
