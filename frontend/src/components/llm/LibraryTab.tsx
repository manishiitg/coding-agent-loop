import { useState, useEffect } from 'react'
import { Trash2, Box, DollarSign, Thermometer, CheckCircle, Play, Loader2, RefreshCw } from 'lucide-react'
import { Button } from '../ui/Button'
import { Card } from '../ui/Card'
import { useLLMStore, useChatStore, useAppStore } from '../../stores'
import { llmConfigService, type ModelMetadata } from '../../services/llm-config-api'
import type { SavedLLM } from '../../services/api-types'

const COMPLEX_PROMPT = `I require a rigorous, multi-vector technical analysis of the Model Context Protocol (MCP) landscape. This task is designed to test your ability to synthesize conflicting data sources.
1. **Initialization**: Verify if 'FILE_NAME' exists in the root workspace. Delete it if present to ensure a clean slate.
2. **Specification Extraction (Playwright)**: Navigate to 'https://modelcontextprotocol.io/docs/concepts/architecture'. Precisely extract the definitions of 'Prompts', 'Resources', and 'Tools'. Then, visit the GitHub repository (find the link on the page) and identify the most recent commit message related to the 'SDK' or core protocol.
3. **Ecosystem & Sentiment Analysis (Context7)**: Perform a targeted search for "MCP vs OpenAI Plugins" and "Anthropic MCP adoption". Identify at least two distinct architectural differences between MCP and OpenAI's deprecated plugin system.
4. **Implementation & Gap Analysis**: Search for "building an MCP server in Python". Compare the official documentation's "Quickstart" steps with a recent community tutorial or blog post. Detail any discrepancies in setup commands or required dependencies.
5. **Report Generation**: Compile a highly technical report in 'FILE_NAME' (in the root) containing:
   - **Executive Abstract**: A synthesis of the protocol's current maturity.
   - **Architecture & Versioning**: The definitions from step 2 and the latest SDK activity.
   - **Comparative Architecture**: The MCP vs Plugins comparison from step 3.
   - **Developer Experience Audit**: The friction points found in step 4 (Docs vs Reality).
6. **Termination**: Ensure all browser contexts are closed upon completion.
Execute this sequentially and with high attention to detail.`

// Helper to format context window size
const formatContextWindow = (tokens?: number): string => {
  if (!tokens) return ''
  if (tokens >= 1000000) return `${(tokens / 1000000).toFixed(1)}M`
  if (tokens >= 1000) return `${(tokens / 1000).toFixed(0)}k`
  return `${tokens}`
}

// Helper to format cost
const formatCost = (cost?: number): string => {
  if (cost === undefined || cost === null) return ''
  return `$${cost.toFixed(2)}`
}

// Helper to get options summary
const getOptionsSummary = (options?: Record<string, unknown>): string => {
  if (!options || Object.keys(options).length === 0) return ''
  const parts: string[] = []
  if (options.reasoning_effort) parts.push(`Reasoning: ${options.reasoning_effort}`)
  if (options.thinking_level) parts.push(`Thinking: ${options.thinking_level}`)
  if (options.thinking_budget) parts.push(`Budget: ${options.thinking_budget}`)
  return parts.join(' • ')
}

interface LibraryTabProps {
  onSelect?: (llm: SavedLLM) => void
}

