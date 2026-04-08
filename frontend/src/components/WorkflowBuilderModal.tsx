import { useState, useEffect, useCallback } from 'react'
import { Layers, Loader2, Search, FolderOpen, Check, Server, Copy, CheckCircle } from 'lucide-react'
import { agentApi } from '../services/api'
import { useMCPStore, useLLMStore, useChatStore } from '../stores'
import { useGlobalPresetStore } from '../stores/useGlobalPresetStore'
import type { PlannerFile, PresetLLMConfig, AgentLLMConfig } from '../services/api-types'

interface WorkflowBuilderModalProps {
  onClose: () => void
}

interface CreatedWorkflow {
  workflowName: string
  folderPath: string
  planNames: string[]
  prompt: string
  presetId: string
}

export default function WorkflowBuilderModal({ onClose }: WorkflowBuilderModalProps) {
  const [plans, setPlans] = useState<PlannerFile[]>([])
  const [selectedPlanIds, setSelectedPlanIds] = useState<Set<string>>(new Set())
  const [workflowName, setWorkflowName] = useState('')
  const [loading, setLoading] = useState(false)
  const [isLoadingPlans, setIsLoadingPlans] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [searchQuery, setSearchQuery] = useState('')
  const [created, setCreated] = useState<CreatedWorkflow | null>(null)
  const [copied, setCopied] = useState(false)

  // Fetch multi-agent plan folders on mount
  useEffect(() => {
    setIsLoadingPlans(true)
    agentApi.getPlannerFiles('Chats').then(response => {
      const planFolders = (response.data || []).filter((f: PlannerFile) => {
        if (f.type !== 'folder' || f.filepath === 'Chats') return false
        const children = f.children || []
        return children.some(child => child.filepath.endsWith('/plan.md') || child.filepath.endsWith('/plan_tracking.md') || child.filepath.endsWith('/.last_used'))
      })
      setPlans(planFolders)
    }).catch(err => {
      console.error('[WorkflowBuilder] Failed to load plans:', err)
      setPlans([])
    }).finally(() => {
      setIsLoadingPlans(false)
    })
  }, [])

  // Auto-generate workflow name from first selected plan
  useEffect(() => {
    if (workflowName === '' && selectedPlanIds.size === 1) {
      const planPath = Array.from(selectedPlanIds)[0]
      const name = planPath.split('/').pop() || planPath
      setWorkflowName(name.replace(/[^a-zA-Z0-9_-]+/g, '-').replace(/^-+|-+$/g, ''))
    }
  }, [selectedPlanIds, workflowName])

  const handleTogglePlan = useCallback((planPath: string) => {
    const newSelected = new Set(selectedPlanIds)
    if (newSelected.has(planPath)) {
      newSelected.delete(planPath)
    } else {
      newSelected.add(planPath)
    }
    setSelectedPlanIds(newSelected)
  }, [selectedPlanIds])

  // Keyboard handler
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        onClose()
      }
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [onClose])

  const handleSubmit = async () => {
    if (!workflowName.trim() || selectedPlanIds.size === 0) return

    // Check if a workflow with this name already exists
    const existingPresets = useGlobalPresetStore.getState().workflowPresets
    const nameExists = existingPresets.some(
      p => p.agentMode === 'workflow' && p.label.toLowerCase() === workflowName.trim().toLowerCase()
    )
    if (nameExists) {
      setError(`A workflow named "${workflowName.trim()}" already exists. Choose a different name.`)
      return
    }

    setLoading(true)
    setError(null)
    try {
      const safeName = workflowName.trim().replace(/[^a-zA-Z0-9_-]+/g, '-').replace(/^-+|-+$/g, '')
      const folderPath = `Workflow/${safeName}`

      // 1. Create workflow folder (409 = already exists, that's OK)
      try {
        await agentApi.createPlannerFolder(folderPath)
      } catch (folderErr: unknown) {
        const status = folderErr && typeof folderErr === 'object' && 'response' in folderErr
          ? (folderErr as { response?: { status?: number } }).response?.status
          : undefined
        if (status !== 409) throw folderErr
      }

      // 2. Copy selected plan folders into planning/reference-plans/
      const planNames: string[] = []
      for (const planPath of selectedPlanIds) {
        const planName = planPath.split('/').pop() || planPath
        planNames.push(planName)
        const destPath = `${folderPath}/planning/reference-plans/${planName}`
        try {
          await agentApi.copyFolder(planPath, destPath)
        } catch (copyErr: unknown) {
          // 409 = already exists, that's OK; anything else is a real error
          const status = copyErr && typeof copyErr === 'object' && 'response' in copyErr
            ? (copyErr as { response?: { status?: number } }).response?.status
            : undefined
          if (status !== 409) {
            console.error(`[WorkflowBuilder] Failed to copy plan ${planPath} to ${destPath}:`, copyErr)
          }
        }
      }

      // 3. Create workflow preset — inherit config from active multi-agent chat tab
      const activeTab = useChatStore.getState().getActiveTab()
      const mcpState = useMCPStore.getState()
      const llmStore = useLLMStore.getState()

      // Use multi-agent chat's servers, falling back to chat servers
      const currentServers = activeTab?.config?.selectedServers?.length
        ? activeTab.config.selectedServers
        : mcpState.chatSelectedServers

      // Use multi-agent chat's skills
      const currentSkills = activeTab?.config?.selectedSkills || []

      // Build tiered LLM config from multi-agent delegation tiers
      const tierConfig = llmStore.delegationTierConfig
      const chatLLM = llmStore.getConfigForMode('multi-agent').primaryConfig
      const defaultLLM: AgentLLMConfig = { provider: chatLLM.provider as AgentLLMConfig['provider'], model_id: chatLLM.model_id }
      const toAgentLLM = (t: { provider: string; model_id: string }): AgentLLMConfig =>
        ({ provider: t.provider as AgentLLMConfig['provider'], model_id: t.model_id })

      const presetLLMConfig: PresetLLMConfig = {
        // Phase LLM = high tier model (or chat primary as fallback)
        provider: (tierConfig?.high?.provider || chatLLM.provider) as PresetLLMConfig['provider'],
        model_id: tierConfig?.high?.model_id || chatLLM.model_id,
        phase_llm: tierConfig?.high ? toAgentLLM(tierConfig.high) : defaultLLM,
        // Tiered allocation mode with high/medium/low from multi-agent config
        llm_allocation_mode: tierConfig ? 'tiered' : 'manual',
        ...(tierConfig ? {
          tiered_config: {
            tier_1: tierConfig.high ? toAgentLLM(tierConfig.high) : defaultLLM,
            tier_2: tierConfig.medium ? toAgentLLM(tierConfig.medium) : defaultLLM,
            tier_3: tierConfig.low ? toAgentLLM(tierConfig.low) : defaultLLM,
          }
        } : {})
      }

      const { savePreset } = useGlobalPresetStore.getState()
      const newPreset = await savePreset(
        workflowName.trim(),
        '',
        currentServers,
        [],
        currentSkills,
        'workflow',
        { filepath: folderPath, content: '', last_modified: '', type: 'folder' } as PlannerFile,
        presetLLMConfig
      )

      if (!newPreset) throw new Error('Failed to create preset')

      // 4. Auto-activate the new workflow preset so the workflow path is immediately available
      useGlobalPresetStore.getState().setActivePreset('workflow', newPreset.id)

      // 5. Build the prompt the user can paste into planning
      const planRefs = planNames.map(n => `- planning/reference-plans/${n}/`).join('\n')
      const prompt = `I've placed reference plans in the workspace for you to study:\n${planRefs}\n\nEach folder has a plan.md with the task breakdown and outputs from previous executions. Analyze these plans — understand what was done, the steps involved, and the results produced. Then design a structured workflow that reproduces this work reliably. Use orchestrator and task agent node types so the workflow can be re-run consistently across different inputs.`

      // Show success state
      setCreated({
        workflowName: workflowName.trim(),
        folderPath,
        planNames,
        prompt,
        presetId: newPreset.id,
      })
    } catch (err: unknown) {
      console.error('[WorkflowBuilder] Submit error:', err)
      setError(err instanceof Error ? err.message : 'Failed to create workflow')
    } finally {
      setLoading(false)
    }
  }

  const handleCopyPrompt = () => {
    if (!created) return
    navigator.clipboard.writeText(created.prompt)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  // Get active config for display — prefer active multi-agent tab's config
  const mcpState = useMCPStore()
  const chatStore = useChatStore()
  const llmStoreState = useLLMStore()
  const currentTab = chatStore.getActiveTab()
  const activeServers = currentTab?.config?.selectedServers?.length
    ? currentTab.config.selectedServers
    : mcpState.chatSelectedServers
  const activeSkills = currentTab?.config?.selectedSkills || []
  const activeTierConfig = llmStoreState.delegationTierConfig
  const chatPrimaryLLM = llmStoreState.getConfigForMode('multi-agent').primaryConfig
  const hasTierConfig = !!(activeTierConfig?.high || activeTierConfig?.medium || activeTierConfig?.low)

  // Helper to get short model name
  const shortModel = (provider?: string, modelId?: string) => {
    if (!modelId) return null
    const parts = modelId.split('/')
    return parts[parts.length - 1]
  }

  // Filter plans by search
  const filteredPlans = plans.filter(p => {
    if (!searchQuery.trim()) return true
    const q = searchQuery.toLowerCase()
    const name = (p.filepath.split('/').pop() || p.filepath).toLowerCase()
    return name.includes(q)
  })

  const canSubmit = workflowName.trim() && selectedPlanIds.size > 0 && !loading

  // ── Success state ──
  if (created) {
    return (
      <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
        <div className="bg-background border border-border rounded-lg shadow-xl w-full max-w-lg flex flex-col">
          {/* Header */}
          <div className="flex items-center justify-between px-4 py-3 border-b border-border bg-secondary">
            <div className="flex items-center gap-2">
              <CheckCircle className="w-4 h-4 text-green-500" />
              <span className="text-sm font-medium text-foreground">Workflow Created</span>
            </div>
            <button onClick={onClose} className="text-muted-foreground hover:text-foreground transition-colors">
              ✕
            </button>
          </div>

          {/* Success content */}
          <div className="px-4 py-4 space-y-4">
            {/* Workflow info */}
            <div className="space-y-2">
              <div className="text-sm text-foreground">
                Workflow <span className="font-semibold">{created.workflowName}</span> has been created.
              </div>
              <div className="text-xs text-muted-foreground">
                Reference plans copied to <code className="px-1 py-0.5 bg-secondary rounded text-foreground">{created.folderPath}/planning/reference-plans/</code>
              </div>
              <ul className="text-xs text-muted-foreground ml-4 list-disc">
                {created.planNames.map(name => (
                  <li key={name}>{name}</li>
                ))}
              </ul>
            </div>

            {/* Next steps */}
            <div className="space-y-2">
              <div className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Next Steps</div>
              <ol className="text-sm text-foreground space-y-1 ml-4 list-decimal">
                <li>Select the <span className="font-medium">{created.workflowName}</span> workflow from the top header</li>
                <li>Click <span className="font-medium">Create Plan</span> to start the planning phase</li>
                <li>Paste the prompt below so the planner reads your reference plans</li>
              </ol>
            </div>

            {/* Copyable prompt */}
            <div className="space-y-1.5">
              <div className="flex items-center justify-between">
                <span className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Planning Prompt</span>
                <button
                  onClick={handleCopyPrompt}
                  className="flex items-center gap-1 text-xs text-primary hover:text-primary/80 transition-colors"
                >
                  {copied ? <Check className="w-3 h-3" /> : <Copy className="w-3 h-3" />}
                  {copied ? 'Copied!' : 'Copy'}
                </button>
              </div>
              <pre className="text-xs bg-secondary border border-border rounded-md p-3 whitespace-pre-wrap text-foreground max-h-32 overflow-y-auto select-all">
                {created.prompt}
              </pre>
            </div>
          </div>

          {/* Footer */}
          <div className="px-4 py-3 border-t border-border bg-secondary flex justify-end">
            <button
              onClick={onClose}
              className="px-4 py-1.5 text-sm font-medium rounded-md bg-primary text-primary-foreground hover:bg-primary/90 transition-colors"
            >
              Done
            </button>
          </div>
        </div>
      </div>
    )
  }

  // ── Form state ──
  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
      <div className="bg-background border border-border rounded-lg shadow-xl w-full max-w-lg max-h-[80vh] flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-3 border-b border-border bg-secondary">
          <div className="flex items-center gap-2">
            <Layers className="w-4 h-4 text-muted-foreground" />
            <span className="text-sm font-medium text-foreground">Build Workflow from Plan Folders</span>
          </div>
          <button onClick={onClose} className="text-muted-foreground hover:text-foreground transition-colors">
            ✕
          </button>
        </div>

        {/* Workflow Name Input */}
        <div className="px-4 py-3 border-b border-border">
          <label className="block text-xs font-medium text-muted-foreground mb-1">
            Workflow Name
          </label>
          <input
            type="text"
            value={workflowName}
            onChange={(e) => { setWorkflowName(e.target.value.replace(/[^a-zA-Z0-9_-]+/g, '-')); setError(null) }}
            placeholder="Enter workflow name (e.g. HDFC-Personal)"
            className="w-full px-3 py-1.5 text-sm bg-secondary border border-border rounded-md text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-primary"
            autoFocus
          />
        </div>

        {/* Plan Search */}
        <div className="px-4 py-2 border-b border-border">
          <div className="relative">
            <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground" />
            <input
              type="text"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder="Search plans..."
              className="w-full pl-8 pr-3 py-1.5 text-sm bg-secondary border border-border rounded-md text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-primary"
            />
          </div>
        </div>

        {/* Plan List */}
        <div className="flex-1 overflow-y-auto min-h-0">
          <div className="px-4 py-1.5">
            <span className="text-xs font-medium text-muted-foreground">
              Select Plan Folders ({selectedPlanIds.size} selected)
            </span>
          </div>
          {isLoadingPlans ? (
            <div className="flex items-center justify-center py-12">
              <Loader2 className="w-5 h-5 animate-spin text-muted-foreground" />
            </div>
          ) : filteredPlans.length === 0 ? (
            <div className="text-center py-12 text-sm text-muted-foreground">
              {searchQuery
                ? 'No matching plans'
                : 'No plans found. Create plans in multi-agent chat mode first.'}
            </div>
          ) : (
            filteredPlans.map((plan) => {
              const planName = plan.filepath.split('/').pop() || plan.filepath
              const isSelected = selectedPlanIds.has(plan.filepath)
              return (
                <div
                  key={plan.filepath}
                  onClick={() => handleTogglePlan(plan.filepath)}
                  role="button"
                  tabIndex={0}
                  onKeyDown={(e) => e.key === 'Enter' && handleTogglePlan(plan.filepath)}
                  className={`w-full px-4 py-2.5 text-left cursor-pointer transition-colors border-b border-border/50 last:border-b-0 flex items-center gap-3 ${
                    isSelected
                      ? 'bg-primary/10'
                      : 'hover:bg-secondary'
                  }`}
                >
                  <div className={`w-4 h-4 rounded border flex items-center justify-center flex-shrink-0 ${
                    isSelected
                      ? 'bg-primary border-primary text-primary-foreground'
                      : 'border-muted-foreground/40'
                  }`}>
                    {isSelected && <Check className="w-3 h-3" />}
                  </div>
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <FolderOpen className="w-3.5 h-3.5 text-muted-foreground flex-shrink-0" />
                      <span className="text-sm font-medium text-foreground truncate">
                        {planName}
                      </span>
                    </div>
                    {plan.last_modified && (
                      <span className="text-xs text-muted-foreground">
                        {new Date(plan.last_modified).toLocaleDateString()}
                      </span>
                    )}
                  </div>
                </div>
              )
            })
          )}
        </div>

        {/* Inherited Config Summary */}
        <div className="px-4 py-2.5 border-t border-border bg-secondary/50 space-y-1.5">
          <div className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Inherited from Chat</div>

          {/* Tier Config */}
          <div className="flex items-start gap-1.5 text-xs">
            <span className="text-muted-foreground shrink-0">LLM Tiers:</span>
            {hasTierConfig ? (
              <div className="flex flex-wrap gap-x-3 gap-y-0.5">
                {activeTierConfig?.high && (
                  <span className="text-foreground">
                    <span className="text-muted-foreground">H:</span> {shortModel(activeTierConfig.high.provider, activeTierConfig.high.model_id)}
                  </span>
                )}
                {activeTierConfig?.medium && (
                  <span className="text-foreground">
                    <span className="text-muted-foreground">M:</span> {shortModel(activeTierConfig.medium.provider, activeTierConfig.medium.model_id)}
                  </span>
                )}
                {activeTierConfig?.low && (
                  <span className="text-foreground">
                    <span className="text-muted-foreground">L:</span> {shortModel(activeTierConfig.low.provider, activeTierConfig.low.model_id)}
                  </span>
                )}
              </div>
            ) : (
              <span className="text-yellow-500">Not configured — will use single model: {shortModel(chatPrimaryLLM.provider, chatPrimaryLLM.model_id) || 'default'}</span>
            )}
          </div>

          {/* MCP Servers */}
          {activeServers.length > 0 && (
            <div className="flex items-center gap-1.5 text-xs">
              <Server className="w-3 h-3 text-muted-foreground" />
              <span className="text-muted-foreground">Servers:</span>
              <span className="text-foreground font-medium">{activeServers.length} active</span>
            </div>
          )}

          {/* Skills */}
          {activeSkills.length > 0 && (
            <div className="flex items-center gap-1.5 text-xs">
              <span className="text-muted-foreground">Skills:</span>
              <span className="text-foreground font-medium">{activeSkills.length} selected</span>
            </div>
          )}
        </div>

        {/* Error */}
        {error && (
          <div className="px-4 py-2 border-t border-border bg-destructive/10 text-destructive text-xs">
            {error}
          </div>
        )}

        {/* Footer */}
        <div className="px-4 py-3 border-t border-border bg-secondary flex items-center justify-between">
          <span className="text-xs text-muted-foreground">
            Esc to close
          </span>
          <div className="flex items-center gap-2">
            <button
              onClick={onClose}
              className="px-3 py-1.5 text-sm text-muted-foreground hover:text-foreground transition-colors rounded-md border border-border hover:bg-secondary"
            >
              Cancel
            </button>
            <button
              onClick={handleSubmit}
              disabled={!canSubmit}
              className={`px-4 py-1.5 text-sm font-medium rounded-md transition-colors flex items-center gap-2 ${
                canSubmit
                  ? 'bg-primary text-primary-foreground hover:bg-primary/90'
                  : 'bg-muted text-muted-foreground cursor-not-allowed'
              }`}
            >
              {loading && <Loader2 className="w-3.5 h-3.5 animate-spin" />}
              Create Workflow
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
