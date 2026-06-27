import { useEffect, useMemo, useState } from 'react'
import { Terminal, CheckCircle, AlertCircle, Loader2 } from 'lucide-react'
import { Button } from '../ui/Button'
import { Card } from '../ui/Card'
import { TierModelSelector } from '../ui/TierModelSelector'
import { DynamicModelSelector } from '../ui/DynamicModelSelector'
import { CodingAgentCapabilities } from './CodingAgentCapabilities'
import { useLLMStore } from '../../stores'
import { llmConfigService, type ModelMetadata, type ProviderManifestEntry } from '../../services/llm-config-api'
import { providerKeysApi, type StoredProviderKeys } from '../../api/scheduler'

interface CodingAgentSectionProps {
  provider: ProviderManifestEntry
  onPublished?: () => void
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

export function CodingAgentSection({ provider, onPublished }: CodingAgentSectionProps) {
  const {
    saveLLM,
    savedLLMs,
  } = useLLMStore()
  const [selectedModel, setSelectedModel] = useState(provider.default_model_id || provider.id)
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

  const handleTestConnection = async () => {
    setTestStatus('testing')
    setTestMessage(null)
    try {
      const response = await llmConfigService.validateAPIKey({
        provider: provider.id as Parameters<typeof llmConfigService.validateAPIKey>[0]['provider'],
        model_id: selectedModel !== provider.id ? selectedModel : undefined,
        ...(provider.id === 'pi-cli' && piAuthSpec && piAuthKey.trim() ? { api_key: piAuthKey.trim() } : {}),
      })
      if (response.valid) {
        setTestStatus('valid')
        setTestMessage(response.message || `${provider.display_name} is working.`)
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
    const value = piAuthKey.trim()
    const payload: StoredProviderKeys = {
      pi_provider_keys: {
        [piAuthSpec.providerKey]: value || '__DELETE__',
      },
    }
    if (piAuthSpec.topLevelKey) {
      ;(payload as Record<string, unknown>)[piAuthSpec.topLevelKey] = value || '__DELETE__'
    }
    try {
      await providerKeysApi.save(payload)
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

      const llmModel = {
        provider: provider.id as Parameters<typeof saveLLM>[0]['provider'],
        model_id: selectedModel,
        ...(Object.keys(options).length > 0 ? { options } : {}),
      }
      const displayModelName = currentModelMetadata?.model_name || provider.display_name
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
    if (showEffort) return `${provider.display_name} (${modelName}, ${effortLevel} effort)`
    if (selectedModel === provider.id) return provider.display_name
    return `${provider.display_name} — ${modelName}`
  }

  return (
    <div className="space-y-5">
      <div className="flex items-center justify-between">
        <h3 className="text-lg font-semibold text-foreground">{provider.display_name}</h3>
      </div>

      {/* Info card */}
      <Card className="p-4">
        <div className="flex items-start gap-3">
          <Terminal className="w-5 h-5 text-primary mt-0.5 flex-shrink-0" />
          <div className="space-y-1">
            <h4 className="font-medium text-foreground">{provider.auth_description}</h4>
            <p className="text-sm text-muted-foreground">{provider.description}</p>
          </div>
        </div>
      </Card>

      {/* Capabilities */}
      <CodingAgentCapabilities provider={provider.id} modelId={selectedModel} />

      {/* Model selection */}
      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-3">Model</h4>
        {provider.id === 'pi-cli' && (
          <p className="mb-3 text-xs leading-relaxed text-muted-foreground">
            The picker shows our latest supported Gemini, Z.AI, MiniMax, Kimi, and DeepSeek shortlist. Paste any other Pi model id in custom mode.
          </p>
        )}
        {isDynamic ? (
          <DynamicModelSelector
            provider={provider.id}
            selectedModelId={selectedModel}
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
                  disabled={piAuthLoading || piAuthStatus === 'saving'}
                />
                <Button
                  variant="outline"
                  size="sm"
                  onClick={handleSavePiAuth}
                  disabled={piAuthLoading || piAuthStatus === 'saving'}
                >
                  {piAuthStatus === 'saving' ? <Loader2 className="w-4 h-4 animate-spin" /> : 'Save'}
                </Button>
              </div>
            </div>
            <p className="text-xs leading-relaxed text-muted-foreground">
              {piAuthSpec.help} Stored encrypted in workspace provider auth and exported to Pi as {piAuthSpec.envNames.join(' / ')}.
            </p>
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
      {showEffort && (
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
        <p className="text-sm text-muted-foreground mb-3">
          Sends a test prompt to verify the CLI is installed and authenticated.
        </p>
        <div className="space-y-3">
          <Button
            variant="outline"
            size="sm"
            onClick={handleTestConnection}
            disabled={testStatus === 'testing'}
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

      {/* Publish to library */}
      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-3">Publish to Library</h4>

        {alreadyPublished && !isPublishing && (
          <div className="flex items-center gap-2 text-sm text-green-600 dark:text-green-400 mb-3">
            <CheckCircle className="w-4 h-4" />
            {provider.display_name} is already published in your library.
          </div>
        )}

        {publishStatus === 'success' && !isPublishing && (
          <div className="flex items-center gap-2 text-sm text-green-600 dark:text-green-400 mb-3">
            <CheckCircle className="w-4 h-4" />
            Published successfully!
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
          >
            Publish to Library
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
                disabled={!publishName.trim() || isSubmitting}
                size="sm"
              >
                {isSubmitting ? (
                  <><Loader2 className="w-3 h-3 mr-1 animate-spin" />Publishing...</>
                ) : (
                  'Publish'
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
    </div>
  )
}
