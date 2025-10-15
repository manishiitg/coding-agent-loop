import { useEffect } from 'react'
import { useLLMStore } from '../stores/useLLMStore'

/**
 * Hook to automatically load LLM defaults from backend on app startup
 * This replaces hardcoded defaults with backend configuration
 */
export function useLLMDefaults() {
  const { defaultsLoaded, loadDefaultsFromBackend, error } = useLLMStore()

  useEffect(() => {
    // Only load defaults if not already loaded
    if (!defaultsLoaded) {
      loadDefaultsFromBackend()
    }
  }, [defaultsLoaded, loadDefaultsFromBackend])

  return {
    defaultsLoaded,
    error,
    isLoading: !defaultsLoaded && !error
  }
}
