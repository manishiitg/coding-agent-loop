import { useState, useEffect } from 'react'
import { Key, CheckCircle, AlertCircle, Loader2, BookOpen, Plus, Trash2, Globe, MapPin, RefreshCw } from 'lucide-react'
import { Button } from './ui/Button'
import { Card } from './ui/Card'
import { useLLMStore } from '../stores'
import type { ExtendedLLMConfiguration } from '../services/api-types'
import type { ModelMetadata } from '../services/llm-config-api'
import { llmConfigService } from '../services/llm-config-api'
import { ModelSelector } from './ui/ModelSelector'
import { ModelOptionsConfig } from './llm/ModelOptionsConfig'

interface AzureSectionProps {
  config: ExtendedLLMConfiguration
  onUpdate: (config: ExtendedLLMConfiguration) => void
  onTestAPIKey: (apiKey: string, modelId?: string, options?: Record<string, unknown>, temperature?: number) => void
  apiKeyStatus: 'idle' | 'testing' | 'valid' | 'invalid' | 'timeout'
  apiKeyError: string | null
  metadata?: ModelMetadata[]
}

export function AzureSection({ config, onUpdate, onTestAPIKey, apiKeyStatus, apiKeyError, metadata }: AzureSectionProps) {
  const [apiKey, setApiKey] = useState(config.api_key || '')
  const [endpoint, setEndpoint] = useState(config.endpoint || '')
  const [region, setRegion] = useState(config.region || '')
  const [apiVersion, setApiVersion] = useState((config.options?.api_version as string) || 'v1')
  const [newCustomModel, setNewCustomModel] = useState('')
  const [isPublishing, setIsPublishing] = useState(false)
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [publishName, setPublishName] = useState('')
  const [publishError, setPublishError] = useState<string | null>(null)

  // Azure-specific states
  const [isAuthenticated, setIsAuthenticated] = useState(false)
  const [isAuthenticating, setIsAuthenticating] = useState(false)
  const [authError, setAuthError] = useState<string | null>(null)
  const [azureModels, setAzureModels] = useState<string[]>([])
  const [isFetchingModels, setIsFetchingModels] = useState(false)
  const [endpointCorrectionMessage, setEndpointCorrectionMessage] = useState<string | null>(null)

  const { 
    availableAzureModels, 
    customAzureModels, 
    addCustomAzureModel, 
    removeCustomAzureModel, 
    refreshAvailableLLMs, 
    saveLLM, 
    testAPIKey: testAPIKeyFromStore,
    lockedProviders,
    llmConfigLocked
  } = useLLMStore()

  const isLocked = llmConfigLocked || lockedProviders.includes('azure')

  useEffect(() => {
    if (config.api_key) setApiKey(config.api_key)
    if (config.endpoint) setEndpoint(config.endpoint)
    if (config.region) setRegion(config.region)
    if (config.options?.api_version) setApiVersion(config.options.api_version as string)

    // If credentials are already saved, consider authenticated
    if (config.api_key && config.endpoint) {
      setIsAuthenticated(true)
    }
  }, [config.api_key, config.endpoint, config.region, config.options?.api_version])

  const handleEndpointChange = (newEndpoint: string) => {
    setEndpoint(newEndpoint)
    setIsAuthenticated(false)
    setAuthError(null)
  }

  const handleAPIKeyChange = (newApiKey: string) => {
    setApiKey(newApiKey)
    setIsAuthenticated(false)
    setAuthError(null)
    setPublishError(null)
  }

  const handleRegionChange = (newRegion: string) => {
    setRegion(newRegion)
  }

  const handleApiVersionChange = (newApiVersion: string) => {
    setApiVersion(newApiVersion)
    onUpdate({ ...config, options: { ...config.options, api_version: newApiVersion } })
  }

  // Authenticate Azure credentials and fetch deployed models via backend API
  const handleAuthenticate = async () => {
    if (!endpoint.trim() || !apiKey.trim()) {
      setAuthError('Endpoint and API Key are required')
      return
    }

    setIsAuthenticating(true)
    setAuthError(null)
    setIsFetchingModels(true)

    try {
      // Use backend API to fetch deployed models from Azure
      const response = await llmConfigService.getAzureDeployedModels(endpoint.trim(), apiKey.trim())

      console.log('[Azure Deployments API] Response:', response)

      if (response.error) {
        throw new Error(response.error)
      }

      // Extract model IDs from the response
      const models = (response.models || []).map((m: ModelMetadata) => m.model_id)

      console.log('[Azure Deployments API] Deployed models:', models)
      setAzureModels(models)
      setIsAuthenticated(true)

      // Update config with credentials
      onUpdate({
        ...config,
        api_key: apiKey,
        endpoint: endpoint,
        region: region,
        options: { ...config.options, api_version: apiVersion }
      })
    } catch (err) {
      setAuthError(err instanceof Error ? err.message : 'Authentication failed')
      setIsAuthenticated(false)
    } finally {
      setIsAuthenticating(false)
      setIsFetchingModels(false)
    }
  }

  // Refresh models list
  const handleRefreshModels = async () => {
    if (!isAuthenticated) return
    setIsFetchingModels(true)
    await handleAuthenticate()
  }

  // Drive all models from fetched Azure models, metadata, or store
  const modelsFromMetadata = metadata?.filter(m => m.provider === 'azure').map(m => m.model_id) || []
  const allModels = Array.from(new Set([...azureModels, ...modelsFromMetadata, ...availableAzureModels, ...customAzureModels]))

  const handleAddCustomModel = () => {
    if (newCustomModel.trim() && !allModels.includes(newCustomModel.trim())) {
      addCustomAzureModel(newCustomModel.trim())
      setNewCustomModel('')
      refreshAvailableLLMs()
    }
  }

  const handleRemoveCustomModel = (model: string) => {
    removeCustomAzureModel(model)
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

    // Validate API key and endpoint are required for Azure
    if (!apiKey.trim()) {
      setPublishError('API key is required to publish Azure models')
      return
    }
    if (!endpoint.trim()) {
      setPublishError('Endpoint URL is required to publish Azure models')
      return
    }

    setIsSubmitting(true)
    setPublishError(null)

    try {
      // Test API key before saving - merge temperature and Azure-specific options
      const optionsWithAzure = {
        ...config.options,
        endpoint,
        region,
        api_version: apiVersion,
        ...(config.temperature !== undefined ? { temperature: config.temperature } : {})
      }
      const testResult = await testAPIKeyFromStore('azure', apiKey, config.model_id, optionsWithAzure)

      if (testResult.valid) {
        // Check if backend returned a corrected/optimized endpoint
        const finalEndpoint = (testResult.correctedOptions?.endpoint as string) || endpoint
        const finalModelId = (testResult.correctedOptions?.model_id as string) || config.model_id

        // If endpoint was corrected, update the UI state and show message
        if (testResult.correctedOptions?.endpoint && testResult.correctedOptions.endpoint !== endpoint) {
          setEndpoint(testResult.correctedOptions.endpoint as string)
          setEndpointCorrectionMessage(`Endpoint optimized to: ${testResult.correctedOptions.endpoint}`)
          console.log(`[Azure] Endpoint optimized: ${endpoint} -> ${testResult.correctedOptions.endpoint}`)
        } else {
          setEndpointCorrectionMessage(null)
        }

        const llmModel = {
          provider: 'azure' as const,
          model_id: finalModelId,
          api_key: apiKey,     // Use local apiKey state (user input)
          endpoint: finalEndpoint,  // Use corrected endpoint if provided
          region: region,      // Include Azure region
          options: { ...config.options, api_version: apiVersion },  // Include api_version in options
          temperature: config.temperature
        }

        const currentModelMetadata = metadata?.find(m => m.model_id === finalModelId)
        saveLLM(llmModel, publishName.trim(), currentModelMetadata?.model_name, 'api_key')
        setPublishName('')
        setIsPublishing(false)
        setIsSubmitting(false)
        // Also update the status in parent via callback with corrected endpoint
        await onTestAPIKey(apiKey, finalModelId, { ...config.options, endpoint: finalEndpoint, region, api_version: apiVersion }, config.temperature)

        // Update the config with corrected endpoint
        if (finalEndpoint !== endpoint) {
          onUpdate({ ...config, endpoint: finalEndpoint, model_id: finalModelId })
        }
      } else {
        setPublishError(testResult.error || 'API key validation failed. Please check your API key and endpoint.')
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
        <h3 className="text-lg font-semibold text-foreground">Azure AI Configuration</h3>
        {isLocked && (
          <div className="flex items-center gap-1.5 px-2 py-1 bg-yellow-500/10 border border-yellow-500/20 rounded text-yellow-600 dark:text-yellow-500 text-xs font-medium">
            <Key className="w-3.5 h-3.5" />
            Locked by Admin
          </div>
        )}
      </div>

      {/* Step 1: Azure Credentials */}
      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-4">Azure Credentials</h4>
        <div className="space-y-4">
          {isLocked && (
            <div className="text-xs text-muted-foreground bg-secondary/30 p-2 rounded border border-border/50 mb-2">
              Configuration for this provider is managed by your administrator and cannot be changed.
            </div>
          )}
          {/* Endpoint */}
          <div className="space-y-2">
            <div className="flex items-center gap-2">
              <Globe className="w-4 h-4 text-muted-foreground" />
              <label className="text-sm font-medium text-foreground">Endpoint URL</label>
            </div>
            <input
              type="text"
              value={endpoint}
              onChange={(e) => handleEndpointChange(e.target.value)}
              placeholder={isLocked ? "••••••••••••••••" : "https://your-resource.services.ai.azure.com"}
              className="w-full px-3 py-2 border border-border rounded-md bg-background text-foreground focus:ring-2 focus:ring-primary focus:border-primary"
              disabled={isAuthenticating || isLocked}
            />
            {!isLocked && (
              <p className="text-xs text-muted-foreground">
                Azure AI Services endpoint (e.g., https://your-resource.services.ai.azure.com)
              </p>
            )}
          </div>

          {/* API Key */}
          <div className="space-y-2">
            <div className="flex items-center gap-2">
              <Key className="w-4 h-4 text-muted-foreground" />
              <label className="text-sm font-medium text-foreground">API Key</label>
            </div>
            <input
              type="password"
              value={apiKey}
              onChange={(e) => handleAPIKeyChange(e.target.value)}
              placeholder={isLocked ? "••••••••••••••••" : "Enter your Azure API key"}
              className="w-full px-3 py-2 border border-border rounded-md bg-background text-foreground focus:ring-2 focus:ring-primary focus:border-primary"
              disabled={isAuthenticating || isLocked}
            />
          </div>

          {/* Region (optional) */}
          <div className="space-y-2">
            <div className="flex items-center gap-2">
              <MapPin className="w-4 h-4 text-muted-foreground" />
              <label className="text-sm font-medium text-foreground">Region (optional)</label>
            </div>
            <input
              type="text"
              value={region}
              onChange={(e) => handleRegionChange(e.target.value)}
              placeholder="e.g., eastus, westeurope"
              className="w-full px-3 py-2 border border-border rounded-md bg-background text-foreground focus:ring-2 focus:ring-primary focus:border-primary"
              disabled={isAuthenticating || isLocked}
            />
            {!isLocked && (
              <p className="text-xs text-muted-foreground">
                Azure region for reference (usually embedded in endpoint URL)
              </p>
            )}
          </div>

          {/* API Version */}
          <div className="space-y-2">
            <div className="flex items-center gap-2">
              <Key className="w-4 h-4 text-muted-foreground" />
              <label className="text-sm font-medium text-foreground">API Version</label>
            </div>
            <input
              type="text"
              value={apiVersion}
              onChange={(e) => handleApiVersionChange(e.target.value)}
              placeholder="v1"
              className="w-full px-3 py-2 border border-border rounded-md bg-background text-foreground focus:ring-2 focus:ring-primary focus:border-primary"
              disabled={isAuthenticating || isLocked}
            />
            {!isLocked && (
              <p className="text-xs text-muted-foreground">
                API version for Azure AI Foundry (use "v1" for Responses API, or specific version like "2024-10-21")
              </p>
            )}
          </div>

          {/* Authenticate Button */}
          {!isLocked && (
            <div className="pt-2">
              <Button
                onClick={handleAuthenticate}
                disabled={!endpoint.trim() || !apiKey.trim() || isAuthenticating}
                className="w-full"
              >
                {isAuthenticating ? (
                  <>
                    <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                    Authenticating...
                  </>
                ) : isAuthenticated ? (
                  <>
                    <CheckCircle className="w-4 h-4 mr-2 text-green-500" />
                    Authenticated - {azureModels.length} models found
                  </>
                ) : (
                  'Authenticate & Fetch Models'
                )}
              </Button>
            </div>
          )}

          {/* Auth Error */}
          {authError && (
            <div className="text-sm text-red-600 dark:text-red-400 flex items-center gap-1 bg-red-50 dark:bg-red-900/20 p-2 rounded-md">
              <AlertCircle className="w-4 h-4 flex-shrink-0" />
              <span>{authError}</span>
            </div>
          )}

          {/* Auth Success */}
          {isAuthenticated && !authError && (
            <div className="text-sm text-green-600 dark:text-green-400 flex items-center gap-1 bg-green-50 dark:bg-green-900/20 p-2 rounded-md">
              <CheckCircle className="w-4 h-4 flex-shrink-0" />
              <span>Successfully connected to Azure AI. Found {azureModels.length} available models.</span>
            </div>
          )}
        </div>
      </Card>

      {/* Step 2: Model Selection (only shown after authentication) */}
      {(isAuthenticated || isLocked) && (
        <Card className="p-4">
          <div className="flex items-center justify-between mb-4">
            <h4 className="font-medium text-foreground">Model Selection</h4>
            {!isLocked && (
              <Button
                onClick={handleRefreshModels}
                disabled={isFetchingModels}
                size="sm"
                variant="outline"
              >
                {isFetchingModels ? (
                  <Loader2 className="w-4 h-4 animate-spin" />
                ) : (
                  <RefreshCw className="w-4 h-4" />
                )}
              </Button>
            )}
          </div>
          <div className="space-y-4">
            <div>
              <label className="block text-sm font-medium text-muted-foreground mb-2">Primary Model</label>
              <ModelSelector
                value={config.model_id}
                onChange={(val) => onUpdate({ ...config, model_id: val })}
                models={allModels}
                metadata={metadata || []}
                placeholder="Select an Azure model"
                disabled={isLocked && allModels.length <= 1}
              />
              <ModelOptionsConfig
                metadata={currentModelMetadata}
                options={config.options || {}}
                temperature={config.temperature}
                onChange={handleOptionsChange}
                disabled={isLocked}
              />
            </div>

            {/* Test Model */}
            {!isLocked && (
              <div className="border-t border-border pt-4 space-y-3">
                <div className="flex gap-2">
                  <Button
                    onClick={() => onTestAPIKey(apiKey, config.model_id, { ...config.options, endpoint, region, api_version: apiVersion }, config.temperature)}
                    disabled={!config.model_id || apiKeyStatus === 'testing'}
                    size="sm"
                    variant="outline"
                    className="flex-1"
                  >
                    {apiKeyStatus === 'testing' ? (
                      <>
                        <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                        Testing...
                      </>
                    ) : apiKeyStatus === 'valid' ? (
                      <>
                        <CheckCircle className="w-4 h-4 mr-2 text-green-500" />
                        Model Working
                      </>
                    ) : apiKeyStatus === 'invalid' ? (
                      <>
                        <AlertCircle className="w-4 h-4 mr-2 text-red-500" />
                        Test Failed
                      </>
                    ) : (
                      'Test Model'
                    )}
                  </Button>
                </div>
                {apiKeyStatus === 'valid' && (
                  <div className="text-sm text-green-600 dark:text-green-400 flex items-center gap-1">
                    <CheckCircle className="w-4 h-4" />
                    Model is working correctly
                  </div>
                )}
                {endpointCorrectionMessage && (
                  <div className="text-sm text-blue-600 dark:text-blue-400 flex items-center gap-1 bg-blue-50 dark:bg-blue-900/20 p-2 rounded-md">
                    <CheckCircle className="w-4 h-4" />
                    {endpointCorrectionMessage}
                  </div>
                )}
                {apiKeyStatus === 'invalid' && (
                  <div className="text-sm text-red-600 dark:text-red-400 flex items-center gap-1">
                    <AlertCircle className="w-4 h-4" />
                    {apiKeyError || 'Model test failed'}
                  </div>
                )}
              </div>
            )}

            {/* Publish to Library */}
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
                    Publish to Library
                  </Button>
                )}
              </div>
            )}
          </div>
        </Card>
      )}

      {/* Custom Models (only shown after authentication) */}
      {!isLocked && isAuthenticated && (
        <Card className="p-4">
          <h4 className="font-medium text-foreground mb-4">Custom Models</h4>
          <div className="space-y-3">
            <div className="flex gap-2">
              <input
                type="text"
                value={newCustomModel}
                onChange={(e) => setNewCustomModel(e.target.value)}
                placeholder="Enter custom model/deployment name"
                className="flex-1 px-3 py-2 border border-border rounded-md bg-background text-foreground focus:ring-2 focus:ring-primary focus:border-primary"
                onKeyPress={(e) => e.key === 'Enter' && handleAddCustomModel()}
              />
              <Button onClick={handleAddCustomModel} disabled={!newCustomModel.trim() || allModels.includes(newCustomModel.trim())} size="sm" variant="outline">
                <Plus className="w-4 h-4" />
              </Button>
            </div>
            <p className="text-xs text-muted-foreground">
              Add custom deployment names if your model isn't listed above
            </p>
            {customAzureModels.length > 0 && (
              <div className="space-y-2">
                <h5 className="text-sm font-medium text-muted-foreground">Custom Models:</h5>
                <div className="space-y-1">
                  {customAzureModels.map((model) => (
                    <div key={model} className="flex items-center justify-between bg-muted/50 p-2 rounded-md">
                      <span className="text-sm text-foreground font-mono">{model}</span>
                      <Button onClick={() => handleRemoveCustomModel(model)} size="sm" variant="ghost" className="text-red-600 hover:text-red-700 hover:bg-red-50 dark:text-red-400 dark:hover:text-red-300 dark:hover:bg-red-900/20">
                        <Trash2 className="w-4 h-4" />
                      </Button>
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
