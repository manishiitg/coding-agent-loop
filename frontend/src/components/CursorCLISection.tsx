import { useEffect, useMemo, useState } from 'react'
import { Terminal, CheckCircle, AlertCircle, Loader2 } from 'lucide-react'
import { Button } from './ui/Button'
import { Card } from './ui/Card'
import { useLLMStore } from '../stores'
import { llmConfigService, type ModelMetadata } from '../services/llm-config-api'
import { CodingAgentCapabilities } from './llm/CodingAgentCapabilities'

interface CursorCLISectionProps {
  onPublished?: () => void
  metadata?: ModelMetadata[]
}

export function CursorCLISection({ onPublished, metadata = [] }: CursorCLISectionProps) {
  const [isPublishing, setIsPublishing] = useState(false)
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [publishName, setPublishName] = useState('')
  const [publishStatus, setPublishStatus] = useState<'idle' | 'success' | 'error'>('idle')
  const [publishError, setPublishError] = useState<string | null>(null)
  const [selectedModel, setSelectedModel] = useState('cursor-cli')
  const { saveLLM, savedLLMs } = useLLMStore()

  const [testStatus, setTestStatus] = useState<'idle' | 'testing' | 'valid' | 'invalid'>('idle')
  const [testMessage, setTestMessage] = useState<string | null>(null)

  const cursorModels = useMemo(
    () => metadata.filter(m => m.provider === 'cursor-cli'),
    [metadata]
  )
  const currentModelMetadata = cursorModels.find(m => m.model_id === selectedModel)

  useEffect(() => {
    if (cursorModels.length === 0) return
    const modelExists = cursorModels.some(m => m.model_id === selectedModel)
    if (!modelExists) {
      setSelectedModel(cursorModels[0].model_id)
    }
  }, [cursorModels, selectedModel])

  const alreadyPublished = savedLLMs.some(
    llm => llm.provider === 'cursor-cli' && llm.model_id === selectedModel
  )

  const handleTestConnection = async () => {
    setTestStatus('testing')
    setTestMessage(null)

    try {
      const response = await llmConfigService.validateAPIKey({
        provider: 'cursor-cli',
        model_id: selectedModel,
      })

      if (response.valid) {
        setTestStatus('valid')
        setTestMessage(response.message || 'Cursor CLI is working.')
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
        provider: 'cursor-cli' as const,
        model_id: selectedModel,
      }

      await saveLLM(llmModel, publishName.trim(), 'Cursor CLI', 'none', currentModelMetadata)
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
        <h3 className="text-lg font-semibold text-foreground">Cursor CLI Configuration</h3>
      </div>

      <Card className="p-4">
        <div className="flex items-start gap-3">
          <Terminal className="w-5 h-5 text-primary mt-0.5 flex-shrink-0" />
          <div className="space-y-2">
            <h4 className="font-medium text-foreground">Local CLI Provider</h4>
            <p className="text-sm text-muted-foreground">
              Cursor CLI uses the locally installed <code className="text-xs bg-secondary px-1 py-0.5 rounded">cursor-agent</code> CLI through tmux.
              Authentication is handled by Cursor login or <code className="text-xs bg-secondary px-1 py-0.5 rounded">CURSOR_API_KEY</code>.
            </p>
            <p className="text-sm text-muted-foreground">
              The backend passes Runloop tools through the MCP bridge and keeps live chat turns steerable while the Cursor agent is running.
            </p>
          </div>
        </div>
      </Card>

      <CodingAgentCapabilities provider="cursor-cli" modelId={selectedModel} />

      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-3">Model</h4>
        <select
          value={selectedModel}
          onChange={e => setSelectedModel(e.target.value)}
          className="w-full px-3 py-2 text-sm bg-background border border-border rounded-md focus:outline-none focus:ring-1 focus:ring-primary"
        >
          {cursorModels.map(m => (
            <option key={m.model_id} value={m.model_id}>{m.model_name || m.model_id}</option>
          ))}
        </select>
        {selectedModel === 'cursor-cli' ? (
          <p className="mt-1.5 text-xs text-muted-foreground">
            Uses Cursor's default routing for the signed-in account.
          </p>
        ) : (
          <p className="mt-1.5 text-xs text-muted-foreground">
            Passes <code className="bg-secondary px-1 py-0.5 rounded">--model {selectedModel}</code> to the CLI.
          </p>
        )}
      </Card>

      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-3">Test Connection</h4>
        <p className="text-sm text-muted-foreground mb-3">
          Sends a real test prompt through Cursor CLI tmux mode to verify it is installed and authenticated.
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

      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-3">Publish to Library</h4>

        {alreadyPublished && !isPublishing && (
          <div className="flex items-center gap-2 text-sm text-green-600 dark:text-green-400 mb-3">
            <CheckCircle className="w-4 h-4" />
            Cursor CLI is already published in your library.
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
              setPublishName(currentModelMetadata?.model_name || 'Cursor CLI')
              setIsPublishing(true)
              setPublishError(null)
            }}
            disabled={alreadyPublished}
            size="sm"
          >
            Publish to Library
          </Button>
        ) : (
          <div className="space-y-3">
            <input
              type="text"
              value={publishName}
              onChange={e => setPublishName(e.target.value)}
              placeholder="Display name"
              className="w-full px-3 py-2 text-sm bg-background border border-border rounded-md focus:outline-none focus:ring-1 focus:ring-primary"
              autoFocus
            />
            <div className="flex gap-2">
              <Button
                onClick={handlePublishToLibrary}
                disabled={!publishName.trim() || isSubmitting}
                size="sm"
              >
                {isSubmitting ? (
                  <>
                    <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                    Publishing...
                  </>
                ) : (
                  'Publish'
                )}
              </Button>
              <Button
                variant="outline"
                onClick={() => {
                  setIsPublishing(false)
                  setPublishName('')
                  setPublishError(null)
                }}
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
