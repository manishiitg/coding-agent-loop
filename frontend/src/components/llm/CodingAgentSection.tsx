import { useEffect, useMemo, useState } from 'react'
import { Terminal, CheckCircle, AlertCircle, Loader2 } from 'lucide-react'
import { Button } from '../ui/Button'
import { Card } from '../ui/Card'
import { TierModelSelector } from '../ui/TierModelSelector'
import { DynamicModelSelector } from '../ui/DynamicModelSelector'
import { CodingAgentCapabilities } from './CodingAgentCapabilities'
import { useLLMStore } from '../../stores'
import { READ_ONLY_TITLE } from '../../hooks/useCanWriteWorkflow'
import { llmConfigService, type ModelMetadata, type ProviderManifestEntry } from '../../services/llm-config-api'
import { providerKeysApi, type StoredProviderKeys } from '../../api/scheduler'

interface CodingAgentSectionProps {
  provider: ProviderManifestEntry
  onPublished?: () => void
  /** Scopes this section to one Pi CLI backend group (e.g. "Gemini") -- the
   * `provider` prop stays the shared pi-cli manifest entry across every
   * group tab, so display text and default-model selection must key off
   * this instead of `provider` fields wherever they'd otherwise say
   * "Pi CLI" or default to the pi-cli-wide model. */
  groupFilter?: string
  /** The current user can't change workflow state: test, key save, and
   * publish disable. Model/effort pickers stay usable since they're local
   * state until published. */
  readOnly?: boolean
  /** 'library' (default) is the full LLM-library editor: model + effort
   * pickers and Save-to-Library. 'workflow' is the workflow panel's
   * drill-in, where the only decisions are "does this provider work here"
   * and "use it for this workflow": model/effort come from the provider's
   * tier defaults, so those pickers and the publish card are hidden. */
  variant?: 'library' | 'workflow'
  /** Model to test against when the picker is hidden (workflow variant):
   * the row's default model, which for a Pi group also selects which
   * provider key the auth card asks for. */
  initialModelId?: string
  /** True when this provider is already the workflow's selection. */
  inUse?: boolean
  /** Renders the "Use in this workflow" action (workflow variant). */
  onUseInWorkflow?: () => void | Promise<void>
  /** User-facing owner of the provider choice. */
  scopeNoun?: string
}

type PiTopLevelProviderKey =
  | 'pi_cli'
  | 'openai'
  | 'anthropic'
  | 'openrouter'
  | 'zai'
  | 'kimi'
  | 'minimax'

type PiAuthSpec = {
  providerKey: string
  label: string
  envNames: string[]
  topLevelKey?: PiTopLevelProviderKey
  help: string
}

function piProviderPrefix(modelId: string): string {
  const trimmed = modelId.trim()
  if (!trimmed || trimmed === 'pi-cli' || trimmed === 'auto') return 'google'
  const slash = trimmed.indexOf('/')
  if (slash > 0) return trimmed.slice(0, slash).trim().toLowerCase()
  return 'google'
}

function genericPiEnvName(providerKey: string): string {
  return `${providerKey.trim().toUpperCase().replace(/[-.]/g, '_')}_API_KEY`
}

