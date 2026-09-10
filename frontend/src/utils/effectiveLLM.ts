import type { SavedLLM } from '../services/api-types'

export interface LLMChoice {
  provider: string
  model_id: string
}

export interface EffectiveLLM extends LLMChoice {
  /** True when the lock replaced the caller's choice with the published default. */
  forcedByLock: boolean
}

const norm = (value: string | undefined | null) => (value ?? '').trim().toLowerCase()

/**
 * What a workflow or chat actually runs on when the deployment's LLM settings
 * are locked. Mirrors the server (resolveLockedLLM): under LLM_CONFIG_LOCKED a
 * saved choice only counts if the published list names it; product-profile
 * bindings take precedence, and other choices use the first published entry.
 * The UI used to show the saved choice regardless
 * (a workflow synced from another machine kept showing claude-sonnet-5 on a
 * Cursor-only deployment), so every model label should go through this.
 */
export function effectiveLLMUnderLock(
  choice: Partial<LLMChoice> | null | undefined,
  locked: boolean,
  published: SavedLLM[],
  source?: 'agent_profile',
): EffectiveLLM | null {
  const provider = choice?.provider?.trim() || ''
  const modelId = choice?.model_id?.trim() || ''
  const own: EffectiveLLM | null = provider && modelId ? { provider, model_id: modelId, forcedByLock: false } : null
  // Product profiles are operator-owned bindings and win under the lock,
  // matching the server's resolveLockedLLM agent_profile exception.
  if (own && source === 'agent_profile') return own
  if (!locked || published.length === 0) return own
  const allowed = published.some(
    (entry) => norm(entry.provider) === norm(provider) && norm(entry.model_id) === norm(modelId),
  )
  if (own && allowed) return own
  const fallback = published[0]
  if (!fallback?.provider || !fallback?.model_id) return own
  return { provider: fallback.provider, model_id: fallback.model_id, forcedByLock: true }
}

/**
 * Provider-only variant for surfaces that know the provider but not the model
 * (the composer's "Sending to …" / live-input labels). Under a lock the next
 * turn runs on the published default unless the caller's provider is itself
 * published or owned by a product profile, so that is what the label must say -- it read the workflow's
 * saved builder provider and announced "Sending to Claude Code" on a
 * deployment locked to Cursor (RTS, 2026-09-03).
 */
export function effectiveProviderUnderLock(
  provider: string | null | undefined,
  locked: boolean,
  published: SavedLLM[],
  source?: 'agent_profile',
): string | null {
  const own = provider?.trim() || null
  if (own && source === 'agent_profile') return own
  if (!locked || published.length === 0) return own
  if (own && published.some((entry) => norm(entry.provider) === norm(own))) return own
  return published[0]?.provider?.trim() || own
}
