import { useState, useEffect } from 'react'
import { Key, CheckCircle, AlertCircle, Loader2, BookOpen, Plus, Trash2 } from 'lucide-react'
import { Button } from './ui/Button'
import { Card } from './ui/Card'
import { useLLMStore } from '../stores'
import type { ExtendedLLMConfiguration } from '../services/api-types'
import type { ModelMetadata } from '../services/llm-config-api'
import { ModelSelector } from './ui/ModelSelector'
import { ModelOptionsConfig } from './llm/ModelOptionsConfig'

interface VertexSectionProps {
  config: ExtendedLLMConfiguration
  onUpdate: (config: ExtendedLLMConfiguration) => void
  onTestAPIKey: (apiKey: string, modelId?: string, options?: Record<string, unknown>, temperature?: number) => void
  apiKeyStatus: 'idle' | 'testing' | 'valid' | 'invalid' | 'timeout'
  apiKeyError: string | null
  isPrimary: boolean
  onSetPrimary: () => void
  metadata?: ModelMetadata[]
}

export function VertexSection({ config, onUpdate, onTestAPIKey, apiKeyStatus, apiKeyError, isPrimary, onSetPrimary, metadata }: VertexSectionProps) {
  const [apiKey, setApiKey] = useState(config.api_key || '')
  const [newCustomModel, setNewCustomModel] = useState('')
  const [isPublishing, setIsPublishing] = useState(false)
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [publishName, setPublishName] = useState('')
  const [publishError, setPublishError] = useState<string | null>(null)
  const { availableVertexModels, customVertexModels, addCustomVertexModel, removeCustomVertexModel, refreshAvailableLLMs, saveLLM, testAPIKey: testAPIKeyFromStore } = useLLMStore()

  useEffect(() => {
    if (config.api_key) setApiKey(config.api_key)
  }, [config.api_key])

  const handleAPIKeyChange = (newApiKey: string) => {
    setApiKey(newApiKey)
    onUpdate({ ...config, api_key: newApiKey })
    setPublishError(null)
  }

  // Drive all models from metadata if available, otherwise fallback to store
  const modelsFromMetadata = metadata?.filter(m => m.provider === 'vertex').map(m => m.model_id) || []
  const allModels = Array.from(new Set([...modelsFromMetadata, ...availableVertexModels, ...customVertexModels]))

  useEffect(() => {
    if (!config.model_id && allModels.length > 0) {
      onUpdate({ ...config, model_id: allModels[0] })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [allModels.length])

  const handleAddCustomModel = () => {
    if (newCustomModel.trim() && !allModels.includes(newCustomModel.trim())) {
      addCustomVertexModel(newCustomModel.trim())
      setNewCustomModel('')
      refreshAvailableLLMs()
    }
  }
  
  const handleRemoveCustomModel = (model: string) => {
    removeCustomVertexModel(model)
    refreshAvailableLLMs()
  }

  const handleOptionsChange = (newOptions: Record<string, unknown>, newTemp?: number) => {
    onUpdate({ ...config, options: newOptions, temperature: newTemp })
  }

  const generateDefaultName = (): string => {
    if (!config.model_id) return ''
    
    const parts: string[] = []
    const currentModelMetadata = metadata?.find(m => m.model_id === config.model_id)
    
    // Add model ID (simplified - remove version dates if present)
    const modelId = config.model_id.replace(/-20\d{6}/g, '').replace(/-20\d{2}-\d{2}-\d{2}/g, '')
    parts.push(modelId)
    
    // Add temperature (use default 0 if not set, same as UI)
    const temp = config.temperature !== undefined ? config.temperature : 0
    parts.push(`temp${temp.toFixed(1)}`)
    
    // Add key options with defaults (same logic as ModelOptionsConfig)
    if (currentModelMetadata?.supports_thinking_level) {
      const thinkingLevel = (config.options?.thinking_level as string) || 'high'
      parts.push(`thinking-${thinkingLevel}`)
    }
    if (currentModelMetadata?.supports_reasoning_effort) {
      const reasoningEffort = (config.options?.reasoning_effort as string) || 'medium'
      parts.push(`reasoning-${reasoningEffort}`)
    }
    if (currentModelMetadata?.supports_thinking_budget) {
      const thinkingBudget = (config.options?.thinking_budget as number) || 1024
      parts.push(`budget-${thinkingBudget}`)
    }
    
    return parts.join('-')
  }

  const handlePublishToLibrary = async () => {
    if (!publishName.trim() || !config.model_id) return
    
    setIsSubmitting(true)
    setPublishError(null)

    try {
      // Test credentials (API key is optional for Vertex, uses OAuth/ADC) - merge temperature into options
      const optionsWithTemp = config.temperature !== undefined 
        ? { ...config.options, temperature: config.temperature }
        : config.options
      const testResult = await testAPIKeyFromStore('vertex', apiKey || '', config.model_id, optionsWithTemp)
      
      if (testResult.valid) {
        const llmModel = {
          provider: 'vertex' as const,
          model_id: config.model_id,
          api_key: config.api_key,
          options: config.options,
          temperature: config.temperature
        }
        
        const currentModelMetadata = metadata?.find(m => m.model_id === config.model_id)
        // Vertex uses OAuth/ADC if no API key, otherwise api_key
        const authMethod = apiKey ? 'api_key' : 'oauth'
        saveLLM(llmModel, publishName.trim(), currentModelMetadata?.model_name, authMethod)
        setPublishName('')
        setIsPublishing(false)
        setIsSubmitting(false)
        // Also update the status in parent via callback
        await onTestAPIKey(apiKey || '', config.model_id, config.options, config.temperature)
      } else {
        setPublishError(testResult.error || 'Authentication validation failed. Please check your credentials and try again.')
      }
    } catch (err) {
      setPublishError(err instanceof Error ? err.message : 'Failed to publish. Please try again.')
    } finally {
      setIsSubmitting(false)
    }
  }

  const currentModelMetadata = metadata?.find(m => m.model_id === config.model_id)

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h3 className="text-lg font-semibold text-foreground">Vertex AI Configuration</h3>
      </div>
      
      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-4">Model Selection</h4>
        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-muted-foreground mb-2">Primary Model</label>
            <ModelSelector
              value={config.model_id || ''}
              onChange={(val) => onUpdate({ ...config, model_id: val })}
              models={allModels}
              metadata={metadata || []}
              placeholder="Select a Vertex AI model"
            />
            <ModelOptionsConfig 
              metadata={currentModelMetadata} 
              options={config.options || {}} 
              temperature={config.temperature}
              onChange={handleOptionsChange}
            />
          </div>

          <div className="border-t border-border pt-4 space-y-3">
            <div className="flex items-center gap-2"><Key className="w-4 h-4 text-muted-foreground" /><h5 className="text-sm font-medium text-foreground">API Key (Optional)</h5></div>
            <div className="text-sm text-blue-600 dark:text-blue-400 bg-blue-50 dark:bg-blue-900/20 p-3 rounded-md border border-blue-200 dark:border-blue-800">
              <div className="flex items-start gap-2">
                <AlertCircle className="w-4 h-4 mt-0.5 flex-shrink-0" />
                <div>
                  <p className="font-medium mb-1">Authentication Methods</p>
                  <p className="text-xs">API key is optional. If not provided, the system will automatically try: 1) gcloud CLI auth, 2) Service Account (GOOGLE_APPLICATION_CREDENTIALS), 3) Application Default Credentials.</p>
                </div>
              </div>
            </div>
            {config.model_id && config.model_id.startsWith('claude-') && (
              <div className="text-sm text-amber-600 dark:text-amber-400 bg-amber-50 dark:bg-amber-900/20 p-3 rounded-md border border-amber-200 dark:border-amber-800">
                <div className="flex items-start gap-2">
                  <AlertCircle className="w-4 h-4 mt-0.5 flex-shrink-0" />
                  <div>
                    <p className="font-medium mb-1">Anthropic Model Detected</p>
                    <p className="text-xs">This model requires <code className="bg-amber-100 dark:bg-amber-900/40 px-1 rounded">VERTEX_PROJECT_ID</code> and <code className="bg-amber-100 dark:bg-amber-900/40 px-1 rounded">VERTEX_LOCATION_ID</code> environment variables set on the server.</p>
                  </div>
                </div>
              </div>
            )}
            {apiKey && <div className="text-sm text-blue-600 dark:text-blue-400 bg-blue-50 dark:bg-blue-900/20 p-2 rounded-md"><div className="flex items-center gap-2"><CheckCircle className="w-4 h-4" /><span>API key loaded from environment variables</span></div></div>}
            <div className="space-y-2">
              <div className="flex gap-2">
                <input type="password" value={apiKey} onChange={(e) => handleAPIKeyChange(e.target.value)} placeholder="Enter your Vertex AI API key (optional)" className="flex-1 px-3 py-2 border border-border rounded-md bg-background text-foreground focus:ring-2 focus:ring-primary focus:border-primary" />
                <Button onClick={() => onTestAPIKey(apiKey || '', config.model_id, config.options, config.temperature)} disabled={apiKeyStatus === 'testing'} size="sm" variant="outline">
                  {apiKeyStatus === 'testing' ? <Loader2 className="w-4 h-4 animate-spin" /> : apiKeyStatus === 'valid' ? <CheckCircle className="w-4 h-4 text-green-500" /> : apiKeyStatus === 'invalid' ? <AlertCircle className="w-4 h-4 text-red-500" /> : 'Test'}
                </Button>
              </div>
              {!apiKey && <div className="text-xs text-muted-foreground">No API key provided - will test using OAuth authentication (gcloud/service account/ADC).</div>}
              {apiKey && <div className="text-xs text-muted-foreground"><button onClick={() => handleAPIKeyChange('')} className="text-primary hover:underline">Clear and enter new key</button></div>}
              {apiKeyStatus === 'valid' && <div className="text-sm text-green-600 dark:text-green-400 flex items-center gap-1"><CheckCircle className="w-4 h-4" />Authentication successful</div>}
              {apiKeyStatus === 'invalid' && <div className="text-sm text-red-600 dark:text-red-400 flex items-center gap-1"><AlertCircle className="w-4 h-4" />{apiKeyError || 'Authentication failed'}</div>}
            </div>
          </div>

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
        </div>
      </Card>

      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-4">Custom Models</h4>
        <div className="space-y-3">
          <div className="flex gap-2">
            <input type="text" value={newCustomModel} onChange={(e) => setNewCustomModel(e.target.value)} placeholder="Enter custom model ID" className="flex-1 px-3 py-2 border border-border rounded-md bg-background text-foreground focus:ring-2 focus:ring-primary focus:border-primary" onKeyPress={(e) => e.key === 'Enter' && handleAddCustomModel()} />
            <Button onClick={handleAddCustomModel} disabled={!newCustomModel.trim() || allModels.includes(newCustomModel.trim())} size="sm" variant="outline"><Plus className="w-4 h-4" /></Button>
          </div>
          {customVertexModels.length > 0 && (
            <div className="space-y-2">
              <h5 className="text-sm font-medium text-muted-foreground">Custom Models:</h5>
              <div className="space-y-1">
                {customVertexModels.map((model) => (
                  <div key={model} className="flex items-center justify-between bg-muted/50 p-2 rounded-md">
                    <span className="text-sm text-foreground font-mono">{model}</span>
                    <Button onClick={() => handleRemoveCustomModel(model)} size="sm" variant="ghost" className="text-red-600 hover:text-red-700 hover:bg-red-50 dark:text-red-400 dark:hover:text-red-300 dark:hover:bg-red-900/20"><Trash2 className="w-4 h-4" /></Button>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      </Card>
    </div>
  )
}
