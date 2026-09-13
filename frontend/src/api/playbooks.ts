import axios from 'axios'
import { getApiBaseUrl, getAuthToken } from '../services/api'
import type { InstalledPlaybook } from '../services/api-types'
import type { PlaybookCatalogItem, PlaybookPulseFocus, PlaybookRecommendedTool, PlaybookSetupInput } from '../components/playbooks/playbookCatalog'

type ServerPlaybook = {
  id: string
  title: string
  description: string
  version: string
  category: string
  order: number
  setup_prompt?: string
  setup_inputs?: PlaybookSetupInput[]
  required_capabilities?: string[]
  recommended_tools?: PlaybookRecommendedTool[]
  pulse_focus?: PlaybookPulseFocus[]
  outputs?: string[]
}

const api = axios.create({ baseURL: getApiBaseUrl(), headers: { 'Content-Type': 'application/json' } })
api.interceptors.request.use(config => {
  const token = getAuthToken()
  if (token && config.headers) config.headers.Authorization = `Bearer ${token}`
  return config
})

const normalize = (item: ServerPlaybook): PlaybookCatalogItem => ({
  id: item.id,
  title: item.title,
  description: item.description,
  version: item.version,
  category: item.category,
  order: item.order,
  inputCount: item.setup_inputs?.length || 0,
  toolCount: item.recommended_tools?.length || 0,
  setupPrompt: item.setup_prompt,
  setupInputs: item.setup_inputs || [],
  requiredCapabilities: item.required_capabilities || [],
  recommendedTools: item.recommended_tools || [],
  pulseFocus: item.pulse_focus || [],
  outputs: item.outputs || [],
})

export const playbooksApi = {
  list: async (): Promise<PlaybookCatalogItem[]> => {
    const response = await api.get<{ playbooks: ServerPlaybook[] }>('/api/playbooks')
    return (response.data.playbooks || []).map(normalize)
  },
  listInstalled: async (workspacePath: string): Promise<InstalledPlaybook[]> => {
    const response = await api.get<{ installed: InstalledPlaybook[] }>('/api/workflows/playbooks', { params: { workspace_path: workspacePath } })
    return response.data.installed || []
  },
  install: async (workspacePath: string, playbookId: string): Promise<InstalledPlaybook> => {
    const response = await api.post<{ installed: InstalledPlaybook }>('/api/workflows/playbooks/install', {
      workspace_path: workspacePath,
      playbook_id: playbookId,
    })
    return response.data.installed
  },
}
