import { useEffect } from 'react'
import { useLLMStore } from '../stores/useLLMStore'

/**
 * Hook to automatically load LLM defaults from backend on app startup
 * This replaces hardcoded defaults with backend configuration
 */
export function useLLMDefaults() {
  const { defaultsLoaded, loadDefaultsFromBackend, loadDelegationTierDefaults, error } = useLLMStore()

  useEffect(() => {
    if (!defaultsLoaded) {
      loadDefaultsFromBackend()
    }
    loadDelegationTierDefaults()
  }, [defaultsLoaded, loadDefaultsFromBackend, loadDelegationTierDefaults])

  return {
    defaultsLoaded,
    error,
    isLoading: !defaultsLoaded && !error
  }
}
