import { getApiBaseUrl, getAuthToken } from '../services/api'
import { parseAgentworksProductCommands, type AgentworksProductCommand } from './agentworksProductCommands'

export const AGENTWORKS_PROFILE_ID = 'agentworks'
export const AGENTWORKS_PROFILE_VERSION = 1

export async function loadAgentworksProductCommands(): Promise<AgentworksProductCommand[]> {
  const token = getAuthToken()
  const response = await fetch(`${getApiBaseUrl()}/api/agent-profiles/${encodeURIComponent(AGENTWORKS_PROFILE_ID)}?version=${AGENTWORKS_PROFILE_VERSION}`, {
    headers: token ? { Authorization: `Bearer ${token}` } : undefined,
  })
  if (!response.ok) throw new Error(`Unable to load AgentWorks commands (${response.status})`)
  return parseAgentworksProductCommands(await response.json() as { commands?: Array<Record<string, unknown>> })
}
