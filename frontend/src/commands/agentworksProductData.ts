import { getApiBaseUrl, getAuthToken } from '../services/api'
import { parseAgentworksProductCommands, type AgentworksProductCommand } from './agentworksProductCommands'

export const AGENTWORKS_PROFILE_ID = 'agentworks'
export const AGENTWORKS_PROFILE_VERSION = 1

let loaded: Promise<AgentworksProductCommand[]> | null = null

// The workflow chat remounts on every workflow switch; load the product
// commands once per page. A missing profile (404) is a stable answer (no
// commands), so it is cached too; other failures may be retried.
export function loadAgentworksProductCommands(): Promise<AgentworksProductCommand[]> {
  if (!loaded) {
    loaded = fetchAgentworksProductCommands().catch(error => {
      loaded = null
      throw error
    })
  }
  return loaded
}

export function resetAgentworksProductCommandsCache(): void {
  loaded = null
}

async function fetchAgentworksProductCommands(): Promise<AgentworksProductCommand[]> {
  const token = getAuthToken()
  const response = await fetch(`${getApiBaseUrl()}/api/agent-profiles/${encodeURIComponent(AGENTWORKS_PROFILE_ID)}?version=${AGENTWORKS_PROFILE_VERSION}`, {
    headers: token ? { Authorization: `Bearer ${token}` } : undefined,
  })
  if (response.status === 404) return []
  if (!response.ok) throw new Error(`Unable to load AgentWorks commands (${response.status})`)
  return parseAgentworksProductCommands(await response.json() as { commands?: Array<Record<string, unknown>> })
}
