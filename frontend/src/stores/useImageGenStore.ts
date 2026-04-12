import { create } from 'zustand'
import { persist } from 'zustand/middleware'

export interface ImageGenConfig {
  provider: string  // e.g. 'vertex' or 'minimax-coding-plan'
  modelId: string   // e.g. 'gemini-3.1-flash-image-preview' or 'image-01'
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
        modelId: 'gemini-3.1-flash-image-preview',
        apiKey: '',
      },
      setConfig: (config) =>
        set((state) => ({
          config: { ...state.config, ...config },
        })),
    }),
    {
      name: 'image-gen-store',
    }
  )
)
