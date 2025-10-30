// Shared types for all stores

// Re-export API types for convenience
export type { PlannerFile, PollingEvent, LLMConfiguration } from '../services/api-types'

// MCP Server Types
export interface ToolDefinition {
  name: string
  description: string
  parameters: Record<string, unknown>
  status?: string
  error?: string
  server?: string
  toolsEnabled?: number
  function_names?: string[]
  tools?: ToolDetail[]
}

export interface ToolDetail {
  name: string
  description: string
  server: string
  parameters?: Record<string, {
    description?: string
    type?: string
  }>
  required?: string[]
}

// File Context Types
export interface FileContextItem {
  name: string
  path: string
  type: 'file' | 'folder'
}

// Chat Session Types
export interface ChatSession {
  id: string
  title: string
  createdAt: number
  lastActivity: number
}

// UI State Types
export interface Toast {
  id: string
  message: string
  type: 'success' | 'info' | 'error' | 'warning'
}

// Agent Mode Types
export type AgentMode = 'simple' | 'ReAct' | 'orchestrator' | 'workflow'

// Workflow Types
export type WorkflowPhase = 'pre-verification' | 'post-verification' | 'post-verification-todo-refinement'

// Store Action Types
export interface StoreActions {
  // Generic actions that all stores might need
  reset: () => void
  setLoading: (loading: boolean) => void
  setError: (error: string | null) => void
}
