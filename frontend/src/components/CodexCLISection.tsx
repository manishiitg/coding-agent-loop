import { useState } from 'react'
import { Terminal, CheckCircle, AlertCircle, Loader2 } from 'lucide-react'
import { Button } from './ui/Button'
import { Card } from './ui/Card'
import { useLLMStore } from '../stores'
import { llmConfigService } from '../services/llm-config-api'

interface CodexCLISectionProps {
  onPublished?: () => void
}

const CODEX_CLI_MODELS = [
  { id: 'codex-cli', label: 'Auto (default)', description: 'Uses the CLI default model' },
  { id: 'gpt-5.4', label: 'GPT-5.4 — Flagship', description: '1M context, strongest reasoning' },
  { id: 'gpt-5.4-mini', label: 'GPT-5.4 Mini — Fast', description: '400K context, fast and affordable' },
  { id: 'gpt-5.3-codex', label: 'GPT-5.3-Codex — Coding', description: '400K context, industry-leading code' },
]

export function CodexCLISection({ onPublished }: CodexCLISectionProps) {
  const [isPublishing, setIsPublishing] = useState(false)
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [publishName, setPublishName] = useState('')
  const [publishStatus, setPublishStatus] = useState<'idle' | 'success' | 'error'>('idle')
  const [publishError, setPublishError] = useState<string | null>(null)
  const [selectedModel, setSelectedModel] = useState('codex-cli')
  const [reasoningEffort, setReasoningEffort] = useState('medium')
  const { saveLLM, savedLLMs } = useLLMStore()

  // Test connection state
  const [testStatus, setTestStatus] = useState<'idle' | 'testing' | 'valid' | 'invalid'>('idle')
  const [testMessage, setTestMessage] = useState<string | null>(null)

  const alreadyPublished = savedLLMs.some(
    llm => llm.provider === 'codex-cli' && llm.model_id === selectedModel
  )

  const handleTestConnection = async () => {
    setTestStatus('testing')
    setTestMessage(null)

    try {
      const response = await llmConfigService.validateAPIKey({
        provider: 'codex-cli',
      })

      if (response.valid) {
        setTestStatus('valid')
        setTestMessage(response.message || 'Codex CLI is working.')
      } else {
        setTestStatus('invalid')
        setTestMessage(response.message || response.error || 'Validation failed.')
      }
    } catch (err) {
      setTestStatus('invalid')
      setTestMessage(err instanceof Error ? err.message : 'Connection test failed.')
    }
  }

  const handlePublishToLibrary = () => {
    if (!publishName.trim()) return

    setIsSubmitting(true)
    setPublishError(null)

    try {
      const llmModel = {
        provider: 'codex-cli' as const,
        model_id: selectedModel,
        options: { reasoning_effort: reasoningEffort },
      }

      saveLLM(llmModel, publishName.trim(), 'OpenAI Codex CLI', 'none')
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
        <h3 className="text-lg font-semibold text-foreground">Codex CLI Configuration</h3>
      </div>

      <Card className="p-4">
        <div className="flex items-start gap-3">
          <Terminal className="w-5 h-5 text-primary mt-0.5 flex-shrink-0" />
          <div className="space-y-2">
            <h4 className="font-medium text-foreground">Local CLI Provider</h4>
            <p className="text-sm text-muted-foreground">
              OpenAI Codex uses the locally installed <code className="text-xs bg-secondary px-1 py-0.5 rounded">codex</code> CLI for inference.
              Authentication is handled via <code className="text-xs bg-secondary px-1 py-0.5 rounded">CODEX_API_KEY</code> or <code className="text-xs bg-secondary px-1 py-0.5 rounded">OPENAI_API_KEY</code> environment variable, or via <code className="text-xs bg-secondary px-1 py-0.5 rounded">codex login</code>.
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
          {CODEX_CLI_MODELS.map(m => (
            <option key={m.id} value={m.id}>{m.label}</option>
          ))}
        </select>
        {selectedModel !== 'codex-cli' && (
          <p className="mt-1.5 text-xs text-muted-foreground">
            Passes <code className="bg-secondary px-1 py-0.5 rounded">--model {selectedModel}</code> to the CLI.
          </p>
        )}
      </Card>

      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-3">Reasoning Effort</h4>
        <select
          value={reasoningEffort}
          onChange={e => setReasoningEffort(e.target.value)}
          className="w-full px-3 py-2 text-sm bg-background border border-border rounded-md focus:outline-none focus:ring-1 focus:ring-primary"
        >
          {(selectedModel === 'gpt-5.4'
            ? ['none', 'low', 'medium', 'high', 'xhigh']
            : ['low', 'medium', 'high', 'xhigh']
          ).map(level => (
            <option key={level} value={level}>
              {level.charAt(0).toUpperCase() + level.slice(1)}
            </option>
          ))}
        </select>
        <p className="mt-1.5 text-xs text-muted-foreground">
          Controls how deeply the model reasons before responding. Higher = slower but more thorough.
        </p>
      </Card>

      {/* Test Connection */}
      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-3">Test Connection</h4>
        <p className="text-sm text-muted-foreground mb-3">
          Sends a test prompt to the Codex CLI to verify it is installed and authenticated.
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
            Codex CLI is already published in your library.
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
              const modelEntry = CODEX_CLI_MODELS.find(m => m.id === selectedModel)
              setPublishName(modelEntry ? `Codex CLI (${modelEntry.label.split(' —')[0]}, ${reasoningEffort} effort)` : 'Codex CLI')
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
                placeholder="e.g., Codex CLI"
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