function piAuthSpecForModel(modelId: string): PiAuthSpec {
  const prefix = piProviderPrefix(modelId)
  switch (prefix) {
    case 'google':
    case 'google-vertex':
      return {
        providerKey: 'google',
        topLevelKey: 'pi_cli',
        label: 'Gemini / Google AI Studio API key',
        envNames: ['GEMINI_API_KEY', 'GOOGLE_API_KEY', 'PI_API_KEY'],
        help: 'Used by Pi for Google Gemini models.',
      }
    case 'openai':
      return {
        providerKey: 'openai',
        topLevelKey: 'openai',
        label: 'OpenAI API key',
        envNames: ['OPENAI_API_KEY'],
        help: 'Used by Pi for OpenAI-routed models.',
      }
    case 'anthropic':
      return {
        providerKey: 'anthropic',
        topLevelKey: 'anthropic',
        label: 'Anthropic API key',
        envNames: ['ANTHROPIC_API_KEY'],
        help: 'Used by Pi for Anthropic-routed models.',
      }
    case 'openrouter':
      return {
        providerKey: 'openrouter',
        topLevelKey: 'openrouter',
        label: 'OpenRouter API key',
        envNames: ['OPENROUTER_API_KEY'],
        help: 'Used by Pi for OpenRouter models.',
      }
    case 'deepseek':
      return {
        providerKey: 'deepseek',
        label: 'DeepSeek API key',
        envNames: ['DEEPSEEK_API_KEY'],
        help: 'Used by Pi for DeepSeek models.',
      }
    case 'xai':
      return {
        providerKey: 'xai',
        label: 'xAI (Grok) API key',
        envNames: ['XAI_API_KEY'],
        help: 'Used by Pi for xAI Grok models.',
      }
    case 'zai':
      return {
        providerKey: 'zai',
        topLevelKey: 'zai',
        label: 'Z.AI API key',
        envNames: ['ZAI_API_KEY'],
        help: 'Used by Pi for ZAI Coding Plan global models.',
      }
    case 'zai-coding-cn':
      return {
        providerKey: 'zai-coding-cn',
        label: 'Z.AI China API key',
        envNames: ['ZAI_CODING_CN_API_KEY'],
        help: 'Used by Pi for ZAI Coding Plan China models.',
      }
    case 'opencode':
    case 'opencode-go':
      return {
        providerKey: prefix,
        label: 'OpenCode API key',
        envNames: ['OPENCODE_API_KEY'],
        help: 'Used by Pi for OpenCode-routed models.',
      }
    case 'kimi-coding':
    case 'moonshotai':
    case 'moonshotai-cn':
      return {
        providerKey: prefix === 'kimi-coding' ? 'kimi-coding' : prefix,
        topLevelKey: 'kimi',
        label: 'Kimi API key',
        envNames: ['KIMI_API_KEY'],
        help: 'Used by Pi for Kimi/Moonshot coding models.',
      }
    case 'minimax':
      return {
        providerKey: 'minimax',
        topLevelKey: 'minimax',
        label: 'MiniMax API key',
        envNames: ['MINIMAX_API_KEY'],
        help: 'Used by Pi for MiniMax models.',
      }
    case 'minimax-cn':
      return {
        providerKey: 'minimax-cn',
        label: 'MiniMax China API key',
        envNames: ['MINIMAX_CN_API_KEY'],
        help: 'Used by Pi for MiniMax China models.',
      }
    default:
      return {
        providerKey: prefix,
        label: `${prefix} API key`,
        envNames: [genericPiEnvName(prefix)],
        help: 'Used by Pi for the selected custom provider prefix.',
      }
  }
}

function piAuthValue(keys: StoredProviderKeys | undefined, spec: PiAuthSpec): string {
  if (!keys) return ''
  const providerKey = keys.pi_provider_keys?.[spec.providerKey]
  if (providerKey?.trim()) return providerKey
  if (spec.topLevelKey) {
    const value = keys[spec.topLevelKey]
    if (typeof value === 'string' && value.trim()) return value
  }
  return ''
}

