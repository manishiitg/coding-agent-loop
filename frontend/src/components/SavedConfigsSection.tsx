import { useState, useEffect } from 'react'
import { Trash2, Edit2, CheckCircle, Star, Copy, ChevronDown, ChevronUp, ChevronRight } from 'lucide-react'
import { Button } from './ui/Button'
import { Card } from './ui/Card'
import { useSavedLLMConfigsStore } from '../stores/useSavedLLMConfigsStore'
import { useLLMStore } from '../stores/useLLMStore'
import type { SavedLLMConfig, LLMProvider, LLMOptions, FallbackModel } from '../services/api-types'

// ═══════════════════════════════════════════════════════════════════
// SAVED CONFIGS SECTION
// View, select, edit saved LLM configurations
// To create new configs, use "Save as Configuration" in provider tabs
// ═══════════════════════════════════════════════════════════════════

interface SavedConfigsSectionProps {
  getAvailableModelsForProvider: (provider: LLMProvider) => string[]
  onSwitchToProvider?: (provider: LLMProvider) => void  // Optional: switch to provider tab
}

// Provider badge colors (matching the modal's styling)
const getProviderColor = (provider: string) => {
  switch (provider) {
    case 'openrouter': return 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200'
    case 'bedrock': return 'bg-orange-100 text-orange-800 dark:bg-orange-900 dark:text-orange-200'
    case 'openai': return 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200'
    case 'vertex': return 'bg-purple-100 text-purple-800 dark:bg-purple-900 dark:text-purple-200'
    case 'anthropic': return 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200'
    default: return 'bg-gray-100 text-gray-800 dark:bg-gray-900 dark:text-gray-200'
  }
}

const ALL_PROVIDERS: LLMProvider[] = ['openrouter', 'bedrock', 'openai', 'vertex', 'anthropic']

