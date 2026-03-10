import { useState, useEffect } from 'react'
import { Key, CheckCircle, AlertCircle, Loader2, BookOpen } from 'lucide-react'
import { Button } from './ui/Button'
import { Card } from './ui/Card'
import { useLLMStore } from '../stores'
import type { ExtendedLLMConfiguration } from '../services/api-types'
import type { ModelMetadata } from '../services/llm-config-api'
import { ModelSelector } from './ui/ModelSelector'
import { ModelOptionsConfig } from './llm/ModelOptionsConfig'

interface MiniMaxCodingPlanSectionProps {
  config: ExtendedLLMConfiguration
  onUpdate: (config: ExtendedLLMConfiguration) => void
  onTestAPIKey: (apiKey: string, modelId?: string, options?: Record<string, unknown>, temperature?: number) => void
  apiKeyStatus: 'idle' | 'testing' | 'valid' | 'invalid' | 'timeout'
  apiKeyError: string | null
  metadata?: ModelMetadata[]
}

export function MiniMaxCodingPlanSection({ config, onUpdate, onTestAPIKey, apiKeyStatus, apiKeyError, metadata }: MiniMaxCodingPlanSectionProps) {
  const [apiKey, setApiKey] = useState(config.api_key || '')
  const [isPublishing, setIsPublishing] = useState(false)
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [publishName, setPublishName] = useState('')
  const [publishError, setPublishError] = useState<string | null>(null)
  const { availableMinimaxCodingPlanModels, saveLLM, testAPIKey: testAPIKeyFromStore, lockedProviders, llmConfigLocked } = useLLMStore()

  const isLocked = llmConfigLocked || lockedProviders.includes('minimax-coding-plan')

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

  const handleOptionsChange = (newOptions: Record<string, unknown>, newTemp?: number) => {
    onUpdate({ ...config, options: newOptions, temperature: newTemp })
  }

  const generateDefaultName = (): string => {
    if (!config.model_id) return ''
    const parts: string[] = []
    const modelId = config.model_id.replace(/-20\d{6}/g, '').replace(/-20\d{2}-\d{2}-\d{2}/g, '')
    parts.push(modelId)
    const temp = config.temperature !== undefined ? config.temperature : 0
    parts.push(`temp${temp.toFixed(1)}`)
    return parts.join('-')
  }

  const handlePublishToLibrary = async () => {
    if (!publishName.trim() || !config.model_id) return

    if (!apiKey.trim()) {
      setPublishError('API key is required to publish MiniMax Coding Plan models')
      return
    }

    setIsSubmitting(true)
    setPublishError(null)

    try {
      const optionsWithTemp = config.temperature !== undefined
        ? { ...config.options, temperature: config.temperature }
        : config.options
      const testResult = await testAPIKeyFromStore('minimax-coding-plan', apiKey, config.model_id, optionsWithTemp)

      if (testResult.valid) {
        const llmModel = {
          provider: 'minimax-coding-plan' as const,
          model_id: config.model_id,
          api_key: config.api_key,
          options: config.options,
          temperature: config.temperature
        }

        const currentModelMetadata = metadata?.find(m => m.model_id === config.model_id && m.provider === 'minimax-coding-plan')
        saveLLM(llmModel, publishName.trim(), currentModelMetadata?.model_name, 'api_key')
        setPublishName('')
        setIsPublishing(false)
        setIsSubmitting(false)
        await onTestAPIKey(apiKey, config.model_id, config.options, config.temperature)
      } else {
        setPublishError(testResult.error || 'API key validation failed. Please check your API key and try again.')
      }
    } catch (err) {
      setPublishError(err instanceof Error ? err.message : 'Failed to publish. Please try again.')
    } finally {
      setIsSubmitting(false)
    }
  }

  // Show Anthropic model names from available list (coding plan uses Anthropic model names)
  const allModels = Array.from(new Set([...availableMinimaxCodingPlanModels]))
  const currentModelMetadata = metadata?.find(m => m.model_id === config.model_id && m.provider === 'minimax-coding-plan')

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-lg font-semibold text-foreground">MiniMax Coding Plan</h3>
          <p className="text-sm text-muted-foreground mt-0.5">
            Uses Anthropic model names (e.g. claude-sonnet-4-5) routed via MiniMax's Anthropic-compatible endpoint.
          </p>
        </div>
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
            <label className="block text-sm font-medium text-muted-foreground mb-2">Primary Model (Anthropic model name)</label>
            <ModelSelector
              value={config.model_id}
              onChange={(val) => onUpdate({ ...config, model_id: val })}
              models={allModels}
              metadata={metadata || []}
              placeholder="e.g. claude-sonnet-4-5"
              disabled={isLocked}
            />
            {currentModelMetadata && (
              <ModelOptionsConfig
                metadata={currentModelMetadata}
                options={config.options || {}}
                temperature={config.temperature}
                onChange={handleOptionsChange}
                disabled={isLocked}
              />
            )}
          </div>

          <div className="border-t border-border pt-4 space-y-3">
            <div className="flex items-center gap-2">
              <Key className="w-4 h-4 text-muted-foreground" />
              <h5 className="text-sm font-medium text-foreground">Coding Plan API Key</h5>
            </div>
            <div className="text-xs text-muted-foreground bg-blue-50 dark:bg-blue-900/10 p-2 rounded border border-blue-200 dark:border-blue-800">
              Use your MiniMax <strong>Coding Plan</strong> API key (starts with <code>sk-cp-</code>). Requests are sent with <code>User-Agent: claude-code/2.1.71</code> so MiniMax routes them correctly.
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
                  placeholder={isLocked ? "••••••••••••••••" : "Enter your MiniMax Coding Plan API key (sk-cp-...)"}
                  className="flex-1 px-3 py-2 border border-border rounded-md bg-background text-foreground focus:ring-2 focus:ring-primary focus:border-primary"
                  disabled={isLocked}
                />
                {!isLocked && (
                  <Button
                    onClick={() => onTestAPIKey(apiKey, config.model_id, config.options, config.temperature)}
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

          {!isLocked && (
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
