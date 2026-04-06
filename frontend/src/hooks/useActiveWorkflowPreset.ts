import { useGlobalPresetStore } from '../stores/useGlobalPresetStore'
import type { CustomPreset } from '../types/preset'

/**
 * Hook to get the active workflow preset.
 * Workflows are file-backed manifests, not DB presets.
 * Use this instead of manually looking up customPresets/predefinedPresets.
 */
export function useActiveWorkflowPreset(): CustomPreset | null {
  const activePresetId = useGlobalPresetStore(state => state.activePresetIds.workflow)
  const getActivePreset = useGlobalPresetStore(state => state.getActivePreset)

  if (!activePresetId) return null
  return getActivePreset('workflow') as CustomPreset | null
}