export function SavedConfigsSection({ getAvailableModelsForProvider, onSwitchToProvider }: SavedConfigsSectionProps) {
  const {
    configs,
    primaryConfigId,
    fallbackConfigIds,
    updateConfig,
    deleteConfig,
    duplicateConfig,
    setPrimaryConfigId,
    addFallbackConfigId,
    removeFallbackConfigId,
    reorderFallbackConfigIds
  } = useSavedLLMConfigsStore()

  const { setPrimaryConfig } = useLLMStore()

  // ─────────────────────────────────────────────────────────────────
  // Sync saved config selections to useLLMStore
  // This ensures the primaryConfig sent to backend reflects saved config choices
  // ─────────────────────────────────────────────────────────────────
  useEffect(() => {
    // Get the primary saved config
    const primaryConfig = primaryConfigId 
      ? configs.find(c => c.id === primaryConfigId)
      : null

    if (primaryConfig) {
      // Convert fallback configs to FallbackModel array
      const fallbackModels: FallbackModel[] = fallbackConfigIds
        .map((id, index) => {
          const config = configs.find(c => c.id === id)
          if (!config) return null
          const fm: FallbackModel = {
            model_id: config.model_id,
            provider: config.provider,
            priority: index + 1
          }
          if (config.options) {
            fm.options = config.options
          }
          return fm
        })
        .filter((fm): fm is FallbackModel => fm !== null)

      // Update the LLM store with the resolved config
      setPrimaryConfig({
        provider: primaryConfig.provider,
        model_id: primaryConfig.model_id,
        fallback_models: fallbackModels,
        options: primaryConfig.options
      })
    }
  }, [configs, primaryConfigId, fallbackConfigIds, setPrimaryConfig])

  // Edit mode state
  const [editingConfigId, setEditingConfigId] = useState<string | null>(null)
  const [editFormName, setEditFormName] = useState('')
  const [editFormProvider, setEditFormProvider] = useState<LLMProvider>('openrouter')
  const [editFormModelId, setEditFormModelId] = useState('')
  const [editFormTemperature, setEditFormTemperature] = useState<number | undefined>(undefined)
  const [editFormMaxTokens, setEditFormMaxTokens] = useState<number | undefined>(undefined)
  const [showEditAdvanced, setShowEditAdvanced] = useState(false)

  // Start editing a config
  const handleEditConfig = (config: SavedLLMConfig) => {
    setEditingConfigId(config.id)
    setEditFormName(config.name)
    setEditFormProvider(config.provider)
    setEditFormModelId(config.model_id)
    setEditFormTemperature(config.options?.temperature)
    setEditFormMaxTokens(config.options?.max_tokens)
    setShowEditAdvanced(!!config.options?.temperature || !!config.options?.max_tokens)
  }

  // Cancel editing
  const handleCancelEdit = () => {
    setEditingConfigId(null)
    setEditFormName('')
    setEditFormProvider('openrouter')
    setEditFormModelId('')
    setEditFormTemperature(undefined)
    setEditFormMaxTokens(undefined)
    setShowEditAdvanced(false)
  }

  // Handle provider change in edit form
  const handleEditProviderChange = (provider: LLMProvider) => {
    setEditFormProvider(provider)
    const models = getAvailableModelsForProvider(provider)
    setEditFormModelId(models[0] || '')
  }

  // Save edited config
  const handleSaveEdit = () => {
    if (!editingConfigId || !editFormName.trim() || !editFormModelId) return

    const options: LLMOptions | undefined = 
      (editFormTemperature !== undefined || editFormMaxTokens !== undefined)
        ? {
            temperature: editFormTemperature,
            max_tokens: editFormMaxTokens
          }
        : undefined

    updateConfig(editingConfigId, {
      name: editFormName.trim(),
      provider: editFormProvider,
      model_id: editFormModelId,
      options
    })

    handleCancelEdit()
  }

  // Handle delete with confirmation
  const handleDeleteConfig = (id: string, name: string) => {
    if (window.confirm(`Delete "${name}"? This cannot be undone.`)) {
      deleteConfig(id)
    }
  }

  // Handle duplicate
  const handleDuplicateConfig = (id: string, name: string) => {
    duplicateConfig(id, `${name} (Copy)`)
  }

  // Toggle primary selection
  const handleTogglePrimary = (id: string) => {
    if (primaryConfigId === id) {
      setPrimaryConfigId(null)
    } else {
      // Remove from fallbacks if it was there
      if (fallbackConfigIds.includes(id)) {
        removeFallbackConfigId(id)
      }
      setPrimaryConfigId(id)
    }
  }

  // Toggle fallback selection
  const handleToggleFallback = (id: string) => {
    if (fallbackConfigIds.includes(id)) {
      removeFallbackConfigId(id)
    } else if (primaryConfigId !== id) {
      addFallbackConfigId(id)
    }
  }

  // Move fallback up/down
  const handleMoveFallback = (id: string, direction: 'up' | 'down') => {
    const currentIndex = fallbackConfigIds.indexOf(id)
    if (currentIndex === -1) return

    const newIndex = direction === 'up' ? currentIndex - 1 : currentIndex + 1
    if (newIndex < 0 || newIndex >= fallbackConfigIds.length) return

    const newOrder = [...fallbackConfigIds]
    ;[newOrder[currentIndex], newOrder[newIndex]] = [newOrder[newIndex], newOrder[currentIndex]]
    reorderFallbackConfigIds(newOrder)
  }

  const editAvailableModels = getAvailableModelsForProvider(editFormProvider)

  return (
    <div className="space-y-6">
      {/* Header */}
      <div>
        <h2 className="text-xl font-semibold text-foreground">Saved Configurations</h2>
        <p className="text-sm text-muted-foreground mt-1">
          Select saved configs as primary or fallback. To create new configs, configure and test in provider tabs, then click "Save as Configuration".
        </p>
      </div>

      {/* Quick Tips */}
      {configs.length === 0 && (
        <Card className="p-6 bg-muted/30 border-dashed">
          <h3 className="font-medium text-foreground mb-3">Getting Started</h3>
          <ol className="text-sm text-muted-foreground space-y-2">
            <li className="flex items-start gap-2">
              <span className="font-medium text-primary">1.</span>
              Go to a provider tab (OpenRouter, OpenAI, etc.)
            </li>
            <li className="flex items-start gap-2">
              <span className="font-medium text-primary">2.</span>
              Enter your API key and test it
            </li>
            <li className="flex items-start gap-2">
              <span className="font-medium text-primary">3.</span>
              Select a model and configure options
            </li>
            <li className="flex items-start gap-2">
              <span className="font-medium text-primary">4.</span>
              Click "Save as Configuration" to save it here
            </li>
          </ol>
          {onSwitchToProvider && (
            <div className="mt-4 pt-4 border-t border-border">
              <p className="text-sm text-muted-foreground mb-2">Quick links:</p>
              <div className="flex flex-wrap gap-2">
                {ALL_PROVIDERS.map(provider => (
                  <Button
                    key={provider}
                    variant="outline"
                    size="sm"
                    onClick={() => onSwitchToProvider(provider)}
                    className="flex items-center gap-1"
                  >
                    {provider.charAt(0).toUpperCase() + provider.slice(1)}
                    <ChevronRight className="w-3 h-3" />
                  </Button>
                ))}
              </div>
            </div>
          )}
        </Card>
      )}

      {/* Configs List */}
      {configs.length > 0 && (
        <div className="space-y-3">
          {configs.map((config) => {
            const isPrimary = primaryConfigId === config.id
            const fallbackIndex = fallbackConfigIds.indexOf(config.id)
            const isFallback = fallbackIndex !== -1
            const isEditing = editingConfigId === config.id

            if (isEditing) {
              // Inline Edit Form
              return (
                <Card key={config.id} className="p-4 ring-2 ring-primary">
                  <h4 className="font-medium text-foreground mb-4">Edit Configuration</h4>
                  <div className="space-y-4">
                    {/* Name */}
                    <div>
                      <label className="block text-sm font-medium text-muted-foreground mb-1">
                        Name <span className="text-destructive">*</span>
                      </label>
                      <input
                        type="text"
                        value={editFormName}
                        onChange={(e) => setEditFormName(e.target.value)}
                        placeholder="e.g., Production Claude"
                        className="w-full px-3 py-2 border border-border rounded-md bg-background text-foreground focus:ring-2 focus:ring-primary focus:border-primary"
                      />
                    </div>

                    {/* Provider */}
                    <div>
                      <label className="block text-sm font-medium text-muted-foreground mb-1">
                        Provider
                      </label>
                      <select
                        value={editFormProvider}
                        onChange={(e) => handleEditProviderChange(e.target.value as LLMProvider)}
                        className="w-full px-3 py-2 border border-border rounded-md bg-background text-foreground focus:ring-2 focus:ring-primary focus:border-primary"
                      >
                        {ALL_PROVIDERS.map((provider) => (
                          <option key={provider} value={provider}>
                            {provider.charAt(0).toUpperCase() + provider.slice(1)}
                          </option>
                        ))}
                      </select>
                    </div>

                    {/* Model */}
                    <div>
                      <label className="block text-sm font-medium text-muted-foreground mb-1">
                        Model
                      </label>
                      <select
                        value={editFormModelId}
                        onChange={(e) => setEditFormModelId(e.target.value)}
                        className="w-full px-3 py-2 border border-border rounded-md bg-background text-foreground focus:ring-2 focus:ring-primary focus:border-primary"
                      >
                        <option value="">Select model...</option>
                        {editAvailableModels.map((model) => (
                          <option key={model} value={model}>{model}</option>
                        ))}
                      </select>
                    </div>

                    {/* Advanced Options Toggle */}
                    <button
                      type="button"
                      onClick={() => setShowEditAdvanced(!showEditAdvanced)}
                      className="flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground"
                    >
                      {showEditAdvanced ? <ChevronUp className="w-4 h-4" /> : <ChevronDown className="w-4 h-4" />}
                      Advanced Options
                    </button>

                    {/* Advanced Options */}
                    {showEditAdvanced && (
                      <div className="space-y-4 pl-4 border-l-2 border-muted">
                        <div>
                          <label className="block text-sm font-medium text-muted-foreground mb-1">
                            Temperature
                          </label>
                          <input
                            type="number"
                            min="0"
                            max="2"
                            step="0.1"
                            value={editFormTemperature ?? ''}
                            onChange={(e) => setEditFormTemperature(e.target.value ? parseFloat(e.target.value) : undefined)}
                            placeholder="0.0 - 2.0"
                            className="w-full px-3 py-2 border border-border rounded-md bg-background text-foreground focus:ring-2 focus:ring-primary focus:border-primary"
                          />
                        </div>
                        <div>
                          <label className="block text-sm font-medium text-muted-foreground mb-1">
                            Max Tokens
                          </label>
                          <input
                            type="number"
                            min="1"
                            value={editFormMaxTokens ?? ''}
                            onChange={(e) => setEditFormMaxTokens(e.target.value ? parseInt(e.target.value) : undefined)}
                            placeholder="e.g., 4096"
                            className="w-full px-3 py-2 border border-border rounded-md bg-background text-foreground focus:ring-2 focus:ring-primary focus:border-primary"
                          />
                        </div>
                      </div>
                    )}

                    {/* Actions */}
                    <div className="flex items-center justify-end gap-2 pt-2">
                      <Button variant="outline" onClick={handleCancelEdit}>
                        Cancel
                      </Button>
                      <Button onClick={handleSaveEdit} disabled={!editFormName.trim() || !editFormModelId}>
                        Save Changes
                      </Button>
                    </div>
                  </div>
                </Card>
              )
            }

            // Normal view
            return (
              <Card 
                key={config.id} 
                className={`p-4 ${isPrimary ? 'ring-2 ring-primary' : isFallback ? 'ring-1 ring-blue-400' : ''}`}
              >
                <div className="flex items-start justify-between gap-4">
                  {/* Config Info */}
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 mb-1">
                      <span className="font-medium text-foreground truncate">{config.name}</span>
                      {isPrimary && (
                        <span className="flex items-center gap-1 text-xs bg-primary text-primary-foreground px-2 py-0.5 rounded">
                          <Star className="w-3 h-3" /> Primary
                        </span>
                      )}
                      {isFallback && (
                        <span className="text-xs bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200 px-2 py-0.5 rounded">
                          Fallback #{fallbackIndex + 1}
                        </span>
                      )}
                    </div>
                    <div className="flex items-center gap-2">
                      <span className={`text-xs px-2 py-0.5 rounded ${getProviderColor(config.provider)}`}>
                        {config.provider}
                      </span>
                      <span className="text-sm text-muted-foreground truncate">{config.model_id}</span>
                    </div>
                    {config.options && (config.options.temperature !== undefined || config.options.max_tokens !== undefined) && (
                      <div className="text-xs text-muted-foreground mt-1">
                        {config.options.temperature !== undefined && `temp: ${config.options.temperature}`}
                        {config.options.temperature !== undefined && config.options.max_tokens !== undefined && ' • '}
                        {config.options.max_tokens !== undefined && `max_tokens: ${config.options.max_tokens}`}
                      </div>
                    )}
                  </div>

                  {/* Actions */}
                  <div className="flex items-center gap-1 flex-shrink-0">
                    {/* Primary Toggle */}
                    <Button
                      size="sm"
                      variant={isPrimary ? "default" : "outline"}
                      onClick={() => handleTogglePrimary(config.id)}
                      title={isPrimary ? "Remove as primary" : "Set as primary"}
                      className="h-8 px-2"
                    >
                      <Star className={`w-4 h-4 ${isPrimary ? 'fill-current' : ''}`} />
                    </Button>

                    {/* Fallback Toggle */}
                    <Button
                      size="sm"
                      variant={isFallback ? "default" : "outline"}
                      onClick={() => handleToggleFallback(config.id)}
                      disabled={isPrimary}
                      title={isFallback ? "Remove from fallbacks" : "Add to fallbacks"}
                      className="h-8 px-2"
                    >
                      <CheckCircle className={`w-4 h-4 ${isFallback ? 'fill-current' : ''}`} />
                    </Button>

                    {/* Move Up/Down for Fallbacks */}
                    {isFallback && (
                      <>
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => handleMoveFallback(config.id, 'up')}
                          disabled={fallbackIndex === 0}
                          title="Move up"
                          className="h-8 px-2"
                        >
                          <ChevronUp className="w-4 h-4" />
                        </Button>
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => handleMoveFallback(config.id, 'down')}
                          disabled={fallbackIndex === fallbackConfigIds.length - 1}
                          title="Move down"
                          className="h-8 px-2"
                        >
                          <ChevronDown className="w-4 h-4" />
                        </Button>
                      </>
                    )}

                    {/* Edit */}
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => handleEditConfig(config)}
                      title="Edit"
                      className="h-8 px-2"
                    >
                      <Edit2 className="w-4 h-4" />
                    </Button>

                    {/* Duplicate */}
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => handleDuplicateConfig(config.id, config.name)}
                      title="Duplicate"
                      className="h-8 px-2"
                    >
                      <Copy className="w-4 h-4" />
                    </Button>

                    {/* Delete */}
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => handleDeleteConfig(config.id, config.name)}
                      title="Delete"
                      className="h-8 px-2 text-destructive hover:text-destructive"
                    >
                      <Trash2 className="w-4 h-4" />
                    </Button>
                  </div>
                </div>
              </Card>
            )
          })}
        </div>
      )}

      {/* Selection Summary */}
      {(primaryConfigId || fallbackConfigIds.length > 0) && (
        <Card className="p-4 bg-muted/50">
          <h4 className="font-medium text-foreground mb-2">Current Selection</h4>
          <div className="space-y-1 text-sm">
            {primaryConfigId && (
              <div className="flex items-center gap-2">
                <Star className="w-4 h-4 text-primary" />
                <span className="text-muted-foreground">Primary:</span>
                <span className="font-medium">{configs.find(c => c.id === primaryConfigId)?.name || 'Unknown'}</span>
              </div>
            )}
            {fallbackConfigIds.length > 0 && (
              <div className="flex items-start gap-2">
                <CheckCircle className="w-4 h-4 text-blue-500 mt-0.5" />
                <span className="text-muted-foreground">Fallbacks:</span>
                <span className="font-medium">
                  {fallbackConfigIds.map((id) => {
                    const config = configs.find(c => c.id === id)
                    return config?.name || 'Unknown'
                  }).join(' → ')}
                </span>
              </div>
            )}
          </div>
        </Card>
      )}
    </div>
  )
}
