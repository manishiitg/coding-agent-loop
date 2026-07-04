import { create } from 'zustand'
import { persist } from 'zustand/middleware'

export const DEFAULT_IMAGE_GEN_MODEL_ID = 'gemini-3.1-flash-image'

const LEGACY_IMAGE_MODEL_ALIASES: Record<string, string> = {
  'gemini-3.1-flash-image-preview': 'gemini-3.1-flash-image',
  'gemini-3-pro-image-preview': 'gemini-3-pro-image',
}

export function normalizeImageGenModelId(modelId: string): string {
  const normalized = modelId.trim().toLowerCase()
  if (!normalized) return DEFAULT_IMAGE_GEN_MODEL_ID
  if (normalized.startsWith('imagen-')) return DEFAULT_IMAGE_GEN_MODEL_ID
  return LEGACY_IMAGE_MODEL_ALIASES[normalized] ?? modelId
}

export interface ImageGenConfig {
  provider: string  // e.g. 'vertex' or 'codex-cli'
  modelId: string   // e.g. 'gemini-3.1-flash-image' or 'gpt-5.4-mini'
  apiKey: string    // provider-specific API key override
}

interface ImageGenStore {
  config: ImageGenConfig
  setConfig: (config: Partial<ImageGenConfig>) => void
}

export const useImageGenStore = create<ImageGenStore>()(
  persist(
    (set) => ({
      config: {
        provider: 'vertex',
        modelId: DEFAULT_IMAGE_GEN_MODEL_ID,
        apiKey: '',
      },
      setConfig: (config) =>
        set((state) => {
          const next = { ...state.config, ...config }
          if (next.provider === 'vertex') {
            next.modelId = normalizeImageGenModelId(next.modelId)
          }
          return { config: next }
        }),
    }),
    {
      name: 'image-gen-store',
      version: 2,
      migrate: (persistedState) => {
        const state = persistedState as Partial<ImageGenStore> | undefined
        if (!state?.config) return persistedState
        return {
          ...state,
          config: {
            ...state.config,
            modelId: state.config.provider === 'vertex'
              ? normalizeImageGenModelId(state.config.modelId)
              : state.config.modelId,
          },
        }
      },
    }
  )
)
