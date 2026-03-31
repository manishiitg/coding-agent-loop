import { useEffect, useMemo, useState } from 'react'
import { Terminal, CheckCircle, AlertCircle, Loader2, Eye, EyeOff, ChevronDown } from 'lucide-react'
import { Button } from './ui/Button'
import { Card } from './ui/Card'
import { useLLMStore } from '../stores'
import { llmConfigService, type ModelMetadata } from '../services/llm-config-api'

interface GeminiCLISectionProps {
  onPublished?: () => void
  onModelChange?: (modelId: string) => void
  metadata?: ModelMetadata[]
}

export function GeminiCLISection({ onPublished, onModelChange, metadata = [] }: GeminiCLISectionProps) {
  const [isPublishing, setIsPublishing] = useState(false)
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [publishName, setPublishName] = useState('')
  const [publishStatus, setPublishStatus] = useState<'idle' | 'success' | 'error'>('idle')
  const [publishError, setPublishError] = useState<string | null>(null)
  const [showApiKey, setShowApiKey] = useState(false)
  const { saveLLM, savedLLMs, geminiCliApiKey, setGeminiCliApiKey, geminiCliModel, setGeminiCliModel } = useLLMStore()

  // Test connection state
  const [testStatus, setTestStatus] = useState<'idle' | 'testing' | 'valid' | 'invalid'>('idle')
  const [testMessage, setTestMessage] = useState<string | null>(null)
  const geminiModels = useMemo(
    () => metadata.filter(m => m.provider === 'gemini-cli'),
    [metadata]
  )
  const currentModelMetadata = geminiModels.find(m => m.model_id === geminiCliModel)

  useEffect(() => {
    if (geminiModels.length === 0) return
    const modelExists = geminiModels.some(m => m.model_id === geminiCliModel)
    if (!modelExists) {
      const nextModel = geminiModels[0].model_id
      setGeminiCliModel(nextModel)
      onModelChange?.(nextModel)
    }
  }, [geminiModels, geminiCliModel, onModelChange, setGeminiCliModel])

  const alreadyPublished = savedLLMs.some(
    llm => llm.provider === 'gemini-cli' && llm.model_id === geminiCliModel
  )

  const handleTestConnection = async () => {
    setTestStatus('testing')
    setTestMessage(null)

    try {
      const response = await llmConfigService.validateAPIKey({
        provider: 'gemini-cli',
        api_key: geminiCliApiKey || undefined,
      })

      if (response.valid) {
        setTestStatus('valid')
        setTestMessage(response.message || 'Gemini CLI is working.')
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
      const llmModel = {
        provider: 'gemini-cli' as const,
        model_id: geminiCliModel,
      }

      await saveLLM(llmModel, publishName.trim(), `Gemini CLI (${geminiCliModel})`, 'none', currentModelMetadata)
      setPublishName('')
      setIsPublishing(false)
      setPublishStatus('success')
      onPublished?.()
      setTimeout(() => setPublishStatus('idle'), 3000)
    } catch (err) {
      setPublishError(err instanceof Error ? err.message : 'Failed to publish. Please try again.')
      setPublishStatus('error')
    } finally {
      setIsSubmitting(false)
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h3 className="text-lg font-semibold text-foreground">Gemini CLI Configuration</h3>
      </div>

      <Card className="p-4">
        <div className="flex items-start gap-3">
          <Terminal className="w-5 h-5 text-primary mt-0.5 flex-shrink-0" />
          <div className="space-y-2">
            <h4 className="font-medium text-foreground">Local CLI Provider</h4>
            <p className="text-sm text-muted-foreground">
              Gemini CLI uses the locally installed <code className="text-xs bg-secondary px-1 py-0.5 rounded">gemini</code> CLI for inference.
              It handles model selection and tool execution. Provide your Gemini API key below, or leave blank to use the server&apos;s <code className="text-xs bg-secondary px-1 py-0.5 rounded">GEMINI_API_KEY</code> environment variable.
            </p>
            <p className="text-sm text-muted-foreground">
              Some agent features (context summarization, tool search, code execution mode, context editing) are automatically disabled when using this provider.
            </p>
          </div>
        </div>
      </Card>

      {/* API Key */}
      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-3">API Key</h4>
        <p className="text-sm text-muted-foreground mb-3">
          Get your API key from <a href="https://aistudio.google.com/apikey" target="_blank" rel="noopener noreferrer" className="text-primary hover:underline">Google AI Studio</a>.
          Leave blank if <code className="text-xs bg-secondary px-1 py-0.5 rounded">GEMINI_API_KEY</code> is set on the server.
        </p>
        <div className="relative">
          <input
            type={showApiKey ? 'text' : 'password'}
            value={geminiCliApiKey}
            onChange={(e) => setGeminiCliApiKey(e.target.value)}
            className="w-full px-3 py-2 pr-10 text-sm bg-background border border-border rounded-md focus:outline-none focus:ring-1 focus:ring-primary font-mono"
            placeholder="AIza..."
          />
          <button
            type="button"
            onClick={() => setShowApiKey(!showApiKey)}
            className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
          >
            {showApiKey ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
          </button>
        </div>
      </Card>

      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-3">Model</h4>
        <p className="text-sm text-muted-foreground mb-3">
          Select which Gemini model to use. <code className="text-xs bg-secondary px-1 py-0.5 rounded">auto</code> lets the CLI pick the best model automatically.
        </p>
        <div className="relative">
          <select
            value={geminiCliModel}
            onChange={(e) => {
              setGeminiCliModel(e.target.value)
              onModelChange?.(e.target.value)
            }}
            className="w-full appearance-none px-3 py-2 pr-8 text-sm bg-background border border-border rounded-md focus:outline-none focus:ring-1 focus:ring-primary font-mono cursor-pointer"
          >
            {geminiModels.map(model => (
              <option key={model.model_id} value={model.model_id}>
                {model.model_name || model.model_id}
              </option>
            ))}
          </select>
          <ChevronDown className="absolute right-2 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground pointer-events-none" />
        </div>
      </Card>

      {/* Test Connection */}
      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-3">Test Connection</h4>
        <p className="text-sm text-muted-foreground mb-3">
          Sends a test prompt to the Gemini CLI to verify it is installed and authenticated.
        </p>
        <div className="space-y-3">
          <Button
            variant="outline"
            size="sm"
            onClick={handleTestConnection}
            disabled={testStatus === 'testing'}
          >
            {testStatus === 'testing' ? (
              <>
                <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                Testing...
              </>
            ) : testStatus === 'valid' ? (
              <>
                <CheckCircle className="w-4 h-4 mr-2 text-green-500" />
                Test Again
              </>
            ) : testStatus === 'invalid' ? (
              <>
                <AlertCircle className="w-4 h-4 mr-2 text-red-500" />
                Retry Test
              </>
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

      {/* Publish to Library */}
      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-3">Publish to Library</h4>

        {alreadyPublished && !isPublishing && (
          <div className="flex items-center gap-2 text-sm text-green-600 dark:text-green-400 mb-3">
            <CheckCircle className="w-4 h-4" />
            Gemini CLI is already published in your library.
          </div>
        )}

        {publishStatus === 'success' && !isPublishing && (
          <div className="flex items-center gap-2 text-sm text-green-600 dark:text-green-400 mb-3">
            <CheckCircle className="w-4 h-4" />
            Published successfully!
          </div>
        )}

        {!isPublishing ? (
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              setIsPublishing(true)
              setPublishName(`Gemini CLI (${geminiCliModel})`)
              setPublishError(null)
            }}
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
                onChange={(e) => {
                  setPublishName(e.target.value)
                  setPublishError(null)
                }}
                className="w-full px-3 py-2 text-sm bg-background border border-border rounded-md focus:outline-none focus:ring-1 focus:ring-primary"
                placeholder="e.g., Gemini CLI"
              />
            </div>
            {publishError && (
              <div className="flex items-center gap-2 text-sm text-red-500">
                <AlertCircle className="w-4 h-4" />
                {publishError}
              </div>
            )}
            <div className="flex items-center gap-2">
              <Button
                variant="default"
                size="sm"
                onClick={handlePublishToLibrary}
                disabled={isSubmitting || !publishName.trim()}
              >
                {isSubmitting ? (
                  <>
                    <Loader2 className="w-3 h-3 mr-1 animate-spin" />
                    Publishing...
                  </>
                ) : (
                  'Confirm'
                )}
              </Button>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => {
                  setIsPublishing(false)
                  setPublishError(null)
                }}
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
