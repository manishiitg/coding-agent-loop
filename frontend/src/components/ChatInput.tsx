import { routeForQueuedMessage, splitQueuedMessages } from '../utils/queuedMessageDelivery'
import { resolvePiModelGroup } from '../utils/llmDisplay'
import React, { useRef, useCallback, useMemo, useState, useEffect, useLayoutEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useShallow } from 'zustand/react/shallow'

const DBG = '[skill-popup]'
import { Send, Wand2, Loader2, Globe, Layers, X, History, Server, Download, Paperclip, Terminal, Plus, Play } from 'lucide-react'
import { Button } from './ui/Button'
import { Textarea } from './ui/Textarea'
import FileContextDisplay from './FileContextDisplay'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from './ui/tooltip'
import ServerSelectionDropdown from './ServerSelectionDropdown'
import SkillSelectionDropdown from './skills/SkillSelectionDropdown'
import FileSelectionDialog from './FileSelectionDialog'
import CommandSelectionDialog from './CommandSelectionDialog'
import { CommandEditorDialog } from './commands/CommandEditorDialog'
import { findCommand, findCommandAnyMode, loadAndRegisterUserCommands, type CommandContext, type CommandDefinition } from '../commands'
import { commandsApi } from '../api/commands'
import WorkflowSelectionDialog from './WorkflowSelectionDialog'
import { isChatCompatiblePhase } from '../utils/chatSubmitHelpers'
import { useWorkflowStore } from '../stores/useWorkflowStore'
import { useWorkflowManifestStore } from '../stores/useWorkflowManifestStore'
import { chromeCdpInstallCommand, chromeCdpLaunchCommand, chromeCdpVerifyCommand, chromeCdpZipUrl } from '../utils/cdpSetup'
import { CHAT_TOOL_COMMAND_EVENT, chatToolCommandFromEvent } from '../utils/chatToolEvents'
import { loadAgentProfileCapabilityEnabled, loadAgentProfileProviderOptions, loadAgentProfileRuntime, type AgentProfileProviderOption } from '../utils/agentProfileCapabilities'
import { llmConfigService, type ModelMetadata } from '../services/llm-config-api'
import ModelReasoningControl from './ui/ModelReasoningControl'
import NewChatControl from './ui/NewChatControl'
import { MicButton, type MicButtonHandle, type MicState } from '../voice/MicButton'
import { useCapabilitiesStore } from '../stores/useCapabilitiesStore'
import { hasActiveSessionWork } from '../utils/activitySessions'
import { headerStatusLabel, statusTone } from '../utils/globalActivityMonitorStatus'
import { shouldClearAcceptedChatDraft } from '../utils/chatSubmissionDraft'
import { liveTerminalControlKey } from '../utils/liveTerminalKeys'
import { chatUsesStructuredTransport, shouldRouteChatInputToLiveTransport, shouldShowLiveTerminalControl } from '../utils/liveInputSubmission'
import { effectiveLLMUnderLock, effectiveProviderUnderLock } from '../utils/effectiveLLM'
import { normalizeEventViewMode } from '../stores/useChatStore'
import { activateTab } from '../utils/activateTab'
import { isBlankWorkflowBuilderTab } from '../utils/workflowTabResolution'

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

// Clipboard image helpers live in ./clipboardImages so they can be unit
// tested without importing this component (which pulls the whole app graph).


const getHttpErrorStatus = (err: unknown): number | undefined => {
  return (err as { response?: { status?: number } } | undefined)?.response?.status
}

const isLiveCodingSessionGoneStatus = (status: number | undefined) => {
  return status === 404 || status === 409 || status === 410
}

const isLikelyBackendUnavailableError = (err: unknown) => {
  const status = getHttpErrorStatus(err)
  if (status !== undefined) return false
  const candidate = err as { code?: string; request?: unknown; message?: string } | undefined
  const message = String(candidate?.message ?? '').toLowerCase()
  return Boolean(candidate?.request)
    || candidate?.code === 'ERR_NETWORK'
    || message.includes('network error')
    || message.includes('failed to fetch')
}

type LiveMessageDeliveryStatus = 'sending' | 'sent_to_cli' | 'queued_for_injection' | 'next_turn_started' | 'queued_locally' | 'failed'

interface LiveMessageDelivery {
  status: LiveMessageDeliveryStatus
  message: string
  provider?: string
  detail?: string
}

const formatLiveInputProviderLabel = (provider?: string | null, modelId?: string | null) => {
  const normalized = (provider || '').trim().toLowerCase()
  switch (normalized) {
    case 'claude-code':
    case 'claude_code':
      return 'Claude Code'
    case 'codex-cli':
    case 'codex_cli':
      return 'Codex CLI'
    case 'cursor-cli':
    case 'cursor_cli':
      return 'Cursor CLI'
    case 'pi-cli':
    case 'pi_cli': {
      const group = resolvePiModelGroup(modelId || undefined)
      return group || 'the coding agent'
    }
    default:
      return provider ? provider.replace(/[-_]/g, ' ') : 'live agent'
  }
}

const liveDeliveryPreview = (message: string) => {
  const normalized = message.replace(/\s+/g, ' ').trim()
  if (normalized.length <= 90) return normalized
  return `${normalized.slice(0, 87)}...`
}

import InlineSelectionPopup from './InlineSelectionPopup'
import type { InlineSelectionItem } from './InlineSelectionPopup'
import SkillImportDialog from './skills/SkillImportDialog'
import { MCPConfigPopup } from './MCPConfigPopup'
import MCPDetailsModal from './MCPDetailsModal'
import LLMConfigurationModal from './LLMConfigurationModal'
import type { PlannerFile, LLMProvider, TerminalSnapshot } from '../services/api-types'
import type { LLMOption } from '../types/llm'
import { useAppStore, useMCPStore, useLLMStore, useChatStore } from '../stores'
import { useWorkspaceStore } from '../stores/useWorkspaceStore'
import { useCommandDialogStore } from '../stores/useCommandDialogStore'
import { usePresetApplication, useGlobalPresetStore } from '../stores/useGlobalPresetStore'
import { useModeStore } from '../stores/useModeStore'
import { agentApi } from '../services/api'
import { skillsApi } from '../api/skills'
import type { Skill } from '../types/skills'
import { getClipboardImageFiles } from './clipboardImages'
import { shouldUsePastedTextAttachment } from '../utils/chatPasteBehavior'
import { isMainAgentTerminal } from '../utils/terminalIdentity'

const AUTO_NOTIFICATION_PREFIX = '[AUTO-NOTIFICATION]'
const FALLBACK_CODING_AGENT_PROVIDERS = new Set(['claude-code', 'codex-cli', 'cursor-cli', 'pi-cli'])
// Providers whose chat turns never have a tmux pane (server: codingAgentUsesStructuredTransport).
const STRUCTURED_TRANSPORT_PROVIDERS = new Set(['cursor-cli'])
const FALLBACK_LIVE_INPUT_PROVIDERS = new Set(['claude-code', 'codex-cli', 'cursor-cli', 'pi-cli'])

interface ChatInputProps {
  // Handlers (callbacks only)
  onSubmit: (
    query: string,
    options?: { preferLiveInput?: boolean; sourceTabId?: string }
  ) => boolean | void | Promise<boolean | void>
  onStopStreaming: () => void
  onNewChat?: () => void
  // Optional tab scope for embedded chat panes, such as WorkflowLayout. When
  // omitted, ChatInput uses the globally active chat tab.
  tabId?: string | null
  // True while a native restored terminal is still being located. Once the
  // terminal exists, ChatArea passes false so the input does not keep showing a
  // stale "Resuming coding session" banner.
  restoredConversationPending?: boolean
  // Product surfaces keep the shared transport but hide developer/provider
  // controls and render a simple customer-facing composer.
  surfaceVariant?: 'default' | 'product'
  placeholderOverride?: string
  showNewChatAction?: boolean
  hideRuntimeStatus?: boolean
}