export function CodingAgentSection({ provider, onPublished, groupFilter, readOnly = false, variant = 'library', initialModelId, inUse = false, onUseInWorkflow, scopeNoun = 'workflow' }: CodingAgentSectionProps) {
  const saveLLM = useLLMStore(state => state.saveLLM)
  const savedLLMs = useLLMStore(state => state.savedLLMs)
  const workflowMode = variant === 'workflow'
  // A group-scoped tab has no valid pi-cli-wide default to start from --
  // DynamicModelSelector picks the group's own default once its catalog
  // loads (see its groupFilter-aware auto-select effect). In the workflow
  // variant that picker is hidden, so the caller supplies the row's default
  // model up front instead.
  const [selectedModel, setSelectedModel] = useState(initialModelId ?? (groupFilter ? '' : (provider.default_model_id || provider.id)))
  const [using, setUsing] = useState(false)
  const displayName = groupFilter || provider.display_name
  const [effortLevel, setEffortLevel] = useState('high')
  const [isPublishing, setIsPublishing] = useState(false)
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [publishName, setPublishName] = useState('')
  const [publishStatus, setPublishStatus] = useState<'idle' | 'success' | 'error'>('idle')
  const [publishError, setPublishError] = useState<string | null>(null)
  const [testStatus, setTestStatus] = useState<'idle' | 'testing' | 'valid' | 'invalid'>('idle')
  const [testMessage, setTestMessage] = useState<string | null>(null)
  const [piAuthKey, setPiAuthKey] = useState('')
  const [piAuthLoading, setPiAuthLoading] = useState(false)
  const [piAuthStatus, setPiAuthStatus] = useState<'idle' | 'saving' | 'saved' | 'error'>('idle')
  const [piAuthError, setPiAuthError] = useState<string | null>(null)

  const models = useMemo(() => provider.models || [], [provider.models])
  const currentModelMetadata = models.find(m => m.model_id === selectedModel) as ModelMetadata | undefined
  const isDynamic = provider.model_selection_mode === 'dynamic'
  const piAuthSpec = useMemo(
    () => provider.id === 'pi-cli' ? piAuthSpecForModel(selectedModel) : null,
    [provider.id, selectedModel]
  )

  const effortLevels = currentModelMetadata?.reasoning_effort_levels?.length
    ? currentModelMetadata.reasoning_effort_levels
    : ['low', 'medium', 'high', 'max']
  const showEffort = currentModelMetadata?.supports_reasoning_effort

  useEffect(() => {
    if (isDynamic || models.length === 0) return
    const exists = models.some(m => m.model_id === selectedModel)
    if (!exists) setSelectedModel(models[0].model_id)
  }, [models, selectedModel, isDynamic])

  useEffect(() => {
    if (!piAuthSpec) return
    let cancelled = false
    setPiAuthLoading(true)
    setPiAuthError(null)
    setPiAuthStatus('idle')
    providerKeysApi.load()
      .then(keys => {
        if (cancelled) return
        setPiAuthKey(piAuthValue(keys, piAuthSpec))
      })
      .catch(() => {
        if (cancelled) return
        setPiAuthKey('')
      })
      .finally(() => {
        if (!cancelled) setPiAuthLoading(false)
      })
    return () => { cancelled = true }
  }, [piAuthSpec])

  const alreadyPublished = savedLLMs.some(
    llm => llm.provider === provider.id && llm.model_id === selectedModel
  )

  // claude-code and codex-cli already get every model x reasoning-effort
  // combination auto-published server-side (see buildAutoPublishedCodingAgentLLMs
  // in published_llm_store.go) -- manual publish for these two just adds
  // near-duplicate clutter to the saved-configs list. cursor-cli isn't
  // auto-published and has real model variation worth naming (Grok,
  // Composer), so it keeps publish.
  const canPublish = provider.id !== 'claude-code' && provider.id !== 'codex-cli'

  const savePiAuthKey = async () => {
    if (!piAuthSpec) return
    const value = piAuthKey.trim()
    const payload: StoredProviderKeys = {
      pi_provider_keys: {
        [piAuthSpec.providerKey]: value || '__DELETE__',
      },
    }
    if (piAuthSpec.topLevelKey) {
      ;(payload as Record<string, unknown>)[piAuthSpec.topLevelKey] = value || '__DELETE__'
    }
    await providerKeysApi.save(payload)
  }

  const handleTestConnection = async () => {
    setTestStatus('testing')
    setTestMessage(null)
    try {
      const response = await llmConfigService.validateAPIKey({
        provider: provider.id as Parameters<typeof llmConfigService.validateAPIKey>[0]['provider'],
        model_id: selectedModel && selectedModel !== provider.id ? selectedModel : undefined,
        ...(provider.id === 'pi-cli' && piAuthSpec && piAuthKey.trim() ? { api_key: piAuthKey.trim() } : {}),
      })
      if (response.valid) {
        let successMessage = response.message || `${displayName} is working.`
        if (provider.id === 'pi-cli' && piAuthSpec && piAuthKey.trim()) {
          await savePiAuthKey()
          setPiAuthStatus('saved')
          setTimeout(() => setPiAuthStatus('idle'), 2500)
          successMessage = `${successMessage} Provider key saved for runtime.`
        } else if (provider.id === 'pi-cli' && piAuthSpec) {
          successMessage = `${successMessage} No key was saved; this test used existing server or CLI auth. Add and save a provider key if this model must work on another machine.`
        }
        setTestStatus('valid')
        setTestMessage(successMessage)
      } else {
        setTestStatus('invalid')
        setTestMessage(response.message || response.error || 'Validation failed.')
      }
    } catch (err) {
      setTestStatus('invalid')
      setTestMessage(err instanceof Error ? err.message : 'Connection test failed.')
    }
  }

  const handleSavePiAuth = async () => {
    if (!piAuthSpec) return
    setPiAuthStatus('saving')
    setPiAuthError(null)
    try {
      await savePiAuthKey()
      setPiAuthStatus('saved')
      setTimeout(() => setPiAuthStatus('idle'), 2500)
    } catch (err) {
      setPiAuthStatus('error')
      setPiAuthError(err instanceof Error ? err.message : 'Failed to save provider key.')
    }
  }

  const handlePublishToLibrary = async () => {
    if (!publishName.trim()) return
    setIsSubmitting(true)
    setPublishError(null)
    try {
      const options: Record<string, unknown> = {}
      if (showEffort) options.reasoning_effort = effortLevel

      if (provider.id === 'pi-cli' && piAuthSpec && piAuthKey.trim()) {
        await savePiAuthKey()
        setPiAuthStatus('saved')
        setTimeout(() => setPiAuthStatus('idle'), 2500)
      }

      const llmModel = {
        provider: provider.id as Parameters<typeof saveLLM>[0]['provider'],
        model_id: selectedModel,
        ...(Object.keys(options).length > 0 ? { options } : {}),
      }
      const displayModelName = currentModelMetadata?.model_name || displayName
      await saveLLM(llmModel, publishName.trim(), displayModelName, 'none', currentModelMetadata)
      setPublishName('')
      setIsPublishing(false)
      setPublishStatus('success')
      onPublished?.()
      setTimeout(() => setPublishStatus('idle'), 3000)
    } catch (err) {
      setPublishError(err instanceof Error ? err.message : 'Failed to publish.')
      setPublishStatus('error')
    } finally {
      setIsSubmitting(false)
    }
  }

  const defaultPublishName = () => {
    const modelName = currentModelMetadata?.model_name || selectedModel
    if (showEffort) return `${displayName} (${modelName}, ${effortLevel} effort)`
    if (selectedModel === provider.id) return displayName
    return `${displayName} — ${modelName}`
  }

  return (
    <div className="space-y-5">
      <div className="flex items-center justify-between">
        <h3 className="text-lg font-semibold text-foreground">{displayName}</h3>
      </div>

      {/* Info card and capabilities: library only. The workflow drill-in is
          two actions (test, use); the provider blurb and capability grid
          belong to the library editor. */}
      {!workflowMode && (
      <Card className="p-4">
        <div className="flex items-start gap-3">
          <Terminal className="w-5 h-5 text-primary mt-0.5 flex-shrink-0" />
          <div className="space-y-1">
            <h4 className="font-medium text-foreground">{groupFilter ? 'API key' : provider.auth_description}</h4>
            <p className="text-sm text-muted-foreground">
              {groupFilter ? `Uses the latest supported ${groupFilter} models.` : provider.description}
            </p>
          </div>
        </div>
      </Card>

      )}
      {!workflowMode && <CodingAgentCapabilities provider={provider.id} modelId={selectedModel} />}

      {/* Model selection (library only: the workflow variant runs on the
          provider's tier defaults, pinned per role under Advanced if needed) */}
      {!workflowMode && (
      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-3">Model</h4>
        {provider.id === 'pi-cli' && (
          <p className="mb-3 text-xs leading-relaxed text-muted-foreground">
            {groupFilter
              ? `The picker shows our latest supported ${groupFilter} shortlist. Paste any other ${groupFilter} model id in custom mode.`
              : 'The picker shows our latest supported Gemini, Z.AI, MiniMax, Kimi, and DeepSeek shortlist. Paste any other Pi model id in custom mode.'}
          </p>
        )}
        {isDynamic ? (
          <DynamicModelSelector
            provider={provider.id}
            selectedModelId={selectedModel}
            groupFilter={groupFilter}
            onSelect={id => {
              setSelectedModel(id)
              setTestStatus('idle')
              setTestMessage(null)
            }}
          />
        ) : models.length > 0 ? (
          <>
            <TierModelSelector
              models={models}
              selectedModelId={selectedModel}
              onSelect={id => {
                setSelectedModel(id)
                setTestStatus('idle')
                setTestMessage(null)
              }}
            />
            {selectedModel !== provider.id && selectedModel !== provider.default_model_id && (
              <p className="mt-1.5 text-xs text-muted-foreground">
                Passes <code className="bg-secondary px-1 py-0.5 rounded">--model {selectedModel}</code> to the CLI.
              </p>
            )}
          </>
        ) : (
          <p className="text-sm text-muted-foreground">No models available for this provider.</p>
        )}
      </Card>
      )}

      {piAuthSpec && (
        <Card className="p-4">
          <h4 className="font-medium text-foreground mb-3">Provider Auth</h4>
          <div className="space-y-3">
            <div>
              <label className="block text-sm font-medium text-muted-foreground mb-2">{piAuthSpec.label}</label>
              <div className="flex gap-2">
                <input
                  type="password"
                  value={piAuthKey}
                  onChange={e => {
                    setPiAuthKey(e.target.value)
                    setPiAuthStatus('idle')
                    setPiAuthError(null)
                    setTestStatus('idle')
                    setTestMessage(null)
                  }}
                  placeholder={piAuthLoading ? 'Loading saved key...' : `Enter ${piAuthSpec.envNames[0]}`}
                  className="flex-1 px-3 py-2 text-sm bg-background border border-border rounded-md focus:outline-none focus:ring-1 focus:ring-primary"
                  disabled={readOnly || piAuthLoading || piAuthStatus === 'saving'}
                />
                <Button
                  variant="outline"
                  size="sm"
                  onClick={handleSavePiAuth}
                  disabled={readOnly || piAuthLoading || piAuthStatus === 'saving'}
                >
                  {piAuthStatus === 'saving' ? <Loader2 className="w-4 h-4 animate-spin" /> : 'Save'}
                </Button>
              </div>
            </div>
            {!workflowMode && (
            <p className="text-xs leading-relaxed text-muted-foreground">
              {piAuthSpec.help} Testing or saving this configuration stores the key encrypted for runtime, then exports it to Pi as {piAuthSpec.envNames.join(' / ')}.
            </p>
            )}
            {piAuthStatus === 'saved' && (
              <div className="flex items-start gap-2 text-sm text-green-600 dark:text-green-400">
                <CheckCircle className="w-4 h-4 mt-0.5 flex-shrink-0" />
                <span>Provider key saved.</span>
              </div>
            )}
            {piAuthStatus === 'error' && (
              <div className="flex items-start gap-2 text-sm text-red-500">
                <AlertCircle className="w-4 h-4 mt-0.5 flex-shrink-0" />
                <span>{piAuthError || 'Failed to save provider key.'}</span>
              </div>
            )}
          </div>
        </Card>
      )}

      {/* Effort/reasoning level (when model supports it) */}
      {showEffort && !workflowMode && (
        <Card className="p-4">
          <h4 className="font-medium text-foreground mb-3">Effort Level</h4>
          <select
            value={effortLevel}
            onChange={e => setEffortLevel(e.target.value)}
            className="w-full px-3 py-2 text-sm bg-background border border-border rounded-md focus:outline-none focus:ring-1 focus:ring-primary"
          >
            {effortLevels.map(level => (
              <option key={level} value={level}>
                {level.charAt(0).toUpperCase() + level.slice(1)}
              </option>
            ))}
          </select>
          <p className="mt-1.5 text-xs text-muted-foreground">
            Controls how deeply the model reasons.
          </p>
        </Card>
      )}

      {/* Test connection */}
      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-3">Test Connection</h4>
        {!workflowMode && (
        <p className="text-sm text-muted-foreground mb-3">
          Sends a test prompt to verify the CLI is installed and authenticated. For Pi, a typed key is saved encrypted after a successful test.
        </p>
        )}
        <div className="space-y-3">
          <Button
            variant="outline"
            size="sm"
            onClick={handleTestConnection}
            disabled={readOnly || testStatus === 'testing'}
            title={readOnly ? READ_ONLY_TITLE : undefined}
          >
            {testStatus === 'testing' ? (
              <><Loader2 className="w-4 h-4 mr-2 animate-spin" />Testing...</>
            ) : testStatus === 'valid' ? (
              <><CheckCircle className="w-4 h-4 mr-2 text-green-500" />Test Again</>
            ) : testStatus === 'invalid' ? (
              <><AlertCircle className="w-4 h-4 mr-2 text-red-500" />Retry Test</>
            ) : (
              'Test Connection'
            )}
          </Button>

          {testStatus === 'valid' && testMessage && (
            <div className="flex items-start gap-2 text-sm text-green-600 dark:text-green-400">
              <CheckCircle className="w-4 h-4 mt-0.5 flex-shrink-0" />
              <span>{testMessage}</span>
            </div>
          )}
          {testStatus === 'invalid' && testMessage && (
            <div className="flex items-start gap-2 text-sm text-red-500">
              <AlertCircle className="w-4 h-4 mt-0.5 flex-shrink-0" />
              <span>{testMessage}</span>
            </div>
          )}
        </div>
      </Card>

      {/* Use in this scope (workflow variant): the one decision this
          drill-in exists for. Test first is recommended but not enforced --
          a provider the status line already reports Ready needs no test. */}
      {workflowMode && onUseInWorkflow && (
      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-3">Use in this {scopeNoun}</h4>
        {inUse ? (
          <div className="flex items-center gap-2 text-sm text-green-600 dark:text-green-400">
            <CheckCircle className="w-4 h-4" />
            This {scopeNoun} already runs on {displayName}.
          </div>
        ) : (
          <div className="space-y-3">
            <Button
              size="sm"
              onClick={async () => {
                setUsing(true)
                try { await onUseInWorkflow() } finally { setUsing(false) }
              }}
              disabled={readOnly || using || (piAuthSpec !== null && !piAuthKey.trim() && piAuthStatus !== 'saved')}
              title={readOnly ? READ_ONLY_TITLE : piAuthSpec && !piAuthKey.trim() && piAuthStatus !== 'saved' ? 'Save the provider key first' : undefined}
            >
              {using ? <><Loader2 className="w-4 h-4 mr-2 animate-spin" />Saving…</> : `Use in this ${scopeNoun}`}
            </Button>
            {piAuthSpec && !piAuthKey.trim() && piAuthStatus !== 'saved' && (
              <p className="text-xs text-muted-foreground">Save a provider key above to enable this.</p>
            )}
          </div>
        )}
      </Card>
      )}

      {/* Save reusable configuration (library only) */}
      {canPublish && !workflowMode && (
      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-3">Save Configuration</h4>

        {alreadyPublished && !isPublishing && (
          <div className="flex items-center gap-2 text-sm text-green-600 dark:text-green-400 mb-3">
            <CheckCircle className="w-4 h-4" />
            This provider and model combination is already saved.
          </div>
        )}

        {publishStatus === 'success' && !isPublishing && (
          <div className="flex items-center gap-2 text-sm text-green-600 dark:text-green-400 mb-3">
            <CheckCircle className="w-4 h-4" />
            Configuration saved.
          </div>
        )}

        {publishError && (
          <div className="flex items-center gap-2 text-sm text-red-500 mb-3">
            <AlertCircle className="w-4 h-4" />
            {publishError}
          </div>
        )}

        {!isPublishing ? (
          <Button
            onClick={() => {
              setPublishName(defaultPublishName())
              setIsPublishing(true)
              setPublishError(null)
            }}
            size="sm"
            disabled={readOnly}
            title={readOnly ? READ_ONLY_TITLE : undefined}
          >
            Save to Library
          </Button>
        ) : (
          <div className="space-y-3">
            <div>
              <label className="block text-sm font-medium text-muted-foreground mb-1">Display Name</label>
              <input
                type="text"
                value={publishName}
                onChange={e => { setPublishName(e.target.value); setPublishError(null) }}
                className="w-full px-3 py-2 text-sm bg-background border border-border rounded-md focus:outline-none focus:ring-1 focus:ring-primary"
                placeholder="e.g., Claude Code"
                autoFocus
              />
            </div>
            <div className="flex gap-2">
              <Button
                onClick={handlePublishToLibrary}
                disabled={readOnly || !publishName.trim() || isSubmitting}
                title={readOnly ? READ_ONLY_TITLE : undefined}
                size="sm"
              >
                {isSubmitting ? (
                  <><Loader2 className="w-3 h-3 mr-1 animate-spin" />Saving...</>
                ) : (
                  'Save'
                )}
              </Button>
              <Button
                variant="outline"
                onClick={() => { setIsPublishing(false); setPublishError(null) }}
                disabled={isSubmitting}
                size="sm"
              >
                Cancel
              </Button>
            </div>
          </div>
        )}
      </Card>
      )}
    </div>
  )
}
