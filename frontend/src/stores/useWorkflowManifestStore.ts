import { create } from 'zustand'
import { sessionShareApi } from '../services/api'
import type {
  WorkflowManifest,
  DiscoveredWorkflow,
  WorkflowCapabilities,
  WorkflowExecutionDefaults,
  WorkflowOwnership,
  WorkflowScheduleEntry,
} from '../services/api-types'

export interface WorkflowManifestState {
  // Discovered workflows from manifest scan
  workflows: DiscoveredWorkflow[]
  isLoading: boolean
  lastRefreshed: number | null

  // Active workflow (selected for editing/execution)
  activeWorkflowId: string | null

  // Actions
  refreshWorkflows: () => Promise<void>
  getActiveWorkflow: () => DiscoveredWorkflow | null
  setActiveWorkflowId: (id: string | null) => void
  getWorkflowByPath: (workspacePath: string) => DiscoveredWorkflow | undefined
  getWorkflowById: (workflowId: string) => DiscoveredWorkflow | undefined

  // CRUD
  createWorkflow: (label: string, workspacePath: string, capabilities?: Partial<WorkflowCapabilities>) => Promise<WorkflowManifest>
  updateWorkflow: (workspacePath: string, updates: {
    label?: string
    query?: string
    capabilities?: WorkflowCapabilities
    execution_defaults?: WorkflowExecutionDefaults
    ownership?: WorkflowOwnership
    schedules?: WorkflowScheduleEntry[]
  }) => Promise<WorkflowManifest>
  deleteWorkflow: (workspacePath: string) => Promise<void>
  duplicateWorkflow: (sourceWorkspacePath: string, targetWorkspacePath: string, newLabel?: string) => Promise<WorkflowManifest>
}

export const useWorkflowManifestStore = create<WorkflowManifestState>((set, get) => ({
  workflows: [],
  isLoading: false,
  lastRefreshed: null,
  activeWorkflowId: null,

  refreshWorkflows: async () => {
    set({ isLoading: true })
    try {
      const response = await sessionShareApi.listWorkflowManifests()
      set({
        workflows: response.workflows || [],
        lastRefreshed: Date.now(),
      })
    } catch (error) {
      console.error('[WorkflowManifestStore] Failed to refresh workflows:', error)
    } finally {
      set({ isLoading: false })
    }
  },

  getActiveWorkflow: () => {
    const { workflows, activeWorkflowId } = get()
    if (!activeWorkflowId) return null
    return workflows.find(w => w.manifest.id === activeWorkflowId) ?? null
  },

  setActiveWorkflowId: (id: string | null) => {
    set({ activeWorkflowId: id })
  },

  getWorkflowByPath: (workspacePath: string) => {
    return get().workflows.find(w => w.workspace_path === workspacePath)
  },

  getWorkflowById: (workflowId: string) => {
    return get().workflows.find(w => w.manifest.id === workflowId)
  },

  createWorkflow: async (label, workspacePath, capabilities) => {
    const response = await sessionShareApi.createWorkflowManifest({
      label,
      workspace_path: workspacePath,
      capabilities,
    })
    // Refresh the list to include the new workflow
    await get().refreshWorkflows()
    return response.manifest
  },

  updateWorkflow: async (workspacePath, updates) => {
    const response = await sessionShareApi.updateWorkflowManifest({
      workspace_path: workspacePath,
      ...updates,
    })
    // Refresh to pick up changes
    await get().refreshWorkflows()
    return response.manifest
  },

  deleteWorkflow: async (workspacePath) => {
    await sessionShareApi.deleteWorkflowManifest(workspacePath)
    // Refresh to remove from list
    const { activeWorkflowId, workflows } = get()
    const deleted = workflows.find(w => w.workspace_path === workspacePath)
    if (deleted && deleted.manifest.id === activeWorkflowId) {
      set({ activeWorkflowId: null })
    }
    await get().refreshWorkflows()
  },

  duplicateWorkflow: async (sourceWorkspacePath, targetWorkspacePath, newLabel) => {
    const response = await sessionShareApi.duplicateWorkflowManifest({
      source_workspace_path: sourceWorkspacePath,
      target_workspace_path: targetWorkspacePath,
      new_label: newLabel,
    })
    await get().refreshWorkflows()
    return response.manifest
  },
}))
