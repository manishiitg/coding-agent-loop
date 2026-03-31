import { useEffect, useMemo, useState } from 'react'
import { Terminal, CheckCircle, AlertCircle, Loader2 } from 'lucide-react'
import { Button } from './ui/Button'
import { Card } from './ui/Card'
import { useLLMStore } from '../stores'
import { llmConfigService, type ModelMetadata } from '../services/llm-config-api'

interface ClaudeCodeSectionProps {
  onPublished?: () => void
  metadata?: ModelMetadata[]
}

export function ClaudeCodeSection({ onPublished, metadata = [] }: ClaudeCodeSectionProps) {
  const [isPublishing, setIsPublishing] = useState(false)
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [publishName, setPublishName] = useState('')
  const [publishStatus, setPublishStatus] = useState<'idle' | 'success' | 'error'>('idle')
  const [publishError, setPublishError] = useState<string | null>(null)
  const [selectedModel, setSelectedModel] = useState('claude-code')
  const [effortLevel, setEffortLevel] = useState('high')
  const { saveLLM, savedLLMs } = useLLMStore()

  // Test connection state
  const [testStatus, setTestStatus] = useState<'idle' | 'testing' | 'valid' | 'invalid'>('idle')
  const [testMessage, setTestMessage] = useState<string | null>(null)
  const claudeModels = useMemo(
    () => metadata.filter(m => m.provider === 'claude-code'),
    [metadata]
  )
  const currentModelMetadata = claudeModels.find(m => m.model_id === selectedModel)
  const effortLevels = currentModelMetadata?.reasoning_effort_levels?.length
    ? currentModelMetadata.reasoning_effort_levels
    : ['low', 'medium', 'high', 'max']

  useEffect(() => {
    if (claudeModels.length === 0) return
    const modelExists = claudeModels.some(m => m.model_id === selectedModel)
    if (!modelExists) {
      setSelectedModel(claudeModels[0].model_id)
    }
  }, [claudeModels, selectedModel])

  const alreadyPublished = savedLLMs.some(
    llm => llm.provider === 'claude-code' && llm.model_id === selectedModel
  )

  const handleTestConnection = async () => {
    setTestStatus('testing')
    setTestMessage(null)

    try {
      const response = await llmConfigService.validateAPIKey({
        provider: 'claude-code',
      })

      if (response.valid) {
        setTestStatus('valid')
        setTestMessage(response.message || 'Claude Code CLI is working.')
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
        provider: 'claude-code' as const,
        model_id: selectedModel,
        options: { reasoning_effort: effortLevel },
      }

      await saveLLM(llmModel, publishName.trim(), 'Claude Code CLI', 'none', currentModelMetadata)
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
        <h3 className="text-lg font-semibold text-foreground">Claude Code Configuration</h3>
      </div>

      <Card className="p-4">
        <div className="flex items-start gap-3">
          <Terminal className="w-5 h-5 text-primary mt-0.5 flex-shrink-0" />
          <div className="space-y-2">
            <h4 className="font-medium text-foreground">Local CLI Provider</h4>
            <p className="text-sm text-muted-foreground">
              Claude Code uses the locally installed <code className="text-xs bg-secondary px-1 py-0.5 rounded">claude</code> CLI for inference.
              It handles its own authentication, model selection, and tool execution — no API key, model picker, or temperature setting is needed.
            </p>
            <p className="text-sm text-muted-foreground">
              Some agent features (context summarization, tool search, code execution mode, context editing) are automatically disabled when using this provider.
            </p>
          </div>
        </div>
      </Card>

      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-3">Model</h4>
        <select
          value={selectedModel}
          onChange={e => setSelectedModel(e.target.value)}
          className="w-full px-3 py-2 text-sm bg-background border border-border rounded-md focus:outline-none focus:ring-1 focus:ring-primary"
        >
          {claudeModels.map(m => (
            <option key={m.model_id} value={m.model_id}>{m.model_name || m.model_id}</option>
          ))}
        </select>
        {selectedModel !== 'claude-code' && (
          <p className="mt-1.5 text-xs text-muted-foreground">
            Passes <code className="bg-secondary px-1 py-0.5 rounded">--model {selectedModel}</code> to the CLI.
          </p>
        )}
      </Card>

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
          Controls how deeply Claude reasons. Passes <code className="bg-secondary px-1 py-0.5 rounded">--effort {effortLevel}</code> to the CLI.
        </p>
      </Card>

      {/* Test Connection */}
      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-3">Test Connection</h4>
        <p className="text-sm text-muted-foreground mb-3">
          Sends a test prompt to the Claude Code CLI to verify it is installed and authenticated.
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
            Claude Code is already published in your library.
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
              setPublishName(currentModelMetadata ? `Claude Code (${currentModelMetadata.model_name}, ${effortLevel} effort)` : 'Claude Code')
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
                placeholder="e.g., Claude Code"
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
