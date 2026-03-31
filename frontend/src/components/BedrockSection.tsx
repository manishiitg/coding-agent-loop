import { useState } from 'react'
import { Key, CheckCircle, XCircle, Loader2, BookOpen, Plus, Trash2, AlertCircle } from 'lucide-react'
import { Button } from './ui/Button'
import { Card } from './ui/Card'
import { useLLMStore } from '../stores'
import type { ExtendedLLMConfiguration } from '../services/api-types'
import type { ModelMetadata } from '../services/llm-config-api'
import { ModelSelector } from './ui/ModelSelector'
import { ModelOptionsConfig } from './llm/ModelOptionsConfig'

type APIKeyStatusValue = 'idle' | 'testing' | 'valid' | 'invalid' | 'timeout'

interface BedrockSectionProps {
  config: ExtendedLLMConfiguration
  onUpdate: (config: ExtendedLLMConfiguration) => void
  onTestAPIKey: (apiKey: string, modelId?: string, options?: Record<string, unknown>, temperature?: number) => void
  apiKeyStatus: APIKeyStatusValue
  apiKeyError: string | null
  metadata?: ModelMetadata[]
}

export function BedrockSection({ config, onUpdate, onTestAPIKey, apiKeyStatus, apiKeyError, metadata }: BedrockSectionProps) {
  const [region, setRegion] = useState(config.region || 'us-east-1')
  const [newCustomModel, setNewCustomModel] = useState('')
  const [isPublishing, setIsPublishing] = useState(false)
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [publishName, setPublishName] = useState('')
  const [publishError, setPublishError] = useState<string | null>(null)
  const { customBedrockModels, addCustomBedrockModel, removeCustomBedrockModel, availableBedrockModels, saveLLM, testAPIKey: testAPIKeyFromStore, lockedProviders, llmConfigLocked } = useLLMStore()
  
  const isLocked = llmConfigLocked || lockedProviders.includes('bedrock')

  const handleRegionChange = (newRegion: string) => {
    setRegion(newRegion)
    onUpdate({ ...config, region: newRegion })
  }

  // Drive all models from metadata if available, otherwise fallback to store
  const modelsFromMetadata = metadata?.filter(m => m.provider === 'bedrock').map(m => m.model_id) || []
  const allModels = Array.from(new Set([...modelsFromMetadata, ...availableBedrockModels, ...customBedrockModels]))
  
  const handleAddCustomModel = () => {
    if (newCustomModel.trim() && !allModels.includes(newCustomModel.trim())) {
      addCustomBedrockModel(newCustomModel.trim())
      setNewCustomModel('')
      useLLMStore.getState().refreshAvailableLLMs()
    }
  }
  
  const handleRemoveCustomModel = (model: string) => {
    removeCustomBedrockModel(model)
    useLLMStore.getState().refreshAvailableLLMs()
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
      // Test credentials (API key is optional for Bedrock, uses IAM) - merge temperature into options
      const optionsWithTemp = config.temperature !== undefined 
        ? { ...config.options, temperature: config.temperature }
        : config.options
      const testResult = await testAPIKeyFromStore('bedrock', 'test', config.model_id, optionsWithTemp)
      
      if (testResult.valid) {
        const llmModel = {
          provider: 'bedrock' as const,
          model_id: config.model_id,
          region: config.region,
          options: config.options,
          temperature: config.temperature
        }
        
        const currentModelMetadata = metadata?.find(m => m.model_id === config.model_id)
        // Bedrock uses OAuth/IAM, so auth_method is 'oauth' or 'none'
        await saveLLM(llmModel, publishName.trim(), currentModelMetadata?.model_name, 'oauth', currentModelMetadata)
        setPublishName('')
        setIsPublishing(false)
        setIsSubmitting(false)
        // Also update the status in parent via callback
        await onTestAPIKey('test', config.model_id, config.options, config.temperature)
      } else {
        setPublishError(testResult.error || 'AWS credentials validation failed. Please check your AWS configuration and try again.')
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
        <h3 className="text-lg font-semibold text-foreground">AWS Bedrock Configuration</h3>
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
              models={allModels}
              metadata={metadata || []}
              placeholder="Select a Bedrock model"
              disabled={isLocked}
            />
            <ModelOptionsConfig 
              metadata={currentModelMetadata} 
              options={config.options || {}} 
              temperature={config.temperature}
              onChange={handleOptionsChange}
              disabled={isLocked}
            />
          </div>

          <div className="border-t border-border pt-4 space-y-3">
            <div className="flex items-center gap-2"><Key className="w-4 h-4 text-muted-foreground" /><h5 className="text-sm font-medium text-foreground">AWS Configuration</h5></div>
            {region && !isLocked && <div className="text-sm text-blue-600 dark:text-blue-400 bg-blue-50 dark:bg-blue-900/20 p-2 rounded-md"><div className="flex items-center gap-2"><CheckCircle className="w-4 h-4" /><span>AWS region loaded from environment variables</span></div></div>}
            <div className="space-y-2">
              <label className="block text-sm font-medium text-muted-foreground">AWS Region</label>
              <select value={region} onChange={(e) => handleRegionChange(e.target.value)} className="w-full px-3 py-2 border border-border rounded-md bg-background text-foreground focus:ring-2 focus:ring-primary focus:border-primary" disabled={isLocked}>
                <option value="us-east-1">US East (N. Virginia)</option>
                <option value="us-west-2">US West (Oregon)</option>
                <option value="eu-west-1">Europe (Ireland)</option>
                <option value="ap-southeast-1">Asia Pacific (Singapore)</option>
              </select>
              {!isLocked && <div className="text-xs text-muted-foreground">Uses AWS IAM roles for authentication. Make sure your AWS credentials are configured.</div>}
            </div>
          </div>

          {!isLocked && (
            <div className="border-t border-border pt-4 space-y-3">
              <div className="flex items-center gap-2"><Key className="w-4 h-4 text-muted-foreground" /><h5 className="text-sm font-medium text-foreground">Test AWS Credentials</h5></div>
              <div className="flex items-center gap-3">
                <Button onClick={() => onTestAPIKey('test', config.model_id, config.options, config.temperature)} disabled={apiKeyStatus === 'testing'} variant="outline" size="sm">
                  {apiKeyStatus === 'testing' ? <><Loader2 className="w-4 h-4 mr-2 animate-spin" />Testing...</> : <><CheckCircle className="w-4 h-4 mr-2" />Test Credentials</>}
                </Button>
                {apiKeyStatus === 'valid' && <div className="flex items-center gap-2 text-green-600 dark:text-green-400"><CheckCircle className="w-4 h-4" /><span className="text-sm">AWS credentials are valid</span></div>}
                {apiKeyStatus === 'invalid' && <div className="flex items-center gap-2 text-red-600 dark:text-red-400"><XCircle className="w-4 h-4" /><span className="text-sm">{apiKeyError || 'AWS credentials are invalid'}</span></div>}
              </div>
            </div>
          )}

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

      {!isLocked && (
        <Card className="p-4">
          <h4 className="font-medium text-foreground mb-4">Custom Models</h4>
          <div className="space-y-3">
            <div className="flex gap-2">
              <input type="text" value={newCustomModel} onChange={(e) => setNewCustomModel(e.target.value)} placeholder="Enter custom model ID" className="flex-1 px-3 py-2 border border-border rounded-md bg-background text-foreground focus:ring-2 focus:ring-primary focus:border-primary" onKeyPress={(e) => e.key === 'Enter' && handleAddCustomModel()} />
              <Button onClick={handleAddCustomModel} disabled={!newCustomModel.trim() || allModels.includes(newCustomModel.trim())} size="sm" variant="outline"><Plus className="w-4 h-4" /></Button>
            </div>
            {customBedrockModels.length > 0 && (
              <div className="space-y-2">
                <h5 className="text-sm font-medium text-muted-foreground">Custom Models:</h5>
                <div className="space-y-1">
                  {customBedrockModels.map((model) => (
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
      )}
    </div>
  )
}
