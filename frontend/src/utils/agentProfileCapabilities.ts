import { getApiBaseUrl, getAuthToken } from '../services/api'

// Reads a capability declared on an agent profile's runtime.capabilities block
// (agentprofiles.RuntimeCapabilities in agent_go/pkg/agentprofiles/types.go).
// Deliberately generic over the profile id: a product opts into a shared
// capability (browser, secrets, voice, ...) by declaring it in its OWN
// product.yaml, so the frontend must not hardcode which product has which
// capability — that defeats the point of it being product.yaml-driven.

export type AgentProfileProviderOption = {
  id: string
  label?: string
  provider?: string
  model_id?: string
  default?: boolean
  options?: Record<string, unknown>
  /** Curates the composer's model list to exactly these ids; empty offers every catalog model for `provider`. */
  models?: string[]
  /** Offers a reasoning-effort control (low → high) for this engine; empty offers none. */
  reasoning_efforts?: string[]
}

type AgentProfileResponse = {
  runtime?: {
    provider?: string
    model_id?: string
    transport?: string
    capabilities?: Record<string, unknown>
    provider_options?: AgentProfileProviderOption[]
  }
}

const capabilityCache = new Map<string, Promise<boolean>>()
const profileCache = new Map<string, Promise<AgentProfileResponse>>()

function loadAgentProfile(profileId: string, version?: number): Promise<AgentProfileResponse> {
  const normalizedVersion = version && version > 0 ? version : undefined
  const cacheKey = `${profileId}::${normalizedVersion ?? 'latest'}`
  const cached = profileCache.get(cacheKey)
  if (cached) return cached

  const token = getAuthToken()
  const versionQuery = normalizedVersion ? `?version=${normalizedVersion}` : ''
  const promise = fetch(`${getApiBaseUrl()}/api/agent-profiles/${encodeURIComponent(profileId)}${versionQuery}`, {
    headers: token ? { Authorization: `Bearer ${token}` } : undefined,
  }).then((response) => {
    if (!response.ok) throw new Error(`Unable to load agent profile ${profileId} (${response.status})`)
    return response.json() as Promise<AgentProfileResponse>
  })

  profileCache.set(cacheKey, promise)
  return promise
}

export async function loadAgentProfileRuntime(
  profileId: string,
  version?: number,
): Promise<{ provider: string; model_id: string; transport?: string } | null> {
  if (!profileId) return null
  try {
    const profile = await loadAgentProfile(profileId, version)
    const provider = profile.runtime?.provider?.trim() || ''
    const modelId = profile.runtime?.model_id?.trim() || ''
    const transport = profile.runtime?.transport?.trim() || undefined
    return provider && modelId ? { provider, model_id: modelId, transport } : null
  } catch {
    return null
  }
}

/**
 * Resolves whether `profileId` declared `capability` as anything other than
 * "disabled" (or absent). Mirrors agentprofiles.CapabilityRequirement: string
 * equality rather than a closed union, so an unrecognized future requirement
 * value (e.g. a new tier between preferred/optional) still gates as enabled
 * rather than silently hiding a capability the backend actually turned on.
 *
 * Cached per (profileId, capability) for the page session — this is read at
 * composer-mount time, and a profile's declared capabilities do not change
 * without a server restart.
 */
export function loadAgentProfileCapabilityEnabled(profileId: string, capability: string, version?: number): Promise<boolean> {
  if (!profileId) return Promise.resolve(false)
  const cacheKey = `${profileId}::${version ?? 'latest'}::${capability}`
  const cached = capabilityCache.get(cacheKey)
  if (cached) return cached

  const promise = loadAgentProfile(profileId, version)
    .then((profile) => {
      const value = profile.runtime?.capabilities?.[capability]
      const asStr = typeof value === 'string' ? value.trim() : ''
      return asStr !== '' && asStr !== 'disabled'
    })
    .catch(() => false)

  capabilityCache.set(cacheKey, promise)
  return promise
}

/**
 * The client-selectable (provider, model) bindings a profile declares in
 * product.yaml (runtime.provider_options). Empty when the profile declares
 * none: the composer then shows no model switcher, and the server picks the
 * profile's default binding. Same cache as the capability reads.
 */
export async function loadAgentProfileProviderOptions(profileId: string, version?: number): Promise<AgentProfileProviderOption[]> {
  if (!profileId) return []
  try {
    const profile = await loadAgentProfile(profileId, version)
    const options = profile.runtime?.provider_options
    if (!Array.isArray(options)) return []
    return options.filter((o): o is AgentProfileProviderOption => !!o && typeof o.id === 'string' && o.id.trim() !== '')
  } catch {
    return []
  }
}
