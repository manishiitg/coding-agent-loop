// Export all stores for easy importing
export { useAppStore } from './useAppStore'
export { useLLMStore } from './useLLMStore'
export { useMCPStore } from './useMCPStore'
export { useChatStore } from './useChatStore'
export { useWorkspaceStore } from './useWorkspaceStore'
export { useGlobalPresetStore, usePresetApplication, usePresetManagement, usePresetState } from './useGlobalPresetStore'
export { useWorkflowStore, useWorkflowPhases, useWorkflowPhasesLoading, useWorkflowRunFolders, useWorkflowProgress, useCompletedStepIndices } from './useWorkflowStore'
export type { ExecutionModeType } from './useWorkflowStore'
export { useRunningWorkflowsStore, useRunningWorkflows, useShowRunningDrawer, useRunningWorkflowsRunningCount, useRunningWorkflowsTotalCount } from './useRunningWorkflowsStore'
export type { RunningWorkflow } from './useRunningWorkflowsStore'
export { useCapabilitiesStore } from './useCapabilitiesStore'
export { useSecretsStore } from './useSecretsStore'
export type { StoredSecret } from './useSecretsStore'

// Export types
export type * from './types'

// Store initialization helper
export const initializeStores = async () => {
  // Initialize capabilities store first as other stores might depend on it
  const { useCapabilitiesStore } = await import('./useCapabilitiesStore')
  await useCapabilitiesStore.getState().fetchCapabilities()

  // Initialize MCP store by loading tools
  const { useMCPStore } = await import('./useMCPStore')
  await useMCPStore.getState().refreshTools()
  
  // Initialize LLM store by loading available LLMs
  const { useLLMStore } = await import('./useLLMStore')
  await useLLMStore.getState().refreshAvailableLLMs()
  
  // Initialize workspace store
  const { useWorkspaceStore } = await import('./useWorkspaceStore')
  useWorkspaceStore.getState()
  
  // Initialize global preset store
  const { useGlobalPresetStore } = await import('./useGlobalPresetStore')
  await useGlobalPresetStore.getState().refreshPresets()
  
  // Initialize workflow store by loading phases
  const { useWorkflowStore } = await import('./useWorkflowStore')
  await useWorkflowStore.getState().loadPhases()
}
