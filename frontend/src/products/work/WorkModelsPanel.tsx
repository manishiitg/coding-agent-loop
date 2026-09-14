import { useCallback, useEffect, useMemo, useState } from 'react'
import { BrainCircuit } from 'lucide-react'
import { TierModelSelector } from '../../components/ui/TierModelSelector'
import { AskAIButton } from '../../components/workflow/AskAIButton'
import WorkflowLLMConfigurationPanel from '../../components/workflow/WorkflowLLMConfigurationPanel'
import type { LLMProvider, PresetLLMConfig } from '../../services/api-types'
import type { ModelMetadata } from '../../services/llm-config-api'
import { useChatStore } from '../../stores/useChatStore'
import { useLLMStore } from '../../stores/useLLMStore'
import { buildAgentProfileEngineGroups, loadAgentProfileProviderOptions, type AgentProfileProviderOption } from '../../utils/agentProfileCapabilities'
import { WORK_PROFILE_ID, WORK_PROFILE_VERSION } from './workData'
import { workLLMSelectionFromConfig } from './workSessions'
import type { WorkRuntimeSelection } from './workTabs'

export function WorkModelsPanel({
  tabId,
  workspacePath,
  onAsk,
  projectLLMConfig,
  onRuntimeChange,
}: {
  tabId: string
  workspacePath: string
  onAsk: (message: string) => void
  projectLLMConfig?: PresetLLMConfig
  onRuntimeChange: (selection: WorkRuntimeSelection) => void | Promise<void>
}) {
  const tab = useChatStore(state => state.chatTabs[tabId])
  const events = useChatStore(state => tab?.sessionId ? state.tabEvents[tab.sessionId] : undefined)
  const activeRuntime = useChatStore(state => tab?.sessionId
    ? state.activeSessionsCache.find(session => session.session_id === tab.sessionId)?.runtime
    : undefined)
  const providerManifest = useLLMStore(state => state.providerManifest)
  const providerManifestLoaded = useLLMStore(state => state.providerManifestLoaded)
  const loadProviderManifest = useLLMStore(state => state.loadProviderManifest)
  const [options, setOptions] = useState<AgentProfileProviderOption[]>([])

  useEffect(() => {
    let cancelled = false
    void loadAgentProfileProviderOptions(WORK_PROFILE_ID, WORK_PROFILE_VERSION).then(loaded => {
      if (!cancelled) setOptions(loaded)
    })
    return () => { cancelled = true }
  }, [])

  useEffect(() => {
    if (!providerManifestLoaded) void loadProviderManifest()
  }, [loadProviderManifest, providerManifestLoaded])

  const modelCatalog = useMemo(
    () => providerManifest.flatMap(provider => provider.models || []),
    [providerManifest],
  )
  const engineGroups = useMemo(
    // Work intentionally offers the full platform catalog for each CLI. The
    // profile's model list may be present in an older running server until it
    // restarts, so do not let that stale curation hide the new project picker.
    () => buildAgentProfileEngineGroups(options.map(option => ({ ...option, models: undefined })), modelCatalog),
    [modelCatalog, options],
  )
  const workProviderIds = useMemo(
    () => options.map(option => option.provider || option.id),
    [options],
  )

  const hasStarted = events?.some(event => event.type === 'user_message') ?? false
  const savedSelection = workLLMSelectionFromConfig(projectLLMConfig)
  const savedOption = options.find(option => option.provider === savedSelection?.provider)
  const runtimeOption = options.find(option => option.provider === activeRuntime?.provider)
  const metadataOption = options.find(option => option.id === tab?.metadata?.agentProfileEngine)
  const selectedOption = savedOption
    || metadataOption
    || runtimeOption
    || options.find(option => option.default)
    || options[0]
  const llmConfig = useMemo<PresetLLMConfig | undefined>(() => selectedOption?.provider ? {
    schema_version: 2,
    mode: 'provider_profile',
    provider: selectedOption.provider as LLMProvider,
  } : undefined, [selectedOption])
  const defaultForOption = useCallback((option: AgentProfileProviderOption | undefined) => {
    if (!option) return { modelId: '', reasoningEffort: undefined as string | undefined }
    const defaults = providerManifest.find(provider => provider.id === option.provider)?.default_tier_models?.builder
    return {
      modelId: defaults?.model_id || option.model_id || '',
      reasoningEffort: typeof defaults?.options?.reasoning_effort === 'string'
        ? defaults.options.reasoning_effort
        : typeof option.options?.reasoning_effort === 'string'
          ? option.options.reasoning_effort
          : option.reasoning_efforts?.[0],
    }
  }, [providerManifest])

  const currentGroup = engineGroups.find(group => group.option.id === selectedOption?.id)
  const selectedDefaults = defaultForOption(selectedOption)
  const metadataMatchesSelectedProvider = tab?.metadata?.agentProfileEngine === selectedOption?.id
  const currentModelId = savedSelection?.modelId
    || (metadataMatchesSelectedProvider ? tab?.metadata?.agentProfileModelID : undefined)
    || activeRuntime?.model_id
    || selectedDefaults.modelId
  const selectableModels = useMemo(() => {
    const metadataById = new Map(modelCatalog.map(model => [model.model_id, model]))
    return (currentGroup?.models || []).map(({ id, label }) => metadataById.get(id) || {
      model_id: id,
      model_name: label,
      provider: selectedOption?.provider || '',
      context_window: 0,
      input_cost_per_1m: 0,
      output_cost_per_1m: 0,
    } satisfies ModelMetadata)
  }, [currentGroup?.models, modelCatalog, selectedOption?.provider])

  const selectProvider = (config: PresetLLMConfig) => {
    const option = options.find(candidate => candidate.provider === config.provider)
    if (!option) return
    const defaults = defaultForOption(option)
    void onRuntimeChange({
      engine: option.id,
      provider: option.provider,
      modelId: defaults.modelId,
      reasoningEffort: defaults.reasoningEffort,
    })
  }

  const selectModel = (modelId: string) => {
    if (!selectedOption) return
    const metadataMatchesProvider = tab?.metadata?.agentProfileEngine === selectedOption.id
    void onRuntimeChange({
      engine: selectedOption.id,
      provider: selectedOption.provider,
      modelId,
      reasoningEffort: (metadataMatchesProvider ? tab?.metadata?.agentProfileReasoningEffort : undefined) || selectedDefaults.reasoningEffort,
    })
  }

  return (
    <section className="flex h-full min-h-0 w-full flex-col bg-background">
      <header className="flex shrink-0 items-start gap-3 border-b px-4 py-3">
        <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
          <BrainCircuit className="h-4 w-4" />
        </div>
        <div className="min-w-0 flex-1">
          <h2 className="text-sm font-semibold text-foreground">Project agent configuration</h2>
          <p className="mt-0.5 text-xs text-muted-foreground">Choose the coding agent and model this project uses. You can change either at any time.</p>
        </div>
        <AskAIButton
          workspacePath={workspacePath}
          message="Help me choose between the coding agents available for this project. Explain the practical differences before changing anything."
          onAsk={onAsk}
          className="flex shrink-0 items-center gap-1.5 self-center rounded-md border border-border px-2.5 py-1.5 text-xs font-medium text-muted-foreground transition-colors hover:border-primary/40 hover:text-primary"
        />
      </header>
      <div className="min-h-0 flex-1 overflow-y-auto p-4">
        <WorkflowLLMConfigurationPanel
          workspacePath={workspacePath}
          llmConfig={llmConfig}
          onChange={selectProvider}
          scopeNoun="project"
          canWriteOverride
          allowedProviderIds={workProviderIds}
          splitPiProviders={false}
          showModelsPerRole={false}
        />
        <section className="mt-4 rounded-xl border border-border bg-card p-4">
          <h3 className="text-sm font-semibold text-foreground">Model</h3>
          <p className="mt-1 text-xs text-muted-foreground">The provider's Builder model is selected by default. You can change the model at any time.</p>
          <TierModelSelector
            models={selectableModels}
            selectedModelId={currentModelId}
            onSelect={selectModel}
            className="mt-3"
          />
        </section>
        {hasStarted && <p className="mt-3 text-xs text-muted-foreground">Changing the coding agent or model relaunches this project's retained session on the next message while keeping the project chat history.</p>}
      </div>
    </section>
  )
}