export function LibraryTab({ onSelect }: LibraryTabProps) {
  const { savedLLMs, deleteSavedLLM, agentConfig, setShowLLMModal, setAzureConfig, azureConfig, defaultPublishedLLMsLocked, loadDefaultsFromBackend } = useLLMStore()
  const { createChatTab, setTabConfig } = useChatStore()
  const { setAgentMode } = useAppStore()
  const [metadataMap, setMetadataMap] = useState<Record<string, ModelMetadata>>({})
  const [testStatus, setTestStatus] = useState<string | null>(null)
  const [isRefreshing, setIsRefreshing] = useState(false)

  // Check if a saved LLM matches the current primary
  const isPrimary = (llm: SavedLLM): boolean => {
    if (!agentConfig?.primary) return false
    return agentConfig.primary.provider === llm.provider &&
           agentConfig.primary.model_id === llm.model_id
  }

  // Fetch model metadata for costs and context window
  useEffect(() => {
    const fetchMetadata = async () => {
      try {
        const response = await llmConfigService.getModelMetadata()
        const map: Record<string, ModelMetadata> = {}
        response.models.forEach(m => {
          map[m.model_id] = m
        })
        setMetadataMap(map)
      } catch (e) {
        console.warn('Failed to fetch model metadata:', e)
      }
    }
    fetchMetadata()
  }, [])

  const handleTestComplexPrompt = async (llm: SavedLLM) => {
    // Show message
    setTestStatus(`Initiating complex agent task with ${llm.name}...`)

    // Switch to Simple/Chat Mode
    setAgentMode('simple')

    // Published LLMs no longer carry secrets; Azure auth is loaded from workspace-backed
    // provider config and still needs to be present in the store for runtime execution.
    if (llm.provider === 'azure' && azureConfig.endpoint && azureConfig.api_key) {
      setAzureConfig({
        ...azureConfig,
        provider: 'azure',
        model_id: llm.model_id,
        api_key: azureConfig.api_key,
        endpoint: azureConfig.endpoint,
        region: azureConfig.region,
        options: llm.options,
        temperature: llm.temperature
      })
    }

    // Create a new tab
    const tabId = await createChatTab(`Test: ${llm.name}`, { mode: 'multi-agent' })

    // Generate dynamic filename
    const safeModelName = llm.name.replace(/[^a-z0-9]/gi, '_').toLowerCase()
    const fileName = `research-${safeModelName}.md`
    const prompt = COMPLEX_PROMPT.replace(/FILE_NAME/g, fileName)

    // Configure the tab
    setTabConfig(tabId, {
      inputText: prompt,
      autoRun: true,
      selectedServers: ['playwright', 'context7'], // Assuming workspace is auto-enabled via enableWorkspaceAccess
      enableWorkspaceAccess: true,
      llmConfig: {
        provider: llm.provider,
        model_id: llm.model_id,
        options: llm.options,
        fallback_models: [],
        cross_provider_fallback: undefined
      }
    })

    // Close quickly after setting up the tab
    setTimeout(() => {
        setShowLLMModal(false)
        setTestStatus(null)
    }, 500)
  }

  const handleRefreshLibrary = async () => {
    setIsRefreshing(true)
    try {
      await loadDefaultsFromBackend()
    } finally {
      setIsRefreshing(false)
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h3 className="text-lg font-semibold text-foreground">Published LLM</h3>
        <Button
          size="sm"
          variant="ghost"
          onClick={() => { void handleRefreshLibrary() }}
          disabled={isRefreshing}
          className="h-8 px-2"
          title="Refresh published LLMs from workspace"
        >
          <RefreshCw className={`w-4 h-4 ${isRefreshing ? 'animate-spin' : ''}`} />
        </Button>
      </div>

      {testStatus && (
        <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg p-4 flex items-center gap-3 text-blue-700 dark:text-blue-300 animate-in fade-in slide-in-from-top-2">
          <Loader2 className="w-5 h-5 animate-spin" />
          <div className="flex-1">
            <p className="font-medium">{testStatus}</p>
            <p className="text-xs opacity-80 mt-0.5">Closing configuration window to show you the chat...</p>
          </div>
        </div>
      )}

      <div className="text-sm text-muted-foreground mb-4">
        Use Publish in Provider tabs to save configurations here. Select one as Primary LLM.
      </div>

      {/* List */}
      <div className="space-y-3">
        {savedLLMs.length === 0 ? (
          <div className="text-center p-8 text-muted-foreground bg-muted/30 rounded-lg">
            No published LLMs yet. Configure a model in Provider tabs and publish it here.
          </div>
        ) : (
          savedLLMs.map((llm) => {
            const metadata = metadataMap[llm.model_id]
            const optionsSummary = getOptionsSummary(llm.options)
            const apiKeyLast4 = llm.api_key && llm.api_key.length >= 4
              ? `...${llm.api_key.slice(-4)}`
              : null
            const isCurrentPrimary = isPrimary(llm)

            return (
              <Card key={llm.id} className={`p-3 ${isCurrentPrimary ? 'ring-2 ring-primary' : ''}`}>
                <div className="flex items-start justify-between gap-3">
                  <div className="flex-1 min-w-0">
                    {/* Name and Provider */}
                    <div className="font-medium flex items-center gap-2 flex-wrap">
                      <span className="truncate">{llm.name}</span>
                      {isCurrentPrimary && (
                        <span className="text-xs px-1.5 py-0.5 rounded bg-primary text-primary-foreground font-medium flex-shrink-0 flex items-center gap-1">
                          <CheckCircle className="w-3 h-3" />
                          Primary
                        </span>
                      )}
                      <span className="text-xs px-1.5 py-0.5 rounded bg-secondary text-secondary-foreground font-normal capitalize flex-shrink-0">
                        {llm.provider}
                      </span>
                      {apiKeyLast4 && (
                        <span className="text-xs px-1.5 py-0.5 rounded bg-muted text-muted-foreground font-mono flex-shrink-0" title="API Key (last 4 chars)">
                          🔑 {apiKeyLast4}
                        </span>
                      )}
                    </div>

                    {/* Model ID */}
                    <div className="text-xs text-muted-foreground truncate mt-0.5">
                      {llm.model_id}
                    </div>

                    {/* Metadata row: context, cost, temperature */}
                    <div className="flex flex-wrap items-center gap-2 mt-1.5 text-[11px] text-muted-foreground">
                      {metadata?.context_window && (
                        <span className="flex items-center gap-0.5" title="Context window">
                          <Box className="w-3 h-3" />
                          {formatContextWindow(metadata.context_window)}
                        </span>
                      )}
                      {metadata?.input_cost_per_1m !== undefined && (
                        <span className="flex items-center gap-0.5" title="Input cost per 1M tokens">
                          <DollarSign className="w-3 h-3" />
                          {formatCost(metadata.input_cost_per_1m)}/1M in
                        </span>
                      )}
                      {metadata?.output_cost_per_1m !== undefined && (
                        <span title="Output cost per 1M tokens">
                          {formatCost(metadata.output_cost_per_1m)}/1M out
                        </span>
                      )}
                      {llm.temperature !== undefined && (
                        <span className="flex items-center gap-0.5" title="Temperature">
                          <Thermometer className="w-3 h-3" />
                          {llm.temperature.toFixed(1)}
                        </span>
                      )}
                    </div>

                    {/* Options row: reasoning, thinking, etc. */}
                    {optionsSummary && (
                      <div className="text-[11px] text-primary/70 mt-1">
                        {optionsSummary}
                      </div>
                    )}
                  </div>

                  {/* Action buttons */}
                  <div className="flex flex-col gap-2 flex-shrink-0">
                    <Button 
                      size="sm" 
                      variant="outline" 
                      onClick={() => handleTestComplexPrompt(llm)} 
                      className="whitespace-nowrap text-xs h-7 px-2"
                      title="Test Complex Agentic Prompt"
                      disabled={testStatus !== null}
                    >
                      <Play className="w-3 h-3 mr-1" />
                      Test Agent
                    </Button>
                    {onSelect && !isCurrentPrimary && (
                      <Button size="sm" variant="default" onClick={() => onSelect(llm)} className="whitespace-nowrap">
                        Set as Primary
                      </Button>
                    )}
                    {!defaultPublishedLLMsLocked && (
                      <Button size="sm" variant="ghost" onClick={() => { void deleteSavedLLM(llm.id) }} className="text-destructive hover:text-destructive hover:bg-destructive/10">
                        <Trash2 className="w-4 h-4" />
                      </Button>
                    )}
                  </div>
                </div>
              </Card>
            )
          })
        )}
      </div>
    </div>
  )
}
