export const LLM_DISCOVERY_ONBOARDING_DISMISSED_KEY = 'llm_discovery_onboarding_dismissed'
export const WORKFLOW_WALKTHROUGH_DISMISSED_KEY = 'workflow_walkthrough_dismissed'

export const LLM_DISCOVERY_ONBOARDING_OPENED_EVENT = 'llm-discovery-onboarding-opened'
export const LLM_DISCOVERY_ONBOARDING_CLEARED_EVENT = 'llm-discovery-onboarding-cleared'

type LLMDiscoveryOnboardingState = 'pending' | 'open' | 'cleared'

const getWindowState = (): LLMDiscoveryOnboardingState | undefined => {
  if (typeof window === 'undefined') return undefined
  return (window as Window & { __llmDiscoveryOnboardingState?: LLMDiscoveryOnboardingState }).__llmDiscoveryOnboardingState
}

const setWindowState = (state: LLMDiscoveryOnboardingState) => {
  if (typeof window === 'undefined') return
  ;(window as Window & { __llmDiscoveryOnboardingState?: LLMDiscoveryOnboardingState }).__llmDiscoveryOnboardingState = state
}

const getStorageValue = (key: string): string | null => {
  if (typeof window === 'undefined') return null
  try {
    return window.localStorage.getItem(key)
  } catch {
    return null
  }
}

const setStorageValue = (key: string, value: string) => {
  if (typeof window === 'undefined') return
  try {
    window.localStorage.setItem(key, value)
  } catch {
    // Storage may be unavailable; in-memory onboarding state still works.
  }
}

export const isLLMDiscoveryOnboardingDismissed = () =>
  getStorageValue(LLM_DISCOVERY_ONBOARDING_DISMISSED_KEY) === 'true'

export const dismissLLMDiscoveryOnboarding = () => {
  setStorageValue(LLM_DISCOVERY_ONBOARDING_DISMISSED_KEY, 'true')
}

export const isWorkflowWalkthroughDismissed = () =>
  getStorageValue(WORKFLOW_WALKTHROUGH_DISMISSED_KEY) === 'true'

export const dismissWorkflowWalkthrough = () => {
  setStorageValue(WORKFLOW_WALKTHROUGH_DISMISSED_KEY, 'true')
}

export const getLLMDiscoveryOnboardingState = (): LLMDiscoveryOnboardingState => {
  const state = getWindowState()
  if (state) return state
  return isLLMDiscoveryOnboardingDismissed() ? 'cleared' : 'pending'
}

export const markLLMDiscoveryOnboardingOpen = () => {
  setWindowState('open')
  if (typeof window === 'undefined') return
  window.dispatchEvent(new Event(LLM_DISCOVERY_ONBOARDING_OPENED_EVENT))
}

export const markLLMDiscoveryOnboardingCleared = () => {
  setWindowState('cleared')
  if (typeof window === 'undefined') return
  window.dispatchEvent(new Event(LLM_DISCOVERY_ONBOARDING_CLEARED_EVENT))
}
