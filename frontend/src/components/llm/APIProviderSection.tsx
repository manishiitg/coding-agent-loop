import { useState, useEffect, useMemo } from 'react'
import { Key, CheckCircle, AlertCircle, Loader2, BookOpen, Globe, MapPin, RefreshCw } from 'lucide-react'
import { Button } from '../ui/Button'
import { Card } from '../ui/Card'
import { ModelSelector } from '../ui/ModelSelector'
import { ModelOptionsConfig } from './ModelOptionsConfig'
import { useLLMStore } from '../../stores'
import type { ExtendedLLMConfiguration } from '../../services/api-types'
import type { ModelMetadata, ProviderManifestEntry } from '../../services/llm-config-api'
import { llmConfigService } from '../../services/llm-config-api'

interface APIProviderSectionProps {
  provider: ProviderManifestEntry
  config: ExtendedLLMConfiguration
  onUpdate: (config: ExtendedLLMConfiguration) => void
  onTestAPIKey: (apiKey: string, modelId?: string, options?: Record<string, unknown>) => void
  apiKeyStatus: 'idle' | 'testing' | 'valid' | 'invalid' | 'timeout'
  apiKeyError: string | null
  metadata?: ModelMetadata[]
}

export function APIProviderSection({
  provider,
  config,
  onUpdate,
  onTestAPIKey,
  apiKeyStatus,
  apiKeyError,
  metadata = [],
}: APIProviderSectionProps) {
  const [apiKey, setApiKey] = useState(config.api_key || '')
  const [endpoint, setEndpoint] = useState(config.endpoint || '')
  const [region, setRegion] = useState(config.region || '')
  const [isPublishing, setIsPublishing] = useState(false)
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [publishName, setPublishName] = useState('')
  const [publishError, setPublishError] = useState<string | null>(null)
  const [azureModels, setAzureModels] = useState<string[]>([])
  const [isFetchingModels, setIsFetchingModels] = useState(false)
  const [ollamaModels, setOllamaModels] = useState<string[]>([])
  const [ollamaFetchError, setOllamaFetchError] = useState<string | null>(null)

  const { saveLLM, testAPIKey: testAPIKeyFromStore, lockedProviders, llmConfigLocked } = useLLMStore()
  const isLocked = llmConfigLocked || lockedProviders.includes(provider.id)
  const isAzure = provider.id === 'azure'
  const isBedrock = provider.id === 'bedrock'
  const isOllama = provider.id === 'ollama'
  const needsApiKey = provider.requires_api_key && !isBedrock

  useEffect(() => {
    if (config.api_key) setApiKey(config.api_key)
    if (config.endpoint) setEndpoint(config.endpoint)
    if (config.region) setRegion(config.region || (isBedrock ? 'us-east-1' : ''))
  }, [config.api_key, config.endpoint, config.region, isBedrock])

  const modelsFromMetadata = useMemo(
    () => metadata.filter(m => m.provider === provider.id).map(m => m.model_id),
    [metadata, provider.id]
  )
  const manifestModels = useMemo(
    () => (provider.models || []).map(m => m.model_id),
    [provider.models]
  )
  const allModels = useMemo(
    () => Array.from(new Set([...modelsFromMetadata, ...manifestModels, ...azureModels])),
    [modelsFromMetadata, manifestModels, azureModels]
  )

  const currentModelMetadata = metadata.find(m => m.model_id === config.model_id && m.provider === provider.id)
    || metadata.find(m => m.model_id === config.model_id)

  const handleAPIKeyChange = (newApiKey: string) => {
    setApiKey(newApiKey)
    onUpdate({ ...config, api_key: newApiKey })
    setPublishError(null)
  }

  const handleOptionsChange = (newOptions: Record<string, unknown>) => {
    onUpdate({ ...config, options: newOptions })
  }

  const handleRegionChange = (newRegion: string) => {
    setRegion(newRegion)
    onUpdate({ ...config, region: newRegion })
  }

  const handleEndpointChange = (newEndpoint: string) => {
    setEndpoint(newEndpoint)
    onUpdate({ ...config, endpoint: newEndpoint })
  }

  const fetchAzureModels = async () => {
    if (!endpoint.trim() || !apiKey.trim()) return
    setIsFetchingModels(true)
    try {
      const result = await llmConfigService.getAzureDeployedModels(endpoint, apiKey)
      if (result.models?.length) {
        setAzureModels(result.models.map(m => m.model_id))
      }
    } catch {
      // Silently fail — user can still type a model
    } finally {
      setIsFetchingModels(false)
    }
  }

  const fetchOllamaModels = async () => {
    setIsFetchingModels(true)
    setOllamaFetchError(null)
    try {
      const result = await llmConfigService.getOllamaModels(
        endpoint || 'http://localhost:11434',
        apiKey
      )
      const ids = (result.models || []).map((m: { model_id: string }) => m.model_id).filter(Boolean)
      if (ids.length > 0) {
        setOllamaModels(ids)
      } else {
        setOllamaFetchError('No models found. Make sure Ollama is running and has models pulled.')
      }
    } catch {
      setOllamaFetchError('Could not reach Ollama server. Check the URL and try again.')
    } finally {
      setIsFetchingModels(false)
    }
  }

  const generateDefaultName = (): string => {
    if (!config.model_id) return ''
    const parts: string[] = []
    const modelId = config.model_id.replace(/-20\d{6}/g, '').replace(/-20\d{2}-\d{2}-\d{2}/g, '')
    parts.push(modelId)
    if (currentModelMetadata?.supports_thinking_level) {
      parts.push(`thinking-${(config.options?.thinking_level as string) || 'high'}`)
    }
    if (currentModelMetadata?.supports_reasoning_effort) {
      parts.push(`reasoning-${(config.options?.reasoning_effort as string) || 'medium'}`)
    }
    if (currentModelMetadata?.supports_thinking_budget) {
      parts.push(`budget-${(config.options?.thinking_budget as number) || 1024}`)
    }
    return parts.join('-')
  }

  const handlePublishToLibrary = async () => {
    if (!publishName.trim() || !config.model_id) return
    if (needsApiKey && !apiKey.trim()) {
      setPublishError(`API key is required to publish ${provider.display_name} models`)
      return
    }

    setIsSubmitting(true)
    setPublishError(null)
    try {
      const ollamaOptions = isOllama
        ? { ...config.options, base_url: endpoint || 'http://localhost:11434' }
        : config.options
      const testResult = await testAPIKeyFromStore(
        provider.id as Parameters<typeof testAPIKeyFromStore>[0],
        needsApiKey || isOllama ? apiKey : '',
        config.model_id,
        isOllama ? ollamaOptions : config.options
      )
      if (testResult.valid) {
        const llmModel = {
          provider: provider.id as ExtendedLLMConfiguration['provider'],
          model_id: config.model_id,
          ...(needsApiKey && config.api_key ? { api_key: config.api_key } : {}),
          ...(isOllama && apiKey ? { api_key: apiKey } : {}),
          ...(config.region ? { region: config.region } : {}),
          ...(isOllama && endpoint ? { endpoint } : config.endpoint ? { endpoint: config.endpoint } : {}),
          options: isOllama
            ? { ...config.options, base_url: endpoint || 'http://localhost:11434' }
            : config.options,
        }
        await saveLLM(llmModel, publishName.trim(), currentModelMetadata?.model_name, needsApiKey ? 'api_key' : 'none', currentModelMetadata)
        setPublishName('')
        setIsPublishing(false)
        await onTestAPIKey(apiKey, config.model_id, isOllama ? { ...config.options, base_url: endpoint || 'http://localhost:11434' } : config.options)
      } else {
        setPublishError(testResult.error || 'Validation failed. Check your credentials and try again.')
      }
    } catch (err) {
      setPublishError(err instanceof Error ? err.message : 'Failed to publish.')
    } finally {
      setIsSubmitting(false)
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h3 className="text-lg font-semibold text-foreground">{provider.display_name}</h3>
        {isLocked && (
          <div className="flex items-center gap-1.5 px-2 py-1 bg-yellow-500/10 border border-yellow-500/20 rounded text-yellow-600 dark:text-yellow-500 text-xs font-medium">
            <Key className="w-3.5 h-3.5" />
            Locked by Admin
          </div>
        )}
      </div>

      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-4">Model Selection</h4>
        <div className="space-y-4">
          {isLocked && (
            <div className="text-xs text-muted-foreground bg-secondary/30 p-2 rounded border border-border/50 mb-2">
              Configuration for this provider is managed by your administrator and cannot be changed.
            </div>
          )}

          {!isOllama && (
            <div>
              <label className="block text-sm font-medium text-muted-foreground mb-2">Primary Model</label>
              <ModelSelector
                value={config.model_id}
                onChange={val => onUpdate({ ...config, model_id: val })}
                models={allModels}
                metadata={metadata}
                placeholder={`Select a ${provider.display_name} model`}
                disabled={isLocked}
              />
              {currentModelMetadata && (
                <ModelOptionsConfig
                  metadata={currentModelMetadata}
                  options={config.options || {}}
                  onChange={handleOptionsChange}
                  disabled={isLocked}
                />
              )}
            </div>
          )}

          {/* Azure-specific: Endpoint + Region */}
          {isAzure && (
            <div className="border-t border-border pt-4 space-y-3">
              <div className="flex items-center gap-2">
                <Globe className="w-4 h-4 text-muted-foreground" />
                <h5 className="text-sm font-medium text-foreground">Azure Endpoint</h5>
              </div>
              <input
                type="text"
                value={endpoint}
                onChange={e => handleEndpointChange(e.target.value)}
                placeholder="https://your-resource.openai.azure.com"
                className="w-full px-3 py-2 border border-border rounded-md bg-background text-foreground focus:ring-2 focus:ring-primary"
                disabled={isLocked}
              />
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="block text-xs font-medium text-muted-foreground mb-1">
                    <MapPin className="w-3 h-3 inline mr-1" />Region
                  </label>
                  <input
                    type="text"
                    value={region}
                    onChange={e => handleRegionChange(e.target.value)}
                    placeholder="eastus"
                    className="w-full px-3 py-2 text-sm border border-border rounded-md bg-background focus:ring-1 focus:ring-primary"
                    disabled={isLocked}
                  />
                </div>
                <div>
                  <label className="block text-xs font-medium text-muted-foreground mb-1">API Version</label>
                  <input
                    type="text"
                    value={(config.options?.api_version as string) || 'v1'}
                    onChange={e => onUpdate({ ...config, options: { ...config.options, api_version: e.target.value } })}
                    placeholder="v1"
                    className="w-full px-3 py-2 text-sm border border-border rounded-md bg-background focus:ring-1 focus:ring-primary"
                    disabled={isLocked}
                  />
                </div>
              </div>
              {endpoint.trim() && apiKey.trim() && (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={fetchAzureModels}
                  disabled={isFetchingModels || isLocked}
                >
                  {isFetchingModels ? (
                    <><Loader2 className="w-3.5 h-3.5 mr-1.5 animate-spin" />Fetching...</>
                  ) : (
                    <><RefreshCw className="w-3.5 h-3.5 mr-1.5" />Fetch Deployed Models</>
                  )}
                </Button>
              )}
            </div>
          )}

          {/* Bedrock-specific: Region */}
          {isBedrock && (
            <div className="border-t border-border pt-4 space-y-3">
              <div className="flex items-center gap-2">
                <MapPin className="w-4 h-4 text-muted-foreground" />
                <h5 className="text-sm font-medium text-foreground">AWS Region</h5>
              </div>
              <select
                value={region || 'us-east-1'}
                onChange={e => handleRegionChange(e.target.value)}
                disabled={isLocked}
                className="w-full px-3 py-2 text-sm border border-border rounded-md bg-background focus:ring-1 focus:ring-primary disabled:opacity-50"
              >
                {['us-east-1', 'us-west-2', 'eu-west-1', 'eu-central-1', 'ap-southeast-1', 'ap-northeast-1'].map(r => (
                  <option key={r} value={r}>{r}</option>
                ))}
              </select>
              <p className="text-xs text-muted-foreground">
                Bedrock uses IAM role credentials. No API key required.
              </p>
            </div>
          )}

          {/* Ollama-specific: Server URL + Model + API key */}
          {isOllama && (
            <div className="border-t border-border pt-4 space-y-3">
              <div className="flex items-center gap-2">
                <Globe className="w-4 h-4 text-muted-foreground" />
                <h5 className="text-sm font-medium text-foreground">Ollama Server URL</h5>
              </div>
              <input
                type="text"
                value={endpoint}
                onChange={e => handleEndpointChange(e.target.value)}
                placeholder="http://localhost:11434"
                className="w-full px-3 py-2 border border-border rounded-md bg-background text-foreground focus:ring-2 focus:ring-primary"
                disabled={isLocked}
              />
              <p className="text-xs text-muted-foreground">
                Default is <code>http://localhost:11434</code> for local Ollama. Use your cloud endpoint URL for Ollama Cloud.
              </p>

              {/* Model selection — fetch from server then pick from dropdown */}
              <div className="border-t border-border pt-3 space-y-2">
                <div className="flex items-center justify-between">
                  <label className="text-sm font-medium text-foreground">Model</label>
                  {!isLocked && (
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={fetchOllamaModels}
                      disabled={isFetchingModels}
                    >
                      {isFetchingModels ? (
                        <><Loader2 className="w-3.5 h-3.5 mr-1.5 animate-spin" />Fetching...</>
                      ) : (
                        <><RefreshCw className="w-3.5 h-3.5 mr-1.5" />Fetch Models</>
                      )}
                    </Button>
                  )}
                </div>
                {ollamaModels.length > 0 ? (
                  <select
                    value={config.model_id || ''}
                    onChange={e => onUpdate({ ...config, model_id: e.target.value })}
                    disabled={isLocked}
                    className="w-full px-3 py-2 text-sm border border-border rounded-md bg-background text-foreground focus:ring-1 focus:ring-primary disabled:opacity-50"
                  >
                    <option value="">Select a model...</option>
                    {ollamaModels.map(m => (
                      <option key={m} value={m}>{m}</option>
                    ))}
                  </select>
                ) : (
                  <p className="text-xs text-muted-foreground">
                    Click <strong>Fetch Models</strong> to load available models from your Ollama server.
                  </p>
                )}
                {ollamaFetchError && (
                  <div className="text-xs text-red-500 flex items-center gap-1">
                    <AlertCircle className="w-3.5 h-3.5" />{ollamaFetchError}
                  </div>
                )}
              </div>

              {/* Ollama API key (optional for local) */}
              <div className="border-t border-border pt-3 space-y-2">
                <div className="flex items-center gap-2">
                  <Key className="w-4 h-4 text-muted-foreground" />
                  <h5 className="text-sm font-medium text-foreground">API Key <span className="font-normal text-muted-foreground">(optional for local)</span></h5>
                </div>
                <div className="flex gap-2">
                  <input
                    type="password"
                    value={apiKey}
                    onChange={e => handleAPIKeyChange(e.target.value)}
                    placeholder={isLocked ? '••••••••••••••••' : 'Enter Ollama API key (leave blank for local)'}
                    className="flex-1 px-3 py-2 border border-border rounded-md bg-background text-foreground focus:ring-2 focus:ring-primary"
                    disabled={isLocked}
                  />
                  {!isLocked && (
                    <Button
                      onClick={() => onTestAPIKey(apiKey, config.model_id, { ...config.options, base_url: endpoint || 'http://localhost:11434' })}
                      disabled={apiKeyStatus === 'testing'}
                      size="sm"
                      variant="outline"
                    >
                      {apiKeyStatus === 'testing' ? (
                        <Loader2 className="w-4 h-4 animate-spin" />
                      ) : apiKeyStatus === 'valid' ? (
                        <CheckCircle className="w-4 h-4 text-green-500" />
                      ) : apiKeyStatus === 'invalid' ? (
                        <AlertCircle className="w-4 h-4 text-red-500" />
                      ) : (
                        'Test'
                      )}
                    </Button>
                  )}
                </div>
                {!isLocked && apiKeyStatus === 'valid' && (
                  <div className="text-sm text-green-600 dark:text-green-400 flex items-center gap-1">
                    <CheckCircle className="w-4 h-4" />Connected to Ollama successfully
                  </div>
                )}
                {!isLocked && (apiKeyStatus === 'invalid' || apiKeyStatus === 'timeout') && (
                  <div className="text-sm text-red-600 dark:text-red-400 flex items-center gap-1">
                    <AlertCircle className="w-4 h-4" />{apiKeyError || 'Could not connect to Ollama'}
                  </div>
                )}
              </div>
            </div>
          )}

          {/* API Key — non-Ollama providers */}
          {needsApiKey && (
            <div className="border-t border-border pt-4 space-y-3">
              <div className="flex items-center gap-2">
                <Key className="w-4 h-4 text-muted-foreground" />
                <h5 className="text-sm font-medium text-foreground">API Key</h5>
              </div>
              {provider.api_key_env && apiKey && !isLocked && (
                <div className="text-sm text-blue-600 dark:text-blue-400 bg-blue-50 dark:bg-blue-900/20 p-2 rounded-md">
                  <div className="flex items-center gap-2">
                    <CheckCircle className="w-4 h-4" />
                    <span>API key loaded{provider.api_key_env ? ` (${provider.api_key_env})` : ''}</span>
                  </div>
                </div>
              )}
              <div className="space-y-2">
                <div className="flex gap-2">
                  <input
                    type="password"
                    value={apiKey}
                    onChange={e => handleAPIKeyChange(e.target.value)}
                    placeholder={isLocked ? '••••••••••••••••' : `Enter your ${provider.display_name} API key`}
                    className="flex-1 px-3 py-2 border border-border rounded-md bg-background text-foreground focus:ring-2 focus:ring-primary"
                    disabled={isLocked}
                  />
                  {!isLocked && (
                    <Button
                      onClick={() => onTestAPIKey(apiKey, config.model_id, config.options)}
                      disabled={!apiKey.trim() || apiKeyStatus === 'testing'}
                      size="sm"
                      variant="outline"
                    >
                      {apiKeyStatus === 'testing' ? (
                        <Loader2 className="w-4 h-4 animate-spin" />
                      ) : apiKeyStatus === 'valid' ? (
                        <CheckCircle className="w-4 h-4 text-green-500" />
                      ) : apiKeyStatus === 'invalid' ? (
                        <AlertCircle className="w-4 h-4 text-red-500" />
                      ) : (
                        'Test'
                      )}
                    </Button>
                  )}
                </div>
                {provider.api_key_url && !isLocked && (
                  <div className="text-xs text-muted-foreground">
                    Get your API key at{' '}
                    <a href={provider.api_key_url} target="_blank" rel="noopener noreferrer" className="text-primary hover:underline">
                      {new URL(provider.api_key_url).hostname}
                    </a>
                  </div>
                )}
                {!isLocked && apiKeyStatus === 'valid' && (
                  <div className="text-sm text-green-600 dark:text-green-400 flex items-center gap-1">
                    <CheckCircle className="w-4 h-4" />API key is valid
                  </div>
                )}
                {!isLocked && apiKeyStatus === 'invalid' && (
                  <div className="text-sm text-red-600 dark:text-red-400 flex items-center gap-1">
                    <AlertCircle className="w-4 h-4" />{apiKeyError || 'API key is invalid'}
                  </div>
                )}
                {!isLocked && apiKeyStatus === 'timeout' && (
                  <div className="text-sm text-yellow-600 dark:text-yellow-400 flex items-center gap-1">
                    <AlertCircle className="w-4 h-4" />{apiKeyError || 'Validation timeout'}
                  </div>
                )}
              </div>
            </div>
          )}

          {/* Publish */}
          {!isLocked && (
            <div className="border-t border-border pt-4">
              {isPublishing ? (
                <div className="space-y-3">
                  <div className="flex gap-2">
                    <input
                      type="text"
                      value={publishName}
                      onChange={e => { setPublishName(e.target.value); setPublishError(null) }}
                      placeholder="Enter configuration name..."
                      className="flex-1 px-3 py-2 text-sm border border-border rounded-md bg-background text-foreground focus:ring-1 focus:ring-primary"
                      autoFocus
                      onKeyDown={e => e.key === 'Enter' && handlePublishToLibrary()}
                    />
                    <Button onClick={handlePublishToLibrary} size="sm" disabled={!publishName.trim() || isSubmitting}>
                      {isSubmitting ? <Loader2 className="w-4 h-4 animate-spin" /> : 'Save'}
                    </Button>
                    <Button onClick={() => { setIsPublishing(false); setPublishName(''); setPublishError(null) }} size="sm" variant="ghost" disabled={isSubmitting}>
                      Cancel
                    </Button>
                  </div>
                  {publishError && (
                    <div className="text-sm text-red-600 dark:text-red-400 flex items-center gap-1">
                      <AlertCircle className="w-4 h-4" />{publishError}
                    </div>
                  )}
                </div>
              ) : (
                <Button
                  onClick={() => { setPublishName(generateDefaultName()); setIsPublishing(true) }}
                  size="sm"
                  variant="outline"
                  disabled={!config.model_id}
                  className="w-full"
                >
                  <BookOpen className="w-4 h-4 mr-2" />
                  Publish
                </Button>
              )}
            </div>
          )}
        </div>
      </Card>
    </div>
  )
}
