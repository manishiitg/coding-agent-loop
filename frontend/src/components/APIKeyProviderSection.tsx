import { useState, useEffect } from 'react'
import { Key, CheckCircle, AlertCircle, Loader2, BookOpen } from 'lucide-react'
import { Button } from './ui/Button'
import { Card } from './ui/Card'
import { useLLMStore } from '../stores'
import type { ExtendedLLMConfiguration, LLMProvider } from '../services/api-types'
import type { ModelMetadata } from '../services/llm-config-api'
import { ModelSelector } from './ui/ModelSelector'
import { ModelOptionsConfig } from './llm/ModelOptionsConfig'

type APIKeyProvider = Extract<
  LLMProvider,
  'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic' | 'azure' | 'z-ai' | 'kimi' | 'minimax' | 'minimax-coding-plan' | 'elevenlabs' | 'deepgram'
>

interface APIKeyProviderSectionProps {
  provider: APIKeyProvider
  providerLabel: string
  modelPlaceholder: string
  publishErrorLabel: string
  config: ExtendedLLMConfiguration
  models: string[]
  onUpdate: (config: ExtendedLLMConfiguration) => void
  onTestAPIKey: (apiKey: string, modelId?: string, options?: Record<string, unknown>) => void
  apiKeyStatus: 'idle' | 'testing' | 'valid' | 'invalid' | 'timeout'
  apiKeyError: string | null
  metadata?: ModelMetadata[]
  allowPublish?: boolean
}

