import { useEffect, useMemo, useState } from 'react'
import { Terminal, CheckCircle, AlertCircle, Loader2, KeyRound, ExternalLink } from 'lucide-react'
import { Button } from '../ui/Button'
import { Card } from '../ui/Card'
import { TierModelSelector } from '../ui/TierModelSelector'
import { DynamicModelSelector } from '../ui/DynamicModelSelector'
import { CodingAgentCapabilities } from './CodingAgentCapabilities'
import { useLLMStore } from '../../stores'
import { llmConfigService, type ModelMetadata, type ProviderManifestEntry } from '../../services/llm-config-api'

// isOpenCodeSubProvider identifies tiles whose API key should be stored in
// the per-env-var sub-key map rather than the legacy single OpenCode key.
function isOpenCodeSubProvider(id: string): boolean {
  return id.startsWith('opencode-cli-')
}

interface CodingAgentSectionProps {
  provider: ProviderManifestEntry
  onPublished?: () => void
}

export function CodingAgentSection({ provider, onPublished }: CodingAgentSectionProps) {
  const {
    saveLLM,
    savedLLMs,
    openCodeCliSubKeys,
    setOpenCodeCliSubKey,
    clearOpenCodeCliSubKey,
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

  // --- Credential card state (OpenCode sub-provider tiles only) ---
  const showCredentialCard =
    provider.requires_api_key &&
    !!provider.api_key_env &&
    isOpenCodeSubProvider(provider.id)
  const storedSubKey = provider.api_key_env
    ? openCodeCliSubKeys?.[provider.api_key_env] || ''
    : ''
  const [credentialDraft, setCredentialDraft] = useState(storedSubKey)
  const [credentialDirty, setCredentialDirty] = useState(false)
  const [revealCredential, setRevealCredential] = useState(false)
  useEffect(() => {
    // Sync the input when the underlying stored key changes outside this
    // component (e.g. on initial workspace load or another tab editing it).
    if (!credentialDirty) setCredentialDraft(storedSubKey)
  }, [storedSubKey, credentialDirty])

  const models = useMemo(() => provider.models || [], [provider.models])
  const currentModelMetadata = models.find(m => m.model_id === selectedModel) as ModelMetadata | undefined
  const isDynamic = provider.model_selection_mode === 'dynamic'

  const effortLevels = currentModelMetadata?.reasoning_effort_levels?.length
    ? currentModelMetadata.reasoning_effort_levels
    : ['low', 'medium', 'high', 'max']
  const showEffort = currentModelMetadata?.supports_reasoning_effort

  useEffect(() => {
    if (isDynamic || models.length === 0) return
    const exists = models.some(m => m.model_id === selectedModel)
    if (!exists) setSelectedModel(models[0].model_id)
  }, [models, selectedModel, isDynamic])

  const alreadyPublished = savedLLMs.some(
    llm => llm.provider === provider.id && llm.model_id === selectedModel
  )

  const handleSaveCredential = () => {
    if (!provider.api_key_env) return
    const trimmed = credentialDraft.trim()
    if (trimmed) {
      setOpenCodeCliSubKey(provider.api_key_env, trimmed)
    } else {
      clearOpenCodeCliSubKey(provider.api_key_env)
    }
    setCredentialDirty(false)
    setTestStatus('idle')
    setTestMessage(null)
  }
  const handleClearCredential = () => {
    if (!provider.api_key_env) return
    clearOpenCodeCliSubKey(provider.api_key_env)
    setCredentialDraft('')
    setCredentialDirty(false)
    setTestStatus('idle')
    setTestMessage(null)
  }

  const handleTestConnection = async () => {
    setTestStatus('testing')
    setTestMessage(null)
    try {
      // For OpenCode sub-provider tiles, send the in-memory draft key so
      // the user can verify a credential without first hitting "Save".
      const inlineApiKey = showCredentialCard
        ? credentialDraft.trim() || storedSubKey
        : undefined
      const response = await llmConfigService.validateAPIKey({
        provider: provider.id as Parameters<typeof llmConfigService.validateAPIKey>[0]['provider'],
        model_id: selectedModel !== provider.id ? selectedModel : undefined,
        api_key: inlineApiKey,
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

  const handlePublishToLibrary = async () => {
    if (!publishName.trim()) return
    setIsSubmitting(true)
    setPublishError(null)
    try {
      const options: Record<string, unknown> = {}
      if (showEffort) options.reasoning_effort = effortLevel

      const llmModel = {
        // provider.id may be one of the legacy CLI ids (claude-code,
        // codex-cli, gemini-cli, cursor-cli, opencode-cli) or one of the
        // OpenCode sub-provider tiles (opencode-cli-kimi, ...,
        // opencode-cli-free). The LLMProvider union covers all of them.
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

      {/* API key entry — only for OpenCode sub-provider tiles that require
          a credential. The legacy `opencode-cli` tile and the other CLI
          providers (Claude Code, Codex, Gemini, Cursor) authenticate via
          their own native flows and don't render this card. */}
      {showCredentialCard && (
        <Card className="p-4">
          <div className="flex items-start gap-3 mb-3">
            <KeyRound className="w-5 h-5 text-primary mt-0.5 flex-shrink-0" />
            <div className="flex-1">
              <h4 className="font-medium text-foreground">{provider.display_name} API key</h4>
              <p className="text-sm text-muted-foreground">
                Stored encrypted in your workspace and exported as
                <code className="bg-secondary px-1 py-0.5 rounded mx-1">{provider.api_key_env}</code>
                only when this tile is the active provider.
              </p>
              {provider.api_key_url && (
                <a
                  href={provider.api_key_url}
                  target="_blank"
                  rel="noreferrer"
                  className="mt-1 inline-flex items-center gap-1 text-xs text-primary hover:underline"
                >
                  Get a key <ExternalLink className="w-3 h-3" />
                </a>
              )}
            </div>
          </div>
          <div className="space-y-2">
            <div className="flex gap-2">
              <input
                type={revealCredential ? 'text' : 'password'}
                value={credentialDraft}
                onChange={(e) => {
                  setCredentialDraft(e.target.value)
                  setCredentialDirty(e.target.value !== storedSubKey)
                  setTestStatus('idle')
                  setTestMessage(null)
                }}
                placeholder={storedSubKey ? '••••••• (stored, paste a new key to replace)' : `Paste your ${provider.api_key_env} key`}
                className="flex-1 px-3 py-2 text-sm bg-background border border-border rounded-md font-mono focus:outline-none focus:ring-1 focus:ring-primary"
                spellCheck={false}
                autoCorrect="off"
                autoCapitalize="off"
              />
              <Button
                variant="outline"
                size="sm"
                onClick={() => setRevealCredential((v) => !v)}
                type="button"
              >
                {revealCredential ? 'Hide' : 'Show'}
              </Button>
            </div>
            <div className="flex items-center gap-2">
              <Button
                size="sm"
                onClick={handleSaveCredential}
                disabled={!credentialDirty}
              >
                {storedSubKey ? 'Update key' : 'Save key'}
              </Button>
              {storedSubKey && (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={handleClearCredential}
                >
                  Remove key
                </Button>
              )}
              {!credentialDirty && storedSubKey && (
                <span className="inline-flex items-center gap-1 text-xs text-green-600 dark:text-green-400">
                  <CheckCircle className="w-3.5 h-3.5" /> Stored
                </span>
              )}
              {credentialDirty && (
                <span className="text-xs text-muted-foreground">Unsaved changes</span>
              )}
            </div>
          </div>
        </Card>
      )}

      {/* Model selection */}
      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-3">Model</h4>
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