function isAutoNotificationMessage(msg: string): boolean {
  return msg.startsWith(AUTO_NOTIFICATION_PREFIX)
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
  tabId: scopedTabId,
  restoredConversationPending = true,
  surfaceVariant = 'default',
  placeholderOverride,
  showNewChatAction = false,
  hideRuntimeStatus = false,
  onNewChat,
}) => {
  const isProductSurface = surfaceVariant === 'product'
  // Store subscriptions
  const {
    agentMode,
    setWorkspaceMinimized,
    showWorkflowsOverview
  } = useAppStore(useShallow(state => ({
    agentMode: state.agentMode,
    setWorkspaceMinimized: state.setWorkspaceMinimized,
    showWorkflowsOverview: state.showWorkflowsOverview,
  })))
  const selectedModeCategory = useModeStore(state => state.selectedModeCategory)
  const storeActiveTabId = useChatStore(state => state.activeTabId)
  const createChatTab = useChatStore(state => state.createChatTab)
  const activeTabId = scopedTabId === null ? null : (scopedTabId ?? storeActiveTabId)
  // Use the scoped tab as the mode source when ChatInput is embedded. The global
  // mode category can lag behind WorkflowLayout, which would otherwise make a
  // workflow builder input behave like product-profile chat.
  const activeTab = useChatStore(state =>
    activeTabId ? state.chatTabs[activeTabId] : undefined
  )
  const activeTabEvents = useChatStore(state =>
    activeTab?.sessionId ? state.tabEvents[activeTab.sessionId] : undefined
  )
  // Main tmux is a first-class alternate view of this chat. Child-terminal
  // inspection is still developer-only, but opening the main pane must not
  // require a diagnostic flag.
  const mainTerminalAvailable = !!activeTab?.sessionId
  const terminalViewSelected = normalizeEventViewMode(activeTab?.viewMode) === 'terminal'
  const tabModeCategory = activeTab?.metadata?.mode
  const isWorkflowMode = tabModeCategory === 'workflow' || selectedModeCategory === 'workflow'
  const isMultiAgentMode = !isWorkflowMode && (tabModeCategory === 'multi-agent' || selectedModeCategory === 'multi-agent')
  // Detect workflow phase chat — hide extras like browser, skills, etc.
  const workflowPhaseId = useChatStore(state => {
    const tab = activeTabId ? state.chatTabs[activeTabId] : null
    if (tab?.metadata?.mode !== 'workflow' || !tab?.metadata?.phaseId) return undefined
    return isChatCompatiblePhase(tab.metadata.phaseId) ? tab.metadata.phaseId : undefined
  })
  const isWorkflowPhaseChat = !!workflowPhaseId
  const isInteractiveWorkflowBuilderChat = workflowPhaseId === 'workflow-builder'
  const workflowPhasePreset = useGlobalPresetStore(state => state.getActivePreset('workflow'))
  const activeWorkflowPresetId = useGlobalPresetStore(state => state.activePresetIds.workflow)
  // Read Builder LLM from workflow manifest (source of truth), not the global preset.
  // Subscribe to the manifest store so the provider/model badge updates without reopening the chat.
  const workflowPhaseWorkspacePath = isWorkflowPhaseChat ? workflowPhasePreset?.selectedFolder?.filepath : undefined
  const manifestBuilderLLM = useWorkflowManifestStore(state => {
    if (!workflowPhaseWorkspacePath) return null
    const wf = state.workflows.find(item => item.workspace_path === workflowPhaseWorkspacePath)
    return wf?.manifest?.capabilities?.llm_config?.builder_llm ?? null
  })
  // Hide extras (servers, skills, agent mode, etc.) in workflow mode but show in multi-agent
  const hideExtras = isWorkflowMode

  const runChatPresetId = activeTab?.metadata?.presetQueryId || activeWorkflowPresetId
  const showRunChatAction = !!(
    activeTab &&
    runChatPresetId &&
    isBlankWorkflowBuilderTab(
      activeTab,
      runChatPresetId,
      activeTab.sessionId && activeTabEvents
        ? { [activeTab.sessionId]: activeTabEvents }
        : {},
    )
  )
  const handleStartRunChat = useCallback(async () => {
    if (!runChatPresetId) return
    const newTabId = await createChatTab('Run chat', {
      mode: 'workflow',
      phaseId: 'workflow-builder',
      phaseName: 'Automation Run',
      presetQueryId: runChatPresetId,
      workshopMode: 'run',
    })
    activateTab(newTabId)
    useWorkflowStore.getState().setShowChatArea(true)
  }, [createChatTab, runChatPresetId])

  // Use selectors to subscribe only to specific values, reducing re-renders
  const setTabConfig = useChatStore(state => state.setTabConfig)
  const addToast = useChatStore(state => state.addToast)
  // Get active tab and its config. Embedded workflow panes pass tabId so this
  // remains scoped to the visible pane instead of the global active chat.
  const isOrganizationAssistant = !!activeTab?.metadata?.isOrganizationAssistant
  const agentProfileWorkspace = activeTab?.metadata?.agentProfileWorkspace
  const agentProfileId = activeTab?.metadata?.agentProfileId
  const agentProfileVersion = activeTab?.metadata?.agentProfileVersion
  // SparkQuill's own composer groups controls the opposite way from every
  // other product: attachment/commands/model on the left, mic+send on the
  // right. Scoped to its two profiles so AgentWorks and Video Studio keep
  // the default arrangement.
  const sparkQuillComposerLayout = agentProfileId === 'sparkquill' || agentProfileId === 'sparkquill-child'
  // Mic control. On a product surface it is gated per-profile
  // (agentprofiles.RuntimeCapabilities.Voice) rather than hardcoded to any one
  // product — see agentProfileCapabilities.ts. AgentWorks' own composer has no
  // agent profile, so there the gate is whether this server build carries the
  // engine at all (capabilities.voice.available, false in a CGO_ENABLED=0
  // build): the mic never appears where it would only answer 503.
  const platformVoiceAvailable = useCapabilitiesStore(state => state.capabilities?.voice?.available ?? false)
  const [profileVoiceEnabled, setProfileVoiceEnabled] = useState(false)
  const voiceCapabilityEnabled = isProductSurface ? profileVoiceEnabled : platformVoiceAvailable
  // Model switcher for product surfaces. The profile declares its runtimes
  // (product.yaml runtime.provider_options: Claude Code, Codex…); the
  // platform's model catalog supplies the models each of those offers. The
  // provider is chosen for a new chat and locked once the chat has a turn —
  // a Codex thread and a Claude Code session are separate CLI state, so the
  // other runtime would start blank — while the model can change any time.
  // A choice lands on the tab (sent as `engine` + `model_id` on every
  // profile query) and is announced for the product to persist.
  const [engineOptions, setEngineOptions] = useState<AgentProfileProviderOption[]>([])
  const [modelCatalog, setModelCatalog] = useState<ModelMetadata[]>([])
  useEffect(() => {
    if (!isProductSurface || !agentProfileId) {
      setEngineOptions([])
      return
    }
    let cancelled = false
    void loadAgentProfileProviderOptions(agentProfileId, agentProfileVersion).then((options) => {
      if (!cancelled) setEngineOptions(options)
    })
    void llmConfigService.getModelMetadata().then((r) => {
      if (!cancelled) setModelCatalog(Array.isArray(r?.models) ? r.models : [])
    }).catch(() => undefined)
    return () => { cancelled = true }
  }, [isProductSurface, agentProfileId, agentProfileVersion])
  const currentEngine = activeTab?.metadata?.agentProfileEngine
    || engineOptions.find((o) => o.default)?.id
    || engineOptions[0]?.id
    || ''
  const engineGroups = useMemo(() => {
    // The catalog carries a few pseudo-entries per coding CLI (its own name,
    // reasoning levels); only real model ids are offered.
    const pseudo = new Set(['high', 'medium', 'low'])
    return engineOptions.map((option) => {
      const provider = (option.provider ?? '').trim()
      const catalogByID = new Map(modelCatalog.filter((m) => m.provider === provider).map((m) => [m.model_id, m]))
      // An engine that curates its own Models offers exactly those, in that
      // order; otherwise every model the catalog lists for its provider.
      const models = option.models && option.models.length > 0
        ? option.models.map((id) => ({ id, label: catalogByID.get(id)?.model_name || id }))
        : modelCatalog
            .filter((m) => m.provider === provider && m.model_id !== provider && !pseudo.has(m.model_id))
            .map((m) => ({ id: m.model_id, label: m.model_name || m.model_id }))
      const own = (option.model_id ?? '').trim()
      if (own && !models.some((m) => m.id === own)) models.unshift({ id: own, label: own })
      const reasoningLevels = (option.reasoning_efforts ?? []).map((id) => ({ id, label: id.charAt(0).toUpperCase() + id.slice(1) }))
      return { option, models, reasoningLevels }
    })
  }, [engineOptions, modelCatalog])
  const currentGroup = engineGroups.find((g) => g.option.id === currentEngine)
  const currentModel = activeTab?.metadata?.agentProfileModelID || (currentGroup?.option.model_id ?? '') || currentGroup?.models[0]?.id || ''
  const defaultReasoningEffort = typeof currentGroup?.option.options?.reasoning_effort === 'string' ? currentGroup.option.options.reasoning_effort : undefined
  const currentReasoningEffort = activeTab?.metadata?.agentProfileReasoningEffort || defaultReasoningEffort || currentGroup?.reasoningLevels[0]?.id || ''
  const chatHasTurns = useMemo(() => (activeTabEvents ?? []).some((e) => e.type === 'user_message'), [activeTabEvents])
  const selectProductEngine = useCallback((engine: string, modelId: string, reasoningEffort?: string) => {
    if (!activeTabId || !agentProfileId) return
    useChatStore.getState().setTabMetadata(activeTabId, { agentProfileEngine: engine, agentProfileModelID: modelId, ...(reasoningEffort ? { agentProfileReasoningEffort: reasoningEffort } : {}) })
    window.dispatchEvent(new CustomEvent('agentworks:product-engine-selected', { detail: { profileId: agentProfileId, engine, modelId, reasoningEffort } }))
  }, [activeTabId, agentProfileId])

  // "New chat" for product surfaces, offered when the profile declares
  // runtime.capabilities.new_conversation; the product owns what happens.
  const [newConversationEnabled, setNewConversationEnabled] = useState(false)
  useEffect(() => {
    if (!isProductSurface || !agentProfileId) {
      setNewConversationEnabled(false)
      return
    }
    let cancelled = false
    void loadAgentProfileCapabilityEnabled(agentProfileId, 'new_conversation', agentProfileVersion).then((enabled) => {
      if (!cancelled) setNewConversationEnabled(enabled)
    })
    return () => { cancelled = true }
  }, [isProductSurface, agentProfileId, agentProfileVersion])
  const requestNewConversation = useCallback((engine: string) => {
    if (!agentProfileId) return
    window.dispatchEvent(new CustomEvent('agentworks:product-new-conversation', { detail: { profileId: agentProfileId, engine } }))
  }, [agentProfileId])

  useEffect(() => {
    if (!isProductSurface || !agentProfileId) {
      setProfileVoiceEnabled(false)
      return
    }
    let cancelled = false
    void loadAgentProfileCapabilityEnabled(agentProfileId, 'voice', agentProfileVersion).then((enabled) => {
      if (!cancelled) setProfileVoiceEnabled(enabled)
    })
    return () => { cancelled = true }
  }, [isProductSurface, agentProfileId, agentProfileVersion])

  // Product-owned chats display the provider/model declared by their profile,
  // not the stale model config a durable tab may have inherited before the
  // profile contract existed. The running session still wins below so a real
  // server-selected fallback remains visible while it is active.
  const [agentProfileRuntime, setAgentProfileRuntime] = useState<{ provider: string; model_id: string; transport?: string } | null>(null)
  useEffect(() => {
    if (!isProductSurface || !agentProfileId) {
      setAgentProfileRuntime(null)
      return
    }
    let cancelled = false
    void loadAgentProfileRuntime(agentProfileId, agentProfileVersion).then((runtime) => {
      if (!cancelled) setAgentProfileRuntime(runtime)
    })
    return () => { cancelled = true }
  }, [agentProfileId, agentProfileVersion, isProductSurface])

  // Memoize tabConfig to prevent unnecessary re-renders
  const tabConfig = useMemo(() => activeTab?.config, [activeTab?.config])

  // CRITICAL: Always use tab's status - never fall back to global to prevent mixing
  // If no active tab, this is an error condition (tabs should always exist)
  const isStreaming = activeTab?.isStreaming ?? false
  // Whether a turn is in flight at all. isStreaming alone is deliberately false
  // while only background agents run (so the composer stays usable), and it
  // follows the server's can_steer, which falls through to a volatile tmux
  // busy-content heuristic. Mounting the cancel control on it made the control
  // flicker several times a second during a run. Gate the control's PRESENCE on
  // this; keep isStreaming for whether the composer accepts input.
  const isTurnInFlight = isStreaming || (activeTab?.hasRunningBgAgents ?? false)
  const canSteer = activeTab?.canSteer ?? false
  const tabSessionId = activeTab?.sessionId ?? null
  const isViewOnly = activeTab?.metadata?.isViewOnly ?? false
  const activeSession = useChatStore(state => {
    if (!tabSessionId) return undefined
    return state.activeSessionsCache.find(session => session.session_id === tabSessionId)
  })
  const activeSessionRuntime = hasActiveSessionWork(activeSession) ? activeSession?.runtime : undefined
  const {
    providerManifest,
    providerManifestLoaded,
    loadProviderManifest,
    primaryConfig,
    llmConfigLocked,
    publishedLLMs,
  } = useLLMStore(useShallow(state => ({
    providerManifest: state.providerManifest,
    providerManifestLoaded: state.providerManifestLoaded,
    loadProviderManifest: state.loadProviderManifest,
    primaryConfig: state.primaryConfig,
    llmConfigLocked: state.llmConfigLocked,
    publishedLLMs: state.savedLLMs,
  })))
  
  // Note: activeTab may be undefined during initial render before tabs are created
  // This is expected and will resolve once the tab store initializes
  
  // Always use tab-specific config (ChatInput is only in multi-agent mode)
  // Memoize to prevent unnecessary re-renders when other config values change
  const chatFileContext = useMemo(() => tabConfig?.fileContext || [], [tabConfig?.fileContext])
  const chatPastedAttachments = useMemo(() => tabConfig?.pastedAttachments || [], [tabConfig?.pastedAttachments])
  const restoredConversationPath = tabConfig?.restoredConversationPath?.trim() || ''

  // Get input text from tab config (source of truth for persistence)
  const storedInputText = tabConfig?.inputText || ''

  // Local state for immediate UI updates (prevents Zustand updates on every keystroke)
  const [localInputText, setLocalInputText] = useState(storedInputText)

  // Voice dictation (MicButton) delivers the whole utterance ONCE, on stop —
  // the composer appends it to whatever was typed and leaves sending to the
  // user (deliberately no auto-send in AgentWorks, unlike SparkQuill). While
  // recording, Enter is routed to the mic (handleKeyDown) so it stops the
  // dictation instead of submitting a half-typed draft. The live banner
  // renders into micBannerHost, above the composer.
  const micRef = useRef<MicButtonHandle>(null)
  const micStateRef = useRef<MicState>('idle')
  const [micBannerHost, setMicBannerHost] = useState<HTMLDivElement | null>(null)
  const handleMicStateChange = useCallback((s: MicState) => { micStateRef.current = s }, [])
  const handleVoiceText = useCallback((text: string) => {
    const base = localInputText
    const joined = base && !base.endsWith(' ') && !base.endsWith('\n') ? `${base} ${text}` : `${base}${text}`
    setLocalInputText(joined)
    if (activeTabId) setTabConfig(activeTabId, { inputText: joined })
    // So Enter immediately sends — without this the composer stays unfocused
    // after dictation and Enter does nothing.
    textareaRef.current?.focus()
  }, [localInputText, activeTabId, setTabConfig])

  const inputText = localInputText
  const inputOwnerTabIdRef = useRef(activeTabId)

  // Debounce ref for syncing to store
  const syncToStoreTimeoutRef = useRef<NodeJS.Timeout | null>(null)

  // Sync local state FROM store when store changes externally (preset sync, etc.)
  useLayoutEffect(() => {
    // A workflow/session switch can reuse this mounted input. Never let a draft
    // from the previous tab become the next tab's submitted value, and never let
    // its pending debounce suppress the new tab's store -> local sync.
    if (inputOwnerTabIdRef.current !== activeTabId) {
      const previousTabId = inputOwnerTabIdRef.current
      if (syncToStoreTimeoutRef.current) {
        clearTimeout(syncToStoreTimeoutRef.current)
        syncToStoreTimeoutRef.current = null
        if (previousTabId) {
          useChatStore.getState().setTabConfig(previousTabId, { inputText: localInputText })
        }
      }
      inputOwnerTabIdRef.current = activeTabId
      setLocalInputText(storedInputText)
      return
    }
    // Only sync if store value differs and we're not in the middle of typing
    if (storedInputText !== localInputText && !syncToStoreTimeoutRef.current) {
      setLocalInputText(storedInputText)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeTabId, storedInputText]) // Intentionally exclude localInputText to avoid loops

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
  const multiAgentEffectiveLLMConfig = useMemo(() => {
    if (!isMultiAgentMode) return null

    const runtimeProvider = activeSessionRuntime?.provider?.trim()
    const runtimeModel = activeSessionRuntime?.model_id?.trim()
    if (runtimeProvider && runtimeModel) {
      return { provider: runtimeProvider as LLMProvider, model_id: runtimeModel }
    }

    if (agentProfileRuntime?.provider && agentProfileRuntime.model_id) {
      return {
        provider: agentProfileRuntime.provider as LLMProvider,
        model_id: agentProfileRuntime.model_id,
      }
    }

    if (primaryConfig?.provider && primaryConfig.model_id) {
      return { provider: primaryConfig.provider, model_id: primaryConfig.model_id }
    }
    return null
  }, [
    activeSessionRuntime?.model_id,
    activeSessionRuntime?.provider,
    agentProfileRuntime?.model_id,
    agentProfileRuntime?.provider,
    isMultiAgentMode,
    primaryConfig,
  ])

  const effectiveProviderForSteer = useMemo(() => {
    // The saved/preset provider is only a preference: under a locked
    // deployment the turn runs on the published default (same rule as the
    // status chip and the server's resolveLockedLLM), so every label derived
    // from this value must reflect the lock too.
    const saved = (() => {
      if (isWorkflowPhaseChat) {
        return manifestBuilderLLM?.provider
          || workflowPhasePreset?.llmConfig?.builder_llm?.provider
          || workflowPhasePreset?.llmConfig?.provider
          || tabConfig?.llmConfig?.provider
          || null
      }
      if (multiAgentEffectiveLLMConfig?.provider) return multiAgentEffectiveLLMConfig.provider
      return tabConfig?.llmConfig?.provider ?? null
    })()
    return effectiveProviderUnderLock(saved, llmConfigLocked, publishedLLMs)
  }, [
    isWorkflowPhaseChat,
    llmConfigLocked,
    manifestBuilderLLM?.provider,
    multiAgentEffectiveLLMConfig?.provider,
    publishedLLMs,
    tabConfig?.llmConfig?.provider,
    workflowPhasePreset?.llmConfig?.builder_llm?.provider,
    workflowPhasePreset?.llmConfig?.provider])
  // Cursor normally runs on structured JSON, while Workflow Builder uses a
  // retained tmux pane for terminal inspection and live steering. Prefer the
  // backend-reported transport once a session exists; this fallback keeps the
  // control correct before the first runtime event arrives.
  const activeReportedTransport = (activeSession?.runtime?.transport || '').trim().toLowerCase()
  const profileReportedTransport = (agentProfileRuntime?.transport || '').trim().toLowerCase()
  const reportedTransport = ['structured', 'tmux'].includes(activeReportedTransport)
    ? activeReportedTransport
    : ['structured', 'tmux'].includes(profileReportedTransport)
      ? profileReportedTransport
      : ''
  const currentChatUsesStructuredTransport = chatUsesStructuredTransport({
    isInteractiveWorkflowBuilder: isInteractiveWorkflowBuilderChat,
    reportedTransport,
    providerUsesStructuredTransport: STRUCTURED_TRANSPORT_PROVIDERS.has((effectiveProviderForSteer || '').trim().toLowerCase()),
  })
  const liveTerminalOffered = mainTerminalAvailable && !currentChatUsesStructuredTransport
  useEffect(() => {
    if (!providerManifestLoaded) {
      void loadProviderManifest()
    }
  }, [loadProviderManifest, providerManifestLoaded])

  const selectedProviderManifestEntry = useMemo(
    () => providerManifest.find(provider => provider.id === effectiveProviderForSteer),
    [effectiveProviderForSteer, providerManifest]
  )
  // All coding-agent runtimes, driven by the backend provider contract.
  const isCLIProvider = useMemo(
    () => selectedProviderManifestEntry?.integration_kind === 'coding_agent'
      || FALLBACK_CODING_AGENT_PROVIDERS.has(effectiveProviderForSteer || ''),
    [effectiveProviderForSteer, selectedProviderManifestEntry?.integration_kind]
  )
  // Only providers whose backend contract supports live input should bypass
  // the normal queued-message path. This lets new coding CLIs work without
  // adding another frontend provider list.
  const supportsLiveCodingAgentInput = useMemo(
    () => !currentChatUsesStructuredTransport && (
      selectedProviderManifestEntry?.coding_agent?.supports_live_input === true
      || FALLBACK_LIVE_INPUT_PROVIDERS.has(effectiveProviderForSteer || '')
    ),
    [currentChatUsesStructuredTransport, effectiveProviderForSteer, selectedProviderManifestEntry]
  )
  const canShowSteer = useMemo(() => canSteer && !isCLIProvider, [canSteer, isCLIProvider])
  const browserMode = useMemo(() => tabConfig?.browserMode ?? 'auto', [tabConfig?.browserMode])
  const cdpPort = useMemo(() => tabConfig?.cdpPort ?? 9222, [tabConfig?.cdpPort])
  const workspaceActiveFolder = useWorkspaceStore(state => state.activeFolder)
  const [cdpConnected, setCdpConnected] = useState<boolean | null>(null)
  const [cdpError, setCdpError] = useState<string | null>(null)
  const [cdpChecking, setCdpChecking] = useState(false)
  const [showCdpPopup, setShowCdpPopup] = useState(false)
  const [isUploadingFiles, setIsUploadingFiles] = useState(false)
  const [isDraggingFiles, setIsDraggingFiles] = useState(false)
  const isCdpDisconnected = browserMode === 'cdp' && cdpConnected === false

  // File context operations (always update tab config)
  const removeFileFromContext = useCallback((path: string) => {
    if (activeTabId && activeTab) {
      const newFileContext = chatFileContext.filter(f => f.path !== path)
      const configUpdate = activeTab.config?.restoredConversationPath === path
        ? {
            fileContext: newFileContext,
            restoredConversationPath: undefined,
            restoredConversationSummary: undefined,
            restoredConversationTitle: undefined,
            restoredConversationWorkshopModeLabel: undefined,
            restoredConversationRuntimeLabel: undefined,
            restoredConversationNativeResume: undefined,
          }
        : { fileContext: newFileContext }
      setTabConfig(activeTabId, configUpdate)
    }
  }, [activeTabId, activeTab, chatFileContext, setTabConfig])
  
  const clearFileContext = useCallback(() => {
    if (activeTabId) {
      setTabConfig(activeTabId, {
        fileContext: [],
        restoredConversationPath: undefined,
        restoredConversationSummary: undefined,
        restoredConversationTitle: undefined,
        restoredConversationWorkshopModeLabel: undefined,
        restoredConversationRuntimeLabel: undefined,
        restoredConversationNativeResume: undefined,
      })
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
      const existingContext = useChatStore.getState().getTabConfig(activeTabId)?.fileContext || chatFileContext
      if (existingContext.some((item: { path: string }) => item.path === file.path)) return
      setTabConfig(activeTabId, { fileContext: [...existingContext, file] })
    }
  }, [activeTabId, activeTab, chatFileContext, setTabConfig])
  
  const {
    toolList: mcpToolList,
    setChatSelectedServers
  } = useMCPStore(useShallow(state => ({
    toolList: state.toolList,
    setChatSelectedServers: state.setChatSelectedServers,
  })))

  const availableServers = useMemo(
    () => [...new Set(mcpToolList.filter(t => t.status === 'ok').map(t => t.server).filter((s): s is string => typeof s === 'string'))],
    [mcpToolList]
  )

  const setBrowserMode = useCallback((mode: 'none' | 'auto' | 'headless' | 'cdp') => {
    if (!activeTabId) return

    if (mode === 'auto' || mode === 'headless' || mode === 'cdp') {
      setTabConfig(activeTabId, {
        browserMode: mode,
        enableBrowserAccess: true,
        useCdp: mode === 'cdp',
      })
      if (!showCdpPopup) setWorkspaceMinimized(false)
    } else {
      setTabConfig(activeTabId, {
        browserMode: 'none',
        enableBrowserAccess: false,
        useCdp: false,
      })
    }
  }, [activeTabId, setTabConfig, setWorkspaceMinimized, showCdpPopup])

  const setCdpPort = useCallback((port: number) => {
    if (activeTabId) {
      setTabConfig(activeTabId, { cdpPort: port })
    }
  }, [activeTabId, setTabConfig])

  const checkCdpConnection = useCallback(async (port: number) => {
    setCdpChecking(true)
    setCdpConnected(null)
    setCdpError(null)
    try {
      const result = await agentApi.checkCdpPort(port)
      setCdpConnected(result.connected)
      setCdpError(result.connected ? null : result.error || null)
    } catch {
      setCdpConnected(false)
      setCdpError('Unable to check the CDP port.')
    } finally {
      setCdpChecking(false)
    }
  }, [])

  // Auto-check CDP connection when automatic/CDP mode is active or port changes.
  // In automatic mode this is informational: an unavailable CDP browser falls
  // back to headless at runtime instead of blocking the chat.
  useEffect(() => {
    if (browserMode !== 'auto' && browserMode !== 'cdp') {
      setCdpConnected(null)
      setCdpError(null)
      return
    }
    const timer = setTimeout(() => {
      checkCdpConnection(cdpPort)
    }, 500)
    return () => clearTimeout(timer)
  }, [browserMode, cdpPort, checkCdpConnection])

  useEffect(() => {
    const handleChatToolCommand = (event: Event) => {
      const command = chatToolCommandFromEvent(event)
      if (command !== 'browser' || hideExtras) return

      if (!activeTabId) {
        addToast('No active chat tab yet.', 'info')
        return
      }

      if (browserMode === 'none') {
        setBrowserMode('auto')
      }
      setShowCdpPopup(true)
      setWorkspaceMinimized(true)
    }

    window.addEventListener(CHAT_TOOL_COMMAND_EVENT, handleChatToolCommand)
    return () => window.removeEventListener(CHAT_TOOL_COMMAND_EVENT, handleChatToolCommand)
  }, [activeTabId, addToast, browserMode, hideExtras, setBrowserMode, setWorkspaceMinimized])

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

  
  // State for summarization
  const [isSummarizing, setIsSummarizing] = useState(false)

  // State for steer message loading
  const [steeringIndex, setSteeringIndex] = useState<number | null>(null)
  const [liveMessageDelivery, setLiveMessageDelivery] = useState<LiveMessageDelivery | null>(null)
  const liveMessageDeliveryTimerRef = useRef<number | null>(null)

  const scheduleLiveMessageDeliveryClear = useCallback(() => {
    if (liveMessageDeliveryTimerRef.current !== null) {
      window.clearTimeout(liveMessageDeliveryTimerRef.current)
    }
    liveMessageDeliveryTimerRef.current = window.setTimeout(() => {
      setLiveMessageDelivery(null)
      liveMessageDeliveryTimerRef.current = null
    }, 6000)
  }, [])

  useEffect(() => {
    return () => {
      if (liveMessageDeliveryTimerRef.current !== null) {
        window.clearTimeout(liveMessageDeliveryTimerRef.current)
      }
    }
  }, [])

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
    window.setTimeout(() => {
      const chatStore = useChatStore.getState()
      const tab = chatStore.chatTabs[activeTabId]
      if (!tab?.isStreaming) return
      chatStore.getActiveSessions(true).catch(error => {
        console.warn('[ChatInput] Failed to refresh active sessions after queueing message', error)
      })
    }, 0)
  }, [activeTabId, setTabConfig])

  const handleSteerQueuedMessage = useCallback(async (index: number, msg: string) => {
    if (!canShowSteer || !tabSessionId) return

    setSteeringIndex(index)
    try {
      const response = await agentApi.sendLiveInput(tabSessionId, msg)
      setLiveMessageDelivery({
        status: response.delivery_status || 'queued_for_injection',
        message: msg,
        provider: response.provider || effectiveProviderForSteer || undefined,
      })
      scheduleLiveMessageDeliveryClear()
      removeQueuedMessageAtIndex(index)
    } catch (err) {
      const status = getHttpErrorStatus(err)
      if (isLiveCodingSessionGoneStatus(status)) {
        if (activeTabId) {
          const chatStore = useChatStore.getState()
          chatStore.setTabCanSteer(activeTabId, false)
          chatStore.setTabStreaming(activeTabId, false)
        }
        setLiveMessageDelivery({
          status: 'queued_locally',
          message: msg,
          provider: effectiveProviderForSteer || undefined,
        })
        scheduleLiveMessageDeliveryClear()
        addToast('The live agent turn has ended. The queued message will send as the next turn.', 'info')
      } else if (isLikelyBackendUnavailableError(err)) {
        setLiveMessageDelivery({
          status: 'failed',
          message: msg,
          provider: effectiveProviderForSteer || undefined,
          detail: 'Backend unavailable',
        })
        scheduleLiveMessageDeliveryClear()
        addToast('Backend is unavailable. The queued message is still saved.', 'error')
      } else {
        setLiveMessageDelivery({
          status: 'failed',
          message: msg,
          provider: effectiveProviderForSteer || undefined,
          detail: err instanceof Error ? err.message : 'Unknown error',
        })
        scheduleLiveMessageDeliveryClear()
        addToast('Failed to steer message: ' + (err instanceof Error ? err.message : 'Unknown error'), 'error')
      }
    } finally {
      setSteeringIndex(null)
    }
  }, [activeTabId, addToast, canShowSteer, effectiveProviderForSteer, removeQueuedMessageAtIndex, scheduleLiveMessageDeliveryClear, tabSessionId])

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
        newServers = [...serversWithoutNoServers, server]
      }

      setTabConfig(activeTabId, { selectedServers: newServers })
      // Sync to chat-specific MCP store so new chat tabs inherit this selection
      setChatSelectedServers(newServers)
    }
  }, [activeTabId, manualSelectedServers, setTabConfig, setChatSelectedServers])
  
  const onSelectAllServers = useCallback(() => {
    if (activeTabId) {
      const allServers = availableServers
      setTabConfig(activeTabId, { selectedServers: allServers })
      // Sync to chat-specific MCP store so new chat tabs inherit this selection
      setChatSelectedServers(allServers)
    }
  }, [activeTabId, availableServers, setTabConfig, setChatSelectedServers])

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

  const {
    availableLLMs,
    getCurrentLLMOption,
  } = useLLMStore(useShallow(state => ({
    availableLLMs: state.availableLLMs,
    getCurrentLLMOption: state.getCurrentLLMOption,
  })))

  const scrollToFile = useWorkspaceStore(state => state.scrollToFile)
  const { showSkillImport, showMCPDetails, showMCPConfig, showModels, openDialog, closeDialog } = useCommandDialogStore(useShallow(state => ({
    showSkillImport: state.showSkillImport,
    showMCPDetails: state.showMCPDetails,
    showMCPConfig: state.showMCPConfig,
    showModels: state.showModels,
    openDialog: state.openDialog,
    closeDialog: state.closeDialog,
  })))

  // Computed values - get LLM option from tab config
  const primaryLLM = useMemo(() => {
    if (isWorkflowPhaseChat) {
      // Show the Builder model from workflow manifest (source of truth for backend).
      const manifestConfig = useWorkflowManifestStore.getState().getWorkflowByPath(workflowPhaseWorkspacePath || '')?.manifest?.capabilities?.llm_config
      const builderLLM = manifestBuilderLLM ?? (() => {
        if (manifestConfig?.mode !== 'provider_profile' || !manifestConfig.provider) return null
        return providerManifest.find(provider => provider.id === manifestConfig.provider)?.default_tier_models?.builder ?? null
      })()
      if (builderLLM?.provider && builderLLM?.model_id) {
        const found = availableLLMs.find(llm =>
          llm.provider === builderLLM.provider && llm.model === builderLLM.model_id
        )
        if (found) return found
        return {
          provider: builderLLM.provider,
          model: builderLLM.model_id,
          label: `${builderLLM.provider} - ${builderLLM.model_id}`,
          description: 'Builder LLM'
        }
      }
      // Fallback to preset
      const preset = workflowPhasePreset
      const presetBuilderLLM = preset?.llmConfig?.builder_llm ?? (() => {
        const profileProvider = preset?.llmConfig?.mode === 'provider_profile' ? preset.llmConfig.provider : undefined
        return providerManifest.find(provider => provider.id === profileProvider)?.default_tier_models?.builder ?? null
      })()
      if (presetBuilderLLM?.provider && presetBuilderLLM?.model_id) {
        const found = availableLLMs.find(llm =>
          llm.provider === presetBuilderLLM.provider && llm.model === presetBuilderLLM.model_id
        )
        if (found) return found
        return {
          provider: presetBuilderLLM.provider,
          model: presetBuilderLLM.model_id,
          label: `${presetBuilderLLM.provider} - ${presetBuilderLLM.model_id}`,
          description: 'Builder LLM'
        }
      }
    }

    const resolveOption = (provider: string, model: string, description: string): LLMOption => {
      const found = availableLLMs.find(llm =>
        llm.provider === provider && llm.model === model
      )
      if (found) return found
      return {
        provider: provider as LLMProvider,
        model,
        label: `${provider} - ${model}`,
        description
      }
    }

    if (isMultiAgentMode && multiAgentEffectiveLLMConfig?.provider && multiAgentEffectiveLLMConfig.model_id) {
      return resolveOption(
        multiAgentEffectiveLLMConfig.provider,
        multiAgentEffectiveLLMConfig.model_id,
        activeSessionRuntime?.provider ? 'Running multi-agent model' : 'Multi-agent main model'
      )
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
    isMultiAgentMode,
    multiAgentEffectiveLLMConfig?.model_id,
    multiAgentEffectiveLLMConfig?.provider,
    activeSessionRuntime?.provider,
    manifestBuilderLLM,
    workflowPhasePreset,
    providerManifest,
    workflowPhaseWorkspacePath,
  ])

  // The main agent runs in a tmux pane only for coding-agent CLI providers
  // (claude-code, codex-cli, cursor-cli, pi-cli, ...). This drives whether
  // the "keyboard → terminal" toggle is offered. Derived from primaryLLM
  // (which always resolves) rather than effectiveProviderForSteer (which is
  // null until a model is explicitly chosen on the tab).
  const mainAgentIsTmuxCLI = useMemo(() => {
    const provider = primaryLLM?.provider || effectiveProviderForSteer || ''
    if (!provider) return false
    const entry = providerManifest.find(p => p.id === provider)
    return (entry?.integration_kind === 'coding_agent' && !entry.deprecated) || FALLBACK_CODING_AGENT_PROVIDERS.has(provider)
  }, [primaryLLM?.provider, effectiveProviderForSteer, providerManifest])

  // The terminal snapshot already carries the coding CLI's structured status
  // line. Surface it next to the model in the composer so a user can see plan
  // usage (for example "5h 11% · 7d 22%") without opening the terminal.
  const { data: chatInputTerminalStatus } = useQuery({
    queryKey: ['chat-input-statusline', tabSessionId],
    queryFn: () => agentApi.listTerminals(tabSessionId!, 'none'),
    enabled: !hideRuntimeStatus && !isProductSurface && mainAgentIsTmuxCLI && !!tabSessionId,
    staleTime: 1_000,
    retry: false,
    refetchInterval: (query) => {
      const mainTerminal = query.state.data?.terminals?.find(isMainAgentTerminal)
      return isTurnInFlight || mainTerminal?.active ? 3_000 : false
    },
  })
  const chatInputStatusExtras = useMemo(() => {
    const mainTerminal: TerminalSnapshot | undefined = chatInputTerminalStatus?.terminals?.find(isMainAgentTerminal)
    const rawExtras = mainTerminal?.status?.status_meta?.status_extras
    return Array.isArray(rawExtras)
      ? rawExtras.filter((value): value is string => typeof value === 'string' && value.trim() !== '')
      : []
  }, [chatInputTerminalStatus?.terminals])
  const chatInputStatusLine = chatInputStatusExtras.join(' · ')

  // Use the exact same authoritative activity classification as the global
  // monitor. A retained tmux session is intentionally "idle" there; merely
  // keeping its process alive must never make this spinner claim the agent is
  // still working.
  const mainAgentRuntimeStatus = useMemo(() => {
    // Under a locked deployment the next turn runs on the published default
    // whatever this workflow saved or a past session reported, so that is
    // what the composer shows (server: resolveLockedLLM).
    const effective = effectiveLLMUnderLock(
      {
        provider: activeSession?.runtime?.provider?.trim() || primaryLLM?.provider?.trim() || '',
        model_id: activeSession?.runtime?.model_id?.trim() || primaryLLM?.model?.trim() || '',
      },
      llmConfigLocked,
      publishedLLMs,
    )
    const provider = effective?.provider || ''
    const model = effective?.model_id || ''
    if (!provider) return null

    // A durable foreground completion settles the turn even when the periodic
    // activity snapshot has not refreshed yet. This is the same boundary the
    // global monitor eventually observes, applied immediately to the open chat.
    const tone = activeTab?.isCompleted && !isTurnInFlight
      ? 'idle'
      : activeSession ? statusTone(activeSession) : 'idle'
    const waiting = tone === 'needs-input'
    const running = tone === 'running' || tone === 'background'
    return {
      // The composer is user-facing: transport names such as "claude-code"
      // do not add useful context here. Keep the provider only as a fallback
      // when the runtime has not reported its model yet.
      label: model || provider,
      state: waiting ? 'waiting' as const : running ? 'running' as const : 'ready' as const,
      activityLabel: activeSession ? headerStatusLabel(activeSession) : 'idle',
    }
  }, [
    activeSession,
    activeSession?.runtime?.model_id,
    activeSession?.runtime?.provider,
    activeSession?.runtime_state?.phase,
    activeSession?.runtime_state?.waiting_for_user,
    activeSession?.status,
    activeSession?.runtime_state?.background_live,
    activeSession?.has_running_background_agents,
    activeSession?.running_background_agent_count,
    activeSession?.needs_user_input,
    activeTab?.isCompleted,
    isTurnInFlight,
    primaryLLM?.model,
    primaryLLM?.provider,
    llmConfigLocked,
    publishedLLMs,
  ])

  // mainAgentRuntimeStatus reads activeSession from activeSessionsCache, a
  // 30s-TTL cache that nothing polls on a timer inside the workflow-builder
  // view (only the main chat view's GlobalActivityMonitor does, every 5s).
  // The tab strip's own busy dot reads chatTabs[tabId].isStreaming /
  // .hasRunningBgAgents directly -- live, event-driven -- so left alone this
  // composer chip can visibly lag it: still showing "running" up to 30s
  // after a background agent/step actually finished (reported live: the
  // composer's spinner kept going after the tab strip had already gone
  // idle). Force a refresh right when the live signal transitions instead
  // of waiting on the cache's own TTL.
  const liveTabBusy = (activeTab?.isStreaming ?? false) || (activeTab?.hasRunningBgAgents ?? false)
  const lastLiveTabBusyRef = useRef(liveTabBusy)
  useEffect(() => {
    if (lastLiveTabBusyRef.current === liveTabBusy) return
    lastLiveTabBusyRef.current = liveTabBusy
    useChatStore.getState().getActiveSessions(true).catch(error => {
      console.warn('[ChatInput] Failed to refresh active sessions after live busy-state change', error)
    })
  }, [liveTabBusy])

  // Preset folder selection
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const fileUploadInputRef = useRef<HTMLInputElement>(null)

  const uploadFilesToChatRef = useRef<(files: File[]) => Promise<void>>(async () => {})
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

  const restoredResumeTitle = useMemo(() => {
    if (tabConfig?.restoredConversationTitle?.trim()) return tabConfig.restoredConversationTitle.trim()
    return restoredConversationPath.split('/').pop() || 'Previous chat'
  }, [restoredConversationPath, tabConfig?.restoredConversationTitle])

  const restoredResumeRuntimeLabel = tabConfig?.restoredConversationRuntimeLabel?.trim() || undefined

  const restoredResumeWorkshopModeLabel = tabConfig?.restoredConversationWorkshopModeLabel?.trim() || undefined

  const restoredResumeUsesNative = tabConfig?.restoredConversationNativeResume === true
  const showRestoredConversationIndicator =
    !!restoredConversationPath && (!restoredResumeUsesNative || restoredConversationPending)

  const clearRestoredConversation = useCallback(() => {
    if (!activeTabId) return
    setTabConfig(activeTabId, {
      fileContext: restoredConversationPath
        ? chatFileContext.filter(item => item.path !== restoredConversationPath)
        : chatFileContext,
      restoredConversationPath: undefined,
      restoredConversationSummary: undefined,
      restoredConversationTitle: undefined,
      restoredConversationWorkshopModeLabel: undefined,
      restoredConversationRuntimeLabel: undefined,
      restoredConversationNativeResume: undefined,
    })
    addToast('Resume cleared', 'info')
    setTimeout(() => textareaRef.current?.focus(), 0)
  }, [activeTabId, addToast, chatFileContext, restoredConversationPath, setTabConfig])

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

  // Auto-resize textarea based on content
  const adjustTextareaHeight = useCallback(() => {
    if (textareaRef.current) {
      const textarea = textareaRef.current
      // Product surfaces start one line shorter (36px vs 40px) to stay compact
      // when empty, but still grow with real wrapped content rather than
      // scrolling it off-screen horizontally.
      const floor = isProductSurface ? 36 : 40
      // Fast path: the box is already at its floor and the content fits (no
      // vertical overflow). There is nothing to grow or shrink, so DON'T flip
      // height to 'auto' — that forced reflow is what jitters the flex column and
      // fires the terminal's ResizeObserver on every keystroke, even a single
      // character that needs no growth at all.
      if (textarea.style.height === `${floor}px` && textarea.scrollHeight <= textarea.clientHeight) {
        return
      }
      // Reset height to auto to get correct scrollHeight
      textarea.style.height = 'auto'
      // Calculate new height (min = floor, max 100px)
      // scrollHeight includes padding, so we get the exact content height
      const newHeight = Math.min(Math.max(textarea.scrollHeight, floor), 100)
      const newHeightPx = `${newHeight}px`
      // Only write when it actually changes so an unchanged height never leaves a
      // pending style mutation / extra layout pass.
      if (textarea.style.height !== newHeightPx) {
        textarea.style.height = newHeightPx
      }
    }
  }, [isProductSurface])

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
        textareaRef.current.style.height = isProductSurface ? '36px' : '40px'
      } else {
        adjustTextareaHeight()
      }
    }
  }, [inputText, adjustTextareaHeight, isProductSurface])
  
  // Set initial height on mount
  useEffect(() => {
    if (textareaRef.current) {
      textareaRef.current.style.height = isProductSurface ? '36px' : '40px'
    }
  }, [isProductSurface])


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
  const latestQueryToSubmitRef = useRef(queryToSubmit)
  latestQueryToSubmitRef.current = queryToSubmit

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

  // tmux-transport coding CLIs keep a persistent session, so input should go to
  // the backend immediately rather than the local queue. Workflow chat tabs also
  // go straight to the backend when they already have a session: the backend owns
  // whether to inject into an attached CLI, resume native history, or start a new
  // turn. The local queue is only useful for non-workflow API chat providers.
  const routeLiveInputToCLI = useMemo(
    () => shouldRouteChatInputToLiveTransport({
      hasSession: Boolean(tabSessionId),
      isCodingAgentProvider: mainAgentIsTmuxCLI,
      isWorkflowMode,
      usesStructuredTransport: currentChatUsesStructuredTransport,
    }),
    [currentChatUsesStructuredTransport, isWorkflowMode, mainAgentIsTmuxCLI, tabSessionId]
  )

  // Deliver queued HUMAN messages into a turn that is already running, using
  // the same two routes typed text uses. Messages reach the queue from outside
  // this input box — "ask in chat" on a pending decision is the case that
  // surfaced this — and used to sit there until the turn ended. On a coding CLI
  // there was not even a steer button to force them through, since canShowSteer
  // is false for those providers.
  //
  // Auto-notifications are left queued on purpose; they wait for idle.
  const liveDeliveryInFlightRef = useRef(false)
  useEffect(() => {
    if (liveDeliveryInFlightRef.current) return
    const { human } = splitQueuedMessages(queuedMessages, AUTO_NOTIFICATION_PREFIX)
    if (human.length === 0) return

    const route = routeForQueuedMessage({
      isStreaming,
      hasSession: Boolean(tabSessionId),
      // This is capability-derived and false for structured workflow steps.
      // Interactive Claude, Codex, Cursor, Pi, and future coding CLIs all use
      // the same backend-owned native live-input route.
      canUseLiveQuery: routeLiveInputToCLI,
      canSteer,
    })
    if (route === 'wait') return

    liveDeliveryInFlightRef.current = true
    try {
      if (route === 'steer') {
        // One at a time: handleSteerQueuedMessage reports per-message delivery
        // status and removes the message itself. The effect re-runs for the next.
        const index = queuedMessages.findIndex(message => message === human[0])
        if (index >= 0) void handleSteerQueuedMessage(index, human[0])
        return
      }
      // live-query: same single-entry path as typing into a CLI/workflow chat.
      const remaining = queuedMessages.filter(message => message.startsWith(AUTO_NOTIFICATION_PREFIX))
      if (activeTabId) setTabConfig(activeTabId, { queuedMessages: remaining })
      onSubmit(human.map(message => message.trim()).join('\n\n'), { preferLiveInput: true })
    } finally {
      // Released on the next tick so a single state update cannot re-enter,
      // while a genuinely new queued message still gets picked up.
      window.setTimeout(() => { liveDeliveryInFlightRef.current = false }, 0)
    }
  }, [
    activeTabId,
    canSteer,
    handleSteerQueuedMessage,
    isStreaming,
    onSubmit,
    queuedMessages,
    routeLiveInputToCLI,
    setTabConfig,
    tabSessionId,
  ])

  // Ref for debounced file removal check
  const fileRemovalTimeoutRef = useRef<NodeJS.Timeout | null>(null)

  // Guard: prevent form submit from firing when Stop button click causes a button swap
  // (React re-renders Stop→Send mid-click, causing the browser to dispatch submit on the new button)

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

  // Route a control/navigation key to the coding-agent tmux pane. Terminal
  // process liveness is authoritative on the backend: an interactive CLI can
  // remain usable after its current agent turn has stopped streaming.
  const escSentinelRef = useRef(false)
  const sendLiveCodingAgentControlKey = useCallback(async (key: string, options?: { showToast?: boolean }): Promise<boolean> => {
    if (!supportsLiveCodingAgentInput || !tabSessionId) return false
    if (key === 'Escape') {
      if (escSentinelRef.current) return true // debounce rapid double-presses
      escSentinelRef.current = true
      setTimeout(() => { escSentinelRef.current = false }, 250)
    }
    try {
      await agentApi.sendControlKey(tabSessionId, key)
      if (options?.showToast ?? key === 'Escape') {
        addToast(`Sent ${key} to ${formatLiveInputProviderLabel(effectiveProviderForSteer, primaryLLM?.model)} — Stop button ends the session`, 'info')
      }
      return true
    } catch (err) {
      const status = getHttpErrorStatus(err)
      if (isLiveCodingSessionGoneStatus(status)) {
        if (activeTabId) {
          const chatStore = useChatStore.getState()
          chatStore.setTabCanSteer(activeTabId, false)
        }
      }
      return false
    }
  }, [activeTabId, addToast, effectiveProviderForSteer, primaryLLM?.model, supportsLiveCodingAgentInput, tabSessionId])

  const ensureMultiAgentTabReady = useCallback(async (): Promise<boolean> => {
    if (!isMultiAgentMode || showWorkflowsOverview) return false

    const chatStore = useChatStore.getState()
    const currentActiveTab = chatStore.activeTabId ? chatStore.chatTabs[chatStore.activeTabId] : null
    if (
      currentActiveTab?.metadata?.mode === 'multi-agent' &&
      !!currentActiveTab.metadata?.agentProfileId &&
      currentActiveTab.metadata?.isOrganizationAssistant !== true
    ) {
      return true
    }
    return false
  }, [isMultiAgentMode, showWorkflowsOverview])

  // Keep genuinely large pasted content out of the textarea and insert a stable
  // marker the message can refer to. Ordinary text remains a normal inline paste.
  const handlePaste = useCallback((e: React.ClipboardEvent<HTMLTextAreaElement>) => {
    const pastedImageFiles = getClipboardImageFiles(e.clipboardData)
    if (pastedImageFiles.length > 0) {
      e.preventDefault()
      void uploadFilesToChatRef.current(pastedImageFiles)
      return
    }

    const pasted = e.clipboardData?.getData('text') ?? ''
    if (!pasted) return

    if (!shouldUsePastedTextAttachment(pasted)) return

    const textarea = e.currentTarget
    const start = textarea.selectionStart ?? inputText.length
    const end = textarea.selectionEnd ?? inputText.length
    const before = inputText.slice(0, start)
    const after = inputText.slice(end)
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

    // Product chats intentionally do not expose AgentWorks command, workflow,
    // skill, server, or file-reference syntax. Attachments still use the
    // dedicated paperclip button below.
    if (isProductSurface) {
      setShowFileDialog(false)
      setShowCommandDialog(false)
      setShowWorkflowDialog(false)
      setShowSkillPopup(false)
      setShowServerPopup(false)
      return
    }

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
  }, [chatFileContext, chatPastedAttachments, removeFileFromContext, showCommandDialog, showWorkflowDialog, activeTabId, setTabConfig, adjustTextareaHeight, isProductSurface, isWorkflowPhaseChat])

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
    const effectiveWorkshopMode = activeTab?.metadata?.workshopMode
      || (presetId && workflowState.workshopModeByPreset[presetId])
      || workflowState.workshopMode

    return {
      workflowMode: workflowState.workflowMode,
      workshopMode: effectiveWorkshopMode,
    }
  }, [activeTab?.metadata?.workshopMode])

  const applyWorkflowCommandRequirements = useCallback((cmd: CommandDefinition) => {
    if (selectedModeCategory !== 'workflow') return
    if (!cmd.requiredWorkflowMode && !cmd.requiredWorkshopMode) return

    const workflowStore = useWorkflowStore.getState()
    const { workflowMode: currentWorkflowMode, workshopMode: currentWorkshopMode } = getEffectiveWorkflowModes()

    const requiredWorkshopModes = cmd.requiredWorkshopMode
      ? (Array.isArray(cmd.requiredWorkshopMode) ? cmd.requiredWorkshopMode : [cmd.requiredWorkshopMode])
      : []
    // If current workshop mode is already one of the allowed modes, no switch needed
    const workshopModeMatches = requiredWorkshopModes.length === 0 || requiredWorkshopModes.includes(currentWorkshopMode)
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

    // Retained coding CLIs accept live follow-ups through their provider-native
    // transport. Only non-interactive/structured turns use the local queue.
    const queueAwareOnSubmit = (query: string) => {
      const trimmed = query?.trim()
      if (!trimmed) return
      // SINGLE-ENTRY routing: the backend resolves tmux liveness, provider input
      // semantics, and any turn-boundary fallback. Do not duplicate that policy
      // in the command UI.
      if (routeLiveInputToCLI) {
        onSubmit(trimmed, { preferLiveInput: true })
        return
      }
      if (isStreaming) {
        const currentQueued = tabConfig?.queuedMessages || []
        setTabConfig(activeTabId, {
          inputText: '',
          queuedMessages: [...currentQueued, trimmed]
        })
        addToast('Builder is busy — message queued for the next turn', 'info')
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
      setTabConfig,
      addToast,
      handleSummarize,
      handleCompact,
      getAppStore: () => useAppStore.getState(),
      getWorkspaceStore: () => useWorkspaceStore.getState(),
      getWorkflowStore: () => useWorkflowStore.getState(),
      modeCategory: selectedModeCategory ?? undefined,
      workflowMode: effectiveModes.workflowMode,
      workshopMode: effectiveModes.workshopMode,
      workflowPhaseId
    }
  }, [activeTabId, tabSessionId, tabConfig, isSummarizing, isStreaming, routeLiveInputToCLI, onSubmit, openDialog, setTabConfig, addToast, handleSummarize, handleCompact, getEffectiveWorkflowModes, selectedModeCategory, workflowPhaseId])

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
        const targetLabel = availableInWorkflow ? 'automation' : 'multi-agent'
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
    if (isStreaming && !tabSessionId) return 'This chat is still initializing. Please wait a moment.'
    if (!isStreaming && !hasSubmitTarget) return 'This chat is still initializing. Please wait a moment.'
    return null
  }, [queryToSubmit, isViewOnly, isCdpDisconnected, isStreaming, tabSessionId, hasSubmitTarget])

  // routeSubmit is the single send-routing decision shared by Enter (handleKeyDown)
  // and the Send button (handleSubmit).
  //
  // INTERACTIVE CODING CLI — every message uses the normal single-entry backend
  // route, including while a turn is running. Claude, Codex, and interactive
  // Cursor sessions each own their native live-vs-next-turn behavior.
  //
  // STRUCTURED / NON-INTERACTIVE — keep the ordinary steer-or-queue behavior.
  const routeSubmit = useCallback(async (query: string) => {
    const trimmed = query?.trim() || ''
    if (!trimmed) return

    // A retained CLI is explicitly designed to receive input while busy, so it
    // continues into the live-delivery branch below. Structured workflow-step
    // turns never satisfy routeLiveInputToCLI and remain queued.
    if (isStreaming && !routeLiveInputToCLI) {
      clearInputState()
      queueStreamingMessage(query)
      addToast('Agent is busy — message queued for the next turn', 'info')
      return
    }

    if (routeLiveInputToCLI) {
      if (hasSubmitTarget) {
        const submittedTabId = activeTabId || undefined
        const submittedDraft = {
          inputText,
          pastedAttachments: chatPastedAttachments,
        }
        setLiveMessageDelivery({
          status: 'sending',
          message: query,
          provider: effectiveProviderForSteer || undefined,
        })
        // Live-input acknowledgement can lag while the retained CLI wakes up.
        // The conversation adds an optimistic user row immediately, so release
        // the composer now instead of leaving the submitted text looking stuck.
        clearInputState()
        let accepted: boolean | void
        try {
          accepted = await onSubmit(query, {
            preferLiveInput: true,
            sourceTabId: submittedTabId,
          })
        } catch (error) {
          console.error('[ChatInput] Live message submission failed', error)
          accepted = false
        }
        if (accepted === false) {
          // Do not overwrite text the user began typing while delivery was in
          // flight, and do not restore a draft into a tab they navigated away
          // from. Otherwise put the original text/attachments back for retry.
          if (submittedTabId && inputOwnerTabIdRef.current === submittedTabId && !latestQueryToSubmitRef.current.trim()) {
            setLocalInputText(submittedDraft.inputText)
            setTabConfig(submittedTabId, submittedDraft)
          }
          setLiveMessageDelivery({
            status: 'failed',
            message: query,
            provider: effectiveProviderForSteer || undefined,
            detail: 'Not accepted; draft kept',
          })
          scheduleLiveMessageDeliveryClear()
          return
        }
        setLiveMessageDelivery({
          status: 'sent_to_cli',
          message: query,
          provider: effectiveProviderForSteer || undefined,
        })
        scheduleLiveMessageDeliveryClear()
        return
      }
      const reason = getSubmitBlockReason()
      if (reason) addToast(reason, 'info')
      return
    }

    if (canSubmitImmediately) {
      const submittedTabId = activeTabId || undefined
      let accepted: boolean | void
      try {
        accepted = await onSubmit(query, { sourceTabId: submittedTabId })
      } catch (error) {
        console.error('[ChatInput] Message submission failed', error)
        accepted = false
      }
      if (shouldClearAcceptedChatDraft({
        accepted,
        submittedTabId,
        currentTabId: inputOwnerTabIdRef.current,
        submittedMessage: query,
        currentMessage: latestQueryToSubmitRef.current,
      })) {
        clearInputState()
      } else if (accepted === false) {
        addToast('Message was not accepted. Your draft was kept.', 'warning')
      }
    } else if (canSubmit && isStreaming) {
      clearInputState()
      queueStreamingMessage(query)
    } else {
      const reason = getSubmitBlockReason()
      if (reason) addToast(reason, 'info')
    }
  }, [routeLiveInputToCLI, hasSubmitTarget, activeTabId, inputText, chatPastedAttachments, effectiveProviderForSteer, onSubmit, scheduleLiveMessageDeliveryClear, clearInputState, setTabConfig, getSubmitBlockReason, addToast, canSubmitImmediately, canSubmit, isStreaming, queueStreamingMessage])

  const handleKeyDown = useCallback((e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    // If any selection dialog is open, let it handle keyboard events
    if (showCommandDialog || showFileDialog || showWorkflowDialog || showSkillPopup || showServerPopup) {
      // Prevent default for arrow keys, enter, escape so textarea doesn't move cursor
      if (['ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight', 'Enter', 'Escape'].includes(e.key)) {
        e.preventDefault()
        return
      }
    }

    // With an empty input, route prompt/menu keys into the coding-agent pane.
    // Do not gate this on isStreaming/canSteer: a completed agent turn can leave
    // its interactive tmux process alive and waiting for keyboard input.
    const liveCliKey = liveTerminalControlKey(e, queryToSubmit)
    if (liveCliKey && supportsLiveCodingAgentInput && tabSessionId) {
      e.preventDefault()
      void sendLiveCodingAgentControlKey(liveCliKey, { showToast: false }).then((delivered) => {
        // Escape retains its old non-tmux cancellation fallback while a turn is
        // streaming. Other navigation keys have no meaningful chat fallback.
        if (!delivered && liveCliKey === 'Escape' && isStreaming) onStopStreaming()
      })
      return
    }

    // Non-tmux/API providers retain the previous Escape behavior.
    if (e.key === 'Escape' && isStreaming) {
      e.preventDefault()
      onStopStreaming()
      return
    }

    // Handle normal Enter to submit. Shift/Ctrl/Cmd+Enter insert a newline.
    if (e.key === 'Enter' && !e.shiftKey && !e.ctrlKey && !e.metaKey) {
      e.preventDefault()

      // Mid-dictation, Enter stops the dictation; the text lands in the
      // composer and the user sends it with a second Enter.
      if (micStateRef.current === 'recording') {
        micRef.current?.stopDictation()
        return
      }

      // Check for slash commands
      const trimmedQuery = queryToSubmit?.trim() || ''
      if (executeSlashCommandFromQuery(trimmedQuery)) {
        return
      }

      void routeSubmit(queryToSubmit)
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
  }, [showFileDialog, showCommandDialog, showWorkflowDialog, showSkillPopup, showServerPopup, isStreaming, onStopStreaming, queryToSubmit, executeSlashCommandFromQuery, tabSessionId, routeSubmit, supportsLiveCodingAgentInput, sendLiveCodingAgentControlKey, inputText, setLocalInputText])

  const handleSubmit = useCallback((e: React.FormEvent) => {
    e.preventDefault()

    // Check for slash commands
    const trimmedQuery = queryToSubmit?.trim() || ''
    if (executeSlashCommandFromQuery(trimmedQuery)) {
      return
    }

    void routeSubmit(queryToSubmit)
  }, [queryToSubmit, executeSlashCommandFromQuery, routeSubmit])

  const handleSendButtonClick = useCallback(() => {
    const trimmedQuery = queryToSubmit?.trim() || ''
    if (executeSlashCommandFromQuery(trimmedQuery)) {
      return
    }

    void routeSubmit(queryToSubmit)
  }, [queryToSubmit, executeSlashCommandFromQuery, routeSubmit])


  // Opens the same command menu a typed "/" does, for anyone who would not
  // think to type it. slashPosition uses the current text length rather than
  // an actual "/" character -- handleCommandSelect only reads up to that
  // index as context, it never requires the character to be present, so this
  // reproduces typed-slash behavior without mutating what the user wrote.
  const openCommandMenu = useCallback(() => {
    if (showCommandDialog) {
      setShowCommandDialog(false)
      return
    }
    setSlashPosition(inputText.length)
    setCommandSearchQuery('')
    setShowCommandDialog(true)
    setShowFileDialog(false)
    setShowWorkflowDialog(false)
    const rect = textareaRef.current?.getBoundingClientRect()
    if (rect) {
      setCommandDialogPosition({ bottom: window.innerHeight - rect.top + 8, left: rect.left + window.scrollX })
    }
    textareaRef.current?.focus()
  }, [inputText, showCommandDialog])

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
  }, [inputText, atPosition, fileSearchQuery, chatFileContext, addFileToContext, scrollToFile, activeTabId])

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
    if (agentProfileWorkspace) {
      return `${agentProfileWorkspace.replace(/\/$/, '')}/uploads`
    }
    if (selectedModeCategory === 'workflow') {
      // activeWorkflowWorkspacePath resolves the workflow this chat tab is
      // actually scoped to. workspaceActiveFolder is the file browser's own
      // navigation state -- a separate piece of state that can be empty or
      // pointed elsewhere while the chat itself is scoped to a workflow, which
      // silently dropped pasted screenshots into the shared Workflow/ root
      // instead of the workflow's own folder (confirmed live: a pasted image
      // from the confida-login chat landed at Workflow/pasted-image-*.png).
      return activeWorkflowWorkspacePath || workspaceActiveFolder || 'Workflow'
    }
    return 'Chats'
  }, [activeWorkflowWorkspacePath, agentProfileWorkspace, selectedModeCategory, workspaceActiveFolder])

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
      if (activeTabId) {
        const latestFileContext = useChatStore.getState().getTabConfig(activeTabId)?.fileContext || chatFileContext
        const seenPaths = new Set(latestFileContext.map((item: { path: string }) => item.path))
        const uploadedContextItems = uploadedPaths
          .filter((path) => {
            if (seenPaths.has(path)) return false
            seenPaths.add(path)
            return true
          })
          .map((path) => ({
            name: path.split('/').pop() || path,
            path,
            type: 'file' as const,
          }))

        if (uploadedContextItems.length > 0) {
          setTabConfig(activeTabId, {
            fileContext: [...latestFileContext, ...uploadedContextItems],
          })
        }
      }

      const refs = uploadedPaths.map(path => `@${path}`).join(' ')
      const prefix = inputText.trim().length > 0 ? `${inputText} ` : ''
      const newText = `${prefix}${refs} `
      setLocalInputText(newText)
      if (activeTabId) {
        setTabConfig(activeTabId, { inputText: newText })
      }

      const ws = useWorkspaceStore.getState()
      ws.fetchFiles(
        ws.activeFolder ?? undefined,
        ws.activeFolder ? undefined : { maxDepth: 2 }
      ).catch(() => {})

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
  }, [activeTabId, isUploadingFiles, uploadTargetFolder, chatFileContext, inputText, setTabConfig, addToast])

  useEffect(() => {
    uploadFilesToChatRef.current = uploadFilesToChat
  }, [uploadFilesToChat])

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
  // Product follow-ups are queued while a structured turn is working, including
  // the short interval before the backend has attached the live session.
  const submitButtonDisabled = !hasValidQuery || !hasSubmitTarget || isViewOnly || isCdpDisconnected
  
  // Memoized placeholder
  const placeholder = useMemo(() => {
    if (isViewOnly) return "View only — cannot continue this conversation"
    if (isProductSurface) return isStreaming ? 'Add a message…' : (placeholderOverride || 'Describe what you want to create…')
    if (agentProfileWorkspace) return 'Describe the video you want to make… (@ files, / commands)'
    if (isWorkflowPhaseChat) {
      return 'Chat with the automation builder... (@ files, / commands, # automations)'
    }
    const baseHints = "@ files, / commands, # automations, ! skills, $ servers"
    if (!tabSessionId && (canBootstrapMultiAgentTab || canBootstrapWorkflowPhaseTab)) return `Ask anything... chat will initialize on send (${baseHints})`
    if (isMultiAgentMode) return `Ask anything... (${baseHints})`
    return `Ask anything... (${baseHints})`
  }, [agentProfileWorkspace, isProductSurface, isStreaming, isViewOnly, isMultiAgentMode, isWorkflowPhaseChat, tabSessionId, canBootstrapMultiAgentTab, canBootstrapWorkflowPhaseTab])

  const liveDeliveryProviderLabel = formatLiveInputProviderLabel(liveMessageDelivery?.provider || effectiveProviderForSteer, primaryLLM?.model)
  const liveDeliveryText = liveMessageDelivery
    ? liveMessageDelivery.status === 'sending'
      ? isProductSurface ? 'Sending message…' : `Sending to ${liveDeliveryProviderLabel}...`
      : liveMessageDelivery.status === 'sent_to_cli'
        ? isProductSurface ? 'Message sent' : `Sent to ${liveDeliveryProviderLabel}`
      : liveMessageDelivery.status === 'queued_for_injection'
          ? isProductSurface ? 'Message queued' : 'Queued for next model turn'
        : liveMessageDelivery.status === 'next_turn_started'
            ? isProductSurface ? 'Working on your follow-up' : `Started next ${liveDeliveryProviderLabel} turn`
          : liveMessageDelivery.status === 'queued_locally'
              ? isProductSurface ? 'Message saved' : 'Saved to queue'
              : isProductSurface ? 'Could not send the message' : 'Could not submit live input'
    : ''
  // The chat history already echoes an accepted message as its own bubble
  // once delivery reaches 'sent_to_cli' or 'next_turn_started' (see
  // ChatArea's submitQueryImmediately, which appends an optimistic user
  // message event for exactly those two statuses, on every surface, not
  // just product chat). Live submissions now receive that same optimistic
  // bubble before their acknowledgement arrives, so the sending indicator is
  // status-only rather than a second copy of the message. Queued/local-save
  // states never get a bubble, so they still need the banner as the only
  // signal; failed submissions retain their message preview for retry context.
  const showLiveDelivery = Boolean(liveMessageDelivery && (
    liveMessageDelivery.status === 'sending' ||
    liveMessageDelivery.status === 'failed' ||
    (!isProductSurface && liveMessageDelivery.status !== 'sent_to_cli' && liveMessageDelivery.status !== 'next_turn_started')
  ))
  const liveDeliveryClass = liveMessageDelivery?.status === 'failed'
    ? 'text-amber-600 dark:text-amber-300'
    : liveMessageDelivery?.status === 'sending'
      ? 'text-blue-600 dark:text-blue-300'
      : 'text-emerald-600 dark:text-emerald-300'

  // Product chats use the roomier project layout; workflow mode keeps the
  // existing toolbar alignment.
  const inputPadX = isProductSurface ? 'px-3' : isMultiAgentMode ? 'px-3' : 'px-4'

  // For view-only (restored) tabs, show a minimal indicator instead of the full input form
  if (isViewOnly) {
    const isScheduledRun = activeTab?.metadata?.isScheduledRun
    const isBotRun = activeTab?.metadata?.isBotRun
    const jobName = activeTab?.metadata?.scheduledJobName
    const botPlatform = activeTab?.metadata?.botPlatform
    return (
      <div data-tour="chat-input-area" data-testid="tour-chat-input-area" className={`${inputPadX} ${isProductSurface ? 'py-1' : 'py-2'}`}>
        <div className="relative flex items-center justify-center gap-2 py-1 text-xs text-muted-foreground">
          <History className="w-3.5 h-3.5" />
          <span>
            {isScheduledRun
              ? `Scheduled run — view only${jobName ? ` (${jobName})` : ''}`
              : isBotRun
                ? `${botPlatform || 'Bot'} run — view only`
              : 'View only — restored conversation'}
          </span>
          {liveTerminalOffered && activeTabId && (
            <Button
              type="button"
              variant={terminalViewSelected ? 'secondary' : 'ghost'}
              size="icon"
              onClick={() => useChatStore.getState().setTabViewMode(
                activeTabId,
                terminalViewSelected ? 'formatted' : 'terminal',
              )}
              className="absolute right-0 h-7 w-7 p-0"
              aria-label={terminalViewSelected ? 'Return to conversation' : 'Open live view'}
              title={terminalViewSelected ? 'Return to conversation' : 'Open live view'}
            >
              <Terminal className="h-3.5 w-3.5" />
            </Button>
          )}
        </div>
      </div>
    )
  }

  // Extracted so SparkQuill can place them on the opposite side of the
  // composer from every other product (sparkQuillComposerLayout above) —
  // one JSX definition each, rendered in whichever group applies.
  const micEl = voiceCapabilityEnabled && (
    <MicButton
      ref={micRef}
      profileId={isProductSurface ? (agentProfileId ?? '') : ''}
      onText={handleVoiceText}
      onStateChange={handleMicStateChange}
      bannerHost={micBannerHost}
      shortcutEnabled={!scopedTabId}
      disabled={isSummarizing}
    />
  )
  const sparkleEl = (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type="button"
          variant="outline"
          size="icon"
          disabled={isSummarizing}
          onClick={openCommandMenu}
          className="h-7 w-7 p-0"
          data-testid="chat-command-menu-button"
          aria-label="Browse commands"
        >
          <Wand2 className="w-3.5 h-3.5" />
        </Button>
      </TooltipTrigger>
      <TooltipContent>
        <p>Browse commands</p>
      </TooltipContent>
    </Tooltip>
  )
  const attachmentEl = (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type="button"
          variant="outline"
          size="icon"
          disabled={isSummarizing || isUploadingFiles}
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
          className="h-7 w-7 p-0"
          data-testid="chat-upload-button"
          aria-label="Attach files"
        >
          {isUploadingFiles ? (
            <Loader2 className="w-3.5 h-3.5 animate-spin" />
          ) : (
            <Paperclip className="w-3.5 h-3.5" />
          )}
        </Button>
      </TooltipTrigger>
      <TooltipContent>
        <p>{isUploadingFiles ? 'Uploading files...' : isProductSurface ? 'Attach files to this project' : `Upload file(s) to ${uploadTargetFolder}`}</p>
      </TooltipContent>
    </Tooltip>
  )

  return (
    <TooltipProvider>
      <div className={isProductSurface ? 'border-t border-border bg-background py-1.5 shadow-[0_-8px_24px_rgba(15,23,42,0.08)]' : 'space-y-1'} data-product-chat-input={isProductSurface || undefined}>
      {/* Pasted-text Attachments */}
      {chatPastedAttachments.length > 0 && (
        <div className={inputPadX}>
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

      {/* Pending resume indicator */}
      {!isProductSurface && showRestoredConversationIndicator && (
        <div className={`${inputPadX} border-t border-border`}>
          <div className="mb-1 rounded-md border border-border bg-card px-2 py-1 shadow-sm">
            <div className="flex min-w-0 items-center justify-between gap-2">
              <div className="flex min-w-0 items-center gap-1.5 text-xs text-foreground">
                {restoredResumeUsesNative ? (
                  <Terminal className="h-3.5 w-3.5 shrink-0 text-primary" />
                ) : (
                  <History className="h-3.5 w-3.5 shrink-0 text-primary" />
                )}
                <span className="shrink-0 font-semibold">
                  {restoredResumeUsesNative ? 'Resuming coding session' : 'Resuming previous chat'}
                </span>
                <span className="truncate text-muted-foreground" title={restoredConversationPath}>
                  {restoredResumeTitle}
                </span>
                {restoredResumeWorkshopModeLabel && (
                  <span className="hidden shrink-0 rounded border border-border bg-background px-1 py-0.5 text-[10px] font-medium uppercase text-muted-foreground sm:inline">
                    Mode: {restoredResumeWorkshopModeLabel}
                  </span>
                )}
                {restoredResumeRuntimeLabel && (
                  <span className="hidden shrink-0 rounded border border-border bg-background px-1 py-0.5 font-mono text-[10px] uppercase text-muted-foreground sm:inline">
                    {restoredResumeRuntimeLabel}
                  </span>
                )}
              </div>
              <button
                type="button"
                onClick={clearRestoredConversation}
                className="rounded p-0.5 text-muted-foreground hover:bg-muted hover:text-foreground"
                title="Clear pending resume"
              >
                <X className="h-3 w-3" />
              </button>
            </div>
          </div>
        </div>
      )}

      {/* File Context Display */}
      {chatFileContext.length > 0 && (
        <div className={`${inputPadX} border-t border-gray-200 dark:border-gray-700`}>
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
        <div className={inputPadX}>
          <div className="border rounded px-1.5 py-0.5 mb-1 bg-gray-50 dark:bg-gray-800 border-gray-200 dark:border-gray-700">
            <div className="flex items-center gap-1.5 flex-wrap">
              <span className="text-xs font-medium text-gray-600 dark:text-gray-400">
                <Layers className="w-3 h-3 inline-block mr-0.5 -mt-0.5" />
                Automations:
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
                    title="Remove automation context"
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
      {/* The transcript above ends with its own margin; keep the band's top
          padding small so the last message and the composer read as one column. */}
      <div data-tour="chat-input-area" data-testid="tour-chat-input-area" className={`${inputPadX} ${isProductSurface ? 'py-2' : 'pt-1 pb-2'}`}>
        <form onSubmit={handleSubmit} className={isProductSurface ? 'relative' : 'relative space-y-1'}>
          {/* The mic's banner (download progress, "Listening" with the live
              transcript) portals here, in normal flow directly above the
              composer box, so it can never be clipped by a container. */}
          <div ref={setMicBannerHost} className="empty:hidden" />
          <div className={isProductSurface
            // Keep the customer composer visually steady while events stream.
            // The former ring-4 plus catch-all `transition` made a harmless
            // focus hand-off look like a pulsing purple border on redraws.
            ? 'space-y-0.5 rounded-2xl border border-border bg-card px-1.5 py-1 shadow-sm transition-colors duration-150 focus-within:border-ring'
            : 'space-y-1 rounded-xl border border-slate-700/80 bg-[#101513] p-1.5 shadow-sm transition focus-within:border-slate-500'}>
            {showLiveDelivery && liveMessageDelivery && (
              <div className={`flex min-w-0 items-center gap-1.5 text-[11px] ${liveDeliveryClass}`}>
                {liveMessageDelivery.status === 'sending' ? (
                  <Loader2 className="h-3 w-3 shrink-0 animate-spin" />
                ) : (
                  <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-current" />
                )}
                <span className="shrink-0 font-medium">{liveDeliveryText}</span>
                {!isProductSurface && liveMessageDelivery.status !== 'sending' && <span className="min-w-0 truncate opacity-75">
                  {liveDeliveryPreview(liveMessageDelivery.message)}
                </span>}
                {!isProductSurface && liveMessageDelivery.detail && (
                  <span className="shrink-0 opacity-75">({liveMessageDelivery.detail})</span>
                )}
              </div>
            )}
            {/* Queued messages: a message sent while the agent is still working is
                held here until the current turn ends, then sent as the next one.
                The product surface used to collapse this to a bare "N messages
                queued" count with no visible content -- reported directly: a
                message typed mid-turn "doesn't show up anywhere". Reusing the
                same list the developer surface uses (minus the Steer button,
                which assumes CLI/live-run concepts this audience shouldn't need)
                means what was actually typed stays visible while it waits. */}
            {queuedMessages.length > 0 && (
              <div className="space-y-1">
                {queuedDisplayItems.map((item, index) => {
                  if (item.type === 'auto-group') {
                    return (
                      <QueuedAutoNotificationGroup
                        key={`auto-group-${item.items[0]?.index ?? index}`}
                        items={item.items}
                        onDelete={removeQueuedMessageAtIndex}
                        onSteer={!isProductSurface && canShowSteer && tabSessionId ? handleSteerQueuedMessage : undefined}
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
                      onSteer={!isProductSurface && canShowSteer && tabSessionId ? () => handleSteerQueuedMessage(item.index, item.msg) : undefined}
                      isSteering={steeringIndex === item.index}
                    />
                  )
                })}
              </div>
            )}
            {/* Show text input — compact and auto-growing (no reserved dead
                space). adjustTextareaHeight's early-return guard prevents the
                per-keystroke height='auto' reflow that would otherwise resize the
                terminal on single-line typing; only a real wrap grows it. */}
            <div>
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
              rows={isProductSurface ? 1 : undefined}
              placeholder={placeholder}
              className={`${isProductSurface ? '!min-h-[32px] max-h-[100px] !border-0 !bg-transparent !px-1.5 !py-1 text-sm text-foreground !shadow-none focus-visible:!ring-0 placeholder:text-sm placeholder:text-muted-foreground' : '!min-h-[36px] max-h-[100px] !border-0 !bg-transparent !py-1.5 !px-2 text-xs !shadow-none focus-visible:!ring-0 placeholder:text-xs'} resize-none overflow-y-auto leading-[1.3] ${
                isDraggingFiles ? 'ring-2 ring-blue-500 border-blue-500 bg-blue-50/30 dark:bg-blue-900/10' : ''
              }`}
              disabled={inputDisabled}
              data-testid="chat-input-textarea"
            />
            </div>
            {isDraggingFiles && (
              <div className="text-[11px] text-blue-600 dark:text-blue-400 px-1">
                Drop files to upload and attach to this chat
              </div>
            )}
            <div className="flex justify-between items-center">
              <div className="flex items-center gap-1.5">
                {showNewChatAction && onNewChat ? (
                  <Button
                    type="button"
                    variant="ghost"
                    onClick={onNewChat}
                    disabled={isTurnInFlight}
                    className="h-7 gap-1.5 px-2 text-[11px] text-muted-foreground"
                    aria-label="Start a new chat"
                    title={isTurnInFlight ? 'Wait for the current response or stop it first' : 'Start a new chat'}
                  >
                    <Plus className="h-3.5 w-3.5" />
                    New chat
                  </Button>
                ) : null}
                {!hideRuntimeStatus && mainAgentRuntimeStatus && (
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <div
                        className="flex h-7 max-w-[205px] items-center gap-1.5 px-1 font-mono text-[11px] text-muted-foreground"
                        role="status"
                        aria-label={`${mainAgentRuntimeStatus.label} — ${mainAgentRuntimeStatus.state}`}
                      >
                        {mainAgentRuntimeStatus.state === 'running' ? (
                          <Loader2 className="h-3 w-3 shrink-0 animate-spin text-lime-300" aria-hidden="true" />
                        ) : mainAgentRuntimeStatus.state === 'waiting' ? (
                          <span className="h-2 w-2 shrink-0 rounded-full bg-amber-400" aria-hidden="true" />
                        ) : (
                          <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-lime-300" aria-hidden="true" />
                        )}
                        <span className="truncate">{mainAgentRuntimeStatus.label}</span>
                      </div>
                    </TooltipTrigger>
                    <TooltipContent side="top">
                      <p>{mainAgentRuntimeStatus.label} — {mainAgentRuntimeStatus.activityLabel}</p>
                    </TooltipContent>
                  </Tooltip>
                )}
                {chatInputStatusLine && (
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <div
                        data-testid="chat-input-statusline"
                        className="flex h-7 max-w-[220px] items-center rounded-md border border-lime-400/20 bg-lime-400/5 px-1.5 font-mono text-[10px] text-lime-200/90"
                        role="status"
                        aria-label={`Provider status: ${chatInputStatusLine}`}
                      >
                        <span className="truncate">{chatInputStatusLine}</span>
                      </div>
                    </TooltipTrigger>
                      <TooltipContent side="top">
                        <p>Runtime status · {chatInputStatusLine}</p>
                      </TooltipContent>
                  </Tooltip>
                )}
                {activeTabId && shouldShowLiveTerminalControl(
                  liveTerminalOffered,
                  isProductSurface,
                  isInteractiveWorkflowBuilderChat,
                ) && (
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button
                        type="button"
                        variant={terminalViewSelected ? 'secondary' : 'outline'}
                        size="icon"
                        onClick={() => useChatStore.getState().setTabViewMode(
                          activeTabId,
                          terminalViewSelected ? 'formatted' : 'terminal',
                        )}
                        className="h-7 w-7 p-0"
                        aria-label={terminalViewSelected ? 'Return to conversation' : 'Open live view'}
                      >
                        <Terminal className="w-3.5 h-3.5" />
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>
                      <p>{terminalViewSelected ? 'Return to conversation' : 'Open live view'}</p>
                    </TooltipContent>
                  </Tooltip>
                )}
                {/* Server and LLM Selection — hidden in workflow phase chat (servers come from preset) */}
                {(
                  <div data-tour="chat-input-tools" data-testid="tour-chat-input-tools" className="flex items-center gap-2">
                      {sparkQuillComposerLayout && (
                        <>
                          {attachmentEl}
                          {sparkleEl}
                        </>
                      )}
                      {!sparkQuillComposerLayout && micEl}
                      {isProductSurface && newConversationEnabled && (
                        <NewChatControl
                          engines={engineGroups.map((g) => ({ id: g.option.id, label: g.option.label || g.option.id }))}
                          disabled={isSummarizing}
                          onStart={requestNewConversation}
                        />
                      )}
                      {isProductSurface && engineGroups.some((g) => g.models.length > 0) && (
                        <ModelReasoningControl
                          engines={engineGroups.map((g) => ({ id: g.option.id, label: g.option.label || g.option.id, models: g.models }))}
                          currentEngineId={currentEngine}
                          currentModelId={currentModel}
                          engineChangeable={!chatHasTurns}
                          reasoningLevels={currentGroup?.reasoningLevels ?? []}
                          currentReasoningEffort={currentReasoningEffort}
                          defaultReasoningEffort={defaultReasoningEffort}
                          disabled={isSummarizing}
                          onSelect={selectProductEngine}
                        />
                      )}
                      <>
                        {!hideExtras && !isMultiAgentMode && (
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
                        {!hideExtras && !isMultiAgentMode && (
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

                    {/* Browser access lives in the chat header for multi-agent mode. */}
                    {!hideExtras && !isMultiAgentMode && <button
                      type="button"
                      data-tour="chat-browser-tools"
                      data-testid="tour-chat-browser-tools"
                      onClick={() => {
                        if (browserMode === 'none') {
                          // Enabling browser: prefer connected CDP, otherwise use headless.
                          setBrowserMode('auto')
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
                          : browserMode === 'auto'
                              ? 'bg-cyan-900/40 border-cyan-600 text-cyan-300'
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
                            : browserMode === 'auto'
                                ? 'bg-cyan-800 text-cyan-100'
                                : 'bg-blue-800 text-blue-200'
                        }`}>
                          {browserMode === 'cdp' ? 'CDP' : browserMode === 'auto' ? 'Auto' : 'Headless'}
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
                    <div className="w-full max-w-3xl overflow-hidden rounded-xl border border-border bg-background text-foreground shadow-2xl" onClick={(e) => e.stopPropagation()}>
                      {/* Header */}
                      <div className="flex items-start justify-between gap-4 border-b border-border px-5 py-4">
                        <div className="flex items-start gap-3">
                          <div className="flex h-9 w-9 items-center justify-center rounded-lg border border-primary/20 bg-primary/10 text-primary">
                            <Globe className="h-4 w-4" />
                          </div>
                          <div>
                            <h3 className="text-base font-semibold">Browser Access</h3>
                            <p className="mt-0.5 text-xs text-muted-foreground">Choose how this chat can drive websites.</p>
                          </div>
                        </div>
                        <button onClick={() => { setShowCdpPopup(false); setWorkspaceMinimized(false) }} className="rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground">
                          <X className="w-5 h-5" />
                        </button>
                      </div>

                      {/* Content */}
                      <div className="space-y-4 px-5 py-4">

                        {/* Mode options */}
                        <div className="grid gap-3 md:grid-cols-3">
                          {/* Automatic */}
                          <label className={`flex min-h-[132px] cursor-pointer flex-col gap-3 rounded-lg border p-3 transition-colors ${
                            browserMode === 'auto'
                              ? 'border-cyan-500 bg-cyan-500/10 ring-1 ring-cyan-500/20'
                              : 'border-border bg-card/40 hover:bg-muted/50'
                          }`}>
                            <input
                              type="radio"
                              name="browserMode"
                              checked={browserMode === 'auto'}
                              onChange={() => setBrowserMode('auto')}
                              className="sr-only"
                            />
                            <div className="flex items-center gap-2">
                              <span className={`h-3 w-3 rounded-full border ${browserMode === 'auto' ? 'border-cyan-400 bg-cyan-400 ring-4 ring-cyan-400/20' : 'border-muted-foreground/50'}`} />
                              <div className="text-sm font-medium">Automatic</div>
                            </div>
                            <div className="text-xs leading-5 text-muted-foreground">
                              Uses Local Chrome through CDP when reachable; otherwise uses headless agent-browser.
                            </div>
                            <div className={`mt-auto inline-flex w-fit items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-medium ${cdpChecking || cdpConnected === null ? 'bg-amber-500/10 text-amber-300' : cdpConnected ? 'bg-emerald-500/10 text-emerald-300' : 'bg-blue-500/10 text-blue-300'}`}>
                              <span className={`h-1.5 w-1.5 rounded-full ${cdpConnected ? 'bg-emerald-400' : cdpChecking || cdpConnected === null ? 'bg-amber-400' : 'bg-blue-400'}`} />
                              {cdpChecking || cdpConnected === null ? 'Checking CDP' : cdpConnected ? 'Will use CDP' : 'Will use headless'}
                            </div>
                          </label>

                          {/* Headless */}
                          <label className={`flex min-h-[132px] cursor-pointer flex-col gap-3 rounded-lg border p-3 transition-colors ${
                            browserMode === 'headless'
                              ? 'border-primary bg-primary/10 ring-1 ring-primary/20'
                              : 'border-border bg-card/40 hover:bg-muted/50'
                          }`}>
                            <input
                              type="radio"
                              name="browserMode"
                              checked={browserMode === 'headless'}
                              onChange={() => setBrowserMode('headless')}
                              className="sr-only"
                            />
                            <div className="flex items-center gap-2">
                              <span className={`h-3 w-3 rounded-full border ${browserMode === 'headless' ? 'border-primary bg-primary ring-4 ring-primary/20' : 'border-muted-foreground/50'}`} />
                              <div className="text-sm font-medium">Headless</div>
                            </div>
                            <div className="text-xs leading-5 text-muted-foreground">
                              Background Chromium via{' '}
                              <a
                                href="https://github.com/vercel/agent-browser"
                                target="_blank"
                                rel="noopener noreferrer"
                                className="text-primary hover:underline"
                                onClick={(e) => e.stopPropagation()}
                              >
                                agent-browser
                              </a>
                              . No visible window.
                            </div>
                          </label>

                          {/* CDP */}
                          <label className={`flex min-h-[132px] cursor-pointer flex-col gap-3 rounded-lg border p-3 transition-colors ${
                            browserMode === 'cdp'
                              ? 'border-emerald-500 bg-emerald-500/10 ring-1 ring-emerald-500/20'
                              : 'border-border bg-card/40 hover:bg-muted/50'
                          }`}>
                            <input
                              type="radio"
                              name="browserMode"
                              checked={browserMode === 'cdp'}
                              onChange={() => setBrowserMode('cdp')}
                              className="sr-only"
                            />
                            <div className="flex items-center gap-2">
                              <span className={`h-3 w-3 rounded-full border ${browserMode === 'cdp' ? 'border-emerald-400 bg-emerald-400 ring-4 ring-emerald-400/20' : 'border-muted-foreground/50'}`} />
                              <div className="text-sm font-medium">Local Chrome</div>
                            </div>
                            <div className="text-xs leading-5 text-muted-foreground">
                              Connects to your real Chrome browser through CDP. Useful when login state matters.
                            </div>
                            {browserMode === 'cdp' && (
                              <div className={`mt-auto inline-flex w-fit items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-medium ${
                                cdpChecking || cdpConnected === null
                                  ? 'bg-amber-500/10 text-amber-300'
                                  : cdpConnected
                                    ? 'bg-emerald-500/10 text-emerald-300'
                                    : 'bg-red-500/10 text-red-300'
                              }`}>
                                <span className={`h-1.5 w-1.5 rounded-full ${cdpConnected ? 'bg-emerald-400' : cdpChecking || cdpConnected === null ? 'bg-amber-400' : 'bg-red-400'}`} />
                                {cdpChecking || cdpConnected === null ? 'Needs check' : cdpConnected ? 'Connected' : 'Not reachable'}
                              </div>
                            )}
                          </label>

                        </div>

                        {/* Context panel */}
                        <div className="rounded-lg border border-border bg-muted/25 p-4">
                          {browserMode === 'cdp' && (
                            <div className="space-y-4">
                              <div className="flex flex-wrap items-center justify-between gap-3">
                                <div>
                                  <p className="text-sm font-medium">Local Chrome connection</p>
                                  <p className="mt-0.5 text-xs text-muted-foreground">CDP keeps normal actions in the background; creating or switching tabs may bring visible Chrome forward.</p>
                                </div>
                                <div className="flex items-center gap-2">
                                  <label className="text-xs text-muted-foreground">Port</label>
                                  <input
                                    type="number"
                                    value={cdpPort}
                                    onChange={(e) => setCdpPort(parseInt(e.target.value) || 9222)}
                                    className="h-8 w-20 rounded-md border border-border bg-background px-2 text-sm text-foreground outline-none focus:border-primary"
                                    min={1}
                                    max={65535}
                                  />
                                  <button
                                    type="button"
                                    onClick={() => checkCdpConnection(cdpPort)}
                                    disabled={cdpChecking}
                                    className="h-8 rounded-md border border-border bg-background px-3 text-xs font-medium text-foreground transition-colors hover:bg-muted disabled:opacity-50"
                                  >
                                    {cdpChecking ? 'Checking...' : 'Check'}
                                  </button>
                                </div>
                              </div>

                              <div className="flex items-start gap-2 rounded-md border border-border bg-background/70 px-3 py-2">
                                {cdpChecking ? (
                                  <>
                                    <div className="mt-1 h-2 w-2 shrink-0 rounded-full bg-amber-400 animate-pulse" />
                                    <span className="text-xs text-amber-300">Checking port {cdpPort}...</span>
                                  </>
                                ) : cdpConnected === true ? (
                                  <>
                                    <div className="mt-1 h-2 w-2 shrink-0 rounded-full bg-emerald-400" />
                                    <span className="text-xs text-emerald-300">Connected on port {cdpPort}.</span>
                                  </>
                                ) : cdpConnected === false ? (
                                  <>
                                    <div className="mt-1 h-2 w-2 shrink-0 rounded-full bg-red-400" />
                                    <span className="text-xs text-red-300">
                                      Not reachable on port {cdpPort}.{cdpError ? ` ${cdpError}` : ''}
                                    </span>
                                  </>
                                ) : (
                                  <span className="text-xs text-muted-foreground">Check the connection before saving CDP mode.</span>
                                )}
                              </div>

                              <div className="grid gap-3 md:grid-cols-2">
                                {navigator.platform?.includes('Mac') && (
                                  <div className="rounded-md border border-border bg-background/70 p-3">
                                    <p className="text-xs font-medium">macOS launcher</p>
                                    <p className="mt-1 text-xs text-muted-foreground">Install a dedicated Chrome CDP app profile.</p>
                                    <code className="mt-2 block rounded-md border border-border bg-black/30 px-2 py-1.5 text-[10px] text-emerald-300 break-all">
                                      {chromeCdpInstallCommand(cdpPort)}
                                    </code>
                                    <p className="mt-2 rounded-md border border-amber-500/25 bg-amber-500/10 px-2 py-1.5 text-[11px] leading-snug text-amber-200">
                                      The installer clears quarantine, signs locally, opens the app, and checks port {cdpPort}. If macOS still blocks first launch, allow Chrome CDP in Privacy &amp; Security and open it again.
                                    </p>
                                    <a
                                      href={chromeCdpZipUrl}
                                      download="Chrome-CDP-macOS.zip"
                                      target="_blank"
                                      rel="noopener noreferrer"
                                      onClick={(e) => e.stopPropagation()}
                                      className="mt-2 inline-flex items-center gap-1.5 rounded-md bg-emerald-600 px-2.5 py-1.5 text-xs font-medium text-white transition-colors hover:bg-emerald-500"
                                    >
                                      <Download className="h-3.5 w-3.5" />
                                      Download app
                                    </a>
                                  </div>
                                )}
                                <div className="rounded-md border border-border bg-background/70 p-3">
                                  <p className="text-xs font-medium">Terminal launch</p>
                                  <code className="mt-2 block rounded-md border border-border bg-black/30 px-2 py-1.5 text-[10px] text-emerald-300 break-all">
                                    {chromeCdpLaunchCommand(cdpPort, navigator.platform)}
                                  </code>
                                  <p className="mt-2 text-xs font-medium">Verify</p>
                                  <code className="mt-1 block rounded-md border border-border bg-black/30 px-2 py-1.5 text-[10px] text-sky-300 break-all">
                                    {chromeCdpVerifyCommand(cdpPort)}
                                  </code>
                                </div>
                              </div>
                            </div>
                          )}

                          {browserMode === 'headless' && (
                            <div className="space-y-2">
                              <p className="text-sm font-medium">Headless browser</p>
                              <p className="text-xs leading-5 text-muted-foreground">
                                Powered by{' '}
                                <a
                                  href="https://github.com/vercel/agent-browser"
                                  target="_blank"
                                  rel="noopener noreferrer"
                                  className="text-primary hover:underline"
                                >
                                  agent-browser
                                </a>
                                {' '}by Vercel, running inside a Docker container. The agent navigates Chromium in the background with no visible browser window.
                              </p>
                            </div>
                          )}

                          {browserMode === 'none' && (
                            <p className="text-xs text-muted-foreground">Select a mode to see configuration options.</p>
                          )}
                        </div>
                      </div>

                      {/* Footer */}
                      <div className="flex items-center justify-between gap-3 border-t border-border bg-muted/20 px-5 py-3">
                        <button
                          type="button"
                          onClick={() => {
                            setBrowserMode('none')
                            setShowCdpPopup(false)
                            setWorkspaceMinimized(false)
                          }}
                          className="rounded-md px-3 py-2 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                        >
                          Disable Browser
                        </button>
                        <button
                          type="button"
                          onClick={() => { setShowCdpPopup(false); setWorkspaceMinimized(false) }}
                          disabled={browserMode === 'cdp' && cdpConnected !== true}
                          className="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:cursor-not-allowed disabled:opacity-50"
                        >
                          {browserMode === 'cdp' && cdpConnected !== true ? (cdpChecking ? 'Checking...' : 'Connect Chrome First') : 'Done'}
                        </button>
                      </div>
                    </div>
                  </div>
                )}

                {/* Status text - removed observer initialization message */}
              </div>
              {/* Show old buttons */}
              {(
                <div className="flex items-center gap-1">
                  {isSummarizing ? (
                    <div className="flex items-center gap-2 px-3 py-1.5 text-sm text-gray-600 dark:text-gray-400">
                      <Loader2 className="w-4 h-4 animate-spin" />
                      <span>Summarizing...</span>
                    </div>
                  ) : (
                    <div data-tour="chat-send-controls" data-testid="tour-chat-send-controls" className="flex items-center gap-1">
                      {!isProductSurface && showRunChatAction && (
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <Button
                              type="button"
                              variant="outline"
                              onClick={() => { void handleStartRunChat() }}
                              className="h-7 shrink-0 gap-1.5 px-2 text-[11px] text-muted-foreground"
                              data-testid="chat-start-run-mode-button"
                              aria-label="Start chat in Run mode"
                            >
                              <Play className="h-3.5 w-3.5" />
                              Run chat
                            </Button>
                          </TooltipTrigger>
                          <TooltipContent>
                            <p>Start a separate chat with the Run-mode prompt and permissions</p>
                          </TooltipContent>
                        </Tooltip>
                      )}
                      {!sparkQuillComposerLayout && sparkleEl}
                      {!sparkQuillComposerLayout && attachmentEl}
                      {sparkQuillComposerLayout && micEl}
                      {/* Sending during a running turn is supported: routeSubmit
                          queues the message and the live-delivery effect steers
                          it into the turn. Hiding the button hid that. */}
                      {(
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <Button
                              type="button"
                              onClick={handleSendButtonClick}
                              disabled={submitButtonDisabled}
                              size="icon"
                              className={isProductSurface ? 'h-7 w-7 bg-violet-600 p-0 text-white hover:bg-violet-500' : 'h-7 w-7 p-0'}
                              data-testid="chat-submit-button"
                              aria-label="Send message"
                            >
                              <Send className="w-3.5 h-3.5" />
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
          disabled={isSummarizing || isUploadingFiles}
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
        agentProfileId={activeTab?.metadata?.agentProfileId}
        {...(isProductSurface ? {} : {
          onManageCommands: handleManageCommands,
          onEditCommand: handleEditCommand,
          onDeleteCommand: handleDeleteCommand,
        })}
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