export function APIKeyProviderSection({
  provider,
  providerLabel,
  modelPlaceholder,
  publishErrorLabel,
  config,
  models,
  onUpdate,
  onTestAPIKey,
  apiKeyStatus,
  apiKeyError,
  metadata,
  allowPublish = true,
}: APIKeyProviderSectionProps) {
  const [apiKey, setApiKey] = useState(config.api_key || '')
  const [isPublishing, setIsPublishing] = useState(false)
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [publishName, setPublishName] = useState('')
  const [publishError, setPublishError] = useState<string | null>(null)
  const { saveLLM, testAPIKey: testAPIKeyFromStore, lockedProviders, llmConfigLocked } = useLLMStore()

  const isLocked = llmConfigLocked || lockedProviders.includes(provider)

  useEffect(() => {
    if (config.api_key) {
      setApiKey(config.api_key)
    }
  }, [config.api_key])

  const handleAPIKeyChange = (newApiKey: string) => {
    setApiKey(newApiKey)
    onUpdate({ ...config, api_key: newApiKey })
    setPublishError(null)
  }

  const handleOptionsChange = (newOptions: Record<string, unknown>) => {
    onUpdate({ ...config, options: newOptions })
  }

  const generateDefaultName = (): string => {
    if (!config.model_id) return ''
    const parts: string[] = []
    const modelId = config.model_id.replace(/-20\d{6}/g, '').replace(/-20\d{2}-\d{2}-\d{2}/g, '')
    parts.push(modelId)
    return parts.join('-')
  }

  const currentModelMetadata = metadata?.find(m => m.model_id === config.model_id && m.provider === provider)

  const handlePublishToLibrary = async () => {
    if (!publishName.trim() || !config.model_id) return

    if (!apiKey.trim()) {
      setPublishError(`API key is required to publish ${publishErrorLabel} models`)
      return
    }

    setIsSubmitting(true)
    setPublishError(null)

    try {
      const testResult = await testAPIKeyFromStore(provider, apiKey, config.model_id, config.options)

      if (testResult.valid) {
        const llmModel = {
          provider,
          model_id: config.model_id,
          api_key: config.api_key,
          options: config.options,
        }

        await saveLLM(llmModel, publishName.trim(), currentModelMetadata?.model_name, 'api_key', currentModelMetadata)
        setPublishName('')
        setIsPublishing(false)
        setIsSubmitting(false)
        await onTestAPIKey(apiKey, config.model_id, config.options)
      } else {
        setPublishError(testResult.error || 'API key validation failed. Please check your API key and try again.')
      }
    } catch (err) {
      setPublishError(err instanceof Error ? err.message : 'Failed to publish. Please try again.')
    } finally {
      setIsSubmitting(false)
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h3 className="text-lg font-semibold text-foreground">{providerLabel} Configuration</h3>
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
          <div>
            <label className="block text-sm font-medium text-muted-foreground mb-2">Primary Model</label>
            <ModelSelector
              value={config.model_id}
              onChange={(val) => onUpdate({ ...config, model_id: val })}
              models={models}
              metadata={metadata || []}
              placeholder={modelPlaceholder}
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

          <div className="border-t border-border pt-4 space-y-3">
            <div className="flex items-center gap-2">
              <Key className="w-4 h-4 text-muted-foreground" />
              <h5 className="text-sm font-medium text-foreground">API Key</h5>
            </div>
            {apiKey && !isLocked && (
              <div className="text-sm text-blue-600 dark:text-blue-400 bg-blue-50 dark:bg-blue-900/20 p-2 rounded-md">
                <div className="flex items-center gap-2">
                  <CheckCircle className="w-4 h-4" />
                  <span>API key loaded from environment variables</span>
                </div>
              </div>
            )}
            <div className="space-y-2">
              <div className="flex gap-2">
                <input
                  type="password"
                  value={apiKey}
                  onChange={(e) => handleAPIKeyChange(e.target.value)}
                  placeholder={isLocked ? '••••••••••••••••' : `Enter your ${providerLabel} API key`}
                  className="flex-1 px-3 py-2 border border-border rounded-md bg-background text-foreground focus:ring-2 focus:ring-primary focus:border-primary"
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
              {apiKey && !isLocked && (
                <div className="text-xs text-muted-foreground">
                  <button onClick={() => handleAPIKeyChange('')} className="text-primary hover:underline">Clear and enter new key</button>
                </div>
              )}
              {!isLocked && apiKeyStatus === 'valid' && <div className="text-sm text-green-600 dark:text-green-400 flex items-center gap-1"><CheckCircle className="w-4 h-4" />API key is valid</div>}
              {!isLocked && apiKeyStatus === 'invalid' && <div className="text-sm text-red-600 dark:text-red-400 flex items-center gap-1"><AlertCircle className="w-4 h-4" />{apiKeyError || 'API key is invalid'}</div>}
              {!isLocked && apiKeyStatus === 'timeout' && <div className="text-sm text-yellow-600 dark:text-yellow-400 flex items-center gap-1"><AlertCircle className="w-4 h-4" />{apiKeyError || 'Validation timeout - check your connection'}</div>}
            </div>
          </div>

          {!isLocked && allowPublish && (
            <div className="border-t border-border pt-4">
              {isPublishing ? (
                <div className="space-y-3">
                  <div className="flex gap-2">
                    <input
                      type="text"
                      value={publishName}
                      onChange={(e) => { setPublishName(e.target.value); setPublishError(null) }}
                      placeholder="Enter configuration name..."
                      className="flex-1 px-3 py-2 text-sm border border-border rounded-md bg-background text-foreground focus:ring-1 focus:ring-primary focus:border-primary"
                      autoFocus
                      onKeyPress={(e) => e.key === 'Enter' && handlePublishToLibrary()}
                    />
                    <Button onClick={handlePublishToLibrary} size="sm" disabled={!publishName.trim() || isSubmitting}>
                      {isSubmitting ? <Loader2 className="w-4 h-4 animate-spin" /> : 'Save'}
                    </Button>
                    <Button onClick={() => { setIsPublishing(false); setPublishName(''); setPublishError(null) }} size="sm" variant="ghost" disabled={isSubmitting}>Cancel</Button>
                  </div>
                  {publishError && (
                    <div className="text-sm text-red-600 dark:text-red-400 flex items-center gap-1">
                      <AlertCircle className="w-4 h-4" />
                      {publishError}
                    </div>
                  )}
                </div>
              ) : (
                <Button
                  onClick={() => {
                    setPublishName(generateDefaultName())
                    setIsPublishing(true)
                  }}
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
