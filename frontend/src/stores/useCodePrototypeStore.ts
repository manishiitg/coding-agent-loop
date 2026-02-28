import { create } from 'zustand'
import { persist } from 'zustand/middleware'

export interface PrototypeProjectConfig {
  selected_servers: string[]
  selected_tools: string[]
  selected_secrets: string[]
  selected_skills: string[]
  selected_subagents: string[]
  llm_config?: { model: string; provider: string }
}

export interface PrototypeProject {
  name: string
  type: 'frontend-only' | 'backend-only' | 'fullstack'
  description?: string
  created_at: string
  config: PrototypeProjectConfig
  deployments?: DeploymentRecord[]
}

export interface DeploymentRecord {
  id: string
  provider: string
  url: string
  timestamp: string
  status: 'success' | 'failed'
  logs?: string
}

interface CodePrototypeState {
  currentProject: PrototypeProject | null
  projectList: PrototypeProject[]
  isDeploying: boolean
  deployOutput: string[]
  lastDeployedUrl: string | null
  selectedProvider: 'k8s' | 'vercel' | 'railway'
  showPreview: boolean
  previewUrl: string | null

  setCurrentProject: (p: PrototypeProject | null) => void
  setProjectList: (list: PrototypeProject[]) => void
  setDeploying: (v: boolean) => void
  appendDeployLog: (line: string) => void
  clearDeployLog: () => void
  setLastDeployedUrl: (url: string | null) => void
  setSelectedProvider: (p: 'k8s' | 'vercel' | 'railway') => void
  setShowPreview: (v: boolean) => void
  setPreviewUrl: (url: string | null) => void
}

export const useCodePrototypeStore = create<CodePrototypeState>()(
  persist(
    (set) => ({
      currentProject: null,
      projectList: [],
      isDeploying: false,
      deployOutput: [],
      lastDeployedUrl: null,
      selectedProvider: 'k8s',
      showPreview: false,
      previewUrl: null,

      setCurrentProject: (p) => set({ currentProject: p }),
      setProjectList: (list) => set({ projectList: list }),
      setDeploying: (v) => set({ isDeploying: v }),
      appendDeployLog: (line) => set((s) => ({ deployOutput: [...s.deployOutput, line] })),
      clearDeployLog: () => set({ deployOutput: [] }),
      setLastDeployedUrl: (url) => set((s) => ({
        lastDeployedUrl: url,
        // Auto-populate preview URL when a deploy succeeds
        previewUrl: url ?? s.previewUrl,
      })),
      setSelectedProvider: (p) => set({ selectedProvider: p }),
      setShowPreview: (v) => set({ showPreview: v }),
      setPreviewUrl: (url) => set({ previewUrl: url }),
    }),
    {
      name: 'code-prototype-store',
      partialize: (s) => ({
        currentProject: s.currentProject,
        selectedProvider: s.selectedProvider,
        showPreview: s.showPreview,
        previewUrl: s.previewUrl,
      }),
    }
  )
)
